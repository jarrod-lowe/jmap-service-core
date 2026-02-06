package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	lambdasvc "github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"github.com/jarrod-lowe/jmap-service-core/internal/bloballocate"
	"github.com/jarrod-lowe/jmap-service-core/internal/blobcomplete"
	"github.com/jarrod-lowe/jmap-service-core/internal/db"
	"github.com/jarrod-lowe/jmap-service-core/internal/dispatcher"
	"github.com/jarrod-lowe/jmap-service-core/internal/plugin"
	"github.com/jarrod-lowe/jmap-service-core/internal/resultref"
	"github.com/jarrod-lowe/jmap-service-libs/awsinit"
	"github.com/jarrod-lowe/jmap-service-libs/jmaperror"
	"github.com/jarrod-lowe/jmap-service-libs/logging"
	"github.com/jarrod-lowe/jmap-service-libs/tracing"
)

var logger = logging.New()

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

// DefaultDispatcherPoolSize is the default number of concurrent workers for method dispatch
const DefaultDispatcherPoolSize = 4

// Dependencies for handler (injectable for testing)
type Dependencies struct {
	Registry             *plugin.Registry
	Invoker              plugin.Invoker
	BlobAllocator        *bloballocate.Handler
	BlobCompleter        *blobcomplete.Handler
	DispatcherPoolSize   int
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
		problemJSON, _ := json.Marshal(jmaperror.NotJSON("Invalid JSON in request body").ToMap())
		return Response{
			StatusCode: 400,
			Headers:    map[string]string{"Content-Type": "application/problem+json"},
			Body:       string(problemJSON),
		}, nil
	}

	// Validate capabilities
	for _, cap := range jmapReq.Using {
		if !deps.Registry.HasCapability(cap) {
			problemJSON, _ := json.Marshal(jmaperror.UnknownCapability("Unknown capability: " + cap).ToMap())
			return Response{
				StatusCode: 400,
				Headers:    map[string]string{"Content-Type": "application/problem+json"},
				Body:       string(problemJSON),
			}, nil
		}
	}

	// Compute service URLs from env vars + request stage
	stage := request.RequestContext.Stage
	if stage == "" {
		stage = "v1"
	}
	cdnURL := fmt.Sprintf("https://%s/%s", os.Getenv("API_DOMAIN"), stage)
	apiURL := fmt.Sprintf("https://%s.execute-api.%s.amazonaws.com/%s", request.RequestContext.APIID, os.Getenv("AWS_REGION"), stage)

	// Process method calls in parallel with dependency tracking
	processor := &JMAPCallProcessor{
		AccountID: accountID,
		RequestID: request.RequestContext.RequestID,
		UsingCaps: jmapReq.Using,
		CDNURL:    cdnURL,
		APIURL:    apiURL,
		IsIAMAuth: isIAMAuthenticatedRequest(request),
	}

	cfg := dispatcher.Config{
		Calls:     jmapReq.MethodCalls,
		PoolSize:  deps.DispatcherPoolSize,
		Processor: processor,
	}

	methodResponses := dispatcher.Execute(ctx, cfg)

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

// JMAPCallProcessor implements dispatcher.CallProcessor for JMAP method calls
type JMAPCallProcessor struct {
	AccountID string
	RequestID string
	UsingCaps []string
	CDNURL    string
	APIURL    string
	IsIAMAuth bool
}

// Process implements dispatcher.CallProcessor
func (p *JMAPCallProcessor) Process(ctx context.Context, idx int, call []any, depResponses []resultref.MethodResponse) []any {
	return processMethodCall(ctx, p.AccountID, call, idx, p.RequestID, depResponses, p.UsingCaps, p.CDNURL, p.APIURL, p.IsIAMAuth)
}

