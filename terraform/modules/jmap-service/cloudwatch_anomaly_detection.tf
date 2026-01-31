# CloudWatch Anomaly Detection Configuration for JMAP Service Core
#
# This file contains the centralized anomaly detection configuration.
# Individual anomaly detectors are defined in their respective Lambda files,
# but they reference these shared locals for consistency.
#
# Note: The anomaly detectors are already defined in individual Lambda files.
# This file provides aggregated alarm definitions for HIGH and MEDIUM severity anomalies.

locals {
  # Map evaluation frequency seconds to CloudWatch values
  evaluation_frequency_map = {
    300  = "FIVE_MIN"
    900  = "FIFTEEN_MIN"
    1800 = "THIRTY_MIN"
    3600 = "ONE_HOUR"
  }

  # Resolved evaluation frequency string
  anomaly_evaluation_frequency = local.evaluation_frequency_map[var.anomaly_detection_evaluation_frequency]

  # List of all Lambda log group ARNs for reference
  lambda_log_groups = {
    get-jmap-session = aws_cloudwatch_log_group.get_jmap_session_logs.arn
    jmap-api         = aws_cloudwatch_log_group.jmap_api_logs.arn
    blob-upload      = aws_cloudwatch_log_group.blob_upload_logs.arn
    blob-download    = aws_cloudwatch_log_group.blob_download_logs.arn
    blob-delete      = aws_cloudwatch_log_group.blob_delete_logs.arn
    blob-cleanup     = aws_cloudwatch_log_group.blob_cleanup_logs.arn
    blob-confirm     = aws_cloudwatch_log_group.blob_confirm_logs.arn
    core-echo        = aws_cloudwatch_log_group.core_echo_logs.arn
  }

  # Map of detector names to their resources for alarm aggregation
  anomaly_detector_names = [
    aws_cloudwatch_log_anomaly_detector.get_jmap_session_anomaly.detector_name,
    aws_cloudwatch_log_anomaly_detector.jmap_api_anomaly.detector_name,
    aws_cloudwatch_log_anomaly_detector.blob_upload_anomaly.detector_name,
    aws_cloudwatch_log_anomaly_detector.blob_download_anomaly.detector_name,
    aws_cloudwatch_log_anomaly_detector.blob_delete_anomaly.detector_name,
    aws_cloudwatch_log_anomaly_detector.blob_cleanup_anomaly.detector_name,
    aws_cloudwatch_log_anomaly_detector.blob_confirm_anomaly.detector_name,
    aws_cloudwatch_log_anomaly_detector.core_echo_anomaly.detector_name,
  ]
}

# =============================================================================
# Aggregated Anomaly Alarms
# =============================================================================

# Note: CloudWatch Log Anomaly Detectors publish metrics to the LogMetrics namespace
# with dimensions for AnomalyDetectorName and Priority (HIGH, MEDIUM, LOW).
#
# We create composite alarms that aggregate across all detectors for each severity level.

# Alarm for any HIGH priority anomalies across all Lambda functions
resource "aws_cloudwatch_metric_alarm" "anomaly_high" {
  alarm_name          = "${local.resource_prefix}-anomaly-high-${var.environment}"
  alarm_description   = "Alerts when any Lambda function has HIGH priority log anomalies detected"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  threshold           = 0
  treat_missing_data  = "notBreaching"

  # Use metric math to sum anomaly counts across all detectors
  metric_query {
    id          = "total_high"
    expression  = "SUM(METRICS())"
    label       = "Total HIGH Anomalies"
    return_data = true
  }

  dynamic "metric_query" {
    for_each = local.anomaly_detector_names
    content {
      id = "m${metric_query.key}"
      metric {
        metric_name = "AnomalyDetected"
        namespace   = "AWS/Logs"
        period      = 300
        stat        = "Sum"
        dimensions = {
          AnomalyDetectorName = metric_query.value
          Priority            = "HIGH"
        }
      }
      return_data = false
    }
  }

  alarm_actions = [var.alarm_sns_topic_arn]
  ok_actions    = [var.alarm_sns_topic_arn]

  tags = {
    Name        = "${local.resource_prefix}-anomaly-high-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
    Severity    = "HIGH"
  }
}

# Alarm for any MEDIUM priority anomalies across all Lambda functions
resource "aws_cloudwatch_metric_alarm" "anomaly_medium" {
  alarm_name          = "${local.resource_prefix}-anomaly-medium-${var.environment}"
  alarm_description   = "Alerts when any Lambda function has MEDIUM priority log anomalies detected"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  threshold           = 0
  treat_missing_data  = "notBreaching"

  # Use metric math to sum anomaly counts across all detectors
  metric_query {
    id          = "total_medium"
    expression  = "SUM(METRICS())"
    label       = "Total MEDIUM Anomalies"
    return_data = true
  }

  dynamic "metric_query" {
    for_each = local.anomaly_detector_names
    content {
      id = "m${metric_query.key}"
      metric {
        metric_name = "AnomalyDetected"
        namespace   = "AWS/Logs"
        period      = 300
        stat        = "Sum"
        dimensions = {
          AnomalyDetectorName = metric_query.value
          Priority            = "MEDIUM"
        }
      }
      return_data = false
    }
  }

  alarm_actions = [var.alarm_sns_topic_arn]
  ok_actions    = [var.alarm_sns_topic_arn]

  tags = {
    Name        = "${local.resource_prefix}-anomaly-medium-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
    Severity    = "MEDIUM"
  }
}
