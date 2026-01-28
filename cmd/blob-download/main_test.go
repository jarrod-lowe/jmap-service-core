package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/jarrod-lowe/jmap-service-core/internal/plugin"
)

// =============================================================================
// ParseBlobID unit tests
// =============================================================================

func TestParseBlobID_SimpleBlobID(t *testing.T) {
	result, err := ParseBlobID("abc123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.BaseBlobID != "abc123" {
		t.Errorf("expected BaseBlobID 'abc123', got '%s'", result.BaseBlobID)
	}
	if result.HasRange {
		t.Error("expected HasRange to be false for simple blobId")
	}
}

func TestParseBlobID_CompositeBlobID(t *testing.T) {
	result, err := ParseBlobID("abc123,1024,5120")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.BaseBlobID != "abc123" {
		t.Errorf("expected BaseBlobID 'abc123', got '%s'", result.BaseBlobID)
	}
	if !result.HasRange {
		t.Error("expected HasRange to be true for composite blobId")
	}
	if result.StartByte != 1024 {
		t.Errorf("expected StartByte 1024, got %d", result.StartByte)
	}
	if result.EndByte != 5120 {
		t.Errorf("expected EndByte 5120, got %d", result.EndByte)
	}
}

func TestParseBlobID_NegativeStart(t *testing.T) {
	_, err := ParseBlobID("abc123,-1,100")
	if err == nil {
		t.Error("expected error for negative start byte")
	}
}

func TestParseBlobID_StartGreaterThanOrEqualEnd(t *testing.T) {
	// start == end
	_, err := ParseBlobID("abc123,100,100")
	if err == nil {
		t.Error("expected error when start == end")
	}

	// start > end
	_, err = ParseBlobID("abc123,100,50")
	if err == nil {
		t.Error("expected error when start > end")
	}
}

func TestParseBlobID_NonNumericValues(t *testing.T) {
	_, err := ParseBlobID("abc123,foo,bar")
	if err == nil {
		t.Error("expected error for non-numeric start/end")
	}
}

func TestParseBlobID_TooManyCommas(t *testing.T) {
	_, err := ParseBlobID("a,b,c,d")
	if err == nil {
		t.Error("expected error for too many commas")
	}
}

func TestParseBlobID_OneComma(t *testing.T) {
	// Edge case: blobId with one comma should be treated as simple blobId
	result, err := ParseBlobID("abc,def")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.BaseBlobID != "abc,def" {
		t.Errorf("expected BaseBlobID 'abc,def', got '%s'", result.BaseBlobID)
	}
	if result.HasRange {
		t.Error("expected HasRange to be false for blobId with one comma")
	}
}

func TestParseBlobID_ZeroStartByte(t *testing.T) {
	// Zero is valid for start byte
	result, err := ParseBlobID("abc123,0,999")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.BaseBlobID != "abc123" {
		t.Errorf("expected BaseBlobID 'abc123', got '%s'", result.BaseBlobID)
	}
	if !result.HasRange {
		t.Error("expected HasRange to be true")
	}
	if result.StartByte != 0 {
		t.Errorf("expected StartByte 0, got %d", result.StartByte)
	}
	if result.EndByte != 999 {
		t.Errorf("expected EndByte 999, got %d", result.EndByte)
	}
}

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

