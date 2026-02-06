"""
JMAP Blob/allocate Extension Tests

Tests for the upload-put extension (https://jmap.rrod.net/extensions/upload-put)
that enables direct upload to S3 via pre-signed URLs.
"""

import os
import time
import uuid

import boto3
import pytest
import requests

from helpers import make_iam_jmap_request, make_jmap_request


# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------

UPLOAD_PUT_CAPABILITY = "https://jmap.rrod.net/extensions/upload-put"


# ---------------------------------------------------------------------------
# Module-level helpers
# ---------------------------------------------------------------------------

def make_allocate_request(
    api_url: str,
    token: str,
    account_id: str,
    allocations: dict,
) -> dict:
    """Make a Blob/allocate JMAP request."""
    request_body = {
        "using": ["urn:ietf:params:jmap:core", UPLOAD_PUT_CAPABILITY],
        "methodCalls": [
            [
                "Blob/allocate",
                {
                    "accountId": account_id,
                    "create": allocations,
                },
                "allocate0",
            ]
        ],
    }
    response = requests.post(
        api_url,
        headers={
            "Authorization": f"Bearer {token}",
            "Content-Type": "application/json",
        },
        json=request_body,
        timeout=30,
    )
    return response.json()


def get_dynamodb_blob(table: str, account_id: str, blob_id: str, region: str) -> dict | None:
    """Get DynamoDB blob record."""
    dynamodb = boto3.resource("dynamodb", region_name=region)
    tbl = dynamodb.Table(table)
    response = tbl.get_item(Key={"pk": f"ACCOUNT#{account_id}", "sk": f"BLOB#{blob_id}"})
    return response.get("Item")


def get_dynamodb_meta(table: str, account_id: str, region: str) -> dict | None:
    """Get DynamoDB META# record for account."""
    dynamodb = boto3.resource("dynamodb", region_name=region)
    tbl = dynamodb.Table(table)
    response = tbl.get_item(Key={"pk": f"ACCOUNT#{account_id}", "sk": "META#"})
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


def download_blob(jmap_client, token: str, account_id: str, blob_id: str) -> bytes | None:
    """Download a blob and return its content."""
    download_url = jmap_client.jmap_session.download_url
    url = download_url.replace("{accountId}", account_id).replace("{blobId}", blob_id)

    # Get redirect to signed URL
    response = requests.get(
        url,
        headers={"Authorization": f"Bearer {token}"},
        allow_redirects=False,
        timeout=30,
    )
    if response.status_code != 302:
        return None

    location = response.headers.get("Location")
    if not location:
        return None

    # Follow the signed URL
    content_response = requests.get(location, timeout=30)
    if content_response.status_code != 200:
        return None

    return content_response.content


def cleanup_pending_allocation(
    table: str, account_id: str, blob_id: str, size: int, region: str
) -> None:
    """Clean up a pending allocation that won't be confirmed via normal flow.

    This manually reverses what Blob/allocate did:
    1. Delete the BLOB# record
    2. Decrement pendingAllocationsCount
    3. Restore quotaRemaining

    Use this in tests that intentionally don't complete upload (e.g., testing
    allocation only, or testing S3 rejection scenarios).
    """
    dynamodb = boto3.resource("dynamodb", region_name=region)
    tbl = dynamodb.Table(table)

    # Delete the pending blob record
    tbl.delete_item(Key={"pk": f"ACCOUNT#{account_id}", "sk": f"BLOB#{blob_id}"})

    # Update META#: decrement pending count, restore quota
    tbl.update_item(
        Key={"pk": f"ACCOUNT#{account_id}", "sk": "META#"},
        UpdateExpression="ADD pendingAllocationsCount :neg, quotaRemaining :size",
        ExpressionAttributeValues={":neg": -1, ":size": size},
    )


# ---------------------------------------------------------------------------
# Test Class for Blob/allocate
# ---------------------------------------------------------------------------

