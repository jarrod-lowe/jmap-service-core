package main

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/cloudfront/sign"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

var (
	logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
)

// BlobDB handles DynamoDB operations for blob metadata
type BlobDB interface {
	GetBlob(ctx context.Context, accountID, blobID string) (*BlobRecord, error)
}

// URLSigner generates CloudFront signed URLs
type URLSigner interface {
	Sign(url string, expiry time.Time) (string, error)
}

// SecretsReader reads secrets from Secrets Manager
type SecretsReader interface {
	GetPrivateKey(ctx context.Context, secretARN string) (string, error)
}

// BlobRecord represents a blob record from DynamoDB
type BlobRecord struct {
	BlobID      string `dynamodbav:"blobId"`
	AccountID   string `dynamodbav:"accountId"`
	Size        int64  `dynamodbav:"size"`
	ContentType string `dynamodbav:"contentType"`
	S3Key       string `dynamodbav:"s3Key"`
	CreatedAt   string `dynamodbav:"createdAt"`
}

// ErrorResponse is the error response format
type ErrorResponse struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

// Response is the API Gateway proxy response
type Response struct {
	StatusCode int               `json:"statusCode"`
	Headers    map[string]string `json:"headers"`
	Body       string            `json:"body"`
}

// Config holds application configuration
type Config struct {
	CloudFrontDomain   string
	CloudFrontKeyPairID string
	PrivateKeySecretARN string
	SignedURLExpiry    time.Duration
}

// Dependencies for handler (injectable for testing)
type Dependencies struct {
	DB            BlobDB
	Signer        URLSigner
	SecretsReader SecretsReader
	Config        Config
}

var deps *Dependencies

// handler processes blob download requests
func handler(ctx context.Context, request events.APIGatewayProxyRequest) (Response, error) {
	// Extract accountId from path
	pathAccountID := request.PathParameters["accountId"]
	if pathAccountID == "" {
		return errorResponse(400, "invalidArguments", "Missing accountId in path")
	}

	// Extract blobId from path
	blobID := request.PathParameters["blobId"]
	if blobID == "" {
		return errorResponse(400, "invalidArguments", "Missing blobId in path")
	}

	// Extract authenticated account ID (from JWT or path for IAM)
	authAccountID, err := extractAccountID(request)
	if err != nil {
		logger.WarnContext(ctx, "Failed to extract account ID",
			slog.String("request_id", request.RequestContext.RequestID),
			slog.String("error", err.Error()),
		)
		return errorResponse(401, "unauthorized", "Missing or invalid authentication")
	}

	// Validate path accountId matches authenticated accountId
	if pathAccountID != authAccountID {
		logger.WarnContext(ctx, "Account ID mismatch",
			slog.String("request_id", request.RequestContext.RequestID),
			slog.String("path_account_id", pathAccountID),
			slog.String("auth_account_id", authAccountID),
		)
		return errorResponse(403, "forbidden", "Account ID mismatch")
	}

	// Look up blob in DynamoDB
	blob, err := deps.DB.GetBlob(ctx, pathAccountID, blobID)
	if err != nil {
		logger.ErrorContext(ctx, "Failed to get blob from DynamoDB",
			slog.String("request_id", request.RequestContext.RequestID),
			slog.String("error", err.Error()),
		)
		return errorResponse(500, "serverFail", "Failed to retrieve blob metadata")
	}

	// Check if blob exists
	if blob == nil {
		logger.InfoContext(ctx, "Blob not found",
			slog.String("request_id", request.RequestContext.RequestID),
			slog.String("account_id", pathAccountID),
			slog.String("blob_id", blobID),
		)
		return errorResponse(404, "notFound", "Blob not found")
	}

	// Verify blob ownership (defense in depth - should match since we query by account)
	if blob.AccountID != pathAccountID {
		logger.WarnContext(ctx, "Blob ownership mismatch",
			slog.String("request_id", request.RequestContext.RequestID),
			slog.String("blob_account_id", blob.AccountID),
			slog.String("request_account_id", pathAccountID),
		)
		return errorResponse(404, "notFound", "Blob not found")
	}

	// Generate CloudFront signed URL
	blobURL := fmt.Sprintf("https://%s/blobs/%s", deps.Config.CloudFrontDomain, blob.S3Key)
	expiry := time.Now().Add(deps.Config.SignedURLExpiry)

	signedURL, err := deps.Signer.Sign(blobURL, expiry)
	if err != nil {
		logger.ErrorContext(ctx, "Failed to sign URL",
			slog.String("request_id", request.RequestContext.RequestID),
			slog.String("error", err.Error()),
		)
		return errorResponse(500, "serverFail", "Failed to generate download URL")
	}

	logger.InfoContext(ctx, "Blob download redirect",
		slog.String("request_id", request.RequestContext.RequestID),
		slog.String("account_id", pathAccountID),
		slog.String("blob_id", blobID),
	)

	return Response{
		StatusCode: 302,
		Headers: map[string]string{
			"Location":      signedURL,
			"Cache-Control": "no-store",
		},
		Body: "",
	}, nil
}

