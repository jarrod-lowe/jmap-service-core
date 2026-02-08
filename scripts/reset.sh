#!/usr/bin/env bash
#
# Reset script for JMAP Service
#
# Usage: ./reset.sh [ENV] [--dry-run]
#   ENV: test or prod (default: test)
#   --dry-run: Preview changes without executing
#
# Prerequisites:
#   - AWS CLI configured with ses-mail profile
#   - jq installed (for JSON parsing)
#   - yq installed (for YAML parsing)
#   - Terraform applied for the target environment
#   - test-user.yaml exists
#

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
ENV="${1:-test}"
DRY_RUN=false
if [[ "${2:-}" == "--dry-run" ]]; then
    DRY_RUN=true
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
TF_DIR="$PROJECT_DIR/terraform/environments/$ENV"
TEST_USER_FILE="$PROJECT_DIR/test-user.yaml"
AWS_PROFILE="ses-mail"

START_TIME=$(date +%s)

# Counters
S3_OBJECTS_DELETED=0
DYNAMODB_ITEMS_DELETED=0
COGNITO_USERS_DELETED=0
ACCOUNT_REINITIALIZED=false

# Validate environment
if [[ "$ENV" != "test" && "$ENV" != "prod" ]]; then
    echo -e "${RED}ERROR: ENV must be 'test' or 'prod'${NC}"
    exit 1
fi

# Check prerequisites
check_prerequisites() {
    echo "Checking prerequisites..."
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

    # Verify AWS credentials are valid
    if ! AWS_PROFILE="$AWS_PROFILE" aws sts get-caller-identity &> /dev/null; then
        echo -e "${RED}ERROR: AWS credentials not valid for profile $AWS_PROFILE${NC}"
        exit 1
    fi

    echo -e "${GREEN}  Prerequisites check passed${NC}"
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
    TEST_USERNAME=$(yq ".env.$ENV.username" "$TEST_USER_FILE")
    TEST_PASSWORD=$(yq ".env.$ENV.password" "$TEST_USER_FILE")

    if [[ -z "$TEST_USERNAME" || "$TEST_USERNAME" == "null" ]]; then
        echo -e "${RED}ERROR: Username not found in $TEST_USER_FILE for env.$ENV${NC}"
        exit 1
    fi

    if [[ -z "$TEST_PASSWORD" || "$TEST_PASSWORD" == "null" ]]; then
        echo -e "${RED}ERROR: Password not found in $TEST_USER_FILE for env.$ENV${NC}"
        exit 1
    fi

    # Get terraform outputs
    S3_BUCKET=$(tf_output "blob_bucket_name")
    DYNAMODB_TABLE=$(tf_output "dynamodb_table_name")
    USER_POOL_ID=$(tf_output "cognito_user_pool_id")
    CLIENT_ID=$(tf_output "cognito_client_id")

    # Extract region from user pool ID
    REGION=$(echo "$USER_POOL_ID" | cut -d'_' -f1)

    if [[ -z "$S3_BUCKET" || -z "$DYNAMODB_TABLE" || -z "$USER_POOL_ID" || -z "$CLIENT_ID" ]]; then
        echo -e "${RED}ERROR: Failed to load required terraform outputs${NC}"
        exit 1
    fi

    echo "  Environment: $ENV"
    echo "  Region: $REGION"
    echo "  S3 Bucket: $S3_BUCKET"
    echo "  DynamoDB Table: $DYNAMODB_TABLE"
    echo "  Cognito User Pool: $USER_POOL_ID"
    echo "  Test User: $TEST_USERNAME"
}

# Display confirmation prompt
confirm_reset() {
    if [[ "$DRY_RUN" == true ]]; then
        echo -e "${YELLOW}Running in DRY-RUN mode - no changes will be made${NC}"
        return
    fi

    # Skip confirmation for test environment (it's for development)
    if [[ "$ENV" == "test" ]]; then
        echo ""
        echo "Resetting test environment (no confirmation required)..."
        return
    fi

    # Require confirmation for prod
    echo ""
    echo -e "${RED}WARNING: This will DELETE the following in PRODUCTION:${NC}"
    echo "  - S3 Bucket: $S3_BUCKET - ALL objects"
    echo "  - DynamoDB Table: $DYNAMODB_TABLE - ALL records except PLUGIN# items"
    echo "  - Cognito User Pool: $USER_POOL_ID - ALL users except $TEST_USERNAME"
    echo ""
    echo "  The test user account will be reinitialized with fresh quota."
    echo ""
    read -p "Type 'yes' to proceed, or 'n' to abort: " -r
    echo

    if [[ ! $REPLY =~ ^yes$ ]]; then
        echo -e "${YELLOW}Reset aborted by user${NC}"
        exit 0
    fi
}

