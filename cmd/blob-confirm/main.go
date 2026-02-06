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
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/jarrod-lowe/jmap-service-libs/awsinit"
	"github.com/jarrod-lowe/jmap-service-libs/logging"
	"github.com/jarrod-lowe/jmap-service-libs/tracing"
	"go.opentelemetry.io/otel/attribute"
)

var logger = logging.New()

// ConfirmStorage handles S3 operations for blob confirmation
type ConfirmStorage interface {
	ConfirmTag(ctx context.Context, key string) error
	DeleteObject(ctx context.Context, key string) error
}

// BlobInfo holds status and metadata about a blob record
type BlobInfo struct {
	Status      string
	SizeUnknown bool
	IAMAuth     bool
}

// ConfirmDB handles DynamoDB operations for blob confirmation
type ConfirmDB interface {
	GetBlobInfo(ctx context.Context, accountID, blobID string) (*BlobInfo, error)
	ConfirmBlob(ctx context.Context, accountID, blobID string, actualSize int64, sizeUnknown bool, iamAuth bool) error
}

// Dependencies for handler (injectable for testing)
type Dependencies struct {
	Storage ConfirmStorage
	DB      ConfirmDB
}

var deps *Dependencies

// handler processes S3 ObjectCreated events to confirm blob uploads
func handler(ctx context.Context, event events.S3Event) error {
	ctx, span := tracing.StartHandlerSpan(ctx, "BlobConfirmHandler",
		tracing.Function("blob-confirm"),
	)
	defer span.End()

	for _, record := range event.Records {
		key := record.S3.Object.Key
		span.SetAttributes(
			attribute.String("s3.bucket", record.S3.Bucket.Name),
			attribute.String("s3.key", key),
		)
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
		span.SetAttributes(tracing.AccountID(accountID), tracing.BlobID(blobID))

		// Check blob record status
		blobInfo, err := deps.DB.GetBlobInfo(ctx, accountID, blobID)
		if err != nil {
			logger.ErrorContext(ctx, "Failed to get blob info",
				slog.String("account_id", accountID),
				slog.String("blob_id", blobID),
				slog.String("error", err.Error()),
			)
			return fmt.Errorf("failed to get blob info: %w", err)
		}

		// If record not found, skip - this object may be from a different upload path
		// (e.g., traditional /upload/ endpoint) or the record may have been cleaned up.
		// The blob-alloc-cleanup Lambda handles expired pending allocations.
		if blobInfo == nil {
			logger.WarnContext(ctx, "Blob record not found, skipping (may be traditional upload)",
				slog.String("account_id", accountID),
				slog.String("blob_id", blobID),
			)
			continue
		}

		// If already confirmed, skip (idempotent)
		if blobInfo.Status == "confirmed" {
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
		actualSize := record.S3.Object.Size
		if err := deps.DB.ConfirmBlob(ctx, accountID, blobID, actualSize, blobInfo.SizeUnknown, blobInfo.IAMAuth); err != nil {
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

// GetBlobInfo returns status and metadata for a blob record.
// Returns nil if not found.
func (d *DynamoDBConfirmStore) GetBlobInfo(ctx context.Context, accountID, blobID string) (*BlobInfo, error) {
	result, err := d.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(d.tableName),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: fmt.Sprintf("ACCOUNT#%s", accountID)},
			"sk": &types.AttributeValueMemberS{Value: fmt.Sprintf("BLOB#%s", blobID)},
		},
		ProjectionExpression: aws.String("#status, sizeUnknown, iamAuth"),
		ExpressionAttributeNames: map[string]string{
			"#status": "status",
		},
	})
	if err != nil {
		return nil, err
	}

	if result.Item == nil {
		return nil, nil // Not found
	}

	info := &BlobInfo{}

	if statusAttr, ok := result.Item["status"].(*types.AttributeValueMemberS); ok {
		info.Status = statusAttr.Value
	}

	if suAttr, ok := result.Item["sizeUnknown"].(*types.AttributeValueMemberBOOL); ok {
		info.SizeUnknown = suAttr.Value
	}

	if iaAttr, ok := result.Item["iamAuth"].(*types.AttributeValueMemberBOOL); ok {
		info.IAMAuth = iaAttr.Value
	}

	return info, nil
}

