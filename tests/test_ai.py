"""Security, scope and replay coverage for the scoped machine API."""
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timezone

from fastapi.testclient import TestClient
import pytest

from ptmove.api import create_app
from ptmove.models import Rule


ADMIN_PASSWORD = "correct-password"
WORKER_TOKEN = "w" * 40


@pytest.fixture
def client(tmp_path):
    app = create_app(tmp_path, ADMIN_PASSWORD, "s" * 40, WORKER_TOKEN)
    with TestClient(app) as test_client:
        yield test_client


def admin_headers(client):
    response = client.post("/api/auth/login", json={"password": ADMIN_PASSWORD})
    assert response.status_code == 200, response.text
    return {"X-CSRF-Token": response.json()["csrf_token"]}


def create_key(client, headers, *, scopes=None, name="test key", expires_days=30):
    body = {"name": name, "expires_days": expires_days}
    if scopes is not None:
        body["scopes"] = scopes
    response = client.post("/api/api-keys", headers=headers, json=body)
    assert response.status_code == 200, response.text
    result = response.json()
    return result, {"Authorization": "Bearer " + result["token"]}


def make_profile(client, identity="test-tracker", host="test-tracker.example"):
    profile = client.get("/api/sites/template").json()
    profile.update(
        id=identity,
        name="Test tracker",
        base_url="https://" + host,
        ptnexus_site=None,
        roles=["source", "target"],
        official_groups=[],
        announce_hosts=[host],
    )
    return profile


def candidate(source_id):
    return {
        "source_id": str(source_id),
        "title": f"Test item {source_id}",
        "size_bytes": 2 * 1024**3,
        "seeders": 10,
        "leechers": 2,
        "official": True,
        "category": "movie",
        "detail_url": f"https://zmpt.cc/details.php?id={source_id}",
        "download_url": f"https://zmpt.cc/download.php?id={source_id}&passkey=test-passkey",
        "restricted": False,
        "restriction_reason": "",
        "permission_verified": True,
        "target_exists": False,
        "target_work_exists": False,
        "published_at": datetime.now(timezone.utc).isoformat(),
        "metadata": {},
    }


def seed_candidates(client, count=2):
    store = client.app.state.store
    run = store.create_run()
    job = store.claim("scanner")['job']
    store.complete(job["id"], "scanner", job["lease_token"], True,
                   {"items": [candidate(i) for i in range(1, count + 1)]})
    assert run["run_id"]
    return [item["id"] for item in store.candidates()["items"]]


def test_machine_credentials_are_separate_from_admin_and_worker(client):
    admin = admin_headers(client)
    _, machine = create_key(client, admin, scopes=["read", "write", "publish"])

    # Admin cookies and the independent worker credential never act as machine keys.
    assert client.get("/api/v1/dashboard").status_code == 401
    assert client.get("/api/v1/dashboard", headers={"Authorization": "Bearer " + WORKER_TOKEN}).status_code == 401
    assert client.get("/api/worker/config", headers=machine).status_code == 401
    # Remove the already-authenticated admin cookie before testing the admin route.
    client.cookies.clear()
    assert client.get("/api/api-keys", headers=machine).status_code == 401
    assert client.get("/api/v1/dashboard", headers=machine).status_code == 200


def test_admin_key_creation_requires_csrf_and_defaults_to_read_only(client):
    assert client.post("/api/api-keys", json={"name": "blocked"}).status_code == 401
    headers = admin_headers(client)
    assert client.post("/api/api-keys", json={"name": "blocked"}).status_code == 403

    created, bearer = create_key(client, headers)
    assert created["key"]["scopes"] == ["read"]
    assert client.get("/api/v1/dashboard", headers=bearer).status_code == 200
    before = client.app.state.store.dashboard()
    mutation = client.post("/api/v1/scans", headers={**bearer, "Idempotency-Key": "read-only-scan"})
    assert mutation.status_code == 403
    assert client.app.state.store.dashboard()["counts"] == before["counts"]


