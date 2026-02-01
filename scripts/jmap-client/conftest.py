"""pytest conftest for JMAP e2e tests with ephemeral test users."""

import os
import secrets
import string
import time
from dataclasses import dataclass
from uuid import uuid4

import boto3
import jmapc
import jmapc.session
import pytest
import requests

# Monkey-patch jmapc.Session to make event_source_url optional
# RFC 8620 allows omitting eventSourceUrl when SSE is not supported
from typing import Optional
jmapc.session.Session.__dataclass_fields__['event_source_url'].default = None
jmapc.session.Session.__dataclass_fields__['event_source_url'].type = Optional[str]


@dataclass
class TestAccount:
    """Ephemeral test account credentials."""
    username: str
    password: str
    token: str
    account_id: str


def generate_password(length: int = 24) -> str:
    """Generate a secure random password meeting Cognito requirements."""
    # Ensure we have at least one of each required character type
    chars = [
        secrets.choice(string.ascii_lowercase),
        secrets.choice(string.ascii_uppercase),
        secrets.choice(string.digits),
        secrets.choice("!@#$%^&*"),
    ]
    # Fill the rest with random characters
    remaining = length - len(chars)
    chars.extend(secrets.choice(string.ascii_letters + string.digits + "!@#$%^&*")
                 for _ in range(remaining))
    # Shuffle to avoid predictable positions
    secrets.SystemRandom().shuffle(chars)
    return "".join(chars)


def create_test_user(cognito_client, user_pool_id: str) -> tuple[str, str]:
    """Create an ephemeral Cognito user for testing."""
    username = f"jmap-test-{uuid4()}@test.local"
    password = generate_password()

    # Create user with temporary password
    cognito_client.admin_create_user(
        UserPoolId=user_pool_id,
        Username=username,
        TemporaryPassword=password,
        MessageAction="SUPPRESS",  # Don't send welcome email
    )

    # Set permanent password (bypasses force-change flow)
    cognito_client.admin_set_user_password(
        UserPoolId=user_pool_id,
        Username=username,
        Password=password,
        Permanent=True,
    )

    return username, password


def authenticate_user(cognito_client, user_pool_id: str, client_id: str,
                      username: str, password: str) -> str:
    """Authenticate user and return JWT token."""
    response = cognito_client.admin_initiate_auth(
        UserPoolId=user_pool_id,
        ClientId=client_id,
        AuthFlow="ADMIN_NO_SRP_AUTH",
        AuthParameters={
            "USERNAME": username,
            "PASSWORD": password,
        },
    )
    return response["AuthenticationResult"]["IdToken"]


def delete_test_user(cognito_client, user_pool_id: str, username: str) -> None:
    """Delete the ephemeral test user."""
    cognito_client.admin_delete_user(
        UserPoolId=user_pool_id,
        Username=username,
    )


def verify_dynamodb_clean(dynamodb_client, table_name: str, account_id: str) -> list[str]:
    """Verify only infrastructure records exist. Returns list of orphan sks if any.

    Infrastructure records that are expected to remain:
    - META# - account metadata
    - STATE#* - permanent state counters per object type
    - CHANGE#* - change log entries with 7-day TTL for /changes API
    """
    response = dynamodb_client.query(
        TableName=table_name,
        KeyConditionExpression="pk = :pk",
        ExpressionAttributeValues={":pk": {"S": f"ACCOUNT#{account_id}"}},
        ProjectionExpression="sk",
    )

    orphans = []
    for item in response.get("Items", []):
        sk = item["sk"]["S"]
        # Allow infrastructure records: META#, STATE#*, CHANGE#*
        if sk.startswith(("META#", "STATE#", "CHANGE#")):
            continue
        orphans.append(sk)

    return orphans


def verify_s3_clean(s3_client, bucket: str, account_id: str) -> list[str]:
    """Verify no S3 objects exist for account. Returns list of orphan keys if any."""
    if not bucket:
        return []

    response = s3_client.list_objects_v2(
        Bucket=bucket,
        Prefix=f"{account_id}/",
    )

    return [obj["Key"] for obj in response.get("Contents", [])]


def delete_infrastructure_records(dynamodb_client, table_name: str, account_id: str) -> None:
    """Delete infrastructure records (META#, STATE#*, CHANGE#*) for cleanup."""
    # Query all records for this account
    response = dynamodb_client.query(
        TableName=table_name,
        KeyConditionExpression="pk = :pk",
        ExpressionAttributeValues={":pk": {"S": f"ACCOUNT#{account_id}"}},
        ProjectionExpression="sk",
    )

    # Delete infrastructure records
    for item in response.get("Items", []):
        sk = item["sk"]["S"]
        if sk.startswith(("META#", "STATE#", "CHANGE#")):
            dynamodb_client.delete_item(
                TableName=table_name,
                Key={
                    "pk": {"S": f"ACCOUNT#{account_id}"},
                    "sk": {"S": sk},
                },
            )


@pytest.fixture(scope="session")
def jmap_host():
    """JMAP host URL from environment."""
    host = os.environ.get("JMAP_HOST")
    assert host, "JMAP_HOST environment variable not set"
    return host


@pytest.fixture(scope="session")
def blob_bucket():
    """S3 blob bucket name from environment."""
    return os.environ.get("BLOB_BUCKET", "")


@pytest.fixture(scope="session")
def dynamodb_table():
    """DynamoDB core table name from environment."""
    return os.environ.get("DYNAMODB_TABLE", "")


@pytest.fixture(scope="session")
def dynamodb_email_table():
    """DynamoDB email plugin table name from environment."""
    return os.environ.get("DYNAMODB_EMAIL_TABLE", "")


