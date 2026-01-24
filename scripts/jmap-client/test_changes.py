"""
State tracking JMAP tests.

Tests for Email/changes, Mailbox/changes, and Thread/changes methods per RFC 8620 Section 5.2.
"""

import uuid
from datetime import datetime, timezone, timedelta

import requests


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


def get_email_state(api_url: str, token: str, account_id: str) -> str | None:
    """Get current Email state from Email/get."""
    email_get_call = [
        "Email/get",
        {
            "accountId": account_id,
            "ids": [],  # Empty array - just getting state
        },
        "getState0",
    ]

    try:
        response = make_jmap_request(api_url, token, [email_get_call])
    except Exception:
        return None

    if "methodResponses" not in response:
        return None

    method_responses = response["methodResponses"]
    if len(method_responses) == 0:
        return None

    response_name, response_data, _ = method_responses[0]
    if response_name != "Email/get":
        return None

    return response_data.get("state")


def get_mailbox_state(api_url: str, token: str, account_id: str) -> str | None:
    """Get current Mailbox state from Mailbox/get."""
    mailbox_get_call = [
        "Mailbox/get",
        {
            "accountId": account_id,
            "ids": [],  # Empty array - just getting state
        },
        "getState0",
    ]

    try:
        response = make_jmap_request(api_url, token, [mailbox_get_call])
    except Exception:
        return None

    if "methodResponses" not in response:
        return None

    method_responses = response["methodResponses"]
    if len(method_responses) == 0:
        return None

    response_name, response_data, _ = method_responses[0]
    if response_name != "Mailbox/get":
        return None

    return response_data.get("state")


def get_thread_state(api_url: str, token: str, account_id: str) -> str | None:
    """Get current Thread state from Thread/get."""
    thread_get_call = [
        "Thread/get",
        {
            "accountId": account_id,
            "ids": [],  # Empty array - just getting state
        },
        "getState0",
    ]

    try:
        response = make_jmap_request(api_url, token, [thread_get_call])
    except Exception:
        return None

    if "methodResponses" not in response:
        return None

    method_responses = response["methodResponses"]
    if len(method_responses) == 0:
        return None

    response_name, response_data, _ = method_responses[0]
    if response_name != "Thread/get":
        return None

    return response_data.get("state")


def create_test_mailbox(api_url: str, token: str, account_id: str) -> str | None:
    """Create a test mailbox. Returns mailbox ID or None on failure."""
    unique_id = str(uuid.uuid4())[:8]
    mailbox_name = f"ChangesTest-{unique_id}"

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

    try:
        response = make_jmap_request(api_url, token, [mailbox_set_call])
    except Exception:
        return None

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
) -> str | None:
    """Import a test email. Returns email ID or None on failure."""
    unique_id = str(uuid.uuid4())
    message_id = f"<changes-test-{unique_id}@jmap-test.example>"

    received_at = datetime.now(timezone.utc)
    received_at_str = received_at.strftime("%Y-%m-%dT%H:%M:%SZ")
    date_str = received_at.strftime("%a, %d %b %Y %H:%M:%S %z")

    email_content = f"""From: Changes Test <changes-test@example.com>
To: Test Recipient <recipient@example.com>
Subject: Changes Test Email {unique_id[:8]}
Date: {date_str}
Message-ID: {message_id}
Content-Type: text/plain; charset=utf-8

This is a test email for changes tracking tests.
""".replace("\n", "\r\n")

    # Upload email as blob
    upload_endpoint = upload_url.replace("{accountId}", account_id)
    headers = {
        "Authorization": f"Bearer {token}",
        "Content-Type": "message/rfc822",
    }

    try:
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
    except Exception:
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
                    "receivedAt": received_at_str,
                }
            },
        },
        "import0",
    ]

    try:
        import_response = make_jmap_request(api_url, token, [import_call])
    except Exception:
        return None

    if "methodResponses" not in import_response:
        return None

    method_responses = import_response["methodResponses"]
    if len(method_responses) == 0:
        return None

    response_name, response_data, _ = method_responses[0]
    if response_name != "Email/import":
        return None

    created = response_data.get("created", {})
    if "email" not in created:
        return None

    return created["email"].get("id")


def import_email_with_headers(
    api_url: str,
    upload_url: str,
    token: str,
    account_id: str,
    mailbox_id: str,
    message_id: str,
    in_reply_to: str | None,
    subject: str,
    received_at: datetime,
) -> tuple[str | None, str | None]:
    """
    Import an email with specific Message-ID and In-Reply-To headers.

    Returns (email_id, thread_id) or (None, None) on failure.
    """
    received_at_str = received_at.strftime("%Y-%m-%dT%H:%M:%SZ")
    date_str = received_at.strftime("%a, %d %b %Y %H:%M:%S %z")

    # Build headers
    headers_block = f"""From: Thread Test <thread-test@example.com>
To: Test Recipient <recipient@example.com>
Subject: {subject}
Date: {date_str}
Message-ID: {message_id}"""

    if in_reply_to:
        headers_block += f"\nIn-Reply-To: {in_reply_to}"

    email_content = f"""{headers_block}
Content-Type: text/plain; charset=utf-8

This is a test email for thread changes testing.
Subject: {subject}
""".replace("\n", "\r\n")

    # Upload email as blob
    upload_endpoint = upload_url.replace("{accountId}", account_id)
    headers = {
        "Authorization": f"Bearer {token}",
        "Content-Type": "message/rfc822",
    }

    try:
        upload_response = requests.post(
            upload_endpoint,
            headers=headers,
            data=email_content.encode("utf-8"),
            timeout=30,
        )
        if upload_response.status_code != 201:
            return None, None
        upload_data = upload_response.json()
        blob_id = upload_data.get("blobId")
        if not blob_id:
            return None, None
    except Exception:
        return None, None

    # Import email
    import_call = [
        "Email/import",
        {
            "accountId": account_id,
            "emails": {
                "email": {
                    "blobId": blob_id,
                    "mailboxIds": {mailbox_id: True},
                    "receivedAt": received_at_str,
                }
            },
        },
        "import0",
    ]

    try:
        import_response = make_jmap_request(api_url, token, [import_call])
    except Exception:
        return None, None

    if "methodResponses" not in import_response:
        return None, None

    method_responses = import_response["methodResponses"]
    if len(method_responses) == 0:
        return None, None

    response_name, response_data, _ = method_responses[0]
    if response_name != "Email/import":
        return None, None

    created = response_data.get("created", {})
    if "email" not in created:
        return None, None

    email_info = created["email"]
    email_id = email_info.get("id")
    thread_id = email_info.get("threadId")

    return email_id, thread_id