def test_key_is_only_returned_once_and_only_digest_is_persisted(client, tmp_path):
    headers = admin_headers(client)
    created, _ = create_key(client, headers)
    token = created["token"]
    assert token.startswith("ptm_")
    key_id = created["key"]["id"]

    listed = client.get("/api/api-keys", headers=headers)
    assert listed.status_code == 200
    assert token not in listed.text
    assert "digest" not in listed.text
    with client.app.state.store.connection() as con:
        row = con.execute("SELECT digest FROM api_keys WHERE id=?", (key_id,)).fetchone()
    assert row is not None and row["digest"] != token
    assert len(row["digest"]) == 64
    for path in tmp_path.glob("controller.sqlite3*"):
        assert token.encode() not in path.read_bytes()


def test_expired_and_revoked_keys_cannot_read_or_replay(client):
    admin = admin_headers(client)
    created, bearer = create_key(client, admin, scopes=["read", "write", "publish"])
    initial = client.post("/api/v1/scans", headers={**bearer, "Idempotency-Key": "replay-scan-001"})
    assert initial.status_code == 200

    revoked = client.delete(f"/api/api-keys/{created['key']['id']}", headers=admin)
    assert revoked.status_code == 200
    assert client.get("/api/v1/dashboard", headers=bearer).status_code == 401
    replay = client.post("/api/v1/scans", headers={**bearer, "Idempotency-Key": "replay-scan-001"})
    assert replay.status_code == 401
    assert client.app.state.store.dashboard()["counts"]["pending"] == 0

    expired, expired_bearer = create_key(client, admin, scopes=["read"])
    with client.app.state.store.connection(True) as con:
        con.execute("UPDATE api_keys SET expires=0 WHERE id=?", (expired["key"]["id"],))
    assert client.get("/api/v1/dashboard", headers=expired_bearer).status_code == 401


def test_all_mutations_require_idempotency_and_scan_replay_is_single_effect(client):
    admin = admin_headers(client)
    _, bearer = create_key(client, admin, scopes=["read", "write", "publish"])
    assert client.post("/api/v1/scans", headers=bearer).status_code == 422
    headers = {**bearer, "Idempotency-Key": "concurrent-scan-001"}

    def scan(_):
        return client.post("/api/v1/scans", headers=headers)

    with ThreadPoolExecutor(max_workers=8) as pool:
        responses = list(pool.map(scan, range(8)))
    assert all(response.status_code == 200 for response in responses), [r.text for r in responses]
    assert len({response.json()["run_id"] for response in responses}) == 1
    with client.app.state.store.connection() as con:
        assert con.execute("SELECT COUNT(*) FROM runs").fetchone()[0] == 1
        assert con.execute("SELECT COUNT(*) FROM jobs WHERE kind='scan'").fetchone()[0] == 1


def test_reused_idempotency_key_rejects_changed_enqueue_body_and_new_key_is_new_intent(client):
    admin = admin_headers(client)
    _, bearer = create_key(client, admin, scopes=["read", "write", "publish"])
    ids = seed_candidates(client, count=2)
    first = client.post("/api/v1/tasks", headers={**bearer, "Idempotency-Key": "enqueue-items-01"}, json={"candidate_ids": [ids[0]]})
    assert first.status_code == 200, first.text
    assert first.json()["queued"] == 1
    changed = client.post("/api/v1/tasks", headers={**bearer, "Idempotency-Key": "enqueue-items-01"}, json={"candidate_ids": [ids[1]]})
    assert changed.status_code == 409
    second = client.post("/api/v1/tasks", headers={**bearer, "Idempotency-Key": "enqueue-items-02"}, json={"candidate_ids": [ids[1]]})
    assert second.status_code == 200, second.text
    assert second.json()["queued"] == 1
    assert client.app.state.store.tasks()["total"] == 2


def test_write_scope_cannot_change_enabled_rules_or_protected_settings(client):
    admin = admin_headers(client)
    _, write_key = create_key(client, admin, scopes=["read", "write"])
    store = client.app.state.store
    enabled = Rule(enabled=True).model_dump()
    store.set_rule(enabled)

    changed_rule = {**enabled, "min_seeders": enabled["min_seeders"] + 1}
    response = client.put("/api/v1/rules", headers={**write_key, "Idempotency-Key": "change-rule-001"}, json=changed_rule)
    assert response.status_code == 403
    response = client.put("/api/v1/settings", headers={**write_key, "Idempotency-Key": "change-settings-1"}, json={"qb_username": "changed"})
    assert response.status_code == 403
    assert store.get_rule() == enabled
    assert store.get_settings()["qb_username"] == ""


