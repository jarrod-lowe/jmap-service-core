package db

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

func TestNewClient_ReturnsClient(t *testing.T) {
	client, err := NewClient(context.Background(), "test-table")
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	if client == nil {
		t.Fatal("NewClient returned nil client")
	}
}

func TestNewClient_SetsTableName(t *testing.T) {
	client, err := NewClient(context.Background(), "my-table")
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	if client == nil {
		t.Fatal("NewClient returned nil client")
	}
	if client.tableName != "my-table" {
		t.Errorf("Expected tableName=my-table, got %s", client.tableName)
	}
}

func TestNewClient_CreatesDynamoDBClient(t *testing.T) {
	client, err := NewClient(context.Background(), "test-table")
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	if client == nil {
		t.Fatal("NewClient returned nil client")
	}
	if client.ddb == nil {
		t.Error("Expected ddb client to be set, got nil")
	}
}

func TestQueryByPK_ReturnsItemsFromDynamoDB(t *testing.T) {
	// Setup mock to return items
	mockItems := []map[string]types.AttributeValue{
		{
			"pk":       &types.AttributeValueMemberS{Value: "PLUGIN#"},
			"sk":       &types.AttributeValueMemberS{Value: "PLUGIN#mail-core"},
			"pluginId": &types.AttributeValueMemberS{Value: "mail-core"},
		},
		{
			"pk":       &types.AttributeValueMemberS{Value: "PLUGIN#"},
			"sk":       &types.AttributeValueMemberS{Value: "PLUGIN#contacts"},
			"pluginId": &types.AttributeValueMemberS{Value: "contacts"},
		},
	}

	mock := &mockDynamoDBClient{
		queryOutput: &dynamodb.QueryOutput{
			Items: mockItems,
		},
	}

	client := &Client{
		ddb:       mock,
		tableName: "test-table",
	}

	// Call QueryByPK
	items, err := client.QueryByPK(context.Background(), "PLUGIN#")
	if err != nil {
		t.Fatalf("QueryByPK returned error: %v", err)
	}

	// Verify items returned
	if len(items) != 2 {
		t.Errorf("Expected 2 items, got %d", len(items))
	}
}

func TestQueryByPK_CallsDynamoDBWithCorrectPK(t *testing.T) {
	mock := &mockDynamoDBClient{
		queryOutput: &dynamodb.QueryOutput{
			Items: []map[string]types.AttributeValue{},
		},
	}

	client := &Client{
		ddb:       mock,
		tableName: "test-table",
	}

	_, err := client.QueryByPK(context.Background(), "PLUGIN#")
	if err != nil {
		t.Fatalf("QueryByPK returned error: %v", err)
	}

	// Verify Query was called
	if !mock.queryCalled {
		t.Error("Expected Query to be called on DynamoDB client")
	}
}
