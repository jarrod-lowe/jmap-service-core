output "hello_world_function_name" {
  description = "Name of the hello-world Lambda function"
  value       = module.jmap_service.hello_world_function_name
}

output "hello_world_function_arn" {
  description = "ARN of the hello-world Lambda function"
  value       = module.jmap_service.hello_world_function_arn
}

output "hello_world_function_url" {
  description = "Function URL for hello-world Lambda"
  value       = module.jmap_service.hello_world_function_url
}

output "hello_world_log_group" {
  description = "CloudWatch log group for hello-world Lambda"
  value       = module.jmap_service.hello_world_log_group
}

output "hello_world_role_arn" {
  description = "IAM role ARN for hello-world Lambda"
  value       = module.jmap_service.hello_world_role_arn
}