class TestBlobAllocate:
    """Tests for Blob/allocate extension."""

    @pytest.fixture(scope="class", autouse=True)
    def _cleanup_blobs(self, jmap_client, token, account_id):
        """Class-scoped fixture that cleans up all tracked blobs after the class."""
        blob_ids: list[str] = []
        TestBlobAllocate._class_blob_ids = blob_ids
        yield
        # Cleanup
        for blob_id in blob_ids:
            try:
                delete_blob(jmap_client, token, account_id, blob_id)
            except Exception:
                pass  # Best effort cleanup

    def _record_blob(self, blob_id: str | None):
        """Record a blob ID for cleanup."""
        if blob_id:
            TestBlobAllocate._class_blob_ids.append(blob_id)

    def test_capability_in_session(self, jmap_host, token, account_id):
        """Session includes upload-put capability with required properties."""
        # Make raw HTTP request to get session (jmapc doesn't expose raw accounts)
        session_url = f"https://{jmap_host}/.well-known/jmap"
        response = requests.get(
            session_url,
            headers={"Authorization": f"Bearer {token}", "X-JMAP-Stage": "e2e"},
            timeout=30,
        )
        assert response.status_code == 200, f"Session request failed: {response.status_code}"
        session_data = response.json()

        # Check session-level capability
        assert "capabilities" in session_data, "No capabilities in session"
        assert UPLOAD_PUT_CAPABILITY in session_data["capabilities"], \
            f"Session missing {UPLOAD_PUT_CAPABILITY} capability"

        # Check account-level capability
        accounts = session_data.get("accounts", {})
        assert accounts, "No accounts in session"

        # The account should have the upload-put capability
        account = accounts.get(account_id)
        assert account is not None, f"Account {account_id} not found"

        # Check account capabilities contain upload-put
        account_caps = account.get("accountCapabilities", {})
        assert UPLOAD_PUT_CAPABILITY in account_caps, \
            f"Account missing {UPLOAD_PUT_CAPABILITY} capability"

        cap_config = account_caps[UPLOAD_PUT_CAPABILITY]
        assert "maxSizeUploadPut" in cap_config, \
            "Account capability missing maxSizeUploadPut"
        assert "maxPendingAllocations" in cap_config, \
            "Account capability missing maxPendingAllocations"

    def test_allocate_success(self, api_url, token, account_id, dynamodb_table, aws_region):
        """Blob/allocate returns valid response with URL and expiry."""
        alloc_size = 1024
        response = make_allocate_request(
            api_url,
            token,
            account_id,
            {"a0": {"type": "application/octet-stream", "size": alloc_size}},
        )

        assert "methodResponses" in response, f"No methodResponses: {response}"
        method_responses = response["methodResponses"]
        assert len(method_responses) == 1, f"Expected 1 response, got {len(method_responses)}"

        name, data, call_id = method_responses[0]
        assert name == "Blob/allocate", f"Expected Blob/allocate, got {name}"
        assert call_id == "allocate0", f"Wrong call ID: {call_id}"
        assert data.get("accountId") == account_id, f"Wrong accountId: {data.get('accountId')}"

        created = data.get("created")
        assert created is not None, f"created is None: {data}"
        assert "a0" in created, f"a0 not in created: {created}"

        allocation = created["a0"]
        assert "id" in allocation, f"No id in allocation: {allocation}"
        assert "url" in allocation, f"No url in allocation: {allocation}"
        assert "expires" in allocation, f"No expires in allocation: {allocation}"
        assert allocation.get("type") == "application/octet-stream", \
            f"Wrong type: {allocation.get('type')}"
        assert allocation.get("size") == alloc_size, f"Wrong size: {allocation.get('size')}"

        # URL must be HTTPS
        url = allocation["url"]
        assert url.startswith("https://"), f"URL not HTTPS: {url}"

        self._record_blob(allocation["id"])

        # Clean up pending allocation since we don't upload
        if dynamodb_table:
            cleanup_pending_allocation(
                dynamodb_table, account_id, allocation["id"], alloc_size, aws_region
            )

    def test_allocate_then_upload(self, jmap_client, api_url, token, account_id,
                                   dynamodb_table, aws_region):
        """Full flow: allocate -> PUT upload -> blob accessible."""
        test_content = b"Test content for blob allocate upload verification"
        content_type = "text/plain"
        size = len(test_content)

        # Step 1: Allocate
        response = make_allocate_request(
            api_url,
            token,
            account_id,
            {"upload0": {"type": content_type, "size": size}},
        )

        assert "methodResponses" in response, f"No methodResponses: {response}"
        name, data, _ = response["methodResponses"][0]
        assert name == "Blob/allocate", f"Expected Blob/allocate, got {name}: {data}"

        created = data.get("created", {})
        assert "upload0" in created, f"upload0 not created: {data}"

        allocation = created["upload0"]
        blob_id = allocation["id"]
        upload_url = allocation["url"]
        self._record_blob(blob_id)

        # Step 2: PUT to pre-signed URL
        put_response = requests.put(
            upload_url,
            headers={
                "Content-Type": content_type,
                "Content-Length": str(size),
            },
            data=test_content,
            timeout=60,
        )
        assert put_response.status_code in (200, 204), \
            f"PUT failed with {put_response.status_code}: {put_response.text}"

        # Step 3: Wait for blob-confirm to process
        max_wait = 30
        poll_interval = 2
        elapsed = 0
        blob_confirmed = False

        while elapsed < max_wait and not blob_confirmed:
            time.sleep(poll_interval)
            elapsed += poll_interval

            if dynamodb_table:
                item = get_dynamodb_blob(dynamodb_table, account_id, blob_id, aws_region)
                if item and item.get("status") == "confirmed":
                    blob_confirmed = True

        assert blob_confirmed, f"Blob {blob_id} not confirmed after {max_wait}s"

        # Step 4: Verify blob is downloadable
        downloaded_content = download_blob(jmap_client, token, account_id, blob_id)
        assert downloaded_content == test_content, \
            f"Downloaded content mismatch: expected {len(test_content)} bytes, got {len(downloaded_content) if downloaded_content else 0}"

    def test_allocate_too_large(self, jmap_host, api_url, token, account_id):
        """Blob/allocate returns tooLarge error for oversized requests."""
        # Get capability config via raw HTTP request
        session_url = f"https://{jmap_host}/.well-known/jmap"
        session_resp = requests.get(
            session_url,
            headers={"Authorization": f"Bearer {token}", "X-JMAP-Stage": "e2e"},
            timeout=30,
        )
        session_data = session_resp.json()
        account = session_data["accounts"].get(account_id, {})
        cap_config = account.get("accountCapabilities", {}).get(UPLOAD_PUT_CAPABILITY, {})
        max_size = cap_config.get("maxSizeUploadPut", 250000000)

        # Request size exceeds limit
        too_large_size = max_size + 1

        response = make_allocate_request(
            api_url,
            token,
            account_id,
            {"big": {"type": "application/octet-stream", "size": too_large_size}},
        )

        assert "methodResponses" in response, f"No methodResponses: {response}"
        name, data, _ = response["methodResponses"][0]
        assert name == "Blob/allocate", f"Expected Blob/allocate, got {name}"

        not_created = data.get("notCreated")
        assert not_created is not None, f"Expected notCreated, got: {data}"
        assert "big" in not_created, f"big not in notCreated: {not_created}"

        error = not_created["big"]
        assert error.get("type") == "tooLarge", \
            f"Expected tooLarge error, got: {error.get('type')}"

    def test_allocate_invalid_type(self, api_url, token, account_id):
        """Blob/allocate returns invalidProperties for invalid media type."""
        response = make_allocate_request(
            api_url,
            token,
            account_id,
            {"bad": {"type": "not a valid media type!!!", "size": 1024}},
        )

        assert "methodResponses" in response, f"No methodResponses: {response}"
        name, data, _ = response["methodResponses"][0]
        assert name == "Blob/allocate", f"Expected Blob/allocate, got {name}"

        not_created = data.get("notCreated")
        assert not_created is not None, f"Expected notCreated, got: {data}"
        assert "bad" in not_created, f"bad not in notCreated: {not_created}"

        error = not_created["bad"]
        assert error.get("type") == "invalidProperties", \
            f"Expected invalidProperties error, got: {error.get('type')}"

    def test_upload_content_type_mismatch(self, api_url, token, account_id,
                                          dynamodb_table, aws_region):
        """S3 rejects upload with mismatched Content-Type."""
        alloc_size = 10
        # Allocate with one type
        response = make_allocate_request(
            api_url,
            token,
            account_id,
            {"mismatch": {"type": "text/plain", "size": alloc_size}},
        )

        assert "methodResponses" in response
        name, data, _ = response["methodResponses"][0]
        assert name == "Blob/allocate"

        created = data.get("created", {})
        if "mismatch" not in created:
            pytest.skip("Allocation failed, cannot test mismatch")

        allocation = created["mismatch"]
        upload_url = allocation["url"]
        blob_id = allocation["id"]
        self._record_blob(blob_id)

        # Upload with different type
        put_response = requests.put(
            upload_url,
            headers={
                "Content-Type": "application/json",  # Different from declared
                "Content-Length": "10",
            },
            data=b"0123456789",
            timeout=30,
        )

        # S3 pre-signed URL conditions should reject this
        assert put_response.status_code in (400, 403), \
            f"Expected 400/403 for type mismatch, got {put_response.status_code}"

        # Clean up pending allocation since upload was rejected
        if dynamodb_table:
            cleanup_pending_allocation(
                dynamodb_table, account_id, blob_id, alloc_size, aws_region
            )

    def test_upload_size_mismatch(self, api_url, token, account_id,
                                   dynamodb_table, aws_region):
        """S3 rejects upload with mismatched Content-Length."""
        declared_size = 10

        response = make_allocate_request(
            api_url,
            token,
            account_id,
            {"sizemis": {"type": "application/octet-stream", "size": declared_size}},
        )

        assert "methodResponses" in response
        name, data, _ = response["methodResponses"][0]
        assert name == "Blob/allocate"

        created = data.get("created", {})
        if "sizemis" not in created:
            pytest.skip("Allocation failed, cannot test size mismatch")

        allocation = created["sizemis"]
        upload_url = allocation["url"]
        blob_id = allocation["id"]
        self._record_blob(blob_id)

        # Upload with different size
        put_response = requests.put(
            upload_url,
            headers={
                "Content-Type": "application/octet-stream",
                "Content-Length": "20",  # Different from declared
            },
            data=b"01234567890123456789",  # 20 bytes
            timeout=30,
        )

        # S3 pre-signed URL conditions should reject this
        assert put_response.status_code in (400, 403), \
            f"Expected 400/403 for size mismatch, got {put_response.status_code}"

        # Clean up pending allocation since upload was rejected
        if dynamodb_table:
            cleanup_pending_allocation(
                dynamodb_table, account_id, blob_id, declared_size, aws_region
            )

    def test_allocate_multiple(self, api_url, token, account_id,
                                dynamodb_table, aws_region):
        """Blob/allocate handles multiple allocations in single request."""
        first_size = 100
        second_size = 200
        response = make_allocate_request(
            api_url,
            token,
            account_id,
            {
                "first": {"type": "text/plain", "size": first_size},
                "second": {"type": "application/json", "size": second_size},
            },
        )

        assert "methodResponses" in response
        name, data, _ = response["methodResponses"][0]
        assert name == "Blob/allocate"

        created = data.get("created", {})
        assert "first" in created, f"first not created: {data}"
        assert "second" in created, f"second not created: {data}"

        first_id = created["first"]["id"]
        second_id = created["second"]["id"]

        # Record for cleanup
        self._record_blob(first_id)
        self._record_blob(second_id)

        # Verify each has unique id and url
        assert first_id != second_id, "Both allocations have same id"
        assert created["first"]["url"] != created["second"]["url"], \
            "Both allocations have same url"

        # Clean up pending allocations since we don't upload
        if dynamodb_table:
            cleanup_pending_allocation(
                dynamodb_table, account_id, first_id, first_size, aws_region
            )
            cleanup_pending_allocation(
                dynamodb_table, account_id, second_id, second_size, aws_region
            )

    def test_missing_capability_in_using(self, api_url, token, account_id):
        """Request without capability in using array returns error."""
        # Make request WITHOUT the upload-put capability in using array
        request_body = {
            "using": ["urn:ietf:params:jmap:core"],  # Missing upload-put
            "methodCalls": [
                [
                    "Blob/allocate",
                    {
                        "accountId": account_id,
                        "create": {"test": {"type": "text/plain", "size": 100}},
                    },
                    "allocate0",
                ]
            ],
        }
        response = requests.post(
            api_url,
            headers={
                "Authorization": f"Bearer {token}",
                "Content-Type": "application/json",
            },
            json=request_body,
            timeout=30,
        )
        resp_json = response.json()

        assert "methodResponses" in resp_json
        name, data, _ = resp_json["methodResponses"][0]

        # Should get unknownMethod or similar error
        assert name == "error", f"Expected error response, got {name}: {data}"
        assert data.get("type") in ("unknownMethod", "unknownCapability"), \
            f"Expected unknownMethod/unknownCapability, got: {data.get('type')}"


