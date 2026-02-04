package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/jarrod-lowe/jmap-service-libs/awsinit"
	"github.com/jarrod-lowe/jmap-service-libs/logging"
)

var logger = logging.New()

// BlobDeleter deletes blob objects from S3
type BlobDeleter interface {
	DeleteObject(ctx context.Context, bucket, key string) error
}

// BlobDBDeleter deletes blob records from DynamoDB and restores quota
type BlobDBDeleter interface {
	DeleteBlobRecord(ctx context.Context, pk, sk string, accountID string, size int64) error
}

// Dependencies for handler (injectable for testing)
type Dependencies struct {
	S3Deleter  BlobDeleter
	DBDeleter  BlobDBDeleter
	BlobBucket string
}

var deps *Dependencies

// handler processes DynamoDB stream events for blob cleanup
func handler(ctx context.Context, event events.DynamoDBEvent) error {
	for _, record := range event.Records {
		if err := processRecord(ctx, record); err != nil {
			return err
		}
	}
	return nil
}

// processRecord handles a single DynamoDB stream record
func processRecord(ctx context.Context, record events.DynamoDBEventRecord) error {
	// Only process MODIFY events
	if record.EventName != "MODIFY" {
		return nil
	}

	newImage := record.Change.NewImage
	oldImage := record.Change.OldImage

	// Check if deletedAt was added (present in new, absent in old)
	_, hasNewDeletedAt := newImage["deletedAt"]
	_, hasOldDeletedAt := oldImage["deletedAt"]

	if !hasNewDeletedAt || hasOldDeletedAt {
		return nil
	}

	// Extract fields from new image
	pk, ok := extractStringAttribute(newImage, "pk")
	if !ok {
		logger.WarnContext(ctx, "Missing pk in stream record")
		return fmt.Errorf("missing pk in stream record")
	}

	sk, ok := extractStringAttribute(newImage, "sk")
	if !ok {
		logger.WarnContext(ctx, "Missing sk in stream record")
		return fmt.Errorf("missing sk in stream record")
	}

	s3Key, ok := extractStringAttribute(newImage, "s3Key")
	if !ok {
		logger.WarnContext(ctx, "Missing s3Key in stream record")
		return fmt.Errorf("missing s3Key in stream record")
	}

	accountID, _ := extractStringAttribute(newImage, "accountId")
	blobID, _ := extractStringAttribute(newImage, "blobId")
	size := extractNumberAttribute(newImage, "size")

	logger.InfoContext(ctx, "Cleaning up deleted blob",
		slog.String("account_id", accountID),
		slog.String("blob_id", blobID),
		slog.String("s3_key", s3Key),
		slog.Int64("size", size),
	)

	// Delete S3 object
	if err := deps.S3Deleter.DeleteObject(ctx, deps.BlobBucket, s3Key); err != nil {
		logger.ErrorContext(ctx, "Failed to delete S3 object",
			slog.String("s3_key", s3Key),
			slog.String("error", err.Error()),
		)
		return fmt.Errorf("failed to delete S3 object %s: %w", s3Key, err)
	}

	// Delete DynamoDB record and restore quota
	if err := deps.DBDeleter.DeleteBlobRecord(ctx, pk, sk, accountID, size); err != nil {
		logger.ErrorContext(ctx, "Failed to delete DynamoDB record",
			slog.String("pk", pk),
			slog.String("sk", sk),
			slog.String("error", err.Error()),
		)
		return fmt.Errorf("failed to delete DynamoDB record %s/%s: %w", pk, sk, err)
	}

	logger.InfoContext(ctx, "Blob cleanup complete",
		slog.String("account_id", accountID),
		slog.String("blob_id", blobID),
	)

	return nil
}

// extractStringAttribute extracts a string value from a DynamoDB stream attribute map
func extractStringAttribute(image map[string]events.DynamoDBAttributeValue, key string) (string, bool) {
	attr, ok := image[key]
	if !ok {
		return "", false
	}
	if attr.DataType() != events.DataTypeString {
		return "", false
	}
	val := attr.String()
	if val == "" {
		return "", false
	}
	return val, true
}

// extractNumberAttribute extracts a number value from a DynamoDB stream attribute map
func extractNumberAttribute(image map[string]events.DynamoDBAttributeValue, key string) int64 {
	attr, ok := image[key]
	if !ok {
		return 0
	}
	if attr.DataType() != events.DataTypeNumber {
		return 0
	}
	val, _ := attr.Integer()
	return val
}

// =============================================================================
// Real implementations
// =============================================================================

// S3BlobDeleter implements BlobDeleter using AWS S3
type S3BlobDeleter struct {
	client *s3.Client
}

// NewS3BlobDeleter creates a new S3BlobDeleter
func NewS3BlobDeleter(client *s3.Client) *S3BlobDeleter {
	return &S3BlobDeleter{client: client}
}

// DeleteObject deletes an object from S3
func (d *S3BlobDeleter) DeleteObject(ctx context.Context, bucket, key string) error {
	_, err := d.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	return err
}

// DynamoDBBlobDeleter implements BlobDBDeleter using AWS DynamoDB
type DynamoDBBlobDeleter struct {
	client    *dynamodb.Client
	tableName string
}

// NewDynamoDBBlobDeleter creates a new DynamoDBBlobDeleter
func NewDynamoDBBlobDeleter(client *dynamodb.Client, tableName string) *DynamoDBBlobDeleter {
	return &DynamoDBBlobDeleter{
		client:    client,
		tableName: tableName,
	}
}

// DeleteBlobRecord deletes a blob record from DynamoDB and restores quota to META#
func (d *DynamoDBBlobDeleter) DeleteBlobRecord(ctx context.Context, pk, sk string, accountID string, size int64) error {
	// If we don't have accountID or size, fall back to simple delete
	if accountID == "" || size == 0 {
		_, err := d.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
			TableName: aws.String(d.tableName),
			Key: map[string]types.AttributeValue{
				"pk": &types.AttributeValueMemberS{Value: pk},
				"sk": &types.AttributeValueMemberS{Value: sk},
			},
		})
		return err
	}

	// Use transaction to delete blob and restore quota atomically
	_, err := d.client.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{
		TransactItems: []types.TransactWriteItem{
			{
				Delete: &types.Delete{
					TableName: aws.String(d.tableName),
					Key: map[string]types.AttributeValue{
						"pk": &types.AttributeValueMemberS{Value: pk},
						"sk": &types.AttributeValueMemberS{Value: sk},
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
					UpdateExpression: aws.String("ADD quotaRemaining :size"),
					ExpressionAttributeValues: map[string]types.AttributeValue{
						":size": &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", size)},
					},
				},
			},
		},
	})
	return err
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

	tableName := os.Getenv("DYNAMODB_TABLE")
	if tableName == "" {
		logger.Error("FATAL: DYNAMODB_TABLE environment variable is required")
		panic("DYNAMODB_TABLE environment variable is required")
	}

	blobBucket := os.Getenv("BLOB_BUCKET")
	if blobBucket == "" {
		logger.Error("FATAL: BLOB_BUCKET environment variable is required")
		panic("BLOB_BUCKET environment variable is required")
	}

	dynamoClient := dynamodb.NewFromConfig(result.Config)
	s3Client := s3.NewFromConfig(result.Config)

	deps = &Dependencies{
		S3Deleter:  NewS3BlobDeleter(s3Client),
		DBDeleter:  NewDynamoDBBlobDeleter(dynamoClient, tableName),
		BlobBucket: blobBucket,
	}

	result.Start(handler)
}
