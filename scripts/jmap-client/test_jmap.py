"""
JMAP Protocol Compliance Tests using jmapc

Uses the independent jmapc Python JMAP client to validate that our server
implementation is compliant with RFC 8620 (JMAP Core).
"""

import time

import boto3
import pytest
import requests
from jmapc import Comparator
from jmapc.methods import CoreEcho, EmailQuery

from helpers import make_jmap_request


# ---------------------------------------------------------------------------
# Module-level helpers
# ---------------------------------------------------------------------------

def get_s3_tags(bucket: str, account_id: str, blob_id: str, region: str) -> dict[str, str]:
    """Get S3 object tags for a blob."""
    s3_client = boto3.client("s3", region_name=region)
    key = f"{account_id}/{blob_id}"
    response = s3_client.get_object_tagging(Bucket=bucket, Key=key)
    return {tag["Key"]: tag["Value"] for tag in response["TagSet"]}


def get_dynamodb_blob(table: str, account_id: str, blob_id: str, region: str) -> dict | None:
    """Get DynamoDB blob record."""
    dynamodb = boto3.resource("dynamodb", region_name=region)
    tbl = dynamodb.Table(table)
    response = tbl.get_item(Key={"pk": f"ACCOUNT#{account_id}", "sk": f"BLOB#{blob_id}"})
    return response.get("Item")


def delete_blob(jmap_client, token: str, account_id: str, blob_id: str) -> requests.Response:
    """Send DELETE /delete/{accountId}/{blobId} with bearer token."""
    download_url = jmap_client.jmap_session.download_url
    delete_url_template = download_url.replace("/download/", "/delete/")
    url = delete_url_template.replace("{accountId}", account_id).replace("{blobId}", blob_id)
    return requests.delete(
        url,
        headers={"Authorization": f"Bearer {token}"},
        timeout=30,
    )


def _upload_blob(upload_url: str, token: str, account_id: str, content: bytes,
                 content_type: str = "application/octet-stream",
                 extra_headers: dict | None = None) -> requests.Response:
    """Upload a blob and return the raw response."""
    upload_endpoint = upload_url.replace("{accountId}", account_id)
    headers = {
        "Authorization": f"Bearer {token}",
        "Content-Type": content_type,
    }
    if extra_headers:
        headers.update(extra_headers)
    return requests.post(upload_endpoint, headers=headers, data=content, timeout=30)


# ---------------------------------------------------------------------------
# Session Discovery
# ---------------------------------------------------------------------------

def test_client_connection(jmap_client):
    """Session Discovery via jmapc Client."""
    session = jmap_client.jmap_session
    assert session.api_url, "api_url is None or empty"

    assert session.capabilities and session.capabilities.core, \
        "capabilities.core is None"

    account_id = jmap_client.account_id
    assert account_id, "No account ID found"

    assert session.primary_accounts, "primary_accounts is None"

    assert session.state, "state is None or empty"


# ---------------------------------------------------------------------------
# Core/echo
# ---------------------------------------------------------------------------

def test_core_echo(jmap_client):
    """Core/echo echoes back arguments unchanged (RFC 8620 Section 3.5)."""
    test_data = {"hello": True, "count": 42, "nested": {"key": "value", "array": [1, 2, 3]}}

    response = jmap_client.request(CoreEcho(data=test_data))
    response_data = getattr(response, "data", None)
    assert response_data == test_data, \
        f"Expected: {test_data}\nGot: {response_data}"


# ---------------------------------------------------------------------------
# Email/query (basic)
# ---------------------------------------------------------------------------

def test_email_query(jmap_client):
    """Basic Email/query request via jmapc."""
    response = jmap_client.request(EmailQuery())

    # Either a successful query or a JMAP error is acceptable at this level
    error_type = getattr(response, "type", None)
    error_desc = getattr(response, "description", None)
    if error_type and error_desc:
        # JMAP error response is acceptable
        return

    ids = getattr(response, "ids", None)
    assert ids is not None, "No ids field in response"
    assert hasattr(response, "query_state"), "No query_state field"


# ---------------------------------------------------------------------------
# Blob tests (with cleanup)
# ---------------------------------------------------------------------------