def test_email_changes_response_structure(
    api_url: str, token: str, account_id: str, results
):
    """
    Test 1: Email/changes Response Structure (RFC 8620 §5.2)

    Validates response has all required fields: accountId, oldState, newState,
    hasMoreChanges, created, updated, destroyed.
    """
    print()
    print("Test: Email/changes response structure (RFC 8620 §5.2)...")

    # Get initial state
    initial_state = get_email_state(api_url, token, account_id)
    if not initial_state:
        results.record_fail(
            "Email/changes response structure", "Failed to get initial Email state"
        )
        return

    # Call Email/changes with sinceState
    changes_call = [
        "Email/changes",
        {
            "accountId": account_id,
            "sinceState": initial_state,
        },
        "changes0",
    ]

    try:
        response = make_jmap_request(api_url, token, [changes_call])
    except Exception as e:
        results.record_fail("Email/changes response structure", str(e))
        return

    if "methodResponses" not in response:
        results.record_fail(
            "Email/changes response structure",
            f"No methodResponses: {response}",
        )
        return

    method_responses = response["methodResponses"]
    if len(method_responses) == 0:
        results.record_fail(
            "Email/changes response structure", "Empty methodResponses"
        )
        return

    response_name, response_data, _ = method_responses[0]
    if response_name == "error":
        results.record_fail(
            "Email/changes response structure",
            f"JMAP error: {response_data.get('type')}: {response_data.get('description')}",
        )
        return

    if response_name != "Email/changes":
        results.record_fail(
            "Email/changes response structure",
            f"Unexpected method: {response_name}",
        )
        return

    # Validate all required fields
    errors = []

    # accountId (must match request)
    if response_data.get("accountId") == account_id:
        pass
    elif "accountId" in response_data:
        errors.append(
            f"accountId mismatch: expected {account_id}, got {response_data.get('accountId')}"
        )
    else:
        errors.append("missing accountId")

    # oldState (must echo sinceState)
    if response_data.get("oldState") == initial_state:
        pass
    elif "oldState" in response_data:
        errors.append(
            f"oldState mismatch: expected {initial_state}, got {response_data.get('oldState')}"
        )
    else:
        errors.append("missing oldState")

    # newState (must be present and be a string)
    new_state = response_data.get("newState")
    if new_state is None:
        errors.append("missing newState")
    elif not isinstance(new_state, str):
        errors.append(f"newState not a string: {type(new_state)}")

    # hasMoreChanges (must be boolean)
    has_more = response_data.get("hasMoreChanges")
    if has_more is None:
        errors.append("missing hasMoreChanges")
    elif not isinstance(has_more, bool):
        errors.append(f"hasMoreChanges not a boolean: {type(has_more)}")

    # created (must be an array)
    created = response_data.get("created")
    if created is None:
        errors.append("missing created")
    elif not isinstance(created, list):
        errors.append(f"created not an array: {type(created)}")

    # updated (must be an array)
    updated = response_data.get("updated")
    if updated is None:
        errors.append("missing updated")
    elif not isinstance(updated, list):
        errors.append(f"updated not an array: {type(updated)}")

    # destroyed (must be an array)
    destroyed = response_data.get("destroyed")
    if destroyed is None:
        errors.append("missing destroyed")
    elif not isinstance(destroyed, list):
        errors.append(f"destroyed not an array: {type(destroyed)}")

    if errors:
        results.record_fail(
            "Email/changes response structure",
            "; ".join(errors),
        )
    else:
        results.record_pass(
            "Email/changes response structure",
            f"All required fields present (oldState={initial_state[:16]}...)",
        )


def test_email_state_changes_after_import(
    api_url: str, upload_url: str, token: str, account_id: str, mailbox_id: str, results
):
    """
    Test 2: State Changes After Email/import (RFC 8620 §5.1)

    Validates that state changes after importing a new email.
    """
    print()
    print("Test: Email state changes after import (RFC 8620 §5.1)...")

    # Get initial state
    initial_state = get_email_state(api_url, token, account_id)
    if not initial_state:
        results.record_fail(
            "Email state changes after import", "Failed to get initial state"
        )
        return

    # Import a new email
    email_id = import_test_email(api_url, upload_url, token, account_id, mailbox_id)
    if not email_id:
        results.record_fail(
            "Email state changes after import", "Failed to import test email"
        )
        return

    # Get new state
    new_state = get_email_state(api_url, token, account_id)
    if not new_state:
        results.record_fail(
            "Email state changes after import", "Failed to get new state"
        )
        return

    # Validate: newState != oldState
    if new_state != initial_state:
        results.record_pass(
            "Email state changes after import",
            f"State changed from {initial_state[:16]}... to {new_state[:16]}...",
        )
    else:
        results.record_fail(
            "Email state changes after import",
            f"State did not change after import (still {initial_state[:16]}...)",
        )


def test_email_changes_returns_created(
    api_url: str, upload_url: str, token: str, account_id: str, mailbox_id: str, results
):
    """
    Test 3: Email/changes Returns Created Email (RFC 8620 §5.2)

    Validates that a newly imported email appears in the created array.
    """
    print()
    print("Test: Email/changes returns created email (RFC 8620 §5.2)...")

    # Get initial state
    initial_state = get_email_state(api_url, token, account_id)
    if not initial_state:
        results.record_fail(
            "Email/changes returns created", "Failed to get initial state"
        )
        return

    # Import a new email
    email_id = import_test_email(api_url, upload_url, token, account_id, mailbox_id)
    if not email_id:
        results.record_fail(
            "Email/changes returns created", "Failed to import test email"
        )
        return

    # Call Email/changes with initial state
    changes_call = [
        "Email/changes",
        {
            "accountId": account_id,
            "sinceState": initial_state,
        },
        "changes0",
    ]

    try:
        response = make_jmap_request(api_url, token, [changes_call])
    except Exception as e:
        results.record_fail("Email/changes returns created", str(e))
        return

    if "methodResponses" not in response:
        results.record_fail(
            "Email/changes returns created",
            f"No methodResponses: {response}",
        )
        return

    method_responses = response["methodResponses"]
    if len(method_responses) == 0:
        results.record_fail("Email/changes returns created", "Empty methodResponses")
        return

    response_name, response_data, _ = method_responses[0]
    if response_name == "error":
        results.record_fail(
            "Email/changes returns created",
            f"JMAP error: {response_data.get('type')}: {response_data.get('description')}",
        )
        return

    if response_name != "Email/changes":
        results.record_fail(
            "Email/changes returns created",
            f"Unexpected method: {response_name}",
        )
        return

    created = response_data.get("created", [])
    if email_id in created:
        results.record_pass(
            "Email/changes returns created",
            f"emailId {email_id} found in created array",
        )
    else:
        results.record_fail(
            "Email/changes returns created",
            f"emailId {email_id} not in created: {created}",
        )


