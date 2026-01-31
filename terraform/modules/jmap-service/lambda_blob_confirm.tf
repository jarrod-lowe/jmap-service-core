# Lambda function for blob-confirm (S3 event trigger for upload confirmation)
# Confirms blob uploads by updating S3 tags and DynamoDB records

# =============================================================================
# CloudWatch Log Group
# =============================================================================

resource "aws_cloudwatch_log_group" "blob_confirm_logs" {
  name              = "/aws/lambda/${local.resource_prefix}-blob-confirm-${var.environment}"
  retention_in_days = var.log_retention_days

  tags = {
    Name        = "${local.resource_prefix}-blob-confirm-logs-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
    Function    = "blob-confirm"
  }
}

# =============================================================================
# Dead Letter Queue
# =============================================================================

resource "aws_sqs_queue" "blob_confirm_dlq" {
  name                      = "${local.resource_prefix}-blob-confirm-dlq-${var.environment}"
  message_retention_seconds = 1209600 # 14 days

  tags = {
    Name        = "${local.resource_prefix}-blob-confirm-dlq-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
    Function    = "blob-confirm"
  }
}

# CloudWatch Alarm for DLQ depth
resource "aws_cloudwatch_metric_alarm" "blob_confirm_dlq_depth" {
  alarm_name          = "${local.resource_prefix}-blob-confirm-dlq-depth-${var.environment}"
  alarm_description   = "Alerts when blob-confirm DLQ has messages"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "ApproximateNumberOfMessagesVisible"
  namespace           = "AWS/SQS"
  period              = 300
  statistic           = "Sum"
  threshold           = 0
  treat_missing_data  = "notBreaching"

  dimensions = {
    QueueName = aws_sqs_queue.blob_confirm_dlq.name
  }

  tags = {
    Name        = "${local.resource_prefix}-blob-confirm-dlq-depth-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
  }
}

# =============================================================================
# IAM Role and Policies
# =============================================================================

resource "aws_iam_role" "blob_confirm_execution" {
  name               = "${local.resource_prefix}-blob-confirm-execution-${var.environment}"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume_role.json

  tags = {
    Name        = "${local.resource_prefix}-blob-confirm-execution-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
    Function    = "blob-confirm"
  }
}

