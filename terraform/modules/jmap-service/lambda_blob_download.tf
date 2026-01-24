# Lambda function for blob-download (GET /download/{accountId}/{blobId} and /download-iam/{accountId}/{blobId})
# Generates CloudFront signed URLs for blob downloads

# =============================================================================
# CloudWatch Log Group
# =============================================================================

resource "aws_cloudwatch_log_group" "blob_download_logs" {
  name              = "/aws/lambda/${local.resource_prefix}-blob-download-${var.environment}"
  retention_in_days = var.log_retention_days

  tags = {
    Name        = "${local.resource_prefix}-blob-download-logs-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
    Function    = "blob-download"
  }
}

# =============================================================================
# IAM Role and Policies
# =============================================================================

resource "aws_iam_role" "blob_download_execution" {
  name               = "${local.resource_prefix}-blob-download-execution-${var.environment}"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume_role.json

  tags = {
    Name        = "${local.resource_prefix}-blob-download-execution-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
    Function    = "blob-download"
  }
}

# Attach AWS managed policy for basic Lambda execution
resource "aws_iam_role_policy_attachment" "blob_download_basic_execution" {
  role       = aws_iam_role.blob_download_execution.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

# Attach AWS managed policy for X-Ray tracing
resource "aws_iam_role_policy_attachment" "blob_download_xray_access" {
  role       = aws_iam_role.blob_download_execution.name
  policy_arn = "arn:aws:iam::aws:policy/AWSXRayDaemonWriteAccess"
}

# IAM policy for CloudWatch Metrics
resource "aws_iam_role_policy" "blob_download_cloudwatch_metrics" {
  name   = "${local.resource_prefix}-blob-download-cloudwatch-metrics-${var.environment}"
  role   = aws_iam_role.blob_download_execution.id
  policy = data.aws_iam_policy_document.cloudwatch_metrics.json
}

# IAM policy for DynamoDB access (read blob records and plugin registry)
data "aws_iam_policy_document" "blob_download_dynamodb" {
  statement {
    effect = "Allow"
    actions = [
      "dynamodb:GetItem",
      "dynamodb:Query"
    ]
    resources = [aws_dynamodb_table.jmap_data.arn]
  }
}

resource "aws_iam_role_policy" "blob_download_dynamodb" {
  name   = "${local.resource_prefix}-blob-download-dynamodb-${var.environment}"
  role   = aws_iam_role.blob_download_execution.id
  policy = data.aws_iam_policy_document.blob_download_dynamodb.json
}

# IAM policy for Secrets Manager access (read CloudFront private key)
data "aws_iam_policy_document" "blob_download_secrets" {
  statement {
    effect = "Allow"
    actions = [
      "secretsmanager:GetSecretValue"
    ]
    resources = [aws_secretsmanager_secret.cloudfront_private_key.arn]
  }
}

resource "aws_iam_role_policy" "blob_download_secrets" {
  name   = "${local.resource_prefix}-blob-download-secrets-${var.environment}"
  role   = aws_iam_role.blob_download_execution.id
  policy = data.aws_iam_policy_document.blob_download_secrets.json
}

# =============================================================================
# Lambda Function
# =============================================================================

resource "aws_lambda_function" "blob_download" {
  filename         = "${path.module}/../../../build/blob-download/lambda.zip"
  function_name    = "${local.resource_prefix}-blob-download-${var.environment}"
  role             = aws_iam_role.blob_download_execution.arn
  handler          = "bootstrap"
  source_code_hash = filebase64sha256("${path.module}/../../../build/blob-download/lambda.zip")
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
      ENVIRONMENT               = var.environment
      DYNAMODB_TABLE            = aws_dynamodb_table.jmap_data.name
      CLOUDFRONT_DOMAIN         = var.domain_name
      CLOUDFRONT_KEY_PAIR_ID    = aws_cloudfront_public_key.blob_signing_current.id
      PRIVATE_KEY_SECRET_ARN    = aws_secretsmanager_secret.cloudfront_private_key.arn
      SIGNED_URL_EXPIRY_SECONDS = tostring(var.signed_url_expiry_seconds)

      # ADOT Collector Configuration
      OPENTELEMETRY_COLLECTOR_CONFIG_URI = "file:///var/task/collector.yaml"
      OPENTELEMETRY_EXTENSION_LOG_LEVEL  = "error"

      # OpenTelemetry SDK Configuration
      OTEL_SERVICE_NAME   = "${local.resource_prefix}-blob-download-${var.environment}"
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
    aws_iam_role_policy_attachment.blob_download_basic_execution,
    aws_iam_role_policy_attachment.blob_download_xray_access,
    aws_iam_role_policy.blob_download_cloudwatch_metrics,
    aws_iam_role_policy.blob_download_dynamodb,
    aws_iam_role_policy.blob_download_secrets,
    aws_cloudwatch_log_group.blob_download_logs
  ]

  tags = {
    Name        = "${local.resource_prefix}-blob-download-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
    Function    = "blob-download"
  }
}

# API Gateway permission to invoke blob-download Lambda
resource "aws_lambda_permission" "blob_download_apigw" {
  statement_id  = "AllowAPIGatewayInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.blob_download.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.api.execution_arn}/*"
}

# =============================================================================
# CloudWatch Monitoring
# =============================================================================

# CloudWatch Log Metric Filter for errors
resource "aws_cloudwatch_log_metric_filter" "blob_download_errors" {
  name           = "${local.resource_prefix}-blob-download-errors-${var.environment}"
  log_group_name = aws_cloudwatch_log_group.blob_download_logs.name
  pattern        = "[timestamp, request_id, level = ERROR*, ...]"

  metric_transformation {
    name      = "BlobDownloadErrorCount"
    namespace = "JMAPService/${var.environment}"
    value     = "1"
    unit      = "Count"
  }
}

# CloudWatch Alarm for blob-download Lambda errors
resource "aws_cloudwatch_metric_alarm" "blob_download_errors" {
  alarm_name          = "${local.resource_prefix}-blob-download-errors-${var.environment}"
  alarm_description   = "Alerts when blob-download Lambda has errors"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "Errors"
  namespace           = "AWS/Lambda"
  period              = 300
  statistic           = "Sum"
  threshold           = 1
  treat_missing_data  = "notBreaching"

  dimensions = {
    FunctionName = aws_lambda_function.blob_download.function_name
  }

  tags = {
    Name        = "${local.resource_prefix}-blob-download-errors-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
  }
}

# CloudWatch Log Anomaly Detector for blob-download Lambda
resource "aws_cloudwatch_log_anomaly_detector" "blob_download_anomaly" {
  log_group_arn_list   = [aws_cloudwatch_log_group.blob_download_logs.arn]
  detector_name        = "${local.resource_prefix}-blob-download-anomaly-${var.environment}"
  enabled              = true
  evaluation_frequency = "FIFTEEN_MIN"
}
