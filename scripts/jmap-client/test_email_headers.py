"""
Email header property tests for RFC 8621 Section 4.1.3.

Tests for the header:{name}[:{form}][:{all}] property syntax.
"""

import uuid
from datetime import datetime, timezone

import requests
from jmapc import Client

from test_email import create_test_mailbox, make_jmap_request


def import_header_test_email(
    api_url: str, upload_url: str, token: str, account_id: str, mailbox_id: str, results
) -> str | None:
    """
    Import an email with crafted headers for testing header property access.

    Creates an RFC 5322 email with:
    - Multiple instances of X-Custom-Header (to test :all)
    - MIME-encoded Subject (to test :asText)
    - Multiple addresses in To (to test :asAddresses)
    - References header (to test :asMessageIds)
    - Date header (to test :asDate)
    - List-Unsubscribe header (to test :asURLs)
    - Resent-Date header (to test additional Date-form header)

    Returns the created email ID, or None on failure.
    """
    unique_id = str(uuid.uuid4())
    message_id = f"<header-test-{unique_id}@jmap-test.example>"

    # RFC 5322 email with headers designed to test all parsed forms
    # Note: Subject uses MIME encoded-word for UTF-8 character
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
""".replace("\n", "\r\n")  # RFC 5322 requires CRLF

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
            results.record_fail(
                "Upload header test email blob",
                f"Got HTTP {upload_response.status_code}: {upload_response.text}",
            )
            return None
        upload_data = upload_response.json()
        blob_id = upload_data.get("blobId")
        if not blob_id:
            results.record_fail("Upload header test email blob", "No blobId in response")
            return None
    except Exception as e:
        results.record_fail("Upload header test email blob", str(e))
        return None

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

    try:
        import_response = make_jmap_request(api_url, token, [import_call])
    except Exception as e:
        results.record_fail("Email/import header test email", str(e))
        return None

    if "methodResponses" not in import_response:
        results.record_fail(
            "Email/import header test email",
            f"No methodResponses: {import_response}",
        )
        return None

    method_responses = import_response["methodResponses"]
    if len(method_responses) == 0:
        results.record_fail("Email/import header test email", "Empty methodResponses")
        return None

    response_name, response_data, _ = method_responses[0]
    if response_name == "error":
        results.record_fail(
            "Email/import header test email",
            f"JMAP error: {response_data.get('type')}: {response_data.get('description')}",
        )
        return None

    created = response_data.get("created", {})
    if "headerTestEmail" not in created:
        not_created = response_data.get("notCreated", {})
        if "headerTestEmail" in not_created:
            error = not_created["headerTestEmail"]
            results.record_fail(
                "Email/import header test email",
                f"Not created: {error.get('type')}: {error.get('description')}",
            )
        else:
            results.record_fail(
                "Email/import header test email",
                f"No headerTestEmail in created: {response_data}",
            )
        return None

    email_id = created["headerTestEmail"].get("id")
    if not email_id:
        results.record_fail(
            "Email/import header test email",
            f"No id in created email: {created['headerTestEmail']}",
        )
        return None

    results.record_pass("Import header test email", f"emailId: {email_id}")
    return email_id


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


def test_raw_header_form(
    api_url: str, token: str, account_id: str, email_id: str, results
):
    """
    Test 1: Raw form - header:{name}

    RFC 8621 Section 4.1.3: Returns the last instance of the header field
    in Raw form.
    """
    print()
    print("Testing header:{name} raw form...")

    email = email_get_with_properties(
        api_url, token, account_id, email_id, ["header:X-Custom-Header"]
    )

    if not email:
        results.record_fail("header:X-Custom-Header raw form", "No email returned")
        return

    if "_error" in email:
        results.record_fail(
            "header:X-Custom-Header raw form",
            f"JMAP error: {email['_error'].get('type')}: {email['_error'].get('description')}",
        )
        return

    # Raw form returns the last instance of multi-valued header
    value = email.get("header:X-Custom-Header")
    if value is not None and "third value" in value:
        results.record_pass("header:X-Custom-Header raw form", f"Value: {value!r}")
    else:
        results.record_fail(
            "header:X-Custom-Header raw form",
            f"Expected 'third value', got: {value!r}",
        )


def test_as_text_form(
    api_url: str, token: str, account_id: str, email_id: str, results
):
    """
    Test 2: Text form - header:{name}:asText

    RFC 8621 Section 4.1.2.2: Decodes MIME encoded-word syntax.
    """
    print()
    print("Testing header:{name}:asText form...")

    email = email_get_with_properties(
        api_url, token, account_id, email_id, ["header:Subject:asText"]
    )

    if not email:
        results.record_fail("header:Subject:asText", "No email returned")
        return

    if "_error" in email:
        results.record_fail(
            "header:Subject:asText",
            f"JMAP error: {email['_error'].get('type')}: {email['_error'].get('description')}",
        )
        return

    # Subject should be decoded from MIME encoded-word
    # =?UTF-8?Q?Test_Subject_with_=C3=A9ncoding?= -> "Test Subject with encoding"
    # (e with acute = U+00E9)
    value = email.get("header:Subject:asText")
    if value is not None and "Test Subject with" in value and "ncoding" in value:
        results.record_pass("header:Subject:asText", f"Value: {value!r}")
    else:
        results.record_fail(
            "header:Subject:asText",
            f"Expected decoded subject with 'Test Subject with...ncoding', got: {value!r}",
        )


def test_as_addresses_form(
    api_url: str, token: str, account_id: str, email_id: str, results
):
    """
    Test 3: Addresses form - header:{name}:asAddresses

    RFC 8621 Section 4.1.2.3: Parses address-list into EmailAddress[] objects.
    """
    print()
    print("Testing header:{name}:asAddresses form...")

    email = email_get_with_properties(
        api_url, token, account_id, email_id, ["header:From:asAddresses"]
    )

    if not email:
        results.record_fail("header:From:asAddresses", "No email returned")
        return

    if "_error" in email:
        results.record_fail(
            "header:From:asAddresses",
            f"JMAP error: {email['_error'].get('type')}: {email['_error'].get('description')}",
        )
        return

    value = email.get("header:From:asAddresses")
    if not value or not isinstance(value, list):
        results.record_fail(
            "header:From:asAddresses",
            f"Expected array of EmailAddress, got: {value}",
        )
        return

    if len(value) >= 1:
        first_addr = value[0]
        if (
            isinstance(first_addr, dict)
            and first_addr.get("email") == "sender@example.com"
            and first_addr.get("name") == "Test Sender"
        ):
            results.record_pass(
                "header:From:asAddresses",
                f"Value: {value}",
            )
        else:
            results.record_fail(
                "header:From:asAddresses",
                f"Expected name='Test Sender', email='sender@example.com', got: {first_addr}",
            )
    else:
        results.record_fail(
            "header:From:asAddresses",
            f"Expected at least 1 address, got: {value}",
        )


def test_as_addresses_multi(
    api_url: str, token: str, account_id: str, email_id: str, results
):
    """
    Test 4: Addresses form with multiple addresses - header:To:asAddresses

    RFC 8621: To header with multiple recipients parsed into EmailAddress[].
    """
    print()
    print("Testing header:To:asAddresses with multiple addresses...")

    email = email_get_with_properties(
        api_url, token, account_id, email_id, ["header:To:asAddresses"]
    )

    if not email:
        results.record_fail("header:To:asAddresses multi", "No email returned")
        return

    if "_error" in email:
        results.record_fail(
            "header:To:asAddresses multi",
            f"JMAP error: {email['_error'].get('type')}: {email['_error'].get('description')}",
        )
        return

    value = email.get("header:To:asAddresses")
    if not value or not isinstance(value, list):
        results.record_fail(
            "header:To:asAddresses multi",
            f"Expected array of EmailAddress, got: {value}",
        )
        return

    if len(value) == 2:
        emails = [addr.get("email") for addr in value]
        if "alice@example.com" in emails and "bob@example.com" in emails:
            results.record_pass(
                "header:To:asAddresses multi",
                f"Found 2 addresses: {value}",
            )
        else:
            results.record_fail(
                "header:To:asAddresses multi",
                f"Expected alice@ and bob@, got: {emails}",
            )
    else:
        results.record_fail(
            "header:To:asAddresses multi",
            f"Expected 2 addresses, got {len(value)}: {value}",
        )


def test_as_message_ids_form(
    api_url: str, token: str, account_id: str, email_id: str, results
):
    """
    Test 5: MessageIds form - header:References:asMessageIds

    RFC 8621 Section 4.1.2.5: Parses msg-id list into String[].
    """
    print()
    print("Testing header:References:asMessageIds form...")

    email = email_get_with_properties(
        api_url, token, account_id, email_id, ["header:References:asMessageIds"]
    )

    if not email:
        results.record_fail("header:References:asMessageIds", "No email returned")
        return

    if "_error" in email:
        results.record_fail(
            "header:References:asMessageIds",
            f"JMAP error: {email['_error'].get('type')}: {email['_error'].get('description')}",
        )
        return

    value = email.get("header:References:asMessageIds")
    if not value or not isinstance(value, list):
        results.record_fail(
            "header:References:asMessageIds",
            f"Expected array of message IDs, got: {value}",
        )
        return

    # References: <ref1@example.com> <ref2@example.com>
    # Should parse to ["ref1@example.com", "ref2@example.com"] (without angle brackets)
    if len(value) == 2 and "ref1@example.com" in value and "ref2@example.com" in value:
        results.record_pass(
            "header:References:asMessageIds",
            f"Value: {value}",
        )
    else:
        results.record_fail(
            "header:References:asMessageIds",
            f"Expected ['ref1@example.com', 'ref2@example.com'], got: {value}",
        )


def test_as_date_form(
    api_url: str, token: str, account_id: str, email_id: str, results
):
    """
    Test 6: Date form - header:Date:asDate

    RFC 8621 Section 4.1.2.6: Parses date-time into ISO 8601 Date string.
    """
    print()
    print("Testing header:Date:asDate form...")

    email = email_get_with_properties(
        api_url, token, account_id, email_id, ["header:Date:asDate"]
    )

    if not email:
        results.record_fail("header:Date:asDate", "No email returned")
        return

    if "_error" in email:
        results.record_fail(
            "header:Date:asDate",
            f"JMAP error: {email['_error'].get('type')}: {email['_error'].get('description')}",
        )
        return

    value = email.get("header:Date:asDate")
    # Date: Mon, 15 Jan 2024 10:30:00 +0000 -> 2024-01-15T10:30:00Z
    if value is not None and "2024-01-15" in value and "10:30:00" in value:
        results.record_pass(
            "header:Date:asDate",
            f"Value: {value}",
        )
    else:
        results.record_fail(
            "header:Date:asDate",
            f"Expected ISO date containing '2024-01-15' and '10:30:00', got: {value}",
        )


def test_as_urls_form(
    api_url: str, token: str, account_id: str, email_id: str, results
):
    """
    Test 7: URLs form - header:List-Unsubscribe:asURLs

    RFC 8621 Section 4.1.2.7: Parses URL list into String[].
    """
    print()
    print("Testing header:List-Unsubscribe:asURLs form...")

    email = email_get_with_properties(
        api_url, token, account_id, email_id, ["header:List-Unsubscribe:asURLs"]
    )

    if not email:
        results.record_fail("header:List-Unsubscribe:asURLs", "No email returned")
        return

    if "_error" in email:
        results.record_fail(
            "header:List-Unsubscribe:asURLs",
            f"JMAP error: {email['_error'].get('type')}: {email['_error'].get('description')}",
        )
        return

    value = email.get("header:List-Unsubscribe:asURLs")
    # List-Unsubscribe: <mailto:unsub@example.com>, <https://example.com/unsub>
    # Should parse to ["mailto:unsub@example.com", "https://example.com/unsub"]
    if not value or not isinstance(value, list):
        results.record_fail(
            "header:List-Unsubscribe:asURLs",
            f"Expected array of URLs, got: {value}",
        )
        return

    has_mailto = any("mailto:unsub@example.com" in url for url in value)
    has_https = any("https://example.com/unsub" in url for url in value)

    if len(value) == 2 and has_mailto and has_https:
        results.record_pass(
            "header:List-Unsubscribe:asURLs",
            f"Value: {value}",
        )
    else:
        results.record_fail(
            "header:List-Unsubscribe:asURLs",
            f"Expected 2 URLs (mailto and https), got: {value}",
        )


def test_all_modifier(
    api_url: str, token: str, account_id: str, email_id: str, results
):
    """
    Test 8: :all modifier - header:X-Custom-Header:all

    RFC 8621 Section 4.1.3: Returns array of all instances in message order.
    """
    print()
    print("Testing header:{name}:all modifier...")

    email = email_get_with_properties(
        api_url, token, account_id, email_id, ["header:X-Custom-Header:all"]
    )

    if not email:
        results.record_fail("header:X-Custom-Header:all", "No email returned")
        return

    if "_error" in email:
        results.record_fail(
            "header:X-Custom-Header:all",
            f"JMAP error: {email['_error'].get('type')}: {email['_error'].get('description')}",
        )
        return

    value = email.get("header:X-Custom-Header:all")
    if not value or not isinstance(value, list):
        results.record_fail(
            "header:X-Custom-Header:all",
            f"Expected array, got: {value}",
        )
        return

    # X-Custom-Header appears 3 times: "first value", "second value", "third value"
    if len(value) == 3:
        # Check order: first, second, third
        if "first" in value[0] and "second" in value[1] and "third" in value[2]:
            results.record_pass(
                "header:X-Custom-Header:all",
                f"Found 3 instances in order: {value}",
            )
        else:
            results.record_fail(
                "header:X-Custom-Header:all",
                f"Unexpected order: {value}",
            )
    else:
        results.record_fail(
            "header:X-Custom-Header:all",
            f"Expected 3 instances, got {len(value)}: {value}",
        )


def test_combined_form_and_all(
    api_url: str, token: str, account_id: str, email_id: str, results
):
    """
    Test 9: Combined form and :all - header:To:asAddresses:all

    RFC 8621 Section 4.1.3: Returns EmailAddress[][] (array of parsed results).
    """
    print()
    print("Testing header:{name}:asAddresses:all combined form...")

    email = email_get_with_properties(
        api_url, token, account_id, email_id, ["header:To:asAddresses:all"]
    )

    if not email:
        results.record_fail("header:To:asAddresses:all", "No email returned")
        return

    if "_error" in email:
        results.record_fail(
            "header:To:asAddresses:all",
            f"JMAP error: {email['_error'].get('type')}: {email['_error'].get('description')}",
        )
        return

    value = email.get("header:To:asAddresses:all")
    if not value or not isinstance(value, list):
        results.record_fail(
            "header:To:asAddresses:all",
            f"Expected nested array, got: {value}",
        )
        return

    # There's only one To header, so outer array has 1 element
    # Inner array has 2 addresses (Alice and Bob)
    if len(value) >= 1 and isinstance(value[0], list):
        inner = value[0]
        if len(inner) == 2:
            emails = [addr.get("email") for addr in inner if isinstance(addr, dict)]
            if "alice@example.com" in emails and "bob@example.com" in emails:
                results.record_pass(
                    "header:To:asAddresses:all",
                    f"Value: {value}",
                )
                return

    results.record_fail(
        "header:To:asAddresses:all",
        f"Expected [[addr1, addr2]], got: {value}",
    )


def test_case_insensitive_matching(
    api_url: str, token: str, account_id: str, email_id: str, results
):
    """
    Test 10: Case-insensitive header name matching

    RFC 8621 Section 4.1.3: Header field names are matched case insensitively.
    """
    print()
    print("Testing case-insensitive header name matching...")

    # Request same header with different casings
    email = email_get_with_properties(
        api_url,
        token,
        account_id,
        email_id,
        ["header:SUBJECT", "header:subject", "header:Subject"],
    )

    if not email:
        results.record_fail("Case-insensitive header matching", "No email returned")
        return

    if "_error" in email:
        results.record_fail(
            "Case-insensitive header matching",
            f"JMAP error: {email['_error'].get('type')}: {email['_error'].get('description')}",
        )
        return

    # All three should return the same value
    # Note: Response should preserve client's property name casing
    upper = email.get("header:SUBJECT")
    lower = email.get("header:subject")
    mixed = email.get("header:Subject")

    # At least one should be present (server preserves requested casing)
    values = [v for v in [upper, lower, mixed] if v is not None]

    if len(values) >= 1:
        # Check they're all the same (case-insensitive match)
        first_val = values[0]
        all_same = all(v == first_val for v in values)
        if all_same:
            results.record_pass(
                "Case-insensitive header matching",
                f"All casings returned same value: {first_val!r}",
            )
        else:
            results.record_fail(
                "Case-insensitive header matching",
                f"Different values for different casings: SUBJECT={upper!r}, subject={lower!r}, Subject={mixed!r}",
            )
    else:
        results.record_fail(
            "Case-insensitive header matching",
            f"No values returned for any casing: {email}",
        )


def test_missing_header(
    api_url: str, token: str, account_id: str, email_id: str, results
):
    """
    Test 11: Missing header returns null or empty array

    RFC 8621 Section 4.1.3:
    - Single form: null if no header field exists
    - :all form: empty array if no header field exists
    """
    print()
    print("Testing missing header returns null/[]...")

    email = email_get_with_properties(
        api_url,
        token,
        account_id,
        email_id,
        ["header:X-Nonexistent", "header:X-Nonexistent:all"],
    )

    if not email:
        results.record_fail("Missing header", "No email returned")
        return

    if "_error" in email:
        results.record_fail(
            "Missing header",
            f"JMAP error: {email['_error'].get('type')}: {email['_error'].get('description')}",
        )
        return

    # Single form should return null
    single = email.get("header:X-Nonexistent")
    all_form = email.get("header:X-Nonexistent:all")

    passed = True

    if single is None:
        results.record_pass(
            "Missing header (single form)",
            "Returned null as expected",
        )
    else:
        results.record_fail(
            "Missing header (single form)",
            f"Expected null, got: {single!r}",
        )
        passed = False

    if all_form is not None and isinstance(all_form, list) and len(all_form) == 0:
        results.record_pass(
            "Missing header (:all form)",
            "Returned empty array as expected",
        )
    else:
        results.record_fail(
            "Missing header (:all form)",
            f"Expected [], got: {all_form!r}",
        )
        passed = False


def test_invalid_form_rejection(
    api_url: str, token: str, account_id: str, email_id: str, results
):
    """
    Test 12: Invalid form combination returns invalidArguments error

    RFC 8621 Section 4.1.3 (line 2421-2422):
    Attempting to fetch a form that is forbidden (e.g., "header:From:asDate")
    MUST result in the method call being rejected with an "invalidArguments" error.
    """
    print()
    print("Testing invalid form rejection (header:From:asDate)...")

    # From header only supports Addresses form, not Date form
    email = email_get_with_properties(
        api_url, token, account_id, email_id, ["header:From:asDate"]
    )

    if not email:
        results.record_fail("Invalid form rejection", "No response returned")
        return

    if "_error" in email:
        error_type = email["_error"].get("type")
        if error_type == "invalidArguments":
            results.record_pass(
                "Invalid form rejection",
                "Correctly returned invalidArguments error",
            )
        else:
            results.record_fail(
                "Invalid form rejection",
                f"Expected 'invalidArguments', got: {error_type}",
            )
    else:
        # No error - this is wrong per RFC
        results.record_fail(
            "Invalid form rejection",
            f"Expected invalidArguments error, got successful response: {email}",
        )


def test_header_properties(client: Client, config, results):
    """
    Main entry point for header property tests.

    Sets up test data and runs all 12 header property test cases.
    """
    print()
    print("=" * 50)
    print("Testing Email/get header:{name} properties (RFC 8621 §4.1.3)")
    print("=" * 50)

    session = client.jmap_session

    try:
        account_id = client.account_id
    except Exception as e:
        results.record_fail("Header property tests", f"No account ID: {e}")
        return

    # Create test mailbox
    mailbox_id = create_test_mailbox(
        session.api_url, config.token, account_id, results
    )
    if not mailbox_id:
        return

    # Import test email with crafted headers
    email_id = import_header_test_email(
        session.api_url, session.upload_url, config.token, account_id, mailbox_id, results
    )
    if not email_id:
        return

    # Run all test cases
    test_raw_header_form(session.api_url, config.token, account_id, email_id, results)
    test_as_text_form(session.api_url, config.token, account_id, email_id, results)
    test_as_addresses_form(session.api_url, config.token, account_id, email_id, results)
    test_as_addresses_multi(session.api_url, config.token, account_id, email_id, results)
    test_as_message_ids_form(session.api_url, config.token, account_id, email_id, results)
    test_as_date_form(session.api_url, config.token, account_id, email_id, results)
    test_as_urls_form(session.api_url, config.token, account_id, email_id, results)
    test_all_modifier(session.api_url, config.token, account_id, email_id, results)
    test_combined_form_and_all(session.api_url, config.token, account_id, email_id, results)
    test_case_insensitive_matching(session.api_url, config.token, account_id, email_id, results)
    test_missing_header(session.api_url, config.token, account_id, email_id, results)
    test_invalid_form_rejection(session.api_url, config.token, account_id, email_id, results)

    # Cleanup: destroy the test email
    if email_id:
        from test_email_set import destroy_emails_and_verify_s3_cleanup

        destroy_emails_and_verify_s3_cleanup(
            session.api_url, config.token, account_id, [email_id], config, results,
            test_name_prefix="[header tests cleanup]",
        )
