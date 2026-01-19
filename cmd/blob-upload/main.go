package main

import (
	"bytes"
	"context"
	"encoding/base64"
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
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/google/uuid"
)

var (
	logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
)

// BlobStorage handles S3 operations
type BlobStorage interface {
	Upload(ctx context.Context, req UploadRequest) error
	ConfirmUpload(ctx context.Context, accountID, blobID string) error
}

// BlobDB handles DynamoDB operations
type BlobDB interface {
	CreateBlobRecord(ctx context.Context, record BlobRecord) error
}

// UUIDGenerator generates unique IDs
type UUIDGenerator interface {
	Generate() string
}

// UploadRequest represents an S3 upload request
type UploadRequest struct {
	Key         string
	Body        []byte
	ContentType string
	AccountID   string
}

// BlobRecord represents a blob record in DynamoDB
type BlobRecord struct {
	BlobID      string
	AccountID   string
	Size        int64
	ContentType string
	S3Key       string
	CreatedAt   string
}

// BlobUploadResponse is the RFC 8620 blob upload response
type BlobUploadResponse struct {
	AccountID string `json:"accountId"`
	BlobID    string `json:"blobId"`
	Type      string `json:"type"`
	Size      int64  `json:"size"`
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
	Storage BlobStorage
	DB      BlobDB
	UUIDGen UUIDGenerator
}

var deps *Dependencies

// handler processes blob upload requests
func handler(ctx context.Context, request events.APIGatewayProxyRequest) (Response, error) {
	// Extract accountId
	accountID, err := extractAccountID(request)
	if err != nil {
		logger.WarnContext(ctx, "Failed to extract account ID",
			slog.String("request_id", request.RequestContext.RequestID),
			slog.String("error", err.Error()),
		)
		return errorResponse(401, "unauthorized", "Missing or invalid authentication")
	}

	// Validate Content-Type header
	contentType := getContentType(request.Headers)
	if contentType == "" {
		logger.WarnContext(ctx, "Missing Content-Type header",
			slog.String("request_id", request.RequestContext.RequestID),
		)
		return errorResponse(400, "invalidArguments", "Content-Type header is required")
	}

	// Decode body
	body, err := decodeBody(request)
	if err != nil {
		logger.WarnContext(ctx, "Failed to decode body",
			slog.String("request_id", request.RequestContext.RequestID),
			slog.String("error", err.Error()),
		)
		return errorResponse(400, "invalidArguments", "Invalid request body")
	}

	// Generate blobId
	blobID := deps.UUIDGen.Generate()
	s3Key := fmt.Sprintf("%s/%s", accountID, blobID)

	// Upload to S3 with pending status
	uploadReq := UploadRequest{
		Key:         s3Key,
		Body:        body,
		ContentType: contentType,
		AccountID:   accountID,
	}
	if err := deps.Storage.Upload(ctx, uploadReq); err != nil {
		logger.ErrorContext(ctx, "Failed to upload to S3",
			slog.String("request_id", request.RequestContext.RequestID),
			slog.String("error", err.Error()),
		)
		return errorResponse(500, "serverFail", "Failed to store blob")
	}

	// Create DynamoDB record
	record := BlobRecord{
		BlobID:      blobID,
		AccountID:   accountID,
		Size:        int64(len(body)),
		ContentType: contentType,
		S3Key:       s3Key,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	if err := deps.DB.CreateBlobRecord(ctx, record); err != nil {
		logger.ErrorContext(ctx, "Failed to create DynamoDB record",
			slog.String("request_id", request.RequestContext.RequestID),
			slog.String("error", err.Error()),
		)
		return errorResponse(500, "serverFail", "Failed to record blob metadata")
	}

	// Confirm upload (update S3 tag to confirmed)
	if err := deps.Storage.ConfirmUpload(ctx, accountID, blobID); err != nil {
		logger.ErrorContext(ctx, "Failed to confirm upload",
			slog.String("request_id", request.RequestContext.RequestID),
			slog.String("error", err.Error()),
		)
		// Note: We don't return error here - the blob is uploaded and recorded
		// The lifecycle policy will handle cleanup if needed
	}

	// Build success response
	response := BlobUploadResponse{
		AccountID: accountID,
		BlobID:    blobID,
		Type:      contentType,
		Size:      int64(len(body)),
	}

	responseBody, err := json.Marshal(response)
	if err != nil {
		logger.ErrorContext(ctx, "Failed to marshal response",
			slog.String("request_id", request.RequestContext.RequestID),
			slog.String("error", err.Error()),
		)
		return errorResponse(500, "serverFail", "Failed to build response")
	}

	logger.InfoContext(ctx, "Blob upload completed",
		slog.String("request_id", request.RequestContext.RequestID),
		slog.String("account_id", accountID),
		slog.String("blob_id", blobID),
		slog.Int64("size", int64(len(body))),
	)

	return Response{
		StatusCode: 201,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       string(responseBody),
	}, nil
}

// extractAccountID extracts account ID from path parameter or JWT claims
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

// getContentType extracts Content-Type from headers (case-insensitive)
func getContentType(headers map[string]string) string {
	for k, v := range headers {
		if k == "Content-Type" || k == "content-type" {
			return v
		}
	}
	return ""
}

// decodeBody decodes the request body (handles base64 encoding)
func decodeBody(request events.APIGatewayProxyRequest) ([]byte, error) {
	if request.IsBase64Encoded {
		return base64.StdEncoding.DecodeString(request.Body)
	}
	return []byte(request.Body), nil
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

// S3BlobStorage implements BlobStorage using AWS S3
type S3BlobStorage struct {
	client     *s3.Client
	bucketName string
}

// NewS3BlobStorage creates a new S3BlobStorage
func NewS3BlobStorage(client *s3.Client, bucketName string) *S3BlobStorage {
	return &S3BlobStorage{
		client:     client,
		bucketName: bucketName,
	}
}

// Upload uploads a blob to S3 with pending status tag
func (s *S3BlobStorage) Upload(ctx context.Context, req UploadRequest) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucketName),
		Key:         aws.String(req.Key),
		Body:        bytes.NewReader(req.Body),
		ContentType: aws.String(req.ContentType),
		Tagging:     aws.String(fmt.Sprintf("Account=%s&Status=pending", req.AccountID)),
	})
	return err
}

