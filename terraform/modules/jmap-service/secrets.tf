# CloudFront Signing Key Management
#
# Uses Terraform's tls_private_key resource to auto-generate RSA key pairs
# for CloudFront signed URL validation. Supports zero-downtime key rotation
# via dual-key support during rotation phase.
#
# Key rotation phases:
#   - "normal": Single current key active
#   - "rotating": Both current and previous keys active (allows old URLs to still work)
#   - "complete": Rotation finished, ready to return to normal

# =============================================================================
# TLS Private Key Generation
# =============================================================================

# Current signing key - always exists
resource "tls_private_key" "cloudfront_signing_current" {
  algorithm = "RSA"
  rsa_bits  = 2048
}

# Previous signing key - only exists during rotation phase
# This allows signed URLs created with the old key to continue working
resource "tls_private_key" "cloudfront_signing_previous" {
  count     = var.cloudfront_signing_key_rotation_phase == "rotating" ? 1 : 0
  algorithm = "RSA"
  rsa_bits  = 2048
}

# =============================================================================
# Secrets Manager - Store Private Key
# =============================================================================

resource "aws_secretsmanager_secret" "cloudfront_private_key" {
  name        = "${local.resource_prefix}-cloudfront-private-key-${var.environment}"
  description = "CloudFront signed URL private key for blob downloads (auto-generated)"

  tags = {
    Name        = "${local.resource_prefix}-cloudfront-private-key-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
  }
}

# Store the current private key in Secrets Manager
resource "aws_secretsmanager_secret_version" "cloudfront_private_key" {
  secret_id     = aws_secretsmanager_secret.cloudfront_private_key.id
  secret_string = tls_private_key.cloudfront_signing_current.private_key_pem
}

# =============================================================================
# SSM Parameter - Track Key Creation Time
# =============================================================================

# Store when the key was created for age monitoring
resource "aws_ssm_parameter" "cloudfront_key_created_at" {
  name        = "/${local.resource_prefix}/${var.environment}/cloudfront-key-created-at"
  type        = "String"
  value       = timestamp()
  description = "Timestamp when the CloudFront signing key was created"

  # Only set on initial creation - updated manually during rotation by running:
  # aws ssm put-parameter --name <name> --value $(date -u +%Y-%m-%dT%H:%M:%SZ) --overwrite
  lifecycle {
    ignore_changes = [value]
  }

  tags = {
    Name        = "${local.resource_prefix}-cloudfront-key-created-at-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
  }
}
