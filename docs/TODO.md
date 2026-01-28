# Future Work Items

## Blob Upload Enhancements (basic upload complete)

- [ ] Monitoring/alarm for stale pending blobs (>5 minutes old)
- [x] Blob download endpoint (GET /download/{accountId}/{blobId})
- [x] Blob delete endpoint (DELETE /delete-iam/{accountId}/{blobId}) with async cleanup
- [ ] Presigned URL flow for uploads >10MB (bypassing API Gateway limit)

## General Improvements

- [ ] Email/query implementation
- [ ] Email/get implementation
- [ ] Email/import implementation with Message-ID deduplication
