# JMAP Plugin Interface

This document describes the plugin interface for extending the JMAP service with additional capabilities.

## Overview

Plugins extend the JMAP service by registering handlers for JMAP method calls. The core service discovers plugins via DynamoDB records and dispatches method calls to plugin Lambda functions.

## Infrastructure Discovery

Plugins discover core infrastructure values via AWS SSM Parameter Store. This eliminates hardcoded values and cross-stack dependencies.

### Available Parameters

All parameters are under the path `/${resource_prefix}/${environment}/`:

| Parameter | Description |
|-----------|-------------|
| `api-gateway-execution-arn` | API Gateway execution ARN for Lambda permissions |
| `api-url` | Public API URL (CloudFront/custom domain) |
| `dynamodb-table-name` | Core DynamoDB table name for plugin registration |
| `dynamodb-table-arn` | Core DynamoDB table ARN for IAM policies |

### Example: Discovering Parameters in Terraform

```hcl
variable "environment" {
  description = "Deployment environment"
  type        = string
}

locals {
  ssm_prefix = "/jmap-service-core/${var.environment}"
}

data "aws_ssm_parameter" "jmap_table_name" {
  name = "${local.ssm_prefix}/dynamodb-table-name"
}

data "aws_ssm_parameter" "jmap_table_arn" {
  name = "${local.ssm_prefix}/dynamodb-table-arn"
}

data "aws_ssm_parameter" "jmap_api_url" {
  name = "${local.ssm_prefix}/api-url"
}

data "aws_ssm_parameter" "jmap_api_gateway_execution_arn" {
  name = "${local.ssm_prefix}/api-gateway-execution-arn"
}

# Use in resources:
resource "aws_dynamodb_table_item" "plugin_registration" {
  table_name = data.aws_ssm_parameter.jmap_table_name.value
  # ...
}
```

## Plugin Registration

Plugins register themselves by creating a DynamoDB record in the core service's table.

### DynamoDB Record Schema

| Field | Type | Description |
| ----- | ---- | ----------- |
| `pk` | String | Always `"PLUGIN#"` |
| `sk` | String | `"PLUGIN#<plugin-name>"` |
| `pluginId` | String | Unique identifier for the plugin |
| `capabilities` | Map | JMAP capabilities provided by this plugin |
| `methods` | Map | Method name to invocation target mapping |
| `registeredAt` | String | RFC3339 timestamp of registration |
| `version` | String | Plugin version |

### Example Record

```json
{
  "pk": "PLUGIN#",
  "sk": "PLUGIN#mail-core",
  "pluginId": "mail-core",
  "capabilities": {
    "urn:ietf:params:jmap:mail": {
      "maxMailboxesPerEmail": null,
      "maxMailboxDepth": 10
    }
  },
  "methods": {
    "Email/get": {
      "invocationType": "lambda-invoke",
      "invokeTarget": "arn:aws:lambda:ap-southeast-2:123456789:function:jmap-plugin-mail-email-read"
    },
    "Email/query": {
      "invocationType": "lambda-invoke",
      "invokeTarget": "arn:aws:lambda:ap-southeast-2:123456789:function:jmap-plugin-mail-email-read"
    },
    "Email/import": {
      "invocationType": "lambda-invoke",
      "invokeTarget": "arn:aws:lambda:ap-southeast-2:123456789:function:jmap-plugin-mail-email-import"
    }
  },
  "registeredAt": "2025-01-17T10:00:00Z",
  "version": "1.0.0"
}
```

### Capabilities

The `capabilities` map defines JMAP capabilities this plugin provides. Keys are capability URNs (e.g., `urn:ietf:params:jmap:mail`), values are the capability configuration objects returned in the JMAP session response.

### Methods

The `methods` map defines which JMAP methods this plugin handles. Keys are method names (e.g., `Email/get`), values define how to invoke the handler:

