"""
Email/set E2E tests.

Tests for Email/set method focusing on mailboxIds changes, mailbox counter updates,
and state tracking per RFC 8620/8621.
"""

import uuid
from datetime import datetime, timezone

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


def create_test_mailbox(api_url: str, token: str, account_id: str) -> str | None:
    """Create a test mailbox. Returns mailbox ID or None on failure."""
    unique_id = str(uuid.uuid4())[:8]
    mailbox_name = f"EmailSetTest-{unique_id}"

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
    return import_email_with_keywords(
        api_url, upload_url, token, account_id, mailbox_id, keywords=None
    )


def import_email_with_keywords(
    api_url: str,
    upload_url: str,
    token: str,
    account_id: str,
    mailbox_id: str,
    keywords: dict | None = None,
) -> str | None:
    """Import a test email with optional keywords. Returns email ID or None on failure."""
    unique_id = str(uuid.uuid4())
    message_id = f"<email-set-test-{unique_id}@jmap-test.example>"

    received_at = datetime.now(timezone.utc)
    received_at_str = received_at.strftime("%Y-%m-%dT%H:%M:%SZ")
    date_str = received_at.strftime("%a, %d %b %Y %H:%M:%S %z")

    email_content = f"""From: Email Set Test <email-set-test@example.com>
To: Test Recipient <recipient@example.com>
Subject: Email Set Test {unique_id[:8]}
Date: {date_str}
Message-ID: {message_id}
Content-Type: text/plain; charset=utf-8

This is a test email for Email/set tests.
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

    # Build import request
    email_data = {
        "blobId": blob_id,
        "mailboxIds": {mailbox_id: True},
        "receivedAt": received_at_str,
    }
    if keywords:
        email_data["keywords"] = keywords

    import_call = [
        "Email/import",
        {
            "accountId": account_id,
            "emails": {
                "email": email_data,
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


def get_mailbox_counts(
    api_url: str, token: str, account_id: str, mailbox_id: str
) -> dict | None:
    """Get totalEmails and unreadEmails for a mailbox via Mailbox/get."""
    mailbox_get_call = [
        "Mailbox/get",
        {
            "accountId": account_id,
            "ids": [mailbox_id],
            "properties": ["totalEmails", "unreadEmails"],
        },
        "getMailbox0",
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

    mailboxes = response_data.get("list", [])
    if not mailboxes:
        return None

    mailbox = mailboxes[0]
    return {
        "totalEmails": mailbox.get("totalEmails"),
        "unreadEmails": mailbox.get("unreadEmails"),
    }


def email_set_update(
    api_url: str,
    token: str,
    account_id: str,
    email_id: str,
    update_patch: dict,
    if_in_state: str | None = None,
) -> dict:
    """Call Email/set with update operation. Returns full response data."""
    email_set_args = {
        "accountId": account_id,
        "update": {
            email_id: update_patch,
        },
    }
    if if_in_state:
        email_set_args["ifInState"] = if_in_state

    email_set_call = [
        "Email/set",
        email_set_args,
        "emailSet0",
    ]

    return make_jmap_request(api_url, token, [email_set_call])


def get_email_mailbox_ids(
    api_url: str, token: str, account_id: str, email_id: str
) -> dict | None:
    """Get the mailboxIds for an email via Email/get."""
    email_get_call = [
        "Email/get",
        {
            "accountId": account_id,
            "ids": [email_id],
            "properties": ["mailboxIds"],
        },
        "getEmail0",
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

    emails = response_data.get("list", [])
    if not emails:
        return None

    return emails[0].get("mailboxIds")


# =============================================================================
# Test: mailboxIds Changes (RFC 8621 Section 4.6)
# =============================================================================


def test_move_email_between_mailboxes(
    api_url: str,
    upload_url: str,
    token: str,
    account_id: str,
    results,
):
    """
    Test: Move email between mailboxes by replacing mailboxIds.

    RFC 8621 Section 4.6: mailboxIds is updatable.
    """
    print()
    print("Test: Move email between mailboxes (RFC 8621 Section 4.6)...")

    # Create two mailboxes
    mailbox_a = create_test_mailbox(api_url, token, account_id)
    if not mailbox_a:
        results.record_fail("Move email test setup", "Failed to create mailbox A")
        return

    mailbox_b = create_test_mailbox(api_url, token, account_id)
    if not mailbox_b:
        results.record_fail("Move email test setup", "Failed to create mailbox B")
        return

    # Import email to mailbox A
    email_id = import_test_email(api_url, upload_url, token, account_id, mailbox_a)
    if not email_id:
        results.record_fail("Move email test setup", "Failed to import email")
        return

    # Move email to mailbox B by replacing mailboxIds
    try:
        response = email_set_update(
            api_url,
            token,
            account_id,
            email_id,
            {"mailboxIds": {mailbox_b: True}},
        )
    except Exception as e:
        results.record_fail("Move email Email/set request", str(e))
        return

    if "methodResponses" not in response:
        results.record_fail("Move email response", f"No methodResponses: {response}")
        return

    method_responses = response["methodResponses"]
    if len(method_responses) == 0:
        results.record_fail("Move email response", "Empty methodResponses")
        return

    response_name, response_data, _ = method_responses[0]
    if response_name == "error":
        results.record_fail(
            "Move email request",
            f"JMAP error: {response_data.get('type')}: {response_data.get('description')}",
        )
        return

    if response_name != "Email/set":
        results.record_fail("Move email response", f"Unexpected method: {response_name}")
        return

    # Check email was updated successfully
    updated = response_data.get("updated", {})
    not_updated = response_data.get("notUpdated", {})

    if email_id in updated or (email_id in updated if updated else False):
        # Verify the email is now only in mailbox B
        mailbox_ids = get_email_mailbox_ids(api_url, token, account_id, email_id)
        if mailbox_ids and mailbox_b in mailbox_ids and mailbox_a not in mailbox_ids:
            results.record_pass(
                "Move email between mailboxes",
                f"Email moved from {mailbox_a} to {mailbox_b}",
            )
        else:
            results.record_fail(
                "Move email between mailboxes",
                f"Expected only {mailbox_b}, got {mailbox_ids}",
            )
    elif email_id in not_updated:
        error = not_updated[email_id]
        results.record_fail(
            "Move email between mailboxes",
            f"Not updated: {error.get('type')}: {error.get('description')}",
        )
    else:
        results.record_fail(
            "Move email between mailboxes",
            f"Email not in updated or notUpdated: {response_data}",
        )


def test_add_email_to_additional_mailbox(
    api_url: str,
    upload_url: str,
    token: str,
    account_id: str,
    results,
):
    """
    Test: Add email to additional mailbox using patch syntax.

    RFC 8620 Section 5.3: Use "mailboxIds/newId": true to add.
    """
    print()
    print("Test: Add email to additional mailbox (RFC 8620 Section 5.3)...")

    # Create two mailboxes
    mailbox_a = create_test_mailbox(api_url, token, account_id)
    if not mailbox_a:
        results.record_fail(
            "Add to mailbox test setup", "Failed to create mailbox A"
        )
        return

    mailbox_b = create_test_mailbox(api_url, token, account_id)
    if not mailbox_b:
        results.record_fail(
            "Add to mailbox test setup", "Failed to create mailbox B"
        )
        return

    # Import email to mailbox A
    email_id = import_test_email(api_url, upload_url, token, account_id, mailbox_a)
    if not email_id:
        results.record_fail("Add to mailbox test setup", "Failed to import email")
        return

    # Add email to mailbox B using patch syntax
    try:
        response = email_set_update(
            api_url,
            token,
            account_id,
            email_id,
            {f"mailboxIds/{mailbox_b}": True},
        )
    except Exception as e:
        results.record_fail("Add to mailbox Email/set request", str(e))
        return

    if "methodResponses" not in response:
        results.record_fail(
            "Add to mailbox response", f"No methodResponses: {response}"
        )
        return

    method_responses = response["methodResponses"]
    if len(method_responses) == 0:
        results.record_fail("Add to mailbox response", "Empty methodResponses")
        return

    response_name, response_data, _ = method_responses[0]
    if response_name == "error":
        results.record_fail(
            "Add to mailbox request",
            f"JMAP error: {response_data.get('type')}: {response_data.get('description')}",
        )
        return

    if response_name != "Email/set":
        results.record_fail(
            "Add to mailbox response", f"Unexpected method: {response_name}"
        )
        return

    updated = response_data.get("updated", {})
    not_updated = response_data.get("notUpdated", {})

    if email_id in updated:
        # Verify the email is now in both mailboxes
        mailbox_ids = get_email_mailbox_ids(api_url, token, account_id, email_id)
        if mailbox_ids and mailbox_a in mailbox_ids and mailbox_b in mailbox_ids:
            results.record_pass(
                "Add email to additional mailbox",
                f"Email now in both {mailbox_a} and {mailbox_b}",
            )
        else:
            results.record_fail(
                "Add email to additional mailbox",
                f"Expected both {mailbox_a} and {mailbox_b}, got {mailbox_ids}",
            )
    elif email_id in not_updated:
        error = not_updated[email_id]
        results.record_fail(
            "Add email to additional mailbox",
            f"Not updated: {error.get('type')}: {error.get('description')}",
        )
    else:
        results.record_fail(
            "Add email to additional mailbox",
            f"Email not in updated or notUpdated: {response_data}",
        )


def test_remove_email_from_one_mailbox(
    api_url: str,
    upload_url: str,
    token: str,
    account_id: str,
    results,
):
    """
    Test: Remove email from one mailbox using patch syntax (must remain in at least one).

    RFC 8620 Section 5.3: Use "mailboxIds/oldId": null to remove.
    """
    print()
    print("Test: Remove email from one mailbox (RFC 8620 Section 5.3)...")

    # Create two mailboxes
    mailbox_a = create_test_mailbox(api_url, token, account_id)
    if not mailbox_a:
        results.record_fail(
            "Remove from mailbox test setup", "Failed to create mailbox A"
        )
        return

    mailbox_b = create_test_mailbox(api_url, token, account_id)
    if not mailbox_b:
        results.record_fail(
            "Remove from mailbox test setup", "Failed to create mailbox B"
        )
        return

    # Import email to mailbox A
    email_id = import_test_email(api_url, upload_url, token, account_id, mailbox_a)
    if not email_id:
        results.record_fail("Remove from mailbox test setup", "Failed to import email")
        return

    # First add to mailbox B
    try:
        response = email_set_update(
            api_url,
            token,
            account_id,
            email_id,
            {f"mailboxIds/{mailbox_b}": True},
        )
    except Exception as e:
        results.record_fail("Remove from mailbox test setup (add to B)", str(e))
        return

    if "methodResponses" not in response:
        results.record_fail(
            "Remove from mailbox test setup", "Failed to add to mailbox B"
        )
        return

    # Now remove from mailbox A using patch syntax
    try:
        response = email_set_update(
            api_url,
            token,
            account_id,
            email_id,
            {f"mailboxIds/{mailbox_a}": None},
        )
    except Exception as e:
        results.record_fail("Remove from mailbox Email/set request", str(e))
        return

    if "methodResponses" not in response:
        results.record_fail(
            "Remove from mailbox response", f"No methodResponses: {response}"
        )
        return

    method_responses = response["methodResponses"]
    if len(method_responses) == 0:
        results.record_fail("Remove from mailbox response", "Empty methodResponses")
        return

    response_name, response_data, _ = method_responses[0]
    if response_name == "error":
        results.record_fail(
            "Remove from mailbox request",
            f"JMAP error: {response_data.get('type')}: {response_data.get('description')}",
        )
        return

    if response_name != "Email/set":
        results.record_fail(
            "Remove from mailbox response", f"Unexpected method: {response_name}"
        )
        return

    updated = response_data.get("updated", {})
    not_updated = response_data.get("notUpdated", {})

    if email_id in updated:
        # Verify the email is now only in mailbox B
        mailbox_ids = get_email_mailbox_ids(api_url, token, account_id, email_id)
        if mailbox_ids and mailbox_b in mailbox_ids and mailbox_a not in mailbox_ids:
            results.record_pass(
                "Remove email from one mailbox",
                f"Email now only in {mailbox_b}",
            )
        else:
            results.record_fail(
                "Remove email from one mailbox",
                f"Expected only {mailbox_b}, got {mailbox_ids}",
            )
    elif email_id in not_updated:
        error = not_updated[email_id]
        results.record_fail(
            "Remove email from one mailbox",
            f"Not updated: {error.get('type')}: {error.get('description')}",
        )
    else:
        results.record_fail(
            "Remove email from one mailbox",
            f"Email not in updated or notUpdated: {response_data}",
        )


def test_remove_all_mailboxes_error(
    api_url: str,
    upload_url: str,
    token: str,
    account_id: str,
    results,
):
    """
    Test: Setting mailboxIds to empty returns invalidProperties error.

    RFC 8621 Section 4.6: Email MUST be in at least one mailbox.
    """
    print()
    print("Test: Remove all mailboxes returns error (RFC 8621 Section 4.6)...")

    # Create mailbox and import email
    mailbox_id = create_test_mailbox(api_url, token, account_id)
    if not mailbox_id:
        results.record_fail(
            "Remove all mailboxes test setup", "Failed to create mailbox"
        )
        return

    email_id = import_test_email(api_url, upload_url, token, account_id, mailbox_id)
    if not email_id:
        results.record_fail(
            "Remove all mailboxes test setup", "Failed to import email"
        )
        return

    # Try to set mailboxIds to empty
    try:
        response = email_set_update(
            api_url,
            token,
            account_id,
            email_id,
            {"mailboxIds": {}},
        )
    except Exception as e:
        results.record_fail("Remove all mailboxes Email/set request", str(e))
        return

    if "methodResponses" not in response:
        results.record_fail(
            "Remove all mailboxes response", f"No methodResponses: {response}"
        )
        return

    method_responses = response["methodResponses"]
    if len(method_responses) == 0:
        results.record_fail("Remove all mailboxes response", "Empty methodResponses")
        return

    response_name, response_data, _ = method_responses[0]
    if response_name == "error":
        # Method-level error - also acceptable
        error_type = response_data.get("type")
        results.record_pass(
            "Remove all mailboxes returns error",
            f"Method error: {error_type}",
        )
        return

    if response_name != "Email/set":
        results.record_fail(
            "Remove all mailboxes response", f"Unexpected method: {response_name}"
        )
        return

    # Check for notUpdated with invalidProperties
    not_updated = response_data.get("notUpdated", {})
    if email_id in not_updated:
        error = not_updated[email_id]
        error_type = error.get("type")
        if error_type == "invalidProperties":
            results.record_pass(
                "Remove all mailboxes returns invalidProperties",
                f"Error: {error.get('description', '')}",
            )
        else:
            results.record_pass(
                "Remove all mailboxes returns error",
                f"Error type: {error_type}: {error.get('description', '')}",
            )
    else:
        # If email was somehow updated, that's wrong
        updated = response_data.get("updated", {})
        if email_id in updated:
            results.record_fail(
                "Remove all mailboxes returns error",
                "Email was updated with empty mailboxIds (should have failed)",
            )
        else:
            results.record_fail(
                "Remove all mailboxes response",
                f"Email not in updated or notUpdated: {response_data}",
            )


# =============================================================================
# Test: Mailbox Counter Updates (RFC 8621 Section 2)
# =============================================================================


def test_total_emails_increment_on_add(
    api_url: str,
    upload_url: str,
    token: str,
    account_id: str,
    results,
):
    """
    Test: totalEmails increases when email added to mailbox.

    RFC 8621 Section 2: totalEmails is the count of emails in mailbox.
    """
    print()
    print("Test: totalEmails increments on add (RFC 8621 Section 2)...")

    # Create two mailboxes
    mailbox_a = create_test_mailbox(api_url, token, account_id)
    if not mailbox_a:
        results.record_fail(
            "totalEmails increment test setup", "Failed to create mailbox A"
        )
        return

    mailbox_b = create_test_mailbox(api_url, token, account_id)
    if not mailbox_b:
        results.record_fail(
            "totalEmails increment test setup", "Failed to create mailbox B"
        )
        return

    # Import email to mailbox A
    email_id = import_test_email(api_url, upload_url, token, account_id, mailbox_a)
    if not email_id:
        results.record_fail(
            "totalEmails increment test setup", "Failed to import email"
        )
        return

    # Get initial counts for mailbox B
    initial_counts = get_mailbox_counts(api_url, token, account_id, mailbox_b)
    if initial_counts is None:
        results.record_fail(
            "totalEmails increment test setup", "Failed to get initial counts for B"
        )
        return

    initial_total = initial_counts.get("totalEmails", 0)

    # Add email to mailbox B
    try:
        response = email_set_update(
            api_url,
            token,
            account_id,
            email_id,
            {f"mailboxIds/{mailbox_b}": True},
        )
    except Exception as e:
        results.record_fail("totalEmails increment Email/set request", str(e))
        return

    if "methodResponses" not in response:
        results.record_fail(
            "totalEmails increment response", f"No methodResponses: {response}"
        )
        return

    # Get new counts for mailbox B
    new_counts = get_mailbox_counts(api_url, token, account_id, mailbox_b)
    if new_counts is None:
        results.record_fail(
            "totalEmails increment test", "Failed to get new counts for B"
        )
        return

    new_total = new_counts.get("totalEmails", 0)

    if new_total == initial_total + 1:
        results.record_pass(
            "totalEmails increments on add",
            f"totalEmails: {initial_total} -> {new_total}",
        )
    else:
        results.record_fail(
            "totalEmails increments on add",
            f"Expected {initial_total + 1}, got {new_total}",
        )


def test_total_emails_decrement_on_remove(
    api_url: str,
    upload_url: str,
    token: str,
    account_id: str,
    results,
):
    """
    Test: totalEmails decreases when email removed from mailbox.

    RFC 8621 Section 2: totalEmails is the count of emails in mailbox.
    """
    print()
    print("Test: totalEmails decrements on remove (RFC 8621 Section 2)...")

    # Create two mailboxes
    mailbox_a = create_test_mailbox(api_url, token, account_id)
    if not mailbox_a:
        results.record_fail(
            "totalEmails decrement test setup", "Failed to create mailbox A"
        )
        return

    mailbox_b = create_test_mailbox(api_url, token, account_id)
    if not mailbox_b:
        results.record_fail(
            "totalEmails decrement test setup", "Failed to create mailbox B"
        )
        return

    # Import email to mailbox A
    email_id = import_test_email(api_url, upload_url, token, account_id, mailbox_a)
    if not email_id:
        results.record_fail(
            "totalEmails decrement test setup", "Failed to import email"
        )
        return

    # Add to mailbox B first
    try:
        response = email_set_update(
            api_url,
            token,
            account_id,
            email_id,
            {f"mailboxIds/{mailbox_b}": True},
        )
    except Exception as e:
        results.record_fail("totalEmails decrement test setup (add to B)", str(e))
        return

    # Get counts for mailbox A before removal
    initial_counts = get_mailbox_counts(api_url, token, account_id, mailbox_a)
    if initial_counts is None:
        results.record_fail(
            "totalEmails decrement test setup", "Failed to get initial counts for A"
        )
        return

    initial_total = initial_counts.get("totalEmails", 0)

    # Remove from mailbox A
    try:
        response = email_set_update(
            api_url,
            token,
            account_id,
            email_id,
            {f"mailboxIds/{mailbox_a}": None},
        )
    except Exception as e:
        results.record_fail("totalEmails decrement Email/set request", str(e))
        return

    if "methodResponses" not in response:
        results.record_fail(
            "totalEmails decrement response", f"No methodResponses: {response}"
        )
        return

    # Get new counts for mailbox A
    new_counts = get_mailbox_counts(api_url, token, account_id, mailbox_a)
    if new_counts is None:
        results.record_fail(
            "totalEmails decrement test", "Failed to get new counts for A"
        )
        return

    new_total = new_counts.get("totalEmails", 0)

    if new_total == initial_total - 1:
        results.record_pass(
            "totalEmails decrements on remove",
            f"totalEmails: {initial_total} -> {new_total}",
        )
    else:
        results.record_fail(
            "totalEmails decrements on remove",
            f"Expected {initial_total - 1}, got {new_total}",
        )


def test_unread_emails_update_on_move(
    api_url: str,
    upload_url: str,
    token: str,
    account_id: str,
    results,
):
    """
    Test: unreadEmails adjusts when unread email moves between mailboxes.

    RFC 8621 Section 2: unreadEmails counts emails without $seen keyword.
    """
    print()
    print("Test: unreadEmails updates on move (RFC 8621 Section 2)...")

    # Create two mailboxes
    mailbox_a = create_test_mailbox(api_url, token, account_id)
    if not mailbox_a:
        results.record_fail(
            "unreadEmails update test setup", "Failed to create mailbox A"
        )
        return

    mailbox_b = create_test_mailbox(api_url, token, account_id)
    if not mailbox_b:
        results.record_fail(
            "unreadEmails update test setup", "Failed to create mailbox B"
        )
        return

    # Import UNREAD email to mailbox A (no $seen keyword)
    email_id = import_email_with_keywords(
        api_url, upload_url, token, account_id, mailbox_a, keywords={}
    )
    if not email_id:
        results.record_fail(
            "unreadEmails update test setup", "Failed to import unread email"
        )
        return

    # Get initial unread counts
    initial_counts_a = get_mailbox_counts(api_url, token, account_id, mailbox_a)
    initial_counts_b = get_mailbox_counts(api_url, token, account_id, mailbox_b)

    if initial_counts_a is None or initial_counts_b is None:
        results.record_fail(
            "unreadEmails update test setup", "Failed to get initial counts"
        )
        return

    initial_unread_a = initial_counts_a.get("unreadEmails", 0)
    initial_unread_b = initial_counts_b.get("unreadEmails", 0)

    # Move email from A to B
    try:
        response = email_set_update(
            api_url,
            token,
            account_id,
            email_id,
            {"mailboxIds": {mailbox_b: True}},
        )
    except Exception as e:
        results.record_fail("unreadEmails update Email/set request", str(e))
        return

    if "methodResponses" not in response:
        results.record_fail(
            "unreadEmails update response", f"No methodResponses: {response}"
        )
        return

    # Get new unread counts
    new_counts_a = get_mailbox_counts(api_url, token, account_id, mailbox_a)
    new_counts_b = get_mailbox_counts(api_url, token, account_id, mailbox_b)

    if new_counts_a is None or new_counts_b is None:
        results.record_fail(
            "unreadEmails update test", "Failed to get new counts"
        )
        return

    new_unread_a = new_counts_a.get("unreadEmails", 0)
    new_unread_b = new_counts_b.get("unreadEmails", 0)

    # Verify unread decreased in A and increased in B
    a_decreased = new_unread_a == initial_unread_a - 1
    b_increased = new_unread_b == initial_unread_b + 1

    if a_decreased and b_increased:
        results.record_pass(
            "unreadEmails updates on move",
            f"A: {initial_unread_a} -> {new_unread_a}, B: {initial_unread_b} -> {new_unread_b}",
        )
    else:
        results.record_fail(
            "unreadEmails updates on move",
            f"A: {initial_unread_a} -> {new_unread_a} (expected -1), "
            f"B: {initial_unread_b} -> {new_unread_b} (expected +1)",
        )


def test_read_email_no_unread_change(
    api_url: str,
    upload_url: str,
    token: str,
    account_id: str,
    results,
):
    """
    Test: Moving email with $seen keyword doesn't change unreadEmails.

    RFC 8621 Section 2: unreadEmails excludes emails with $seen keyword.
    """
    print()
    print("Test: Read email move doesn't change unreadEmails (RFC 8621 Section 2)...")

    # Create two mailboxes
    mailbox_a = create_test_mailbox(api_url, token, account_id)
    if not mailbox_a:
        results.record_fail(
            "Read email move test setup", "Failed to create mailbox A"
        )
        return

    mailbox_b = create_test_mailbox(api_url, token, account_id)
    if not mailbox_b:
        results.record_fail(
            "Read email move test setup", "Failed to create mailbox B"
        )
        return

    # Import READ email (with $seen keyword)
    email_id = import_email_with_keywords(
        api_url, upload_url, token, account_id, mailbox_a, keywords={"$seen": True}
    )
    if not email_id:
        results.record_fail(
            "Read email move test setup", "Failed to import read email"
        )
        return

    # Get initial unread counts
    initial_counts_a = get_mailbox_counts(api_url, token, account_id, mailbox_a)
    initial_counts_b = get_mailbox_counts(api_url, token, account_id, mailbox_b)

    if initial_counts_a is None or initial_counts_b is None:
        results.record_fail(
            "Read email move test setup", "Failed to get initial counts"
        )
        return

    initial_unread_a = initial_counts_a.get("unreadEmails", 0)
    initial_unread_b = initial_counts_b.get("unreadEmails", 0)

    # Move email from A to B
    try:
        response = email_set_update(
            api_url,
            token,
            account_id,
            email_id,
            {"mailboxIds": {mailbox_b: True}},
        )
    except Exception as e:
        results.record_fail("Read email move Email/set request", str(e))
        return

    if "methodResponses" not in response:
        results.record_fail(
            "Read email move response", f"No methodResponses: {response}"
        )
        return

    # Get new unread counts
    new_counts_a = get_mailbox_counts(api_url, token, account_id, mailbox_a)
    new_counts_b = get_mailbox_counts(api_url, token, account_id, mailbox_b)

    if new_counts_a is None or new_counts_b is None:
        results.record_fail(
            "Read email move test", "Failed to get new counts"
        )
        return

    new_unread_a = new_counts_a.get("unreadEmails", 0)
    new_unread_b = new_counts_b.get("unreadEmails", 0)

    # Verify unread counts didn't change
    if new_unread_a == initial_unread_a and new_unread_b == initial_unread_b:
        results.record_pass(
            "Read email move doesn't change unreadEmails",
            f"A: {initial_unread_a} -> {new_unread_a}, B: {initial_unread_b} -> {new_unread_b}",
        )
    else:
        results.record_fail(
            "Read email move doesn't change unreadEmails",
            f"A: {initial_unread_a} -> {new_unread_a}, B: {initial_unread_b} -> {new_unread_b}",
        )


# =============================================================================
# Test: State Tracking (RFC 8620 Section 5.3)
# =============================================================================


def test_email_set_returns_old_and_new_state(
    api_url: str,
    upload_url: str,
    token: str,
    account_id: str,
    results,
):
    """
    Test: Email/set response includes oldState and newState.

    RFC 8620 Section 5.3: /set response MUST include oldState and newState.
    """
    print()
    print("Test: Email/set returns oldState and newState (RFC 8620 Section 5.3)...")

    # Create mailboxes and import email
    mailbox_a = create_test_mailbox(api_url, token, account_id)
    mailbox_b = create_test_mailbox(api_url, token, account_id)
    if not mailbox_a or not mailbox_b:
        results.record_fail(
            "State tracking test setup", "Failed to create mailboxes"
        )
        return

    email_id = import_test_email(api_url, upload_url, token, account_id, mailbox_a)
    if not email_id:
        results.record_fail(
            "State tracking test setup", "Failed to import email"
        )
        return

    # Make an Email/set update
    try:
        response = email_set_update(
            api_url,
            token,
            account_id,
            email_id,
            {f"mailboxIds/{mailbox_b}": True},
        )
    except Exception as e:
        results.record_fail("State tracking Email/set request", str(e))
        return

    if "methodResponses" not in response:
        results.record_fail(
            "State tracking response", f"No methodResponses: {response}"
        )
        return

    method_responses = response["methodResponses"]
    if len(method_responses) == 0:
        results.record_fail("State tracking response", "Empty methodResponses")
        return

    response_name, response_data, _ = method_responses[0]
    if response_name == "error":
        results.record_fail(
            "State tracking request",
            f"JMAP error: {response_data.get('type')}: {response_data.get('description')}",
        )
        return

    if response_name != "Email/set":
        results.record_fail(
            "State tracking response", f"Unexpected method: {response_name}"
        )
        return

    # Check for oldState and newState
    old_state = response_data.get("oldState")
    new_state = response_data.get("newState")

    errors = []
    if old_state is None:
        errors.append("missing oldState")
    elif not isinstance(old_state, str):
        errors.append(f"oldState not a string: {type(old_state)}")

    if new_state is None:
        errors.append("missing newState")
    elif not isinstance(new_state, str):
        errors.append(f"newState not a string: {type(new_state)}")

    if errors:
        results.record_fail(
            "Email/set returns oldState and newState",
            "; ".join(errors),
        )
    else:
        results.record_pass(
            "Email/set returns oldState and newState",
            f"oldState={old_state[:16]}..., newState={new_state[:16]}...",
        )


def test_new_state_differs_after_update(
    api_url: str,
    upload_url: str,
    token: str,
    account_id: str,
    results,
):
    """
    Test: newState differs from oldState after successful update.

    RFC 8620 Section 5.3: State MUST change when data changes.
    """
    print()
    print("Test: newState differs after update (RFC 8620 Section 5.3)...")

    # Create mailboxes and import email
    mailbox_a = create_test_mailbox(api_url, token, account_id)
    mailbox_b = create_test_mailbox(api_url, token, account_id)
    if not mailbox_a or not mailbox_b:
        results.record_fail(
            "State differs test setup", "Failed to create mailboxes"
        )
        return

    email_id = import_test_email(api_url, upload_url, token, account_id, mailbox_a)
    if not email_id:
        results.record_fail(
            "State differs test setup", "Failed to import email"
        )
        return

    # Make an Email/set update
    try:
        response = email_set_update(
            api_url,
            token,
            account_id,
            email_id,
            {f"mailboxIds/{mailbox_b}": True},
        )
    except Exception as e:
        results.record_fail("State differs Email/set request", str(e))
        return

    if "methodResponses" not in response:
        results.record_fail(
            "State differs response", f"No methodResponses: {response}"
        )
        return

    method_responses = response["methodResponses"]
    if len(method_responses) == 0:
        results.record_fail("State differs response", "Empty methodResponses")
        return

    response_name, response_data, _ = method_responses[0]
    if response_name == "error":
        results.record_fail(
            "State differs request",
            f"JMAP error: {response_data.get('type')}: {response_data.get('description')}",
        )
        return

    if response_name != "Email/set":
        results.record_fail(
            "State differs response", f"Unexpected method: {response_name}"
        )
        return

    old_state = response_data.get("oldState")
    new_state = response_data.get("newState")

    if old_state is None or new_state is None:
        results.record_fail(
            "newState differs after update",
            f"Missing state: oldState={old_state}, newState={new_state}",
        )
        return

    if new_state != old_state:
        results.record_pass(
            "newState differs after update",
            f"oldState={old_state[:16]}..., newState={new_state[:16]}...",
        )
    else:
        results.record_fail(
            "newState differs after update",
            f"State unchanged: {old_state}",
        )


def test_if_in_state_success(
    api_url: str,
    upload_url: str,
    token: str,
    account_id: str,
    results,
):
    """
    Test: Request with correct ifInState succeeds.

    RFC 8620 Section 5.3: ifInState is a precondition check.
    """
    print()
    print("Test: ifInState success (RFC 8620 Section 5.3)...")

    # Create mailboxes
    mailbox_a = create_test_mailbox(api_url, token, account_id)
    mailbox_b = create_test_mailbox(api_url, token, account_id)
    if not mailbox_a or not mailbox_b:
        results.record_fail(
            "ifInState success test setup", "Failed to create mailboxes"
        )
        return

    email_id = import_test_email(api_url, upload_url, token, account_id, mailbox_a)
    if not email_id:
        results.record_fail(
            "ifInState success test setup", "Failed to import email"
        )
        return

    # Get current state
    current_state = get_email_state(api_url, token, account_id)
    if not current_state:
        results.record_fail(
            "ifInState success test setup", "Failed to get current state"
        )
        return

    # Make request with correct ifInState
    try:
        response = email_set_update(
            api_url,
            token,
            account_id,
            email_id,
            {f"mailboxIds/{mailbox_b}": True},
            if_in_state=current_state,
        )
    except Exception as e:
        results.record_fail("ifInState success Email/set request", str(e))
        return

    if "methodResponses" not in response:
        results.record_fail(
            "ifInState success response", f"No methodResponses: {response}"
        )
        return

    method_responses = response["methodResponses"]
    if len(method_responses) == 0:
        results.record_fail("ifInState success response", "Empty methodResponses")
        return

    response_name, response_data, _ = method_responses[0]
    if response_name == "error":
        error_type = response_data.get("type")
        if error_type == "stateMismatch":
            results.record_fail(
                "ifInState success",
                "Got stateMismatch error with correct state",
            )
        else:
            results.record_fail(
                "ifInState success",
                f"JMAP error: {error_type}: {response_data.get('description')}",
            )
        return

    if response_name != "Email/set":
        results.record_fail(
            "ifInState success response", f"Unexpected method: {response_name}"
        )
        return

    # Check update succeeded
    updated = response_data.get("updated", {})
    if email_id in updated:
        results.record_pass(
            "ifInState success",
            f"Update succeeded with ifInState={current_state[:16]}...",
        )
    else:
        not_updated = response_data.get("notUpdated", {})
        if email_id in not_updated:
            error = not_updated[email_id]
            results.record_fail(
                "ifInState success",
                f"Not updated: {error.get('type')}: {error.get('description')}",
            )
        else:
            results.record_fail(
                "ifInState success",
                f"Email not in updated or notUpdated: {response_data}",
            )


def test_if_in_state_mismatch_error(
    api_url: str,
    upload_url: str,
    token: str,
    account_id: str,
    results,
):
    """
    Test: Wrong ifInState returns stateMismatch error, no changes applied.

    RFC 8620 Section 5.3: If state doesn't match, return stateMismatch error.
    """
    print()
    print("Test: ifInState mismatch error (RFC 8620 Section 5.3)...")

    # Create mailboxes
    mailbox_a = create_test_mailbox(api_url, token, account_id)
    mailbox_b = create_test_mailbox(api_url, token, account_id)
    if not mailbox_a or not mailbox_b:
        results.record_fail(
            "ifInState mismatch test setup", "Failed to create mailboxes"
        )
        return

    email_id = import_test_email(api_url, upload_url, token, account_id, mailbox_a)
    if not email_id:
        results.record_fail(
            "ifInState mismatch test setup", "Failed to import email"
        )
        return

    # Make request with wrong ifInState
    wrong_state = "wrong-state-value-12345"
    try:
        response = email_set_update(
            api_url,
            token,
            account_id,
            email_id,
            {f"mailboxIds/{mailbox_b}": True},
            if_in_state=wrong_state,
        )
    except Exception as e:
        results.record_fail("ifInState mismatch Email/set request", str(e))
        return

    if "methodResponses" not in response:
        results.record_fail(
            "ifInState mismatch response", f"No methodResponses: {response}"
        )
        return

    method_responses = response["methodResponses"]
    if len(method_responses) == 0:
        results.record_fail("ifInState mismatch response", "Empty methodResponses")
        return

    response_name, response_data, _ = method_responses[0]

    if response_name == "error":
        error_type = response_data.get("type")
        if error_type == "stateMismatch":
            results.record_pass(
                "ifInState mismatch returns stateMismatch error",
                f"Got stateMismatch as expected",
            )
        else:
            results.record_fail(
                "ifInState mismatch returns stateMismatch error",
                f"Expected stateMismatch, got {error_type}",
            )
    else:
        # If we got Email/set response, check that email was NOT actually updated
        mailbox_ids = get_email_mailbox_ids(api_url, token, account_id, email_id)
        if mailbox_ids and mailbox_a in mailbox_ids and mailbox_b not in mailbox_ids:
            results.record_fail(
                "ifInState mismatch returns stateMismatch error",
                "Got Email/set response instead of error, but no changes applied",
            )
        else:
            results.record_fail(
                "ifInState mismatch returns stateMismatch error",
                f"Got Email/set response and changes may have been applied: {mailbox_ids}",
            )


def test_new_state_matches_subsequent_get(
    api_url: str,
    upload_url: str,
    token: str,
    account_id: str,
    results,
):
    """
    Test: newState from Email/set matches state from Email/get.

    RFC 8620 Section 5.3: States must be consistent across methods.
    """
    print()
    print("Test: newState matches subsequent get (RFC 8620 Section 5.3)...")

    # Create mailboxes
    mailbox_a = create_test_mailbox(api_url, token, account_id)
    mailbox_b = create_test_mailbox(api_url, token, account_id)
    if not mailbox_a or not mailbox_b:
        results.record_fail(
            "State consistency test setup", "Failed to create mailboxes"
        )
        return

    email_id = import_test_email(api_url, upload_url, token, account_id, mailbox_a)
    if not email_id:
        results.record_fail(
            "State consistency test setup", "Failed to import email"
        )
        return

    # Make an Email/set update
    try:
        response = email_set_update(
            api_url,
            token,
            account_id,
            email_id,
            {f"mailboxIds/{mailbox_b}": True},
        )
    except Exception as e:
        results.record_fail("State consistency Email/set request", str(e))
        return

    if "methodResponses" not in response:
        results.record_fail(
            "State consistency response", f"No methodResponses: {response}"
        )
        return

    method_responses = response["methodResponses"]
    if len(method_responses) == 0:
        results.record_fail("State consistency response", "Empty methodResponses")
        return

    response_name, response_data, _ = method_responses[0]
    if response_name == "error":
        results.record_fail(
            "State consistency request",
            f"JMAP error: {response_data.get('type')}: {response_data.get('description')}",
        )
        return

    if response_name != "Email/set":
        results.record_fail(
            "State consistency response", f"Unexpected method: {response_name}"
        )
        return

    new_state_from_set = response_data.get("newState")
    if not new_state_from_set:
        results.record_fail(
            "State consistency test",
            "No newState in Email/set response",
        )
        return

    # Get state from Email/get
    state_from_get = get_email_state(api_url, token, account_id)
    if not state_from_get:
        results.record_fail(
            "State consistency test",
            "Failed to get state from Email/get",
        )
        return

    if new_state_from_set == state_from_get:
        results.record_pass(
            "newState matches subsequent get",
            f"Both report state: {new_state_from_set[:16]}...",
        )
    else:
        results.record_fail(
            "newState matches subsequent get",
            f"Email/set newState={new_state_from_set[:16]}..., "
            f"Email/get state={state_from_get[:16]}...",
        )


# =============================================================================
# Test: Cross-Type State Updates
# =============================================================================


def test_mailbox_state_changes_on_email_update(
    api_url: str,
    upload_url: str,
    token: str,
    account_id: str,
    results,
):
    """
    Test: Mailbox state changes when Email mailboxIds updated (counters change).

    RFC 8621 Section 2: Mailbox counters are part of mailbox state.
    """
    print()
    print("Test: Mailbox state changes on email update (RFC 8621 Section 2)...")

    # Create mailboxes
    mailbox_a = create_test_mailbox(api_url, token, account_id)
    mailbox_b = create_test_mailbox(api_url, token, account_id)
    if not mailbox_a or not mailbox_b:
        results.record_fail(
            "Cross-type state test setup", "Failed to create mailboxes"
        )
        return

    email_id = import_test_email(api_url, upload_url, token, account_id, mailbox_a)
    if not email_id:
        results.record_fail(
            "Cross-type state test setup", "Failed to import email"
        )
        return

    # Get initial mailbox state
    initial_mailbox_state = get_mailbox_state(api_url, token, account_id)
    if not initial_mailbox_state:
        results.record_fail(
            "Cross-type state test setup", "Failed to get initial mailbox state"
        )
        return

    # Move email to different mailbox (changes counters)
    try:
        response = email_set_update(
            api_url,
            token,
            account_id,
            email_id,
            {"mailboxIds": {mailbox_b: True}},
        )
    except Exception as e:
        results.record_fail("Cross-type state Email/set request", str(e))
        return

    if "methodResponses" not in response:
        results.record_fail(
            "Cross-type state response", f"No methodResponses: {response}"
        )
        return

    # Get new mailbox state
    new_mailbox_state = get_mailbox_state(api_url, token, account_id)
    if not new_mailbox_state:
        results.record_fail(
            "Cross-type state test", "Failed to get new mailbox state"
        )
        return

    if new_mailbox_state != initial_mailbox_state:
        results.record_pass(
            "Mailbox state changes on email update",
            f"Mailbox state changed from {initial_mailbox_state[:16]}... to {new_mailbox_state[:16]}...",
        )
    else:
        results.record_fail(
            "Mailbox state changes on email update",
            f"Mailbox state unchanged: {initial_mailbox_state[:16]}...",
        )


# =============================================================================
# Main Entry Functions
# =============================================================================


def test_email_set_mailbox_changes(client, config, results):
    """Test Email/set mailboxIds update operations (RFC 8621 Section 4.6)."""
    print()
    print("=" * 40)
    print("Testing Email/set mailboxIds changes (RFC 8621 Section 4.6)...")
    print("=" * 40)

    session = client.jmap_session

    try:
        account_id = client.account_id
    except Exception as e:
        results.record_fail("Email/set mailbox changes setup", f"No account ID: {e}")
        return

    api_url = session.api_url
    upload_url = session.upload_url

    test_move_email_between_mailboxes(
        api_url, upload_url, config.token, account_id, results
    )
    test_add_email_to_additional_mailbox(
        api_url, upload_url, config.token, account_id, results
    )
    test_remove_email_from_one_mailbox(
        api_url, upload_url, config.token, account_id, results
    )
    test_remove_all_mailboxes_error(
        api_url, upload_url, config.token, account_id, results
    )


def test_email_set_counter_updates(client, config, results):
    """Test mailbox counter updates from Email/set (RFC 8621 Section 2)."""
    print()
    print("=" * 40)
    print("Testing Email/set counter updates (RFC 8621 Section 2)...")
    print("=" * 40)

    session = client.jmap_session

    try:
        account_id = client.account_id
    except Exception as e:
        results.record_fail("Email/set counter updates setup", f"No account ID: {e}")
        return

    api_url = session.api_url
    upload_url = session.upload_url

    test_total_emails_increment_on_add(
        api_url, upload_url, config.token, account_id, results
    )
    test_total_emails_decrement_on_remove(
        api_url, upload_url, config.token, account_id, results
    )
    test_unread_emails_update_on_move(
        api_url, upload_url, config.token, account_id, results
    )
    test_read_email_no_unread_change(
        api_url, upload_url, config.token, account_id, results
    )


def test_email_set_state_tracking(client, config, results):
    """Test state tracking with Email/set (RFC 8620 Section 5.3)."""
    print()
    print("=" * 40)
    print("Testing Email/set state tracking (RFC 8620 Section 5.3)...")
    print("=" * 40)

    session = client.jmap_session

    try:
        account_id = client.account_id
    except Exception as e:
        results.record_fail("Email/set state tracking setup", f"No account ID: {e}")
        return

    api_url = session.api_url
    upload_url = session.upload_url

    test_email_set_returns_old_and_new_state(
        api_url, upload_url, config.token, account_id, results
    )
    test_new_state_differs_after_update(
        api_url, upload_url, config.token, account_id, results
    )
    test_if_in_state_success(
        api_url, upload_url, config.token, account_id, results
    )
    test_if_in_state_mismatch_error(
        api_url, upload_url, config.token, account_id, results
    )
    test_new_state_matches_subsequent_get(
        api_url, upload_url, config.token, account_id, results
    )
    test_mailbox_state_changes_on_email_update(
        api_url, upload_url, config.token, account_id, results
    )
