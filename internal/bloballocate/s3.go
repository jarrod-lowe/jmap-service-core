package bloballocate

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// S3PresignClient defines the interface for S3 presign operations
type S3PresignClient interface {
	PresignPutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error)
	PresignUploadPart(ctx context.Context, params *s3.UploadPartInput, optFns ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error)
}

// S3MultipartClient defines the interface for S3 multipart operations (non-presigned)
type S3MultipartClient interface {
	CreateMultipartUpload(ctx context.Context, params *s3.CreateMultipartUploadInput, optFns ...func(*s3.Options)) (*s3.CreateMultipartUploadOutput, error)
	CompleteMultipartUpload(ctx context.Context, params *s3.CompleteMultipartUploadInput, optFns ...func(*s3.Options)) (*s3.CompleteMultipartUploadOutput, error)
	AbortMultipartUpload(ctx context.Context, params *s3.AbortMultipartUploadInput, optFns ...func(*s3.Options)) (*s3.AbortMultipartUploadOutput, error)
}

// PartURL represents a presigned URL for a single upload part
type PartURL struct {
	PartNumber int32  `json:"partNumber"`
	URL        string `json:"url"`
}

// CompletedPart represents a part that has been uploaded by the client
type CompletedPart struct {
	PartNumber int32  `json:"partNumber"`
	ETag       string `json:"etag"`
}

// S3Storage implements Storage using AWS S3
type S3Storage struct {
	presignClient S3PresignClient
	s3Client      S3MultipartClient
	bucketName    string
}

// NewS3Storage creates a new S3Storage
func NewS3Storage(presignClient S3PresignClient, bucketName string, s3Client S3MultipartClient) *S3Storage {
	return &S3Storage{
		presignClient: presignClient,
		s3Client:      s3Client,
		bucketName:    bucketName,
	}
}

// GeneratePresignedPutURL generates a pre-signed URL for PUT upload with constraints
func (s *S3Storage) GeneratePresignedPutURL(ctx context.Context, accountID, blobID string, size int64, contentType string, urlExpirySecs int64, sizeUnknown bool) (string, time.Time, error) {
	key := fmt.Sprintf("%s/%s", accountID, blobID)

	// Note: We don't include Tagging here because it would require the client
	// to send the x-amz-tagging header with the exact same value.
	// Instead, blob-confirm Lambda applies the Status=confirmed tag after upload.
	input := &s3.PutObjectInput{
		Bucket:      aws.String(s.bucketName),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
	}
	if !sizeUnknown {
		input.ContentLength = aws.Int64(size)
	}

	presignReq, err := s.presignClient.PresignPutObject(ctx, input, func(opts *s3.PresignOptions) {
		opts.Expires = time.Duration(urlExpirySecs) * time.Second
	})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("failed to presign PUT request: %w", err)
	}

	urlExpires := time.Now().Add(time.Duration(urlExpirySecs) * time.Second)
	return presignReq.URL, urlExpires, nil
}

// CreateMultipartUpload initiates a multipart upload in S3 and returns the upload ID
func (s *S3Storage) CreateMultipartUpload(ctx context.Context, accountID, blobID, contentType string) (string, error) {
	key := fmt.Sprintf("%s/%s", accountID, blobID)

	output, err := s.s3Client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket:      aws.String(s.bucketName),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return "", fmt.Errorf("failed to create multipart upload: %w", err)
	}

	return aws.ToString(output.UploadId), nil
}

// GeneratePresignedPartURLs generates presigned URLs for uploading individual parts
func (s *S3Storage) GeneratePresignedPartURLs(ctx context.Context, accountID, blobID, uploadID string, partCount int, urlExpirySecs int64) ([]PartURL, time.Time, error) {
	key := fmt.Sprintf("%s/%s", accountID, blobID)
	parts := make([]PartURL, 0, partCount)

	for i := 1; i <= partCount; i++ {
		partNum := int32(i)
		presignReq, err := s.presignClient.PresignUploadPart(ctx, &s3.UploadPartInput{
			Bucket:     aws.String(s.bucketName),
			Key:        aws.String(key),
			UploadId:   aws.String(uploadID),
			PartNumber: aws.Int32(partNum),
		}, func(opts *s3.PresignOptions) {
			opts.Expires = time.Duration(urlExpirySecs) * time.Second
		})
		if err != nil {
			return nil, time.Time{}, fmt.Errorf("failed to presign upload part %d: %w", i, err)
		}

		parts = append(parts, PartURL{
			PartNumber: partNum,
			URL:        presignReq.URL,
		})
	}

	urlExpires := time.Now().Add(time.Duration(urlExpirySecs) * time.Second)
	return parts, urlExpires, nil
}

// CompleteMultipartUpload finalizes a multipart upload in S3
func (s *S3Storage) CompleteMultipartUpload(ctx context.Context, accountID, blobID, uploadID string, parts []CompletedPart) error {
	key := fmt.Sprintf("%s/%s", accountID, blobID)

	s3Parts := make([]s3types.CompletedPart, len(parts))
	for i, p := range parts {
		s3Parts[i] = s3types.CompletedPart{
			PartNumber: aws.Int32(p.PartNumber),
			ETag:       aws.String(p.ETag),
		}
	}

	_, err := s.s3Client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(s.bucketName),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
		MultipartUpload: &s3types.CompletedMultipartUpload{
			Parts: s3Parts,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to complete multipart upload: %w", err)
	}

	return nil
}