def test_write_scope_cannot_update_site_profile_needed_by_enabled_rule(client):
    admin = admin_headers(client)
    _, write_key = create_key(client, admin, scopes=["read", "write"])
    profile = make_profile(client, identity="enabled-site", host="enabled-site.example")
    imported = client.post("/api/v1/sites/import", headers={**write_key, "Idempotency-Key": "site-import-0001"}, json={"profile": profile})
    assert imported.status_code == 200, imported.text

    client.app.state.store.set_rule(Rule(enabled=True).model_dump())
    update = {**profile, "name": "Changed display name"}
    key = "site-update-0001"
    response = client.post("/api/v1/sites/import", headers={**write_key, "Idempotency-Key": key}, json={"profile": update, "update": True})
    assert response.status_code == 403
    site = next(item for item in client.app.state.store.get_sites()["items"] if item["id"] == profile["id"])
    assert site["name"] == "Test tracker"
    assert site["revision"] == 1
    with client.app.state.store.connection() as con:
        assert con.execute("SELECT 1 FROM api_commands WHERE key_id=? AND key=?", (write_key["Authorization"].split(".")[0][4:], key)).fetchone() is None


def test_profile_preview_is_pure_and_invalid_import_does_not_echo_secrets(client):
    admin = admin_headers(client)
    _, bearer = create_key(client, admin, scopes=["read", "write"])
    profile = make_profile(client)
    before_sites = client.app.state.store.get_sites()
    before_events = client.app.state.store.events()
    before_rows = client.app.state.store.dashboard()["counts"]

    preview = client.post("/api/v1/sites/preview", headers=bearer, json=profile)
    assert preview.status_code == 200, preview.text
    assert preview.json()["profile"]["id"] == profile["id"]
    assert client.app.state.store.get_sites() == before_sites
    assert client.app.state.store.events() == before_events
    assert client.app.state.store.dashboard()["counts"] == before_rows

    secret = "profile-secret-must-not-echo"
    invalid = {**profile, "id": "invalid-profile", "cookie": secret, "base_url": "http://invalid.example"}
    response = client.post("/api/v1/sites/import", headers={**bearer, "Idempotency-Key": "bad-site-import-01"}, json={"profile": invalid})
    assert response.status_code == 422
    assert secret not in response.text
    assert "invalid-profile" not in {item["id"] for item in client.app.state.store.get_sites()["items"]}

    imported = client.post("/api/v1/sites/import", headers={**bearer, "Idempotency-Key": "good-site-import-1"}, json={"profile": profile})
    assert imported.status_code == 200, imported.text
    assert imported.json()["configured_secrets"] == {"cookie": False, "rss_url": False}


def test_ai_schema_and_openapi_are_confined_to_v1(client):
    admin = admin_headers(client)
    _, bearer = create_key(client, admin)
    schema = client.get("/api/v1/sites/schema", headers=bearer)
    template = client.get("/api/v1/sites/template", headers=bearer)
    spec = client.get("/api/v1/openapi.json", headers=bearer)
    assert schema.status_code == template.status_code == spec.status_code == 200
    assert schema.json()["properties"]["schema_version"]["default"] == 1
    assert template.json()["schema_version"] == 1
    paths = set(spec.json()["paths"])
    assert paths
    assert all(path.startswith("/api/v1/") for path in paths)
    assert "/api/api-keys" not in paths
    assert "/api/worker/config" not in paths
    assert spec.json()["servers"][0]["url"] == "http://testserver"
    assert "MachineAPIKey" in spec.json()["components"]["securitySchemes"]


def test_old_worker_cannot_claim_new_snapshot_jobs(client):
    admin = admin_headers(client)
    assert client.post("/api/runs", headers=admin, json={}).status_code == 200
    auth = {"Authorization": "Bearer " + WORKER_TOKEN}
    assert client.post("/api/worker/claim", headers=auth, json={"worker_id": "old"}).status_code == 409
    response = client.post("/api/worker/claim", headers=auth, json={"worker_id": "new", "execution_schema": 2})
    assert response.status_code == 200
    assert response.json()["job"]["payload"]["route"]["source"]["id"] == "zmpt"
    assert "settings" in response.json()["job"]["payload"]
