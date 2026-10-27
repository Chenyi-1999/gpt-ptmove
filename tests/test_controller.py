from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timezone
import json

from fastapi.testclient import TestClient
import pytest

from ptmove.api import create_app
from ptmove.db import Conflict, Store
from ptmove.models import Rule
from ptmove.scheduler import due, next_run, tick


def candidate(source_id="1", size=2, **changes):
    return {"source_id": source_id, "title": f"Movie {source_id}", "size_bytes": size * 1024 ** 3, "seeders": 10, "leechers": 2, "official": True, "category": "movie", "detail_url": f"https://zmpt.cc/details.php?id={source_id}", "download_url": f"https://zmpt.cc/download.php?id={source_id}&passkey=scan-secret", "restricted": False, "restriction_reason": "", "permission_verified": True, "target_exists": False, "target_work_exists": False, "published_at": datetime.now(timezone.utc).isoformat(), "metadata": {}, **changes}


@pytest.fixture
def store(tmp_path):
    return Store(tmp_path / "controller.sqlite3")


@pytest.fixture
def client(tmp_path):
    app = create_app(tmp_path, "correct-password", "s" * 40, "w" * 40)
    with TestClient(app) as client:
        yield client


def authenticate(client):
    response = client.post("/api/auth/login", json={"password": "correct-password"})
    assert response.status_code == 200, response.text
    return {"X-CSRF-Token": response.json()["csrf_token"]}


def scan(store, items):
    run = store.create_run()
    job = store.claim("scanner")["job"]
    assert job["kind"] == "scan"
    store.complete(job["id"], "scanner", job["lease_token"], True, {"items": items})
    assert run["run_id"]
    return [item["id"] for item in store.candidates()["items"]]


def transfer(store):
    cid = scan(store, [candidate()])[0]
    task = store.enqueue([cid])["items"][0]
    return task, store.claim("nas")["job"]


def cp(store, job, status, **values):
    if status == "publishing":
        current = store.tasks()["items"][0]["status"]
        if current in ("leased", "downloading"):
            store.checkpoint(job["id"], "nas", job["lease_token"], {"status": "preparing"})
    return store.checkpoint(job["id"], "nas", job["lease_token"], {"status": status, **values})


def test_auth_csrf_origin_and_worker_separation(client):
    assert client.get("/health").json() == {"status": "ok", "version": "0.1.0"}
    assert client.get("/api/tasks").status_code == 401
    headers = authenticate(client)
    assert "HttpOnly" in client.cookies.get("ptmove_session", "") or client.cookies.get("ptmove_session")
    assert client.post("/api/preview", json={"demo": True}).status_code == 403
    assert client.post("/api/preview", json={"demo": True}, headers={**headers, "Origin": "https://evil.test"}).status_code == 403
    assert client.post("/api/preview", json={"demo": True}, headers={**headers, "Origin": "http://testserver"}).status_code == 200
    assert client.get("/api/worker/config").status_code == 401
    assert client.get("/api/worker/config", headers={"Authorization": "Bearer " + "w" * 40}).status_code == 200
    old = client.cookies.get("ptmove_session")
    assert client.post("/api/auth/logout", headers=headers).status_code == 200
    client.cookies.set("ptmove_session", old)
    assert client.get("/api/tasks").status_code == 401


def test_login_throttling_and_missing_password(tmp_path):
    with TestClient(create_app(tmp_path, "good", "s" * 40, "w" * 40)) as client:
        for _ in range(5):
            assert client.post("/api/auth/login", json={"password": "bad"}).status_code == 401
        assert client.post("/api/auth/login", json={"password": "good"}).status_code == 429
    with TestClient(create_app(tmp_path / "empty", "", "s" * 40, "w" * 40)) as client:
        assert client.post("/api/auth/login", json={"password": ""}).status_code == 503


