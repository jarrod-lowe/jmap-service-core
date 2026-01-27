package resultref

import (
	"strings"
)

// ResolveArgs resolves any result references in the args map using previous responses.
// Returns the resolved args or an error if resolution fails.
//
// Result references are identified by keys prefixed with "#" (e.g., "#ids").
// The value must be a ResultReference struct with:
// - ResultOf: the clientId of a previous method call
// - Name: the method name that must match the referenced call
// - Path: a JSON Pointer path to extract from the response
func ResolveArgs(args map[string]any, responses []MethodResponse) (map[string]any, error) {
	// First pass: check for conflicting keys
	if err := checkConflictingKeys(args); err != nil {
		return nil, err
	}

	// Check if there are any result references to resolve
	hasReferences := false
	for key := range args {
		if strings.HasPrefix(key, "#") {
			hasReferences = true
			break
		}
	}

	// No references - return args unchanged
	if !hasReferences {
		return args, nil
	}

	// Build response lookup by clientId
	responseLookup := make(map[string]MethodResponse)
	for _, resp := range responses {
		responseLookup[resp.ClientID] = resp
	}

	// Resolve references
	result := make(map[string]any, len(args))
	for key, value := range args {
		if strings.HasPrefix(key, "#") {
			// This is a result reference
			resolvedKey := strings.TrimPrefix(key, "#")
			resolvedValue, err := resolveReference(value, responseLookup)
			if err != nil {
				return nil, err
			}
			// Per RFC 8620, null means "omit the property" — don't include the key
			if resolvedValue != nil {
				result[resolvedKey] = resolvedValue
			}
		} else {
			result[key] = value
		}
	}

	return result, nil
}

// checkConflictingKeys checks if args contain both "foo" and "#foo" for any key
func checkConflictingKeys(args map[string]any) error {
	for key := range args {
		if strings.HasPrefix(key, "#") {
			baseKey := strings.TrimPrefix(key, "#")
			if _, exists := args[baseKey]; exists {
				return NewInvalidArgumentsError("conflicting keys: both '" + baseKey + "' and '#" + baseKey + "' are present")
			}
		}
	}
	return nil
}

// resolveReference resolves a single result reference
func resolveReference(refValue any, responseLookup map[string]MethodResponse) (any, error) {
	// Parse the reference
	ref, err := parseResultReference(refValue)
	if err != nil {
		return nil, err
	}

	// Find the referenced response
	response, found := responseLookup[ref.ResultOf]
	if !found {
		return nil, NewInvalidResultReferenceError("no response found with clientId '" + ref.ResultOf + "'")
	}

	// Verify the method name matches
	if response.Name != ref.Name {
		return nil, NewInvalidResultReferenceError("response clientId '" + ref.ResultOf + "' has method name '" + response.Name + "', expected '" + ref.Name + "'")
	}

	// Evaluate the path
	result, err := EvaluatePath(response.Args, ref.Path)
	if err != nil {
		return nil, NewInvalidResultReferenceError("failed to evaluate path '" + ref.Path + "': " + err.Error())
	}

	return result, nil
}

// parseResultReference parses a result reference value into a ResultReference struct
func parseResultReference(value any) (*ResultReference, error) {
	obj, ok := value.(map[string]any)
	if !ok {
		return nil, NewInvalidResultReferenceError("result reference must be an object")
	}

	resultOf, ok := obj["resultOf"].(string)
	if !ok {
		return nil, NewInvalidResultReferenceError("result reference 'resultOf' must be a string")
	}

	name, ok := obj["name"].(string)
	if !ok {
		return nil, NewInvalidResultReferenceError("result reference 'name' must be a string")
	}

	path, ok := obj["path"].(string)
	if !ok {
		return nil, NewInvalidResultReferenceError("result reference 'path' must be a string")
	}

	return &ResultReference{
		ResultOf: resultOf,
		Name:     name,
		Path:     path,
	}, nil
}
