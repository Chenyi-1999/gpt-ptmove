"""NAS pull worker. Secrets and media stay on the NAS execution side.

An upload is always fenced with a durable controller checkpoint *and* a local
journal. Recovery of any attempted upload cannot call the publishing adapter.
"""
from __future__ import annotations

import json
import logging
import os
import socket
import tempfile
import threading
import time
import uuid
from datetime import datetime, timezone
from pathlib import Path
from urllib.parse import urlsplit

import httpx

from ptmove import __version__
from ptmove.adapters import (AdapterError, PTNexus, QBittorrent, SOURCE, TARGET,
                            Tracker, resolve_safe, safe_url, same_payload, torrent_metadata)

log = logging.getLogger("ptmove.worker")


class LeaseLost(AdapterError):
    pass


class Controller:
    def __init__(self, url: str, token: str, worker_id: str, client: httpx.Client | None = None):
        safe_url(url)
        host = urlsplit(url).hostname
        # No public HTTP credentials by accident. SSH tunnels use loopback.
        if urlsplit(url).scheme == "http" and host not in {"127.0.0.1", "localhost", "::1", "controller", "host.docker.internal"}:
            if os.getenv("PTMOVE_ALLOW_INSECURE_HTTP") != "1":
                raise AdapterError("执行端需要 HTTPS 或本机 SSH 隧道；禁止向公网 HTTP 发送令牌")
        if not token or len(token) < 24:
            raise AdapterError("未配置有效的执行端令牌")
        self.worker_id = worker_id
        self.resolve = client is None
        self.url = url
        self.client = client or httpx.Client(base_url=url.rstrip("/"), timeout=45, trust_env=False, transport=httpx.HTTPTransport(retries=2, trust_env=False),
                                             headers={"Authorization": "Bearer " + token})

    def call(self, method: str, path: str, **kwargs):
        if self.resolve:
            resolve_safe(self.url)
        try:
            r = self.client.request(method, path, **kwargs)
            if r.status_code == 409:
                raise LeaseLost("任务租约已失效或任务已终止")
            r.raise_for_status()
            return r.json()
        except (httpx.HTTPError, ValueError) as exc:
            raise AdapterError("无法连接控制台或执行端鉴权失败") from exc

    def heartbeat(self):
        return self.call("POST", "/api/worker/heartbeat", json={"worker_id": self.worker_id,
                         "version": __version__, "capabilities": {"qbittorrent": "4.1+/5.x", "ptnexus": True, "source": "zmpt", "target": "novahd"}})

    def claim(self):
        return self.call("POST", "/api/worker/claim", json={"worker_id": self.worker_id}).get("job")

    def config(self):
        return self.call("GET", "/api/worker/config")

    def job(self, job: dict, action: str, **fields):
        return self.call("POST", f"/api/worker/jobs/{job['id']}/{action}",
                         json={"worker_id": self.worker_id, "lease_token": job["lease_token"], **fields})


class LeaseKeeper:
    def __init__(self, controller: Controller, job: dict, interval: float = 30):
        self.controller, self.job, self.interval = controller, job, interval
        self.stopped, self.lost = threading.Event(), threading.Event()
        self.thread = threading.Thread(target=self.loop, daemon=True)

    def loop(self):
        while not self.stopped.wait(self.interval):
            try:
                self.controller.job(self.job, "renew")
                self.controller.heartbeat()
            except AdapterError:
                # One failed renewal is enough to stop new external side effects.
                self.lost.set()
                return

    def check(self):
        if self.lost.is_set():
            raise LeaseLost("续租失败，已停止后续操作")

    def __enter__(self):
        self.thread.start()
        return self

    def __exit__(self, *args):
        self.stopped.set()
        self.thread.join(timeout=2)