@pytest.fixture(scope="session")
def aws_region():
    """AWS region from environment."""
    return os.environ.get("AWS_REGION", "ap-southeast-2")


@pytest.fixture(scope="session")
def cognito_user_pool_id():
    """Cognito User Pool ID from environment."""
    pool_id = os.environ.get("COGNITO_USER_POOL_ID")
    assert pool_id, "COGNITO_USER_POOL_ID environment variable not set"
    return pool_id


@pytest.fixture(scope="session")
def cognito_client_id():
    """Cognito Client ID from environment."""
    client_id = os.environ.get("COGNITO_CLIENT_ID")
    assert client_id, "COGNITO_CLIENT_ID environment variable not set"
    return client_id


@pytest.fixture(scope="session")
def test_account(jmap_host, cognito_user_pool_id, cognito_client_id,
                 dynamodb_table, dynamodb_email_table, blob_bucket, aws_region):
    """Create ephemeral test user, yield credentials, verify and cleanup."""
    cognito = boto3.client("cognito-idp", region_name=aws_region)
    dynamodb = boto3.client("dynamodb", region_name=aws_region)
    s3 = boto3.client("s3", region_name=aws_region)

    # Create user
    username, password = create_test_user(cognito, cognito_user_pool_id)
    print(f"\nCreated test user: {username}")

    try:
        # Authenticate
        token = authenticate_user(cognito, cognito_user_pool_id, cognito_client_id,
                                  username, password)

        # Trigger META# creation via discovery request
        session_url = f"https://{jmap_host}/.well-known/jmap"
        resp = requests.get(session_url, headers={"Authorization": f"Bearer {token}"}, timeout=30)
        resp.raise_for_status()
        session_data = resp.json()

        # Extract account_id from session
        accounts = session_data.get("accounts", {})
        assert accounts, "No accounts in session response"
        account_id = list(accounts.keys())[0]

        # Get api_url for mailbox operations
        api_url = session_data.get("apiUrl")

        # Wait for special mailboxes to be created (async via SQS)
        from helpers import get_all_mailboxes, verify_special_mailboxes

        max_wait = 30
        interval = 2
        start = time.time()
        mailboxes = []

        while time.time() - start < max_wait:
            mailboxes = get_all_mailboxes(api_url, token, account_id)
            if len(mailboxes) >= 6:
                try:
                    verify_special_mailboxes(mailboxes)
                    print(f"Verified {len(mailboxes)} special mailboxes")
                    break
                except AssertionError:
                    pass
            time.sleep(interval)
        else:
            pytest.fail(f"Special mailboxes not created within {max_wait}s. Found: {[m.get('role') for m in mailboxes]}")

        # Store mailbox IDs for cleanup
        mailbox_ids = [m["id"] for m in mailboxes]

        yield TestAccount(username=username, password=password, token=token, account_id=account_id)

        # Cleanup mailboxes created by jmap-service-email
        from helpers import destroy_all_mailboxes
        if mailbox_ids:
            destroy_all_mailboxes(api_url, token, account_id, mailbox_ids)
            print(f"Destroyed {len(mailbox_ids)} mailboxes")

        # Verify cleanliness - this is a test assertion, not cleanup
        # Retry with backoff to allow async cleanup (DynamoDB Streams) to complete
        max_attempts = 6
        wait_seconds = 5
        errors = []

        for attempt in range(max_attempts):
            errors = []

            # Check core table
            if dynamodb_table:
                orphans = verify_dynamodb_clean(dynamodb, dynamodb_table, account_id)
                if orphans:
                    errors.append(f"Orphaned records in {dynamodb_table}: {orphans}")

            # Check email table
            if dynamodb_email_table:
                orphans = verify_dynamodb_clean(dynamodb, dynamodb_email_table, account_id)
                if orphans:
                    errors.append(f"Orphaned records in {dynamodb_email_table}: {orphans}")

            # Check S3
            if blob_bucket:
                orphans = verify_s3_clean(s3, blob_bucket, account_id)
                if orphans:
                    errors.append(f"Orphaned S3 objects in {blob_bucket}: {orphans}")

            if not errors:
                break

            if attempt < max_attempts - 1:
                print(f"Waiting {wait_seconds}s for async cleanup (attempt {attempt + 1}/{max_attempts})...")
                time.sleep(wait_seconds)

        if errors:
            pytest.fail("Test cleanup verification failed:\n" + "\n".join(errors))

        # Cleanup infrastructure (only after verification passes)
        if dynamodb_table:
            delete_infrastructure_records(dynamodb, dynamodb_table, account_id)
        if dynamodb_email_table:
            delete_infrastructure_records(dynamodb, dynamodb_email_table, account_id)

    finally:
        # Always delete the Cognito user
        delete_test_user(cognito, cognito_user_pool_id, username)
        print(f"Deleted test user: {username}")


@pytest.fixture(scope="session")
def token(test_account):
    """JWT token from ephemeral test account."""
    return test_account.token


@pytest.fixture(scope="session")
def account_id(test_account):
    """Account ID from ephemeral test account."""
    return test_account.account_id


@pytest.fixture(scope="session")
def jmap_client(jmap_host, token):
    """Create a jmapc Client connected to the JMAP server."""
    client = jmapc.Client.create_with_api_token(
        host=jmap_host,
        api_token=token,
    )
    return client


@pytest.fixture(scope="session")
def api_url(jmap_client):
    """Get the JMAP API URL from the session."""
    return jmap_client.jmap_session.api_url


@pytest.fixture(scope="session")
def upload_url(jmap_client):
    """Get the JMAP upload URL from the session."""
    return jmap_client.jmap_session.upload_url
