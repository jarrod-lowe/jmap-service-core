# S3 bucket for blob storage (uploads)

resource "aws_s3_bucket" "blobs" {
  bucket = "${local.resource_prefix}-blobs-${var.environment}-${data.aws_caller_identity.current.account_id}"

  tags = {
    Name = "${local.resource_prefix}-blobs-${var.environment}-${data.aws_caller_identity.current.account_id}"
  }
}

# Block all public access
resource "aws_s3_bucket_public_access_block" "blobs" {
  bucket = aws_s3_bucket.blobs.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# Bucket policy to allow CloudFront OAC access for blob downloads
data "aws_iam_policy_document" "blobs_cloudfront_access" {
  statement {
    sid    = "AllowCloudFrontServicePrincipal"
    effect = "Allow"

    principals {
      type        = "Service"
      identifiers = ["cloudfront.amazonaws.com"]
    }

    actions   = ["s3:GetObject"]
    resources = ["${aws_s3_bucket.blobs.arn}/*"]

    condition {
      test     = "StringEquals"
      variable = "AWS:SourceArn"
      values   = [aws_cloudfront_distribution.api.arn]
    }
  }
}

resource "aws_s3_bucket_policy" "blobs" {
  bucket = aws_s3_bucket.blobs.id
  policy = data.aws_iam_policy_document.blobs_cloudfront_access.json
}

# Server-side encryption with S3 managed keys
resource "aws_s3_bucket_server_side_encryption_configuration" "blobs" {
  bucket = aws_s3_bucket.blobs.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

# Suspend versioning to prevent version multiplication attacks
# The PUT upload extension requires versioning suspended so that repeated
# uploads to the same key don't accumulate versions
# Note: "Suspended" is used instead of "Disabled" because S3 doesn't allow
# transitioning from Enabled to Disabled on existing buckets
resource "aws_s3_bucket_versioning" "blobs" {
  bucket = aws_s3_bucket.blobs.id

  versioning_configuration {
    status = "Suspended"
  }
}

# CORS configuration for direct PUT uploads from browser clients
resource "aws_s3_bucket_cors_configuration" "blobs" {
  bucket = aws_s3_bucket.blobs.id

  cors_rule {
    allowed_headers = ["Content-Type", "Content-Length"]
    allowed_methods = ["PUT"]
    allowed_origins = var.cors_allowed_origins
    expose_headers  = ["ETag"]
    max_age_seconds = 3600
  }
}

# Request metrics for CloudWatch dashboard
resource "aws_s3_bucket_metric" "blobs_all_requests" {
  bucket = aws_s3_bucket.blobs.bucket
  name   = "AllRequests"
}

# Lifecycle rules for blob management
resource "aws_s3_bucket_lifecycle_configuration" "blobs" {
  bucket     = aws_s3_bucket.blobs.id
  depends_on = [aws_s3_bucket_versioning.blobs]

  # Delete pending blobs after 7 days
  rule {
    id     = "delete-pending-blobs"
    status = "Enabled"

    filter {
      tag {
        key   = "Status"
        value = "pending"
      }
    }

    expiration {
      days = 7
    }
  }

  # Abort incomplete multipart uploads after 7 days
  rule {
    id     = "abort-incomplete-multipart-uploads"
    status = "Enabled"

    filter {
      prefix = ""
    }

    abort_incomplete_multipart_upload {
      days_after_initiation = 7
    }
  }

}
