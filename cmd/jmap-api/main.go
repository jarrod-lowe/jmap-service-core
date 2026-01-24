package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	lambdasvc "github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/jarrod-lowe/jmap-service-core/internal/db"
	"github.com/jarrod-lowe/jmap-service-core/internal/plugin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/aws/aws-lambda-go/otellambda"
	"go.opentelemetry.io/contrib/instrumentation/github.com/aws/aws-lambda-go/otellambda/xrayconfig"
	"go.opentelemetry.io/contrib/instrumentation/github.com/aws/aws-sdk-go-v2/otelaws"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

var (
	logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
)

// JMAPRequest represents a JMAP request per RFC 8620
type JMAPRequest struct {
	Using       []string        `json:"using"`
	MethodCalls [][]any         `json:"methodCalls"`
	CreatedIDs  map[string]string `json:"createdIds,omitempty"`
}

// JMAPResponse represents a JMAP response per RFC 8620
type JMAPResponse struct {
	MethodResponses [][]any         `json:"methodResponses"`
	CreatedIDs      map[string]string `json:"createdIds,omitempty"`
	SessionState    string            `json:"sessionState"`
}

// Response is the API Gateway proxy response
type Response struct {
	StatusCode int               `json:"statusCode"`
	Headers    map[string]string `json:"headers"`
	Body       string            `json:"body"`
}

// Dependencies for handler (injectable for testing)
type Dependencies struct {
	Registry *plugin.Registry
	Invoker  plugin.Invoker
}

var deps *Dependencies

// handler processes JMAP requests
func handler(ctx context.Context, request events.APIGatewayProxyRequest) (Response, error) {
	tracer := otel.Tracer("jmap-api")
	ctx, span := tracer.Start(ctx, "JmapApiHandler")
	defer span.End()

	span.SetAttributes(
		attribute.String("function", "jmap-api"),
		attribute.String("request_id", request.RequestContext.RequestID),
	)

	// Extract accountId from request (JWT sub or path param)
	accountID, err := extractAccountID(request)
	if err != nil {
		logger.WarnContext(ctx, "Failed to extract account ID",
			slog.String("request_id", request.RequestContext.RequestID),
			slog.String("error", err.Error()),
		)
		return Response{
			StatusCode: 401,
			Headers:    map[string]string{"Content-Type": "application/json"},
			Body:       `{"error":"Unauthorized","message":"Missing or invalid authentication"}`,
		}, nil
	}

	span.SetAttributes(attribute.String("account_id", accountID))

	// Check principal authorization for IAM-authenticated requests
	if isIAMAuthenticatedRequest(request) {
		callerPrincipal := extractCallerPrincipal(request)
		if !deps.Registry.IsAllowedPrincipal(callerPrincipal) {
			logger.WarnContext(ctx, "Unauthorized IAM principal",
				slog.String("request_id", request.RequestContext.RequestID),
				slog.String("caller_principal", callerPrincipal),
			)
			return Response{
				StatusCode: 403,
				Headers:    map[string]string{"Content-Type": "application/json"},
				Body:       `{"type":"forbidden","description":"Principal not authorized for IAM access"}`,
			}, nil
		}
	}

	// Parse JMAP request
	var jmapReq JMAPRequest
	if err := json.Unmarshal([]byte(request.Body), &jmapReq); err != nil {
		logger.WarnContext(ctx, "Invalid JSON in request body",
			slog.String("request_id", request.RequestContext.RequestID),
			slog.String("error", err.Error()),
		)
		return Response{
			StatusCode: 400,
			Headers:    map[string]string{"Content-Type": "application/json"},
			Body:       `{"type":"urn:ietf:params:jmap:error:notJSON","status":400,"detail":"Invalid JSON in request body"}`,
		}, nil
	}

	// Validate capabilities
	for _, cap := range jmapReq.Using {
		if !deps.Registry.HasCapability(cap) {
			return Response{
				StatusCode: 400,
				Headers:    map[string]string{"Content-Type": "application/json"},
				Body:       fmt.Sprintf(`{"type":"urn:ietf:params:jmap:error:unknownCapability","status":400,"detail":"Unknown capability: %s"}`, cap),
			}, nil
		}
	}

	// Process method calls
	methodResponses := make([][]any, 0, len(jmapReq.MethodCalls))
	for i, call := range jmapReq.MethodCalls {
		resp := processMethodCall(ctx, accountID, call, i, request.RequestContext.RequestID)
		methodResponses = append(methodResponses, resp)
	}

	// Build response
	jmapResp := JMAPResponse{
		MethodResponses: methodResponses,
		SessionState:    "0",
	}

	bodyJSON, err := json.Marshal(jmapResp)
	if err != nil {
		logger.ErrorContext(ctx, "Failed to marshal response",
			slog.String("error", err.Error()),
		)
		return Response{
			StatusCode: 500,
			Headers:    map[string]string{"Content-Type": "application/json"},
			Body:       `{"error":"Internal server error"}`,
		}, nil
	}

	logger.InfoContext(ctx, "JMAP request completed",
		slog.String("request_id", request.RequestContext.RequestID),
		slog.String("account_id", accountID),
		slog.Int("method_count", len(jmapReq.MethodCalls)),
	)

	return Response{
		StatusCode: 200,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       string(bodyJSON),
	}, nil
}

