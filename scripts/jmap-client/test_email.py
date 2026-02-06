"""
Email-specific JMAP tests.

Tests for Email/import and Email/get methods per RFC 8621.
"""

import base64
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
    destroy_mailbox,
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

        # Cleanup: destroy the test email and mailbox
        destroy_emails_and_verify_cleanup(
            api_url, token, account_id, [email_id],
        )
        result = destroy_mailbox(api_url, token, account_id, mailbox_id, on_destroy_remove_emails=True)
        assert result.get("methodName") == "Mailbox/set", f"Unexpected: {result}"
        assert mailbox_id in result.get("destroyed", []), f"Mailbox not destroyed: {result}"

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
        result = destroy_mailbox(api_url, token, account_id, mailbox_id, on_destroy_remove_emails=True)
        assert result.get("methodName") == "Mailbox/set", f"Unexpected: {result}"
        assert mailbox_id in result.get("destroyed", []), f"Mailbox not destroyed: {result}"

    def test_query_data_setup(self, query_data):
        """Verify test data was set up correctly."""
        assert len(query_data["email_ids"]) == 3
        assert query_data["mailbox_id"] is not None


class TestEmailBodyValues:
    """Tests for Email bodyValues property per RFC 8621."""

    @pytest.fixture(scope="class")
    def body_values_data(self, api_url, upload_url, token, account_id):
        """Set up test emails for bodyValues testing."""
        mailbox_id = create_test_mailbox(api_url, token, account_id)
        assert mailbox_id, "Failed to create test mailbox"

        email_ids = {}

        # Email 1: Multipart/alternative with text/plain and text/html
        unique_id = str(uuid.uuid4())
        message_id_multipart = f"<body-values-multipart-{unique_id}@jmap-test.example>"
        date_str = datetime.now(timezone.utc).strftime("%a, %d %b %Y %H:%M:%S %z")

        multipart_email = f"""From: Body Values Test <sender@example.com>
To: Test Recipient <recipient@example.com>
Subject: Body Values Multipart Test
Date: {date_str}
Message-ID: {message_id_multipart}
MIME-Version: 1.0
Content-Type: multipart/alternative; boundary="boundary123"

--boundary123
Content-Type: text/plain; charset=utf-8

This is the plain text body content for bodyValues testing.
--boundary123
Content-Type: text/html; charset=utf-8

<html><body><p>This is the HTML body content.</p></body></html>
--boundary123--
""".replace("\n", "\r\n")

        blob_id = upload_email_blob(upload_url, token, account_id, multipart_email)
        assert blob_id, "Failed to upload multipart email"

        import_call = [
            "Email/import",
            {
                "accountId": account_id,
                "emails": {
                    "email": {
                        "blobId": blob_id,
                        "mailboxIds": {mailbox_id: True},
                        "receivedAt": datetime.now(timezone.utc).strftime(
                            "%Y-%m-%dT%H:%M:%SZ"
                        ),
                    }
                },
            },
            "importMultipart",
        ]

        import_response = make_jmap_request(api_url, token, [import_call])
        assert "methodResponses" in import_response, (
            f"No methodResponses: {import_response}"
        )
        response_name, response_data, _ = import_response["methodResponses"][0]
        assert response_name == "Email/import", f"Unexpected: {response_name}"
        created = response_data.get("created", {})
        assert "email" in created, f"Multipart email not created: {response_data}"
        email_ids["multipart"] = created["email"]["id"]

        # Email 2: Invalid charset for encoding problem test
        unique_id2 = str(uuid.uuid4())
        message_id_invalid = f"<body-values-invalid-{unique_id2}@jmap-test.example>"

        invalid_charset_email = f"""From: Invalid Charset Test <sender@example.com>
To: Test Recipient <recipient@example.com>
Subject: Invalid Charset Test
Date: {date_str}
Message-ID: {message_id_invalid}
MIME-Version: 1.0
Content-Type: text/plain; charset=bogus-nonexistent-charset

Some text content with invalid charset declaration.
""".replace("\n", "\r\n")

        blob_id2 = upload_email_blob(upload_url, token, account_id, invalid_charset_email)
        assert blob_id2, "Failed to upload invalid charset email"

        import_call2 = [
            "Email/import",
            {
                "accountId": account_id,
                "emails": {
                    "email": {
                        "blobId": blob_id2,
                        "mailboxIds": {mailbox_id: True},
                        "receivedAt": datetime.now(timezone.utc).strftime(
                            "%Y-%m-%dT%H:%M:%SZ"
                        ),
                    }
                },
            },
            "importInvalidCharset",
        ]

        import_response2 = make_jmap_request(api_url, token, [import_call2])
        assert "methodResponses" in import_response2, (
            f"No methodResponses: {import_response2}"
        )
        response_name2, response_data2, _ = import_response2["methodResponses"][0]
        assert response_name2 == "Email/import", f"Unexpected: {response_name2}"
        created2 = response_data2.get("created", {})
        assert "email" in created2, f"Invalid charset email not created: {response_data2}"
        email_ids["invalid_charset"] = created2["email"]["id"]

        data = {
            "mailbox_id": mailbox_id,
            "email_ids": email_ids,
            "account_id": account_id,
        }

        yield data

        # Cleanup
        all_email_ids = list(email_ids.values())
        destroy_emails_and_verify_cleanup(api_url, token, account_id, all_email_ids)
        result = destroy_mailbox(
            api_url, token, account_id, mailbox_id, on_destroy_remove_emails=True
        )
        assert result.get("methodName") == "Mailbox/set", f"Unexpected: {result}"
        assert mailbox_id in result.get("destroyed", []), (
            f"Mailbox not destroyed: {result}"
        )

    def test_fetch_text_body_values(self, body_values_data, api_url, token, account_id):
        """Test fetchTextBodyValues returns plain text body content."""
        email_id = body_values_data["email_ids"]["multipart"]

        email_get_call = [
            "Email/get",
            {
                "accountId": account_id,
                "ids": [email_id],
                "properties": ["id", "bodyValues", "textBody"],
                "fetchTextBodyValues": True,
            },
            "getTextBody",
        ]

        response = make_jmap_request(api_url, token, [email_get_call])
        assert "methodResponses" in response, f"No methodResponses: {response}"

        resp_name, resp_data, _ = response["methodResponses"][0]
        assert resp_name == "Email/get", f"Unexpected method: {resp_name}"

        emails = resp_data.get("list", [])
        assert len(emails) == 1, f"Expected 1 email, got {len(emails)}"

        email = emails[0]
        body_values = email.get("bodyValues", {})
        assert body_values, "bodyValues is empty or missing"

        # Find the text body part ID
        text_body = email.get("textBody", [])
        assert text_body, "textBody is empty or missing"
        text_part_id = text_body[0].get("partId")
        assert text_part_id, "No partId in textBody"

        # Verify bodyValues contains the text part
        assert text_part_id in body_values, (
            f"partId {text_part_id} not in bodyValues: {body_values.keys()}"
        )

        body_value = body_values[text_part_id]
        assert "value" in body_value, f"No 'value' in bodyValue: {body_value}"
        assert "plain text body content" in body_value["value"], (
            f"Expected text content not found: {body_value['value']}"
        )

        # Verify flags
        assert body_value.get("isEncodingProblem", False) is False, (
            "isEncodingProblem should be False"
        )
        assert body_value.get("isTruncated", False) is False, (
            "isTruncated should be False"
        )

    def test_fetch_html_body_values(self, body_values_data, api_url, token, account_id):
        """Test fetchHTMLBodyValues returns HTML body content."""
        email_id = body_values_data["email_ids"]["multipart"]

        email_get_call = [
            "Email/get",
            {
                "accountId": account_id,
                "ids": [email_id],
                "properties": ["id", "bodyValues", "htmlBody"],
                "fetchHTMLBodyValues": True,
            },
            "getHtmlBody",
        ]

        response = make_jmap_request(api_url, token, [email_get_call])
        assert "methodResponses" in response, f"No methodResponses: {response}"

        resp_name, resp_data, _ = response["methodResponses"][0]
        assert resp_name == "Email/get", f"Unexpected method: {resp_name}"

        emails = resp_data.get("list", [])
        assert len(emails) == 1, f"Expected 1 email, got {len(emails)}"

        email = emails[0]
        body_values = email.get("bodyValues", {})
        assert body_values, "bodyValues is empty or missing"

        # Find the HTML body part ID
        html_body = email.get("htmlBody", [])
        assert html_body, "htmlBody is empty or missing"
        html_part_id = html_body[0].get("partId")
        assert html_part_id, "No partId in htmlBody"

        # Verify bodyValues contains the HTML part
        assert html_part_id in body_values, (
            f"partId {html_part_id} not in bodyValues: {body_values.keys()}"
        )

        body_value = body_values[html_part_id]
        assert "value" in body_value, f"No 'value' in bodyValue: {body_value}"
        assert "HTML body content" in body_value["value"], (
            f"Expected HTML content not found: {body_value['value']}"
        )

    def test_max_body_value_bytes_truncation(
        self, body_values_data, api_url, token, account_id
    ):
        """Test maxBodyValueBytes truncates content and sets isTruncated flag."""
        email_id = body_values_data["email_ids"]["multipart"]
        max_bytes = 20

        email_get_call = [
            "Email/get",
            {
                "accountId": account_id,
                "ids": [email_id],
                "properties": ["id", "bodyValues", "textBody"],
                "fetchTextBodyValues": True,
                "maxBodyValueBytes": max_bytes,
            },
            "getTruncatedBody",
        ]

        response = make_jmap_request(api_url, token, [email_get_call])
        assert "methodResponses" in response, f"No methodResponses: {response}"

        resp_name, resp_data, _ = response["methodResponses"][0]
        assert resp_name == "Email/get", f"Unexpected method: {resp_name}"

        emails = resp_data.get("list", [])
        assert len(emails) == 1, f"Expected 1 email, got {len(emails)}"

        email = emails[0]
        body_values = email.get("bodyValues", {})
        assert body_values, "bodyValues is empty or missing"

        text_body = email.get("textBody", [])
        assert text_body, "textBody is empty or missing"
        text_part_id = text_body[0].get("partId")

        body_value = body_values.get(text_part_id, {})
        assert body_value.get("isTruncated") is True, (
            f"isTruncated should be True: {body_value}"
        )

        # Verify value length does not exceed maxBodyValueBytes
        value = body_value.get("value", "")
        assert len(value.encode("utf-8")) <= max_bytes, (
            f"Value length {len(value.encode('utf-8'))} exceeds max {max_bytes}"
        )

    def test_body_values_without_fetch_flags(
        self, body_values_data, api_url, token, account_id
    ):
        """Test bodyValues is empty when no fetch flags are set."""
        email_id = body_values_data["email_ids"]["multipart"]

        email_get_call = [
            "Email/get",
            {
                "accountId": account_id,
                "ids": [email_id],
                "properties": ["id", "bodyValues"],
                # No fetchTextBodyValues or fetchHTMLBodyValues
            },
            "getNoFetchFlags",
        ]

        response = make_jmap_request(api_url, token, [email_get_call])
        assert "methodResponses" in response, f"No methodResponses: {response}"

        resp_name, resp_data, _ = response["methodResponses"][0]
        assert resp_name == "Email/get", f"Unexpected method: {resp_name}"

        emails = resp_data.get("list", [])
        assert len(emails) == 1, f"Expected 1 email, got {len(emails)}"

        email = emails[0]
        body_values = email.get("bodyValues", {})

        # bodyValues should be empty when no fetch flags are set
        assert body_values == {}, (
            f"bodyValues should be empty without fetch flags: {body_values}"
        )

    def test_encoding_problem(self, body_values_data, api_url, token, account_id):
        """Test isEncodingProblem flag when charset is invalid/undecodable."""
        email_id = body_values_data["email_ids"]["invalid_charset"]

        email_get_call = [
            "Email/get",
            {
                "accountId": account_id,
                "ids": [email_id],
                "properties": ["id", "bodyValues", "textBody"],
                "fetchTextBodyValues": True,
            },
            "getEncodingProblem",
        ]

        response = make_jmap_request(api_url, token, [email_get_call])
        assert "methodResponses" in response, f"No methodResponses: {response}"

        resp_name, resp_data, _ = response["methodResponses"][0]
        assert resp_name == "Email/get", f"Unexpected method: {resp_name}"

        emails = resp_data.get("list", [])
        assert len(emails) == 1, f"Expected 1 email, got {len(emails)}"

        email = emails[0]
        body_values = email.get("bodyValues", {})
        assert body_values, "bodyValues is empty or missing"

        text_body = email.get("textBody", [])
        assert text_body, "textBody is empty or missing"
        text_part_id = text_body[0].get("partId")

        body_value = body_values.get(text_part_id, {})

        # isEncodingProblem should be True for invalid charset
        assert body_value.get("isEncodingProblem") is True, (
            f"isEncodingProblem should be True for invalid charset: {body_value}"
        )

        # value should still contain best-effort decoded content
        value = body_value.get("value", "")
        assert value, "value should contain best-effort decoded content"


