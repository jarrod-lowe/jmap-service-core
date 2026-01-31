package main

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/aws/aws-lambda-go/events"
)

// Mock implementations

type mockS3Deleter struct {
	deleteFunc func(ctx context.Context, bucket, key string) error
	deleteErr  error
	calls      []s3DeleteCall
}

type s3DeleteCall struct {
	Bucket string
	Key    string
}

func (m *mockS3Deleter) DeleteObject(ctx context.Context, bucket, key string) error {
	m.calls = append(m.calls, s3DeleteCall{Bucket: bucket, Key: key})
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, bucket, key)
	}
	return m.deleteErr
}

type mockDBDeleter struct {
	deleteFunc func(ctx context.Context, pk, sk, accountID string, size int64) error
	deleteErr  error
	calls      []dbDeleteCall
}

type dbDeleteCall struct {
	PK        string
	SK        string
	AccountID string
	Size      int64
}

func (m *mockDBDeleter) DeleteBlobRecord(ctx context.Context, pk, sk, accountID string, size int64) error {
	m.calls = append(m.calls, dbDeleteCall{PK: pk, SK: sk, AccountID: accountID, Size: size})
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, pk, sk, accountID, size)
	}
	return m.deleteErr
}

func setupTestDeps(s3d *mockS3Deleter, dbd *mockDBDeleter) {
	deps = &Dependencies{
		S3Deleter:  s3d,
		DBDeleter:  dbd,
		BlobBucket: "test-bucket",
	}
}

func newStringAttr(val string) events.DynamoDBAttributeValue {
	return events.NewStringAttribute(val)
}

func makeModifyRecord(oldImage, newImage map[string]events.DynamoDBAttributeValue) events.DynamoDBEventRecord {
	return events.DynamoDBEventRecord{
		EventName: "MODIFY",
		Change: events.DynamoDBStreamRecord{
			OldImage: oldImage,
			NewImage: newImage,
		},
	}
}

func newNumberAttr(val int64) events.DynamoDBAttributeValue {
	return events.NewNumberAttribute(fmt.Sprintf("%d", val))
}

func blobNewImage() map[string]events.DynamoDBAttributeValue {
	return map[string]events.DynamoDBAttributeValue{
		"pk":        newStringAttr("ACCOUNT#user-456"),
		"sk":        newStringAttr("BLOB#blob-123"),
		"accountId": newStringAttr("user-456"),
		"blobId":    newStringAttr("blob-123"),
		"s3Key":     newStringAttr("user-456/blob-123"),
		"size":      newNumberAttr(1024),
		"deletedAt": newStringAttr("2024-06-01T00:00:00Z"),
	}
}

func blobOldImage() map[string]events.DynamoDBAttributeValue {
	return map[string]events.DynamoDBAttributeValue{
		"pk":        newStringAttr("ACCOUNT#user-456"),
		"sk":        newStringAttr("BLOB#blob-123"),
		"accountId": newStringAttr("user-456"),
		"blobId":    newStringAttr("blob-123"),
		"s3Key":     newStringAttr("user-456/blob-123"),
		"size":      newNumberAttr(1024),
	}
}

