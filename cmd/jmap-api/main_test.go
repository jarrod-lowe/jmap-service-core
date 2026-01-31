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
	result := processMethodCall(ctx, "user-123", call, 0, "req-123", nil, nil)

	if result[0] != "error" {
		t.Errorf("expected error response, got '%v'", result[0])
	}
}

func TestProcessMethodCall_NonStringMethodName_ReturnsError(t *testing.T) {
	setupTestDeps()
	ctx := context.Background()

	call := []any{123, map[string]any{}, "c0"}
	result := processMethodCall(ctx, "user-123", call, 0, "req-123", nil, nil)

	if result[0] != "error" {
		t.Errorf("expected error response, got '%v'", result[0])
	}

	args := result[1].(map[string]any)
	if args["type"] != "invalidArguments" {
		t.Errorf("expected invalidArguments error, got '%v'", args["type"])
	}
}

// setupTestDepsWithPrincipals creates test deps with a registry that has registered principals
func setupTestDepsWithPrincipals(principals []string) {
	tp := noop.NewTracerProvider()
	otel.SetTracerProvider(tp)

	registry := plugin.NewRegistryWithPrincipals(principals)

	deps = &Dependencies{
		Registry: registry,
		Invoker:  &mockInvoker{},
	}
}

func TestHandler_IAMAuth_RegisteredPrincipal_Succeeds(t *testing.T) {
	setupTestDepsWithPrincipals([]string{"arn:aws:iam::123456789012:role/IngestRole"})
	ctx := context.Background()

	request := events.APIGatewayProxyRequest{
		Path: "/jmap-iam/user-123",
		Body: `{"using":[],"methodCalls":[]}`,
		PathParameters: map[string]string{
			"accountId": "user-123",
		},
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "test-request-id",
			Identity: events.APIGatewayRequestIdentity{
				UserArn: "arn:aws:iam::123456789012:role/IngestRole",
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
}

func TestHandler_IAMAuth_UnregisteredPrincipal_Returns403(t *testing.T) {
	setupTestDepsWithPrincipals([]string{"arn:aws:iam::123456789012:role/IngestRole"})
	ctx := context.Background()

	request := events.APIGatewayProxyRequest{
		Path: "/jmap-iam/user-123",
		Body: `{"using":[],"methodCalls":[]}`,
		PathParameters: map[string]string{
			"accountId": "user-123",
		},
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "test-request-id",
			Identity: events.APIGatewayRequestIdentity{
				UserArn: "arn:aws:iam::123456789012:role/UnauthorizedRole",
			},
		},
	}

	response, err := handler(ctx, request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if response.StatusCode != 403 {
		t.Errorf("expected status code 403, got %d. Body: %s", response.StatusCode, response.Body)
	}
}

func TestHandler_CognitoAuth_BypassesPrincipalCheck(t *testing.T) {
	// Registry has no principals registered, but Cognito requests should still work
	setupTestDepsWithPrincipals([]string{})
	ctx := context.Background()

	request := events.APIGatewayProxyRequest{
		Path: "/jmap", // Not /jmap-iam
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

	// Should succeed even with empty principal list because this is Cognito auth
	if response.StatusCode != 200 {
		t.Errorf("expected status code 200, got %d. Body: %s", response.StatusCode, response.Body)
	}
}

func TestHandler_IAMAuth_AssumedRole_MatchesRegisteredRole(t *testing.T) {
	setupTestDepsWithPrincipals([]string{"arn:aws:iam::123456789012:role/IngestRole"})
	ctx := context.Background()

	request := events.APIGatewayProxyRequest{
		Path: "/jmap-iam/user-123",
		Body: `{"using":[],"methodCalls":[]}`,
		PathParameters: map[string]string{
			"accountId": "user-123",
		},
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "test-request-id",
			Identity: events.APIGatewayRequestIdentity{
				// Assumed role ARN should match the registered role
				UserArn: "arn:aws:sts::123456789012:assumed-role/IngestRole/session-123",
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
}

// setupTestDepsWithMethods creates test deps with mock methods for result reference testing
func setupTestDepsWithMethods(invoker *mockInvoker) {
	tp := noop.NewTracerProvider()
	otel.SetTracerProvider(tp)

	registry := plugin.NewRegistry()
	registry.AddMethod("Email/query", plugin.MethodTarget{
		InvocationType: "lambda-invoke",
		InvokeTarget:   "arn:aws:lambda:us-east-1:123456789012:function:email-query",
	})
	registry.AddMethod("Email/get", plugin.MethodTarget{
		InvocationType: "lambda-invoke",
		InvokeTarget:   "arn:aws:lambda:us-east-1:123456789012:function:email-get",
	})

	deps = &Dependencies{
		Registry: registry,
		Invoker:  invoker,
	}
}

func TestHandler_ResultReference_SimplePath_Resolves(t *testing.T) {
	// Mock invoker that returns proper responses
	var capturedGetArgs map[string]any
	invoker := &mockInvoker{
		invokeFunc: func(ctx context.Context, target plugin.MethodTarget, request plugin.PluginInvocationRequest) (*plugin.PluginInvocationResponse, error) {
			if request.Method == "Email/query" {
				return &plugin.PluginInvocationResponse{
					MethodResponse: plugin.MethodResponse{
						Name: "Email/query",
						Args: map[string]any{
							"accountId": request.AccountID,
							"ids":       []any{"email1", "email2", "email3"},
						},
						ClientID: request.ClientID,
					},
				}, nil
			}
			// Email/get - capture the resolved args
			capturedGetArgs = request.Args
			return &plugin.PluginInvocationResponse{
				MethodResponse: plugin.MethodResponse{
					Name:     "Email/get",
					Args:     map[string]any{"accountId": request.AccountID, "list": []any{}},
					ClientID: request.ClientID,
				},
			}, nil
		},
	}

	setupTestDepsWithMethods(invoker)
	ctx := context.Background()

	// Request with result reference: Email/get references Email/query's ids
	request := events.APIGatewayProxyRequest{
		Body: `{
			"using":[],
			"methodCalls":[
				["Email/query",{"accountId":"user-123"},"query0"],
				["Email/get",{"accountId":"user-123","#ids":{"resultOf":"query0","name":"Email/query","path":"/ids"}},"get0"]
			]
		}`,
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

	// Verify the Email/get was called with resolved ids
	if capturedGetArgs == nil {
		t.Fatal("Email/get was not called")
	}

	ids, ok := capturedGetArgs["ids"]
	if !ok {
		t.Fatal("expected 'ids' key in captured args")
	}

	idsSlice, ok := ids.([]any)
	if !ok {
		t.Fatalf("expected ids to be []any, got %T", ids)
	}

	if len(idsSlice) != 3 {
		t.Errorf("expected 3 ids, got %d", len(idsSlice))
	}
}

func TestHandler_ResultReference_WildcardPath_Resolves(t *testing.T) {
	var capturedGetArgs map[string]any
	invoker := &mockInvoker{
		invokeFunc: func(ctx context.Context, target plugin.MethodTarget, request plugin.PluginInvocationRequest) (*plugin.PluginInvocationResponse, error) {
			if request.Method == "Email/query" {
				return &plugin.PluginInvocationResponse{
					MethodResponse: plugin.MethodResponse{
						Name: "Email/query",
						Args: map[string]any{
							"accountId": request.AccountID,
							"list": []any{
								map[string]any{"id": "email1", "threadId": "thread1"},
								map[string]any{"id": "email2", "threadId": "thread2"},
							},
						},
						ClientID: request.ClientID,
					},
				}, nil
			}
			capturedGetArgs = request.Args
			return &plugin.PluginInvocationResponse{
				MethodResponse: plugin.MethodResponse{
					Name:     "Email/get",
					Args:     map[string]any{"accountId": request.AccountID, "list": []any{}},
					ClientID: request.ClientID,
				},
			}, nil
		},
	}

	setupTestDepsWithMethods(invoker)
	ctx := context.Background()

	// Request with wildcard result reference
	request := events.APIGatewayProxyRequest{
		Body: `{
			"using":[],
			"methodCalls":[
				["Email/query",{"accountId":"user-123"},"query0"],
				["Email/get",{"accountId":"user-123","#ids":{"resultOf":"query0","name":"Email/query","path":"/list/*/id"}},"get0"]
			]
		}`,
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

	if capturedGetArgs == nil {
		t.Fatal("Email/get was not called")
	}

	ids, ok := capturedGetArgs["ids"]
	if !ok {
		t.Fatal("expected 'ids' key in captured args")
	}

	idsSlice, ok := ids.([]any)
	if !ok {
		t.Fatalf("expected ids to be []any, got %T", ids)
	}

	if len(idsSlice) != 2 {
		t.Errorf("expected 2 ids, got %d", len(idsSlice))
	}

	if idsSlice[0] != "email1" || idsSlice[1] != "email2" {
		t.Errorf("expected [email1, email2], got %v", idsSlice)
	}
}

func TestHandler_ResultReference_InvalidReference_ReturnsError(t *testing.T) {
	invoker := &mockInvoker{
		invokeFunc: func(ctx context.Context, target plugin.MethodTarget, request plugin.PluginInvocationRequest) (*plugin.PluginInvocationResponse, error) {
			return &plugin.PluginInvocationResponse{
				MethodResponse: plugin.MethodResponse{
					Name:     request.Method,
					Args:     map[string]any{"accountId": request.AccountID},
					ClientID: request.ClientID,
				},
			}, nil
		},
	}

	setupTestDepsWithMethods(invoker)
	ctx := context.Background()

	// Request with invalid result reference (resultOf doesn't exist)
	request := events.APIGatewayProxyRequest{
		Body: `{
			"using":[],
			"methodCalls":[
				["Email/get",{"accountId":"user-123","#ids":{"resultOf":"nonexistent","name":"Email/query","path":"/ids"}},"get0"]
			]
		}`,
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

	// Should still return 200, but with error response
	if response.StatusCode != 200 {
		t.Errorf("expected status code 200, got %d. Body: %s", response.StatusCode, response.Body)
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
	if !ok || errorType != "invalidResultReference" {
		t.Errorf("expected error type 'invalidResultReference', got '%v'", respArgs["type"])
	}
}

func TestHandler_ResultReference_ConflictingKeys_ReturnsError(t *testing.T) {
	invoker := &mockInvoker{
		invokeFunc: func(ctx context.Context, target plugin.MethodTarget, request plugin.PluginInvocationRequest) (*plugin.PluginInvocationResponse, error) {
			return &plugin.PluginInvocationResponse{
				MethodResponse: plugin.MethodResponse{
					Name: "Email/query",
					Args: map[string]any{
						"accountId": request.AccountID,
						"ids":       []any{"email1"},
					},
					ClientID: request.ClientID,
				},
			}, nil
		},
	}

	setupTestDepsWithMethods(invoker)
	ctx := context.Background()

	// Request with both "ids" and "#ids" - should be an error
	request := events.APIGatewayProxyRequest{
		Body: `{
			"using":[],
			"methodCalls":[
				["Email/query",{"accountId":"user-123"},"query0"],
				["Email/get",{"accountId":"user-123","ids":["existing"],"#ids":{"resultOf":"query0","name":"Email/query","path":"/ids"}},"get0"]
			]
		}`,
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

	// First response should be Email/query success
	if len(jmapResp.MethodResponses) != 2 {
		t.Fatalf("expected 2 method responses, got %d", len(jmapResp.MethodResponses))
	}

	// Second response should be error for conflicting keys
	respName, ok := jmapResp.MethodResponses[1][0].(string)
	if !ok || respName != "error" {
		t.Errorf("expected error response for second call, got '%v'", jmapResp.MethodResponses[1][0])
	}

	respArgs, ok := jmapResp.MethodResponses[1][1].(map[string]any)
	if !ok {
		t.Fatalf("expected args to be a map")
	}

	errorType, ok := respArgs["type"].(string)
	if !ok || errorType != "invalidArguments" {
		t.Errorf("expected error type 'invalidArguments', got '%v'", respArgs["type"])
	}
}
