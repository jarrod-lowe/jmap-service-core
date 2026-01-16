# DynamoDB Account Record Design

## Overview

Single-table DynamoDB design for JMAP service. This document covers the first record type: Account metadata.

## Table Structure

**Table Name:** `jmap-service-core-data-${env}`

| Setting | Value |
|---------|-------|
| Billing Mode | PAY_PER_REQUEST |
| Primary Key | `pk` (String) |
| Sort Key | `sk` (String) |
| Point-in-time Recovery | Enabled |

## Account Record

The account record tracks user account metadata and discovery access patterns.

**Keys:**
- `pk`: `ACCOUNT#{userId}` (e.g., `ACCOUNT#abc123`)
- `sk`: `META`

**Attributes:**

| Attribute | Type | Description |
|-----------|------|-------------|
| `owner` | String | `USER#{userId}` - supports future multi-account scenarios |
| `createdAt` | String (ISO8601) | Set once on first access |
| `lastDiscoveryAccess` | String (ISO8601) | Updated on each `/.well-known/jmap` request |

## Behavior

### Auto-creation on First Access

When a user hits `GET /.well-known/jmap`:
1. Lambda extracts `userId` from Cognito JWT `sub` claim
2. Uses `UpdateItem` with `if_not_exists` to atomically:
   - Create record if missing (sets `owner`, `createdAt`)
   - Always update `lastDiscoveryAccess`
3. Returns JMAP session response

No pre-provisioning required - accounts self-create on first discovery request.

### Update Expression

```
SET #owner = if_not_exists(#owner, :owner),
    #createdAt = if_not_exists(#createdAt, :now),
    #lastDiscoveryAccess = :now
```

This handles both create and update in a single atomic operation with no race conditions.

## Future Extensions

- GSI on `owner` when multi-account support is needed
- Additional record types (EMAIL#, MAILBOX#, etc.) will share this table
- `userId = accountId` assumption will change; `owner` field prepares for this
