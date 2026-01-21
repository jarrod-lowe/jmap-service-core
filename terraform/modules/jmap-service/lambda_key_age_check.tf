# Lambda function for key-age-check
# Checks CloudFront signing key age and publishes CloudWatch metric
# Runs daily via EventBridge schedule

# =============================================================================
# CloudWatch Log Group
# =============================================================================

resource "aws_cloudwatch_log_group" "key_age_check_logs" {
  name              = "/aws/lambda/${local.resource_prefix}-key-age-check-${var.environment}"
  retention_in_days = var.log_retention_days

  tags = {
    Name        = "${local.resource_prefix}-key-age-check-logs-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
    Function    = "key-age-check"
  }
}

# =============================================================================
# IAM Role and Policies
# =============================================================================

resource "aws_iam_role" "key_age_check_execution" {
  name               = "${local.resource_prefix}-key-age-check-execution-${var.environment}"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume_role.json

  tags = {
    Name        = "${local.resource_prefix}-key-age-check-execution-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
    Function    = "key-age-check"
  }
}

# Attach AWS managed policy for basic Lambda execution
resource "aws_iam_role_policy_attachment" "key_age_check_basic_execution" {
  role       = aws_iam_role.key_age_check_execution.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

# IAM policy for SSM Parameter access (read key creation timestamp)
data "aws_iam_policy_document" "key_age_check_ssm" {
  statement {
    effect = "Allow"
    actions = [
      "ssm:GetParameter"
    ]
    resources = [aws_ssm_parameter.cloudfront_key_created_at.arn]
  }
}

resource "aws_iam_role_policy" "key_age_check_ssm" {
  name   = "${local.resource_prefix}-key-age-check-ssm-${var.environment}"
  role   = aws_iam_role.key_age_check_execution.id
  policy = data.aws_iam_policy_document.key_age_check_ssm.json
}

# IAM policy for CloudWatch Metrics (publish key age metric)
data "aws_iam_policy_document" "key_age_check_cloudwatch" {
  statement {
    effect = "Allow"
    actions = [
      "cloudwatch:PutMetricData"
    ]
    resources = ["*"]
    condition {
      test     = "StringEquals"
      variable = "cloudwatch:namespace"
      values   = ["JMAPService/${var.environment}"]
    }
  }
}

resource "aws_iam_role_policy" "key_age_check_cloudwatch" {
  name   = "${local.resource_prefix}-key-age-check-cloudwatch-${var.environment}"
  role   = aws_iam_role.key_age_check_execution.id
  policy = data.aws_iam_policy_document.key_age_check_cloudwatch.json
}

# =============================================================================
# Lambda Function
# =============================================================================

resource "aws_lambda_function" "key_age_check" {
  filename         = "${path.module}/../../../build/key-age-check/lambda.zip"
  function_name    = "${local.resource_prefix}-key-age-check-${var.environment}"
  role             = aws_iam_role.key_age_check_execution.arn
  handler          = "bootstrap"
  source_code_hash = filebase64sha256("${path.module}/../../../build/key-age-check/lambda.zip")
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  timeout          = 30
  memory_size      = 128 # Minimal memory for simple metric publishing

  environment {
    variables = {
      SSM_PARAMETER_NAME = aws_ssm_parameter.cloudfront_key_created_at.name
      METRIC_NAMESPACE   = "JMAPService/${var.environment}"
    }
  }

  depends_on = [
    aws_iam_role_policy_attachment.key_age_check_basic_execution,
    aws_iam_role_policy.key_age_check_ssm,
    aws_iam_role_policy.key_age_check_cloudwatch,
    aws_cloudwatch_log_group.key_age_check_logs
  ]

  tags = {
    Name        = "${local.resource_prefix}-key-age-check-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
    Function    = "key-age-check"
  }
}

# =============================================================================
# EventBridge Schedule (Daily)
# =============================================================================

resource "aws_cloudwatch_event_rule" "key_age_check" {
  name                = "${local.resource_prefix}-key-age-check-${var.environment}"
  description         = "Daily check of CloudFront signing key age"
  schedule_expression = "rate(1 day)"

  tags = {
    Name        = "${local.resource_prefix}-key-age-check-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
  }
}

resource "aws_cloudwatch_event_target" "key_age_check" {
  rule      = aws_cloudwatch_event_rule.key_age_check.name
  target_id = "key-age-check-lambda"
  arn       = aws_lambda_function.key_age_check.arn
}

resource "aws_lambda_permission" "key_age_check_eventbridge" {
  statement_id  = "AllowEventBridgeInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.key_age_check.function_name
  principal     = "events.amazonaws.com"
  source_arn    = aws_cloudwatch_event_rule.key_age_check.arn
}

# =============================================================================
# CloudWatch Alarm for Key Age
# =============================================================================

resource "aws_cloudwatch_metric_alarm" "key_age" {
  alarm_name          = "${local.resource_prefix}-cloudfront-signing-key-age-${var.environment}"
  alarm_description   = "CloudFront signing key is older than ${var.cloudfront_signing_key_max_age_days} days - rotation recommended"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "KeyAgeDays"
  namespace           = "JMAPService/${var.environment}"
  period              = 86400 # 1 day
  statistic           = "Maximum"
  threshold           = var.cloudfront_signing_key_max_age_days
  treat_missing_data  = "notBreaching"

  tags = {
    Name        = "${local.resource_prefix}-cloudfront-signing-key-age-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
  }
}
