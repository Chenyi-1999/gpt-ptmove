"""Profiles, route fencing, legacy migration, and atomic command regressions."""
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timezone
import json
from pathlib import Path
import sqlite3

import pytest

from ptmove.db import Conflict, Store
from ptmove.models import Rule
from ptmove.policy import evaluate_candidates
from ptmove.security import Vault
from ptmove.sites import BUILTIN_PROFILES, SiteProfile, profile_url, published_url_valid


def profile(site_id="custom", host="tracker.example", roles=None):
    return {"schema_version": 1, "id": site_id, "name": "Custom tracker", "base_url": "https://" + host, "roles": roles or ["source", "target"], "ptnexus_site": site_id, "announce_hosts": [host]}


def candidate(site="zmpt", source_id="1", **changes):
    base = "https://zmpt.cc" if site == "zmpt" else "https://tracker.example"
    return {"source_id": source_id, "title": "Movie", "size_bytes": 2 * 1024 ** 3, "seeders": 10, "leechers": 2, "official": True, "category": "movie", "detail_url": base + "/details.php?id=" + source_id, "download_url": base + "/download.php?id=" + source_id, "restricted": False, "permission_verified": True, "target_exists": False, "target_work_exists": False, "published_at": datetime.now(timezone.utc).isoformat(), "metadata": {}, **changes}


@pytest.fixture
def store(tmp_path):
    return Store(tmp_path / "state.sqlite3")


def scan(store, items):
    store.create_run()
    job = store.claim("scanner")["job"]
    store.complete(job["id"], "scanner", job["lease_token"], True, {"items": items})
    return store.candidates()["items"][0]["id"]


@pytest.mark.parametrize("url", ["http://tracker.example", "https://127.0.0.1", "https://localhost", "https://private.local", "https://81.70.24.5", "https://0x51461805", "https://1363548165", "https://[::ffff:81.70.24.5]", "https://tracker.example/path", "https://user:secret@tracker.example", "https://tracker.example?passkey=x", "https://tracker.example:444"])
def test_origin_rejects_unsafe_values_offline(url, monkeypatch):
    monkeypatch.setattr("socket.getaddrinfo", lambda *args, **kw: pytest.fail("profile validation must not resolve DNS"))
    with pytest.raises(ValueError):
        SiteProfile.model_validate({**profile(), "base_url": url})


@pytest.mark.parametrize("change", [{"cookie": "secret"}, {"engine": "python"}, {"schema_version": 2}, {"schema_version": True}, {"roles": ["source", "source"]}, {"paths": {"detail": "https://evil.example/{id}"}}, {"paths": {"listing": "//evil.example"}}, {"paths": {"listing": "/%2e%2e/a"}}, {"paths": {"download": "/download.php?id={id}&passkey=secret"}}, {"paths": {"search": "/torrents.php?search={query.__class__}"}}, {"selectors": {"table": "[broken"}}, {"selectors": {"table": "a" * 513}}, {"announce_hosts": ["192.168.1.1"]}])
def test_profile_rejects_credentials_scripts_bad_templates_and_css(change):
    with pytest.raises(ValueError):
        SiteProfile.model_validate({**profile(), **change})


def test_profile_helpers_percent_encode_and_validate_publication():
    p = SiteProfile.model_validate(profile()).model_dump()
    assert profile_url(p, "search", query="a&passkey=bad") == "https://tracker.example/torrents.php?search=a%26passkey%3Dbad"
    assert published_url_valid("https://tracker.example/details.php?id=123", p)
    assert not published_url_valid("https://tracker.example/details.php?id=123&passkey=x", p)
    assert not published_url_valid("https://other.example/details.php?id=123", p)
    with pytest.raises(ValueError):
        profile_url(p, "detail", id="1&foo=x")


