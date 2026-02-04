package tracing

import (
	"context"

	"go.opentelemetry.io/contrib/instrumentation/github.com/aws/aws-lambda-go/otellambda/xrayconfig"
	"go.opentelemetry.io/contrib/propagators/aws/xray"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// InitPropagator registers the composite text map propagator with X-Ray, W3C
// Trace Context, and Baggage. This ensures trace context is properly propagated
// to downstream services via X-Amzn-Trace-Id header.
func InitPropagator() {
	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			xray.Propagator{},
			propagation.TraceContext{},
			propagation.Baggage{},
		),
	)
}

// Init initializes the X-Ray tracer provider and text map propagator.
// Call once at Lambda cold start before any AWS SDK clients are created.
// Returns the TracerProvider for use with otellambda.InstrumentHandler.
func Init(ctx context.Context) (*sdktrace.TracerProvider, error) {
	InitPropagator()
	return xrayconfig.NewTracerProvider(ctx)
}

// RequestID returns an attribute for the request ID
func RequestID(id string) attribute.KeyValue {
	return attribute.String("request_id", id)
}

// AccountID returns an attribute for the account ID
func AccountID(id string) attribute.KeyValue {
	return attribute.String("account_id", id)
}

// BlobID returns an attribute for the blob ID
func BlobID(id string) attribute.KeyValue {
	return attribute.String("blob_id", id)
}

// Function returns an attribute for the function name
func Function(name string) attribute.KeyValue {
	return attribute.String("function", name)
}

// JMAPMethod returns an attribute for the JMAP method name
func JMAPMethod(method string) attribute.KeyValue {
	return attribute.String("jmap.method", method)
}

// JMAPClientID returns an attribute for the JMAP client ID
func JMAPClientID(clientID string) attribute.KeyValue {
	return attribute.String("jmap.client_id", clientID)
}

// JMAPCallIndex returns an attribute for the JMAP call index
func JMAPCallIndex(index int) attribute.KeyValue {
	return attribute.Int("jmap.call_index", index)
}

// StartHandlerSpan creates a root span for a Lambda handler with consistent attributes.
// Returns the updated context and span. Caller must defer span.End().
func StartHandlerSpan(ctx context.Context, handlerName string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	tracer := otel.Tracer("jmap-service")
	ctx, span := tracer.Start(ctx, handlerName)
	span.SetAttributes(attrs...)
	return ctx, span
}

// StartMethodSpan creates a span for a JMAP method call with standard attributes.
// Returns the updated context and span. Caller must defer span.End().
func StartMethodSpan(ctx context.Context, tracerName, methodName, clientID string, callIndex int) (context.Context, trace.Span) {
	tracer := otel.Tracer(tracerName)
	ctx, span := tracer.Start(ctx, "JMAP Method",
		trace.WithAttributes(
			JMAPMethod(methodName),
			JMAPClientID(clientID),
			JMAPCallIndex(callIndex),
		),
	)
	return ctx, span
}

// RecordError marks the span as errored and records the error details.
func RecordError(span trace.Span, err error) {
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

// StartColdStartSpan creates a span for Lambda cold start initialization.
// Returns the updated context and span. Caller must defer span.End().
func StartColdStartSpan(ctx context.Context, functionName string) (context.Context, trace.Span) {
	tracer := otel.Tracer("jmap-service")
	ctx, span := tracer.Start(ctx, "ColdStart",
		trace.WithAttributes(
			Function(functionName),
		),
	)
	return ctx, span
}