class TestEmailWithAttachment:
    """Tests for Email/import with base64-encoded attachment (separate blob upload)."""

    # Known test content for the "PDF" attachment
    ATTACHMENT_CONTENT = b"This is fake PDF content for testing base64 attachment handling."
    TEXT_BODY = "This is the plain text body for attachment testing."

    @pytest.fixture(scope="class")
    def attachment_email_data(self, jmap_client, api_url, upload_url, token, account_id):
        """Import a multipart/mixed email with a base64-encoded attachment."""
        mailbox_id = create_test_mailbox(api_url, token, account_id)
        assert mailbox_id, "Failed to create test mailbox"

        unique_id = str(uuid.uuid4())
        message_id = f"<attachment-test-{unique_id}@jmap-test.example>"
        date_str = datetime.now(timezone.utc).strftime("%a, %d %b %Y %H:%M:%S %z")

        attachment_b64 = base64.b64encode(self.ATTACHMENT_CONTENT).decode("ascii")

        email_content = f"""From: Attachment Test <sender@example.com>
To: Test Recipient <recipient@example.com>
Subject: Attachment Test Email
Date: {date_str}
Message-ID: {message_id}
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary="mixed-boundary-{unique_id[:8]}"

--mixed-boundary-{unique_id[:8]}
Content-Type: text/plain; charset=utf-8

{self.TEXT_BODY}
--mixed-boundary-{unique_id[:8]}
Content-Type: application/pdf; name="test.pdf"
Content-Transfer-Encoding: base64
Content-Disposition: attachment; filename="test.pdf"

{attachment_b64}
--mixed-boundary-{unique_id[:8]}--
""".replace("\n", "\r\n")

        blob_id = upload_email_blob(upload_url, token, account_id, email_content)
        assert blob_id, "Failed to upload attachment email blob"

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
            "importAttachment",
        ]

        import_response = make_jmap_request(api_url, token, [import_call])
        assert "methodResponses" in import_response, (
            f"No methodResponses: {import_response}"
        )

        response_name, response_data, _ = import_response["methodResponses"][0]
        assert response_name != "error", (
            f"JMAP error: {response_data.get('type')}: {response_data.get('description')}"
        )
        assert response_name == "Email/import", (
            f"Unexpected method: {response_name}"
        )

        created = response_data.get("created", {})
        if "email1" not in created:
            not_created = response_data.get("notCreated", {})
            if "email1" in not_created:
                error = not_created["email1"]
                pytest.fail(
                    f"Not created: {error.get('type')}: {error.get('description')}"
                )
            else:
                pytest.fail(f"No email1 in created or notCreated: {response_data}")

        email_id = created["email1"].get("id")
        assert email_id, f"No id in created email: {created['email1']}"

        data = {
            "email_id": email_id,
            "mailbox_id": mailbox_id,
            "account_id": account_id,
        }

        yield data

        # Cleanup
        destroy_emails_and_verify_cleanup(api_url, token, account_id, [email_id])
        result = destroy_mailbox(
            api_url, token, account_id, mailbox_id, on_destroy_remove_emails=True
        )
        assert result.get("methodName") == "Mailbox/set", f"Unexpected: {result}"
        assert mailbox_id in result.get("destroyed", []), (
            f"Mailbox not destroyed: {result}"
        )

    def _get_email(self, attachment_email_data, api_url, token, account_id, properties,
                   fetch_text_body_values=False):
        """Helper to call Email/get with given properties."""
        email_id = attachment_email_data["email_id"]
        args = {
            "accountId": account_id,
            "ids": [email_id],
            "properties": properties,
        }
        if fetch_text_body_values:
            args["fetchTextBodyValues"] = True

        email_get_call = ["Email/get", args, "getAttachmentEmail"]
        response = make_jmap_request(api_url, token, [email_get_call])
        assert "methodResponses" in response, f"No methodResponses: {response}"

        resp_name, resp_data, _ = response["methodResponses"][0]
        assert resp_name == "Email/get", f"Unexpected method: {resp_name}"

        emails = resp_data.get("list", [])
        assert len(emails) == 1, f"Expected 1 email, got {len(emails)}"
        return emails[0]

    def test_has_attachment(self, attachment_email_data, api_url, token, account_id):
        """Email with base64 attachment should have hasAttachment=true."""
        email = self._get_email(
            attachment_email_data, api_url, token, account_id,
            ["id", "hasAttachment"],
        )
        assert email.get("hasAttachment") is True, (
            f"hasAttachment should be True: {email}"
        )

    def test_body_structure_multipart_mixed(
        self, attachment_email_data, api_url, token, account_id
    ):
        """bodyStructure should show multipart/mixed with text/plain + application/pdf subparts."""
        email = self._get_email(
            attachment_email_data, api_url, token, account_id,
            ["id", "bodyStructure"],
        )

        body_structure = email.get("bodyStructure")
        assert body_structure, f"bodyStructure missing: {email}"

        assert body_structure.get("type") == "multipart/mixed", (
            f"Expected multipart/mixed, got: {body_structure.get('type')}"
        )

        sub_parts = body_structure.get("subParts", [])
        assert len(sub_parts) == 2, (
            f"Expected 2 subParts, got {len(sub_parts)}: {sub_parts}"
        )

        # First subpart: text/plain
        assert sub_parts[0].get("type") == "text/plain", (
            f"Expected text/plain, got: {sub_parts[0].get('type')}"
        )

        # Second subpart: application/pdf with attachment disposition
        assert sub_parts[1].get("type") == "application/pdf", (
            f"Expected application/pdf, got: {sub_parts[1].get('type')}"
        )
        assert sub_parts[1].get("disposition") == "attachment", (
            f"Expected disposition=attachment, got: {sub_parts[1].get('disposition')}"
        )
        assert sub_parts[1].get("name") == "test.pdf", (
            f"Expected name=test.pdf, got: {sub_parts[1].get('name')}"
        )

    def test_attachment_has_separate_blob(
        self, attachment_email_data, api_url, token, account_id
    ):
        """Base64-decoded attachment should have its own standalone blobId (not a byte-range composite)."""
        email = self._get_email(
            attachment_email_data, api_url, token, account_id,
            ["id", "bodyStructure"],
        )

        sub_parts = email["bodyStructure"]["subParts"]
        attachment_part = sub_parts[1]
        blob_id = attachment_part.get("blobId")

        assert blob_id, f"Attachment has no blobId: {attachment_part}"
        assert "," not in blob_id, (
            f"blobId contains comma (byte-range composite), expected standalone blob: {blob_id}"
        )

    def test_attachment_blob_downloadable(
        self, attachment_email_data, jmap_client, api_url, token, account_id
    ):
        """Attachment blob should be downloadable and contain decoded (not base64) content."""
        email = self._get_email(
            attachment_email_data, api_url, token, account_id,
            ["id", "bodyStructure"],
        )

        sub_parts = email["bodyStructure"]["subParts"]
        blob_id = sub_parts[1]["blobId"]

        download_url = jmap_client.jmap_session.download_url
        url = download_url.replace("{accountId}", account_id).replace("{blobId}", blob_id)

        # Step 1: Get redirect to signed URL
        response = requests.get(
            url,
            headers={"Authorization": f"Bearer {token}"},
            allow_redirects=False,
            timeout=30,
        )
        assert response.status_code == 302, (
            f"Expected 302 redirect, got {response.status_code}: {response.text}"
        )

        location = response.headers.get("Location")
        assert location, "No Location header in redirect"

        # Step 2: Follow signed URL to get content
        content_response = requests.get(location, timeout=30)
        assert content_response.status_code == 200, (
            f"Expected 200, got {content_response.status_code}: {content_response.text[:200]}"
        )

        assert content_response.content == self.ATTACHMENT_CONTENT, (
            f"Downloaded content mismatch. Expected {len(self.ATTACHMENT_CONTENT)} bytes, "
            f"got {len(content_response.content)} bytes. "
            f"Content: {content_response.content[:100]!r}"
        )

    def test_text_body_values(
        self, attachment_email_data, api_url, token, account_id
    ):
        """Plain text body should be accessible via fetchTextBodyValues."""
        email = self._get_email(
            attachment_email_data, api_url, token, account_id,
            ["id", "bodyValues", "textBody"],
            fetch_text_body_values=True,
        )

        body_values = email.get("bodyValues", {})
        assert body_values, "bodyValues is empty or missing"

        text_body = email.get("textBody", [])
        assert text_body, "textBody is empty or missing"
        text_part_id = text_body[0].get("partId")
        assert text_part_id, "No partId in textBody"

        assert text_part_id in body_values, (
            f"partId {text_part_id} not in bodyValues: {list(body_values.keys())}"
        )

        body_value = body_values[text_part_id]
        assert "value" in body_value, f"No 'value' in bodyValue: {body_value}"
        assert self.TEXT_BODY in body_value["value"], (
            f"Expected text body content not found: {body_value['value']}"
        )

    def test_attachments_array(
        self, attachment_email_data, api_url, token, account_id
    ):
        """The attachments property should contain the PDF attachment."""
        email = self._get_email(
            attachment_email_data, api_url, token, account_id,
            ["id", "attachments"],
        )

        attachments = email.get("attachments", [])
        assert len(attachments) == 1, (
            f"Expected 1 attachment, got {len(attachments)}: {attachments}"
        )

        attachment = attachments[0]
        assert attachment.get("type") == "application/pdf", (
            f"Expected application/pdf, got: {attachment.get('type')}"
        )
        assert attachment.get("name") == "test.pdf", (
            f"Expected name=test.pdf, got: {attachment.get('name')}"
        )
