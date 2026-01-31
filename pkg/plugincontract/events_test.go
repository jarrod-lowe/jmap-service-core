package plugincontract

import (
	"encoding/json"
	"testing"
)

func TestEventPayload_JSONMarshalRoundTrip(t *testing.T) {
	original := EventPayload{
		EventType:  "account.created",
		OccurredAt: "2025-01-20T10:30:00Z",
		AccountID:  "abc123-def456",
		Data: map[string]any{
			"quotaBytes": float64(10000000),
		},
	}

	// Marshal to JSON
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	// Unmarshal back
	var restored EventPayload
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	// Verify all fields
	if restored.EventType != original.EventType {
		t.Errorf("EventType mismatch: expected '%s', got '%s'",
			original.EventType, restored.EventType)
	}

	if restored.OccurredAt != original.OccurredAt {
		t.Errorf("OccurredAt mismatch: expected '%s', got '%s'",
			original.OccurredAt, restored.OccurredAt)
	}

	if restored.AccountID != original.AccountID {
		t.Errorf("AccountID mismatch: expected '%s', got '%s'",
			original.AccountID, restored.AccountID)
	}

	if restored.Data == nil {
		t.Fatal("expected Data to be populated")
	}

	quotaBytes, ok := restored.Data["quotaBytes"].(float64)
	if !ok || quotaBytes != 10000000 {
		t.Errorf("Data.quotaBytes mismatch: expected 10000000, got %v", restored.Data["quotaBytes"])
	}
}

func TestEventPayload_JSONMarshal_UsesCorrectFieldNames(t *testing.T) {
	payload := EventPayload{
		EventType:  "account.created",
		OccurredAt: "2025-01-20T10:30:00Z",
		AccountID:  "abc123",
		Data:       map[string]any{"key": "value"},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal to map: %v", err)
	}

	expectedFields := []string{"eventType", "occurredAt", "accountId", "data"}
	for _, field := range expectedFields {
		if _, ok := parsed[field]; !ok {
			t.Errorf("expected '%s' field in JSON output", field)
		}
	}
}

func TestEventPayload_JSONMarshal_OmitsEmptyData(t *testing.T) {
	payload := EventPayload{
		EventType:  "account.created",
		OccurredAt: "2025-01-20T10:30:00Z",
		AccountID:  "abc123",
		// Data is nil
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal to map: %v", err)
	}

	if _, ok := parsed["data"]; ok {
		t.Error("expected 'data' field to be omitted when nil")
	}
}
