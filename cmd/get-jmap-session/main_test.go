package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/jarrod-lowe/jmap-service-core/internal/db"
	"github.com/jarrod-lowe/jmap-service-core/internal/plugin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace/noop"
)

// mockAccountStore implements AccountStore for testing
type mockAccountStore struct {
	ensureAccountFunc func(ctx context.Context, userID string) (*db.Account, error)
	calledWith        string
}

func (m *mockAccountStore) EnsureAccount(ctx context.Context, userID string) (*db.Account, error) {
	m.calledWith = userID
	if m.ensureAccountFunc != nil {
		return m.ensureAccountFunc(ctx, userID)
	}
	return &db.Account{
		UserID:              userID,
		Owner:               "USER#" + userID,
		CreatedAt:           "2024-01-01T00:00:00Z",
		LastDiscoveryAccess: "2024-01-01T00:00:00Z",
	}, nil
}

// mockPluginQuerier implements plugin.PluginQuerier for testing
type mockPluginQuerier struct {
	items []map[string]types.AttributeValue
}

func (m *mockPluginQuerier) QueryByPK(ctx context.Context, pk string) ([]map[string]types.AttributeValue, error) {
	return m.items, nil
}

// createCorePluginItem creates a plugin item with core capability for testing
func createCorePluginItem() map[string]types.AttributeValue {
	record := plugin.PluginRecord{
		PK:       plugin.PluginPrefix,
		SK:       plugin.PluginPrefix + "core",
		PluginID: "core",
		Capabilities: map[string]map[string]any{
			"urn:ietf:params:jmap:core": {
				"maxSizeUpload":         float64(50000000),
				"maxConcurrentUpload":   float64(4),
				"maxSizeRequest":        float64(10000000),
				"maxConcurrentRequests": float64(4),
				"maxCallsInRequest":     float64(16),
				"maxObjectsInGet":       float64(500),
				"maxObjectsInSet":       float64(500),
				"collationAlgorithms":   []string{"i;ascii-casemap"},
			},
		},
		Methods:      map[string]plugin.MethodTarget{},
		RegisteredAt: "2025-01-17T00:00:00Z",
		Version:      "1.0.0",
	}
	item, _ := attributevalue.MarshalMap(record)
	return item
}

func setupTest() {
	tp := noop.NewTracerProvider()
	otel.SetTracerProvider(tp)
	// Use a mock account store for tests
	accountStore = &mockAccountStore{}
	// Create a registry with core capability loaded
	pluginRegistry = plugin.NewRegistry()
	mock := &mockPluginQuerier{
		items: []map[string]types.AttributeValue{createCorePluginItem()},
	}
	_ = pluginRegistry.LoadFromDynamoDB(context.Background(), mock)
}

func TestHandler_ValidRequest_ReturnsJMAPSession(t *testing.T) {
	setupTest()
	ctx := context.Background()

	request := events.APIGatewayProxyRequest{
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "test-request-id",
			Authorizer: map[string]any{
				"claims": map[string]any{
					"sub": "user-123-abc",
				},
			},
		},
	}

	response, err := handler(ctx, request)

	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if response.StatusCode != 200 {
		t.Errorf("expected status code 200, got %d", response.StatusCode)
	}

	var session JMAPSession
	if err := json.Unmarshal([]byte(response.Body), &session); err != nil {
		t.Fatalf("failed to unmarshal response body: %v", err)
	}

	// Verify account exists with correct ID
	account, exists := session.Accounts["user-123-abc"]
	if !exists {
		t.Fatal("expected account with ID 'user-123-abc' to exist")
	}

	if account.Name != "mailbox" {
		t.Errorf("expected account name 'mailbox', got '%s'", account.Name)
	}

	if !account.IsPersonal {
		t.Error("expected account to be personal")
	}

	// Verify username matches sub
	if session.Username != "user-123-abc" {
		t.Errorf("expected username 'user-123-abc', got '%s'", session.Username)
	}

	// Verify primary account set
	if session.PrimaryAccounts["urn:ietf:params:jmap:core"] != "user-123-abc" {
		t.Errorf("expected primary account for core to be 'user-123-abc'")
	}
}

