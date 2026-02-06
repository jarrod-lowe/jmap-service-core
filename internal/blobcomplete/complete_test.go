package blobcomplete

import (
	"context"
	"fmt"
	"testing"

	"github.com/jarrod-lowe/jmap-service-core/internal/bloballocate"
)

// mockStorage implements Storage for testing
type mockStorage struct {
	completeFunc func(ctx context.Context, accountID, blobID, uploadID string, parts []bloballocate.CompletedPart) error
}

func (m *mockStorage) CompleteMultipartUpload(ctx context.Context, accountID, blobID, uploadID string, parts []bloballocate.CompletedPart) error {
	if m.completeFunc != nil {
		return m.completeFunc(ctx, accountID, blobID, uploadID, parts)
	}
	return nil
}

// mockDB implements DB for testing
type mockDB struct {
	getBlobFunc func(ctx context.Context, accountID, blobID string) (*BlobRecord, error)
}

func (m *mockDB) GetBlobForComplete(ctx context.Context, accountID, blobID string) (*BlobRecord, error) {
	if m.getBlobFunc != nil {
		return m.getBlobFunc(ctx, accountID, blobID)
	}
	return nil, nil
}

func TestComplete_Success(t *testing.T) {
	var capturedParts []bloballocate.CompletedPart
	var capturedUploadID string

	db := &mockDB{
		getBlobFunc: func(ctx context.Context, accountID, blobID string) (*BlobRecord, error) {
			return &BlobRecord{
				Status:    "pending",
				Multipart: true,
				UploadID:  "upload-abc",
			}, nil
		},
	}
	storage := &mockStorage{
		completeFunc: func(ctx context.Context, accountID, blobID, uploadID string, parts []bloballocate.CompletedPart) error {
			capturedUploadID = uploadID
			capturedParts = parts
			return nil
		},
	}

	h := &Handler{Storage: storage, DB: db}

	req := CompleteRequest{
		AccountID: "account-1",
		BlobID:    "blob-1",
		Parts: []bloballocate.CompletedPart{
			{PartNumber: 1, ETag: "\"etag1\""},
			{PartNumber: 2, ETag: "\"etag2\""},
		},
	}

	resp, err := h.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.AccountID != "account-1" {
		t.Errorf("expected accountId 'account-1', got %q", resp.AccountID)
	}
	if resp.BlobID != "blob-1" {
		t.Errorf("expected blobId 'blob-1', got %q", resp.BlobID)
	}
	if capturedUploadID != "upload-abc" {
		t.Errorf("expected uploadID 'upload-abc', got %q", capturedUploadID)
	}
	if len(capturedParts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(capturedParts))
	}
}

func TestComplete_BlobNotFound(t *testing.T) {
	db := &mockDB{
		getBlobFunc: func(ctx context.Context, accountID, blobID string) (*BlobRecord, error) {
			return nil, nil // not found
		},
	}
	storage := &mockStorage{}

	h := &Handler{Storage: storage, DB: db}
	req := CompleteRequest{
		AccountID: "account-1",
		BlobID:    "blob-missing",
		Parts:     []bloballocate.CompletedPart{{PartNumber: 1, ETag: "e1"}},
	}

	_, err := h.Complete(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for blob not found")
	}
	compErr, ok := err.(*CompleteError)
	if !ok {
		t.Fatalf("expected CompleteError, got %T", err)
	}
	if compErr.Type != "blobNotFound" {
		t.Errorf("expected type 'blobNotFound', got %q", compErr.Type)
	}
}

func TestComplete_BlobNotPending(t *testing.T) {
	db := &mockDB{
		getBlobFunc: func(ctx context.Context, accountID, blobID string) (*BlobRecord, error) {
			return &BlobRecord{
				Status:    "confirmed",
				Multipart: true,
				UploadID:  "upload-abc",
			}, nil
		},
	}
	storage := &mockStorage{}

	h := &Handler{Storage: storage, DB: db}
	req := CompleteRequest{
		AccountID: "account-1",
		BlobID:    "blob-1",
		Parts:     []bloballocate.CompletedPart{{PartNumber: 1, ETag: "e1"}},
	}

	_, err := h.Complete(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for non-pending blob")
	}
	compErr, ok := err.(*CompleteError)
	if !ok {
		t.Fatalf("expected CompleteError, got %T", err)
	}
	if compErr.Type != "invalidArguments" {
		t.Errorf("expected type 'invalidArguments', got %q", compErr.Type)
	}
}

