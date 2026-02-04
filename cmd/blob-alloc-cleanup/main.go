package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
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

// PendingAllocation represents an expired pending allocation record
type PendingAllocation struct {
	AccountID string
	BlobID    string
	S3Key     string
	Size      int64
}

// CleanupStorage handles S3 operations for cleanup
type CleanupStorage interface {
	DeleteObject(ctx context.Context, key string) error
}

// CleanupDB handles DynamoDB operations for cleanup
type CleanupDB interface {
	GetExpiredPendingAllocations(ctx context.Context, cutoff time.Time) ([]PendingAllocation, error)
	CleanupAllocation(ctx context.Context, accountID, blobID string, size int64) error
}

// Dependencies for handler (injectable for testing)
type Dependencies struct {
	Storage     CleanupStorage
	DB          CleanupDB
	BufferHours int
}

var deps *Dependencies

// handler processes scheduled cleanup events
func handler(ctx context.Context) error {
	// Calculate cutoff time (url expiry + buffer)
	cutoff := time.Now().Add(-time.Duration(deps.BufferHours) * time.Hour)

	logger.InfoContext(ctx, "Starting blob allocation cleanup",
		slog.Time("cutoff", cutoff),
		slog.Int("buffer_hours", deps.BufferHours),
	)

	// Query for expired pending allocations
	allocations, err := deps.DB.GetExpiredPendingAllocations(ctx, cutoff)
	if err != nil {
		logger.ErrorContext(ctx, "Failed to query expired allocations",
			slog.String("error", err.Error()),
		)
		return fmt.Errorf("failed to query expired allocations: %w", err)
	}

	logger.InfoContext(ctx, "Found expired allocations",
		slog.Int("count", len(allocations)),
	)

	// Process each expired allocation
	cleanedCount := 0
	errorCount := 0
	for _, alloc := range allocations {
		// Delete S3 object first (idempotent - already gone is success)
		if err := deps.Storage.DeleteObject(ctx, alloc.S3Key); err != nil {
			logger.ErrorContext(ctx, "Failed to delete S3 object",
				slog.String("account_id", alloc.AccountID),
				slog.String("blob_id", alloc.BlobID),
				slog.String("s3_key", alloc.S3Key),
				slog.String("error", err.Error()),
			)
			errorCount++
			continue // Don't clean up DynamoDB if S3 delete failed
		}

		// Clean up DynamoDB (delete blob record, restore quota)
		if err := deps.DB.CleanupAllocation(ctx, alloc.AccountID, alloc.BlobID, alloc.Size); err != nil {
			logger.ErrorContext(ctx, "Failed to cleanup DynamoDB record",
				slog.String("account_id", alloc.AccountID),
				slog.String("blob_id", alloc.BlobID),
				slog.String("error", err.Error()),
			)
			errorCount++
			continue
		}

		cleanedCount++
		logger.InfoContext(ctx, "Cleaned up expired allocation",
			slog.String("account_id", alloc.AccountID),
			slog.String("blob_id", alloc.BlobID),
		)
	}

	logger.InfoContext(ctx, "Blob allocation cleanup completed",
		slog.Int("total", len(allocations)),
		slog.Int("cleaned", cleanedCount),
		slog.Int("errors", errorCount),
	)

	return nil
}

// S3CleanupStorage implements CleanupStorage using AWS S3
type S3CleanupStorage struct {
	client     *s3.Client
	bucketName string
}

// NewS3CleanupStorage creates a new S3CleanupStorage
func NewS3CleanupStorage(client *s3.Client, bucketName string) *S3CleanupStorage {
	return &S3CleanupStorage{
		client:     client,
		bucketName: bucketName,
	}
}

// DeleteObject deletes an S3 object
func (s *S3CleanupStorage) DeleteObject(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucketName),
		Key:    aws.String(key),
	})
	return err
}

// DynamoDBCleanupStore implements CleanupDB using AWS DynamoDB
type DynamoDBCleanupStore struct {
	client    *dynamodb.Client
	tableName string
}

// NewDynamoDBCleanupStore creates a new DynamoDBCleanupStore
func NewDynamoDBCleanupStore(client *dynamodb.Client, tableName string) *DynamoDBCleanupStore {
	return &DynamoDBCleanupStore{
		client:    client,
		tableName: tableName,
	}
}

