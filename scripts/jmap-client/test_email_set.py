"""
Email/set E2E tests.

Tests for Email/set method focusing on mailboxIds changes, mailbox counter updates,
and state tracking per RFC 8620/8621.
"""

from helpers import (
    make_jmap_request,
    create_test_mailbox,
    import_test_email,
    get_email_state,
    get_mailbox_state,
    get_mailbox_counts,
    email_set_update,
    get_email_mailbox_ids,
    get_email_keywords,
    destroy_emails_and_verify_cleanup,
)


# =============================================================================
# Test: mailboxIds Changes (RFC 8621 Section 4.6)
# =============================================================================


class TestMailboxChanges:

    def test_move_email_between_mailboxes(self, api_url, upload_url, token, account_id):
        """
        Test: Move email between mailboxes by replacing mailboxIds.

        RFC 8621 Section 4.6: mailboxIds is updatable.
        """
        email_ids = []
        try:
            mailbox_a = create_test_mailbox(api_url, token, account_id)
            assert mailbox_a, "Failed to create mailbox A"

            mailbox_b = create_test_mailbox(api_url, token, account_id)
            assert mailbox_b, "Failed to create mailbox B"

            email_id = import_test_email(api_url, upload_url, token, account_id, mailbox_a)
            assert email_id, "Failed to import email"
            email_ids.append(email_id)

            response = email_set_update(
                api_url, token, account_id, email_id,
                {"mailboxIds": {mailbox_b: True}},
            )

            assert "methodResponses" in response, f"No methodResponses: {response}"
            method_responses = response["methodResponses"]
            assert len(method_responses) > 0, "Empty methodResponses"

            response_name, response_data, _ = method_responses[0]
            assert response_name != "error", (
                f"JMAP error: {response_data.get('type')}: {response_data.get('description')}"
            )
            assert response_name == "Email/set", f"Unexpected method: {response_name}"

            updated = response_data.get("updated", {})
            not_updated = response_data.get("notUpdated", {})

            if email_id in not_updated:
                error = not_updated[email_id]
                raise AssertionError(
                    f"Not updated: {error.get('type')}: {error.get('description')}"
                )

            assert email_id in updated, (
                f"Email not in updated or notUpdated: {response_data}"
            )

            mailbox_ids = get_email_mailbox_ids(api_url, token, account_id, email_id)
            assert mailbox_ids is not None, "Failed to get mailboxIds"
            assert mailbox_b in mailbox_ids, f"Expected {mailbox_b} in mailboxIds, got {mailbox_ids}"
            assert mailbox_a not in mailbox_ids, f"Expected {mailbox_a} not in mailboxIds, got {mailbox_ids}"
        finally:
            if email_ids:
                destroy_emails_and_verify_cleanup(api_url, token, account_id, email_ids)

    def test_add_email_to_additional_mailbox(self, api_url, upload_url, token, account_id):
        """
        Test: Add email to additional mailbox using patch syntax.

        RFC 8620 Section 5.3: Use "mailboxIds/newId": true to add.
        """
        email_ids = []
        try:
            mailbox_a = create_test_mailbox(api_url, token, account_id)
            assert mailbox_a, "Failed to create mailbox A"

            mailbox_b = create_test_mailbox(api_url, token, account_id)
            assert mailbox_b, "Failed to create mailbox B"

            email_id = import_test_email(api_url, upload_url, token, account_id, mailbox_a)
            assert email_id, "Failed to import email"
            email_ids.append(email_id)

            response = email_set_update(
                api_url, token, account_id, email_id,
                {f"mailboxIds/{mailbox_b}": True},
            )

            assert "methodResponses" in response, f"No methodResponses: {response}"
            method_responses = response["methodResponses"]
            assert len(method_responses) > 0, "Empty methodResponses"

            response_name, response_data, _ = method_responses[0]
            assert response_name != "error", (
                f"JMAP error: {response_data.get('type')}: {response_data.get('description')}"
            )
            assert response_name == "Email/set", f"Unexpected method: {response_name}"

            updated = response_data.get("updated", {})
            not_updated = response_data.get("notUpdated", {})

            if email_id in not_updated:
                error = not_updated[email_id]
                raise AssertionError(
                    f"Not updated: {error.get('type')}: {error.get('description')}"
                )

            assert email_id in updated, (
                f"Email not in updated or notUpdated: {response_data}"
            )

            mailbox_ids = get_email_mailbox_ids(api_url, token, account_id, email_id)
            assert mailbox_ids is not None, "Failed to get mailboxIds"
            assert mailbox_a in mailbox_ids, f"Expected {mailbox_a} in mailboxIds, got {mailbox_ids}"
            assert mailbox_b in mailbox_ids, f"Expected {mailbox_b} in mailboxIds, got {mailbox_ids}"
        finally:
            if email_ids:
                destroy_emails_and_verify_cleanup(api_url, token, account_id, email_ids)

    def test_remove_email_from_one_mailbox(self, api_url, upload_url, token, account_id):
        """
        Test: Remove email from one mailbox using patch syntax (must remain in at least one).

        RFC 8620 Section 5.3: Use "mailboxIds/oldId": null to remove.
        """
        email_ids = []
        try:
            mailbox_a = create_test_mailbox(api_url, token, account_id)
            assert mailbox_a, "Failed to create mailbox A"

            mailbox_b = create_test_mailbox(api_url, token, account_id)
            assert mailbox_b, "Failed to create mailbox B"

            email_id = import_test_email(api_url, upload_url, token, account_id, mailbox_a)
            assert email_id, "Failed to import email"
            email_ids.append(email_id)

            # First add to mailbox B
            response = email_set_update(
                api_url, token, account_id, email_id,
                {f"mailboxIds/{mailbox_b}": True},
            )
            assert "methodResponses" in response, "Failed to add to mailbox B"

            # Now remove from mailbox A
            response = email_set_update(
                api_url, token, account_id, email_id,
                {f"mailboxIds/{mailbox_a}": None},
            )

            assert "methodResponses" in response, f"No methodResponses: {response}"
            method_responses = response["methodResponses"]
            assert len(method_responses) > 0, "Empty methodResponses"

            response_name, response_data, _ = method_responses[0]
            assert response_name != "error", (
                f"JMAP error: {response_data.get('type')}: {response_data.get('description')}"
            )
            assert response_name == "Email/set", f"Unexpected method: {response_name}"

            updated = response_data.get("updated", {})
            not_updated = response_data.get("notUpdated", {})

            if email_id in not_updated:
                error = not_updated[email_id]
                raise AssertionError(
                    f"Not updated: {error.get('type')}: {error.get('description')}"
                )

            assert email_id in updated, (
                f"Email not in updated or notUpdated: {response_data}"
            )

            mailbox_ids = get_email_mailbox_ids(api_url, token, account_id, email_id)
            assert mailbox_ids is not None, "Failed to get mailboxIds"
            assert mailbox_b in mailbox_ids, f"Expected {mailbox_b} in mailboxIds, got {mailbox_ids}"
            assert mailbox_a not in mailbox_ids, f"Expected {mailbox_a} not in mailboxIds, got {mailbox_ids}"
        finally:
            if email_ids:
                destroy_emails_and_verify_cleanup(api_url, token, account_id, email_ids)

    def test_remove_all_mailboxes_error(self, api_url, upload_url, token, account_id):
        """
        Test: Setting mailboxIds to empty returns invalidProperties error.

        RFC 8621 Section 4.6: Email MUST be in at least one mailbox.
        """
        email_ids = []
        try:
            mailbox_id = create_test_mailbox(api_url, token, account_id)
            assert mailbox_id, "Failed to create mailbox"

            email_id = import_test_email(api_url, upload_url, token, account_id, mailbox_id)
            assert email_id, "Failed to import email"
            email_ids.append(email_id)

            response = email_set_update(
                api_url, token, account_id, email_id,
                {"mailboxIds": {}},
            )

            assert "methodResponses" in response, f"No methodResponses: {response}"
            method_responses = response["methodResponses"]
            assert len(method_responses) > 0, "Empty methodResponses"

            response_name, response_data, _ = method_responses[0]

            if response_name == "error":
                # Method-level error is acceptable
                return

            assert response_name == "Email/set", f"Unexpected method: {response_name}"

            not_updated = response_data.get("notUpdated", {})
            updated = response_data.get("updated", {})

            assert email_id not in updated, (
                "Email was updated with empty mailboxIds (should have failed)"
            )
            assert email_id in not_updated, (
                f"Email not in updated or notUpdated: {response_data}"
            )
        finally:
            if email_ids:
                destroy_emails_and_verify_cleanup(api_url, token, account_id, email_ids)


