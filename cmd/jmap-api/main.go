package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	lambdasvc "github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"github.com/jarrod-lowe/jmap-service-core/internal/bloballocate"
	"github.com/jarrod-lowe/jmap-service-core/internal/db"
	"github.com/jarrod-lowe/jmap-service-core/internal/plugin"
	"github.com/jarrod-lowe/jmap-service-core/internal/resultref"
	"github.com/jarrod-lowe/jmap-service-libs/tracing"
	"go.opentelemetry.io/contrib/instrumentation/github.com/aws/aws-lambda-go/otellambda"
	"go.opentelemetry.io/contrib/instrumentation/github.com/aws/aws-lambda-go/otellambda/xrayconfig"
	"go.opentelemetry.io/contrib/instrumentation/github.com/aws/aws-sdk-go-v2/otelaws"
	"go.opentelemetry.io/otel"
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
	Registry       *plugin.Registry
	Invoker        plugin.Invoker
	BlobAllocator  *bloballocate.Handler
}

var deps *Dependencies

// handler processes JMAP requests
func handler(ctx context.Context, request events.APIGatewayProxyRequest) (Response, error) {
	ctx, span := tracing.StartHandlerSpan(ctx, "JmapApiHandler",
		tracing.Function("jmap-api"),
		tracing.RequestID(request.RequestContext.RequestID),
	)
	defer span.End()

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

	span.SetAttributes(tracing.AccountID(accountID))

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

	// Process method calls with result reference tracking
	methodResponses := make([][]any, 0, len(jmapReq.MethodCalls))
	trackedResponses := make([]resultref.MethodResponse, 0, len(jmapReq.MethodCalls))
	for i, call := range jmapReq.MethodCalls {
		resp := processMethodCall(ctx, accountID, call, i, request.RequestContext.RequestID, trackedResponses, jmapReq.Using)
		methodResponses = append(methodResponses, resp)
		// Track response for future result references
		trackedResponses = append(trackedResponses, toMethodResponse(resp))
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

// UploadPutCapability is the capability URN for the PUT upload extension
const UploadPutCapability = "https://jmap.rrod.net/extensions/upload-put"

// processMethodCall dispatches a method call to the appropriate plugin
func processMethodCall(ctx context.Context, accountID string, call []any, index int, requestID string, previousResponses []resultref.MethodResponse, usingCaps []string) []any {
	// Extract method name and clientID early for span attributes
	var methodName, clientID string
	if len(call) >= 1 {
		methodName, _ = call[0].(string)
	}
	if len(call) >= 3 {
		clientID, _ = call[2].(string)
	}

	// Create span for this method call
	ctx, span := tracing.StartMethodSpan(ctx, "jmap-api", methodName, clientID, index)
	defer span.End()

	// Validate call structure: [methodName, args, clientId]
	if len(call) != 3 {
		return []any{"error", map[string]any{
			"type":        "invalidArguments",
			"description": "Method call must have exactly 3 elements: [name, args, clientId]",
		}, ""}
	}

	if methodName == "" {
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

	// Resolve result references (RFC 8620 Section 3.7)
	resolvedArgs, err := resultref.ResolveArgs(args, previousResponses)
	if err != nil {
		tracing.RecordError(span, err)
		resolveErr, ok := err.(*resultref.ResolveError)
		if ok {
			return []any{"error", map[string]any{
				"type":        string(resolveErr.Type),
				"description": resolveErr.Description,
			}, clientID}
		}
		return []any{"error", map[string]any{
			"type":        "serverFail",
			"description": "Failed to resolve result references",
		}, clientID}
	}

	// Handle built-in methods before plugin dispatch
	if methodName == "Blob/allocate" {
		return handleBlobAllocate(ctx, accountID, resolvedArgs, clientID, usingCaps)
	}

	// Look up method target
	target := deps.Registry.GetMethodTarget(methodName)
	if target == nil {
		return []any{"error", map[string]any{
			"type": "unknownMethod",
		}, clientID}
	}

	// Validate accountId in args matches authenticated accountId
	if argsAccountID, ok := resolvedArgs["accountId"].(string); ok {
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
		Args:      resolvedArgs,
		ClientID:  clientID,
	}

	// Invoke plugin
	pluginResp, err := deps.Invoker.Invoke(ctx, *target, pluginReq)
	if err != nil {
		tracing.RecordError(span, err)
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

// toMethodResponse converts a JMAP method response array to a MethodResponse struct for tracking
func toMethodResponse(resp []any) resultref.MethodResponse {
	var name string
	var args map[string]any
	var clientID string

	if len(resp) >= 1 {
		name, _ = resp[0].(string)
	}
	if len(resp) >= 2 {
		args, _ = resp[1].(map[string]any)
	}
	if len(resp) >= 3 {
		clientID, _ = resp[2].(string)
	}

	return resultref.MethodResponse{
		Name:     name,
		Args:     args,
		ClientID: clientID,
	}
}

// handleBlobAllocate processes a Blob/allocate method call
func handleBlobAllocate(ctx context.Context, accountID string, args map[string]any, clientID string, usingCaps []string) []any {
	// Check if Blob/allocate is enabled
	if deps.BlobAllocator == nil {
		return []any{"error", map[string]any{
			"type": "unknownMethod",
		}, clientID}
	}

	// Check that the capability is in the using array
	hasCapability := false
	for _, cap := range usingCaps {
		if cap == UploadPutCapability {
			hasCapability = true
			break
		}
	}
	if !hasCapability {
		return []any{"error", map[string]any{
			"type":        "unknownMethod",
			"description": "Blob/allocate requires the " + UploadPutCapability + " capability",
		}, clientID}
	}

	// Validate accountId in args
	argsAccountID, _ := args["accountId"].(string)
	if argsAccountID != "" && argsAccountID != accountID {
		return []any{"error", map[string]any{
			"type":        "accountNotFound",
			"description": "Account ID mismatch",
		}, clientID}
	}

	// Extract the create map (per RFC 9404 pattern)
	createMap, ok := args["create"].(map[string]any)
	if !ok || len(createMap) == 0 {
		return []any{"error", map[string]any{
			"type":        "invalidArguments",
			"description": "create map is required",
		}, clientID}
	}

	created := make(map[string]any)
	notCreated := make(map[string]any)

	// Process each creation request
	for creationID, reqData := range createMap {
		reqMap, ok := reqData.(map[string]any)
		if !ok {
			notCreated[creationID] = map[string]any{
				"type":        "invalidArguments",
				"description": "invalid request format",
			}
			continue
		}

		contentType, _ := reqMap["type"].(string)
		size, _ := reqMap["size"].(float64) // JSON numbers come as float64

		req := bloballocate.AllocateRequest{
			AccountID: accountID,
			Type:      contentType,
			Size:      int64(size),
		}

		resp, err := deps.BlobAllocator.Allocate(ctx, req)
		if err != nil {
			allocErr, ok := err.(*bloballocate.AllocationError)
			if ok {
				errInfo := map[string]any{
					"type":        allocErr.Type,
					"description": allocErr.Message,
				}
				if len(allocErr.Properties) > 0 {
					errInfo["properties"] = allocErr.Properties
				}
				notCreated[creationID] = errInfo
			} else {
				notCreated[creationID] = map[string]any{
					"type":        "serverFail",
					"description": "Failed to allocate blob",
				}
			}
			continue
		}

		created[creationID] = map[string]any{
			"id":      resp.BlobID,
			"type":    resp.Type,
			"size":    resp.Size,
			"url":     resp.URL,
			"expires": resp.URLExpires.UTC().Format("2006-01-02T15:04:05Z"),
		}
	}

	// Build response per RFC 9404 pattern
	response := map[string]any{
		"accountId": accountID,
	}
	if len(created) > 0 {
		response["created"] = created
	} else {
		response["created"] = nil
	}
	if len(notCreated) > 0 {
		response["notCreated"] = notCreated
	} else {
		response["notCreated"] = nil
	}

	return []any{"Blob/allocate", response, clientID}
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

// RealUUIDGenerator generates real UUIDs
type RealUUIDGenerator struct{}

// Generate generates a new UUID v4
func (r *RealUUIDGenerator) Generate() string {
	return uuid.New().String()
}

func main() {
	ctx := context.Background()

	tp, err := tracing.Init(ctx)
	if err != nil {
		logger.Error("FATAL: Failed to initialize tracer provider",
			slog.String("error", err.Error()),
		)
		panic(err)
	}
	otel.SetTracerProvider(tp)

	// Create cold start span - all init AWS calls become children
	ctx, coldStartSpan := tracing.StartColdStartSpan(ctx, "jmap-api")
	defer coldStartSpan.End()

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

	// Initialize Blob/allocate handler
	var blobAllocator *bloballocate.Handler
	blobBucket := os.Getenv("BLOB_BUCKET")
	if blobBucket != "" {
		// Get config from environment
		maxSizeUploadPut, _ := strconv.ParseInt(os.Getenv("MAX_SIZE_UPLOAD_PUT"), 10, 64)
		if maxSizeUploadPut == 0 {
			maxSizeUploadPut = 250000000 // default 250 MB
		}
		maxPendingAllocs, _ := strconv.Atoi(os.Getenv("MAX_PENDING_ALLOCATIONS"))
		if maxPendingAllocs == 0 {
			maxPendingAllocs = 4
		}
		urlExpirySecs, _ := strconv.ParseInt(os.Getenv("ALLOCATION_URL_EXPIRY_SECONDS"), 10, 64)
		if urlExpirySecs == 0 {
			urlExpirySecs = 900 // default 15 minutes
		}

		// Initialize S3 presign client
		s3Client := s3.NewFromConfig(lambdaCfg)
		presignClient := s3.NewPresignClient(s3Client)

		// Initialize DynamoDB client for blob allocations
		ddbClient := dynamodb.NewFromConfig(lambdaCfg)

		blobAllocator = &bloballocate.Handler{
			Storage:          bloballocate.NewS3Storage(presignClient, blobBucket),
			DB:               bloballocate.NewDynamoDBStore(ddbClient, tableName),
			UUIDGen:          &RealUUIDGenerator{},
			MaxSizeUploadPut: maxSizeUploadPut,
			MaxPendingAllocs: maxPendingAllocs,
			URLExpirySecs:    urlExpirySecs,
		}
	}

	deps = &Dependencies{
		Registry:      registry,
		Invoker:       invoker,
		BlobAllocator: blobAllocator,
	}

	lambda.Start(otellambda.InstrumentHandler(handler, xrayconfig.WithRecommendedOptions(tp)...))
}
