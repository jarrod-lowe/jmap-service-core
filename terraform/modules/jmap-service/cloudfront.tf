# ACM Certificate (must be in us-east-1 for CloudFront)

resource "aws_acm_certificate" "api" {
  provider          = aws.us_east_1
  domain_name       = var.domain_name
  validation_method = "DNS"

  lifecycle {
    create_before_destroy = true
  }

  tags = {
    Name = "jmap-service-${var.environment}"
  }
}

# Certificate validation - waits for DNS records to be added externally
# Terraform will pause here until the certificate is validated
resource "aws_acm_certificate_validation" "api" {
  provider        = aws.us_east_1
  certificate_arn = aws_acm_certificate.api.arn

  timeouts {
    create = "45m"
  }
}

# CloudFront Function for JMAP path rewrite
# Rewrites /.well-known/jmap to /{stage}/.well-known/jmap for API Gateway
resource "aws_cloudfront_function" "jmap_redirect" {
  name    = "jmap-well-known-redirect-${var.environment}"
  runtime = "cloudfront-js-2.0"
  publish = true
  code = templatefile("${path.module}/cloudfront-functions/jmap-path-rewrite.js.tftpl", {
    stage_name = aws_api_gateway_stage.v1.stage_name
  })
}

# CloudFront Function for blob path rewrite
# Strips /blobs prefix so /blobs/accountId/blobId becomes /accountId/blobId for S3
resource "aws_cloudfront_function" "blob_path_rewrite" {
  name    = "blob-path-rewrite-${var.environment}"
  runtime = "cloudfront-js-2.0"
  publish = true
  code    = file("${path.module}/cloudfront-functions/blob-path-rewrite.js")
}

# =============================================================================
# CloudFront Key Pair for Signed URLs
# =============================================================================

# CloudFront public key for signed URL validation (current key - always exists)
# The corresponding private key is auto-generated and stored in Secrets Manager
resource "aws_cloudfront_public_key" "blob_signing_current" {
  name        = "blob-signing-key-current-${var.environment}"
  comment     = "Current signing key for blob download signed URLs"
  encoded_key = tls_private_key.cloudfront_signing_current.public_key_pem
}

# CloudFront public key for previous key (only during rotation)
# This allows signed URLs created with the old key to continue working
resource "aws_cloudfront_public_key" "blob_signing_previous" {
  count       = var.cloudfront_signing_key_rotation_phase == "rotating" ? 1 : 0
  name        = "blob-signing-key-previous-${var.environment}"
  comment     = "Previous signing key (rotation in progress)"
  encoded_key = tls_private_key.cloudfront_signing_previous[0].public_key_pem
}

resource "aws_cloudfront_key_group" "blob_signing" {
  name    = "blob-signing-key-group-${var.environment}"
  comment = "Key group for blob download signed URLs"
  items = concat(
    [aws_cloudfront_public_key.blob_signing_current.id],
    var.cloudfront_signing_key_rotation_phase == "rotating" ? [aws_cloudfront_public_key.blob_signing_previous[0].id] : []
  )
}

# =============================================================================
# Origin Access Control for S3
# =============================================================================

resource "aws_cloudfront_origin_access_control" "blobs" {
  name                              = "${local.resource_prefix}-blobs-oac-${var.environment}"
  description                       = "OAC for blob bucket access"
  origin_access_control_origin_type = "s3"
  signing_behavior                  = "always"
  signing_protocol                  = "sigv4"
}

# =============================================================================
# CloudFront Distribution
# =============================================================================