class TestBlobs:
    """Blob upload/download/delete tests with automatic cleanup."""

    @pytest.fixture(autouse=True)
    def _track_blobs(self, jmap_client, token, account_id, upload_url,
                     blob_bucket, dynamodb_table, aws_region):
        """Track uploaded blob IDs and clean up after all blob tests."""
        self._blob_ids: list[str] = []
        self._jmap_client = jmap_client
        self._token = token
        self._account_id = account_id
        self._upload_url = upload_url
        self._blob_bucket = blob_bucket
        self._dynamodb_table = dynamodb_table
        self._aws_region = aws_region
        yield
        # Cleanup runs after each test; we use class-level list via _record_blob
        # Cleanup is handled by the class-scoped fixture below instead.

    @pytest.fixture(scope="class", autouse=True)
    def _cleanup_blobs(self, jmap_client, token, account_id,
                       blob_bucket, dynamodb_table, aws_region):
        """Class-scoped fixture that cleans up all tracked blobs after the class."""
        blob_ids: list[str] = []
        # Stash on the class so individual tests can append
        TestBlobs._class_blob_ids = blob_ids
        yield
        # Cleanup
        if not blob_ids:
            return

        successfully_deleted = []
        for blob_id in blob_ids:
            resp = delete_blob(jmap_client, token, account_id, blob_id)
            assert resp.status_code == 204, \
                f"Blob delete {blob_id} got HTTP {resp.status_code}: {resp.text}"
            successfully_deleted.append(blob_id)

        if not successfully_deleted:
            return

        can_check_s3 = bool(blob_bucket)
        can_check_ddb = bool(dynamodb_table)
        if not can_check_s3 and not can_check_ddb:
            return

        s3_client = boto3.client("s3", region_name=aws_region) if can_check_s3 else None

        max_wait = 30
        poll_interval = 2
        elapsed = 0
        pending_s3 = set(successfully_deleted) if can_check_s3 else set()
        pending_ddb = set(successfully_deleted) if can_check_ddb else set()

        while (pending_s3 or pending_ddb) and elapsed < max_wait:
            time.sleep(poll_interval)
            elapsed += poll_interval

            for bid in list(pending_s3):
                key = f"{account_id}/{bid}"
                try:
                    s3_client.head_object(Bucket=blob_bucket, Key=key)
                except s3_client.exceptions.ClientError as e:
                    if e.response["Error"]["Code"] == "404":
                        pending_s3.discard(bid)

            for bid in list(pending_ddb):
                item = get_dynamodb_blob(dynamodb_table, account_id, bid, aws_region)
                if item is None:
                    pending_ddb.discard(bid)

        for bid in successfully_deleted:
            if can_check_s3:
                assert bid not in pending_s3, \
                    f"S3 object {bid} still exists after {max_wait}s"
            if can_check_ddb:
                assert bid not in pending_ddb, \
                    f"DynamoDB record {bid} still exists after {max_wait}s"

    def _record_blob(self, blob_id: str | None):
        if blob_id:
            TestBlobs._class_blob_ids.append(blob_id)

    def test_blob_upload(self, upload_url, token, account_id):
        """Blob Upload (RFC 8620 Section 6.1)."""
        test_content = b"Test blob content for RFC 8620 compliance"
        response = _upload_blob(upload_url, token, account_id, test_content)

        assert response.status_code == 201, \
            f"Got HTTP {response.status_code}: {response.text}"

        response_data = response.json()

        required_fields = ["accountId", "blobId", "type", "size"]
        for field in required_fields:
            assert field in response_data, f"Response missing '{field}'"

        assert response_data["accountId"] == account_id, \
            f"Expected '{account_id}', got '{response_data.get('accountId')}'"
        assert response_data["size"] == len(test_content), \
            f"Expected {len(test_content)}, got {response_data.get('size')}"
        assert response_data["type"] == "application/octet-stream", \
            f"Expected 'application/octet-stream', got '{response_data.get('type')}'"

        self._record_blob(response_data.get("blobId"))

    def test_blob_download(self, jmap_client, upload_url, token, account_id):
        """Blob Download - redirect to CloudFront signed URL."""
        session = jmap_client.jmap_session
        assert session.download_url, "download_url not in session"

        test_content = b"Test blob content for download verification - unique content 12345"
        upload_response = _upload_blob(upload_url, token, account_id, test_content)
        assert upload_response.status_code == 201, \
            f"Upload got HTTP {upload_response.status_code}: {upload_response.text}"

        upload_data = upload_response.json()
        blob_id = upload_data.get("blobId")
        assert blob_id, "No blobId in upload response"
        self._record_blob(blob_id)

        download_url = session.download_url.replace("{accountId}", account_id).replace(
            "{blobId}", blob_id
        )

        download_response = requests.get(
            download_url,
            headers={"Authorization": f"Bearer {token}"},
            allow_redirects=False,
            timeout=30,
        )
        assert download_response.status_code == 302, \
            f"Got HTTP {download_response.status_code}: {download_response.text}"

        location = download_response.headers.get("Location")
        assert location, "No Location header"

        assert "Signature=" in location or "Key-Pair-Id=" in location, \
            "URL doesn't contain signature parameters"

        content_response = requests.get(location, timeout=30)
        assert content_response.status_code == 200, \
            f"CloudFront got HTTP {content_response.status_code}: {content_response.text}"
        assert content_response.content == test_content, \
            f"Content mismatch: expected {len(test_content)} bytes, got {len(content_response.content)} bytes"

    def test_blob_download_byte_range(self, jmap_client, upload_url, token, account_id):
        """Blob Download Byte Range (composite blobId)."""
        session = jmap_client.jmap_session
        assert session.download_url, "download_url not in session"

        test_content = b"0123456789" * 10
        upload_response = _upload_blob(upload_url, token, account_id, test_content)
        assert upload_response.status_code == 201, \
            f"Upload got HTTP {upload_response.status_code}: {upload_response.text}"

        upload_data = upload_response.json()
        blob_id = upload_data.get("blobId")
        assert blob_id, "No blobId in upload response"
        self._record_blob(blob_id)

        start_byte = 10
        end_byte = 29
        composite_blob_id = f"{blob_id},{start_byte},{end_byte}"
        download_url = session.download_url.replace("{accountId}", account_id).replace(
            "{blobId}", composite_blob_id
        )

        download_response = requests.get(
            download_url,
            headers={"Authorization": f"Bearer {token}"},
            allow_redirects=False,
            timeout=30,
        )
        assert download_response.status_code == 302, \
            f"Got HTTP {download_response.status_code}: {download_response.text}"

        location = download_response.headers.get("Location")
        assert location, "No Location header"
        assert composite_blob_id in location or f"{start_byte},{end_byte}" in location, \
            f"Expected range in URL, got: {location[:100]}..."

        content_response = requests.get(location, timeout=30)
        assert content_response.status_code in (200, 206), \
            f"Got HTTP {content_response.status_code}: {content_response.text[:200]}"

        expected_content = test_content[start_byte:end_byte + 1]
        assert content_response.content == expected_content, \
            f"Expected {len(expected_content)} bytes, got {len(content_response.content)} bytes"

    def test_blob_download_cross_account(self, jmap_client, upload_url, token, account_id):
        """Blob Download Cross-Account Access should be forbidden."""
        session = jmap_client.jmap_session
        assert session.download_url, "download_url not in session"

        test_content = b"Test blob for cross-account check"
        upload_response = _upload_blob(upload_url, token, account_id, test_content)
        assert upload_response.status_code == 201, \
            f"Upload got HTTP {upload_response.status_code}"

        upload_data = upload_response.json()
        blob_id = upload_data.get("blobId")
        assert blob_id, "No blobId"
        self._record_blob(blob_id)

        fake_account_id = "fake-account-id-12345"
        download_url = session.download_url.replace(
            "{accountId}", fake_account_id
        ).replace("{blobId}", blob_id)

        response = requests.get(
            download_url,
            headers={"Authorization": f"Bearer {token}"},
            allow_redirects=False,
            timeout=30,
        )
        assert response.status_code == 403, \
            f"Got HTTP {response.status_code}: {response.text}"

    def test_blob_upload_with_x_parent(self, upload_url, token, account_id,
                                       blob_bucket, dynamodb_table, aws_region):
        """Blob Upload with Valid X-Parent Header."""
        test_content = b"Test blob content with X-Parent header for RFC 8620 compliance"
        x_parent_value = "my-folder/subfolder"

        response = _upload_blob(
            upload_url, token, account_id, test_content,
            extra_headers={"X-Parent": x_parent_value},
        )
        assert response.status_code == 201, \
            f"Got HTTP {response.status_code}: {response.text}"

        response_data = response.json()
        blob_id = response_data.get("blobId")
        assert blob_id, "No blobId in response"
        self._record_blob(blob_id)

        # Verify S3 tag
        assert blob_bucket, "BLOB_BUCKET not configured"
        tags = get_s3_tags(blob_bucket, account_id, blob_id, aws_region)
        assert tags.get("Parent") == x_parent_value, \
            f"Expected Parent='{x_parent_value}', got tags: {tags}"

        # Verify DynamoDB record
        assert dynamodb_table, "DYNAMODB_TABLE not configured"
        item = get_dynamodb_blob(dynamodb_table, account_id, blob_id, aws_region)
        assert item is not None, "Blob record not found"
        assert item.get("parent") == x_parent_value, \
            f"Expected parent='{x_parent_value}', got: {item.get('parent')}"

    def test_blob_upload_invalid_x_parent(self, upload_url, token, account_id):
        """Blob Upload with Invalid X-Parent Header returns 400."""
        test_content = b"Test blob with invalid X-Parent"
        invalid_x_parent = "<script>alert(1)</script>"

        response = _upload_blob(
            upload_url, token, account_id, test_content,
            extra_headers={"X-Parent": invalid_x_parent},
        )
        assert response.status_code == 400, \
            f"Got HTTP {response.status_code}: {response.text}"

        response_data = response.json()
        assert response_data.get("type") == "invalidArguments", \
            f"Got type: {response_data.get('type')}"

    def test_blob_upload_x_parent_too_long(self, upload_url, token, account_id):
        """Blob Upload with X-Parent Exceeding Max Length returns 400."""
        test_content = b"Test blob with too long X-Parent"
        too_long_x_parent = "a" * 129

        response = _upload_blob(
            upload_url, token, account_id, test_content,
            extra_headers={"X-Parent": too_long_x_parent},
        )
        assert response.status_code == 400, \
            f"Got HTTP {response.status_code}: {response.text}"

        response_data = response.json()
        assert response_data.get("type") == "invalidArguments", \
            f"Got type: {response_data.get('type')}"

    def test_blob_upload_without_x_parent(self, upload_url, token, account_id,
                                          blob_bucket, dynamodb_table, aws_region):
        """Blob Upload without X-Parent Header - verify no Parent in storage."""
        test_content = b"Test blob content WITHOUT X-Parent header for storage verification"

        response = _upload_blob(upload_url, token, account_id, test_content)
        assert response.status_code == 201, \
            f"Got HTTP {response.status_code}: {response.text}"

        response_data = response.json()
        blob_id = response_data.get("blobId")
        assert blob_id, "No blobId in response"
        self._record_blob(blob_id)

        # Verify S3 tag is NOT present
        assert blob_bucket, "BLOB_BUCKET not configured"
        tags = get_s3_tags(blob_bucket, account_id, blob_id, aws_region)
        assert "Parent" not in tags, \
            f"Unexpected Parent tag found: {tags.get('Parent')}"

        # Verify DynamoDB record does NOT have parent field
        assert dynamodb_table, "DYNAMODB_TABLE not configured"
        item = get_dynamodb_blob(dynamodb_table, account_id, blob_id, aws_region)
        assert item is not None, "Blob record not found"
        assert "parent" not in item, \
            f"Unexpected parent field found: {item.get('parent')}"


