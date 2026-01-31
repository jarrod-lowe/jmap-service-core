package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/jarrod-lowe/jmap-service-core/internal/db"
	"github.com/jarrod-lowe/jmap-service-core/internal/plugin"
)

var (
	logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
)

// AccountDB handles DynamoDB operations for account metadata
type AccountDB interface {
	CreateAccountMeta(ctx context.Context, accountID string, quotaBytes int64) error
}

// CognitoClient handles Cognito operations
type CognitoClient interface {
	SetUserAttribute(ctx context.Context, userPoolID, username, attrName, attrValue string) error
}

// EventPayload represents a system event notification sent to plugin SQS queues
type EventPayload struct {
	EventType  string         `json:"eventType"`
	OccurredAt string         `json:"occurredAt"`
	AccountID  string         `json:"accountId"`
	Data       map[string]any `json:"data,omitempty"`
}

// EventPublisher publishes events to subscribed plugins
type EventPublisher interface {
	Publish(ctx context.Context, payload EventPayload) error
}

// SQSClient is the interface for SQS operations
type SQSClient interface {
	SendMessage(ctx context.Context, params *sqs.SendMessageInput, optFns ...func(*sqs.Options)) (*sqs.SendMessageOutput, error)
}

// EventTargetGetter provides event targets from the plugin registry
type EventTargetGetter interface {
	GetEventTargets(eventType string) []plugin.AggregatedEventTarget
}

// SQSEventPublisher publishes events to SQS queues
type SQSEventPublisher struct {
	sqsClient SQSClient
	registry  EventTargetGetter
}

// Publish sends the event to all registered SQS targets
func (p *SQSEventPublisher) Publish(ctx context.Context, payload EventPayload) error {
	targets := p.registry.GetEventTargets(payload.EventType)
	if len(targets) == 0 {
		logger.InfoContext(ctx, "No event targets registered",
			slog.String("event_type", payload.EventType))
		return nil
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal event payload: %w", err)
	}

	for _, target := range targets {
		if target.TargetType != "sqs" {
			logger.WarnContext(ctx, "Unknown target type, skipping",
				slog.String("target_type", target.TargetType),
				slog.String("plugin_id", target.PluginID))
			continue
		}

		queueURL := arnToQueueURL(target.TargetArn)

		_, err := p.sqsClient.SendMessage(ctx, &sqs.SendMessageInput{
			QueueUrl:    aws.String(queueURL),
			MessageBody: aws.String(string(body)),
		})
		if err != nil {
			logger.ErrorContext(ctx, "Failed to publish event",
				slog.String("plugin_id", target.PluginID),
				slog.String("queue_url", queueURL),
				slog.String("error", err.Error()))
			// Continue to other targets, don't fail the account init
		} else {
			logger.InfoContext(ctx, "Published event",
				slog.String("event_type", payload.EventType),
				slog.String("plugin_id", target.PluginID))
		}
	}
	return nil
}

// Dependencies for handler (injectable for testing)
type Dependencies struct {
	DB             AccountDB
	Cognito        CognitoClient
	EventPublisher EventPublisher
	DefaultQuota   int64
}

var deps *Dependencies

// handler processes Cognito Post Authentication trigger events
func handler(ctx context.Context, event events.CognitoEventUserPoolsPostAuthentication) (events.CognitoEventUserPoolsPostAuthentication, error) {
	// Check if already initialized
	if event.Request.UserAttributes["custom:account_initialized"] == "true" {
		logger.InfoContext(ctx, "Account already initialized, skipping",
			slog.String("username", event.UserName),
		)
		return event, nil
	}

	// Get sub (account ID)
	accountID := event.Request.UserAttributes["sub"]
	if accountID == "" {
		logger.ErrorContext(ctx, "Missing sub attribute in user attributes",
			slog.String("username", event.UserName),
		)
		return event, fmt.Errorf("missing sub attribute")
	}

	logger.InfoContext(ctx, "Initializing account",
		slog.String("account_id", accountID),
		slog.String("username", event.UserName),
	)

	// Create account META# record in DynamoDB
	if err := deps.DB.CreateAccountMeta(ctx, accountID, deps.DefaultQuota); err != nil {
		logger.ErrorContext(ctx, "Failed to create account metadata",
			slog.String("account_id", accountID),
			slog.String("error", err.Error()),
		)
		return event, fmt.Errorf("failed to create account metadata: %w", err)
	}

	// Publish account.created event to subscribed plugins
	if deps.EventPublisher != nil {
		eventPayload := EventPayload{
			EventType:  "account.created",
			OccurredAt: time.Now().UTC().Format(time.RFC3339),
			AccountID:  accountID,
			Data: map[string]any{
				"quotaBytes": deps.DefaultQuota,
			},
		}
		if err := deps.EventPublisher.Publish(ctx, eventPayload); err != nil {
			logger.ErrorContext(ctx, "Failed to publish account.created event",
				slog.String("account_id", accountID),
				slog.String("error", err.Error()),
			)
			// Don't fail account init if event publishing fails
		}
	}

	// Set account_initialized attribute in Cognito
	if err := deps.Cognito.SetUserAttribute(ctx, event.UserPoolID, event.UserName, "custom:account_initialized", "true"); err != nil {
		logger.ErrorContext(ctx, "Failed to set account_initialized attribute",
			slog.String("account_id", accountID),
			slog.String("error", err.Error()),
		)
		return event, fmt.Errorf("failed to set account_initialized attribute: %w", err)
	}

	logger.InfoContext(ctx, "Account initialized successfully",
		slog.String("account_id", accountID),
	)

	return event, nil
}