def test_demo_and_preview_have_no_transfer_or_quota_side_effect(client):
    headers = authenticate(client)
    before = client.get("/api/dashboard").json()
    for _ in range(3):
        result = client.post("/api/preview", json={"demo": True}, headers=headers).json()
        assert result["demo"] and len(result["items"]) == 5
    after = client.get("/api/dashboard").json()
    assert after["counts"] == before["counts"]
    assert after["quota"] == before["quota"]
    assert after["runs"] == []
    assert client.post("/api/preview", json={"demo": False}, headers=headers).status_code == 200
    db = client.app.state.store
    job = db.claim("scanner")["job"]
    db.complete(job["id"], "scanner", job["lease_token"], True, {"items": [candidate()]})
    after = client.get("/api/dashboard").json()
    assert after["counts"]["candidates"] == 1
    assert after["counts"]["tasks"] == 0
    assert after["quota"]["used"] == 0


def test_settings_encrypted_omission_preserves_empty_clears_and_worker_secrets(client, tmp_path):
    headers = authenticate(client)
    response = client.put("/api/settings", headers=headers, json={"source_cookie": "secret-cookie-123456", "source_rss_url": "https://zmpt.cc/rss.php?passkey=rss-private", "qb_password": "qb-private", "qb_url": "http://192.168.1.2:8080"})
    assert response.status_code == 200
    assert response.json()["source_cookie"] == ""
    assert response.json()["configured_secrets"]["source_cookie"] is True
    client.put("/api/settings", headers=headers, json={"qb_username": "admin"})
    full = client.get("/api/worker/config", headers={"Authorization": "Bearer " + "w" * 40}).json()["settings"]
    assert full["source_cookie"] == "secret-cookie-123456"
    assert full["ptnexus_username"] == "admin"
    for path in tmp_path.glob("controller.sqlite3*"):
        assert b"secret-cookie-123456" not in path.read_bytes()
        assert b"rss-private" not in path.read_bytes()
    client.put("/api/settings", headers=headers, json={"source_cookie": ""})
    assert client.get("/api/settings").json()["configured_secrets"]["source_cookie"] is False
    assert client.put("/api/settings", headers=headers, json={"source_rss_url": "https://example.test/rss"}).status_code == 422
    assert client.put("/api/settings", headers=headers, json={"qb_url": "http://81.70.24.5:8080"}).status_code == 422
    assert client.put("/api/settings", headers=headers, json={"qb_url": "http://[::ffff:81.70.24.5]"}).status_code == 422


def test_atomic_quota_under_concurrent_requests(store):
    ids = scan(store, [candidate(str(i), size=10) for i in range(30)])
    with ThreadPoolExecutor(max_workers=12) as pool:
        results = list(pool.map(lambda cid: store.enqueue([cid]), ids * 2))
    assert sum(result["queued"] for result in results) == 6
    quota = store.dashboard()["quota"]
    assert quota["used"] == 6
    assert quota["reserved_bytes"] == 60 * 1024 ** 3
    assert len(store.tasks()["items"]) == 6


def test_budget_is_atomic_separate_from_slot_limit(store):
    store.set_rule(Rule(monthly_budget_gib=5).model_dump())
    ids = scan(store, [candidate(str(i), size=3) for i in range(10)])
    with ThreadPoolExecutor(max_workers=6) as pool:
        results = list(pool.map(lambda cid: store.enqueue([cid]), ids))
    assert sum(item["queued"] for item in results) == 1
    assert store.dashboard()["quota"]["reserved_bytes"] == 3 * 1024 ** 3


def test_rechecks_policy_at_submission_and_deduplicates(store):
    ids = scan(store, [candidate()])
    store.set_rule(Rule(min_seeders=20).model_dump())
    assert store.enqueue(ids)["queued"] == 0
    store.set_rule(Rule().model_dump())
    assert store.enqueue(ids + ids)["queued"] == 1
    assert store.enqueue(ids)["queued"] == 0
    assert "scan-secret" not in json.dumps(store.candidates())


