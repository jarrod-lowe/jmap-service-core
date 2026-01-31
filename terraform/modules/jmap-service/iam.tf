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

# IAM policy for CloudWatch Metrics
data "aws_iam_policy_document" "cloudwatch_metrics" {
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
  policy = data.aws_iam_policy_document.cloudwatch_metrics.json
}

# IAM policy for DynamoDB access (Query for plugins + account operations)
data "aws_iam_policy_document" "get_jmap_session_dynamodb" {
  statement {
    effect = "Allow"
    actions = [
      "dynamodb:GetItem",
      "dynamodb:UpdateItem",
      "dynamodb:Query"
    ]
    resources = [aws_dynamodb_table.jmap_data.arn]
  }
}

resource "aws_iam_role_policy" "get_jmap_session_dynamodb" {
  name   = "${local.resource_prefix}-get-jmap-session-dynamodb-${var.environment}"
  role   = aws_iam_role.get_jmap_session_execution.id
  policy = data.aws_iam_policy_document.get_jmap_session_dynamodb.json
}

# =============================================================================
# IAM resources for jmap-api Lambda function
# =============================================================================

# IAM role for jmap-api Lambda function
resource "aws_iam_role" "jmap_api_execution" {
  name               = "${local.resource_prefix}-jmap-api-execution-${var.environment}"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume_role.json

  tags = {
    Name        = "${local.resource_prefix}-jmap-api-execution-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
    Function    = "jmap-api"
  }
}

# Attach AWS managed policy for basic Lambda execution
resource "aws_iam_role_policy_attachment" "jmap_api_basic_execution" {
  role       = aws_iam_role.jmap_api_execution.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

# Attach AWS managed policy for X-Ray tracing
resource "aws_iam_role_policy_attachment" "jmap_api_xray_access" {
  role       = aws_iam_role.jmap_api_execution.name
  policy_arn = "arn:aws:iam::aws:policy/AWSXRayDaemonWriteAccess"
}

# IAM policy for CloudWatch Metrics
resource "aws_iam_role_policy" "jmap_api_cloudwatch_metrics" {
  name   = "${local.resource_prefix}-jmap-api-cloudwatch-metrics-${var.environment}"
  role   = aws_iam_role.jmap_api_execution.id
  policy = data.aws_iam_policy_document.cloudwatch_metrics.json
}

# IAM policy for DynamoDB access (Query for plugins + read/write operations for Blob/allocate)
data "aws_iam_policy_document" "jmap_api_dynamodb" {
  statement {
    effect = "Allow"
    actions = [
      "dynamodb:GetItem",
      "dynamodb:Query",
      "dynamodb:TransactWriteItems", # For Blob/allocate transactions
      "dynamodb:UpdateItem",         # Required for Update operations within transactions
      "dynamodb:PutItem",            # Required for Put operations within transactions
    ]
    resources = [aws_dynamodb_table.jmap_data.arn]
  }
}

resource "aws_iam_role_policy" "jmap_api_dynamodb" {
  name   = "${local.resource_prefix}-jmap-api-dynamodb-${var.environment}"
  role   = aws_iam_role.jmap_api_execution.id
  policy = data.aws_iam_policy_document.jmap_api_dynamodb.json
}

# IAM policy for Lambda invocation (to call plugin Lambdas)
data "aws_iam_policy_document" "jmap_api_lambda_invoke" {
  statement {
    effect = "Allow"
    actions = [
      "lambda:InvokeFunction"
    ]
    # Allow invoking any Lambda - plugins will be external
    resources = ["*"]
  }
}

resource "aws_iam_role_policy" "jmap_api_lambda_invoke" {
  name   = "${local.resource_prefix}-jmap-api-lambda-invoke-${var.environment}"
  role   = aws_iam_role.jmap_api_execution.id
  policy = data.aws_iam_policy_document.jmap_api_lambda_invoke.json
}

# IAM policy for S3 presigning (for Blob/allocate PUT URLs)
data "aws_iam_policy_document" "jmap_api_s3_presign" {
  statement {
    effect = "Allow"
    actions = [
      "s3:PutObject",
      "s3:PutObjectTagging",
    ]
    resources = ["${aws_s3_bucket.blobs.arn}/*"]
  }
}

resource "aws_iam_role_policy" "jmap_api_s3_presign" {
  name   = "${local.resource_prefix}-jmap-api-s3-presign-${var.environment}"
  role   = aws_iam_role.jmap_api_execution.id
  policy = data.aws_iam_policy_document.jmap_api_s3_presign.json
}
