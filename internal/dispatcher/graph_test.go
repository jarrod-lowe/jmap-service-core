package dispatcher

import (
	"strings"
	"testing"
)

func TestBuildGraph_NoDependencies(t *testing.T) {
	// Three independent calls - all should be ready immediately
	calls := [][]any{
		{"Email/get", map[string]any{"accountId": "acc1", "ids": []string{"e1"}}, "c0"},
		{"Email/get", map[string]any{"accountId": "acc1", "ids": []string{"e2"}}, "c1"},
		{"Email/get", map[string]any{"accountId": "acc1", "ids": []string{"e3"}}, "c2"},
	}

	deps, dependents, err := BuildGraph(calls)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// All calls should have no dependencies
	for i := 0; i < 3; i++ {
		if len(deps[i]) != 0 {
			t.Errorf("call %d: expected 0 deps, got %d", i, len(deps[i]))
		}
		if len(dependents[i]) != 0 {
			t.Errorf("call %d: expected 0 dependents, got %d", i, len(dependents[i]))
		}
	}
}

func TestBuildGraph_LinearChain(t *testing.T) {
	// Linear chain: c0 → c1 → c2
	// c1 depends on c0, c2 depends on c1
	calls := [][]any{
		{"Email/query", map[string]any{"accountId": "acc1"}, "c0"},
		{"Email/get", map[string]any{
			"accountId": "acc1",
			"#ids": map[string]any{
				"resultOf": "c0",
				"name":     "Email/query",
				"path":     "/ids",
			},
		}, "c1"},
		{"Email/get", map[string]any{
			"accountId": "acc1",
			"#ids": map[string]any{
				"resultOf": "c1",
				"name":     "Email/get",
				"path":     "/list/*/threadId",
			},
		}, "c2"},
	}

	deps, dependents, err := BuildGraph(calls)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// c0 has no deps, c1 depends on c0, c2 depends on c1
	if len(deps[0]) != 0 {
		t.Errorf("c0: expected 0 deps, got %d", len(deps[0]))
	}
	if len(deps[1]) != 1 || deps[1][0] != 0 {
		t.Errorf("c1: expected deps=[0], got %v", deps[1])
	}
	if len(deps[2]) != 1 || deps[2][0] != 1 {
		t.Errorf("c2: expected deps=[1], got %v", deps[2])
	}

	// Check dependents: c0 has c1, c1 has c2, c2 has none
	if len(dependents[0]) != 1 || dependents[0][0] != 1 {
		t.Errorf("c0: expected dependents=[1], got %v", dependents[0])
	}
	if len(dependents[1]) != 1 || dependents[1][0] != 2 {
		t.Errorf("c1: expected dependents=[2], got %v", dependents[1])
	}
	if len(dependents[2]) != 0 {
		t.Errorf("c2: expected 0 dependents, got %d", len(dependents[2]))
	}
}

func TestBuildGraph_DiamondPattern(t *testing.T) {
	// Diamond: c0 fans out to c1 and c2, both converge to c3
	//     c0
	//    /  \
	//   c1  c2
	//    \  /
	//     c3
	calls := [][]any{
		{"Email/query", map[string]any{"accountId": "acc1"}, "c0"},
		{"Email/get", map[string]any{
			"accountId": "acc1",
			"#ids": map[string]any{
				"resultOf": "c0",
				"name":     "Email/query",
				"path":     "/ids",
			},
		}, "c1"},
		{"Thread/get", map[string]any{
			"accountId": "acc1",
			"#ids": map[string]any{
				"resultOf": "c0",
				"name":     "Email/query",
				"path":     "/ids",
			},
		}, "c2"},
		{"Email/set", map[string]any{
			"accountId": "acc1",
			"#emailIds": map[string]any{
				"resultOf": "c1",
				"name":     "Email/get",
				"path":     "/list/*/id",
			},
			"#threadIds": map[string]any{
				"resultOf": "c2",
				"name":     "Thread/get",
				"path":     "/list/*/id",
			},
		}, "c3"},
	}

	deps, dependents, err := BuildGraph(calls)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// c0 has no deps
	if len(deps[0]) != 0 {
		t.Errorf("c0: expected 0 deps, got %d", len(deps[0]))
	}
	// c1 depends on c0
	if len(deps[1]) != 1 || deps[1][0] != 0 {
		t.Errorf("c1: expected deps=[0], got %v", deps[1])
	}
	// c2 depends on c0
	if len(deps[2]) != 1 || deps[2][0] != 0 {
		t.Errorf("c2: expected deps=[0], got %v", deps[2])
	}
	// c3 depends on c1 and c2 (order may vary)
	if len(deps[3]) != 2 {
		t.Errorf("c3: expected 2 deps, got %d", len(deps[3]))
	}
	hasC1 := false
	hasC2 := false
	for _, d := range deps[3] {
		if d == 1 {
			hasC1 = true
		}
		if d == 2 {
			hasC2 = true
		}
	}
	if !hasC1 || !hasC2 {
		t.Errorf("c3: expected deps to include 1 and 2, got %v", deps[3])
	}

	// Check dependents
	// c0 has c1 and c2
	if len(dependents[0]) != 2 {
		t.Errorf("c0: expected 2 dependents, got %d", len(dependents[0]))
	}
	// c1 has c3
	if len(dependents[1]) != 1 || dependents[1][0] != 3 {
		t.Errorf("c1: expected dependents=[3], got %v", dependents[1])
	}
	// c2 has c3
	if len(dependents[2]) != 1 || dependents[2][0] != 3 {
		t.Errorf("c2: expected dependents=[3], got %v", dependents[2])
	}
	// c3 has no dependents
	if len(dependents[3]) != 0 {
		t.Errorf("c3: expected 0 dependents, got %d", len(dependents[3]))
	}
}

func TestBuildGraph_ForwardReferenceError(t *testing.T) {
	// Forward reference: c0 tries to reference c1 which comes later
	calls := [][]any{
		{"Email/get", map[string]any{
			"accountId": "acc1",
			"#ids": map[string]any{
				"resultOf": "c1", // References a later call
				"name":     "Email/query",
				"path":     "/ids",
			},
		}, "c0"},
		{"Email/query", map[string]any{"accountId": "acc1"}, "c1"},
	}

	_, _, err := BuildGraph(calls)
	if err == nil {
		t.Fatal("expected error for forward reference, got nil")
	}
	if !strings.Contains(err.Error(), "forward reference") {
		t.Errorf("expected error message to mention 'forward reference', got: %s", err.Error())
	}
}

func TestBuildGraph_NonexistentReference(t *testing.T) {
	// Reference to a clientId that doesn't exist
	// This should NOT error at graph build time - it will be caught during resolution
	calls := [][]any{
		{"Email/get", map[string]any{
			"accountId": "acc1",
			"#ids": map[string]any{
				"resultOf": "nonexistent",
				"name":     "Email/query",
				"path":     "/ids",
			},
		}, "c0"},
	}

	deps, _, err := BuildGraph(calls)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have no dependencies (nonexistent reference is ignored at graph build time)
	if len(deps[0]) != 0 {
		t.Errorf("c0: expected 0 deps for nonexistent ref, got %d", len(deps[0]))
	}
}
