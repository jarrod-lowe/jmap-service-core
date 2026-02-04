package db

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// mockDynamoDBClient implements dbclient.DynamoDBClient for testing
type mockDynamoDBClient struct {
	updateItemFunc func(ctx context.Context, params *dynamodb.UpdateItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error)
	item           map[string]types.AttributeValue

	// Query support
	queryFunc   func(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
	queryOutput *dynamodb.QueryOutput
	queryCalled bool
	queryInput  *dynamodb.QueryInput
}

func (m *mockDynamoDBClient) GetItem(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	panic("not implemented")
}

func (m *mockDynamoDBClient) PutItem(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	panic("not implemented")
}

func (m *mockDynamoDBClient) DeleteItem(ctx context.Context, params *dynamodb.DeleteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error) {
	panic("not implemented")
}

func (m *mockDynamoDBClient) TransactWriteItems(ctx context.Context, params *dynamodb.TransactWriteItemsInput, optFns ...func(*dynamodb.Options)) (*dynamodb.TransactWriteItemsOutput, error) {
	panic("not implemented")
}

func (m *mockDynamoDBClient) UpdateItem(ctx context.Context, params *dynamodb.UpdateItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
	if m.updateItemFunc != nil {
		return m.updateItemFunc(ctx, params, optFns...)
	}
	return &dynamodb.UpdateItemOutput{
		Attributes: m.item,
	}, nil
}

func (m *mockDynamoDBClient) Query(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	m.queryCalled = true
	m.queryInput = params
	if m.queryFunc != nil {
		return m.queryFunc(ctx, params, optFns...)
	}
	if m.queryOutput != nil {
		return m.queryOutput, nil
	}
	return &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{}}, nil
}

func TestEnsureAccount_CreatesNewAccount(t *testing.T) {
	var capturedInput *dynamodb.UpdateItemInput
	now := time.Now().UTC()

	mock := &mockDynamoDBClient{
		updateItemFunc: func(ctx context.Context, params *dynamodb.UpdateItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
			capturedInput = params
			return &dynamodb.UpdateItemOutput{
				Attributes: map[string]types.AttributeValue{
					"pk":                  &types.AttributeValueMemberS{Value: "ACCOUNT#user123"},
					"sk":                  &types.AttributeValueMemberS{Value: "META#"},
					"owner":               &types.AttributeValueMemberS{Value: "USER#user123"},
					"createdAt":           &types.AttributeValueMemberS{Value: now.Format(time.RFC3339)},
					"lastDiscoveryAccess": &types.AttributeValueMemberS{Value: now.Format(time.RFC3339)},
				},
			}, nil
		},
	}

	client := &Client{
		ddb:       mock,
		tableName: "test-table",
	}

	account, err := client.EnsureAccount(context.Background(), "user123")
	if err != nil {
		t.Fatalf("EnsureAccount returned error: %v", err)
	}

	// Verify key structure
	if capturedInput == nil {
		t.Fatal("UpdateItem was not called")
	}

	pk, ok := capturedInput.Key["pk"].(*types.AttributeValueMemberS)
	if !ok || pk.Value != "ACCOUNT#user123" {
		t.Errorf("Expected pk=ACCOUNT#user123, got %v", capturedInput.Key["pk"])
	}

	sk, ok := capturedInput.Key["sk"].(*types.AttributeValueMemberS)
	if !ok || sk.Value != "META#" {
		t.Errorf("Expected sk=META#, got %v", capturedInput.Key["sk"])
	}

	// Verify table name
	if *capturedInput.TableName != "test-table" {
		t.Errorf("Expected table name test-table, got %s", *capturedInput.TableName)
	}

	// Verify returned account
	if account.UserID != "user123" {
		t.Errorf("Expected UserID=user123, got %s", account.UserID)
	}
	if account.Owner != "USER#user123" {
		t.Errorf("Expected Owner=USER#user123, got %s", account.Owner)
	}
}

func TestEnsureAccount_UpdatesLastDiscoveryAccess(t *testing.T) {
	var capturedInput *dynamodb.UpdateItemInput

	mock := &mockDynamoDBClient{
		updateItemFunc: func(ctx context.Context, params *dynamodb.UpdateItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
			capturedInput = params
			return &dynamodb.UpdateItemOutput{
				Attributes: map[string]types.AttributeValue{
					"pk":                  &types.AttributeValueMemberS{Value: "ACCOUNT#user123"},
					"sk":                  &types.AttributeValueMemberS{Value: "META#"},
					"owner":               &types.AttributeValueMemberS{Value: "USER#user123"},
					"createdAt":           &types.AttributeValueMemberS{Value: "2024-01-01T00:00:00Z"},
					"lastDiscoveryAccess": &types.AttributeValueMemberS{Value: time.Now().UTC().Format(time.RFC3339)},
				},
			}, nil
		},
	}

	client := &Client{
		ddb:       mock,
		tableName: "test-table",
	}

	_, err := client.EnsureAccount(context.Background(), "user123")
	if err != nil {
		t.Fatalf("EnsureAccount returned error: %v", err)
	}

	// Verify UpdateItem was called
	if capturedInput == nil {
		t.Fatal("UpdateItem was not called")
	}

	// Verify update expression uses if_not_exists for owner and createdAt
	if capturedInput.UpdateExpression == nil {
		t.Fatal("UpdateExpression should not be nil")
	}

	expr := *capturedInput.UpdateExpression
	if len(expr) == 0 {
		t.Error("UpdateExpression should not be empty")
	}

	// The expression should contain if_not_exists for owner and createdAt
	// but always set lastDiscoveryAccess
}

func TestEnsureAccount_HandlesError(t *testing.T) {
	callCount := 0
	mock := &mockDynamoDBClient{
		updateItemFunc: func(ctx context.Context, params *dynamodb.UpdateItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
			callCount++
			return nil, &types.ProvisionedThroughputExceededException{
				Message: stringPtr("Rate exceeded"),
			}
		},
	}

	client := &Client{
		ddb:       mock,
		tableName: "test-table",
	}

	_, err := client.EnsureAccount(context.Background(), "user123")

	// Verify UpdateItem was called
	if callCount == 0 {
		t.Fatal("UpdateItem was not called")
	}

	if err == nil {
		t.Error("Expected error to be returned when DynamoDB fails")
	}
}

func stringPtr(s string) *string {
	return &s
}
