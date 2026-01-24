package plugin

import "testing"

func TestIsAllowedARN_ExactRoleMatch_ReturnsTrue(t *testing.T) {
	registeredARNs := []string{
		"arn:aws:iam::123456789012:role/IngestRole",
	}
	callerARN := "arn:aws:iam::123456789012:role/IngestRole"

	result := IsAllowedARN(registeredARNs, callerARN)

	if !result {
		t.Error("expected exact role ARN match to return true")
	}
}

func TestIsAllowedARN_NonMatchingARN_ReturnsFalse(t *testing.T) {
	registeredARNs := []string{
		"arn:aws:iam::123456789012:role/IngestRole",
	}
	callerARN := "arn:aws:iam::123456789012:role/OtherRole"

	result := IsAllowedARN(registeredARNs, callerARN)

	if result {
		t.Error("expected non-matching ARN to return false")
	}
}

func TestIsAllowedARN_AssumedRoleMatchesSourceRole_ReturnsTrue(t *testing.T) {
	registeredARNs := []string{
		"arn:aws:iam::123456789012:role/IngestRole",
	}
	// Assumed role ARN format: arn:aws:sts::<account>:assumed-role/<role-name>/<session-name>
	callerARN := "arn:aws:sts::123456789012:assumed-role/IngestRole/my-session-123"

	result := IsAllowedARN(registeredARNs, callerARN)

	if !result {
		t.Error("expected assumed role caller to match its source role ARN")
	}
}

func TestIsAllowedARN_AssumedRoleDifferentRoleName_ReturnsFalse(t *testing.T) {
	registeredARNs := []string{
		"arn:aws:iam::123456789012:role/IngestRole",
	}
	callerARN := "arn:aws:sts::123456789012:assumed-role/OtherRole/session-456"

	result := IsAllowedARN(registeredARNs, callerARN)

	if result {
		t.Error("expected assumed role with different role name to return false")
	}
}

func TestIsAllowedARN_EmptyRegisteredARNs_ReturnsFalse(t *testing.T) {
	registeredARNs := []string{}
	callerARN := "arn:aws:iam::123456789012:role/AnyRole"

	result := IsAllowedARN(registeredARNs, callerARN)

	if result {
		t.Error("expected empty registered ARNs to return false")
	}
}

func TestIsAllowedARN_EmptyCallerARN_ReturnsFalse(t *testing.T) {
	registeredARNs := []string{
		"arn:aws:iam::123456789012:role/IngestRole",
	}
	callerARN := ""

	result := IsAllowedARN(registeredARNs, callerARN)

	if result {
		t.Error("expected empty caller ARN to return false")
	}
}

func TestIsAllowedARN_MultipleRegisteredARNs_MatchesAny(t *testing.T) {
	registeredARNs := []string{
		"arn:aws:iam::123456789012:role/RoleA",
		"arn:aws:iam::123456789012:role/RoleB",
		"arn:aws:iam::123456789012:role/RoleC",
	}
	callerARN := "arn:aws:iam::123456789012:role/RoleB"

	result := IsAllowedARN(registeredARNs, callerARN)

	if !result {
		t.Error("expected to match any of multiple registered ARNs")
	}
}

func TestIsAllowedARN_AssumedRoleDifferentAccount_ReturnsFalse(t *testing.T) {
	registeredARNs := []string{
		"arn:aws:iam::123456789012:role/IngestRole",
	}
	// Different account number
	callerARN := "arn:aws:sts::999999999999:assumed-role/IngestRole/session"

	result := IsAllowedARN(registeredARNs, callerARN)

	if result {
		t.Error("expected assumed role from different account to return false")
	}
}

func TestIsAllowedARN_AssumedRoleWithPathComponent_ReturnsTrue(t *testing.T) {
	registeredARNs := []string{
		"arn:aws:iam::123456789012:role/services/IngestRole",
	}
	callerARN := "arn:aws:sts::123456789012:assumed-role/services/IngestRole/my-session"

	result := IsAllowedARN(registeredARNs, callerARN)

	if !result {
		t.Errorf("Expected true for assumed-role with path component matching registered role")
	}
}

func TestIsAllowedARN_AssumedRoleWithMultiplePathComponents_ReturnsTrue(t *testing.T) {
	registeredARNs := []string{
		"arn:aws:iam::123456789012:role/prod/auth/IngestRole",
	}
	callerARN := "arn:aws:sts::123456789012:assumed-role/prod/auth/IngestRole/session-123"

	result := IsAllowedARN(registeredARNs, callerARN)

	if !result {
		t.Errorf("Expected true for assumed-role with multiple path components matching registered role")
	}
}
