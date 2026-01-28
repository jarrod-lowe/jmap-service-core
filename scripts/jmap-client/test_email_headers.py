"""
Email header property tests for RFC 8621 Section 4.1.3.

Tests for the header:{name}[:{form}][:{all}] property syntax.
"""

import uuid
from datetime import datetime, timezone

import pytest
import requests

from helpers import (
    make_jmap_request,
    create_test_mailbox,
    upload_email_blob,
    destroy_emails_and_verify_cleanup,
)


def email_get_with_properties(
    api_url: str, token: str, account_id: str, email_id: str, properties: list[str]
) -> dict | None:
    """
    Make a raw JMAP Email/get request with specific properties.

    Returns the first email object from the response, or None on error.
    """
    get_call = [
        "Email/get",
        {
            "accountId": account_id,
            "ids": [email_id],
            "properties": properties,
        },
        "getHeader",
    ]

    response = make_jmap_request(api_url, token, [get_call])

    if "methodResponses" not in response:
        return None

    method_responses = response["methodResponses"]
    if len(method_responses) == 0:
        return None

    response_name, response_data, _ = method_responses[0]
    if response_name == "error":
        # Return the error data so tests can check for invalidArguments
        return {"_error": response_data}

    emails = response_data.get("list", [])
    if not emails:
        return None

    return emails[0]


