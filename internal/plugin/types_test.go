package plugin

import (
	"encoding/json"
	"testing"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
)

func TestPluginRecord_DynamoDBMarshal_PreservesKeys(t *testing.T) {
	record := PluginRecord{
		PK:       "PLUGIN#",
		SK:       "PLUGIN#mail-core",
		PluginID: "mail-core",
		Capabilities: map[string]map[string]any{
			"urn:ietf:params:jmap:mail": {
				"maxMailboxDepth": 10,
			},
		},
		Methods: map[string]MethodTarget{
			"Email/get": {
				InvocationType: "lambda-invoke",
				InvokeTarget:   "arn:aws:lambda:ap-southeast-2:123:function:test",
			},
		},
		RegisteredAt: "2025-01-17T10:00:00Z",
		Version:      "1.0.0",
	}

	av, err := attributevalue.MarshalMap(record)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	// Verify pk attribute exists
	if _, ok := av["pk"]; !ok {
		t.Error("expected 'pk' attribute in marshaled output")
	}

	// Verify sk attribute exists
	if _, ok := av["sk"]; !ok {
		t.Error("expected 'sk' attribute in marshaled output")
	}

	// Verify pluginId attribute exists
	if _, ok := av["pluginId"]; !ok {
		t.Error("expected 'pluginId' attribute in marshaled output")
	}
}

func TestPluginRecord_DynamoDBUnmarshal_RestoresData(t *testing.T) {
	// Simulate DynamoDB item
	item := map[string]any{
		"pk":       "PLUGIN#",
		"sk":       "PLUGIN#mail-core",
		"pluginId": "mail-core",
		"capabilities": map[string]any{
			"urn:ietf:params:jmap:mail": map[string]any{
				"maxMailboxDepth": 10,
			},
		},
		"methods": map[string]any{
			"Email/get": map[string]any{
				"invocationType": "lambda-invoke",
				"invokeTarget":   "arn:aws:lambda:ap-southeast-2:123:function:test",
			},
		},
		"registeredAt": "2025-01-17T10:00:00Z",
		"version":      "1.0.0",
	}

	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		t.Fatalf("failed to marshal test data: %v", err)
	}

	var record PluginRecord
	err = attributevalue.UnmarshalMap(av, &record)
	if err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if record.PK != "PLUGIN#" {
		t.Errorf("expected PK='PLUGIN#', got '%s'", record.PK)
	}

	if record.PluginID != "mail-core" {
		t.Errorf("expected PluginID='mail-core', got '%s'", record.PluginID)
	}

	if record.Version != "1.0.0" {
		t.Errorf("expected Version='1.0.0', got '%s'", record.Version)
	}
}

func TestMethodTarget_DynamoDBRoundTrip(t *testing.T) {
	original := MethodTarget{
		InvocationType: "lambda-invoke",
		InvokeTarget:   "arn:aws:lambda:ap-southeast-2:123:function:test",
	}

	av, err := attributevalue.MarshalMap(original)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var restored MethodTarget
	err = attributevalue.UnmarshalMap(av, &restored)
	if err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if restored.InvocationType != original.InvocationType {
		t.Errorf("InvocationType mismatch: expected '%s', got '%s'",
			original.InvocationType, restored.InvocationType)
	}

	if restored.InvokeTarget != original.InvokeTarget {
		t.Errorf("InvokeTarget mismatch: expected '%s', got '%s'",
			original.InvokeTarget, restored.InvokeTarget)
	}
}