def test_registry_preview_no_writes_immutable_identity_explicit_update(store):
    before = store.get_sites()
    assert not store.preview_site(profile())["exists"]
    assert store.get_sites() == before
    result = store.import_site(profile())
    assert result["revision"] == 1 and not result["builtin"]
    with pytest.raises(Conflict):
        store.import_site(profile())
    with pytest.raises(Conflict):
        store.import_site({**profile(), "base_url": "https://other.example"}, update=True)
    with pytest.raises(Conflict):
        store.import_site(profile("alias"))
    with pytest.raises(Conflict):
        store.import_site(BUILTIN_PROFILES["zmpt"], update=True)
    changed = store.import_site({**profile(), "name": "Renamed"}, update=True)
    assert changed["revision"] == 2 and changed["digest"] != result["digest"]


def test_site_credentials_encrypted_and_isolated_with_legacy_fallback(store):
    store.set_settings({"source_cookie": "legacy-secret", "source_rss_url": "https://zmpt.cc/rss.php?passkey=legacy-rss"})
    assert store.route_settings(store.resolve_route())["source_cookie"] == "legacy-secret"
    store.import_site(profile())
    result = store.set_site_credentials("custom", {"cookie": "custom-secret", "rss_url": "https://tracker.example/rss.php?passkey=rss-secret"})
    assert result["configured_secrets"] == {"cookie": True, "rss_url": True}
    with pytest.raises(ValueError):
        store.set_site_credentials("custom", {"rss_url": "https://zmpt.cc/rss.php"})
    store.set_rule(Rule(source_site="custom").model_dump())
    selected = store.route_settings(store.resolve_route())
    assert selected["source_cookie"] == "custom-secret"
    assert "custom-secret" not in json.dumps(store.get_sites())
    assert "rss-secret" not in json.dumps(store.get_sites())
    with store.connection() as con:
        assert "custom-secret" not in store._clean(con, "custom-secret")
    for path in Path(store.path).parent.glob("state.sqlite3*"):
        assert b"custom-secret" not in path.read_bytes()
    store.set_site_credentials("zmpt", {"cookie": ""})
    assert store.route_settings({"source": BUILTIN_PROFILES["zmpt"], "target": BUILTIN_PROFILES["novahd"]})["source_cookie"] == ""


def test_policy_uses_actual_selected_source_origin(store):
    p = SiteProfile.model_validate(profile()).model_dump()
    rule = Rule(source_site="custom")
    assert evaluate_candidates([candidate("custom")], rule, p)[0]["selected"]
    assert not evaluate_candidates([candidate()], rule, p)[0]["selected"]


def test_ptnexus_mapping_can_be_added_before_rescanning_without_wasting_quota(store):
    store.import_site({**profile(), "ptnexus_site": None})
    store.set_rule(Rule(target_site="custom").model_dump())
    cid = scan(store, [candidate()])
    assert store.enqueue([cid])["queued"] == 0
    assert store.dashboard()["quota"]["used"] == 0
    store.import_site({**profile(), "ptnexus_site": "newmapping"}, update=True)
    assert store.get_site("custom")["ptnexus_site"] == "newmapping"
    assert store.enqueue([cid])["queued"] == 0


def test_cross_site_ids_and_task_routes_are_frozen(store):
    original_id = scan(store, [candidate()])
    first = store.enqueue([original_id])["items"][0]
    store.import_site(profile())
    store.set_rule(Rule(source_site="custom").model_dump())
    # Drain original transfer before scanner gets its new scan lease.
    transfer = store.claim("old-nas")["job"]
    assert transfer["payload"]["route"]["source"]["id"] == "zmpt"
    assert transfer["payload"]["rule"]["source_site"] == "zmpt"
    new_id = scan(store, [candidate("custom", source_site="forged", target_site="forged")])
    assert original_id != new_id
    assert store.enqueue([new_id])["queued"] == 1
    assert len(store.tasks()["items"]) == 2
    assert store.tasks()["items"][-1]["id"] == first["id"]
    assert store.candidates()["items"][0]["source_site"] == "custom"


