package resultref

import (
	"reflect"
	"testing"
)

func TestResolveArgs_NoReferences_PassesThrough(t *testing.T) {
	args := map[string]any{
		"accountId": "user-123",
		"ids":       []any{"email1", "email2"},
	}
	responses := []MethodResponse{}

	result, err := ResolveArgs(args, responses)
	if err != nil {
		t.Fatalf("ResolveArgs returned error: %v", err)
	}

	if !reflect.DeepEqual(result, args) {
		t.Errorf("expected args to pass through unchanged, got %v", result)
	}
}

func TestResolveArgs_SimpleReference_Resolves(t *testing.T) {
	args := map[string]any{
		"accountId": "user-123",
		"#ids": map[string]any{
			"resultOf": "query0",
			"name":     "Email/query",
			"path":     "/ids",
		},
	}
	responses := []MethodResponse{
		{
			ClientID: "query0",
			Name:     "Email/query",
			Args: map[string]any{
				"ids": []any{"email1", "email2", "email3"},
			},
		},
	}

	result, err := ResolveArgs(args, responses)
	if err != nil {
		t.Fatalf("ResolveArgs returned error: %v", err)
	}

	// Should have "ids" (resolved), not "#ids"
	if _, ok := result["#ids"]; ok {
		t.Error("expected #ids to be removed after resolution")
	}

	ids, ok := result["ids"]
	if !ok {
		t.Fatal("expected 'ids' to be present in result")
	}

	expected := []any{"email1", "email2", "email3"}
	if !reflect.DeepEqual(ids, expected) {
		t.Errorf("expected ids %v, got %v", expected, ids)
	}
}

func TestResolveArgs_ConflictingKeys_ReturnsError(t *testing.T) {
	// Both "ids" and "#ids" present - this is an error per RFC 8620
	args := map[string]any{
		"accountId": "user-123",
		"ids":       []any{"existing"},
		"#ids": map[string]any{
			"resultOf": "query0",
			"name":     "Email/query",
			"path":     "/ids",
		},
	}
	responses := []MethodResponse{}

	_, err := ResolveArgs(args, responses)
	if err == nil {
		t.Error("expected error for conflicting keys, got nil")
	}

	resolveErr, ok := err.(*ResolveError)
	if !ok {
		t.Fatalf("expected ResolveError, got %T", err)
	}

	if resolveErr.Type != ErrorInvalidArguments {
		t.Errorf("expected ErrorInvalidArguments, got %v", resolveErr.Type)
	}
}

func TestResolveArgs_ResultOfNotFound_ReturnsError(t *testing.T) {
	args := map[string]any{
		"accountId": "user-123",
		"#ids": map[string]any{
			"resultOf": "nonexistent",
			"name":     "Email/query",
			"path":     "/ids",
		},
	}
	responses := []MethodResponse{
		{
			ClientID: "query0",
			Name:     "Email/query",
			Args:     map[string]any{"ids": []any{"email1"}},
		},
	}

	_, err := ResolveArgs(args, responses)
	if err == nil {
		t.Error("expected error for resultOf not found, got nil")
	}

	resolveErr, ok := err.(*ResolveError)
	if !ok {
		t.Fatalf("expected ResolveError, got %T", err)
	}

	if resolveErr.Type != ErrorInvalidResultReference {
		t.Errorf("expected ErrorInvalidResultReference, got %v", resolveErr.Type)
	}
}

func TestResolveArgs_NameMismatch_ReturnsError(t *testing.T) {
	args := map[string]any{
		"accountId": "user-123",
		"#ids": map[string]any{
			"resultOf": "query0",
			"name":     "Email/get", // Wrong name - response is Email/query
			"path":     "/ids",
		},
	}
	responses := []MethodResponse{
		{
			ClientID: "query0",
			Name:     "Email/query",
			Args:     map[string]any{"ids": []any{"email1"}},
		},
	}

	_, err := ResolveArgs(args, responses)
	if err == nil {
		t.Error("expected error for name mismatch, got nil")
	}

	resolveErr, ok := err.(*ResolveError)
	if !ok {
		t.Fatalf("expected ResolveError, got %T", err)
	}

	if resolveErr.Type != ErrorInvalidResultReference {
		t.Errorf("expected ErrorInvalidResultReference, got %v", resolveErr.Type)
	}
}