# ---------------------------------------------------------------------------
# Email/query E2E tests (RFC 8620 Section 5.5, RFC 8621 Section 4.4)
# ---------------------------------------------------------------------------

@pytest.fixture(scope="module")
def query_test_data(jmap_client, api_url, upload_url, token, account_id):
    """Set up test data for Email/query tests: mailbox with 3 emails at staggered times."""
    from helpers import create_test_mailbox, destroy_emails_and_verify_cleanup
    import time as _time

    mailbox_id = create_test_mailbox(api_url, token, account_id, prefix="QueryTest")
    assert mailbox_id, "Failed to create test mailbox"

    email_ids = []
    for i in range(3):
        from helpers import import_test_email
        email_id = import_test_email(api_url, upload_url, token, account_id, mailbox_id)
        assert email_id, f"Failed to import test email {i}"
        email_ids.append(email_id)
        if i < 2:
            _time.sleep(1)  # stagger receivedAt

    data = {
        "account_id": account_id,
        "mailbox_id": mailbox_id,
        "email_ids": email_ids,
    }

    yield data

    # Cleanup
    destroy_emails_and_verify_cleanup(api_url, token, account_id, email_ids)


def test_email_query_response_structure(jmap_client, query_test_data):
    """Email/query Response Structure (RFC 8620 Section 5.5)."""
    response = jmap_client.request(EmailQuery())

    error_type = getattr(response, "type", None)
    assert not error_type, \
        f"JMAP error: {error_type}: {getattr(response, 'description', '')}"

    resp_account_id = getattr(response, "account_id", None)
    assert resp_account_id == query_test_data["account_id"], \
        f"Expected {query_test_data['account_id']}, got {resp_account_id}"

    query_state = getattr(response, "query_state", None)
    assert query_state is not None, "queryState is None"

    can_calculate = getattr(response, "can_calculate_changes", None)
    assert can_calculate is not None, "canCalculateChanges is None"

    position = getattr(response, "position", None)
    assert position is not None, "position is None"

    ids = getattr(response, "ids", None)
    assert ids is not None and isinstance(ids, list), f"ids is {type(ids)}"


