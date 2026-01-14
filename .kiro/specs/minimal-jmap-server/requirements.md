# Requirements Document

## Introduction

This document specifies the requirements for a minimal JMAP (JSON Meta Application Protocol) email server implementation. The system provides server-side email ingestion, storage, and retrieval capabilities through standard JMAP endpoints, supporting both user authentication via Cognito and machine authentication via AWS IAM for email ingestion pipelines.

## Glossary

- **JMAP_Server**: The email server system implementing JMAP protocol endpoints
- **Email_Ingestion_Pipeline**: The automated system that processes inbound emails from SES
- **Session_Discovery_Endpoint**: The `/.well-known/jmap` endpoint for JMAP session discovery
- **User_API_Endpoint**: The `/jmap` endpoint for authenticated user requests
- **Machine_API_Endpoint**: The `/jmap-iam/{accountId}` endpoint for machine ingestion
- **Account_ID**: The unique identifier for a user account (Cognito sub claim)
- **Email_Object**: A JMAP Email object representing an email message
- **Blob_ID**: An opaque identifier referencing raw email content in S3

## Requirements

### Requirement 1: JMAP Session Discovery

**User Story:** As a JMAP client, I want to discover session information, so that I can authenticate and access the JMAP API endpoints.

#### Acceptance Criteria

1. WHEN a client requests `GET /.well-known/jmap` with valid Cognito JWT, THE JMAP_Server SHALL return a session object with API URL and capabilities
2. WHEN the session object is returned, THE JMAP_Server SHALL include exactly one account with accountId matching the JWT sub claim
3. WHEN the session object is returned, THE JMAP_Server SHALL include core and mail capabilities in the capabilities object
4. IF the Cognito JWT is invalid or missing, THEN THE JMAP_Server SHALL return HTTP 401 Unauthorized

### Requirement 2: User Email Querying

**User Story:** As an authenticated user, I want to query my emails, so that I can retrieve a list of my messages.

#### Acceptance Criteria

1. WHEN a user calls `Email/query` via `/jmap` endpoint, THE JMAP_Server SHALL return email IDs sorted by receivedAt descending
2. WHEN the accountId in method arguments differs from JWT sub claim, THE JMAP_Server SHALL reject the request with JMAP error
3. WHEN a limit parameter is provided, THE JMAP_Server SHALL return at most that number of email IDs
4. WHEN an empty filter is provided, THE JMAP_Server SHALL return all emails for the account
5. IF the user is not authenticated with valid Cognito JWT, THEN THE JMAP_Server SHALL return HTTP 401 Unauthorized

### Requirement 3: User Email Retrieval

**User Story:** As an authenticated user, I want to get email details, so that I can read the content and metadata of my messages.

#### Acceptance Criteria

1. WHEN a user calls `Email/get` with valid email IDs, THE JMAP_Server SHALL return email objects with metadata
2. WHEN email objects are returned, THE JMAP_Server SHALL include id, receivedAt, from, to, and subject properties
3. WHEN the accountId in method arguments differs from JWT sub claim, THE JMAP_Server SHALL reject the request with JMAP error
4. WHEN invalid email IDs are requested, THE JMAP_Server SHALL return notFound array with those IDs
5. IF the user is not authenticated with valid Cognito JWT, THEN THE JMAP_Server SHALL return HTTP 401 Unauthorized

### Requirement 4: Machine Email Ingestion

**User Story:** As an email ingestion pipeline, I want to import emails via machine authentication, so that inbound emails can be stored in user accounts.

#### Acceptance Criteria

1. WHEN the pipeline calls `Email/import` via `/jmap-iam/{accountId}` with AWS IAM authentication, THE JMAP_Server SHALL create email records in DynamoDB
2. WHEN the accountId in method arguments differs from path accountId, THE JMAP_Server SHALL reject the request with JMAP error
3. WHEN duplicate emails are imported based on Message-ID header, THE JMAP_Server SHALL prevent duplicate storage and return existing email ID
4. WHEN email import succeeds, THE JMAP_Server SHALL return created mapping with new email IDs
5. IF the caller lacks valid AWS IAM authentication, THEN THE JMAP_Server SHALL return HTTP 401 or 403

### Requirement 5: Email Storage and Persistence

**User Story:** As the system, I want to store email data reliably, so that emails are preserved and can be retrieved efficiently.

#### Acceptance Criteria

1. WHEN an email is imported, THE JMAP_Server SHALL store email metadata in DynamoDB with partition key `acct#{accountId}` and sort key `email#{emailId}`
2. WHEN storing email metadata, THE JMAP_Server SHALL include emailId, accountId, receivedAt, from, to, subject, and blob reference
3. WHEN querying emails by receivedAt, THE JMAP_Server SHALL use GSI with GSI1PK `acct#{accountId}` and GSI1SK `recvAt#{receivedAt}#email#{emailId}`
4. WHEN checking for duplicates, THE JMAP_Server SHALL query by accountId and Message-ID header combination
5. THE JMAP_Server SHALL reference raw email content in S3 using blobId or S3 bucket/key information

### Requirement 6: Authentication and Authorization

**User Story:** As a system administrator, I want secure authentication and authorization, so that only authorized users and systems can access email data.

#### Acceptance Criteria

1. WHEN user endpoints are accessed, THE JMAP_Server SHALL validate Cognito JWT tokens and extract accountId from sub claim
2. WHEN machine endpoints are accessed, THE JMAP_Server SHALL validate AWS IAM SigV4 authentication
3. WHEN method calls include accountId parameters, THE JMAP_Server SHALL verify they match the authenticated principal's accountId
4. WHEN authentication fails, THE JMAP_Server SHALL return appropriate HTTP error codes (401/403)
5. THE JMAP_Server SHALL enforce least-privilege access where machine principals cannot access user endpoints

### Requirement 7: JMAP Protocol Compliance

**User Story:** As a JMAP client developer, I want standard JMAP protocol compliance, so that I can use standard JMAP libraries and tools.

#### Acceptance Criteria

1. WHEN processing JMAP requests, THE JMAP_Server SHALL parse using and methodCalls arrays according to JMAP specification
2. WHEN returning JMAP responses, THE JMAP_Server SHALL format methodResponses array according to JMAP specification
3. WHEN unsupported methods are called, THE JMAP_Server SHALL return unknownMethod error for those calls while processing supported calls
4. WHEN method arguments are invalid, THE JMAP_Server SHALL return invalidArguments error for those calls
5. THE JMAP_Server SHALL maintain JMAP call ordering and return responses in the same order as requests

### Requirement 8: Error Handling and Resilience

**User Story:** As a system operator, I want robust error handling, so that the system gracefully handles failures and provides useful diagnostics.

#### Acceptance Criteria

1. WHEN invalid JSON is received, THE JMAP_Server SHALL return HTTP 400 Bad Request
2. WHEN DynamoDB operations fail, THE JMAP_Server SHALL return appropriate JMAP error responses
3. WHEN S3 operations fail during blob resolution, THE JMAP_Server SHALL handle errors gracefully
4. WHEN processing multiple method calls, THE JMAP_Server SHALL continue processing remaining calls if individual calls fail
5. THE JMAP_Server SHALL log structured error information for debugging and monitoring