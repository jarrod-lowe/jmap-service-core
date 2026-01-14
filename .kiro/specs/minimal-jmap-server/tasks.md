# Implementation Plan: Minimal JMAP Server

## Overview

This implementation plan breaks down the minimal JMAP server into discrete coding tasks that build incrementally. The approach starts with core infrastructure setup, implements the JMAP protocol foundation, adds authentication and data storage, and concludes with observability and deployment automation.

## Tasks

- [ ] 1. Project Setup and Infrastructure Foundation
  - Create Go module with proper directory structure
  - Set up Makefile with build, test, deploy, and clean targets
  - Configure cross-compilation for ARM64 Lambda architecture
  - Initialize Terraform configuration for AWS resources
  - _Requirements: Implementation Technology Stack_

- [ ]* 1.1 Write property test for project structure validation
  - **Property 1: Build system consistency**
  - **Validates: Requirements: Build System**

- [ ] 2. Core JMAP Protocol Implementation
  - [ ] 2.1 Implement JMAP request/response envelope structures
    - Define Go structs for JMAP request and response formats
    - Implement JSON marshaling/unmarshaling with proper validation
    - _Requirements: 7.1, 7.2_

  - [ ]* 2.2 Write property test for JMAP envelope parsing
    - **Property 14: JMAP Request Parsing**
    - **Validates: Requirements 7.1**

  - [ ]* 2.3 Write property test for JMAP response formatting
    - **Property 15: JMAP Response Format Compliance**
    - **Validates: Requirements 7.2, 7.5**

  - [ ] 2.4 Implement JMAP method dispatcher
    - Create method routing system for supported JMAP methods
    - Handle unknown methods with proper error responses
    - _Requirements: 7.3, 7.4_

  - [ ]* 2.5 Write property test for unsupported method handling
    - **Property 16: Unsupported Method Handling**
    - **Validates: Requirements 7.3**

  - [ ]* 2.6 Write property test for invalid arguments handling
    - **Property 17: Invalid Arguments Error Handling**
    - **Validates: Requirements 7.4**

- [ ] 3. Authentication and Authorization Framework
  - [ ] 3.1 Implement Cognito JWT validation
    - Parse and validate Cognito JWT tokens
    - Extract accountId from sub claim
    - _Requirements: 6.1, 1.1_

  - [ ]* 3.2 Write property test for JWT processing
    - **Property 13: JWT Processing Consistency**
    - **Validates: Requirements 6.1**

  - [ ] 3.3 Implement IAM authentication handling
    - Process API Gateway IAM context for machine endpoints
    - Extract accountId from path parameters
    - _Requirements: 4.1, 4.2_

  - [ ] 3.4 Implement accountId authorization checks
    - Validate method accountId parameters match authenticated principal
    - Return appropriate JMAP errors for mismatches
    - _Requirements: 2.2, 3.3, 4.2_

  - [ ]* 3.5 Write property test for accountId authorization
    - **Property 3: AccountId Authorization Consistency**
    - **Validates: Requirements 2.2, 3.3, 4.2, 6.3**

  - [ ]* 3.6 Write property test for authentication error handling
    - **Property 2: Authentication Enforcement**
    - **Validates: Requirements 1.4, 2.5, 3.5, 4.5, 6.4**

- [ ] 4. Checkpoint - Core Protocol and Auth
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 5. Data Storage Layer Implementation
  - [ ] 5.1 Implement DynamoDB client and table operations
    - Set up AWS SDK v2 DynamoDB client
    - Implement email record CRUD operations
    - Configure GSI queries for email listing
    - _Requirements: 5.1, 5.2, 5.3_

  - [ ]* 5.2 Write property test for storage key format
    - **Property 9: Storage Key Format Consistency**
    - **Validates: Requirements 5.1, 5.2**

  - [ ]* 5.3 Write property test for GSI query structure
    - **Property 10: GSI Query Structure**
    - **Validates: Requirements 5.3**

  - [ ] 5.4 Implement email deduplication logic
    - Create deduplication table operations
    - Implement Message-ID based duplicate checking
    - _Requirements: 4.3, 5.4_

  - [ ]* 5.5 Write property test for deduplication
    - **Property 8: Email Deduplication Prevention**
    - **Validates: Requirements 4.3**

  - [ ]* 5.6 Write property test for deduplication queries
    - **Property 11: Deduplication Query Consistency**
    - **Validates: Requirements 5.4**

  - [ ] 5.7 Implement S3 blob reference handling
    - Create blobId encoding/decoding for S3 references
    - Implement S3 client for raw email access
    - _Requirements: 5.5_

  - [ ]* 5.8 Write property test for blob references
    - **Property 12: Blob Reference Storage**
    - **Validates: Requirements 5.5**

- [ ] 6. JMAP Method Implementations
  - [ ] 6.1 Implement Email/query method
    - Process query filters and sorting parameters
    - Execute DynamoDB GSI queries with pagination
    - Return email IDs in proper JMAP response format
    - _Requirements: 2.1, 2.3, 2.4_

  - [ ]* 6.2 Write property test for email query ordering
    - **Property 4: Email Query Ordering and Limits**
    - **Validates: Requirements 2.1, 2.3**

  - [ ]* 6.3 Write property test for empty filter handling
    - **Property 5: Empty Filter Returns All Emails**
    - **Validates: Requirements 2.4**

  - [ ] 6.4 Implement Email/get method
    - Batch retrieve email records from DynamoDB
    - Transform database records to JMAP Email objects
    - Handle invalid IDs with notFound responses
    - _Requirements: 3.1, 3.2, 3.4_

  - [ ]* 6.5 Write property test for email retrieval
    - **Property 6: Email Retrieval Completeness**
    - **Validates: Requirements 3.1, 3.2, 3.4**

  - [ ] 6.6 Implement Email/import method
    - Parse import requests and validate blob references
    - Create email records with deduplication checks
    - Return created mappings in JMAP format
    - _Requirements: 4.1, 4.4_

  - [ ]* 6.7 Write property test for email import
    - **Property 7: Email Import Creates Records**
    - **Validates: Requirements 4.1, 4.4**

