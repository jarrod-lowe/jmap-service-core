package plugin

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// mockQuerier implements PluginQuerier for testing
type mockQuerier struct {
	items []map[string]types.AttributeValue
	err   error
}

func (m *mockQuerier) QueryByPK(ctx context.Context, pk string) ([]map[string]types.AttributeValue, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.items, nil
}

func createTestPluginItem(pluginID string, capabilities map[string]map[string]any, methods map[string]MethodTarget) map[string]types.AttributeValue {
	record := PluginRecord{
		PK:           PluginPrefix,
		SK:           PluginPrefix + pluginID,
		PluginID:     pluginID,
		Capabilities: capabilities,
		Methods:      methods,
		RegisteredAt: "2025-01-17T10:00:00Z",
		Version:      "1.0.0",
	}
	item, _ := attributevalue.MarshalMap(record)
	return item
}

func TestRegistry_LoadFromDynamoDB_LoadsPlugins(t *testing.T) {
	mock := &mockQuerier{
		items: []map[string]types.AttributeValue{
			createTestPluginItem("mail-core", map[string]map[string]any{
				"urn:ietf:params:jmap:mail": {"maxMailboxDepth": 10},
			}, map[string]MethodTarget{
				"Email/get": {
					InvocationType: "lambda-invoke",
					InvokeTarget:   "arn:aws:lambda:ap-southeast-2:123:function:mail-get",
				},
			}),
		},
	}

	registry := NewRegistry()
	err := registry.LoadFromDynamoDB(context.Background(), mock)
	if err != nil {
		t.Fatalf("LoadFromDynamoDB returned error: %v", err)
	}

	// Verify plugin was loaded
	if len(registry.plugins) != 1 {
		t.Errorf("Expected 1 plugin, got %d", len(registry.plugins))
	}
}

func TestRegistry_GetMethodTarget_ReturnsTarget(t *testing.T) {
	mock := &mockQuerier{
		items: []map[string]types.AttributeValue{
			createTestPluginItem("mail-core", map[string]map[string]any{}, map[string]MethodTarget{
				"Email/get": {
					InvocationType: "lambda-invoke",
					InvokeTarget:   "arn:aws:lambda:ap-southeast-2:123:function:mail-get",
				},
			}),
		},
	}

	registry := NewRegistry()
	_ = registry.LoadFromDynamoDB(context.Background(), mock)

	target := registry.GetMethodTarget("Email/get")
	if target == nil {
		t.Fatal("Expected non-nil target for Email/get")
	}

	if target.InvocationType != "lambda-invoke" {
		t.Errorf("Expected InvocationType='lambda-invoke', got '%s'", target.InvocationType)
	}

	if target.InvokeTarget != "arn:aws:lambda:ap-southeast-2:123:function:mail-get" {
		t.Errorf("Expected correct InvokeTarget, got '%s'", target.InvokeTarget)
	}
}

func TestRegistry_GetMethodTarget_ReturnsNilForUnknown(t *testing.T) {
	registry := NewRegistry()

	target := registry.GetMethodTarget("Unknown/method")
	if target != nil {
		t.Error("Expected nil target for unknown method")
	}
}

func TestRegistry_HasCapability_ReturnsTrueForLoaded(t *testing.T) {
	mock := &mockQuerier{
		items: []map[string]types.AttributeValue{
			createTestPluginItem("mail-core", map[string]map[string]any{
				"urn:ietf:params:jmap:mail": {"maxMailboxDepth": 10},
			}, map[string]MethodTarget{}),
		},
	}

	registry := NewRegistry()
	_ = registry.LoadFromDynamoDB(context.Background(), mock)

	if !registry.HasCapability("urn:ietf:params:jmap:mail") {
		t.Error("Expected HasCapability to return true for loaded capability")
	}
}

func TestRegistry_HasCapability_ReturnsFalseForUnknown(t *testing.T) {
	registry := NewRegistry()

	if registry.HasCapability("urn:unknown:capability") {
		t.Error("Expected HasCapability to return false for unknown capability")
	}
}

func TestRegistry_GetCapabilities_ReturnsAllCapabilities(t *testing.T) {
	mock := &mockQuerier{
		items: []map[string]types.AttributeValue{
			createTestPluginItem("mail-core", map[string]map[string]any{
				"urn:ietf:params:jmap:mail": {},
			}, map[string]MethodTarget{}),
			createTestPluginItem("contacts", map[string]map[string]any{
				"urn:ietf:params:jmap:contacts": {},
			}, map[string]MethodTarget{}),
		},
	}

	registry := NewRegistry()
	_ = registry.LoadFromDynamoDB(context.Background(), mock)

	caps := registry.GetCapabilities()
	if len(caps) != 2 {
		t.Errorf("Expected 2 capabilities, got %d", len(caps))
	}

	// Check both are present (order doesn't matter)
	hasMailCap := false
	hasContactsCap := false
	for _, c := range caps {
		if c == "urn:ietf:params:jmap:mail" {
			hasMailCap = true
		}
		if c == "urn:ietf:params:jmap:contacts" {
			hasContactsCap = true
		}
	}
	if !hasMailCap {
		t.Error("Expected urn:ietf:params:jmap:mail capability")
	}
	if !hasContactsCap {
		t.Error("Expected urn:ietf:params:jmap:contacts capability")
	}
}

