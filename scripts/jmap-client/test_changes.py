"""
State tracking JMAP tests.

Tests for Email/changes, Mailbox/changes, and Thread/changes methods per RFC 8620 Section 5.2.
"""

import uuid
from datetime import datetime, timezone, timedelta

import pytest

from helpers import (
    make_jmap_request,
    create_test_mailbox,
    import_test_email,
    import_email_with_headers,
    get_email_state,
    get_mailbox_state,
    get_thread_state,
    destroy_emails_and_verify_cleanup,
    destroy_mailbox,
)


class TestEmailChanges:
    """Tests for Email/changes (RFC 8620 Section 5.2)."""

    @pytest.fixture(scope="class")
    def mailbox_and_cleanup(self, api_url, upload_url, token, account_id):
        """Create a test mailbox and track email IDs for cleanup."""
        mailbox_id = create_test_mailbox(api_url, token, account_id, prefix="ChangesTest")
        assert mailbox_id is not None, "Failed to create test mailbox"
        email_ids = []
        yield mailbox_id, email_ids
        if email_ids:
            destroy_emails_and_verify_cleanup(api_url, token, account_id, email_ids)
        destroy_mailbox(api_url, token, account_id, mailbox_id, on_destroy_remove_emails=True)

    def test_response_structure(self, api_url, token, account_id):
        """Email/changes response has all required fields (RFC 8620 Section 5.2)."""
        initial_state = get_email_state(api_url, token, account_id)
        assert initial_state is not None, "Failed to get initial Email state"

        changes_call = [
            "Email/changes",
            {"accountId": account_id, "sinceState": initial_state},
            "changes0",
        ]

        response = make_jmap_request(api_url, token, [changes_call])
        assert "methodResponses" in response, f"No methodResponses: {response}"

        method_responses = response["methodResponses"]
        assert len(method_responses) > 0, "Empty methodResponses"

        response_name, response_data, _ = method_responses[0]
        assert response_name != "error", (
            f"JMAP error: {response_data.get('type')}: {response_data.get('description')}"
        )
        assert response_name == "Email/changes", f"Unexpected method: {response_name}"

        assert response_data.get("accountId") == account_id, (
            f"accountId mismatch: expected {account_id}, got {response_data.get('accountId')}"
        )
        assert response_data.get("oldState") == initial_state, (
            f"oldState mismatch: expected {initial_state}, got {response_data.get('oldState')}"
        )

        new_state = response_data.get("newState")
        assert new_state is not None, "missing newState"
        assert isinstance(new_state, str), f"newState not a string: {type(new_state)}"

        has_more = response_data.get("hasMoreChanges")
        assert has_more is not None, "missing hasMoreChanges"
        assert isinstance(has_more, bool), f"hasMoreChanges not a boolean: {type(has_more)}"

        created = response_data.get("created")
        assert created is not None, "missing created"
        assert isinstance(created, list), f"created not an array: {type(created)}"

        updated = response_data.get("updated")
        assert updated is not None, "missing updated"
        assert isinstance(updated, list), f"updated not an array: {type(updated)}"

        destroyed = response_data.get("destroyed")
        assert destroyed is not None, "missing destroyed"
        assert isinstance(destroyed, list), f"destroyed not an array: {type(destroyed)}"

    def test_state_changes_after_import(self, api_url, upload_url, token, account_id, mailbox_and_cleanup):
        """Email state changes after importing a new email (RFC 8620 Section 5.1)."""
        mailbox_id, email_ids = mailbox_and_cleanup

        initial_state = get_email_state(api_url, token, account_id)
        assert initial_state is not None, "Failed to get initial state"

        email_id = import_test_email(api_url, upload_url, token, account_id, mailbox_id)
        assert email_id is not None, "Failed to import test email"
        email_ids.append(email_id)

        new_state = get_email_state(api_url, token, account_id)
        assert new_state is not None, "Failed to get new state"
        assert new_state != initial_state, (
            f"State did not change after import (still {initial_state[:16]}...)"
        )

    def test_returns_created_email(self, api_url, upload_url, token, account_id, mailbox_and_cleanup):
        """Email/changes returns newly imported email in created array (RFC 8620 Section 5.2)."""
        mailbox_id, email_ids = mailbox_and_cleanup

        initial_state = get_email_state(api_url, token, account_id)
        assert initial_state is not None, "Failed to get initial state"

        email_id = import_test_email(api_url, upload_url, token, account_id, mailbox_id)
        assert email_id is not None, "Failed to import test email"
        email_ids.append(email_id)

        changes_call = [
            "Email/changes",
            {"accountId": account_id, "sinceState": initial_state},
            "changes0",
        ]

        response = make_jmap_request(api_url, token, [changes_call])
        assert "methodResponses" in response, f"No methodResponses: {response}"

        method_responses = response["methodResponses"]
        assert len(method_responses) > 0, "Empty methodResponses"

        response_name, response_data, _ = method_responses[0]
        assert response_name != "error", (
            f"JMAP error: {response_data.get('type')}: {response_data.get('description')}"
        )
        assert response_name == "Email/changes", f"Unexpected method: {response_name}"

        created = response_data.get("created", [])
        assert email_id in created, f"emailId {email_id} not in created: {created}"

    def test_max_changes_limit(self, api_url, upload_url, token, account_id, mailbox_and_cleanup):
        """Email/changes maxChanges limits total IDs returned (RFC 8620 Section 5.2)."""
        mailbox_id, email_ids = mailbox_and_cleanup

        initial_state = get_email_state(api_url, token, account_id)
        assert initial_state is not None, "Failed to get initial state"

        for _ in range(3):
            email_id = import_test_email(api_url, upload_url, token, account_id, mailbox_id)
            assert email_id is not None, "Failed to import test email"
            email_ids.append(email_id)

        changes_call = [
            "Email/changes",
            {"accountId": account_id, "sinceState": initial_state, "maxChanges": 1},
            "changes0",
        ]

        response = make_jmap_request(api_url, token, [changes_call])
        assert "methodResponses" in response, f"No methodResponses: {response}"

        method_responses = response["methodResponses"]
        assert len(method_responses) > 0, "Empty methodResponses"

        response_name, response_data, _ = method_responses[0]
        assert response_name != "error", (
            f"JMAP error: {response_data.get('type')}: {response_data.get('description')}"
        )
        assert response_name == "Email/changes", f"Unexpected method: {response_name}"

        created = response_data.get("created", [])
        updated = response_data.get("updated", [])
        destroyed = response_data.get("destroyed", [])
        has_more = response_data.get("hasMoreChanges")

        total_ids = len(created) + len(updated) + len(destroyed)
        assert total_ids <= 1, f"total IDs = {total_ids} (expected <= 1)"
        assert has_more is True, f"Expected hasMoreChanges=true, got {has_more}"

    def test_pagination(self, api_url, upload_url, token, account_id, mailbox_and_cleanup):
        """Email/changes pagination returns all emails eventually (RFC 8620 Section 5.2)."""
        mailbox_id, email_ids = mailbox_and_cleanup

        initial_state = get_email_state(api_url, token, account_id)
        assert initial_state is not None, "Failed to get initial state"

        expected_email_ids = []
        for _ in range(3):
            email_id = import_test_email(api_url, upload_url, token, account_id, mailbox_id)
            assert email_id is not None, "Failed to import test email"
            expected_email_ids.append(email_id)
        email_ids.extend(expected_email_ids)

        all_created = []
        current_state = initial_state
        max_iterations = 10

        for iteration in range(1, max_iterations + 1):
            changes_call = [
                "Email/changes",
                {"accountId": account_id, "sinceState": current_state, "maxChanges": 1},
                f"changes{iteration}",
            ]

            response = make_jmap_request(api_url, token, [changes_call])
            assert "methodResponses" in response, f"No methodResponses: {response}"

            method_responses = response["methodResponses"]
            assert len(method_responses) > 0, "Empty methodResponses"

            response_name, response_data, _ = method_responses[0]
            assert response_name != "error", (
                f"JMAP error: {response_data.get('type')}: {response_data.get('description')}"
            )
            assert response_name == "Email/changes", f"Unexpected method: {response_name}"

            created = response_data.get("created", [])
            all_created.extend(created)
            current_state = response_data.get("newState")
            has_more = response_data.get("hasMoreChanges", False)

            if not has_more:
                break

        missing = set(expected_email_ids) - set(all_created)
        assert not missing, f"Missing emails: {missing} (found: {all_created})"

    def test_invalid_state_returns_error(self, api_url, token, account_id):
        """Email/changes returns cannotCalculateChanges for invalid sinceState (RFC 8620 Section 5.2)."""
        changes_call = [
            "Email/changes",
            {"accountId": account_id, "sinceState": "invalid-state-string"},
            "changes0",
        ]

        response = make_jmap_request(api_url, token, [changes_call])
        assert "methodResponses" in response, f"No methodResponses: {response}"

        method_responses = response["methodResponses"]
        assert len(method_responses) > 0, "Empty methodResponses"

        response_name, response_data, _ = method_responses[0]
        assert response_name == "error", f"Expected error response, got {response_name}"
        assert response_data.get("type") == "cannotCalculateChanges", (
            f"Expected cannotCalculateChanges, got {response_data.get('type')}"
        )


