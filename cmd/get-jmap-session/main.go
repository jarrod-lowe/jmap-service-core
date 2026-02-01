package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/jarrod-lowe/jmap-service-core/internal/db"
	"github.com/jarrod-lowe/jmap-service-core/internal/plugin"
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

// AccountStore defines the interface for account operations
type AccountStore interface {
	EnsureAccount(ctx context.Context, userID string) (*db.Account, error)
}

// accountStore is the package-level account store (injectable for testing)
var accountStore AccountStore

// pluginRegistry holds loaded plugin configuration (injectable for testing)
var pluginRegistry *plugin.Registry

// JMAPSession represents the JMAP Session object per RFC 8620
type JMAPSession struct {
	Capabilities    map[string]any     `json:"capabilities"`
	Accounts        map[string]Account `json:"accounts"`
	PrimaryAccounts map[string]string  `json:"primaryAccounts"`
	Username        string             `json:"username"`
	APIUrl          string             `json:"apiUrl"`
	DownloadUrl     string             `json:"downloadUrl"`
	UploadUrl       string             `json:"uploadUrl"`
	EventSourceUrl  string             `json:"eventSourceUrl,omitempty"`
	State           string             `json:"state"`
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

	// Ensure account exists and update lastDiscoveryAccess
	_, err = accountStore.EnsureAccount(ctx, userID)
	if err != nil {
		logger.ErrorContext(ctx, "Failed to ensure account",
			slog.String("request_id", request.RequestContext.RequestID),
			slog.String("account_id", userID),
			slog.String("error", err.Error()),
		)
		return Response{
			StatusCode: 500,
			Headers:    map[string]string{"Content-Type": "application/json"},
			Body:       `{"error":"Internal server error"}`,
		}, nil
	}

	session := buildSession(userID, config, pluginRegistry)

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

func buildSession(userID string, cfg Config, registry *plugin.Registry) JMAPSession {
	baseURL := fmt.Sprintf("https://%s/v1", cfg.APIDomain)

	// Build capabilities, accounts, and primaryAccounts from registry
	capabilities := make(map[string]any)
	accountCapabilities := make(map[string]any)
	primaryAccounts := make(map[string]string)

	if registry != nil {
		for _, cap := range registry.GetCapabilities() {
			capConfig := registry.GetCapabilityConfig(cap)
			if capConfig == nil {
				capConfig = map[string]any{}
			}
			capabilities[cap] = capConfig
			// For upload-put extension, include config in account capabilities
			// so clients know the limits for this account
			if cap == "https://jmap.rrod.net/extensions/upload-put" {
				accountCapabilities[cap] = capConfig
			} else {
				accountCapabilities[cap] = map[string]any{}
			}
			primaryAccounts[cap] = userID
		}
	}

	return JMAPSession{
		Capabilities: capabilities,
		Accounts: map[string]Account{
			userID: {
				Name:                "mailbox",
				IsPersonal:          true,
				IsReadOnly:          false,
				AccountCapabilities: accountCapabilities,
			},
		},
		PrimaryAccounts: primaryAccounts,
		Username:        userID,
		APIUrl:          fmt.Sprintf("%s/jmap", baseURL),
		DownloadUrl:     fmt.Sprintf("%s/download/{accountId}/{blobId}", baseURL),
		UploadUrl:       fmt.Sprintf("%s/upload/{accountId}", baseURL),
		State:           "0",
	}
}

func main() {
	ctx := context.Background()

	tp, err := xrayconfig.NewTracerProvider(ctx)
	if err != nil {
		logger.Error("FATAL: Failed to initialize tracer provider",
			slog.String("error", err.Error()),
		)
		panic(err)
	}
	otel.SetTracerProvider(tp)

	// Initialize DynamoDB client with OTel instrumentation
	tableName := os.Getenv("DYNAMODB_TABLE")
	if tableName == "" {
		logger.Error("FATAL: DYNAMODB_TABLE environment variable is required")
		panic("DYNAMODB_TABLE environment variable is required")
	}
	dbClient, err := db.NewClient(ctx, tableName)
	if err != nil {
		logger.Error("FATAL: Failed to initialize DynamoDB client",
			slog.String("error", err.Error()),
		)
		panic(err)
	}
	accountStore = dbClient

	// Load plugin registry
	pluginRegistry = plugin.NewRegistry()
	if err := pluginRegistry.LoadFromDynamoDB(ctx, dbClient); err != nil {
		logger.Error("FATAL: Failed to load plugin registry",
			slog.String("error", err.Error()),
		)
		panic(err)
	}

	lambda.Start(otellambda.InstrumentHandler(handler, xrayconfig.WithRecommendedOptions(tp)...))
}
