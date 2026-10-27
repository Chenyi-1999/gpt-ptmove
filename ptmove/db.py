"""Durable controller state.

Invariants: reservations use BEGIN IMMEDIATE; one source/target/torrent/month
has one task; frozen routes prevent destination changes; lease tokens fence all
worker writes. Evidence and reserved bytes are monotonic. No transaction does
network I/O.
"""
from contextlib import contextmanager
from contextvars import ContextVar
from datetime import datetime, timezone
import hashlib
import json
from pathlib import Path
import secrets
import sqlite3
import time
from urllib.parse import parse_qsl, urlencode, urlsplit, urlunsplit

from .models import Rule
from .policy import evaluate_candidates
from .sites import BUILTIN_PROFILES, SiteProfile, profile_digest, published_url_valid, route_digest, validate_rss
from .scheduler import due, month_key, next_run
from .security import SETTING_KEYS, SECRET_SETTINGS, Vault, redact, validate_url

from . import __version__

VERSION = __version__
LEASE_SECONDS = 180
TERMINAL = {"succeeded", "failed", "needs_review", "cancelled"}
PROGRESS = {"queued": 0, "leased": 1, "downloading": 2, "preparing": 3, "publishing": 4, "published": 5, "seeding": 6, "succeeded": 7}


class Conflict(ValueError):
    pass


class NotFound(ValueError):
    pass


def stamp(value=None):
    return datetime.fromtimestamp(time.time() if value is None else value, timezone.utc).isoformat()


def safe_url(value):
    if not value:
        return value
    try:
        p = urlsplit(value)
        query = urlencode([(key, val if key.lower() == "id" else "[REDACTED]") for key, val in parse_qsl(p.query, keep_blank_values=True)])
        return urlunsplit((p.scheme, p.netloc, p.path, query, ""))
    except ValueError:
        return "[REDACTED]"


