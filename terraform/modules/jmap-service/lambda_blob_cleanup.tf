# Lambda function for blob-cleanup (DynamoDB Streams trigger)
# Asynchronously deletes S3 objects and DynamoDB records after blobs are marked as deleted

# =============================================================================
# CloudWatch Log Group
# =============================================================================

resource "aws_cloudwatch_log_group" "blob_cleanup_logs" {
  name              = "/aws/lambda/${local.resource_prefix}-blob-cleanup-${var.environment}"
  retention_in_days = var.log_retention_days

  tags = {
    Name        = "${local.resource_prefix}-blob-cleanup-logs-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
    Function    = "blob-cleanup"
  }
}

# =============================================================================
# IAM Role and Policies
# =============================================================================

resource "aws_iam_role" "blob_cleanup_execution" {
  name               = "${local.resource_prefix}-blob-cleanup-execution-${var.environment}"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume_role.json

  tags = {
    Name        = "${local.resource_prefix}-blob-cleanup-execution-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
    Function    = "blob-cleanup"
  }
}

# Attach AWS managed policy for basic Lambda execution
resource "aws_iam_role_policy_attachment" "blob_cleanup_basic_execution" {
  role       = aws_iam_role.blob_cleanup_execution.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

# Attach AWS managed policy for X-Ray tracing
resource "aws_iam_role_policy_attachment" "blob_cleanup_xray_access" {
  role       = aws_iam_role.blob_cleanup_execution.name
  policy_arn = "arn:aws:iam::aws:policy/AWSXRayDaemonWriteAccess"
}

# IAM policy for CloudWatch Metrics
resource "aws_iam_role_policy" "blob_cleanup_cloudwatch_metrics" {
  name   = "${local.resource_prefix}-blob-cleanup-cloudwatch-metrics-${var.environment}"
  role   = aws_iam_role.blob_cleanup_execution.id
  policy = data.aws_iam_policy_document.cloudwatch_metrics.json
}

# IAM policy for DynamoDB access (delete records + read stream)
data "aws_iam_policy_document" "blob_cleanup_dynamodb" {
  statement {
    effect = "Allow"
    actions = [
      "dynamodb:DeleteItem"
    ]
    resources = [aws_dynamodb_table.jmap_data.arn]
  }

  statement {
    effect = "Allow"
    actions = [
      "dynamodb:GetRecords",
      "dynamodb:GetShardIterator",
      "dynamodb:DescribeStream",
      "dynamodb:ListStreams"
    ]
    resources = ["${aws_dynamodb_table.jmap_data.arn}/stream/*"]
  }
}

resource "aws_iam_role_policy" "blob_cleanup_dynamodb" {
  name   = "${local.resource_prefix}-blob-cleanup-dynamodb-${var.environment}"
  role   = aws_iam_role.blob_cleanup_execution.id
  policy = data.aws_iam_policy_document.blob_cleanup_dynamodb.json
}

# IAM policy for S3 access (delete blob objects)
data "aws_iam_policy_document" "blob_cleanup_s3" {
  statement {
    effect = "Allow"
    actions = [
      "s3:DeleteObject"
    ]
    resources = ["${aws_s3_bucket.blobs.arn}/*"]
  }
}

resource "aws_iam_role_policy" "blob_cleanup_s3" {
  name   = "${local.resource_prefix}-blob-cleanup-s3-${var.environment}"
  role   = aws_iam_role.blob_cleanup_execution.id
  policy = data.aws_iam_policy_document.blob_cleanup_s3.json
}

# =============================================================================
# Lambda Function
# =============================================================================

resource "aws_lambda_function" "blob_cleanup" {
  filename         = "${path.module}/../../../build/blob-cleanup/lambda.zip"
  function_name    = "${local.resource_prefix}-blob-cleanup-${var.environment}"
  role             = aws_iam_role.blob_cleanup_execution.arn
  handler          = "bootstrap"
  source_code_hash = filebase64sha256("${path.module}/../../../build/blob-cleanup/lambda.zip")
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
      BLOB_BUCKET    = aws_s3_bucket.blobs.bucket

      # ADOT Collector Configuration
      OPENTELEMETRY_COLLECTOR_CONFIG_URI = "file:///var/task/collector.yaml"
      OPENTELEMETRY_EXTENSION_LOG_LEVEL  = "error"

      # OpenTelemetry SDK Configuration
      OTEL_SERVICE_NAME   = "${local.resource_prefix}-blob-cleanup-${var.environment}"
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
    aws_iam_role_policy_attachment.blob_cleanup_basic_execution,
    aws_iam_role_policy_attachment.blob_cleanup_xray_access,
    aws_iam_role_policy.blob_cleanup_cloudwatch_metrics,
    aws_iam_role_policy.blob_cleanup_dynamodb,
    aws_iam_role_policy.blob_cleanup_s3,
    aws_cloudwatch_log_group.blob_cleanup_logs
  ]

  tags = {
    Name        = "${local.resource_prefix}-blob-cleanup-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
    Function    = "blob-cleanup"
  }
}

