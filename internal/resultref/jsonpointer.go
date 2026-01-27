package resultref

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/qri-io/jsonpointer"
)

// EvaluatePath evaluates a JSON Pointer path against data, with support for
// the JMAP wildcard extension (*).
// Standard JSON Pointer paths (RFC 6901) are supported, plus:
// - /list/* extracts matching elements from all array items
// - Wildcards flatten nested arrays when extracting arrays
func EvaluatePath(data any, path string) (any, error) {
	// Empty path returns the whole document
	if path == "" {
		return data, nil
	}

	// Check for wildcard
	if strings.Contains(path, "/*") {
		return evaluateWildcardPath(data, path)
	}

	// Standard JSON Pointer evaluation
	ptr, err := jsonpointer.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("invalid JSON Pointer: %w", err)
	}

	result, err := ptr.Eval(data)
	if err != nil {
		return nil, fmt.Errorf("path not found: %s", path)
	}

	// The jsonpointer library returns (nil, nil) for both "key missing" and
	// "key exists with null value". Distinguish by checking the parent object.
	if result == nil {
		if keyExists(data, path) {
			return nil, nil
		}
		return nil, fmt.Errorf("path not found: %s", path)
	}

	return result, nil
}

// keyExists checks whether the final segment of a JSON Pointer path exists in the data,
// to distinguish between a null value and a missing key.
func keyExists(data any, path string) bool {
	// Parse path into segments: "/a/b/c" -> ["a", "b", "c"]
	segments := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(segments) == 0 {
		return false
	}

	// Navigate to the parent (all segments except the last)
	parent := data
	for _, seg := range segments[:len(segments)-1] {
		switch p := parent.(type) {
		case map[string]any:
			var ok bool
			parent, ok = p[seg]
			if !ok {
				return false
			}
		case []any:
			idx, err := strconv.Atoi(seg)
			if err != nil || idx < 0 || idx >= len(p) {
				return false
			}
			parent = p[idx]
		default:
			return false
		}
	}

	// Check if the final segment exists in the parent
	finalSeg := segments[len(segments)-1]
	switch p := parent.(type) {
	case map[string]any:
		_, exists := p[finalSeg]
		return exists
	case []any:
		idx, err := strconv.Atoi(finalSeg)
		return err == nil && idx >= 0 && idx < len(p)
	default:
		return false
	}
}

// evaluateWildcardPath handles paths containing the JMAP wildcard (*) extension
func evaluateWildcardPath(data any, path string) (any, error) {
	// Split path at first wildcard
	wildcardIdx := strings.Index(path, "/*")
	beforeWildcard := path[:wildcardIdx]
	afterWildcard := path[wildcardIdx+2:] // Skip "/*"

	// Get the array at the path before the wildcard
	var arrayData any
	var err error
	if beforeWildcard == "" {
		arrayData = data
	} else {
		ptr, err := jsonpointer.Parse(beforeWildcard)
		if err != nil {
			return nil, fmt.Errorf("invalid JSON Pointer before wildcard: %w", err)
		}
		arrayData, err = ptr.Eval(data)
		if err != nil {
			return nil, fmt.Errorf("path not found before wildcard: %s", beforeWildcard)
		}
	}

	// Verify it's an array
	arr, ok := arrayData.([]any)
	if !ok {
		return nil, fmt.Errorf("wildcard requires an array, got %T at path %s", arrayData, beforeWildcard)
	}

	// Extract values from each array element
	results := make([]any, 0, len(arr))
	for i, item := range arr {
		var value any
		if afterWildcard == "" {
			// Wildcard at end of path - extract whole item
			value = item
		} else {
			// Continue evaluating the remaining path
			value, err = EvaluatePath(item, afterWildcard)
			if err != nil {
				return nil, fmt.Errorf("failed to evaluate path %s on array element %d: %w", afterWildcard, i, err)
			}
		}

		// Flatten arrays per JMAP spec
		if valueArr, isArr := value.([]any); isArr {
			results = append(results, valueArr...)
		} else {
			results = append(results, value)
		}
	}

	return results, nil
}

// evaluateArrayIndex evaluates a numeric array index
func evaluateArrayIndex(arr []any, indexStr string) (any, error) {
	idx, err := strconv.Atoi(indexStr)
	if err != nil {
		return nil, fmt.Errorf("invalid array index: %s", indexStr)
	}

	if idx < 0 || idx >= len(arr) {
		return nil, fmt.Errorf("array index out of bounds: %d", idx)
	}

	return arr[idx], nil
}
