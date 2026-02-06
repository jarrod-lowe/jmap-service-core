package bloballocate

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// AllocateRequest is the Blob/allocate method request
type AllocateRequest struct {
	AccountID   string `json:"accountId"`
	Type        string `json:"type"`        // MIME type
	Size        int64  `json:"size"`        // Size in bytes
	SizeUnknown bool   `json:"sizeUnknown"` // True when size is not declared (IAM path)
}

// AllocateResponse is the Blob/allocate method response
type AllocateResponse struct {
	AccountID  string    `json:"accountId"`
	BlobID     string    `json:"blobId"`
	Type       string    `json:"type"`
	Size       int64     `json:"size"`
	URL        string    `json:"url"`
	URLExpires time.Time `json:"urlExpires"`
}

// AllocationError represents a JMAP error from Blob/allocate
type AllocationError struct {
	Type       string   // JMAP error type
	Message    string   // Error description
	Properties []string // Property names for invalidProperties errors
}

func (e *AllocationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Type, e.Message)
}

// Storage handles S3 operations for blob allocation
type Storage interface {
	GeneratePresignedPutURL(ctx context.Context, accountID, blobID string, size int64, contentType string, urlExpirySecs int64, sizeUnknown bool) (string, time.Time, error)
}

// DB handles DynamoDB operations for blob allocation
type DB interface {
	AllocateBlob(ctx context.Context, accountID, blobID string, size int64, contentType string, urlExpiresAt time.Time, maxPending int, s3Key string, sizeUnknown bool) error
}

// UUIDGenerator generates unique IDs
type UUIDGenerator interface {
	Generate() string
}

// Handler handles Blob/allocate method calls
type Handler struct {
	Storage          Storage
	DB               DB
	UUIDGen          UUIDGenerator
	MaxSizeUploadPut int64
	MaxPendingAllocs int
	URLExpirySecs    int64
}

// Allocate processes a Blob/allocate request
func (h *Handler) Allocate(ctx context.Context, req AllocateRequest) (*AllocateResponse, error) {
	// Validate size (skip when size is unknown, e.g. IAM path)
	if !req.SizeUnknown {
		if req.Size <= 0 {
			return nil, &AllocationError{Type: "invalidArguments", Message: "size must be greater than 0"}
		}
		if req.Size > h.MaxSizeUploadPut {
			return nil, &AllocationError{
				Type:    "tooLarge",
				Message: fmt.Sprintf("size %d exceeds maximum %d bytes", req.Size, h.MaxSizeUploadPut),
			}
		}
	}

	// Validate media type
	if !isValidMediaType(req.Type) {
		return nil, &AllocationError{Type: "invalidProperties", Message: "type must be a valid media type", Properties: []string{"type"}}
	}

	// Generate blobId
	blobID := h.UUIDGen.Generate()
	s3Key := fmt.Sprintf("%s/%s", req.AccountID, blobID)

	// Generate pre-signed URL first (before DB transaction to minimize wasted DB writes)
	url, urlExpires, err := h.Storage.GeneratePresignedPutURL(ctx, req.AccountID, blobID, req.Size, req.Type, h.URLExpirySecs, req.SizeUnknown)
	if err != nil {
		return nil, &AllocationError{Type: "serverFail", Message: "failed to generate upload URL"}
	}

	// Create allocation record with transaction (checks quota and pending limits)
	if err := h.DB.AllocateBlob(ctx, req.AccountID, blobID, req.Size, req.Type, urlExpires, h.MaxPendingAllocs, s3Key, req.SizeUnknown); err != nil {
		// Check if it's an AllocationError (tooManyPending, overQuota, etc.)
		if allocErr, ok := err.(*AllocationError); ok {
			return nil, allocErr
		}
		// Log the actual error for debugging
		return nil, &AllocationError{Type: "serverFail", Message: fmt.Sprintf("failed to create allocation record: %v", err)}
	}

	return &AllocateResponse{
		AccountID:  req.AccountID,
		BlobID:     blobID,
		Type:       req.Type,
		Size:       req.Size,
		URL:        url,
		URLExpires: urlExpires,
	}, nil
}

// isValidMediaType checks if a string is a valid MIME type
// Basic validation: must have type/subtype format
func isValidMediaType(mediaType string) bool {
	if mediaType == "" {
		return false
	}

	// Strip any parameters (e.g., "text/plain; charset=utf-8" -> "text/plain")
	parts := strings.SplitN(mediaType, ";", 2)
	base := strings.TrimSpace(parts[0])

	// Must contain exactly one /
	slashIdx := strings.Index(base, "/")
	if slashIdx <= 0 || slashIdx == len(base)-1 {
		return false
	}

	// Type and subtype must not be empty
	typePart := base[:slashIdx]
	subtype := base[slashIdx+1:]
	if typePart == "" || subtype == "" {
		return false
	}

	return true
}
