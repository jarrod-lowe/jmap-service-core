# SSM Parameters for plugin infrastructure discovery
#
# Plugins can use aws_ssm_parameter data sources to discover these values
# instead of hardcoding or cross-referencing terraform state.

resource "aws_ssm_parameter" "api_gateway_execution_arn" {
  name        = "/${local.resource_prefix}/${var.environment}/api-gateway-execution-arn"
  type        = "String"
  value       = aws_api_gateway_rest_api.api.execution_arn
  description = "API Gateway execution ARN for Lambda permission source_arn"

  tags = {
    Name        = "${local.resource_prefix}-api-gateway-execution-arn-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
  }
}

resource "aws_ssm_parameter" "api_url" {
  name        = "/${local.resource_prefix}/${var.environment}/api-url"
  type        = "String"
  value       = "https://${var.domain_name}"
  description = "Public API URL via CloudFront/custom domain"

  tags = {
    Name        = "${local.resource_prefix}-api-url-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
  }
}

resource "aws_ssm_parameter" "dynamodb_table_name" {
  name        = "/${local.resource_prefix}/${var.environment}/dynamodb-table-name"
  type        = "String"
  value       = aws_dynamodb_table.jmap_data.name
  description = "Core DynamoDB table name for plugin registration"

  tags = {
    Name        = "${local.resource_prefix}-dynamodb-table-name-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
  }
}

resource "aws_ssm_parameter" "dynamodb_table_arn" {
  name        = "/${local.resource_prefix}/${var.environment}/dynamodb-table-arn"
  type        = "String"
  value       = aws_dynamodb_table.jmap_data.arn
  description = "Core DynamoDB table ARN for IAM policy resources"

  tags = {
    Name        = "${local.resource_prefix}-dynamodb-table-arn-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
  }
}