def test_email_changes_max_changes_limit(
    api_url: str, upload_url: str, token: str, account_id: str, mailbox_id: str, results
):
    """
    Test 4: Email/changes maxChanges Limit (RFC 8620 §5.2)

    Validates that maxChanges limits the total IDs returned and sets hasMoreChanges.
    """
    print()
    print("Test: Email/changes maxChanges limit (RFC 8620 §5.2)...")

    # Get initial state
    initial_state = get_email_state(api_url, token, account_id)
    if not initial_state:
        results.record_fail(
            "Email/changes maxChanges limit", "Failed to get initial state"
        )
        return

    # Import 3 emails
    email_ids = []
    for _ in range(3):
        email_id = import_test_email(api_url, upload_url, token, account_id, mailbox_id)
        if not email_id:
            results.record_fail(
                "Email/changes maxChanges limit", "Failed to import test email"
            )
            return
        email_ids.append(email_id)

    # Call Email/changes with maxChanges=1
    changes_call = [
        "Email/changes",
        {
            "accountId": account_id,
            "sinceState": initial_state,
            "maxChanges": 1,
        },
        "changes0",
    ]

    try:
        response = make_jmap_request(api_url, token, [changes_call])
    except Exception as e:
        results.record_fail("Email/changes maxChanges limit", str(e))
        return

    if "methodResponses" not in response:
        results.record_fail(
            "Email/changes maxChanges limit",
            f"No methodResponses: {response}",
        )
        return

    method_responses = response["methodResponses"]
    if len(method_responses) == 0:
        results.record_fail("Email/changes maxChanges limit", "Empty methodResponses")
        return

    response_name, response_data, _ = method_responses[0]
    if response_name == "error":
        results.record_fail(
            "Email/changes maxChanges limit",
            f"JMAP error: {response_data.get('type')}: {response_data.get('description')}",
        )
        return

    if response_name != "Email/changes":
        results.record_fail(
            "Email/changes maxChanges limit",
            f"Unexpected method: {response_name}",
        )
        return

    created = response_data.get("created", [])
    updated = response_data.get("updated", [])
    destroyed = response_data.get("destroyed", [])
    has_more = response_data.get("hasMoreChanges")

    total_ids = len(created) + len(updated) + len(destroyed)

    # Validate: total IDs <= maxChanges
    if total_ids <= 1:
        results.record_pass(
            "Email/changes respects maxChanges",
            f"total IDs = {total_ids} (<= maxChanges=1)",
        )
    else:
        results.record_fail(
            "Email/changes respects maxChanges",
            f"total IDs = {total_ids} (expected <= 1)",
        )
        return

    # Validate: hasMoreChanges=true since we created 3 but limited to 1
    if has_more is True:
        results.record_pass(
            "Email/changes hasMoreChanges is true",
            "hasMoreChanges=true when more changes exist",
        )
    else:
        results.record_fail(
            "Email/changes hasMoreChanges is true",
            f"Expected hasMoreChanges=true, got {has_more}",
        )


def test_email_changes_pagination(
    api_url: str, upload_url: str, token: str, account_id: str, mailbox_id: str, results
):
    """
    Test 5: Email/changes Pagination (RFC 8620 §5.2)

    Validates that paginating through changes returns all emails eventually.
    """
    print()
    print("Test: Email/changes pagination (RFC 8620 §5.2)...")

    # Get initial state
    initial_state = get_email_state(api_url, token, account_id)
    if not initial_state:
        results.record_fail(
            "Email/changes pagination", "Failed to get initial state"
        )
        return

    # Import 3 emails
    expected_email_ids = []
    for _ in range(3):
        email_id = import_test_email(api_url, upload_url, token, account_id, mailbox_id)
        if not email_id:
            results.record_fail(
                "Email/changes pagination", "Failed to import test email"
            )
            return
        expected_email_ids.append(email_id)

    # Paginate through changes with maxChanges=1
    all_created = []
    current_state = initial_state
    iterations = 0
    max_iterations = 10  # Safety limit

    while iterations < max_iterations:
        iterations += 1

        changes_call = [
            "Email/changes",
            {
                "accountId": account_id,
                "sinceState": current_state,
                "maxChanges": 1,
            },
            f"changes{iterations}",
        ]

        try:
            response = make_jmap_request(api_url, token, [changes_call])
        except Exception as e:
            results.record_fail("Email/changes pagination", str(e))
            return

        if "methodResponses" not in response:
            results.record_fail(
                "Email/changes pagination",
                f"No methodResponses: {response}",
            )
            return

        method_responses = response["methodResponses"]
        if len(method_responses) == 0:
            results.record_fail("Email/changes pagination", "Empty methodResponses")
            return

        response_name, response_data, _ = method_responses[0]
        if response_name == "error":
            results.record_fail(
                "Email/changes pagination",
                f"JMAP error: {response_data.get('type')}: {response_data.get('description')}",
            )
            return

        if response_name != "Email/changes":
            results.record_fail(
                "Email/changes pagination",
                f"Unexpected method: {response_name}",
            )
            return

        created = response_data.get("created", [])
        all_created.extend(created)
        current_state = response_data.get("newState")
        has_more = response_data.get("hasMoreChanges", False)

        if not has_more:
            break

    # Validate: all expected emails appear in created arrays
    missing = set(expected_email_ids) - set(all_created)
    if not missing:
        results.record_pass(
            "Email/changes pagination",
            f"Found all {len(expected_email_ids)} emails after {iterations} iterations",
        )
    else:
        results.record_fail(
            "Email/changes pagination",
            f"Missing emails: {missing} (found: {all_created})",
        )


def test_email_changes_invalid_state(api_url: str, token: str, account_id: str, results):
    """
    Test 6: Email/changes cannotCalculateChanges Error (RFC 8620 §5.2)

    Validates that an invalid sinceState returns cannotCalculateChanges error.
    """
    print()
    print("Test: Email/changes cannotCalculateChanges error (RFC 8620 §5.2)...")

    changes_call = [
        "Email/changes",
        {
            "accountId": account_id,
            "sinceState": "invalid-state-string",
        },
        "changes0",
    ]

    try:
        response = make_jmap_request(api_url, token, [changes_call])
    except Exception as e:
        results.record_fail("Email/changes cannotCalculateChanges", str(e))
        return

    if "methodResponses" not in response:
        results.record_fail(
            "Email/changes cannotCalculateChanges",
            f"No methodResponses: {response}",
        )
        return

    method_responses = response["methodResponses"]
    if len(method_responses) == 0:
        results.record_fail(
            "Email/changes cannotCalculateChanges", "Empty methodResponses"
        )
        return

    response_name, response_data, _ = method_responses[0]

    if response_name == "error":
        error_type = response_data.get("type")
        if error_type == "cannotCalculateChanges":
            results.record_pass(
                "Email/changes cannotCalculateChanges",
                "Returns cannotCalculateChanges error for invalid state",
            )
        else:
            results.record_fail(
                "Email/changes cannotCalculateChanges",
                f"Expected cannotCalculateChanges, got {error_type}",
            )
    else:
        results.record_fail(
            "Email/changes cannotCalculateChanges",
            f"Expected error response, got {response_name}",
        )


