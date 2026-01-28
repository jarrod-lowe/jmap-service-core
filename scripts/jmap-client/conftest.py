"""pytest conftest for JMAP e2e tests."""

import os

import pytest
import jmapc


@pytest.fixture(scope="session")
def jmap_host():
    """JMAP host URL from environment."""
    host = os.environ.get("JMAP_HOST")
    assert host, "JMAP_HOST environment variable not set"
    return host


@pytest.fixture(scope="session")
def token():
    """JWT token from environment."""
    t = os.environ.get("JMAP_API_TOKEN")
    assert t, "JMAP_API_TOKEN environment variable not set"
    return t


@pytest.fixture(scope="session")
def blob_bucket():
    """S3 blob bucket name from environment."""
    return os.environ.get("BLOB_BUCKET", "")


@pytest.fixture(scope="session")
def dynamodb_table():
    """DynamoDB table name from environment."""
    return os.environ.get("DYNAMODB_TABLE", "")


@pytest.fixture(scope="session")
def aws_region():
    """AWS region from environment."""
    return os.environ.get("AWS_REGION", "ap-southeast-2")


@pytest.fixture(scope="session")
def jmap_client(jmap_host, token):
    """Create a jmapc Client connected to the JMAP server."""
    client = jmapc.Client.create_with_api_token(
        host=jmap_host,
        api_token=token,
    )
    return client


@pytest.fixture(scope="session")
def account_id(jmap_client):
    """Get the primary account ID from the JMAP session."""
    return jmap_client.account_id


@pytest.fixture(scope="session")
def api_url(jmap_client):
    """Get the JMAP API URL from the session."""
    return jmap_client.jmap_session.api_url


@pytest.fixture(scope="session")
def upload_url(jmap_client):
    """Get the JMAP upload URL from the session."""
    return jmap_client.jmap_session.upload_url
