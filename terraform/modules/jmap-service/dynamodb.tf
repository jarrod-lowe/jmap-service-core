# DynamoDB table for JMAP service (single-table design)
resource "aws_dynamodb_table" "jmap_data" {
  name         = "${local.resource_prefix}-data-${var.environment}"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "pk"
  range_key    = "sk"

  attribute {
    name = "pk"
    type = "S"
  }

  attribute {
    name = "sk"
    type = "S"
  }

  stream_enabled   = true
  stream_view_type = "NEW_AND_OLD_IMAGES"

  point_in_time_recovery {
    enabled = true
  }

  tags = {
    Name        = "${local.resource_prefix}-data-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
  }
}
