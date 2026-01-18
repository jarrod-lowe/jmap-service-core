package main

import (
	"context"
	"reflect"
	"testing"

	"github.com/jarrod-lowe/jmap-service-core/pkg/plugincontract"
)

func TestHandler_EchoesArgsExactly(t *testing.T) {
	ctx := context.Background()

	request := plugincontract.PluginInvocationRequest{
		RequestID: "test-req-123",
		CallIndex: 0,
		AccountID: "user-123",
		Method:    "Core/echo",
		Args:      map[string]any{"hello": true, "count": float64(42)},
		ClientID:  "c1",
	}

	resp, err := handler(ctx, request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if !reflect.DeepEqual(resp.MethodResponse.Args, request.Args) {
		t.Errorf("expected args %v, got %v", request.Args, resp.MethodResponse.Args)
	}
}

func TestHandler_PreservesClientID(t *testing.T) {
	ctx := context.Background()

	request := plugincontract.PluginInvocationRequest{
		RequestID: "test-req-456",
		CallIndex: 0,
		AccountID: "user-123",
		Method:    "Core/echo",
		Args:      map[string]any{},
		ClientID:  "my-client-id-abc",
	}

	resp, err := handler(ctx, request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if resp.MethodResponse.ClientID != "my-client-id-abc" {
		t.Errorf("expected clientId 'my-client-id-abc', got '%s'", resp.MethodResponse.ClientID)
	}
}

func TestHandler_ResponseNameIsCoreEcho(t *testing.T) {
	ctx := context.Background()

	request := plugincontract.PluginInvocationRequest{
		RequestID: "test-req-789",
		CallIndex: 0,
		AccountID: "user-123",
		Method:    "Core/echo",
		Args:      map[string]any{"test": "value"},
		ClientID:  "c1",
	}

	resp, err := handler(ctx, request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if resp.MethodResponse.Name != "Core/echo" {
		t.Errorf("expected response name 'Core/echo', got '%s'", resp.MethodResponse.Name)
	}
}

func TestHandler_HandlesEmptyArgs(t *testing.T) {
	ctx := context.Background()

	request := plugincontract.PluginInvocationRequest{
		RequestID: "test-req-empty",
		CallIndex: 0,
		AccountID: "user-123",
		Method:    "Core/echo",
		Args:      map[string]any{},
		ClientID:  "c1",
	}

	resp, err := handler(ctx, request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if len(resp.MethodResponse.Args) != 0 {
		t.Errorf("expected empty args, got %v", resp.MethodResponse.Args)
	}
}

func TestHandler_HandlesNestedObjects(t *testing.T) {
	ctx := context.Background()

	nestedArgs := map[string]any{
		"level1": map[string]any{
			"level2": map[string]any{
				"value": "deep",
			},
			"array": []any{"a", "b", float64(3)},
		},
		"simple": "top",
	}

	request := plugincontract.PluginInvocationRequest{
		RequestID: "test-req-nested",
		CallIndex: 0,
		AccountID: "user-123",
		Method:    "Core/echo",
		Args:      nestedArgs,
		ClientID:  "c1",
	}

	resp, err := handler(ctx, request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if !reflect.DeepEqual(resp.MethodResponse.Args, nestedArgs) {
		t.Errorf("nested objects not preserved.\nExpected: %v\nGot: %v", nestedArgs, resp.MethodResponse.Args)
	}
}
