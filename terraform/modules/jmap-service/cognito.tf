# Cognito User Pool for JMAP Service authentication

resource "aws_cognito_user_pool" "main" {
  name = "jmap-service-${var.environment}"

  # Email-only sign-in
  username_attributes      = ["email"]
  auto_verified_attributes = ["email"]

  # Password policy (Cognito defaults)
  password_policy {
    minimum_length                   = 8
    require_lowercase                = true
    require_numbers                  = true
    require_symbols                  = true
    require_uppercase                = true
    temporary_password_validity_days = 7
  }

  # Account recovery via email
  account_recovery_setting {
    recovery_mechanism {
      name     = "verified_email"
      priority = 1
    }
  }

  # Schema - just use default attributes, sub claim is automatic
  schema {
    name                     = "email"
    attribute_data_type      = "String"
    mutable                  = true
    required                 = true
    developer_only_attribute = false

    string_attribute_constraints {
      min_length = 0
      max_length = 2048
    }
  }

  tags = {
    Name        = "jmap-service-${var.environment}"
    Environment = var.environment
    Service     = "jmap-service"
  }
}

# App Client for JMAP applications
resource "aws_cognito_user_pool_client" "jmap_client" {
  name         = "jmap-client"
  user_pool_id = aws_cognito_user_pool.main.id

  # No client secret for SPA/mobile compatibility
  generate_secret = false

  # Auth flows
  explicit_auth_flows = [
    "ALLOW_USER_SRP_AUTH",
    "ALLOW_REFRESH_TOKEN_AUTH",
    "ALLOW_ADMIN_USER_PASSWORD_AUTH"
  ]

  # Token validity
  access_token_validity  = 1  # 1 hour
  refresh_token_validity = 30 # 30 days
  id_token_validity      = 1  # 1 hour

  token_validity_units {
    access_token  = "hours"
    refresh_token = "days"
    id_token      = "hours"
  }

  # Prevent user existence errors (security best practice)
  prevent_user_existence_errors = "ENABLED"

  # OAuth configuration for mobile apps and websites
  allowed_oauth_flows                  = ["code"]
  allowed_oauth_flows_user_pool_client = true
  allowed_oauth_scopes                 = ["openid", "email", "profile"]
  supported_identity_providers         = ["COGNITO"]

  # Callback URLs (configure per environment)
  callback_urls = var.oauth_callback_urls
  logout_urls   = var.oauth_logout_urls
}

# Cognito User Pool Domain for OAuth endpoints
resource "aws_cognito_user_pool_domain" "main" {
  domain       = "jmap-service-${var.environment}"
  user_pool_id = aws_cognito_user_pool.main.id
}