func TestComplete_BlobNotMultipart(t *testing.T) {
	db := &mockDB{
		getBlobFunc: func(ctx context.Context, accountID, blobID string) (*BlobRecord, error) {
			return &BlobRecord{
				Status:    "pending",
				Multipart: false,
				UploadID:  "",
			}, nil
		},
	}
	storage := &mockStorage{}

	h := &Handler{Storage: storage, DB: db}
	req := CompleteRequest{
		AccountID: "account-1",
		BlobID:    "blob-1",
		Parts:     []bloballocate.CompletedPart{{PartNumber: 1, ETag: "e1"}},
	}

	_, err := h.Complete(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for non-multipart blob")
	}
	compErr, ok := err.(*CompleteError)
	if !ok {
		t.Fatalf("expected CompleteError, got %T", err)
	}
	if compErr.Type != "invalidArguments" {
		t.Errorf("expected type 'invalidArguments', got %q", compErr.Type)
	}
}

func TestComplete_MissingUploadId(t *testing.T) {
	db := &mockDB{
		getBlobFunc: func(ctx context.Context, accountID, blobID string) (*BlobRecord, error) {
			return &BlobRecord{
				Status:    "pending",
				Multipart: true,
				UploadID:  "", // missing
			}, nil
		},
	}
	storage := &mockStorage{}

	h := &Handler{Storage: storage, DB: db}
	req := CompleteRequest{
		AccountID: "account-1",
		BlobID:    "blob-1",
		Parts:     []bloballocate.CompletedPart{{PartNumber: 1, ETag: "e1"}},
	}

	_, err := h.Complete(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for missing uploadId")
	}
	compErr, ok := err.(*CompleteError)
	if !ok {
		t.Fatalf("expected CompleteError, got %T", err)
	}
	if compErr.Type != "serverFail" {
		t.Errorf("expected type 'serverFail', got %q", compErr.Type)
	}
}

func TestComplete_EmptyParts(t *testing.T) {
	h := &Handler{Storage: &mockStorage{}, DB: &mockDB{}}
	req := CompleteRequest{
		AccountID: "account-1",
		BlobID:    "blob-1",
		Parts:     []bloballocate.CompletedPart{}, // empty
	}

	_, err := h.Complete(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for empty parts")
	}
	compErr, ok := err.(*CompleteError)
	if !ok {
		t.Fatalf("expected CompleteError, got %T", err)
	}
	if compErr.Type != "invalidArguments" {
		t.Errorf("expected type 'invalidArguments', got %q", compErr.Type)
	}
}

func TestComplete_S3Error(t *testing.T) {
	db := &mockDB{
		getBlobFunc: func(ctx context.Context, accountID, blobID string) (*BlobRecord, error) {
			return &BlobRecord{
				Status:    "pending",
				Multipart: true,
				UploadID:  "upload-abc",
			}, nil
		},
	}
	storage := &mockStorage{
		completeFunc: func(ctx context.Context, accountID, blobID, uploadID string, parts []bloballocate.CompletedPart) error {
			return fmt.Errorf("S3 error")
		},
	}

	h := &Handler{Storage: storage, DB: db}
	req := CompleteRequest{
		AccountID: "account-1",
		BlobID:    "blob-1",
		Parts:     []bloballocate.CompletedPart{{PartNumber: 1, ETag: "e1"}},
	}

	_, err := h.Complete(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for S3 failure")
	}
	compErr, ok := err.(*CompleteError)
	if !ok {
		t.Fatalf("expected CompleteError, got %T", err)
	}
	if compErr.Type != "serverFail" {
		t.Errorf("expected type 'serverFail', got %q", compErr.Type)
	}
}

func TestComplete_DBGetError(t *testing.T) {
	db := &mockDB{
		getBlobFunc: func(ctx context.Context, accountID, blobID string) (*BlobRecord, error) {
			return nil, fmt.Errorf("DynamoDB error")
		},
	}
	storage := &mockStorage{}

	h := &Handler{Storage: storage, DB: db}
	req := CompleteRequest{
		AccountID: "account-1",
		BlobID:    "blob-1",
		Parts:     []bloballocate.CompletedPart{{PartNumber: 1, ETag: "e1"}},
	}

	_, err := h.Complete(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for DB failure")
	}
	compErr, ok := err.(*CompleteError)
	if !ok {
		t.Fatalf("expected CompleteError, got %T", err)
	}
	if compErr.Type != "serverFail" {
		t.Errorf("expected type 'serverFail', got %q", compErr.Type)
	}
}