def test_cancel_releases_only_untouched_queue_and_no_duplicate_reenqueue(store):
    ids = scan(store, [candidate("1"), candidate("2")])
    tasks = store.enqueue(ids)["items"]
    store.cancel(tasks[0]["id"])
    assert store.dashboard()["quota"]["used"] == 1
    assert store.enqueue([tasks[0]["candidate_id"]])["queued"] == 0
    job = store.claim("nas")["job"]
    with pytest.raises(Conflict):
        store.cancel(job["payload"]["task"]["id"])


def test_stale_lease_fencing_and_single_job_per_worker(store):
    task, old = transfer(store)
    assert store.claim("nas")["job"] is None
    with store.connection(True) as con:
        con.execute("UPDATE jobs SET lease_expires=0 WHERE id=?", (old["id"],))
    new = store.claim("nas2")["job"]
    assert new["id"] == old["id"] and new["lease_token"] != old["lease_token"]
    with pytest.raises(Conflict):
        store.renew(old["id"], "nas", old["lease_token"])
    with pytest.raises(Conflict):
        cp(store, old, "downloading", downloaded_bytes=12)
    with pytest.raises(Conflict):
        store.complete(old["id"], "nas", old["lease_token"], True, {})
    assert store.renew(new["id"], "nas2", new["lease_token"]) == {"ok": True}


def test_abandoned_publishing_is_needs_review_and_never_reposted(store):
    task, job = transfer(store)
    cp(store, job, "publishing")
    with store.connection(True) as con:
        con.execute("UPDATE jobs SET lease_expires=0 WHERE id=?", (job["id"],))
    assert store.claim("other")["job"] is None
    assert store.tasks()["items"][0]["status"] == "needs_review"
    with pytest.raises(Conflict):
        store.retry(task["id"])
    assert store.dashboard()["quota"]["used"] == 1


def test_failed_postpublish_preserves_url_and_retries_seed_repair_without_hash(store):
    task, job = transfer(store)
    cp(store, job, "publishing")
    cp(store, job, "published", published_url="https://pt.novahd.top/details.php?id=8")
    store.complete(job["id"], "nas", job["lease_token"], False, {"message": "target torrent not yet accessible"})
    assert store.tasks()["items"][0]["status"] == "published"
    assert store.retry(task["id"])["kind"] == "seed_repair"
    repair = store.claim("nas")["job"]
    assert repair["kind"] == "seed_repair"
    assert repair["payload"]["task"]["published_url"].endswith("id=8")
    with pytest.raises(Conflict):
        cp(store, repair, "publishing")
    cp(store, repair, "seeding", target_hash="target-hash")
    store.complete(repair["id"], "nas", repair["lease_token"], True, {})
    quota = store.dashboard()["quota"]
    assert quota["used"] == quota["published"] == quota["succeeded"] == 1


def test_expired_published_lease_repairs_without_reposting(store):
    _, job = transfer(store)
    cp(store, job, "publishing")
    cp(store, job, "published", published_url="https://pt.novahd.top/details.php?id=8")
    with store.connection(True) as con:
        con.execute("UPDATE jobs SET lease_expires=0 WHERE id=?", (job["id"],))
    assert store.claim("repair-node")["job"]["kind"] == "seed_repair"


def test_unknown_upload_failure_keeps_bytes_and_cannot_retry(store):
    task, job = transfer(store)
    cp(store, job, "downloading", downloaded_bytes=123)
    cp(store, job, "publishing")
    store.complete(job["id"], "nas", job["lease_token"], False, {"message": "POST timeout"})
    assert store.tasks()["items"][0]["status"] == "needs_review"
    assert store.dashboard()["quota"]["reserved_bytes"] == 2 * 1024 ** 3
    with pytest.raises(Conflict):
        store.retry(task["id"])


def test_partial_download_failure_keeps_bytes_and_retry_does_not_reserve_again(store):
    task, job = transfer(store)
    cp(store, job, "downloading", downloaded_bytes=123)
    store.complete(job["id"], "nas", job["lease_token"], False, {})
    assert store.retry(task["id"])["kind"] == "transfer"
    assert store.dashboard()["quota"]["used"] == 1
    assert store.dashboard()["quota"]["reserved_bytes"] == 2 * 1024 ** 3


