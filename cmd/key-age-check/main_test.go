package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Mock implementations for testing

type mockSSMReader struct {
	timestamp string
	err       error
}

func (m *mockSSMReader) GetParameter(ctx context.Context, name string) (string, error) {
	return m.timestamp, m.err
}

type mockMetricsPublisher struct {
	publishedValue float64
	publishedName  string
	err            error
}

func (m *mockMetricsPublisher) PublishMetric(ctx context.Context, name string, value float64) error {
	m.publishedName = name
	m.publishedValue = value
	return m.err
}

func setupTestDeps(ssm *mockSSMReader, metrics *mockMetricsPublisher) {
	deps = &Dependencies{
		SSMReader:        ssm,
		MetricsPublisher: metrics,
		Config: Config{
			SSMParameterName: "/test/cloudfront-key-created-at",
			MetricNamespace:  "TestNamespace",
		},
	}
}

// Test 1: Successfully calculates and publishes key age
func TestKeyAgeCheck_Success(t *testing.T) {
	// Key created 30 days ago
	createdAt := time.Now().AddDate(0, 0, -30).UTC().Format(time.RFC3339)
	ssm := &mockSSMReader{timestamp: createdAt}
	metrics := &mockMetricsPublisher{}
	setupTestDeps(ssm, metrics)

	ctx := context.Background()
	err := checkKeyAge(ctx)
	if err != nil {
		t.Fatalf("checkKeyAge returned error: %v", err)
	}

	if metrics.publishedName != "KeyAgeDays" {
		t.Errorf("expected metric name 'KeyAgeDays', got '%s'", metrics.publishedName)
	}

	// Allow 1 day tolerance for time zone differences
	if metrics.publishedValue < 29 || metrics.publishedValue > 31 {
		t.Errorf("expected key age around 30 days, got %f", metrics.publishedValue)
	}
}

// Test 2: Handles SSM read failure
func TestKeyAgeCheck_SSMFailure(t *testing.T) {
	ssm := &mockSSMReader{err: errors.New("SSM error")}
	metrics := &mockMetricsPublisher{}
	setupTestDeps(ssm, metrics)

	ctx := context.Background()
	err := checkKeyAge(ctx)
	if err == nil {
		t.Fatal("expected error when SSM fails, got nil")
	}

	if metrics.publishedName != "" {
		t.Error("should not publish metric when SSM fails")
	}
}

// Test 3: Handles invalid timestamp format
func TestKeyAgeCheck_InvalidTimestamp(t *testing.T) {
	ssm := &mockSSMReader{timestamp: "not-a-valid-timestamp"}
	metrics := &mockMetricsPublisher{}
	setupTestDeps(ssm, metrics)

	ctx := context.Background()
	err := checkKeyAge(ctx)
	if err == nil {
		t.Fatal("expected error for invalid timestamp, got nil")
	}
}

// Test 4: Handles metric publish failure
func TestKeyAgeCheck_MetricPublishFailure(t *testing.T) {
	createdAt := time.Now().AddDate(0, 0, -10).UTC().Format(time.RFC3339)
	ssm := &mockSSMReader{timestamp: createdAt}
	metrics := &mockMetricsPublisher{err: errors.New("CloudWatch error")}
	setupTestDeps(ssm, metrics)

	ctx := context.Background()
	err := checkKeyAge(ctx)
	if err == nil {
		t.Fatal("expected error when metrics publish fails, got nil")
	}
}

// Test 5: Correctly calculates age for very old key
func TestKeyAgeCheck_OldKey(t *testing.T) {
	// Key created 365 days ago
	createdAt := time.Now().AddDate(0, 0, -365).UTC().Format(time.RFC3339)
	ssm := &mockSSMReader{timestamp: createdAt}
	metrics := &mockMetricsPublisher{}
	setupTestDeps(ssm, metrics)

	ctx := context.Background()
	err := checkKeyAge(ctx)
	if err != nil {
		t.Fatalf("checkKeyAge returned error: %v", err)
	}

	// Allow 1 day tolerance
	if metrics.publishedValue < 364 || metrics.publishedValue > 366 {
		t.Errorf("expected key age around 365 days, got %f", metrics.publishedValue)
	}
}

// Test 6: Correctly handles brand new key
func TestKeyAgeCheck_NewKey(t *testing.T) {
	// Key created today
	createdAt := time.Now().UTC().Format(time.RFC3339)
	ssm := &mockSSMReader{timestamp: createdAt}
	metrics := &mockMetricsPublisher{}
	setupTestDeps(ssm, metrics)

	ctx := context.Background()
	err := checkKeyAge(ctx)
	if err != nil {
		t.Fatalf("checkKeyAge returned error: %v", err)
	}

	// Should be 0 or very close to 0
	if metrics.publishedValue < 0 || metrics.publishedValue > 1 {
		t.Errorf("expected key age around 0 days, got %f", metrics.publishedValue)
	}
}