func TestResolveArgs_MultipleReferences_ResolvesAll(t *testing.T) {
	args := map[string]any{
		"accountId": "user-123",
		"#ids": map[string]any{
			"resultOf": "query0",
			"name":     "Email/query",
			"path":     "/ids",
		},
		"#threadIds": map[string]any{
			"resultOf": "query0",
			"name":     "Email/query",
			"path":     "/list/*/threadId",
		},
	}
	responses := []MethodResponse{
		{
			ClientID: "query0",
			Name:     "Email/query",
			Args: map[string]any{
				"ids": []any{"email1", "email2"},
				"list": []any{
					map[string]any{"threadId": "thread1"},
					map[string]any{"threadId": "thread2"},
				},
			},
		},
	}

	result, err := ResolveArgs(args, responses)
	if err != nil {
		t.Fatalf("ResolveArgs returned error: %v", err)
	}

	expectedIds := []any{"email1", "email2"}
	if !reflect.DeepEqual(result["ids"], expectedIds) {
		t.Errorf("expected ids %v, got %v", expectedIds, result["ids"])
	}

	expectedThreadIds := []any{"thread1", "thread2"}
	if !reflect.DeepEqual(result["threadIds"], expectedThreadIds) {
		t.Errorf("expected threadIds %v, got %v", expectedThreadIds, result["threadIds"])
	}
}

func TestResolveArgs_PathEvaluationFails_ReturnsError(t *testing.T) {
	args := map[string]any{
		"accountId": "user-123",
		"#ids": map[string]any{
			"resultOf": "query0",
			"name":     "Email/query",
			"path":     "/nonexistent",
		},
	}
	responses := []MethodResponse{
		{
			ClientID: "query0",
			Name:     "Email/query",
			Args:     map[string]any{"ids": []any{"email1"}},
		},
	}

	_, err := ResolveArgs(args, responses)
	if err == nil {
		t.Error("expected error for path evaluation failure, got nil")
	}

	resolveErr, ok := err.(*ResolveError)
	if !ok {
		t.Fatalf("expected ResolveError, got %T", err)
	}

	if resolveErr.Type != ErrorInvalidResultReference {
		t.Errorf("expected ErrorInvalidResultReference, got %v", resolveErr.Type)
	}
}

func TestResolveArgs_InvalidResultReferenceFormat_ReturnsError(t *testing.T) {
	args := map[string]any{
		"accountId": "user-123",
		"#ids":      "not a result reference object",
	}
	responses := []MethodResponse{}

	_, err := ResolveArgs(args, responses)
	if err == nil {
		t.Error("expected error for invalid result reference format, got nil")
	}

	resolveErr, ok := err.(*ResolveError)
	if !ok {
		t.Fatalf("expected ResolveError, got %T", err)
	}

	if resolveErr.Type != ErrorInvalidResultReference {
		t.Errorf("expected ErrorInvalidResultReference, got %v", resolveErr.Type)
	}
}

func TestResolveArgs_ReferenceLaterResponse_UsesOnlyPreviousResponses(t *testing.T) {
	// This tests that we only look at responses BEFORE the current call
	args := map[string]any{
		"accountId": "user-123",
		"#ids": map[string]any{
			"resultOf": "query1", // This ID exists but at index 1, not before index 0
			"name":     "Email/query",
			"path":     "/ids",
		},
	}
	// If we're processing call at index 0, we have no previous responses
	responses := []MethodResponse{}

	_, err := ResolveArgs(args, responses)
	if err == nil {
		t.Error("expected error when referencing a response that doesn't exist yet")
	}
}
