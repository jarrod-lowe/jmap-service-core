# OpenTelemetry Configuration

<!-- cSpell:ignore awsxray parentbased traceidratio fromjson startswith otelcol Analyzes -->

This document describes the OpenTelemetry (OTel) and AWS Distro for OpenTelemetry (ADOT) configuration for the JMAP service Lambda functions.

## Table of Contents

- [Overview](#overview)
- [Architecture](#architecture)
- [ADOT Lambda Layer](#adot-lambda-layer)
- [Collector Configuration](#collector-configuration)
- [Lambda Environment Variables](#lambda-environment-variables)
- [CloudWatch Integration](#cloudwatch-integration)
- [Recent Updates](#recent-updates)
- [Verification](#verification)
- [Troubleshooting](#troubleshooting)

## Overview

Our Lambda functions use OpenTelemetry for distributed tracing with the following components:

- **OpenTelemetry Go SDK**: Instruments application code with custom spans
- **ADOT Lambda Layer**: Provides the OpenTelemetry Collector as a Lambda extension
- **ADOT Collector**: Receives OTLP traces and exports to AWS X-Ray
- **AWS X-Ray**: Visualizes distributed traces and service maps
- **CloudWatch Logs**: Captures application logs with minimal ADOT overhead

## Architecture

```text
┌─────────────────────────────────────────────────────────────┐
│ Lambda Function (ARM64)                                     │
│                                                             │
│  ┌──────────────────────┐                                  │
│  │ Go Application       │                                  │
│  │ - OTel Go SDK        │                                  │
│  │ - Custom Spans       │                                  │
│  │ - xrayconfig         │                                  │
│  └──────────┬───────────┘                                  │
│             │                                               │
│             │ OTLP (gRPC)                                   │
│             │ localhost:4317                                │
│             ▼                                               │
│  ┌──────────────────────┐                                  │
│  │ ADOT Collector       │ ← ADOT Lambda Layer              │
│  │ - OTLP Receiver      │                                  │
│  │ - X-Ray Exporter     │                                  │
│  │ - Error-only logging │                                  │
│  └──────────┬───────────┘                                  │
└─────────────┼───────────────────────────────────────────────┘
              │
              │ AWS X-Ray API
              ▼
    ┌─────────────────────┐         ┌──────────────────────┐
    │ AWS X-Ray           │         │ CloudWatch Logs      │
    │ - Trace storage     │         │ - Application logs   │
    │ - Service map       │         │ - Lambda platform    │
    │ - Trace analytics   │         │ - Anomaly detection  │
    └─────────────────────┘         └──────────────────────┘
```

## ADOT Lambda Layer

### Current Configuration

**File**: `terraform/modules/jmap-service/main.tf`

```hcl
locals {
  # ADOT Collector account ID (constant across regions)
  adot_account_id = "901920570463"

  # ADOT Collector layer name
  adot_layer_name    = "aws-otel-collector-arm64-ver-0-117-0"
  adot_layer_version = "1"

  # Construct ARN dynamically using current region
  adot_layer_arn = "arn:aws:lambda:${data.aws_region.current.id}:${local.adot_account_id}:layer:${local.adot_layer_name}:${local.adot_layer_version}"
}
```

### Layer Details

- **Layer Name**: `aws-otel-collector-arm64-ver-0-117-0`
- **Layer Version**: `1`
- **OpenTelemetry Collector**: v0.117.0
- **ADOT Collector for Lambda**: v0.43.0
- **Architecture**: ARM64 (cost-optimized)
- **Created**: May 30, 2025
- **Status**: ✅ Latest available version (verified January 2026)

### Checking for Updates

To check for newer ADOT layer versions:

```bash
# Test for newer version (replace XXX with version number)
AWS_PROFILE=ses-mail aws lambda get-layer-version-by-arn \
  --arn "arn:aws:lambda:ap-southeast-2:901920570463:layer:aws-otel-collector-arm64-ver-0-XXX-0:1" \
  --region ap-southeast-2

# Success = layer exists and is available
# AccessDeniedException = layer doesn't exist (not published yet)
```

**Update schedule**: Check quarterly or after major OpenTelemetry releases.

## Collector Configuration

### Configuration File

**File**: `collector.yaml` (packaged with Lambda deployment)

```yaml
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: localhost:4317
      http:
        endpoint: localhost:4318

exporters:
  awsxray:
    # Region is automatically detected from Lambda environment

service:
  telemetry:
    logs:
      level: error    # Only show errors, suppress all info/warn
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [awsxray]
```

### Configuration Rationale

**Minimal logging configuration**:

- **No debug exporter**: Removed to eliminate verbose trace dumps in CloudWatch
- **Error-only telemetry**: Suppresses operational info/warn logs from collector
- **No metrics exporter**: Reduces configuration surface, not needed for Lambda

**Benefits**:

- Reduced CloudWatch log volume by 67% (~30 lines → 10 lines per invocation)
- Eliminated 21+ ADOT info-level log messages per invocation
- Lower CloudWatch costs
- Clean, readable logs (only application + Lambda platform logs)
- Full X-Ray tracing functionality maintained

## Lambda Environment Variables

**File**: `terraform/modules/jmap-service/lambda.tf`

### ADOT Collector Configuration

```hcl
# ADOT Collector Configuration
OPENTELEMETRY_COLLECTOR_CONFIG_URI = "file:///var/task/collector.yaml"
OPENTELEMETRY_EXTENSION_LOG_LEVEL  = "error"
```

- `OPENTELEMETRY_COLLECTOR_CONFIG_URI`: Points to custom collector.yaml in Lambda package
- `OPENTELEMETRY_EXTENSION_LOG_LEVEL`: Suppresses extension startup/operational logs (reduces ~21 lines)

### OpenTelemetry SDK Configuration

```hcl
# OpenTelemetry SDK Configuration
OTEL_SERVICE_NAME   = "${local.resource_prefix}-hello-world-${var.environment}"
OTEL_TRACES_SAMPLER = "always_on"

# CRITICAL: Include xray propagator for Lambda context extraction
OTEL_PROPAGATORS = "tracecontext,baggage,xray"

# OTLP Exporter Configuration (points to ADOT Collector)
OTEL_EXPORTER_OTLP_ENDPOINT = "http://localhost:4317"
OTEL_EXPORTER_OTLP_PROTOCOL = "grpc"

# Resource attributes for better trace identification
OTEL_RESOURCE_ATTRIBUTES = "service.version=1.0.0"
```

### Critical Configuration Notes

**X-Ray Propagator**: The `OTEL_PROPAGATORS = "tracecontext,baggage,xray"` setting is **critical** for Lambda context extraction. Without `xray` in this list, Lambda's automatic X-Ray integration won't properly connect with ADOT traces.

**Always-on sampling**: Production uses `always_on` sampling to capture all traces. For high-volume services, consider `parentbased_traceidratio` with a sampling rate.

## CloudWatch Integration

### Log Groups

**Resource**: `terraform/modules/jmap-service/cloudwatch.tf`

```hcl
resource "aws_cloudwatch_log_group" "hello_world_logs" {
  name              = "/aws/lambda/${local.resource_prefix}-hello-world-${var.environment}"
  retention_in_days = var.log_retention_days
}
```

### Log Anomaly Detection

**Added**: January 2026

```hcl
resource "aws_cloudwatch_log_anomaly_detector" "hello_world_anomaly" {
  log_group_arn_list   = [aws_cloudwatch_log_group.hello_world_logs.arn]
  detector_name        = "${local.resource_prefix}-hello-world-anomaly-${var.environment}"
  enabled              = true
  evaluation_frequency = "FIFTEEN_MIN"
}
```

**Features**:

- Machine learning-based anomaly detection
- 15-minute evaluation frequency
- ~1-2 week training period to establish baseline
- Detects unusual error rates, log patterns, and volume changes
- 7-day anomaly visibility window

**View anomalies**: CloudWatch Console → Logs → Log groups → Select group → Anomalies tab

### Error Metric Filter

```hcl
resource "aws_cloudwatch_log_metric_filter" "hello_world_errors" {
  name           = "${local.resource_prefix}-hello-world-errors-${var.environment}"
  log_group_name = aws_cloudwatch_log_group.hello_world_logs.name
  pattern        = "[timestamp, request_id, level = ERROR*, ...]"

  metric_transformation {
    name      = "ErrorCount"
    namespace = "JMAPService/${var.environment}"
    value     = "1"
    unit      = "Count"
  }
}
```

### CloudWatch Alarms

```hcl
resource "aws_cloudwatch_metric_alarm" "hello_world_errors" {
  alarm_name          = "${local.resource_prefix}-hello-world-errors-${var.environment}"
  alarm_description   = "Alerts when hello-world Lambda has errors"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "Errors"
  namespace           = "AWS/Lambda"
  period              = 300
  statistic           = "Sum"
  threshold           = 1
  treat_missing_data  = "notBreaching"
}
```

## Recent Updates

### January 2026 - ADOT Logging Optimization

**Problem**: Excessive ADOT collector logs (~30 lines per invocation, including 21+ info-level messages)

**Changes**:

1. Removed debug exporter from `collector.yaml`
2. Set `service.telemetry.logs.level` to `error` (was `warn`)
3. Added `OPENTELEMETRY_EXTENSION_LOG_LEVEL=error` environment variable
4. Removed unused metrics configuration

**Impact**:

- Reduced log volume from ~30 to 10 lines per invocation (67% reduction)
- Eliminated all ADOT startup/operational info messages
- Reduced CloudWatch costs
- Maintained full X-Ray tracing functionality

**Logs before** (30 lines, noisy):

```text
INIT_START ...
{"level":"info","msg":"Launching OpenTelemetry Lambda extension",...}
{"level":"info","msg":"Subscribing",...}
{"level":"info","msg":"Starting GRPC server",...}
{"level":"info","msg":"Starting HTTP server",...}
{"level":"info","msg":"Serving metrics",...}
[... 15+ more ADOT info logs ...]
TELEMETRY ...
{"level":"INFO","msg":"OpenTelemetry initialized..."}
EXTENSION ...
START RequestId: ...
{"level":"INFO","msg":"Processing request",...}
{"level":"INFO","msg":"Request processed successfully"}
END RequestId: ...
REPORT RequestId: ...
XRAY TraceId: ...
```

**Logs after** (10 lines, clean):

```text
INIT_START ...
TELEMETRY Name: collector State: Subscribed
{"level":"INFO","msg":"OpenTelemetry initialized..."}
EXTENSION Name: collector State: Ready
START RequestId: ...
{"level":"INFO","msg":"Processing request",...}
{"level":"INFO","msg":"Request processed successfully"}
END RequestId: ...
REPORT RequestId: ...
XRAY TraceId: ...
```

### January 2026 - CloudWatch Anomaly Detection

**Added**: Log anomaly detector for automated detection of unusual patterns

**Configuration**:

- Evaluation frequency: 15 minutes
- Analyzes all logs (no filter pattern)
- Training period: 1-2 weeks for baseline establishment
- Detects: error rate changes, new error patterns, volume anomalies

### January 2026 - ADOT Layer Verification

**Verified**: Using latest available ADOT Lambda layer

- Current: `aws-otel-collector-arm64-ver-0-117-0:1`
- Tested versions 0-118-0 through 0-122-0: not available yet
- Layer created: May 30, 2025 (current production version)
- Next check: April 2026 (quarterly)

## Verification

### Verify Tracing is Working

```bash
# 1. Invoke Lambda
LAMBDA_URL=$(cd terraform/environments/test && AWS_PROFILE=ses-mail terraform output -raw hello_world_function_url)
curl -s $LAMBDA_URL

# 2. Get trace ID from logs
TRACE_ID=$(AWS_PROFILE=ses-mail aws logs tail /aws/lambda/jmap-service-core-hello-world-test --since 5m --format short | grep "XRAY TraceId" | tail -1 | awk '{print $3}')

# 3. Wait for X-Ray indexing
sleep 45

# 4. Verify trace contains custom span
AWS_PROFILE=ses-mail aws xray batch-get-traces \
  --trace-ids $TRACE_ID \
  --region ap-southeast-2 \
  --output json | jq '.Traces[0].Segments[] | (.Document | fromjson) | {name: .name, subsegments: [.subsegments[]?.name]}'
```

**Expected output**:

```json
{
  "name": "jmap-service-core-hello-world-test",
  "subsegments": [
    "HelloWorldHandler"
  ]
}
```

### Verify Log Volume Reduction

```bash
# Invoke Lambda
curl -s $LAMBDA_URL

# Wait for logs
sleep 10

# Count log lines
AWS_PROFILE=ses-mail aws logs tail /aws/lambda/jmap-service-core-hello-world-test --since 3m --format short | wc -l
```

**Expected**: ~10 lines per cold start invocation

### Verify No ADOT Info Logs

```bash
AWS_PROFILE=ses-mail aws logs tail /aws/lambda/jmap-service-core-hello-world-test --since 3m --format short | grep -E "level.*info|Launching|Subscribing|Starting|Serving|Traces.*debug"
```

**Expected**: Empty output (no matches)

### View X-Ray Service Map

1. Open AWS X-Ray console
2. Navigate to Service Map
3. Verify `jmap-service-core-hello-world-test` appears
4. Verify connections to downstream services (future: DynamoDB, S3)

### Check Anomaly Detector Status

```bash
AWS_PROFILE=ses-mail aws logs list-log-anomaly-detectors --region ap-southeast-2 | jq '.anomalyDetectors[] | select(.detectorName | startswith("jmap-service-core"))'
```

## Troubleshooting

### No Traces in X-Ray

**Symptoms**: Lambda executes successfully but no traces appear in X-Ray

**Checks**:

1. Verify ADOT layer is attached: Check Lambda console → Configuration → Layers
2. Check IAM permissions: Lambda role needs `AWSXRayDaemonWriteAccess` policy
3. Verify environment variables:

   ```bash
   AWS_PROFILE=ses-mail aws lambda get-function-configuration \
     --function-name jmap-service-core-hello-world-test \
     --region ap-southeast-2 | jq '.Environment.Variables | {OPENTELEMETRY_COLLECTOR_CONFIG_URI, OTEL_PROPAGATORS}'
   ```

4. Check for collector errors:

   ```bash
   AWS_PROFILE=ses-mail aws logs tail /aws/lambda/jmap-service-core-hello-world-test --since 10m --format short | grep -i error
   ```

### ADOT Info Logs Still Appearing

**Symptoms**: Still seeing "Launching", "Starting", "Subscribing" messages

**Solution**:

1. Verify `OPENTELEMETRY_EXTENSION_LOG_LEVEL=error` is set
2. Verify `collector.yaml` has `service.telemetry.logs.level: error`
3. Redeploy Lambda:

   ```bash
   AWS_PROFILE=ses-mail make apply ENV=test
   ```

### X-Ray Shows Traces but Missing Custom Spans

**Symptoms**: Traces appear but no `HelloWorldHandler` subsegment

**Checks**:

1. Verify Go code uses `xrayconfig.NewTracerProvider`:

   ```go
   tp, err := xrayconfig.NewTracerProvider(context.Background())
   otel.SetTracerProvider(tp)
   ```

2. Verify `otellambda.InstrumentHandler` wraps handler
3. Check for initialization errors in CloudWatch Logs

### High CloudWatch Costs

**Actions**:

1. Verify log volume reduction: Should be ~10 lines per invocation
2. Check log retention: Set appropriately in `cloudwatch.tf`
3. Review anomaly detector frequency: 15 minutes is optimal for most cases
4. Consider log sampling for very high-volume functions

### Collector Startup Failures

**Symptoms**: Lambda returns 500 error, "unable to start, otelcol state is Closed"

**Common causes**:

1. Invalid `collector.yaml` syntax
2. Empty exporter arrays in pipelines
3. Missing required configuration sections

**Solution**:

1. Validate collector.yaml against OpenTelemetry schema
2. Ensure at least one exporter in each pipeline
3. Check CloudWatch Logs for specific error messages

## References

- [AWS ADOT Lambda Documentation](https://aws-otel.github.io/docs/getting-started/lambda/)
- [OpenTelemetry Collector Configuration](https://opentelemetry.io/docs/collector/configuration/)
- [OpenTelemetry Go SDK](https://opentelemetry.io/docs/languages/go/)
- [AWS X-Ray Developer Guide](https://docs.aws.amazon.com/xray/latest/devguide/aws-xray.html)
- [CloudWatch Logs Anomaly Detection](https://docs.aws.amazon.com/AmazonCloudWatch/latest/logs/LogsAnomalyDetection.html)

## Maintenance Schedule

| Task                             | Frequency | Next Due      |
| -------------------------------- | --------- | ------------- |
| Check for ADOT layer updates     | Quarterly | April 2026    |
| Review anomaly detection alerts  | Weekly    | Ongoing       |
| Review X-Ray service map         | Weekly    | Ongoing       |
| Verify trace completeness        | Monthly   | February 2026 |
| Audit CloudWatch costs           | Monthly   | February 2026 |
