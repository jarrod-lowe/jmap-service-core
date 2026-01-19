package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"

	"github.com/aws/aws-lambda-go/events"
)

// Mock implementations of interfaces for testing

type mockBlobStorage struct {
	uploadFunc    func(ctx context.Context, req UploadRequest) error
	confirmFunc   func(ctx context.Context, accountID, blobID string) error
	uploadErr     error
	confirmErr    error
	uploadedReqs  []UploadRequest
	confirmedIDs  []string
}

func (m *mockBlobStorage) Upload(ctx context.Context, req UploadRequest) error {
	m.uploadedReqs = append(m.uploadedReqs, req)
	if m.uploadFunc != nil {
		return m.uploadFunc(ctx, req)
	}
	return m.uploadErr
}

func (m *mockBlobStorage) ConfirmUpload(ctx context.Context, accountID, blobID string) error {
	m.confirmedIDs = append(m.confirmedIDs, blobID)
	if m.confirmFunc != nil {
		return m.confirmFunc(ctx, accountID, blobID)
	}
	return m.confirmErr
}

type mockBlobDB struct {
	createFunc   func(ctx context.Context, record BlobRecord) error
	createErr    error
	createdRecs  []BlobRecord
}

func (m *mockBlobDB) CreateBlobRecord(ctx context.Context, record BlobRecord) error {
	m.createdRecs = append(m.createdRecs, record)
	if m.createFunc != nil {
		return m.createFunc(ctx, record)
	}
	return m.createErr
}

type mockUUIDGenerator struct {
	nextID string
}

func (m *mockUUIDGenerator) Generate() string {
	return m.nextID
}

func setupTestDeps(storage *mockBlobStorage, db *mockBlobDB, uuidGen *mockUUIDGenerator) {
	deps = &Dependencies{
		Storage:   storage,
		DB:        db,
		UUIDGen:   uuidGen,
	}
}

// Test 1: Valid upload returns RFC 8620 response
func TestHandler_ValidUpload_ReturnsSuccess(t *testing.T) {
	storage := &mockBlobStorage{}
	db := &mockBlobDB{}
	uuidGen := &mockUUIDGenerator{nextID: "test-uuid-1234"}
	setupTestDeps(storage, db, uuidGen)

	body := []byte("test email content")
	request := events.APIGatewayProxyRequest{
		Body:            base64.StdEncoding.EncodeToString(body),
		IsBase64Encoded: true,
		Headers: map[string]string{
			"Content-Type": "message/rfc822",
		},
		PathParameters: map[string]string{
			"accountId": "user-123",
		},
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "req-abc",
		},
	}

	ctx := context.Background()
	response, err := handler(ctx, request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if response.StatusCode != 201 {
		t.Errorf("expected status code 201, got %d. Body: %s", response.StatusCode, response.Body)
	}

	var result BlobUploadResponse
	if err := json.Unmarshal([]byte(response.Body), &result); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if result.AccountID != "user-123" {
		t.Errorf("expected accountId 'user-123', got '%s'", result.AccountID)
	}
	if result.BlobID != "test-uuid-1234" {
		t.Errorf("expected blobId 'test-uuid-1234', got '%s'", result.BlobID)
	}
	if result.Type != "message/rfc822" {
		t.Errorf("expected type 'message/rfc822', got '%s'", result.Type)
	}
	if result.Size != int64(len(body)) {
		t.Errorf("expected size %d, got %d", len(body), result.Size)
	}
}

// Test 2: Missing Content-Type returns 400
func TestHandler_MissingContentType_Returns400(t *testing.T) {
	storage := &mockBlobStorage{}
	db := &mockBlobDB{}
	uuidGen := &mockUUIDGenerator{nextID: "test-uuid"}
	setupTestDeps(storage, db, uuidGen)

	request := events.APIGatewayProxyRequest{
		Body:            base64.StdEncoding.EncodeToString([]byte("content")),
		IsBase64Encoded: true,
		Headers:         map[string]string{}, // No Content-Type
		PathParameters: map[string]string{
			"accountId": "user-123",
		},
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "req-abc",
		},
	}

	ctx := context.Background()
	response, err := handler(ctx, request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if response.StatusCode != 400 {
		t.Errorf("expected status code 400, got %d", response.StatusCode)
	}

	var errResp ErrorResponse
	if err := json.Unmarshal([]byte(response.Body), &errResp); err != nil {
		t.Fatalf("failed to unmarshal error response: %v", err)
	}

	if errResp.Type != "invalidArguments" {
		t.Errorf("expected error type 'invalidArguments', got '%s'", errResp.Type)
	}
}

