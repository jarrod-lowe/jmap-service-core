package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestHandler(t *testing.T) {
	// Use no-op tracer for tests
	tp := noop.NewTracerProvider()
	otel.SetTracerProvider(tp)

	ctx := context.Background()

	request := events.LambdaFunctionURLRequest{
		RequestContext: events.LambdaFunctionURLRequestContext{
			RequestID: "test-request-id",
			HTTP: events.LambdaFunctionURLRequestContextHTTPDescription{
				Path: "/test",
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

	var body ResponseBody
	if err := json.Unmarshal([]byte(response.Body), &body); err != nil {
		t.Fatalf("failed to unmarshal response body: %v", err)
	}

	if body.Status != "ok" {
		t.Errorf("expected status 'ok', got '%s'", body.Status)
	}

	if body.Message != "Hello from JMAP service" {
		t.Errorf("expected message 'Hello from JMAP service', got '%s'", body.Message)
	}
}

func TestHandlerResponseStructure(t *testing.T) {
	// Use no-op tracer for tests
	tp := noop.NewTracerProvider()
	otel.SetTracerProvider(tp)

	ctx := context.Background()

	request := events.LambdaFunctionURLRequest{
		RequestContext: events.LambdaFunctionURLRequestContext{
			RequestID: "test-request-id-2",
		},
	}

	response, _ := handler(ctx, request)

	if response.Headers["Content-Type"] != "application/json" {
		t.Errorf("expected Content-Type header to be 'application/json', got '%s'", response.Headers["Content-Type"])
	}

	// Verify body is valid JSON
	if !json.Valid([]byte(response.Body)) {
		t.Error("response body is not valid JSON")
	}
}
