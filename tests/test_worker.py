"""Controller + real executor integration with isolated adapter doubles.

No HTTP request to any tracker/qB/NAS is made. These tests distinguish mocked
workflow verification from the later, credential-dependent tracker validation.
"""
from datetime import datetime, timezone

import pytest

from ptmove import worker
from ptmove.adapters import AdapterError, torrent_metadata
from ptmove.db import Conflict, Store


def enc(value):
    if isinstance(value, int):
        return b"i" + str(value).encode() + b"e"
    if isinstance(value, str):
        value = value.encode()
    if isinstance(value, bytes):
        return str(len(value)).encode() + b":" + value
    return b"d" + b"".join(enc(k) + enc(v) for k, v in sorted(value.items())) + b"e"


def raw_torrent(source):
    return enc({"announce": "https://pt.novahd.top/announce.php?passkey=hidden" if source == "NOVA" else "https://zmpt.cc/announce.php?passkey=hidden",
                "info": {"name": "Example.mkv", "length": 2 * 1024 ** 3, "piece length": 2 * 1024 ** 3, "pieces": b"p" * 20, "private": 1, "source": source}})


def candidate():
    return {"source_id": "123", "title": "Example.2026.1080p-ZmWeb", "size_bytes": 2 * 1024 ** 3,
            "seeders": 12, "leechers": 5, "official": True, "category": "movie", "permission_verified": True,
            "restricted": False, "target_exists": False, "target_work_exists": False,
            "detail_url": "https://zmpt.cc/details.php?id=123", "download_url": "https://zmpt.cc/download.php?id=123",
            "published_at": datetime.now(timezone.utc).isoformat(), "metadata": {"imdb": "tt12345"}}


class LocalController:
    worker_id = "test-nas"

    def __init__(self, store):
        self.store = store
        self.settings = {"source_cookie": "private-source", "target_cookie": "private-target", "qb_url": "http://qb:8080",
                         "qb_username": "admin", "qb_password": "private-qb", "qb_save_path": "/media", "ptnexus_url": "http://engine:5274",
                         "ptnexus_username": "admin", "ptnexus_password": "private-engine", "ptnexus_downloader_id": "qb-nas"}
        self.after_publish_intent = None

    def config(self):
        return {"settings": self.settings, "rule": self.store.get_rule()}

    def heartbeat(self):
        return {}

    def job(self, job, action, **fields):
        try:
            if action == "checkpoint":
                result = self.store.checkpoint(job["id"], self.worker_id, job["lease_token"], fields)
                if fields["status"] == "publishing" and self.after_publish_intent:
                    self.after_publish_intent()
                return result
            if action == "complete":
                return self.store.complete(job["id"], self.worker_id, job["lease_token"], fields["success"], fields["result"])
            return self.store.renew(job["id"], self.worker_id, job["lease_token"])
        except Conflict as exc:
            raise worker.LeaseLost("lease conflict") from exc


