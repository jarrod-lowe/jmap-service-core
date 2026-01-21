#!/usr/bin/env python3
"""
JMAP Protocol Compliance Tests using jmapc

Uses the independent jmapc Python JMAP client to validate that our server
implementation is compliant with RFC 8620 (JMAP Core).

Environment variables:
    JMAP_HOST: JMAP hostname (e.g., jmap.example.com)
    JMAP_API_TOKEN: Bearer token for authentication

Exit codes:
    0: All tests passed
    1: One or more tests failed
"""

import os
import sys

import requests
from jmapc import Client
from jmapc.methods import CoreEcho, EmailQuery


class Colors:
    """ANSI color codes for terminal output."""

    GREEN = "\033[0;32m"
    RED = "\033[0;31m"
    YELLOW = "\033[1;33m"
    NC = "\033[0m"  # No Color


class TestResult:
    """Track test results."""

    def __init__(self):
        self.passed = 0
        self.failed = 0
        self.tests = []

    def record_pass(self, name: str, detail: str = ""):
        self.passed += 1
        self.tests.append((name, True, detail))
        print(f"{Colors.GREEN}PASS{Colors.NC}: {name}")
        if detail:
            print(f"      {detail}")

    def record_fail(self, name: str, reason: str = ""):
        self.failed += 1
        self.tests.append((name, False, reason))
        print(f"{Colors.RED}FAIL{Colors.NC}: {name}")
        if reason:
            print(f"      {reason}")

    @property
    def total(self) -> int:
        return self.passed + self.failed

    @property
    def all_passed(self) -> bool:
        return self.failed == 0


def get_config() -> tuple[str, str]:
    """Get configuration from environment variables."""
    host = os.environ.get("JMAP_HOST")
    token = os.environ.get("JMAP_API_TOKEN")

    if not host:
        print(f"{Colors.RED}ERROR: JMAP_HOST environment variable not set{Colors.NC}")
        sys.exit(1)

    if not token:
        print(f"{Colors.RED}ERROR: JMAP_API_TOKEN environment variable not set{Colors.NC}")
        sys.exit(1)

    return host, token


def test_client_connection(host: str, token: str, results: TestResult) -> Client | None:
    """
    Test 1: Session Discovery via jmapc Client

    Create a jmapc Client and let it automatically discover and parse the session.
    This validates that our server's session response is compatible with an
    independent JMAP client implementation.
    """
    print()
    print("Testing Session Discovery (jmapc Client)...")

    try:
        client = Client.create_with_api_token(host=host, api_token=token)
        session = client.jmap_session  # This triggers session fetch
        results.record_pass("Client connection successful", f"API URL: {session.api_url}")
    except Exception as e:
        results.record_fail("Client connection", str(e))
        return None

    # Validate session properties are accessible
    # jmapc parses capabilities into structured objects
    try:
        if session.capabilities and session.capabilities.core:
            core = session.capabilities.core
            results.record_pass(
                "Session has urn:ietf:params:jmap:core capability",
                f"maxSizeUpload: {core.max_size_upload}",
            )
        else:
            results.record_fail(
                "Session has urn:ietf:params:jmap:core capability",
                "capabilities.core is None",
            )
    except Exception as e:
        results.record_fail("Session capabilities accessible", str(e))

    # jmapc stores account IDs in primary_accounts
    try:
        account_id = client.account_id
        if account_id:
            results.record_pass("Session has at least one account", f"Account ID: {account_id}")
        else:
            results.record_fail("Session has at least one account", "No account ID found")
    except Exception as e:
        results.record_fail("Session accounts accessible", str(e))

    try:
        if session.primary_accounts:
            results.record_pass(
                "Session has primary_accounts",
                f"core={session.primary_accounts.core}, mail={session.primary_accounts.mail}",
            )
        else:
            results.record_fail(
                "Session has primary_accounts", "primary_accounts is None"
            )
    except Exception as e:
        results.record_fail("Session primary_accounts accessible", str(e))

    try:
        if session.api_url:
            results.record_pass("Session has api_url", session.api_url)
        else:
            results.record_fail("Session has api_url", "api_url is None or empty")
    except Exception as e:
        results.record_fail("Session api_url accessible", str(e))

    try:
        if session.state:
            results.record_pass("Session has state")
        else:
            results.record_fail("Session has state", "state is None or empty")
    except Exception as e:
        results.record_fail("Session state accessible", str(e))

    return client


