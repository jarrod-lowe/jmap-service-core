"""
Tests for Email/query total and collapseThreads features.

Tests for:
1. total field - Returns mailbox email count when inMailbox filter is present
2. collapseThreads parameter - When true, returns only one email per thread
"""

import uuid
from datetime import datetime, timezone, timedelta

import pytest

from helpers import (
    make_jmap_request,
    create_test_mailbox,
    import_email_with_headers,
    get_mailbox_counts,
    destroy_emails_and_verify_cleanup,
    destroy_mailbox,
)


class TestEmailQueryTotal:
    """Tests for the Email/query total field behavior."""

    @pytest.fixture(scope="class")
    def total_test_data(self, api_url, upload_url, token, account_id):
        """Create test mailbox and import 3 test emails."""
        mailbox_id = create_test_mailbox(api_url, token, account_id, prefix="TotalTest")
        assert mailbox_id is not None, "Failed to create test mailbox"

        email_ids = []
        base_time = datetime.now(timezone.utc) - timedelta(seconds=10)

        # Import 3 standalone emails
        for i in range(3):
            unique_id = str(uuid.uuid4())[:8]
            message_id = f"<total-test-{unique_id}@test.example>"

            email_id, _ = import_email_with_headers(
                api_url=api_url,
                upload_url=upload_url,
                token=token,
                account_id=account_id,
                mailbox_id=mailbox_id,
                message_id=message_id,
                in_reply_to=None,
                subject=f"Total Test Email {i}",
                received_at=base_time + timedelta(seconds=i),
            )
            assert email_id is not None, f"Failed to import email {i}"
            email_ids.append(email_id)

        data = {
            "mailbox_id": mailbox_id,
            "email_ids": email_ids,
        }

        yield data

        # Cleanup
        if email_ids:
            destroy_emails_and_verify_cleanup(api_url, token, account_id, email_ids)
        destroy_mailbox(api_url, token, account_id, mailbox_id, on_destroy_remove_emails=True)

    def test_total_with_inMailbox_filter(
        self, total_test_data, api_url, token, account_id
    ):
        """Email/query with inMailbox filter returns total matching Mailbox's totalEmails."""
        mailbox_id = total_test_data["mailbox_id"]

        # Call Email/query with inMailbox filter
        query_call = [
            "Email/query",
            {
                "accountId": account_id,
                "filter": {"inMailbox": mailbox_id},
            },
            "emailQuery0",
        ]

        response = make_jmap_request(api_url, token, [query_call])
        assert "methodResponses" in response, f"No methodResponses: {response}"

        resp_name, resp_data, _ = response["methodResponses"][0]
        assert resp_name == "Email/query", f"Unexpected method: {resp_name}"

        # Assert total is present
        assert "total" in resp_data, f"total not in response: {resp_data}"
        query_total = resp_data["total"]

        # Get totalEmails from Mailbox/get
        mailbox_counts = get_mailbox_counts(api_url, token, account_id, mailbox_id)
        assert mailbox_counts is not None, "Failed to get mailbox counts"
        mailbox_total = mailbox_counts["totalEmails"]

        # Assert they match
        assert query_total == mailbox_total, (
            f"Email/query total ({query_total}) != Mailbox totalEmails ({mailbox_total})"
        )

    def test_total_absent_without_inMailbox_filter(
        self, total_test_data, api_url, token, account_id
    ):
        """Email/query without inMailbox filter does not return total."""
        # Call Email/query with empty filter
        query_call = [
            "Email/query",
            {
                "accountId": account_id,
                "filter": {},
            },
            "emailQuery0",
        ]

        response = make_jmap_request(api_url, token, [query_call])
        assert "methodResponses" in response, f"No methodResponses: {response}"

        resp_name, resp_data, _ = response["methodResponses"][0]
        assert resp_name == "Email/query", f"Unexpected method: {resp_name}"

        # Assert total is NOT present
        assert "total" not in resp_data, (
            f"total should not be in response without inMailbox filter: {resp_data}"
        )


