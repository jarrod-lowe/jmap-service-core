package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

var (
	logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
)

// ConfirmStorage handles S3 operations for blob confirmation
type ConfirmStorage interface {
	ConfirmTag(ctx context.Context, key string) error
	DeleteObject(ctx context.Context, key string) error
}

// ConfirmDB handles DynamoDB operations for blob confirmation
type ConfirmDB interface {
	GetBlobStatus(ctx context.Context, accountID, blobID string) (string, error)
	ConfirmBlob(ctx context.Context, accountID, blobID string) error
}

// Dependencies for handler (injectable for testing)
type Dependencies struct {
	Storage ConfirmStorage
	DB      ConfirmDB
}

var deps *Dependencies

// handler processes S3 ObjectCreated events to confirm blob uploads
func handler(ctx context.Context, event events.S3Event) error {
	for _, record := range event.Records {
		key := record.S3.Object.Key
		logger.InfoContext(ctx, "Processing S3 event",
			slog.String("bucket", record.S3.Bucket.Name),
			slog.String("key", key),
		)

		// Parse key to get accountID and blobID
		accountID, blobID, err := parseS3Key(key)
		if err != nil {
			logger.ErrorContext(ctx, "Invalid S3 key format",
				slog.String("key", key),
				slog.String("error", err.Error()),
			)
			return fmt.Errorf("invalid S3 key format: %w", err)
		}

		// Check blob record status
		status, err := deps.DB.GetBlobStatus(ctx, accountID, blobID)
		if err != nil {
			logger.ErrorContext(ctx, "Failed to get blob status",
				slog.String("account_id", accountID),
				slog.String("blob_id", blobID),
				slog.String("error", err.Error()),
			)
			return fmt.Errorf("failed to get blob status: %w", err)
		}

		// If record not found, skip - this object may be from a different upload path
		// (e.g., traditional /upload/ endpoint) or the record may have been cleaned up.
		// The blob-alloc-cleanup Lambda handles expired pending allocations.
		if status == "" {
			logger.WarnContext(ctx, "Blob record not found, skipping (may be traditional upload)",
				slog.String("account_id", accountID),
				slog.String("blob_id", blobID),
			)
			continue
		}

		// If already confirmed, skip (idempotent)
		if status == "confirmed" {
			logger.InfoContext(ctx, "Blob already confirmed, skipping",
				slog.String("account_id", accountID),
				slog.String("blob_id", blobID),
			)
			continue
		}

		// IMPORTANT: Operation order is intentional for data safety.
		//
		// 1. S3 tag update FIRST: Protects blob from lifecycle deletion. If this
		//    fails, we return an error and retry - the blob remains "pending" but
		//    safe (lifecycle only expires untagged pending blobs after 7 days).
		//
		// 2. DynamoDB confirmation SECOND: Updates status and quota. If this fails
		//    after S3 succeeds:
		//    - The blob is already protected in S3 (tagged as confirmed)
		//    - Lambda retry will succeed (DynamoDB uses conditional writes for idempotency)
		//    - No data loss occurs
		//
		// The reverse order would risk: DynamoDB confirms → S3 tag fails → lifecycle
		// deletes the blob before retry → data loss.
		//
		// On persistent failure: After Lambda retries are exhausted, the S3 event goes
		// to the DLQ (blob_confirm_dlq) and triggers a CloudWatch alarm for investigation.
		if err := deps.Storage.ConfirmTag(ctx, key); err != nil {
			logger.ErrorContext(ctx, "Failed to update S3 tag",
				slog.String("key", key),
				slog.String("error", err.Error()),
			)
			return fmt.Errorf("failed to update S3 tag: %w", err)
		}

		// Confirm blob in DynamoDB (update status, remove GSI keys, decrement pending count)
		if err := deps.DB.ConfirmBlob(ctx, accountID, blobID); err != nil {
			logger.ErrorContext(ctx, "Failed to confirm blob in DynamoDB",
				slog.String("account_id", accountID),
				slog.String("blob_id", blobID),
				slog.String("error", err.Error()),
			)
			return fmt.Errorf("failed to confirm blob: %w", err)
		}

		logger.InfoContext(ctx, "Blob confirmed successfully",
			slog.String("account_id", accountID),
			slog.String("blob_id", blobID),
		)
	}

	return nil
}

// parseS3Key extracts accountID and blobID from S3 key (format: {accountId}/{blobId})
func parseS3Key(key string) (accountID, blobID string, err error) {
	parts := strings.SplitN(key, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid key format: expected {accountId}/{blobId}")
	}
	return parts[0], parts[1], nil
}

// S3ConfirmStorage implements ConfirmStorage using AWS S3
type S3ConfirmStorage struct {
	client     *s3.Client
	bucketName string
}