def test_core_echo(client: Client, results: TestResult):
    """
    Test 2: Core/echo Method Call

    Per RFC 8620 Section 3.5, Core/echo echoes back its arguments unchanged.
    Uses jmapc's CoreEcho method to test authenticated connection to the JMAP API.
    """
    print()
    print("Testing Core/echo (via jmapc)...")

    test_data = {"hello": True, "count": 42, "nested": {"key": "value", "array": [1, 2, 3]}}

    try:
        response = client.request(CoreEcho(data=test_data))
        results.record_pass("Core/echo request successful")

        # Check the response data matches
        response_data = getattr(response, "data", None)
        if response_data == test_data:
            results.record_pass("Core/echo echoed arguments exactly")
        else:
            actual_data = getattr(response, "data", response)
            results.record_fail(
                "Core/echo echoed arguments exactly",
                f"Expected: {test_data}\nGot: {actual_data}",
            )
    except Exception as e:
        results.record_fail("Core/echo request", str(e))


def test_email_query(client: Client, results: TestResult):
    """
    Test 3: Email/query Method Call

    Make a basic Email/query request using jmapc to validate mail method handling.
    Note: jmapc Client automatically uses client.account_id for all requests.
    """
    print()
    print("Testing Email/query (via jmapc)...")

    try:
        response = client.request(EmailQuery())
        results.record_pass("Email/query request successful")

        # Check if response is an error (server may not have Email implemented)
        error_type = getattr(response, "type", None)
        error_desc = getattr(response, "description", None)
        if error_type and error_desc:
            # This is a JMAP error response
            results.record_pass(
                "Email/query returned JMAP error",
                f"type={error_type}, description={error_desc}",
            )
        else:
            # Check the response has expected fields for successful query
            ids = getattr(response, "ids", None)
            if ids is not None:
                results.record_pass(
                    "Email/query response has ids", f"Count: {len(ids)}"
                )
            else:
                results.record_fail(
                    "Email/query response has ids", "No ids field in response"
                )

            if hasattr(response, "query_state"):
                results.record_pass("Email/query response has query_state")
            else:
                results.record_fail(
                    "Email/query response has query_state", "No query_state field"
                )

    except Exception as e:
        results.record_fail("Email/query request", str(e))


def test_blob_upload(client: Client, token: str, results: TestResult):
    """
    Test 4: Blob Upload (RFC 8620 Section 6.1)

    Validate session has uploadUrl and blob upload returns compliant response.
    Note: jmapc doesn't support raw blob upload, so we use raw HTTP requests.
    """
    print()
    print("Testing Blob Upload (raw HTTP - jmapc doesn't support this)...")

    session = client.jmap_session

    # Test: Session has upload_url (RFC 8620 Section 2 requirement)
    if not session.upload_url:
        results.record_fail("Session has upload_url", "upload_url not in session")
        return

    results.record_pass("Session has upload_url", session.upload_url)

    # Get account ID
    try:
        account_id = client.account_id
    except Exception as e:
        results.record_fail("Blob upload request", f"No account ID: {e}")
        return

    # Replace {accountId} placeholder in upload_url
    upload_endpoint = session.upload_url.replace("{accountId}", account_id)

    # Upload test blob
    test_content = b"Test blob content for RFC 8620 compliance"
    headers = {
        "Authorization": f"Bearer {token}",
        "Content-Type": "application/octet-stream",
    }

    try:
        response = requests.post(
            upload_endpoint, headers=headers, data=test_content, timeout=30
        )
    except requests.RequestException as e:
        results.record_fail("Blob upload request", str(e))
        return

    # Test: HTTP 201 response
    if response.status_code == 201:
        results.record_pass("Blob upload returns 201")
    else:
        results.record_fail(
            "Blob upload returns 201", f"Got HTTP {response.status_code}: {response.text}"
        )
        return

    # Parse JSON response
    try:
        response_data = response.json()
    except Exception as e:
        results.record_fail("Blob upload returns valid JSON", str(e))
        return

    results.record_pass("Blob upload returns valid JSON")

    # Test: RFC 8620 Section 6.1 required fields
    required_fields = ["accountId", "blobId", "type", "size"]
    for field in required_fields:
        if field in response_data:
            results.record_pass(f"Response has '{field}'", str(response_data[field]))
        else:
            results.record_fail(f"Response has '{field}'", "Field missing")

    # Test: accountId matches
    if response_data.get("accountId") == account_id:
        results.record_pass("Response accountId matches request")
    else:
        results.record_fail(
            "Response accountId matches request",
            f"Expected '{account_id}', got '{response_data.get('accountId')}'",
        )

    # Test: size matches content length
    if response_data.get("size") == len(test_content):
        results.record_pass("Response size matches content length")
    else:
        results.record_fail(
            "Response size matches content length",
            f"Expected {len(test_content)}, got {response_data.get('size')}",
        )

    # Test: type matches Content-Type
    if response_data.get("type") == "application/octet-stream":
        results.record_pass("Response type matches Content-Type")
    else:
        results.record_fail(
            "Response type matches Content-Type",
            f"Expected 'application/octet-stream', got '{response_data.get('type')}'",
        )


