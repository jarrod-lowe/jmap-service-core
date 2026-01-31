# CloudWatch Dashboard for JMAP Service Core
#
# Provides comprehensive visibility into:
# - API Gateway performance (requests, errors, latency)
# - Lambda function metrics (invocations, errors, duration)
# - DynamoDB operations (capacity, throttling)
# - S3 blob storage (operations, storage usage)
# - Dead letter queues
# - Recent activity logs and errors

locals {
  dashboard_name = "${local.resource_prefix}-dashboard-${var.environment}"
}

resource "aws_cloudwatch_dashboard" "main" {
  dashboard_name = local.dashboard_name

  dashboard_body = jsonencode({
    widgets = concat(
      # Section Header: API Gateway
      [
        {
          type   = "text"
          x      = 0
          y      = 0
          width  = 24
          height = 1
          properties = {
            markdown = "## API Gateway Performance"
          }
        }
      ],

      # Section 1: API Gateway Performance (y=1)
      [
        {
          type   = "metric"
          x      = 0
          y      = 1
          width  = 12
          height = 6
          properties = {
            title  = "API Gateway - Requests & Errors"
            region = var.aws_region
            stat   = "Sum"
            period = 300
            metrics = [
              ["AWS/ApiGateway", "Count", "ApiName", aws_api_gateway_rest_api.api.name, { label = "Requests", color = "#2ca02c" }],
              [".", "4XXError", ".", ".", { label = "4XX Errors", color = "#ff7f0e" }],
              [".", "5XXError", ".", ".", { label = "5XX Errors", color = "#d62728" }]
            ]
            view = "timeSeries"
          }
        },
        {
          type   = "metric"
          x      = 12
          y      = 1
          width  = 12
          height = 6
          properties = {
            title  = "API Gateway - Latency"
            region = var.aws_region
            period = 300
            metrics = [
              ["AWS/ApiGateway", "Latency", "ApiName", aws_api_gateway_rest_api.api.name, { stat = "p50", label = "p50", color = "#1f77b4" }],
              ["...", { stat = "p90", label = "p90", color = "#ff7f0e" }],
              ["...", { stat = "p99", label = "p99", color = "#d62728" }]
            ]
            view = "timeSeries"
            yAxis = {
              left = {
                label     = "Milliseconds"
                showUnits = false
              }
            }
          }
        }
      ],

      # Section Header: Lambda Functions
      [
        {
          type   = "text"
          x      = 0
          y      = 7
          width  = 24
          height = 1
          properties = {
            markdown = "## Lambda Functions"
          }
        }
      ],

      # Section 2: Lambda Functions Overview (y=8)
      [
        {
          type   = "metric"
          x      = 0
          y      = 8
          width  = 12
          height = 6
          properties = {
            title  = "Core Lambdas - Invocations & Errors"
            region = var.aws_region
            stat   = "Sum"
            period = 300
            metrics = [
              ["AWS/Lambda", "Invocations", "FunctionName", aws_lambda_function.get_jmap_session.function_name, { label = "get-jmap-session", color = "#1f77b4" }],
              [".", "Errors", ".", ".", { label = "get-jmap-session errors", color = "#d62728" }],
              [".", "Invocations", ".", aws_lambda_function.jmap_api.function_name, { label = "jmap-api", color = "#2ca02c" }],
              [".", "Errors", ".", ".", { label = "jmap-api errors", color = "#ff7f0e" }]
            ]
            view = "timeSeries"
          }
        },
        {
          type   = "metric"
          x      = 12
          y      = 8
          width  = 12
          height = 6
          properties = {
            title  = "Blob Lambdas - Invocations & Errors"
            region = var.aws_region
            stat   = "Sum"
            period = 300
            metrics = [
              ["AWS/Lambda", "Invocations", "FunctionName", aws_lambda_function.blob_upload.function_name, { label = "blob-upload", color = "#1f77b4" }],
              [".", "Invocations", ".", aws_lambda_function.blob_download.function_name, { label = "blob-download", color = "#2ca02c" }],
              [".", "Invocations", ".", aws_lambda_function.blob_delete.function_name, { label = "blob-delete", color = "#ff7f0e" }],
              [".", "Invocations", ".", aws_lambda_function.blob_cleanup.function_name, { label = "blob-cleanup", color = "#9467bd" }],
              [".", "Invocations", ".", aws_lambda_function.blob_confirm.function_name, { label = "blob-confirm", color = "#8c564b" }],
              [".", "Errors", ".", aws_lambda_function.blob_upload.function_name, { label = "blob-upload errors", color = "#d62728", stat = "Sum" }],
              [".", "Errors", ".", aws_lambda_function.blob_download.function_name, { label = "blob-download errors", color = "#d62728", stat = "Sum" }],
              [".", "Errors", ".", aws_lambda_function.blob_delete.function_name, { label = "blob-delete errors", color = "#d62728", stat = "Sum" }],
              [".", "Errors", ".", aws_lambda_function.blob_cleanup.function_name, { label = "blob-cleanup errors", color = "#d62728", stat = "Sum" }],
              [".", "Errors", ".", aws_lambda_function.blob_confirm.function_name, { label = "blob-confirm errors", color = "#d62728", stat = "Sum" }]
            ]
            view = "timeSeries"
          }
        }
      ],

      # Section 3: Lambda Duration (y=14)
      [
        {
          type   = "metric"
          x      = 0
          y      = 14
          width  = 12
          height = 6
          properties = {
            title  = "Lambda Duration (p99)"
            region = var.aws_region
            stat   = "p99"
            period = 300
            metrics = [
              ["AWS/Lambda", "Duration", "FunctionName", aws_lambda_function.get_jmap_session.function_name, { label = "get-jmap-session" }],
              [".", ".", ".", aws_lambda_function.jmap_api.function_name, { label = "jmap-api" }],
              [".", ".", ".", aws_lambda_function.blob_upload.function_name, { label = "blob-upload" }],
              [".", ".", ".", aws_lambda_function.blob_download.function_name, { label = "blob-download" }],
              [".", ".", ".", aws_lambda_function.blob_delete.function_name, { label = "blob-delete" }]
            ]
            view = "timeSeries"
            yAxis = {
              left = {
                label     = "Milliseconds"
                showUnits = false
              }
            }
          }
        },
        {
          type   = "metric"
          x      = 12
          y      = 14
          width  = 12
          height = 6
          properties = {
            title  = "Lambda Concurrent Executions"
            region = var.aws_region
            stat   = "Maximum"
            period = 60
            metrics = [
              ["AWS/Lambda", "ConcurrentExecutions", "FunctionName", aws_lambda_function.jmap_api.function_name, { label = "jmap-api" }],
              [".", ".", ".", aws_lambda_function.blob_upload.function_name, { label = "blob-upload" }],
              [".", ".", ".", aws_lambda_function.blob_confirm.function_name, { label = "blob-confirm" }],
              [".", ".", ".", aws_lambda_function.blob_cleanup.function_name, { label = "blob-cleanup" }]
            ]
            view = "timeSeries"
          }
        }
      ],

      # Section Header: DynamoDB
      [
        {
          type   = "text"
          x      = 0
          y      = 20
          width  = 24
          height = 1
          properties = {
            markdown = "## DynamoDB Operations"
          }
        }
      ],

      # Section 4: DynamoDB Operations (y=21)
      [
        {
          type   = "metric"
          x      = 0
          y      = 21
          width  = 12
          height = 6
          properties = {
            title  = "DynamoDB - Consumed Capacity"
            region = var.aws_region
            stat   = "Sum"
            period = 300
            metrics = [
              ["AWS/DynamoDB", "ConsumedReadCapacityUnits", "TableName", aws_dynamodb_table.jmap_data.name, { label = "Read Capacity", color = "#1f77b4" }],
              [".", "ConsumedWriteCapacityUnits", ".", ".", { label = "Write Capacity", color = "#ff7f0e" }]
            ]
            view = "timeSeries"
          }
        },
        {
          type   = "metric"
          x      = 12
          y      = 21
          width  = 12
          height = 6
          properties = {
            title  = "DynamoDB - Throttling & Latency"
            region = var.aws_region
            period = 300
            metrics = [
              ["AWS/DynamoDB", "ThrottledRequests", "TableName", aws_dynamodb_table.jmap_data.name, { stat = "Sum", label = "Throttled Requests", color = "#d62728" }],
              [".", "SuccessfulRequestLatency", ".", ".", "Operation", "GetItem", { stat = "Average", label = "GetItem Latency (ms)", color = "#1f77b4", yAxis = "right" }],
              ["...", "PutItem", { stat = "Average", label = "PutItem Latency (ms)", color = "#2ca02c", yAxis = "right" }],
              ["...", "Query", { stat = "Average", label = "Query Latency (ms)", color = "#ff7f0e", yAxis = "right" }]
            ]
            view = "timeSeries"
            yAxis = {
              right = {
                label     = "Milliseconds"
                showUnits = false
              }
            }
          }
        }
      ],

      # Section Header: S3 Blob Storage
      [
        {
          type   = "text"
          x      = 0
          y      = 27
          width  = 24
          height = 1
          properties = {
            markdown = "## S3 Blob Storage"
          }
        }
      ],

      # Section 5: S3 Blob Storage (y=28)
      [
        {
          type   = "metric"
          x      = 0
          y      = 28
          width  = 12
          height = 6
          properties = {
            title  = "S3 - Blob Operations"
            region = var.aws_region
            stat   = "Sum"
            period = 300
            metrics = [
              ["AWS/S3", "PutRequests", "BucketName", aws_s3_bucket.blobs.bucket, "FilterId", "AllRequests", { label = "PutObject", color = "#1f77b4" }],
              [".", "GetRequests", ".", ".", ".", ".", { label = "GetObject", color = "#2ca02c" }],
              [".", "DeleteRequests", ".", ".", ".", ".", { label = "DeleteObject", color = "#d62728" }]
            ]
            view = "timeSeries"
          }
        },
        {
          type   = "metric"
          x      = 12
          y      = 28
          width  = 12
          height = 6
          properties = {
            title  = "S3 - Storage Usage"
            region = var.aws_region
            stat   = "Average"
            period = 86400
            metrics = [
              ["AWS/S3", "BucketSizeBytes", "BucketName", aws_s3_bucket.blobs.bucket, "StorageType", "StandardStorage", { label = "Bucket Size (bytes)", color = "#1f77b4" }],
              [".", "NumberOfObjects", ".", ".", ".", "AllStorageTypes", { label = "Number of Objects", color = "#2ca02c", yAxis = "right" }]
            ]
            view = "timeSeries"
            yAxis = {
              right = {
                label     = "Objects"
                showUnits = false
              }
            }
          }
        }
      ],

      # Section Header: Dead Letter Queues
      [
        {
          type   = "text"
          x      = 0
          y      = 34
          width  = 24
          height = 1
          properties = {
            markdown = "## Dead Letter Queues"
          }
        }
      ],

      # Section 6: Dead Letter Queues (y=35)
      [
        {
          type   = "metric"
          x      = 0
          y      = 35
          width  = 24
          height = 6
          properties = {
            title  = "Dead Letter Queues - Message Depth"
            region = var.aws_region
            stat   = "Maximum"
            period = 60
            metrics = [
              ["AWS/SQS", "ApproximateNumberOfMessagesVisible", "QueueName", aws_sqs_queue.blob_cleanup_dlq.name, { label = "blob-cleanup-dlq", color = "#d62728" }],
              [".", ".", ".", aws_sqs_queue.blob_confirm_dlq.name, { label = "blob-confirm-dlq", color = "#ff7f0e" }]
            ]
            view = "timeSeries"
          }
        }
      ],

      # Section Header: Activity Logs
      [
        {
          type   = "text"
          x      = 0
          y      = 41
          width  = 24
          height = 1
          properties = {
            markdown = "## Activity Logs"
          }
        }
      ],

      # Section 7: Blob Activity Log (y=42)
      [
        {
          type   = "log"
          x      = 0
          y      = 42
          width  = 24
          height = 8
          properties = {
            title  = "Recent Blob Operations"
            region = var.aws_region
            query = join("\n", [
              "SOURCE '${aws_cloudwatch_log_group.blob_upload_logs.name}' | SOURCE '${aws_cloudwatch_log_group.blob_confirm_logs.name}' | SOURCE '${aws_cloudwatch_log_group.blob_download_logs.name}' | SOURCE '${aws_cloudwatch_log_group.blob_delete_logs.name}'",
              "| fields @timestamp, @logStream as function, @message",
              "| filter @message like /blob_id|account_id/",
              "| parse @message '\"msg\":\"*\"' as operation",
              "| parse @message '\"account_id\":\"*\"' as account_id",
              "| parse @message '\"blob_id\":\"*\"' as blob_id",
              "| parse @message '\"size\":*}' as size",
              "| parse @message '\"size\":*,' as size2",
              "| display @timestamp, function, operation, account_id, blob_id, coalesce(size, size2) as blob_size",
              "| sort @timestamp desc",
              "| limit 100"
            ])
            view = "table"
          }
        }
      ],

      # Section 8: Recent Errors (y=50)
      [
        {
          type   = "log"
          x      = 0
          y      = 50
          width  = 24
          height = 8
          properties = {
            title  = "Recent Errors"
            region = var.aws_region
            query = join("\n", [
              "SOURCE '${aws_cloudwatch_log_group.get_jmap_session_logs.name}' | SOURCE '${aws_cloudwatch_log_group.jmap_api_logs.name}' | SOURCE '${aws_cloudwatch_log_group.blob_upload_logs.name}' | SOURCE '${aws_cloudwatch_log_group.blob_download_logs.name}' | SOURCE '${aws_cloudwatch_log_group.blob_delete_logs.name}' | SOURCE '${aws_cloudwatch_log_group.blob_cleanup_logs.name}' | SOURCE '${aws_cloudwatch_log_group.blob_confirm_logs.name}' | SOURCE '${aws_cloudwatch_log_group.core_echo_logs.name}'",
              "| fields @timestamp, @logStream as function, @message",
              "| filter @message like /\"level\":\"ERROR\"/ or @message like /(?i)error/",
              "| parse @message '\"level\":\"*\"' as level",
              "| parse @message '\"msg\":\"*\"' as msg",
              "| parse @message '\"error\":\"*\"' as error",
              "| parse @message '\"request_id\":\"*\"' as request_id",
              "| display @timestamp, function, level, msg, error, request_id",
              "| sort @timestamp desc",
              "| limit 100"
            ])
            view = "table"
          }
        }
      ]
    )
  })

}
