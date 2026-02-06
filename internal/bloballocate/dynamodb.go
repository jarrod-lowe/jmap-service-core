package bloballocate

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// DynamoDBClient defines the interface for DynamoDB operations
type DynamoDBClient interface {
	TransactWriteItems(ctx context.Context, params *dynamodb.TransactWriteItemsInput, optFns ...func(*dynamodb.Options)) (*dynamodb.TransactWriteItemsOutput, error)
	GetItem(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
}

// DynamoDBStore implements DB using AWS DynamoDB
type DynamoDBStore struct {
	client    DynamoDBClient
	tableName string
}

// NewDynamoDBStore creates a new DynamoDBStore
func NewDynamoDBStore(client DynamoDBClient, tableName string) *DynamoDBStore {
	return &DynamoDBStore{
		client:    client,
		tableName: tableName,
	}
}

// AllocateBlob creates a pending allocation record with a transactional write
// that also updates the account META# record (pendingAllocationsCount, quotaRemaining).
// When uploadID is non-empty, stores it on the blob record for multipart upload tracking.
func (d *DynamoDBStore) AllocateBlob(ctx context.Context, accountID, blobID string, size int64, contentType string, urlExpiresAt time.Time, maxPending int, s3Key string, sizeUnknown bool, uploadID string, isIAMAuth bool) error {
	now := time.Now().UTC().Format(time.RFC3339)
	urlExpiresAtStr := urlExpiresAt.UTC().Format(time.RFC3339)

	// Build GSI sort key: EXPIRES#{urlExpiresAt}#{accountId}#{blobId}
	gsi1sk := fmt.Sprintf("EXPIRES#%s#%s#%s", urlExpiresAtStr, accountID, blobID)

	// Build blob record
	// Note: blobId and accountId are stored as explicit attributes (in addition to pk/sk)
	// because blob-download expects them when unmarshaling BlobRecord
	blobItem := map[string]any{
		"pk":           fmt.Sprintf("ACCOUNT#%s", accountID),
		"sk":           fmt.Sprintf("BLOB#%s", blobID),
		"blobId":       blobID,
		"accountId":    accountID,
		"gsi1pk":       "PENDING",
		"gsi1sk":       gsi1sk,
		"status":       "pending",
		"urlExpiresAt": urlExpiresAtStr,
		"size":         size,
		"contentType":  contentType,
		"s3Key":        s3Key,
		"createdAt":    now,
	}
	if sizeUnknown {
		blobItem["sizeUnknown"] = true
	}
	if isIAMAuth {
		blobItem["iamAuth"] = true
	}
	if uploadID != "" {
		blobItem["uploadId"] = uploadID
		blobItem["multipart"] = true
	}

	blobAV, err := attributevalue.MarshalMap(blobItem)
	if err != nil {
		return fmt.Errorf("failed to marshal blob record: %w", err)
	}

	// Build META# key
	metaKey := map[string]types.AttributeValue{
		"pk": &types.AttributeValueMemberS{Value: fmt.Sprintf("ACCOUNT#%s", accountID)},
		"sk": &types.AttributeValueMemberS{Value: "META#"},
	}

	// Build META# update expression and condition.
	// IAM auth: skip pending allocations count (no increment, no limit check).
	// Non-IAM: include pending count increment and limit check.
	var updateExpr, conditionExpr string
	exprValues := map[string]types.AttributeValue{
		":now": &types.AttributeValueMemberS{Value: now},
	}

	if isIAMAuth {
		updateExpr = "SET updatedAt = :now"
		conditionExpr = "attribute_exists(pk)"
	} else {
		updateExpr = "ADD pendingAllocationsCount :one SET updatedAt = :now"
		conditionExpr = "attribute_exists(pk) AND pendingAllocationsCount < :max"
		exprValues[":one"] = &types.AttributeValueMemberN{Value: "1"}
		exprValues[":max"] = &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", maxPending)}
	}

	// When size is known, also deduct quota (applies to both IAM and non-IAM)
	if !sizeUnknown {
		if isIAMAuth {
			updateExpr = "ADD quotaRemaining :negSize SET updatedAt = :now"
		} else {
			updateExpr = "ADD pendingAllocationsCount :one, quotaRemaining :negSize SET updatedAt = :now"
		}
		conditionExpr += " AND quotaRemaining >= :size"
		exprValues[":negSize"] = &types.AttributeValueMemberN{Value: fmt.Sprintf("-%d", size)}
		exprValues[":size"] = &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", size)}
	}

	metaUpdate := &types.Update{
		TableName:                 aws.String(d.tableName),
		Key:                       metaKey,
		UpdateExpression:          aws.String(updateExpr),
		ConditionExpression:       aws.String(conditionExpr),
		ExpressionAttributeValues: exprValues,
	}

	// Transaction: Update META# and Put blob record
	_, err = d.client.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{
		TransactItems: []types.TransactWriteItem{
			{
				Update: metaUpdate,
			},
			{
				Put: &types.Put{
					TableName:           aws.String(d.tableName),
					Item:                blobAV,
					ConditionExpression: aws.String("attribute_not_exists(pk)"),
				},
			},
		},
	})

	if err != nil {
		// Check for transaction cancellation reasons
		var txCanceled *types.TransactionCanceledException
		if errors.As(err, &txCanceled) {
			// Analyze cancellation reasons
			for i, reason := range txCanceled.CancellationReasons {
				if reason.Code != nil && *reason.Code == "ConditionalCheckFailed" {
					if i == 0 {
						// META# update condition failed
						// Could be: account not provisioned, too many pending, or over quota
						// We need to distinguish these cases
						return d.diagnoseMetaConditionFailure(ctx, accountID, maxPending, size, sizeUnknown, isIAMAuth)
					}
					// Blob record already exists (unlikely with UUID)
					return fmt.Errorf("blob record already exists")
				}
			}
		}
		return fmt.Errorf("transaction failed: %w", err)
	}

	return nil
}

