terraform {
  required_version = ">= 1.0"

  required_providers {
    aws = {
      source                = "hashicorp/aws"
      version               = "~> 6.0"
      configuration_aliases = [aws.us_east_1]
    }
    archive = {
      source  = "hashicorp/archive"
      version = "~> 2.0"
    }
  }
}

data "aws_region" "current" {}
data "aws_caller_identity" "current" {}

# Standardized resource naming and ADOT configuration
locals {
  # Standardized resource naming: jmap-service-core-{thing}-{component}-{env}
  resource_prefix = "jmap-service-core"

  # ADOT Collector account ID (constant across regions)
  adot_account_id = "901920570463"

  # ADOT Collector layer name (update version as needed)
  adot_layer_name    = "aws-otel-collector-arm64-ver-0-117-0"
  adot_layer_version = "1"

  # Construct ARN dynamically using current region
  adot_layer_arn = "arn:aws:lambda:${data.aws_region.current.id}:${local.adot_account_id}:layer:${local.adot_layer_name}:${local.adot_layer_version}"
}