// extractAccountID extracts account ID from path parameter or JWT claims
func extractAccountID(request events.APIGatewayProxyRequest) (string, error) {
	// For IAM auth routes (/download-iam/), use path parameter
	if accountID, ok := request.PathParameters["accountId"]; ok && accountID != "" {
		// Check if this is an IAM auth route (no Cognito claims)
		if request.RequestContext.Authorizer == nil {
			return accountID, nil
		}
		// If there are claims, we're on Cognito route - use JWT sub
		if _, hasClaims := request.RequestContext.Authorizer["claims"]; !hasClaims {
			return accountID, nil
		}
	}

	// Fall back to Cognito JWT claims
	authorizer := request.RequestContext.Authorizer
	if authorizer == nil {
		return "", fmt.Errorf("no authorizer context")
	}

	claims, ok := authorizer["claims"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("no claims in authorizer")
	}

	sub, ok := claims["sub"].(string)
	if !ok || sub == "" {
		return "", fmt.Errorf("sub claim not found or empty")
	}

	return sub, nil
}

// errorResponse builds an error response
func errorResponse(statusCode int, errorType, description string) (Response, error) {
	body, _ := json.Marshal(ErrorResponse{Type: errorType, Description: description})
	return Response{
		StatusCode: statusCode,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       string(body),
	}, nil
}

// =============================================================================
// Real implementations
// =============================================================================

// DynamoDBBlobDB implements BlobDB using AWS DynamoDB
type DynamoDBBlobDB struct {
	client    *dynamodb.Client
	tableName string
}

// NewDynamoDBBlobDB creates a new DynamoDBBlobDB
func NewDynamoDBBlobDB(client *dynamodb.Client, tableName string) *DynamoDBBlobDB {
	return &DynamoDBBlobDB{
		client:    client,
		tableName: tableName,
	}
}

// GetBlob retrieves a blob record from DynamoDB
func (d *DynamoDBBlobDB) GetBlob(ctx context.Context, accountID, blobID string) (*BlobRecord, error) {
	result, err := d.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(d.tableName),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: fmt.Sprintf("ACCOUNT#%s", accountID)},
			"sk": &types.AttributeValueMemberS{Value: fmt.Sprintf("BLOB#%s", blobID)},
		},
	})
	if err != nil {
		return nil, err
	}

	if result.Item == nil {
		return nil, nil
	}

	var record BlobRecord
	if err := attributevalue.UnmarshalMap(result.Item, &record); err != nil {
		return nil, err
	}

	return &record, nil
}

// CloudFrontURLSigner implements URLSigner using CloudFront SDK
type CloudFrontURLSigner struct {
	signer *sign.URLSigner
}

// NewCloudFrontURLSigner creates a new CloudFrontURLSigner
func NewCloudFrontURLSigner(keyPairID, privateKeyPEM string) (*CloudFrontURLSigner, error) {
	// Parse the PEM-encoded private key
	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	var privateKey *rsa.PrivateKey
	var err error

	// Try PKCS#1 first, then PKCS#8
	privateKey, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		// Try PKCS#8
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse private key: %w", err)
		}
		var ok bool
		privateKey, ok = key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("private key is not RSA")
		}
	}

	signer := sign.NewURLSigner(keyPairID, privateKey)
	return &CloudFrontURLSigner{signer: signer}, nil
}

