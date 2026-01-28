"""
Thread-specific JMAP tests.

Tests for Thread/get method per RFC 8621 Section 3.
Threading is based on In-Reply-To headers.
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


def create_test_mailbox(api_url: str, token: str, account_id: str) -> str | None:
    """Create a test mailbox. Returns mailbox ID or None on failure."""
    unique_id = str(uuid.uuid4())[:8]
    mailbox_name = f"ThreadTest-{unique_id}"

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

This is a test email for thread testing.
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


def thread_get(
    api_url: str, token: str, account_id: str, thread_ids: list[str]
) -> dict:
    """
    Call Thread/get and return the response data.

    Returns dict with 'list' and 'notFound' keys.
    """
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


def test_thread_operations(client, config, results):
    """
    Run all Thread/get E2E tests.

    Tests threading behavior based on In-Reply-To headers per RFC 8621 Section 3.
    """
    print()
    print("=" * 40)
    print("Testing Thread/get (RFC 8621 §3)...")
    print("=" * 40)

    session = client.jmap_session

    try:
        account_id = client.account_id
    except Exception as e:
        results.record_fail("Thread tests setup", f"No account ID: {e}")
        return

    api_url = session.api_url
    upload_url = session.upload_url

    # Create a test mailbox for all thread tests
    mailbox_id = create_test_mailbox(api_url, config.token, account_id)
    if not mailbox_id:
        results.record_fail("Thread tests setup", "Failed to create test mailbox")
        return

    results.record_pass("Thread tests mailbox created", f"mailboxId: {mailbox_id}")

    all_email_ids = []

    # Test 1: Standalone email gets its own thread
    all_email_ids.extend(test_standalone_email_thread(
        api_url, upload_url, config.token, account_id, mailbox_id, results
    ))

    # Test 2: Reply joins existing thread
    all_email_ids.extend(test_reply_joins_thread(
        api_url, upload_url, config.token, account_id, mailbox_id, results
    ))

    # Test 3: Reply to non-existent message gets own thread
    all_email_ids.extend(test_reply_to_nonexistent(
        api_url, upload_url, config.token, account_id, mailbox_id, results
    ))

    # Test 4: Multiple replies form a chain
    all_email_ids.extend(test_thread_chain(
        api_url, upload_url, config.token, account_id, mailbox_id, results
    ))

    # Test 5: Thread/get with unknown ID returns notFound
    test_thread_not_found(api_url, config.token, account_id, results)

    # Cleanup
    if all_email_ids:
        from test_email_set import destroy_emails_and_verify_s3_cleanup

        destroy_emails_and_verify_s3_cleanup(
            api_url, config.token, account_id, all_email_ids, config, results,
            test_name_prefix="[thread tests cleanup]",
        )


def test_standalone_email_thread(
    api_url: str,
    upload_url: str,
    token: str,
    account_id: str,
    mailbox_id: str,
    results,
) -> list[str]:
    """Test 1: Standalone email (no In-Reply-To) gets its own thread."""
    email_ids_created = []
    print()
    print("Test 1: Standalone email gets its own thread...")

    unique_id = str(uuid.uuid4())[:8]
    message_id = f"<standalone-{unique_id}@test.example>"

    email_id, thread_id = import_email_with_headers(
        api_url=api_url,
        upload_url=upload_url,
        token=token,
        account_id=account_id,
        mailbox_id=mailbox_id,
        message_id=message_id,
        in_reply_to=None,
        subject=f"Standalone Test {unique_id}",
        received_at=datetime.now(timezone.utc),
    )

    if not email_id or not thread_id:
        results.record_fail("Standalone email has own thread", "Failed to import email")
        return email_ids_created
    email_ids_created.append(email_id)

    results.record_pass(
        "Standalone email imported",
        f"emailId: {email_id}, threadId: {thread_id}",
    )

    # Verify Thread/get returns this email in the thread
    thread_response = thread_get(api_url, token, account_id, [thread_id])

    if "error" in thread_response:
        results.record_fail(
            "Standalone email has own thread", f"Thread/get error: {thread_response['error']}"
        )
        return email_ids_created

    thread_list = thread_response.get("list", [])
    if len(thread_list) != 1:
        results.record_fail(
            "Standalone email has own thread",
            f"Expected 1 thread, got {len(thread_list)}",
        )
        return email_ids_created

    thread = thread_list[0]
    email_ids = thread.get("emailIds", [])

    if email_ids == [email_id]:
        results.record_pass(
            "Standalone email has own thread",
            f"Thread contains exactly [{email_id}]",
        )
    else:
        results.record_fail(
            "Standalone email has own thread",
            f"Expected [{email_id}], got {email_ids}",
        )

    return email_ids_created


def test_reply_joins_thread(
    api_url: str,
    upload_url: str,
    token: str,
    account_id: str,
    mailbox_id: str,
    results,
) -> list[str]:
    """Test 2: Reply (In-Reply-To matches Message-ID) joins existing thread."""
    email_ids_created = []
    print()
    print("Test 2: Reply joins existing thread...")

    unique_id = str(uuid.uuid4())[:8]
    message_id_a = f"<parent-{unique_id}@test.example>"

    # Import email A (parent, no In-Reply-To)
    base_time = datetime.now(timezone.utc) - timedelta(seconds=2)
    email_id_a, thread_id_a = import_email_with_headers(
        api_url=api_url,
        upload_url=upload_url,
        token=token,
        account_id=account_id,
        mailbox_id=mailbox_id,
        message_id=message_id_a,
        in_reply_to=None,
        subject=f"Parent Email {unique_id}",
        received_at=base_time,
    )

    if not email_id_a or not thread_id_a:
        results.record_fail("Reply joins existing thread", "Failed to import parent email")
        return email_ids_created
    email_ids_created.append(email_id_a)

    # Import email B (reply, In-Reply-To points to A)
    email_id_b, thread_id_b = import_email_with_headers(
        api_url=api_url,
        upload_url=upload_url,
        token=token,
        account_id=account_id,
        mailbox_id=mailbox_id,
        message_id=f"<reply-{unique_id}@test.example>",
        in_reply_to=message_id_a,
        subject=f"Re: Parent Email {unique_id}",
        received_at=base_time + timedelta(seconds=1),
    )

    if not email_id_b or not thread_id_b:
        results.record_fail("Reply joins existing thread", "Failed to import reply email")
        return email_ids_created
    email_ids_created.append(email_id_b)

    # Both should have the same threadId
    if thread_id_a != thread_id_b:
        results.record_fail(
            "Reply joins existing thread",
            f"Thread IDs differ: parent={thread_id_a}, reply={thread_id_b}",
        )
        return email_ids_created

    results.record_pass(
        "Reply has same threadId as parent",
        f"Both have threadId: {thread_id_a}",
    )

    # Verify Thread/get returns both emails in order
    thread_response = thread_get(api_url, token, account_id, [thread_id_a])

    if "error" in thread_response:
        results.record_fail(
            "Reply joins existing thread",
            f"Thread/get error: {thread_response['error']}",
        )
        return email_ids_created

    thread_list = thread_response.get("list", [])
    if len(thread_list) != 1:
        results.record_fail(
            "Reply joins existing thread",
            f"Expected 1 thread, got {len(thread_list)}",
        )
        return email_ids_created

    thread = thread_list[0]
    email_ids = thread.get("emailIds", [])

    # Should be [parent, reply] sorted by receivedAt oldest-first
    expected = [email_id_a, email_id_b]
    if email_ids == expected:
        results.record_pass(
            "Reply joins existing thread",
            f"Thread contains {email_ids} in receivedAt order",
        )
    else:
        results.record_fail(
            "Reply joins existing thread",
            f"Expected {expected}, got {email_ids}",
        )

    return email_ids_created


def test_reply_to_nonexistent(
    api_url: str,
    upload_url: str,
    token: str,
    account_id: str,
    mailbox_id: str,
    results,
) -> list[str]:
    """Test 3: Reply to non-existent Message-ID gets its own thread."""
    email_ids_created = []
    print()
    print("Test 3: Reply to non-existent message gets own thread...")

    unique_id = str(uuid.uuid4())[:8]
    # Reference a Message-ID that doesn't exist
    nonexistent_message_id = f"<nonexistent-{unique_id}@test.example>"

    email_id, thread_id = import_email_with_headers(
        api_url=api_url,
        upload_url=upload_url,
        token=token,
        account_id=account_id,
        mailbox_id=mailbox_id,
        message_id=f"<orphan-{unique_id}@test.example>",
        in_reply_to=nonexistent_message_id,
        subject=f"Reply to Missing {unique_id}",
        received_at=datetime.now(timezone.utc),
    )

    if not email_id or not thread_id:
        results.record_fail(
            "Reply to non-existent gets own thread", "Failed to import email"
        )
        return email_ids_created
    email_ids_created.append(email_id)

    results.record_pass(
        "Reply to non-existent imported",
        f"emailId: {email_id}, threadId: {thread_id}",
    )

    # Verify Thread/get returns only this email
    thread_response = thread_get(api_url, token, account_id, [thread_id])

    if "error" in thread_response:
        results.record_fail(
            "Reply to non-existent gets own thread",
            f"Thread/get error: {thread_response['error']}",
        )
        return email_ids_created

    thread_list = thread_response.get("list", [])
    if len(thread_list) != 1:
        results.record_fail(
            "Reply to non-existent gets own thread",
            f"Expected 1 thread, got {len(thread_list)}",
        )
        return email_ids_created

    thread = thread_list[0]
    email_ids = thread.get("emailIds", [])

    if email_ids == [email_id]:
        results.record_pass(
            "Reply to non-existent gets own thread",
            f"Thread contains only [{email_id}]",
        )
    else:
        results.record_fail(
            "Reply to non-existent gets own thread",
            f"Expected [{email_id}], got {email_ids}",
        )

    return email_ids_created


def test_thread_chain(
    api_url: str,
    upload_url: str,
    token: str,
    account_id: str,
    mailbox_id: str,
    results,
) -> list[str]:
    """Test 4: Multiple replies form a chain (A -> B -> C all in same thread)."""
    email_ids_created = []
    print()
    print("Test 4: Thread chain with 3 emails...")

    unique_id = str(uuid.uuid4())[:8]
    message_id_a = f"<chain-a-{unique_id}@test.example>"
    message_id_b = f"<chain-b-{unique_id}@test.example>"

    base_time = datetime.now(timezone.utc) - timedelta(seconds=3)

    # Import A (original, no In-Reply-To)
    email_id_a, thread_id_a = import_email_with_headers(
        api_url=api_url,
        upload_url=upload_url,
        token=token,
        account_id=account_id,
        mailbox_id=mailbox_id,
        message_id=message_id_a,
        in_reply_to=None,
        subject=f"Chain Start {unique_id}",
        received_at=base_time,
    )

    if not email_id_a or not thread_id_a:
        results.record_fail("Thread chain has 3 emails", "Failed to import email A")
        return email_ids_created
    email_ids_created.append(email_id_a)

    # Import B (reply to A)
    email_id_b, thread_id_b = import_email_with_headers(
        api_url=api_url,
        upload_url=upload_url,
        token=token,
        account_id=account_id,
        mailbox_id=mailbox_id,
        message_id=message_id_b,
        in_reply_to=message_id_a,
        subject=f"Re: Chain Start {unique_id}",
        received_at=base_time + timedelta(seconds=1),
    )

    if not email_id_b or not thread_id_b:
        results.record_fail("Thread chain has 3 emails", "Failed to import email B")
        return email_ids_created
    email_ids_created.append(email_id_b)

    # Import C (reply to B)
    email_id_c, thread_id_c = import_email_with_headers(
        api_url=api_url,
        upload_url=upload_url,
        token=token,
        account_id=account_id,
        mailbox_id=mailbox_id,
        message_id=f"<chain-c-{unique_id}@test.example>",
        in_reply_to=message_id_b,
        subject=f"Re: Re: Chain Start {unique_id}",
        received_at=base_time + timedelta(seconds=2),
    )

    if not email_id_c or not thread_id_c:
        results.record_fail("Thread chain has 3 emails", "Failed to import email C")
        return email_ids_created
    email_ids_created.append(email_id_c)

    # All three should have the same threadId
    if thread_id_a != thread_id_b or thread_id_b != thread_id_c:
        results.record_fail(
            "Thread chain has 3 emails",
            f"Thread IDs differ: A={thread_id_a}, B={thread_id_b}, C={thread_id_c}",
        )
        return email_ids_created

    results.record_pass(
        "All chain emails have same threadId",
        f"threadId: {thread_id_a}",
    )

    # Verify Thread/get returns all 3 in receivedAt order
    thread_response = thread_get(api_url, token, account_id, [thread_id_a])

    if "error" in thread_response:
        results.record_fail(
            "Thread chain has 3 emails in order",
            f"Thread/get error: {thread_response['error']}",
        )
        return email_ids_created

    thread_list = thread_response.get("list", [])
    if len(thread_list) != 1:
        results.record_fail(
            "Thread chain has 3 emails in order",
            f"Expected 1 thread, got {len(thread_list)}",
        )
        return email_ids_created

    thread = thread_list[0]
    email_ids = thread.get("emailIds", [])

    expected = [email_id_a, email_id_b, email_id_c]
    if email_ids == expected:
        results.record_pass(
            "Thread chain has 3 emails in order",
            f"emailIds: {email_ids}",
        )
    else:
        results.record_fail(
            "Thread chain has 3 emails in order",
            f"Expected {expected}, got {email_ids}",
        )

    return email_ids_created


def test_thread_not_found(api_url: str, token: str, account_id: str, results):
    """Test 5: Thread/get with unknown ID returns notFound."""
    print()
    print("Test 5: Thread/get with unknown ID returns notFound...")

    fake_thread_id = f"T-fake-{uuid.uuid4()}"

    thread_response = thread_get(api_url, token, account_id, [fake_thread_id])

    if "error" in thread_response:
        results.record_fail(
            "Thread/get notFound for unknown ID",
            f"Thread/get error: {thread_response['error']}",
        )
        return

    thread_list = thread_response.get("list", [])
    not_found = thread_response.get("notFound", [])

    if fake_thread_id in not_found:
        results.record_pass(
            "Thread/get notFound for unknown ID",
            f"'{fake_thread_id}' in notFound array",
        )
    else:
        results.record_fail(
            "Thread/get notFound for unknown ID",
            f"Expected '{fake_thread_id}' in notFound, got list={thread_list}, notFound={not_found}",
        )

    # Also verify it's not in the list
    found_ids = [t.get("id") for t in thread_list]
    if fake_thread_id not in found_ids:
        results.record_pass(
            "Unknown thread ID not in list",
            f"list contains {len(thread_list)} threads, none with fake ID",
        )
    else:
        results.record_fail(
            "Unknown thread ID not in list",
            f"Fake ID unexpectedly found in list",
        )
