package bloballocate

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// CapturingDynamoDBClient captures TransactWriteItems calls for inspection
type CapturingDynamoDBClient struct {
	TransactWriteItemsFunc func(ctx context.Context, params *dynamodb.TransactWriteItemsInput, optFns ...func(*dynamodb.Options)) (*dynamodb.TransactWriteItemsOutput, error)
	GetItemFunc            func(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
	LastTransactInput      *dynamodb.TransactWriteItemsInput
}

func (c *CapturingDynamoDBClient) TransactWriteItems(ctx context.Context, params *dynamodb.TransactWriteItemsInput, optFns ...func(*dynamodb.Options)) (*dynamodb.TransactWriteItemsOutput, error) {
	c.LastTransactInput = params
	if c.TransactWriteItemsFunc != nil {
		return c.TransactWriteItemsFunc(ctx, params, optFns...)
	}
	return &dynamodb.TransactWriteItemsOutput{}, nil
}

func (c *CapturingDynamoDBClient) GetItem(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	if c.GetItemFunc != nil {
		return c.GetItemFunc(ctx, params, optFns...)
	}
	return &dynamodb.GetItemOutput{}, nil
}

func TestAllocateBlob_NonMultipart_NoUploadIdStored(t *testing.T) {
	client := &CapturingDynamoDBClient{}
	store := NewDynamoDBStore(client, "test-table")

	err := store.AllocateBlob(ctx(), "account-1", "blob-1", 1024, "application/pdf",
		time.Now().Add(15*time.Minute), 4, "account-1/blob-1", false, "")

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if client.LastTransactInput == nil {
		t.Fatal("expected TransactWriteItems to be called")
	}

	// Check the Put item doesn't have uploadId or multipart attributes
	putItem := client.LastTransactInput.TransactItems[1].Put.Item
	if _, ok := putItem["uploadId"]; ok {
		t.Error("expected no uploadId attribute for non-multipart allocation")
	}
	if _, ok := putItem["multipart"]; ok {
		t.Error("expected no multipart attribute for non-multipart allocation")
	}
}

func TestAllocateBlob_Multipart_StoresUploadId(t *testing.T) {
	client := &CapturingDynamoDBClient{}
	store := NewDynamoDBStore(client, "test-table")

	err := store.AllocateBlob(ctx(), "account-1", "blob-1", 0, "message/rfc822",
		time.Now().Add(15*time.Minute), 4, "account-1/blob-1", true, "upload-xyz-123")

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if client.LastTransactInput == nil {
		t.Fatal("expected TransactWriteItems to be called")
	}

	putItem := client.LastTransactInput.TransactItems[1].Put.Item

	// Verify uploadId is stored
	uploadIdAttr, ok := putItem["uploadId"]
	if !ok {
		t.Fatal("expected uploadId attribute for multipart allocation")
	}
	uploadIdVal, ok := uploadIdAttr.(*types.AttributeValueMemberS)
	if !ok {
		t.Fatalf("expected uploadId to be string, got %T", uploadIdAttr)
	}
	if uploadIdVal.Value != "upload-xyz-123" {
		t.Errorf("expected uploadId 'upload-xyz-123', got %q", uploadIdVal.Value)
	}

	// Verify multipart flag is stored
	multipartAttr, ok := putItem["multipart"]
	if !ok {
		t.Fatal("expected multipart attribute for multipart allocation")
	}
	multipartVal, ok := multipartAttr.(*types.AttributeValueMemberBOOL)
	if !ok {
		t.Fatalf("expected multipart to be bool, got %T", multipartAttr)
	}
	if !multipartVal.Value {
		t.Error("expected multipart to be true")
	}
}

func TestAllocateBlob_EmptyUploadId_NotStored(t *testing.T) {
	client := &CapturingDynamoDBClient{}
	store := NewDynamoDBStore(client, "test-table")

	err := store.AllocateBlob(ctx(), "account-1", "blob-1", 1024, "application/pdf",
		time.Now().Add(15*time.Minute), 4, "account-1/blob-1", false, "")

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	putItem := client.LastTransactInput.TransactItems[1].Put.Item
	if _, ok := putItem["uploadId"]; ok {
		t.Error("expected no uploadId when uploadId is empty string")
	}
}

func TestAllocateBlob_TransactionError_Propagated(t *testing.T) {
	client := &CapturingDynamoDBClient{
		TransactWriteItemsFunc: func(ctx context.Context, params *dynamodb.TransactWriteItemsInput, optFns ...func(*dynamodb.Options)) (*dynamodb.TransactWriteItemsOutput, error) {
			return nil, fmt.Errorf("network error")
		},
	}
	store := NewDynamoDBStore(client, "test-table")

	err := store.AllocateBlob(ctx(), "account-1", "blob-1", 1024, "application/pdf",
		time.Now().Add(15*time.Minute), 4, "account-1/blob-1", false, "")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func ctx() context.Context {
	return context.Background()
}