# =============================================================================
# Test: Mailbox Counter Updates (RFC 8621 Section 2)
# =============================================================================


class TestCounterUpdates:

    def test_total_emails_increment_on_add(self, api_url, upload_url, token, account_id):
        """
        Test: totalEmails increases when email added to mailbox.

        RFC 8621 Section 2: totalEmails is the count of emails in mailbox.
        """
        email_ids = []
        try:
            mailbox_a = create_test_mailbox(api_url, token, account_id)
            assert mailbox_a, "Failed to create mailbox A"

            mailbox_b = create_test_mailbox(api_url, token, account_id)
            assert mailbox_b, "Failed to create mailbox B"

            email_id = import_test_email(api_url, upload_url, token, account_id, mailbox_a)
            assert email_id, "Failed to import email"
            email_ids.append(email_id)

            initial_counts = get_mailbox_counts(api_url, token, account_id, mailbox_b)
            assert initial_counts is not None, "Failed to get initial counts for B"
            initial_total = initial_counts.get("totalEmails", 0)

            response = email_set_update(
                api_url, token, account_id, email_id,
                {f"mailboxIds/{mailbox_b}": True},
            )
            assert "methodResponses" in response, f"No methodResponses: {response}"

            new_counts = get_mailbox_counts(api_url, token, account_id, mailbox_b)
            assert new_counts is not None, "Failed to get new counts for B"
            new_total = new_counts.get("totalEmails", 0)

            assert new_total == initial_total + 1, (
                f"Expected totalEmails {initial_total + 1}, got {new_total}"
            )
        finally:
            if email_ids:
                destroy_emails_and_verify_cleanup(api_url, token, account_id, email_ids)

    def test_total_emails_decrement_on_remove(self, api_url, upload_url, token, account_id):
        """
        Test: totalEmails decreases when email removed from mailbox.

        RFC 8621 Section 2: totalEmails is the count of emails in mailbox.
        """
        email_ids = []
        try:
            mailbox_a = create_test_mailbox(api_url, token, account_id)
            assert mailbox_a, "Failed to create mailbox A"

            mailbox_b = create_test_mailbox(api_url, token, account_id)
            assert mailbox_b, "Failed to create mailbox B"

            email_id = import_test_email(api_url, upload_url, token, account_id, mailbox_a)
            assert email_id, "Failed to import email"
            email_ids.append(email_id)

            # Add to mailbox B first
            response = email_set_update(
                api_url, token, account_id, email_id,
                {f"mailboxIds/{mailbox_b}": True},
            )
            assert "methodResponses" in response

            # Get counts for mailbox A before removal
            initial_counts = get_mailbox_counts(api_url, token, account_id, mailbox_a)
            assert initial_counts is not None, "Failed to get initial counts for A"
            initial_total = initial_counts.get("totalEmails", 0)

            # Remove from mailbox A
            response = email_set_update(
                api_url, token, account_id, email_id,
                {f"mailboxIds/{mailbox_a}": None},
            )
            assert "methodResponses" in response, f"No methodResponses: {response}"

            new_counts = get_mailbox_counts(api_url, token, account_id, mailbox_a)
            assert new_counts is not None, "Failed to get new counts for A"
            new_total = new_counts.get("totalEmails", 0)

            assert new_total == initial_total - 1, (
                f"Expected totalEmails {initial_total - 1}, got {new_total}"
            )
        finally:
            if email_ids:
                destroy_emails_and_verify_cleanup(api_url, token, account_id, email_ids)

    def test_unread_emails_update_on_move(self, api_url, upload_url, token, account_id):
        """
        Test: unreadEmails adjusts when unread email moves between mailboxes.

        RFC 8621 Section 2: unreadEmails counts emails without $seen keyword.
        """
        email_ids = []
        try:
            mailbox_a = create_test_mailbox(api_url, token, account_id)
            assert mailbox_a, "Failed to create mailbox A"

            mailbox_b = create_test_mailbox(api_url, token, account_id)
            assert mailbox_b, "Failed to create mailbox B"

            # Import UNREAD email (no $seen keyword)
            email_id = import_test_email(
                api_url, upload_url, token, account_id, mailbox_a, keywords={}
            )
            assert email_id, "Failed to import unread email"
            email_ids.append(email_id)

            initial_counts_a = get_mailbox_counts(api_url, token, account_id, mailbox_a)
            initial_counts_b = get_mailbox_counts(api_url, token, account_id, mailbox_b)
            assert initial_counts_a is not None and initial_counts_b is not None, (
                "Failed to get initial counts"
            )

            initial_unread_a = initial_counts_a.get("unreadEmails", 0)
            initial_unread_b = initial_counts_b.get("unreadEmails", 0)

            # Move email from A to B
            response = email_set_update(
                api_url, token, account_id, email_id,
                {"mailboxIds": {mailbox_b: True}},
            )
            assert "methodResponses" in response, f"No methodResponses: {response}"

            new_counts_a = get_mailbox_counts(api_url, token, account_id, mailbox_a)
            new_counts_b = get_mailbox_counts(api_url, token, account_id, mailbox_b)
            assert new_counts_a is not None and new_counts_b is not None, (
                "Failed to get new counts"
            )

            new_unread_a = new_counts_a.get("unreadEmails", 0)
            new_unread_b = new_counts_b.get("unreadEmails", 0)

            assert new_unread_a == initial_unread_a - 1, (
                f"A: expected {initial_unread_a - 1}, got {new_unread_a}"
            )
            assert new_unread_b == initial_unread_b + 1, (
                f"B: expected {initial_unread_b + 1}, got {new_unread_b}"
            )
        finally:
            if email_ids:
                destroy_emails_and_verify_cleanup(api_url, token, account_id, email_ids)

    def test_read_email_no_unread_change(self, api_url, upload_url, token, account_id):
        """
        Test: Moving email with $seen keyword doesn't change unreadEmails.

        RFC 8621 Section 2: unreadEmails excludes emails with $seen keyword.
        """
        email_ids = []
        try:
            mailbox_a = create_test_mailbox(api_url, token, account_id)
            assert mailbox_a, "Failed to create mailbox A"

            mailbox_b = create_test_mailbox(api_url, token, account_id)
            assert mailbox_b, "Failed to create mailbox B"

            # Import READ email (with $seen keyword)
            email_id = import_test_email(
                api_url, upload_url, token, account_id, mailbox_a,
                keywords={"$seen": True},
            )
            assert email_id, "Failed to import read email"
            email_ids.append(email_id)

            initial_counts_a = get_mailbox_counts(api_url, token, account_id, mailbox_a)
            initial_counts_b = get_mailbox_counts(api_url, token, account_id, mailbox_b)
            assert initial_counts_a is not None and initial_counts_b is not None, (
                "Failed to get initial counts"
            )

            initial_unread_a = initial_counts_a.get("unreadEmails", 0)
            initial_unread_b = initial_counts_b.get("unreadEmails", 0)

            # Move email from A to B
            response = email_set_update(
                api_url, token, account_id, email_id,
                {"mailboxIds": {mailbox_b: True}},
            )
            assert "methodResponses" in response, f"No methodResponses: {response}"

            new_counts_a = get_mailbox_counts(api_url, token, account_id, mailbox_a)
            new_counts_b = get_mailbox_counts(api_url, token, account_id, mailbox_b)
            assert new_counts_a is not None and new_counts_b is not None, (
                "Failed to get new counts"
            )

            new_unread_a = new_counts_a.get("unreadEmails", 0)
            new_unread_b = new_counts_b.get("unreadEmails", 0)

            assert new_unread_a == initial_unread_a, (
                f"A unread changed: {initial_unread_a} -> {new_unread_a}"
            )
            assert new_unread_b == initial_unread_b, (
                f"B unread changed: {initial_unread_b} -> {new_unread_b}"
            )
        finally:
            if email_ids:
                destroy_emails_and_verify_cleanup(api_url, token, account_id, email_ids)


