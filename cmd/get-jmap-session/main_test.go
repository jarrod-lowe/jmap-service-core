package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace/noop"
)

func setupTest() {
	tp := noop.NewTracerProvider()
	otel.SetTracerProvider(tp)
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
	json.Unmarshal([]byte(response.Body), &session)

	core := session.Capabilities["urn:ietf:params:jmap:core"]

	// RFC 8620 Section 2: Core capability required fields
	if core.MaxSizeUpload <= 0 {
		t.Error("maxSizeUpload must be positive")
	}

	if core.MaxConcurrentUpload <= 0 {
		t.Error("maxConcurrentUpload must be positive")
	}

	if core.MaxSizeRequest <= 0 {
		t.Error("maxSizeRequest must be positive")
	}

	if core.MaxConcurrentRequests <= 0 {
		t.Error("maxConcurrentRequests must be positive")
	}

	if core.MaxCallsInRequest <= 0 {
		t.Error("maxCallsInRequest must be positive")
	}

	if core.MaxObjectsInGet <= 0 {
		t.Error("maxObjectsInGet must be positive")
	}

	if core.MaxObjectsInSet <= 0 {
		t.Error("maxObjectsInSet must be positive")
	}

	if len(core.CollationAlgorithms) == 0 {
		t.Error("collationAlgorithms must have at least one entry")
	}
}

// TestBuildSession_WithInjectedConfig tests buildSession with dependency injection
// This test uses direct Config injection - no environment variables needed
func TestBuildSession_WithInjectedConfig(t *testing.T) {
	cfg := Config{APIDomain: "test.example.com"}
	session := buildSession("user-123", cfg)

	// Verify URLs use the injected domain
	expectedAPIUrl := "https://test.example.com/v1/jmap"
	if session.APIUrl != expectedAPIUrl {
		t.Errorf("expected apiUrl '%s', got '%s'", expectedAPIUrl, session.APIUrl)
	}

	expectedDownloadUrl := "https://test.example.com/v1/download/{accountId}/{blobId}/{name}"
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
