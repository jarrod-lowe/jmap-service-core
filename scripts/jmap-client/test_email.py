"""
Email-specific JMAP tests.

Tests for Email/import and Email/get methods per RFC 8621.
"""

import uuid
from datetime import datetime, timezone

import requests
from jmapc import Client
from jmapc.methods import EmailGet


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


def create_test_mailbox(
    api_url: str, token: str, account_id: str, results
) -> str | None:
    """
    Create a new test mailbox using Mailbox/set.

    Returns the created mailbox ID, or None on failure.
    """
    unique_id = str(uuid.uuid4())[:8]
    mailbox_name = f"Test-{unique_id}"

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
    except Exception as e:
        results.record_fail("Mailbox/set request", str(e))
        return None

    if "methodResponses" not in response:
        results.record_fail(
            "Mailbox/set created mailbox", f"No methodResponses: {response}"
        )
        return None

    method_responses = response["methodResponses"]
    if len(method_responses) == 0:
        results.record_fail("Mailbox/set created mailbox", "Empty methodResponses")
        return None

    response_name, response_data, _ = method_responses[0]
    if response_name == "error":
        error_type = response_data.get("type")
        results.record_fail(
            "Mailbox/set created mailbox",
            f"JMAP error: {error_type}: {response_data.get('description')}",
        )
        return None

    if response_name != "Mailbox/set":
        results.record_fail(
            "Mailbox/set created mailbox",
            f"Unexpected method response: {response_name}",
        )
        return None

    created = response_data.get("created", {})
    mailbox_info = created.get("testMailbox")
    if not mailbox_info:
        not_created = response_data.get("notCreated", {})
        if "testMailbox" in not_created:
            error = not_created["testMailbox"]
            results.record_fail(
                "Mailbox/set created mailbox",
                f"Not created: {error.get('type')}: {error.get('description')}",
            )
        else:
            results.record_fail(
                "Mailbox/set created mailbox",
                f"No testMailbox in created or notCreated: {response_data}",
            )
        return None

    mailbox_id = mailbox_info.get("id")
    if not mailbox_id:
        results.record_fail(
            "Mailbox/set created mailbox",
            f"No id in created mailbox: {mailbox_info}",
        )
        return None

    results.record_pass(
        "Mailbox/set created mailbox", f"id: {mailbox_id}, name: {mailbox_name}"
    )
    return mailbox_id


def verify_mailbox_exists(
    api_url: str, token: str, account_id: str, mailbox_id: str, results
) -> bool:
    """
    Verify a mailbox exists using Mailbox/get.

    Returns True if the mailbox exists, False otherwise.
    """
    mailbox_get_call = [
        "Mailbox/get",
        {
            "accountId": account_id,
            "ids": [mailbox_id],
        },
        "getMailbox0",
    ]

    try:
        response = make_jmap_request(api_url, token, [mailbox_get_call])
    except Exception as e:
        results.record_fail("Mailbox/get verified mailbox exists", str(e))
        return False

    if "methodResponses" not in response:
        results.record_fail(
            "Mailbox/get verified mailbox exists", f"No methodResponses: {response}"
        )
        return False

    method_responses = response["methodResponses"]
    if len(method_responses) == 0:
        results.record_fail(
            "Mailbox/get verified mailbox exists", "Empty methodResponses"
        )
        return False

    response_name, response_data, _ = method_responses[0]
    if response_name == "error":
        error_type = response_data.get("type")
        results.record_fail(
            "Mailbox/get verified mailbox exists",
            f"JMAP error: {error_type}: {response_data.get('description')}",
        )
        return False

    if response_name != "Mailbox/get":
        results.record_fail(
            "Mailbox/get verified mailbox exists",
            f"Unexpected method response: {response_name}",
        )
        return False

    mailboxes = response_data.get("list", [])
    if mailboxes and mailboxes[0].get("id") == mailbox_id:
        results.record_pass("Mailbox/get verified mailbox exists")
        return True

    not_found = response_data.get("notFound", [])
    if mailbox_id in not_found:
        results.record_fail(
            "Mailbox/get verified mailbox exists", f"Mailbox {mailbox_id} not found"
        )
    else:
        results.record_fail(
            "Mailbox/get verified mailbox exists",
            f"Mailbox not in response: {response_data}",
        )
    return False


