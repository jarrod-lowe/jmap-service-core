# Lambda function for core-echo (Core/echo JMAP method)
# Per RFC 8620 Section 3.5, this method echoes back its arguments unchanged

# =============================================================================
# CloudWatch Log Group
# =============================================================================

resource "aws_cloudwatch_log_group" "core_echo_logs" {
  name              = "/aws/lambda/${local.resource_prefix}-core-echo-${var.environment}"
  retention_in_days = var.log_retention_days

  tags = {
    Name        = "${local.resource_prefix}-core-echo-logs-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
    Function    = "core-echo"
  }
}

# =============================================================================
# IAM Role and Policies
# =============================================================================

resource "aws_iam_role" "core_echo_execution" {
  name               = "${local.resource_prefix}-core-echo-execution-${var.environment}"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume_role.json

  tags = {
    Name        = "${local.resource_prefix}-core-echo-execution-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
    Function    = "core-echo"
  }
}

# Attach AWS managed policy for basic Lambda execution
resource "aws_iam_role_policy_attachment" "core_echo_basic_execution" {
  role       = aws_iam_role.core_echo_execution.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

# Attach AWS managed policy for X-Ray tracing
resource "aws_iam_role_policy_attachment" "core_echo_xray_access" {
  role       = aws_iam_role.core_echo_execution.name
  policy_arn = "arn:aws:iam::aws:policy/AWSXRayDaemonWriteAccess"
}

# IAM policy for CloudWatch Metrics
resource "aws_iam_role_policy" "core_echo_cloudwatch_metrics" {
  name   = "${local.resource_prefix}-core-echo-cloudwatch-metrics-${var.environment}"
  role   = aws_iam_role.core_echo_execution.id
  policy = data.aws_iam_policy_document.cloudwatch_metrics.json
}

# =============================================================================
# Lambda Function
# =============================================================================

resource "aws_lambda_function" "core_echo" {
  filename         = "${path.module}/../../../build/core-echo/lambda.zip"
  function_name    = "${local.resource_prefix}-core-echo-${var.environment}"
  role             = aws_iam_role.core_echo_execution.arn
  handler          = "bootstrap"
  source_code_hash = filebase64sha256("${path.module}/../../../build/core-echo/lambda.zip")
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
      OTEL_SERVICE_NAME   = "${local.resource_prefix}-core-echo-${var.environment}"
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
    aws_iam_role_policy_attachment.core_echo_basic_execution,
    aws_iam_role_policy_attachment.core_echo_xray_access,
    aws_iam_role_policy.core_echo_cloudwatch_metrics,
    aws_cloudwatch_log_group.core_echo_logs
  ]

  tags = {
    Name        = "${local.resource_prefix}-core-echo-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
    Function    = "core-echo"
  }
}

# =============================================================================
# CloudWatch Monitoring
# =============================================================================

# CloudWatch Log Metric Filter for errors
resource "aws_cloudwatch_log_metric_filter" "core_echo_errors" {
  name           = "${local.resource_prefix}-core-echo-errors-${var.environment}"
  log_group_name = aws_cloudwatch_log_group.core_echo_logs.name
  pattern        = "[timestamp, request_id, level = ERROR*, ...]"

  metric_transformation {
    name      = "CoreEchoErrorCount"
    namespace = "JMAPService/${var.environment}"
    value     = "1"
    unit      = "Count"
  }
}

# CloudWatch Alarm for core-echo Lambda errors
resource "aws_cloudwatch_metric_alarm" "core_echo_errors" {
  alarm_name          = "${local.resource_prefix}-core-echo-errors-${var.environment}"
  alarm_description   = "Alerts when core-echo Lambda has errors"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "Errors"
  namespace           = "AWS/Lambda"
  period              = 300
  statistic           = "Sum"
  threshold           = 1
  treat_missing_data  = "notBreaching"

  dimensions = {
    FunctionName = aws_lambda_function.core_echo.function_name
  }

  tags = {
    Name        = "${local.resource_prefix}-core-echo-errors-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
  }
}

# CloudWatch Log Anomaly Detector for core-echo Lambda
resource "aws_cloudwatch_log_anomaly_detector" "core_echo_anomaly" {
  log_group_arn_list   = [aws_cloudwatch_log_group.core_echo_logs.arn]
  detector_name        = "${local.resource_prefix}-core-echo-anomaly-${var.environment}"
  enabled              = true
  evaluation_frequency = "FIFTEEN_MIN"
}
