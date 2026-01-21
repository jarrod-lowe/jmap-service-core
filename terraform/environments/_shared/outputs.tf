# API Gateway outputs
output "api_gateway_id" {
  description = "ID of the REST API Gateway"
  value       = module.jmap_service.api_gateway_id
}

output "api_gateway_invoke_url" {
  description = "Invoke URL for API Gateway stage"
  value       = module.jmap_service.api_gateway_invoke_url
}

# CloudFront outputs
output "cloudfront_distribution_id" {
  description = "ID of the CloudFront distribution"
  value       = module.jmap_service.cloudfront_distribution_id
}

output "cloudfront_domain_name" {
  description = "CloudFront domain name for A/ALIAS record"
  value       = module.jmap_service.cloudfront_domain_name
}

output "cloudfront_hosted_zone_id" {
  description = "CloudFront hosted zone ID for ALIAS record"
  value       = module.jmap_service.cloudfront_hosted_zone_id
}

# ACM certificate outputs for DNS validation
output "acm_certificate_arn" {
  description = "ARN of the ACM certificate"
  value       = module.jmap_service.acm_certificate_arn
}

output "acm_validation_records" {
  description = "DNS CNAME records required for ACM certificate validation"
  value       = module.jmap_service.acm_validation_records
}

# Convenience output
output "api_endpoint" {
  description = "Full API endpoint URL via custom domain"
  value       = module.jmap_service.api_endpoint
}

# Cognito outputs
output "cognito_user_pool_id" {
  description = "ID of the Cognito User Pool"
  value       = module.jmap_service.cognito_user_pool_id
}

output "cognito_user_pool_arn" {
  description = "ARN of the Cognito User Pool"
  value       = module.jmap_service.cognito_user_pool_arn
}

output "cognito_client_id" {
  description = "ID of the Cognito App Client"
  value       = module.jmap_service.cognito_client_id
}

output "cognito_domain" {
  description = "Cognito User Pool domain for OAuth"
  value       = module.jmap_service.cognito_domain
}

output "cognito_oauth_endpoint" {
  description = "OAuth 2.0 authorization endpoint"
  value       = module.jmap_service.cognito_oauth_endpoint
}

# get-jmap-session Lambda outputs
output "get_jmap_session_function_name" {
  description = "Name of the get-jmap-session Lambda function"
  value       = module.jmap_service.get_jmap_session_function_name
}

output "get_jmap_session_function_arn" {
  description = "ARN of the get-jmap-session Lambda function"
  value       = module.jmap_service.get_jmap_session_function_arn
}

# JMAP session endpoint
output "jmap_session_endpoint" {
  description = "JMAP session discovery endpoint"
  value       = module.jmap_service.jmap_session_endpoint
}

# JMAP host for client configuration
output "jmap_host" {
  description = "JMAP hostname for client configuration"
  value       = module.jmap_service.jmap_host
}

# Blob download infrastructure outputs
output "cloudfront_private_key_secret_arn" {
  description = "ARN of the Secrets Manager secret for CloudFront private key"
  value       = module.jmap_service.cloudfront_private_key_secret_arn
}

output "cloudfront_key_pair_id" {
  description = "CloudFront key pair ID for signed URL generation"
  value       = module.jmap_service.cloudfront_key_pair_id
}
