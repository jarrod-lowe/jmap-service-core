package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/lambda"
)

// mockLambdaClient implements LambdaClient for testing
type mockLambdaClient struct {
	invokeFunc   func(ctx context.Context, params *lambda.InvokeInput, optFns ...func(*lambda.Options)) (*lambda.InvokeOutput, error)
	invokeCalled bool
	invokeInput  *lambda.InvokeInput
}

func (m *mockLambdaClient) Invoke(ctx context.Context, params *lambda.InvokeInput, optFns ...func(*lambda.Options)) (*lambda.InvokeOutput, error) {
	m.invokeCalled = true
	m.invokeInput = params
	if m.invokeFunc != nil {
		return m.invokeFunc(ctx, params, optFns...)
	}
	return &lambda.InvokeOutput{}, nil
}

func TestLambdaInvoker_InvokesLambdaWithCorrectPayload(t *testing.T) {
	var capturedPayload []byte

	mock := &mockLambdaClient{
		invokeFunc: func(ctx context.Context, params *lambda.InvokeInput, optFns ...func(*lambda.Options)) (*lambda.InvokeOutput, error) {
			capturedPayload = params.Payload
			// Return a successful response
			resp := PluginInvocationResponse{
				MethodResponse: MethodResponse{
					Name:     "Email/get",
					Args:     map[string]any{"accountId": "user-123", "list": []any{}},
					ClientID: "c0",
				},
			}
			respBytes, _ := json.Marshal(resp)
			return &lambda.InvokeOutput{
				Payload:    respBytes,
				StatusCode: 200,
			}, nil
		},
	}

	invoker := NewLambdaInvoker(mock)

	request := PluginInvocationRequest{
		RequestID: "req-123",
		CallIndex: 0,
		AccountID: "user-123",
		Method:    "Email/get",
		Args:      map[string]any{"ids": []string{"email-1"}},
		ClientID:  "c0",
	}

	target := MethodTarget{
		InvocationType: "lambda-invoke",
		InvokeTarget:   "arn:aws:lambda:ap-southeast-2:123:function:mail-get",
	}

	_, err := invoker.Invoke(context.Background(), target, request)
	if err != nil {
		t.Fatalf("Invoke returned error: %v", err)
	}

	// Verify Lambda was called
	if !mock.invokeCalled {
		t.Error("Expected Lambda.Invoke to be called")
	}

	// Verify payload was correct
	if capturedPayload == nil {
		t.Fatal("Expected payload to be captured")
	}

	var sentRequest PluginInvocationRequest
	if err := json.Unmarshal(capturedPayload, &sentRequest); err != nil {
		t.Fatalf("Failed to unmarshal captured payload: %v", err)
	}

	if sentRequest.RequestID != "req-123" {
		t.Errorf("Expected RequestID='req-123', got '%s'", sentRequest.RequestID)
	}

	if sentRequest.Method != "Email/get" {
		t.Errorf("Expected Method='Email/get', got '%s'", sentRequest.Method)
	}
}

func TestLambdaInvoker_UsesCorrectFunctionARN(t *testing.T) {
	mock := &mockLambdaClient{
		invokeFunc: func(ctx context.Context, params *lambda.InvokeInput, optFns ...func(*lambda.Options)) (*lambda.InvokeOutput, error) {
			resp := PluginInvocationResponse{
				MethodResponse: MethodResponse{Name: "Email/get", Args: map[string]any{}, ClientID: "c0"},
			}
			respBytes, _ := json.Marshal(resp)
			return &lambda.InvokeOutput{Payload: respBytes, StatusCode: 200}, nil
		},
	}

	invoker := NewLambdaInvoker(mock)

	target := MethodTarget{
		InvocationType: "lambda-invoke",
		InvokeTarget:   "arn:aws:lambda:ap-southeast-2:123:function:my-function",
	}

	_, err := invoker.Invoke(context.Background(), target, PluginInvocationRequest{})
	if err != nil {
		t.Fatalf("Invoke returned error: %v", err)
	}

	if mock.invokeInput == nil {
		t.Fatal("Expected invokeInput to be captured")
	}

	if mock.invokeInput.FunctionName == nil {
		t.Fatal("Expected FunctionName to be set")
	}

	if *mock.invokeInput.FunctionName != "arn:aws:lambda:ap-southeast-2:123:function:my-function" {
		t.Errorf("Expected FunctionName to be ARN, got '%s'", *mock.invokeInput.FunctionName)
	}
}