// Test: Deleted blob returns 404
func TestDownload_DeletedBlob_Returns404(t *testing.T) {
	blob := &BlobRecord{
		BlobID:      "blob-123",
		AccountID:   "user-456",
		Size:        1024,
		ContentType: "application/octet-stream",
		S3Key:       "user-456/blob-123",
		CreatedAt:   "2024-01-01T00:00:00Z",
		DeletedAt:   "2024-06-01T00:00:00Z",
	}

	db := &mockBlobDB{blob: blob}
	signer := &mockURLSigner{signedURL: "https://cdn.example.com/signed"}
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

// Test 11: IAM auth uses path accountId (detected via Identity.UserArn)
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
	setupTestDepsWithPrincipals(db, signer, secrets, []string{"arn:aws:iam::123456789012:role/ingestion-role"})

	// IAM auth - detected via Identity.UserArn (authoritative signal)
	request := events.APIGatewayProxyRequest{
		PathParameters: map[string]string{
			"accountId": "user-456",
			"blobId":    "blob-123",
		},
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "req-abc",
			Identity: events.APIGatewayRequestIdentity{
				UserArn: "arn:aws:iam::123456789012:role/ingestion-role",
				Caller:  "AROAEXAMPLE:session",
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

// =============================================================================
// Composite blobId integration tests
// =============================================================================

// Test 13: Composite blobId returns 302 with signed URL containing full composite blobId
func TestDownload_CompositeBlobID_Success(t *testing.T) {
	blob := &BlobRecord{
		BlobID:      "blob-123", // Base blob ID
		AccountID:   "user-456",
		Size:        10240,
		ContentType: "message/rfc822",
		S3Key:       "user-456/blob-123",
		CreatedAt:   "2024-01-01T00:00:00Z",
	}

	db := &mockBlobDB{blob: blob}
	signer := &mockURLSigner{signedURL: "https://cdn.example.com/signed"}
	secrets := &mockSecretsReader{}
	setupTestDeps(db, signer, secrets)

	request := events.APIGatewayProxyRequest{
		PathParameters: map[string]string{
			"accountId": "user-456",
			"blobId":    "blob-123,1024,5120", // Composite blobId
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

	// Verify the URL passed to signer contains the full composite blobId
	expectedURL := "https://cdn.example.com/blobs/user-456/blob-123,1024,5120"
	if signer.lastURL != expectedURL {
		t.Errorf("expected signed URL to contain composite blobId\nexpected: %s\ngot: %s", expectedURL, signer.lastURL)
	}
}

// Test 14: Composite blobId with nonexistent base ID returns 404
func TestDownload_CompositeBlobID_NotFound(t *testing.T) {
	db := &mockBlobDB{blob: nil} // No blob found
	signer := &mockURLSigner{}
	secrets := &mockSecretsReader{}
	setupTestDeps(db, signer, secrets)

	request := events.APIGatewayProxyRequest{
		PathParameters: map[string]string{
			"accountId": "user-456",
			"blobId":    "nonexistent,1024,5120", // Composite blobId with nonexistent base
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

// Test 15: Composite blobId with wrong account returns 403
func TestDownload_CompositeBlobID_WrongAccount(t *testing.T) {
	db := &mockBlobDB{}
	signer := &mockURLSigner{}
	secrets := &mockSecretsReader{}
	setupTestDeps(db, signer, secrets)

	request := events.APIGatewayProxyRequest{
		PathParameters: map[string]string{
			"accountId": "user-456",
			"blobId":    "blob-123,1024,5120",
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

// Test 16: Invalid composite blobId format returns 400
func TestDownload_CompositeBlobID_InvalidFormat(t *testing.T) {
	db := &mockBlobDB{}
	signer := &mockURLSigner{}
	secrets := &mockSecretsReader{}
	setupTestDeps(db, signer, secrets)

	testCases := []struct {
		name   string
		blobId string
	}{
		{"negative start", "blob-123,-1,100"},
		{"start >= end", "blob-123,100,50"},
		{"non-numeric", "blob-123,foo,bar"},
		{"too many commas", "a,b,c,d"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			request := events.APIGatewayProxyRequest{
				PathParameters: map[string]string{
					"accountId": "user-456",
					"blobId":    tc.blobId,
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

			if response.StatusCode != 400 {
				t.Errorf("expected status code 400, got %d. Body: %s", response.StatusCode, response.Body)
			}

			var errResp ErrorResponse
			if err := json.Unmarshal([]byte(response.Body), &errResp); err != nil {
				t.Fatalf("failed to unmarshal error response: %v", err)
			}

			if errResp.Type != "invalidArguments" {
				t.Errorf("expected error type 'invalidArguments', got '%s'", errResp.Type)
			}
		})
	}
}

// =============================================================================
// extractAccountID unit tests - testing IAM vs Cognito auth detection
// Uses authoritative API Gateway signals (Identity fields for IAM, Authorizer claims for Cognito)
// =============================================================================

// Test: IAM auth detected by Identity.UserArn - uses path parameter
func TestExtractAccountID_IAMAuth_UsesPathParam(t *testing.T) {
	request := events.APIGatewayProxyRequest{
		PathParameters: map[string]string{"accountId": "test-account-123"},
		RequestContext: events.APIGatewayProxyRequestContext{
			Identity: events.APIGatewayRequestIdentity{
				UserArn: "arn:aws:iam::123456789012:role/lambda-role",
				Caller:  "AROAEXAMPLE:session-name",
			},
		},
	}

	accountID, err := extractAccountID(request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if accountID != "test-account-123" {
		t.Errorf("expected 'test-account-123', got '%s'", accountID)
	}
}

// Test: IAM auth with only Caller field (no UserArn) - still IAM
func TestExtractAccountID_IAMAuth_CallerOnly(t *testing.T) {
	request := events.APIGatewayProxyRequest{
		PathParameters: map[string]string{"accountId": "test-account-456"},
		RequestContext: events.APIGatewayProxyRequestContext{
			Identity: events.APIGatewayRequestIdentity{
				Caller: "AROAEXAMPLE:session-name",
			},
		},
	}

	accountID, err := extractAccountID(request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if accountID != "test-account-456" {
		t.Errorf("expected 'test-account-456', got '%s'", accountID)
	}
}

// Test: IAM auth with Authorizer populated (but no claims) - still uses path param
func TestExtractAccountID_IAMAuth_WithAuthorizerNoClaims(t *testing.T) {
	request := events.APIGatewayProxyRequest{
		PathParameters: map[string]string{"accountId": "test-account-123"},
		RequestContext: events.APIGatewayProxyRequestContext{
			Identity: events.APIGatewayRequestIdentity{
				UserArn: "arn:aws:iam::123456789012:role/lambda-role",
			},
			Authorizer: map[string]interface{}{
				"principalId": "something",
			},
		},
	}

	accountID, err := extractAccountID(request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if accountID != "test-account-123" {
		t.Errorf("expected 'test-account-123', got '%s'", accountID)
	}
}

// Test: IAM auth missing path parameter should error
func TestExtractAccountID_IAMAuth_MissingPathParam(t *testing.T) {
	request := events.APIGatewayProxyRequest{
		PathParameters: map[string]string{}, // No accountId
		RequestContext: events.APIGatewayProxyRequestContext{
			Identity: events.APIGatewayRequestIdentity{
				UserArn: "arn:aws:iam::123456789012:role/lambda-role",
			},
		},
	}

	_, err := extractAccountID(request)
	if err == nil {
		t.Error("expected error for missing accountId on IAM auth")
	}
	if !strings.Contains(err.Error(), "missing accountId") {
		t.Errorf("expected error about missing accountId, got: %v", err)
	}
}

// Test: Cognito auth detected by Authorizer claims - uses JWT sub claim
func TestExtractAccountID_CognitoAuth_UsesJWTClaims(t *testing.T) {
	request := events.APIGatewayProxyRequest{
		PathParameters: map[string]string{"accountId": "path-should-be-ignored"},
		RequestContext: events.APIGatewayProxyRequestContext{
			Identity: events.APIGatewayRequestIdentity{}, // Empty - no IAM
			Authorizer: map[string]interface{}{
				"claims": map[string]interface{}{
					"sub": "cognito-user-id",
				},
			},
		},
	}

	accountID, err := extractAccountID(request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if accountID != "cognito-user-id" {
		t.Errorf("expected 'cognito-user-id', got '%s'", accountID)
	}
}

// Test: No auth (neither IAM nor Cognito) - should error
func TestExtractAccountID_NoAuth_ReturnsError(t *testing.T) {
	request := events.APIGatewayProxyRequest{
		PathParameters: map[string]string{"accountId": "test-account"},
		RequestContext: events.APIGatewayProxyRequestContext{
			Identity:   events.APIGatewayRequestIdentity{}, // Empty
			Authorizer: nil,
		},
	}

	_, err := extractAccountID(request)
	if err == nil {
		t.Error("expected error for no authentication context")
	}
	if !strings.Contains(err.Error(), "no authentication context") {
		t.Errorf("expected error about no authentication context, got: %v", err)
	}
}

// Test: Cognito auth without sub claim should error
func TestExtractAccountID_CognitoAuth_NoSubClaim(t *testing.T) {
	request := events.APIGatewayProxyRequest{
		PathParameters: map[string]string{"accountId": "account-123"},
		RequestContext: events.APIGatewayProxyRequestContext{
			Identity: events.APIGatewayRequestIdentity{}, // Empty - no IAM
			Authorizer: map[string]interface{}{
				"claims": map[string]interface{}{
					"email": "user@example.com", // No sub claim
				},
			},
		},
	}

	_, err := extractAccountID(request)
	if err == nil {
		t.Error("expected error for Cognito auth without sub claim")
	}
}

// Test: Cognito auth with empty sub claim should error
func TestExtractAccountID_CognitoAuth_EmptySubClaim(t *testing.T) {
	request := events.APIGatewayProxyRequest{
		PathParameters: map[string]string{"accountId": "account-123"},
		RequestContext: events.APIGatewayProxyRequestContext{
			Identity: events.APIGatewayRequestIdentity{}, // Empty - no IAM
			Authorizer: map[string]interface{}{
				"claims": map[string]interface{}{
					"sub": "", // Empty sub
				},
			},
		},
	}

	_, err := extractAccountID(request)
	if err == nil {
		t.Error("expected error for empty sub claim")
	}
}

// Test: Authorizer with no claims key should error (no IAM identity)
func TestExtractAccountID_AuthorizerWithoutClaims(t *testing.T) {
	request := events.APIGatewayProxyRequest{
		PathParameters: map[string]string{"accountId": "account-123"},
		RequestContext: events.APIGatewayProxyRequestContext{
			Identity: events.APIGatewayRequestIdentity{}, // Empty - no IAM
			Authorizer: map[string]interface{}{
				"principalId": "some-principal", // No claims key
			},
		},
	}

	_, err := extractAccountID(request)
	if err == nil {
		t.Error("expected error for authorizer without claims")
	}
}

// Test 17: Verifies DynamoDB lookup uses base blob ID, not composite
func TestDownload_CompositeBlobID_LookupUsesBaseID(t *testing.T) {
	var capturedBlobID string

	db := &mockBlobDB{
		getFunc: func(ctx context.Context, accountID, blobID string) (*BlobRecord, error) {
			capturedBlobID = blobID
			return &BlobRecord{
				BlobID:      "blob-123",
				AccountID:   "user-456",
				Size:        10240,
				ContentType: "message/rfc822",
				S3Key:       "user-456/blob-123",
				CreatedAt:   "2024-01-01T00:00:00Z",
			}, nil
		},
	}
	signer := &mockURLSigner{signedURL: "https://signed-url"}
	secrets := &mockSecretsReader{}
	setupTestDeps(db, signer, secrets)

	request := events.APIGatewayProxyRequest{
		PathParameters: map[string]string{
			"accountId": "user-456",
			"blobId":    "blob-123,1024,5120", // Composite blobId
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

	// Verify DynamoDB was queried with base blob ID, not composite
	if capturedBlobID != "blob-123" {
		t.Errorf("expected DynamoDB lookup with 'blob-123', got '%s'", capturedBlobID)
	}
}

// =============================================================================
// IAM Principal Authorization Tests
// =============================================================================

func setupTestDepsWithPrincipals(db *mockBlobDB, signer *mockURLSigner, secrets *mockSecretsReader, principals []string) {
	deps = &Dependencies{
		DB:            db,
		Signer:        signer,
		SecretsReader: secrets,
		Registry:      plugin.NewRegistryWithPrincipals(principals),
		Config: Config{
			CloudFrontDomain:    "cdn.example.com",
			CloudFrontKeyPairID: "KEYPAIRID123",
			PrivateKeySecretARN: "arn:aws:secretsmanager:us-east-1:123456789012:secret:test",
			SignedURLExpiry:     5 * time.Minute,
		},
	}
}

func TestHandler_IAMAuth_RegisteredPrincipal_Succeeds(t *testing.T) {
	db := &mockBlobDB{
		blob: &BlobRecord{
			BlobID:      "blob-123",
			AccountID:   "user-456",
			Size:        1024,
			ContentType: "message/rfc822",
			S3Key:       "user-456/blob-123",
			CreatedAt:   "2024-01-01T00:00:00Z",
		},
	}
	signer := &mockURLSigner{signedURL: "https://cdn.example.com/signed"}
	secrets := &mockSecretsReader{}
	setupTestDepsWithPrincipals(db, signer, secrets, []string{"arn:aws:iam::123456789012:role/IngestRole"})

	request := events.APIGatewayProxyRequest{
		Path: "/download-iam/user-456/blob-123",
		PathParameters: map[string]string{
			"accountId": "user-456",
			"blobId":    "blob-123",
		},
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "req-abc",
			Identity: events.APIGatewayRequestIdentity{
				UserArn: "arn:aws:iam::123456789012:role/IngestRole",
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
}

func TestHandler_IAMAuth_UnregisteredPrincipal_Returns403(t *testing.T) {
	db := &mockBlobDB{}
	signer := &mockURLSigner{}
	secrets := &mockSecretsReader{}
	setupTestDepsWithPrincipals(db, signer, secrets, []string{"arn:aws:iam::123456789012:role/IngestRole"})

	request := events.APIGatewayProxyRequest{
		Path: "/download-iam/user-456/blob-123",
		PathParameters: map[string]string{
			"accountId": "user-456",
			"blobId":    "blob-123",
		},
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "req-abc",
			Identity: events.APIGatewayRequestIdentity{
				UserArn: "arn:aws:iam::123456789012:role/UnauthorizedRole",
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
}

func TestHandler_CognitoAuth_BypassesPrincipalCheck(t *testing.T) {
	db := &mockBlobDB{
		blob: &BlobRecord{
			BlobID:      "blob-123",
			AccountID:   "user-456",
			Size:        1024,
			ContentType: "message/rfc822",
			S3Key:       "user-456/blob-123",
			CreatedAt:   "2024-01-01T00:00:00Z",
		},
	}
	signer := &mockURLSigner{signedURL: "https://cdn.example.com/signed"}
	secrets := &mockSecretsReader{}
	// Empty principals list - but Cognito should still work
	setupTestDepsWithPrincipals(db, signer, secrets, []string{})

	request := events.APIGatewayProxyRequest{
		Path: "/download/user-456/blob-123", // Not /download-iam
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
}
