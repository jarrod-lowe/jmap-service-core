package plugin

import "github.com/jarrod-lowe/jmap-service-core/pkg/plugincontract"

// Type aliases for exported plugin contract types
type PluginInvocationRequest = plugincontract.PluginInvocationRequest
type PluginInvocationResponse = plugincontract.PluginInvocationResponse
type MethodResponse = plugincontract.MethodResponse

// PluginRecord represents a plugin registration in DynamoDB (internal only)
type PluginRecord struct {
	PK               string                    `dynamodbav:"pk"`
	SK               string                    `dynamodbav:"sk"`
	PluginID         string                    `dynamodbav:"pluginId"`
	Capabilities     map[string]map[string]any `dynamodbav:"capabilities"`
	Methods          map[string]MethodTarget   `dynamodbav:"methods"`
	ClientPrincipals []string                  `dynamodbav:"clientPrincipals,omitempty"`
	RegisteredAt     string                    `dynamodbav:"registeredAt"`
	Version          string                    `dynamodbav:"version"`
}

// MethodTarget defines how to invoke a method handler (internal only)
type MethodTarget struct {
	InvocationType string `dynamodbav:"invocationType"`
	InvokeTarget   string `dynamodbav:"invokeTarget"`
}