func TestHandler_MissingSub_Returns401(t *testing.T) {
	setupTest()
	ctx := context.Background()

	request := events.APIGatewayProxyRequest{
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "test-request-id",
			Authorizer: map[string]any{
				"claims": map[string]any{
					// No sub claim
				},
			},
		},
	}

	response, err := handler(ctx, request)

	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if response.StatusCode != 401 {
		t.Errorf("expected status code 401, got %d", response.StatusCode)
	}
}

func TestHandler_ResponseContentType(t *testing.T) {
	setupTest()
	ctx := context.Background()

	request := events.APIGatewayProxyRequest{
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "test-request-id",
			Authorizer: map[string]any{
				"claims": map[string]any{
					"sub": "user-456",
				},
			},
		},
	}

	response, _ := handler(ctx, request)

	// RFC 8620: Session responses MUST be application/json
	if response.Headers["Content-Type"] != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got '%s'", response.Headers["Content-Type"])
	}
}

func TestHandler_RFC8620RequiredFields(t *testing.T) {
	setupTest()
	ctx := context.Background()

	request := events.APIGatewayProxyRequest{
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "test-request-id",
			Authorizer: map[string]any{
				"claims": map[string]any{
					"sub": "user-789",
				},
			},
		},
	}

	response, err := handler(ctx, request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var session JMAPSession
	if err := json.Unmarshal([]byte(response.Body), &session); err != nil {
		t.Fatalf("failed to unmarshal response body: %v", err)
	}

	// RFC 8620 Section 2: Required fields
	if session.Capabilities == nil {
		t.Error("capabilities is required")
	}

	if _, exists := session.Capabilities["urn:ietf:params:jmap:core"]; !exists {
		t.Error("urn:ietf:params:jmap:core capability is required")
	}

	if len(session.Accounts) == 0 {
		t.Error("accounts is required and must have at least one account")
	}

	if session.PrimaryAccounts == nil {
		t.Error("primaryAccounts is required")
	}

	if session.Username == "" {
		t.Error("username is required")
	}

	if session.APIUrl == "" {
		t.Error("apiUrl is required")
	}

	if session.DownloadUrl == "" {
		t.Error("downloadUrl is required")
	}

	if session.UploadUrl == "" {
		t.Error("uploadUrl is required")
	}

	if session.EventSourceUrl == "" {
		t.Error("eventSourceUrl is required")
	}

	if session.State == "" {
		t.Error("state is required")
	}
}

func TestHandler_CoreCapabilityValues(t *testing.T) {
	setupTest()
	ctx := context.Background()

	request := events.APIGatewayProxyRequest{
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "test-request-id",
			Authorizer: map[string]any{
				"claims": map[string]any{
					"sub": "user-cap-test",
				},
			},
		},
	}

	response, _ := handler(ctx, request)

	var session JMAPSession
	if err := json.Unmarshal([]byte(response.Body), &session); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	coreAny, ok := session.Capabilities["urn:ietf:params:jmap:core"]
	if !ok {
		t.Fatal("core capability not found")
	}

	// After JSON unmarshal, capabilities are maps
	core, ok := coreAny.(map[string]any)
	if !ok {
		t.Fatal("core capability is not a map")
	}

	// RFC 8620 Section 2: Core capability required fields
	if maxSizeUpload, _ := core["maxSizeUpload"].(float64); maxSizeUpload <= 0 {
		t.Error("maxSizeUpload must be positive")
	}

	if maxConcurrentUpload, _ := core["maxConcurrentUpload"].(float64); maxConcurrentUpload <= 0 {
		t.Error("maxConcurrentUpload must be positive")
	}

	if maxSizeRequest, _ := core["maxSizeRequest"].(float64); maxSizeRequest <= 0 {
		t.Error("maxSizeRequest must be positive")
	}

	if maxConcurrentRequests, _ := core["maxConcurrentRequests"].(float64); maxConcurrentRequests <= 0 {
		t.Error("maxConcurrentRequests must be positive")
	}

	if maxCallsInRequest, _ := core["maxCallsInRequest"].(float64); maxCallsInRequest <= 0 {
		t.Error("maxCallsInRequest must be positive")
	}

	if maxObjectsInGet, _ := core["maxObjectsInGet"].(float64); maxObjectsInGet <= 0 {
		t.Error("maxObjectsInGet must be positive")
	}

	if maxObjectsInSet, _ := core["maxObjectsInSet"].(float64); maxObjectsInSet <= 0 {
		t.Error("maxObjectsInSet must be positive")
	}

	collation, ok := core["collationAlgorithms"].([]any)
	if !ok || len(collation) == 0 {
		t.Error("collationAlgorithms must have at least one entry")
	}
}

