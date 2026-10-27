"""Durable controller state.

Invariants: every reservation is made under BEGIN IMMEDIATE; one source/month
has one task; a lease token fences every worker write; publication evidence and
reserved bytes are monotonic. No network I/O occurs inside these transactions.
"""
from contextlib import contextmanager
from datetime import datetime, timezone
import json
from pathlib import Path
import re
import secrets
import sqlite3
import time
from urllib.parse import parse_qsl, urlencode, urlsplit, urlunsplit

from .models import Rule
from .policy import evaluate_candidates
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
        Path(self.path).parent.mkdir(parents=True, exist_ok=True)
        self.vault = Vault(Path(self.path).parent)
        with self.connection() as con:
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

    @contextmanager
    def connection(self, write=False):
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
            con.execute("UPDATE config SET value=? WHERE key='rule'", (json.dumps(result),))
            self._event(con, "info", "规则已更新；" + ("定时任务已启用" if result["enabled"] else "定时任务已停用"))
        return result

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
        settings = self._settings(con)
        return redact(text, [value for key, value in settings.items() if key in SECRET_SETTINGS] + list(self.redaction_secrets))

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
            if scheduled and not self._rule(con)["enabled"]:
                return None
            key = month if scheduled else None
            cur = con.execute("INSERT OR IGNORE INTO runs(month,scheduled,schedule_key,status,created_at,updated_at) VALUES(?,?,?,'queued',?,?)", (month, int(scheduled), key, ts, ts))
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
            hidden = [val for key, val in self._settings(con).items() if key in SECRET_SETTINGS]
            items = [self._candidate_public(self._decode(row[0]), hidden) for row in rows]
            return {"items": items, "total": con.execute("SELECT COUNT(*) FROM candidates").fetchone()[0]}

    def _task(self, con, row, worker=False):
        result = self._decode(row["data"])
        result.update(id=row["id"], candidate_id=row["candidate_id"], run_id=row["run_id"], month=row["month"], status=row["status"])
        job = con.execute("SELECT worker_id,lease_token,lease_expires FROM jobs WHERE task_id=?", (row["id"],)).fetchone()
        result.update(worker_id=job["worker_id"] if job else None, lease_expires_at=stamp(job["lease_expires"]) if job and job["lease_expires"] else None)
        if worker:
            result["lease_token"] = job["lease_token"] if job else None
            result["reserved_bytes"] = row["reserved_bytes"]
        else:
            result["candidate"] = self._candidate_public(result["candidate"], [val for key, val in self._settings(con).items() if key in SECRET_SETTINGS])
        return result

    def tasks(self):
        with self.connection() as con:
            items = [self._task(con, row) for row in con.execute("SELECT * FROM tasks ORDER BY id DESC LIMIT 1000")]
            return {"items": items, "total": con.execute("SELECT COUNT(*) FROM tasks").fetchone()[0]}

    def _enqueue(self, con, candidate_ids, run_id=None, month=None):
        rule, month = self._rule(con), month or self._month()
        queued, skipped = [], []
        for cid in dict.fromkeys(candidate_ids):
            row = con.execute("SELECT * FROM candidates WHERE id=?", (cid,)).fetchone()
            reason = None
            if row is None:
                skipped.append({"candidate_id": cid, "reason": "候选不存在"})
                continue
            candidate = evaluate_candidates([self._decode(row["data"])], rule)[0]
            if not candidate["selected"]:
                reason = candidate["reason"]
            elif con.execute("SELECT 1 FROM tasks WHERE source_id=? AND month=?", (row["source_id"], month)).fetchone():
                reason = "本月已创建任务"
            if not reason:
                for previous in con.execute("SELECT data,status FROM tasks WHERE source_id=?", (row["source_id"],)):
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
            data = {"candidate": candidate, "message": "等待 NAS 工作节点", "published_url": None, "source_hash": None, "target_hash": None, "downloaded_bytes": 0, "publish_attempted": False, "ever_claimed": False, "created_at": ts, "updated_at": ts, "attempt_started_at": run[0] if run else ts}
            task_id = con.execute("INSERT INTO tasks(candidate_id,source_id,run_id,month,status,reserved_bytes,data) VALUES(?,?,?,?,'queued',?,?)", (cid, row["source_id"], run_id, month, candidate["size_bytes"], self._encode(data))).lastrowid
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
        if not isinstance(published_url, str) or len(published_url) > 4096 or not re.fullmatch(r"https://pt\.novahd\.top/details\.php\?id=[0-9]+", published_url):
            raise ValueError("published URL must be https://pt.novahd.top/details.php?id=<numeric id>")
        self.reap_expired()
        with self.connection(True) as con:
            row = con.execute("SELECT * FROM tasks WHERE id=?", (task_id,)).fetchone()
            if row is None:
                raise NotFound("task not found")
            data = self._decode(row["data"])
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
        timeout = self._rule(con)["max_run_hours"] * 3600
        for job in con.execute("SELECT * FROM jobs WHERE status IN ('queued','leased')").fetchall():
            row = con.execute("SELECT * FROM tasks WHERE id=?", (job["task_id"],)).fetchone() if job["task_id"] else None
            data = self._decode(row["data"]) if row else None
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
            rule = self._rule(con)
            if job["task_id"]:
                row = con.execute("SELECT * FROM tasks WHERE id=?", (job["task_id"],)).fetchone()
                data = self._decode(row["data"])
                data["ever_claimed"] = True
                self._save_task(con, row, data, "leased" if job["kind"] == "transfer" else row["status"])
                task = self._task(con, con.execute("SELECT * FROM tasks WHERE id=?", (row["id"],)).fetchone(), worker=True)
                payload = {"task": task, "candidate": task["candidate"], "rule": rule}
            else:
                run = con.execute("SELECT * FROM runs WHERE id=?", (job["run_id"],)).fetchone()
                con.execute("UPDATE runs SET status='running',updated_at=? WHERE id=?", (stamp(self.clock()), run["id"]))
                payload = {"run_id": run["id"], "scheduled": bool(run["scheduled"]), "rule": rule}
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
            if last and self.clock() - float(last[0]) < self._rule(con)["publish_interval_seconds"]:
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
                    parsed = urlsplit(value)
                    if parsed.scheme != "https" or parsed.hostname != "pt.novahd.top" or parsed.username or parsed.port not in (None, 443) or parsed.query and any(k in parsed.query.lower() for k in ("passkey", "token", "password")):
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
                items = result.get("items", [])
                if not isinstance(items, list) or len(items) > 2000:
                    raise ValueError("scan result must contain at most 2000 items")
                ids = []
                if success:
                    for item in evaluate_candidates(items, self._rule(con)):
                        source_id = str(item.get("source_id") or "")
                        if not source_id or len(source_id) > 256:
                            continue
                        # Controller owns IDs, regardless of worker-supplied data.
                        item.pop("id", None)
                        con.execute("INSERT INTO candidates(source_id,data,updated_at) VALUES(?,?,?) ON CONFLICT(source_id) DO UPDATE SET data=excluded.data,updated_at=excluded.updated_at", (source_id, self._encode(item), stamp(self.clock())))
                        cid = con.execute("SELECT id FROM candidates WHERE source_id=?", (source_id,)).fetchone()[0]
                        item["id"] = cid
                        con.execute("UPDATE candidates SET data=? WHERE id=?", (self._encode(item), cid))
                        if item["selected"]:
                            ids.append(cid)
                    run = con.execute("SELECT * FROM runs WHERE id=?", (job["run_id"],)).fetchone()
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
            return {"version": VERSION, "month": month, "rule": rule, "quota": {"used": used, "limit": rule["monthly_limit"], "reserved_bytes": reserved, "budget_bytes": int(rule["monthly_budget_gib"] * 1024 ** 3), "succeeded": succeeded, "published": published}, "counts": counts, "workers": workers, "next_run_at": self._now().isoformat() if catch_up else next_run(rule, self._now()), "runs": [dict(row) for row in con.execute("SELECT * FROM runs ORDER BY id DESC LIMIT 50")]}