# Clean S3 bucket
clean_s3() {
    echo ""
    echo "Cleaning S3 bucket: $S3_BUCKET..."

    # Count objects first
    local object_count
    object_count=$(AWS_PROFILE="$AWS_PROFILE" aws s3 ls "s3://$S3_BUCKET/" --recursive 2>/dev/null | wc -l || echo "0")
    object_count=$(echo "$object_count" | tr -d ' ')

    if [[ "$object_count" == "0" ]]; then
        echo -e "${GREEN}  Bucket is already empty (0 objects)${NC}"
        return
    fi

    echo "  Found $object_count objects to delete"

    if [[ "$DRY_RUN" == true ]]; then
        echo -e "${YELLOW}  [DRY-RUN] Would delete $object_count objects${NC}"
        return
    fi

    # Delete all objects
    AWS_PROFILE="$AWS_PROFILE" aws s3 rm "s3://$S3_BUCKET/" --recursive

    S3_OBJECTS_DELETED=$object_count
    echo -e "${GREEN}  Deleted $S3_OBJECTS_DELETED objects${NC}"
}

# Clean DynamoDB table
clean_dynamodb() {
    echo ""
    echo "Cleaning DynamoDB table: $DYNAMODB_TABLE..."

    local scan_output
    local items=()
    local last_evaluated_key=""

    # Scan table with pagination, filtering out PLUGIN# items
    while true; do
        if [[ -z "$last_evaluated_key" ]]; then
            scan_output=$(AWS_PROFILE="$AWS_PROFILE" aws dynamodb scan \
                --table-name "$DYNAMODB_TABLE" \
                --region "$REGION" 2>/dev/null || echo "{}")
        else
            scan_output=$(AWS_PROFILE="$AWS_PROFILE" aws dynamodb scan \
                --table-name "$DYNAMODB_TABLE" \
                --region "$REGION" \
                --exclusive-start-key "$last_evaluated_key" 2>/dev/null || echo "{}")
        fi

        # Extract items that don't start with PLUGIN#
        local page_items
        page_items=$(echo "$scan_output" | jq -c '.Items[] | select(.pk.S | startswith("PLUGIN#") | not) | {pk: .pk, sk: .sk}' 2>/dev/null || echo "")

        if [[ -n "$page_items" ]]; then
            while IFS= read -r item; do
                items+=("$item")
            done <<< "$page_items"
        fi

        # Check for more pages
        last_evaluated_key=$(echo "$scan_output" | jq -c '.LastEvaluatedKey // empty' 2>/dev/null || echo "")
        if [[ -z "$last_evaluated_key" ]]; then
            break
        fi
    done

    local item_count=${#items[@]}

    if [[ $item_count -eq 0 ]]; then
        echo -e "${GREEN}  No items to delete (PLUGIN# items preserved)${NC}"
        return
    fi

    echo "  Found $item_count items to delete (excluding PLUGIN# items)"

    if [[ "$DRY_RUN" == true ]]; then
        echo -e "${YELLOW}  [DRY-RUN] Would delete $item_count items${NC}"
        return
    fi

    # Delete items in batches of 25 (DynamoDB limit)
    local batch_size=25
    local deleted=0
    local batch_requests=""

    for item in "${items[@]}"; do
        batch_requests="$batch_requests{\"DeleteRequest\":{\"Key\":$item}},"
        ((deleted++))

        # When we hit batch size or last item, send the batch
        if [[ $((deleted % batch_size)) -eq 0 ]] || [[ $deleted -eq $item_count ]]; then
            # Remove trailing comma and wrap in array
            batch_requests="[${batch_requests%,}]"

            # Execute batch delete with retry logic
            local retry_count=0
            local max_retries=3
            while [[ $retry_count -lt $max_retries ]]; do
                local batch_output
                batch_output=$(AWS_PROFILE="$AWS_PROFILE" aws dynamodb batch-write-item \
                    --request-items "{\"$DYNAMODB_TABLE\":$batch_requests}" \
                    --region "$REGION" 2>/dev/null || echo "{}")

                # Check for unprocessed items
                local unprocessed
                unprocessed=$(echo "$batch_output" | jq -r '.UnprocessedItems // {} | length' 2>/dev/null || echo "0")

                if [[ "$unprocessed" == "0" ]]; then
                    break
                fi

                # Exponential backoff
                sleep $((2 ** retry_count))
                ((retry_count++))
            done

            if [[ $retry_count -ge $max_retries ]]; then
                echo -e "${YELLOW}  Warning: Some items may not have been deleted (max retries reached)${NC}"
            fi

            batch_requests=""
        fi
    done

    DYNAMODB_ITEMS_DELETED=$deleted
    echo -e "${GREEN}  Deleted $DYNAMODB_ITEMS_DELETED items${NC}"
}

# Clean Cognito users
clean_cognito() {
    echo ""
    echo "Cleaning Cognito users (preserving $TEST_USERNAME)..."

    # List all users
    local users_output
    users_output=$(AWS_PROFILE="$AWS_PROFILE" aws cognito-idp list-users \
        --user-pool-id "$USER_POOL_ID" \
        --region "$REGION" 2>/dev/null || echo "{}")

    # Extract usernames, excluding the test user
    local users
    users=$(echo "$users_output" | jq -r --arg test_email "$TEST_USERNAME" \
        '.Users[] | select((.Attributes | map(select(.Name == "email" and .Value == $test_email)) | length) == 0) | .Username' 2>/dev/null || echo "")

    if [[ -z "$users" ]]; then
        echo -e "${GREEN}  No users to delete (only test user exists)${NC}"
        return
    fi

    local user_count
    user_count=$(echo "$users" | wc -l | tr -d ' ')
    echo "  Found $user_count users to delete"

    if [[ "$DRY_RUN" == true ]]; then
        echo -e "${YELLOW}  [DRY-RUN] Would delete $user_count users:${NC}"
        echo "$users" | sed 's/^/    /'
        return
    fi

    # Delete each user
    local deleted=0
    while IFS= read -r username; do
        if [[ -n "$username" ]]; then
            AWS_PROFILE="$AWS_PROFILE" aws cognito-idp admin-delete-user \
                --user-pool-id "$USER_POOL_ID" \
                --username "$username" \
                --region "$REGION" 2>/dev/null || true
            ((deleted++))
        fi
    done <<< "$users"

    COGNITO_USERS_DELETED=$deleted
    echo -e "${GREEN}  Deleted $COGNITO_USERS_DELETED users${NC}"
}

# Reinitialize test user account
reinitialize_account() {
    if [[ "$DRY_RUN" == true ]]; then
        echo ""
        echo -e "${YELLOW}[DRY-RUN] Would check if test user exists and reinitialize${NC}"
        return 0
    fi

    echo ""
    echo "Checking test user account..."

    # Check if test user exists
    local user_check
    user_check=$(AWS_PROFILE="$AWS_PROFILE" aws cognito-idp admin-get-user \
        --user-pool-id "$USER_POOL_ID" \
        --username "$TEST_USERNAME" \
        --region "$REGION" 2>&1)

    if [[ $? -ne 0 ]]; then
        if echo "$user_check" | grep -q "UserNotFoundException"; then
            echo -e "${RED}ERROR: Test user '$TEST_USERNAME' does not exist in Cognito${NC}"
            echo ""
            echo "The test user should have been created automatically by Terraform."
            echo ""
            echo "Please run the following to deploy and generate credentials:"
            echo ""
            echo -e "  ${GREEN}AWS_PROFILE=$AWS_PROFILE make apply-test ENV=$ENV${NC}"
            echo ""
            return 1
        else
            echo -e "${RED}ERROR: Failed to check if test user exists${NC}"
            echo "$user_check"
            return 1
        fi
    fi

    echo "  Test user exists - reinitializing account..."

    # Set custom:account_initialized to false to trigger reinitialization
    local update_result
    update_result=$(AWS_PROFILE="$AWS_PROFILE" aws cognito-idp admin-update-user-attributes \
        --user-pool-id "$USER_POOL_ID" \
        --username "$TEST_USERNAME" \
        --user-attributes Name=custom:account_initialized,Value=false \
        --region "$REGION" 2>&1)

    if [[ $? -ne 0 ]]; then
        echo -e "${YELLOW}  Warning: Could not update account_initialized attribute${NC}"
        echo "  $update_result"
    fi

    # Authenticate to trigger the Post Authentication Lambda
    local auth_result
    local auth_exit_code
    local auth_params
    auth_params=$(jq -n \
        --arg username "$TEST_USERNAME" \
        --arg password "$TEST_PASSWORD" \
        '{USERNAME: $username, PASSWORD: $password}')
    auth_result=$(AWS_PROFILE="$AWS_PROFILE" aws cognito-idp admin-initiate-auth \
        --user-pool-id "$USER_POOL_ID" \
        --client-id "$CLIENT_ID" \
        --auth-flow ADMIN_NO_SRP_AUTH \
        --auth-parameters "$auth_params" \
        --region "$REGION" 2>&1)
    auth_exit_code=$?

    if [[ $auth_exit_code -ne 0 ]]; then
        echo -e "${RED}ERROR: Failed to authenticate test user${NC}"
        if echo "$auth_result" | grep -q "NotAuthorizedException"; then
            echo ""
            echo "Authentication failed with incorrect username or password."
            echo "This likely means the password in test-user.yaml doesn't match Cognito."
            echo ""
            echo "To fix this, either:"
            echo "  1. Update the password in test-user.yaml to match Cognito"
            echo "  2. Run 'AWS_PROFILE=$AWS_PROFILE make apply ENV=$ENV' to reset the password"
            echo ""
        else
            echo "  Auth error: $(echo "$auth_result" | grep "An error occurred" || echo "$auth_result")"
        fi
        return 1
    fi

    # Verify we got a token
    local token
    token=$(echo "$auth_result" | jq -r '.AuthenticationResult.IdToken // empty' 2>/dev/null)

    if [[ -z "$token" ]]; then
        echo -e "${RED}ERROR: Failed to get authentication token${NC}"
        echo "Auth result:"
        echo "$auth_result"
        return 1
    fi

    ACCOUNT_REINITIALIZED=true
    echo -e "${GREEN}  Test user account reinitialized successfully${NC}"
    return 0
}

# Print summary
print_summary() {
    local end_time
    end_time=$(date +%s)
    local elapsed=$((end_time - START_TIME))

    echo ""
    echo "================================"
    echo "Reset Summary"
    echo "================================"
    echo "Environment: $ENV"
    if [[ "$DRY_RUN" == true ]]; then
        echo -e "Mode: ${YELLOW}DRY-RUN${NC}"
    fi
    echo "S3 objects deleted: $S3_OBJECTS_DELETED"
    echo "DynamoDB items deleted: $DYNAMODB_ITEMS_DELETED"
    echo "Cognito users deleted: $COGNITO_USERS_DELETED"
    if [[ "$DRY_RUN" == false ]]; then
        if [[ "$ACCOUNT_REINITIALIZED" == true ]]; then
            echo "Test user account: Reinitialized"
        else
            echo "Test user account: Not reinitialized (user doesn't exist or authentication failed)"
        fi
    fi
    echo "Time elapsed: ${elapsed}s"
    echo "================================"

    if [[ "$DRY_RUN" == false ]]; then
        if [[ "$ACCOUNT_REINITIALIZED" == true ]]; then
            echo -e "${GREEN}Reset completed successfully${NC}"
        else
            echo -e "${RED}Reset completed with errors - see above${NC}"
        fi
    else
        echo -e "${YELLOW}Dry-run completed - no changes made${NC}"
    fi
}

# Main
main() {
    echo "================================"
    echo "JMAP Service Reset Script"
    echo "================================"

    check_prerequisites
    load_config
    confirm_reset

    # Run cleanup operations
    clean_s3
    clean_dynamodb
    clean_cognito

    # Reinitialize account - fail if this fails
    if ! reinitialize_account; then
        print_summary
        exit 1
    fi

    # Print summary
    print_summary
}

main "$@"
