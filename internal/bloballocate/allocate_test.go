package bloballocate

import (
	"context"
	"errors"
	"testing"
	"time"
)

// MockStorage implements Storage for testing
type MockStorage struct {
	GeneratePresignedURLCalled bool
	GeneratePresignedURLInput  GenerateURLInput
	GeneratePresignedURLResult string
	GeneratePresignedURLErr    error
}

type GenerateURLInput struct {
	AccountID   string
	BlobID      string
	Size        int64
	ContentType string
	SizeUnknown bool
}

func (m *MockStorage) GeneratePresignedPutURL(ctx context.Context, accountID, blobID string, size int64, contentType string, urlExpirySecs int64, sizeUnknown bool) (string, time.Time, error) {
	m.GeneratePresignedURLCalled = true
	m.GeneratePresignedURLInput = GenerateURLInput{
		AccountID:   accountID,
		BlobID:      blobID,
		Size:        size,
		ContentType: contentType,
		SizeUnknown: sizeUnknown,
	}
	if m.GeneratePresignedURLErr != nil {
		return "", time.Time{}, m.GeneratePresignedURLErr
	}
	return m.GeneratePresignedURLResult, time.Now().Add(time.Duration(urlExpirySecs) * time.Second), nil
}

// MockDB implements DB for testing
type MockDB struct {
	AllocateCalled      bool
	AllocateInput       AllocateInput
	AllocateErr         error
	AllocateErrType     string // "tooManyPending", "overQuota", "accountNotProvisioned"
}

type AllocateInput struct {
	AccountID    string
	BlobID       string
	Size         int64
	ContentType  string
	URLExpiresAt time.Time
	SizeUnknown  bool
	UploadID     string
}

func (m *MockDB) AllocateBlob(ctx context.Context, accountID, blobID string, size int64, contentType string, urlExpiresAt time.Time, maxPending int, s3Key string, sizeUnknown bool, uploadID string) error {
	m.AllocateCalled = true
	m.AllocateInput = AllocateInput{
		AccountID:    accountID,
		BlobID:       blobID,
		Size:         size,
		ContentType:  contentType,
		URLExpiresAt: urlExpiresAt,
		SizeUnknown:  sizeUnknown,
		UploadID:     uploadID,
	}
	if m.AllocateErrType != "" {
		return &AllocationError{Type: m.AllocateErrType, Message: "test error"}
	}
	return m.AllocateErr
}

// MockUUIDGen implements UUIDGenerator for testing
type MockUUIDGen struct {
	GenerateResult string
}

func (m *MockUUIDGen) Generate() string {
	return m.GenerateResult
}

func TestAllocate_Success(t *testing.T) {
	mockStorage := &MockStorage{
		GeneratePresignedURLResult: "https://bucket.s3.amazonaws.com/signed-url",
	}
	mockDB := &MockDB{}
	mockUUID := &MockUUIDGen{GenerateResult: "blob-123"}

	handler := &Handler{
		Storage:           mockStorage,
		DB:                mockDB,
		UUIDGen:           mockUUID,
		MaxSizeUploadPut:  250000000,
		MaxPendingAllocs:  4,
		URLExpirySecs:     900,
	}

	req := AllocateRequest{
		AccountID: "account-123",
		Type:      "application/pdf",
		Size:      1024,
	}

	resp, err := handler.Allocate(context.Background(), req)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if resp.AccountID != "account-123" {
		t.Errorf("expected accountId account-123, got %s", resp.AccountID)
	}
	if resp.BlobID != "blob-123" {
		t.Errorf("expected blobId blob-123, got %s", resp.BlobID)
	}
	if resp.URL != "https://bucket.s3.amazonaws.com/signed-url" {
		t.Errorf("expected URL to match, got %s", resp.URL)
	}
	if resp.URLExpires.IsZero() {
		t.Error("expected URLExpires to be set")
	}

	// Verify DB was called
	if !mockDB.AllocateCalled {
		t.Fatal("expected DB.AllocateBlob to be called")
	}
	if mockDB.AllocateInput.AccountID != "account-123" {
		t.Errorf("expected accountId account-123, got %s", mockDB.AllocateInput.AccountID)
	}
	if mockDB.AllocateInput.Size != 1024 {
		t.Errorf("expected size 1024, got %d", mockDB.AllocateInput.Size)
	}

	// Verify Storage was called
	if !mockStorage.GeneratePresignedURLCalled {
		t.Fatal("expected Storage.GeneratePresignedPutURL to be called")
	}
}

