#!/usr/bin/env python3
"""
JMAP Protocol Compliance Tests using jmapc

Uses the independent jmapc Python JMAP client to validate that our server
implementation is compliant with RFC 8620 (JMAP Core).

Environment variables:
    JMAP_API_URL: Base URL of the JMAP API (e.g., https://xxx.execute-api.region.amazonaws.com/test)
    JMAP_API_TOKEN: Bearer token for authentication

Exit codes:
    0: All tests passed
    1: One or more tests failed
"""

import json
import os
import re
import sys
from typing import Any
from urllib.parse import urljoin

import requests
from jmapc import Client
from jmapc.session import Session


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


def camel_to_snake(name: str) -> str:
    """Convert camelCase to snake_case."""
    # Insert underscore before uppercase letters and convert to lowercase
    s1 = re.sub(r"(.)([A-Z][a-z]+)", r"\1_\2", name)
    return re.sub(r"([a-z0-9])([A-Z])", r"\1_\2", s1).lower()


def convert_keys_to_snake_case(data: Any) -> Any:
    """
    Recursively convert all dict keys from camelCase to snake_case.

    jmapc's Session.from_dict expects snake_case keys internally,
    but RFC 8620 specifies camelCase in the JSON wire format.
    """
    if isinstance(data, dict):
        return {camel_to_snake(k): convert_keys_to_snake_case(v) for k, v in data.items()}
    elif isinstance(data, list):
        return [convert_keys_to_snake_case(item) for item in data]
    else:
        return data


def get_config() -> tuple[str, str]:
    """Get configuration from environment variables."""
    api_url = os.environ.get("JMAP_API_URL")
    token = os.environ.get("JMAP_API_TOKEN")

    if not api_url:
        print(f"{Colors.RED}ERROR: JMAP_API_URL environment variable not set{Colors.NC}")
        sys.exit(1)

    if not token:
        print(f"{Colors.RED}ERROR: JMAP_API_TOKEN environment variable not set{Colors.NC}")
        sys.exit(1)

    return api_url, token


def test_session_discovery(api_url: str, token: str, results: TestResult) -> dict | None:
    """
    Test 1: Session Discovery

    Fetch /.well-known/jmap and validate RFC 8620 required fields.
    """
    print()
    print("Testing Session Discovery (GET /.well-known/jmap)...")

    session_url = urljoin(api_url.rstrip("/") + "/", ".well-known/jmap")
    headers = {"Authorization": f"Bearer {token}"}

    try:
        response = requests.get(session_url, headers=headers, timeout=30)
    except requests.RequestException as e:
        results.record_fail("Session Discovery request", str(e))
        return None

    # Test: HTTP 200 response
    if response.status_code == 200:
        results.record_pass("Session Discovery returns 200")
    else:
        results.record_fail(
            "Session Discovery returns 200", f"Got HTTP {response.status_code}"
        )
        return None

    # Parse JSON
    try:
        session_data = response.json()
    except json.JSONDecodeError as e:
        results.record_fail("Session Discovery returns valid JSON", str(e))
        return None

    results.record_pass("Session Discovery returns valid JSON")

    # Test: capabilities field exists and contains urn:ietf:params:jmap:core
    if "capabilities" in session_data and isinstance(session_data["capabilities"], dict):
        if "urn:ietf:params:jmap:core" in session_data["capabilities"]:
            results.record_pass("Session has urn:ietf:params:jmap:core capability")
        else:
            results.record_fail(
                "Session has urn:ietf:params:jmap:core capability",
                f"capabilities: {list(session_data['capabilities'].keys())}",
            )
    else:
        results.record_fail(
            "Session has capabilities dict", f"Got: {type(session_data.get('capabilities'))}"
        )

    # Test: accounts field exists and has at least one account
    if "accounts" in session_data and isinstance(session_data["accounts"], dict):
        if len(session_data["accounts"]) > 0:
            account_ids = list(session_data["accounts"].keys())
            results.record_pass(
                "Session has at least one account", f"Account IDs: {account_ids}"
            )
        else:
            results.record_fail("Session has at least one account", "accounts dict is empty")
    else:
        results.record_fail(
            "Session has accounts dict", f"Got: {type(session_data.get('accounts'))}"
        )

    # Test: primaryAccounts field exists
    if "primaryAccounts" in session_data and isinstance(
        session_data["primaryAccounts"], dict
    ):
        results.record_pass("Session has primaryAccounts")
    else:
        results.record_fail(
            "Session has primaryAccounts",
            f"Got: {type(session_data.get('primaryAccounts'))}",
        )

    # Test: apiUrl field exists and is a string
    if "apiUrl" in session_data and isinstance(session_data["apiUrl"], str):
        results.record_pass("Session has apiUrl", session_data["apiUrl"])
    else:
        results.record_fail(
            "Session has apiUrl string", f"Got: {session_data.get('apiUrl')}"
        )

    # Test: state field exists and is a string
    if "state" in session_data and isinstance(session_data["state"], str):
        results.record_pass("Session has state")
    else:
        results.record_fail(
            "Session has state string", f"Got: {type(session_data.get('state'))}"
        )

    return session_data


