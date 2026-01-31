variable "aws_region" {
  description = "AWS region for resources"
  type        = string
  default     = "ap-southeast-2"
}

variable "environment" {
  description = "Environment name (test, prod)"
  type        = string

  validation {
    condition     = contains(["test", "prod"], var.environment)
    error_message = "Environment must be either 'test' or 'prod'"
  }
}

variable "log_retention_days" {
  description = "CloudWatch log retention in days"
  type        = number
  default     = 30

  validation {
    condition     = contains([1, 3, 5, 7, 14, 30, 60, 90, 120, 150, 180, 365, 400, 545, 731, 1096, 1827, 2192, 2557, 2922, 3288, 3653], var.log_retention_days)
    error_message = "Log retention days must be a valid CloudWatch retention period"
  }
}

variable "lambda_memory_size" {
  description = "Lambda memory size in MB"
  type        = number
  default     = 256

  validation {
    condition     = var.lambda_memory_size >= 128 && var.lambda_memory_size <= 10240
    error_message = "Lambda memory size must be between 128 and 10240 MB"
  }
}

variable "lambda_timeout" {
  description = "Lambda timeout in seconds"
  type        = number
  default     = 30

  validation {
    condition     = var.lambda_timeout >= 1 && var.lambda_timeout <= 900
    error_message = "Lambda timeout must be between 1 and 900 seconds"
  }
}

variable "domain_name" {
  description = "FQDN for the API (e.g., api.example.com)"
  type        = string
}

variable "oauth_callback_urls" {
  description = "OAuth callback URLs for apps and websites"
  type        = list(string)
  default     = ["http://localhost:3000/callback"]
}

variable "oauth_logout_urls" {
  description = "OAuth logout redirect URLs"
  type        = list(string)
  default     = ["http://localhost:3000/logout"]
}

variable "signed_url_expiry_seconds" {
  description = "Expiry time in seconds for CloudFront signed URLs"
  type        = number
  default     = 300

  validation {
    condition     = var.signed_url_expiry_seconds >= 60 && var.signed_url_expiry_seconds <= 604800
    error_message = "Signed URL expiry must be between 60 seconds (1 minute) and 604800 seconds (7 days)"
  }
}

variable "cloudfront_signing_key_rotation_phase" {
  description = "CloudFront signing key rotation phase: 'normal', 'rotating', or 'complete'"
  type        = string
  default     = "normal"

  validation {
    condition     = contains(["normal", "rotating", "complete"], var.cloudfront_signing_key_rotation_phase)
    error_message = "Must be 'normal', 'rotating', or 'complete'"
  }
}

variable "cloudfront_signing_key_max_age_days" {
  description = "Days before CloudFront signing key age alarm triggers"
  type        = number
  default     = 180
}

variable "iam_client_principals" {
  description = "IAM role ARNs authorized to access IAM-authenticated endpoints. These principals are registered by the core plugin."
  type        = list(string)
  default     = []
}

# PUT Upload Extension Variables

variable "allocation_url_expiry_seconds" {
  description = "Pre-signed URL validity period for Blob/allocate"
  type        = number
  default     = 900 # 15 minutes

  validation {
    condition     = var.allocation_url_expiry_seconds >= 60 && var.allocation_url_expiry_seconds <= 3600
    error_message = "Allocation URL expiry must be between 60 seconds (1 minute) and 3600 seconds (1 hour)"
  }
}

variable "allocation_cleanup_buffer_hours" {
  description = "Hours after URL expiry before cleanup processes pending allocation records"
  type        = number
  default     = 72 # 3 days

  validation {
    condition     = var.allocation_cleanup_buffer_hours >= 1 && var.allocation_cleanup_buffer_hours <= 168
    error_message = "Allocation cleanup buffer must be between 1 and 168 hours (7 days)"
  }
}

variable "max_size_upload_put" {
  description = "Maximum blob size for PUT upload in bytes"
  type        = number
  default     = 250000000 # 250 MB

  validation {
    condition     = var.max_size_upload_put >= 1000000 && var.max_size_upload_put <= 5368709120
    error_message = "Max PUT upload size must be between 1 MB and 5 GB"
  }
}

variable "max_pending_allocations" {
  description = "Maximum pending allocations per account"
  type        = number
  default     = 4

  validation {
    condition     = var.max_pending_allocations >= 1 && var.max_pending_allocations <= 100
    error_message = "Max pending allocations must be between 1 and 100"
  }
}

variable "default_quota_bytes" {
  description = "Default storage quota for new accounts in bytes"
  type        = number
  default     = 1073741824 # 1 GB

  validation {
    condition     = var.default_quota_bytes >= 10485760 && var.default_quota_bytes <= 1099511627776
    error_message = "Default quota must be between 10 MB and 1 TB"
  }
}

variable "cors_allowed_origins" {
  description = "Origins allowed for CORS PUT uploads"
  type        = list(string)
  default     = ["*"]
}

# CloudWatch Alarm Configuration

variable "alarm_sns_topic_arn" {
  description = "SNS topic ARN for alarm notifications"
  type        = string
}

variable "anomaly_detection_enabled" {
  description = "Enable CloudWatch anomaly detection"
  type        = bool
  default     = true
}

variable "anomaly_detection_evaluation_frequency" {
  description = "Anomaly detection evaluation frequency in seconds"
  type        = number
  default     = 900 # 15 minutes

  validation {
    condition     = contains([300, 900, 1800, 3600], var.anomaly_detection_evaluation_frequency)
    error_message = "Must be 300 (5min), 900 (15min), 1800 (30min), or 3600 (1hr)"
  }
}
