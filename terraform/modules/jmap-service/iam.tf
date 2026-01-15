# IAM policy document for Lambda assume role
data "aws_iam_policy_document" "lambda_assume_role" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["lambda.amazonaws.com"]
    }
  }
}

# IAM role for hello-world Lambda function
resource "aws_iam_role" "hello_world_execution" {
  name               = "${local.resource_prefix}-hello-world-execution-${var.environment}"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume_role.json

  tags = {
    Name        = "${local.resource_prefix}-hello-world-execution-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
    Function    = "hello-world"
  }
}

# Attach AWS managed policy for basic Lambda execution
resource "aws_iam_role_policy_attachment" "hello_world_basic_execution" {
  role       = aws_iam_role.hello_world_execution.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

# Attach AWS managed policy for X-Ray tracing
resource "aws_iam_role_policy_attachment" "hello_world_xray_access" {
  role       = aws_iam_role.hello_world_execution.name
  policy_arn = "arn:aws:iam::aws:policy/AWSXRayDaemonWriteAccess"
}

# IAM policy for CloudWatch Metrics
data "aws_iam_policy_document" "hello_world_cloudwatch_metrics" {
  statement {
    effect = "Allow"
    actions = [
      "cloudwatch:PutMetricData"
    ]
    resources = ["*"]

    condition {
      test     = "StringEquals"
      variable = "cloudwatch:namespace"
      values   = ["JMAPService"]
    }
  }
}

resource "aws_iam_role_policy" "hello_world_cloudwatch_metrics" {
  name   = "${local.resource_prefix}-hello-world-cloudwatch-metrics-${var.environment}"
  role   = aws_iam_role.hello_world_execution.id
  policy = data.aws_iam_policy_document.hello_world_cloudwatch_metrics.json
}

# IAM role for get-jmap-session Lambda function
resource "aws_iam_role" "get_jmap_session_execution" {
  name               = "${local.resource_prefix}-get-jmap-session-execution-${var.environment}"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume_role.json

  tags = {
    Name        = "${local.resource_prefix}-get-jmap-session-execution-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
    Function    = "get-jmap-session"
  }
}

# Attach AWS managed policy for basic Lambda execution
resource "aws_iam_role_policy_attachment" "get_jmap_session_basic_execution" {
  role       = aws_iam_role.get_jmap_session_execution.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

# Attach AWS managed policy for X-Ray tracing
resource "aws_iam_role_policy_attachment" "get_jmap_session_xray_access" {
  role       = aws_iam_role.get_jmap_session_execution.name
  policy_arn = "arn:aws:iam::aws:policy/AWSXRayDaemonWriteAccess"
}

# IAM policy for CloudWatch Metrics (reuse same policy document)
resource "aws_iam_role_policy" "get_jmap_session_cloudwatch_metrics" {
  name   = "${local.resource_prefix}-get-jmap-session-cloudwatch-metrics-${var.environment}"
  role   = aws_iam_role.get_jmap_session_execution.id
  policy = data.aws_iam_policy_document.hello_world_cloudwatch_metrics.json
}
