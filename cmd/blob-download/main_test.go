package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"
)

// Mock implementations for testing

type mockBlobDB struct {
	getFunc func(ctx context.Context, accountID, blobID string) (*BlobRecord, error)
	getErr  error
	blob    *BlobRecord
}

func (m *mockBlobDB) GetBlob(ctx context.Context, accountID, blobID string) (*BlobRecord, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, accountID, blobID)
	}
	return m.blob, m.getErr
}

type mockURLSigner struct {
	signFunc  func(url string, expiry time.Time) (string, error)
	signErr   error
	signedURL string
	lastURL   string
	lastExpiry time.Time
}

func (m *mockURLSigner) Sign(url string, expiry time.Time) (string, error) {
	m.lastURL = url
	m.lastExpiry = expiry
	if m.signFunc != nil {
		return m.signFunc(url, expiry)
	}
	if m.signErr != nil {
		return "", m.signErr
	}
	return m.signedURL, nil
}

type mockSecretsReader struct {
	getFunc    func(ctx context.Context, secretARN string) (string, error)
	privateKey string
	getErr     error
}

func (m *mockSecretsReader) GetPrivateKey(ctx context.Context, secretARN string) (string, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, secretARN)
	}
	return m.privateKey, m.getErr
}

func setupTestDeps(db *mockBlobDB, signer *mockURLSigner, secrets *mockSecretsReader) {
	deps = &Dependencies{
		DB:            db,
		Signer:        signer,
		SecretsReader: secrets,
		Config: Config{
			CloudFrontDomain:    "cdn.example.com",
			CloudFrontKeyPairID: "KEYPAIRID123",
			PrivateKeySecretARN: "arn:aws:secretsmanager:us-east-1:123456789012:secret:test",
			SignedURLExpiry:     5 * time.Minute,
		},
	}
}

// Test 1: Valid blob returns 302 with signed URL
func TestDownload_Success(t *testing.T) {
	blob := &BlobRecord{
		BlobID:      "blob-123",
		AccountID:   "user-456",
		Size:        1024,
		ContentType: "application/octet-stream",
		S3Key:       "user-456/blob-123",
		CreatedAt:   "2024-01-01T00:00:00Z",
	}

	db := &mockBlobDB{blob: blob}
	signer := &mockURLSigner{signedURL: "https://cdn.example.com/blobs/user-456/blob-123?Signature=abc123"}
	secrets := &mockSecretsReader{privateKey: "test-key"}
	setupTestDeps(db, signer, secrets)

	request := events.APIGatewayProxyRequest{
		PathParameters: map[string]string{
			"accountId": "user-456",
			"blobId":    "blob-123",
		},
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "req-abc",
			Authorizer: map[string]any{
				"claims": map[string]any{
					"sub": "user-456",
				},
			},
		},
	}

	ctx := context.Background()
	response, err := handler(ctx, request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if response.StatusCode != 302 {
		t.Errorf("expected status code 302, got %d. Body: %s", response.StatusCode, response.Body)
	}

	location := response.Headers["Location"]
	if location == "" {
		t.Error("expected Location header to be set")
	}

	if !strings.Contains(location, "cdn.example.com") {
		t.Errorf("expected Location to contain CloudFront domain, got %s", location)
	}

	if response.Headers["Cache-Control"] != "no-store" {
		t.Errorf("expected Cache-Control: no-store, got %s", response.Headers["Cache-Control"])
	}
}