// processMethodCall dispatches a method call to the appropriate plugin
func processMethodCall(ctx context.Context, accountID string, call []any, index int, requestID string, previousResponses []resultref.MethodResponse, usingCaps []string, cdnURL string, apiURL string, isIAMAuth bool) []any {
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
		return []any{"error", jmaperror.InvalidArguments("Method call must have exactly 3 elements: [name, args, clientId]").ToMap(), ""}
	}

	if methodName == "" {
		return []any{"error", jmaperror.InvalidArguments("Method name must be a string").ToMap(), ""}
	}

	args, ok := call[1].(map[string]any)
	if !ok {
		return []any{"error", jmaperror.InvalidArguments("Method arguments must be an object").ToMap(), methodName}
	}

	// Resolve result references (RFC 8620 Section 3.7)
	resolvedArgs, err := resultref.ResolveArgs(args, previousResponses)
	if err != nil {
		tracing.RecordError(span, err)
		resolveErr, ok := err.(*resultref.ResolveError)
		if ok {
			jmapErr := &jmaperror.MethodError{
				ErrType:     string(resolveErr.Type),
				Description: resolveErr.Description,
			}
			return []any{"error", jmapErr.ToMap(), clientID}
		}
		return []any{"error", jmaperror.ServerFail("Failed to resolve result references", err).ToMap(), clientID}
	}

	// Handle built-in methods before plugin dispatch
	if methodName == "Blob/allocate" {
		return handleBlobAllocate(ctx, accountID, resolvedArgs, clientID, usingCaps, isIAMAuth)
	}
	if methodName == "Blob/complete" {
		return handleBlobComplete(ctx, accountID, resolvedArgs, clientID, usingCaps)
	}

	// Look up method target
	target := deps.Registry.GetMethodTarget(methodName)
	if target == nil {
		return []any{"error", jmaperror.UnknownMethod("").ToMap(), clientID}
	}

	// Validate accountId in args matches authenticated accountId
	if argsAccountID, ok := resolvedArgs["accountId"].(string); ok {
		if argsAccountID != accountID {
			return []any{"error", jmaperror.AccountNotFound("Account ID mismatch").ToMap(), clientID}
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
		CDNURL:    cdnURL,
		APIURL:    apiURL,
	}

	// Invoke plugin
	pluginResp, err := deps.Invoker.Invoke(ctx, *target, pluginReq)
	if err != nil {
		tracing.RecordError(span, err)
		logger.ErrorContext(ctx, "Plugin invocation failed",
			slog.String("method", methodName),
			slog.String("error", err.Error()),
		)
		return []any{"error", jmaperror.ServerFail("Plugin invocation failed", err).ToMap(), clientID}
	}

	// Return plugin response as JMAP method response
	return []any{
		pluginResp.MethodResponse.Name,
		pluginResp.MethodResponse.Args,
		pluginResp.MethodResponse.ClientID,
	}
}

