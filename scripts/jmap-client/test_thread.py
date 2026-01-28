"""
Thread-specific JMAP tests.

Tests for Thread/get method per RFC 8621 Section 3.
Threading is based on In-Reply-To headers.
"""

import uuid
from datetime import datetime, timezone, timedelta

import pytest

from helpers import (
    make_jmap_request,
    create_test_mailbox,
    import_email_with_headers,
    get_thread_state,
    thread_get,
    destroy_emails_and_verify_cleanup,
)


class TestThread:
    """Tests for Thread/get (RFC 8621 Section 3)."""

    @pytest.fixture(autouse=True, scope="class")
    def setup_thread_data(self, api_url, upload_url, token, account_id, request):
        """Create test mailbox and emails for thread tests."""
        mailbox_id = create_test_mailbox(api_url, token, account_id, prefix="ThreadTest")
        assert mailbox_id is not None, "Failed to create test mailbox"

        request.cls.mailbox_id = mailbox_id
        request.cls.api_url = api_url
        request.cls.upload_url = upload_url
        request.cls.token = token
        request.cls.account_id = account_id
        request.cls.all_email_ids = []

        yield

        # Cleanup
        if request.cls.all_email_ids:
            destroy_emails_and_verify_cleanup(
                api_url, token, account_id, request.cls.all_email_ids
            )

    def test_standalone_email_gets_own_thread(self):
        """Standalone email (no In-Reply-To) gets its own thread."""
        unique_id = str(uuid.uuid4())[:8]
        message_id = f"<standalone-{unique_id}@test.example>"

        email_id, thread_id = import_email_with_headers(
            api_url=self.api_url,
            upload_url=self.upload_url,
            token=self.token,
            account_id=self.account_id,
            mailbox_id=self.mailbox_id,
            message_id=message_id,
            in_reply_to=None,
            subject=f"Standalone Test {unique_id}",
            received_at=datetime.now(timezone.utc),
        )

        assert email_id is not None, "Failed to import email"
        assert thread_id is not None, "Failed to import email"
        self.all_email_ids.append(email_id)

        thread_response = thread_get(self.api_url, self.token, self.account_id, [thread_id])
        assert "error" not in thread_response, f"Thread/get error: {thread_response.get('error')}"

        thread_list = thread_response.get("list", [])
        assert len(thread_list) == 1, f"Expected 1 thread, got {len(thread_list)}"

        thread = thread_list[0]
        email_ids = thread.get("emailIds", [])
        assert email_ids == [email_id], f"Expected [{email_id}], got {email_ids}"

    def test_reply_joins_existing_thread(self):
        """Reply (In-Reply-To matches Message-ID) joins existing thread."""
        unique_id = str(uuid.uuid4())[:8]
        message_id_a = f"<parent-{unique_id}@test.example>"

        base_time = datetime.now(timezone.utc) - timedelta(seconds=2)

        # Import parent email
        email_id_a, thread_id_a = import_email_with_headers(
            api_url=self.api_url,
            upload_url=self.upload_url,
            token=self.token,
            account_id=self.account_id,
            mailbox_id=self.mailbox_id,
            message_id=message_id_a,
            in_reply_to=None,
            subject=f"Parent Email {unique_id}",
            received_at=base_time,
        )
        assert email_id_a is not None, "Failed to import parent email"
        self.all_email_ids.append(email_id_a)

        # Import reply
        email_id_b, thread_id_b = import_email_with_headers(
            api_url=self.api_url,
            upload_url=self.upload_url,
            token=self.token,
            account_id=self.account_id,
            mailbox_id=self.mailbox_id,
            message_id=f"<reply-{unique_id}@test.example>",
            in_reply_to=message_id_a,
            subject=f"Re: Parent Email {unique_id}",
            received_at=base_time + timedelta(seconds=1),
        )
        assert email_id_b is not None, "Failed to import reply email"
        self.all_email_ids.append(email_id_b)

        # Both should have the same threadId
        assert thread_id_a == thread_id_b, (
            f"Thread IDs differ: parent={thread_id_a}, reply={thread_id_b}"
        )

        # Verify Thread/get returns both emails in order
        thread_response = thread_get(self.api_url, self.token, self.account_id, [thread_id_a])
        assert "error" not in thread_response, f"Thread/get error: {thread_response.get('error')}"

        thread_list = thread_response.get("list", [])
        assert len(thread_list) == 1, f"Expected 1 thread, got {len(thread_list)}"

        thread = thread_list[0]
        email_ids = thread.get("emailIds", [])
        expected = [email_id_a, email_id_b]
        assert email_ids == expected, f"Expected {expected}, got {email_ids}"

    def test_reply_to_nonexistent_gets_own_thread(self):
        """Reply to non-existent Message-ID gets its own thread."""
        unique_id = str(uuid.uuid4())[:8]
        nonexistent_message_id = f"<nonexistent-{unique_id}@test.example>"

        email_id, thread_id = import_email_with_headers(
            api_url=self.api_url,
            upload_url=self.upload_url,
            token=self.token,
            account_id=self.account_id,
            mailbox_id=self.mailbox_id,
            message_id=f"<orphan-{unique_id}@test.example>",
            in_reply_to=nonexistent_message_id,
            subject=f"Reply to Missing {unique_id}",
            received_at=datetime.now(timezone.utc),
        )

        assert email_id is not None, "Failed to import email"
        assert thread_id is not None, "Failed to import email"
        self.all_email_ids.append(email_id)

        thread_response = thread_get(self.api_url, self.token, self.account_id, [thread_id])
        assert "error" not in thread_response, f"Thread/get error: {thread_response.get('error')}"

        thread_list = thread_response.get("list", [])
        assert len(thread_list) == 1, f"Expected 1 thread, got {len(thread_list)}"

        thread = thread_list[0]
        email_ids = thread.get("emailIds", [])
        assert email_ids == [email_id], f"Expected [{email_id}], got {email_ids}"

    def test_thread_chain_three_emails(self):
        """Multiple replies form a chain (A -> B -> C all in same thread)."""
        unique_id = str(uuid.uuid4())[:8]
        message_id_a = f"<chain-a-{unique_id}@test.example>"
        message_id_b = f"<chain-b-{unique_id}@test.example>"

        base_time = datetime.now(timezone.utc) - timedelta(seconds=3)

        # Import A (original)
        email_id_a, thread_id_a = import_email_with_headers(
            api_url=self.api_url,
            upload_url=self.upload_url,
            token=self.token,
            account_id=self.account_id,
            mailbox_id=self.mailbox_id,
            message_id=message_id_a,
            in_reply_to=None,
            subject=f"Chain Start {unique_id}",
            received_at=base_time,
        )
        assert email_id_a is not None, "Failed to import email A"
        self.all_email_ids.append(email_id_a)

        # Import B (reply to A)
        email_id_b, thread_id_b = import_email_with_headers(
            api_url=self.api_url,
            upload_url=self.upload_url,
            token=self.token,
            account_id=self.account_id,
            mailbox_id=self.mailbox_id,
            message_id=message_id_b,
            in_reply_to=message_id_a,
            subject=f"Re: Chain Start {unique_id}",
            received_at=base_time + timedelta(seconds=1),
        )
        assert email_id_b is not None, "Failed to import email B"
        self.all_email_ids.append(email_id_b)

        # Import C (reply to B)
        email_id_c, thread_id_c = import_email_with_headers(
            api_url=self.api_url,
            upload_url=self.upload_url,
            token=self.token,
            account_id=self.account_id,
            mailbox_id=self.mailbox_id,
            message_id=f"<chain-c-{unique_id}@test.example>",
            in_reply_to=message_id_b,
            subject=f"Re: Re: Chain Start {unique_id}",
            received_at=base_time + timedelta(seconds=2),
        )
        assert email_id_c is not None, "Failed to import email C"
        self.all_email_ids.append(email_id_c)

        # All three should have the same threadId
        assert thread_id_a == thread_id_b, (
            f"Thread IDs differ: A={thread_id_a}, B={thread_id_b}"
        )
        assert thread_id_b == thread_id_c, (
            f"Thread IDs differ: B={thread_id_b}, C={thread_id_c}"
        )

        # Verify Thread/get returns all 3 in receivedAt order
        thread_response = thread_get(self.api_url, self.token, self.account_id, [thread_id_a])
        assert "error" not in thread_response, f"Thread/get error: {thread_response.get('error')}"

        thread_list = thread_response.get("list", [])
        assert len(thread_list) == 1, f"Expected 1 thread, got {len(thread_list)}"

        thread = thread_list[0]
        email_ids = thread.get("emailIds", [])
        expected = [email_id_a, email_id_b, email_id_c]
        assert email_ids == expected, f"Expected {expected}, got {email_ids}"

    def test_thread_not_found_for_unknown_id(self):
        """Thread/get with unknown ID returns notFound."""
        fake_thread_id = f"T-fake-{uuid.uuid4()}"

        thread_response = thread_get(self.api_url, self.token, self.account_id, [fake_thread_id])
        assert "error" not in thread_response, f"Thread/get error: {thread_response.get('error')}"

        thread_list = thread_response.get("list", [])
        not_found = thread_response.get("notFound", [])

        assert fake_thread_id in not_found, (
            f"Expected '{fake_thread_id}' in notFound, got list={thread_list}, notFound={not_found}"
        )

        found_ids = [t.get("id") for t in thread_list]
        assert fake_thread_id not in found_ids, "Fake ID unexpectedly found in list"
