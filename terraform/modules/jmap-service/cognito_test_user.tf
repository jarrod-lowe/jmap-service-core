# Generate random password for each test user
resource "random_password" "test_user" {
  count = length(var.test_user_emails)

  length  = 20
  special = true
  # Use Cognito-compatible special characters
  override_special = "!@#$%^&*()-_=+[]{}|;:,.<>?"

  # Ensure password meets Cognito requirements
  min_lower   = 1
  min_upper   = 1
  min_numeric = 1
  min_special = 1
}

# Create test users in Cognito
resource "aws_cognito_user" "test_user" {
  count = length(var.test_user_emails)

  user_pool_id = aws_cognito_user_pool.main.id
  username     = var.test_user_emails[count.index]

  # Set permanent password directly (no temporary password needed)
  password = random_password.test_user[count.index].result

  # Suppress welcome email
  message_action = "SUPPRESS"

  # User is enabled and ready to authenticate
  enabled = true

  attributes = {
    email          = var.test_user_emails[count.index]
    email_verified = "true" # Skip email verification for test users
  }

  # Ensure account-init Lambda is ready
  depends_on = [aws_lambda_permission.account_init_cognito]

  lifecycle {
    # If email changes, replace the user
    create_before_destroy = false

    # Ignore password changes after creation
    ignore_changes = [password]
  }
}

