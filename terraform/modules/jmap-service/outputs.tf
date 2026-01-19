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