// NewS3ConfirmStorage creates a new S3ConfirmStorage
func NewS3ConfirmStorage(client *s3.Client, bucketName string) *S3ConfirmStorage {
	return &S3ConfirmStorage{
		client:     client,
		bucketName: bucketName,
	}
}

// ConfirmTag updates the S3 object tag to confirmed
func (s *S3ConfirmStorage) ConfirmTag(ctx context.Context, key string) error {
	// Parse key to get accountID for tag
	accountID, _, _ := parseS3Key(key)

	_, err := s.client.PutObjectTagging(ctx, &s3.PutObjectTaggingInput{
		Bucket: aws.String(s.bucketName),
		Key:    aws.String(key),
		Tagging: &s3types.Tagging{
			TagSet: []s3types.Tag{
				{Key: aws.String("Account"), Value: aws.String(accountID)},
				{Key: aws.String("Status"), Value: aws.String("confirmed")},
			},
		},
	})
	return err
}

// DeleteObject deletes an S3 object
func (s *S3ConfirmStorage) DeleteObject(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucketName),
		Key:    aws.String(key),
	})
	return err
}

// DynamoDBConfirmStore implements ConfirmDB using AWS DynamoDB
type DynamoDBConfirmStore struct {
	client    *dynamodb.Client
	tableName string
}

// NewDynamoDBConfirmStore creates a new DynamoDBConfirmStore
func NewDynamoDBConfirmStore(client *dynamodb.Client, tableName string) *DynamoDBConfirmStore {
	return &DynamoDBConfirmStore{
		client:    client,
		tableName: tableName,
	}
}

// GetBlobStatus returns the status of a blob record
// Returns "" if not found, "pending" or "confirmed" otherwise
func (d *DynamoDBConfirmStore) GetBlobStatus(ctx context.Context, accountID, blobID string) (string, error) {
	result, err := d.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(d.tableName),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: fmt.Sprintf("ACCOUNT#%s", accountID)},
			"sk": &types.AttributeValueMemberS{Value: fmt.Sprintf("BLOB#%s", blobID)},
		},
		ProjectionExpression: aws.String("#status"),
		ExpressionAttributeNames: map[string]string{
			"#status": "status",
		},
	})
	if err != nil {
		return "", err
	}

	if result.Item == nil {
		return "", nil // Not found
	}

	statusAttr, ok := result.Item["status"].(*types.AttributeValueMemberS)
	if !ok {
		return "", nil // Status attribute missing or wrong type
	}

	return statusAttr.Value, nil
}

// ConfirmBlob updates the blob status to confirmed and decrements the pending count
func (d *DynamoDBConfirmStore) ConfirmBlob(ctx context.Context, accountID, blobID string) error {
	now := time.Now().UTC().Format(time.RFC3339)

	_, err := d.client.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{
		TransactItems: []types.TransactWriteItem{
			{
				Update: &types.Update{
					TableName: aws.String(d.tableName),
					Key: map[string]types.AttributeValue{
						"pk": &types.AttributeValueMemberS{Value: fmt.Sprintf("ACCOUNT#%s", accountID)},
						"sk": &types.AttributeValueMemberS{Value: fmt.Sprintf("BLOB#%s", blobID)},
					},
					UpdateExpression: aws.String(
						"SET #status = :confirmed, confirmedAt = :now " +
							"REMOVE gsi1pk, gsi1sk"),
					ConditionExpression: aws.String("#status = :pending"),
					ExpressionAttributeNames: map[string]string{
						"#status": "status",
					},
					ExpressionAttributeValues: map[string]types.AttributeValue{
						":confirmed": &types.AttributeValueMemberS{Value: "confirmed"},
						":pending":   &types.AttributeValueMemberS{Value: "pending"},
						":now":       &types.AttributeValueMemberS{Value: now},
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
						"ADD pendingAllocationsCount :negOne SET updatedAt = :now"),
					ExpressionAttributeValues: map[string]types.AttributeValue{
						":negOne": &types.AttributeValueMemberN{Value: "-1"},
						":now":    &types.AttributeValueMemberS{Value: now},
					},
				},
			},
		},
	})

	if err != nil {
		// Check for condition check failure (already confirmed)
		var txCanceled *types.TransactionCanceledException
		if errors.As(err, &txCanceled) {
			for _, reason := range txCanceled.CancellationReasons {
				if reason.Code != nil && *reason.Code == "ConditionalCheckFailed" {
					// Already confirmed, this is OK (idempotent)
					return nil
				}
			}
		}
		return err
	}

	return nil
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
		Storage: NewS3ConfirmStorage(s3Client, bucketName),
		DB:      NewDynamoDBConfirmStore(dynamoClient, tableName),
	}

	lambda.Start(handler)
}