// TestBuildSession_WithInjectedConfig tests buildSession with dependency injection
// This test uses direct Config injection - no environment variables needed
func TestBuildSession_WithInjectedConfig(t *testing.T) {
	cfg := Config{APIDomain: "test.example.com"}
	session := buildSession("user-123", cfg, nil)

	// Verify URLs use the injected domain
	expectedAPIUrl := "https://test.example.com/v1/jmap"
	if session.APIUrl != expectedAPIUrl {
		t.Errorf("expected apiUrl '%s', got '%s'", expectedAPIUrl, session.APIUrl)
	}

	expectedDownloadUrl := "https://test.example.com/v1/download/{accountId}/{blobId}"
	if session.DownloadUrl != expectedDownloadUrl {
		t.Errorf("expected downloadUrl '%s', got '%s'", expectedDownloadUrl, session.DownloadUrl)
	}

	expectedUploadUrl := "https://test.example.com/v1/upload/{accountId}"
	if session.UploadUrl != expectedUploadUrl {
		t.Errorf("expected uploadUrl '%s', got '%s'", expectedUploadUrl, session.UploadUrl)
	}

	expectedEventSourceUrl := "https://test.example.com/v1/events/{types}/{closeafter}/{ping}"
	if session.EventSourceUrl != expectedEventSourceUrl {
		t.Errorf("expected eventSourceUrl '%s', got '%s'", expectedEventSourceUrl, session.EventSourceUrl)
	}

	// Verify user ID is used correctly
	if session.Username != "user-123" {
		t.Errorf("expected username 'user-123', got '%s'", session.Username)
	}

	if _, exists := session.Accounts["user-123"]; !exists {
		t.Error("expected account with ID 'user-123' to exist")
	}
}

func TestHandler_CallsEnsureAccount(t *testing.T) {
	setupTest()
	mock := &mockAccountStore{}
	accountStore = mock
	ctx := context.Background()

	request := events.APIGatewayProxyRequest{
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "test-request-id",
			Authorizer: map[string]any{
				"claims": map[string]any{
					"sub": "user-ensure-test",
				},
			},
		},
	}

	_, err := handler(ctx, request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if mock.calledWith != "user-ensure-test" {
		t.Errorf("Expected EnsureAccount to be called with 'user-ensure-test', got '%s'", mock.calledWith)
	}
}

func TestHandler_EnsureAccountError_Returns500(t *testing.T) {
	setupTest()
	mock := &mockAccountStore{
		ensureAccountFunc: func(ctx context.Context, userID string) (*db.Account, error) {
			return nil, errors.New("DynamoDB error")
		},
	}
	accountStore = mock
	ctx := context.Background()

	request := events.APIGatewayProxyRequest{
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "test-request-id",
			Authorizer: map[string]any{
				"claims": map[string]any{
					"sub": "user-error-test",
				},
			},
		},
	}

	response, err := handler(ctx, request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if response.StatusCode != 500 {
		t.Errorf("Expected status code 500 when EnsureAccount fails, got %d", response.StatusCode)
	}
}

// Ensure errors import is used
var _ = errors.New
