package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"go.opentelemetry.io/contrib/instrumentation/github.com/aws/aws-lambda-go/otellambda"
	"go.opentelemetry.io/contrib/instrumentation/github.com/aws/aws-lambda-go/otellambda/xrayconfig"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

var (
	// Structured JSON logger
	logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
)

type Response struct {
	StatusCode int               `json:"statusCode"`
	Headers    map[string]string `json:"headers"`
	Body       string            `json:"body"`
}

type ResponseBody struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

func handler(ctx context.Context, request events.LambdaFunctionURLRequest) (Response, error) {
	// Get tracer from global provider (set up in main() using xrayconfig)
	tracer := otel.Tracer("jmap-hello-world")
	ctx, span := tracer.Start(ctx, "HelloWorldHandler")
	defer span.End()

	// Add attributes
	span.SetAttributes(
		attribute.String("function", "hello-world"),
		attribute.String("request_id", request.RequestContext.RequestID),
		attribute.String("path", request.RequestContext.HTTP.Path),
	)

	// Structured logging with context
	logger.InfoContext(ctx, "Processing request",
		slog.String("request_id", request.RequestContext.RequestID),
		slog.String("path", request.RequestContext.HTTP.Path),
		slog.String("service", "jmap-service"),
	)

	// Create response body
	responseBody := ResponseBody{
		Status:  "ok",
		Message: "Hello from JMAP service",
	}

	bodyJSON, err := json.Marshal(responseBody)
	if err != nil {
		logger.ErrorContext(ctx, "Failed to marshal response",
			slog.String("error", err.Error()),
		)
		return Response{
			StatusCode: 500,
			Headers:    map[string]string{"Content-Type": "application/json"},
			Body:       `{"status":"error","message":"Internal server error"}`,
		}, err
	}

	logger.InfoContext(ctx, "Request processed successfully")

	return Response{
		StatusCode: 200,
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body: string(bodyJSON),
	}, nil
}

func main() {
	// Initialize TracerProvider using xrayconfig for ADOT Lambda Layer
	tp, err := xrayconfig.NewTracerProvider(context.Background())
	if err != nil {
		logger.Error("FATAL: Failed to initialize tracer provider",
			slog.String("error", err.Error()),
		)
		panic(err)
	}
	otel.SetTracerProvider(tp)

	// Wrap handler with OpenTelemetry instrumentation, passing our TracerProvider
	lambda.Start(otellambda.InstrumentHandler(handler, xrayconfig.WithRecommendedOptions(tp)...))
}