class TestEmailQueryCollapseThreads:
    """Tests for the Email/query collapseThreads parameter."""

    @pytest.fixture(scope="class")
    def collapse_threads_data(self, api_url, upload_url, token, account_id):
        """
        Create test mailbox and import emails for thread testing.

        - Email A: standalone (thread 1)
        - Email B: reply to A (joins thread 1)
        - Email C: standalone (thread 2)

        Result: 3 emails in 2 threads.
        """
        mailbox_id = create_test_mailbox(
            api_url, token, account_id, prefix="CollapseTest"
        )
        assert mailbox_id is not None, "Failed to create test mailbox"

        email_ids = []
        base_time = datetime.now(timezone.utc) - timedelta(seconds=10)
        unique_id = str(uuid.uuid4())[:8]

        # Email A - standalone (thread 1)
        message_id_a = f"<collapse-a-{unique_id}@test.example>"
        email_id_a, thread_id_a = import_email_with_headers(
            api_url=api_url,
            upload_url=upload_url,
            token=token,
            account_id=account_id,
            mailbox_id=mailbox_id,
            message_id=message_id_a,
            in_reply_to=None,
            subject=f"Thread 1 Original {unique_id}",
            received_at=base_time,
        )
        assert email_id_a is not None, "Failed to import email A"
        email_ids.append(email_id_a)

        # Email B - reply to A (joins thread 1)
        message_id_b = f"<collapse-b-{unique_id}@test.example>"
        email_id_b, thread_id_b = import_email_with_headers(
            api_url=api_url,
            upload_url=upload_url,
            token=token,
            account_id=account_id,
            mailbox_id=mailbox_id,
            message_id=message_id_b,
            in_reply_to=message_id_a,
            subject=f"Re: Thread 1 Original {unique_id}",
            received_at=base_time + timedelta(seconds=1),
        )
        assert email_id_b is not None, "Failed to import email B"
        email_ids.append(email_id_b)

        # Verify B joined A's thread
        assert thread_id_a == thread_id_b, (
            f"Email B should join thread 1: A={thread_id_a}, B={thread_id_b}"
        )

        # Email C - standalone (thread 2)
        message_id_c = f"<collapse-c-{unique_id}@test.example>"
        email_id_c, thread_id_c = import_email_with_headers(
            api_url=api_url,
            upload_url=upload_url,
            token=token,
            account_id=account_id,
            mailbox_id=mailbox_id,
            message_id=message_id_c,
            in_reply_to=None,
            subject=f"Thread 2 Standalone {unique_id}",
            received_at=base_time + timedelta(seconds=2),
        )
        assert email_id_c is not None, "Failed to import email C"
        email_ids.append(email_id_c)

        # Verify C got its own thread
        assert thread_id_c != thread_id_a, (
            f"Email C should have different thread: A={thread_id_a}, C={thread_id_c}"
        )

        data = {
            "mailbox_id": mailbox_id,
            "email_ids": email_ids,
            "thread_1_id": thread_id_a,
            "thread_2_id": thread_id_c,
        }

        yield data

        # Cleanup
        if email_ids:
            destroy_emails_and_verify_cleanup(api_url, token, account_id, email_ids)
        destroy_mailbox(api_url, token, account_id, mailbox_id, on_destroy_remove_emails=True)

    def test_collapseThreads_true(
        self, collapse_threads_data, api_url, token, account_id
    ):
        """Email/query with collapseThreads: true returns one email per thread."""
        mailbox_id = collapse_threads_data["mailbox_id"]

        query_call = [
            "Email/query",
            {
                "accountId": account_id,
                "filter": {"inMailbox": mailbox_id},
                "collapseThreads": True,
            },
            "emailQuery0",
        ]

        response = make_jmap_request(api_url, token, [query_call])
        assert "methodResponses" in response, f"No methodResponses: {response}"

        resp_name, resp_data, _ = response["methodResponses"][0]
        assert resp_name == "Email/query", f"Unexpected method: {resp_name}"

        # Should have exactly 2 email IDs (one per thread)
        ids = resp_data.get("ids", [])
        assert len(ids) == 2, (
            f"Expected 2 emails (one per thread), got {len(ids)}: {ids}"
        )

        # Verify collapseThreads is echoed as true
        assert resp_data.get("collapseThreads") is True, (
            f"Expected collapseThreads: true in response: {resp_data}"
        )

    def test_collapseThreads_false(
        self, collapse_threads_data, api_url, token, account_id
    ):
        """Email/query with collapseThreads: false returns all emails."""
        mailbox_id = collapse_threads_data["mailbox_id"]

        query_call = [
            "Email/query",
            {
                "accountId": account_id,
                "filter": {"inMailbox": mailbox_id},
                "collapseThreads": False,
            },
            "emailQuery0",
        ]

        response = make_jmap_request(api_url, token, [query_call])
        assert "methodResponses" in response, f"No methodResponses: {response}"

        resp_name, resp_data, _ = response["methodResponses"][0]
        assert resp_name == "Email/query", f"Unexpected method: {resp_name}"

        # Should have all 3 email IDs
        ids = resp_data.get("ids", [])
        assert len(ids) == 3, (
            f"Expected 3 emails (all), got {len(ids)}: {ids}"
        )

        # Verify collapseThreads is echoed as false
        assert resp_data.get("collapseThreads") is False, (
            f"Expected collapseThreads: false in response: {resp_data}"
        )

    def test_collapseThreads_default(
        self, collapse_threads_data, api_url, token, account_id
    ):
        """Email/query without collapseThreads defaults to false."""
        mailbox_id = collapse_threads_data["mailbox_id"]

        query_call = [
            "Email/query",
            {
                "accountId": account_id,
                "filter": {"inMailbox": mailbox_id},
                # Note: collapseThreads NOT specified
            },
            "emailQuery0",
        ]

        response = make_jmap_request(api_url, token, [query_call])
        assert "methodResponses" in response, f"No methodResponses: {response}"

        resp_name, resp_data, _ = response["methodResponses"][0]
        assert resp_name == "Email/query", f"Unexpected method: {resp_name}"

        # collapseThreads should default to false
        assert resp_data.get("collapseThreads") is False, (
            f"Expected collapseThreads: false (default) in response: {resp_data}"
        )