| Field | Type | Description |
| ----- | ---- | ----------- |
| `invocationType` | String | Currently only `"lambda-invoke"` is supported |
| `invokeTarget` | String | Lambda function ARN |

## Plugin Invocation Contract

### Request Payload (Core to Plugin)

When the core service invokes a plugin, it sends this JSON payload:

```json
{
  "requestId": "apigw-request-id",
  "callIndex": 0,
  "accountId": "user-123",
  "method": "Email/get",
  "args": {
    "accountId": "user-123",
    "ids": ["email-1", "email-2"],
    "properties": ["id", "subject", "from"]
  },
  "clientId": "c0"
}
```

| Field | Type | Description |
| ----- | ---- | ----------- |
| `requestId` | String | API Gateway request ID for correlation |
| `callIndex` | Integer | Position of this call in the methodCalls array |
| `accountId` | String | Authenticated account ID |
| `method` | String | JMAP method name |
| `args` | Object | Method arguments from the JMAP request |
| `clientId` | String | Client-provided call identifier |

### Success Response (Plugin to Core)

```json
{
  "methodResponse": {
    "name": "Email/get",
    "args": {
      "accountId": "user-123",
      "list": [...],
      "notFound": []
    },
    "clientId": "c0"
  }
}
```

| Field | Type | Description |
| ----- | ---- | ----------- |
| `methodResponse.name` | String | Method name for the response |
| `methodResponse.args` | Object | JMAP response data |
| `methodResponse.clientId` | String | Echo back the client ID from request |

### Error Response (Plugin to Core)

For JMAP-level errors (invalid arguments, not found, etc.):

```json
{
  "methodResponse": {
    "name": "error",
    "args": {
      "type": "invalidArguments",
      "description": "ids is required"
    },
    "clientId": "c0"
  }
}
```

Standard JMAP error types (RFC 8620 Section 3.6.2):

- `unknownMethod` - Method not supported
- `invalidArguments` - Invalid method arguments
- `invalidResultReference` - Back-reference resolution failed
- `forbidden` - Not authorized
- `accountNotFound` - Account doesn't exist
- `accountNotSupportedByMethod` - Method not available for this account
- `accountReadOnly` - Write attempted on read-only account
- `serverFail` - Internal server error
- `serverUnavailable` - Server temporarily unavailable
- `serverPartialFail` - Some operations succeeded
- `unknownCapability` - Capability not supported

## Error Handling

### Plugin Responsibilities

1. Return valid JSON responses always
2. Use JMAP error types for application-level errors
3. Include the `clientId` in all responses
4. Handle timeouts gracefully (core enforces 25s limit)

### Core Service Handling

1. **Lambda invocation failure** (timeout, crash): Returns `serverFail` error
2. **Invalid JSON response**: Returns `serverFail` error
3. **Plugin JMAP errors**: Passed through to client unchanged
4. **Partial failure**: Remaining method calls continue processing

## Example Plugin Terraform