class Store:
    def __init__(self, path, clock=None, redaction_secrets=()):
        self.path = str(path)
        self.clock = clock or time.time
        self.redaction_secrets = redaction_secrets
        self._ambient = ContextVar("ptmove_connection", default=None)
        Path(self.path).parent.mkdir(parents=True, exist_ok=True)
        self.vault = Vault(Path(self.path).parent)
        with self.connection() as con:
            if con.execute("PRAGMA user_version").fetchone()[0] > 1:
                raise ValueError("database schema is newer than this application")
            con.executescript("""
                PRAGMA journal_mode=WAL;
                CREATE TABLE IF NOT EXISTS config (key TEXT PRIMARY KEY, value TEXT NOT NULL);
                CREATE TABLE IF NOT EXISTS candidates (id INTEGER PRIMARY KEY, source_id TEXT NOT NULL UNIQUE, data TEXT NOT NULL, updated_at TEXT NOT NULL);
                CREATE TABLE IF NOT EXISTS runs (id INTEGER PRIMARY KEY, month TEXT NOT NULL, scheduled INTEGER NOT NULL, schedule_key TEXT UNIQUE, status TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
                CREATE TABLE IF NOT EXISTS tasks (id INTEGER PRIMARY KEY, candidate_id INTEGER NOT NULL, source_id TEXT NOT NULL, run_id INTEGER, month TEXT NOT NULL, status TEXT NOT NULL, slot INTEGER NOT NULL DEFAULT 1, reserved_bytes INTEGER NOT NULL, data TEXT NOT NULL, UNIQUE(source_id, month));
                CREATE TABLE IF NOT EXISTS jobs (id INTEGER PRIMARY KEY, kind TEXT NOT NULL, task_id INTEGER UNIQUE, run_id INTEGER, status TEXT NOT NULL, worker_id TEXT, lease_token TEXT, lease_expires REAL, created_at TEXT NOT NULL);
                CREATE INDEX IF NOT EXISTS jobs_status ON jobs(status, id);
                CREATE INDEX IF NOT EXISTS tasks_month ON tasks(month);
                CREATE TABLE IF NOT EXISTS workers (id TEXT PRIMARY KEY, version TEXT NOT NULL, capabilities TEXT NOT NULL, last_seen TEXT NOT NULL);
                CREATE TABLE IF NOT EXISTS events (id INTEGER PRIMARY KEY, severity TEXT NOT NULL, message TEXT NOT NULL, created_at TEXT NOT NULL);
                CREATE TABLE IF NOT EXISTS revoked_sessions (id TEXT PRIMARY KEY, expires REAL NOT NULL);
            """)
            con.execute("INSERT OR IGNORE INTO config VALUES ('rule', ?)", (json.dumps(Rule().model_dump()),))
        with self.connection(True) as con:
            self._migrate_sites(con)

    @contextmanager
    def connection(self, write=False):
        ambient = self._ambient.get()
        if ambient is not None:
            yield ambient
            return
        con = sqlite3.connect(self.path, timeout=30, isolation_level=None)
        con.row_factory = sqlite3.Row
        con.execute("PRAGMA busy_timeout=30000")
        con.execute("PRAGMA foreign_keys=ON")
        try:
            if write:
                con.execute("BEGIN IMMEDIATE")
            yield con
            if write:
                con.commit()
        except Exception:
            if write:
                con.rollback()
            raise
        finally:
            con.close()

    def _migrate_sites(self, con):
        """One transaction preserves row IDs, jobs, reservations and evidence."""
        con.execute("CREATE TABLE IF NOT EXISTS sites (id TEXT PRIMARY KEY, data TEXT NOT NULL, revision INTEGER NOT NULL)")
        con.execute("CREATE TABLE IF NOT EXISTS site_credentials (id TEXT PRIMARY KEY, data TEXT NOT NULL)")
        con.execute("CREATE TABLE IF NOT EXISTS api_commands (key_id TEXT NOT NULL, key TEXT NOT NULL, operation TEXT NOT NULL, body_hash TEXT NOT NULL, response TEXT NOT NULL, UNIQUE(key_id,key))")
        if "source_site" not in {r[1] for r in con.execute("PRAGMA table_info(candidates)")}:
            con.execute("CREATE TABLE candidates_v1 (id INTEGER PRIMARY KEY, source_site TEXT NOT NULL, target_site TEXT NOT NULL, source_id TEXT NOT NULL, data TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE(source_site,target_site,source_id))")
            con.execute("INSERT INTO candidates_v1 SELECT id,'zmpt','novahd',source_id,data,updated_at FROM candidates")
            con.execute("DROP TABLE candidates")
            con.execute("ALTER TABLE candidates_v1 RENAME TO candidates")
            con.execute("CREATE TABLE tasks_v1 (id INTEGER PRIMARY KEY, candidate_id INTEGER NOT NULL, source_site TEXT NOT NULL, target_site TEXT NOT NULL, source_id TEXT NOT NULL, run_id INTEGER, month TEXT NOT NULL, status TEXT NOT NULL, slot INTEGER NOT NULL DEFAULT 1, reserved_bytes INTEGER NOT NULL, data TEXT NOT NULL, UNIQUE(source_site,target_site,source_id,month))")
            con.execute("INSERT INTO tasks_v1 SELECT id,candidate_id,'zmpt','novahd',source_id,run_id,month,status,slot,reserved_bytes,data FROM tasks")
            con.execute("DROP TABLE tasks")
            con.execute("ALTER TABLE tasks_v1 RENAME TO tasks")
            con.execute("CREATE INDEX tasks_month ON tasks(month)")
        if "snapshot" not in {r[1] for r in con.execute("PRAGMA table_info(runs)")}:
            con.execute("ALTER TABLE runs ADD COLUMN snapshot TEXT")
        rule = Rule.model_validate(self._rule(con)).model_dump()
        con.execute("UPDATE config SET value=? WHERE key='rule'", (json.dumps(rule),))
        legacy_rule = {**rule, "source_site": "zmpt", "target_site": "novahd"}
        legacy_route = {"source": BUILTIN_PROFILES["zmpt"], "target": BUILTIN_PROFILES["novahd"]}
        snapshot = {"rule": legacy_rule, "route": legacy_route}
        con.execute("UPDATE runs SET snapshot=? WHERE snapshot IS NULL", (self._encode(snapshot),))
        for row in con.execute("SELECT id,data FROM tasks").fetchall():
            data = self._decode(row["data"])
            if "route" not in data:
                data.update(rule=legacy_rule, route=legacy_route)
                data["candidate"].update(source_site="zmpt", target_site="novahd", route_digest=route_digest(legacy_route))
                con.execute("UPDATE tasks SET data=? WHERE id=?", (self._encode(data), row["id"]))
        for row in con.execute("SELECT id,data FROM candidates").fetchall():
            data = self._decode(row["data"])
            if "source_site" not in data:
                data.update(source_site="zmpt", target_site="novahd", route_digest=route_digest(legacy_route))
                con.execute("UPDATE candidates SET data=? WHERE id=?", (self._encode(data), row["id"]))
        con.execute("PRAGMA user_version=1")

    def atomic_command(self, key_id, idempotency_key, operation, body, callback, authorize=None):
        """Only database callbacks: response and side effects commit together."""
        if not isinstance(idempotency_key, str) or not 1 <= len(idempotency_key) <= 200 or any(ord(c) < 33 or ord(c) > 126 for c in idempotency_key):
            raise ValueError("invalid idempotency key")
        digest = hashlib.sha256(json.dumps(body, sort_keys=True, separators=(",", ":"), ensure_ascii=False, allow_nan=False).encode()).hexdigest()
        with self.connection(True) as con:
            token = self._ambient.set(con)
            try:
                if authorize is not None:
                    authorize()
                row = con.execute("SELECT * FROM api_commands WHERE key_id=? AND key=?", (key_id, idempotency_key)).fetchone()
                if row:
                    if row["operation"] != operation or row["body_hash"] != digest:
                        raise Conflict("idempotency key already used for a different command")
                    return self._decode(row["response"])
                result = callback()
                con.execute("INSERT INTO api_commands VALUES(?,?,?,?,?)", (key_id, idempotency_key, operation, digest, self._encode(result)))
                return result
            finally:
                self._ambient.reset(token)

    def _encode(self, value):
        return self.vault.encrypt(json.dumps(value, ensure_ascii=False))

    def _decode(self, value):
        return json.loads(self.vault.decrypt(value))

    def _now(self):
        return datetime.fromtimestamp(self.clock(), timezone.utc)

    def _month(self):
        return month_key(self._now())

    def _rule(self, con):
        return json.loads(con.execute("SELECT value FROM config WHERE key='rule'").fetchone()[0])

    def get_rule(self):
        with self.connection() as con:
            return self._rule(con)

    def set_rule(self, value):
        result = Rule.model_validate(value).model_dump()
        with self.connection(True) as con:
            self.resolve_route(con=con, rule=result)
            con.execute("UPDATE config SET value=? WHERE key='rule'", (json.dumps(result),))
            self._event(con, "info", "规则已更新；" + ("定时任务已启用" if result["enabled"] else "定时任务已停用"))
        return result

    def _site(self, con, site_id):
        if site_id in BUILTIN_PROFILES:
            return json.loads(json.dumps(BUILTIN_PROFILES[site_id]))
        row = con.execute("SELECT data FROM sites WHERE id=?", (site_id,)).fetchone()
        if row is None:
            raise NotFound("site not found")
        return self._decode(row[0])

    def get_site(self, site_id):
        with self.connection() as con:
            return self._site(con, site_id)

    def _site_credentials(self, con, site_id):
        row = con.execute("SELECT data FROM site_credentials WHERE id=?", (site_id,)).fetchone()
        values = self._decode(row[0]) if row else {}
        settings = self._settings(con)
        fallback = {"zmpt": {"cookie": settings["source_cookie"], "rss_url": settings["source_rss_url"]}, "novahd": {"cookie": settings["target_cookie"]}}.get(site_id, {})
        return {"cookie": "", "rss_url": "", **fallback, **values}

    def _site_public(self, con, site_id):
        profile = self._site(con, site_id)
        row = con.execute("SELECT revision FROM sites WHERE id=?", (site_id,)).fetchone()
        values = self._site_credentials(con, site_id)
        return {**profile, "builtin": site_id in BUILTIN_PROFILES, "revision": row[0] if row else 1, "digest": profile_digest(profile), "configured_secrets": {k: bool(v) for k, v in values.items()}}

    def get_sites(self):
        with self.connection() as con:
            ids = [*BUILTIN_PROFILES, *(r[0] for r in con.execute("SELECT id FROM sites ORDER BY id"))]
            return {"items": [self._site_public(con, site_id) for site_id in ids]}

    def _validate_site(self, con, value):
        if isinstance(value, str):
            if len(value.encode()) > 65536:
                raise ValueError("site profile exceeds 64 KiB")
            value = json.loads(value)
        if len(json.dumps(value, ensure_ascii=False).encode()) > 65536:
            raise ValueError("site profile exceeds 64 KiB")
        profile = SiteProfile.model_validate(value).model_dump()
        if profile["id"] in BUILTIN_PROFILES:
            raise Conflict("builtin sites cannot be overwritten")
        existing = con.execute("SELECT data FROM sites WHERE id=?", (profile["id"],)).fetchone()
        if existing:
            previous = self._decode(existing[0])
            if profile["base_url"] != previous["base_url"]:
                raise Conflict("site identity is immutable; import a new site id")
        others = list(BUILTIN_PROFILES.values()) + [self._decode(r[0]) for r in con.execute("SELECT data FROM sites WHERE id<>?", (profile["id"],))]
        if any(p["base_url"] == profile["base_url"] for p in others):
            raise Conflict("site origin is already registered")
        return profile, bool(existing)

    def preview_site(self, value):
        with self.connection() as con:
            profile, exists = self._validate_site(con, value)
            return {"profile": profile, "builtin": False, "exists": exists, "digest": profile_digest(profile)}

    def import_site(self, value, update=False):
        with self.connection(True) as con:
            profile, exists = self._validate_site(con, value)
            if exists and not update:
                raise Conflict("site exists; explicit update is required")
            con.execute("INSERT INTO sites(id,data,revision) VALUES(?,?,1) ON CONFLICT(id) DO UPDATE SET data=excluded.data,revision=sites.revision+1", (profile["id"], self._encode(profile)))
            self._event(con, "info", f"站点 {profile['id']} 配置已{'更新' if exists else '导入'}")
            return self._site_public(con, profile["id"])

    def set_site_credentials(self, site_id, patch):
        if not isinstance(patch, dict) or set(patch) - {"cookie", "rss_url"}:
            raise ValueError("invalid site credential fields")
        patch = {k: v for k, v in patch.items() if v is not None}
        if any(not isinstance(v, str) or len(v) > (16384 if k == "cookie" else 4096) or "\r" in v or "\n" in v for k, v in patch.items()):
            raise ValueError("invalid site credentials")
        with self.connection(True) as con:
            profile = self._site(con, site_id)
            if "rss_url" in patch:
                validate_rss(patch["rss_url"], profile)
            row = con.execute("SELECT data FROM site_credentials WHERE id=?", (site_id,)).fetchone()
            values = self._decode(row[0]) if row else {}
            values.update(patch)
            con.execute("INSERT INTO site_credentials VALUES(?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data", (site_id, self._encode(values)))
            self._event(con, "info", f"站点 {site_id} 的连接凭据已更新")
            return self._site_public(con, site_id)

    def resolve_route(self, con=None, rule=None):
        if con is None:
            with self.connection() as connection:
                return self.resolve_route(con=connection, rule=rule)
        rule = Rule.model_validate(rule or self._rule(con)).model_dump()
        route = {key: self._site(con, rule[key + "_site"]) for key in ("source", "target")}
        if any(key not in route[key]["roles"] for key in route):
            raise ValueError("site does not support the selected role")
        return route

    def route_settings(self, route):
        with self.connection() as con:
            settings = self._settings(con)
            source = self._site_credentials(con, route["source"]["id"])
            target = self._site_credentials(con, route["target"]["id"])
            return {**settings, "source_cookie": source["cookie"], "source_rss_url": source["rss_url"], "target_cookie": target["cookie"]}

    def _settings(self, con):
        row = con.execute("SELECT value FROM config WHERE key='settings'").fetchone()
        return {**dict.fromkeys(SETTING_KEYS, ""), "ptnexus_username": "admin", **(self._decode(row[0]) if row else {})}

    def get_settings(self, full=False):
        with self.connection() as con:
            result = self._settings(con)
        if full:
            return result
        configured = {key: bool(result[key]) for key in SECRET_SETTINGS}
        return {**{key: "" if key in SECRET_SETTINGS else value for key, value in result.items()}, "configured_secrets": configured}

    def set_settings(self, patch):
        for key, value in patch.items():
            if key not in SETTING_KEYS or not isinstance(value, str):
                raise ValueError("invalid setting")
            if key in ("source_rss_url", "qb_url", "ptnexus_url"):
                validate_url(value, rss=key == "source_rss_url")
        with self.connection(True) as con:
            result = self._settings(con)
            result.update(patch)
            con.execute("INSERT INTO config VALUES ('settings', ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", (self._encode(result),))
            self._event(con, "info", "连接设置已更新")
        return self.get_settings()

    def _clean(self, con, text):
        return redact(text, self._known_secrets(con))

    def _known_secrets(self, con):
        settings = self._settings(con)
        site_values = [value for row in con.execute("SELECT data FROM site_credentials") for value in self._decode(row[0]).values()]
        return [value for key, value in settings.items() if key in SECRET_SETTINGS] + site_values + list(self.redaction_secrets)

    def _event(self, con, severity, message):
        con.execute("INSERT INTO events(severity,message,created_at) VALUES(?,?,?)", (severity, self._clean(con, message), stamp(self.clock())))

    def events(self):
        with self.connection() as con:
            return {"items": [dict(row) for row in con.execute("SELECT * FROM events ORDER BY id DESC LIMIT 200")]}

    def revoke_session(self, sid, expires):
        with self.connection(True) as con:
            con.execute("DELETE FROM revoked_sessions WHERE expires<?", (self.clock(),))
            con.execute("INSERT OR REPLACE INTO revoked_sessions VALUES (?,?)", (sid, expires))

    def session_revoked(self, sid):
        with self.connection() as con:
            return con.execute("SELECT 1 FROM revoked_sessions WHERE id=?", (sid,)).fetchone() is not None

    def create_run(self, scheduled=False, now=None, preview=False):
        month = month_key(now or self._now())
        ts = stamp(self.clock())
        with self.connection(True) as con:
            rule = self._rule(con)
            if scheduled and not rule["enabled"]:
                return None
            snapshot = {"rule": rule, "route": self.resolve_route(con=con, rule=rule)}
            key = month if scheduled else None
            cur = con.execute("INSERT OR IGNORE INTO runs(month,scheduled,schedule_key,status,created_at,updated_at,snapshot) VALUES(?,?,?,'queued',?,?,?)", (month, int(scheduled), key, ts, ts, self._encode(snapshot)))
            if not cur.rowcount:
                return None
            run_id = cur.lastrowid
            job = con.execute("INSERT INTO jobs(kind,run_id,status,created_at) VALUES('scan',?,'queued',?)", (run_id, ts))
            self._event(con, "info", f"已创建{'定时' if scheduled else '预览' if preview else '手动'}扫描 #{run_id}")
            return {"run_id": run_id, "job_id": job.lastrowid, "status": "queued", "demo": False}

    def _candidate_public(self, candidate, known_secrets=()):
        result = dict(candidate)
        result["download_url"] = safe_url(result.get("download_url"))
        result["detail_url"] = safe_url(result.get("detail_url"))
        # Tracker metadata is untrusted and may contain cookies or passkeys.
        result["metadata"] = {key: result.get("metadata", {}).get(key) for key in ("year", "imdb", "douban", "imdb_id", "tmdb_id", "description", "poster_url") if key in result.get("metadata", {})}
        secrets_to_hide = list(known_secrets) + list(self.redaction_secrets)
        def clean(value):
            if isinstance(value, str):
                return redact(value, secrets_to_hide)
            if isinstance(value, dict):
                return {key: clean(val) for key, val in value.items() if isinstance(val, (str, int, float, bool)) or val is None}
            return value
        result = {key: (value if key in ("detail_url", "download_url") else clean(value)) for key, value in result.items()}
        for key in ("detail_url", "download_url"):
            if isinstance(result.get(key), str):
                for value in secrets_to_hide:
                    if value:
                        result[key] = result[key].replace(value, "[REDACTED]")
        return result

    def candidates(self):
        with self.connection() as con:
            rows = con.execute("SELECT data FROM candidates ORDER BY id DESC LIMIT 2000").fetchall()
            hidden = self._known_secrets(con)
            items = [self._candidate_public(self._decode(row[0]), hidden) for row in rows]
            return {"items": items, "total": con.execute("SELECT COUNT(*) FROM candidates").fetchone()[0]}

    def _task(self, con, row, worker=False):
        result = self._decode(row["data"])
        result.update(id=row["id"], candidate_id=row["candidate_id"], run_id=row["run_id"], month=row["month"], status=row["status"], source_site=row["source_site"], target_site=row["target_site"])
        job = con.execute("SELECT worker_id,lease_token,lease_expires FROM jobs WHERE task_id=?", (row["id"],)).fetchone()
        result.update(worker_id=job["worker_id"] if job else None, lease_expires_at=stamp(job["lease_expires"]) if job and job["lease_expires"] else None)
        if worker:
            result["lease_token"] = job["lease_token"] if job else None
            result["reserved_bytes"] = row["reserved_bytes"]
        else:
            result["candidate"] = self._candidate_public(result["candidate"], self._known_secrets(con))
        return result

    def tasks(self):
        with self.connection() as con:
            items = [self._task(con, row) for row in con.execute("SELECT * FROM tasks ORDER BY id DESC LIMIT 1000")]
            return {"items": items, "total": con.execute("SELECT COUNT(*) FROM tasks").fetchone()[0]}

    def _enqueue(self, con, candidate_ids, run_id=None, month=None):
        rule, month = self._rule(con), month or self._month()
        route = self.resolve_route(con=con, rule=rule)
        if not all(route[key].get("ptnexus_site") and route[key].get("announce_hosts") for key in ("source", "target")):
            return {"items": [], "queued": 0, "skipped": [{"candidate_id": cid, "reason": "请先补齐来源和目标站的PTNexus映射及Tracker域名，并重新扫描"} for cid in candidate_ids]}
        if run_id:
            run = con.execute("SELECT snapshot FROM runs WHERE id=?", (run_id,)).fetchone()
            frozen = self._decode(run[0])
            if frozen["rule"] != rule or frozen["route"] != route:
                return {"items": [], "queued": 0, "skipped": [{"candidate_id": cid, "reason": "扫描期间规则或站点配置已变更，请重新扫描"} for cid in candidate_ids]}
        identity = (rule["source_site"], rule["target_site"])
        queued, skipped = [], []
        for cid in dict.fromkeys(candidate_ids):
            row = con.execute("SELECT * FROM candidates WHERE id=?", (cid,)).fetchone()
            reason = None
            if row is None:
                skipped.append({"candidate_id": cid, "reason": "候选不存在"})
                continue
            original = self._decode(row["data"])
            candidate = evaluate_candidates([original], rule, route["source"])[0]
            if (row["source_site"], row["target_site"]) != identity or original.get("route_digest") != route_digest(route):
                reason = "候选站点或配置已变更，请重新扫描"
            elif not candidate["selected"]:
                reason = candidate["reason"]
            elif con.execute("SELECT 1 FROM tasks WHERE source_site=? AND target_site=? AND source_id=? AND month=?", (*identity, row["source_id"], month)).fetchone():
                reason = "本月已创建任务"
            if not reason:
                for previous in con.execute("SELECT data,status FROM tasks WHERE source_site=? AND target_site=? AND source_id=?", (*identity, row["source_id"],)):
                    evidence = self._decode(previous["data"])
                    if evidence["published_url"] or evidence["publish_attempted"]:
                        reason = "历史任务已发布或发布结果未核实，禁止重复发布"
                        break
                    if previous["status"] not in TERMINAL:
                        reason = "已有未完成任务"
                        break
            used, reserved = con.execute("SELECT COALESCE(SUM(slot),0),COALESCE(SUM(reserved_bytes),0) FROM tasks WHERE month=?", (month,)).fetchone()
            if not reason and (used >= rule["monthly_limit"] or reserved + candidate["size_bytes"] > int(rule["monthly_budget_gib"] * 1024 ** 3)):
                reason = "月度数量或下载预算不足"
            if reason:
                skipped.append({"candidate_id": cid, "reason": reason})
                continue
            ts = stamp(self.clock())
            run = con.execute("SELECT created_at FROM runs WHERE id=?", (run_id,)).fetchone() if run_id else None
            data = {"candidate": candidate, "rule": rule, "route": route, "message": "等待 NAS 工作节点", "published_url": None, "source_hash": None, "target_hash": None, "downloaded_bytes": 0, "publish_attempted": False, "ever_claimed": False, "created_at": ts, "updated_at": ts, "attempt_started_at": run[0] if run else ts}
            task_id = con.execute("INSERT INTO tasks(candidate_id,source_site,target_site,source_id,run_id,month,status,reserved_bytes,data) VALUES(?,?,?,?,?,?,'queued',?,?)", (cid, *identity, row["source_id"], run_id, month, candidate["size_bytes"], self._encode(data))).lastrowid
            con.execute("INSERT INTO jobs(kind,task_id,run_id,status,created_at) VALUES('transfer',?,?,'queued',?)", (task_id, run_id, ts))
            queued.append(self._task(con, con.execute("SELECT * FROM tasks WHERE id=?", (task_id,)).fetchone()))
        if queued:
            self._event(con, "info", f"已预留 {len(queued)} 个转种任务的月度名额和下载预算")
        return {"items": queued, "queued": len(queued), "skipped": skipped}

    def enqueue(self, candidate_ids):
        with self.connection(True) as con:
            self._expire(con)
            return self._enqueue(con, candidate_ids)

    def _save_task(self, con, row, data, status):
        data["updated_at"] = stamp(self.clock())
        con.execute("UPDATE tasks SET data=?,status=? WHERE id=?", (self._encode(data), status, row["id"]))

    def cancel(self, task_id):
        with self.connection(True) as con:
            row = con.execute("SELECT * FROM tasks WHERE id=?", (task_id,)).fetchone()
            if row is None:
                raise NotFound("task not found")
            data = self._decode(row["data"])
            # A leased worker may already be downloading; only untouched queued jobs release quota.
            if row["status"] != "queued" or data.get("ever_claimed", True) or data["publish_attempted"] or data["downloaded_bytes"]:
                raise Conflict("only an untouched queued transfer can be cancelled")
            self._save_task(con, row, {**data, "message": "已取消"}, "cancelled")
            con.execute("UPDATE tasks SET slot=0,reserved_bytes=0 WHERE id=?", (task_id,))
            con.execute("UPDATE jobs SET status='cancelled',lease_token=NULL,lease_expires=NULL WHERE task_id=?", (task_id,))
            return {"ok": True}

    def retry(self, task_id):
        with self.connection(True) as con:
            row = con.execute("SELECT * FROM tasks WHERE id=?", (task_id,)).fetchone()
            if row is None:
                raise NotFound("task not found")
            data = self._decode(row["data"])
            job = con.execute("SELECT * FROM jobs WHERE task_id=?", (task_id,)).fetchone()
            if job["status"] in ("queued", "leased"):
                raise Conflict("task already active")
            if data["published_url"]:
                kind, status = "seed_repair", "published"
            elif data["publish_attempted"] or row["status"] in ("needs_review", "cancelled", "succeeded"):
                raise Conflict("publication outcome requires reconciliation; automatic repost forbidden")
            elif row["status"] == "failed":
                if row["month"] != self._month():
                    raise Conflict("prior-month transfer must reserve a new current-month task")
                kind, status = "transfer", "queued"
            else:
                raise Conflict("task cannot safely be retried")
            data["message"] = "等待补种修复" if kind == "seed_repair" else "等待安全重试"
            data["attempt_started_at"] = stamp(self.clock())
            self._save_task(con, row, data, status)
            con.execute("UPDATE jobs SET kind=?,status='queued',worker_id=NULL,lease_token=NULL,lease_expires=NULL,created_at=? WHERE id=?", (kind, stamp(self.clock()), job["id"]))
            self._event(con, "info", f"任务 #{task_id} 已安排 {'补种修复' if kind == 'seed_repair' else '下载重试'}")
            return {"ok": True, "kind": kind}

    def reconcile(self, task_id, published_url):
        """An operator may supply missing evidence, never authorize another upload.

        The URL remains unverified until the NAS checks the target torrent against
        the retained source payload. This transition cannot release reservations.
        """
        self.reap_expired()
        with self.connection(True) as con:
            row = con.execute("SELECT * FROM tasks WHERE id=?", (task_id,)).fetchone()
            if row is None:
                raise NotFound("task not found")
            data = self._decode(row["data"])
            if not published_url_valid(published_url, data["route"]["target"]):
                raise ValueError("published URL must match the task's frozen target details URL")
            job = con.execute("SELECT * FROM jobs WHERE task_id=?", (task_id,)).fetchone()
            if row["status"] != "needs_review" or data["publish_attempted"] is not True or data["published_url"]:
                raise Conflict("only an uncertain attempted publication without a URL can be reconciled")
            if not job or job["status"] in ("queued", "leased"):
                raise Conflict("task must have a terminal fenced job before reconciliation")
            data.update(published_url=published_url, ever_claimed=True, attempt_started_at=stamp(self.clock()), message="管理员已提供目标发布链接，等待 NAS 验证目标资源并修复做种")
            self._save_task(con, row, data, "published")
            con.execute("UPDATE jobs SET kind='seed_repair',status='queued',worker_id=NULL,lease_token=NULL,lease_expires=NULL,created_at=? WHERE id=?", (stamp(self.clock()), job["id"]))
            self._event(con, "warning", f"管理员已提供任务 #{task_id} 的发布链接；仅安排 NAS 验证和补种修复，禁止重新发布")
            return {"ok": True, "kind": "seed_repair"}

    def heartbeat(self, worker_id, version, capabilities):
        with self.connection(True) as con:
            con.execute("INSERT INTO workers VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET version=excluded.version,capabilities=excluded.capabilities,last_seen=excluded.last_seen", (worker_id, version, json.dumps(capabilities), stamp(self.clock())))
        return {"ok": True}

    def _expire(self, con):
        for job in con.execute("SELECT * FROM jobs WHERE status IN ('queued','leased')").fetchall():
            row = con.execute("SELECT * FROM tasks WHERE id=?", (job["task_id"],)).fetchone() if job["task_id"] else None
            data = self._decode(row["data"]) if row else None
            frozen_rule = data["rule"] if data else self._decode(con.execute("SELECT snapshot FROM runs WHERE id=?", (job["run_id"],)).fetchone()[0])["rule"]
            timeout = frozen_rule["max_run_hours"] * 3600
            start = data.get("attempt_started_at", data["created_at"]) if data and job["kind"] == "transfer" else job["created_at"]
            timed_out = self.clock() - datetime.fromisoformat(start).timestamp() >= timeout
            month_expired = bool(row and row["month"] != self._month() and not data["publish_attempted"] and not data["published_url"])
            lease_expired = job["status"] == "leased" and job["lease_expires"] <= self.clock()
            if not timed_out and not lease_expired and not month_expired:
                continue
            if job["task_id"]:
                if data["published_url"]:
                    kind, status, jobstatus = "seed_repair", "published", "failed" if timed_out else "queued"
                elif data["publish_attempted"]:
                    kind, status, jobstatus = job["kind"], "needs_review", "failed"
                else:
                    kind, status, jobstatus = job["kind"], "failed" if timed_out or month_expired else "queued", "failed" if timed_out or month_expired else "queued"
                data["message"] = "已跨入新月份；原任务停止并保留配额，需按新月份重新提交" if month_expired else "运行总时限已到；保留下载配额和发布证据" if timed_out else "租约过期；等待人工核实发布结果" if status == "needs_review" else "租约过期；已安全重新排队"
                self._save_task(con, row, data, status)
                con.execute("UPDATE jobs SET kind=?,status=?,worker_id=NULL,lease_token=NULL,lease_expires=NULL,created_at=? WHERE id=?", (kind, jobstatus, stamp(self.clock()) if kind == "seed_repair" else job["created_at"], job["id"]))
                self._event(con, "warning", f"任务 #{row['id']} {data['message']}")
            else:
                status = "failed" if timed_out else "queued"
                con.execute("UPDATE jobs SET status=?,worker_id=NULL,lease_token=NULL,lease_expires=NULL WHERE id=?", (status, job["id"]))
                con.execute("UPDATE runs SET status=?,updated_at=? WHERE id=?", (status, stamp(self.clock()), job["run_id"]))

    def reap_expired(self):
        with self.connection(True) as con:
            self._expire(con)

    def claim(self, worker_id):
        with self.connection(True) as con:
            self._expire(con)
            # Stricter than transfer-only: one outstanding job per worker prevents scan floods too.
            if con.execute("SELECT 1 FROM jobs WHERE status='leased' AND worker_id=?", (worker_id,)).fetchone():
                return {"job": None}
            job = con.execute("SELECT * FROM jobs WHERE status='queued' ORDER BY id LIMIT 1").fetchone()
            if not job:
                return {"job": None}
            token, expiry = secrets.token_urlsafe(32), self.clock() + LEASE_SECONDS
            con.execute("UPDATE jobs SET status='leased',worker_id=?,lease_token=?,lease_expires=? WHERE id=?", (worker_id, token, expiry, job["id"]))
            if job["task_id"]:
                row = con.execute("SELECT * FROM tasks WHERE id=?", (job["task_id"],)).fetchone()
                data = self._decode(row["data"])
                data["ever_claimed"] = True
                self._save_task(con, row, data, "leased" if job["kind"] == "transfer" else row["status"])
                task = self._task(con, con.execute("SELECT * FROM tasks WHERE id=?", (row["id"],)).fetchone(), worker=True)
                payload = {"task": task, "candidate": task["candidate"], "rule": data["rule"], "route": data["route"]}
            else:
                run = con.execute("SELECT * FROM runs WHERE id=?", (job["run_id"],)).fetchone()
                con.execute("UPDATE runs SET status='running',updated_at=? WHERE id=?", (stamp(self.clock()), run["id"]))
                frozen = self._decode(run["snapshot"])
                payload = {"run_id": run["id"], "scheduled": bool(run["scheduled"]), "rule": frozen["rule"], "route": frozen["route"]}
            return {"job": {"id": job["id"], "kind": job["kind"], "lease_token": token, "payload": payload, "lease_expires_at": stamp(expiry)}}

    def _lease(self, con, job_id, worker_id, token):
        job = con.execute("SELECT * FROM jobs WHERE id=?", (job_id,)).fetchone()
        if not job or job["status"] != "leased" or job["worker_id"] != worker_id or job["lease_token"] != token or job["lease_expires"] <= self.clock():
            raise Conflict("stale, expired or invalid lease")
        return job

    def renew(self, job_id, worker_id, token):
        self.reap_expired()
        with self.connection(True) as con:
            self._lease(con, job_id, worker_id, token)
            con.execute("UPDATE jobs SET lease_expires=? WHERE id=?", (self.clock() + LEASE_SECONDS, job_id))
        return {"ok": True}

    def _update_progress(self, con, job, update, complete=False, success=False):
        row = con.execute("SELECT * FROM tasks WHERE id=?", (job["task_id"],)).fetchone()
        data = self._decode(row["data"])
        current = row["status"]
        status = update.get("status") or ("succeeded" if success else "failed")
        if status not in set(PROGRESS) | {"failed", "needs_review"} or status == "queued":
            raise Conflict("invalid worker status")
        if current in TERMINAL:
            raise Conflict("task is terminal")
        if complete and success and status != "succeeded":
            raise Conflict("successful transfer completion requires succeeded status")
        if complete and not success and status == "succeeded":
            raise Conflict("failed completion cannot claim success")
        if status == "publishing":
            if data["publish_attempted"]:
                raise Conflict("publication was already attempted; repost forbidden")
            if current != "preparing":
                raise Conflict("publication must follow preparing checkpoint")
            if con.execute("SELECT 1 FROM tasks WHERE id<>? AND status='publishing'", (row["id"],)).fetchone():
                raise Conflict("another publication is in progress")
            last = con.execute("SELECT value FROM config WHERE key='last_publish_at'").fetchone()
            if last and self.clock() - float(last[0]) < data["rule"]["publish_interval_seconds"]:
                raise Conflict("publish interval has not elapsed")
            con.execute("INSERT INTO config VALUES('last_publish_at', ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", (str(self.clock()),))
        if update.get("publish_attempted") is False and data["publish_attempted"]:
            raise Conflict("publication evidence cannot be cleared")
        for key in ("published_url", "source_hash", "target_hash"):
            value = update.get(key)
            if value is not None:
                if data.get(key) and value != data[key]:
                    raise Conflict(f"{key} cannot be cleared or changed")
                if key == "published_url" and value:
                    if not published_url_valid(value, data["route"]["target"]):
                        raise Conflict("invalid published URL")
                data[key] = value
        if update.get("downloaded_bytes") is not None:
            amount = update["downloaded_bytes"]
            if not isinstance(amount, int) or isinstance(amount, bool) or amount < data["downloaded_bytes"]:
                raise Conflict("downloaded bytes cannot decrease")
            if amount > data["candidate"]["size_bytes"]:
                raise Conflict("downloaded bytes exceeds reserved size")
            data["downloaded_bytes"] = amount
        if job["kind"] == "seed_repair" and status in ("leased", "downloading", "preparing", "publishing"):
            raise Conflict("published tasks may only repair seeding")
        if status == "published" and not data["published_url"]:
            raise Conflict("published stage requires durable published_url")
        if status in ("seeding", "succeeded") and not (data["published_url"] and data["target_hash"]):
            raise Conflict("target verification requires published_url and target_hash")
        if status in ("publishing", "published", "seeding", "succeeded"):
            data["publish_attempted"] = True
        if update.get("publish_attempted"):
            data["publish_attempted"] = True
        if status in PROGRESS and current in PROGRESS and PROGRESS[status] < PROGRESS[current]:
            raise Conflict("task stage cannot move backwards")
        if status == "failed" and data["publish_attempted"]:
            status = "published" if data["published_url"] else "needs_review"
        data["message"] = self._clean(con, update.get("message", ""))
        self._save_task(con, row, data, status)
        if complete or status in TERMINAL:
            con.execute("UPDATE jobs SET status=?,lease_token=NULL,lease_expires=NULL WHERE id=?", ("succeeded" if status == "succeeded" else "failed", job["id"]))
            self._event(con, "info" if status == "succeeded" else "warning", f"任务 #{row['id']}: {status} {data['message']}")

    def checkpoint(self, job_id, worker_id, token, update):
        self.reap_expired()
        with self.connection(True) as con:
            job = self._lease(con, job_id, worker_id, token)
            if not job["task_id"]:
                raise Conflict("scan jobs have no transfer checkpoint")
            self._update_progress(con, job, update)
        return {"ok": True}

    def complete(self, job_id, worker_id, token, success, result):
        self.reap_expired()
        with self.connection(True) as con:
            job = self._lease(con, job_id, worker_id, token)
            if job["kind"] == "scan":
                run = con.execute("SELECT * FROM runs WHERE id=?", (job["run_id"],)).fetchone()
                frozen = self._decode(run["snapshot"])
                items = result.get("items", [])
                if not isinstance(items, list) or len(items) > 2000:
                    raise ValueError("scan result must contain at most 2000 items")
                ids = []
                if success:
                    for item in evaluate_candidates(items, frozen["rule"], frozen["route"]["source"]):
                        source_id = str(item.get("source_id") or "")
                        if not source_id or len(source_id) > 256:
                            continue
                        # Controller owns IDs, regardless of worker-supplied data.
                        item.pop("id", None)
                        identity = (frozen["rule"]["source_site"], frozen["rule"]["target_site"])
                        item.update(source_site=identity[0], target_site=identity[1], route_digest=route_digest(frozen["route"]))
                        con.execute("INSERT INTO candidates(source_site,target_site,source_id,data,updated_at) VALUES(?,?,?,?,?) ON CONFLICT(source_site,target_site,source_id) DO UPDATE SET data=excluded.data,updated_at=excluded.updated_at", (*identity, source_id, self._encode(item), stamp(self.clock())))
                        cid = con.execute("SELECT id FROM candidates WHERE source_site=? AND target_site=? AND source_id=?", (*identity, source_id)).fetchone()[0]
                        item["id"] = cid
                        con.execute("UPDATE candidates SET data=? WHERE id=?", (self._encode(item), cid))
                        if item["selected"]:
                            ids.append(cid)
                    if run["scheduled"] and self._rule(con)["enabled"]:
                        # Charge the current month; a delayed old run cannot evade this month's limits.
                        self._enqueue(con, ids, run_id=run["id"])
                status = "succeeded" if success else "failed"
                con.execute("UPDATE jobs SET status=?,lease_token=NULL,lease_expires=NULL WHERE id=?", (status, job_id))
                con.execute("UPDATE runs SET status=?,updated_at=? WHERE id=?", (status, stamp(self.clock()), job["run_id"]))
                self._event(con, "info" if success else "warning", f"扫描 #{job['run_id']}: {status} {result.get('message', '')}")
            else:
                self._update_progress(con, job, result, complete=True, success=success)
        return {"ok": True}

    def dashboard(self):
        with self.connection(True) as con:
            self._expire(con)
            rule, month = self._rule(con), self._month()
            used, reserved, succeeded = con.execute("SELECT COALESCE(SUM(slot),0),COALESCE(SUM(reserved_bytes),0),COALESCE(SUM(status='succeeded'),0) FROM tasks WHERE month=?", (month,)).fetchone()
            published = sum(bool(self._decode(row[0]).get("published_url")) for row in con.execute("SELECT data FROM tasks WHERE month=?", (month,)))
            counts = {"candidates": con.execute("SELECT COUNT(*) FROM candidates").fetchone()[0], "tasks": con.execute("SELECT COUNT(*) FROM tasks").fetchone()[0], "pending": con.execute("SELECT COUNT(*) FROM tasks WHERE status NOT IN ('succeeded','failed','needs_review','cancelled')").fetchone()[0], "succeeded": con.execute("SELECT COUNT(*) FROM tasks WHERE status='succeeded'").fetchone()[0], "needs_review": con.execute("SELECT COUNT(*) FROM tasks WHERE status='needs_review'").fetchone()[0]}
            workers = [{**dict(row), "worker_id": row["id"], "capabilities": json.loads(row["capabilities"])} for row in con.execute("SELECT * FROM workers ORDER BY last_seen DESC")]
            catch_up = due(rule, self._now()) and not con.execute("SELECT 1 FROM runs WHERE schedule_key=?", (month,)).fetchone()
            return {"version": VERSION, "month": month, "rule": rule, "quota": {"used": used, "limit": rule["monthly_limit"], "reserved_bytes": reserved, "budget_bytes": int(rule["monthly_budget_gib"] * 1024 ** 3), "succeeded": succeeded, "published": published}, "counts": counts, "workers": workers, "next_run_at": self._now().isoformat() if catch_up else next_run(rule, self._now()), "runs": [dict(row) for row in con.execute("SELECT id,month,scheduled,schedule_key,status,created_at,updated_at FROM runs ORDER BY id DESC LIMIT 50")]}
