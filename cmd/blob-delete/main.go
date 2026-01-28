package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/jarrod-lowe/jmap-service-core/internal/db"
	"github.com/jarrod-lowe/jmap-service-core/internal/plugin"
)

var (
	logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
)

// BlobRecord represents a blob record from DynamoDB
type BlobRecord struct {
	BlobID      string `dynamodbav:"blobId"`
	AccountID   string `dynamodbav:"accountId"`
	Size        int64  `dynamodbav:"size"`
	ContentType string `dynamodbav:"contentType"`
	S3Key       string `dynamodbav:"s3Key"`
	CreatedAt   string `dynamodbav:"createdAt"`
	DeletedAt   string `dynamodbav:"deletedAt,omitempty"`
}

// BlobDB handles DynamoDB operations for blob metadata
type BlobDB interface {
	GetBlob(ctx context.Context, accountID, blobID string) (*BlobRecord, error)
	MarkBlobDeleted(ctx context.Context, accountID, blobID string, deletedAt string) error
}

// PrincipalChecker checks if a caller is allowed to access IAM endpoints
type PrincipalChecker interface {
	IsAllowedPrincipal(callerARN string) bool
}

// ErrorResponse is the error response format
type ErrorResponse struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

// Response is the API Gateway proxy response
type Response struct {
	StatusCode int               `json:"statusCode"`
	Headers    map[string]string `json:"headers"`
	Body       string            `json:"body"`
}

// Dependencies for handler (injectable for testing)
type Dependencies struct {
	DB       BlobDB
	Registry PrincipalChecker
}

var deps *Dependencies

// handler processes blob delete requests
func handler(ctx context.Context, request events.APIGatewayProxyRequest) (Response, error) {
	// Extract accountId from path
	pathAccountID := request.PathParameters["accountId"]
	if pathAccountID == "" {
		return errorResponse(400, "invalidArguments", "Missing accountId in path")
	}

	// Extract blobId from path
	blobID := request.PathParameters["blobId"]
	if blobID == "" {
		return errorResponse(400, "invalidArguments", "Missing blobId in path")
	}

	// Extract authenticated account ID (from JWT or path for IAM)
	authAccountID, err := extractAccountID(request)
	if err != nil {
		logger.WarnContext(ctx, "Failed to extract account ID",
			slog.String("request_id", request.RequestContext.RequestID),
			slog.String("error", err.Error()),
		)
		return errorResponse(401, "unauthorized", "Missing or invalid authentication")
	}

	// Check principal authorization for IAM-authenticated requests
	if isIAMAuthenticatedRequest(request) {
		callerPrincipal := extractCallerPrincipal(request)
		if !deps.Registry.IsAllowedPrincipal(callerPrincipal) {
			logger.WarnContext(ctx, "Unauthorized IAM principal",
				slog.String("request_id", request.RequestContext.RequestID),
				slog.String("caller_principal", callerPrincipal),
			)
			return errorResponse(403, "forbidden", "Principal not authorized for IAM access")
		}
	}

	// Validate path accountId matches authenticated accountId
	if pathAccountID != authAccountID {
		logger.WarnContext(ctx, "Account ID mismatch",
			slog.String("request_id", request.RequestContext.RequestID),
			slog.String("path_account_id", pathAccountID),
			slog.String("auth_account_id", authAccountID),
		)
		return errorResponse(403, "forbidden", "Account ID mismatch")
	}

	// Look up blob in DynamoDB
	blob, err := deps.DB.GetBlob(ctx, pathAccountID, blobID)
	if err != nil {
		logger.ErrorContext(ctx, "Failed to get blob from DynamoDB",
			slog.String("request_id", request.RequestContext.RequestID),
			slog.String("error", err.Error()),
		)
		return errorResponse(500, "serverFail", "Failed to retrieve blob metadata")
	}

	// Check if blob exists
	if blob == nil {
		logger.InfoContext(ctx, "Blob not found",
			slog.String("request_id", request.RequestContext.RequestID),
			slog.String("account_id", pathAccountID),
			slog.String("blob_id", blobID),
		)
		return errorResponse(404, "notFound", "Blob not found")
	}

	// Verify blob ownership (defense in depth)
	if blob.AccountID != pathAccountID {
		logger.WarnContext(ctx, "Blob ownership mismatch",
			slog.String("request_id", request.RequestContext.RequestID),
			slog.String("blob_account_id", blob.AccountID),
			slog.String("request_account_id", pathAccountID),
		)
		return errorResponse(404, "notFound", "Blob not found")
	}

	// Check if already deleted
	if blob.DeletedAt != "" {
		return errorResponse(404, "notFound", "Blob not found")
	}

	// Mark blob as deleted
	deletedAt := time.Now().UTC().Format(time.RFC3339)
	if err := deps.DB.MarkBlobDeleted(ctx, pathAccountID, blobID, deletedAt); err != nil {
		logger.ErrorContext(ctx, "Failed to mark blob as deleted",
			slog.String("request_id", request.RequestContext.RequestID),
			slog.String("error", err.Error()),
		)
		return errorResponse(500, "serverFail", "Failed to delete blob")
	}

	logger.InfoContext(ctx, "Blob marked as deleted",
		slog.String("request_id", request.RequestContext.RequestID),
		slog.String("account_id", pathAccountID),
		slog.String("blob_id", blobID),
	)

	return Response{
		StatusCode: 204,
		Headers:    map[string]string{},
		Body:       "",
	}, nil
}