def test_mailbox_changes_response_structure(
    api_url: str, token: str, account_id: str, results
):
    """
    Test 7: Mailbox/changes Response Structure (RFC 8620 §5.2 + RFC 8621 §2.2)

    Validates response has all required fields including updatedProperties.
    """
    print()
    print("Test: Mailbox/changes response structure (RFC 8620 §5.2 + RFC 8621 §2.2)...")

    # Get initial state
    initial_state = get_mailbox_state(api_url, token, account_id)
    if not initial_state:
        results.record_fail(
            "Mailbox/changes response structure", "Failed to get initial Mailbox state"
        )
        return

    # Call Mailbox/changes with sinceState
    changes_call = [
        "Mailbox/changes",
        {
            "accountId": account_id,
            "sinceState": initial_state,
        },
        "changes0",
    ]

    try:
        response = make_jmap_request(api_url, token, [changes_call])
    except Exception as e:
        results.record_fail("Mailbox/changes response structure", str(e))
        return

    if "methodResponses" not in response:
        results.record_fail(
            "Mailbox/changes response structure",
            f"No methodResponses: {response}",
        )
        return

    method_responses = response["methodResponses"]
    if len(method_responses) == 0:
        results.record_fail(
            "Mailbox/changes response structure", "Empty methodResponses"
        )
        return

    response_name, response_data, _ = method_responses[0]
    if response_name == "error":
        results.record_fail(
            "Mailbox/changes response structure",
            f"JMAP error: {response_data.get('type')}: {response_data.get('description')}",
        )
        return

    if response_name != "Mailbox/changes":
        results.record_fail(
            "Mailbox/changes response structure",
            f"Unexpected method: {response_name}",
        )
        return

    # Validate all required fields
    errors = []

    # accountId
    if response_data.get("accountId") != account_id:
        errors.append(f"accountId mismatch or missing")

    # oldState
    if response_data.get("oldState") != initial_state:
        errors.append(f"oldState mismatch or missing")

    # newState
    if not isinstance(response_data.get("newState"), str):
        errors.append("newState missing or not a string")

    # hasMoreChanges
    if not isinstance(response_data.get("hasMoreChanges"), bool):
        errors.append("hasMoreChanges missing or not a boolean")

    # created
    if not isinstance(response_data.get("created"), list):
        errors.append("created missing or not an array")

    # updated
    if not isinstance(response_data.get("updated"), list):
        errors.append("updated missing or not an array")

    # destroyed
    if not isinstance(response_data.get("destroyed"), list):
        errors.append("destroyed missing or not an array")

    # updatedProperties (may be null or array per RFC 8621 §2.2)
    updated_props = response_data.get("updatedProperties")
    if "updatedProperties" not in response_data:
        errors.append("updatedProperties missing")
    elif updated_props is not None and not isinstance(updated_props, list):
        errors.append(f"updatedProperties not null or array: {type(updated_props)}")

    if errors:
        results.record_fail(
            "Mailbox/changes response structure",
            "; ".join(errors),
        )
    else:
        results.record_pass(
            "Mailbox/changes response structure",
            f"All required fields present including updatedProperties",
        )


def test_mailbox_state_changes_after_create(
    api_url: str, token: str, account_id: str, results
):
    """
    Test 8: State Changes After Mailbox/set (RFC 8620 §5.1)

    Validates that state changes after creating a new mailbox.
    """
    print()
    print("Test: Mailbox state changes after create (RFC 8620 §5.1)...")

    # Get initial state
    initial_state = get_mailbox_state(api_url, token, account_id)
    if not initial_state:
        results.record_fail(
            "Mailbox state changes after create", "Failed to get initial state"
        )
        return

    # Create a new mailbox
    mailbox_id = create_test_mailbox(api_url, token, account_id)
    if not mailbox_id:
        results.record_fail(
            "Mailbox state changes after create", "Failed to create test mailbox"
        )
        return

    # Get new state
    new_state = get_mailbox_state(api_url, token, account_id)
    if not new_state:
        results.record_fail(
            "Mailbox state changes after create", "Failed to get new state"
        )
        return

    # Validate: newState != oldState
    if new_state != initial_state:
        results.record_pass(
            "Mailbox state changes after create",
            f"State changed from {initial_state[:16]}... to {new_state[:16]}...",
        )
    else:
        results.record_fail(
            "Mailbox state changes after create",
            f"State did not change after create (still {initial_state[:16]}...)",
        )


def test_mailbox_changes_returns_created(
    api_url: str, token: str, account_id: str, results
):
    """
    Test 9: Mailbox/changes Returns Created Mailbox (RFC 8620 §5.2)

    Validates that a newly created mailbox appears in the created array.
    """
    print()
    print("Test: Mailbox/changes returns created mailbox (RFC 8620 §5.2)...")

    # Get initial state
    initial_state = get_mailbox_state(api_url, token, account_id)
    if not initial_state:
        results.record_fail(
            "Mailbox/changes returns created", "Failed to get initial state"
        )
        return

    # Create a new mailbox
    mailbox_id = create_test_mailbox(api_url, token, account_id)
    if not mailbox_id:
        results.record_fail(
            "Mailbox/changes returns created", "Failed to create test mailbox"
        )
        return

    # Call Mailbox/changes with initial state
    changes_call = [
        "Mailbox/changes",
        {
            "accountId": account_id,
            "sinceState": initial_state,
        },
        "changes0",
    ]

    try:
        response = make_jmap_request(api_url, token, [changes_call])
    except Exception as e:
        results.record_fail("Mailbox/changes returns created", str(e))
        return

    if "methodResponses" not in response:
        results.record_fail(
            "Mailbox/changes returns created",
            f"No methodResponses: {response}",
        )
        return

    method_responses = response["methodResponses"]
    if len(method_responses) == 0:
        results.record_fail("Mailbox/changes returns created", "Empty methodResponses")
        return

    response_name, response_data, _ = method_responses[0]
    if response_name == "error":
        results.record_fail(
            "Mailbox/changes returns created",
            f"JMAP error: {response_data.get('type')}: {response_data.get('description')}",
        )
        return

    if response_name != "Mailbox/changes":
        results.record_fail(
            "Mailbox/changes returns created",
            f"Unexpected method: {response_name}",
        )
        return

    created = response_data.get("created", [])
    if mailbox_id in created:
        results.record_pass(
            "Mailbox/changes returns created",
            f"mailboxId {mailbox_id} found in created array",
        )
    else:
        results.record_fail(
            "Mailbox/changes returns created",
            f"mailboxId {mailbox_id} not in created: {created}",
        )