def test_publication_evidence_and_downloads_are_monotonic(store):
    _, job = transfer(store)
    cp(store, job, "downloading", downloaded_bytes=123)
    with pytest.raises(Conflict):
        cp(store, job, "downloading", downloaded_bytes=122)
    cp(store, job, "publishing")
    with pytest.raises(Conflict):
        cp(store, job, "preparing")
    with pytest.raises(Conflict):
        cp(store, job, "publishing", publish_attempted=False)
    cp(store, job, "published", published_url="https://pt.novahd.top/details.php?id=8")
    with pytest.raises(Conflict):
        cp(store, job, "published", published_url="")
    with pytest.raises(Conflict):
        cp(store, job, "succeeded")


def test_schedule_uniqueness_concurrent_and_disabled_default(store):
    instant = datetime(2026, 10, 27, 12, tzinfo=timezone.utc)
    assert tick(store, instant) is None
    store.set_rule(Rule(enabled=True).model_dump())
    with ThreadPoolExecutor(max_workers=8) as pool:
        results = list(pool.map(lambda _: tick(store, instant), range(20)))
    assert sum(value is not None for value in results) == 1
    assert len(store.dashboard()["runs"]) == 1
    assert tick(store, datetime(2026, 11, 1, 1, tzinfo=timezone.utc)) is not None


def test_scheduled_scan_enqueues_but_disabling_before_completion_stops_it(store):
    store.set_rule(Rule(enabled=True).model_dump())
    store.create_run(scheduled=True)
    job = store.claim("scanner")["job"]
    assert job["payload"]["scheduled"] is True
    store.set_rule(Rule().model_dump())
    store.complete(job["id"], "scanner", job["lease_token"], True, {"items": [candidate()]})
    assert store.tasks()["total"] == 0


def test_event_redaction_and_persistence(store):
    store.set_settings({"source_cookie": "sensitive-cookie", "qb_password": "sensitive-password"})
    _, job = transfer(store)
    store.complete(job["id"], "nas", job["lease_token"], False, {"message": "sensitive-cookie password=sensitive-password https://zmpt.cc/download?passkey=hidden"})
    events = json.dumps(store.events())
    assert "sensitive-cookie" not in events and "sensitive-password" not in events and "passkey=hidden" not in events
    reopened = Store(store.path)
    assert reopened.dashboard()["quota"]["used"] == 1
    assert reopened.get_settings(full=True)["source_cookie"] == "sensitive-cookie"


def test_payload_limit_and_rule_validation(client):
    headers = authenticate(client)
    assert client.put("/api/rules", headers=headers, json={**Rule().model_dump(), "monthly_limit": 101}).status_code == 422
    assert client.post("/api/preview", headers=headers, content=b"x" * (4 * 1024 * 1024 + 1)).status_code == 413


def test_run_deadline_fences_even_renewed_lease_and_keeps_budget(store):
    task, job = transfer(store)
    cp(store, job, "downloading", downloaded_bytes=1)
    store.clock = lambda: datetime.now(timezone.utc).timestamp() + 73 * 3600
    with pytest.raises(Conflict):
        store.renew(job["id"], "nas", job["lease_token"])
    assert store.tasks()["items"][0]["status"] == "failed"
    assert store.dashboard()["quota"]["reserved_bytes"] == 2 * 1024 ** 3
    assert store.retry(task["id"])["kind"] == "transfer"
    assert store.claim("nas")["job"]["id"] == job["id"]


def test_run_deadline_preserves_known_publication_for_repair(store):
    task, job = transfer(store)
    cp(store, job, "publishing")
    cp(store, job, "published", published_url="https://pt.novahd.top/details.php?id=9")
    store.clock = lambda: datetime.now(timezone.utc).timestamp() + 73 * 3600
    assert store.claim("nas")["job"] is None
    assert store.tasks()["items"][0]["status"] == "published"
    assert store.retry(task["id"])["kind"] == "seed_repair"