// Test 2: Blob not found returns 404
func TestDownload_BlobNotFound(t *testing.T) {
	db := &mockBlobDB{blob: nil} // No blob found
	signer := &mockURLSigner{}
	secrets := &mockSecretsReader{}
	setupTestDeps(db, signer, secrets)

	request := events.APIGatewayProxyRequest{
		PathParameters: map[string]string{
			"accountId": "user-456",
			"blobId":    "nonexistent-blob",
		},
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "req-abc",
			Authorizer: map[string]any{
				"claims": map[string]any{
					"sub": "user-456",
				},
			},
		},
	}

	ctx := context.Background()
	response, err := handler(ctx, request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if response.StatusCode != 404 {
		t.Errorf("expected status code 404, got %d. Body: %s", response.StatusCode, response.Body)
	}

	var errResp ErrorResponse
	if err := json.Unmarshal([]byte(response.Body), &errResp); err != nil {
		t.Fatalf("failed to unmarshal error response: %v", err)
	}

	if errResp.Type != "notFound" {
		t.Errorf("expected error type 'notFound', got '%s'", errResp.Type)
	}
}

// Test 3: Blob exists but different owner returns 404 (not 403 to avoid information leakage)
func TestDownload_WrongAccount(t *testing.T) {
	// Blob belongs to a different account
	blob := &BlobRecord{
		BlobID:      "blob-123",
		AccountID:   "other-user",
		Size:        1024,
		ContentType: "application/octet-stream",
		S3Key:       "other-user/blob-123",
		CreatedAt:   "2024-01-01T00:00:00Z",
	}

	db := &mockBlobDB{blob: blob}
	signer := &mockURLSigner{}
	secrets := &mockSecretsReader{}
	setupTestDeps(db, signer, secrets)

	request := events.APIGatewayProxyRequest{
		PathParameters: map[string]string{
			"accountId": "user-456",
			"blobId":    "blob-123",
		},
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "req-abc",
			Authorizer: map[string]any{
				"claims": map[string]any{
					"sub": "user-456",
				},
			},
		},
	}

	ctx := context.Background()
	response, err := handler(ctx, request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	// Should return 404, not 403, to avoid leaking info about blob existence
	if response.StatusCode != 404 {
		t.Errorf("expected status code 404, got %d. Body: %s", response.StatusCode, response.Body)
	}
}

// Test 4: Path accountId doesn't match authenticated accountId returns 403
func TestDownload_AccountMismatch(t *testing.T) {
	db := &mockBlobDB{}
	signer := &mockURLSigner{}
	secrets := &mockSecretsReader{}
	setupTestDeps(db, signer, secrets)

	request := events.APIGatewayProxyRequest{
		PathParameters: map[string]string{
			"accountId": "user-456",
			"blobId":    "blob-123",
		},
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "req-abc",
			Authorizer: map[string]any{
				"claims": map[string]any{
					"sub": "different-user", // Different from path accountId
				},
			},
		},
	}

	ctx := context.Background()
	response, err := handler(ctx, request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if response.StatusCode != 403 {
		t.Errorf("expected status code 403, got %d. Body: %s", response.StatusCode, response.Body)
	}

	var errResp ErrorResponse
	if err := json.Unmarshal([]byte(response.Body), &errResp); err != nil {
		t.Fatalf("failed to unmarshal error response: %v", err)
	}

	if errResp.Type != "forbidden" {
		t.Errorf("expected error type 'forbidden', got '%s'", errResp.Type)
	}
}

// Test 5: DynamoDB failure returns 500
func TestDownload_DynamoDBFailure(t *testing.T) {
	db := &mockBlobDB{getErr: errors.New("DynamoDB error")}
	signer := &mockURLSigner{}
	secrets := &mockSecretsReader{}
	setupTestDeps(db, signer, secrets)

	request := events.APIGatewayProxyRequest{
		PathParameters: map[string]string{
			"accountId": "user-456",
			"blobId":    "blob-123",
		},
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "req-abc",
			Authorizer: map[string]any{
				"claims": map[string]any{
					"sub": "user-456",
				},
			},
		},
	}

	ctx := context.Background()
	response, err := handler(ctx, request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if response.StatusCode != 500 {
		t.Errorf("expected status code 500, got %d. Body: %s", response.StatusCode, response.Body)
	}

	var errResp ErrorResponse
	if err := json.Unmarshal([]byte(response.Body), &errResp); err != nil {
		t.Fatalf("failed to unmarshal error response: %v", err)
	}

	if errResp.Type != "serverFail" {
		t.Errorf("expected error type 'serverFail', got '%s'", errResp.Type)
	}
}