// Test 3: Missing accountId returns 401
func TestHandler_MissingAccountID_Returns401(t *testing.T) {
	storage := &mockBlobStorage{}
	db := &mockBlobDB{}
	uuidGen := &mockUUIDGenerator{nextID: "test-uuid"}
	setupTestDeps(storage, db, uuidGen)

	request := events.APIGatewayProxyRequest{
		Body:            base64.StdEncoding.EncodeToString([]byte("content")),
		IsBase64Encoded: true,
		Headers: map[string]string{
			"Content-Type": "message/rfc822",
		},
		PathParameters: map[string]string{}, // No accountId
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "req-abc",
		},
	}

	ctx := context.Background()
	response, err := handler(ctx, request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if response.StatusCode != 401 {
		t.Errorf("expected status code 401, got %d", response.StatusCode)
	}
}

// Test 4: S3 upload failure returns 500
func TestHandler_S3UploadFailure_Returns500(t *testing.T) {
	storage := &mockBlobStorage{uploadErr: errors.New("S3 error")}
	db := &mockBlobDB{}
	uuidGen := &mockUUIDGenerator{nextID: "test-uuid"}
	setupTestDeps(storage, db, uuidGen)

	request := events.APIGatewayProxyRequest{
		Body:            base64.StdEncoding.EncodeToString([]byte("content")),
		IsBase64Encoded: true,
		Headers: map[string]string{
			"Content-Type": "message/rfc822",
		},
		PathParameters: map[string]string{
			"accountId": "user-123",
		},
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "req-abc",
		},
	}

	ctx := context.Background()
	response, err := handler(ctx, request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if response.StatusCode != 500 {
		t.Errorf("expected status code 500, got %d", response.StatusCode)
	}

	var errResp ErrorResponse
	if err := json.Unmarshal([]byte(response.Body), &errResp); err != nil {
		t.Fatalf("failed to unmarshal error response: %v", err)
	}

	if errResp.Type != "serverFail" {
		t.Errorf("expected error type 'serverFail', got '%s'", errResp.Type)
	}
}

// Test 5: DynamoDB failure returns 500
func TestHandler_DynamoDBFailure_Returns500(t *testing.T) {
	storage := &mockBlobStorage{}
	db := &mockBlobDB{createErr: errors.New("DynamoDB error")}
	uuidGen := &mockUUIDGenerator{nextID: "test-uuid"}
	setupTestDeps(storage, db, uuidGen)

	request := events.APIGatewayProxyRequest{
		Body:            base64.StdEncoding.EncodeToString([]byte("content")),
		IsBase64Encoded: true,
		Headers: map[string]string{
			"Content-Type": "message/rfc822",
		},
		PathParameters: map[string]string{
			"accountId": "user-123",
		},
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "req-abc",
		},
	}

	ctx := context.Background()
	response, err := handler(ctx, request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if response.StatusCode != 500 {
		t.Errorf("expected status code 500, got %d", response.StatusCode)
	}
}

// Test 6: Upload creates correct S3 key structure
func TestHandler_UploadCreatesCorrectS3Key(t *testing.T) {
	storage := &mockBlobStorage{}
	db := &mockBlobDB{}
	uuidGen := &mockUUIDGenerator{nextID: "blob-id-xyz"}
	setupTestDeps(storage, db, uuidGen)

	body := []byte("email content")
	request := events.APIGatewayProxyRequest{
		Body:            base64.StdEncoding.EncodeToString(body),
		IsBase64Encoded: true,
		Headers: map[string]string{
			"Content-Type": "message/rfc822",
		},
		PathParameters: map[string]string{
			"accountId": "account-456",
		},
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "req-abc",
		},
	}

	ctx := context.Background()
	_, err := handler(ctx, request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if len(storage.uploadedReqs) != 1 {
		t.Fatalf("expected 1 upload, got %d", len(storage.uploadedReqs))
	}

	uploadReq := storage.uploadedReqs[0]
	expectedKey := "account-456/blob-id-xyz"
	if uploadReq.Key != expectedKey {
		t.Errorf("expected S3 key '%s', got '%s'", expectedKey, uploadReq.Key)
	}
	if uploadReq.ContentType != "message/rfc822" {
		t.Errorf("expected content type 'message/rfc822', got '%s'", uploadReq.ContentType)
	}
	if string(uploadReq.Body) != string(body) {
		t.Errorf("body mismatch")
	}
}

// Test 7: DynamoDB record has correct structure
func TestHandler_CreatesDynamoDBRecordCorrectly(t *testing.T) {
	storage := &mockBlobStorage{}
	db := &mockBlobDB{}
	uuidGen := &mockUUIDGenerator{nextID: "blob-id-999"}
	setupTestDeps(storage, db, uuidGen)

	body := []byte("test content 123")
	request := events.APIGatewayProxyRequest{
		Body:            base64.StdEncoding.EncodeToString(body),
		IsBase64Encoded: true,
		Headers: map[string]string{
			"Content-Type": "application/octet-stream",
		},
		PathParameters: map[string]string{
			"accountId": "user-abc",
		},
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "req-xyz",
		},
	}

	ctx := context.Background()
	_, err := handler(ctx, request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if len(db.createdRecs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(db.createdRecs))
	}

	record := db.createdRecs[0]
	if record.BlobID != "blob-id-999" {
		t.Errorf("expected blobId 'blob-id-999', got '%s'", record.BlobID)
	}
	if record.AccountID != "user-abc" {
		t.Errorf("expected accountId 'user-abc', got '%s'", record.AccountID)
	}
	if record.ContentType != "application/octet-stream" {
		t.Errorf("expected contentType 'application/octet-stream', got '%s'", record.ContentType)
	}
	if record.Size != int64(len(body)) {
		t.Errorf("expected size %d, got %d", len(body), record.Size)
	}
	if record.S3Key != "user-abc/blob-id-999" {
		t.Errorf("expected s3Key 'user-abc/blob-id-999', got '%s'", record.S3Key)
	}
}