class Journal:
    def __init__(self, root: Path):
        self.root = root
        root.mkdir(parents=True, exist_ok=True)

    def path(self, task_id: str, suffix=".json") -> Path:
        import hashlib
        return self.root / (hashlib.sha256(str(task_id).encode()).hexdigest() + suffix)

    def load(self, task_id: str) -> dict:
        try:
            return json.loads(self.path(task_id).read_text("utf-8"))
        except FileNotFoundError:
            return {}

    def save(self, task_id: str, data: dict):
        dest = self.path(task_id)
        fd, name = tempfile.mkstemp(dir=self.root, prefix="journal-", suffix=".tmp")
        try:
            with os.fdopen(fd, "w", encoding="utf-8") as f:
                json.dump(data, f, ensure_ascii=False)
                f.flush()
                os.fsync(f.fileno())
            os.chmod(name, 0o600)
            os.replace(name, dest)
        finally:
            if os.path.exists(name):
                os.unlink(name)

    def source(self, task_id: str, raw: bytes | None = None) -> bytes:
        p = self.path(task_id, ".torrent")
        if raw is not None:
            with p.open("wb") as f:
                f.write(raw)
                f.flush()
                os.fsync(f.fileno())
            p.chmod(0o600)
            return raw
        return p.read_bytes()


