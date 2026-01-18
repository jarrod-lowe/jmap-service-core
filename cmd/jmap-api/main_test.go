package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/jarrod-lowe/jmap-service-core/internal/plugin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace/noop"
)

// mockInvoker implements plugin.Invoker for testing
type mockInvoker struct {
	invokeFunc func(ctx context.Context, target plugin.MethodTarget, request plugin.PluginInvocationRequest) (*plugin.PluginInvocationResponse, error)
}

func (m *mockInvoker) Invoke(ctx context.Context, target plugin.MethodTarget, request plugin.PluginInvocationRequest) (*plugin.PluginInvocationResponse, error) {
	if m.invokeFunc != nil {
		return m.invokeFunc(ctx, target, request)
	}
	return &plugin.PluginInvocationResponse{
		MethodResponse: plugin.MethodResponse{
			Name:     request.Method,
			Args:     map[string]any{"accountId": request.AccountID},
			ClientID: request.ClientID,
		},
	}, nil
}

func setupTestDeps() {
	tp := noop.NewTracerProvider()
	otel.SetTracerProvider(tp)

	registry := plugin.NewRegistry()
	// Manually add a test method to the registry
	// We'll use a helper to populate the registry

	deps = &Dependencies{
		Registry: registry,
		Invoker:  &mockInvoker{},
	}
}

func TestHandler_ValidJMAPRequest_ReturnsResponse(t *testing.T) {
	setupTestDeps()
	ctx := context.Background()

	// Empty using array since registry is empty
	request := events.APIGatewayProxyRequest{
		Body: `{"using":[],"methodCalls":[]}`,
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "test-request-id",
			Authorizer: map[string]any{
				"claims": map[string]any{
					"sub": "user-123",
				},
			},
		},
	}

	response, err := handler(ctx, request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if response.StatusCode != 200 {
		t.Errorf("expected status code 200, got %d. Body: %s", response.StatusCode, response.Body)
	}

	var jmapResp JMAPResponse
	if err := json.Unmarshal([]byte(response.Body), &jmapResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if jmapResp.SessionState == "" {
		t.Error("expected sessionState to be set")
	}
}

func TestHandler_InvalidJSON_Returns400(t *testing.T) {
	setupTestDeps()
	ctx := context.Background()

	request := events.APIGatewayProxyRequest{
		Body: `{invalid json}`,
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "test-request-id",
			Authorizer: map[string]any{
				"claims": map[string]any{
					"sub": "user-123",
				},
			},
		},
	}

	response, err := handler(ctx, request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if response.StatusCode != 400 {
		t.Errorf("expected status code 400, got %d", response.StatusCode)
	}

	if response.Headers["Content-Type"] != "application/json" {
		t.Errorf("expected Content-Type application/json")
	}
}

func TestHandler_MissingAuth_Returns401(t *testing.T) {
	setupTestDeps()
	ctx := context.Background()

	request := events.APIGatewayProxyRequest{
		Body: `{"using":[],"methodCalls":[]}`,
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "test-request-id",
			// No authorizer
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

func TestExtractAccountID_FromJWTSub(t *testing.T) {
	request := events.APIGatewayProxyRequest{
		RequestContext: events.APIGatewayProxyRequestContext{
			Authorizer: map[string]any{
				"claims": map[string]any{
					"sub": "user-jwt-123",
				},
			},
		},
	}

	accountID, err := extractAccountID(request)
	if err != nil {
		t.Fatalf("extractAccountID returned error: %v", err)
	}

	if accountID != "user-jwt-123" {
		t.Errorf("expected 'user-jwt-123', got '%s'", accountID)
	}
}

func TestExtractAccountID_FromPathParam(t *testing.T) {
	request := events.APIGatewayProxyRequest{
		PathParameters: map[string]string{
			"accountId": "user-iam-456",
		},
		RequestContext: events.APIGatewayProxyRequestContext{
			Authorizer: map[string]any{
				"claims": map[string]any{
					"sub": "should-not-use-this",
				},
			},
		},
	}

	accountID, err := extractAccountID(request)
	if err != nil {
		t.Fatalf("extractAccountID returned error: %v", err)
	}

	// Path param takes precedence
	if accountID != "user-iam-456" {
		t.Errorf("expected 'user-iam-456', got '%s'", accountID)
	}
}

func TestHandler_UnknownMethod_ReturnsUnknownMethodError(t *testing.T) {
	setupTestDeps()
	ctx := context.Background()

	// Registry is empty, so any method should be unknown
	request := events.APIGatewayProxyRequest{
		Body: `{"using":[],"methodCalls":[["Unknown/method",{"accountId":"user-123"},"c0"]]}`,
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "test-request-id",
			Authorizer: map[string]any{
				"claims": map[string]any{
					"sub": "user-123",
				},
			},
		},
	}

	response, err := handler(ctx, request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if response.StatusCode != 200 {
		t.Errorf("expected status code 200 (JMAP errors are in body), got %d", response.StatusCode)
	}

	var jmapResp JMAPResponse
	if err := json.Unmarshal([]byte(response.Body), &jmapResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if len(jmapResp.MethodResponses) != 1 {
		t.Fatalf("expected 1 method response, got %d", len(jmapResp.MethodResponses))
	}

	respName, ok := jmapResp.MethodResponses[0][0].(string)
	if !ok || respName != "error" {
		t.Errorf("expected error response, got '%v'", jmapResp.MethodResponses[0][0])
	}

	respArgs, ok := jmapResp.MethodResponses[0][1].(map[string]any)
	if !ok {
		t.Fatalf("expected args to be a map")
	}

	errorType, ok := respArgs["type"].(string)
	if !ok || errorType != "unknownMethod" {
		t.Errorf("expected error type 'unknownMethod', got '%v'", respArgs["type"])
	}
}

func TestHandler_ResponseContentType(t *testing.T) {
	setupTestDeps()
	ctx := context.Background()

	request := events.APIGatewayProxyRequest{
		Body: `{"using":[],"methodCalls":[]}`,
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "test-request-id",
			Authorizer: map[string]any{
				"claims": map[string]any{
					"sub": "user-123",
				},
			},
		},
	}

	response, _ := handler(ctx, request)

	if response.Headers["Content-Type"] != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got '%s'", response.Headers["Content-Type"])
	}
}

func TestProcessMethodCall_InvalidCallStructure_ReturnsError(t *testing.T) {
	setupTestDeps()
	ctx := context.Background()

	// Call with wrong number of elements
	call := []any{"method", "not-an-object"}
	result := processMethodCall(ctx, "user-123", call, 0, "req-123")

	if result[0] != "error" {
		t.Errorf("expected error response, got '%v'", result[0])
	}
}

func TestProcessMethodCall_NonStringMethodName_ReturnsError(t *testing.T) {
	setupTestDeps()
	ctx := context.Background()

	call := []any{123, map[string]any{}, "c0"}
	result := processMethodCall(ctx, "user-123", call, 0, "req-123")

	if result[0] != "error" {
		t.Errorf("expected error response, got '%v'", result[0])
	}

	args := result[1].(map[string]any)
	if args["type"] != "invalidArguments" {
		t.Errorf("expected invalidArguments error, got '%v'", args["type"])
	}
}