def test_email_query_empty_filter(jmap_client, query_test_data):
    """Empty Filter Returns All Emails (RFC 8621 Section 4.4.1)."""
    response = jmap_client.request(EmailQuery())

    error_type = getattr(response, "type", None)
    assert not error_type, \
        f"JMAP error: {error_type}: {getattr(response, 'description', '')}"

    ids = getattr(response, "ids", None) or []
    test_email_ids = set(query_test_data["email_ids"])
    result_ids = set(ids)

    missing = test_email_ids - result_ids
    assert not missing, f"Missing {len(missing)} emails: {missing}"


def test_email_query_sort_received_at_desc(jmap_client, query_test_data):
    """receivedAt Sorting (RFC 8621 Section 4.4.2)."""
    response = jmap_client.request(
        EmailQuery(sort=[Comparator(property="receivedAt", is_ascending=False)])
    )

    error_type = getattr(response, "type", None)
    assert not error_type, \
        f"JMAP error: {error_type}: {getattr(response, 'description', '')}"

    ids = getattr(response, "ids", None) or []
    test_ids = query_test_data["email_ids"]
    result_test_ids = [id for id in ids if id in test_ids]

    assert len(result_test_ids) == len(test_ids), \
        f"Expected {len(test_ids)} test emails, found {len(result_test_ids)}"

    expected_order = list(reversed(test_ids))
    assert result_test_ids == expected_order, \
        f"Expected {expected_order}, got {result_test_ids}"


