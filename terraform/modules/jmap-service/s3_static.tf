# S3 bucket for static documentation content

resource "aws_s3_bucket" "static_docs" {
  bucket = "${local.resource_prefix}-static-docs-${var.environment}-${data.aws_caller_identity.current.account_id}"

  tags = {
    Name = "${local.resource_prefix}-static-docs-${var.environment}"
  }
}

resource "aws_s3_bucket_public_access_block" "static_docs" {
  bucket                  = aws_s3_bucket.static_docs.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_server_side_encryption_configuration" "static_docs" {
  bucket = aws_s3_bucket.static_docs.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

# OAC for CloudFront access
resource "aws_cloudfront_origin_access_control" "static_docs" {
  name                              = "${local.resource_prefix}-static-docs-oac-${var.environment}"
  description                       = "OAC for static documentation access"
  origin_access_control_origin_type = "s3"
  signing_behavior                  = "always"
  signing_protocol                  = "sigv4"
}

data "aws_iam_policy_document" "static_docs_cloudfront_access" {
  statement {
    sid    = "AllowCloudFrontServicePrincipal"
    effect = "Allow"

    principals {
      type        = "Service"
      identifiers = ["cloudfront.amazonaws.com"]
    }

    actions   = ["s3:GetObject"]
    resources = ["${aws_s3_bucket.static_docs.arn}/*"]

    condition {
      test     = "StringEquals"
      variable = "AWS:SourceArn"
      values   = [aws_cloudfront_distribution.api.arn]
    }
  }
}

resource "aws_s3_bucket_policy" "static_docs" {
  bucket = aws_s3_bucket.static_docs.id
  policy = data.aws_iam_policy_document.static_docs_cloudfront_access.json
}

# Upload rendered extension docs - auto-discovers all .txt files in build/docs/
resource "aws_s3_object" "extension_docs" {
  for_each     = fileset("${path.module}/../../../build/docs", "*.txt")
  bucket       = aws_s3_bucket.static_docs.id
  key          = "extensions/${trimsuffix(each.value, ".txt")}"
  source       = "${path.module}/../../../build/docs/${each.value}"
  content_type = "text/plain; charset=utf-8"
  etag         = filemd5("${path.module}/../../../build/docs/${each.value}")
}

# Invalidate CloudFront cache for /extensions/* when docs change
action "aws_cloudfront_create_invalidation" "extensions" {
  config {
    distribution_id = aws_cloudfront_distribution.api.id
    paths           = ["/extensions/*"]
  }
}

resource "terraform_data" "extension_docs_invalidation" {
  depends_on = [aws_s3_object.extension_docs]

  triggers_replace = jsonencode({
    for f in fileset("${path.module}/../../../build/docs", "*.txt") :
    f => filemd5("${path.module}/../../../build/docs/${f}")
  })

  lifecycle {
    action_trigger {
      events  = [after_create]
      actions = [action.aws_cloudfront_create_invalidation.extensions]
    }
  }
}