def test_scan_deadline_does_not_requeue_forever(store):
    store.create_run()
    job = store.claim("scanner")["job"]
    store.clock = lambda: datetime.now(timezone.utc).timestamp() + 73 * 3600
    assert store.claim("scanner2")["job"] is None
    with pytest.raises(Conflict):
        store.complete(job["id"], "scanner", job["lease_token"], True, {"items": []})
    assert store.dashboard()["runs"][0]["status"] == "failed"


def test_publish_gate_serializes_workers_and_enforces_durable_interval(store):
    ids = scan(store, [candidate("1"), candidate("2")])
    store.enqueue(ids)
    first, second = store.claim("one")["job"], store.claim("two")["job"]
    for worker, job in (("one", first), ("two", second)):
        store.checkpoint(job["id"], worker, job["lease_token"], {"status": "preparing"})
    store.checkpoint(first["id"], "one", first["lease_token"], {"status": "publishing"})
    with pytest.raises(Conflict):
        store.checkpoint(second["id"], "two", second["lease_token"], {"status": "publishing"})
    with pytest.raises(Conflict):
        store.checkpoint(first["id"], "one", first["lease_token"], {"status": "publishing"})
    store.checkpoint(first["id"], "one", first["lease_token"], {"status": "published", "published_url": "https://pt.novahd.top/details.php?id=1"})
    with pytest.raises(Conflict):
        store.checkpoint(second["id"], "two", second["lease_token"], {"status": "publishing"})
    # A rejected gate did not mark the second transfer as attempted.
    second_task = next(item for item in store.tasks()["items"] if item["id"] == second["payload"]["task"]["id"])
    assert second_task["publish_attempted"] is False and second_task["status"] == "preparing"
    # Clock advances while both workers keep renewing their leases.
    store.clock = lambda: datetime.now(timezone.utc).timestamp() + 601
    with store.connection(True) as con:
        con.execute("UPDATE jobs SET lease_expires=? WHERE status='leased'", (store.clock() + 180,))
    store.checkpoint(second["id"], "two", second["lease_token"], {"status": "publishing"})


def test_month_end_schedule_uses_shanghai_and_last_day():
    rule = Rule(enabled=True, day=31, hour=2).model_dump()
    assert due(rule, datetime(2026, 2, 27, 18, tzinfo=timezone.utc))
    assert not due(rule, datetime(2026, 2, 27, 17, 59, tzinfo=timezone.utc))
    assert next_run(rule, datetime(2026, 2, 27, 19, tzinfo=timezone.utc)).startswith("2026-03-31T02:00")


def test_false_success_and_foreign_sensitive_candidate_fields_are_rejected(store):
    store.set_settings({"source_cookie": "candidate-cookie"})
    scan(store, [candidate(source_cookie="injected-secret", title="candidate-cookie", metadata={"description": "candidate-cookie"})])
    visible = json.dumps(store.candidates())
    assert "injected-secret" not in visible and "candidate-cookie" not in visible
    cid = store.candidates()["items"][0]["id"]
    store.enqueue([cid])
    job = store.claim("nas")["job"]
    with pytest.raises(Conflict):
        store.complete(job["id"], "nas", job["lease_token"], False, {"status": "succeeded", "published_url": "https://pt.novahd.top/details.php?id=1", "target_hash": "hash"})


def test_production_refuses_missing_secure_configuration(tmp_path, monkeypatch):
    monkeypatch.setenv("PTMOVE_ENV", "production")
    with pytest.raises(RuntimeError, match="production requires"):
        with TestClient(create_app(tmp_path, "short", "s", "w")):
            pass


def test_claim_history_never_refunds_zero_progress_after_expiry(store):
    task, job = transfer(store)
    cp(store, job, "downloading", source_hash="source-already-added", downloaded_bytes=0)
    with store.connection(True) as con:
        con.execute("UPDATE jobs SET lease_expires=0 WHERE id=?", (job["id"],))
    store.reap_expired()
    assert store.tasks()["items"][0]["status"] == "queued"
    with pytest.raises(Conflict):
        store.cancel(task["id"])
    quota = store.dashboard()["quota"]
    assert quota["used"] == 1 and quota["reserved_bytes"] == 2 * 1024 ** 3
    reopened = Store(store.path)
    with pytest.raises(Conflict):
        reopened.cancel(task["id"])