def test_jmapc_session_parsing(session_data: dict, results: TestResult) -> Session | None:
    """
    Test 2: jmapc Session Parsing

    Use the jmapc library to parse the session data. This validates that
    our session format is compatible with an independent JMAP client.

    Note: jmapc's Session.from_dict expects snake_case keys internally,
    but RFC 8620 specifies camelCase in the JSON wire format. We convert
    the keys before passing to jmapc to test compatibility.
    """
    print()
    print("Testing jmapc Session Parsing...")

    try:
        # Convert camelCase keys to snake_case for jmapc's internal format
        snake_case_data = convert_keys_to_snake_case(session_data)

        session = Session.from_dict(snake_case_data)
        results.record_pass("jmapc parses session successfully")
        return session
    except Exception as e:
        results.record_fail("jmapc parses session successfully", str(e))
        return None


def test_email_query(api_url: str, token: str, session_data: dict, results: TestResult):
    """
    Test 3: Email/query Method Call

    Make a basic Email/query request to the apiUrl and validate the response structure.
    """
    print()
    print("Testing Email/query (POST to apiUrl)...")

    # Get apiUrl and first account ID
    api_endpoint = session_data.get("apiUrl")
    if not api_endpoint:
        results.record_fail("Email/query request", "No apiUrl in session")
        return

    accounts = session_data.get("accounts", {})
    if not accounts:
        results.record_fail("Email/query request", "No accounts in session")
        return

    account_id = list(accounts.keys())[0]

    # Build JMAP request
    jmap_request = {
        "using": ["urn:ietf:params:jmap:core", "urn:ietf:params:jmap:mail"],
        "methodCalls": [["Email/query", {"accountId": account_id}, "call1"]],
    }

    headers = {
        "Authorization": f"Bearer {token}",
        "Content-Type": "application/json",
    }

    try:
        response = requests.post(
            api_endpoint, headers=headers, json=jmap_request, timeout=30
        )
    except requests.RequestException as e:
        results.record_fail("Email/query request", str(e))
        return

    # Test: HTTP 200 response
    if response.status_code == 200:
        results.record_pass("Email/query returns 200")
    else:
        results.record_fail(
            "Email/query returns 200", f"Got HTTP {response.status_code}"
        )
        return

    # Parse JSON response
    try:
        response_data = response.json()
    except json.JSONDecodeError as e:
        results.record_fail("Email/query returns valid JSON", str(e))
        return

    results.record_pass("Email/query returns valid JSON")

    # Validate JMAP response structure
    # Must have methodResponses array
    if "methodResponses" in response_data and isinstance(
        response_data["methodResponses"], list
    ):
        results.record_pass("Response has methodResponses array")
    else:
        results.record_fail(
            "Response has methodResponses array",
            f"Got: {type(response_data.get('methodResponses'))}",
        )
        return

    # Check the first method response
    if len(response_data["methodResponses"]) > 0:
        method_response = response_data["methodResponses"][0]
        if isinstance(method_response, list) and len(method_response) >= 3:
            method_name = method_response[0]
            method_args = method_response[1]
            call_id = method_response[2]

            # Response call_id should match request call_id
            if call_id == "call1":
                results.record_pass("Response call_id matches request")
            else:
                results.record_fail(
                    "Response call_id matches request",
                    f"Expected 'call1', got '{call_id}'",
                )

            # Method name should be Email/query or error
            if method_name in ["Email/query", "error"]:
                results.record_pass(
                    f"Response method is '{method_name}'",
                    f"Args: {json.dumps(method_args)[:100]}...",
                )
            else:
                results.record_fail(
                    "Response method is Email/query or error",
                    f"Got: {method_name}",
                )
        else:
            results.record_fail(
                "Method response is [name, args, callId] tuple",
                f"Got: {method_response}",
            )
    else:
        results.record_fail("methodResponses has at least one response", "Array is empty")