def test_mailbox_changes_invalid_state(
    api_url: str, token: str, account_id: str, results
):
    """
    Test 10: Mailbox/changes cannotCalculateChanges Error (RFC 8620 §5.2)

    Validates that an invalid sinceState returns cannotCalculateChanges error.
    """
    print()
    print("Test: Mailbox/changes cannotCalculateChanges error (RFC 8620 §5.2)...")

    changes_call = [
        "Mailbox/changes",
        {
            "accountId": account_id,
            "sinceState": "invalid-state-string",
        },
        "changes0",
    ]

    try:
        response = make_jmap_request(api_url, token, [changes_call])
    except Exception as e:
        results.record_fail("Mailbox/changes cannotCalculateChanges", str(e))
        return

    if "methodResponses" not in response:
        results.record_fail(
            "Mailbox/changes cannotCalculateChanges",
            f"No methodResponses: {response}",
        )
        return

    method_responses = response["methodResponses"]
    if len(method_responses) == 0:
        results.record_fail(
            "Mailbox/changes cannotCalculateChanges", "Empty methodResponses"
        )
        return

    response_name, response_data, _ = method_responses[0]

    if response_name == "error":
        error_type = response_data.get("type")
        if error_type == "cannotCalculateChanges":
            results.record_pass(
                "Mailbox/changes cannotCalculateChanges",
                "Returns cannotCalculateChanges error for invalid state",
            )
        else:
            results.record_fail(
                "Mailbox/changes cannotCalculateChanges",
                f"Expected cannotCalculateChanges, got {error_type}",
            )
    else:
        results.record_fail(
            "Mailbox/changes cannotCalculateChanges",
            f"Expected error response, got {response_name}",
        )


def test_email_changes(client, config, results):
    """
    Run all Email/changes E2E tests.

    Tests state tracking behavior per RFC 8620 Section 5.2.
    """
    print()
    print("=" * 40)
    print("Testing Email/changes (RFC 8620 §5.2)...")
    print("=" * 40)

    session = client.jmap_session

    try:
        account_id = client.account_id
    except Exception as e:
        results.record_fail("Email/changes tests setup", f"No account ID: {e}")
        return

    api_url = session.api_url
    upload_url = session.upload_url

    # Create a test mailbox for email import tests
    mailbox_id = create_test_mailbox(api_url, config.token, account_id)
    if not mailbox_id:
        results.record_fail("Email/changes tests setup", "Failed to create test mailbox")
        return

    results.record_pass("Email/changes tests mailbox created", f"mailboxId: {mailbox_id}")

    # Test 1: Response structure
    test_email_changes_response_structure(api_url, config.token, account_id, results)

    # Test 2: State changes after import
    test_email_state_changes_after_import(
        api_url, upload_url, config.token, account_id, mailbox_id, results
    )

    # Test 3: Returns created email
    test_email_changes_returns_created(
        api_url, upload_url, config.token, account_id, mailbox_id, results
    )

    # Test 4: maxChanges limit
    test_email_changes_max_changes_limit(
        api_url, upload_url, config.token, account_id, mailbox_id, results
    )

    # Test 5: Pagination
    test_email_changes_pagination(
        api_url, upload_url, config.token, account_id, mailbox_id, results
    )

    # Test 6: cannotCalculateChanges error
    test_email_changes_invalid_state(api_url, config.token, account_id, results)


def test_mailbox_changes(client, config, results):
    """
    Run all Mailbox/changes E2E tests.

    Tests state tracking behavior per RFC 8620 Section 5.2 and RFC 8621 Section 2.2.
    """
    print()
    print("=" * 40)
    print("Testing Mailbox/changes (RFC 8620 §5.2)...")
    print("=" * 40)

    session = client.jmap_session

    try:
        account_id = client.account_id
    except Exception as e:
        results.record_fail("Mailbox/changes tests setup", f"No account ID: {e}")
        return

    api_url = session.api_url

    # Test 7: Response structure
    test_mailbox_changes_response_structure(api_url, config.token, account_id, results)

    # Test 8: State changes after create
    test_mailbox_state_changes_after_create(api_url, config.token, account_id, results)

    # Test 9: Returns created mailbox
    test_mailbox_changes_returns_created(api_url, config.token, account_id, results)

    # Test 10: cannotCalculateChanges error
    test_mailbox_changes_invalid_state(api_url, config.token, account_id, results)


