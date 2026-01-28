package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/jarrod-lowe/jmap-service-core/internal/plugin"
)

// Mock implementations for testing

type mockBlobDB struct {
	getFunc        func(ctx context.Context, accountID, blobID string) (*BlobRecord, error)
	markDeleteFunc func(ctx context.Context, accountID, blobID string, deletedAt string) error
	blob           *BlobRecord
	getErr         error
	markErr        error
}

func (m *mockBlobDB) GetBlob(ctx context.Context, accountID, blobID string) (*BlobRecord, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, accountID, blobID)
	}
	return m.blob, m.getErr
}

func (m *mockBlobDB) MarkBlobDeleted(ctx context.Context, accountID, blobID string, deletedAt string) error {
	if m.markDeleteFunc != nil {
		return m.markDeleteFunc(ctx, accountID, blobID, deletedAt)
	}
	return m.markErr
}

func setupTestDeps(db *mockBlobDB, principals []string) {
	deps = &Dependencies{
		DB:       db,
		Registry: plugin.NewRegistryWithPrincipals(principals),
	}
}

func iamRequest(accountID, blobID, userArn string) events.APIGatewayProxyRequest {
	return events.APIGatewayProxyRequest{
		PathParameters: map[string]string{
			"accountId": accountID,
			"blobId":    blobID,
		},
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "req-test",
			Identity: events.APIGatewayRequestIdentity{
				UserArn: userArn,
			},
		},
	}
}

var testPrincipal = "arn:aws:iam::123456789012:role/IngestRole"

func testBlob() *BlobRecord {
	return &BlobRecord{
		BlobID:      "blob-123",
		AccountID:   "user-456",
		Size:        1024,
		ContentType: "application/octet-stream",
		S3Key:       "user-456/blob-123",
		CreatedAt:   "2024-01-01T00:00:00Z",
	}
}

