# API Gateway outputs
output "api_gateway_id" {
  description = "ID of the REST API Gateway"
  value       = aws_api_gateway_rest_api.api.id
}

output "api_gateway_invoke_url" {
  description = "Invoke URL for API Gateway stage"
  value       = aws_api_gateway_stage.v1.invoke_url
}

# CloudFront outputs
output "cloudfront_distribution_id" {
  description = "ID of the CloudFront distribution"
  value       = aws_cloudfront_distribution.api.id
}

output "cloudfront_domain_name" {
  description = "CloudFront domain name for A/ALIAS record"
  value       = aws_cloudfront_distribution.api.domain_name
}

output "cloudfront_hosted_zone_id" {
  description = "CloudFront hosted zone ID for ALIAS record"
  value       = aws_cloudfront_distribution.api.hosted_zone_id
}

# ACM certificate outputs for DNS validation
output "acm_certificate_arn" {
  description = "ARN of the ACM certificate"
  value       = aws_acm_certificate.api.arn
}

output "acm_validation_records" {
  description = "DNS CNAME records required for ACM certificate validation"
  value = {
    for dvo in aws_acm_certificate.api.domain_validation_options : dvo.domain_name => {
      name  = dvo.resource_record_name
      type  = dvo.resource_record_type
      value = dvo.resource_record_value
    }
  }
}

# Convenience output
output "api_endpoint" {
  description = "Full API endpoint URL via custom domain"
  value       = "https://${var.domain_name}/health"
}

# Cognito outputs
output "cognito_user_pool_id" {
  description = "ID of the Cognito User Pool"
  value       = aws_cognito_user_pool.main.id
}

output "cognito_user_pool_arn" {
  description = "ARN of the Cognito User Pool"
  value       = aws_cognito_user_pool.main.arn
}

output "cognito_client_id" {
  description = "ID of the Cognito App Client"
  value       = aws_cognito_user_pool_client.jmap_client.id
}

output "cognito_domain" {
  description = "Cognito User Pool domain for OAuth"
  value       = aws_cognito_user_pool_domain.main.domain
}

output "cognito_oauth_endpoint" {
  description = "OAuth 2.0 authorization endpoint"
  value       = "https://${aws_cognito_user_pool_domain.main.domain}.auth.${var.aws_region}.amazoncognito.com"
}

# get-jmap-session Lambda outputs
output "get_jmap_session_function_name" {
  description = "Name of the get-jmap-session Lambda function"
  value       = aws_lambda_function.get_jmap_session.function_name
}

output "get_jmap_session_function_arn" {
  description = "ARN of the get-jmap-session Lambda function"
  value       = aws_lambda_function.get_jmap_session.arn
}

# JMAP session endpoint
output "jmap_session_endpoint" {
  description = "JMAP session discovery endpoint"
  value       = "https://${var.domain_name}/.well-known/jmap"
}

# JMAP host for client configuration
output "jmap_host" {
  description = "JMAP hostname for client configuration"
  value       = var.domain_name
}

# Blob download infrastructure outputs
output "cloudfront_private_key_secret_arn" {
  description = "ARN of the Secrets Manager secret for CloudFront private key"
  value       = aws_secretsmanager_secret.cloudfront_private_key.arn
}

output "cloudfront_key_pair_id" {
  description = "CloudFront key pair ID for signed URL generation"
  value       = aws_cloudfront_public_key.blob_signing_current.id
}

# Storage outputs for e2e tests
output "blob_bucket_name" {
  description = "Name of the S3 bucket for blob storage"
  value       = aws_s3_bucket.blobs.bucket
}

output "dynamodb_table_name" {
  description = "Name of the DynamoDB table"
  value       = aws_dynamodb_table.jmap_data.name
}

# CloudWatch Dashboard outputs
output "dashboard_url" {
  description = "URL to the CloudWatch dashboard"
  value       = "https://${var.aws_region}.console.aws.amazon.com/cloudwatch/home?region=${var.aws_region}#dashboards:name=${aws_cloudwatch_dashboard.main.dashboard_name}"
}

# Resource Group outputs
output "resource_group_name" {
  description = "Name of the AWS Resource Group"
  value       = aws_resourcegroups_group.jmap_service.name
}

output "resource_group_url" {
  description = "URL to view the Resource Group in AWS Console"
  value       = "https://console.aws.amazon.com/resource-groups/group/${aws_resourcegroups_group.jmap_service.name}"
}

# E2E test client
output "e2e_test_role_arn" {
  description = "ARN of the IAM role for e2e test client"
  value       = aws_iam_role.e2e_test_client.arn
}