def test_scan_completion_uses_snapshot_and_stale_candidates_cannot_enqueue(store):
    store.import_site(profile())
    store.set_rule(Rule(source_site="custom").model_dump())
    store.create_run()
    job = store.claim("scanner")["job"]
    store.set_rule(Rule(min_seeders=20).model_dump())
    store.complete(job["id"], "scanner", job["lease_token"], True, {"items": [candidate("custom")]})
    item = store.candidates()["items"][0]
    assert item["selected"] and item["source_site"] == "custom"
    assert store.enqueue([item["id"]])["queued"] == 0
    store.set_rule(Rule(source_site="custom").model_dump())
    store.import_site({**profile(), "selectors": {"title": "h2"}}, update=True)
    assert store.enqueue([item["id"]])["queued"] == 0


def test_changed_scheduled_rule_does_not_autoenqueue(store):
    store.set_rule(Rule(enabled=True).model_dump())
    store.create_run(scheduled=True)
    job = store.claim("scanner")["job"]
    store.set_rule(Rule(enabled=True, min_seeders=0).model_dump())
    store.complete(job["id"], "scanner", job["lease_token"], True, {"items": [candidate()]})
    assert store.tasks()["total"] == 0
    assert store.candidates()["total"] == 1


def test_publication_evidence_follows_frozen_target_after_rule_change(store):
    store.import_site(profile())
    store.set_rule(Rule(target_site="custom").model_dump())
    cid = scan(store, [candidate()])
    store.enqueue([cid])
    job = store.claim("nas")["job"]
    store.set_rule(Rule().model_dump())
    store.import_site({**profile(), "paths": {"detail": "/new.php?id={id}"}}, update=True)
    store.checkpoint(job["id"], "nas", job["lease_token"], {"status": "preparing"})
    store.checkpoint(job["id"], "nas", job["lease_token"], {"status": "publishing"})
    with pytest.raises(Conflict):
        store.checkpoint(job["id"], "nas", job["lease_token"], {"status": "published", "published_url": "https://pt.novahd.top/details.php?id=9"})
    store.checkpoint(job["id"], "nas", job["lease_token"], {"status": "published", "published_url": "https://tracker.example/details.php?id=9"})
    assert store.tasks()["items"][0]["published_url"].startswith("https://tracker.example/")


def test_atomic_command_concurrency_replay_authorization_and_rollback(store):
    def execute(_):
        return store.atomic_command("key1", "once", "scan", {}, store.create_run)
    with ThreadPoolExecutor(max_workers=6) as pool:
        results = list(pool.map(execute, range(12)))
    assert len({r["run_id"] for r in results}) == 1
    assert len(store.dashboard()["runs"]) == 1
    with pytest.raises(Conflict):
        store.atomic_command("key1", "once", "scan", {"changed": True}, store.create_run)
    def denied():
        raise PermissionError("revoked")
    with pytest.raises(PermissionError):
        store.atomic_command("key1", "once", "scan", {}, store.create_run, authorize=denied)
    def fails():
        store.create_run()
        raise ValueError("failure after write")
    with pytest.raises(ValueError):
        store.atomic_command("key1", "failed", "scan", {}, fails)
    assert len(store.dashboard()["runs"]) == 1
    assert store.atomic_command("key1", "failed", "scan", {}, store.create_run)["run_id"] != results[0]["run_id"]


def test_reconciliation_uses_original_custom_target(store):
    store.import_site(profile())
    store.set_rule(Rule(target_site="custom").model_dump())
    cid = scan(store, [candidate()])
    task = store.enqueue([cid])["items"][0]
    job = store.claim("nas")["job"]
    store.checkpoint(job["id"], "nas", job["lease_token"], {"status": "preparing"})
    store.checkpoint(job["id"], "nas", job["lease_token"], {"status": "publishing"})
    store.complete(job["id"], "nas", job["lease_token"], False, {"message": "ambiguous upload"})
    store.set_rule(Rule().model_dump())
    store.import_site({**profile(), "paths": {"detail": "/new.php?id={id}"}}, update=True)
    with pytest.raises(ValueError):
        store.reconcile(task["id"], "https://pt.novahd.top/details.php?id=12")
    assert store.reconcile(task["id"], "https://tracker.example/details.php?id=12") == {"ok": True, "kind": "seed_repair"}
    assert store.dashboard()["quota"]["used"] == 1