func TestAllocate_TooLarge(t *testing.T) {
	handler := &Handler{
		MaxSizeUploadPut: 1000,
		MaxPendingAllocs: 4,
		URLExpirySecs:    900,
	}

	req := AllocateRequest{
		AccountID: "account-123",
		Type:      "application/pdf",
		Size:      2000, // exceeds MaxSizeUploadPut
	}

	_, err := handler.Allocate(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for too large size")
	}

	allocErr, ok := err.(*AllocationError)
	if !ok {
		t.Fatalf("expected AllocationError, got %T", err)
	}
	if allocErr.Type != "tooLarge" {
		t.Errorf("expected type tooLarge, got %s", allocErr.Type)
	}
}

func TestAllocate_InvalidType(t *testing.T) {
	handler := &Handler{
		MaxSizeUploadPut: 250000000,
		MaxPendingAllocs: 4,
		URLExpirySecs:    900,
	}

	req := AllocateRequest{
		AccountID: "account-123",
		Type:      "invalid", // not a valid MIME type
		Size:      1024,
	}

	_, err := handler.Allocate(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for invalid type")
	}

	allocErr, ok := err.(*AllocationError)
	if !ok {
		t.Fatalf("expected AllocationError, got %T", err)
	}
	if allocErr.Type != "invalidProperties" {
		t.Errorf("expected type invalidProperties, got %s", allocErr.Type)
	}
	if len(allocErr.Properties) != 1 || allocErr.Properties[0] != "type" {
		t.Errorf("expected properties [type], got %v", allocErr.Properties)
	}
}

func TestAllocate_MissingSize(t *testing.T) {
	handler := &Handler{
		MaxSizeUploadPut: 250000000,
		MaxPendingAllocs: 4,
		URLExpirySecs:    900,
	}

	req := AllocateRequest{
		AccountID: "account-123",
		Type:      "application/pdf",
		Size:      0, // size must be > 0
	}

	_, err := handler.Allocate(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for missing size")
	}

	allocErr, ok := err.(*AllocationError)
	if !ok {
		t.Fatalf("expected AllocationError, got %T", err)
	}
	if allocErr.Type != "invalidArguments" {
		t.Errorf("expected type invalidArguments, got %s", allocErr.Type)
	}
}

func TestAllocate_TooManyPending(t *testing.T) {
	mockStorage := &MockStorage{
		GeneratePresignedURLResult: "https://bucket.s3.amazonaws.com/signed-url",
	}
	mockDB := &MockDB{
		AllocateErrType: "tooManyPending",
	}
	mockUUID := &MockUUIDGen{GenerateResult: "blob-123"}

	handler := &Handler{
		Storage:          mockStorage,
		DB:               mockDB,
		UUIDGen:          mockUUID,
		MaxSizeUploadPut: 250000000,
		MaxPendingAllocs: 4,
		URLExpirySecs:    900,
	}

	req := AllocateRequest{
		AccountID: "account-123",
		Type:      "application/pdf",
		Size:      1024,
	}

	_, err := handler.Allocate(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for too many pending")
	}

	allocErr, ok := err.(*AllocationError)
	if !ok {
		t.Fatalf("expected AllocationError, got %T", err)
	}
	if allocErr.Type != "tooManyPending" {
		t.Errorf("expected type tooManyPending, got %s", allocErr.Type)
	}
}