// GetExpiredPendingAllocations queries the GSI for expired pending allocations
func (d *DynamoDBCleanupStore) GetExpiredPendingAllocations(ctx context.Context, cutoff time.Time) ([]PendingAllocation, error) {
	// Build cutoff string for GSI query
	// GSI1SK format: EXPIRES#{urlExpiresAt}#{accountId}#{blobId}
	cutoffStr := fmt.Sprintf("EXPIRES#%s#", cutoff.UTC().Format(time.RFC3339))

	result, err := d.client.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(d.tableName),
		IndexName:              aws.String("gsi1"),
		KeyConditionExpression: aws.String("gsi1pk = :pending AND gsi1sk < :cutoff"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pending": &types.AttributeValueMemberS{Value: "PENDING"},
			":cutoff":  &types.AttributeValueMemberS{Value: cutoffStr},
		},
	})
	if err != nil {
		return nil, err
	}

	allocations := make([]PendingAllocation, 0, len(result.Items))
	for _, item := range result.Items {
		alloc := PendingAllocation{}

		// Extract accountId from pk (ACCOUNT#{accountId})
		if pkAttr, ok := item["pk"].(*types.AttributeValueMemberS); ok {
			if len(pkAttr.Value) > 8 {
				alloc.AccountID = pkAttr.Value[8:] // Skip "ACCOUNT#"
			}
		}

		// Extract blobId from sk (BLOB#{blobId})
		if skAttr, ok := item["sk"].(*types.AttributeValueMemberS); ok {
			if len(skAttr.Value) > 5 {
				alloc.BlobID = skAttr.Value[5:] // Skip "BLOB#"
			}
		}

		// Extract s3Key
		if s3KeyAttr, ok := item["s3Key"].(*types.AttributeValueMemberS); ok {
			alloc.S3Key = s3KeyAttr.Value
		}

		// Extract size
		if sizeAttr, ok := item["size"].(*types.AttributeValueMemberN); ok {
			alloc.Size, _ = strconv.ParseInt(sizeAttr.Value, 10, 64)
		}

		if alloc.AccountID != "" && alloc.BlobID != "" {
			allocations = append(allocations, alloc)
		}
	}

	return allocations, nil
}

// CleanupAllocation deletes the blob record and restores quota atomically
func (d *DynamoDBCleanupStore) CleanupAllocation(ctx context.Context, accountID, blobID string, size int64) error {
	now := time.Now().UTC().Format(time.RFC3339)

	_, err := d.client.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{
		TransactItems: []types.TransactWriteItem{
			{
				Delete: &types.Delete{
					TableName: aws.String(d.tableName),
					Key: map[string]types.AttributeValue{
						"pk": &types.AttributeValueMemberS{Value: fmt.Sprintf("ACCOUNT#%s", accountID)},
						"sk": &types.AttributeValueMemberS{Value: fmt.Sprintf("BLOB#%s", blobID)},
					},
					// Only delete if still pending
					ConditionExpression: aws.String("#status = :pending"),
					ExpressionAttributeNames: map[string]string{
						"#status": "status",
					},
					ExpressionAttributeValues: map[string]types.AttributeValue{
						":pending": &types.AttributeValueMemberS{Value: "pending"},
					},
				},
			},
			{
				Update: &types.Update{
					TableName: aws.String(d.tableName),
					Key: map[string]types.AttributeValue{
						"pk": &types.AttributeValueMemberS{Value: fmt.Sprintf("ACCOUNT#%s", accountID)},
						"sk": &types.AttributeValueMemberS{Value: "META#"},
					},
					UpdateExpression: aws.String(
						"ADD pendingAllocationsCount :negOne, quotaRemaining :size SET updatedAt = :now"),
					ExpressionAttributeValues: map[string]types.AttributeValue{
						":negOne": &types.AttributeValueMemberN{Value: "-1"},
						":size":   &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", size)},
						":now":    &types.AttributeValueMemberS{Value: now},
					},
				},
			},
		},
	})

	return err
}

func main() {
	ctx := context.Background()

	// Initialize tracer provider
	tp, err := tracing.Init(ctx)
	if err != nil {
		logger.Error("FATAL: Failed to initialize tracer provider",
			slog.String("error", err.Error()),
		)
		panic(err)
	}
	otel.SetTracerProvider(tp)

	// Create cold start span - all init AWS calls become children
	ctx, coldStartSpan := tracing.StartColdStartSpan(ctx, "blob-alloc-cleanup")
	defer coldStartSpan.End()

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

	bufferHours, _ := strconv.Atoi(os.Getenv("CLEANUP_BUFFER_HOURS"))
	if bufferHours == 0 {
		bufferHours = 72 // Default 3 days
	}

	// Initialize AWS clients
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		logger.Error("FATAL: Failed to load AWS config",
			slog.String("error", err.Error()),
		)
		panic(err)
	}
	otelaws.AppendMiddlewares(&cfg.APIOptions)

	s3Client := s3.NewFromConfig(cfg)
	dynamoClient := dynamodb.NewFromConfig(cfg)

	deps = &Dependencies{
		Storage:     NewS3CleanupStorage(s3Client, bucketName),
		DB:          NewDynamoDBCleanupStore(dynamoClient, tableName),
		BufferHours: bufferHours,
	}

	lambda.Start(otellambda.InstrumentHandler(handler, xrayconfig.WithRecommendedOptions(tp)...))
}
