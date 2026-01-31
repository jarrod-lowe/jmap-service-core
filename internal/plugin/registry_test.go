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

// createTestPluginItemWithPrincipals creates a plugin record with clientPrincipals
func createTestPluginItemWithPrincipals(pluginID string, principals []string) map[string]types.AttributeValue {
	record := PluginRecord{
		PK:               PluginPrefix,
		SK:               PluginPrefix + pluginID,
		PluginID:         pluginID,
		Capabilities:     map[string]map[string]any{},
		Methods:          map[string]MethodTarget{},
		ClientPrincipals: principals,
		RegisteredAt:     "2025-01-17T10:00:00Z",
		Version:          "1.0.0",
	}
	item, _ := attributevalue.MarshalMap(record)
	return item
}

func TestRegistry_IsAllowedPrincipal_RegisteredPrincipalIsAllowed(t *testing.T) {
	mock := &mockQuerier{
		items: []map[string]types.AttributeValue{
			createTestPluginItemWithPrincipals("ingest-plugin", []string{
				"arn:aws:iam::123456789012:role/IngestRole",
			}),
		},
	}

	registry := NewRegistry()
	_ = registry.LoadFromDynamoDB(context.Background(), mock)

	if !registry.IsAllowedPrincipal("arn:aws:iam::123456789012:role/IngestRole") {
		t.Error("expected registered principal to be allowed")
	}
}

func TestRegistry_IsAllowedPrincipal_UnregisteredPrincipalIsDenied(t *testing.T) {
	mock := &mockQuerier{
		items: []map[string]types.AttributeValue{
			createTestPluginItemWithPrincipals("ingest-plugin", []string{
				"arn:aws:iam::123456789012:role/IngestRole",
			}),
		},
	}

	registry := NewRegistry()
	_ = registry.LoadFromDynamoDB(context.Background(), mock)

	if registry.IsAllowedPrincipal("arn:aws:iam::123456789012:role/OtherRole") {
		t.Error("expected unregistered principal to be denied")
	}
}

func TestRegistry_IsAllowedPrincipal_AssumedRoleMatchesRegisteredRole(t *testing.T) {
	mock := &mockQuerier{
		items: []map[string]types.AttributeValue{
			createTestPluginItemWithPrincipals("ingest-plugin", []string{
				"arn:aws:iam::123456789012:role/IngestRole",
			}),
		},
	}

	registry := NewRegistry()
	_ = registry.LoadFromDynamoDB(context.Background(), mock)

	// Assumed role should match the registered role
	assumedRoleARN := "arn:aws:sts::123456789012:assumed-role/IngestRole/session-123"
	if !registry.IsAllowedPrincipal(assumedRoleARN) {
		t.Error("expected assumed role caller to match registered role ARN")
	}
}

func TestRegistry_IsAllowedPrincipal_EmptyRegistryDeniesAll(t *testing.T) {
	registry := NewRegistry()

	if registry.IsAllowedPrincipal("arn:aws:iam::123456789012:role/AnyRole") {
		t.Error("expected empty registry to deny all principals")
	}
}

func TestRegistry_IsAllowedPrincipal_AggregatesFromMultiplePlugins(t *testing.T) {
	mock := &mockQuerier{
		items: []map[string]types.AttributeValue{
			createTestPluginItemWithPrincipals("plugin-a", []string{
				"arn:aws:iam::123456789012:role/RoleA",
			}),
			createTestPluginItemWithPrincipals("plugin-b", []string{
				"arn:aws:iam::123456789012:role/RoleB",
			}),
		},
	}

	registry := NewRegistry()
	_ = registry.LoadFromDynamoDB(context.Background(), mock)

	// Both roles from different plugins should be allowed
	if !registry.IsAllowedPrincipal("arn:aws:iam::123456789012:role/RoleA") {
		t.Error("expected RoleA from plugin-a to be allowed")
	}
	if !registry.IsAllowedPrincipal("arn:aws:iam::123456789012:role/RoleB") {
		t.Error("expected RoleB from plugin-b to be allowed")
	}
}

func TestRegistry_IsAllowedPrincipal_PluginWithNoPrincipals(t *testing.T) {
	mock := &mockQuerier{
		items: []map[string]types.AttributeValue{
			createTestPluginItem("no-principals", map[string]map[string]any{}, map[string]MethodTarget{}),
		},
	}

	registry := NewRegistry()
	_ = registry.LoadFromDynamoDB(context.Background(), mock)

	// Plugin with no principals should not affect the allow list
	if registry.IsAllowedPrincipal("arn:aws:iam::123456789012:role/AnyRole") {
		t.Error("expected registry with no principals to deny all")
	}
}

