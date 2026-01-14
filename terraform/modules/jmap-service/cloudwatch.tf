# CloudWatch Log Group for hello-world Lambda function
# Pre-create to ensure retention and lifecycle settings
resource "aws_cloudwatch_log_group" "hello_world_logs" {
  name              = "/aws/lambda/${local.resource_prefix}-hello-world-${var.environment}"
  retention_in_days = var.log_retention_days

  tags = {
    Name        = "${local.resource_prefix}-hello-world-logs-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
    Function    = "hello-world"
  }
}

# CloudWatch Log Metric Filter for errors
resource "aws_cloudwatch_log_metric_filter" "hello_world_errors" {
  name           = "${local.resource_prefix}-hello-world-errors-${var.environment}"
  log_group_name = aws_cloudwatch_log_group.hello_world_logs.name
  pattern        = "[timestamp, request_id, level = ERROR*, ...]"

  metric_transformation {
    name      = "ErrorCount"
    namespace = "JMAPService/${var.environment}"
    value     = "1"
    unit      = "Count"
  }
}

# CloudWatch Alarm for Lambda errors
resource "aws_cloudwatch_metric_alarm" "hello_world_errors" {
  alarm_name          = "${local.resource_prefix}-hello-world-errors-${var.environment}"
  alarm_description   = "Alerts when hello-world Lambda has errors"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "Errors"
  namespace           = "AWS/Lambda"
  period              = 300
  statistic           = "Sum"
  threshold           = 1
  treat_missing_data  = "notBreaching"

  dimensions = {
    FunctionName = aws_lambda_function.hello_world.function_name
  }

  tags = {
    Name        = "${local.resource_prefix}-hello-world-errors-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
  }
}

# CloudWatch Log Anomaly Detector for hello-world Lambda
resource "aws_cloudwatch_log_anomaly_detector" "hello_world_anomaly" {
  log_group_arn_list = [aws_cloudwatch_log_group.hello_world_logs.arn]
  detector_name      = "${local.resource_prefix}-hello-world-anomaly-${var.environment}"
  enabled            = true

  # Evaluation frequency: how often CloudWatch analyzes the logs
  # FIVE_MIN, TEN_MIN, FIFTEEN_MIN, THIRTY_MIN, or ONE_HOUR
  evaluation_frequency = "FIFTEEN_MIN"
}
