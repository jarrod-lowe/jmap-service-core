# Lambda function for hello-world
resource "aws_lambda_function" "hello_world" {
  filename         = "${path.module}/../../../build/hello-world/lambda.zip"
  function_name    = "${local.resource_prefix}-hello-world-${var.environment}"
  role             = aws_iam_role.hello_world_execution.arn
  handler          = "bootstrap"
  source_code_hash = filebase64sha256("${path.module}/../../../build/hello-world/lambda.zip")
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  timeout          = var.lambda_timeout
  memory_size      = var.lambda_memory_size

  # Add ADOT Collector layer for OpenTelemetry sidecar
  layers = [local.adot_layer_arn]

  # Enable X-Ray tracing (ADOT Collector exports to X-Ray)
  tracing_config {
    mode = "Active"
  }

  environment {
    variables = {
      ENVIRONMENT = var.environment

      # ADOT Collector Configuration
      OPENTELEMETRY_COLLECTOR_CONFIG_URI = "file:///var/task/collector.yaml"
      OPENTELEMETRY_EXTENSION_LOG_LEVEL  = "error"

      # OpenTelemetry SDK Configuration
      OTEL_SERVICE_NAME   = "${local.resource_prefix}-hello-world-${var.environment}"
      OTEL_TRACES_SAMPLER = "always_on"

      # CRITICAL: Include xray propagator for Lambda context extraction
      OTEL_PROPAGATORS = "tracecontext,baggage,xray"

      # OTLP Exporter Configuration (points to ADOT Collector)
      OTEL_EXPORTER_OTLP_ENDPOINT = "http://localhost:4317"
      OTEL_EXPORTER_OTLP_PROTOCOL = "grpc"

      # Resource attributes for better trace identification
      OTEL_RESOURCE_ATTRIBUTES = "service.version=1.0.0"
    }
  }

  depends_on = [
    aws_iam_role_policy_attachment.hello_world_basic_execution,
    aws_iam_role_policy_attachment.hello_world_xray_access,
    aws_iam_role_policy.hello_world_cloudwatch_metrics,
    aws_cloudwatch_log_group.hello_world_logs
  ]

  tags = {
    Name        = "${local.resource_prefix}-hello-world-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
    Function    = "hello-world"
  }
}

# Lambda Function URL (for easy testing without API Gateway)
resource "aws_lambda_function_url" "hello_world" {
  function_name      = aws_lambda_function.hello_world.function_name
  authorization_type = "NONE"

  cors {
    allow_origins = ["*"]
    allow_methods = ["GET", "POST"]
    max_age       = 86400
  }
}
