#!/usr/bin/env python3
"""Bounded native API smoke test (Python standard library only).

Credentials arrive only as one stdin JSON object: {"username": ..., "password": ...}.
Never put them in arguments, shell history, logs, or a committed fixture. Example:
  <secret-provider> | python3 nexus/scripts/smoke_native.py --base http://127.0.0.1:18121
Production requires --read-only and port 18120. No redirects, proxies, retries,
scans, runs, downloads, or uploads. Canary must use disposable data with no NAS
or tracker connections. A synthetic example.com profile and revoked key metadata
remain in that disposable database (there is no profile-delete API).
"""

import argparse
import copy
import datetime
import json
import re
import sys
import urllib.error
import urllib.request
import uuid


class SmokeFailure(Exception):
    """Only fixed, credential-free diagnostics may be attached."""


def check(condition, label):
    if not condition:
        raise SmokeFailure(label)


def validate_base(base, read_only):
    allowed = {"http://127.0.0.1:18121", "http://152.136.236.238:18121"}
    if read_only:
        allowed |= {"http://127.0.0.1:18120", "http://152.136.236.238:18120"}
    check(base in allowed, "base must be an exact allowed IP/port; production requires --read-only")
    return base


def read_credentials(stream):
    # Bounded read; never include parser exceptions or input in diagnostics.
    try:
        raw = stream.read(16385)
        value = json.loads(raw)
    except Exception:
        raise SmokeFailure("stdin must contain a bounded credentials JSON object") from None
    check(len(raw) <= 16384 and isinstance(value, dict), "invalid credentials object")
    check(set(value) == {"username", "password"}, "credentials require username/password only")
    check(all(isinstance(value[k], str) and value[k] for k in value), "credentials must be nonempty strings")
    return value


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


class Client:
    def __init__(self, base, read_only=False):
        self.base = validate_base(base, read_only)
        self.read_only = read_only
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())

    def request(self, method, path, body=None, token=None, headers=None, expected=200):
        check(path.startswith("/") and ("?" not in path or path == "/api/automation/tasks?active=1") and "#" not in path and ".." not in path,
              "unsafe request path")
        if self.read_only:
            check(method == "GET" or (method == "POST" and path == "/api/auth/login"),
                  "read-only mode blocked a mutation")
        # Defense against accidental extension into network/transfer workflows.
        blocked = ("/scans", "/runs", "/tasks", "/preview")
        if method != "GET" and any(part in path for part in blocked):
            check(path in {"/api/site-profiles/preview", "/api/v1/sites/preview"},
                  "workflow/network command is forbidden")
        data = None if body is None else json.dumps(body, separators=(",", ":")).encode()
        request_headers = {"Accept": "application/json", "Content-Type": "application/json"}
        if token:
            request_headers["Authorization"] = "Bearer " + token
        request_headers.update(headers or {})
        req = urllib.request.Request(self.base + path, data=data, method=method, headers=request_headers)
        try:
            response = self.opener.open(req, timeout=15)
        except urllib.error.HTTPError as exc:
            response = exc
        except Exception:
            raise SmokeFailure("HTTP transport failed; no request was retried") from None
        with response:
            status = response.code
            raw = response.read(2 * 1024 * 1024 + 1)
        check(status == expected, "unexpected HTTP status for " + method + " " + path +
              " (expected " + str(expected) + ", got " + str(status) + ")")
        check(len(raw) <= 2 * 1024 * 1024, "response exceeded limit")
        try:
            return json.loads(raw)
        except Exception:
            raise SmokeFailure("response was not JSON") from None


def no_secrets(value, label):
    """Public metadata must exclude secret-bearing fields at every depth."""
    if isinstance(value, dict):
        for key, nested in value.items():
            check(not any(word in key.lower() for word in
                          ("token", "secret", "password", "passkey", "cookie", "hash")), label)
            check(key.lower() not in {"rss", "credentials", "api_key"}, label)
            no_secrets(nested, label)
    elif isinstance(value, list):
        for nested in value:
            no_secrets(nested, label)


