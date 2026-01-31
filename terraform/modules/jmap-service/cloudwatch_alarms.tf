# Consolidated CloudWatch Alarms for JMAP Service Core
#
# These alarms monitor:
# - API Gateway errors and latency
# - DynamoDB throttling
#
# All alarms route to SNS for notifications.
# Lambda-specific error alarms are defined in their respective Lambda files.

# =============================================================================
# API Gateway Alarms
# =============================================================================

# Alarm for any 5XX errors (server errors)
resource "aws_cloudwatch_metric_alarm" "api_5xx_errors" {
  alarm_name          = "${local.resource_prefix}-api-5xx-${var.environment}"
  alarm_description   = "Alerts when API Gateway returns any 5XX server errors"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "5XXError"
  namespace           = "AWS/ApiGateway"
  period              = 300
  statistic           = "Sum"
  threshold           = 0
  treat_missing_data  = "notBreaching"

  dimensions = {
    ApiName = aws_api_gateway_rest_api.api.name
  }

  alarm_actions = [var.alarm_sns_topic_arn]
  ok_actions    = [var.alarm_sns_topic_arn]

  tags = {
    Name        = "${local.resource_prefix}-api-5xx-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
  }
}

# Alarm for high 4XX error rate
resource "aws_cloudwatch_metric_alarm" "api_4xx_high" {
  alarm_name          = "${local.resource_prefix}-api-4xx-high-${var.environment}"
  alarm_description   = "Alerts when API Gateway 4XX client errors exceed threshold"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "4XXError"
  namespace           = "AWS/ApiGateway"
  period              = 300
  statistic           = "Sum"
  threshold           = 50
  treat_missing_data  = "notBreaching"

  dimensions = {
    ApiName = aws_api_gateway_rest_api.api.name
  }

  alarm_actions = [var.alarm_sns_topic_arn]
  ok_actions    = [var.alarm_sns_topic_arn]

  tags = {
    Name        = "${local.resource_prefix}-api-4xx-high-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
  }
}

# Alarm for high API latency (p99 > 10 seconds)
resource "aws_cloudwatch_metric_alarm" "api_latency_high" {
  alarm_name          = "${local.resource_prefix}-api-latency-${var.environment}"
  alarm_description   = "Alerts when API Gateway p99 latency exceeds 10 seconds"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "Latency"
  namespace           = "AWS/ApiGateway"
  period              = 300
  extended_statistic  = "p99"
  threshold           = 10000
  treat_missing_data  = "notBreaching"

  dimensions = {
    ApiName = aws_api_gateway_rest_api.api.name
  }

  alarm_actions = [var.alarm_sns_topic_arn]
  ok_actions    = [var.alarm_sns_topic_arn]

  tags = {
    Name        = "${local.resource_prefix}-api-latency-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
  }
}

# =============================================================================
# DynamoDB Alarms
# =============================================================================

# Alarm for any DynamoDB throttling
resource "aws_cloudwatch_metric_alarm" "dynamodb_throttle" {
  alarm_name          = "${local.resource_prefix}-dynamodb-throttle-${var.environment}"
  alarm_description   = "Alerts when DynamoDB throttles any requests"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "ThrottledRequests"
  namespace           = "AWS/DynamoDB"
  period              = 300
  statistic           = "Sum"
  threshold           = 0
  treat_missing_data  = "notBreaching"

  dimensions = {
    TableName = aws_dynamodb_table.jmap_data.name
  }

  alarm_actions = [var.alarm_sns_topic_arn]
  ok_actions    = [var.alarm_sns_topic_arn]

  tags = {
    Name        = "${local.resource_prefix}-dynamodb-throttle-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
  }
}
