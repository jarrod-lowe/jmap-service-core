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
	Multipart   bool   `json:"multipart"`   // True for multipart upload (IAM-only)
	IsIAMAuth   bool   `json:"-"`           // True when request is IAM-authenticated
}

// AllocateResponse is the Blob/allocate method response
type AllocateResponse struct {
	AccountID  string    `json:"accountId"`
	BlobID     string    `json:"blobId"`
	Type       string    `json:"type"`
	Size       int64     `json:"size"`
	URL        string    `json:"url"`
	URLExpires time.Time `json:"urlExpires"`
	Parts      []PartURL `json:"parts,omitempty"` // Non-nil for multipart uploads
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

// MultipartStorage handles S3 multipart upload operations
type MultipartStorage interface {
	CreateMultipartUpload(ctx context.Context, accountID, blobID, contentType string) (string, error)
	GeneratePresignedPartURLs(ctx context.Context, accountID, blobID, uploadID string, partCount int, urlExpirySecs int64) ([]PartURL, time.Time, error)
}

// DB handles DynamoDB operations for blob allocation
type DB interface {
	AllocateBlob(ctx context.Context, accountID, blobID string, size int64, contentType string, urlExpiresAt time.Time, maxPending int, s3Key string, sizeUnknown bool, uploadID string, isIAMAuth bool) error
}

// UUIDGenerator generates unique IDs
type UUIDGenerator interface {
	Generate() string
}

// DefaultMultipartPartCount is the default number of presigned part URLs to generate
const DefaultMultipartPartCount = 100

// Handler handles Blob/allocate method calls
type Handler struct {
	Storage             Storage
	MultipartStorage    MultipartStorage
	DB                  DB
	UUIDGen             UUIDGenerator
	MaxSizeUploadPut    int64
	MaxPendingAllocs    int
	URLExpirySecs       int64
	MultipartPartCount  int
}

// Allocate processes a Blob/allocate request
func (h *Handler) Allocate(ctx context.Context, req AllocateRequest) (*AllocateResponse, error) {
	// Multipart requires SizeUnknown
	if req.Multipart && !req.SizeUnknown {
		return nil, &AllocationError{Type: "invalidArguments", Message: "multipart requires unknown size"}
	}

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

	if req.Multipart {
		return h.allocateMultipart(ctx, req, blobID, s3Key)
	}

	return h.allocateSinglePut(ctx, req, blobID, s3Key)
}

// allocateSinglePut handles the standard single-PUT upload flow
func (h *Handler) allocateSinglePut(ctx context.Context, req AllocateRequest, blobID, s3Key string) (*AllocateResponse, error) {
	url, urlExpires, err := h.Storage.GeneratePresignedPutURL(ctx, req.AccountID, blobID, req.Size, req.Type, h.URLExpirySecs, req.SizeUnknown)
	if err != nil {
		return nil, &AllocationError{Type: "serverFail", Message: "failed to generate upload URL"}
	}

	if err := h.DB.AllocateBlob(ctx, req.AccountID, blobID, req.Size, req.Type, urlExpires, h.MaxPendingAllocs, s3Key, req.SizeUnknown, "", req.IsIAMAuth); err != nil {
		if allocErr, ok := err.(*AllocationError); ok {
			return nil, allocErr
		}
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

// allocateMultipart handles the multipart upload flow
func (h *Handler) allocateMultipart(ctx context.Context, req AllocateRequest, blobID, s3Key string) (*AllocateResponse, error) {
	// Create multipart upload in S3
	uploadID, err := h.MultipartStorage.CreateMultipartUpload(ctx, req.AccountID, blobID, req.Type)
	if err != nil {
		return nil, &AllocationError{Type: "serverFail", Message: "failed to create multipart upload"}
	}

	// Generate presigned URLs for parts
	partCount := h.MultipartPartCount
	if partCount == 0 {
		partCount = DefaultMultipartPartCount
	}

	parts, urlExpires, err := h.MultipartStorage.GeneratePresignedPartURLs(ctx, req.AccountID, blobID, uploadID, partCount, h.URLExpirySecs)
	if err != nil {
		return nil, &AllocationError{Type: "serverFail", Message: "failed to generate part upload URLs"}
	}

	// Store allocation with upload ID
	if err := h.DB.AllocateBlob(ctx, req.AccountID, blobID, 0, req.Type, urlExpires, h.MaxPendingAllocs, s3Key, true, uploadID, req.IsIAMAuth); err != nil {
		if allocErr, ok := err.(*AllocationError); ok {
			return nil, allocErr
		}
		return nil, &AllocationError{Type: "serverFail", Message: fmt.Sprintf("failed to create allocation record: %v", err)}
	}

	return &AllocateResponse{
		AccountID:  req.AccountID,
		BlobID:     blobID,
		Type:       req.Type,
		Size:       0,
		URLExpires: urlExpires,
		Parts:      parts,
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