func TestPluginInvocationRequest_JSONMarshal(t *testing.T) {
	req := PluginInvocationRequest{
		RequestID: "req-123",
		CallIndex: 0,
		AccountID: "user-456",
		Method:    "Email/get",
		Args: map[string]any{
			"accountId": "user-456",
			"ids":       []string{"email-1", "email-2"},
		},
		ClientID: "c0",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	// Verify JSON uses correct field names
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if _, ok := parsed["requestId"]; !ok {
		t.Error("expected 'requestId' in JSON output")
	}

	if _, ok := parsed["callIndex"]; !ok {
		t.Error("expected 'callIndex' in JSON output")
	}

	if _, ok := parsed["accountId"]; !ok {
		t.Error("expected 'accountId' in JSON output")
	}

	if _, ok := parsed["method"]; !ok {
		t.Error("expected 'method' in JSON output")
	}

	if _, ok := parsed["clientId"]; !ok {
		t.Error("expected 'clientId' in JSON output")
	}
}

func TestPluginInvocationResponse_JSONUnmarshal(t *testing.T) {
	jsonData := `{
		"methodResponse": {
			"name": "Email/get",
			"args": {
				"accountId": "user-456",
				"list": [],
				"notFound": []
			},
			"clientId": "c0"
		}
	}`

	var resp PluginInvocationResponse
	err := json.Unmarshal([]byte(jsonData), &resp)
	if err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if resp.MethodResponse.Name != "Email/get" {
		t.Errorf("expected Name='Email/get', got '%s'", resp.MethodResponse.Name)
	}

	if resp.MethodResponse.ClientID != "c0" {
		t.Errorf("expected ClientID='c0', got '%s'", resp.MethodResponse.ClientID)
	}

	if resp.MethodResponse.Args == nil {
		t.Error("expected Args to be populated")
	}
}

func TestPluginInvocationResponse_ErrorResponse(t *testing.T) {
	jsonData := `{
		"methodResponse": {
			"name": "error",
			"args": {
				"type": "invalidArguments",
				"description": "ids is required"
			},
			"clientId": "c0"
		}
	}`

	var resp PluginInvocationResponse
	err := json.Unmarshal([]byte(jsonData), &resp)
	if err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if resp.MethodResponse.Name != "error" {
		t.Errorf("expected Name='error', got '%s'", resp.MethodResponse.Name)
	}

	errorType, ok := resp.MethodResponse.Args["type"].(string)
	if !ok || errorType != "invalidArguments" {
		t.Errorf("expected error type='invalidArguments', got '%v'", resp.MethodResponse.Args["type"])
	}
}

func TestPluginRecord_DynamoDBUnmarshal_WithEvents(t *testing.T) {
	// Simulate DynamoDB item with events field
	item := map[string]any{
		"pk":       "PLUGIN#",
		"sk":       "PLUGIN#mail-core",
		"pluginId": "mail-core",
		"capabilities": map[string]any{
			"urn:ietf:params:jmap:mail": map[string]any{
				"maxMailboxDepth": 10,
			},
		},
		"methods": map[string]any{
			"Email/get": map[string]any{
				"invocationType": "lambda-invoke",
				"invokeTarget":   "arn:aws:lambda:ap-southeast-2:123:function:test",
			},
		},
		"events": map[string]any{
			"account.created": map[string]any{
				"targetType": "sqs",
				"targetArn":  "arn:aws:sqs:ap-southeast-2:123456789012:jmap-service-email-events",
			},
		},
		"registeredAt": "2025-01-17T10:00:00Z",
		"version":      "1.0.0",
	}

	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		t.Fatalf("failed to marshal test data: %v", err)
	}

	var record PluginRecord
	err = attributevalue.UnmarshalMap(av, &record)
	if err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	// Verify events were unmarshaled
	if record.Events == nil {
		t.Fatal("expected Events to be populated")
	}

	eventTarget, ok := record.Events["account.created"]
	if !ok {
		t.Fatal("expected 'account.created' event to be present")
	}

	if eventTarget.TargetType != "sqs" {
		t.Errorf("expected TargetType='sqs', got '%s'", eventTarget.TargetType)
	}

	if eventTarget.TargetArn != "arn:aws:sqs:ap-southeast-2:123456789012:jmap-service-email-events" {
		t.Errorf("expected correct TargetArn, got '%s'", eventTarget.TargetArn)
	}
}

func TestEventTarget_DynamoDBRoundTrip(t *testing.T) {
	original := EventTarget{
		TargetType: "sqs",
		TargetArn:  "arn:aws:sqs:ap-southeast-2:123456789012:jmap-service-email-events",
	}

	av, err := attributevalue.MarshalMap(original)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var restored EventTarget
	err = attributevalue.UnmarshalMap(av, &restored)
	if err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if restored.TargetType != original.TargetType {
		t.Errorf("TargetType mismatch: expected '%s', got '%s'",
			original.TargetType, restored.TargetType)
	}

	if restored.TargetArn != original.TargetArn {
		t.Errorf("TargetArn mismatch: expected '%s', got '%s'",
			original.TargetArn, restored.TargetArn)
	}
}
