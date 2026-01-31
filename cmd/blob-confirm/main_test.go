package main

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-lambda-go/events"
)

// MockStorage implements ConfirmStorage for testing
type MockStorage struct {
	ConfirmTagCalled bool
	ConfirmTagKey    string
	ConfirmTagErr    error
	DeleteObjectCalled bool
	DeleteObjectKey  string
	DeleteObjectErr  error
}

func (m *MockStorage) ConfirmTag(ctx context.Context, key string) error {
	m.ConfirmTagCalled = true
	m.ConfirmTagKey = key
	return m.ConfirmTagErr
}

func (m *MockStorage) DeleteObject(ctx context.Context, key string) error {
	m.DeleteObjectCalled = true
	m.DeleteObjectKey = key
	return m.DeleteObjectErr
}

// MockDB implements ConfirmDB for testing
type MockDB struct {
	GetBlobStatusCalled bool
	GetBlobStatusInput  GetBlobStatusInput
	GetBlobStatusResult string // "pending", "confirmed", "" (not found)
	GetBlobStatusErr    error

	ConfirmBlobCalled bool
	ConfirmBlobInput  ConfirmBlobInput
	ConfirmBlobErr    error
}

type GetBlobStatusInput struct {
	AccountID string
	BlobID    string
}

type ConfirmBlobInput struct {
	AccountID string
	BlobID    string
}

func (m *MockDB) GetBlobStatus(ctx context.Context, accountID, blobID string) (string, error) {
	m.GetBlobStatusCalled = true
	m.GetBlobStatusInput = GetBlobStatusInput{AccountID: accountID, BlobID: blobID}
	return m.GetBlobStatusResult, m.GetBlobStatusErr
}

func (m *MockDB) ConfirmBlob(ctx context.Context, accountID, blobID string) error {
	m.ConfirmBlobCalled = true
	m.ConfirmBlobInput = ConfirmBlobInput{AccountID: accountID, BlobID: blobID}
	return m.ConfirmBlobErr
}

func TestHandler_ConfirmSuccess(t *testing.T) {
	mockStorage := &MockStorage{}
	mockDB := &MockDB{GetBlobStatusResult: "pending"}

	deps = &Dependencies{
		Storage: mockStorage,
		DB:      mockDB,
	}

	event := events.S3Event{
		Records: []events.S3EventRecord{
			{
				S3: events.S3Entity{
					Bucket: events.S3Bucket{Name: "test-bucket"},
					Object: events.S3Object{Key: "account-123/blob-456"},
				},
			},
		},
	}

	err := handler(context.Background(), event)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify DB GetBlobStatus was called
	if !mockDB.GetBlobStatusCalled {
		t.Error("expected GetBlobStatus to be called")
	}
	if mockDB.GetBlobStatusInput.AccountID != "account-123" {
		t.Errorf("expected accountID account-123, got %s", mockDB.GetBlobStatusInput.AccountID)
	}
	if mockDB.GetBlobStatusInput.BlobID != "blob-456" {
		t.Errorf("expected blobID blob-456, got %s", mockDB.GetBlobStatusInput.BlobID)
	}

	// Verify Storage ConfirmTag was called first
	if !mockStorage.ConfirmTagCalled {
		t.Error("expected ConfirmTag to be called")
	}
	if mockStorage.ConfirmTagKey != "account-123/blob-456" {
		t.Errorf("expected key account-123/blob-456, got %s", mockStorage.ConfirmTagKey)
	}

	// Verify DB ConfirmBlob was called
	if !mockDB.ConfirmBlobCalled {
		t.Error("expected ConfirmBlob to be called")
	}
}

func TestHandler_BlobNotFound_Skips(t *testing.T) {
	mockStorage := &MockStorage{}
	mockDB := &MockDB{GetBlobStatusResult: ""} // Not found (may be traditional upload)

	deps = &Dependencies{
		Storage: mockStorage,
		DB:      mockDB,
	}

	event := events.S3Event{
		Records: []events.S3EventRecord{
			{
				S3: events.S3Entity{
					Bucket: events.S3Bucket{Name: "test-bucket"},
					Object: events.S3Object{Key: "account-123/blob-456"},
				},
			},
		},
	}

	err := handler(context.Background(), event)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Should NOT delete S3 object - this may be a traditional upload that has no status field
	// Traditional uploads are already confirmed synchronously by blob-upload
	if mockStorage.DeleteObjectCalled {
		t.Error("expected DeleteObject NOT to be called when blob record not found")
	}

	// Should NOT call ConfirmTag
	if mockStorage.ConfirmTagCalled {
		t.Error("expected ConfirmTag NOT to be called when blob record not found")
	}

	// Should NOT call ConfirmBlob
	if mockDB.ConfirmBlobCalled {
		t.Error("expected ConfirmBlob NOT to be called when blob record not found")
	}
}

