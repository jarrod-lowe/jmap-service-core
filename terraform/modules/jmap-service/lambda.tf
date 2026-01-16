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

# Lambda function for get-jmap-session (/.well-known/jmap endpoint)
resource "aws_lambda_function" "get_jmap_session" {
  filename         = "${path.module}/../../../build/get-jmap-session/lambda.zip"
  function_name    = "${local.resource_prefix}-get-jmap-session-${var.environment}"
  role             = aws_iam_role.get_jmap_session_execution.arn
  handler          = "bootstrap"
  source_code_hash = filebase64sha256("${path.module}/../../../build/get-jmap-session/lambda.zip")
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
      ENVIRONMENT    = var.environment
      API_DOMAIN     = var.domain_name
      DYNAMODB_TABLE = aws_dynamodb_table.jmap_data.name

      # ADOT Collector Configuration
      OPENTELEMETRY_COLLECTOR_CONFIG_URI = "file:///var/task/collector.yaml"
      OPENTELEMETRY_EXTENSION_LOG_LEVEL  = "error"

      # OpenTelemetry SDK Configuration
      OTEL_SERVICE_NAME   = "${local.resource_prefix}-get-jmap-session-${var.environment}"
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
    aws_iam_role_policy_attachment.get_jmap_session_basic_execution,
    aws_iam_role_policy_attachment.get_jmap_session_xray_access,
    aws_iam_role_policy.get_jmap_session_cloudwatch_metrics,
    aws_iam_role_policy.get_jmap_session_dynamodb,
    aws_cloudwatch_log_group.get_jmap_session_logs
  ]

  tags = {
    Name        = "${local.resource_prefix}-get-jmap-session-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
    Function    = "get-jmap-session"
  }
}

# API Gateway permission to invoke get-jmap-session Lambda
resource "aws_lambda_permission" "get_jmap_session_apigw" {
  statement_id  = "AllowAPIGatewayInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.get_jmap_session.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.api.execution_arn}/*"
}
