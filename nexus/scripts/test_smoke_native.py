"""Offline regression coverage: python -m unittest discover -s nexus/scripts -p test_smoke_native.py.

Uses in-memory stubs only; never opens a socket or consumes real credentials.
"""
import contextlib
import io
import unittest
from unittest import mock

import smoke_native as smoke


class GuardTests(unittest.TestCase):
    def test_exact_canary_and_production_guard(self):
        for host in ("127.0.0.1", "152.136.236.238"):
            smoke.validate_base("http://" + host + ":18121", False)
            smoke.validate_base("http://" + host + ":18120", True)
            with self.assertRaises(smoke.SmokeFailure):
                smoke.validate_base("http://" + host + ":18120", False)
        for base in ("http://81.70.24.5:18121", "http://localhost:18121",
                     "http://127.0.0.1:18121/", "http://127.0.0.1:18121@81.70.24.5",
                     "http://example.com:18121", "https://127.0.0.1:18121"):
            with self.assertRaises(smoke.SmokeFailure):
                smoke.validate_base(base, True)

    def test_blocked_commands_never_reach_opener(self):
        for readonly, paths in ((True, ("/api/machine-keys", "/api/site-profiles/import")),
                                (False, ("/api/v1/scans", "/api/v1/tasks", "/api/automation/runs",
                                         "/api/automation/tasks/id/reconcile", "/api/automation/preview"))):
            client = smoke.Client("http://127.0.0.1:18121", readonly)
            client.opener = mock.Mock()
            for path in paths:
                with self.assertRaises(smoke.SmokeFailure):
                    client.request("POST", path, {})
            client.opener.open.assert_not_called()

    def test_no_redirect_following(self):
        handler = smoke.NoRedirect()
        self.assertIsNone(handler.redirect_request(None, None, 302, "", {}, "http://81.70.24.5"))

    def test_credentials_bounded_and_errors_redacted(self):
        self.assertEqual(smoke.read_credentials(io.StringIO('{"username":"u","password":"p"}')),
                         {"username": "u", "password": "p"})
        for raw in ('sensitive-value', '{"username":"u","password":null}',
                    '{"username":"u","password":"p","extra":"sensitive-value"}', "x" * 16385):
            with self.assertRaises(smoke.SmokeFailure) as caught:
                smoke.read_credentials(io.StringIO(raw))
            self.assertNotIn("sensitive-value", str(caught.exception))

    def test_metadata_recurses(self):
        smoke.no_secrets({"keys": [{"id": "public", "scopes": ["read"]}]}, "redacted")
        for field in ("token", "password", "secret", "passkey", "cookie", "hash", "rss"):
            with self.assertRaises(smoke.SmokeFailure):
                smoke.no_secrets({"keys": [{field: "sensitive-value"}]}, "redacted")

    def test_exception_output_contains_no_payload(self):
        output, error = io.StringIO(), io.StringIO()
        with mock.patch.object(smoke, "Client", side_effect=RuntimeError("sensitive-value")), \
             contextlib.redirect_stdout(output), contextlib.redirect_stderr(error):
            self.assertEqual(smoke.main(["--base", "http://127.0.0.1:18121"]), 1)
        self.assertNotIn("sensitive-value", output.getvalue() + error.getvalue())

    def test_cleanup_continues_after_restore_failure(self):
        client = mock.Mock()
        instance = smoke.Smoke(client)
        instance.jwt = "not-a-real-token"
        instance.rules_dirty = True
        instance.original_rules = {"enabled": False}
        instance.profile_import_attempted = True
        instance.key_create_attempted = True
        key = {"id": "a" * 24, "name": instance.prefix + "-read", "revoked": False}
        client.request.side_effect = [smoke.SmokeFailure("restore failed"), {"sites": []},
                                      {"keys": [key]}, {"success": True},
                                      {"keys": [dict(key, revoked=True)]}]
        self.assertEqual(instance.cleanup(), ["rules restoration failed"])
        self.assertTrue(any(call.args[:2] == ("DELETE", "/api/machine-keys/" + "a" * 24)
                            for call in client.request.call_args_list))


if __name__ == "__main__":
    unittest.main()
