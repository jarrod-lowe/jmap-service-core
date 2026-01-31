package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

// MockStorage implements CleanupStorage for testing
type MockStorage struct {
	DeleteObjectCalled bool
	DeleteObjectKeys   []string
	DeleteObjectErr    error
}

func (m *MockStorage) DeleteObject(ctx context.Context, key string) error {
	m.DeleteObjectCalled = true
	m.DeleteObjectKeys = append(m.DeleteObjectKeys, key)
	return m.DeleteObjectErr
}

// MockDB implements CleanupDB for testing
type MockDB struct {
	GetExpiredPendingCalled bool
	GetExpiredPendingResult []PendingAllocation
	GetExpiredPendingErr    error

	CleanupAllocationCalled bool
	CleanupAllocationInputs []CleanupInput
	CleanupAllocationErr    error
}

type CleanupInput struct {
	AccountID string
	BlobID    string
	Size      int64
}

func (m *MockDB) GetExpiredPendingAllocations(ctx context.Context, cutoff time.Time) ([]PendingAllocation, error) {
	m.GetExpiredPendingCalled = true
	return m.GetExpiredPendingResult, m.GetExpiredPendingErr
}

func (m *MockDB) CleanupAllocation(ctx context.Context, accountID, blobID string, size int64) error {
	m.CleanupAllocationCalled = true
	m.CleanupAllocationInputs = append(m.CleanupAllocationInputs, CleanupInput{
		AccountID: accountID,
		BlobID:    blobID,
		Size:      size,
	})
	return m.CleanupAllocationErr
}

func TestHandler_NoExpiredAllocations(t *testing.T) {
	mockStorage := &MockStorage{}
	mockDB := &MockDB{GetExpiredPendingResult: []PendingAllocation{}}

	deps = &Dependencies{
		Storage:     mockStorage,
		DB:          mockDB,
		BufferHours: 72,
	}

	err := handler(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !mockDB.GetExpiredPendingCalled {
		t.Error("expected GetExpiredPendingAllocations to be called")
	}

	// Should not call delete or cleanup when there are no expired allocations
	if mockStorage.DeleteObjectCalled {
		t.Error("expected DeleteObject NOT to be called when no expired allocations")
	}
	if mockDB.CleanupAllocationCalled {
		t.Error("expected CleanupAllocation NOT to be called when no expired allocations")
	}
}

func TestHandler_CleansUpExpiredAllocations(t *testing.T) {
	mockStorage := &MockStorage{}
	mockDB := &MockDB{
		GetExpiredPendingResult: []PendingAllocation{
			{AccountID: "account-1", BlobID: "blob-1", S3Key: "account-1/blob-1", Size: 1024},
			{AccountID: "account-2", BlobID: "blob-2", S3Key: "account-2/blob-2", Size: 2048},
		},
	}

	deps = &Dependencies{
		Storage:     mockStorage,
		DB:          mockDB,
		BufferHours: 72,
	}

	err := handler(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify S3 objects were deleted
	if len(mockStorage.DeleteObjectKeys) != 2 {
		t.Errorf("expected 2 S3 objects deleted, got %d", len(mockStorage.DeleteObjectKeys))
	}

	// Verify DynamoDB records were cleaned up
	if len(mockDB.CleanupAllocationInputs) != 2 {
		t.Errorf("expected 2 cleanup calls, got %d", len(mockDB.CleanupAllocationInputs))
	}

	// Verify first cleanup
	if mockDB.CleanupAllocationInputs[0].AccountID != "account-1" {
		t.Errorf("expected first accountID account-1, got %s", mockDB.CleanupAllocationInputs[0].AccountID)
	}
	if mockDB.CleanupAllocationInputs[0].Size != 1024 {
		t.Errorf("expected first size 1024, got %d", mockDB.CleanupAllocationInputs[0].Size)
	}
}

func TestHandler_S3DeleteFails_ContinuesWithOthers(t *testing.T) {
	mockStorage := &MockStorage{DeleteObjectErr: errors.New("S3 error")}
	mockDB := &MockDB{
		GetExpiredPendingResult: []PendingAllocation{
			{AccountID: "account-1", BlobID: "blob-1", S3Key: "account-1/blob-1", Size: 1024},
		},
	}

	deps = &Dependencies{
		Storage:     mockStorage,
		DB:          mockDB,
		BufferHours: 72,
	}

	// Handler should not return error for S3 failures (they're logged but not fatal)
	err := handler(context.Background())
	if err != nil {
		t.Fatalf("expected no error (S3 errors are non-fatal), got %v", err)
	}

	// Should NOT call CleanupAllocation when S3 delete fails
	if mockDB.CleanupAllocationCalled {
		t.Error("expected CleanupAllocation NOT to be called when S3 delete fails")
	}
}

func TestHandler_DBCleanupFails_ContinuesWithOthers(t *testing.T) {
	mockStorage := &MockStorage{}
	mockDB := &MockDB{
		GetExpiredPendingResult: []PendingAllocation{
			{AccountID: "account-1", BlobID: "blob-1", S3Key: "account-1/blob-1", Size: 1024},
			{AccountID: "account-2", BlobID: "blob-2", S3Key: "account-2/blob-2", Size: 2048},
		},
		CleanupAllocationErr: errors.New("DynamoDB error"),
	}

	deps = &Dependencies{
		Storage:     mockStorage,
		DB:          mockDB,
		BufferHours: 72,
	}

	// Handler should not return error for individual cleanup failures
	err := handler(context.Background())
	if err != nil {
		t.Fatalf("expected no error (individual failures are non-fatal), got %v", err)
	}

	// Both S3 objects should have been attempted
	if len(mockStorage.DeleteObjectKeys) != 2 {
		t.Errorf("expected 2 S3 delete attempts, got %d", len(mockStorage.DeleteObjectKeys))
	}
}

func TestHandler_GetExpiredFails_ReturnsError(t *testing.T) {
	mockStorage := &MockStorage{}
	mockDB := &MockDB{
		GetExpiredPendingErr: errors.New("DynamoDB query error"),
	}

	deps = &Dependencies{
		Storage:     mockStorage,
		DB:          mockDB,
		BufferHours: 72,
	}

	err := handler(context.Background())
	if err == nil {
		t.Fatal("expected error when GetExpiredPendingAllocations fails")
	}
}