// Test 6: URL signing failure returns 500
func TestDownload_SigningFailure(t *testing.T) {
	blob := &BlobRecord{
		BlobID:      "blob-123",
		AccountID:   "user-456",
		Size:        1024,
		ContentType: "application/octet-stream",
		S3Key:       "user-456/blob-123",
		CreatedAt:   "2024-01-01T00:00:00Z",
	}

	db := &mockBlobDB{blob: blob}
	signer := &mockURLSigner{signErr: errors.New("signing error")}
	secrets := &mockSecretsReader{}
	setupTestDeps(db, signer, secrets)

	request := events.APIGatewayProxyRequest{
		PathParameters: map[string]string{
			"accountId": "user-456",
			"blobId":    "blob-123",
		},
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "req-abc",
			Authorizer: map[string]any{
				"claims": map[string]any{
					"sub": "user-456",
				},
			},
		},
	}

	ctx := context.Background()
	response, err := handler(ctx, request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if response.StatusCode != 500 {
		t.Errorf("expected status code 500, got %d. Body: %s", response.StatusCode, response.Body)
	}

	var errResp ErrorResponse
	if err := json.Unmarshal([]byte(response.Body), &errResp); err != nil {
		t.Fatalf("failed to unmarshal error response: %v", err)
	}

	if errResp.Type != "serverFail" {
		t.Errorf("expected error type 'serverFail', got '%s'", errResp.Type)
	}
}

// Test 7: Signed URL has correct expiry
func TestDownload_URLExpiry(t *testing.T) {
	blob := &BlobRecord{
		BlobID:      "blob-123",
		AccountID:   "user-456",
		Size:        1024,
		ContentType: "application/octet-stream",
		S3Key:       "user-456/blob-123",
		CreatedAt:   "2024-01-01T00:00:00Z",
	}

	db := &mockBlobDB{blob: blob}
	signer := &mockURLSigner{signedURL: "https://signed-url"}
	secrets := &mockSecretsReader{}
	setupTestDeps(db, signer, secrets)

	// Set custom expiry
	deps.Config.SignedURLExpiry = 10 * time.Minute

	request := events.APIGatewayProxyRequest{
		PathParameters: map[string]string{
			"accountId": "user-456",
			"blobId":    "blob-123",
		},
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "req-abc",
			Authorizer: map[string]any{
				"claims": map[string]any{
					"sub": "user-456",
				},
			},
		},
	}

	beforeCall := time.Now()
	ctx := context.Background()
	_, err := handler(ctx, request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	afterCall := time.Now()

	// Verify the expiry passed to signer is approximately 10 minutes from now
	expectedMinExpiry := beforeCall.Add(10 * time.Minute)
	expectedMaxExpiry := afterCall.Add(10 * time.Minute)

	if signer.lastExpiry.Before(expectedMinExpiry) || signer.lastExpiry.After(expectedMaxExpiry) {
		t.Errorf("expected expiry around %v, got %v", expectedMinExpiry, signer.lastExpiry)
	}
}

