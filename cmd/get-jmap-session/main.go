package main

import (
	"context"
	"encoding/json"
	"fmt"
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
	logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
)

// JMAPSession represents the JMAP Session object per RFC 8620
type JMAPSession struct {
	Capabilities    map[string]CoreCapability `json:"capabilities"`
	Accounts        map[string]Account        `json:"accounts"`
	PrimaryAccounts map[string]string         `json:"primaryAccounts"`
	Username        string                    `json:"username"`
	APIUrl          string                    `json:"apiUrl"`
	DownloadUrl     string                    `json:"downloadUrl"`
	UploadUrl       string                    `json:"uploadUrl"`
	EventSourceUrl  string                    `json:"eventSourceUrl"`
	State           string                    `json:"state"`
}

// CoreCapability represents the urn:ietf:params:jmap:core capability
type CoreCapability struct {
	MaxSizeUpload         int64    `json:"maxSizeUpload"`
	MaxConcurrentUpload   int      `json:"maxConcurrentUpload"`
	MaxSizeRequest        int64    `json:"maxSizeRequest"`
	MaxConcurrentRequests int      `json:"maxConcurrentRequests"`
	MaxCallsInRequest     int      `json:"maxCallsInRequest"`
	MaxObjectsInGet       int      `json:"maxObjectsInGet"`
	MaxObjectsInSet       int      `json:"maxObjectsInSet"`
	CollationAlgorithms   []string `json:"collationAlgorithms"`
}

// Account represents a JMAP account
type Account struct {
	Name                string            `json:"name"`
	IsPersonal          bool              `json:"isPersonal"`
	IsReadOnly          bool              `json:"isReadOnly"`
	AccountCapabilities map[string]any    `json:"accountCapabilities"`
}

// Response is the API Gateway proxy response
type Response struct {
	StatusCode int               `json:"statusCode"`
	Headers    map[string]string `json:"headers"`
	Body       string            `json:"body"`
}

const coreCapability = "urn:ietf:params:jmap:core"

// Config holds application configuration
type Config struct {
	APIDomain string
}

// LoadConfig loads configuration from environment variables
func LoadConfig() Config {
	domain := os.Getenv("API_DOMAIN")
	if domain == "" {
		domain = "localhost"
	}
	return Config{APIDomain: domain}
}

var config = LoadConfig()

func handler(ctx context.Context, request events.APIGatewayProxyRequest) (Response, error) {
	tracer := otel.Tracer("jmap-get-session")
	ctx, span := tracer.Start(ctx, "GetJmapSessionHandler")
	defer span.End()

	span.SetAttributes(
		attribute.String("function", "get-jmap-session"),
		attribute.String("request_id", request.RequestContext.RequestID),
	)

	// Extract sub claim from Cognito authorizer
	userID, err := extractSubClaim(request)
	if err != nil {
		logger.WarnContext(ctx, "Missing or invalid sub claim",
			slog.String("request_id", request.RequestContext.RequestID),
			slog.String("error", err.Error()),
		)
		return Response{
			StatusCode: 401,
			Headers:    map[string]string{"Content-Type": "application/json"},
			Body:       `{"error":"Unauthorized","message":"Missing or invalid authentication"}`,
		}, nil
	}

	span.SetAttributes(attribute.String("account_id", userID))

	logger.InfoContext(ctx, "Processing session request",
		slog.String("request_id", request.RequestContext.RequestID),
		slog.String("account_id", userID),
	)

	session := buildSession(userID, config)

	bodyJSON, err := json.Marshal(session)
	if err != nil {
		logger.ErrorContext(ctx, "Failed to marshal session",
			slog.String("error", err.Error()),
		)
		return Response{
			StatusCode: 500,
			Headers:    map[string]string{"Content-Type": "application/json"},
			Body:       `{"error":"Internal server error"}`,
		}, nil
	}

	logger.InfoContext(ctx, "Session request completed",
		slog.String("request_id", request.RequestContext.RequestID),
		slog.String("account_id", userID),
	)

	return Response{
		StatusCode: 200,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       string(bodyJSON),
	}, nil
}

func extractSubClaim(request events.APIGatewayProxyRequest) (string, error) {
	authorizer := request.RequestContext.Authorizer
	if authorizer == nil {
		return "", fmt.Errorf("no authorizer context")
	}

	claims, ok := authorizer["claims"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("no claims in authorizer")
	}

	sub, ok := claims["sub"].(string)
	if !ok || sub == "" {
		return "", fmt.Errorf("sub claim not found or empty")
	}

	return sub, nil
}

func buildSession(userID string, cfg Config) JMAPSession {
	baseURL := fmt.Sprintf("https://%s/v1", cfg.APIDomain)

	return JMAPSession{
		Capabilities: map[string]CoreCapability{
			coreCapability: {
				MaxSizeUpload:         50000000,
				MaxConcurrentUpload:   4,
				MaxSizeRequest:        10000000,
				MaxConcurrentRequests: 4,
				MaxCallsInRequest:     16,
				MaxObjectsInGet:       500,
				MaxObjectsInSet:       500,
				CollationAlgorithms:   []string{"i;ascii-casemap"},
			},
		},
		Accounts: map[string]Account{
			userID: {
				Name:       "mailbox",
				IsPersonal: true,
				IsReadOnly: false,
				AccountCapabilities: map[string]any{
					coreCapability: map[string]any{},
				},
			},
		},
		PrimaryAccounts: map[string]string{
			coreCapability: userID,
		},
		Username:       userID,
		APIUrl:         fmt.Sprintf("%s/jmap", baseURL),
		DownloadUrl:    fmt.Sprintf("%s/download/{accountId}/{blobId}/{name}", baseURL),
		UploadUrl:      fmt.Sprintf("%s/upload/{accountId}", baseURL),
		EventSourceUrl: fmt.Sprintf("%s/events/{types}/{closeafter}/{ping}", baseURL),
		State:          "0",
	}
}

func main() {
	tp, err := xrayconfig.NewTracerProvider(context.Background())
	if err != nil {
		logger.Error("FATAL: Failed to initialize tracer provider",
			slog.String("error", err.Error()),
		)
		panic(err)
	}
	otel.SetTracerProvider(tp)

	lambda.Start(otellambda.InstrumentHandler(handler, xrayconfig.WithRecommendedOptions(tp)...))
}