resource "aws_cloudfront_distribution" "api" {
  enabled         = true
  comment         = "JMAP Service API - ${var.environment}"
  aliases         = [var.domain_name]
  price_class     = "PriceClass_All"
  http_version    = "http2and3"
  is_ipv6_enabled = true

  # API Gateway origin
  origin {
    domain_name = "${aws_api_gateway_rest_api.api.id}.execute-api.${var.aws_region}.amazonaws.com"
    origin_id   = "api-gateway"
    # Note: No origin_path - CloudFront functions rewrite paths to add stage prefix

    custom_origin_config {
      http_port              = 80
      https_port             = 443
      origin_protocol_policy = "https-only"
      origin_ssl_protocols   = ["TLSv1.2"]
    }
  }

  # S3 origin for blob storage (accessed via signed URLs)
  origin {
    domain_name              = aws_s3_bucket.blobs.bucket_regional_domain_name
    origin_id                = "s3-blobs"
    origin_access_control_id = aws_cloudfront_origin_access_control.blobs.id
  }

  # Blob download path - requires signed URL
  # Path pattern: /blobs/{accountId}/{blobId}
  ordered_cache_behavior {
    path_pattern           = "/blobs/*"
    target_origin_id       = "s3-blobs"
    viewer_protocol_policy = "redirect-to-https"
    allowed_methods        = ["GET", "HEAD"]
    cached_methods         = ["GET", "HEAD"]
    compress               = true

    # Use managed cache policy for S3 origin
    cache_policy_id = data.aws_cloudfront_cache_policy.caching_optimized.id

    # Require signed URLs
    trusted_key_groups = [aws_cloudfront_key_group.blob_signing.id]

    # Rewrite path to strip /blobs prefix before sending to S3
    function_association {
      event_type   = "viewer-request"
      function_arn = aws_cloudfront_function.blob_path_rewrite.arn
    }
  }

  # Rewrite /.well-known/jmap to /{stage}/.well-known/jmap (adds API Gateway stage prefix)
  ordered_cache_behavior {
    path_pattern           = "/.well-known/jmap"
    target_origin_id       = "api-gateway"
    viewer_protocol_policy = "redirect-to-https"
    allowed_methods        = ["GET", "HEAD", "OPTIONS"]
    cached_methods         = ["GET", "HEAD"]
    compress               = true

    cache_policy_id            = data.aws_cloudfront_cache_policy.caching_disabled.id
    origin_request_policy_id   = data.aws_cloudfront_origin_request_policy.all_viewer_except_host_header.id
    response_headers_policy_id = data.aws_cloudfront_response_headers_policy.cors_preflight.id

    function_association {
      event_type   = "viewer-request"
      function_arn = aws_cloudfront_function.jmap_redirect.arn
    }
  }

  default_cache_behavior {
    target_origin_id       = "api-gateway"
    viewer_protocol_policy = "redirect-to-https"
    allowed_methods        = ["DELETE", "GET", "HEAD", "OPTIONS", "PATCH", "POST", "PUT"]
    cached_methods         = ["GET", "HEAD"]
    compress               = true

    # Disable caching for API responses
    cache_policy_id            = data.aws_cloudfront_cache_policy.caching_disabled.id
    origin_request_policy_id   = data.aws_cloudfront_origin_request_policy.all_viewer_except_host_header.id
    response_headers_policy_id = data.aws_cloudfront_response_headers_policy.cors_preflight.id
  }

  viewer_certificate {
    acm_certificate_arn      = aws_acm_certificate_validation.api.certificate_arn
    ssl_support_method       = "sni-only"
    minimum_protocol_version = "TLSv1.2_2021"
  }

  restrictions {
    geo_restriction {
      restriction_type = "none"
    }
  }

  # Note: Logging disabled for $0 plan. Enable logging_config block if needed.

  tags = {
    Name = "jmap-service-${var.environment}"
  }
}

# Managed cache policies
data "aws_cloudfront_cache_policy" "caching_disabled" {
  name = "Managed-CachingDisabled"
}

data "aws_cloudfront_cache_policy" "caching_optimized" {
  name = "Managed-CachingOptimized"
}

data "aws_cloudfront_origin_request_policy" "all_viewer_except_host_header" {
  name = "Managed-AllViewerExceptHostHeader"
}

data "aws_cloudfront_response_headers_policy" "cors_preflight" {
  name = "Managed-CORS-With-Preflight"
}