def test_month_boundary_fences_prepublish_and_requires_current_month_reservation(store):
    store.clock = lambda: datetime(2026, 10, 31, 15, 59, tzinfo=timezone.utc).timestamp()
    task, job = transfer(store)
    cp(store, job, "preparing", downloaded_bytes=2 * 1024 ** 3)
    store.clock = lambda: datetime(2026, 10, 31, 16, 1, tzinfo=timezone.utc).timestamp()
    with pytest.raises(Conflict):
        cp(store, job, "publishing")
    assert store.tasks()["items"][0]["status"] == "failed"
    assert store.dashboard()["month"] == "2026-11"
    assert store.dashboard()["quota"]["used"] == 0
    with pytest.raises(Conflict):
        store.retry(task["id"])
    new = store.enqueue([task["candidate_id"]])
    assert new["queued"] == 1 and new["items"][0]["month"] == "2026-11"
    assert store.dashboard()["quota"]["used"] == 1
    with store.connection() as con:
        old = con.execute("SELECT slot,reserved_bytes FROM tasks WHERE id=?", (task["id"],)).fetchone()
        assert old["slot"] == 1 and old["reserved_bytes"] == 2 * 1024 ** 3


@pytest.mark.parametrize("confirmed", [False, True])
def test_historical_publication_barrier_blocks_new_month_repost(store, confirmed):
    store.clock = lambda: datetime(2026, 10, 31, 15, 59, tzinfo=timezone.utc).timestamp()
    task, job = transfer(store)
    cp(store, job, "publishing")
    if confirmed:
        cp(store, job, "published", published_url="https://pt.novahd.top/details.php?id=11")
    store.complete(job["id"], "nas", job["lease_token"], False, {})
    store.clock = lambda: datetime(2026, 10, 31, 16, 1, tzinfo=timezone.utc).timestamp()
    result = store.enqueue([task["candidate_id"]])
    assert result["queued"] == 0
    assert "历史任务" in result["skipped"][0]["reason"]
    if confirmed:
        assert store.retry(task["id"])["kind"] == "seed_repair"
    else:
        with pytest.raises(Conflict):
            store.retry(task["id"])


def test_reconciliation_requires_admin_csrf_and_only_queues_repair(client):
    store = client.app.state.store
    task, job = transfer(store)
    cp(store, job, "publishing")
    store.complete(job["id"], "nas", job["lease_token"], False, {"message": "upload response lost"})
    before = store.dashboard()["quota"]
    endpoint = f"/api/tasks/{task['id']}/reconcile"
    body = {"published_url": "https://pt.novahd.top/details.php?id=123"}
    assert client.post(endpoint, json=body).status_code == 401
    headers = authenticate(client)
    assert client.post(endpoint, json=body).status_code == 403
    assert client.post(endpoint, json=body, headers={**headers, "Origin": "https://evil.test"}).status_code == 403
    response = client.post(endpoint, json=body, headers=headers)
    assert response.status_code == 200 and response.json() == {"ok": True, "kind": "seed_repair"}
    item = store.tasks()["items"][0]
    assert item["status"] == "published" and item["publish_attempted"] is True
    assert item["published_url"] == body["published_url"]
    after = store.dashboard()["quota"]
    assert after["used"] == before["used"] == 1
    assert after["reserved_bytes"] == before["reserved_bytes"] == 2 * 1024 ** 3
    assert client.post(endpoint, json={"published_url": "https://pt.novahd.top/details.php?id=456"}, headers=headers).status_code == 409
    repair = store.claim("nas")["job"]
    assert repair["kind"] == "seed_repair" and repair["lease_token"] != job["lease_token"]
    assert repair["payload"]["task"]["published_url"] == body["published_url"]
    with pytest.raises(Conflict):
        cp(store, repair, "publishing")
    # A wrong operator URL can fail target verification without authorizing upload.
    store.complete(repair["id"], "nas", repair["lease_token"], False, {"message": "target payload mismatch"})
    assert store.retry(task["id"])["kind"] == "seed_repair"
    assert store.dashboard()["quota"]["reserved_bytes"] == before["reserved_bytes"]