// Test 8: Signed URL contains correct blob path
func TestDownload_URLContainsBlobPath(t *testing.T) {
	blob := &BlobRecord{
		BlobID:      "blob-123",
		AccountID:   "user-456",
		Size:        1024,
		ContentType: "application/octet-stream",
		S3Key:       "user-456/blob-123",
		CreatedAt:   "2024-01-01T00:00:00Z",
	}

	db := &mockBlobDB{blob: blob}
	signer := &mockURLSigner{signedURL: "https://signed-url"}
	secrets := &mockSecretsReader{}
	setupTestDeps(db, signer, secrets)

	request := events.APIGatewayProxyRequest{
		PathParameters: map[string]string{
			"accountId": "user-456",
			"blobId":    "blob-123",
		},
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "req-abc",
			Authorizer: map[string]any{
				"claims": map[string]any{
					"sub": "user-456",
				},
			},
		},
	}

	ctx := context.Background()
	_, err := handler(ctx, request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	expectedURL := "https://cdn.example.com/blobs/user-456/blob-123"
	if signer.lastURL != expectedURL {
		t.Errorf("expected URL %s, got %s", expectedURL, signer.lastURL)
	}
}

// Test 9: Missing accountId in path returns 400
func TestDownload_MissingAccountId(t *testing.T) {
	db := &mockBlobDB{}
	signer := &mockURLSigner{}
	secrets := &mockSecretsReader{}
	setupTestDeps(db, signer, secrets)

	request := events.APIGatewayProxyRequest{
		PathParameters: map[string]string{
			"blobId": "blob-123",
			// Missing accountId
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
		t.Errorf("expected status code 400, got %d. Body: %s", response.StatusCode, response.Body)
	}
}

// Test 10: Missing blobId in path returns 400
func TestDownload_MissingBlobId(t *testing.T) {
	db := &mockBlobDB{}
	signer := &mockURLSigner{}
	secrets := &mockSecretsReader{}
	setupTestDeps(db, signer, secrets)

	request := events.APIGatewayProxyRequest{
		PathParameters: map[string]string{
			"accountId": "user-456",
			// Missing blobId
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
		t.Errorf("expected status code 400, got %d. Body: %s", response.StatusCode, response.Body)
	}
}

// Test 11: IAM auth route uses path accountId
func TestDownload_IAMAuthUsesPathAccountId(t *testing.T) {
	blob := &BlobRecord{
		BlobID:      "blob-123",
		AccountID:   "user-456",
		Size:        1024,
		ContentType: "application/octet-stream",
		S3Key:       "user-456/blob-123",
		CreatedAt:   "2024-01-01T00:00:00Z",
	}

	db := &mockBlobDB{blob: blob}
	signer := &mockURLSigner{signedURL: "https://signed-url"}
	secrets := &mockSecretsReader{}
	setupTestDeps(db, signer, secrets)

	// IAM auth - no Cognito authorizer/claims
	request := events.APIGatewayProxyRequest{
		PathParameters: map[string]string{
			"accountId": "user-456",
			"blobId":    "blob-123",
		},
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID:  "req-abc",
			Authorizer: nil, // No authorizer for IAM
		},
	}

	ctx := context.Background()
	response, err := handler(ctx, request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if response.StatusCode != 302 {
		t.Errorf("expected status code 302, got %d. Body: %s", response.StatusCode, response.Body)
	}
}

// Test 12: Response body is empty for redirect
func TestDownload_RedirectBodyEmpty(t *testing.T) {
	blob := &BlobRecord{
		BlobID:      "blob-123",
		AccountID:   "user-456",
		Size:        1024,
		ContentType: "application/octet-stream",
		S3Key:       "user-456/blob-123",
		CreatedAt:   "2024-01-01T00:00:00Z",
	}

	db := &mockBlobDB{blob: blob}
	signer := &mockURLSigner{signedURL: "https://signed-url"}
	secrets := &mockSecretsReader{}
	setupTestDeps(db, signer, secrets)

	request := events.APIGatewayProxyRequest{
		PathParameters: map[string]string{
			"accountId": "user-456",
			"blobId":    "blob-123",
		},
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "req-abc",
			Authorizer: map[string]any{
				"claims": map[string]any{
					"sub": "user-456",
				},
			},
		},
	}

	ctx := context.Background()
	response, err := handler(ctx, request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if response.Body != "" {
		t.Errorf("expected empty body for redirect, got %s", response.Body)
	}
}