def test_email_query_pagination(jmap_client, query_test_data):
    """Pagination with position/limit (RFC 8620 Section 5.5)."""
    response1 = jmap_client.request(EmailQuery(position=0, limit=1))

    error_type = getattr(response1, "type", None)
    assert not error_type, f"JMAP error: {error_type}"

    ids1 = getattr(response1, "ids", None) or []
    pos1 = getattr(response1, "position", None)

    assert len(ids1) == 1, f"Expected 1, got {len(ids1)}"
    assert pos1 == 0, f"Expected position 0, got {pos1}"

    response2 = jmap_client.request(EmailQuery(position=1, limit=1))
    ids2 = getattr(response2, "ids", None) or []
    pos2 = getattr(response2, "position", None)

    assert len(ids2) == 1, f"Expected 1, got {len(ids2)}"
    assert pos2 == 1, f"Expected position 1, got {pos2}"

    assert ids1[0] != ids2[0], f"Both pages returned {ids1[0]}"


def test_email_query_calculate_total(jmap_client, query_test_data):
    """calculateTotal (RFC 8620 Section 5.5)."""
    response = jmap_client.request(EmailQuery(calculate_total=True))

    error_type = getattr(response, "type", None)
    assert not error_type, f"JMAP error: {error_type}"

    total = getattr(response, "total", None)
    if total is not None:
        assert total >= len(query_test_data["email_ids"]), \
            f"total={total}, expected >= {len(query_test_data['email_ids'])}"

    # With calculateTotal=False
    response2 = jmap_client.request(EmailQuery(calculate_total=False))
    # RFC says total MAY be omitted - either None or present is acceptable
    # No assertion needed here; both outcomes are valid.


def test_email_query_position_beyond_results(jmap_client, query_test_data):
    """Empty Results for Position Beyond Results (RFC 8620 Section 5.5)."""
    response = jmap_client.request(EmailQuery(position=9999))

    error_type = getattr(response, "type", None)
    assert not error_type, f"Position beyond results should not be an error, got: {error_type}"

    ids = getattr(response, "ids", None)
    assert ids is not None and isinstance(ids, list) and len(ids) == 0, \
        f"ids should be empty array, got: {ids}"


def test_email_query_stable_sort(jmap_client, query_test_data):
    """Stable Sort Order (RFC 8620 Section 5.5)."""
    response1 = jmap_client.request(EmailQuery())
    error_type = getattr(response1, "type", None)
    assert not error_type, f"JMAP error (call 1): {error_type}"
    ids1 = getattr(response1, "ids", None) or []

    response2 = jmap_client.request(EmailQuery())
    error_type2 = getattr(response2, "type", None)
    assert not error_type2, f"JMAP error (call 2): {error_type2}"
    ids2 = getattr(response2, "ids", None) or []

    assert ids1 == ids2, \
        f"Call 1: {ids1[:5]}..., Call 2: {ids2[:5]}..."