class Executor:
    def __init__(self, controller: Controller, state_dir: Path, sleep=time.sleep):
        self.controller, self.state_dir, self.sleep = controller, state_dir, sleep
        self.journal = Journal(state_dir / "journal")

    def checkpoint(self, job: dict, keeper: LeaseKeeper, status: str, message: str, **fields):
        keeper.check()
        self.controller.job(job, "checkpoint", status=status, message=message, **fields)

    @staticmethod
    def require(settings: dict, *keys):
        if any(not str(settings.get(k, "")).strip() for k in keys):
            raise AdapterError("连接配置不完整，请先在连接设置中填写站点、qB 和 PTNexus 参数")

    def scan(self, job: dict, settings: dict, keeper: LeaseKeeper):
        self.require(settings, "source_cookie", "target_cookie")
        rule = job["payload"]["rule"]
        source, target = Tracker(SOURCE, settings["source_cookie"]), Tracker(TARGET, settings["target_cookie"])
        try:
            items = source.candidates(settings.get("source_rss_url", ""), rule["candidate_limit"])
            results = []
            for candidate in items:
                keeper.check()
                try:
                    current = source.inspect(candidate)
                    current["target_exists"], current["target_work_exists"] = target.duplicates(current)
                    current["download_url"] = f"{SOURCE}/download.php?id={current['source_id']}"
                    results.append(current)
                except AdapterError as exc:
                    results.append({**candidate, "permission_verified": False, "restriction_reason": str(exc)})
                self.sleep(2)  # bound tracker load, not a mechanism to evade access controls
            keeper.check()
            self.controller.job(job, "complete", success=True, result={"items": results, "message": f"已检查 {len(results)} 个候选；不确定项保留跳过原因"})
        finally:
            source.close()
            target.close()

    def _wait_complete(self, qb: QBittorrent, info_hash: str, timeout: float, keeper: LeaseKeeper, progress=None, metadata=None):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            keeper.check()
            info = qb.info(info_hash)
            if info:
                if info.get("state") in {"error", "missingFiles", "unknown"}:
                    raise AdapterError("qB 报告资源错误或文件缺失")
                if progress:
                    progress(info)
                if float(info.get("progress", 0)) >= 1 and int(info.get("amount_left", -1)) == 0 and info.get("state") not in {"checkingDL", "checkingUP", "checkingResumeData", "allocating"}:
                    if metadata:
                        qb.verify_files(info_hash, metadata)
                    return info
            self.sleep(10)
        raise AdapterError("下载或校验超时，保留资源与额度供后续处理")

    def _interval(self, rule: dict, keeper: LeaseKeeper):
        stamp = self.state_dir / "last_publish.txt"
        try:
            last = float(stamp.read_text("ascii"))
        except (FileNotFoundError, ValueError):
            last = 0
        while time.time() - last < rule["publish_interval_seconds"]:
            keeper.check()
            self.sleep(min(10, max(0.1, rule["publish_interval_seconds"] - (time.time() - last))))
        return stamp

    def transfer(self, job: dict, settings: dict, keeper: LeaseKeeper):
        payload = job["payload"]
        task, candidate, rule = payload["task"], payload["candidate"], payload["rule"]
        task_id = str(task["id"])
        state = self.journal.load(task_id)
        url = state.get("published_url") or task.get("published_url") or ""
        attempted = state.get("publish_attempted") or task.get("publish_attempted")
        repair = job["kind"] == "seed_repair" or bool(url)
        if attempted and not url:
            raise AdapterError("上次发布结果不确定，请人工确认目标站；禁止自动重复发布")
        self.require(settings, "source_cookie", "target_cookie", "qb_url", "qb_username", "qb_password", "qb_save_path")
        if not repair:
            self.require(settings, "ptnexus_url", "ptnexus_password", "ptnexus_downloader_id")
        source = Tracker(SOURCE, settings["source_cookie"])
        target = Tracker(TARGET, settings["target_cookie"])
        qb = QBittorrent(settings["qb_url"], settings["qb_username"], settings["qb_password"])
        engine = None
        try:
            qb.login()
            if not repair:
                # Check PTNexus connectivity before downloading any payload.
                engine = PTNexus(settings["ptnexus_url"], settings.get("ptnexus_username", "admin"), settings["ptnexus_password"])
                engine.login()
                current = source.inspect(candidate)
                current["target_exists"], current["target_work_exists"] = target.duplicates(current)
                from ptmove.policy import evaluate_candidates
                evaluated = evaluate_candidates([current], rule)[0]
                if not evaluated.get("selected"):
                    raise AdapterError("下载前复查未通过：" + evaluated.get("reason", "信息不确定"))
                candidate = current
                raw = source.torrent(candidate)
                meta = torrent_metadata(raw)
                if meta["size_bytes"] > int(task.get("reserved_bytes", candidate["size_bytes"])):
                    raise AdapterError("实际种子体积超过预留下载预算，需重新选种")
                self.journal.source(task_id, raw)
                keeper.check()
                source_hash = qb.add(raw, settings["qb_save_path"])
                state.update(source_hash=source_hash, publish_attempted=False)
                self.journal.save(task_id, state)
                last_progress = [0.0]
                def progress(info):
                    if time.monotonic() - last_progress[0] >= 30:
                        last_progress[0] = time.monotonic()
                        self.checkpoint(job, keeper, "downloading", f"资源下载进度 {float(info.get('progress', 0)):.0%}",
                                        source_hash=source_hash, downloaded_bytes=min(meta["size_bytes"], int(info.get("downloaded", 0))))
                self.checkpoint(job, keeper, "downloading", "已添加源站种子，等待资源完整下载", source_hash=source_hash)
                info = self._wait_complete(qb, source_hash, rule["max_download_hours"] * 3600, keeper, progress, meta)
                save_path = info.get("save_path") or settings["qb_save_path"]
                self.checkpoint(job, keeper, "preparing", "资源下载完成，正在准备媒体信息和发布资料", source_hash=source_hash,
                                downloaded_bytes=min(meta["size_bytes"], int(info.get("downloaded", meta["size_bytes"]))))
                prepared = engine.prepare(candidate, save_path, settings["ptnexus_downloader_id"])
                # Wait throttle BEFORE final duplicate/restriction checks.
                stamp = self._interval(rule, keeper)
                candidate = source.inspect(candidate)
                exists, work_exists = target.duplicates(candidate)
                candidate.update(target_exists=exists, target_work_exists=work_exists)
                if not evaluate_candidates([candidate], rule)[0].get("selected"):
                    raise AdapterError("发布前复查未通过，已停止发布")
                # Both checkpoints must succeed before a single external upload POST.
                self.checkpoint(job, keeper, "publishing", "已记录发布意图，提交一次发布请求", publish_attempted=True)
                state["publish_attempted"] = True
                self.journal.save(task_id, state)
                stamp.write_text(str(time.time()), "ascii")
                keeper.check()
                url = engine.publish(prepared)
                state["published_url"] = url
                self.journal.save(task_id, state)
                self.checkpoint(job, keeper, "published", "目标站已确认发布，开始添加目标种子", published_url=url)
            else:
                if not url:
                    raise AdapterError("缺少已确认的发布链接，只能人工核实")
                raw = self.journal.source(task_id) if self.journal.path(task_id, ".torrent").exists() else source.torrent(source.inspect(candidate))
                meta = torrent_metadata(raw)
                source_hash = state.get("source_hash") or task.get("source_hash") or meta["hash"]
                info = qb.info(source_hash)
                if not info or float(info.get("progress", 0)) < 1 or int(info.get("amount_left", -1)) != 0:
                    raise AdapterError("源资源未完整保留，暂停修复做种")
                save_path = info.get("save_path") or settings["qb_save_path"]
                qb.verify_files(source_hash, meta)
            target_raw = target.torrent(url)
            target_meta = torrent_metadata(target_raw)
            if not same_payload(meta, target_meta):
                raise AdapterError("目标种子文件清单或分片与源资源不同，需要人工核实")
            keeper.check()
            target_hash = qb.add_target(target_raw, save_path)
            state.update(target_hash=target_hash, published_url=url)
            self.journal.save(task_id, state)
            self.checkpoint(job, keeper, "seeding", "目标种子已添加，正在完整校验数据", published_url=url, target_hash=target_hash)
            keeper.check()
            qb.recheck(target_hash)
            self.sleep(5)
            self._wait_complete(qb, target_hash, 3600, keeper, metadata=target_meta)
            keeper.check()
            qb.start(target_hash)
            deadline = time.monotonic() + 180
            while time.monotonic() < deadline:
                keeper.check()
                info = qb.info(target_hash)
                tracker_ok = any(urlsplit(t.get("url", "")).hostname == "pt.novahd.top" and t.get("status") == 2 for t in qb.trackers(target_hash))
                if info and info.get("state") in {"uploading", "stalledUP", "forcedUP"} and float(info.get("progress", 0)) >= 1 and tracker_ok:
                    self.controller.job(job, "complete", success=True, result={"status": "succeeded", "message": "目标数据校验完成，Tracker 已确认做种",
                                        "published_url": url, "source_hash": source_hash, "target_hash": target_hash})
                    return
                self.sleep(10)
            raise AdapterError("已发布并校验，但尚未确认目标 Tracker 做种；仅重试做种修复")
        finally:
            qb.close()
            source.close()
            target.close()
            if engine:
                engine.close()

    def execute(self, job: dict):
        with LeaseKeeper(self.controller, job) as keeper:
            try:
                config = self.controller.config()
                if job["kind"] == "scan":
                    self.scan(job, config["settings"], keeper)
                elif job["kind"] in {"transfer", "seed_repair"}:
                    self.transfer(job, config["settings"], keeper)
                else:
                    raise AdapterError("不支持的任务类型")
            except LeaseLost:
                log.warning("任务租约失效，停止后续操作")
            except Exception as exc:
                # Never send repr(exception), upstream HTML, URLs, or credentials.
                message = str(exc) if isinstance(exc, AdapterError) else "执行发生异常，请检查执行端日志和配置"
                log.warning("任务未完成：%s", message)
                result = {"message": message, "status": "failed"}
                task = job.get("payload", {}).get("task", {})
                if task.get("id"):
                    state = self.journal.load(str(task["id"]))
                    for key in ("published_url", "source_hash", "target_hash"):
                        if state.get(key):
                            result[key] = state[key]
                try:
                    keeper.check()
                    self.controller.job(job, "complete", success=False, result=result)
                except AdapterError:
                    log.warning("无法回报任务，控制台将按租约状态恢复；不会自动重复发布")


