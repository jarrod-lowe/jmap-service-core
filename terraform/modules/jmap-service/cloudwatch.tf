# CloudWatch Log Group for get-jmap-session Lambda function
resource "aws_cloudwatch_log_group" "get_jmap_session_logs" {
  name              = "/aws/lambda/${local.resource_prefix}-get-jmap-session-${var.environment}"
  retention_in_days = var.log_retention_days

  tags = {
    Name        = "${local.resource_prefix}-get-jmap-session-logs-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
    Function    = "get-jmap-session"
  }
}

# CloudWatch Log Metric Filter for errors
resource "aws_cloudwatch_log_metric_filter" "get_jmap_session_errors" {
  name           = "${local.resource_prefix}-get-jmap-session-errors-${var.environment}"
  log_group_name = aws_cloudwatch_log_group.get_jmap_session_logs.name
  pattern        = "[timestamp, request_id, level = ERROR*, ...]"

  metric_transformation {
    name      = "GetJmapSessionErrorCount"
    namespace = "JMAPService/${var.environment}"
    value     = "1"
    unit      = "Count"
  }
}

# CloudWatch Alarm for get-jmap-session Lambda errors
resource "aws_cloudwatch_metric_alarm" "get_jmap_session_errors" {
  alarm_name          = "${local.resource_prefix}-get-jmap-session-errors-${var.environment}"
  alarm_description   = "Alerts when get-jmap-session Lambda has errors"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "Errors"
  namespace           = "AWS/Lambda"
  period              = 300
  statistic           = "Sum"
  threshold           = 1
  treat_missing_data  = "notBreaching"

  dimensions = {
    FunctionName = aws_lambda_function.get_jmap_session.function_name
  }

  tags = {
    Name        = "${local.resource_prefix}-get-jmap-session-errors-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
  }
}

# CloudWatch Log Anomaly Detector for get-jmap-session Lambda
resource "aws_cloudwatch_log_anomaly_detector" "get_jmap_session_anomaly" {
  log_group_arn_list   = [aws_cloudwatch_log_group.get_jmap_session_logs.arn]
  detector_name        = "${local.resource_prefix}-get-jmap-session-anomaly-${var.environment}"
  enabled              = true
  evaluation_frequency = "FIFTEEN_MIN"
}

# =============================================================================
# CloudWatch resources for jmap-api Lambda function
# =============================================================================

# CloudWatch Log Group for jmap-api Lambda function
resource "aws_cloudwatch_log_group" "jmap_api_logs" {
  name              = "/aws/lambda/${local.resource_prefix}-jmap-api-${var.environment}"
  retention_in_days = var.log_retention_days

  tags = {
    Name        = "${local.resource_prefix}-jmap-api-logs-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
    Function    = "jmap-api"
  }
}

# CloudWatch Log Metric Filter for errors
resource "aws_cloudwatch_log_metric_filter" "jmap_api_errors" {
  name           = "${local.resource_prefix}-jmap-api-errors-${var.environment}"
  log_group_name = aws_cloudwatch_log_group.jmap_api_logs.name
  pattern        = "[timestamp, request_id, level = ERROR*, ...]"

  metric_transformation {
    name      = "JmapApiErrorCount"
    namespace = "JMAPService/${var.environment}"
    value     = "1"
    unit      = "Count"
  }
}

# CloudWatch Alarm for jmap-api Lambda errors
resource "aws_cloudwatch_metric_alarm" "jmap_api_errors" {
  alarm_name          = "${local.resource_prefix}-jmap-api-errors-${var.environment}"
  alarm_description   = "Alerts when jmap-api Lambda has errors"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "Errors"
  namespace           = "AWS/Lambda"
  period              = 300
  statistic           = "Sum"
  threshold           = 1
  treat_missing_data  = "notBreaching"

  dimensions = {
    FunctionName = aws_lambda_function.jmap_api.function_name
  }

  tags = {
    Name        = "${local.resource_prefix}-jmap-api-errors-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
  }
}

# CloudWatch Log Anomaly Detector for jmap-api Lambda
resource "aws_cloudwatch_log_anomaly_detector" "jmap_api_anomaly" {
  log_group_arn_list   = [aws_cloudwatch_log_group.jmap_api_logs.arn]
  detector_name        = "${local.resource_prefix}-jmap-api-anomaly-${var.environment}"
  enabled              = true
  evaluation_frequency = "FIFTEEN_MIN"
}