func TestAllocate_OverQuota(t *testing.T) {
	mockStorage := &MockStorage{
		GeneratePresignedURLResult: "https://bucket.s3.amazonaws.com/signed-url",
	}
	mockDB := &MockDB{
		AllocateErrType: "overQuota",
	}
	mockUUID := &MockUUIDGen{GenerateResult: "blob-123"}

	handler := &Handler{
		Storage:          mockStorage,
		DB:               mockDB,
		UUIDGen:          mockUUID,
		MaxSizeUploadPut: 250000000,
		MaxPendingAllocs: 4,
		URLExpirySecs:    900,
	}

	req := AllocateRequest{
		AccountID: "account-123",
		Type:      "application/pdf",
		Size:      1024,
	}

	_, err := handler.Allocate(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for over quota")
	}

	allocErr, ok := err.(*AllocationError)
	if !ok {
		t.Fatalf("expected AllocationError, got %T", err)
	}
	if allocErr.Type != "overQuota" {
		t.Errorf("expected type overQuota, got %s", allocErr.Type)
	}
}

func TestAllocate_AccountNotProvisioned(t *testing.T) {
	mockStorage := &MockStorage{
		GeneratePresignedURLResult: "https://bucket.s3.amazonaws.com/signed-url",
	}
	mockDB := &MockDB{
		AllocateErrType: "accountNotProvisioned",
	}
	mockUUID := &MockUUIDGen{GenerateResult: "blob-123"}

	handler := &Handler{
		Storage:          mockStorage,
		DB:               mockDB,
		UUIDGen:          mockUUID,
		MaxSizeUploadPut: 250000000,
		MaxPendingAllocs: 4,
		URLExpirySecs:    900,
	}

	req := AllocateRequest{
		AccountID: "account-123",
		Type:      "application/pdf",
		Size:      1024,
	}

	_, err := handler.Allocate(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for account not provisioned")
	}

	allocErr, ok := err.(*AllocationError)
	if !ok {
		t.Fatalf("expected AllocationError, got %T", err)
	}
	if allocErr.Type != "accountNotProvisioned" {
		t.Errorf("expected type accountNotProvisioned, got %s", allocErr.Type)
	}
}

func TestAllocate_StorageError(t *testing.T) {
	mockStorage := &MockStorage{
		GeneratePresignedURLErr: errors.New("S3 error"),
	}
	mockDB := &MockDB{}
	mockUUID := &MockUUIDGen{GenerateResult: "blob-123"}

	handler := &Handler{
		Storage:          mockStorage,
		DB:               mockDB,
		UUIDGen:          mockUUID,
		MaxSizeUploadPut: 250000000,
		MaxPendingAllocs: 4,
		URLExpirySecs:    900,
	}

	req := AllocateRequest{
		AccountID: "account-123",
		Type:      "application/pdf",
		Size:      1024,
	}

	_, err := handler.Allocate(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for storage failure")
	}

	// Storage errors should be wrapped, not AllocationErrors
	allocErr, ok := err.(*AllocationError)
	if !ok {
		t.Fatalf("expected AllocationError, got %T", err)
	}
	if allocErr.Type != "serverFail" {
		t.Errorf("expected type serverFail, got %s", allocErr.Type)
	}
}