def main():
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
    # HTTP clients log full URLs at INFO; prevent passkeys leaking via library logs.
    logging.getLogger("httpx").setLevel(logging.WARNING)
    logging.getLogger("httpcore").setLevel(logging.WARNING)
    state_dir = Path(os.getenv("PTMOVE_WORKER_STATE_DIR", "/state"))
    state_dir.mkdir(parents=True, exist_ok=True)
    identity = state_dir / "worker-id"
    if not identity.exists():
        identity.write_text(os.getenv("PTMOVE_WORKER_ID") or ("nas-" + uuid.uuid4().hex[:12]), "ascii")
    worker_id = os.getenv("PTMOVE_WORKER_ID") or identity.read_text("ascii").strip()
    controller = Controller(os.getenv("PTMOVE_CONTROLLER_URL", "http://127.0.0.1:18120"), os.getenv("PTMOVE_WORKER_TOKEN", ""), worker_id)
    executor = Executor(controller, state_dir)
    log.info("NAS 执行端 %s 已启动，等待任务", worker_id)
    while True:
        try:
            controller.heartbeat()
            job = controller.claim()
            if job:
                executor.execute(job)
                continue
        except AdapterError as exc:
            log.warning("%s", str(exc))
        time.sleep(15)


if __name__ == "__main__":
    main()