class TestMailboxChanges:
    """Tests for Mailbox/changes (RFC 8620 Section 5.2 + RFC 8621 Section 2.2)."""

    def test_response_structure(self, api_url, token, account_id):
        """Mailbox/changes response has all required fields including updatedProperties."""
        initial_state = get_mailbox_state(api_url, token, account_id)
        assert initial_state is not None, "Failed to get initial Mailbox state"

        changes_call = [
            "Mailbox/changes",
            {"accountId": account_id, "sinceState": initial_state},
            "changes0",
        ]

        response = make_jmap_request(api_url, token, [changes_call])
        assert "methodResponses" in response, f"No methodResponses: {response}"

        method_responses = response["methodResponses"]
        assert len(method_responses) > 0, "Empty methodResponses"

        response_name, response_data, _ = method_responses[0]
        assert response_name != "error", (
            f"JMAP error: {response_data.get('type')}: {response_data.get('description')}"
        )
        assert response_name == "Mailbox/changes", f"Unexpected method: {response_name}"

        assert response_data.get("accountId") == account_id
        assert response_data.get("oldState") == initial_state
        assert isinstance(response_data.get("newState"), str), "newState missing or not a string"
        assert isinstance(response_data.get("hasMoreChanges"), bool), "hasMoreChanges missing or not a boolean"
        assert isinstance(response_data.get("created"), list), "created missing or not an array"
        assert isinstance(response_data.get("updated"), list), "updated missing or not an array"
        assert isinstance(response_data.get("destroyed"), list), "destroyed missing or not an array"

        assert "updatedProperties" in response_data, "updatedProperties missing"
        updated_props = response_data.get("updatedProperties")
        assert updated_props is None or isinstance(updated_props, list), (
            f"updatedProperties not null or array: {type(updated_props)}"
        )

    def test_state_changes_after_create(self, api_url, token, account_id):
        """Mailbox state changes after creating a new mailbox (RFC 8620 Section 5.1)."""
        initial_state = get_mailbox_state(api_url, token, account_id)
        assert initial_state is not None, "Failed to get initial state"

        mailbox_id = create_test_mailbox(api_url, token, account_id)
        assert mailbox_id is not None, "Failed to create test mailbox"

        new_state = get_mailbox_state(api_url, token, account_id)
        assert new_state is not None, "Failed to get new state"
        assert new_state != initial_state, (
            f"State did not change after create (still {initial_state[:16]}...)"
        )

        destroy_mailbox(api_url, token, account_id, mailbox_id)

    def test_returns_created_mailbox(self, api_url, token, account_id):
        """Mailbox/changes returns newly created mailbox in created array (RFC 8620 Section 5.2)."""
        initial_state = get_mailbox_state(api_url, token, account_id)
        assert initial_state is not None, "Failed to get initial state"

        mailbox_id = create_test_mailbox(api_url, token, account_id)
        assert mailbox_id is not None, "Failed to create test mailbox"

        changes_call = [
            "Mailbox/changes",
            {"accountId": account_id, "sinceState": initial_state},
            "changes0",
        ]

        response = make_jmap_request(api_url, token, [changes_call])
        assert "methodResponses" in response, f"No methodResponses: {response}"

        method_responses = response["methodResponses"]
        assert len(method_responses) > 0, "Empty methodResponses"

        response_name, response_data, _ = method_responses[0]
        assert response_name != "error", (
            f"JMAP error: {response_data.get('type')}: {response_data.get('description')}"
        )
        assert response_name == "Mailbox/changes", f"Unexpected method: {response_name}"

        created = response_data.get("created", [])
        assert mailbox_id in created, f"mailboxId {mailbox_id} not in created: {created}"

        destroy_mailbox(api_url, token, account_id, mailbox_id)

    def test_invalid_state_returns_error(self, api_url, token, account_id):
        """Mailbox/changes returns cannotCalculateChanges for invalid sinceState (RFC 8620 Section 5.2)."""
        changes_call = [
            "Mailbox/changes",
            {"accountId": account_id, "sinceState": "invalid-state-string"},
            "changes0",
        ]

        response = make_jmap_request(api_url, token, [changes_call])
        assert "methodResponses" in response, f"No methodResponses: {response}"

        method_responses = response["methodResponses"]
        assert len(method_responses) > 0, "Empty methodResponses"

        response_name, response_data, _ = method_responses[0]
        assert response_name == "error", f"Expected error response, got {response_name}"
        assert response_data.get("type") == "cannotCalculateChanges", (
            f"Expected cannotCalculateChanges, got {response_data.get('type')}"
        )


