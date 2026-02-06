package blobcomplete

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// DynamoDBClient defines the interface for DynamoDB operations needed by blobcomplete
type DynamoDBClient interface {
	GetItem(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
}

// DynamoDBStore implements DB using AWS DynamoDB
type DynamoDBStore struct {
	client    DynamoDBClient
	tableName string
}

// NewDynamoDBStore creates a new DynamoDBStore for blobcomplete
func NewDynamoDBStore(client DynamoDBClient, tableName string) *DynamoDBStore {
	return &DynamoDBStore{
		client:    client,
		tableName: tableName,
	}
}

// GetBlobForComplete returns the blob record fields needed for Blob/complete validation.
// Returns nil if the blob record is not found.
func (d *DynamoDBStore) GetBlobForComplete(ctx context.Context, accountID, blobID string) (*BlobRecord, error) {
	result, err := d.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(d.tableName),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: fmt.Sprintf("ACCOUNT#%s", accountID)},
			"sk": &types.AttributeValueMemberS{Value: fmt.Sprintf("BLOB#%s", blobID)},
		},
		ProjectionExpression: aws.String("#status, multipart, uploadId"),
		ExpressionAttributeNames: map[string]string{
			"#status": "status",
		},
	})
	if err != nil {
		return nil, err
	}

	if result.Item == nil {
		return nil, nil
	}

	record := &BlobRecord{}

	if statusAttr, ok := result.Item["status"].(*types.AttributeValueMemberS); ok {
		record.Status = statusAttr.Value
	}
	if mpAttr, ok := result.Item["multipart"].(*types.AttributeValueMemberBOOL); ok {
		record.Multipart = mpAttr.Value
	}
	if uidAttr, ok := result.Item["uploadId"].(*types.AttributeValueMemberS); ok {
		record.UploadID = uidAttr.Value
	}

	return record, nil
}
