"""
Result Reference tests (RFC 8620 Section 3.7).

Tests for JMAP result references that allow method calls to reference
results from previous method calls in the same request.
"""

import pytest

from helpers import (
    make_jmap_request,
    create_test_mailbox,
    import_test_email,
    destroy_emails_and_verify_cleanup,
    destroy_mailbox,
)


class TestResultReferences:
    """Tests for JMAP result references (RFC 8620 Section 3.7)."""

    @pytest.fixture(scope="class")
    def test_data(self, api_url, upload_url, token, account_id):
        """Create a mailbox and 3 test emails for result reference tests."""
        mailbox_id = create_test_mailbox(
            api_url, token, account_id, prefix="ResultRef-Test"
        )
        assert mailbox_id, "Failed to create test mailbox"

        email_ids = []
        for i in range(3):
            email_id = import_test_email(
                api_url, upload_url, token, account_id, mailbox_id
            )
            assert email_id, f"Failed to import test email {i}"
            email_ids.append(email_id)

        yield {"mailbox_id": mailbox_id, "email_ids": email_ids}

        # Cleanup
        destroy_emails_and_verify_cleanup(
            api_url, token, account_id, email_ids
        )
        destroy_mailbox(api_url, token, account_id, mailbox_id)

    def test_simple_result_reference(self, api_url, token, account_id, test_data):
        """
        Simple Result Reference (RFC 8620 Section 3.7).

        Email/query returns ids, Email/get references those ids via #ids.
        """
        mailbox_id = test_data["mailbox_id"]

        method_calls = [
            [
                "Email/query",
                {
                    "accountId": account_id,
                    "filter": {"inMailbox": mailbox_id},
                },
                "query0",
            ],
            [
                "Email/get",
                {
                    "accountId": account_id,
                    "#ids": {
                        "resultOf": "query0",
                        "name": "Email/query",
                        "path": "/ids",
                    },
                    "properties": ["id", "subject"],
                },
                "get0",
            ],
        ]

        response = make_jmap_request(api_url, token, method_calls)
        assert "methodResponses" in response, f"No methodResponses: {response}"

        method_responses = response["methodResponses"]
        assert len(method_responses) == 2, (
            f"Expected 2 responses, got {len(method_responses)}"
        )

        # Check Email/query response
        query_name, query_data, _ = method_responses[0]
        assert query_name != "error", (
            f"Query error: {query_data.get('type')}: {query_data.get('description')}"
        )
        assert query_name == "Email/query", f"Unexpected response: {query_name}"
        query_ids = query_data.get("ids", [])

        # Check Email/get response
        get_name, get_data, _ = method_responses[1]
        assert get_name != "error", (
            f"Get error: {get_data.get('type')}: {get_data.get('description')}"
        )
        assert get_name == "Email/get", f"Unexpected response: {get_name}"

        email_list = get_data.get("list", [])
        assert len(email_list) == len(query_ids), (
            f"Expected {len(query_ids)} emails, got {len(email_list)}"
        )

        get_ids = [e.get("id") for e in email_list]
        assert set(get_ids) == set(query_ids), (
            f"Query IDs: {query_ids}, Get IDs: {get_ids}"
        )

    def test_wildcard_result_reference(self, api_url, token, account_id, test_data):
        """
        Wildcard Result Reference Path (RFC 8620 Section 3.7).

        Use /list/*/id to extract all id values from the list array.
        """
        mailbox_id = test_data["mailbox_id"]

        method_calls = [
            [
                "Email/query",
                {
                    "accountId": account_id,
                    "filter": {"inMailbox": mailbox_id},
                    "limit": 2,
                },
                "query0",
            ],
            [
                "Email/get",
                {
                    "accountId": account_id,
                    "#ids": {
                        "resultOf": "query0",
                        "name": "Email/query",
                        "path": "/ids",
                    },
                    "properties": ["id", "threadId"],
                },
                "get0",
            ],
            [
                "Thread/get",
                {
                    "accountId": account_id,
                    "#ids": {
                        "resultOf": "get0",
                        "name": "Email/get",
                        "path": "/list/*/threadId",
                    },
                },
                "thread0",
            ],
        ]

        response = make_jmap_request(api_url, token, method_calls)
        assert "methodResponses" in response, f"No methodResponses: {response}"

        method_responses = response["methodResponses"]
        assert len(method_responses) == 3, (
            f"Expected 3 responses, got {len(method_responses)}"
        )

        # Check Email/get response (second call)
        get_name, get_data, _ = method_responses[1]
        assert get_name != "error", (
            f"Error: {get_data.get('type')}: {get_data.get('description')}"
        )

        email_list = get_data.get("list", [])
        thread_ids = [e.get("threadId") for e in email_list if e.get("threadId")]

        # Check Thread/get response (third call)
        thread_name, thread_data, _ = method_responses[2]
        assert thread_name != "error", (
            f"Error: {thread_data.get('type')}: {thread_data.get('description')}"
        )
        assert thread_name == "Thread/get", f"Unexpected response: {thread_name}"

        thread_list = thread_data.get("list", [])
        # Thread count may differ from thread_ids due to deduplication
        assert len(thread_list) > 0, "Thread/get returned no threads"
        assert len(thread_list) <= len(thread_ids), (
            f"Thread/get returned more threads ({len(thread_list)}) than threadIds ({len(thread_ids)})"
        )

    def test_invalid_result_of(self, api_url, token, account_id, test_data):
        """
        Invalid resultOf Reference (RFC 8620 Section 3.7).

        When resultOf references a non-existent clientId, return invalidResultReference.
        """
        method_calls = [
            [
                "Email/get",
                {
                    "accountId": account_id,
                    "#ids": {
                        "resultOf": "nonexistent",
                        "name": "Email/query",
                        "path": "/ids",
                    },
                },
                "get0",
            ],
        ]

        response = make_jmap_request(api_url, token, method_calls)
        assert "methodResponses" in response, f"No methodResponses: {response}"

        method_responses = response["methodResponses"]
        assert len(method_responses) == 1, (
            f"Expected 1 response, got {len(method_responses)}"
        )

        response_name, response_data, _ = method_responses[0]
        assert response_name == "error", f"Expected error, got {response_name}"

        error_type = response_data.get("type")
        assert error_type == "invalidResultReference", (
            f"Expected invalidResultReference, got {error_type}"
        )

    def test_name_mismatch(self, api_url, token, account_id, test_data):
        """
        Result Reference Name Mismatch (RFC 8620 Section 3.7).

        When the name in result reference doesn't match the actual method response,
        return invalidResultReference.
        """
        mailbox_id = test_data["mailbox_id"]

        method_calls = [
            [
                "Email/query",
                {
                    "accountId": account_id,
                    "filter": {"inMailbox": mailbox_id},
                },
                "query0",
            ],
            [
                "Email/get",
                {
                    "accountId": account_id,
                    "#ids": {
                        "resultOf": "query0",
                        "name": "Email/get",  # Wrong! Should be "Email/query"
                        "path": "/ids",
                    },
                },
                "get0",
            ],
        ]

        response = make_jmap_request(api_url, token, method_calls)
        assert "methodResponses" in response, f"No methodResponses: {response}"

        method_responses = response["methodResponses"]
        assert len(method_responses) == 2, (
            f"Expected 2 responses, got {len(method_responses)}"
        )

        # First response should be successful Email/query
        query_name, _, _ = method_responses[0]
        assert query_name == "Email/query", f"Expected Email/query, got {query_name}"

        # Second response should be error
        get_name, get_data, _ = method_responses[1]
        assert get_name == "error", f"Expected error, got {get_name}"

        error_type = get_data.get("type")
        assert error_type == "invalidResultReference", (
            f"Expected invalidResultReference, got {error_type}"
        )

    def test_conflicting_keys(self, api_url, token, account_id, test_data):
        """
        Conflicting Keys Error (RFC 8620 Section 3.7).

        When both "ids" and "#ids" are present, return invalidArguments.
        """
        mailbox_id = test_data["mailbox_id"]

        method_calls = [
            [
                "Email/query",
                {
                    "accountId": account_id,
                    "filter": {"inMailbox": mailbox_id},
                },
                "query0",
            ],
            [
                "Email/get",
                {
                    "accountId": account_id,
                    "ids": ["existing-id"],  # Explicit ids
                    "#ids": {  # Plus result reference - conflict!
                        "resultOf": "query0",
                        "name": "Email/query",
                        "path": "/ids",
                    },
                },
                "get0",
            ],
        ]

        response = make_jmap_request(api_url, token, method_calls)
        assert "methodResponses" in response, f"No methodResponses: {response}"

        method_responses = response["methodResponses"]
        assert len(method_responses) == 2, (
            f"Expected 2 responses, got {len(method_responses)}"
        )

        # Second response should be error
        get_name, get_data, _ = method_responses[1]
        assert get_name == "error", f"Expected error, got {get_name}"

        error_type = get_data.get("type")
        assert error_type == "invalidArguments", (
            f"Expected invalidArguments, got {error_type}"
        )
