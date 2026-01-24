package plugin

import "strings"

// IsAllowedARN checks if the callerARN matches any of the registered ARNs.
// It handles assumed-role ARN translation: if the caller is using an assumed role
// (arn:aws:sts::<account>:assumed-role/<role>/<session>), it matches against the
// source role ARN (arn:aws:iam::<account>:role/<role>).
func IsAllowedARN(registeredARNs []string, callerARN string) bool {
	if callerARN == "" {
		return false
	}

	// Normalize caller ARN (convert assumed-role to role ARN if needed)
	normalizedCaller := normalizeARN(callerARN)

	for _, registered := range registeredARNs {
		if registered == normalizedCaller {
			return true
		}
	}

	return false
}

// normalizeARN converts an assumed-role ARN to its source role ARN.
// Input:  arn:aws:sts::123456789012:assumed-role/RoleName/SessionName
// Output: arn:aws:iam::123456789012:role/RoleName
// If the ARN is already a role ARN or any other format, it's returned unchanged.
func normalizeARN(arn string) string {
	// Check if this is an assumed-role ARN
	// Format: arn:aws:sts::<account>:assumed-role/<role>/<session>
	if !strings.Contains(arn, ":assumed-role/") {
		return arn
	}

	// Parse the ARN parts
	// arn:aws:sts::123456789012:assumed-role/RoleName/SessionName
	parts := strings.Split(arn, ":")
	if len(parts) < 6 {
		return arn
	}

	// Extract account ID (parts[4])
	accountID := parts[4]

	// Extract role name from the resource part (parts[5])
	// Format: assumed-role/RoleName/SessionName
	// Or with path: assumed-role/path/to/RoleName/SessionName
	resource := parts[5]
	resourceParts := strings.Split(resource, "/")
	if len(resourceParts) < 3 {
		return arn // Need at least: assumed-role, RoleName, SessionName
	}

	// Join all parts except first (assumed-role) and last (session name)
	roleName := strings.Join(resourceParts[1:len(resourceParts)-1], "/")

	// Construct the IAM role ARN
	// arn:aws:iam::<account>:role/<role>
	return "arn:aws:iam::" + accountID + ":role/" + roleName
}