@pytest.mark.parametrize("url", [
    "http://pt.novahd.top/details.php?id=123", "https://81.70.24.5/details.php?id=123",
    "https://pt.novahd.top.evil.test/details.php?id=123", "https://pt.novahd.top/details.php?id=123&passkey=private",
    "https://pt.novahd.top/details.php?id=123#secret", "https://pt.novahd.top/details.php?id=abc",
    "https://user@pt.novahd.top/details.php?id=123", "https://pt.novahd.top/details.php?id=123\n",
])
def test_reconciliation_strict_target_url_does_not_mutate_state(store, url):
    task, job = transfer(store)
    cp(store, job, "publishing")
    store.complete(job["id"], "nas", job["lease_token"], False, {})
    with pytest.raises(ValueError):
        store.reconcile(task["id"], url)
    assert store.tasks()["items"][0]["status"] == "needs_review"
    assert store.tasks()["items"][0]["published_url"] is None
    assert store.dashboard()["quota"]["used"] == 1


@pytest.mark.parametrize("state", ["queued", "cancelled", "failed", "published", "succeeded"])
def test_reconciliation_rejects_nonambiguous_states(store, state):
    cid = scan(store, [candidate()])[0]
    task = store.enqueue([cid])["items"][0]
    if state == "cancelled":
        store.cancel(task["id"])
    elif state != "queued":
        job = store.claim("nas")["job"]
        if state == "failed":
            store.complete(job["id"], "nas", job["lease_token"], False, {})
        else:
            cp(store, job, "publishing")
            cp(store, job, "published", published_url="https://pt.novahd.top/details.php?id=8")
            if state == "succeeded":
                cp(store, job, "seeding", target_hash="target-hash")
                store.complete(job["id"], "nas", job["lease_token"], True, {})
            else:
                store.complete(job["id"], "nas", job["lease_token"], False, {})
    before = store.tasks()["items"][0]
    with pytest.raises(Conflict):
        store.reconcile(task["id"], "https://pt.novahd.top/details.php?id=999")
    assert store.tasks()["items"][0] == before


def test_reconciliation_across_month_preserves_old_quota_and_starts_repair_window(store):
    store.clock = lambda: datetime(2026, 10, 31, 15, 59, tzinfo=timezone.utc).timestamp()
    task, job = transfer(store)
    cp(store, job, "publishing")
    store.complete(job["id"], "nas", job["lease_token"], False, {})
    store.clock = lambda: datetime(2026, 11, 5, 16, 1, tzinfo=timezone.utc).timestamp()
    assert store.reconcile(task["id"], "https://pt.novahd.top/details.php?id=123")["kind"] == "seed_repair"
    assert store.claim("nas")["job"]["kind"] == "seed_repair"
    assert store.dashboard()["quota"]["used"] == 0
    with store.connection() as con:
        row = con.execute("SELECT month,slot,reserved_bytes FROM tasks WHERE id=?", (task["id"],)).fetchone()
        assert row["month"] == "2026-10" and row["slot"] == 1 and row["reserved_bytes"] == 2 * 1024 ** 3


def test_worker_compose_matches_nas_runtime_and_never_contains_token(client):
    authenticate(client)
    compose = client.get("/api/worker-compose").json()["compose"]
    for expected in ("image: ptmove:0.1.0", "network_mode: host", "env_file: .env.nas", "PTMOVE_WORKER_STATE_DIR: /state", "./worker-state:/state", "read_only: true", "healthcheck:\n      disable: true"):
        assert expected in compose
    assert "w" * 40 not in compose and "PTMOVE_WORKER_TOKEN" not in compose
