#!/usr/bin/env bash
#
# JMAP Protocol Compliance Test Wrapper
#
# Uses jmapc (independent Python JMAP client) to validate protocol compliance.
#
# Usage: ./jmap-client-test.sh [ENV]
#   ENV: test or prod (default: test)
#
# Prerequisites:
#   - AWS CLI configured with ses-mail profile
#   - Python 3 with venv installed
#   - jq installed
#   - yq installed (for YAML parsing)
#   - Terraform applied for the target environment
#   - Python venv created via 'make scripts/.venv'
#

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
ENV="${1:-test}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
TF_DIR="$PROJECT_DIR/terraform/environments/$ENV"
TEST_USER_FILE="$PROJECT_DIR/test-user.yaml"
VENV_DIR="$PROJECT_DIR/scripts/.venv"
AWS_PROFILE="ses-mail"

# Validate environment
if [[ "$ENV" != "test" && "$ENV" != "prod" ]]; then
    echo -e "${RED}ERROR: ENV must be 'test' or 'prod'${NC}"
    exit 1
fi

# Check prerequisites
check_prerequisites() {
    local missing=()

    if ! command -v python3 &> /dev/null; then
        missing+=("python3")
    fi

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

    if [[ ! -d "$VENV_DIR" ]]; then
        echo -e "${RED}ERROR: Python venv not found. Run 'make scripts/.venv' first.${NC}"
        exit 1
    fi

    if [[ ! -f "$VENV_DIR/bin/python" ]]; then
        echo -e "${RED}ERROR: Python venv is corrupted. Delete scripts/.venv and run 'make scripts/.venv' again.${NC}"
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
    auth_result=$(AWS_PROFILE="$AWS_PROFILE" aws cognito-idp admin-initiate-auth \
        --user-pool-id "$USER_POOL_ID" \
        --client-id "$CLIENT_ID" \
        --auth-flow ADMIN_NO_SRP_AUTH \
        --auth-parameters "USERNAME=$USERNAME,PASSWORD=$PASSWORD" \
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

# Run the Python test
run_python_test() {
    echo ""
    echo "Running JMAP protocol compliance tests..."
    echo ""

    # Export environment variables for the Python script
    export JMAP_API_URL="$API_URL"
    export JMAP_API_TOKEN="$TOKEN"

    # Run the Python test script
    "$VENV_DIR/bin/python" "$SCRIPT_DIR/jmap-client/test_jmap.py"
}

# Main
main() {
    echo "========================================"
    echo "JMAP Protocol Compliance Tests (jmapc)"
    echo "========================================"

    check_prerequisites
    load_config
    get_token
    run_python_test
}

main "$@"