```hcl
# variables.tf
variable "environment" {
  description = "Deployment environment (test, prod)"
  type        = string
}

variable "plugin_name" {
  description = "Plugin identifier"
  type        = string
  default     = "mail-core"
}

variable "plugin_version" {
  description = "Plugin version"
  type        = string
  default     = "1.0.0"
}

# ssm_discovery.tf - Discover core infrastructure via SSM
locals {
  ssm_prefix = "/jmap-service-core/${var.environment}"
}

data "aws_ssm_parameter" "jmap_table_name" {
  name = "${local.ssm_prefix}/dynamodb-table-name"
}

data "aws_ssm_parameter" "jmap_table_arn" {
  name = "${local.ssm_prefix}/dynamodb-table-arn"
}

data "aws_ssm_parameter" "jmap_api_gateway_execution_arn" {
  name = "${local.ssm_prefix}/api-gateway-execution-arn"
}

# lambda.tf
resource "aws_lambda_function" "email_read" {
  function_name = "jmap-plugin-${var.plugin_name}-email-read"
  runtime       = "provided.al2023"
  architectures = ["arm64"]
  handler       = "bootstrap"
  # ... other configuration
}

resource "aws_lambda_function" "email_import" {
  function_name = "jmap-plugin-${var.plugin_name}-email-import"
  runtime       = "provided.al2023"
  architectures = ["arm64"]
  handler       = "bootstrap"
  # ... other configuration
}

# registration.tf
resource "aws_dynamodb_table_item" "plugin_registration" {
  table_name = data.aws_ssm_parameter.jmap_table_name.value
  hash_key   = "pk"
  range_key  = "sk"

  item = jsonencode({
    pk = { S = "PLUGIN#" }
    sk = { S = "PLUGIN#${var.plugin_name}" }
    pluginId = { S = var.plugin_name }
    capabilities = {
      M = {
        "urn:ietf:params:jmap:mail" = {
          M = {
            maxMailboxesPerEmail = { NULL = true }
            maxMailboxDepth = { N = "10" }
          }
        }
      }
    }
    methods = {
      M = {
        "Email/get" = {
          M = {
            invocationType = { S = "lambda-invoke" }
            invokeTarget = { S = aws_lambda_function.email_read.arn }
          }
        }
        "Email/query" = {
          M = {
            invocationType = { S = "lambda-invoke" }
            invokeTarget = { S = aws_lambda_function.email_read.arn }
          }
        }
        "Email/import" = {
          M = {
            invocationType = { S = "lambda-invoke" }
            invokeTarget = { S = aws_lambda_function.email_import.arn }
          }
        }
      }
    }
    registeredAt = { S = timestamp() }
    version = { S = var.plugin_version }
  })
}

# iam.tf - Grant core service permission to invoke plugin lambdas
resource "aws_lambda_permission" "allow_jmap_core_email_read" {
  statement_id  = "AllowJMAPCoreInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.email_read.function_name
  principal     = "lambda.amazonaws.com"
  source_arn    = "${data.aws_ssm_parameter.jmap_api_gateway_execution_arn.value}/*"
}

resource "aws_lambda_permission" "allow_jmap_core_email_import" {
  statement_id  = "AllowJMAPCoreInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.email_import.function_name
  principal     = "lambda.amazonaws.com"
  source_arn    = "${data.aws_ssm_parameter.jmap_api_gateway_execution_arn.value}/*"
}
```

## Session Discovery

The JMAP session endpoint (`/.well-known/jmap`) automatically includes capabilities from all registered plugins. The core service:

1. Queries all records with `pk = "PLUGIN#"`
2. Aggregates capabilities from all plugins
3. Merges with core capabilities (`urn:ietf:params:jmap:core`)
4. Returns combined session object

Clients can then use any capability advertised in the session.

## Best Practices

1. **Single responsibility**: Each Lambda should handle related methods (e.g., read operations vs. write operations)
2. **Idempotency**: Import operations should be idempotent using Message-ID or similar
3. **Timeouts**: Plugin Lambdas should complete within 25 seconds
4. **Logging**: Include `requestId` and `accountId` in all log entries for tracing
5. **Versioning**: Update the `version` field when changing plugin behaviour

## Go Plugin Development

For Go-based plugins, import the contract types directly from the core module:

```go
import "github.com/jarrod-lowe/jmap-service-core/pkg/plugincontract"

func handler(ctx context.Context, req plugincontract.PluginInvocationRequest) (plugincontract.PluginInvocationResponse, error) {
    // Handle the request
    return plugincontract.PluginInvocationResponse{
        MethodResponse: plugincontract.MethodResponse{
            Name:     req.Method,
            Args:     map[string]any{"accountId": req.AccountID},
            ClientID: req.ClientID,
        },
    }, nil
}
```

Available types in `pkg/plugincontract`:

- `PluginInvocationRequest` - Request payload sent from core to plugin
- `PluginInvocationResponse` - Response wrapper from plugin to core
- `MethodResponse` - JMAP method response structure
