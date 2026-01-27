package resultref

import (
	"reflect"
	"testing"
)

func TestEvaluatePath_RootProperty(t *testing.T) {
	data := map[string]any{
		"ids": []any{"id1", "id2", "id3"},
	}

	result, err := EvaluatePath(data, "/ids")
	if err != nil {
		t.Fatalf("EvaluatePath returned error: %v", err)
	}

	expected := []any{"id1", "id2", "id3"}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("expected %v, got %v", expected, result)
	}
}

func TestEvaluatePath_NestedProperty(t *testing.T) {
	data := map[string]any{
		"list": []any{
			map[string]any{"id": "msg1"},
			map[string]any{"id": "msg2"},
		},
	}

	result, err := EvaluatePath(data, "/list/0/id")
	if err != nil {
		t.Fatalf("EvaluatePath returned error: %v", err)
	}

	if result != "msg1" {
		t.Errorf("expected 'msg1', got '%v'", result)
	}
}

func TestEvaluatePath_Wildcard(t *testing.T) {
	data := map[string]any{
		"list": []any{
			map[string]any{"threadId": "thread1"},
			map[string]any{"threadId": "thread2"},
			map[string]any{"threadId": "thread3"},
		},
	}

	result, err := EvaluatePath(data, "/list/*/threadId")
	if err != nil {
		t.Fatalf("EvaluatePath returned error: %v", err)
	}

	expected := []any{"thread1", "thread2", "thread3"}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("expected %v, got %v", expected, result)
	}
}

func TestEvaluatePath_WildcardFlattening(t *testing.T) {
	// When wildcard extracts arrays, they should be flattened
	data := map[string]any{
		"list": []any{
			map[string]any{"emailIds": []any{"email1", "email2"}},
			map[string]any{"emailIds": []any{"email3"}},
			map[string]any{"emailIds": []any{"email4", "email5"}},
		},
	}

	result, err := EvaluatePath(data, "/list/*/emailIds")
	if err != nil {
		t.Fatalf("EvaluatePath returned error: %v", err)
	}

	// Should be flattened to single array
	expected := []any{"email1", "email2", "email3", "email4", "email5"}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("expected %v, got %v", expected, result)
	}
}

func TestEvaluatePath_RootDocument(t *testing.T) {
	data := map[string]any{
		"ids": []any{"id1"},
	}

	result, err := EvaluatePath(data, "")
	if err != nil {
		t.Fatalf("EvaluatePath returned error: %v", err)
	}

	if !reflect.DeepEqual(result, data) {
		t.Errorf("expected %v, got %v", data, result)
	}
}

func TestEvaluatePath_NullValue(t *testing.T) {
	// A key that exists with an explicit nil value should return (nil, nil)
	data := map[string]any{
		"updatedProperties": nil,
		"ids":               []any{"id1"},
	}

	result, err := EvaluatePath(data, "/updatedProperties")
	if err != nil {
		t.Fatalf("expected no error for null value, got: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result for null value, got: %v", result)
	}
}

func TestEvaluatePath_MissingKey(t *testing.T) {
	// A key that does not exist should return an error
	data := map[string]any{
		"ids": []any{"id1"},
	}

	_, err := EvaluatePath(data, "/nonexistent")
	if err == nil {
		t.Error("expected error for missing key, got nil")
	}
}

func TestEvaluatePath_InvalidPath_ReturnsError(t *testing.T) {
	data := map[string]any{
		"ids": []any{"id1"},
	}

	_, err := EvaluatePath(data, "/nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent path, got nil")
	}
}

func TestEvaluatePath_WildcardOnNonArray_ReturnsError(t *testing.T) {
	data := map[string]any{
		"notArray": "string",
	}

	_, err := EvaluatePath(data, "/notArray/*/foo")
	if err == nil {
		t.Error("expected error for wildcard on non-array, got nil")
	}
}

func TestEvaluatePath_WildcardEmptyArray(t *testing.T) {
	data := map[string]any{
		"list": []any{},
	}

	result, err := EvaluatePath(data, "/list/*/id")
	if err != nil {
		t.Fatalf("EvaluatePath returned error: %v", err)
	}

	// Empty array should return empty array
	expected := []any{}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("expected %v, got %v", expected, result)
	}
}
