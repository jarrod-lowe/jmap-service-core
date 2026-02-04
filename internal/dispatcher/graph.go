package dispatcher

import (
	"fmt"
	"strings"
)

// BuildGraph constructs a dependency graph from JMAP method calls.
// It returns:
// - deps: map from call index to slice of indices it depends on
// - dependents: map from call index to slice of indices that depend on it
// - error if there are invalid or forward references
func BuildGraph(calls [][]any) (deps map[int][]int, dependents map[int][]int, err error) {
	deps = make(map[int][]int)
	dependents = make(map[int][]int)

	// Build clientId → index lookup
	clientIDToIndex := make(map[string]int)
	for i, call := range calls {
		if len(call) >= 3 {
			if clientID, ok := call[2].(string); ok && clientID != "" {
				clientIDToIndex[clientID] = i
			}
		}
	}

	// Scan each call's args for result references
	for i, call := range calls {
		if len(call) < 2 {
			continue
		}
		args, ok := call[1].(map[string]any)
		if !ok {
			continue
		}

		// Find all result references in args (keys starting with "#")
		depIndices := findDependencies(args, clientIDToIndex, i)
		for _, depIdx := range depIndices {
			if depIdx >= i {
				return nil, nil, fmt.Errorf("forward reference: call %d references call %d", i, depIdx)
			}
		}

		deps[i] = depIndices
		// Update dependents map
		for _, depIdx := range depIndices {
			dependents[depIdx] = append(dependents[depIdx], i)
		}
	}

	return deps, dependents, nil
}

// findDependencies scans args for result references and returns indices of dependencies
func findDependencies(args map[string]any, clientIDToIndex map[string]int, currentIdx int) []int {
	seen := make(map[int]bool)
	var deps []int

	for key, value := range args {
		if !strings.HasPrefix(key, "#") {
			continue
		}

		// This is a result reference - extract the resultOf clientId
		ref, ok := value.(map[string]any)
		if !ok {
			continue
		}
		resultOf, ok := ref["resultOf"].(string)
		if !ok {
			continue
		}

		depIdx, exists := clientIDToIndex[resultOf]
		if !exists {
			continue // Will be caught later during resolution
		}

		if !seen[depIdx] {
			seen[depIdx] = true
			deps = append(deps, depIdx)
		}
	}

	return deps
}
