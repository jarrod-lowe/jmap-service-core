package tracing

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestRequestID(t *testing.T) {
	// Test that RequestID returns the expected attribute
	attr := RequestID("test-request-123")

	if attr.Key != "request_id" {
		t.Errorf("expected key 'request_id', got %q", attr.Key)
	}
	if attr.Value.AsString() != "test-request-123" {
		t.Errorf("expected value 'test-request-123', got %q", attr.Value.AsString())
	}
}

func TestAccountID(t *testing.T) {
	attr := AccountID("user-456")

	if attr.Key != "account_id" {
		t.Errorf("expected key 'account_id', got %q", attr.Key)
	}
	if attr.Value.AsString() != "user-456" {
		t.Errorf("expected value 'user-456', got %q", attr.Value.AsString())
	}
}

func TestBlobID(t *testing.T) {
	attr := BlobID("blob-789")

	if attr.Key != "blob_id" {
		t.Errorf("expected key 'blob_id', got %q", attr.Key)
	}
	if attr.Value.AsString() != "blob-789" {
		t.Errorf("expected value 'blob-789', got %q", attr.Value.AsString())
	}
}

func TestFunction(t *testing.T) {
	attr := Function("blob-upload")

	if attr.Key != "function" {
		t.Errorf("expected key 'function', got %q", attr.Key)
	}
	if attr.Value.AsString() != "blob-upload" {
		t.Errorf("expected value 'blob-upload', got %q", attr.Value.AsString())
	}
}

func TestStartHandlerSpan(t *testing.T) {
	// Set up an in-memory exporter for testing
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
	)
	otel.SetTracerProvider(tp)
	defer tp.Shutdown(context.Background())

	ctx := context.Background()

	// Call StartHandlerSpan with attributes
	ctx, span := StartHandlerSpan(ctx, "TestHandler",
		RequestID("req-123"),
		AccountID("acct-456"),
	)
	span.End()

	// Force flush
	tp.ForceFlush(context.Background())

	// Verify span was created with correct name and attributes
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	s := spans[0]
	if s.Name != "TestHandler" {
		t.Errorf("expected span name 'TestHandler', got %q", s.Name)
	}

	// Check attributes
	attrMap := make(map[string]string)
	for _, attr := range s.Attributes {
		attrMap[string(attr.Key)] = attr.Value.AsString()
	}

	if attrMap["request_id"] != "req-123" {
		t.Errorf("expected request_id 'req-123', got %q", attrMap["request_id"])
	}
	if attrMap["account_id"] != "acct-456" {
		t.Errorf("expected account_id 'acct-456', got %q", attrMap["account_id"])
	}
}

func TestRecordError(t *testing.T) {
	// Set up an in-memory exporter for testing
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
	)
	otel.SetTracerProvider(tp)
	defer tp.Shutdown(context.Background())

	ctx := context.Background()
	tracer := otel.Tracer("test")
	ctx, span := tracer.Start(ctx, "TestSpan")

	// Record an error
	testErr := &testError{message: "something went wrong"}
	RecordError(span, testErr)
	span.End()

	// Force flush
	tp.ForceFlush(context.Background())

	// Verify the span has error status
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	s := spans[0]

	// Check that an error event was recorded
	if len(s.Events) == 0 {
		t.Error("expected at least one event (error), got none")
	}

	// Verify error status code
	if s.Status.Code != codes.Error {
		t.Errorf("expected error status code %d, got %d", codes.Error, s.Status.Code)
	}
}

type testError struct {
	message string
}

func (e *testError) Error() string {
	return e.message
}

func TestJMAPMethod(t *testing.T) {
	attr := JMAPMethod("Email/get")

	if attr.Key != "jmap.method" {
		t.Errorf("expected key 'jmap.method', got %q", attr.Key)
	}
	if attr.Value.AsString() != "Email/get" {
		t.Errorf("expected value 'Email/get', got %q", attr.Value.AsString())
	}
}