// Test: Successful delete returns 204
func TestDelete_Success_Returns204(t *testing.T) {
	db := &mockBlobDB{blob: testBlob()}
	setupTestDeps(db, []string{testPrincipal})

	request := iamRequest("user-456", "blob-123", testPrincipal)

	response, err := handler(context.Background(), request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if response.StatusCode != 204 {
		t.Errorf("expected 204, got %d. Body: %s", response.StatusCode, response.Body)
	}

	if response.Body != "" {
		t.Errorf("expected empty body, got %q", response.Body)
	}
}

// Test: Blob not found returns 404
func TestDelete_BlobNotFound_Returns404(t *testing.T) {
	db := &mockBlobDB{blob: nil}
	setupTestDeps(db, []string{testPrincipal})

	request := iamRequest("user-456", "nonexistent", testPrincipal)

	response, err := handler(context.Background(), request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if response.StatusCode != 404 {
		t.Errorf("expected 404, got %d", response.StatusCode)
	}
}

// Test: Wrong account returns 404 (not 403 to avoid info leakage)
func TestDelete_WrongAccount_Returns404(t *testing.T) {
	blob := testBlob()
	blob.AccountID = "other-user"
	db := &mockBlobDB{blob: blob}
	setupTestDeps(db, []string{testPrincipal})

	request := iamRequest("user-456", "blob-123", testPrincipal)

	response, err := handler(context.Background(), request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if response.StatusCode != 404 {
		t.Errorf("expected 404, got %d", response.StatusCode)
	}
}

// Test: Already deleted blob returns 404
func TestDelete_AlreadyDeleted_Returns404(t *testing.T) {
	blob := testBlob()
	blob.DeletedAt = "2024-06-01T00:00:00Z"
	db := &mockBlobDB{blob: blob}
	setupTestDeps(db, []string{testPrincipal})

	request := iamRequest("user-456", "blob-123", testPrincipal)

	response, err := handler(context.Background(), request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if response.StatusCode != 404 {
		t.Errorf("expected 404, got %d", response.StatusCode)
	}
}

// Test: Missing accountId returns 400
func TestDelete_MissingAccountId_Returns400(t *testing.T) {
	db := &mockBlobDB{}
	setupTestDeps(db, []string{testPrincipal})

	request := events.APIGatewayProxyRequest{
		PathParameters: map[string]string{
			"blobId": "blob-123",
		},
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "req-test",
			Identity: events.APIGatewayRequestIdentity{
				UserArn: testPrincipal,
			},
		},
	}

	response, err := handler(context.Background(), request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if response.StatusCode != 400 {
		t.Errorf("expected 400, got %d", response.StatusCode)
	}
}

// Test: Missing blobId returns 400
func TestDelete_MissingBlobId_Returns400(t *testing.T) {
	db := &mockBlobDB{}
	setupTestDeps(db, []string{testPrincipal})

	request := events.APIGatewayProxyRequest{
		PathParameters: map[string]string{
			"accountId": "user-456",
		},
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "req-test",
			Identity: events.APIGatewayRequestIdentity{
				UserArn: testPrincipal,
			},
		},
	}

	response, err := handler(context.Background(), request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if response.StatusCode != 400 {
		t.Errorf("expected 400, got %d", response.StatusCode)
	}
}

// Test: Unregistered principal returns 403
func TestDelete_UnregisteredPrincipal_Returns403(t *testing.T) {
	db := &mockBlobDB{}
	setupTestDeps(db, []string{testPrincipal})

	request := iamRequest("user-456", "blob-123", "arn:aws:iam::123456789012:role/UnauthorizedRole")

	response, err := handler(context.Background(), request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if response.StatusCode != 403 {
		t.Errorf("expected 403, got %d", response.StatusCode)
	}
}

// Test: No IAM auth returns 401
func TestDelete_NoAuth_Returns401(t *testing.T) {
	db := &mockBlobDB{}
	setupTestDeps(db, []string{testPrincipal})

	request := events.APIGatewayProxyRequest{
		PathParameters: map[string]string{
			"accountId": "user-456",
			"blobId":    "blob-123",
		},
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "req-test",
		},
	}

	response, err := handler(context.Background(), request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if response.StatusCode != 401 {
		t.Errorf("expected 401, got %d", response.StatusCode)
	}
}

// Test: DynamoDB GetBlob failure returns 500
func TestDelete_DBGetFailure_Returns500(t *testing.T) {
	db := &mockBlobDB{getErr: errors.New("dynamo error")}
	setupTestDeps(db, []string{testPrincipal})

	request := iamRequest("user-456", "blob-123", testPrincipal)

	response, err := handler(context.Background(), request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if response.StatusCode != 500 {
		t.Errorf("expected 500, got %d", response.StatusCode)
	}
}

// Test: DynamoDB MarkBlobDeleted failure returns 500
func TestDelete_DBMarkFailure_Returns500(t *testing.T) {
	db := &mockBlobDB{blob: testBlob(), markErr: errors.New("update error")}
	setupTestDeps(db, []string{testPrincipal})

	request := iamRequest("user-456", "blob-123", testPrincipal)

	response, err := handler(context.Background(), request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if response.StatusCode != 500 {
		t.Errorf("expected 500, got %d", response.StatusCode)
	}
}

// Test: MarkBlobDeleted is called with correct parameters
func TestDelete_CallsMarkBlobDeletedWithCorrectParams(t *testing.T) {
	var calledAccountID, calledBlobID, calledDeletedAt string
	db := &mockBlobDB{
		blob: testBlob(),
		markDeleteFunc: func(ctx context.Context, accountID, blobID string, deletedAt string) error {
			calledAccountID = accountID
			calledBlobID = blobID
			calledDeletedAt = deletedAt
			return nil
		},
	}
	setupTestDeps(db, []string{testPrincipal})

	request := iamRequest("user-456", "blob-123", testPrincipal)

	response, err := handler(context.Background(), request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if response.StatusCode != 204 {
		t.Errorf("expected 204, got %d. Body: %s", response.StatusCode, response.Body)
	}

	if calledAccountID != "user-456" {
		t.Errorf("expected accountID 'user-456', got %q", calledAccountID)
	}
	if calledBlobID != "blob-123" {
		t.Errorf("expected blobID 'blob-123', got %q", calledBlobID)
	}
	if calledDeletedAt == "" {
		t.Error("expected deletedAt to be set")
	}
}

// =============================================================================
// Cognito auth tests
// =============================================================================

func cognitoRequest(pathAccountID, blobID, sub string) events.APIGatewayProxyRequest {
	return events.APIGatewayProxyRequest{
		PathParameters: map[string]string{
			"accountId": pathAccountID,
			"blobId":    blobID,
		},
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "req-test",
			Authorizer: map[string]any{
				"claims": map[string]any{
					"sub": sub,
				},
			},
		},
	}
}

// Test: Cognito auth successful delete returns 204
func TestDelete_Cognito_Success_Returns204(t *testing.T) {
	db := &mockBlobDB{blob: testBlob()}
	setupTestDeps(db, []string{testPrincipal})

	request := cognitoRequest("user-456", "blob-123", "user-456")

	response, err := handler(context.Background(), request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if response.StatusCode != 204 {
		t.Errorf("expected 204, got %d. Body: %s", response.StatusCode, response.Body)
	}
}

// Test: Cognito auth account mismatch returns 403
func TestDelete_Cognito_AccountMismatch_Returns403(t *testing.T) {
	db := &mockBlobDB{blob: testBlob()}
	setupTestDeps(db, []string{testPrincipal})

	// sub claim says "user-456" but path says "other-user"
	request := cognitoRequest("other-user", "blob-123", "user-456")

	response, err := handler(context.Background(), request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if response.StatusCode != 403 {
		t.Errorf("expected 403, got %d. Body: %s", response.StatusCode, response.Body)
	}
}

// Test: Cognito auth skips principal check (not IAM)
func TestDelete_Cognito_SkipsPrincipalCheck(t *testing.T) {
	db := &mockBlobDB{blob: testBlob()}
	// No principals registered - would fail if principal check ran
	setupTestDeps(db, []string{})

	request := cognitoRequest("user-456", "blob-123", "user-456")

	response, err := handler(context.Background(), request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if response.StatusCode != 204 {
		t.Errorf("expected 204 (principal check skipped for Cognito), got %d. Body: %s", response.StatusCode, response.Body)
	}
}

// Test: Cognito auth with no claims returns 401
func TestDelete_Cognito_NoClaims_Returns401(t *testing.T) {
	db := &mockBlobDB{}
	setupTestDeps(db, []string{testPrincipal})

	request := events.APIGatewayProxyRequest{
		PathParameters: map[string]string{
			"accountId": "user-456",
			"blobId":    "blob-123",
		},
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID:  "req-test",
			Authorizer: map[string]any{},
		},
	}

	response, err := handler(context.Background(), request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if response.StatusCode != 401 {
		t.Errorf("expected 401, got %d. Body: %s", response.StatusCode, response.Body)
	}
}

// Test: Error response body is valid JSON with correct type
func TestDelete_ErrorResponseFormat(t *testing.T) {
	db := &mockBlobDB{blob: nil}
	setupTestDeps(db, []string{testPrincipal})

	request := iamRequest("user-456", "nonexistent", testPrincipal)

	response, err := handler(context.Background(), request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var errResp ErrorResponse
	if err := json.Unmarshal([]byte(response.Body), &errResp); err != nil {
		t.Fatalf("failed to unmarshal error response: %v", err)
	}

	if errResp.Type != "notFound" {
		t.Errorf("expected error type 'notFound', got %q", errResp.Type)
	}
}