func TestLambdaInvoker_ReturnsPluginResponse(t *testing.T) {
	mock := &mockLambdaClient{
		invokeFunc: func(ctx context.Context, params *lambda.InvokeInput, optFns ...func(*lambda.Options)) (*lambda.InvokeOutput, error) {
			resp := PluginInvocationResponse{
				MethodResponse: MethodResponse{
					Name:     "Email/get",
					Args:     map[string]any{"accountId": "user-123", "list": []any{}, "notFound": []any{}},
					ClientID: "c0",
				},
			}
			respBytes, _ := json.Marshal(resp)
			return &lambda.InvokeOutput{Payload: respBytes, StatusCode: 200}, nil
		},
	}

	invoker := NewLambdaInvoker(mock)

	resp, err := invoker.Invoke(context.Background(), MethodTarget{InvokeTarget: "arn:test"}, PluginInvocationRequest{ClientID: "c0"})
	if err != nil {
		t.Fatalf("Invoke returned error: %v", err)
	}

	if resp == nil {
		t.Fatal("Expected non-nil response")
	}

	if resp.MethodResponse.Name != "Email/get" {
		t.Errorf("Expected Name='Email/get', got '%s'", resp.MethodResponse.Name)
	}

	if resp.MethodResponse.ClientID != "c0" {
		t.Errorf("Expected ClientID='c0', got '%s'", resp.MethodResponse.ClientID)
	}
}

func TestLambdaInvoker_ReturnsErrorOnLambdaFailure(t *testing.T) {
	mock := &mockLambdaClient{
		invokeFunc: func(ctx context.Context, params *lambda.InvokeInput, optFns ...func(*lambda.Options)) (*lambda.InvokeOutput, error) {
			return nil, errors.New("Lambda invocation failed")
		},
	}

	invoker := NewLambdaInvoker(mock)

	_, err := invoker.Invoke(context.Background(), MethodTarget{InvokeTarget: "arn:test"}, PluginInvocationRequest{})

	if err == nil {
		t.Error("Expected error when Lambda fails")
	}
}

func TestLambdaInvoker_ReturnsErrorResponse(t *testing.T) {
	mock := &mockLambdaClient{
		invokeFunc: func(ctx context.Context, params *lambda.InvokeInput, optFns ...func(*lambda.Options)) (*lambda.InvokeOutput, error) {
			resp := PluginInvocationResponse{
				MethodResponse: MethodResponse{
					Name: "error",
					Args: map[string]any{
						"type":        "invalidArguments",
						"description": "ids is required",
					},
					ClientID: "c0",
				},
			}
			respBytes, _ := json.Marshal(resp)
			return &lambda.InvokeOutput{Payload: respBytes, StatusCode: 200}, nil
		},
	}

	invoker := NewLambdaInvoker(mock)

	resp, err := invoker.Invoke(context.Background(), MethodTarget{InvokeTarget: "arn:test"}, PluginInvocationRequest{ClientID: "c0"})
	if err != nil {
		t.Fatalf("Invoke returned error: %v", err)
	}

	if resp == nil {
		t.Fatal("Expected non-nil response")
	}

	if resp.MethodResponse.Name != "error" {
		t.Errorf("Expected error response, got '%s'", resp.MethodResponse.Name)
	}

	errorType, ok := resp.MethodResponse.Args["type"].(string)
	if !ok || errorType != "invalidArguments" {
		t.Errorf("Expected error type 'invalidArguments', got '%v'", resp.MethodResponse.Args["type"])
	}
}