def test_email_import_and_get(client: Client, config, results):
    """
    Full lifecycle test: Mailbox/set -> Mailbox/get -> Email/import -> Email/get.

    Creates a fresh test mailbox, verifies it exists, imports an RFC 5322 email,
    and retrieves it with correct property values per RFC 8621.
    """
    print()
    print("Testing Email/import and Email/get round-trip...")

    session = client.jmap_session

    # Get account ID
    try:
        account_id = client.account_id
    except Exception as e:
        results.record_fail("Email import/get test", f"No account ID: {e}")
        return

    # Step 1: Create test mailbox (Mailbox/set)
    mailbox_id = create_test_mailbox(
        session.api_url, config.token, account_id, results
    )
    if not mailbox_id:
        return

    # Step 2: Verify mailbox exists (Mailbox/get)
    if not verify_mailbox_exists(
        session.api_url, config.token, account_id, mailbox_id, results
    ):
        return

    # Step 3: Generate unique Message-ID to avoid deduplication conflicts
    unique_id = str(uuid.uuid4())
    message_id = f"<test-import-{unique_id}@jmap-test.example>"

    # Step 4: Create RFC 5322 email content with known values
    date_str = datetime.now(timezone.utc).strftime("%a, %d %b %Y %H:%M:%S %z")
    email_content = f"""From: Test Sender <sender@example.com>
To: Test Recipient <recipient@example.com>
Subject: Test Email for E2E Import Verification
Date: {date_str}
Message-ID: {message_id}
Content-Type: text/plain; charset=utf-8

This is the test email body content for JMAP import verification.
""".replace("\n", "\r\n")  # RFC 5322 requires CRLF

    # Step 5: Upload email as blob
    upload_endpoint = session.upload_url.replace("{accountId}", account_id)
    headers = {
        "Authorization": f"Bearer {config.token}",
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
            results.record_fail(
                "Upload email blob",
                f"Got HTTP {upload_response.status_code}: {upload_response.text}",
            )
            return
        upload_data = upload_response.json()
        blob_id = upload_data.get("blobId")
        if not blob_id:
            results.record_fail("Upload email blob", "No blobId in response")
            return
        results.record_pass("Upload email blob", f"blobId: {blob_id}")
    except Exception as e:
        results.record_fail("Upload email blob", str(e))
        return

    # Step 6: Call Email/import via raw JMAP request
    import_call = [
        "Email/import",
        {
            "accountId": account_id,
            "emails": {
                "email1": {
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

    try:
        import_response = make_jmap_request(
            session.api_url, config.token, [import_call]
        )
    except Exception as e:
        results.record_fail("Email/import request", str(e))
        return

    # Check for JMAP-level errors
    if "methodResponses" not in import_response:
        results.record_fail(
            "Email/import response",
            f"No methodResponses in response: {import_response}",
        )
        return

    method_responses = import_response["methodResponses"]
    if len(method_responses) == 0:
        results.record_fail("Email/import response", "Empty methodResponses")
        return

    response_name, response_data, response_id = method_responses[0]
    if response_name == "error":
        results.record_fail(
            "Email/import request",
            f"JMAP error: {response_data.get('type')}: {response_data.get('description')}",
        )
        return

    if response_name != "Email/import":
        results.record_fail(
            "Email/import request",
            f"Unexpected method response: {response_name}",
        )
        return

    # Step 7: Extract created email ID from response
    created = response_data.get("created", {})
    if "email1" not in created:
        not_created = response_data.get("notCreated", {})
        if "email1" in not_created:
            error = not_created["email1"]
            results.record_fail(
                "Email/import created email",
                f"Not created: {error.get('type')}: {error.get('description')}",
            )
        else:
            results.record_fail(
                "Email/import created email",
                f"No email1 in created or notCreated: {response_data}",
            )
        return

    email_id = created["email1"].get("id")
    if not email_id:
        results.record_fail(
            "Email/import created email",
            f"No id in created email: {created['email1']}",
        )
        return

    results.record_pass("Email/import created email", f"emailId: {email_id}")

    # Step 8: Call Email/get via jmapc to retrieve the email
    try:
        get_response = client.request(
            EmailGet(
                ids=[email_id],
                properties=[
                    "id",
                    "from",
                    "to",
                    "subject",
                    "messageId",
                    "receivedAt",
                    "preview",
                    "mailboxIds",
                ],
            )
        )
    except Exception as e:
        results.record_fail("Email/get request", str(e))
        return

    # Check for errors
    error_type = getattr(get_response, "type", None)
    if error_type:
        results.record_fail(
            "Email/get request",
            f"JMAP error: {error_type}: {getattr(get_response, 'description', '')}",
        )
        return

    emails = getattr(get_response, "data", None)
    if not emails or len(emails) == 0:
        results.record_fail("Email/get returned email", "No emails in response")
        return

    email = emails[0]
    results.record_pass("Email/get returned email")

    # Step 9: Assert all properties match expected values

    # Verify id is present and matches
    email_id_from_get = getattr(email, "id", None)
    if email_id_from_get == email_id:
        results.record_pass("Email id matches", email_id)
    else:
        results.record_fail(
            "Email id matches", f"Expected {email_id}, got {email_id_from_get}"
        )

    # Verify from contains sender@example.com
    # Note: jmapc maps JSON "from" field to "mail_from" attribute (not "from_")
    from_addresses = getattr(email, "mail_from", None)
    if from_addresses:
        from_str = str(from_addresses)
        if "sender@example.com" in from_str:
            results.record_pass("Email from matches", from_str)
        else:
            results.record_fail(
                "Email from matches",
                f"Expected 'sender@example.com' in {from_str}",
            )
    else:
        results.record_fail("Email from matches", "No from field")

    # Verify to contains recipient@example.com
    to_addresses = getattr(email, "to", None)
    if to_addresses:
        to_str = str(to_addresses)
        if "recipient@example.com" in to_str:
            results.record_pass("Email to matches", to_str)
        else:
            results.record_fail(
                "Email to matches",
                f"Expected 'recipient@example.com' in {to_str}",
            )
    else:
        results.record_fail("Email to matches", "No to field")

    # Verify subject matches exactly
    subject = getattr(email, "subject", None)
    expected_subject = "Test Email for E2E Import Verification"
    if subject == expected_subject:
        results.record_pass("Email subject matches", subject)
    else:
        results.record_fail(
            "Email subject matches",
            f"Expected '{expected_subject}', got '{subject}'",
        )

    # Verify messageId contains our generated Message-ID
    message_ids = getattr(email, "message_id", None)
    if message_ids:
        message_id_str = str(message_ids)
        # Strip angle brackets for comparison
        expected_msg_id = message_id.strip("<>")
        if expected_msg_id in message_id_str:
            results.record_pass("Email messageId matches", message_id_str)
        else:
            results.record_fail(
                "Email messageId matches",
                f"Expected '{expected_msg_id}' in {message_id_str}",
            )
    else:
        results.record_fail("Email messageId matches", "No messageId field")

    # Verify receivedAt is present
    received_at = getattr(email, "received_at", None)
    if received_at:
        results.record_pass("Email receivedAt present", str(received_at))
    else:
        results.record_fail("Email receivedAt present", "No receivedAt field")

    # Verify preview contains body content
    preview = getattr(email, "preview", None)
    if preview:
        if "test email body content" in preview.lower():
            results.record_pass("Email preview matches", preview[:80] + "...")
        else:
            results.record_fail(
                "Email preview matches",
                f"Expected 'test email body content' in '{preview}'",
            )
    else:
        results.record_fail("Email preview matches", "No preview field")