func TestAllocate_SizeUnknown_Success(t *testing.T) {
	mockStorage := &MockStorage{
		GeneratePresignedURLResult: "https://bucket.s3.amazonaws.com/signed-url",
	}
	mockDB := &MockDB{}
	mockUUID := &MockUUIDGen{GenerateResult: "blob-unknown-size"}

	handler := &Handler{
		Storage:          mockStorage,
		DB:               mockDB,
		UUIDGen:          mockUUID,
		MaxSizeUploadPut: 250000000,
		MaxPendingAllocs: 4,
		URLExpirySecs:    900,
	}

	req := AllocateRequest{
		AccountID:   "account-123",
		Type:        "message/rfc822",
		Size:        0,
		SizeUnknown: true,
	}

	resp, err := handler.Allocate(context.Background(), req)
	if err != nil {
		t.Fatalf("expected no error for SizeUnknown=true with size=0, got %v", err)
	}

	if resp.BlobID != "blob-unknown-size" {
		t.Errorf("expected blobId blob-unknown-size, got %s", resp.BlobID)
	}
	if resp.Size != 0 {
		t.Errorf("expected size 0, got %d", resp.Size)
	}
}

func TestAllocate_SizeUnknown_StillValidatesType(t *testing.T) {
	handler := &Handler{
		MaxSizeUploadPut: 250000000,
		MaxPendingAllocs: 4,
		URLExpirySecs:    900,
	}

	req := AllocateRequest{
		AccountID:   "account-123",
		Type:        "invalid",
		Size:        0,
		SizeUnknown: true,
	}

	_, err := handler.Allocate(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for invalid type even with SizeUnknown=true")
	}

	allocErr, ok := err.(*AllocationError)
	if !ok {
		t.Fatalf("expected AllocationError, got %T", err)
	}
	if allocErr.Type != "invalidProperties" {
		t.Errorf("expected type invalidProperties, got %s", allocErr.Type)
	}
}

func TestAllocate_SizeUnknown_PassesFlagToDB(t *testing.T) {
	mockStorage := &MockStorage{
		GeneratePresignedURLResult: "https://bucket.s3.amazonaws.com/signed-url",
	}
	mockDB := &MockDB{}
	mockUUID := &MockUUIDGen{GenerateResult: "blob-flag-test"}

	handler := &Handler{
		Storage:          mockStorage,
		DB:               mockDB,
		UUIDGen:          mockUUID,
		MaxSizeUploadPut: 250000000,
		MaxPendingAllocs: 4,
		URLExpirySecs:    900,
	}

	req := AllocateRequest{
		AccountID:   "account-123",
		Type:        "application/octet-stream",
		Size:        0,
		SizeUnknown: true,
	}

	_, err := handler.Allocate(context.Background(), req)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !mockDB.AllocateInput.SizeUnknown {
		t.Error("expected SizeUnknown=true to be passed to DB.AllocateBlob")
	}
	if !mockStorage.GeneratePresignedURLInput.SizeUnknown {
		t.Error("expected SizeUnknown=true to be passed to Storage.GeneratePresignedPutURL")
	}
}

func TestAllocate_SizeUnknown_CognitoStillRequiresSize(t *testing.T) {
	handler := &Handler{
		MaxSizeUploadPut: 250000000,
		MaxPendingAllocs: 4,
		URLExpirySecs:    900,
	}

	// SizeUnknown=false (Cognito path) with size=0 should still fail
	req := AllocateRequest{
		AccountID:   "account-123",
		Type:        "application/pdf",
		Size:        0,
		SizeUnknown: false,
	}

	_, err := handler.Allocate(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for size=0 with SizeUnknown=false")
	}

	allocErr, ok := err.(*AllocationError)
	if !ok {
		t.Fatalf("expected AllocationError, got %T", err)
	}
	if allocErr.Type != "invalidArguments" {
		t.Errorf("expected type invalidArguments, got %s", allocErr.Type)
	}
}

// MockMultipartStorage implements MultipartStorage for testing
type MockMultipartStorage struct {
	CreateMultipartUploadCalled bool
	CreateMultipartUploadID     string
	CreateMultipartUploadErr    error
	GeneratePartURLsCalled      bool
	GeneratePartURLsResult      []PartURL
	GeneratePartURLsErr         error
}

func (m *MockMultipartStorage) CreateMultipartUpload(ctx context.Context, accountID, blobID, contentType string) (string, error) {
	m.CreateMultipartUploadCalled = true
	if m.CreateMultipartUploadErr != nil {
		return "", m.CreateMultipartUploadErr
	}
	return m.CreateMultipartUploadID, nil
}