// handleBlobAllocate processes a Blob/allocate method call
func handleBlobAllocate(ctx context.Context, accountID string, args map[string]any, clientID string, usingCaps []string, isIAMAuth bool) []any {
	// Check if Blob/allocate is enabled
	if deps.BlobAllocator == nil {
		return []any{"error", jmaperror.UnknownMethod("").ToMap(), clientID}
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
		return []any{"error", jmaperror.UnknownMethod("Blob/allocate requires the " + UploadPutCapability + " capability").ToMap(), clientID}
	}

	// Validate accountId in args
	argsAccountID, _ := args["accountId"].(string)
	if argsAccountID != "" && argsAccountID != accountID {
		return []any{"error", jmaperror.AccountNotFound("Account ID mismatch").ToMap(), clientID}
	}

	// Extract the create map (per RFC 9404 pattern)
	createMap, ok := args["create"].(map[string]any)
	if !ok || len(createMap) == 0 {
		return []any{"error", jmaperror.InvalidArguments("create map is required").ToMap(), clientID}
	}

	created := make(map[string]any)
	notCreated := make(map[string]any)

	// Process each creation request
	for creationID, reqData := range createMap {
		reqMap, ok := reqData.(map[string]any)
		if !ok {
			notCreated[creationID] = jmaperror.InvalidArguments("invalid request format").ToMap()
			continue
		}

		contentType, _ := reqMap["type"].(string)
		size, _ := reqMap["size"].(float64) // JSON numbers come as float64
		multipart, _ := reqMap["multipart"].(bool)

		// Multipart is IAM-only
		if multipart && !isIAMAuth {
			notCreated[creationID] = (&jmaperror.SetError{
				ErrType:     "invalidArguments",
				Description: "multipart upload is only available via IAM authentication",
			}).ToMap()
			continue
		}

		req := bloballocate.AllocateRequest{
			AccountID:   accountID,
			Type:        contentType,
			Size:        int64(size),
			SizeUnknown: (isIAMAuth && int64(size) == 0) || multipart,
			Multipart:   multipart,
		}

		resp, err := deps.BlobAllocator.Allocate(ctx, req)
		if err != nil {
			allocErr, ok := err.(*bloballocate.AllocationError)
			if ok {
				setErr := &jmaperror.SetError{
					ErrType:     allocErr.Type,
					Description: allocErr.Message,
					Properties:  allocErr.Properties,
				}
				notCreated[creationID] = setErr.ToMap()
			} else {
				notCreated[creationID] = jmaperror.SetServerFail("Failed to allocate blob").ToMap()
			}
			continue
		}

		createdEntry := map[string]any{
			"id":      resp.BlobID,
			"type":    resp.Type,
			"size":    resp.Size,
			"expires": resp.URLExpires.UTC().Format("2006-01-02T15:04:05Z"),
		}
		if len(resp.Parts) > 0 {
			// Multipart response: include parts, no single URL
			partsOut := make([]map[string]any, len(resp.Parts))
			for i, p := range resp.Parts {
				partsOut[i] = map[string]any{
					"partNumber": p.PartNumber,
					"url":        p.URL,
				}
			}
			createdEntry["parts"] = partsOut
		} else {
			createdEntry["url"] = resp.URL
		}
		created[creationID] = createdEntry
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

// handleBlobComplete processes a Blob/complete method call
func handleBlobComplete(ctx context.Context, accountID string, args map[string]any, clientID string, usingCaps []string) []any {
	// Check if Blob/complete is enabled
	if deps.BlobCompleter == nil {
		return []any{"error", jmaperror.UnknownMethod("").ToMap(), clientID}
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
		return []any{"error", jmaperror.UnknownMethod("Blob/complete requires the " + UploadPutCapability + " capability").ToMap(), clientID}
	}

	// Validate accountId in args
	argsAccountID, _ := args["accountId"].(string)
	if argsAccountID != "" && argsAccountID != accountID {
		return []any{"error", jmaperror.AccountNotFound("Account ID mismatch").ToMap(), clientID}
	}

	// Extract blobId
	blobID, _ := args["id"].(string)
	if blobID == "" {
		return []any{"error", jmaperror.InvalidArguments("id is required").ToMap(), clientID}
	}

	// Extract parts array
	partsRaw, ok := args["parts"].([]any)
	if !ok || len(partsRaw) == 0 {
		return []any{"error", jmaperror.InvalidArguments("parts array is required and must not be empty").ToMap(), clientID}
	}

	parts := make([]bloballocate.CompletedPart, 0, len(partsRaw))
	for _, pRaw := range partsRaw {
		pMap, ok := pRaw.(map[string]any)
		if !ok {
			return []any{"error", jmaperror.InvalidArguments("each part must be an object with partNumber and etag").ToMap(), clientID}
		}
		partNum, _ := pMap["partNumber"].(float64)
		etag, _ := pMap["etag"].(string)
		if partNum <= 0 || etag == "" {
			return []any{"error", jmaperror.InvalidArguments("each part must have a positive partNumber and non-empty etag").ToMap(), clientID}
		}
		parts = append(parts, bloballocate.CompletedPart{
			PartNumber: int32(partNum),
			ETag:       etag,
		})
	}

	req := blobcomplete.CompleteRequest{
		AccountID: accountID,
		BlobID:    blobID,
		Parts:     parts,
	}

	resp, err := deps.BlobCompleter.Complete(ctx, req)
	if err != nil {
		compErr, ok := err.(*blobcomplete.CompleteError)
		if ok {
			return []any{"error", (&jmaperror.MethodError{
				ErrType:     compErr.Type,
				Description: compErr.Message,
			}).ToMap(), clientID}
		}
		return []any{"error", jmaperror.ServerFail("Failed to complete multipart upload", err).ToMap(), clientID}
	}

	response := map[string]any{
		"accountId": resp.AccountID,
		"id":        resp.BlobID,
	}

	return []any{"Blob/complete", response, clientID}
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

	result, err := awsinit.Init(ctx, awsinit.WithHTTPHandler("jmap-api"))
	if err != nil {
		logger.Error("FATAL: Failed to initialize AWS",
			slog.String("error", err.Error()),
		)
		panic(err)
	}
	defer result.Cleanup()

	// Initialize DynamoDB client
	tableName := os.Getenv("DYNAMODB_TABLE")
	if tableName == "" {
		logger.Error("FATAL: DYNAMODB_TABLE environment variable is required")
		panic("DYNAMODB_TABLE environment variable is required")
	}
	dbClient := db.NewClientFromConfig(result.Config, tableName)

	// Load plugin registry
	registry := plugin.NewRegistry()
	if err := registry.LoadFromDynamoDB(result.Ctx, dbClient); err != nil {
		logger.Error("FATAL: Failed to load plugin registry",
			slog.String("error", err.Error()),
		)
		panic(err)
	}

	// Initialize Lambda invoker
	lambdaClient := lambdasvc.NewFromConfig(result.Config)
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
		s3Client := s3.NewFromConfig(result.Config)
		presignClient := s3.NewPresignClient(s3Client)

		// Initialize DynamoDB client for blob allocations
		ddbClient := dynamodb.NewFromConfig(result.Config)

		s3Storage := bloballocate.NewS3Storage(presignClient, blobBucket, s3Client)

		blobAllocator = &bloballocate.Handler{
			Storage:          s3Storage,
			MultipartStorage: s3Storage,
			DB:               bloballocate.NewDynamoDBStore(ddbClient, tableName),
			UUIDGen:          &RealUUIDGenerator{},
			MaxSizeUploadPut: maxSizeUploadPut,
			MaxPendingAllocs: maxPendingAllocs,
			URLExpirySecs:    urlExpirySecs,
		}
	}

	// Configure dispatcher pool size
	dispatcherPoolSize := DefaultDispatcherPoolSize
	if poolSizeStr := os.Getenv("JMAP_DISPATCHER_PARALLELISM"); poolSizeStr != "" {
		if parsed, err := strconv.Atoi(poolSizeStr); err == nil && parsed > 0 {
			dispatcherPoolSize = parsed
		}
	}

	// Initialize Blob/complete handler (reuses same S3 and DynamoDB clients)
	var blobCompleter *blobcomplete.Handler
	if blobBucket != "" {
		s3Client := s3.NewFromConfig(result.Config)
		ddbClient := dynamodb.NewFromConfig(result.Config)
		s3Storage := bloballocate.NewS3Storage(s3.NewPresignClient(s3Client), blobBucket, s3Client)
		blobCompleter = &blobcomplete.Handler{
			Storage: s3Storage,
			DB:      blobcomplete.NewDynamoDBStore(ddbClient, tableName),
		}
	}

	deps = &Dependencies{
		Registry:           registry,
		Invoker:            invoker,
		BlobAllocator:      blobAllocator,
		BlobCompleter:      blobCompleter,
		DispatcherPoolSize: dispatcherPoolSize,
	}

	result.Start(handler)
}