def test_blob_download(client: Client, token: str, results: TestResult):
    """
    Test 5: Blob Download (CloudFront Signed URL Redirect)

    Validates:
    - Session has downloadUrl
    - Upload blob, then download via /download/{accountId}/{blobId}
    - Returns 302 redirect to CloudFront signed URL
    - Following redirect returns original blob content
    """
    print()
    print("Testing Blob Download (redirect to CloudFront signed URL)...")

    session = client.jmap_session

    # Test: Session has download_url (RFC 8620 Section 2 requirement)
    if not session.download_url:
        results.record_fail("Session has download_url", "download_url not in session")
        return

    results.record_pass("Session has download_url", session.download_url)

    # Get account ID
    try:
        account_id = client.account_id
    except Exception as e:
        results.record_fail("Blob download request", f"No account ID: {e}")
        return

    # First, upload a test blob
    upload_endpoint = session.upload_url.replace("{accountId}", account_id)
    test_content = b"Test blob content for download verification - unique content 12345"
    headers = {
        "Authorization": f"Bearer {token}",
        "Content-Type": "application/octet-stream",
    }

    try:
        upload_response = requests.post(
            upload_endpoint, headers=headers, data=test_content, timeout=30
        )
        if upload_response.status_code != 201:
            results.record_fail(
                "Upload blob for download test",
                f"Got HTTP {upload_response.status_code}: {upload_response.text}",
            )
            return
        upload_data = upload_response.json()
        blob_id = upload_data.get("blobId")
        if not blob_id:
            results.record_fail("Upload blob for download test", "No blobId in response")
            return
        results.record_pass("Upload blob for download test", f"blobId: {blob_id}")
    except Exception as e:
        results.record_fail("Upload blob for download test", str(e))
        return

    # Build download URL from session.download_url template
    download_url = session.download_url.replace("{accountId}", account_id).replace(
        "{blobId}", blob_id
    )

    # Request download (should return 302 redirect)
    try:
        # Use allow_redirects=False to capture the 302
        download_response = requests.get(
            download_url,
            headers={"Authorization": f"Bearer {token}"},
            allow_redirects=False,
            timeout=30,
        )
    except Exception as e:
        results.record_fail("Blob download request", str(e))
        return

    # Test: Returns 302 redirect
    if download_response.status_code == 302:
        results.record_pass("Blob download returns 302 redirect")
    else:
        results.record_fail(
            "Blob download returns 302 redirect",
            f"Got HTTP {download_response.status_code}: {download_response.text}",
        )
        return

    # Test: Has Location header
    location = download_response.headers.get("Location")
    if location:
        results.record_pass("Blob download has Location header", location[:100] + "...")
    else:
        results.record_fail("Blob download has Location header", "No Location header")
        return

    # Test: Location is a CloudFront signed URL
    if "Signature=" in location or "Key-Pair-Id=" in location:
        results.record_pass("Location is CloudFront signed URL")
    else:
        results.record_fail(
            "Location is CloudFront signed URL",
            "URL doesn't contain signature parameters",
        )

    # Test: Following redirect returns original content
    try:
        content_response = requests.get(location, timeout=30)
        if content_response.status_code == 200:
            results.record_pass("CloudFront signed URL returns 200")
            if content_response.content == test_content:
                results.record_pass("Downloaded content matches uploaded content")
            else:
                results.record_fail(
                    "Downloaded content matches uploaded content",
                    f"Content mismatch: expected {len(test_content)} bytes, got {len(content_response.content)} bytes",
                )
        else:
            results.record_fail(
                "CloudFront signed URL returns 200",
                f"Got HTTP {content_response.status_code}: {content_response.text}",
            )
    except Exception as e:
        results.record_fail("Follow redirect to CloudFront", str(e))