// createTestPluginItemWithEvents creates a plugin record with events
func createTestPluginItemWithEvents(pluginID string, events map[string]EventTarget) map[string]types.AttributeValue {
	record := PluginRecord{
		PK:           PluginPrefix,
		SK:           PluginPrefix + pluginID,
		PluginID:     pluginID,
		Capabilities: map[string]map[string]any{},
		Methods:      map[string]MethodTarget{},
		Events:       events,
		RegisteredAt: "2025-01-17T10:00:00Z",
		Version:      "1.0.0",
	}
	item, _ := attributevalue.MarshalMap(record)
	return item
}

func TestRegistry_GetEventTargets_ReturnsTargetsForEventType(t *testing.T) {
	mock := &mockQuerier{
		items: []map[string]types.AttributeValue{
			createTestPluginItemWithEvents("mail-core", map[string]EventTarget{
				"account.created": {
					TargetType: "sqs",
					TargetArn:  "arn:aws:sqs:ap-southeast-2:123456789012:jmap-service-email-events",
				},
			}),
		},
	}

	registry := NewRegistry()
	_ = registry.LoadFromDynamoDB(context.Background(), mock)

	targets := registry.GetEventTargets("account.created")
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}

	if targets[0].PluginID != "mail-core" {
		t.Errorf("expected PluginID='mail-core', got '%s'", targets[0].PluginID)
	}

	if targets[0].TargetType != "sqs" {
		t.Errorf("expected TargetType='sqs', got '%s'", targets[0].TargetType)
	}

	if targets[0].TargetArn != "arn:aws:sqs:ap-southeast-2:123456789012:jmap-service-email-events" {
		t.Errorf("expected correct TargetArn, got '%s'", targets[0].TargetArn)
	}
}

func TestRegistry_GetEventTargets_ReturnsEmptyForUnknownEvent(t *testing.T) {
	mock := &mockQuerier{
		items: []map[string]types.AttributeValue{
			createTestPluginItemWithEvents("mail-core", map[string]EventTarget{
				"account.created": {
					TargetType: "sqs",
					TargetArn:  "arn:aws:sqs:ap-southeast-2:123456789012:jmap-service-email-events",
				},
			}),
		},
	}

	registry := NewRegistry()
	_ = registry.LoadFromDynamoDB(context.Background(), mock)

	targets := registry.GetEventTargets("unknown.event")
	if len(targets) != 0 {
		t.Errorf("expected 0 targets for unknown event, got %d", len(targets))
	}
}

func TestRegistry_GetEventTargets_AggregatesFromMultiplePlugins(t *testing.T) {
	mock := &mockQuerier{
		items: []map[string]types.AttributeValue{
			createTestPluginItemWithEvents("mail-core", map[string]EventTarget{
				"account.created": {
					TargetType: "sqs",
					TargetArn:  "arn:aws:sqs:ap-southeast-2:123456789012:jmap-service-email-events",
				},
			}),
			createTestPluginItemWithEvents("contacts-plugin", map[string]EventTarget{
				"account.created": {
					TargetType: "sqs",
					TargetArn:  "arn:aws:sqs:ap-southeast-2:123456789012:jmap-service-contacts-events",
				},
			}),
		},
	}

	registry := NewRegistry()
	_ = registry.LoadFromDynamoDB(context.Background(), mock)

	targets := registry.GetEventTargets("account.created")
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(targets))
	}

	// Verify both plugins are represented
	pluginIDs := make(map[string]bool)
	for _, target := range targets {
		pluginIDs[target.PluginID] = true
	}

	if !pluginIDs["mail-core"] {
		t.Error("expected mail-core plugin in targets")
	}
	if !pluginIDs["contacts-plugin"] {
		t.Error("expected contacts-plugin in targets")
	}
}

func TestRegistry_GetEventTargets_PluginWithNoEvents(t *testing.T) {
	mock := &mockQuerier{
		items: []map[string]types.AttributeValue{
			createTestPluginItem("no-events", map[string]map[string]any{}, map[string]MethodTarget{}),
		},
	}

	registry := NewRegistry()
	_ = registry.LoadFromDynamoDB(context.Background(), mock)

	targets := registry.GetEventTargets("account.created")
	if len(targets) != 0 {
		t.Errorf("expected 0 targets for plugin with no events, got %d", len(targets))
	}
}