func TestHandler_AlreadyConfirmed_Idempotent(t *testing.T) {
	mockStorage := &MockStorage{}
	mockDB := &MockDB{GetBlobStatusResult: "confirmed"}

	deps = &Dependencies{
		Storage: mockStorage,
		DB:      mockDB,
	}

	event := events.S3Event{
		Records: []events.S3EventRecord{
			{
				S3: events.S3Entity{
					Bucket: events.S3Bucket{Name: "test-bucket"},
					Object: events.S3Object{Key: "account-123/blob-456"},
				},
			},
		},
	}

	err := handler(context.Background(), event)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Should NOT call ConfirmBlob or ConfirmTag (already confirmed)
	if mockDB.ConfirmBlobCalled {
		t.Error("expected ConfirmBlob NOT to be called when already confirmed")
	}
	if mockStorage.ConfirmTagCalled {
		t.Error("expected ConfirmTag NOT to be called when already confirmed")
	}
}

func TestHandler_ConfirmTagFails_ReturnsError(t *testing.T) {
	mockStorage := &MockStorage{ConfirmTagErr: errors.New("S3 error")}
	mockDB := &MockDB{GetBlobStatusResult: "pending"}

	deps = &Dependencies{
		Storage: mockStorage,
		DB:      mockDB,
	}

	event := events.S3Event{
		Records: []events.S3EventRecord{
			{
				S3: events.S3Entity{
					Bucket: events.S3Bucket{Name: "test-bucket"},
					Object: events.S3Object{Key: "account-123/blob-456"},
				},
			},
		},
	}

	err := handler(context.Background(), event)
	if err == nil {
		t.Fatal("expected error when S3 fails")
	}

	// Should NOT call ConfirmBlob when tag update fails
	if mockDB.ConfirmBlobCalled {
		t.Error("expected ConfirmBlob NOT to be called when S3 tag update fails")
	}
}

func TestHandler_ConfirmBlobFails_ReturnsError(t *testing.T) {
	mockStorage := &MockStorage{}
	mockDB := &MockDB{
		GetBlobStatusResult: "pending",
		ConfirmBlobErr:      errors.New("DynamoDB error"),
	}

	deps = &Dependencies{
		Storage: mockStorage,
		DB:      mockDB,
	}

	event := events.S3Event{
		Records: []events.S3EventRecord{
			{
				S3: events.S3Entity{
					Bucket: events.S3Bucket{Name: "test-bucket"},
					Object: events.S3Object{Key: "account-123/blob-456"},
				},
			},
		},
	}

	err := handler(context.Background(), event)
	if err == nil {
		t.Fatal("expected error when DynamoDB fails")
	}
}

func TestHandler_InvalidKeyFormat_ReturnsError(t *testing.T) {
	mockStorage := &MockStorage{}
	mockDB := &MockDB{}

	deps = &Dependencies{
		Storage: mockStorage,
		DB:      mockDB,
	}

	event := events.S3Event{
		Records: []events.S3EventRecord{
			{
				S3: events.S3Entity{
					Bucket: events.S3Bucket{Name: "test-bucket"},
					Object: events.S3Object{Key: "invalid-key-no-slash"},
				},
			},
		},
	}

	err := handler(context.Background(), event)
	if err == nil {
		t.Fatal("expected error for invalid key format")
	}
}

func TestParseS3Key(t *testing.T) {
	tests := []struct {
		name      string
		key       string
		accountID string
		blobID    string
		valid     bool
	}{
		{"valid", "account-123/blob-456", "account-123", "blob-456", true},
		{"no slash", "invalid", "", "", false},
		{"trailing slash", "account-123/", "", "", false},
		{"leading slash", "/blob-456", "", "", false},
		{"empty", "", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			accountID, blobID, err := parseS3Key(tt.key)
			if tt.valid {
				if err != nil {
					t.Errorf("expected valid, got error: %v", err)
				}
				if accountID != tt.accountID {
					t.Errorf("expected accountID %s, got %s", tt.accountID, accountID)
				}
				if blobID != tt.blobID {
					t.Errorf("expected blobID %s, got %s", tt.blobID, blobID)
				}
			} else {
				if err == nil {
					t.Error("expected error for invalid key")
				}
			}
		})
	}
}
