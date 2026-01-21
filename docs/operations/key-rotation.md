# CloudFront Signing Key Rotation

## Overview

CloudFront signed URLs for blob downloads use RSA key pairs managed by Terraform. The private key is auto-generated using `tls_private_key` and stored in AWS Secrets Manager. The public key is registered with CloudFront.

A Lambda function runs daily to check key age and publish a CloudWatch metric (`KeyAgeDays`). An alarm triggers when the key exceeds the configured age threshold (default: 180 days).

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│ Terraform                                                    │
│  tls_private_key.current ──┬──► aws_cloudfront_public_key   │
│  tls_private_key.previous ─┘    ──► aws_cloudfront_key_group│
│         │                                                    │
│         └──► aws_secretsmanager_secret_version              │
│         └──► aws_ssm_parameter (creation timestamp)         │
└─────────────────────────────────────────────────────────────┘
                              │
┌─────────────────────────────────────────────────────────────┐
│ Alerting                                                     │
│  EventBridge (daily) ──► Lambda (key-age-check)             │
│                              │                               │
│                              ▼                               │
│                    CloudWatch Metric (KeyAgeDays)           │
│                              │                               │
│                              ▼                               │
│                    CloudWatch Alarm (age > threshold)       │
└─────────────────────────────────────────────────────────────┘
```

## When to Rotate

- **CloudWatch alarm fires** - Key age exceeds configured threshold (default: 180 days)
- **Security incident** - Suspected key compromise requires immediate rotation

## Rotation Procedure

Key rotation uses a three-phase approach to ensure zero downtime:

### Phase 1: Start Rotation (Dual Keys Active)

```bash
# Deploy with both old and new keys active
AWS_PROFILE=ses-mail terraform apply \
  -var="cloudfront_signing_key_rotation_phase=rotating" \
  -target=module.jmap_service
```

This creates:
- A new RSA key pair (current)
- Keeps the old key (previous) active in CloudFront key group
- Updates Secrets Manager with the new private key
- Lambda starts signing with the new key

Both keys are valid during this phase, so existing signed URLs continue to work.

### Phase 2: Wait for URL Expiry

Wait for the signed URL TTL to expire (default: 5 minutes, configured by `signed_url_expiry_seconds`).

```bash
# Wait for signed URL TTL plus buffer
sleep 360  # 6 minutes to be safe
```

This ensures any URLs signed with the old key have expired.

### Phase 3: Complete Rotation (Remove Old Key)

```bash
# Remove the old key, keeping only the new one
AWS_PROFILE=ses-mail terraform apply \
  -var="cloudfront_signing_key_rotation_phase=complete" \
  -target=module.jmap_service
```

### Phase 4: Return to Normal

```bash
# Final apply without rotation variables (uses defaults)
AWS_PROFILE=ses-mail terraform apply -target=module.jmap_service

# Update the SSM timestamp to reflect new key creation time
aws ssm put-parameter \
  --name "/jmap-service/test/cloudfront-key-created-at" \
  --value "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --overwrite
```

## Verification

After rotation, verify blob downloads still work:

```bash
# Get auth token (use your test user credentials)
TOKEN=$(...)

# Request a download redirect
curl -v -H "Authorization: Bearer $TOKEN" \
  "https://{domain}/v1/download/{accountId}/{blobId}"

# Should return 302 with Location header containing signed URL
# Following the redirect should return blob content
```

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `cloudfront_signing_key_rotation_phase` | `normal` | Rotation phase: `normal`, `rotating`, `complete` |
| `cloudfront_signing_key_max_age_days` | `180` | Days before alarm triggers |
| `signed_url_expiry_seconds` | `300` | Signed URL TTL (affects wait time in rotation) |

## Security Notes

- **Terraform state contains private key** - Ensure S3 backend has `encrypt = true`
- **Never share private key** - It's auto-generated and stored only in Secrets Manager
- **Rotate immediately if compromised** - Follow emergency rotation procedure below

## Emergency Rotation (Key Compromise)

If the key is suspected to be compromised:

1. **Immediately rotate** - Run phases 1-4 above without waiting
2. **Accept brief service disruption** - Some in-flight downloads may fail
3. **Monitor for unauthorized access** - Check CloudFront access logs
4. **Investigate compromise** - Audit access to Secrets Manager and Terraform state

## TODO: Automatic Rotation

Future enhancement: Use Terraform's `time_rotating` resource to automatically trigger rotation every N days, eliminating manual intervention. This would require:

- `time_rotating` resource with `rotation_days`
- `keepers` block on `tls_private_key` to trigger regeneration
- Careful handling of the dual-key transition period
- Automated SSM timestamp update