def test_thread_changes_response_structure(
    api_url: str, token: str, account_id: str, results
):
    """
    Test: Thread/changes Response Structure (RFC 8620 §5.2)

    Validates response has all required fields: accountId, oldState, newState,
    hasMoreChanges, created, updated, destroyed.
    """
    print()
    print("Test: Thread/changes response structure (RFC 8620 §5.2)...")

    # Get initial state
    initial_state = get_thread_state(api_url, token, account_id)
    if not initial_state:
        results.record_fail(
            "Thread/changes response structure", "Failed to get initial Thread state"
        )
        return

    # Call Thread/changes with sinceState
    changes_call = [
        "Thread/changes",
        {
            "accountId": account_id,
            "sinceState": initial_state,
        },
        "changes0",
    ]

    try:
        response = make_jmap_request(api_url, token, [changes_call])
    except Exception as e:
        results.record_fail("Thread/changes response structure", str(e))
        return

    if "methodResponses" not in response:
        results.record_fail(
            "Thread/changes response structure",
            f"No methodResponses: {response}",
        )
        return

    method_responses = response["methodResponses"]
    if len(method_responses) == 0:
        results.record_fail(
            "Thread/changes response structure", "Empty methodResponses"
        )
        return

    response_name, response_data, _ = method_responses[0]
    if response_name == "error":
        results.record_fail(
            "Thread/changes response structure",
            f"JMAP error: {response_data.get('type')}: {response_data.get('description')}",
        )
        return

    if response_name != "Thread/changes":
        results.record_fail(
            "Thread/changes response structure",
            f"Unexpected method: {response_name}",
        )
        return

    # Validate all required fields
    errors = []

    # accountId (must match request)
    if response_data.get("accountId") == account_id:
        pass
    elif "accountId" in response_data:
        errors.append(
            f"accountId mismatch: expected {account_id}, got {response_data.get('accountId')}"
        )
    else:
        errors.append("missing accountId")

    # oldState (must echo sinceState)
    if response_data.get("oldState") == initial_state:
        pass
    elif "oldState" in response_data:
        errors.append(
            f"oldState mismatch: expected {initial_state}, got {response_data.get('oldState')}"
        )
    else:
        errors.append("missing oldState")

    # newState (must be present and be a string)
    new_state = response_data.get("newState")
    if new_state is None:
        errors.append("missing newState")
    elif not isinstance(new_state, str):
        errors.append(f"newState not a string: {type(new_state)}")

    # hasMoreChanges (must be boolean)
    has_more = response_data.get("hasMoreChanges")
    if has_more is None:
        errors.append("missing hasMoreChanges")
    elif not isinstance(has_more, bool):
        errors.append(f"hasMoreChanges not a boolean: {type(has_more)}")

    # created (must be an array)
    created = response_data.get("created")
    if created is None:
        errors.append("missing created")
    elif not isinstance(created, list):
        errors.append(f"created not an array: {type(created)}")

    # updated (must be an array)
    updated = response_data.get("updated")
    if updated is None:
        errors.append("missing updated")
    elif not isinstance(updated, list):
        errors.append(f"updated not an array: {type(updated)}")

    # destroyed (must be an array)
    destroyed = response_data.get("destroyed")
    if destroyed is None:
        errors.append("missing destroyed")
    elif not isinstance(destroyed, list):
        errors.append(f"destroyed not an array: {type(destroyed)}")

    if errors:
        results.record_fail(
            "Thread/changes response structure",
            "; ".join(errors),
        )
    else:
        results.record_pass(
            "Thread/changes response structure",
            f"All required fields present (oldState={initial_state[:16]}...)",
        )


def test_thread_state_changes_after_import(
    api_url: str, upload_url: str, token: str, account_id: str, mailbox_id: str, results
):
    """
    Test: Thread State Changes After Email/import (RFC 8620 §5.1)

    Validates that Thread state changes after importing a new standalone email.
    """
    print()
    print("Test: Thread state changes after import (RFC 8620 §5.1)...")

    # Get initial state
    initial_state = get_thread_state(api_url, token, account_id)
    if not initial_state:
        results.record_fail(
            "Thread state changes after import", "Failed to get initial state"
        )
        return

    # Import a standalone email (creates new thread)
    unique_id = str(uuid.uuid4())[:8]
    message_id = f"<thread-changes-test-{unique_id}@test.example>"

    email_id, thread_id = import_email_with_headers(
        api_url=api_url,
        upload_url=upload_url,
        token=token,
        account_id=account_id,
        mailbox_id=mailbox_id,
        message_id=message_id,
        in_reply_to=None,
        subject=f"Thread Changes Test {unique_id}",
        received_at=datetime.now(timezone.utc),
    )

    if not email_id or not thread_id:
        results.record_fail(
            "Thread state changes after import", "Failed to import test email"
        )
        return

    # Get new state
    new_state = get_thread_state(api_url, token, account_id)
    if not new_state:
        results.record_fail(
            "Thread state changes after import", "Failed to get new state"
        )
        return

    # Validate: newState != oldState
    if new_state != initial_state:
        results.record_pass(
            "Thread state changes after import",
            f"State changed from {initial_state[:16]}... to {new_state[:16]}...",
        )
    else:
        results.record_fail(
            "Thread state changes after import",
            f"State did not change after import (still {initial_state[:16]}...)",
        )


def test_thread_changes_returns_created(
    api_url: str, upload_url: str, token: str, account_id: str, mailbox_id: str, results
):
    """
    Test: Thread/changes Returns Created Thread (RFC 8620 §5.2)

    Validates that a newly created thread appears in the created array.
    """
    print()
    print("Test: Thread/changes returns created thread (RFC 8620 §5.2)...")

    # Get initial state
    initial_state = get_thread_state(api_url, token, account_id)
    if not initial_state:
        results.record_fail(
            "Thread/changes returns created", "Failed to get initial state"
        )
        return

    # Import a standalone email (creates new thread)
    unique_id = str(uuid.uuid4())[:8]
    message_id = f"<thread-changes-created-{unique_id}@test.example>"

    email_id, thread_id = import_email_with_headers(
        api_url=api_url,
        upload_url=upload_url,
        token=token,
        account_id=account_id,
        mailbox_id=mailbox_id,
        message_id=message_id,
        in_reply_to=None,
        subject=f"Thread Created Test {unique_id}",
        received_at=datetime.now(timezone.utc),
    )

    if not email_id or not thread_id:
        results.record_fail(
            "Thread/changes returns created", "Failed to import test email"
        )
        return

    # Call Thread/changes with initial state
    changes_call = [
        "Thread/changes",
        {
            "accountId": account_id,
            "sinceState": initial_state,
        },
        "changes0",
    ]

    try:
        response = make_jmap_request(api_url, token, [changes_call])
    except Exception as e:
        results.record_fail("Thread/changes returns created", str(e))
        return

    if "methodResponses" not in response:
        results.record_fail(
            "Thread/changes returns created",
            f"No methodResponses: {response}",
        )
        return

    method_responses = response["methodResponses"]
    if len(method_responses) == 0:
        results.record_fail("Thread/changes returns created", "Empty methodResponses")
        return

    response_name, response_data, _ = method_responses[0]
    if response_name == "error":
        results.record_fail(
            "Thread/changes returns created",
            f"JMAP error: {response_data.get('type')}: {response_data.get('description')}",
        )
        return

    if response_name != "Thread/changes":
        results.record_fail(
            "Thread/changes returns created",
            f"Unexpected method: {response_name}",
        )
        return

    created = response_data.get("created", [])
    if thread_id in created:
        results.record_pass(
            "Thread/changes returns created",
            f"threadId {thread_id} found in created array",
        )
    else:
        results.record_fail(
            "Thread/changes returns created",
            f"threadId {thread_id} not in created: {created}",
        )