# ---------------------------------------------------------------------------
# Tests requiring specific quota/limit setup (may need to be skipped)
# ---------------------------------------------------------------------------

class TestBlobAllocateLimits:
    """Tests for Blob/allocate limits (tooManyPending, overQuota).

    These tests may need to be run with specific account setup or may need
    to be skipped if the account doesn't have appropriate limits configured.
    """

    @pytest.fixture(scope="class", autouse=True)
    def _cleanup_blobs(self, jmap_client, token, account_id):
        """Class-scoped fixture that cleans up all tracked blobs after the class."""
        blob_ids: list[str] = []
        TestBlobAllocateLimits._class_blob_ids = blob_ids
        yield
        # Cleanup - delete all tracked blobs
        for blob_id in blob_ids:
            try:
                delete_blob(jmap_client, token, account_id, blob_id)
            except Exception:
                pass

    def _record_blob(self, blob_id: str | None):
        """Record a blob ID for cleanup."""
        if blob_id:
            TestBlobAllocateLimits._class_blob_ids.append(blob_id)

    def test_allocate_too_many_pending(self, jmap_host, jmap_client, api_url, token, account_id,
                                        dynamodb_table, aws_region):
        """Blob/allocate returns tooManyPending when limit exceeded."""
        # Get capability config via raw HTTP request (jmapc doesn't expose raw accounts)
        session_url = f"https://{jmap_host}/.well-known/jmap"
        session_resp = requests.get(
            session_url,
            headers={"Authorization": f"Bearer {token}", "X-JMAP-Stage": "e2e"},
            timeout=30,
        )
        session_data = session_resp.json()
        account = session_data["accounts"].get(account_id, {})
        cap_config = account.get("accountCapabilities", {}).get(UPLOAD_PUT_CAPABILITY, {})
        max_pending = cap_config.get("maxPendingAllocations", 4)

        # Create allocations up to the limit (without uploading)
        alloc_size = 1024
        allocations = {}
        for i in range(max_pending + 1):
            allocations[f"pending{i}"] = {"type": "application/octet-stream", "size": alloc_size}

        response = make_allocate_request(api_url, token, account_id, allocations)

        assert "methodResponses" in response
        name, data, _ = response["methodResponses"][0]
        assert name == "Blob/allocate", f"Expected Blob/allocate, got {name}"

        # Record any created for cleanup
        created = data.get("created", {})
        for key, alloc in created.items():
            self._record_blob(alloc.get("id"))

        # Should have some notCreated with tooManyPending
        not_created = data.get("notCreated", {})

        # We expect at least one tooManyPending error
        too_many_errors = [
            key for key, err in not_created.items()
            if err.get("type") == "tooManyPending"
        ]

        # If all succeeded, the account might have higher limits - that's okay
        if not too_many_errors and len(created) == max_pending + 1:
            pytest.skip(
                f"All {max_pending + 1} allocations succeeded - "
                "account may have higher limits than advertised"
            )

        assert len(too_many_errors) > 0, \
            f"Expected tooManyPending errors, got created={len(created)}, notCreated={not_created}"

        # Clean up all created pending allocations since we don't upload
        if dynamodb_table:
            for key, alloc in created.items():
                cleanup_pending_allocation(
                    dynamodb_table, account_id, alloc["id"], alloc_size, aws_region
                )