// Sign generates a signed URL for the given resource
func (s *CloudFrontURLSigner) Sign(url string, expiry time.Time) (string, error) {
	signedURL, err := s.signer.Sign(url, expiry)
	if err != nil {
		return "", fmt.Errorf("failed to generate signed URL: %w", err)
	}

	return signedURL, nil
}

// SecretsManagerReader implements SecretsReader using AWS Secrets Manager
type SecretsManagerReader struct {
	client *secretsmanager.Client
}

// NewSecretsManagerReader creates a new SecretsManagerReader
func NewSecretsManagerReader(client *secretsmanager.Client) *SecretsManagerReader {
	return &SecretsManagerReader{client: client}
}

// GetPrivateKey retrieves the private key from Secrets Manager
func (s *SecretsManagerReader) GetPrivateKey(ctx context.Context, secretARN string) (string, error) {
	result, err := s.client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(secretARN),
	})
	if err != nil {
		return "", err
	}

	if result.SecretString == nil {
		return "", fmt.Errorf("secret value is empty")
	}

	return *result.SecretString, nil
}

func main() {
	ctx := context.Background()

	// Get required environment variables
	tableName := os.Getenv("DYNAMODB_TABLE")
	if tableName == "" {
		logger.Error("FATAL: DYNAMODB_TABLE environment variable is required")
		panic("DYNAMODB_TABLE environment variable is required")
	}

	cloudfrontDomain := os.Getenv("CLOUDFRONT_DOMAIN")
	if cloudfrontDomain == "" {
		logger.Error("FATAL: CLOUDFRONT_DOMAIN environment variable is required")
		panic("CLOUDFRONT_DOMAIN environment variable is required")
	}

	keyPairID := os.Getenv("CLOUDFRONT_KEY_PAIR_ID")
	if keyPairID == "" {
		logger.Error("FATAL: CLOUDFRONT_KEY_PAIR_ID environment variable is required")
		panic("CLOUDFRONT_KEY_PAIR_ID environment variable is required")
	}

	privateKeySecretARN := os.Getenv("PRIVATE_KEY_SECRET_ARN")
	if privateKeySecretARN == "" {
		logger.Error("FATAL: PRIVATE_KEY_SECRET_ARN environment variable is required")
		panic("PRIVATE_KEY_SECRET_ARN environment variable is required")
	}

	expirySeconds := 300 // default 5 minutes
	if expiryStr := os.Getenv("SIGNED_URL_EXPIRY_SECONDS"); expiryStr != "" {
		if parsed, err := strconv.Atoi(expiryStr); err == nil {
			expirySeconds = parsed
		}
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
	secretsClient := secretsmanager.NewFromConfig(cfg)

	// Read private key from Secrets Manager
	secretsReader := NewSecretsManagerReader(secretsClient)
	privateKey, err := secretsReader.GetPrivateKey(ctx, privateKeySecretARN)
	if err != nil {
		logger.Error("FATAL: Failed to read private key from Secrets Manager",
			slog.String("error", err.Error()),
		)
		panic(err)
	}

	// Create CloudFront URL signer
	signer, err := NewCloudFrontURLSigner(keyPairID, privateKey)
	if err != nil {
		logger.Error("FATAL: Failed to create CloudFront signer",
			slog.String("error", err.Error()),
		)
		panic(err)
	}

	deps = &Dependencies{
		DB:            NewDynamoDBBlobDB(dynamoClient, tableName),
		Signer:        signer,
		SecretsReader: secretsReader,
		Config: Config{
			CloudFrontDomain:    cloudfrontDomain,
			CloudFrontKeyPairID: keyPairID,
			PrivateKeySecretARN: privateKeySecretARN,
			SignedURLExpiry:     time.Duration(expirySeconds) * time.Second,
		},
	}

	lambda.Start(handler)
}