def test_thread_changes_returns_updated(
    api_url: str, upload_url: str, token: str, account_id: str, mailbox_id: str, results
):
    """
    Test: Thread/changes Returns Updated Thread (RFC 8620 §5.2)

    Validates that when a reply is added to an existing thread, the thread
    appears in the updated array.
    """
    print()
    print("Test: Thread/changes returns updated thread (RFC 8620 §5.2)...")

    # Get initial state
    initial_state = get_thread_state(api_url, token, account_id)
    if not initial_state:
        results.record_fail(
            "Thread/changes returns updated", "Failed to get initial state"
        )
        return

    # Import a parent email (creates new thread)
    unique_id = str(uuid.uuid4())[:8]
    message_id_parent = f"<thread-changes-parent-{unique_id}@test.example>"

    base_time = datetime.now(timezone.utc) - timedelta(seconds=2)

    email_id_parent, thread_id = import_email_with_headers(
        api_url=api_url,
        upload_url=upload_url,
        token=token,
        account_id=account_id,
        mailbox_id=mailbox_id,
        message_id=message_id_parent,
        in_reply_to=None,
        subject=f"Thread Updated Parent {unique_id}",
        received_at=base_time,
    )

    if not email_id_parent or not thread_id:
        results.record_fail(
            "Thread/changes returns updated", "Failed to import parent email"
        )
        return

    # Get intermediate state (after creating thread, before updating it)
    intermediate_state = get_thread_state(api_url, token, account_id)
    if not intermediate_state:
        results.record_fail(
            "Thread/changes returns updated", "Failed to get intermediate state"
        )
        return

    # Import a reply (updates existing thread)
    email_id_reply, thread_id_reply = import_email_with_headers(
        api_url=api_url,
        upload_url=upload_url,
        token=token,
        account_id=account_id,
        mailbox_id=mailbox_id,
        message_id=f"<thread-changes-reply-{unique_id}@test.example>",
        in_reply_to=message_id_parent,
        subject=f"Re: Thread Updated Parent {unique_id}",
        received_at=base_time + timedelta(seconds=1),
    )

    if not email_id_reply or not thread_id_reply:
        results.record_fail(
            "Thread/changes returns updated", "Failed to import reply email"
        )
        return

    # Verify the reply joined the same thread
    if thread_id != thread_id_reply:
        results.record_fail(
            "Thread/changes returns updated",
            f"Reply did not join parent thread: parent={thread_id}, reply={thread_id_reply}",
        )
        return

    # Call Thread/changes with intermediate state
    changes_call = [
        "Thread/changes",
        {
            "accountId": account_id,
            "sinceState": intermediate_state,
        },
        "changes0",
    ]

    try:
        response = make_jmap_request(api_url, token, [changes_call])
    except Exception as e:
        results.record_fail("Thread/changes returns updated", str(e))
        return

    if "methodResponses" not in response:
        results.record_fail(
            "Thread/changes returns updated",
            f"No methodResponses: {response}",
        )
        return

    method_responses = response["methodResponses"]
    if len(method_responses) == 0:
        results.record_fail("Thread/changes returns updated", "Empty methodResponses")
        return

    response_name, response_data, _ = method_responses[0]
    if response_name == "error":
        results.record_fail(
            "Thread/changes returns updated",
            f"JMAP error: {response_data.get('type')}: {response_data.get('description')}",
        )
        return

    if response_name != "Thread/changes":
        results.record_fail(
            "Thread/changes returns updated",
            f"Unexpected method: {response_name}",
        )
        return

    updated = response_data.get("updated", [])
    if thread_id in updated:
        results.record_pass(
            "Thread/changes returns updated",
            f"threadId {thread_id} found in updated array",
        )
    else:
        results.record_fail(
            "Thread/changes returns updated",
            f"threadId {thread_id} not in updated: {updated}",
        )


def test_thread_changes_max_changes_limit(
    api_url: str, upload_url: str, token: str, account_id: str, mailbox_id: str, results
):
    """
    Test: Thread/changes maxChanges Limit (RFC 8620 §5.2)

    Validates that maxChanges limits the total IDs returned and sets hasMoreChanges.
    """
    print()
    print("Test: Thread/changes maxChanges limit (RFC 8620 §5.2)...")

    # Get initial state
    initial_state = get_thread_state(api_url, token, account_id)
    if not initial_state:
        results.record_fail(
            "Thread/changes maxChanges limit", "Failed to get initial state"
        )
        return

    # Import 3 standalone emails (creates 3 new threads)
    thread_ids = []
    for i in range(3):
        unique_id = str(uuid.uuid4())[:8]
        message_id = f"<thread-changes-max-{i}-{unique_id}@test.example>"

        email_id, thread_id = import_email_with_headers(
            api_url=api_url,
            upload_url=upload_url,
            token=token,
            account_id=account_id,
            mailbox_id=mailbox_id,
            message_id=message_id,
            in_reply_to=None,
            subject=f"Thread Max Changes Test {i} {unique_id}",
            received_at=datetime.now(timezone.utc),
        )

        if not email_id or not thread_id:
            results.record_fail(
                "Thread/changes maxChanges limit", f"Failed to import test email {i}"
            )
            return
        thread_ids.append(thread_id)

    # Call Thread/changes with maxChanges=1
    changes_call = [
        "Thread/changes",
        {
            "accountId": account_id,
            "sinceState": initial_state,
            "maxChanges": 1,
        },
        "changes0",
    ]

    try:
        response = make_jmap_request(api_url, token, [changes_call])
    except Exception as e:
        results.record_fail("Thread/changes maxChanges limit", str(e))
        return

    if "methodResponses" not in response:
        results.record_fail(
            "Thread/changes maxChanges limit",
            f"No methodResponses: {response}",
        )
        return

    method_responses = response["methodResponses"]
    if len(method_responses) == 0:
        results.record_fail("Thread/changes maxChanges limit", "Empty methodResponses")
        return

    response_name, response_data, _ = method_responses[0]
    if response_name == "error":
        results.record_fail(
            "Thread/changes maxChanges limit",
            f"JMAP error: {response_data.get('type')}: {response_data.get('description')}",
        )
        return

    if response_name != "Thread/changes":
        results.record_fail(
            "Thread/changes maxChanges limit",
            f"Unexpected method: {response_name}",
        )
        return

    created = response_data.get("created", [])
    updated = response_data.get("updated", [])
    destroyed = response_data.get("destroyed", [])
    has_more = response_data.get("hasMoreChanges")

    total_ids = len(created) + len(updated) + len(destroyed)

    # Validate: total IDs <= maxChanges
    if total_ids <= 1:
        results.record_pass(
            "Thread/changes respects maxChanges",
            f"total IDs = {total_ids} (<= maxChanges=1)",
        )
    else:
        results.record_fail(
            "Thread/changes respects maxChanges",
            f"total IDs = {total_ids} (expected <= 1)",
        )
        return

    # Validate: hasMoreChanges=true since we created 3 but limited to 1
    if has_more is True:
        results.record_pass(
            "Thread/changes hasMoreChanges is true",
            "hasMoreChanges=true when more changes exist",
        )
    else:
        results.record_fail(
            "Thread/changes hasMoreChanges is true",
            f"Expected hasMoreChanges=true, got {has_more}",
        )