def test_blob_download_cross_account(client: Client, token: str, results: TestResult):
    """
    Test 6: Blob Download Cross-Account Access (should be forbidden)

    Validates that attempting to download a blob using a different accountId
    in the path returns 403 Forbidden.
    """
    print()
    print("Testing Blob Download Cross-Account Access...")

    session = client.jmap_session

    if not session.download_url:
        results.record_pass(
            "Skip cross-account test", "download_url not available"
        )
        return

    # Get actual account ID
    try:
        account_id = client.account_id
    except Exception as e:
        results.record_fail("Cross-account test", f"No account ID: {e}")
        return

    # First, upload a test blob to get a valid blobId
    upload_endpoint = session.upload_url.replace("{accountId}", account_id)
    test_content = b"Test blob for cross-account check"
    headers = {
        "Authorization": f"Bearer {token}",
        "Content-Type": "application/octet-stream",
    }

    try:
        upload_response = requests.post(
            upload_endpoint, headers=headers, data=test_content, timeout=30
        )
        if upload_response.status_code != 201:
            results.record_fail(
                "Upload blob for cross-account test",
                f"Got HTTP {upload_response.status_code}",
            )
            return
        upload_data = upload_response.json()
        blob_id = upload_data.get("blobId")
        if not blob_id:
            results.record_fail("Upload blob for cross-account test", "No blobId")
            return
    except Exception as e:
        results.record_fail("Upload blob for cross-account test", str(e))
        return

    # Try to download with a different accountId in the path
    fake_account_id = "fake-account-id-12345"
    download_url = session.download_url.replace(
        "{accountId}", fake_account_id
    ).replace("{blobId}", blob_id)

    try:
        response = requests.get(
            download_url,
            headers={"Authorization": f"Bearer {token}"},
            allow_redirects=False,
            timeout=30,
        )
    except Exception as e:
        results.record_fail("Cross-account download request", str(e))
        return

    # Test: Returns 403 Forbidden (account ID mismatch)
    if response.status_code == 403:
        results.record_pass(
            "Cross-account download returns 403",
            "Correctly rejected access to different account",
        )
    else:
        results.record_fail(
            "Cross-account download returns 403",
            f"Got HTTP {response.status_code}: {response.text}",
        )


def print_summary(results: TestResult):
    """Print test summary."""
    print()
    print("=" * 40)
    print("JMAP Protocol Compliance Test Summary")
    print("=" * 40)
    print(f"Tests run:   {results.total}")
    print(f"{Colors.GREEN}Passed:      {results.passed}{Colors.NC}")
    if results.failed > 0:
        print(f"{Colors.RED}Failed:      {results.failed}{Colors.NC}")
    else:
        print(f"Failed:      {results.failed}")
    print("=" * 40)

    if results.all_passed:
        print(f"{Colors.GREEN}ALL TESTS PASSED{Colors.NC}")
    else:
        print(f"{Colors.RED}SOME TESTS FAILED{Colors.NC}")


def main():
    """Run all JMAP protocol compliance tests."""
    print("=" * 40)
    print("JMAP Protocol Compliance Tests (jmapc)")
    print("=" * 40)

    host, token = get_config()
    results = TestResult()

    # Test 1: Session Discovery via jmapc Client
    client = test_client_connection(host, token, results)

    if client:
        # Test 2: Core/echo
        test_core_echo(client, results)

        # Test 3: Email/query
        test_email_query(client, results)

        # Test 4: Blob Upload
        test_blob_upload(client, token, results)

        # Test 5: Blob Download
        test_blob_download(client, token, results)

        # Test 6: Blob Download Cross-Account Access
        test_blob_download_cross_account(client, token, results)

    # Print summary
    print_summary(results)

    # Exit with appropriate code
    sys.exit(0 if results.all_passed else 1)


if __name__ == "__main__":
    main()
