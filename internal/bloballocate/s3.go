package bloballocate

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3PresignClient defines the interface for S3 presign operations
type S3PresignClient interface {
	PresignPutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error)
}

// S3Storage implements Storage using AWS S3
type S3Storage struct {
	presignClient S3PresignClient
	bucketName    string
}

// NewS3Storage creates a new S3Storage
func NewS3Storage(presignClient S3PresignClient, bucketName string) *S3Storage {
	return &S3Storage{
		presignClient: presignClient,
		bucketName:    bucketName,
	}
}

// GeneratePresignedPutURL generates a pre-signed URL for PUT upload with constraints
func (s *S3Storage) GeneratePresignedPutURL(ctx context.Context, accountID, blobID string, size int64, contentType string, urlExpirySecs int64) (string, time.Time, error) {
	key := fmt.Sprintf("%s/%s", accountID, blobID)

	// Note: We don't include Tagging here because it would require the client
	// to send the x-amz-tagging header with the exact same value.
	// Instead, blob-confirm Lambda applies the Status=confirmed tag after upload.
	presignReq, err := s.presignClient.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucketName),
		Key:           aws.String(key),
		ContentType:   aws.String(contentType),
		ContentLength: aws.Int64(size),
	}, func(opts *s3.PresignOptions) {
		opts.Expires = time.Duration(urlExpirySecs) * time.Second
	})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("failed to presign PUT request: %w", err)
	}

	urlExpires := time.Now().Add(time.Duration(urlExpirySecs) * time.Second)
	return presignReq.URL, urlExpires, nil
}
