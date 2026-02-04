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
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/cloudfront/sign"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/jarrod-lowe/jmap-service-core/internal/db"
	"github.com/jarrod-lowe/jmap-service-core/internal/plugin"
	"github.com/jarrod-lowe/jmap-service-libs/awsinit"
	"github.com/jarrod-lowe/jmap-service-libs/logging"
	"github.com/jarrod-lowe/jmap-service-libs/tracing"
)

var logger = logging.New()

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
	DeletedAt   string `dynamodbav:"deletedAt,omitempty"`
}

// ParsedBlobID contains the parsed components of a potentially composite blobId
type ParsedBlobID struct {
	BaseBlobID string
	HasRange   bool
	StartByte  int64
	EndByte    int64
}

// ParseBlobID parses a blobId which may be composite (baseBlobId,startByte,endByte)
// Returns the parsed components. For simple blobIds, HasRange will be false.
func ParseBlobID(blobID string) (ParsedBlobID, error) {
	parts := strings.Split(blobID, ",")

	switch len(parts) {
	case 1, 2:
		// Simple blobId (no commas or one comma - treated as simple)
		return ParsedBlobID{BaseBlobID: blobID, HasRange: false}, nil

	case 3:
		// Composite blobId: baseBlobId,startByte,endByte
		baseBlobID := parts[0]

		startByte, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return ParsedBlobID{}, fmt.Errorf("invalid start byte: %w", err)
		}

		endByte, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			return ParsedBlobID{}, fmt.Errorf("invalid end byte: %w", err)
		}

		if startByte < 0 {
			return ParsedBlobID{}, fmt.Errorf("start byte must be non-negative")
		}

		if startByte >= endByte {
			return ParsedBlobID{}, fmt.Errorf("start byte must be less than end byte")
		}

		return ParsedBlobID{
			BaseBlobID: baseBlobID,
			HasRange:   true,
			StartByte:  startByte,
			EndByte:    endByte,
		}, nil

	default:
		// Too many commas
		return ParsedBlobID{}, fmt.Errorf("invalid blobId format: too many commas")
	}
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

// PrincipalChecker checks if a caller is allowed to access IAM endpoints
type PrincipalChecker interface {
	IsAllowedPrincipal(callerARN string) bool
}

// Dependencies for handler (injectable for testing)
type Dependencies struct {
	DB            BlobDB
	Signer        URLSigner
	SecretsReader SecretsReader
	Registry      PrincipalChecker
	Config        Config
}

var deps *Dependencies