# SQS Dead Letter Queue for failed stream processing
resource "aws_sqs_queue" "blob_cleanup_dlq" {
  name                      = "${local.resource_prefix}-blob-cleanup-dlq-${var.environment}"
  message_retention_seconds = 1209600 # 14 days

  tags = {
    Name        = "${local.resource_prefix}-blob-cleanup-dlq-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
    Function    = "blob-cleanup"
  }
}

# IAM policy for SQS DLQ access
data "aws_iam_policy_document" "blob_cleanup_sqs" {
  statement {
    effect    = "Allow"
    actions   = ["sqs:SendMessage"]
    resources = [aws_sqs_queue.blob_cleanup_dlq.arn]
  }
}

resource "aws_iam_role_policy" "blob_cleanup_sqs" {
  name   = "${local.resource_prefix}-blob-cleanup-sqs-${var.environment}"
  role   = aws_iam_role.blob_cleanup_execution.id
  policy = data.aws_iam_policy_document.blob_cleanup_sqs.json
}

# DynamoDB Streams event source mapping
resource "aws_lambda_event_source_mapping" "blob_cleanup_stream" {
  event_source_arn  = aws_dynamodb_table.jmap_data.stream_arn
  function_name     = aws_lambda_function.blob_cleanup.arn
  starting_position = "LATEST"
  batch_size        = 10

  # Only retry failed batches for a limited time
  maximum_retry_attempts = 3

  # Filter to only invoke for blob soft-delete transitions
  filter_criteria {
    filter {
      pattern = jsonencode({
        eventName = ["MODIFY"]
        dynamodb = {
          NewImage = {
            deletedAt = { S = [{ "exists" = true }] }
            sk        = { S = [{ "prefix" = "BLOB#" }] }
          }
          OldImage = {
            deletedAt = [{ "exists" = false }]
          }
        }
      })
    }
  }

  # Send failed events to DLQ
  destination_config {
    on_failure {
      destination_arn = aws_sqs_queue.blob_cleanup_dlq.arn
    }
  }

  depends_on = [aws_iam_role_policy.blob_cleanup_sqs]

  tags = {
    Name        = "${local.resource_prefix}-blob-cleanup-stream-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
  }
}

# =============================================================================
# CloudWatch Monitoring
# =============================================================================

# CloudWatch Log Metric Filter for errors
resource "aws_cloudwatch_log_metric_filter" "blob_cleanup_errors" {
  name           = "${local.resource_prefix}-blob-cleanup-errors-${var.environment}"
  log_group_name = aws_cloudwatch_log_group.blob_cleanup_logs.name
  pattern        = "[timestamp, request_id, level = ERROR*, ...]"

  metric_transformation {
    name      = "BlobCleanupErrorCount"
    namespace = "JMAPService/${var.environment}"
    value     = "1"
    unit      = "Count"
  }
}

# CloudWatch Alarm for blob-cleanup Lambda errors
resource "aws_cloudwatch_metric_alarm" "blob_cleanup_errors" {
  alarm_name          = "${local.resource_prefix}-blob-cleanup-errors-${var.environment}"
  alarm_description   = "Alerts when blob-cleanup Lambda has errors"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "Errors"
  namespace           = "AWS/Lambda"
  period              = 300
  statistic           = "Sum"
  threshold           = 1
  treat_missing_data  = "notBreaching"

  dimensions = {
    FunctionName = aws_lambda_function.blob_cleanup.function_name
  }

  tags = {
    Name        = "${local.resource_prefix}-blob-cleanup-errors-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
  }
}

# CloudWatch Log Anomaly Detector for blob-cleanup Lambda
resource "aws_cloudwatch_log_anomaly_detector" "blob_cleanup_anomaly" {
  log_group_arn_list   = [aws_cloudwatch_log_group.blob_cleanup_logs.arn]
  detector_name        = "${local.resource_prefix}-blob-cleanup-anomaly-${var.environment}"
  enabled              = true
  evaluation_frequency = "FIFTEEN_MIN"
}

# CloudWatch Alarm for blob-cleanup DLQ messages
resource "aws_cloudwatch_metric_alarm" "blob_cleanup_dlq" {
  alarm_name          = "${local.resource_prefix}-blob-cleanup-dlq-${var.environment}"
  alarm_description   = "Alerts when blob-cleanup DLQ has messages (failed stream processing)"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "ApproximateNumberOfMessagesVisible"
  namespace           = "AWS/SQS"
  period              = 300
  statistic           = "Maximum"
  threshold           = 0
  treat_missing_data  = "notBreaching"

  dimensions = {
    QueueName = aws_sqs_queue.blob_cleanup_dlq.name
  }

  tags = {
    Name        = "${local.resource_prefix}-blob-cleanup-dlq-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
  }
}
