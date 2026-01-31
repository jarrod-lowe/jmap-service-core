package main

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/jarrod-lowe/jmap-service-core/internal/plugin"
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
		DB:           mockDB,
		Cognito:      mockCognito,
		DefaultQuota: 1073741824,
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

// MockEventPublisher implements EventPublisher for testing
type MockEventPublisher struct {
	PublishCalled bool
	PublishInputs []PublishInput
	PublishErr    error
}

type PublishInput struct {
	EventType string
	AccountID string
	Data      map[string]any
}

func (m *MockEventPublisher) Publish(ctx context.Context, payload EventPayload) error {
	m.PublishCalled = true
	m.PublishInputs = append(m.PublishInputs, PublishInput{
		EventType: payload.EventType,
		AccountID: payload.AccountID,
		Data:      payload.Data,
	})
	return m.PublishErr
}

func TestHandler_PublishesAccountCreatedEvent(t *testing.T) {
	mockDB := &MockDynamoDB{}
	mockCognito := &MockCognito{}
	mockPublisher := &MockEventPublisher{}

	deps = &Dependencies{
		DB:             mockDB,
		Cognito:        mockCognito,
		EventPublisher: mockPublisher,
		DefaultQuota:   1073741824,
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
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Should publish account.created event
	if !mockPublisher.PublishCalled {
		t.Fatal("expected EventPublisher.Publish to be called")
	}

	if len(mockPublisher.PublishInputs) != 1 {
		t.Fatalf("expected 1 publish call, got %d", len(mockPublisher.PublishInputs))
	}

	published := mockPublisher.PublishInputs[0]
	if published.EventType != "account.created" {
		t.Errorf("expected eventType 'account.created', got %q", published.EventType)
	}
	if published.AccountID != "user-123" {
		t.Errorf("expected accountId 'user-123', got %q", published.AccountID)
	}
	if published.Data["quotaBytes"] != int64(1073741824) {
		t.Errorf("expected data.quotaBytes 1073741824, got %v", published.Data["quotaBytes"])
	}
}

func TestHandler_DoesNotPublishEventWhenAlreadyInitialized(t *testing.T) {
	mockDB := &MockDynamoDB{}
	mockCognito := &MockCognito{}
	mockPublisher := &MockEventPublisher{}

	deps = &Dependencies{
		DB:             mockDB,
		Cognito:        mockCognito,
		EventPublisher: mockPublisher,
		DefaultQuota:   1073741824,
	}

	event := events.CognitoEventUserPoolsPostAuthentication{
		CognitoEventUserPoolsHeader: events.CognitoEventUserPoolsHeader{
			UserPoolID: "ap-southeast-2_abc123",
			UserName:   "testuser",
		},
		Request: events.CognitoEventUserPoolsPostAuthenticationRequest{
			UserAttributes: map[string]string{
				"sub":                        "user-123",
				"custom:account_initialized": "true",
			},
		},
	}

	_, err := handler(context.Background(), event)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Should NOT publish event when already initialized
	if mockPublisher.PublishCalled {
		t.Error("expected EventPublisher.Publish NOT to be called when already initialized")
	}
}

func TestHandler_ContinuesOnEventPublishError(t *testing.T) {
	mockDB := &MockDynamoDB{}
	mockCognito := &MockCognito{}
	mockPublisher := &MockEventPublisher{
		PublishErr: errors.New("SQS error"),
	}

	deps = &Dependencies{
		DB:             mockDB,
		Cognito:        mockCognito,
		EventPublisher: mockPublisher,
		DefaultQuota:   1073741824,
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

	// Should NOT fail the handler even if event publishing fails
	_, err := handler(context.Background(), event)
	if err != nil {
		t.Fatalf("expected no error even when event publishing fails, got %v", err)
	}

	// Verify publish was attempted
	if !mockPublisher.PublishCalled {
		t.Error("expected EventPublisher.Publish to be called")
	}
}

func TestArnToQueueURL(t *testing.T) {
	tests := []struct {
		name     string
		arn      string
		expected string
	}{
		{
			name:     "valid SQS ARN",
			arn:      "arn:aws:sqs:ap-southeast-2:123456789012:my-queue",
			expected: "https://sqs.ap-southeast-2.amazonaws.com/123456789012/my-queue",
		},
		{
			name:     "us-east-1 region",
			arn:      "arn:aws:sqs:us-east-1:999888777666:another-queue",
			expected: "https://sqs.us-east-1.amazonaws.com/999888777666/another-queue",
		},
		{
			name:     "invalid ARN - too few parts",
			arn:      "arn:aws:sqs:region",
			expected: "",
		},
		{
			name:     "empty ARN",
			arn:      "",
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := arnToQueueURL(tc.arn)
			if result != tc.expected {
				t.Errorf("arnToQueueURL(%q) = %q, want %q", tc.arn, result, tc.expected)
			}
		})
	}
}

// MockSQSClient implements SQSClient for testing
type MockSQSClient struct {
	SendMessageCalled  bool
	SendMessageInputs  []MockSendMessageInput
	SendMessageErr     error
	SendMessageResults []*sqs.SendMessageOutput
	callIndex          int
}

type MockSendMessageInput struct {
	QueueURL    string
	MessageBody string
}

func (m *MockSQSClient) SendMessage(ctx context.Context, input *sqs.SendMessageInput, optFns ...func(*sqs.Options)) (*sqs.SendMessageOutput, error) {
	m.SendMessageCalled = true
	m.SendMessageInputs = append(m.SendMessageInputs, MockSendMessageInput{
		QueueURL:    *input.QueueUrl,
		MessageBody: *input.MessageBody,
	})
	if m.SendMessageErr != nil {
		return nil, m.SendMessageErr
	}
	if m.callIndex < len(m.SendMessageResults) {
		result := m.SendMessageResults[m.callIndex]
		m.callIndex++
		return result, nil
	}
	return &sqs.SendMessageOutput{}, nil
}

// MockEventTargetGetter implements EventTargetGetter for testing
type MockEventTargetGetter struct {
	Targets []plugin.AggregatedEventTarget
}

func (m *MockEventTargetGetter) GetEventTargets(eventType string) []plugin.AggregatedEventTarget {
	return m.Targets
}

func TestSQSEventPublisher_Publish_SendsToAllTargets(t *testing.T) {
	mockSQS := &MockSQSClient{}
	mockRegistry := &MockEventTargetGetter{
		Targets: []plugin.AggregatedEventTarget{
			{
				PluginID:   "plugin-a",
				TargetType: "sqs",
				TargetArn:  "arn:aws:sqs:ap-southeast-2:123456789012:queue-a",
			},
			{
				PluginID:   "plugin-b",
				TargetType: "sqs",
				TargetArn:  "arn:aws:sqs:ap-southeast-2:123456789012:queue-b",
			},
		},
	}

	publisher := &SQSEventPublisher{
		sqsClient: mockSQS,
		registry:  mockRegistry,
	}

	payload := EventPayload{
		EventType:  "account.created",
		OccurredAt: "2026-02-01T00:00:00Z",
		AccountID:  "user-123",
		Data: map[string]any{
			"quotaBytes": int64(1073741824),
		},
	}

	err := publisher.Publish(context.Background(), payload)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !mockSQS.SendMessageCalled {
		t.Fatal("expected SQS.SendMessage to be called")
	}

	if len(mockSQS.SendMessageInputs) != 2 {
		t.Fatalf("expected 2 SendMessage calls, got %d", len(mockSQS.SendMessageInputs))
	}

	// Verify first message
	if mockSQS.SendMessageInputs[0].QueueURL != "https://sqs.ap-southeast-2.amazonaws.com/123456789012/queue-a" {
		t.Errorf("unexpected queue URL: %s", mockSQS.SendMessageInputs[0].QueueURL)
	}

	// Verify second message
	if mockSQS.SendMessageInputs[1].QueueURL != "https://sqs.ap-southeast-2.amazonaws.com/123456789012/queue-b" {
		t.Errorf("unexpected queue URL: %s", mockSQS.SendMessageInputs[1].QueueURL)
	}
}

func TestSQSEventPublisher_Publish_NoTargets(t *testing.T) {
	mockSQS := &MockSQSClient{}
	mockRegistry := &MockEventTargetGetter{
		Targets: []plugin.AggregatedEventTarget{},
	}

	publisher := &SQSEventPublisher{
		sqsClient: mockSQS,
		registry:  mockRegistry,
	}

	payload := EventPayload{
		EventType: "account.created",
		AccountID: "user-123",
	}

	err := publisher.Publish(context.Background(), payload)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Should not call SQS when no targets
	if mockSQS.SendMessageCalled {
		t.Error("expected SQS.SendMessage NOT to be called when no targets")
	}
}

func TestSQSEventPublisher_Publish_SkipsNonSQSTargets(t *testing.T) {
	mockSQS := &MockSQSClient{}
	mockRegistry := &MockEventTargetGetter{
		Targets: []plugin.AggregatedEventTarget{
			{
				PluginID:   "plugin-a",
				TargetType: "lambda", // Not SQS
				TargetArn:  "arn:aws:lambda:ap-southeast-2:123456789012:function:my-func",
			},
			{
				PluginID:   "plugin-b",
				TargetType: "sqs",
				TargetArn:  "arn:aws:sqs:ap-southeast-2:123456789012:queue-b",
			},
		},
	}

	publisher := &SQSEventPublisher{
		sqsClient: mockSQS,
		registry:  mockRegistry,
	}

	payload := EventPayload{
		EventType: "account.created",
		AccountID: "user-123",
	}

	err := publisher.Publish(context.Background(), payload)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Should only call SQS once (for the SQS target)
	if len(mockSQS.SendMessageInputs) != 1 {
		t.Errorf("expected 1 SendMessage call, got %d", len(mockSQS.SendMessageInputs))
	}

	if mockSQS.SendMessageInputs[0].QueueURL != "https://sqs.ap-southeast-2.amazonaws.com/123456789012/queue-b" {
		t.Errorf("unexpected queue URL: %s", mockSQS.SendMessageInputs[0].QueueURL)
	}
}

func TestSQSEventPublisher_Publish_ContinuesOnSQSError(t *testing.T) {
	mockSQS := &MockSQSClient{
		SendMessageErr: errors.New("SQS error"),
	}
	mockRegistry := &MockEventTargetGetter{
		Targets: []plugin.AggregatedEventTarget{
			{
				PluginID:   "plugin-a",
				TargetType: "sqs",
				TargetArn:  "arn:aws:sqs:ap-southeast-2:123456789012:queue-a",
			},
			{
				PluginID:   "plugin-b",
				TargetType: "sqs",
				TargetArn:  "arn:aws:sqs:ap-southeast-2:123456789012:queue-b",
			},
		},
	}

	publisher := &SQSEventPublisher{
		sqsClient: mockSQS,
		registry:  mockRegistry,
	}

	payload := EventPayload{
		EventType: "account.created",
		AccountID: "user-123",
	}

	// Should not return error even if SQS fails
	err := publisher.Publish(context.Background(), payload)
	if err != nil {
		t.Fatalf("expected no error even on SQS failures, got %v", err)
	}

	// Should still attempt both targets
	if len(mockSQS.SendMessageInputs) != 2 {
		t.Errorf("expected 2 SendMessage attempts, got %d", len(mockSQS.SendMessageInputs))
	}
}
