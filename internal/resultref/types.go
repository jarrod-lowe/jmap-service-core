package resultref

import "fmt"

// ResultReference represents a JMAP result reference per RFC 8620 Section 3.7
type ResultReference struct {
	ResultOf string `json:"resultOf"` // Client ID of the method call to reference
	Name     string `json:"name"`     // Method name that must match the referenced call
	Path     string `json:"path"`     // JSON Pointer path to extract from the result
}

// MethodResponse represents a tracked method response for result reference resolution
type MethodResponse struct {
	ClientID string
	Name     string
	Args     map[string]any
}

// ErrorType represents a JMAP error type
type ErrorType string

const (
	// ErrorInvalidResultReference is returned when a result reference cannot be resolved
	ErrorInvalidResultReference ErrorType = "invalidResultReference"
	// ErrorInvalidArguments is returned when args contain conflicting keys
	ErrorInvalidArguments ErrorType = "invalidArguments"
)

// ResolveError represents an error during result reference resolution
type ResolveError struct {
	Type        ErrorType
	Description string
}

func (e *ResolveError) Error() string {
	return fmt.Sprintf("%s: %s", e.Type, e.Description)
}

// NewInvalidResultReferenceError creates an invalidResultReference error
func NewInvalidResultReferenceError(description string) *ResolveError {
	return &ResolveError{
		Type:        ErrorInvalidResultReference,
		Description: description,
	}
}

// NewInvalidArgumentsError creates an invalidArguments error
func NewInvalidArgumentsError(description string) *ResolveError {
	return &ResolveError{
		Type:        ErrorInvalidArguments,
		Description: description,
	}
}
