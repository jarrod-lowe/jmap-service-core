package db

import (
	"context"
	"testing"
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