// Test: MODIFY event with deletedAt added triggers cleanup
func TestCleanup_DeletedAtAdded_DeletesS3AndDB(t *testing.T) {
	s3d := &mockS3Deleter{}
	dbd := &mockDBDeleter{}
	setupTestDeps(s3d, dbd)

	event := events.DynamoDBEvent{
		Records: []events.DynamoDBEventRecord{
			makeModifyRecord(blobOldImage(), blobNewImage()),
		},
	}

	err := handler(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(s3d.calls) != 1 {
		t.Fatalf("expected 1 S3 delete call, got %d", len(s3d.calls))
	}
	if s3d.calls[0].Bucket != "test-bucket" {
		t.Errorf("expected bucket 'test-bucket', got %q", s3d.calls[0].Bucket)
	}
	if s3d.calls[0].Key != "user-456/blob-123" {
		t.Errorf("expected key 'user-456/blob-123', got %q", s3d.calls[0].Key)
	}

	if len(dbd.calls) != 1 {
		t.Fatalf("expected 1 DB delete call, got %d", len(dbd.calls))
	}
	if dbd.calls[0].PK != "ACCOUNT#user-456" {
		t.Errorf("expected pk 'ACCOUNT#user-456', got %q", dbd.calls[0].PK)
	}
	if dbd.calls[0].SK != "BLOB#blob-123" {
		t.Errorf("expected sk 'BLOB#blob-123', got %q", dbd.calls[0].SK)
	}
}

// Test: INSERT events are ignored
func TestCleanup_InsertEvent_Ignored(t *testing.T) {
	s3d := &mockS3Deleter{}
	dbd := &mockDBDeleter{}
	setupTestDeps(s3d, dbd)

	event := events.DynamoDBEvent{
		Records: []events.DynamoDBEventRecord{
			{
				EventName: "INSERT",
				Change: events.DynamoDBStreamRecord{
					NewImage: blobNewImage(),
				},
			},
		},
	}

	err := handler(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(s3d.calls) != 0 {
		t.Errorf("expected 0 S3 delete calls, got %d", len(s3d.calls))
	}
}

// Test: REMOVE events are ignored
func TestCleanup_RemoveEvent_Ignored(t *testing.T) {
	s3d := &mockS3Deleter{}
	dbd := &mockDBDeleter{}
	setupTestDeps(s3d, dbd)

	event := events.DynamoDBEvent{
		Records: []events.DynamoDBEventRecord{
			{EventName: "REMOVE"},
		},
	}

	err := handler(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(s3d.calls) != 0 {
		t.Errorf("expected 0 S3 delete calls, got %d", len(s3d.calls))
	}
}

// Test: MODIFY without deletedAt being added is ignored
func TestCleanup_ModifyWithoutDeletedAt_Ignored(t *testing.T) {
	s3d := &mockS3Deleter{}
	dbd := &mockDBDeleter{}
	setupTestDeps(s3d, dbd)

	// Both old and new lack deletedAt
	oldImg := blobOldImage()
	newImg := blobOldImage() // no deletedAt

	event := events.DynamoDBEvent{
		Records: []events.DynamoDBEventRecord{
			makeModifyRecord(oldImg, newImg),
		},
	}

	err := handler(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(s3d.calls) != 0 {
		t.Errorf("expected 0 S3 delete calls, got %d", len(s3d.calls))
	}
}

// Test: MODIFY where deletedAt already existed in old image is ignored
func TestCleanup_DeletedAtAlreadyExisted_Ignored(t *testing.T) {
	s3d := &mockS3Deleter{}
	dbd := &mockDBDeleter{}
	setupTestDeps(s3d, dbd)

	oldImg := blobNewImage() // has deletedAt
	newImg := blobNewImage() // also has deletedAt

	event := events.DynamoDBEvent{
		Records: []events.DynamoDBEventRecord{
			makeModifyRecord(oldImg, newImg),
		},
	}

	err := handler(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(s3d.calls) != 0 {
		t.Errorf("expected 0 S3 delete calls, got %d", len(s3d.calls))
	}
}

// Test: S3 delete failure returns error (triggers Lambda retry)
func TestCleanup_S3DeleteFailure_ReturnsError(t *testing.T) {
	s3d := &mockS3Deleter{deleteErr: errors.New("s3 error")}
	dbd := &mockDBDeleter{}
	setupTestDeps(s3d, dbd)

	event := events.DynamoDBEvent{
		Records: []events.DynamoDBEventRecord{
			makeModifyRecord(blobOldImage(), blobNewImage()),
		},
	}

	err := handler(context.Background(), event)
	if err == nil {
		t.Fatal("expected error from S3 delete failure")
	}
}

// Test: DB delete failure returns error (triggers Lambda retry)
func TestCleanup_DBDeleteFailure_ReturnsError(t *testing.T) {
	s3d := &mockS3Deleter{}
	dbd := &mockDBDeleter{deleteErr: errors.New("db error")}
	setupTestDeps(s3d, dbd)

	event := events.DynamoDBEvent{
		Records: []events.DynamoDBEventRecord{
			makeModifyRecord(blobOldImage(), blobNewImage()),
		},
	}

	err := handler(context.Background(), event)
	if err == nil {
		t.Fatal("expected error from DB delete failure")
	}
}

// Test: Missing pk in stream record returns error
func TestCleanup_MissingPK_ReturnsError(t *testing.T) {
	s3d := &mockS3Deleter{}
	dbd := &mockDBDeleter{}
	setupTestDeps(s3d, dbd)

	newImg := blobNewImage()
	delete(newImg, "pk")

	event := events.DynamoDBEvent{
		Records: []events.DynamoDBEventRecord{
			makeModifyRecord(blobOldImage(), newImg),
		},
	}

	err := handler(context.Background(), event)
	if err == nil {
		t.Fatal("expected error for missing pk")
	}
}

// Test: Missing s3Key in stream record returns error
func TestCleanup_MissingS3Key_ReturnsError(t *testing.T) {
	s3d := &mockS3Deleter{}
	dbd := &mockDBDeleter{}
	setupTestDeps(s3d, dbd)

	newImg := blobNewImage()
	delete(newImg, "s3Key")

	event := events.DynamoDBEvent{
		Records: []events.DynamoDBEventRecord{
			makeModifyRecord(blobOldImage(), newImg),
		},
	}

	err := handler(context.Background(), event)
	if err == nil {
		t.Fatal("expected error for missing s3Key")
	}
}

// Test: Multiple records in event are all processed
func TestCleanup_MultipleRecords_AllProcessed(t *testing.T) {
	s3d := &mockS3Deleter{}
	dbd := &mockDBDeleter{}
	setupTestDeps(s3d, dbd)

	newImg2 := map[string]events.DynamoDBAttributeValue{
		"pk":        newStringAttr("ACCOUNT#user-789"),
		"sk":        newStringAttr("BLOB#blob-999"),
		"accountId": newStringAttr("user-789"),
		"blobId":    newStringAttr("blob-999"),
		"s3Key":     newStringAttr("user-789/blob-999"),
		"size":      newNumberAttr(2048),
		"deletedAt": newStringAttr("2024-06-02T00:00:00Z"),
	}
	oldImg2 := map[string]events.DynamoDBAttributeValue{
		"pk":        newStringAttr("ACCOUNT#user-789"),
		"sk":        newStringAttr("BLOB#blob-999"),
		"accountId": newStringAttr("user-789"),
		"blobId":    newStringAttr("blob-999"),
		"s3Key":     newStringAttr("user-789/blob-999"),
		"size":      newNumberAttr(2048),
	}

	event := events.DynamoDBEvent{
		Records: []events.DynamoDBEventRecord{
			makeModifyRecord(blobOldImage(), blobNewImage()),
			makeModifyRecord(oldImg2, newImg2),
		},
	}

	err := handler(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(s3d.calls) != 2 {
		t.Errorf("expected 2 S3 delete calls, got %d", len(s3d.calls))
	}
	if len(dbd.calls) != 2 {
		t.Errorf("expected 2 DB delete calls, got %d", len(dbd.calls))
	}
}

// Test: Error on first record stops processing (returns error for retry)
func TestCleanup_ErrorOnFirstRecord_StopsProcessing(t *testing.T) {
	s3d := &mockS3Deleter{deleteErr: errors.New("s3 error")}
	dbd := &mockDBDeleter{}
	setupTestDeps(s3d, dbd)

	event := events.DynamoDBEvent{
		Records: []events.DynamoDBEventRecord{
			makeModifyRecord(blobOldImage(), blobNewImage()),
			makeModifyRecord(blobOldImage(), blobNewImage()),
		},
	}

	err := handler(context.Background(), event)
	if err == nil {
		t.Fatal("expected error")
	}

	// Only one S3 call should have been made before the error
	if len(s3d.calls) != 1 {
		t.Errorf("expected 1 S3 delete call, got %d", len(s3d.calls))
	}
}
