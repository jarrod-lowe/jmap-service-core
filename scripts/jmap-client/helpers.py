"""Shared helpers for JMAP e2e tests."""

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


def create_test_mailbox(
    api_url: str, token: str, account_id: str, prefix: str = "Test"
) -> str | None:
    """Create a test mailbox. Returns mailbox ID or None on failure."""
    unique_id = str(uuid.uuid4())[:8]
    mailbox_name = f"{prefix}-{unique_id}"

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


def upload_email_blob(
    upload_url: str,
    token: str,
    account_id: str,
    email_content: str,
) -> str | None:
    """Upload an email as a blob. Returns blobId or None on failure."""
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
        return upload_data.get("blobId")
    except Exception:
        return None


def import_test_email(
    api_url: str,
    upload_url: str,
    token: str,
    account_id: str,
    mailbox_id: str,
    keywords: dict | None = None,
) -> str | None:
    """Import a test email. Returns email ID or None on failure."""
    unique_id = str(uuid.uuid4())
    message_id = f"<test-{unique_id}@jmap-test.example>"

    received_at = datetime.now(timezone.utc)
    received_at_str = received_at.strftime("%Y-%m-%dT%H:%M:%SZ")
    date_str = received_at.strftime("%a, %d %b %Y %H:%M:%S %z")

    email_content = f"""From: Test Sender <test@example.com>
To: Test Recipient <recipient@example.com>
Subject: Test Email {unique_id[:8]}
Date: {date_str}
Message-ID: {message_id}
Content-Type: text/plain; charset=utf-8

This is a test email.
""".replace("\n", "\r\n")

    blob_id = upload_email_blob(upload_url, token, account_id, email_content)
    if not blob_id:
        return None

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

    headers_block = f"""From: Test Sender <test@example.com>
To: Test Recipient <recipient@example.com>
Subject: {subject}
Date: {date_str}
Message-ID: {message_id}"""

    if in_reply_to:
        headers_block += f"\nIn-Reply-To: {in_reply_to}"

    email_content = f"""{headers_block}
Content-Type: text/plain; charset=utf-8

Test email: {subject}
""".replace("\n", "\r\n")

    blob_id = upload_email_blob(upload_url, token, account_id, email_content)
    if not blob_id:
        return None, None

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
    return email_info.get("id"), email_info.get("threadId")


def get_email_state(api_url: str, token: str, account_id: str) -> str | None:
    """Get current Email state from Email/get."""
    email_get_call = [
        "Email/get",
        {
            "accountId": account_id,
            "ids": [],
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
            "ids": [],
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
            "ids": [],
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
    """Call Email/set with update operation. Returns full response."""
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


def get_email_keywords(
    api_url: str, token: str, account_id: str, email_id: str
) -> dict | None:
    """Get the keywords for an email via Email/get."""
    email_get_call = [
        "Email/get",
        {
            "accountId": account_id,
            "ids": [email_id],
            "properties": ["keywords"],
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

    return emails[0].get("keywords")


def thread_get(
    api_url: str, token: str, account_id: str, thread_ids: list[str]
) -> dict:
    """Call Thread/get and return the response data."""
    thread_get_call = [
        "Thread/get",
        {
            "accountId": account_id,
            "ids": thread_ids,
        },
        "threadGet0",
    ]

    try:
        response = make_jmap_request(api_url, token, [thread_get_call])
    except Exception as e:
        return {"error": str(e)}

    if "methodResponses" not in response:
        return {"error": f"No methodResponses: {response}"}

    method_responses = response["methodResponses"]
    if len(method_responses) == 0:
        return {"error": "Empty methodResponses"}

    response_name, response_data, _ = method_responses[0]
    if response_name == "error":
        return {
            "error": f"{response_data.get('type')}: {response_data.get('description')}"
        }

    if response_name != "Thread/get":
        return {"error": f"Unexpected method: {response_name}"}

    return response_data


def destroy_emails_and_verify_cleanup(
    api_url: str,
    token: str,
    account_id: str,
    email_ids: list[str],
):
    """
    Destroy emails via Email/set destroy and verify blob cleanup via DELETE endpoint.

    Raises AssertionError on failure (for use with pytest).
    """
    if not email_ids:
        return

    # Step 1: Get blobIds for the emails
    blob_ids = []
    email_get_call = [
        "Email/get",
        {
            "accountId": account_id,
            "ids": email_ids,
            "properties": ["blobId"],
        },
        "getBlobIds",
    ]
    try:
        get_response = make_jmap_request(api_url, token, [email_get_call])
        if "methodResponses" in get_response:
            resp_name, resp_data, _ = get_response["methodResponses"][0]
            if resp_name == "Email/get":
                for email in resp_data.get("list", []):
                    bid = email.get("blobId")
                    if bid:
                        blob_ids.append(bid)
    except Exception:
        pass  # Non-fatal

    # Step 2: Call Email/set destroy
    email_set_call = [
        "Email/set",
        {
            "accountId": account_id,
            "destroy": email_ids,
        },
        "destroyEmails",
    ]

    response = make_jmap_request(api_url, token, [email_set_call])
    assert "methodResponses" in response, f"No methodResponses: {response}"

    method_responses = response["methodResponses"]
    assert len(method_responses) > 0, "Empty methodResponses"

    response_name, response_data, _ = method_responses[0]
    assert response_name != "error", (
        f"JMAP error: {response_data.get('type')}: {response_data.get('description')}"
    )
    assert response_name == "Email/set", f"Unexpected method: {response_name}"

    destroyed = response_data.get("destroyed", [])
    assert set(destroyed) == set(email_ids), (
        f"Not all destroyed. Expected {email_ids}, got {destroyed}"
    )

    # Step 3: Verify emails are gone
    verify_get_call = [
        "Email/get",
        {
            "accountId": account_id,
            "ids": email_ids,
        },
        "verifyDestroyed",
    ]

    try:
        verify_response = make_jmap_request(api_url, token, [verify_get_call])
        if "methodResponses" in verify_response:
            resp_name, resp_data, _ = verify_response["methodResponses"][0]
            if resp_name == "Email/get":
                not_found = resp_data.get("notFound", [])
                assert set(not_found) == set(email_ids), (
                    f"Expected all in notFound, got notFound={not_found}"
                )
    except AssertionError:
        raise
    except Exception:
        pass  # Non-fatal verification

    # Step 4: Verify blob cleanup via DELETE endpoint
    if not blob_ids:
        return

    base_url = api_url.rsplit("/jmap", 1)[0]

    for blob_id in blob_ids:
        delete_url = f"{base_url}/delete/{account_id}/{blob_id}"
        try:
            resp = requests.delete(
                delete_url,
                headers={"Authorization": f"Bearer {token}"},
                timeout=30,
            )
            assert resp.status_code in (204, 404), (
                f"DELETE {blob_id} returned unexpected {resp.status_code}: {resp.text}"
            )
        except AssertionError:
            raise
        except Exception:
            pass  # Non-fatal
