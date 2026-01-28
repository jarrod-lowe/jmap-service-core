"""
Email-specific JMAP tests.

Tests for Email/import and Email/get methods per RFC 8621.
"""

import uuid
from datetime import datetime, timezone, timedelta

import pytest
import requests
from jmapc.methods import EmailGet

from helpers import (
    make_jmap_request,
    create_test_mailbox,
    upload_email_blob,
    destroy_emails_and_verify_cleanup,
)


class TestEmailImportAndGet:
    """Full lifecycle test: Mailbox/set -> Mailbox/get -> Email/import -> Email/get."""

    @pytest.fixture(scope="class")
    def test_data(self, jmap_client, account_id, api_url, upload_url, token):
        """Set up mailbox, import email, and return all test data for assertions."""
        # Step 1: Create test mailbox
        mailbox_id = create_test_mailbox(api_url, token, account_id)
        assert mailbox_id, "Mailbox/set failed to create mailbox"

        # Step 2: Verify mailbox exists
        mailbox_get_call = [
            "Mailbox/get",
            {"accountId": account_id, "ids": [mailbox_id]},
            "getMailbox0",
        ]
        response = make_jmap_request(api_url, token, [mailbox_get_call])
        assert "methodResponses" in response, f"No methodResponses: {response}"
        resp_name, resp_data, _ = response["methodResponses"][0]
        assert resp_name == "Mailbox/get", f"Unexpected method response: {resp_name}"
        mailboxes = resp_data.get("list", [])
        assert mailboxes and mailboxes[0].get("id") == mailbox_id, (
            f"Mailbox {mailbox_id} not found in response: {resp_data}"
        )

        # Step 3: Generate unique Message-ID
        unique_id = str(uuid.uuid4())
        message_id = f"<test-import-{unique_id}@jmap-test.example>"

        # Step 4: Create RFC 5322 email content
        date_str = datetime.now(timezone.utc).strftime("%a, %d %b %Y %H:%M:%S %z")
        email_content = f"""From: Test Sender <sender@example.com>
To: Test Recipient <recipient@example.com>
Subject: Test Email for E2E Import Verification
Date: {date_str}
Message-ID: {message_id}
Content-Type: text/plain; charset=utf-8

This is the test email body content for JMAP import verification.
""".replace("\n", "\r\n")

        # Step 5: Upload email as blob
        blob_id = upload_email_blob(upload_url, token, account_id, email_content)
        assert blob_id, "Upload email blob failed - no blobId returned"

        # Step 6: Call Email/import
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

        import_response = make_jmap_request(api_url, token, [import_call])
        assert "methodResponses" in import_response, (
            f"No methodResponses in response: {import_response}"
        )

        method_responses = import_response["methodResponses"]
        assert len(method_responses) > 0, "Empty methodResponses"

        response_name, response_data, _ = method_responses[0]
        assert response_name != "error", (
            f"JMAP error: {response_data.get('type')}: {response_data.get('description')}"
        )
        assert response_name == "Email/import", (
            f"Unexpected method response: {response_name}"
        )

        # Step 7: Extract created email ID
        created = response_data.get("created", {})
        if "email1" not in created:
            not_created = response_data.get("notCreated", {})
            if "email1" in not_created:
                error = not_created["email1"]
                pytest.fail(
                    f"Not created: {error.get('type')}: {error.get('description')}"
                )
            else:
                pytest.fail(
                    f"No email1 in created or notCreated: {response_data}"
                )

        email_id = created["email1"].get("id")
        assert email_id, f"No id in created email: {created['email1']}"

        # Step 8: Call Email/get via jmapc
        get_response = jmap_client.request(
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

        error_type = getattr(get_response, "type", None)
        assert not error_type, (
            f"JMAP error: {error_type}: {getattr(get_response, 'description', '')}"
        )

        emails = getattr(get_response, "data", None)
        assert emails and len(emails) > 0, "No emails in response"

        data = {
            "email": emails[0],
            "email_id": email_id,
            "message_id": message_id,
            "mailbox_id": mailbox_id,
        }

        yield data

        # Cleanup: destroy the test email
        destroy_emails_and_verify_cleanup(
            api_url, token, account_id, [email_id],
        )

    def test_email_get_returned_email(self, test_data):
        assert test_data["email"] is not None

    def test_email_id_matches(self, test_data):
        email = test_data["email"]
        email_id_from_get = getattr(email, "id", None)
        assert email_id_from_get == test_data["email_id"], (
            f"Expected {test_data['email_id']}, got {email_id_from_get}"
        )

    def test_email_from_matches(self, test_data):
        email = test_data["email"]
        from_addresses = getattr(email, "mail_from", None)
        assert from_addresses, "No from field"
        from_str = str(from_addresses)
        assert "sender@example.com" in from_str, (
            f"Expected 'sender@example.com' in {from_str}"
        )

    def test_email_to_matches(self, test_data):
        email = test_data["email"]
        to_addresses = getattr(email, "to", None)
        assert to_addresses, "No to field"
        to_str = str(to_addresses)
        assert "recipient@example.com" in to_str, (
            f"Expected 'recipient@example.com' in {to_str}"
        )

    def test_email_subject_matches(self, test_data):
        email = test_data["email"]
        subject = getattr(email, "subject", None)
        assert subject == "Test Email for E2E Import Verification", (
            f"Expected 'Test Email for E2E Import Verification', got '{subject}'"
        )

    def test_email_message_id_matches(self, test_data):
        email = test_data["email"]
        message_ids = getattr(email, "message_id", None)
        assert message_ids, "No messageId field"
        message_id_str = str(message_ids)
        expected_msg_id = test_data["message_id"].strip("<>")
        assert expected_msg_id in message_id_str, (
            f"Expected '{expected_msg_id}' in {message_id_str}"
        )

    def test_email_received_at_present(self, test_data):
        email = test_data["email"]
        received_at = getattr(email, "received_at", None)
        assert received_at, "No receivedAt field"

    def test_email_preview_matches(self, test_data):
        email = test_data["email"]
        preview = getattr(email, "preview", None)
        assert preview, "No preview field"
        assert "test email body content" in preview.lower(), (
            f"Expected 'test email body content' in '{preview}'"
        )


class TestEmailQuery:
    """Tests for Email/query method."""

    @pytest.fixture(scope="class")
    def query_data(self, api_url, upload_url, token, account_id):
        """Set up 3 test emails with staggered receivedAt times."""
        mailbox_id = create_test_mailbox(api_url, token, account_id)
        assert mailbox_id, "Failed to create test mailbox"

        email_ids = []
        received_ats = []

        for i in range(3):
            unique_id = str(uuid.uuid4())
            message_id = f"<query-test-{unique_id}@jmap-test.example>"

            received_at = datetime.now(timezone.utc) - timedelta(seconds=(2 - i))
            received_at_str = received_at.strftime("%Y-%m-%dT%H:%M:%SZ")
            received_ats.append(received_at_str)

            date_str = received_at.strftime("%a, %d %b %Y %H:%M:%S %z")
            email_content = f"""From: Query Test {i} <sender{i}@example.com>
To: Test Recipient <recipient@example.com>
Subject: Query Test Email {i}
Date: {date_str}
Message-ID: {message_id}
Content-Type: text/plain; charset=utf-8

This is query test email number {i}.
""".replace("\n", "\r\n")

            blob_id = upload_email_blob(upload_url, token, account_id, email_content)
            assert blob_id, f"Failed to upload query test email {i}"

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
                f"import{i}",
            ]

            import_response = make_jmap_request(api_url, token, [import_call])
            assert "methodResponses" in import_response, (
                f"No methodResponses for email {i}: {import_response}"
            )

            method_responses = import_response["methodResponses"]
            assert len(method_responses) > 0, f"Empty methodResponses for email {i}"

            response_name, response_data, _ = method_responses[0]
            assert response_name != "error", (
                f"JMAP error importing email {i}: "
                f"{response_data.get('type')}: {response_data.get('description')}"
            )
            assert response_name == "Email/import", (
                f"Unexpected method for email {i}: {response_name}"
            )

            created = response_data.get("created", {})
            assert "email" in created, (
                f"Email {i} not created: {response_data}"
            )

            email_id = created["email"].get("id")
            assert email_id, f"No id for email {i}: {created['email']}"
            email_ids.append(email_id)

        data = {
            "mailbox_id": mailbox_id,
            "email_ids": email_ids,
            "received_ats": received_ats,
            "account_id": account_id,
        }

        yield data

        # Cleanup
        destroy_emails_and_verify_cleanup(
            api_url, token, account_id, email_ids,
        )

    def test_query_data_setup(self, query_data):
        """Verify test data was set up correctly."""
        assert len(query_data["email_ids"]) == 3
        assert query_data["mailbox_id"] is not None
