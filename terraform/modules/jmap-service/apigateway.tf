# REST API Gateway with OpenAPI specification

locals {
  openapi_body = templatefile("${path.module}/openapi.yaml", {
    cognito_user_pool_arn       = aws_cognito_user_pool.main.arn
    aws_region                  = var.aws_region
    get_jmap_session_lambda_arn = aws_lambda_function.get_jmap_session.arn
    jmap_api_lambda_arn         = aws_lambda_function.jmap_api.arn
    blob_upload_lambda_arn      = aws_lambda_function.blob_upload.arn
    blob_download_lambda_arn    = aws_lambda_function.blob_download.arn
    blob_delete_lambda_arn      = aws_lambda_function.blob_delete.arn
  })
}

resource "aws_api_gateway_rest_api" "api" {
  name        = "jmap-service-${var.environment}"
  description = "JMAP Service API"

  body = local.openapi_body

  # Explicitly set binary media types to only blob upload content types.
  # This prevents */* from being added which would base64-encode all bodies
  # including application/json requests to the JMAP API.
  binary_media_types = [
    "application/octet-stream",
    "message/rfc822",
  ]

  endpoint_configuration {
    types = ["REGIONAL"]
  }
}

resource "aws_api_gateway_deployment" "api" {
  rest_api_id = aws_api_gateway_rest_api.api.id

  triggers = {
    # Include both openapi body and binary media types to ensure redeployment
    # when either changes
    redeployment = sha1(jsonencode({
      body               = local.openapi_body
      binary_media_types = aws_api_gateway_rest_api.api.binary_media_types
    }))
  }

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_api_gateway_stage" "v1" {
  deployment_id = aws_api_gateway_deployment.api.id
  rest_api_id   = aws_api_gateway_rest_api.api.id
  stage_name    = "v1"

  access_log_settings {
    destination_arn = aws_cloudwatch_log_group.api_gateway_logs.arn
    format = jsonencode({
      requestId      = "$context.requestId"
      ip             = "$context.identity.sourceIp"
      caller         = "$context.identity.caller"
      user           = "$context.identity.user"
      requestTime    = "$context.requestTime"
      httpMethod     = "$context.httpMethod"
      resourcePath   = "$context.resourcePath"
      status         = "$context.status"
      protocol       = "$context.protocol"
      responseLength = "$context.responseLength"
    })
  }

  depends_on = [aws_api_gateway_account.api]
}

# CloudWatch log group for API Gateway access logs
resource "aws_cloudwatch_log_group" "api_gateway_logs" {
  name              = "/aws/apigateway/jmap-service-${var.environment}"
  retention_in_days = var.log_retention_days
}

# IAM role for API Gateway CloudWatch logging
resource "aws_api_gateway_account" "api" {
  cloudwatch_role_arn = aws_iam_role.api_gateway_cloudwatch.arn
}

resource "aws_iam_role" "api_gateway_cloudwatch" {
  name = "jmap-service-apigw-cloudwatch-${var.environment}"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRole"
        Effect = "Allow"
        Principal = {
          Service = "apigateway.amazonaws.com"
        }
      }
    ]
  })
}

resource "aws_iam_role_policy_attachment" "api_gateway_cloudwatch" {
  role       = aws_iam_role.api_gateway_cloudwatch.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonAPIGatewayPushToCloudWatchLogs"
}