def test_legacy_migration_preserves_ids_reservations_evidence_and_jobs(tmp_path):
    path = tmp_path / "legacy.sqlite3"
    vault = Vault(tmp_path)
    encode = lambda v: vault.encrypt(json.dumps(v))
    now = datetime.now(timezone.utc).isoformat()
    c = candidate()
    c["id"] = 13
    data = {"candidate": c, "message": "uncertain", "published_url": None, "publish_attempted": True, "downloaded_bytes": 123, "created_at": now, "updated_at": now}
    with sqlite3.connect(path) as con:
        con.executescript("""
            CREATE TABLE config(key TEXT PRIMARY KEY,value TEXT NOT NULL);
            CREATE TABLE candidates(id INTEGER PRIMARY KEY,source_id TEXT NOT NULL UNIQUE,data TEXT NOT NULL,updated_at TEXT NOT NULL);
            CREATE TABLE tasks(id INTEGER PRIMARY KEY,candidate_id INTEGER NOT NULL,source_id TEXT NOT NULL,run_id INTEGER,month TEXT NOT NULL,status TEXT NOT NULL,slot INTEGER NOT NULL DEFAULT 1,reserved_bytes INTEGER NOT NULL,data TEXT NOT NULL,UNIQUE(source_id,month));
            CREATE TABLE runs(id INTEGER PRIMARY KEY,month TEXT NOT NULL,scheduled INTEGER NOT NULL,schedule_key TEXT UNIQUE,status TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
            CREATE TABLE jobs(id INTEGER PRIMARY KEY,kind TEXT NOT NULL,task_id INTEGER UNIQUE,run_id INTEGER,status TEXT NOT NULL,worker_id TEXT,lease_token TEXT,lease_expires REAL,created_at TEXT NOT NULL);
        """)
        legacy_rule = Rule().model_dump(exclude={"source_site", "target_site"})
        con.execute("INSERT INTO config VALUES('rule',?)", (json.dumps(legacy_rule),))
        con.execute("INSERT INTO candidates VALUES(13,'1',?,?)", (encode(c), now))
        con.execute("INSERT INTO runs VALUES(17,'2026-10',0,NULL,'succeeded',?,?)", (now, now))
        con.execute("INSERT INTO tasks VALUES(19,13,'1',17,'2026-10','needs_review',1,2147483648,?)", (encode(data),))
        con.execute("INSERT INTO jobs VALUES(23,'transfer',19,17,'failed',NULL,NULL,NULL,?)", (now,))
    # A corrupted legacy row fails the migration; all schema/data rewrites must
    # roll back together so repairing the row permits a clean retry.
    with sqlite3.connect(path) as con:
        con.execute("UPDATE candidates SET data='broken'")
    with pytest.raises(Exception):
        Store(path)
    with sqlite3.connect(path) as con:
        assert "source_site" not in {r[1] for r in con.execute("PRAGMA table_info(candidates)")}
        assert "snapshot" not in {r[1] for r in con.execute("PRAGMA table_info(runs)")}
        assert con.execute("PRAGMA user_version").fetchone()[0] == 0
        con.execute("UPDATE candidates SET data=?", (encode(c),))
    store = Store(path)
    task = store.tasks()["items"][0]
    assert task["id"] == 19 and task["candidate_id"] == 13 and task["run_id"] == 17
    assert task["publish_attempted"] and task["downloaded_bytes"] == 123
    assert task["route"]["target"]["id"] == "novahd"
    with store.connection() as con:
        assert con.execute("SELECT id FROM jobs WHERE task_id=19").fetchone()[0] == 23
        assert con.execute("SELECT slot,reserved_bytes FROM tasks").fetchone()[:] == (1, 2147483648)
    reopened = Store(path)
    assert reopened.tasks()["items"][0]["route"] == task["route"]
    with sqlite3.connect(path) as con:
        con.execute("PRAGMA user_version=999")
    with pytest.raises(ValueError, match="newer"):
        Store(path)