# =============================================================================
# Test: State Tracking (RFC 8620 Section 5.3)
# =============================================================================


class TestStateTracking:

    def test_email_set_returns_old_and_new_state(self, api_url, upload_url, token, account_id):
        """
        Test: Email/set response includes oldState and newState.

        RFC 8620 Section 5.3: /set response MUST include oldState and newState.
        """
        email_ids = []
        try:
            mailbox_a = create_test_mailbox(api_url, token, account_id)
            mailbox_b = create_test_mailbox(api_url, token, account_id)
            assert mailbox_a and mailbox_b, "Failed to create mailboxes"

            email_id = import_test_email(api_url, upload_url, token, account_id, mailbox_a)
            assert email_id, "Failed to import email"
            email_ids.append(email_id)

            response = email_set_update(
                api_url, token, account_id, email_id,
                {f"mailboxIds/{mailbox_b}": True},
            )

            assert "methodResponses" in response, f"No methodResponses: {response}"
            method_responses = response["methodResponses"]
            assert len(method_responses) > 0, "Empty methodResponses"

            response_name, response_data, _ = method_responses[0]
            assert response_name != "error", (
                f"JMAP error: {response_data.get('type')}: {response_data.get('description')}"
            )
            assert response_name == "Email/set", f"Unexpected method: {response_name}"

            old_state = response_data.get("oldState")
            new_state = response_data.get("newState")

            assert old_state is not None, "missing oldState"
            assert isinstance(old_state, str), f"oldState not a string: {type(old_state)}"
            assert new_state is not None, "missing newState"
            assert isinstance(new_state, str), f"newState not a string: {type(new_state)}"
        finally:
            if email_ids:
                destroy_emails_and_verify_cleanup(api_url, token, account_id, email_ids)

    def test_new_state_differs_after_update(self, api_url, upload_url, token, account_id):
        """
        Test: newState differs from oldState after successful update.

        RFC 8620 Section 5.3: State MUST change when data changes.
        """
        email_ids = []
        try:
            mailbox_a = create_test_mailbox(api_url, token, account_id)
            mailbox_b = create_test_mailbox(api_url, token, account_id)
            assert mailbox_a and mailbox_b, "Failed to create mailboxes"

            email_id = import_test_email(api_url, upload_url, token, account_id, mailbox_a)
            assert email_id, "Failed to import email"
            email_ids.append(email_id)

            response = email_set_update(
                api_url, token, account_id, email_id,
                {f"mailboxIds/{mailbox_b}": True},
            )

            assert "methodResponses" in response, f"No methodResponses: {response}"
            method_responses = response["methodResponses"]
            assert len(method_responses) > 0, "Empty methodResponses"

            response_name, response_data, _ = method_responses[0]
            assert response_name != "error", (
                f"JMAP error: {response_data.get('type')}: {response_data.get('description')}"
            )
            assert response_name == "Email/set", f"Unexpected method: {response_name}"

            old_state = response_data.get("oldState")
            new_state = response_data.get("newState")

            assert old_state is not None and new_state is not None, (
                f"Missing state: oldState={old_state}, newState={new_state}"
            )
            assert new_state != old_state, f"State unchanged: {old_state}"
        finally:
            if email_ids:
                destroy_emails_and_verify_cleanup(api_url, token, account_id, email_ids)

    def test_if_in_state_success(self, api_url, upload_url, token, account_id):
        """
        Test: Request with correct ifInState succeeds.

        RFC 8620 Section 5.3: ifInState is a precondition check.
        """
        email_ids = []
        try:
            mailbox_a = create_test_mailbox(api_url, token, account_id)
            mailbox_b = create_test_mailbox(api_url, token, account_id)
            assert mailbox_a and mailbox_b, "Failed to create mailboxes"

            email_id = import_test_email(api_url, upload_url, token, account_id, mailbox_a)
            assert email_id, "Failed to import email"
            email_ids.append(email_id)

            current_state = get_email_state(api_url, token, account_id)
            assert current_state, "Failed to get current state"

            response = email_set_update(
                api_url, token, account_id, email_id,
                {f"mailboxIds/{mailbox_b}": True},
                if_in_state=current_state,
            )

            assert "methodResponses" in response, f"No methodResponses: {response}"
            method_responses = response["methodResponses"]
            assert len(method_responses) > 0, "Empty methodResponses"

            response_name, response_data, _ = method_responses[0]
            assert response_name != "error", (
                f"JMAP error: {response_data.get('type')}: {response_data.get('description')}"
            )
            assert response_name == "Email/set", f"Unexpected method: {response_name}"

            updated = response_data.get("updated", {})
            not_updated = response_data.get("notUpdated", {})

            if email_id in not_updated:
                error = not_updated[email_id]
                raise AssertionError(
                    f"Not updated: {error.get('type')}: {error.get('description')}"
                )

            assert email_id in updated, (
                f"Email not in updated or notUpdated: {response_data}"
            )
        finally:
            if email_ids:
                destroy_emails_and_verify_cleanup(api_url, token, account_id, email_ids)

    def test_if_in_state_mismatch_error(self, api_url, upload_url, token, account_id):
        """
        Test: Wrong ifInState returns stateMismatch error, no changes applied.

        RFC 8620 Section 5.3: If state doesn't match, return stateMismatch error.
        """
        email_ids = []
        try:
            mailbox_a = create_test_mailbox(api_url, token, account_id)
            mailbox_b = create_test_mailbox(api_url, token, account_id)
            assert mailbox_a and mailbox_b, "Failed to create mailboxes"

            email_id = import_test_email(api_url, upload_url, token, account_id, mailbox_a)
            assert email_id, "Failed to import email"
            email_ids.append(email_id)

            wrong_state = "wrong-state-value-12345"
            response = email_set_update(
                api_url, token, account_id, email_id,
                {f"mailboxIds/{mailbox_b}": True},
                if_in_state=wrong_state,
            )

            assert "methodResponses" in response, f"No methodResponses: {response}"
            method_responses = response["methodResponses"]
            assert len(method_responses) > 0, "Empty methodResponses"

            response_name, response_data, _ = method_responses[0]

            assert response_name == "error", (
                f"Expected error response, got {response_name}"
            )
            error_type = response_data.get("type")
            assert error_type == "stateMismatch", (
                f"Expected stateMismatch, got {error_type}"
            )
        finally:
            if email_ids:
                destroy_emails_and_verify_cleanup(api_url, token, account_id, email_ids)

    def test_new_state_matches_subsequent_get(self, api_url, upload_url, token, account_id):
        """
        Test: newState from Email/set matches state from Email/get.

        RFC 8620 Section 5.3: States must be consistent across methods.
        """
        email_ids = []
        try:
            mailbox_a = create_test_mailbox(api_url, token, account_id)
            mailbox_b = create_test_mailbox(api_url, token, account_id)
            assert mailbox_a and mailbox_b, "Failed to create mailboxes"

            email_id = import_test_email(api_url, upload_url, token, account_id, mailbox_a)
            assert email_id, "Failed to import email"
            email_ids.append(email_id)

            response = email_set_update(
                api_url, token, account_id, email_id,
                {f"mailboxIds/{mailbox_b}": True},
            )

            assert "methodResponses" in response, f"No methodResponses: {response}"
            method_responses = response["methodResponses"]
            assert len(method_responses) > 0, "Empty methodResponses"

            response_name, response_data, _ = method_responses[0]
            assert response_name != "error", (
                f"JMAP error: {response_data.get('type')}: {response_data.get('description')}"
            )
            assert response_name == "Email/set", f"Unexpected method: {response_name}"

            new_state_from_set = response_data.get("newState")
            assert new_state_from_set, "No newState in Email/set response"

            state_from_get = get_email_state(api_url, token, account_id)
            assert state_from_get, "Failed to get state from Email/get"

            assert new_state_from_set == state_from_get, (
                f"Email/set newState={new_state_from_set}, Email/get state={state_from_get}"
            )
        finally:
            if email_ids:
                destroy_emails_and_verify_cleanup(api_url, token, account_id, email_ids)

    def test_mailbox_state_changes_on_email_update(self, api_url, upload_url, token, account_id):
        """
        Test: Mailbox state changes when Email mailboxIds updated (counters change).

        RFC 8621 Section 2: Mailbox counters are part of mailbox state.
        """
        email_ids = []
        try:
            mailbox_a = create_test_mailbox(api_url, token, account_id)
            mailbox_b = create_test_mailbox(api_url, token, account_id)
            assert mailbox_a and mailbox_b, "Failed to create mailboxes"

            email_id = import_test_email(api_url, upload_url, token, account_id, mailbox_a)
            assert email_id, "Failed to import email"
            email_ids.append(email_id)

            initial_mailbox_state = get_mailbox_state(api_url, token, account_id)
            assert initial_mailbox_state, "Failed to get initial mailbox state"

            response = email_set_update(
                api_url, token, account_id, email_id,
                {"mailboxIds": {mailbox_b: True}},
            )
            assert "methodResponses" in response, f"No methodResponses: {response}"

            new_mailbox_state = get_mailbox_state(api_url, token, account_id)
            assert new_mailbox_state, "Failed to get new mailbox state"

            assert new_mailbox_state != initial_mailbox_state, (
                f"Mailbox state unchanged: {initial_mailbox_state}"
            )
        finally:
            if email_ids:
                destroy_emails_and_verify_cleanup(api_url, token, account_id, email_ids)


