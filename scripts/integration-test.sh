#!/usr/bin/env bash
#
# Integration test script for JMAP Service
#
# Usage: ./integration-test.sh [ENV]
#   ENV: test or prod (default: test)
#
# Prerequisites:
#   - AWS CLI configured with ses-mail profile
#   - jq installed
#   - yq installed (for YAML parsing)
#   - Terraform applied for the target environment
#

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Counters
TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0

# Configuration
ENV="${1:-test}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
TF_DIR="$PROJECT_DIR/terraform/environments/$ENV"
TEST_USER_FILE="$PROJECT_DIR/test-user.yaml"
AWS_PROFILE="ses-mail"

# Validate environment
if [[ "$ENV" != "test" && "$ENV" != "prod" ]]; then
    echo -e "${RED}ERROR: ENV must be 'test' or 'prod'${NC}"
    exit 1
fi

# Check prerequisites
check_prerequisites() {
    local missing=()

    if ! command -v jq &> /dev/null; then
        missing+=("jq")
    fi

    if ! command -v yq &> /dev/null; then
        missing+=("yq")
    fi

    if ! command -v aws &> /dev/null; then
        missing+=("aws-cli")
    fi

    if [[ ${#missing[@]} -gt 0 ]]; then
        echo -e "${RED}ERROR: Missing required tools: ${missing[*]}${NC}"
        exit 1
    fi

    if [[ ! -f "$TEST_USER_FILE" ]]; then
        echo -e "${RED}ERROR: Test user file not found: $TEST_USER_FILE${NC}"
        exit 1
    fi

    if [[ ! -d "$TF_DIR/.terraform" ]]; then
        echo -e "${RED}ERROR: Terraform not initialized for $ENV. Run 'make init ENV=$ENV' first.${NC}"
        exit 1
    fi
}

# Get terraform output value
tf_output() {
    local key="$1"
    cd "$TF_DIR" && AWS_PROFILE="$AWS_PROFILE" terraform output -raw "$key" 2>/dev/null
}

# Load configuration from terraform and test-user.yaml
load_config() {
    echo "Loading configuration..."

    # Get credentials from test-user.yaml
    USERNAME=$(yq ".env.$ENV.username" "$TEST_USER_FILE")
    PASSWORD=$(yq ".env.$ENV.password" "$TEST_USER_FILE")

    if [[ -z "$USERNAME" || "$USERNAME" == "null" ]]; then
        echo -e "${RED}ERROR: Username not found in $TEST_USER_FILE for env.$ENV${NC}"
        exit 1
    fi

    # Get terraform outputs
    USER_POOL_ID=$(tf_output "cognito_user_pool_id")
    CLIENT_ID=$(tf_output "cognito_client_id")
    API_URL=$(tf_output "api_gateway_invoke_url")

    # Extract region from API URL or use default
    REGION=$(echo "$API_URL" | sed -n 's/.*execute-api\.\([^.]*\)\.amazonaws\.com.*/\1/p')
    if [[ -z "$REGION" ]]; then
        REGION="ap-southeast-2"
    fi

    echo "  Environment: $ENV"
    echo "  Region: $REGION"
    echo "  API URL: $API_URL"
    echo "  User: $USERNAME"
}

# Authenticate and get token
get_token() {
    echo "Authenticating..."

    local auth_result
    local auth_params
    auth_params=$(jq -n \
        --arg username "$USERNAME" \
        --arg password "$PASSWORD" \
        '{USERNAME: $username, PASSWORD: $password}')
    auth_result=$(AWS_PROFILE="$AWS_PROFILE" aws cognito-idp admin-initiate-auth \
        --user-pool-id "$USER_POOL_ID" \
        --client-id "$CLIENT_ID" \
        --auth-flow ADMIN_NO_SRP_AUTH \
        --auth-parameters "$auth_params" \
        --region "$REGION" \
        2>&1)

    if [[ $? -ne 0 ]]; then
        echo -e "${RED}ERROR: Authentication failed${NC}"
        echo "$auth_result"
        exit 1
    fi

    TOKEN=$(echo "$auth_result" | jq -r '.AuthenticationResult.IdToken')

    if [[ -z "$TOKEN" || "$TOKEN" == "null" ]]; then
        echo -e "${RED}ERROR: Failed to extract token from auth result${NC}"
        echo "$auth_result"
        exit 1
    fi

    echo -e "${GREEN}  Authentication successful${NC}"
}

# Test helper functions
# Note: Using pre-increment (++VAR) because post-increment returns the original
# value, which is 0 on first call, causing exit code 1 with set -e
pass() {
    local test_name="$1"
    echo -e "${GREEN}PASS${NC}: $test_name"
    ((++TESTS_PASSED))
    ((++TESTS_RUN))
}

fail() {
    local test_name="$1"
    local reason="${2:-}"
    echo -e "${RED}FAIL${NC}: $test_name"
    if [[ -n "$reason" ]]; then
        echo "      $reason"
    fi
    ((++TESTS_FAILED))
    ((++TESTS_RUN))
}

# Make authenticated request
api_get() {
    local path="$1"
    curl -s -w "\n%{http_code}" \
        -H "Authorization: Bearer $TOKEN" \
        "${API_URL}${path}"
}

api_post() {
    local path="$1"
    local body="$2"
    curl -s -w "\n%{http_code}" \
        -H "Authorization: Bearer $TOKEN" \
        -H "Content-Type: application/json" \
        -d "$body" \
        "${API_URL}${path}"
}

# Parse response to get body and status code
parse_response() {
    local response="$1"
    RESPONSE_BODY=$(echo "$response" | sed '$d')
    RESPONSE_CODE=$(echo "$response" | tail -1)
}

# Test: Session Discovery
test_session_discovery() {
    echo ""
    echo "Testing Session Discovery (GET /.well-known/jmap)..."

    local response
    response=$(api_get "/.well-known/jmap")
    parse_response "$response"

    # Test 1: HTTP 200
    if [[ "$RESPONSE_CODE" == "200" ]]; then
        pass "Session Discovery returns 200"
    else
        fail "Session Discovery returns 200" "Got HTTP $RESPONSE_CODE"
        return
    fi

    # Test 2: Has core capability
    if printf '%s' "$RESPONSE_BODY" | jq -e '.capabilities["urn:ietf:params:jmap:core"]' > /dev/null 2>&1; then
        pass "Session has urn:ietf:params:jmap:core capability"
    else
        fail "Session has urn:ietf:params:jmap:core capability"
    fi

    # Test 3: Has mail capability
    if printf '%s' "$RESPONSE_BODY" | jq -e '.capabilities["urn:ietf:params:jmap:mail"]' > /dev/null 2>&1; then
        pass "Session has urn:ietf:params:jmap:mail capability"
    else
        fail "Session has urn:ietf:params:jmap:mail capability"
    fi

    # Test 4: Has accounts
    if printf '%s' "$RESPONSE_BODY" | jq -e '.accounts | keys | length > 0' > /dev/null 2>&1; then
        pass "Session has at least one account"
        # Extract account ID for later tests
        ACCOUNT_ID=$(printf '%s' "$RESPONSE_BODY" | jq -r '.accounts | keys[0]')
        echo "      Account ID: $ACCOUNT_ID"
    else
        fail "Session has at least one account"
    fi
}

# Test: Email/get method
test_email_get() {
    echo ""
    echo "Testing Email/get (POST /jmap)..."

    local body
    body=$(cat <<EOF
{
    "using": ["urn:ietf:params:jmap:core", "urn:ietf:params:jmap:mail"],
    "methodCalls": [
        ["Email/get", {"accountId": "$ACCOUNT_ID", "ids": []}, "call1"]
    ]
}
EOF
)

    local response
    response=$(api_post "/jmap" "$body")
    parse_response "$response"

    # Test: Should get serverFail error (plugin not implemented)
    if [[ "$RESPONSE_CODE" == "200" ]]; then
        pass "Email/get returns 200"
    else
        fail "Email/get returns 200" "Got HTTP $RESPONSE_CODE"
        return
    fi

    # Check for expected error
    local error_type
    error_type=$(printf '%s' "$RESPONSE_BODY" | jq -r '.methodResponses[0][0]' 2>/dev/null) || error_type=""

    if [[ "$error_type" == "error" ]]; then
        local error_msg
        error_msg=$(printf '%s' "$RESPONSE_BODY" | jq -r '.methodResponses[0][1].type' 2>/dev/null) || error_msg=""
        if [[ "$error_msg" == "serverFail" ]]; then
            pass "Email/get returns serverFail (plugin not yet implemented)"
        else
            fail "Email/get returns serverFail" "Got error type: $error_msg"
        fi
    else
        fail "Email/get returns expected error response" "Got: $error_type"
    fi
}

# Test: Email/query method
test_email_query() {
    echo ""
    echo "Testing Email/query (POST /jmap)..."

    local body
    body=$(cat <<EOF
{
    "using": ["urn:ietf:params:jmap:core", "urn:ietf:params:jmap:mail"],
    "methodCalls": [
        ["Email/query", {"accountId": "$ACCOUNT_ID"}, "call1"]
    ]
}
EOF
)

    local response
    response=$(api_post "/jmap" "$body")
    parse_response "$response"

    if [[ "$RESPONSE_CODE" == "200" ]]; then
        pass "Email/query returns 200"
    else
        fail "Email/query returns 200" "Got HTTP $RESPONSE_CODE"
        return
    fi

    # Check for expected error
    local error_type
    error_type=$(printf '%s' "$RESPONSE_BODY" | jq -r '.methodResponses[0][0]' 2>/dev/null) || error_type=""

    if [[ "$error_type" == "error" ]]; then
        local error_msg
        error_msg=$(printf '%s' "$RESPONSE_BODY" | jq -r '.methodResponses[0][1].type' 2>/dev/null) || error_msg=""
        if [[ "$error_msg" == "serverFail" ]]; then
            pass "Email/query returns serverFail (plugin not yet implemented)"
        else
            fail "Email/query returns serverFail" "Got error type: $error_msg"
        fi
    else
        fail "Email/query returns expected error response" "Got: $error_type"
    fi
}

# Test: Email/import method
test_email_import() {
    echo ""
    echo "Testing Email/import (POST /jmap)..."

    local body
    body=$(cat <<EOF
{
    "using": ["urn:ietf:params:jmap:core", "urn:ietf:params:jmap:mail"],
    "methodCalls": [
        ["Email/import", {"accountId": "$ACCOUNT_ID", "emails": {}}, "call1"]
    ]
}
EOF
)

    local response
    response=$(api_post "/jmap" "$body")
    parse_response "$response"

    if [[ "$RESPONSE_CODE" == "200" ]]; then
        pass "Email/import returns 200"
    else
        fail "Email/import returns 200" "Got HTTP $RESPONSE_CODE"
        return
    fi

    # Check for expected error
    local error_type
    error_type=$(printf '%s' "$RESPONSE_BODY" | jq -r '.methodResponses[0][0]' 2>/dev/null) || error_type=""

    if [[ "$error_type" == "error" ]]; then
        local error_msg
        error_msg=$(printf '%s' "$RESPONSE_BODY" | jq -r '.methodResponses[0][1].type' 2>/dev/null) || error_msg=""
        if [[ "$error_msg" == "serverFail" ]]; then
            pass "Email/import returns serverFail (plugin not yet implemented)"
        else
            fail "Email/import returns serverFail" "Got error type: $error_msg"
        fi
    else
        fail "Email/import returns expected error response" "Got: $error_type"
    fi
}

# Print summary
print_summary() {
    echo ""
    echo "================================"
    echo "Integration Test Summary"
    echo "================================"
    echo "Environment: $ENV"
    echo "Tests run:   $TESTS_RUN"
    echo -e "Passed:      ${GREEN}$TESTS_PASSED${NC}"
    if [[ $TESTS_FAILED -gt 0 ]]; then
        echo -e "Failed:      ${RED}$TESTS_FAILED${NC}"
    else
        echo -e "Failed:      $TESTS_FAILED"
    fi
    echo "================================"

    if [[ $TESTS_FAILED -gt 0 ]]; then
        echo -e "${RED}INTEGRATION TESTS FAILED${NC}"
        return 1
    else
        echo -e "${GREEN}ALL TESTS PASSED${NC}"
        return 0
    fi
}

# Main
main() {
    echo "================================"
    echo "JMAP Service Integration Tests"
    echo "================================"

    check_prerequisites
    load_config
    get_token

    # Run tests
    test_session_discovery
    test_email_get
    test_email_query
    test_email_import

    # Print summary and exit with appropriate code
    print_summary
}

main "$@"
