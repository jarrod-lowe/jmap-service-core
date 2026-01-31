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

# Lambda function for jmap-api (/jmap and /jmap-iam/{accountId} endpoints)
resource "aws_lambda_function" "jmap_api" {
  filename         = "${path.module}/../../../build/jmap-api/lambda.zip"
  function_name    = "${local.resource_prefix}-jmap-api-${var.environment}"
  role             = aws_iam_role.jmap_api_execution.arn
  handler          = "bootstrap"
  source_code_hash = filebase64sha256("${path.module}/../../../build/jmap-api/lambda.zip")
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  timeout          = 29 # 29s to allow 25s plugin timeout + buffer
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

      # Blob/allocate configuration
      BLOB_BUCKET                   = aws_s3_bucket.blobs.bucket
      MAX_SIZE_UPLOAD_PUT           = tostring(var.max_size_upload_put)
      MAX_PENDING_ALLOCATIONS       = tostring(var.max_pending_allocations)
      ALLOCATION_URL_EXPIRY_SECONDS = tostring(var.allocation_url_expiry_seconds)

      # ADOT Collector Configuration
      OPENTELEMETRY_COLLECTOR_CONFIG_URI = "file:///var/task/collector.yaml"
      OPENTELEMETRY_EXTENSION_LOG_LEVEL  = "error"

      # OpenTelemetry SDK Configuration
      OTEL_SERVICE_NAME   = "${local.resource_prefix}-jmap-api-${var.environment}"
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
    aws_iam_role_policy_attachment.jmap_api_basic_execution,
    aws_iam_role_policy_attachment.jmap_api_xray_access,
    aws_iam_role_policy.jmap_api_cloudwatch_metrics,
    aws_iam_role_policy.jmap_api_dynamodb,
    aws_iam_role_policy.jmap_api_lambda_invoke,
    aws_iam_role_policy.jmap_api_s3_presign,
    aws_cloudwatch_log_group.jmap_api_logs
  ]

  tags = {
    Name        = "${local.resource_prefix}-jmap-api-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
    Function    = "jmap-api"
  }
}

# API Gateway permission to invoke jmap-api Lambda
resource "aws_lambda_permission" "jmap_api_apigw" {
  statement_id  = "AllowAPIGatewayInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.jmap_api.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.api.execution_arn}/*"
}