// DynamoDBAccountDB implements AccountDB using AWS DynamoDB
type DynamoDBAccountDB struct {
	client    *dynamodb.Client
	tableName string
}

// NewDynamoDBAccountDB creates a new DynamoDBAccountDB
func NewDynamoDBAccountDB(client *dynamodb.Client, tableName string) *DynamoDBAccountDB {
	return &DynamoDBAccountDB{
		client:    client,
		tableName: tableName,
	}
}

// CreateAccountMeta creates the account META# record with default quota
func (d *DynamoDBAccountDB) CreateAccountMeta(ctx context.Context, accountID string, quotaBytes int64) error {
	now := time.Now().UTC().Format(time.RFC3339)

	item := map[string]any{
		"pk":                       fmt.Sprintf("ACCOUNT#%s", accountID),
		"sk":                       "META#",
		"accountType":              "default",
		"pendingAllocationsCount":  0,
		"quotaBytes":               quotaBytes,
		"quotaRemaining":           quotaBytes,
		"createdAt":                now,
		"updatedAt":                now,
	}

	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return fmt.Errorf("failed to marshal item: %w", err)
	}

	// Use condition to prevent overwriting existing record
	_, err = d.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(d.tableName),
		Item:                av,
		ConditionExpression: aws.String("attribute_not_exists(pk)"),
	})
	if err != nil {
		// Check if it's a condition check failure (record already exists)
		var ccf *ddbtypes.ConditionalCheckFailedException
		if ok := errors.As(err, &ccf); ok {
			// Record already exists, this is OK (idempotent)
			logger.InfoContext(ctx, "Account metadata already exists",
				slog.String("account_id", accountID),
			)
			return nil
		}
		return err
	}

	return nil
}

// CognitoIDP implements CognitoClient using AWS Cognito
type CognitoIDP struct {
	client *cognitoidentityprovider.Client
}

// NewCognitoIDP creates a new CognitoIDP
func NewCognitoIDP(client *cognitoidentityprovider.Client) *CognitoIDP {
	return &CognitoIDP{client: client}
}

// SetUserAttribute sets a user attribute in Cognito
func (c *CognitoIDP) SetUserAttribute(ctx context.Context, userPoolID, username, attrName, attrValue string) error {
	_, err := c.client.AdminUpdateUserAttributes(ctx, &cognitoidentityprovider.AdminUpdateUserAttributesInput{
		UserPoolId: aws.String(userPoolID),
		Username:   aws.String(username),
		UserAttributes: []types.AttributeType{
			{
				Name:  aws.String(attrName),
				Value: aws.String(attrValue),
			},
		},
	})
	return err
}

// arnToQueueURL converts an SQS ARN to a queue URL
// arn:aws:sqs:region:account:queue-name -> https://sqs.region.amazonaws.com/account/queue-name
func arnToQueueURL(arn string) string {
	parts := strings.Split(arn, ":")
	if len(parts) != 6 {
		return ""
	}
	region := parts[3]
	account := parts[4]
	queueName := parts[5]
	return fmt.Sprintf("https://sqs.%s.amazonaws.com/%s/%s", region, account, queueName)
}

func main() {
	ctx := context.Background()

	// Get required environment variables
	tableName := os.Getenv("DYNAMODB_TABLE")
	if tableName == "" {
		logger.Error("FATAL: DYNAMODB_TABLE environment variable is required")
		panic("DYNAMODB_TABLE environment variable is required")
	}

	defaultQuotaStr := os.Getenv("DEFAULT_QUOTA_BYTES")
	if defaultQuotaStr == "" {
		logger.Error("FATAL: DEFAULT_QUOTA_BYTES environment variable is required")
		panic("DEFAULT_QUOTA_BYTES environment variable is required")
	}

	defaultQuota, err := strconv.ParseInt(defaultQuotaStr, 10, 64)
	if err != nil {
		logger.Error("FATAL: DEFAULT_QUOTA_BYTES must be a valid integer",
			slog.String("value", defaultQuotaStr),
			slog.String("error", err.Error()),
		)
		panic("DEFAULT_QUOTA_BYTES must be a valid integer")
	}

	// Initialize AWS clients
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		logger.Error("FATAL: Failed to load AWS config",
			slog.String("error", err.Error()),
		)
		panic(err)
	}

	dynamoClient := dynamodb.NewFromConfig(cfg)
	cognitoClient := cognitoidentityprovider.NewFromConfig(cfg)
	sqsClient := sqs.NewFromConfig(cfg)

	// Load plugin registry for event publishing
	dbClient, err := db.NewClient(ctx, tableName)
	if err != nil {
		logger.Error("FATAL: Failed to create DB client",
			slog.String("error", err.Error()),
		)
		panic(err)
	}

	registry := plugin.NewRegistry()
	if err := registry.LoadFromDynamoDB(ctx, dbClient); err != nil {
		logger.Error("FATAL: Failed to load plugin registry",
			slog.String("error", err.Error()),
		)
		panic(err)
	}

	deps = &Dependencies{
		DB:             NewDynamoDBAccountDB(dynamoClient, tableName),
		Cognito:        NewCognitoIDP(cognitoClient),
		EventPublisher: &SQSEventPublisher{sqsClient: sqsClient, registry: registry},
		DefaultQuota:   defaultQuota,
	}

	lambda.Start(handler)
}