func TestRegistry_GetCapabilityConfig_ReturnsMergedConfig(t *testing.T) {
	mock := &mockQuerier{
		items: []map[string]types.AttributeValue{
			createTestPluginItem("mail-core", map[string]map[string]any{
				"urn:ietf:params:jmap:mail": {
					"maxMailboxDepth": 10,
					"maxMailboxesPerEmail": nil,
				},
			}, map[string]MethodTarget{}),
		},
	}

	registry := NewRegistry()
	_ = registry.LoadFromDynamoDB(context.Background(), mock)

	config := registry.GetCapabilityConfig("urn:ietf:params:jmap:mail")
	if config == nil {
		t.Fatal("Expected non-nil capability config")
	}

	depth, ok := config["maxMailboxDepth"]
	if !ok {
		t.Error("Expected maxMailboxDepth in config")
	}
	// Note: JSON numbers unmarshal as float64
	if depthFloat, ok := depth.(float64); !ok || depthFloat != 10 {
		t.Errorf("Expected maxMailboxDepth=10, got %v", depth)
	}
}

func TestRegistry_MultiplePlugins_MergesMethods(t *testing.T) {
	mock := &mockQuerier{
		items: []map[string]types.AttributeValue{
			createTestPluginItem("mail-read", map[string]map[string]any{}, map[string]MethodTarget{
				"Email/get":   {InvocationType: "lambda-invoke", InvokeTarget: "arn:get"},
				"Email/query": {InvocationType: "lambda-invoke", InvokeTarget: "arn:query"},
			}),
			createTestPluginItem("mail-write", map[string]map[string]any{}, map[string]MethodTarget{
				"Email/set":    {InvocationType: "lambda-invoke", InvokeTarget: "arn:set"},
				"Email/import": {InvocationType: "lambda-invoke", InvokeTarget: "arn:import"},
			}),
		},
	}

	registry := NewRegistry()
	_ = registry.LoadFromDynamoDB(context.Background(), mock)

	// All methods should be available
	methods := []string{"Email/get", "Email/query", "Email/set", "Email/import"}
	for _, m := range methods {
		if registry.GetMethodTarget(m) == nil {
			t.Errorf("Expected method %s to be available", m)
		}
	}
}

func TestRegistry_LoadFromDynamoDB_MergesCapabilityConfig(t *testing.T) {
	// Two plugins contribute to the same capability - configs should be merged
	mock := &mockQuerier{
		items: []map[string]types.AttributeValue{
			createTestPluginItem("core-base", map[string]map[string]any{
				"urn:ietf:params:jmap:core": {
					"maxSizeUpload":       float64(50000000),
					"maxConcurrentUpload": float64(4),
				},
			}, map[string]MethodTarget{}),
			createTestPluginItem("core-extended", map[string]map[string]any{
				"urn:ietf:params:jmap:core": {
					"maxSizeRequest":    float64(10000000),
					"maxConcurrentUpload": float64(8), // Override the previous value
				},
			}, map[string]MethodTarget{}),
		},
	}

	registry := NewRegistry()
	err := registry.LoadFromDynamoDB(context.Background(), mock)
	if err != nil {
		t.Fatalf("LoadFromDynamoDB returned error: %v", err)
	}

	config := registry.GetCapabilityConfig("urn:ietf:params:jmap:core")
	if config == nil {
		t.Fatal("Expected non-nil capability config for core")
	}

	// Value from first plugin should be present
	if maxUpload, ok := config["maxSizeUpload"].(float64); !ok || maxUpload != 50000000 {
		t.Errorf("Expected maxSizeUpload=50000000 from first plugin, got %v", config["maxSizeUpload"])
	}

	// Value from second plugin should be present
	if maxRequest, ok := config["maxSizeRequest"].(float64); !ok || maxRequest != 10000000 {
		t.Errorf("Expected maxSizeRequest=10000000 from second plugin, got %v", config["maxSizeRequest"])
	}

	// Overwritten value should have the later value
	if maxConcurrentUpload, ok := config["maxConcurrentUpload"].(float64); !ok || maxConcurrentUpload != 8 {
		t.Errorf("Expected maxConcurrentUpload=8 (overwritten), got %v", config["maxConcurrentUpload"])
	}
}

func TestRegistry_NewRegistry_HasNoCapabilities(t *testing.T) {
	registry := NewRegistry()

	caps := registry.GetCapabilities()
	if len(caps) != 0 {
		t.Errorf("Expected empty registry to have no capabilities, got %d", len(caps))
	}

	if registry.HasCapability("urn:ietf:params:jmap:core") {
		t.Error("Expected empty registry to not have core capability")
	}
}