class TestThreadChanges:
    """Tests for Thread/changes (RFC 8620 Section 5.2 + RFC 8621 Section 3.2)."""

    @pytest.fixture(scope="class")
    def mailbox_and_cleanup(self, api_url, upload_url, token, account_id):
        """Create a test mailbox and track email IDs for cleanup."""
        mailbox_id = create_test_mailbox(api_url, token, account_id, prefix="ThreadChangesTest")
        assert mailbox_id is not None, "Failed to create test mailbox"
        email_ids = []
        yield mailbox_id, email_ids
        if email_ids:
            destroy_emails_and_verify_cleanup(api_url, token, account_id, email_ids)
        destroy_mailbox(api_url, token, account_id, mailbox_id, on_destroy_remove_emails=True)

    def test_response_structure(self, api_url, token, account_id):
        """Thread/changes response has all required fields (RFC 8620 Section 5.2)."""
        initial_state = get_thread_state(api_url, token, account_id)
        assert initial_state is not None, "Failed to get initial Thread state"

        changes_call = [
            "Thread/changes",
            {"accountId": account_id, "sinceState": initial_state},
            "changes0",
        ]

        response = make_jmap_request(api_url, token, [changes_call])
        assert "methodResponses" in response, f"No methodResponses: {response}"

        method_responses = response["methodResponses"]
        assert len(method_responses) > 0, "Empty methodResponses"

        response_name, response_data, _ = method_responses[0]
        assert response_name != "error", (
            f"JMAP error: {response_data.get('type')}: {response_data.get('description')}"
        )
        assert response_name == "Thread/changes", f"Unexpected method: {response_name}"

        assert response_data.get("accountId") == account_id, (
            f"accountId mismatch: expected {account_id}, got {response_data.get('accountId')}"
        )
        assert response_data.get("oldState") == initial_state, (
            f"oldState mismatch: expected {initial_state}, got {response_data.get('oldState')}"
        )

        new_state = response_data.get("newState")
        assert new_state is not None, "missing newState"
        assert isinstance(new_state, str), f"newState not a string: {type(new_state)}"

        has_more = response_data.get("hasMoreChanges")
        assert has_more is not None, "missing hasMoreChanges"
        assert isinstance(has_more, bool), f"hasMoreChanges not a boolean: {type(has_more)}"

        assert isinstance(response_data.get("created"), list), "created missing or not an array"
        assert isinstance(response_data.get("updated"), list), "updated missing or not an array"
        assert isinstance(response_data.get("destroyed"), list), "destroyed missing or not an array"

    def test_state_changes_after_import(self, api_url, upload_url, token, account_id, mailbox_and_cleanup):
        """Thread state changes after importing a new standalone email (RFC 8620 Section 5.1)."""
        mailbox_id, email_ids = mailbox_and_cleanup

        initial_state = get_thread_state(api_url, token, account_id)
        assert initial_state is not None, "Failed to get initial state"

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
        assert email_id is not None and thread_id is not None, "Failed to import test email"
        email_ids.append(email_id)

        new_state = get_thread_state(api_url, token, account_id)
        assert new_state is not None, "Failed to get new state"
        assert new_state != initial_state, (
            f"State did not change after import (still {initial_state[:16]}...)"
        )

    def test_returns_created_thread(self, api_url, upload_url, token, account_id, mailbox_and_cleanup):
        """Thread/changes returns newly created thread in created array (RFC 8620 Section 5.2)."""
        mailbox_id, email_ids = mailbox_and_cleanup

        initial_state = get_thread_state(api_url, token, account_id)
        assert initial_state is not None, "Failed to get initial state"

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
        assert email_id is not None and thread_id is not None, "Failed to import test email"
        email_ids.append(email_id)

        changes_call = [
            "Thread/changes",
            {"accountId": account_id, "sinceState": initial_state},
            "changes0",
        ]

        response = make_jmap_request(api_url, token, [changes_call])
        assert "methodResponses" in response, f"No methodResponses: {response}"

        method_responses = response["methodResponses"]
        assert len(method_responses) > 0, "Empty methodResponses"

        response_name, response_data, _ = method_responses[0]
        assert response_name != "error", (
            f"JMAP error: {response_data.get('type')}: {response_data.get('description')}"
        )
        assert response_name == "Thread/changes", f"Unexpected method: {response_name}"

        created = response_data.get("created", [])
        assert thread_id in created, f"threadId {thread_id} not in created: {created}"

    def test_returns_updated_thread(self, api_url, upload_url, token, account_id, mailbox_and_cleanup):
        """Thread/changes returns updated thread when reply is added (RFC 8620 Section 5.2)."""
        mailbox_id, email_ids = mailbox_and_cleanup

        initial_state = get_thread_state(api_url, token, account_id)
        assert initial_state is not None, "Failed to get initial state"

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
        assert email_id_parent is not None and thread_id is not None, "Failed to import parent email"
        email_ids.append(email_id_parent)

        intermediate_state = get_thread_state(api_url, token, account_id)
        assert intermediate_state is not None, "Failed to get intermediate state"

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
        assert email_id_reply is not None and thread_id_reply is not None, "Failed to import reply email"
        email_ids.append(email_id_reply)

        assert thread_id == thread_id_reply, (
            f"Reply did not join parent thread: parent={thread_id}, reply={thread_id_reply}"
        )

        changes_call = [
            "Thread/changes",
            {"accountId": account_id, "sinceState": intermediate_state},
            "changes0",
        ]

        response = make_jmap_request(api_url, token, [changes_call])
        assert "methodResponses" in response, f"No methodResponses: {response}"

        method_responses = response["methodResponses"]
        assert len(method_responses) > 0, "Empty methodResponses"

        response_name, response_data, _ = method_responses[0]
        assert response_name != "error", (
            f"JMAP error: {response_data.get('type')}: {response_data.get('description')}"
        )
        assert response_name == "Thread/changes", f"Unexpected method: {response_name}"

        updated = response_data.get("updated", [])
        assert thread_id in updated, f"threadId {thread_id} not in updated: {updated}"

    def test_max_changes_limit(self, api_url, upload_url, token, account_id, mailbox_and_cleanup):
        """Thread/changes maxChanges limits total IDs returned (RFC 8620 Section 5.2)."""
        mailbox_id, email_ids = mailbox_and_cleanup

        initial_state = get_thread_state(api_url, token, account_id)
        assert initial_state is not None, "Failed to get initial state"

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
            assert email_id is not None and thread_id is not None, f"Failed to import test email {i}"
            email_ids.append(email_id)

        changes_call = [
            "Thread/changes",
            {"accountId": account_id, "sinceState": initial_state, "maxChanges": 1},
            "changes0",
        ]

        response = make_jmap_request(api_url, token, [changes_call])
        assert "methodResponses" in response, f"No methodResponses: {response}"

        method_responses = response["methodResponses"]
        assert len(method_responses) > 0, "Empty methodResponses"

        response_name, response_data, _ = method_responses[0]
        assert response_name != "error", (
            f"JMAP error: {response_data.get('type')}: {response_data.get('description')}"
        )
        assert response_name == "Thread/changes", f"Unexpected method: {response_name}"

        created = response_data.get("created", [])
        updated = response_data.get("updated", [])
        destroyed = response_data.get("destroyed", [])
        has_more = response_data.get("hasMoreChanges")

        total_ids = len(created) + len(updated) + len(destroyed)
        assert total_ids <= 1, f"total IDs = {total_ids} (expected <= 1)"
        assert has_more is True, f"Expected hasMoreChanges=true, got {has_more}"

    def test_pagination(self, api_url, upload_url, token, account_id, mailbox_and_cleanup):
        """Thread/changes pagination returns all threads eventually (RFC 8620 Section 5.2)."""
        mailbox_id, email_ids = mailbox_and_cleanup

        initial_state = get_thread_state(api_url, token, account_id)
        assert initial_state is not None, "Failed to get initial state"

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
            assert email_id is not None and thread_id is not None, f"Failed to import test email {i}"
            email_ids.append(email_id)
            expected_thread_ids.append(thread_id)

        all_created = []
        current_state = initial_state
        max_iterations = 10

        for iteration in range(1, max_iterations + 1):
            changes_call = [
                "Thread/changes",
                {"accountId": account_id, "sinceState": current_state, "maxChanges": 1},
                f"changes{iteration}",
            ]

            response = make_jmap_request(api_url, token, [changes_call])
            assert "methodResponses" in response, f"No methodResponses: {response}"

            method_responses = response["methodResponses"]
            assert len(method_responses) > 0, "Empty methodResponses"

            response_name, response_data, _ = method_responses[0]
            assert response_name != "error", (
                f"JMAP error: {response_data.get('type')}: {response_data.get('description')}"
            )
            assert response_name == "Thread/changes", f"Unexpected method: {response_name}"

            created = response_data.get("created", [])
            all_created.extend(created)
            current_state = response_data.get("newState")
            has_more = response_data.get("hasMoreChanges", False)

            if not has_more:
                break

        missing = set(expected_thread_ids) - set(all_created)
        assert not missing, f"Missing threads: {missing} (found: {all_created})"

    def test_invalid_state_returns_error(self, api_url, token, account_id):
        """Thread/changes returns cannotCalculateChanges for invalid sinceState (RFC 8620 Section 5.2)."""
        changes_call = [
            "Thread/changes",
            {"accountId": account_id, "sinceState": "invalid-state-string"},
            "changes0",
        ]

        response = make_jmap_request(api_url, token, [changes_call])
        assert "methodResponses" in response, f"No methodResponses: {response}"

        method_responses = response["methodResponses"]
        assert len(method_responses) > 0, "Empty methodResponses"

        response_name, response_data, _ = method_responses[0]
        assert response_name == "error", f"Expected error response, got {response_name}"
        assert response_data.get("type") == "cannotCalculateChanges", (
            f"Expected cannotCalculateChanges, got {response_data.get('type')}"
        )
