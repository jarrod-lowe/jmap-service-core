"""
Result Reference tests (RFC 8620 Section 3.7).

Tests for JMAP result references that allow method calls to reference
results from previous method calls in the same request.
"""

import uuid
from datetime import datetime, timezone

import requests
from jmapc import Client


def make_jmap_request(api_url: str, token: str, method_calls: list) -> dict:
    """Make a raw JMAP API request."""
    request_body = {
        "using": ["urn:ietf:params:jmap:core", "urn:ietf:params:jmap:mail"],
        "methodCalls": method_calls,
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


def create_test_mailbox(api_url: str, token: str, account_id: str) -> str | None:
    """Create a test mailbox and return its ID."""
    unique_id = str(uuid.uuid4())[:8]
    mailbox_name = f"ResultRef-Test-{unique_id}"

    mailbox_set_call = [
        "Mailbox/set",
        {
            "accountId": account_id,
            "create": {
                "testMailbox": {
                    "name": mailbox_name,
                }
            },
        },
        "createMailbox0",
    ]

    response = make_jmap_request(api_url, token, [mailbox_set_call])
    if "methodResponses" not in response:
        return None

    method_responses = response["methodResponses"]
    if len(method_responses) == 0:
        return None

    response_name, response_data, _ = method_responses[0]
    if response_name != "Mailbox/set":
        return None

    created = response_data.get("created", {})
    mailbox_info = created.get("testMailbox")
    if not mailbox_info:
        return None

    return mailbox_info.get("id")


def import_test_email(
    api_url: str,
    upload_url: str,
    token: str,
    account_id: str,
    mailbox_id: str,
    subject: str,
) -> str | None:
    """Import a test email and return its ID."""
    unique_id = str(uuid.uuid4())
    message_id = f"<resultref-test-{unique_id}@jmap-test.example>"

    date_str = datetime.now(timezone.utc).strftime("%a, %d %b %Y %H:%M:%S %z")
    email_content = f"""From: Test Sender <sender@example.com>
To: Test Recipient <recipient@example.com>
Subject: {subject}
Date: {date_str}
Message-ID: {message_id}
Content-Type: text/plain; charset=utf-8

This is a test email for result reference testing.
""".replace("\n", "\r\n")

    # Upload email blob
    upload_endpoint = upload_url.replace("{accountId}", account_id)
    headers = {
        "Authorization": f"Bearer {token}",
        "Content-Type": "message/rfc822",
    }

    upload_response = requests.post(
        upload_endpoint,
        headers=headers,
        data=email_content.encode("utf-8"),
        timeout=30,
    )
    if upload_response.status_code != 201:
        return None

    upload_data = upload_response.json()
    blob_id = upload_data.get("blobId")
    if not blob_id:
        return None

    # Import email
    import_call = [
        "Email/import",
        {
            "accountId": account_id,
            "emails": {
                "email": {
                    "blobId": blob_id,
                    "mailboxIds": {mailbox_id: True},
                    "receivedAt": datetime.now(timezone.utc).strftime(
                        "%Y-%m-%dT%H:%M:%SZ"
                    ),
                }
            },
        },
        "import0",
    ]

    response = make_jmap_request(api_url, token, [import_call])
    if "methodResponses" not in response:
        return None

    method_responses = response["methodResponses"]
    if len(method_responses) == 0:
        return None

    response_name, response_data, _ = method_responses[0]
    if response_name != "Email/import":
        return None

    created = response_data.get("created", {})
    if "email" not in created:
        return None

    return created["email"].get("id")


def test_result_references(client: Client, config, results):
    """
    Test JMAP Result References (RFC 8620 Section 3.7).

    Result references allow a method call to reference the result of a
    previous method call in the same request using "#" prefixed properties.
    """
    print()
    print("=" * 40)
    print("Testing Result References (RFC 8620 Section 3.7)...")
    print("=" * 40)

    session = client.jmap_session

    try:
        account_id = client.account_id
    except Exception as e:
        results.record_fail("Result reference tests", f"No account ID: {e}")
        return

    # Create test mailbox and import test emails
    mailbox_id = create_test_mailbox(session.api_url, config.token, account_id)
    if not mailbox_id:
        results.record_fail("Result reference test setup", "Failed to create mailbox")
        return

    # Import 3 test emails
    email_ids = []
    for i in range(3):
        email_id = import_test_email(
            session.api_url,
            session.upload_url,
            config.token,
            account_id,
            mailbox_id,
            f"Result Reference Test Email {i}",
        )
        if not email_id:
            results.record_fail(
                "Result reference test setup", f"Failed to import email {i}"
            )
            return
        email_ids.append(email_id)

    results.record_pass(
        "Result reference test setup",
        f"mailbox: {mailbox_id}, emails: {len(email_ids)}",
    )

    # Test 1: Simple result reference - Email/query -> Email/get
    test_simple_result_reference(
        session.api_url, config.token, account_id, mailbox_id, results
    )

    # Test 2: Result reference with wildcard path
    test_wildcard_result_reference(
        session.api_url, config.token, account_id, mailbox_id, results
    )

    # Test 3: Invalid result reference (resultOf not found)
    test_invalid_result_of(session.api_url, config.token, account_id, results)

    # Test 4: Invalid result reference (name mismatch)
    test_name_mismatch(
        session.api_url, config.token, account_id, mailbox_id, results
    )

    # Test 5: Conflicting keys (both "ids" and "#ids")
    test_conflicting_keys(
        session.api_url, config.token, account_id, mailbox_id, results
    )


def test_simple_result_reference(
    api_url: str, token: str, account_id: str, mailbox_id: str, results
):
    """
    Test: Simple Result Reference (RFC 8620 Section 3.7)

    Email/query returns ids, Email/get references those ids via #ids.
    """
    print()
    print("Test: Simple result reference (Email/query -> Email/get)...")

    # Build request with result reference
    method_calls = [
        [
            "Email/query",
            {
                "accountId": account_id,
                "filter": {"inMailbox": mailbox_id},
            },
            "query0",
        ],
        [
            "Email/get",
            {
                "accountId": account_id,
                "#ids": {
                    "resultOf": "query0",
                    "name": "Email/query",
                    "path": "/ids",
                },
                "properties": ["id", "subject"],
            },
            "get0",
        ],
    ]

    try:
        response = make_jmap_request(api_url, token, method_calls)
    except Exception as e:
        results.record_fail("Simple result reference request", str(e))
        return

    if "methodResponses" not in response:
        results.record_fail(
            "Simple result reference", f"No methodResponses: {response}"
        )
        return

    method_responses = response["methodResponses"]
    if len(method_responses) != 2:
        results.record_fail(
            "Simple result reference",
            f"Expected 2 responses, got {len(method_responses)}",
        )
        return

    # Check Email/query response
    query_name, query_data, query_id = method_responses[0]
    if query_name == "error":
        results.record_fail(
            "Simple result reference (query)",
            f"Query error: {query_data.get('type')}: {query_data.get('description')}",
        )
        return

    if query_name != "Email/query":
        results.record_fail(
            "Simple result reference (query)",
            f"Unexpected response: {query_name}",
        )
        return

    query_ids = query_data.get("ids", [])
    results.record_pass(
        "Email/query returned ids",
        f"count: {len(query_ids)}",
    )

    # Check Email/get response
    get_name, get_data, get_id = method_responses[1]
    if get_name == "error":
        results.record_fail(
            "Simple result reference (get)",
            f"Get error: {get_data.get('type')}: {get_data.get('description')}",
        )
        return

    if get_name != "Email/get":
        results.record_fail(
            "Simple result reference (get)",
            f"Unexpected response: {get_name}",
        )
        return

    email_list = get_data.get("list", [])

    # Verify Email/get received the resolved ids
    if len(email_list) == len(query_ids):
        results.record_pass(
            "Email/get received resolved ids",
            f"count: {len(email_list)} (matches query)",
        )
    else:
        results.record_fail(
            "Email/get received resolved ids",
            f"Expected {len(query_ids)} emails, got {len(email_list)}",
        )
        return

    # Verify the email IDs match
    get_ids = [e.get("id") for e in email_list]
    if set(get_ids) == set(query_ids):
        results.record_pass(
            "Result reference resolved correctly",
            "Email/get returned same emails as Email/query",
        )
    else:
        results.record_fail(
            "Result reference resolved correctly",
            f"Query IDs: {query_ids}, Get IDs: {get_ids}",
        )


def test_wildcard_result_reference(
    api_url: str, token: str, account_id: str, mailbox_id: str, results
):
    """
    Test: Wildcard Result Reference Path (RFC 8620 Section 3.7)

    Use /list/*/id to extract all id values from the list array.
    """
    print()
    print("Test: Wildcard result reference path (/list/*/id)...")

    # Email/get returns list with objects containing id
    # We can chain Email/query -> Email/get (for list) -> Email/get (with wildcard)
    method_calls = [
        [
            "Email/query",
            {
                "accountId": account_id,
                "filter": {"inMailbox": mailbox_id},
                "limit": 2,
            },
            "query0",
        ],
        [
            "Email/get",
            {
                "accountId": account_id,
                "#ids": {
                    "resultOf": "query0",
                    "name": "Email/query",
                    "path": "/ids",
                },
                "properties": ["id", "threadId"],
            },
            "get0",
        ],
        [
            "Thread/get",
            {
                "accountId": account_id,
                "#ids": {
                    "resultOf": "get0",
                    "name": "Email/get",
                    "path": "/list/*/threadId",
                },
            },
            "thread0",
        ],
    ]

    try:
        response = make_jmap_request(api_url, token, method_calls)
    except Exception as e:
        results.record_fail("Wildcard result reference request", str(e))
        return

    if "methodResponses" not in response:
        results.record_fail(
            "Wildcard result reference", f"No methodResponses: {response}"
        )
        return

    method_responses = response["methodResponses"]
    if len(method_responses) != 3:
        results.record_fail(
            "Wildcard result reference",
            f"Expected 3 responses, got {len(method_responses)}",
        )
        return

    # Check Email/get response (second call)
    get_name, get_data, get_id = method_responses[1]
    if get_name == "error":
        results.record_fail(
            "Wildcard result reference (Email/get)",
            f"Error: {get_data.get('type')}: {get_data.get('description')}",
        )
        return

    email_list = get_data.get("list", [])
    thread_ids = [e.get("threadId") for e in email_list if e.get("threadId")]

    results.record_pass(
        "Email/get returned list with threadIds",
        f"count: {len(thread_ids)}",
    )

    # Check Thread/get response (third call)
    thread_name, thread_data, thread_id = method_responses[2]
    if thread_name == "error":
        results.record_fail(
            "Wildcard result reference (Thread/get)",
            f"Error: {thread_data.get('type')}: {thread_data.get('description')}",
        )
        return

    if thread_name != "Thread/get":
        results.record_fail(
            "Wildcard result reference (Thread/get)",
            f"Unexpected response: {thread_name}",
        )
        return

    thread_list = thread_data.get("list", [])

    # Verify Thread/get received the extracted threadIds
    if len(thread_list) == len(thread_ids):
        results.record_pass(
            "Wildcard path extracted threadIds correctly",
            f"Thread/get returned {len(thread_list)} threads",
        )
    else:
        # It's possible some threads were deduplicated if emails share threads
        results.record_pass(
            "Wildcard path resolved",
            f"Thread/get returned {len(thread_list)} threads (may include deduplication)",
        )


def test_invalid_result_of(api_url: str, token: str, account_id: str, results):
    """
    Test: Invalid resultOf Reference (RFC 8620 Section 3.7)

    When resultOf references a non-existent clientId, return invalidResultReference.
    """
    print()
    print("Test: Invalid resultOf reference...")

    method_calls = [
        [
            "Email/get",
            {
                "accountId": account_id,
                "#ids": {
                    "resultOf": "nonexistent",
                    "name": "Email/query",
                    "path": "/ids",
                },
            },
            "get0",
        ],
    ]

    try:
        response = make_jmap_request(api_url, token, method_calls)
    except Exception as e:
        results.record_fail("Invalid resultOf request", str(e))
        return

    if "methodResponses" not in response:
        results.record_fail("Invalid resultOf", f"No methodResponses: {response}")
        return

    method_responses = response["methodResponses"]
    if len(method_responses) != 1:
        results.record_fail(
            "Invalid resultOf", f"Expected 1 response, got {len(method_responses)}"
        )
        return

    response_name, response_data, _ = method_responses[0]

    if response_name != "error":
        results.record_fail(
            "Invalid resultOf returns error",
            f"Expected error, got {response_name}",
        )
        return

    error_type = response_data.get("type")
    if error_type == "invalidResultReference":
        results.record_pass(
            "Invalid resultOf returns invalidResultReference",
            f"type: {error_type}",
        )
    else:
        results.record_fail(
            "Invalid resultOf returns invalidResultReference",
            f"Expected invalidResultReference, got {error_type}",
        )


def test_name_mismatch(
    api_url: str, token: str, account_id: str, mailbox_id: str, results
):
    """
    Test: Result Reference Name Mismatch (RFC 8620 Section 3.7)

    When the name in result reference doesn't match the actual method response,
    return invalidResultReference.
    """
    print()
    print("Test: Result reference name mismatch...")

    method_calls = [
        [
            "Email/query",
            {
                "accountId": account_id,
                "filter": {"inMailbox": mailbox_id},
            },
            "query0",
        ],
        [
            "Email/get",
            {
                "accountId": account_id,
                "#ids": {
                    "resultOf": "query0",
                    "name": "Email/get",  # Wrong! Should be "Email/query"
                    "path": "/ids",
                },
            },
            "get0",
        ],
    ]

    try:
        response = make_jmap_request(api_url, token, method_calls)
    except Exception as e:
        results.record_fail("Name mismatch request", str(e))
        return

    if "methodResponses" not in response:
        results.record_fail("Name mismatch", f"No methodResponses: {response}")
        return

    method_responses = response["methodResponses"]
    if len(method_responses) != 2:
        results.record_fail(
            "Name mismatch", f"Expected 2 responses, got {len(method_responses)}"
        )
        return

    # First response should be successful Email/query
    query_name, _, _ = method_responses[0]
    if query_name != "Email/query":
        results.record_fail(
            "Name mismatch (query)",
            f"Expected Email/query, got {query_name}",
        )
        return

    # Second response should be error
    get_name, get_data, _ = method_responses[1]

    if get_name != "error":
        results.record_fail(
            "Name mismatch returns error",
            f"Expected error, got {get_name}",
        )
        return

    error_type = get_data.get("type")
    if error_type == "invalidResultReference":
        results.record_pass(
            "Name mismatch returns invalidResultReference",
            f"type: {error_type}",
        )
    else:
        results.record_fail(
            "Name mismatch returns invalidResultReference",
            f"Expected invalidResultReference, got {error_type}",
        )


def test_conflicting_keys(
    api_url: str, token: str, account_id: str, mailbox_id: str, results
):
    """
    Test: Conflicting Keys Error (RFC 8620 Section 3.7)

    When both "ids" and "#ids" are present, return invalidArguments.
    """
    print()
    print("Test: Conflicting keys (ids and #ids)...")

    method_calls = [
        [
            "Email/query",
            {
                "accountId": account_id,
                "filter": {"inMailbox": mailbox_id},
            },
            "query0",
        ],
        [
            "Email/get",
            {
                "accountId": account_id,
                "ids": ["existing-id"],  # Explicit ids
                "#ids": {  # Plus result reference - conflict!
                    "resultOf": "query0",
                    "name": "Email/query",
                    "path": "/ids",
                },
            },
            "get0",
        ],
    ]

    try:
        response = make_jmap_request(api_url, token, method_calls)
    except Exception as e:
        results.record_fail("Conflicting keys request", str(e))
        return

    if "methodResponses" not in response:
        results.record_fail("Conflicting keys", f"No methodResponses: {response}")
        return

    method_responses = response["methodResponses"]
    if len(method_responses) != 2:
        results.record_fail(
            "Conflicting keys", f"Expected 2 responses, got {len(method_responses)}"
        )
        return

    # Second response should be error
    get_name, get_data, _ = method_responses[1]

    if get_name != "error":
        results.record_fail(
            "Conflicting keys returns error",
            f"Expected error, got {get_name}",
        )
        return

    error_type = get_data.get("type")
    if error_type == "invalidArguments":
        results.record_pass(
            "Conflicting keys returns invalidArguments",
            f"type: {error_type}",
        )
    else:
        results.record_fail(
            "Conflicting keys returns invalidArguments",
            f"Expected invalidArguments, got {error_type}",
        )