// Test 8: Confirms upload after DynamoDB write
func TestHandler_ConfirmsUploadAfterDBWrite(t *testing.T) {
	storage := &mockBlobStorage{}
	db := &mockBlobDB{}
	uuidGen := &mockUUIDGenerator{nextID: "blob-to-confirm"}
	setupTestDeps(storage, db, uuidGen)

	request := events.APIGatewayProxyRequest{
		Body:            base64.StdEncoding.EncodeToString([]byte("content")),
		IsBase64Encoded: true,
		Headers: map[string]string{
			"Content-Type": "message/rfc822",
		},
		PathParameters: map[string]string{
			"accountId": "user-123",
		},
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "req-abc",
		},
	}

	ctx := context.Background()
	_, err := handler(ctx, request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if len(storage.confirmedIDs) != 1 {
		t.Fatalf("expected 1 confirmation, got %d", len(storage.confirmedIDs))
	}

	if storage.confirmedIDs[0] != "blob-to-confirm" {
		t.Errorf("expected confirmed blobId 'blob-to-confirm', got '%s'", storage.confirmedIDs[0])
	}
}

// Test 9: Non-base64 body is handled
func TestHandler_NonBase64Body_HandlesCorrectly(t *testing.T) {
	storage := &mockBlobStorage{}
	db := &mockBlobDB{}
	uuidGen := &mockUUIDGenerator{nextID: "test-uuid"}
	setupTestDeps(storage, db, uuidGen)

	body := "plain text body"
	request := events.APIGatewayProxyRequest{
		Body:            body,
		IsBase64Encoded: false, // Not base64 encoded
		Headers: map[string]string{
			"Content-Type": "text/plain",
		},
		PathParameters: map[string]string{
			"accountId": "user-123",
		},
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "req-abc",
		},
	}

	ctx := context.Background()
	response, err := handler(ctx, request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if response.StatusCode != 201 {
		t.Errorf("expected status code 201, got %d. Body: %s", response.StatusCode, response.Body)
	}

	var result BlobUploadResponse
	if err := json.Unmarshal([]byte(response.Body), &result); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if result.Size != int64(len(body)) {
		t.Errorf("expected size %d, got %d", len(body), result.Size)
	}
}

// Test 10: Extracts accountId from JWT claims when path param absent
func TestHandler_ExtractsAccountIDFromJWT(t *testing.T) {
	storage := &mockBlobStorage{}
	db := &mockBlobDB{}
	uuidGen := &mockUUIDGenerator{nextID: "test-uuid"}
	setupTestDeps(storage, db, uuidGen)

	request := events.APIGatewayProxyRequest{
		Body:            base64.StdEncoding.EncodeToString([]byte("content")),
		IsBase64Encoded: true,
		Headers: map[string]string{
			"Content-Type": "message/rfc822",
		},
		PathParameters: map[string]string{}, // No path param
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "req-abc",
			Authorizer: map[string]any{
				"claims": map[string]any{
					"sub": "jwt-user-456",
				},
			},
		},
	}

	ctx := context.Background()
	response, err := handler(ctx, request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if response.StatusCode != 201 {
		t.Errorf("expected status code 201, got %d. Body: %s", response.StatusCode, response.Body)
	}

	var result BlobUploadResponse
	if err := json.Unmarshal([]byte(response.Body), &result); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if result.AccountID != "jwt-user-456" {
		t.Errorf("expected accountId 'jwt-user-456', got '%s'", result.AccountID)
	}
}

// Test 11: Response Content-Type is application/json
func TestHandler_ResponseContentType(t *testing.T) {
	storage := &mockBlobStorage{}
	db := &mockBlobDB{}
	uuidGen := &mockUUIDGenerator{nextID: "test-uuid"}
	setupTestDeps(storage, db, uuidGen)

	request := events.APIGatewayProxyRequest{
		Body:            base64.StdEncoding.EncodeToString([]byte("content")),
		IsBase64Encoded: true,
		Headers: map[string]string{
			"Content-Type": "message/rfc822",
		},
		PathParameters: map[string]string{
			"accountId": "user-123",
		},
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "req-abc",
		},
	}

	ctx := context.Background()
	response, err := handler(ctx, request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if response.Headers["Content-Type"] != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got '%s'", response.Headers["Content-Type"])
	}
}
