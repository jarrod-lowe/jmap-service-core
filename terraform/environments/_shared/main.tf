terraform {
  required_version = ">= 1.0"

  backend "s3" {
    # Bucket name is set via -backend-config in Makefile
    # Key includes environment from -backend-config
    # Region is set via -backend-config
    encrypt = true
  }

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
    archive = {
      source  = "hashicorp/archive"
      version = "~> 2.0"
    }
    tls = {
      source  = "hashicorp/tls"
      version = "~> 4.0"
    }
    time = {
      source  = "hashicorp/time"
      version = "~> 0.9"
    }
  }
}

provider "aws" {
  region = var.aws_region

  default_tags {
    tags = {
      Project     = "jmap-service"
      ManagedBy   = "terraform"
      Environment = var.environment
      Application = "jmap-service-${var.environment}"
    }
  }
}

# CloudFront requires ACM certificates in us-east-1
provider "aws" {
  alias  = "us_east_1"
  region = "us-east-1"

  default_tags {
    tags = {
      Project     = "jmap-service"
      ManagedBy   = "terraform"
      Environment = var.environment
      Application = "jmap-service-${var.environment}"
    }
  }
}

module "jmap_service" {
  source = "../../modules/jmap-service"

  providers = {
    aws           = aws
    aws.us_east_1 = aws.us_east_1
  }

  aws_region                            = var.aws_region
  environment                           = var.environment
  log_retention_days                    = var.log_retention_days
  lambda_memory_size                    = var.lambda_memory_size
  lambda_timeout                        = var.lambda_timeout
  domain_name                           = var.domain_name
  signed_url_expiry_seconds             = var.signed_url_expiry_seconds
  cloudfront_signing_key_rotation_phase = var.cloudfront_signing_key_rotation_phase
  cloudfront_signing_key_max_age_days   = var.cloudfront_signing_key_max_age_days
}
