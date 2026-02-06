#!/usr/bin/env bash
#
# JMAP Protocol Compliance Test Wrapper
#
# Uses jmapc (independent Python JMAP client) to validate protocol compliance.
# Creates ephemeral Cognito users for test isolation.
#
# Usage: ./jmap-client-test.sh [ENV]
#   ENV: test or prod (default: test)
#
# Prerequisites:
#   - AWS CLI configured with ses-mail profile
#   - Python 3 with venv installed
#   - jq installed
#   - Terraform applied for the target environment
#   - Python venv created via 'make scripts/.venv'
#

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m' # No Color

# Configuration
ENV="${1:-test}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
TF_DIR="$PROJECT_DIR/terraform/environments/$ENV"
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

    if ! command -v aws &> /dev/null; then
        missing+=("aws-cli")
    fi

    if [[ ${#missing[@]} -gt 0 ]]; then
        echo -e "${RED}ERROR: Missing required tools: ${missing[*]}${NC}"
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

# Load configuration from terraform outputs
load_config() {
    echo "Loading configuration..."

    # Get terraform outputs
    USER_POOL_ID=$(tf_output "cognito_user_pool_id")
    CLIENT_ID=$(tf_output "cognito_client_id")
    JMAP_HOST=$(tf_output "jmap_host")
    BLOB_BUCKET=$(tf_output "blob_bucket_name")
    DYNAMODB_TABLE=$(tf_output "dynamodb_table_name")
    API_GATEWAY_INVOKE_URL=$(tf_output "api_gateway_invoke_url")
    E2E_TEST_ROLE_ARN=$(tf_output "e2e_test_role_arn")

    # Email plugin table (may not exist)
    DYNAMODB_EMAIL_TABLE="${DYNAMODB_TABLE/core/email}" || ""

    # Extract region from Cognito User Pool ID (format: region_xxxxxx)
    REGION=$(echo "$USER_POOL_ID" | cut -d'_' -f1)
    if [[ -z "$REGION" ]]; then
        REGION="ap-southeast-2"
    fi

    echo "  Environment: $ENV"
    echo "  Region: $REGION"
    echo "  JMAP Host: $JMAP_HOST"
    echo "  Core Table: $DYNAMODB_TABLE"
    echo "  Email Table: $DYNAMODB_EMAIL_TABLE"
    echo -e "${GREEN}  Using ephemeral test users${NC}"
}

# Run the Python test
run_python_test() {
    echo ""
    echo "Running JMAP protocol compliance tests..."
    echo ""

    # Export environment variables for the Python script
    export JMAP_HOST="$JMAP_HOST"
    export BLOB_BUCKET="$BLOB_BUCKET"
    export DYNAMODB_TABLE="$DYNAMODB_TABLE"
    export DYNAMODB_EMAIL_TABLE="$DYNAMODB_EMAIL_TABLE"
    export API_GATEWAY_INVOKE_URL="$API_GATEWAY_INVOKE_URL"
    export E2E_TEST_ROLE_ARN="$E2E_TEST_ROLE_ARN"
    export AWS_REGION="$REGION"
    export COGNITO_USER_POOL_ID="$USER_POOL_ID"
    export COGNITO_CLIENT_ID="$CLIENT_ID"

    # Activate venv and run pytest
    source "$VENV_DIR/bin/activate"
    python -m pytest "$SCRIPT_DIR/jmap-client/" -v ${PYTEST_ARGS:-} "$@"
}

# Main
main() {
    echo "========================================"
    echo "JMAP Protocol Compliance Tests (jmapc)"
    echo "========================================"

    check_prerequisites
    load_config
    run_python_test
}

main "$@"
