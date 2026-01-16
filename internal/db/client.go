package db

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"go.opentelemetry.io/contrib/instrumentation/github.com/aws/aws-sdk-go-v2/otelaws"
)

// Key prefixes for single-table design
const (
	PKPrefixAccount = "ACCOUNT#"
	PKPrefixUser    = "USER#"
	SKMeta          = "META#"
)

// DynamoDBClient defines the interface for DynamoDB operations
type DynamoDBClient interface {
	UpdateItem(ctx context.Context, params *dynamodb.UpdateItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error)
}

// Client wraps DynamoDB operations with OTel tracing
type Client struct {
	ddb       DynamoDBClient
	tableName string
}

// NewClient creates a new DynamoDB client with OTel instrumentation
func NewClient(ctx context.Context, tableName string) (*Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Add OTel instrumentation for X-Ray tracing
	otelaws.AppendMiddlewares(&cfg.APIOptions)

	ddb := dynamodb.NewFromConfig(cfg)

	return &Client{
		ddb:       ddb,
		tableName: tableName,
	}, nil
}

// Account represents an account record in DynamoDB
type Account struct {
	PK                  string `dynamodbav:"pk"`
	SK                  string `dynamodbav:"sk"`
	UserID              string `dynamodbav:"-"` // Derived from PK, not stored
	Owner               string `dynamodbav:"owner"`
	CreatedAt           string `dynamodbav:"createdAt"`
	LastDiscoveryAccess string `dynamodbav:"lastDiscoveryAccess"`
}

// EnsureAccount creates or updates an account record.
// Uses if_not_exists for owner and createdAt (set only on creation),
// and always updates lastDiscoveryAccess.
func (c *Client) EnsureAccount(ctx context.Context, userID string) (*Account, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	pk := PKPrefixAccount + userID
	owner := PKPrefixUser + userID

	// Build key using attributevalue
	key, err := attributevalue.MarshalMap(map[string]string{
		"pk": pk,
		"sk": SKMeta,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal key: %w", err)
	}

	// Build update expression using expression builder
	update := expression.Set(
		expression.Name("owner"),
		expression.IfNotExists(expression.Name("owner"), expression.Value(owner)),
	).Set(
		expression.Name("createdAt"),
		expression.IfNotExists(expression.Name("createdAt"), expression.Value(now)),
	).Set(
		expression.Name("lastDiscoveryAccess"),
		expression.Value(now),
	)

	expr, err := expression.NewBuilder().WithUpdate(update).Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build expression: %w", err)
	}

	input := &dynamodb.UpdateItemInput{
		TableName:                 aws.String(c.tableName),
		Key:                       key,
		UpdateExpression:          expr.Update(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		ReturnValues:              types.ReturnValueAllNew,
	}

	output, err := c.ddb.UpdateItem(ctx, input)
	if err != nil {
		return nil, err
	}

	// Unmarshal response into Account struct
	var account Account
	if err := attributevalue.UnmarshalMap(output.Attributes, &account); err != nil {
		return nil, fmt.Errorf("failed to unmarshal account: %w", err)
	}
	account.UserID = userID

	return &account, nil
}
