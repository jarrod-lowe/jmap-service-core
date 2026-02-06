# IAM role for e2e test client
#
# A dedicated role that can be assumed by any principal in the account
# (e.g. the SSO admin role used by developers). It is automatically
# registered as an allowed IAM client principal and has execute-api:Invoke
# permission on the IAM-authenticated API Gateway endpoints.

data "aws_iam_policy_document" "e2e_test_client_assume" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRole"]
    principals {
      type        = "AWS"
      identifiers = ["arn:aws:iam::${data.aws_caller_identity.current.account_id}:root"]
    }
  }
}

resource "aws_iam_role" "e2e_test_client" {
  name               = "${local.resource_prefix}-e2e-test-client-${var.environment}"
  assume_role_policy = data.aws_iam_policy_document.e2e_test_client_assume.json
}

data "aws_iam_policy_document" "e2e_test_api_invoke" {
  statement {
    effect  = "Allow"
    actions = ["execute-api:Invoke"]
    resources = [
      "${aws_api_gateway_rest_api.api.execution_arn}/*/POST/jmap-iam/*",
      "${aws_api_gateway_rest_api.api.execution_arn}/*/POST/upload-iam/*",
      "${aws_api_gateway_rest_api.api.execution_arn}/*/GET/download-iam/*",
      "${aws_api_gateway_rest_api.api.execution_arn}/*/DELETE/delete-iam/*",
    ]
  }
}

resource "aws_iam_role_policy" "e2e_test_api_invoke" {
  name   = "api-gateway-invoke"
  role   = aws_iam_role.e2e_test_client.id
  policy = data.aws_iam_policy_document.e2e_test_api_invoke.json
}
