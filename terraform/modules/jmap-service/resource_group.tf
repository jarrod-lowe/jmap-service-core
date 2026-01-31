# AWS Resource Group for JMAP Service Core
#
# Groups all JMAP Service resources by Project and Environment tags
# for organized resource management in the AWS Console.

resource "aws_resourcegroups_group" "jmap_service" {
  name        = "jmap-service-core-${var.environment}"
  description = "All JMAP Service Core resources for the ${var.environment} environment"

  resource_query {
    query = jsonencode({
      ResourceTypeFilters = ["AWS::AllSupported"]
      TagFilters = [
        {
          Key    = "Project"
          Values = ["jmap-service"]
        },
        {
          Key    = "Environment"
          Values = [var.environment]
        }
      ]
    })
  }

  tags = {
    Name    = "jmap-service-core-${var.environment}"
    Purpose = "Resource group for all jmap-service resources in ${var.environment}"
  }
}