func (m *MockMultipartStorage) GeneratePresignedPartURLs(ctx context.Context, accountID, blobID, uploadID string, partCount int, urlExpirySecs int64) ([]PartURL, time.Time, error) {
	m.GeneratePartURLsCalled = true
	if m.GeneratePartURLsErr != nil {
		return nil, time.Time{}, m.GeneratePartURLsErr
	}
	result := m.GeneratePartURLsResult
	if result == nil {
		// Generate default parts
		result = make([]PartURL, partCount)
		for i := 0; i < partCount; i++ {
			result[i] = PartURL{PartNumber: int32(i + 1), URL: "https://example.com/part"}
		}
	}
	return result, time.Now().Add(time.Duration(urlExpirySecs) * time.Second), nil
}

func TestAllocate_Multipart_Success(t *testing.T) {
	mockStorage := &MockStorage{}
	mockMultipart := &MockMultipartStorage{
		CreateMultipartUploadID: "upload-abc",
	}
	mockDB := &MockDB{}
	mockUUID := &MockUUIDGen{GenerateResult: "blob-mp-1"}

	handler := &Handler{
		Storage:            mockStorage,
		MultipartStorage:   mockMultipart,
		DB:                 mockDB,
		UUIDGen:            mockUUID,
		MaxSizeUploadPut:   250000000,
		MaxPendingAllocs:   4,
		URLExpirySecs:      900,
		MultipartPartCount: 5,
	}

	req := AllocateRequest{
		AccountID:   "account-1",
		Type:        "message/rfc822",
		SizeUnknown: true,
		Multipart:   true,
	}

	resp, err := handler.Allocate(context.Background(), req)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Should return parts, not a single URL
	if resp.URL != "" {
		t.Errorf("expected empty URL for multipart, got %q", resp.URL)
	}
	if len(resp.Parts) != 5 {
		t.Fatalf("expected 5 parts, got %d", len(resp.Parts))
	}
	if resp.BlobID != "blob-mp-1" {
		t.Errorf("expected blobId 'blob-mp-1', got %q", resp.BlobID)
	}
	if resp.Size != 0 {
		t.Errorf("expected size 0 for multipart, got %d", resp.Size)
	}

	// Verify multipart storage was called, not single-PUT storage
	if !mockMultipart.CreateMultipartUploadCalled {
		t.Error("expected CreateMultipartUpload to be called")
	}
	if !mockMultipart.GeneratePartURLsCalled {
		t.Error("expected GeneratePresignedPartURLs to be called")
	}
	if mockStorage.GeneratePresignedURLCalled {
		t.Error("expected single-PUT GeneratePresignedPutURL NOT to be called")
	}

	// Verify DB was called
	if !mockDB.AllocateCalled {
		t.Fatal("expected DB.AllocateBlob to be called")
	}
}

func TestAllocate_Multipart_RequiresSizeUnknown(t *testing.T) {
	handler := &Handler{
		MaxSizeUploadPut: 250000000,
		MaxPendingAllocs: 4,
		URLExpirySecs:    900,
	}

	req := AllocateRequest{
		AccountID:   "account-1",
		Type:        "message/rfc822",
		Size:        1024,
		SizeUnknown: false,
		Multipart:   true,
	}

	_, err := handler.Allocate(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for multipart without sizeUnknown")
	}

	allocErr, ok := err.(*AllocationError)
	if !ok {
		t.Fatalf("expected AllocationError, got %T", err)
	}
	if allocErr.Type != "invalidArguments" {
		t.Errorf("expected type invalidArguments, got %s", allocErr.Type)
	}
}

