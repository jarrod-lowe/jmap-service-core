"""
Mailbox/set E2E tests.

Tests for Mailbox/set semantics per RFC 8621 Section 2.5.
"""

import uuid

import pytest
import requests

from helpers import (
    make_jmap_request,
    create_test_mailbox,
    import_test_email,
    destroy_emails_and_verify_cleanup,
    destroy_mailbox,
)


class TestMailboxSetDestroy:
    """Tests for Mailbox/set destroy (RFC 8621 Section 2.5)."""

    def test_destroy_empty_mailbox(self, api_url, upload_url, token, account_id):
        """
        Destroy an empty mailbox with onDestroyRemoveEmails=False succeeds.

        RFC 8621 Section 2.5: An empty mailbox can always be destroyed.
        """
        mailbox_id = create_test_mailbox(api_url, token, account_id, prefix="DestroyEmpty")
        assert mailbox_id, "Failed to create mailbox"

        result = destroy_mailbox(api_url, token, account_id, mailbox_id, on_destroy_remove_emails=False)
        assert result.get("methodName") == "Mailbox/set", f"Unexpected method: {result}"
        assert mailbox_id in result.get("destroyed", []), (
            f"Mailbox not in destroyed: {result}"
        )

    def test_destroy_mailbox_with_email_fails_without_flag(
        self, api_url, upload_url, token, account_id
    ):
        """
        Destroy a mailbox containing email with onDestroyRemoveEmails=False fails.

        RFC 8621 Section 2.5: MUST return mailboxHasEmail error if mailbox
        contains emails and onDestroyRemoveEmails is false.
        """
        mailbox_id = None
        email_ids = []
        try:
            mailbox_id = create_test_mailbox(api_url, token, account_id, prefix="DestroyHasEmail")
            assert mailbox_id, "Failed to create mailbox"

            email_id = import_test_email(api_url, upload_url, token, account_id, mailbox_id)
            assert email_id, "Failed to import email"
            email_ids.append(email_id)

            result = destroy_mailbox(api_url, token, account_id, mailbox_id, on_destroy_remove_emails=False)
            assert result.get("methodName") == "Mailbox/set", f"Unexpected method: {result}"

            not_destroyed = result.get("notDestroyed", {})
            assert mailbox_id in not_destroyed, (
                f"Expected mailbox in notDestroyed, got: {result}"
            )
            error = not_destroyed[mailbox_id]
            assert error.get("type") == "mailboxHasEmail", (
                f"Expected mailboxHasEmail error, got: {error}"
            )
        finally:
            if email_ids:
                destroy_emails_and_verify_cleanup(api_url, token, account_id, email_ids)
            if mailbox_id:
                # Force cleanup
                destroy_mailbox(api_url, token, account_id, mailbox_id, on_destroy_remove_emails=True)

    def test_destroy_mailbox_with_email_succeeds_with_flag(
        self, api_url, upload_url, token, account_id
    ):
        """
        Destroy a mailbox containing email with onDestroyRemoveEmails=True succeeds.

        RFC 8621 Section 2.5: If onDestroyRemoveEmails is true, emails only in
        this mailbox are destroyed, and the mailbox is removed.
        """
        mailbox_id = create_test_mailbox(api_url, token, account_id, prefix="DestroyWithFlag")
        assert mailbox_id, "Failed to create mailbox"

        email_id = import_test_email(api_url, upload_url, token, account_id, mailbox_id)
        assert email_id, "Failed to import email"

        result = destroy_mailbox(api_url, token, account_id, mailbox_id, on_destroy_remove_emails=True)
        assert result.get("methodName") == "Mailbox/set", f"Unexpected method: {result}"
        assert mailbox_id in result.get("destroyed", []), (
            f"Mailbox not in destroyed: {result}"
        )

        # Verify email is also gone
        email_get_call = [
            "Email/get",
            {"accountId": account_id, "ids": [email_id]},
            "verifyGone",
        ]
        response = make_jmap_request(api_url, token, [email_get_call])
        assert "methodResponses" in response
        resp_name, resp_data, _ = response["methodResponses"][0]
        assert resp_name == "Email/get"
        not_found = resp_data.get("notFound", [])
        assert email_id in not_found, (
            f"Expected email in notFound after mailbox destroy, got: {resp_data}"
        )

    def test_destroy_mailbox_with_email_in_multiple_mailboxes(
        self, api_url, upload_url, token, account_id
    ):
        """
        Destroy mailbox A with onDestroyRemoveEmails=True when email is also in mailbox B.

        RFC 8621 Section 2.5: Email in multiple mailboxes is removed from the
        destroyed mailbox but not destroyed.
        """
        mailbox_a = None
        mailbox_b = None
        email_ids = []
        try:
            mailbox_a = create_test_mailbox(api_url, token, account_id, prefix="DestroyMultiA")
            assert mailbox_a, "Failed to create mailbox A"

            mailbox_b = create_test_mailbox(api_url, token, account_id, prefix="DestroyMultiB")
            assert mailbox_b, "Failed to create mailbox B"

            # Import email into mailbox A
            email_id = import_test_email(api_url, upload_url, token, account_id, mailbox_a)
            assert email_id, "Failed to import email"
            email_ids.append(email_id)

            # Add email to mailbox B as well
            update_call = [
                "Email/set",
                {
                    "accountId": account_id,
                    "update": {
                        email_id: {f"mailboxIds/{mailbox_b}": True},
                    },
                },
                "addToB",
            ]
            update_response = make_jmap_request(api_url, token, [update_call])
            assert "methodResponses" in update_response
            resp_name, resp_data, _ = update_response["methodResponses"][0]
            assert resp_name == "Email/set", f"Unexpected: {resp_name}"
            assert email_id in resp_data.get("updated", {}), f"Failed to add to B: {resp_data}"

            # Destroy mailbox A
            result = destroy_mailbox(api_url, token, account_id, mailbox_a, on_destroy_remove_emails=True)
            assert result.get("methodName") == "Mailbox/set", f"Unexpected method: {result}"
            assert mailbox_a in result.get("destroyed", []), (
                f"Mailbox A not destroyed: {result}"
            )
            mailbox_a = None  # Already destroyed

            # Verify email still exists and is in mailbox B
            email_get_call = [
                "Email/get",
                {
                    "accountId": account_id,
                    "ids": [email_id],
                    "properties": ["id", "mailboxIds"],
                },
                "verifyStillExists",
            ]
            response = make_jmap_request(api_url, token, [email_get_call])
            assert "methodResponses" in response
            resp_name, resp_data, _ = response["methodResponses"][0]
            assert resp_name == "Email/get"

            emails = resp_data.get("list", [])
            assert len(emails) == 1, f"Expected email to still exist, got: {resp_data}"
            mailbox_ids = emails[0].get("mailboxIds", {})
            assert mailbox_b in mailbox_ids, (
                f"Expected email in mailbox B, got: {mailbox_ids}"
            )
        finally:
            if email_ids:
                destroy_emails_and_verify_cleanup(api_url, token, account_id, email_ids)
            for mb_id in [mailbox_a, mailbox_b]:
                if mb_id:
                    destroy_mailbox(api_url, token, account_id, mb_id, on_destroy_remove_emails=True)

    def test_max_mailbox_depth_is_one(self, jmap_host, token):
        """
        Server advertises maxMailboxDepth=1 (no child mailboxes).

        Verifies the urn:ietf:params:jmap:mail capability includes maxMailboxDepth: 1.
        """
        session_url = f"https://{jmap_host}/.well-known/jmap"
        resp = requests.get(
            session_url,
            headers={"Authorization": f"Bearer {token}"},
            timeout=30,
        )
        assert resp.status_code == 200, f"Session request failed: {resp.status_code}"
        session = resp.json()

        mail_cap = session.get("capabilities", {}).get("urn:ietf:params:jmap:mail", {})
        assert mail_cap.get("maxMailboxDepth") == 1, (
            f"Expected maxMailboxDepth=1, got: {mail_cap}"
        )

    def test_create_child_mailbox_rejected(self, api_url, token, account_id):
        """
        Creating a mailbox with a non-null parentId is rejected when maxMailboxDepth=1.

        Since the server advertises maxMailboxDepth=1, no child mailboxes are allowed.
        """
        parent_id = None
        try:
            parent_id = create_test_mailbox(api_url, token, account_id, prefix="DepthParent")
            assert parent_id, "Failed to create parent mailbox"

            child_name = f"DepthChild-{str(uuid.uuid4())[:8]}"
            child_create_call = [
                "Mailbox/set",
                {
                    "accountId": account_id,
                    "create": {
                        "child": {
                            "name": child_name,
                            "parentId": parent_id,
                        }
                    },
                },
                "createChild",
            ]
            response = make_jmap_request(api_url, token, [child_create_call])
            assert "methodResponses" in response
            resp_name, resp_data, _ = response["methodResponses"][0]
            assert resp_name == "Mailbox/set", f"Unexpected: {resp_name}"

            not_created = resp_data.get("notCreated", {})
            assert "child" in not_created, (
                f"Expected child in notCreated, got: {resp_data}"
            )
        finally:
            if parent_id:
                destroy_mailbox(api_url, token, account_id, parent_id, on_destroy_remove_emails=True)
