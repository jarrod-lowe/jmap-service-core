package main

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-lambda-go/events"
)

// MockDynamoDB implements AccountDB for testing
type MockDynamoDB struct {
	CreateAccountMetaCalled bool
	CreateAccountMetaInput  CreateAccountMetaInput
	CreateAccountMetaErr    error
}

type CreateAccountMetaInput struct {
	AccountID       string
	QuotaBytes      int64
	QuotaRemaining  int64
	AccountType     string
}

func (m *MockDynamoDB) CreateAccountMeta(ctx context.Context, accountID string, quotaBytes int64) error {
	m.CreateAccountMetaCalled = true
	m.CreateAccountMetaInput = CreateAccountMetaInput{
		AccountID:      accountID,
		QuotaBytes:     quotaBytes,
		QuotaRemaining: quotaBytes,
	}
	return m.CreateAccountMetaErr
}

// MockCognito implements CognitoClient for testing
type MockCognito struct {
	SetUserAttributeCalled bool
	SetUserAttributeInput  SetUserAttributeInput
	SetUserAttributeErr    error
}

type SetUserAttributeInput struct {
	UserPoolID string
	Username   string
	AttrName   string
	AttrValue  string
}

func (m *MockCognito) SetUserAttribute(ctx context.Context, userPoolID, username, attrName, attrValue string) error {
	m.SetUserAttributeCalled = true
	m.SetUserAttributeInput = SetUserAttributeInput{
		UserPoolID: userPoolID,
		Username:   username,
		AttrName:   attrName,
		AttrValue:  attrValue,
	}
	return m.SetUserAttributeErr
}

func TestHandler_AlreadyInitialized(t *testing.T) {
	mockDB := &MockDynamoDB{}
	mockCognito := &MockCognito{}

	deps = &Dependencies{
		DB:            mockDB,
		Cognito:       mockCognito,
		DefaultQuota:  1073741824,
	}

	event := events.CognitoEventUserPoolsPostAuthentication{
		CognitoEventUserPoolsHeader: events.CognitoEventUserPoolsHeader{
			UserPoolID: "ap-southeast-2_abc123",
			UserName:   "testuser",
		},
		Request: events.CognitoEventUserPoolsPostAuthenticationRequest{
			UserAttributes: map[string]string{
				"sub":                         "user-123",
				"custom:account_initialized": "true",
			},
		},
	}

	result, err := handler(context.Background(), event)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Should return event unchanged
	if result.UserPoolID != event.UserPoolID {
		t.Errorf("expected UserPoolID %s, got %s", event.UserPoolID, result.UserPoolID)
	}

	// Should NOT call DynamoDB
	if mockDB.CreateAccountMetaCalled {
		t.Error("expected DynamoDB not to be called when already initialized")
	}

	// Should NOT call Cognito
	if mockCognito.SetUserAttributeCalled {
		t.Error("expected Cognito not to be called when already initialized")
	}
}