// extractAccountID extracts account ID using authoritative API Gateway signals.
// - IAM auth: Identity.UserArn or Identity.Caller is populated → use path param
// - Cognito auth: Authorizer["claims"]["sub"] is populated → use JWT sub claim
// These fields are populated by API Gateway and cannot be spoofed by clients.
func extractAccountID(request events.APIGatewayProxyRequest) (string, error) {
	identity := request.RequestContext.Identity

	// IAM auth: API Gateway populates Identity.UserArn and/or Identity.Caller
	// These fields cannot be spoofed by the client
	if identity.UserArn != "" || identity.Caller != "" {
		accountID, ok := request.PathParameters["accountId"]
		if !ok || accountID == "" {
			return "", fmt.Errorf("missing accountId path parameter for IAM auth")
		}
		return accountID, nil
	}

	// Cognito auth: API Gateway populates Authorizer with claims
	authorizer := request.RequestContext.Authorizer
	if authorizer == nil {
		return "", fmt.Errorf("no authentication context (neither IAM nor Cognito)")
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

// isIAMAuthenticatedRequest checks if the request is IAM-authenticated
// by checking if UserArn is populated in the request context
func isIAMAuthenticatedRequest(request events.APIGatewayProxyRequest) bool {
	return request.RequestContext.Identity.UserArn != ""
}

// extractCallerPrincipal extracts the caller's IAM principal ARN from the request
func extractCallerPrincipal(request events.APIGatewayProxyRequest) string {
	return request.RequestContext.Identity.UserArn
}

// errorResponse builds an error response
func errorResponse(statusCode int, errorType, description string) (Response, error) {
	body, _ := json.Marshal(ErrorResponse{Type: errorType, Description: description})
	return Response{
		StatusCode: statusCode,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       string(body),
	}, nil
}

// =============================================================================
// Real implementations
// =============================================================================

// DynamoDBBlobDB implements BlobDB using AWS DynamoDB
type DynamoDBBlobDB struct {
	client    *dynamodb.Client
	tableName string
}

// NewDynamoDBBlobDB creates a new DynamoDBBlobDB
func NewDynamoDBBlobDB(client *dynamodb.Client, tableName string) *DynamoDBBlobDB {
	return &DynamoDBBlobDB{
		client:    client,
		tableName: tableName,
	}
}

// GetBlob retrieves a blob record from DynamoDB
func (d *DynamoDBBlobDB) GetBlob(ctx context.Context, accountID, blobID string) (*BlobRecord, error) {
	result, err := d.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(d.tableName),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: fmt.Sprintf("ACCOUNT#%s", accountID)},
			"sk": &types.AttributeValueMemberS{Value: fmt.Sprintf("BLOB#%s", blobID)},
		},
	})
	if err != nil {
		return nil, err
	}

	if result.Item == nil {
		return nil, nil
	}

	var record BlobRecord
	if err := attributevalue.UnmarshalMap(result.Item, &record); err != nil {
		return nil, err
	}

	return &record, nil
}

// MarkBlobDeleted sets the deletedAt attribute on a blob record
func (d *DynamoDBBlobDB) MarkBlobDeleted(ctx context.Context, accountID, blobID string, deletedAt string) error {
	_, err := d.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(d.tableName),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: fmt.Sprintf("ACCOUNT#%s", accountID)},
			"sk": &types.AttributeValueMemberS{Value: fmt.Sprintf("BLOB#%s", blobID)},
		},
		UpdateExpression: aws.String("SET #deletedAt = :deletedAt"),
		ExpressionAttributeNames: map[string]string{
			"#deletedAt": "deletedAt",
		},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":deletedAt": &types.AttributeValueMemberS{Value: deletedAt},
		},
	})
	return err
}

func main() {
	ctx := context.Background()

	// Get required environment variables
	tableName := os.Getenv("DYNAMODB_TABLE")
	if tableName == "" {
		logger.Error("FATAL: DYNAMODB_TABLE environment variable is required")
		panic("DYNAMODB_TABLE environment variable is required")
	}

	// Initialize AWS clients
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		logger.Error("FATAL: Failed to load AWS config",
			slog.String("error", err.Error()),
		)
		panic(err)
	}

	dynamoClient := dynamodb.NewFromConfig(cfg)

	// Initialize database client for plugin registry
	dbClient, err := db.NewClient(ctx, tableName)
	if err != nil {
		logger.Error("FATAL: Failed to initialize DynamoDB client for plugin registry",
			slog.String("error", err.Error()),
		)
		panic(err)
	}

	// Load plugin registry for IAM principal authorization
	registry := plugin.NewRegistry()
	if err := registry.LoadFromDynamoDB(ctx, dbClient); err != nil {
		logger.Error("FATAL: Failed to load plugin registry",
			slog.String("error", err.Error()),
		)
		panic(err)
	}

	deps = &Dependencies{
		DB:       NewDynamoDBBlobDB(dynamoClient, tableName),
		Registry: registry,
	}

	lambda.Start(handler)
}
