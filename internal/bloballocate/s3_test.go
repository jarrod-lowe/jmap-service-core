package bloballocate

import (
	"context"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// MockS3PresignClient implements S3PresignClient for testing
type MockS3PresignClient struct {
	PresignPutObjectFunc    func(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error)
	PresignUploadPartFunc   func(ctx context.Context, params *s3.UploadPartInput, optFns ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error)
	PresignUploadPartCalls  []s3.UploadPartInput
}

func (m *MockS3PresignClient) PresignPutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error) {
	if m.PresignPutObjectFunc != nil {
		return m.PresignPutObjectFunc(ctx, params, optFns...)
	}
	return &v4.PresignedHTTPRequest{URL: "https://example.com/presigned"}, nil
}

func (m *MockS3PresignClient) PresignUploadPart(ctx context.Context, params *s3.UploadPartInput, optFns ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error) {
	if m.PresignUploadPartCalls == nil {
		m.PresignUploadPartCalls = []s3.UploadPartInput{}
	}
	m.PresignUploadPartCalls = append(m.PresignUploadPartCalls, *params)
	if m.PresignUploadPartFunc != nil {
		return m.PresignUploadPartFunc(ctx, params, optFns...)
	}
	return &v4.PresignedHTTPRequest{URL: fmt.Sprintf("https://example.com/part/%d", aws.ToInt32(params.PartNumber))}, nil
}

// MockS3MultipartClient implements S3MultipartClient for testing
type MockS3MultipartClient struct {
	CreateMultipartUploadFunc   func(ctx context.Context, params *s3.CreateMultipartUploadInput, optFns ...func(*s3.Options)) (*s3.CreateMultipartUploadOutput, error)
	CompleteMultipartUploadFunc func(ctx context.Context, params *s3.CompleteMultipartUploadInput, optFns ...func(*s3.Options)) (*s3.CompleteMultipartUploadOutput, error)
	AbortMultipartUploadFunc    func(ctx context.Context, params *s3.AbortMultipartUploadInput, optFns ...func(*s3.Options)) (*s3.AbortMultipartUploadOutput, error)
}

func (m *MockS3MultipartClient) CreateMultipartUpload(ctx context.Context, params *s3.CreateMultipartUploadInput, optFns ...func(*s3.Options)) (*s3.CreateMultipartUploadOutput, error) {
	if m.CreateMultipartUploadFunc != nil {
		return m.CreateMultipartUploadFunc(ctx, params, optFns...)
	}
	return &s3.CreateMultipartUploadOutput{UploadId: aws.String("test-upload-id")}, nil
}

func (m *MockS3MultipartClient) CompleteMultipartUpload(ctx context.Context, params *s3.CompleteMultipartUploadInput, optFns ...func(*s3.Options)) (*s3.CompleteMultipartUploadOutput, error) {
	if m.CompleteMultipartUploadFunc != nil {
		return m.CompleteMultipartUploadFunc(ctx, params, optFns...)
	}
	return &s3.CompleteMultipartUploadOutput{}, nil
}

func (m *MockS3MultipartClient) AbortMultipartUpload(ctx context.Context, params *s3.AbortMultipartUploadInput, optFns ...func(*s3.Options)) (*s3.AbortMultipartUploadOutput, error) {
	if m.AbortMultipartUploadFunc != nil {
		return m.AbortMultipartUploadFunc(ctx, params, optFns...)
	}
	return &s3.AbortMultipartUploadOutput{}, nil
}

func TestCreateMultipartUpload_Success(t *testing.T) {
	var capturedInput *s3.CreateMultipartUploadInput
	mockS3 := &MockS3MultipartClient{
		CreateMultipartUploadFunc: func(ctx context.Context, params *s3.CreateMultipartUploadInput, optFns ...func(*s3.Options)) (*s3.CreateMultipartUploadOutput, error) {
			capturedInput = params
			return &s3.CreateMultipartUploadOutput{
				UploadId: aws.String("upload-abc-123"),
			}, nil
		},
	}
	mockPresign := &MockS3PresignClient{}

	storage := NewS3Storage(mockPresign, "test-bucket", mockS3)
	uploadID, err := storage.CreateMultipartUpload(context.Background(), "account-1", "blob-1", "message/rfc822")

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if uploadID != "upload-abc-123" {
		t.Errorf("expected uploadID 'upload-abc-123', got %q", uploadID)
	}
	if capturedInput == nil {
		t.Fatal("expected CreateMultipartUpload to be called")
	}
	if aws.ToString(capturedInput.Bucket) != "test-bucket" {
		t.Errorf("expected bucket 'test-bucket', got %q", aws.ToString(capturedInput.Bucket))
	}
	if aws.ToString(capturedInput.Key) != "account-1/blob-1" {
		t.Errorf("expected key 'account-1/blob-1', got %q", aws.ToString(capturedInput.Key))
	}
	if aws.ToString(capturedInput.ContentType) != "message/rfc822" {
		t.Errorf("expected content type 'message/rfc822', got %q", aws.ToString(capturedInput.ContentType))
	}
}

