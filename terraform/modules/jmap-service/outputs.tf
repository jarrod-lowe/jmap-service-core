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