# Attach AWS managed policy for basic Lambda execution
resource "aws_iam_role_policy_attachment" "blob_confirm_basic_execution" {
  role       = aws_iam_role.blob_confirm_execution.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

# Attach AWS managed policy for X-Ray tracing
resource "aws_iam_role_policy_attachment" "blob_confirm_xray_access" {
  role       = aws_iam_role.blob_confirm_execution.name
  policy_arn = "arn:aws:iam::aws:policy/AWSXRayDaemonWriteAccess"
}

# IAM policy for CloudWatch Metrics
resource "aws_iam_role_policy" "blob_confirm_cloudwatch_metrics" {
  name   = "${local.resource_prefix}-blob-confirm-cloudwatch-metrics-${var.environment}"
  role   = aws_iam_role.blob_confirm_execution.id
  policy = data.aws_iam_policy_document.cloudwatch_metrics.json
}

# IAM policy for DynamoDB access
data "aws_iam_policy_document" "blob_confirm_dynamodb" {
  statement {
    effect = "Allow"
    actions = [
      "dynamodb:GetItem",
      "dynamodb:TransactWriteItems",
      "dynamodb:UpdateItem", # Required for Update operations within transactions
    ]
    resources = [aws_dynamodb_table.jmap_data.arn]
  }
}

resource "aws_iam_role_policy" "blob_confirm_dynamodb" {
  name   = "${local.resource_prefix}-blob-confirm-dynamodb-${var.environment}"
  role   = aws_iam_role.blob_confirm_execution.id
  policy = data.aws_iam_policy_document.blob_confirm_dynamodb.json
}

# IAM policy for S3 access (tagging and delete)
data "aws_iam_policy_document" "blob_confirm_s3" {
  statement {
    effect = "Allow"
    actions = [
      "s3:PutObjectTagging",
      "s3:DeleteObject",
    ]
    resources = ["${aws_s3_bucket.blobs.arn}/*"]
  }
}

resource "aws_iam_role_policy" "blob_confirm_s3" {
  name   = "${local.resource_prefix}-blob-confirm-s3-${var.environment}"
  role   = aws_iam_role.blob_confirm_execution.id
  policy = data.aws_iam_policy_document.blob_confirm_s3.json
}

# IAM policy for SQS DLQ access
data "aws_iam_policy_document" "blob_confirm_sqs" {
  statement {
    effect = "Allow"
    actions = [
      "sqs:SendMessage",
    ]
    resources = [aws_sqs_queue.blob_confirm_dlq.arn]
  }
}

resource "aws_iam_role_policy" "blob_confirm_sqs" {
  name   = "${local.resource_prefix}-blob-confirm-sqs-${var.environment}"
  role   = aws_iam_role.blob_confirm_execution.id
  policy = data.aws_iam_policy_document.blob_confirm_sqs.json
}

# =============================================================================
# Lambda Function
# =============================================================================

resource "aws_lambda_function" "blob_confirm" {
  filename         = "${path.module}/../../../build/blob-confirm/lambda.zip"
  function_name    = "${local.resource_prefix}-blob-confirm-${var.environment}"
  role             = aws_iam_role.blob_confirm_execution.arn
  handler          = "bootstrap"
  source_code_hash = filebase64sha256("${path.module}/../../../build/blob-confirm/lambda.zip")
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

  # Dead letter queue for failed invocations
  dead_letter_config {
    target_arn = aws_sqs_queue.blob_confirm_dlq.arn
  }

  environment {
    variables = {
      ENVIRONMENT    = var.environment
      DYNAMODB_TABLE = aws_dynamodb_table.jmap_data.name
      BLOB_BUCKET    = aws_s3_bucket.blobs.bucket

      # ADOT Collector Configuration
      OPENTELEMETRY_COLLECTOR_CONFIG_URI = "file:///var/task/collector.yaml"
      OPENTELEMETRY_EXTENSION_LOG_LEVEL  = "error"

      # OpenTelemetry SDK Configuration
      OTEL_SERVICE_NAME   = "${local.resource_prefix}-blob-confirm-${var.environment}"
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
    aws_iam_role_policy_attachment.blob_confirm_basic_execution,
    aws_iam_role_policy_attachment.blob_confirm_xray_access,
    aws_iam_role_policy.blob_confirm_cloudwatch_metrics,
    aws_iam_role_policy.blob_confirm_dynamodb,
    aws_iam_role_policy.blob_confirm_s3,
    aws_iam_role_policy.blob_confirm_sqs,
    aws_cloudwatch_log_group.blob_confirm_logs
  ]

  tags = {
    Name        = "${local.resource_prefix}-blob-confirm-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
    Function    = "blob-confirm"
  }
}

# Permission for S3 to invoke the Lambda
resource "aws_lambda_permission" "blob_confirm_s3" {
  statement_id  = "AllowS3Invoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.blob_confirm.function_name
  principal     = "s3.amazonaws.com"
  source_arn    = aws_s3_bucket.blobs.arn
}

# S3 bucket notification for object creation
resource "aws_s3_bucket_notification" "blobs_notification" {
  bucket = aws_s3_bucket.blobs.id

  lambda_function {
    lambda_function_arn = aws_lambda_function.blob_confirm.arn
    events              = ["s3:ObjectCreated:Put"]
  }

  depends_on = [aws_lambda_permission.blob_confirm_s3]
}

# =============================================================================
# CloudWatch Monitoring
# =============================================================================

# CloudWatch Log Metric Filter for errors
resource "aws_cloudwatch_log_metric_filter" "blob_confirm_errors" {
  name           = "${local.resource_prefix}-blob-confirm-errors-${var.environment}"
  log_group_name = aws_cloudwatch_log_group.blob_confirm_logs.name
  pattern        = "[timestamp, request_id, level = ERROR*, ...]"

  metric_transformation {
    name      = "BlobConfirmErrorCount"
    namespace = "JMAPService/${var.environment}"
    value     = "1"
    unit      = "Count"
  }
}

# CloudWatch Alarm for blob-confirm Lambda errors
resource "aws_cloudwatch_metric_alarm" "blob_confirm_errors" {
  alarm_name          = "${local.resource_prefix}-blob-confirm-errors-${var.environment}"
  alarm_description   = "Alerts when blob-confirm Lambda has errors"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "Errors"
  namespace           = "AWS/Lambda"
  period              = 300
  statistic           = "Sum"
  threshold           = 1
  treat_missing_data  = "notBreaching"

  dimensions = {
    FunctionName = aws_lambda_function.blob_confirm.function_name
  }

  tags = {
    Name        = "${local.resource_prefix}-blob-confirm-errors-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
  }
}

# CloudWatch Log Anomaly Detector for blob-confirm Lambda
resource "aws_cloudwatch_log_anomaly_detector" "blob_confirm_anomaly" {
  log_group_arn_list   = [aws_cloudwatch_log_group.blob_confirm_logs.arn]
  detector_name        = "${local.resource_prefix}-blob-confirm-anomaly-${var.environment}"
  enabled              = true
  evaluation_frequency = "FIFTEEN_MIN"
}