// diagnoseMetaConditionFailure determines why the META# condition failed
func (d *DynamoDBStore) diagnoseMetaConditionFailure(ctx context.Context, accountID string, maxPending int, size int64, sizeUnknown bool, isIAMAuth bool) error {
	// Query the META# record to determine which condition failed
	result, err := d.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(d.tableName),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: fmt.Sprintf("ACCOUNT#%s", accountID)},
			"sk": &types.AttributeValueMemberS{Value: "META#"},
		},
		ProjectionExpression: aws.String("pendingAllocationsCount, quotaRemaining"),
	})
	if err != nil {
		// Can't diagnose, return generic error
		return &AllocationError{
			Type:    "serverFail",
			Message: "Failed to diagnose allocation failure",
		}
	}

	// If META# doesn't exist, account is not provisioned
	if result.Item == nil {
		return &AllocationError{
			Type:    "accountNotProvisioned",
			Message: "Account is not provisioned",
		}
	}

	// Parse values from the record
	var pendingCount int
	var quotaRemaining int64

	if v, ok := result.Item["pendingAllocationsCount"]; ok {
		if n, ok := v.(*types.AttributeValueMemberN); ok {
			fmt.Sscanf(n.Value, "%d", &pendingCount)
		}
	}

	if v, ok := result.Item["quotaRemaining"]; ok {
		if n, ok := v.(*types.AttributeValueMemberN); ok {
			fmt.Sscanf(n.Value, "%d", &quotaRemaining)
		}
	}

	// Check which condition failed (skip pending check for IAM auth)
	if !isIAMAuth && pendingCount >= maxPending {
		return &AllocationError{
			Type:    "tooManyPending",
			Message: fmt.Sprintf("Too many pending allocations (%d/%d)", pendingCount, maxPending),
		}
	}

	if !sizeUnknown && quotaRemaining < size {
		return &AllocationError{
			Type:    "overQuota",
			Message: fmt.Sprintf("Insufficient quota remaining (%d bytes needed, %d available)", size, quotaRemaining),
		}
	}

	// If we get here, the condition may have been a race condition
	// (values changed between our check and the transaction)
	return &AllocationError{
		Type:    "serverFail",
		Message: "Allocation failed due to concurrent modification",
	}
}
