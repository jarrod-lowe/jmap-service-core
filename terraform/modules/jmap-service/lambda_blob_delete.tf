# Lambda function for blob-delete (DELETE /delete-iam/{accountId}/{blobId})
# Marks blobs as deleted by setting deletedAt on DynamoDB record (IAM auth only)

# =============================================================================
# CloudWatch Log Group
# =============================================================================

resource "aws_cloudwatch_log_group" "blob_delete_logs" {
  name              = "/aws/lambda/${local.resource_prefix}-blob-delete-${var.environment}"
  retention_in_days = var.log_retention_days

  tags = {
    Name        = "${local.resource_prefix}-blob-delete-logs-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
    Function    = "blob-delete"
  }
}

# =============================================================================
# IAM Role and Policies
# =============================================================================

resource "aws_iam_role" "blob_delete_execution" {
  name               = "${local.resource_prefix}-blob-delete-execution-${var.environment}"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume_role.json

  tags = {
    Name        = "${local.resource_prefix}-blob-delete-execution-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
    Function    = "blob-delete"
  }
}

# Attach AWS managed policy for basic Lambda execution
resource "aws_iam_role_policy_attachment" "blob_delete_basic_execution" {
  role       = aws_iam_role.blob_delete_execution.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

# Attach AWS managed policy for X-Ray tracing
resource "aws_iam_role_policy_attachment" "blob_delete_xray_access" {
  role       = aws_iam_role.blob_delete_execution.name
  policy_arn = "arn:aws:iam::aws:policy/AWSXRayDaemonWriteAccess"
}

# IAM policy for CloudWatch Metrics
resource "aws_iam_role_policy" "blob_delete_cloudwatch_metrics" {
  name   = "${local.resource_prefix}-blob-delete-cloudwatch-metrics-${var.environment}"
  role   = aws_iam_role.blob_delete_execution.id
  policy = data.aws_iam_policy_document.cloudwatch_metrics.json
}

# IAM policy for DynamoDB access (read blob records, update for marking deleted, and query for plugin registry)
data "aws_iam_policy_document" "blob_delete_dynamodb" {
  statement {
    effect = "Allow"
    actions = [
      "dynamodb:GetItem",
      "dynamodb:UpdateItem",
      "dynamodb:Query"
    ]
    resources = [aws_dynamodb_table.jmap_data.arn]
  }
}

resource "aws_iam_role_policy" "blob_delete_dynamodb" {
  name   = "${local.resource_prefix}-blob-delete-dynamodb-${var.environment}"
  role   = aws_iam_role.blob_delete_execution.id
  policy = data.aws_iam_policy_document.blob_delete_dynamodb.json
}

# =============================================================================
# Lambda Function
# =============================================================================

resource "aws_lambda_function" "blob_delete" {
  filename         = "${path.module}/../../../build/blob-delete/lambda.zip"
  function_name    = "${local.resource_prefix}-blob-delete-${var.environment}"
  role             = aws_iam_role.blob_delete_execution.arn
  handler          = "bootstrap"
  source_code_hash = filebase64sha256("${path.module}/../../../build/blob-delete/lambda.zip")
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
      DYNAMODB_TABLE = aws_dynamodb_table.jmap_data.name

      # ADOT Collector Configuration
      OPENTELEMETRY_COLLECTOR_CONFIG_URI = "file:///var/task/collector.yaml"
      OPENTELEMETRY_EXTENSION_LOG_LEVEL  = "error"

      # OpenTelemetry SDK Configuration
      OTEL_SERVICE_NAME   = "${local.resource_prefix}-blob-delete-${var.environment}"
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
    aws_iam_role_policy_attachment.blob_delete_basic_execution,
    aws_iam_role_policy_attachment.blob_delete_xray_access,
    aws_iam_role_policy.blob_delete_cloudwatch_metrics,
    aws_iam_role_policy.blob_delete_dynamodb,
    aws_cloudwatch_log_group.blob_delete_logs
  ]

  tags = {
    Name        = "${local.resource_prefix}-blob-delete-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
    Function    = "blob-delete"
  }
}

# API Gateway permission to invoke blob-delete Lambda
resource "aws_lambda_permission" "blob_delete_apigw" {
  statement_id  = "AllowAPIGatewayInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.blob_delete.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.api.execution_arn}/*"
}

# =============================================================================
# CloudWatch Monitoring
# =============================================================================

# CloudWatch Log Metric Filter for errors
resource "aws_cloudwatch_log_metric_filter" "blob_delete_errors" {
  name           = "${local.resource_prefix}-blob-delete-errors-${var.environment}"
  log_group_name = aws_cloudwatch_log_group.blob_delete_logs.name
  pattern        = "[timestamp, request_id, level = ERROR*, ...]"

  metric_transformation {
    name      = "BlobDeleteErrorCount"
    namespace = "JMAPService/${var.environment}"
    value     = "1"
    unit      = "Count"
  }
}

# CloudWatch Alarm for blob-delete Lambda errors
resource "aws_cloudwatch_metric_alarm" "blob_delete_errors" {
  alarm_name          = "${local.resource_prefix}-blob-delete-errors-${var.environment}"
  alarm_description   = "Alerts when blob-delete Lambda has errors"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "Errors"
  namespace           = "AWS/Lambda"
  period              = 300
  statistic           = "Sum"
  threshold           = 1
  treat_missing_data  = "notBreaching"

  dimensions = {
    FunctionName = aws_lambda_function.blob_delete.function_name
  }

  tags = {
    Name        = "${local.resource_prefix}-blob-delete-errors-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
  }
}

# CloudWatch Log Anomaly Detector for blob-delete Lambda
resource "aws_cloudwatch_log_anomaly_detector" "blob_delete_anomaly" {
  log_group_arn_list   = [aws_cloudwatch_log_group.blob_delete_logs.arn]
  detector_name        = "${local.resource_prefix}-blob-delete-anomaly-${var.environment}"
  enabled              = true
  evaluation_frequency = "FIFTEEN_MIN"
}
