package plugincontract

// EventPayload represents a system event notification sent to plugin SQS queues
type EventPayload struct {
	EventType  string         `json:"eventType"`            // Event type identifier (e.g., "account.created")
	OccurredAt string         `json:"occurredAt"`           // ISO 8601 timestamp when the event occurred
	AccountID  string         `json:"accountId"`            // Account ID related to this event
	Data       map[string]any `json:"data,omitempty"`       // Event-specific data (optional)
}