// ConfirmBlob updates the blob status to confirmed and decrements the pending count.
// When sizeUnknown is true, it also sets the actual size and deducts quota.
// When iamAuth is true, skips pending allocations count decrement.
func (d *DynamoDBConfirmStore) ConfirmBlob(ctx context.Context, accountID, blobID string, actualSize int64, sizeUnknown bool, iamAuth bool) error {
	now := time.Now().UTC().Format(time.RFC3339)

	blobKey := map[string]types.AttributeValue{
		"pk": &types.AttributeValueMemberS{Value: fmt.Sprintf("ACCOUNT#%s", accountID)},
		"sk": &types.AttributeValueMemberS{Value: fmt.Sprintf("BLOB#%s", blobID)},
	}
	metaKey := map[string]types.AttributeValue{
		"pk": &types.AttributeValueMemberS{Value: fmt.Sprintf("ACCOUNT#%s", accountID)},
		"sk": &types.AttributeValueMemberS{Value: "META#"},
	}

	// Build blob record update: confirm status, remove GSI keys
	blobUpdateExpr := "SET #status = :confirmed, confirmedAt = :now REMOVE gsi1pk, gsi1sk"
	blobExprNames := map[string]string{"#status": "status"}
	blobExprValues := map[string]types.AttributeValue{
		":confirmed": &types.AttributeValueMemberS{Value: "confirmed"},
		":pending":   &types.AttributeValueMemberS{Value: "pending"},
		":now":       &types.AttributeValueMemberS{Value: now},
	}

	// When size was unknown, also set actual size and remove sizeUnknown attr
	if sizeUnknown {
		blobUpdateExpr = "SET #status = :confirmed, confirmedAt = :now, #size = :actualSize REMOVE gsi1pk, gsi1sk, sizeUnknown"
		blobExprNames["#size"] = "size"
		blobExprValues[":actualSize"] = &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", actualSize)}
	}

	blobUpdate := &types.Update{
		TableName:                 aws.String(d.tableName),
		Key:                       blobKey,
		UpdateExpression:          aws.String(blobUpdateExpr),
		ConditionExpression:       aws.String("#status = :pending"),
		ExpressionAttributeNames:  blobExprNames,
		ExpressionAttributeValues: blobExprValues,
	}

	// Build META# update: decrement pending count (unless IAM auth), and deduct quota if size was unknown
	var metaUpdateExpr string
	metaValues := map[string]types.AttributeValue{
		":now": &types.AttributeValueMemberS{Value: now},
	}

	if iamAuth {
		// IAM auth: no pending count to decrement
		metaUpdateExpr = "SET updatedAt = :now"
		if sizeUnknown {
			metaUpdateExpr = "ADD quotaRemaining :negSize SET updatedAt = :now"
			metaValues[":negSize"] = &types.AttributeValueMemberN{Value: fmt.Sprintf("-%d", actualSize)}
		}
	} else {
		metaUpdateExpr = "ADD pendingAllocationsCount :negOne SET updatedAt = :now"
		metaValues[":negOne"] = &types.AttributeValueMemberN{Value: "-1"}
		if sizeUnknown {
			metaUpdateExpr = "ADD pendingAllocationsCount :negOne, quotaRemaining :negSize SET updatedAt = :now"
			metaValues[":negSize"] = &types.AttributeValueMemberN{Value: fmt.Sprintf("-%d", actualSize)}
		}
	}

	metaUpdate := &types.Update{
		TableName:                 aws.String(d.tableName),
		Key:                       metaKey,
		UpdateExpression:          aws.String(metaUpdateExpr),
		ExpressionAttributeValues: metaValues,
	}

	_, err := d.client.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{
		TransactItems: []types.TransactWriteItem{
			{Update: blobUpdate},
			{Update: metaUpdate},
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

	result, err := awsinit.Init(ctx)
	if err != nil {
		logger.Error("FATAL: Failed to initialize AWS",
			slog.String("error", err.Error()),
		)
		panic(err)
	}
	defer result.Cleanup()

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

	s3Client := s3.NewFromConfig(result.Config)
	dynamoClient := dynamodb.NewFromConfig(result.Config)

	deps = &Dependencies{
		Storage: NewS3ConfirmStorage(s3Client, bucketName),
		DB:      NewDynamoDBConfirmStore(dynamoClient, tableName),
	}

	result.Start(handler)
}
