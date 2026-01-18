// Package plugincontract defines the contract types for JMAP plugins.
// These types are used for communication between the JMAP core service and plugins.
package plugincontract

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
