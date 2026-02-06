package blobcomplete

import (
	"context"
	"fmt"

	"github.com/jarrod-lowe/jmap-service-core/internal/bloballocate"
)

// CompleteRequest is the Blob/complete method request
type CompleteRequest struct {
	AccountID string                       `json:"accountId"`
	BlobID    string                       `json:"blobId"`
	Parts     []bloballocate.CompletedPart `json:"parts"`
}

// CompleteResponse is the Blob/complete method response
type CompleteResponse struct {
	AccountID string `json:"accountId"`
	BlobID    string `json:"blobId"`
}

// CompleteError represents a JMAP error from Blob/complete
type CompleteError struct {
	Type    string
	Message string
}

func (e *CompleteError) Error() string {
	return fmt.Sprintf("%s: %s", e.Type, e.Message)
}

// BlobRecord holds the DynamoDB record fields needed by Blob/complete
type BlobRecord struct {
	Status    string
	Multipart bool
	UploadID  string
}

// Storage handles S3 operations for completing multipart uploads
type Storage interface {
	CompleteMultipartUpload(ctx context.Context, accountID, blobID, uploadID string, parts []bloballocate.CompletedPart) error
}

// DB handles DynamoDB operations for Blob/complete
type DB interface {
	GetBlobForComplete(ctx context.Context, accountID, blobID string) (*BlobRecord, error)
}

// Handler handles Blob/complete method calls
type Handler struct {
	Storage Storage
	DB      DB
}

// Complete processes a Blob/complete request
func (h *Handler) Complete(ctx context.Context, req CompleteRequest) (*CompleteResponse, error) {
	// Validate parts are non-empty
	if len(req.Parts) == 0 {
		return nil, &CompleteError{Type: "invalidArguments", Message: "parts must not be empty"}
	}

	// Get blob record from DynamoDB
	record, err := h.DB.GetBlobForComplete(ctx, req.AccountID, req.BlobID)
	if err != nil {
		return nil, &CompleteError{Type: "serverFail", Message: fmt.Sprintf("failed to get blob record: %v", err)}
	}
	if record == nil {
		return nil, &CompleteError{Type: "blobNotFound", Message: "blob not found"}
	}

	// Verify blob is pending
	if record.Status != "pending" {
		return nil, &CompleteError{Type: "invalidArguments", Message: fmt.Sprintf("blob is not pending (status: %s)", record.Status)}
	}

	// Verify blob is multipart
	if !record.Multipart {
		return nil, &CompleteError{Type: "invalidArguments", Message: "blob is not a multipart upload"}
	}

	// Verify uploadId exists
	if record.UploadID == "" {
		return nil, &CompleteError{Type: "serverFail", Message: "blob record missing uploadId"}
	}

	// Complete the multipart upload in S3
	// This creates the final S3 object, which triggers the S3 ObjectCreated event → blob-confirm
	if err := h.Storage.CompleteMultipartUpload(ctx, req.AccountID, req.BlobID, record.UploadID, req.Parts); err != nil {
		return nil, &CompleteError{Type: "serverFail", Message: fmt.Sprintf("failed to complete multipart upload: %v", err)}
	}

	return &CompleteResponse{
		AccountID: req.AccountID,
		BlobID:    req.BlobID,
	}, nil
}