@pytest.fixture
def execution(tmp_path, monkeypatch):
    store = Store(tmp_path / "controller.sqlite3")
    run = store.create_run()
    scan = store.claim("scanner")["job"]
    store.complete(scan["id"], "scanner", scan["lease_token"], True, {"items": [candidate()]})
    store.enqueue([store.candidates()["items"][0]["id"]])
    job = store.claim("test-nas")["job"]
    controller = LocalController(store)
    executor = worker.Executor(controller, tmp_path / "nas", sleep=lambda _: None)
    calls = {"publish": 0, "add_target": 0, "verify": 0, "fail_publish": False, "fail_seed": False}

    class Tracker:
        def __init__(self, base, cookie):
            self.base = base
        def close(self):
            pass
        def inspect(self, item):
            return dict(item)
        def duplicates(self, item):
            return False, False
        def torrent(self, value):
            return raw_torrent("NOVA" if self.base == worker.TARGET else "ZM")

    class QB:
        def __init__(self, *args, **kwargs):
            pass
        def close(self):
            pass
        def login(self):
            return {"version": "v5.1.4"}
        def info(self, info_hash):
            return {"hash": info_hash, "progress": 1, "amount_left": 0, "state": "stalledUP", "save_path": "/media", "downloaded": 2 * 1024 ** 3}
        def add(self, raw, path):
            return torrent_metadata(raw)["hash"]
        def add_target(self, raw, path):
            calls["add_target"] += 1
            if calls["fail_seed"]:
                raise AdapterError("模拟目标下载器不可用")
            return torrent_metadata(raw)["hash"]
        def verify_files(self, *args):
            calls["verify"] += 1
        def recheck(self, info_hash):
            pass
        def start(self, info_hash):
            pass
        def trackers(self, info_hash):
            return [{"url": "https://pt.novahd.top/announce.php?passkey=private", "status": 2}]

    class Engine:
        def __init__(self, *args):
            pass
        def close(self):
            pass
        def login(self):
            pass
        def prepare(self, *args):
            return {"task_id": "engine-ctx"}
        def publish(self, payload):
            calls["publish"] += 1
            if calls["fail_publish"]:
                raise AdapterError("模拟发布超时")
            return "https://pt.novahd.top/details.php?id=456"

    monkeypatch.setattr(worker, "Tracker", Tracker)
    monkeypatch.setattr(worker, "QBittorrent", QB)
    monkeypatch.setattr(worker, "PTNexus", Engine)
    return store, controller, executor, job, calls


def test_controller_to_executor_complete_workflow(execution):
    store, _, executor, job, calls = execution
    executor.execute(job)
    task = store.tasks()["items"][0]
    assert task["status"] == "succeeded"
    assert calls["publish"] == 1 and calls["add_target"] == 1 and calls["verify"] == 2
    assert task["published_url"].endswith("id=456")
    assert store.dashboard()["quota"]["used"] == 1


def test_worker_token_mode_does_not_require_username_or_password(execution):
    store, controller, executor, job, calls = execution
    controller.settings.update(qb_api_key="qbt_" + "B" * 28, qb_username="", qb_password="")
    executor.execute(job)
    assert store.tasks()["items"][0]["status"] == "succeeded"
    assert calls["publish"] == 1


def test_uncertain_publish_cannot_be_retried_or_republished(execution):
    store, _, executor, job, calls = execution
    calls["fail_publish"] = True
    executor.execute(job)
    task = store.tasks()["items"][0]
    assert task["status"] == "needs_review"
    with pytest.raises(Conflict):
        store.retry(task["id"])
    with pytest.raises(AdapterError, match="禁止自动重复发布"):
        executor.transfer(job, executor.controller.settings, worker.LeaseKeeper(executor.controller, job))
    assert calls["publish"] == 1


def test_publish_receipt_persists_and_retry_repairs_only_seeding(execution):
    store, controller, executor, job, calls = execution
    calls["fail_seed"] = True
    executor.execute(job)
    task = store.tasks()["items"][0]
    assert task["status"] == "published" and not task["target_hash"]
    assert store.retry(task["id"])["kind"] == "seed_repair"
    repair = store.claim(controller.worker_id)["job"]
    calls["fail_seed"] = False
    executor.execute(repair)
    assert store.tasks()["items"][0]["status"] == "succeeded"
    assert calls["publish"] == 1 and calls["add_target"] == 2
    assert store.dashboard()["quota"]["used"] == 1


def test_lease_lost_during_checkpoint_prevents_external_publish(execution):
    store, controller, executor, job, calls = execution
    keeper = worker.LeaseKeeper(controller, job)
    controller.after_publish_intent = keeper.lost.set
    with pytest.raises(worker.LeaseLost):
        executor.transfer(job, controller.settings, keeper)
    assert calls["publish"] == 0
    assert store.tasks()["items"][0]["publish_attempted"]


def test_public_plain_http_controller_is_rejected():
    with pytest.raises(AdapterError, match="HTTPS"):
        worker.Controller("http://152.136.236.238:18120", "t" * 40, "nas")
