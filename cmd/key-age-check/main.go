package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/jarrod-lowe/jmap-service-libs/logging"
	"github.com/jarrod-lowe/jmap-service-libs/tracing"
	"go.opentelemetry.io/contrib/instrumentation/github.com/aws/aws-lambda-go/otellambda"
	"go.opentelemetry.io/contrib/instrumentation/github.com/aws/aws-lambda-go/otellambda/xrayconfig"
	"go.opentelemetry.io/contrib/instrumentation/github.com/aws/aws-sdk-go-v2/otelaws"
	"go.opentelemetry.io/otel"
)

var logger = logging.New()

// SSMReader reads parameters from SSM Parameter Store
type SSMReader interface {
	GetParameter(ctx context.Context, name string) (string, error)
}

// MetricsPublisher publishes metrics to CloudWatch
type MetricsPublisher interface {
	PublishMetric(ctx context.Context, name string, value float64) error
}

// Config holds application configuration
type Config struct {
	SSMParameterName string
	MetricNamespace  string
}

// Dependencies for handler (injectable for testing)
type Dependencies struct {
	SSMReader        SSMReader
	MetricsPublisher MetricsPublisher
	Config           Config
}

var deps *Dependencies

// checkKeyAge reads the key creation timestamp and publishes age metric
func checkKeyAge(ctx context.Context) error {
	// Read timestamp from SSM
	timestamp, err := deps.SSMReader.GetParameter(ctx, deps.Config.SSMParameterName)
	if err != nil {
		return fmt.Errorf("failed to read SSM parameter: %w", err)
	}

	// Parse timestamp
	createdAt, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return fmt.Errorf("failed to parse timestamp: %w", err)
	}

	// Calculate age in days
	age := time.Since(createdAt)
	ageDays := age.Hours() / 24

	// Publish metric
	if err := deps.MetricsPublisher.PublishMetric(ctx, "KeyAgeDays", ageDays); err != nil {
		return fmt.Errorf("failed to publish metric: %w", err)
	}

	logger.InfoContext(ctx, "Key age check completed",
		slog.Float64("age_days", ageDays),
		slog.String("created_at", timestamp),
	)

	return nil
}

// handler is the Lambda entry point
func handler(ctx context.Context) error {
	return checkKeyAge(ctx)
}

// =============================================================================
// Real implementations
// =============================================================================

// SSMParameterReader implements SSMReader using AWS SSM
type SSMParameterReader struct {
	client *ssm.Client
}

// NewSSMParameterReader creates a new SSMParameterReader
func NewSSMParameterReader(client *ssm.Client) *SSMParameterReader {
	return &SSMParameterReader{client: client}
}

// GetParameter retrieves a parameter from SSM
func (r *SSMParameterReader) GetParameter(ctx context.Context, name string) (string, error) {
	result, err := r.client.GetParameter(ctx, &ssm.GetParameterInput{
		Name: aws.String(name),
	})
	if err != nil {
		return "", err
	}

	if result.Parameter == nil || result.Parameter.Value == nil {
		return "", fmt.Errorf("parameter value is empty")
	}

	return *result.Parameter.Value, nil
}

// CloudWatchMetricsPublisher implements MetricsPublisher using CloudWatch
type CloudWatchMetricsPublisher struct {
	client    *cloudwatch.Client
	namespace string
}

// NewCloudWatchMetricsPublisher creates a new CloudWatchMetricsPublisher
func NewCloudWatchMetricsPublisher(client *cloudwatch.Client, namespace string) *CloudWatchMetricsPublisher {
	return &CloudWatchMetricsPublisher{
		client:    client,
		namespace: namespace,
	}
}

// PublishMetric publishes a metric to CloudWatch
func (p *CloudWatchMetricsPublisher) PublishMetric(ctx context.Context, name string, value float64) error {
	_, err := p.client.PutMetricData(ctx, &cloudwatch.PutMetricDataInput{
		Namespace: aws.String(p.namespace),
		MetricData: []types.MetricDatum{
			{
				MetricName: aws.String(name),
				Value:      aws.Float64(value),
				Unit:       types.StandardUnitCount,
			},
		},
	})
	return err
}

func main() {
	ctx := context.Background()

	// Initialize tracer provider
	tp, err := tracing.Init(ctx)
	if err != nil {
		logger.Error("FATAL: Failed to initialize tracer provider",
			slog.String("error", err.Error()),
		)
		panic(err)
	}
	otel.SetTracerProvider(tp)

	// Create cold start span - all init AWS calls become children
	ctx, coldStartSpan := tracing.StartColdStartSpan(ctx, "key-age-check")
	defer coldStartSpan.End()

	// Get required environment variables
	ssmParameterName := os.Getenv("SSM_PARAMETER_NAME")
	if ssmParameterName == "" {
		logger.Error("FATAL: SSM_PARAMETER_NAME environment variable is required")
		panic("SSM_PARAMETER_NAME environment variable is required")
	}

	metricNamespace := os.Getenv("METRIC_NAMESPACE")
	if metricNamespace == "" {
		logger.Error("FATAL: METRIC_NAMESPACE environment variable is required")
		panic("METRIC_NAMESPACE environment variable is required")
	}

	// Initialize AWS clients
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		logger.Error("FATAL: Failed to load AWS config",
			slog.String("error", err.Error()),
		)
		panic(err)
	}
	otelaws.AppendMiddlewares(&cfg.APIOptions)

	ssmClient := ssm.NewFromConfig(cfg)
	cwClient := cloudwatch.NewFromConfig(cfg)

	deps = &Dependencies{
		SSMReader:        NewSSMParameterReader(ssmClient),
		MetricsPublisher: NewCloudWatchMetricsPublisher(cwClient, metricNamespace),
		Config: Config{
			SSMParameterName: ssmParameterName,
			MetricNamespace:  metricNamespace,
		},
	}

	lambda.Start(otellambda.InstrumentHandler(handler, xrayconfig.WithRecommendedOptions(tp)...))
}