def test_core_echo(api_url: str, token: str, session_data: dict, results: TestResult):
    """
    Test 4: Core/echo Method Call

    Per RFC 8620 Section 3.5, Core/echo echoes back its arguments unchanged.
    This tests authenticated connection to the JMAP API.
    """
    print()
    print("Testing Core/echo (POST to apiUrl)...")

    # Get apiUrl
    api_endpoint = session_data.get("apiUrl")
    if not api_endpoint:
        results.record_fail("Core/echo request", "No apiUrl in session")
        return

    # Build JMAP request with test arguments
    test_args = {
        "hello": True,
        "count": 42,
        "nested": {"key": "value", "array": [1, 2, 3]},
    }

    jmap_request = {
        "using": ["urn:ietf:params:jmap:core"],
        "methodCalls": [["Core/echo", test_args, "echo1"]],
    }

    headers = {
        "Authorization": f"Bearer {token}",
        "Content-Type": "application/json",
    }

    try:
        response = requests.post(
            api_endpoint, headers=headers, json=jmap_request, timeout=30
        )
    except requests.RequestException as e:
        results.record_fail("Core/echo request", str(e))
        return

    # Test: HTTP 200 response
    if response.status_code == 200:
        results.record_pass("Core/echo returns 200")
    else:
        results.record_fail(
            "Core/echo returns 200", f"Got HTTP {response.status_code}: {response.text}"
        )
        return

    # Parse JSON response
    try:
        response_data = response.json()
    except json.JSONDecodeError as e:
        results.record_fail("Core/echo returns valid JSON", str(e))
        return

    results.record_pass("Core/echo returns valid JSON")

    # Validate JMAP response structure
    if "methodResponses" not in response_data or not isinstance(
        response_data["methodResponses"], list
    ):
        results.record_fail(
            "Response has methodResponses array",
            f"Got: {type(response_data.get('methodResponses'))}",
        )
        return

    if len(response_data["methodResponses"]) == 0:
        results.record_fail("methodResponses has at least one response", "Array is empty")
        return

    method_response = response_data["methodResponses"][0]
    if not isinstance(method_response, list) or len(method_response) < 3:
        results.record_fail(
            "Method response is [name, args, callId] tuple",
            f"Got: {method_response}",
        )
        return

    method_name = method_response[0]
    method_args = method_response[1]
    call_id = method_response[2]

    # Test: Response call_id should match request call_id
    if call_id == "echo1":
        results.record_pass("Core/echo response call_id matches request")
    else:
        results.record_fail(
            "Core/echo response call_id matches request",
            f"Expected 'echo1', got '{call_id}'",
        )

    # Test: Method name should be Core/echo
    if method_name == "Core/echo":
        results.record_pass("Response method is 'Core/echo'")
    else:
        results.record_fail(
            "Response method is 'Core/echo'",
            f"Got: {method_name}",
        )
        return

    # Test: Arguments should be echoed back exactly
    if method_args == test_args:
        results.record_pass("Core/echo echoed arguments exactly")
    else:
        results.record_fail(
            "Core/echo echoed arguments exactly",
            f"Expected: {test_args}\nGot: {method_args}",
        )


def test_blob_upload(api_url: str, token: str, session_data: dict, results: TestResult):
    """
    Test 5: Blob Upload (RFC 8620 Section 6.1)

    Validate session has uploadUrl and blob upload returns compliant response.
    """
    print()
    print("Testing Blob Upload (POST to uploadUrl)...")

    # Test: Session has uploadUrl (RFC 8620 Section 2 requirement)
    upload_url = session_data.get("uploadUrl")
    if not upload_url:
        results.record_fail("Session has uploadUrl", "uploadUrl not in session")
        return

    results.record_pass("Session has uploadUrl", upload_url)

    # Get account ID
    accounts = session_data.get("accounts", {})
    if not accounts:
        results.record_fail("Blob upload request", "No accounts in session")
        return
    account_id = list(accounts.keys())[0]

    # Replace {accountId} placeholder in uploadUrl
    upload_endpoint = upload_url.replace("{accountId}", account_id)

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
    except json.JSONDecodeError as e:
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
        results.record_fail("Response accountId matches request",
            f"Expected '{account_id}', got '{response_data.get('accountId')}'")

    # Test: size matches content length
    if response_data.get("size") == len(test_content):
        results.record_pass("Response size matches content length")
    else:
        results.record_fail("Response size matches content length",
            f"Expected {len(test_content)}, got {response_data.get('size')}")

    # Test: type matches Content-Type
    if response_data.get("type") == "application/octet-stream":
        results.record_pass("Response type matches Content-Type")
    else:
        results.record_fail("Response type matches Content-Type",
            f"Expected 'application/octet-stream', got '{response_data.get('type')}'")


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

    api_url, token = get_config()
    results = TestResult()

    # Test 1: Session Discovery
    session_data = test_session_discovery(api_url, token, results)

    if session_data:
        # Test 2: jmapc Session Parsing
        test_jmapc_session_parsing(session_data, results)

        # Test 3: Email/query
        test_email_query(api_url, token, session_data, results)

        # Test 4: Core/echo
        test_core_echo(api_url, token, session_data, results)

        # Test 5: Blob Upload
        test_blob_upload(api_url, token, session_data, results)

    # Print summary
    print_summary(results)

    # Exit with appropriate code
    sys.exit(0 if results.all_passed else 1)


if __name__ == "__main__":
    main()