func TestCreateMultipartUpload_Error(t *testing.T) {
	mockS3 := &MockS3MultipartClient{
		CreateMultipartUploadFunc: func(ctx context.Context, params *s3.CreateMultipartUploadInput, optFns ...func(*s3.Options)) (*s3.CreateMultipartUploadOutput, error) {
			return nil, fmt.Errorf("access denied")
		},
	}
	mockPresign := &MockS3PresignClient{}

	storage := NewS3Storage(mockPresign, "test-bucket", mockS3)
	_, err := storage.CreateMultipartUpload(context.Background(), "account-1", "blob-1", "message/rfc822")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGeneratePresignedPartURLs_Success(t *testing.T) {
	mockPresign := &MockS3PresignClient{}
	mockS3 := &MockS3MultipartClient{}

	storage := NewS3Storage(mockPresign, "test-bucket", mockS3)
	parts, expires, err := storage.GeneratePresignedPartURLs(context.Background(), "account-1", "blob-1", "upload-123", 3, 900)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts, got %d", len(parts))
	}
	if expires.IsZero() {
		t.Fatal("expected non-zero expires time")
	}

	// Verify part numbers are 1-indexed
	for i, part := range parts {
		expectedPartNum := int32(i + 1)
		if part.PartNumber != expectedPartNum {
			t.Errorf("part %d: expected PartNumber %d, got %d", i, expectedPartNum, part.PartNumber)
		}
		if part.URL == "" {
			t.Errorf("part %d: expected non-empty URL", i)
		}
	}

	// Verify presign calls had correct inputs
	if len(mockPresign.PresignUploadPartCalls) != 3 {
		t.Fatalf("expected 3 presign calls, got %d", len(mockPresign.PresignUploadPartCalls))
	}
	for i, call := range mockPresign.PresignUploadPartCalls {
		if aws.ToString(call.Bucket) != "test-bucket" {
			t.Errorf("call %d: expected bucket 'test-bucket', got %q", i, aws.ToString(call.Bucket))
		}
		if aws.ToString(call.Key) != "account-1/blob-1" {
			t.Errorf("call %d: expected key 'account-1/blob-1', got %q", i, aws.ToString(call.Key))
		}
		if aws.ToString(call.UploadId) != "upload-123" {
			t.Errorf("call %d: expected uploadId 'upload-123', got %q", i, aws.ToString(call.UploadId))
		}
		if aws.ToInt32(call.PartNumber) != int32(i+1) {
			t.Errorf("call %d: expected partNumber %d, got %d", i, i+1, aws.ToInt32(call.PartNumber))
		}
	}
}

func TestGeneratePresignedPartURLs_PresignError(t *testing.T) {
	mockPresign := &MockS3PresignClient{
		PresignUploadPartFunc: func(ctx context.Context, params *s3.UploadPartInput, optFns ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error) {
			return nil, fmt.Errorf("presign failed")
		},
	}
	mockS3 := &MockS3MultipartClient{}

	storage := NewS3Storage(mockPresign, "test-bucket", mockS3)
	_, _, err := storage.GeneratePresignedPartURLs(context.Background(), "account-1", "blob-1", "upload-123", 3, 900)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCompleteMultipartUpload_Success(t *testing.T) {
	var capturedInput *s3.CompleteMultipartUploadInput
	mockS3 := &MockS3MultipartClient{
		CompleteMultipartUploadFunc: func(ctx context.Context, params *s3.CompleteMultipartUploadInput, optFns ...func(*s3.Options)) (*s3.CompleteMultipartUploadOutput, error) {
			capturedInput = params
			return &s3.CompleteMultipartUploadOutput{}, nil
		},
	}
	mockPresign := &MockS3PresignClient{}

	storage := NewS3Storage(mockPresign, "test-bucket", mockS3)
	completedParts := []CompletedPart{
		{PartNumber: 1, ETag: "\"etag1\""},
		{PartNumber: 2, ETag: "\"etag2\""},
	}
	err := storage.CompleteMultipartUpload(context.Background(), "account-1", "blob-1", "upload-123", completedParts)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if capturedInput == nil {
		t.Fatal("expected CompleteMultipartUpload to be called")
	}
	if aws.ToString(capturedInput.Bucket) != "test-bucket" {
		t.Errorf("expected bucket 'test-bucket', got %q", aws.ToString(capturedInput.Bucket))
	}
	if aws.ToString(capturedInput.Key) != "account-1/blob-1" {
		t.Errorf("expected key 'account-1/blob-1', got %q", aws.ToString(capturedInput.Key))
	}
	if aws.ToString(capturedInput.UploadId) != "upload-123" {
		t.Errorf("expected uploadId 'upload-123', got %q", aws.ToString(capturedInput.UploadId))
	}
	if len(capturedInput.MultipartUpload.Parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(capturedInput.MultipartUpload.Parts))
	}
	if aws.ToInt32(capturedInput.MultipartUpload.Parts[0].PartNumber) != 1 {
		t.Errorf("expected first part number 1, got %d", aws.ToInt32(capturedInput.MultipartUpload.Parts[0].PartNumber))
	}
	if aws.ToString(capturedInput.MultipartUpload.Parts[0].ETag) != "\"etag1\"" {
		t.Errorf("expected first part ETag '\"etag1\"', got %q", aws.ToString(capturedInput.MultipartUpload.Parts[0].ETag))
	}
}

func TestCompleteMultipartUpload_Error(t *testing.T) {
	mockS3 := &MockS3MultipartClient{
		CompleteMultipartUploadFunc: func(ctx context.Context, params *s3.CompleteMultipartUploadInput, optFns ...func(*s3.Options)) (*s3.CompleteMultipartUploadOutput, error) {
			return nil, fmt.Errorf("complete failed")
		},
	}
	mockPresign := &MockS3PresignClient{}

	storage := NewS3Storage(mockPresign, "test-bucket", mockS3)
	err := storage.CompleteMultipartUpload(context.Background(), "account-1", "blob-1", "upload-123", []CompletedPart{{PartNumber: 1, ETag: "etag1"}})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
