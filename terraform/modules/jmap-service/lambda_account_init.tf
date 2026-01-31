# Lambda function for account-init (Cognito Post Authentication trigger)
# Initializes account META# record with default quota on first authentication

# =============================================================================
# CloudWatch Log Group
# =============================================================================

resource "aws_cloudwatch_log_group" "account_init_logs" {
  name              = "/aws/lambda/${local.resource_prefix}-account-init-${var.environment}"
  retention_in_days = var.log_retention_days

  tags = {
    Name        = "${local.resource_prefix}-account-init-logs-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
    Function    = "account-init"
  }
}

# =============================================================================
# IAM Role and Policies
# =============================================================================

resource "aws_iam_role" "account_init_execution" {
  name               = "${local.resource_prefix}-account-init-execution-${var.environment}"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume_role.json

  tags = {
    Name        = "${local.resource_prefix}-account-init-execution-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
    Function    = "account-init"
  }
}

# Attach AWS managed policy for basic Lambda execution
resource "aws_iam_role_policy_attachment" "account_init_basic_execution" {
  role       = aws_iam_role.account_init_execution.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

# Attach AWS managed policy for X-Ray tracing
resource "aws_iam_role_policy_attachment" "account_init_xray_access" {
  role       = aws_iam_role.account_init_execution.name
  policy_arn = "arn:aws:iam::aws:policy/AWSXRayDaemonWriteAccess"
}

# IAM policy for CloudWatch Metrics
resource "aws_iam_role_policy" "account_init_cloudwatch_metrics" {
  name   = "${local.resource_prefix}-account-init-cloudwatch-metrics-${var.environment}"
  role   = aws_iam_role.account_init_execution.id
  policy = data.aws_iam_policy_document.cloudwatch_metrics.json
}

# IAM policy for DynamoDB access (PutItem for account META# record)
data "aws_iam_policy_document" "account_init_dynamodb" {
  statement {
    effect = "Allow"
    actions = [
      "dynamodb:PutItem",
    ]
    resources = [aws_dynamodb_table.jmap_data.arn]
  }
}

resource "aws_iam_role_policy" "account_init_dynamodb" {
  name   = "${local.resource_prefix}-account-init-dynamodb-${var.environment}"
  role   = aws_iam_role.account_init_execution.id
  policy = data.aws_iam_policy_document.account_init_dynamodb.json
}

# IAM policy for Cognito access (AdminUpdateUserAttributes)
# Note: Using constructed ARN to avoid dependency cycle with cognito.tf
data "aws_iam_policy_document" "account_init_cognito" {
  statement {
    effect = "Allow"
    actions = [
      "cognito-idp:AdminUpdateUserAttributes",
    ]
    resources = [
      "arn:aws:cognito-idp:${data.aws_region.current.id}:${data.aws_caller_identity.current.account_id}:userpool/*"
    ]
  }
}

resource "aws_iam_role_policy" "account_init_cognito" {
  name   = "${local.resource_prefix}-account-init-cognito-${var.environment}"
  role   = aws_iam_role.account_init_execution.id
  policy = data.aws_iam_policy_document.account_init_cognito.json
}

# =============================================================================
# Lambda Function
# =============================================================================

resource "aws_lambda_function" "account_init" {
  filename         = "${path.module}/../../../build/account-init/lambda.zip"
  function_name    = "${local.resource_prefix}-account-init-${var.environment}"
  role             = aws_iam_role.account_init_execution.arn
  handler          = "bootstrap"
  source_code_hash = filebase64sha256("${path.module}/../../../build/account-init/lambda.zip")
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
      ENVIRONMENT         = var.environment
      DYNAMODB_TABLE      = aws_dynamodb_table.jmap_data.name
      DEFAULT_QUOTA_BYTES = tostring(var.default_quota_bytes)

      # ADOT Collector Configuration
      OPENTELEMETRY_COLLECTOR_CONFIG_URI = "file:///var/task/collector.yaml"
      OPENTELEMETRY_EXTENSION_LOG_LEVEL  = "error"

      # OpenTelemetry SDK Configuration
      OTEL_SERVICE_NAME   = "${local.resource_prefix}-account-init-${var.environment}"
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
    aws_iam_role_policy_attachment.account_init_basic_execution,
    aws_iam_role_policy_attachment.account_init_xray_access,
    aws_iam_role_policy.account_init_cloudwatch_metrics,
    aws_iam_role_policy.account_init_dynamodb,
    aws_iam_role_policy.account_init_cognito,
    aws_cloudwatch_log_group.account_init_logs
  ]

  tags = {
    Name        = "${local.resource_prefix}-account-init-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
    Function    = "account-init"
  }
}

# Permission for Cognito to invoke the Lambda
# Note: Using constructed ARN to avoid dependency cycle with cognito.tf
resource "aws_lambda_permission" "account_init_cognito" {
  statement_id  = "AllowCognitoInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.account_init.function_name
  principal     = "cognito-idp.amazonaws.com"
  source_arn    = "arn:aws:cognito-idp:${data.aws_region.current.id}:${data.aws_caller_identity.current.account_id}:userpool/*"
}

# =============================================================================
# CloudWatch Monitoring
# =============================================================================

# CloudWatch Log Metric Filter for errors
resource "aws_cloudwatch_log_metric_filter" "account_init_errors" {
  name           = "${local.resource_prefix}-account-init-errors-${var.environment}"
  log_group_name = aws_cloudwatch_log_group.account_init_logs.name
  pattern        = "[timestamp, request_id, level = ERROR*, ...]"

  metric_transformation {
    name      = "AccountInitErrorCount"
    namespace = "JMAPService/${var.environment}"
    value     = "1"
    unit      = "Count"
  }
}

# CloudWatch Alarm for account-init Lambda errors
resource "aws_cloudwatch_metric_alarm" "account_init_errors" {
  alarm_name          = "${local.resource_prefix}-account-init-errors-${var.environment}"
  alarm_description   = "Alerts when account-init Lambda has errors"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "Errors"
  namespace           = "AWS/Lambda"
  period              = 300
  statistic           = "Sum"
  threshold           = 1
  treat_missing_data  = "notBreaching"

  dimensions = {
    FunctionName = aws_lambda_function.account_init.function_name
  }

  tags = {
    Name        = "${local.resource_prefix}-account-init-errors-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
  }
}

# CloudWatch Log Anomaly Detector for account-init Lambda
resource "aws_cloudwatch_log_anomaly_detector" "account_init_anomaly" {
  log_group_arn_list   = [aws_cloudwatch_log_group.account_init_logs.arn]
  detector_name        = "${local.resource_prefix}-account-init-anomaly-${var.environment}"
  enabled              = true
  evaluation_frequency = "FIFTEEN_MIN"
}