func TestHandler_NewAccount_Success(t *testing.T) {
	mockDB := &MockDynamoDB{}
	mockCognito := &MockCognito{}

	deps = &Dependencies{
		DB:            mockDB,
		Cognito:       mockCognito,
		DefaultQuota:  1073741824,
	}

	event := events.CognitoEventUserPoolsPostAuthentication{
		CognitoEventUserPoolsHeader: events.CognitoEventUserPoolsHeader{
			UserPoolID: "ap-southeast-2_abc123",
			UserName:   "testuser",
		},
		Request: events.CognitoEventUserPoolsPostAuthenticationRequest{
			UserAttributes: map[string]string{
				"sub": "user-123",
			},
		},
	}

	result, err := handler(context.Background(), event)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Should return event unchanged
	if result.UserPoolID != event.UserPoolID {
		t.Errorf("expected UserPoolID %s, got %s", event.UserPoolID, result.UserPoolID)
	}

	// Should call DynamoDB with correct parameters
	if !mockDB.CreateAccountMetaCalled {
		t.Fatal("expected DynamoDB to be called")
	}
	if mockDB.CreateAccountMetaInput.AccountID != "user-123" {
		t.Errorf("expected accountID user-123, got %s", mockDB.CreateAccountMetaInput.AccountID)
	}
	if mockDB.CreateAccountMetaInput.QuotaBytes != 1073741824 {
		t.Errorf("expected quotaBytes 1073741824, got %d", mockDB.CreateAccountMetaInput.QuotaBytes)
	}

	// Should call Cognito to set attribute
	if !mockCognito.SetUserAttributeCalled {
		t.Fatal("expected Cognito to be called")
	}
	if mockCognito.SetUserAttributeInput.UserPoolID != "ap-southeast-2_abc123" {
		t.Errorf("expected userPoolID ap-southeast-2_abc123, got %s", mockCognito.SetUserAttributeInput.UserPoolID)
	}
	if mockCognito.SetUserAttributeInput.Username != "testuser" {
		t.Errorf("expected username testuser, got %s", mockCognito.SetUserAttributeInput.Username)
	}
	if mockCognito.SetUserAttributeInput.AttrName != "custom:account_initialized" {
		t.Errorf("expected attrName custom:account_initialized, got %s", mockCognito.SetUserAttributeInput.AttrName)
	}
	if mockCognito.SetUserAttributeInput.AttrValue != "true" {
		t.Errorf("expected attrValue true, got %s", mockCognito.SetUserAttributeInput.AttrValue)
	}
}

func TestHandler_DynamoDB_Error(t *testing.T) {
	mockDB := &MockDynamoDB{
		CreateAccountMetaErr: errors.New("DynamoDB error"),
	}
	mockCognito := &MockCognito{}

	deps = &Dependencies{
		DB:            mockDB,
		Cognito:       mockCognito,
		DefaultQuota:  1073741824,
	}

	event := events.CognitoEventUserPoolsPostAuthentication{
		CognitoEventUserPoolsHeader: events.CognitoEventUserPoolsHeader{
			UserPoolID: "ap-southeast-2_abc123",
			UserName:   "testuser",
		},
		Request: events.CognitoEventUserPoolsPostAuthenticationRequest{
			UserAttributes: map[string]string{
				"sub": "user-123",
			},
		},
	}

	_, err := handler(context.Background(), event)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Should NOT call Cognito if DynamoDB fails
	if mockCognito.SetUserAttributeCalled {
		t.Error("expected Cognito not to be called when DynamoDB fails")
	}
}

func TestHandler_Cognito_Error(t *testing.T) {
	mockDB := &MockDynamoDB{}
	mockCognito := &MockCognito{
		SetUserAttributeErr: errors.New("Cognito error"),
	}

	deps = &Dependencies{
		DB:            mockDB,
		Cognito:       mockCognito,
		DefaultQuota:  1073741824,
	}

	event := events.CognitoEventUserPoolsPostAuthentication{
		CognitoEventUserPoolsHeader: events.CognitoEventUserPoolsHeader{
			UserPoolID: "ap-southeast-2_abc123",
			UserName:   "testuser",
		},
		Request: events.CognitoEventUserPoolsPostAuthenticationRequest{
			UserAttributes: map[string]string{
				"sub": "user-123",
			},
		},
	}

	_, err := handler(context.Background(), event)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestHandler_MissingSub(t *testing.T) {
	mockDB := &MockDynamoDB{}
	mockCognito := &MockCognito{}

	deps = &Dependencies{
		DB:            mockDB,
		Cognito:       mockCognito,
		DefaultQuota:  1073741824,
	}

	event := events.CognitoEventUserPoolsPostAuthentication{
		CognitoEventUserPoolsHeader: events.CognitoEventUserPoolsHeader{
			UserPoolID: "ap-southeast-2_abc123",
			UserName:   "testuser",
		},
		Request: events.CognitoEventUserPoolsPostAuthenticationRequest{
			UserAttributes: map[string]string{
				// sub is missing
			},
		},
	}

	_, err := handler(context.Background(), event)
	if err == nil {
		t.Fatal("expected error for missing sub, got nil")
	}
}