def test_thread_changes_pagination(
    api_url: str, upload_url: str, token: str, account_id: str, mailbox_id: str, results
):
    """
    Test: Thread/changes Pagination (RFC 8620 §5.2)

    Validates that paginating through changes returns all threads eventually.
    """
    print()
    print("Test: Thread/changes pagination (RFC 8620 §5.2)...")

    # Get initial state
    initial_state = get_thread_state(api_url, token, account_id)
    if not initial_state:
        results.record_fail(
            "Thread/changes pagination", "Failed to get initial state"
        )
        return

    # Import 3 standalone emails (creates 3 new threads)
    expected_thread_ids = []
    for i in range(3):
        unique_id = str(uuid.uuid4())[:8]
        message_id = f"<thread-changes-page-{i}-{unique_id}@test.example>"

        email_id, thread_id = import_email_with_headers(
            api_url=api_url,
            upload_url=upload_url,
            token=token,
            account_id=account_id,
            mailbox_id=mailbox_id,
            message_id=message_id,
            in_reply_to=None,
            subject=f"Thread Pagination Test {i} {unique_id}",
            received_at=datetime.now(timezone.utc),
        )

        if not email_id or not thread_id:
            results.record_fail(
                "Thread/changes pagination", f"Failed to import test email {i}"
            )
            return
        expected_thread_ids.append(thread_id)

    # Paginate through changes with maxChanges=1
    all_created = []
    current_state = initial_state
    iterations = 0
    max_iterations = 10  # Safety limit

    while iterations < max_iterations:
        iterations += 1

        changes_call = [
            "Thread/changes",
            {
                "accountId": account_id,
                "sinceState": current_state,
                "maxChanges": 1,
            },
            f"changes{iterations}",
        ]

        try:
            response = make_jmap_request(api_url, token, [changes_call])
        except Exception as e:
            results.record_fail("Thread/changes pagination", str(e))
            return

        if "methodResponses" not in response:
            results.record_fail(
                "Thread/changes pagination",
                f"No methodResponses: {response}",
            )
            return

        method_responses = response["methodResponses"]
        if len(method_responses) == 0:
            results.record_fail("Thread/changes pagination", "Empty methodResponses")
            return

        response_name, response_data, _ = method_responses[0]
        if response_name == "error":
            results.record_fail(
                "Thread/changes pagination",
                f"JMAP error: {response_data.get('type')}: {response_data.get('description')}",
            )
            return

        if response_name != "Thread/changes":
            results.record_fail(
                "Thread/changes pagination",
                f"Unexpected method: {response_name}",
            )
            return

        created = response_data.get("created", [])
        all_created.extend(created)
        current_state = response_data.get("newState")
        has_more = response_data.get("hasMoreChanges", False)

        if not has_more:
            break

    # Validate: all expected threads appear in created arrays
    missing = set(expected_thread_ids) - set(all_created)
    if not missing:
        results.record_pass(
            "Thread/changes pagination",
            f"Found all {len(expected_thread_ids)} threads after {iterations} iterations",
        )
    else:
        results.record_fail(
            "Thread/changes pagination",
            f"Missing threads: {missing} (found: {all_created})",
        )


def test_thread_changes_invalid_state(api_url: str, token: str, account_id: str, results):
    """
    Test: Thread/changes cannotCalculateChanges Error (RFC 8620 §5.2)

    Validates that an invalid sinceState returns cannotCalculateChanges error.
    """
    print()
    print("Test: Thread/changes cannotCalculateChanges error (RFC 8620 §5.2)...")

    changes_call = [
        "Thread/changes",
        {
            "accountId": account_id,
            "sinceState": "invalid-state-string",
        },
        "changes0",
    ]

    try:
        response = make_jmap_request(api_url, token, [changes_call])
    except Exception as e:
        results.record_fail("Thread/changes cannotCalculateChanges", str(e))
        return

    if "methodResponses" not in response:
        results.record_fail(
            "Thread/changes cannotCalculateChanges",
            f"No methodResponses: {response}",
        )
        return

    method_responses = response["methodResponses"]
    if len(method_responses) == 0:
        results.record_fail(
            "Thread/changes cannotCalculateChanges", "Empty methodResponses"
        )
        return

    response_name, response_data, _ = method_responses[0]

    if response_name == "error":
        error_type = response_data.get("type")
        if error_type == "cannotCalculateChanges":
            results.record_pass(
                "Thread/changes cannotCalculateChanges",
                "Returns cannotCalculateChanges error for invalid state",
            )
        else:
            results.record_fail(
                "Thread/changes cannotCalculateChanges",
                f"Expected cannotCalculateChanges, got {error_type}",
            )
    else:
        results.record_fail(
            "Thread/changes cannotCalculateChanges",
            f"Expected error response, got {response_name}",
        )


def test_thread_changes(client, config, results):
    """
    Run all Thread/changes E2E tests.

    Tests state tracking behavior per RFC 8620 Section 5.2 and RFC 8621 Section 3.2.
    """
    print()
    print("=" * 40)
    print("Testing Thread/changes (RFC 8620 §5.2)...")
    print("=" * 40)

    session = client.jmap_session

    try:
        account_id = client.account_id
    except Exception as e:
        results.record_fail("Thread/changes tests setup", f"No account ID: {e}")
        return

    api_url = session.api_url
    upload_url = session.upload_url

    # Create a test mailbox for email import tests
    mailbox_id = create_test_mailbox(api_url, config.token, account_id)
    if not mailbox_id:
        results.record_fail("Thread/changes tests setup", "Failed to create test mailbox")
        return

    results.record_pass("Thread/changes tests mailbox created", f"mailboxId: {mailbox_id}")

    # Test: Response structure
    test_thread_changes_response_structure(api_url, config.token, account_id, results)

    # Test: State changes after import
    test_thread_state_changes_after_import(
        api_url, upload_url, config.token, account_id, mailbox_id, results
    )

    # Test: Returns created thread
    test_thread_changes_returns_created(
        api_url, upload_url, config.token, account_id, mailbox_id, results
    )

    # Test: Returns updated thread
    test_thread_changes_returns_updated(
        api_url, upload_url, config.token, account_id, mailbox_id, results
    )

    # Test: maxChanges limit
    test_thread_changes_max_changes_limit(
        api_url, upload_url, config.token, account_id, mailbox_id, results
    )

    # Test: Pagination
    test_thread_changes_pagination(
        api_url, upload_url, config.token, account_id, mailbox_id, results
    )

    # Test: cannotCalculateChanges error
    test_thread_changes_invalid_state(api_url, config.token, account_id, results)
