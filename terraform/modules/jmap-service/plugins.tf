# Plugin records for JMAP capabilities

# Core JMAP capability - provides RFC 8620 required fields
resource "aws_dynamodb_table_item" "core_plugin" {
  table_name = aws_dynamodb_table.jmap_data.name
  hash_key   = "pk"
  range_key  = "sk"

  item = jsonencode({
    pk       = { S = "PLUGIN#" }
    sk       = { S = "PLUGIN#core" }
    pluginId = { S = "core" }
    capabilities = {
      M = {
        "urn:ietf:params:jmap:core" = {
          M = {
            maxSizeUpload         = { N = "10000000" }
            maxConcurrentUpload   = { N = "4" }
            maxSizeRequest        = { N = "10000000" }
            maxConcurrentRequests = { N = "4" }
            maxCallsInRequest     = { N = "16" }
            maxObjectsInGet       = { N = "500" }
            maxObjectsInSet       = { N = "500" }
            collationAlgorithms   = { L = [{ S = "i;ascii-casemap" }] }
          }
        }
      }
    }
    methods = {
      M = {
        "Core/echo" = {
          M = {
            invocationType = { S = "lambda-invoke" }
            invokeTarget   = { S = aws_lambda_function.core_echo.arn }
          }
        }
      }
    }
    registeredAt = { S = "2025-01-17T00:00:00Z" }
    version      = { S = "1.0.0" }
  })
}