func TestAllocate_Multipart_CreateUploadError(t *testing.T) {
	mockMultipart := &MockMultipartStorage{
		CreateMultipartUploadErr: errors.New("S3 error"),
	}
	mockDB := &MockDB{}
	mockUUID := &MockUUIDGen{GenerateResult: "blob-err"}

	handler := &Handler{
		Storage:          &MockStorage{},
		MultipartStorage: mockMultipart,
		DB:               mockDB,
		UUIDGen:          mockUUID,
		MaxSizeUploadPut: 250000000,
		MaxPendingAllocs: 4,
		URLExpirySecs:    900,
	}

	req := AllocateRequest{
		AccountID:   "account-1",
		Type:        "message/rfc822",
		SizeUnknown: true,
		Multipart:   true,
	}

	_, err := handler.Allocate(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for create multipart upload failure")
	}

	allocErr, ok := err.(*AllocationError)
	if !ok {
		t.Fatalf("expected AllocationError, got %T", err)
	}
	if allocErr.Type != "serverFail" {
		t.Errorf("expected type serverFail, got %s", allocErr.Type)
	}
}

func TestAllocate_Multipart_GeneratePartURLsError(t *testing.T) {
	mockMultipart := &MockMultipartStorage{
		CreateMultipartUploadID: "upload-abc",
		GeneratePartURLsErr:    errors.New("presign error"),
	}
	mockDB := &MockDB{}
	mockUUID := &MockUUIDGen{GenerateResult: "blob-err"}

	handler := &Handler{
		Storage:          &MockStorage{},
		MultipartStorage: mockMultipart,
		DB:               mockDB,
		UUIDGen:          mockUUID,
		MaxSizeUploadPut: 250000000,
		MaxPendingAllocs: 4,
		URLExpirySecs:    900,
	}

	req := AllocateRequest{
		AccountID:   "account-1",
		Type:        "message/rfc822",
		SizeUnknown: true,
		Multipart:   true,
	}

	_, err := handler.Allocate(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for part URL generation failure")
	}

	allocErr, ok := err.(*AllocationError)
	if !ok {
		t.Fatalf("expected AllocationError, got %T", err)
	}
	if allocErr.Type != "serverFail" {
		t.Errorf("expected type serverFail, got %s", allocErr.Type)
	}
}

func TestAllocate_Multipart_DefaultPartCount(t *testing.T) {
	mockMultipart := &MockMultipartStorage{
		CreateMultipartUploadID: "upload-abc",
	}
	mockDB := &MockDB{}
	mockUUID := &MockUUIDGen{GenerateResult: "blob-default"}

	handler := &Handler{
		Storage:          &MockStorage{},
		MultipartStorage: mockMultipart,
		DB:               mockDB,
		UUIDGen:          mockUUID,
		MaxSizeUploadPut: 250000000,
		MaxPendingAllocs: 4,
		URLExpirySecs:    900,
		// MultipartPartCount not set — should use default
	}

	req := AllocateRequest{
		AccountID:   "account-1",
		Type:        "message/rfc822",
		SizeUnknown: true,
		Multipart:   true,
	}

	resp, err := handler.Allocate(context.Background(), req)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(resp.Parts) != DefaultMultipartPartCount {
		t.Errorf("expected %d parts (default), got %d", DefaultMultipartPartCount, len(resp.Parts))
	}
}

func TestValidateMediaType(t *testing.T) {
	tests := []struct {
		name      string
		mediaType string
		valid     bool
	}{
		{"valid simple", "application/pdf", true},
		{"valid with param", "text/plain; charset=utf-8", true},
		{"valid image", "image/png", true},
		{"valid multipart", "multipart/form-data", true},
		{"invalid no slash", "application", false},
		{"invalid empty", "", false},
		{"invalid just slash", "/", false},
		{"invalid leading slash", "/pdf", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidMediaType(tt.mediaType)
			if result != tt.valid {
				t.Errorf("isValidMediaType(%q) = %v, want %v", tt.mediaType, result, tt.valid)
			}
		})
	}
}
