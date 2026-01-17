package plugin

// PluginRecord represents a plugin registration in DynamoDB
type PluginRecord struct {
	PK           string                       `dynamodbav:"pk"`
	SK           string                       `dynamodbav:"sk"`
	PluginID     string                       `dynamodbav:"pluginId"`
	Capabilities map[string]map[string]any    `dynamodbav:"capabilities"`
	Methods      map[string]MethodTarget      `dynamodbav:"methods"`
	RegisteredAt string                       `dynamodbav:"registeredAt"`
	Version      string                       `dynamodbav:"version"`
}

// MethodTarget defines how to invoke a method handler
type MethodTarget struct {
	InvocationType string `dynamodbav:"invocationType"`
	InvokeTarget   string `dynamodbav:"invokeTarget"`
}

// PluginInvocationRequest is the payload sent from core to plugin
type PluginInvocationRequest struct {
	RequestID string         `json:"requestId"`
	CallIndex int            `json:"callIndex"`
	AccountID string         `json:"accountId"`
	Method    string         `json:"method"`
	Args      map[string]any `json:"args"`
	ClientID  string         `json:"clientId"`
}

// PluginInvocationResponse is the response from plugin to core
type PluginInvocationResponse struct {
	MethodResponse MethodResponse `json:"methodResponse"`
}

// MethodResponse represents a single JMAP method response
type MethodResponse struct {
	Name     string         `json:"name"`
	Args     map[string]any `json:"args"`
	ClientID string         `json:"clientId"`
}