func TestJMAPClientID(t *testing.T) {
	attr := JMAPClientID("c0")

	if attr.Key != "jmap.client_id" {
		t.Errorf("expected key 'jmap.client_id', got %q", attr.Key)
	}
	if attr.Value.AsString() != "c0" {
		t.Errorf("expected value 'c0', got %q", attr.Value.AsString())
	}
}

func TestJMAPCallIndex(t *testing.T) {
	attr := JMAPCallIndex(2)

	if attr.Key != "jmap.call_index" {
		t.Errorf("expected key 'jmap.call_index', got %q", attr.Key)
	}
	if attr.Value.AsInt64() != 2 {
		t.Errorf("expected value 2, got %d", attr.Value.AsInt64())
	}
}

func TestStartMethodSpan(t *testing.T) {
	// Set up an in-memory exporter for testing
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
	)
	otel.SetTracerProvider(tp)
	defer tp.Shutdown(context.Background())

	ctx := context.Background()

	// Call StartMethodSpan
	ctx, span := StartMethodSpan(ctx, "jmap-api", "Email/get", "c0", 1)
	span.End()

	// Force flush
	tp.ForceFlush(context.Background())

	// Verify span was created with correct name and attributes
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	s := spans[0]
	if s.Name != "JMAP Method" {
		t.Errorf("expected span name 'JMAP Method', got %q", s.Name)
	}

	// Check attributes
	attrMap := make(map[attribute.Key]attribute.Value)
	for _, attr := range s.Attributes {
		attrMap[attr.Key] = attr.Value
	}

	if attrMap["jmap.method"].AsString() != "Email/get" {
		t.Errorf("expected jmap.method 'Email/get', got %q", attrMap["jmap.method"].AsString())
	}
	if attrMap["jmap.client_id"].AsString() != "c0" {
		t.Errorf("expected jmap.client_id 'c0', got %q", attrMap["jmap.client_id"].AsString())
	}
	if attrMap["jmap.call_index"].AsInt64() != 1 {
		t.Errorf("expected jmap.call_index 1, got %d", attrMap["jmap.call_index"].AsInt64())
	}
}

func TestStartColdStartSpan(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
	)
	otel.SetTracerProvider(tp)
	defer tp.Shutdown(context.Background())

	ctx := context.Background()

	ctx, span := StartColdStartSpan(ctx, "test-function")
	span.End()

	tp.ForceFlush(context.Background())

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	s := spans[0]
	if s.Name != "ColdStart" {
		t.Errorf("expected span name 'ColdStart', got %q", s.Name)
	}

	attrMap := make(map[string]string)
	for _, attr := range s.Attributes {
		attrMap[string(attr.Key)] = attr.Value.AsString()
	}
	if attrMap["function"] != "test-function" {
		t.Errorf("expected function 'test-function', got %q", attrMap["function"])
	}
}

func TestInitSetsPropagator(t *testing.T) {
	// Reset propagator to default before test
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator())

	// Call InitPropagator (the new function that sets up the propagator)
	InitPropagator()

	// Get the registered propagator
	propagator := otel.GetTextMapPropagator()

	// Create a test carrier to verify propagator injects X-Ray headers
	carrier := propagation.MapCarrier{}

	// Create a context with a valid span
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
	)
	otel.SetTracerProvider(tp)
	defer tp.Shutdown(context.Background())

	tracer := otel.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test-span")
	defer span.End()

	// Inject the trace context into the carrier
	propagator.Inject(ctx, carrier)

	// Verify X-Amzn-Trace-Id header is set (X-Ray propagator injects this)
	xrayHeader := carrier.Get("X-Amzn-Trace-Id")
	if xrayHeader == "" {
		t.Error("expected X-Amzn-Trace-Id header to be set after Init, got empty string")
	}

	// Verify traceparent header is also set (W3C TraceContext propagator)
	traceparent := carrier.Get("traceparent")
	if traceparent == "" {
		t.Error("expected traceparent header to be set after Init, got empty string")
	}
}