# =============================================================================
# Test: Keyword Updates (RFC 8621 Section 4.4)
# =============================================================================


class TestKeywordUpdates:

    def test_add_seen_keyword_decreases_unread(self, api_url, upload_url, token, account_id):
        """
        Test: Adding $seen keyword decreases unreadEmails.

        RFC 8621 Section 2: unreadEmails counts emails WITHOUT $seen keyword.
        RFC 8621 Section 4.4: keywords is updatable.
        """
        email_ids = []
        try:
            mailbox_id = create_test_mailbox(api_url, token, account_id)
            assert mailbox_id, "Failed to create mailbox"

            # Import UNREAD email (no $seen keyword)
            email_id = import_test_email(
                api_url, upload_url, token, account_id, mailbox_id, keywords={}
            )
            assert email_id, "Failed to import unread email"
            email_ids.append(email_id)

            initial_counts = get_mailbox_counts(api_url, token, account_id, mailbox_id)
            assert initial_counts is not None, "Failed to get initial counts"
            initial_unread = initial_counts.get("unreadEmails", 0)

            # Add $seen keyword via patch
            response = email_set_update(
                api_url, token, account_id, email_id,
                {"keywords/$seen": True},
            )

            assert "methodResponses" in response, f"No methodResponses: {response}"
            method_responses = response["methodResponses"]
            assert len(method_responses) > 0, "Empty methodResponses"

            response_name, response_data, _ = method_responses[0]
            assert response_name != "error", (
                f"JMAP error: {response_data.get('type')}: {response_data.get('description')}"
            )
            assert response_name == "Email/set", f"Unexpected method: {response_name}"

            updated = response_data.get("updated", {})
            not_updated = response_data.get("notUpdated", {})

            if email_id in not_updated:
                error = not_updated[email_id]
                raise AssertionError(
                    f"Not updated: {error.get('type')}: {error.get('description')}"
                )
            assert email_id in updated, (
                f"Email not in updated or notUpdated: {response_data}"
            )

            # Verify keywords
            keywords = get_email_keywords(api_url, token, account_id, email_id)
            assert keywords is not None, "Failed to get keywords"
            assert keywords.get("$seen"), f"Expected $seen=true, got keywords: {keywords}"

            # Verify unread count decreased
            new_counts = get_mailbox_counts(api_url, token, account_id, mailbox_id)
            assert new_counts is not None, "Failed to get new counts"
            new_unread = new_counts.get("unreadEmails", 0)

            assert new_unread == initial_unread - 1, (
                f"Expected unreadEmails {initial_unread - 1}, got {new_unread}"
            )
        finally:
            if email_ids:
                destroy_emails_and_verify_cleanup(api_url, token, account_id, email_ids)

    def test_remove_seen_keyword_increases_unread(self, api_url, upload_url, token, account_id):
        """
        Test: Removing $seen keyword increases unreadEmails.

        RFC 8621 Section 2: unreadEmails counts emails WITHOUT $seen keyword.
        RFC 8620 Section 5.3: Use patch "keywords/$seen": null to remove.
        """
        email_ids = []
        try:
            mailbox_id = create_test_mailbox(api_url, token, account_id)
            assert mailbox_id, "Failed to create mailbox"

            # Import READ email (with $seen keyword)
            email_id = import_test_email(
                api_url, upload_url, token, account_id, mailbox_id,
                keywords={"$seen": True},
            )
            assert email_id, "Failed to import read email"
            email_ids.append(email_id)

            initial_counts = get_mailbox_counts(api_url, token, account_id, mailbox_id)
            assert initial_counts is not None, "Failed to get initial counts"
            initial_unread = initial_counts.get("unreadEmails", 0)

            # Remove $seen keyword via patch (set to null)
            response = email_set_update(
                api_url, token, account_id, email_id,
                {"keywords/$seen": None},
            )

            assert "methodResponses" in response, f"No methodResponses: {response}"
            method_responses = response["methodResponses"]
            assert len(method_responses) > 0, "Empty methodResponses"

            response_name, response_data, _ = method_responses[0]
            assert response_name != "error", (
                f"JMAP error: {response_data.get('type')}: {response_data.get('description')}"
            )
            assert response_name == "Email/set", f"Unexpected method: {response_name}"

            updated = response_data.get("updated", {})
            not_updated = response_data.get("notUpdated", {})

            if email_id in not_updated:
                error = not_updated[email_id]
                raise AssertionError(
                    f"Not updated: {error.get('type')}: {error.get('description')}"
                )
            assert email_id in updated, (
                f"Email not in updated or notUpdated: {response_data}"
            )

            # Verify keywords - after removing all keywords, server may return None or {}
            keywords = get_email_keywords(api_url, token, account_id, email_id)
            if keywords is None:
                keywords = {}
            assert not keywords.get("$seen"), (
                f"Expected $seen absent, got keywords: {keywords}"
            )

            # Verify unread count increased
            new_counts = get_mailbox_counts(api_url, token, account_id, mailbox_id)
            assert new_counts is not None, "Failed to get new counts"
            new_unread = new_counts.get("unreadEmails", 0)

            assert new_unread == initial_unread + 1, (
                f"Expected unreadEmails {initial_unread + 1}, got {new_unread}"
            )
        finally:
            if email_ids:
                destroy_emails_and_verify_cleanup(api_url, token, account_id, email_ids)

    def test_set_keywords_via_full_replacement(self, api_url, upload_url, token, account_id):
        """
        Test: Set keywords via full replacement.

        RFC 8621 Section 4.4: keywords is updatable as a full map replacement.
        """
        email_ids = []
        try:
            mailbox_id = create_test_mailbox(api_url, token, account_id)
            assert mailbox_id, "Failed to create mailbox"

            # Import email with some keywords
            email_id = import_test_email(
                api_url, upload_url, token, account_id, mailbox_id,
                keywords={"$seen": True, "$answered": True},
            )
            assert email_id, "Failed to import email"
            email_ids.append(email_id)

            # Verify initial keywords
            initial_keywords = get_email_keywords(api_url, token, account_id, email_id)
            assert initial_keywords is not None, "Failed to get initial keywords"
            assert initial_keywords.get("$seen") and initial_keywords.get("$answered"), (
                f"Expected $seen and $answered, got: {initial_keywords}"
            )

            # Replace keywords with entirely different set
            response = email_set_update(
                api_url, token, account_id, email_id,
                {"keywords": {"$flagged": True}},
            )

            assert "methodResponses" in response, f"No methodResponses: {response}"
            method_responses = response["methodResponses"]
            assert len(method_responses) > 0, "Empty methodResponses"

            response_name, response_data, _ = method_responses[0]
            assert response_name != "error", (
                f"JMAP error: {response_data.get('type')}: {response_data.get('description')}"
            )
            assert response_name == "Email/set", f"Unexpected method: {response_name}"

            updated = response_data.get("updated", {})
            not_updated = response_data.get("notUpdated", {})

            if email_id in not_updated:
                error = not_updated[email_id]
                raise AssertionError(
                    f"Not updated: {error.get('type')}: {error.get('description')}"
                )
            assert email_id in updated, (
                f"Email not in updated or notUpdated: {response_data}"
            )

            # Verify keywords replaced completely
            new_keywords = get_email_keywords(api_url, token, account_id, email_id)
            assert new_keywords is not None, "Failed to get new keywords"
            assert new_keywords.get("$flagged"), (
                f"Expected $flagged, got: {new_keywords}"
            )
            assert not new_keywords.get("$seen"), (
                f"Expected $seen absent, got: {new_keywords}"
            )
            assert not new_keywords.get("$answered"), (
                f"Expected $answered absent, got: {new_keywords}"
            )
        finally:
            if email_ids:
                destroy_emails_and_verify_cleanup(api_url, token, account_id, email_ids)

    def test_add_non_unread_keyword_no_counter_change(self, api_url, upload_url, token, account_id):
        """
        Test: Adding non-$seen keyword doesn't change unreadEmails.

        RFC 8621 Section 2: Only $seen affects unreadEmails count.
        """
        email_ids = []
        try:
            mailbox_id = create_test_mailbox(api_url, token, account_id)
            assert mailbox_id, "Failed to create mailbox"

            # Import UNREAD email
            email_id = import_test_email(
                api_url, upload_url, token, account_id, mailbox_id, keywords={}
            )
            assert email_id, "Failed to import email"
            email_ids.append(email_id)

            initial_counts = get_mailbox_counts(api_url, token, account_id, mailbox_id)
            assert initial_counts is not None, "Failed to get initial counts"
            initial_unread = initial_counts.get("unreadEmails", 0)

            # Add $flagged keyword via patch
            response = email_set_update(
                api_url, token, account_id, email_id,
                {"keywords/$flagged": True},
            )

            assert "methodResponses" in response, f"No methodResponses: {response}"
            method_responses = response["methodResponses"]
            assert len(method_responses) > 0, "Empty methodResponses"

            response_name, response_data, _ = method_responses[0]
            assert response_name != "error", (
                f"JMAP error: {response_data.get('type')}: {response_data.get('description')}"
            )
            assert response_name == "Email/set", f"Unexpected method: {response_name}"

            updated = response_data.get("updated", {})
            not_updated = response_data.get("notUpdated", {})

            if email_id in not_updated:
                error = not_updated[email_id]
                raise AssertionError(
                    f"Not updated: {error.get('type')}: {error.get('description')}"
                )
            assert email_id in updated, (
                f"Email not in updated or notUpdated: {response_data}"
            )

            # Verify keywords
            keywords = get_email_keywords(api_url, token, account_id, email_id)
            assert keywords is not None, "Failed to get keywords"
            assert keywords.get("$flagged"), f"Expected $flagged=true, got keywords: {keywords}"

            # Verify unread count unchanged
            new_counts = get_mailbox_counts(api_url, token, account_id, mailbox_id)
            assert new_counts is not None, "Failed to get new counts"
            new_unread = new_counts.get("unreadEmails", 0)

            assert new_unread == initial_unread, (
                f"Expected unreadEmails unchanged at {initial_unread}, got {new_unread}"
            )
        finally:
            if email_ids:
                destroy_emails_and_verify_cleanup(api_url, token, account_id, email_ids)

    def test_email_state_changes_on_keyword_update(self, api_url, upload_url, token, account_id):
        """
        Test: Email state changes when keywords updated.

        RFC 8620 Section 5.3: State MUST change when data changes.
        """
        email_ids = []
        try:
            mailbox_id = create_test_mailbox(api_url, token, account_id)
            assert mailbox_id, "Failed to create mailbox"

            email_id = import_test_email(
                api_url, upload_url, token, account_id, mailbox_id, keywords={}
            )
            assert email_id, "Failed to import email"
            email_ids.append(email_id)

            # Update keywords
            response = email_set_update(
                api_url, token, account_id, email_id,
                {"keywords/$seen": True},
            )

            assert "methodResponses" in response, f"No methodResponses: {response}"
            method_responses = response["methodResponses"]
            assert len(method_responses) > 0, "Empty methodResponses"

            response_name, response_data, _ = method_responses[0]
            assert response_name != "error", (
                f"JMAP error: {response_data.get('type')}: {response_data.get('description')}"
            )
            assert response_name == "Email/set", f"Unexpected method: {response_name}"

            old_state = response_data.get("oldState")
            new_state = response_data.get("newState")

            assert old_state is not None and new_state is not None, (
                f"Missing state: oldState={old_state}, newState={new_state}"
            )
            assert new_state != old_state, f"State unchanged: {old_state}"

            # Verify newState matches subsequent Email/get
            state_from_get = get_email_state(api_url, token, account_id)
            assert state_from_get == new_state, (
                f"Email/set newState={new_state}, Email/get state={state_from_get}"
            )
        finally:
            if email_ids:
                destroy_emails_and_verify_cleanup(api_url, token, account_id, email_ids)

    def test_mailbox_state_changes_on_seen_keyword_update(self, api_url, upload_url, token, account_id):
        """
        Test: Mailbox state changes when $seen keyword changes (affects unreadEmails).

        RFC 8621 Section 2: Mailbox counters are part of mailbox state.
        """
        email_ids = []
        try:
            mailbox_id = create_test_mailbox(api_url, token, account_id)
            assert mailbox_id, "Failed to create mailbox"

            # Import UNREAD email
            email_id = import_test_email(
                api_url, upload_url, token, account_id, mailbox_id, keywords={}
            )
            assert email_id, "Failed to import email"
            email_ids.append(email_id)

            initial_mailbox_state = get_mailbox_state(api_url, token, account_id)
            assert initial_mailbox_state, "Failed to get initial mailbox state"

            # Add $seen keyword (changes unreadEmails counter)
            response = email_set_update(
                api_url, token, account_id, email_id,
                {"keywords/$seen": True},
            )

            assert "methodResponses" in response, f"No methodResponses: {response}"
            method_responses = response["methodResponses"]
            assert len(method_responses) > 0, "Empty methodResponses"

            response_name, response_data, _ = method_responses[0]
            assert response_name != "error", (
                f"JMAP error: {response_data.get('type')}: {response_data.get('description')}"
            )

            new_mailbox_state = get_mailbox_state(api_url, token, account_id)
            assert new_mailbox_state, "Failed to get new mailbox state"

            assert new_mailbox_state != initial_mailbox_state, (
                f"Mailbox state unchanged: {initial_mailbox_state}"
            )
        finally:
            if email_ids:
                destroy_emails_and_verify_cleanup(api_url, token, account_id, email_ids)


