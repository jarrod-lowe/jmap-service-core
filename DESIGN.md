# Design document (MVP)

## Overview

This MVP provides:

* A **standard JMAP Session discovery endpoint**: `GET /.well-known/jmap`
* A **standard JMAP API endpoint for user reads**: `POST /jmap` (Cognito JWT)
* A **JMAP API endpoint for machine ingestion**: `POST /jmap-iam/{accountId}` (AWS_IAM SigV4)

Scope deliberately excludes:

* threads
* mail submission
* mailbox CRUD
* full-text search / advanced filters
* push notifications
* blob upload/download URLs for third-party clients

The intended first client is *your own* (so we can start with a very small JMAP Mail surface).

## Authentication & authorization

### User calls (`/.well-known/jmap`, `/jmap`)

* API Gateway enforces **Cognito User Pool** authorizer.
* The user identity is extracted from JWT claims:

  * `sub` is treated as the effective `accountId` (MVP).
* Authorization rule (MVP):

  * All JMAP method args `accountId` must equal JWT `sub` (or server rejects with JMAP error).

### Machine calls (`/jmap-iam/{accountId}`)

* API Gateway enforces **AWS_IAM** (SigV4).
* The caller is a role assumed by Step Functions or Lambda (SES ingestion pipeline).
* Authorization rule:

  * Path parameter `{accountId}` is authoritative.
  * Any `accountId` in the JMAP method args must match `{accountId}`.

IAM policy (conceptual) should be least-privilege:

* `execute-api:Invoke` only on `POST /jmap-iam/*`
* no access to `/jmap` or `/.well-known/jmap`

## High-level components

### API Gateway (REST or HTTP API)

* Stage name: `v1`
* Routes:

  * `GET /.well-known/jmap` → `GetJmapSessionFunction` (Lambda proxy)
  * `POST /jmap` → `JmapApiFunction` (Lambda proxy)
  * `POST /jmap-iam/{accountId}` → `JmapApiFunction` (same Lambda proxy)

### Lambda functions

1. **GetJmapSessionFunction**
2. **JmapApiFunction** (shared handler for both `/jmap` and `/jmap-iam/{accountId}`)

(You can keep it to these two Lambdas in the MVP.)

## Infrastructure Implementation Status

### ✅ Completed (Phase 1 - Pipeline Validation)

**Build Pipeline:**

* Go module setup with AWS SDK v2 + Powertools v2
* Makefile-driven build system for linux/arm64 Lambda compilation
* Per-lambda build directories (build/{lambda-name}/)
* Automated zipping and packaging

**Lambda Functions:**

* `hello-world` - Dummy Lambda demonstrating observability patterns
  * Location: `cmd/hello-world/main.go`
  * Structured JSON logging via AWS Lambda Powertools for Go v2
  * X-Ray tracing with custom subsegments and annotations
  * CloudWatch custom metrics
  * Returns: `{"status":"ok","message":"Hello from JMAP service"}`

**Infrastructure (Terraform):**

* Module organization: `terraform/modules/jmap-service/`
* Multi-environment: test and prod configurations
* S3 state backend with auto-provisioning
* IAM roles with least-privilege policies (logs, X-Ray, metrics only)
* CloudWatch log groups with retention policies (7d test, 30d prod)
* CloudWatch metric filters and alarms for errors
* Lambda Function URLs for testing (NONE authorization)

**Observability:**

* CloudWatch Logs with structured JSON format
* X-Ray active tracing with custom subsegments
* CloudWatch custom metrics (RequestCount in JMAPService namespace)
* Error monitoring and alarms

### 🚧 Not Yet Implemented

**API Layer:**

* API Gateway with REST API
* Cognito User Pool and authorizer
* IAM authorizer for machine access
* Request validation
* CORS configuration

**Data Storage:**

* DynamoDB single-table design
* S3 bucket for email blobs
* GSI for email queries

**JMAP Functions:**

* GetJmapSessionFunction (`GET /.well-known/jmap`)
* JmapApiFunction (`POST /jmap` and `POST /jmap-iam/{accountId}`)

**JMAP Methods:**

* Email/import (with Message-ID deduplication)
* Email/query (with filtering and pagination)
* Email/get (with property selection)

**Deployment Commands:**

```bash
# Build and deploy to test environment
make build ENV=test
make package ENV=test
make init ENV=test
make plan ENV=test
make apply ENV=test

# Test the deployment
make outputs ENV=test  # Get Function URL
curl -X POST <function-url>  # Should return {"status":"ok","message":"Hello from JMAP service"}
```

## Storage model (minimal but workable)

### S3

* Stores immutable raw RFC 5322 message blobs (written by SES receipt rule).
* Key scheme example:

  * `raw/{accountId}/{yyyy}/{mm}/{dd}/{ingestId}.eml`

#### DynamoDB (minimal)

1. `Emails` table

   * Partition key: `pk` = `acct#{accountId}`
   * Sort key: `sk` = `email#{emailId}`
   * Attributes:

     * `emailId`
     * `accountId`
     * `receivedAt` (ISO string)
     * parsed fields: `from`, `to`, `subject`
     * `snippet` (optional)
     * `rawBucket`, `rawKey` (or a `blobId` mapping)
     * `messageIdHeader` (optional)
     * `sizeBytes` (optional)

2. GSI for listing newest-first

   * GSI1PK: `acct#{accountId}`
   * GSI1SK: `recvAt#{receivedAt}#email#{emailId}`
   * This supports `Email/query` sorted by receivedAt desc.