class TestEmailHeaders:
    """Tests for Email/get header:{name} properties (RFC 8621 Section 4.1.3)."""

    @pytest.fixture(scope="class")
    def header_test_email(self, api_url, upload_url, token, account_id):
        """Create a test email with crafted headers for all header property tests."""
        # Create test mailbox
        mailbox_id = create_test_mailbox(api_url, token, account_id)
        assert mailbox_id is not None, "Failed to create test mailbox"

        # Build RFC 5322 email with headers designed to test all parsed forms
        unique_id = str(uuid.uuid4())
        message_id = f"<header-test-{unique_id}@jmap-test.example>"

        email_content = f"""From: "Test Sender" <sender@example.com>
To: "Alice" <alice@example.com>, Bob <bob@example.com>
Subject: =?UTF-8?Q?Test_Subject_with_=C3=A9ncoding?=
Date: Mon, 15 Jan 2024 10:30:00 +0000
Message-ID: {message_id}
In-Reply-To: <parent-message@example.com>
References: <ref1@example.com> <ref2@example.com>
X-Custom-Header: first value
X-Custom-Header: second value
X-Custom-Header: third value
List-Unsubscribe: <mailto:unsub@example.com>, <https://example.com/unsub>
List-Post: <mailto:post@lists.example.com>
Resent-Date: Tue, 16 Jan 2024 11:45:00 +0000
Content-Type: text/plain; charset=utf-8

This is the test email body for header property testing.
""".replace("\n", "\r\n")

        # Upload email as blob
        blob_id = upload_email_blob(upload_url, token, account_id, email_content)
        assert blob_id is not None, "Failed to upload header test email blob"

        # Import email via Email/import
        import_call = [
            "Email/import",
            {
                "accountId": account_id,
                "emails": {
                    "headerTestEmail": {
                        "blobId": blob_id,
                        "mailboxIds": {mailbox_id: True},
                        "receivedAt": datetime.now(timezone.utc).strftime(
                            "%Y-%m-%dT%H:%M:%SZ"
                        ),
                    }
                },
            },
            "importHeaderTest",
        ]

        import_response = make_jmap_request(api_url, token, [import_call])
        assert "methodResponses" in import_response, (
            f"No methodResponses: {import_response}"
        )

        method_responses = import_response["methodResponses"]
        assert len(method_responses) > 0, "Empty methodResponses"

        response_name, response_data, _ = method_responses[0]
        assert response_name != "error", (
            f"JMAP error: {response_data.get('type')}: {response_data.get('description')}"
        )

        created = response_data.get("created", {})
        assert "headerTestEmail" in created, (
            f"headerTestEmail not in created: {response_data}"
        )

        email_id = created["headerTestEmail"].get("id")
        assert email_id is not None, (
            f"No id in created email: {created['headerTestEmail']}"
        )

        yield email_id

        # Cleanup
        destroy_emails_and_verify_cleanup(
            api_url, token, account_id, [email_id]
        )

    def test_raw_header_form(self, api_url, token, account_id, header_test_email):
        """
        Raw form - header:{name}

        RFC 8621 Section 4.1.3: Returns the last instance of the header field
        in Raw form.
        """
        email = email_get_with_properties(
            api_url, token, account_id, header_test_email, ["header:X-Custom-Header"]
        )

        assert email is not None, "No email returned"
        assert "_error" not in email, (
            f"JMAP error: {email['_error'].get('type')}: {email['_error'].get('description')}"
        )

        value = email.get("header:X-Custom-Header")
        assert value is not None and "third value" in value, (
            f"Expected 'third value', got: {value!r}"
        )

    def test_as_text_form(self, api_url, token, account_id, header_test_email):
        """
        Text form - header:{name}:asText

        RFC 8621 Section 4.1.2.2: Decodes MIME encoded-word syntax.
        """
        email = email_get_with_properties(
            api_url, token, account_id, header_test_email, ["header:Subject:asText"]
        )

        assert email is not None, "No email returned"
        assert "_error" not in email, (
            f"JMAP error: {email['_error'].get('type')}: {email['_error'].get('description')}"
        )

        value = email.get("header:Subject:asText")
        assert value is not None and "Test Subject with" in value and "ncoding" in value, (
            f"Expected decoded subject with 'Test Subject with...ncoding', got: {value!r}"
        )

    def test_as_addresses_form(self, api_url, token, account_id, header_test_email):
        """
        Addresses form - header:{name}:asAddresses

        RFC 8621 Section 4.1.2.3: Parses address-list into EmailAddress[] objects.
        """
        email = email_get_with_properties(
            api_url, token, account_id, header_test_email, ["header:From:asAddresses"]
        )

        assert email is not None, "No email returned"
        assert "_error" not in email, (
            f"JMAP error: {email['_error'].get('type')}: {email['_error'].get('description')}"
        )

        value = email.get("header:From:asAddresses")
        assert value is not None and isinstance(value, list), (
            f"Expected array of EmailAddress, got: {value}"
        )
        assert len(value) >= 1, f"Expected at least 1 address, got: {value}"

        first_addr = value[0]
        assert isinstance(first_addr, dict), f"Expected dict, got: {first_addr}"
        assert first_addr.get("email") == "sender@example.com", (
            f"Expected email='sender@example.com', got: {first_addr}"
        )
        assert first_addr.get("name") == "Test Sender", (
            f"Expected name='Test Sender', got: {first_addr}"
        )

    def test_as_addresses_multi(self, api_url, token, account_id, header_test_email):
        """
        Addresses form with multiple addresses - header:To:asAddresses

        RFC 8621: To header with multiple recipients parsed into EmailAddress[].
        """
        email = email_get_with_properties(
            api_url, token, account_id, header_test_email, ["header:To:asAddresses"]
        )

        assert email is not None, "No email returned"
        assert "_error" not in email, (
            f"JMAP error: {email['_error'].get('type')}: {email['_error'].get('description')}"
        )

        value = email.get("header:To:asAddresses")
        assert value is not None and isinstance(value, list), (
            f"Expected array of EmailAddress, got: {value}"
        )
        assert len(value) == 2, f"Expected 2 addresses, got {len(value)}: {value}"

        emails = [addr.get("email") for addr in value]
        assert "alice@example.com" in emails and "bob@example.com" in emails, (
            f"Expected alice@ and bob@, got: {emails}"
        )

    def test_as_message_ids_form(self, api_url, token, account_id, header_test_email):
        """
        MessageIds form - header:References:asMessageIds

        RFC 8621 Section 4.1.2.5: Parses msg-id list into String[].
        """
        email = email_get_with_properties(
            api_url, token, account_id, header_test_email,
            ["header:References:asMessageIds"],
        )

        assert email is not None, "No email returned"
        assert "_error" not in email, (
            f"JMAP error: {email['_error'].get('type')}: {email['_error'].get('description')}"
        )

        value = email.get("header:References:asMessageIds")
        assert value is not None and isinstance(value, list), (
            f"Expected array of message IDs, got: {value}"
        )
        assert len(value) == 2 and "ref1@example.com" in value and "ref2@example.com" in value, (
            f"Expected ['ref1@example.com', 'ref2@example.com'], got: {value}"
        )

    def test_as_date_form(self, api_url, token, account_id, header_test_email):
        """
        Date form - header:Date:asDate

        RFC 8621 Section 4.1.2.6: Parses date-time into ISO 8601 Date string.
        """
        email = email_get_with_properties(
            api_url, token, account_id, header_test_email, ["header:Date:asDate"]
        )

        assert email is not None, "No email returned"
        assert "_error" not in email, (
            f"JMAP error: {email['_error'].get('type')}: {email['_error'].get('description')}"
        )

        value = email.get("header:Date:asDate")
        assert value is not None and "2024-01-15" in value and "10:30:00" in value, (
            f"Expected ISO date containing '2024-01-15' and '10:30:00', got: {value}"
        )

    def test_as_urls_form(self, api_url, token, account_id, header_test_email):
        """
        URLs form - header:List-Unsubscribe:asURLs

        RFC 8621 Section 4.1.2.7: Parses URL list into String[].
        """
        email = email_get_with_properties(
            api_url, token, account_id, header_test_email,
            ["header:List-Unsubscribe:asURLs"],
        )

        assert email is not None, "No email returned"
        assert "_error" not in email, (
            f"JMAP error: {email['_error'].get('type')}: {email['_error'].get('description')}"
        )

        value = email.get("header:List-Unsubscribe:asURLs")
        assert value is not None and isinstance(value, list), (
            f"Expected array of URLs, got: {value}"
        )

        has_mailto = any("mailto:unsub@example.com" in url for url in value)
        has_https = any("https://example.com/unsub" in url for url in value)
        assert len(value) == 2 and has_mailto and has_https, (
            f"Expected 2 URLs (mailto and https), got: {value}"
        )

    def test_all_modifier(self, api_url, token, account_id, header_test_email):
        """
        :all modifier - header:X-Custom-Header:all

        RFC 8621 Section 4.1.3: Returns array of all instances in message order.
        """
        email = email_get_with_properties(
            api_url, token, account_id, header_test_email,
            ["header:X-Custom-Header:all"],
        )

        assert email is not None, "No email returned"
        assert "_error" not in email, (
            f"JMAP error: {email['_error'].get('type')}: {email['_error'].get('description')}"
        )

        value = email.get("header:X-Custom-Header:all")
        assert value is not None and isinstance(value, list), (
            f"Expected array, got: {value}"
        )
        assert len(value) == 3, f"Expected 3 instances, got {len(value)}: {value}"
        assert "first" in value[0] and "second" in value[1] and "third" in value[2], (
            f"Unexpected order: {value}"
        )

    def test_combined_form_and_all(self, api_url, token, account_id, header_test_email):
        """
        Combined form and :all - header:To:asAddresses:all

        RFC 8621 Section 4.1.3: Returns EmailAddress[][] (array of parsed results).
        """
        email = email_get_with_properties(
            api_url, token, account_id, header_test_email,
            ["header:To:asAddresses:all"],
        )

        assert email is not None, "No email returned"
        assert "_error" not in email, (
            f"JMAP error: {email['_error'].get('type')}: {email['_error'].get('description')}"
        )

        value = email.get("header:To:asAddresses:all")
        assert value is not None and isinstance(value, list), (
            f"Expected nested array, got: {value}"
        )
        assert len(value) >= 1 and isinstance(value[0], list), (
            f"Expected [[addr1, addr2]], got: {value}"
        )

        inner = value[0]
        assert len(inner) == 2, f"Expected 2 addresses in inner array, got: {inner}"

        emails = [addr.get("email") for addr in inner if isinstance(addr, dict)]
        assert "alice@example.com" in emails and "bob@example.com" in emails, (
            f"Expected alice@ and bob@, got: {emails}"
        )

    def test_case_insensitive_matching(
        self, api_url, token, account_id, header_test_email
    ):
        """
        Case-insensitive header name matching

        RFC 8621 Section 4.1.3: Header field names are matched case insensitively.
        """
        email = email_get_with_properties(
            api_url,
            token,
            account_id,
            header_test_email,
            ["header:SUBJECT", "header:subject", "header:Subject"],
        )

        assert email is not None, "No email returned"
        assert "_error" not in email, (
            f"JMAP error: {email['_error'].get('type')}: {email['_error'].get('description')}"
        )

        upper = email.get("header:SUBJECT")
        lower = email.get("header:subject")
        mixed = email.get("header:Subject")

        values = [v for v in [upper, lower, mixed] if v is not None]
        assert len(values) >= 1, f"No values returned for any casing: {email}"

        first_val = values[0]
        assert all(v == first_val for v in values), (
            f"Different values for different casings: SUBJECT={upper!r}, subject={lower!r}, Subject={mixed!r}"
        )

    def test_missing_header_single_form(
        self, api_url, token, account_id, header_test_email
    ):
        """
        Missing header returns null (single form).

        RFC 8621 Section 4.1.3: null if no header field exists.
        """
        email = email_get_with_properties(
            api_url,
            token,
            account_id,
            header_test_email,
            ["header:X-Nonexistent"],
        )

        assert email is not None, "No email returned"
        assert "_error" not in email, (
            f"JMAP error: {email['_error'].get('type')}: {email['_error'].get('description')}"
        )

        single = email.get("header:X-Nonexistent")
        assert single is None, f"Expected null, got: {single!r}"

    def test_missing_header_all_form(
        self, api_url, token, account_id, header_test_email
    ):
        """
        Missing header returns empty array (:all form).

        RFC 8621 Section 4.1.3: empty array if no header field exists.
        """
        email = email_get_with_properties(
            api_url,
            token,
            account_id,
            header_test_email,
            ["header:X-Nonexistent:all"],
        )

        assert email is not None, "No email returned"
        assert "_error" not in email, (
            f"JMAP error: {email['_error'].get('type')}: {email['_error'].get('description')}"
        )

        all_form = email.get("header:X-Nonexistent:all")
        assert all_form is not None and isinstance(all_form, list) and len(all_form) == 0, (
            f"Expected [], got: {all_form!r}"
        )

    def test_invalid_form_rejection(
        self, api_url, token, account_id, header_test_email
    ):
        """
        Invalid form combination returns invalidArguments error.

        RFC 8621 Section 4.1.3 (line 2421-2422):
        Attempting to fetch a form that is forbidden (e.g., "header:From:asDate")
        MUST result in the method call being rejected with an "invalidArguments" error.
        """
        email = email_get_with_properties(
            api_url, token, account_id, header_test_email, ["header:From:asDate"]
        )

        assert email is not None, "No response returned"
        assert "_error" in email, (
            f"Expected invalidArguments error, got successful response: {email}"
        )
        assert email["_error"].get("type") == "invalidArguments", (
            f"Expected 'invalidArguments', got: {email['_error'].get('type')}"
        )