// extractAccountID extracts account ID from JWT claims or path parameter
func extractAccountID(request events.APIGatewayProxyRequest) (string, error) {
	// Check path parameter first (IAM auth)
	if accountID, ok := request.PathParameters["accountId"]; ok && accountID != "" {
		return accountID, nil
	}

	// Fall back to Cognito JWT claims
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

// processMethodCall dispatches a method call to the appropriate plugin
func processMethodCall(ctx context.Context, accountID string, call []any, index int, requestID string) []any {
	// Validate call structure: [methodName, args, clientId]
	if len(call) != 3 {
		return []any{"error", map[string]any{
			"type":        "invalidArguments",
			"description": "Method call must have exactly 3 elements: [name, args, clientId]",
		}, ""}
	}

	methodName, ok := call[0].(string)
	if !ok {
		return []any{"error", map[string]any{
			"type":        "invalidArguments",
			"description": "Method name must be a string",
		}, ""}
	}

	args, ok := call[1].(map[string]any)
	if !ok {
		return []any{"error", map[string]any{
			"type":        "invalidArguments",
			"description": "Method arguments must be an object",
		}, methodName}
	}

	clientID, ok := call[2].(string)
	if !ok {
		clientID = ""
	}

	// Look up method target
	target := deps.Registry.GetMethodTarget(methodName)
	if target == nil {
		return []any{"error", map[string]any{
			"type": "unknownMethod",
		}, clientID}
	}

	// Validate accountId in args matches authenticated accountId
	if argsAccountID, ok := args["accountId"].(string); ok {
		if argsAccountID != accountID {
			return []any{"error", map[string]any{
				"type":        "accountNotFound",
				"description": "Account ID mismatch",
			}, clientID}
		}
	}

	// Build plugin request
	pluginReq := plugin.PluginInvocationRequest{
		RequestID: requestID,
		CallIndex: index,
		AccountID: accountID,
		Method:    methodName,
		Args:      args,
		ClientID:  clientID,
	}

	// Invoke plugin
	pluginResp, err := deps.Invoker.Invoke(ctx, *target, pluginReq)
	if err != nil {
		logger.ErrorContext(ctx, "Plugin invocation failed",
			slog.String("method", methodName),
			slog.String("error", err.Error()),
		)
		return []any{"error", map[string]any{
			"type":        "serverFail",
			"description": "Plugin invocation failed",
		}, clientID}
	}

	// Return plugin response as JMAP method response
	return []any{
		pluginResp.MethodResponse.Name,
		pluginResp.MethodResponse.Args,
		pluginResp.MethodResponse.ClientID,
	}
}

// isIAMAuthenticatedRequest checks if the request is IAM-authenticated
// by checking if UserArn is populated in the request context
func isIAMAuthenticatedRequest(request events.APIGatewayProxyRequest) bool {
	return request.RequestContext.Identity.UserArn != ""
}

// extractCallerPrincipal extracts the caller's IAM principal ARN from the request
func extractCallerPrincipal(request events.APIGatewayProxyRequest) string {
	return request.RequestContext.Identity.UserArn
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

	// Initialize DynamoDB client
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

	// Load plugin registry
	registry := plugin.NewRegistry()
	if err := registry.LoadFromDynamoDB(ctx, dbClient); err != nil {
		logger.Error("FATAL: Failed to load plugin registry",
			slog.String("error", err.Error()),
		)
		panic(err)
	}

	// Initialize Lambda invoker
	lambdaCfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		logger.Error("FATAL: Failed to load AWS config for Lambda",
			slog.String("error", err.Error()),
		)
		panic(err)
	}
	otelaws.AppendMiddlewares(&lambdaCfg.APIOptions)
	lambdaClient := lambdasvc.NewFromConfig(lambdaCfg)
	invoker := plugin.NewLambdaInvoker(lambdaClient)

	deps = &Dependencies{
		Registry: registry,
		Invoker:  invoker,
	}

	lambda.Start(otellambda.InstrumentHandler(handler, xrayconfig.WithRecommendedOptions(tp)...))
}
