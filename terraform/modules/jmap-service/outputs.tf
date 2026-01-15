output "hello_world_function_name" {
  description = "Name of the hello-world Lambda function"
  value       = aws_lambda_function.hello_world.function_name
}

output "hello_world_function_arn" {
  description = "ARN of the hello-world Lambda function"
  value       = aws_lambda_function.hello_world.arn
}

output "hello_world_function_url" {
  description = "Function URL for hello-world Lambda"
  value       = aws_lambda_function_url.hello_world.function_url
}

output "hello_world_log_group" {
  description = "CloudWatch log group for hello-world Lambda"
  value       = aws_cloudwatch_log_group.hello_world_logs.name
}

output "hello_world_role_arn" {
  description = "IAM role ARN for hello-world Lambda"
  value       = aws_iam_role.hello_world_execution.arn
}

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