3. (Recommended) `EmailDedupe` table (or GSI)

   * Prevent duplicate ingestion of the same message
   * Keyed by: `acct#{accountId}` + `msgid#{Message-Id}` (or `sesMessageId` as fallback)

Why this matters: retries in ingestion are normal; dedupe prevents duplicate messages and “phantom mail”.

## Inbound ingestion flow (SES → S3 → Step Functions/Lambda → JMAP import)

### Step 1: SES receives mail

* SES receipt rule stores raw message in S3.

### Step 2: Orchestrator triggers

* Either:

  * S3 event → EventBridge → Step Functions
  * or S3 event → Lambda (MVP acceptable)

### Step 3: Determine target accountId

* Use recipient mapping logic (you control):

  * For MVP: map `RCPT TO` address → Cognito user `sub` (accountId)
  * This can be a DynamoDB lookup or deterministic mapping.

### Step 4: Call your API

* Orchestrator calls:

  * `POST https://.../v1/jmap-iam/{accountId}`
* Signed with SigV4 using the orchestrator’s IAM role.

### Step 5: JMAP method invoked for ingest

* MVP method: **`Email/import`**
* Inputs include:

  * `receivedAt`
  * `keywords` (e.g. `$seen: false`)
  * a way to reference the raw message blob

#### Blob reference strategy (MVP)

To keep MVP small without implementing JMAP upload/download URLs yet, pick one of:

A) “Server-known blobId” (recommended MVP pragmatic approach):

* Define `blobId` as an opaque token that your server can resolve to S3.
* For example, a base64url encoding of `{bucket}:{key}` or an internal ID stored in DynamoDB.
* Only your ingestion pipeline uses it; your first UI doesn’t need to fetch raw blobs.

B) Add download/proxy later:

* When you want third-party JMAP client compatibility, you can later implement `downloadUrl` / blob endpoints properly. MVP doesn’t need that.

## JMAP surface supported in MVP

You are *not* implementing all of JMAP Mail; you’re implementing a **minimal subset** behind a JMAP envelope.

### User endpoint `/jmap` (Cognito)

Support:

* `Email/query`:

  * filter `{}` (match all) only (or a tiny subset later)
  * sort by `receivedAt`
  * limit
* `Email/get`:

  * returns metadata needed by your client:

    * `id`
    * `receivedAt`
    * `from`, `to`, `subject`
    * optionally `snippet`
    * optionally `headers` list (if you want)

### Machine endpoint `/jmap-iam/{accountId}` (SigV4)

Support:

* `Email/import`:

  * validates `{accountId}` matches
  * reads S3 metadata (optional) and records email in DynamoDB
  * runs dedupe check
  * returns created ids and errors per JMAP set/import semantics (minimal)

Unsupported methods:

* return a JMAP error response tuple for that call (e.g., `unknownMethod`), while preserving partial success for other calls.

## Lambda responsibilities

### GetJmapSessionFunction

Input:

* API Gateway event with Cognito authorizer context

Behaviour:

1. Extract `sub` from JWT.
2. Build Session object:

   * `apiUrl`: points to `/jmap`
   * `capabilities`: include core + mail (even if minimal)
   * `accounts`: a map with a single entry whose key is `sub`
3. Return JSON 200.

Notes:

* Keep `downloadUrl`, `uploadUrl`, push URLs absent for now.
* This endpoint is stable even if you later add multiple accounts.

### JmapApiFunction

One handler for both `/jmap` and `/jmap-iam/{accountId}`.

Input:

* API Gateway event includes:

  * `routeKey` / path
  * auth context (Cognito claims) OR SigV4 principal data
  * path params (for `accountId` on IAM route)
  * body: JMAP request

Behaviour (shared):

1. Parse JSON body into:

   * `using`
   * `methodCalls`
2. Determine effective `accountId`:

   * If Cognito route: `accountId = jwt.sub`
   * If IAM route: `accountId = pathParams.accountId`
3. For each methodCall in order:

   * Validate capability usage minimally (`core`, `mail`)
   * Validate `accountId` in args matches effective accountId
   * Dispatch:

     * `Email/query` → DynamoDB query on GSI (desc order), return ids
     * `Email/get` → DynamoDB batch-get by ids, return requested properties
     * `Email/import` → dedupe check → create record(s) → return created mapping
     * else → return `error` tuple for that call
4. Return JMAP `methodResponses` array.

Error handling:

* Invalid JSON → HTTP 400
* Invalid method args → return per-call `error` tuple, keep processing others
* IAM principal not allowed (defence-in-depth) → HTTP 403 or per-call error

Idempotency:

* `Email/import` must dedupe using Message-Id header if available; otherwise fallback to SES receipt id + S3 key.

## Observability

Minimum recommended:

* Structured logs with:

  * request id, route, effective accountId
  * method names
  * counts (imported emails, queried ids)
  * dedupe hits
* CloudWatch metrics:

  * `IngestedCount`
  * `DedupeHitCount`
  * `UserQueryCount`
  * `ErrorsByType`
* Correlation:

  * include SES receipt id / S3 key in ingest logs

## Future-proof seams (without building them now)

This MVP is intentionally minimal, but it’s set up so later you can add:

* multiple accounts in Session (`accounts` map expands)
* authorization check `sub -> accountId list`
* threads (`Thread` objects) and `threadId` on Email
* blob download URLs (`downloadUrl`) and raw fetch
* richer `Email/query` filtering and text indexing
* push event source URL

None of those require changing the fundamental endpoints.