// handler processes blob download requests
func handler(ctx context.Context, request events.APIGatewayProxyRequest) (Response, error) {
	ctx, span := tracing.StartHandlerSpan(ctx, "BlobDownloadHandler",
		tracing.Function("blob-download"),
		tracing.RequestID(request.RequestContext.RequestID),
	)
	defer span.End()

	// Extract accountId from path
	pathAccountID := request.PathParameters["accountId"]
	if pathAccountID == "" {
		return errorResponse(400, "invalidArguments", "Missing accountId in path")
	}
	span.SetAttributes(tracing.AccountID(pathAccountID))

	// Extract blobId from path
	blobID := request.PathParameters["blobId"]
	if blobID == "" {
		return errorResponse(400, "invalidArguments", "Missing blobId in path")
	}
	span.SetAttributes(tracing.BlobID(blobID))

	// Parse blobId (may be composite: baseBlobId,startByte,endByte)
	parsedBlobID, err := ParseBlobID(blobID)
	if err != nil {
		logger.WarnContext(ctx, "Invalid blobId format",
			slog.String("request_id", request.RequestContext.RequestID),
			slog.String("blob_id", blobID),
			slog.String("error", err.Error()),
		)
		return errorResponse(400, "invalidArguments", "Invalid blobId format")
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

	// Check principal authorization for IAM-authenticated requests
	if isIAMAuthenticatedRequest(request) {
		callerPrincipal := extractCallerPrincipal(request)
		if !deps.Registry.IsAllowedPrincipal(callerPrincipal) {
			logger.WarnContext(ctx, "Unauthorized IAM principal",
				slog.String("request_id", request.RequestContext.RequestID),
				slog.String("caller_principal", callerPrincipal),
			)
			return errorResponse(403, "forbidden", "Principal not authorized for IAM access")
		}
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

	// Look up blob in DynamoDB using base blob ID (without range suffix)
	blob, err := deps.DB.GetBlob(ctx, pathAccountID, parsedBlobID.BaseBlobID)
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

	// Check if blob has been marked as deleted
	if blob.DeletedAt != "" {
		logger.InfoContext(ctx, "Blob is deleted",
			slog.String("request_id", request.RequestContext.RequestID),
			slog.String("account_id", pathAccountID),
			slog.String("blob_id", blobID),
		)
		return errorResponse(404, "notFound", "Blob not found")
	}

	// Generate CloudFront signed URL
	// Use the original blobId (which may include range suffix) so CloudFront function can extract it
	blobURL := fmt.Sprintf("https://%s/blobs/%s/%s", deps.Config.CloudFrontDomain, pathAccountID, blobID)
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

// extractAccountID extracts account ID using authoritative API Gateway signals.
// - IAM auth: Identity.UserArn or Identity.Caller is populated → use path param
// - Cognito auth: Authorizer["claims"]["sub"] is populated → use JWT sub claim
// These fields are populated by API Gateway and cannot be spoofed by clients.
func extractAccountID(request events.APIGatewayProxyRequest) (string, error) {
	identity := request.RequestContext.Identity

	// IAM auth: API Gateway populates Identity.UserArn and/or Identity.Caller
	// These fields cannot be spoofed by the client
	if identity.UserArn != "" || identity.Caller != "" {
		accountID, ok := request.PathParameters["accountId"]
		if !ok || accountID == "" {
			return "", fmt.Errorf("missing accountId path parameter for IAM auth")
		}
		return accountID, nil
	}

	// Cognito auth: API Gateway populates Authorizer with claims
	authorizer := request.RequestContext.Authorizer
	if authorizer == nil {
		return "", fmt.Errorf("no authentication context (neither IAM nor Cognito)")
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

// isIAMAuthenticatedRequest checks if the request is IAM-authenticated
// by checking if UserArn is populated in the request context
func isIAMAuthenticatedRequest(request events.APIGatewayProxyRequest) bool {
	return request.RequestContext.Identity.UserArn != ""
}

// extractCallerPrincipal extracts the caller's IAM principal ARN from the request
func extractCallerPrincipal(request events.APIGatewayProxyRequest) string {
	return request.RequestContext.Identity.UserArn
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

	result, err := awsinit.Init(ctx, awsinit.WithHTTPHandler("blob-download"))
	if err != nil {
		logger.Error("FATAL: Failed to initialize AWS",
			slog.String("error", err.Error()),
		)
		panic(err)
	}
	defer result.Cleanup()

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

	dynamoClient := dynamodb.NewFromConfig(result.Config)
	secretsClient := secretsmanager.NewFromConfig(result.Config)

	// Read private key from Secrets Manager
	secretsReader := NewSecretsManagerReader(secretsClient)
	privateKey, err := secretsReader.GetPrivateKey(result.Ctx, privateKeySecretARN)
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

	// Initialize database client for plugin registry
	dbClient := db.NewClientFromConfig(result.Config, tableName)

	// Load plugin registry for IAM principal authorization
	registry := plugin.NewRegistry()
	if err := registry.LoadFromDynamoDB(result.Ctx, dbClient); err != nil {
		logger.Error("FATAL: Failed to load plugin registry",
			slog.String("error", err.Error()),
		)
		panic(err)
	}

	deps = &Dependencies{
		DB:            NewDynamoDBBlobDB(dynamoClient, tableName),
		Signer:        signer,
		SecretsReader: secretsReader,
		Registry:      registry,
		Config: Config{
			CloudFrontDomain:    cloudfrontDomain,
			CloudFrontKeyPairID: keyPairID,
			PrivateKeySecretARN: privateKeySecretARN,
			SignedURLExpiry:     time.Duration(expirySeconds) * time.Second,
		},
	}

	result.Start(handler)
}