// ConfirmUpload updates the S3 object tag to confirmed
func (s *S3BlobStorage) ConfirmUpload(ctx context.Context, accountID, blobID string) error {
	key := fmt.Sprintf("%s/%s", accountID, blobID)
	_, err := s.client.PutObjectTagging(ctx, &s3.PutObjectTaggingInput{
		Bucket: aws.String(s.bucketName),
		Key:    aws.String(key),
		Tagging: &types.Tagging{
			TagSet: []types.Tag{
				{Key: aws.String("Account"), Value: aws.String(accountID)},
				{Key: aws.String("Status"), Value: aws.String("confirmed")},
			},
		},
	})
	return err
}

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

// CreateBlobRecord creates a blob record in DynamoDB
func (d *DynamoDBBlobDB) CreateBlobRecord(ctx context.Context, record BlobRecord) error {
	item := map[string]any{
		"pk":          fmt.Sprintf("ACCOUNT#%s", record.AccountID),
		"sk":          fmt.Sprintf("BLOB#%s", record.BlobID),
		"blobId":      record.BlobID,
		"accountId":   record.AccountID,
		"size":        record.Size,
		"contentType": record.ContentType,
		"s3Key":       record.S3Key,
		"createdAt":   record.CreatedAt,
	}

	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return err
	}

	_, err = d.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(d.tableName),
		Item:      av,
	})
	return err
}

// RealUUIDGenerator generates real UUIDs
type RealUUIDGenerator struct{}

// Generate generates a new UUID v4
func (r *RealUUIDGenerator) Generate() string {
	return uuid.New().String()
}

func main() {
	ctx := context.Background()

	// Get required environment variables
	tableName := os.Getenv("DYNAMODB_TABLE")
	if tableName == "" {
		logger.Error("FATAL: DYNAMODB_TABLE environment variable is required")
		panic("DYNAMODB_TABLE environment variable is required")
	}

	bucketName := os.Getenv("BLOB_BUCKET")
	if bucketName == "" {
		logger.Error("FATAL: BLOB_BUCKET environment variable is required")
		panic("BLOB_BUCKET environment variable is required")
	}

	// Initialize AWS clients
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		logger.Error("FATAL: Failed to load AWS config",
			slog.String("error", err.Error()),
		)
		panic(err)
	}

	s3Client := s3.NewFromConfig(cfg)
	dynamoClient := dynamodb.NewFromConfig(cfg)

	deps = &Dependencies{
		Storage: NewS3BlobStorage(s3Client, bucketName),
		DB:      NewDynamoDBBlobDB(dynamoClient, tableName),
		UUIDGen: &RealUUIDGenerator{},
	}

	lambda.Start(handler)
}