- [ ] 7. Lambda Function Handlers
  - [ ] 7.1 Implement GetJmapSessionFunction
    - Create Lambda handler for session discovery endpoint
    - Build JMAP Session object with account and capabilities
    - _Requirements: 1.1, 1.2, 1.3_

  - [ ]* 7.2 Write property test for session discovery
    - **Property 1: Session Discovery Returns Valid Structure**
    - **Validates: Requirements 1.1, 1.2, 1.3**

  - [ ] 7.3 Implement JmapApiFunction
    - Create unified Lambda handler for both user and machine endpoints
    - Route requests based on authentication context
    - Process JMAP method calls with error handling
    - _Requirements: 7.5, 8.4_

  - [ ]* 7.4 Write property test for partial failure processing
    - **Property 19: Partial Failure Processing**
    - **Validates: Requirements 8.4**

  - [ ] 7.5 Implement JSON parsing and error handling
    - Add robust JSON parsing with proper error responses
    - Handle malformed requests gracefully
    - _Requirements: 8.1_

  - [ ]* 7.6 Write property test for JSON error handling
    - **Property 18: JSON Parsing Error Handling**
    - **Validates: Requirements 8.1**

- [ ] 8. Checkpoint - Core Functionality Complete
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 9. Observability and Monitoring
  - [ ] 9.1 Implement structured logging
    - Add JSON-formatted logging with correlation IDs
    - Include user context and performance metrics
    - Configure log levels and retention policies
    - _Requirements: Logging and Auditing_

  - [ ] 9.2 Implement X-Ray tracing
    - Add X-Ray SDK integration for distributed tracing
    - Create custom segments for JMAP method processing
    - Instrument DynamoDB and S3 operations
    - _Requirements: X-Ray Distributed Tracing_

  - [ ] 9.3 Add CloudWatch metrics
    - Implement custom metrics for business and operational data
    - Create metric filters for error tracking
    - _Requirements: CloudWatch Dashboard_

- [ ] 10. Infrastructure as Code
  - [ ] 10.1 Create Terraform modules for core resources
    - Define DynamoDB tables with proper GSI configuration
    - Create S3 buckets for raw email storage
    - Set up IAM roles and policies for Lambda functions
    - _Requirements: Infrastructure as Code_

  - [ ] 10.2 Create Terraform modules for API Gateway
    - Configure API Gateway with proper routing
    - Set up Cognito and IAM authorizers
    - Enable request validation and CORS
    - _Requirements: API Gateway Configuration_

  - [ ] 10.3 Create Terraform modules for Lambda functions
    - Define Lambda functions with ARM64 architecture
    - Configure environment variables and VPC settings
    - Set up CloudWatch log groups and X-Ray tracing
    - _Requirements: Lambda Functions, ARM64 Architecture_

  - [ ] 10.4 Create CloudWatch dashboard and alarms
    - Define operational and business metric dashboards
    - Set up alerting for error rates and performance issues
    - Configure log-based alarms for security events
    - _Requirements: CloudWatch Dashboard, Alarms_

- [ ] 11. Build and Deployment Automation
  - [ ] 11.1 Complete Makefile implementation
    - Implement cross-compilation for ARM64 Lambda deployment
    - Add test execution with coverage reporting
    - Create deployment pipeline with Terraform integration
    - _Requirements: Build System_

  - [ ] 11.2 Create deployment scripts
    - Add AWS profile configuration handling
    - Implement environment-specific deployments
    - Create rollback and cleanup procedures
    - _Requirements: AWS Profile Configuration_

- [ ] 12. Documentation and README
  - [ ] 12.1 Create comprehensive README.md
    - Document setup and deployment procedures
    - Include AWS profile configuration instructions
    - Add troubleshooting and operational guides
    - _Requirements: Documentation_

  - [ ] 12.2 Document JMAP compliance and limitations
    - Specify supported vs. unsupported JMAP features
    - Provide migration path for full JMAP compliance
    - Include API compatibility notes for client developers
    - _Requirements: JMAP Protocol Compliance_

- [ ] 13. Final Integration and Validation
  - [ ] 13.1 Run end-to-end integration tests
    - Test complete email ingestion and retrieval flow
    - Validate JMAP protocol compliance with real clients
    - Verify observability and monitoring functionality
    - _Requirements: All Requirements_

  - [ ]* 13.2 Write integration tests for JMAP compliance
    - Test JMAP protocol conformance with standard test suites
    - Validate response formats against JMAP JSON schemas
    - _Requirements: JMAP Protocol Compliance_

- [ ] 14. Final checkpoint - Complete system validation
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation and user feedback
- Property tests validate universal correctness properties from design document
- Unit tests complement property tests by validating specific examples and integration scenarios
- ARM64 Lambda architecture provides cost optimization and performance benefits
- Comprehensive observability ensures operational excellence and compliance