class Smoke:
    def __init__(self, client):
        self.client = client
        self.jwt = None
        self.prefix = "native-smoke-" + uuid.uuid4().hex[:16]
        self.profile_id = "smoke_" + uuid.uuid4().hex[:16]
        self.keys = {}
        self.original_rules = None
        self.rules_dirty = False
        self.profile_import_attempted = False
        self.key_create_attempted = False
        self.checks = 0

    def admin(self, method, path, body=None, expected=200, token=None):
        return self.client.request(method, path, body, self.jwt if token is None else token, expected=expected)

    def machine(self, scope, method, path, body=None, expected=200, idem=None):
        headers = {}
        if method != "GET":
            headers["Idempotency-Key"] = idem or uuid.uuid4().hex
        return self.client.request(method, "/api/v1" + path, body, self.keys[scope]["token"], headers, expected)

    def passed(self, label):
        self.checks += 1
        print("PASS " + label, flush=True)

    def baseline(self, credentials, expected_version):
        health = self.client.request("GET", "/health")
        version = health.get("version")
        check(health.get("service") == "ptmove-nexus" and isinstance(version, str) and bool(version) and
              (expected_version is None or version == expected_version), "wrong native service/version")
        for path in ("/api/automation/settings", "/api/site-profiles", "/api/machine-keys", "/api/machine-openapi"):
            self.client.request("GET", path, expected=401)
        self.passed("native health and administrator isolation")
        login = self.client.request("POST", "/api/auth/login", credentials)
        check(login.get("success") is True and isinstance(login.get("token"), str) and login["token"], "login failed")
        self.jwt = login["token"]
        self.passed("administrator login")
        for headers in ({}, {"Cookie": "session=" + self.jwt},
                        {"X-API-Key": "synthetic-nas-key", "X-Internal-API-Key": "synthetic-internal-key"}):
            self.client.request("GET", "/api/v1/dashboard", headers=headers, expected=401)
        self.client.request("GET", "/api/v1/dashboard", token=self.jwt, expected=401)
        self.passed("machine API rejects browser JWT, Cookie and NAS headers")
        state = self.admin("GET", "/api/automation/settings")
        settings = state.get("settings", {})
        check(state.get("success") is True, "settings unavailable")
        for key in ("enabled", "auto_publish", "schedule_enabled"):
            check(settings.get(key) is False, "automation must start fully disabled")
        if not self.client.read_only:
            for key, expected in {"monthly_limit": 6, "min_size_gib": 1, "max_size_gib": 10,
                                  "batch_size": 20, "source_site": "zmpt", "target_site": "novahd"}.items():
                check(settings.get(key) == expected, "unexpected initial native rule defaults")
        check("monthly_budget_gib" not in settings, "obsolete monthly size budget still exposed")
        active_tasks = self.admin("GET", "/api/automation/tasks?active=1")
        check(active_tasks.get("success") is True and isinstance(active_tasks.get("tasks"), list), "active task endpoint unavailable")
        self.original_rules = copy.deepcopy(settings)
        self.initial_state = state
        self.initial_tasks = self.admin("GET", "/api/automation/tasks")
        self.initial_runs = self.admin("GET", "/api/automation/runs")
        if not self.client.read_only:
            check(self.initial_tasks.get("success") is True and
                  self.initial_tasks.get("tasks") in ([], None) and
                  self.initial_runs.get("success") is True and self.initial_runs.get("runs") == [],
                  "canary must have no existing automation tasks or runs")
        sites = self.admin("GET", "/api/site-profiles").get("sites", [])
        no_secrets(sites, "site list exposed a secret field")
        for site_id in ("zmpt", "novahd"):
            check(any(p.get("id") == site_id and p.get("builtin") is True for p in sites), "missing built-in source/target")
        self.passed("disabled defaults and built-in source/target")
        schema = self.admin("GET", "/api/site-profiles/schema")
        check(schema.get("additionalProperties") is False and "base_url" in schema.get("properties", {}), "invalid profile JSON Schema")
        self.template = self.admin("GET", "/api/site-profiles/template")
        no_secrets(self.template, "template exposed secrets")
        doc = self.admin("GET", "/api/machine-openapi")
        check(doc.get("openapi") == "3.1.0" and "/api/v1/sites/import" in doc.get("paths", {}), "invalid OpenAPI download")
        check(all(p.startswith("/api/v1/") for p in doc["paths"]), "OpenAPI exposed administrator routes")
        self.passed("schema, template and administrator OpenAPI download")

    def profiles(self):
        profile = copy.deepcopy(self.template)
        profile.update(id=self.profile_id, name="Disposable Native Smoke", base_url="https://example.com", ptnexus_site="zmpt")
        before = self.admin("GET", "/api/site-profiles")
        self.admin("POST", "/api/site-profiles/preview", profile)
        check(self.admin("GET", "/api/site-profiles") == before, "profile preview persisted state")
        bad = dict(profile, password="synthetic-forbidden-value")
        self.admin("POST", "/api/site-profiles/preview", bad, expected=400)
        self.profile_import_attempted = True
        created = self.admin("POST", "/api/site-profiles/import", {"profile": profile})["profile"]
        check(created.get("revision") == 1, "initial profile revision is wrong")
        self.admin("POST", "/api/site-profiles/import", {"profile": profile}, expected=409)
        self.admin("POST", "/api/site-profiles/import", {"profile": profile, "update": True, "expected_revision": 999}, expected=409)
        updated = self.admin("POST", "/api/site-profiles/import", {"profile": profile, "update": True, "expected_revision": 1})["profile"]
        check(updated.get("revision") == 2, "profile update revision is wrong")
        path = "/api/site-profiles/" + self.profile_id
        exported = self.admin("GET", path + "/export")
        no_secrets(exported, "export exposed secrets")
        check("revision" not in exported and "digest" not in exported and exported == profile, "export is not reimportable declaration")
        self.admin("PUT", path + "/credentials", {"cookie": "synthetic-cookie", "rss": "https://example.com/rss.php?passkey=synthetic-rss"})
        configured = self.admin("GET", path + "/credentials")
        check(configured.get("has_cookie") is True and configured.get("has_rss") is True, "credentials not configured")
        self.admin("PUT", path + "/credentials", {})
        check(self.admin("GET", path + "/credentials") == configured, "omitted credentials were not preserved")
        self.admin("PUT", path + "/credentials", {"rss": ""})
        partial = self.admin("GET", path + "/credentials")
        check(partial.get("has_cookie") is True and partial.get("has_rss") is False, "independent credential clearing failed")
        self.admin("PUT", path + "/credentials", {"cookie": "", "rss": ""})
        cleared = self.admin("GET", path + "/credentials")
        check(cleared.get("has_cookie") is False and cleared.get("has_rss") is False, "credential clearing failed")
        check(self.admin("GET", path + "/export") == exported, "credentials changed public export")
        self.passed("offline preview, import, revision conflict, export and credential preserve/clear")

    def machine_checks(self):
        expires = (datetime.datetime.now(datetime.timezone.utc) + datetime.timedelta(hours=1)).isoformat().replace("+00:00", "Z")
        for name, scopes in (("read", ["read"]), ("write", ["read", "write"]), ("publish", ["read", "publish"])):
            self.key_create_attempted = True
            out = self.admin("POST", "/api/machine-keys", {"name": self.prefix + "-" + name, "scopes": scopes, "expires_at": expires}, expected=201)
            self.keys[name] = out
            check(isinstance(out.get("token"), str) and re.fullmatch(r"ptn_[0-9a-f]{64}", out["token"]), "key creation did not return one-time token")
            check(set(out.get("key", {})) == {"id", "name", "scopes", "expires_at", "created_at", "revoked", "status"}, "unexpected key metadata")
            check(out["key"]["status"] == "active", "new key is not active")
        listed = self.admin("GET", "/api/machine-keys")
        no_secrets(listed, "key list exposed secret material")
        for name in self.keys:
            self.machine(name, "GET", "/dashboard")
            identity = self.machine(name, "GET", "/connection")
            check(identity.get("key", {}).get("id") == self.keys[name]["key"]["id"], "connection identity mismatch")
            no_secrets(identity, "connection exposed secret material")
        self.client.request("GET", "/api/machine-keys", token=self.keys["read"]["token"], expected=401)
        self.passed("one-time machine keys, public metadata and reverse authentication isolation")
        # Scope regressions must still restore an unexpectedly accepted patch.
        self.rules_dirty = True
        self.machine("read", "PUT", "/rules", {}, expected=403)
        self.machine("publish", "PUT", "/rules", {}, expected=403)
        self.machine("write", "PUT", "/rules", {"auto_publish": True}, expected=403)
        self.client.request("PUT", "/api/v1/rules", {}, self.keys["write"]["token"], expected=400)
        self.passed("read/write/publish independent scopes and mandatory idempotency")
        idem = uuid.uuid4().hex
        patch = {"enabled": False, "auto_publish": False, "schedule_enabled": False,
                 "batch_size": 21}
        self.rules_dirty = True  # Mark before request: a lost response can still have committed.
        first = self.machine("write", "PUT", "/rules", patch, idem=idem)
        replay = self.machine("write", "PUT", "/rules", patch, idem=idem)
        check(first == replay and first.get("settings", {}).get("batch_size") == 21, "idempotent response differs")
        self.machine("write", "PUT", "/rules", dict(patch, batch_size=22), expected=409, idem=idem)
        check(self.admin("GET", "/api/automation/settings")["settings"]["batch_size"] == 21, "conflicting replay changed rules")
        revoked = self.keys["read"]
        self.admin("DELETE", "/api/machine-keys/" + revoked["key"]["id"])
        self.machine("read", "GET", "/dashboard", expected=401)
        # Revocation also blocks a previously successful command replay.
        writer = self.keys["write"]
        self.admin("DELETE", "/api/machine-keys/" + writer["key"]["id"])
        self.machine("write", "PUT", "/rules", patch, expected=401, idem=idem)
        self.passed("durable replay/conflict and revoked-key replay rejection")

    def cleanup(self):
        failures = []
        # Cleanup uses only administrator routes. Never retry an ambiguous command.
        if self.rules_dirty:
            try:
                self.admin("PUT", "/api/automation/settings", self.original_rules)
                check(self.admin("GET", "/api/automation/settings")["settings"] == self.original_rules, "restore mismatch")
            except Exception:
                failures.append("rules restoration failed")
        if self.profile_import_attempted:
            try:
                profiles = self.admin("GET", "/api/site-profiles")["sites"]
                if any(p.get("id") == self.profile_id for p in profiles):
                    path = "/api/site-profiles/" + self.profile_id + "/credentials"
                    self.admin("PUT", path, {"cookie": "", "rss": ""})
                    state = self.admin("GET", path)
                    check(state.get("has_cookie") is False and state.get("has_rss") is False, "clear mismatch")
            except Exception:
                failures.append("synthetic credentials cleanup failed")
        if self.key_create_attempted:
            try:
                # Covers an ambiguous key-create response without exposing the token.
                rows = self.admin("GET", "/api/machine-keys")["keys"]
                for row in rows:
                    if row.get("name", "").startswith(self.prefix + "-") and not row.get("revoked"):
                        key_id = row.get("id", "")
                        check(isinstance(key_id, str) and re.fullmatch(r"[0-9a-f]{24}", key_id), "invalid cleanup key id")
                        self.admin("DELETE", "/api/machine-keys/" + key_id)
                rows = self.admin("GET", "/api/machine-keys")["keys"]
                check(all(row.get("revoked") for row in rows if row.get("name", "").startswith(self.prefix + "-")), "revoke mismatch")
            except Exception:
                failures.append("test key revocation failed")
        return failures

    def unchanged_work(self):
        check(self.admin("GET", "/api/automation/tasks") == self.initial_tasks, "task state changed")
        check(self.admin("GET", "/api/automation/runs") == self.initial_runs, "run history changed")
        final = self.admin("GET", "/api/automation/settings")
        check(final == self.initial_state, "rules or quota changed after cleanup")
        self.passed("rules restored; quota, tasks and run history unchanged")


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base", required=True, help="exact http://127.0.0.1:18121 or http://152.136.236.238:18121")
    parser.add_argument("--read-only", action="store_true", help="only health/login/auth and GET checks; permits port 18120")
    parser.add_argument("--expected-version", help="require this exact version from the health endpoint")
    args = parser.parse_args(argv)
    smoke = None
    failure = None
    cleanup_failures = []
    try:
        client = Client(args.base, args.read_only)
        credentials = read_credentials(sys.stdin)
        smoke = Smoke(client)
        smoke.baseline(credentials, args.expected_version)
        del credentials
        if not args.read_only:
            smoke.profiles()
            smoke.machine_checks()
    except SmokeFailure as exc:
        failure = str(exc)
    except BaseException:
        # No tracebacks: library/parser exceptions can contain secrets or bodies.
        failure = "unexpected or interrupted smoke failure (details suppressed)"
    finally:
        if smoke is not None and not args.read_only:
            cleanup_failures = smoke.cleanup()
    if smoke is not None and smoke.original_rules is not None and not failure and not cleanup_failures:
        try:
            smoke.unchanged_work()
        except BaseException:
            failure = "final state verification failed"
    if failure:
        print("FAIL " + failure, file=sys.stderr)
    for issue in cleanup_failures:
        print("CLEANUP FAILURE " + issue, file=sys.stderr)
    if failure or cleanup_failures:
        return 1
    print("PASS native smoke complete (" + str(smoke.checks) + " groups; " +
          ("read-only" if args.read_only else "disposable canary; public fixture remains") + ")")
    return 0


if __name__ == "__main__":
    sys.exit(main())