# =============================================================================
# Test: Invalid Patch Errors (RFC 8620 Section 5.3)
# =============================================================================


class TestInvalidPatches:

    def test_invalid_patch_non_existent_property(self, api_url, upload_url, token, account_id):
        """
        Test: Patch to non-existent property returns invalidPatch/invalidProperties.

        RFC 8620 Section 5.3: Invalid patch paths must be rejected.
        """
        email_ids = []
        try:
            mailbox_id = create_test_mailbox(api_url, token, account_id)
            assert mailbox_id, "Failed to create mailbox"

            email_id = import_test_email(api_url, upload_url, token, account_id, mailbox_id)
            assert email_id, "Failed to import email"
            email_ids.append(email_id)

            response = email_set_update(
                api_url, token, account_id, email_id,
                {"nonExistent/foo": True},
            )

            assert "methodResponses" in response, f"No methodResponses: {response}"
            method_responses = response["methodResponses"]
            assert len(method_responses) > 0, "Empty methodResponses"

            response_name, response_data, _ = method_responses[0]

            if response_name == "error":
                # Method-level error is acceptable
                return

            assert response_name == "Email/set", f"Unexpected method: {response_name}"

            not_updated = response_data.get("notUpdated", {})
            updated = response_data.get("updated", {})

            assert email_id not in updated, (
                "Update succeeded when it should have failed"
            )
            assert email_id in not_updated, (
                f"Email not in updated or notUpdated: {response_data}"
            )
        finally:
            if email_ids:
                destroy_emails_and_verify_cleanup(api_url, token, account_id, email_ids)

    def test_invalid_patch_nested_non_existent_path(self, api_url, upload_url, token, account_id):
        """
        Test: Patch to nested path that doesn't exist returns error.

        RFC 8620 Section 5.3: "All parts prior to the last MUST already exist
        on the object being patched."
        """
        email_ids = []
        try:
            mailbox_id = create_test_mailbox(api_url, token, account_id)
            assert mailbox_id, "Failed to create mailbox"

            email_id = import_test_email(api_url, upload_url, token, account_id, mailbox_id)
            assert email_id, "Failed to import email"
            email_ids.append(email_id)

            response = email_set_update(
                api_url, token, account_id, email_id,
                {"keywords/nested/deep": True},
            )

            assert "methodResponses" in response, f"No methodResponses: {response}"
            method_responses = response["methodResponses"]
            assert len(method_responses) > 0, "Empty methodResponses"

            response_name, response_data, _ = method_responses[0]

            if response_name == "error":
                # Method-level error is acceptable
                return

            assert response_name == "Email/set", f"Unexpected method: {response_name}"

            not_updated = response_data.get("notUpdated", {})
            updated = response_data.get("updated", {})

            assert email_id not in updated, (
                "Update succeeded when it should have failed"
            )
            assert email_id in not_updated, (
                f"Email not in updated or notUpdated: {response_data}"
            )
        finally:
            if email_ids:
                destroy_emails_and_verify_cleanup(api_url, token, account_id, email_ids)

    def test_invalid_patch_immutable_property(self, api_url, upload_url, token, account_id):
        """
        Test: Patch to server-set/immutable property returns invalidProperties.

        RFC 8621 Section 4: id and receivedAt are server-set properties that
        cannot be modified after creation.
        """
        email_ids = []
        try:
            mailbox_id = create_test_mailbox(api_url, token, account_id)
            assert mailbox_id, "Failed to create mailbox"

            email_id = import_test_email(api_url, upload_url, token, account_id, mailbox_id)
            assert email_id, "Failed to import email"
            email_ids.append(email_id)

            response = email_set_update(
                api_url, token, account_id, email_id,
                {"receivedAt": "2020-01-01T00:00:00Z"},
            )

            assert "methodResponses" in response, f"No methodResponses: {response}"
            method_responses = response["methodResponses"]
            assert len(method_responses) > 0, "Empty methodResponses"

            response_name, response_data, _ = method_responses[0]

            if response_name == "error":
                # Method-level error is acceptable
                return

            assert response_name == "Email/set", f"Unexpected method: {response_name}"

            not_updated = response_data.get("notUpdated", {})
            updated = response_data.get("updated", {})

            assert email_id not in updated, (
                "Update succeeded when it should have failed (receivedAt is immutable)"
            )
            assert email_id in not_updated, (
                f"Email not in updated or notUpdated: {response_data}"
            )
        finally:
            if email_ids:
                destroy_emails_and_verify_cleanup(api_url, token, account_id, email_ids)
