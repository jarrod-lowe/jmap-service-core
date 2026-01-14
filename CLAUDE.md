# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a **JMAP (JSON Meta Application Protocol) email server** implementation designed for AWS serverless infrastructure. The system provides server-side email ingestion, storage, and retrieval through standard JMAP endpoints with dual authentication:

- **User authentication**: Cognito JWT for client applications
- **Machine authentication**: AWS IAM SigV4 for automated email ingestion pipelines

## Build and Development Commands

We will use a Makefile for presenting all the operations to the use (such as plans, cleans, applies, etc). Terraform will be used for infrastructure. See `../ses-mail` for an example of a project using those.

## Architecture Overview

### High-Level Components

**API Gateway Routes**:

- `GET /.well-known/jmap` → Session discovery (Cognito auth) → `GetJmapSessionFunction`
- `POST /jmap` → User JMAP API (Cognito auth) → `JmapApiFunction`
- `POST /jmap-iam/{accountId}` → Machine ingestion (IAM auth) → `JmapApiFunction`

**Lambda Functions** (Go, ARM64):

1. **GetJmapSessionFunction**: Returns JMAP session object with account info and capabilities
2. **JmapApiFunction**: Unified handler for both user and machine JMAP requests, dispatches to method implementations

**Data Storage**:

Use a single dynamodb table (with single table design) for data. Use S3 for blob (e.g. message) storage. In the future, there may need to be some other storage for vectors for search.

### Authentication Flow

**User Flow (Cognito JWT)**:

1. API Gateway validates JWT and extracts `sub` claim as accountId
2. Lambda enforces accountId matching in all method arguments
3. Only the user's own emails are accessible

**Machine Flow (AWS IAM)**:

1. Ingestion pipeline signs requests with SigV4
2. Path parameter `{accountId}` is authoritative

Machine flow will be used for ingest and testing.

### JMAP Methods Supported (MVP)

The `/jmap` and `jmap-iam/{accountId}` endpoints will be served by the same lambda. The first methods to implement will be:

- `Email/query`: List emails with filtering (empty filter only), sorting by receivedAt desc, pagination
- `Email/get`: Retrieve email objects by ID with properties: id, receivedAt, from, to, subject, snippet
- `Email/import`: Import emails with deduplication based on Message-ID header

Returns `unknownMethod` error while processing other calls in same request

## Key Design Decisions

### Deduplication

- Uses Message-ID header to prevent duplicate email imports
- Critical for handling SES ingestion retries

### Authorization Model

- All method calls validate accountId matches authenticated principal
- User endpoints: accountId = JWT `sub` claim (through a function; as this will change in the future)
- Machine endpoints: accountId = path parameter `{accountId}`
- Rejects mismatches with JMAP error responses

### Error Handling

- HTTP-level: 400 (invalid JSON), 401/403 (auth), 500 (server errors)
- JMAP-level: Per-call error tuples for `unknownMethod`, `invalidArguments`, etc.
- Partial failure support: Processes all method calls independently
- Graceful degradation for DynamoDB/S3 failures

## Testing Strategy

### Unit Testing

- Co-located with source files using `_test.go` suffix
- Covers specific scenarios, edge cases, error conditions
- Integration tests for end-to-end JMAP request/response flows
- Use the TDD superpower for all go code

## Infrastructure as Code (Terraform)

**AWS Profile**: All operations use `AWS_PROFILE` environment variable

**Resources**:

- API Gateway with Cognito and IAM authorizers, request validation
- Lambda functions (ARM64 architecture for cost optimization)
- DynamoDB tables with GSI configuration
- S3 buckets for raw email storage
- CloudWatch dashboards, alarms, and log groups
- X-Ray tracing configuration

## Observability

### Structured Logging

- JSON format with correlation IDs, accountId context, performance metrics
- Application logs: 30 days retention

### CloudWatch Metrics and Alarms

- Operational: Lambda duration/errors, DynamoDB throttling, API Gateway latency
- Business: Email volumes, JMAP method usage, auth patterns
- Alarms: Error rates >1% for 5 minutes, Lambda timeouts, unusual auth failures

### X-Ray Tracing

- End-to-end traces from API Gateway → Lambda → DynamoDB/S3
- Custom segments for JMAP method processing
- Service map visualization and latency analysis

## Important Implementation Notes

### JMAP Protocol Compliance

- Full compliance with RFC 8620 (JMAP Core) for implemented methods
- Partial compliance with RFC 8621 (JMAP Mail) for MVP subset
- Session discovery follows standard JMAP authentication flow
- Method calls processed in order with proper response correlation

### MVP Exclusions (Deliberate)

- Threads (no `threadId` on emails)
- Mail submission
- Mailbox CRUD operations
- Full-text search / advanced filtering
- Push notifications
- Public blob upload/download URLs

### Future Extension Points

- Multiple accounts per user (Session.accounts map expands)
- Thread support via `threadId` attribute
- Blob download URLs for third-party JMAP clients
- Richer Email/query filtering and text indexing
- Push event notifications

## Email Ingestion Flow (SES → JMAP)

A separate project is responsible for managing the SMTP listener. It will call the `/jmap-iam/{accountId}` endpoint to send emails to this JMAP server.

## Development Workflow

When implementing new features or fixing issues:

1. **Review requirements and design docs** before writing code
2. **Implement using the TDD superpower** for go code use TDD (although note that you need to write stubs so tests can pass)
  a. **Validate JMAP compliance** ensure tests for protocol check against RFC 8620/8621 for implemented methods
3. **Add CloudWatch metrics** and structured logging for new operations
4. **Document** any new features, details, or deviations from standard JMAP behaviour
5. **Compile/Plan/Build and Deploy**

## Critical Constraints

- **ARM64 Lambda architecture** required for cost optimization
- **AWS_PROFILE environment variable** must be respected by all AWS operations
- **accountId validation** is mandatory for all JMAP methods - never skip
- **Deduplication on Message-ID** is critical - prevents phantom duplicate emails
- **Partial failure processing** - never abort entire request due to single method failure
- **Structured logging** - JSON. Always include correlation ID and accountId context