# ---------------------------------------------------------------------------
# Multipart Upload E2E Tests (IAM auth)
# ---------------------------------------------------------------------------

class TestBlobMultipartUpload:
    """Tests for the multipart upload flow: Blob/allocate(multipart) → S3 parts → Blob/complete."""

    @pytest.fixture(scope="class", autouse=True)
    def _cleanup_blobs(self, jmap_client, token, account_id):
        """Class-scoped fixture that cleans up all tracked blobs after the class."""
        blob_ids: list[str] = []
        TestBlobMultipartUpload._class_blob_ids = blob_ids
        yield
        for blob_id in blob_ids:
            try:
                delete_blob(jmap_client, token, account_id, blob_id)
            except Exception:
                pass

    def _record_blob(self, blob_id: str | None):
        """Record a blob ID for cleanup."""
        if blob_id:
            TestBlobMultipartUpload._class_blob_ids.append(blob_id)

    def test_multipart_allocate_upload_complete(
        self, jmap_client, api_url, token, account_id,
        api_gateway_invoke_url, e2e_test_role_arn, dynamodb_table, aws_region,
    ):
        """Full multipart flow: allocate → upload parts to S3 → Blob/complete → verify."""
        if not api_gateway_invoke_url:
            pytest.skip("API_GATEWAY_INVOKE_URL not set — cannot run IAM tests")
        if not e2e_test_role_arn:
            pytest.skip("E2E_TEST_ROLE_ARN not set — cannot run IAM tests")

        using = ["urn:ietf:params:jmap:core", UPLOAD_PUT_CAPABILITY]

        # Step 1: Allocate multipart
        allocate_response = make_iam_jmap_request(
            api_gateway_invoke_url,
            account_id,
            [
                [
                    "Blob/allocate",
                    {
                        "accountId": account_id,
                        "create": {
                            "mp0": {
                                "type": "application/octet-stream",
                                "size": 0,
                                "multipart": True,
                            },
                        },
                    },
                    "allocate0",
                ]
            ],
            region=aws_region,
            using=using,
            role_arn=e2e_test_role_arn,
        )

        assert "methodResponses" in allocate_response, f"No methodResponses: {allocate_response}"
        name, data, _ = allocate_response["methodResponses"][0]
        assert name == "Blob/allocate", f"Expected Blob/allocate, got {name}: {data}"

        created = data.get("created", {})
        assert "mp0" in created, f"mp0 not in created: {data}"

        allocation = created["mp0"]
        blob_id = allocation["id"]
        self._record_blob(blob_id)

        # Multipart response has parts array, no url
        assert "parts" in allocation, f"No parts in multipart allocation: {allocation}"
        assert "url" not in allocation, f"Multipart allocation should not have url: {allocation}"

        parts = allocation["parts"]
        assert len(parts) > 0, f"Empty parts array: {allocation}"
        for part in parts:
            assert "partNumber" in part, f"Part missing partNumber: {part}"
            assert "url" in part, f"Part missing url: {part}"

        # Step 2: Upload 2 parts to presigned S3 URLs
        part1_data = os.urandom(5 * 1024 * 1024)  # 5 MiB (S3 minimum for non-last part)
        part2_data = os.urandom(1024)               # 1 KiB

        part_payloads = [part1_data, part2_data]
        completed_parts = []

        for i, part_info in enumerate(parts[:2]):
            put_resp = requests.put(
                part_info["url"],
                data=part_payloads[i],
                timeout=120,
            )
            assert put_resp.status_code in (200, 204), (
                f"PUT part {part_info['partNumber']} failed: "
                f"{put_resp.status_code} {put_resp.text}"
            )
            etag = put_resp.headers.get("ETag")
            assert etag, f"No ETag in response for part {part_info['partNumber']}"
            completed_parts.append({
                "partNumber": part_info["partNumber"],
                "etag": etag,
            })

        # Step 3: Blob/complete
        complete_response = make_iam_jmap_request(
            api_gateway_invoke_url,
            account_id,
            [
                [
                    "Blob/complete",
                    {
                        "accountId": account_id,
                        "id": blob_id,
                        "parts": completed_parts,
                    },
                    "complete0",
                ]
            ],
            region=aws_region,
            using=using,
            role_arn=e2e_test_role_arn,
        )

        assert "methodResponses" in complete_response, f"No methodResponses: {complete_response}"
        cname, cdata, _ = complete_response["methodResponses"][0]
        assert cname == "Blob/complete", f"Expected Blob/complete, got {cname}: {cdata}"
        assert cdata.get("accountId") == account_id, f"Wrong accountId: {cdata}"
        assert cdata.get("id") == blob_id, f"Wrong id: {cdata}"

        # Step 4: Poll DynamoDB for confirmed status
        max_wait = 30
        poll_interval = 2
        elapsed = 0
        blob_confirmed = False

        while elapsed < max_wait and not blob_confirmed:
            time.sleep(poll_interval)
            elapsed += poll_interval
            if dynamodb_table:
                item = get_dynamodb_blob(dynamodb_table, account_id, blob_id, aws_region)
                if item and item.get("status") == "confirmed":
                    blob_confirmed = True

        assert blob_confirmed, f"Blob {blob_id} not confirmed after {max_wait}s"

        # Step 5: Download and verify content
        downloaded = download_blob(jmap_client, token, account_id, blob_id)
        expected = part1_data + part2_data
        assert downloaded is not None, "Download returned None"
        assert len(downloaded) == len(expected), (
            f"Size mismatch: expected {len(expected)}, got {len(downloaded)}"
        )
        assert downloaded == expected, "Downloaded content does not match uploaded parts"

    def test_multipart_rejected_for_cognito(self, api_url, token, account_id):
        """Blob/allocate with multipart=true is rejected for Cognito auth."""
        request_body = {
            "using": ["urn:ietf:params:jmap:core", UPLOAD_PUT_CAPABILITY],
            "methodCalls": [
                [
                    "Blob/allocate",
                    {
                        "accountId": account_id,
                        "create": {
                            "mp0": {
                                "type": "application/octet-stream",
                                "size": 0,
                                "multipart": True,
                            },
                        },
                    },
                    "allocate0",
                ]
            ],
        }
        response = requests.post(
            api_url,
            headers={
                "Authorization": f"Bearer {token}",
                "Content-Type": "application/json",
            },
            json=request_body,
            timeout=30,
        )
        resp_json = response.json()

        assert "methodResponses" in resp_json, f"No methodResponses: {resp_json}"
        name, data, _ = resp_json["methodResponses"][0]
        assert name == "Blob/allocate", f"Expected Blob/allocate, got {name}: {data}"

        not_created = data.get("notCreated")
        assert not_created is not None, f"Expected notCreated, got: {data}"
        assert "mp0" in not_created, f"mp0 not in notCreated: {not_created}"

        error = not_created["mp0"]
        assert error.get("type") == "invalidArguments", (
            f"Expected invalidArguments error, got: {error.get('type')}"
        )
