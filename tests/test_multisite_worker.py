"""Offline dynamic-route integration and network boundary regression checks."""
from copy import deepcopy
from datetime import datetime, timezone
from types import SimpleNamespace

import httpx
import pytest

from ptmove import worker
from ptmove.adapters import (AdapterError, PinnedTransport, Tracker, parse_listing,
                             profile_torrent_id, torrent_metadata, verify_torrent_networks)
from ptmove.db import Store
from ptmove.models import Rule
from ptmove.sites import BUILTIN_PROFILES, SiteProfile, profile_url
from test_adapters import bencode


def profile(identity, host):
    return SiteProfile(id=identity, name=identity, base_url="https://" + host,
        roles=["source", "target"], ptnexus_site=identity, announce_hosts=[host], official_groups=["official"],
        paths={"detail": "/torrent/{id}", "download": "/files/{id}", "search": "/find?q={query}"},
        selectors={"detail_link": 'a[href*="/torrent/"]', "download_link": 'a[href*="/files/"]'}).model_dump()


def raw(host, source="SRC", **extra):
    return bencode({"announce": "https://" + host + "/announce?passkey=hidden", "info": {
        "name": "Example.mkv", "length": 2 * 1024 ** 3, "piece length": 2 * 1024 ** 3,
        "pieces": b"p" * 20, "private": 1, "source": source}, **extra})


def test_custom_paths_listing_rss_and_target_download():
    p = profile("custom", "tracker.example")
    listing = '<table class="torrents"><tr><th>Type</th><th>Name</th><th>Size</th><th>Seeders</th><th>Leechers</th></tr><tr><td>movie</td><td><a href="/torrent/42">Movie-official</a></td><td>2 GiB</td><td>12</td><td>3</td></tr></table>'
    items = parse_listing(listing, p["base_url"], 50, p)
    assert items[0]["source_id"] == "42" and items[0]["official"]
    assert items[0]["download_url"] == "https://tracker.example/files/42"
    paths = []
    def handler(request):
        paths.append(request.url.path)
        if request.url.path == "/rss":
            return httpx.Response(200, text='<rss version="2.0"><channel><title>Feed</title><item><title>Example</title><link>https://tracker.example/torrent/42</link></item></channel></rss>')
        return httpx.Response(200, content=raw("tracker.example"))
    tracker = Tracker(p, "cookie", httpx.Client(transport=httpx.MockTransport(handler)))
    assert tracker.candidates("https://tracker.example/rss", 10)[0]["source_id"] == "42"
    assert torrent_metadata(tracker.torrent("https://tracker.example/torrent/42"))["private"]
    assert paths == ["/rss", "/files/42"]
    with pytest.raises(AdapterError):
        profile_torrent_id("https://evil.example/torrent/42", p)
    assert profile_torrent_id("https://tracker.example/torrent/42", p) == "42"
    assert profile_torrent_id("https://tracker.example/files/42?passkey=secret", p, "download") == "42"
    assert profile_torrent_id("https://zmpt.cc/download.php?passkey=secret&id=42", BUILTIN_PROFILES["zmpt"], "download") == "42"
    assert profile_torrent_id("https://zmpt.cc/download.php?id=42&id=99", BUILTIN_PROFILES["zmpt"], "download") == ""


def test_duplicate_search_retains_nexusphp_imdb_mode():
    queries = []
    def handler(request):
        queries.append(dict(request.url.params))
        return httpx.Response(200, text="<p>没有找到种子</p>")
    tracker = Tracker(BUILTIN_PROFILES["novahd"], "cookie", httpx.Client(transport=httpx.MockTransport(handler)))
    assert tracker.duplicates({"title": "Movie", "metadata": {"imdb": "tt1234567"}}) == (False, False)
    assert queries[0] == {"search": "tt1234567", "search_area": "4", "search_mode": "0"}


def test_empty_unrecognized_table_is_not_evidence_of_absence():
    tracker = Tracker(BUILTIN_PROFILES["novahd"], "cookie", httpx.Client(transport=httpx.MockTransport(
        lambda request: httpx.Response(200, text='<table class="torrents"><tr><td>Loading...</td></tr></table>'))))
    assert tracker.duplicates({"title": "Movie", "metadata": {"imdb": "tt1234567"}}) == (None, None)


def test_pinned_transport_preserves_tls_host_and_blocks_any_protected_answer(monkeypatch):
    import ptmove.adapters as adapters
    observed = []
    monkeypatch.setattr(adapters.socket, "getaddrinfo", lambda *a, **k: [(2, 1, 6, "", ("1.1.1.1", 443))])
    def handle(self, request):
        observed.append((request.url.host, request.headers["Host"], request.extensions["sni_hostname"]))
        return httpx.Response(200)
    monkeypatch.setattr(httpx.HTTPTransport, "handle_request", handle)
    transport = PinnedTransport(public_only=True)
    request = httpx.Request("GET", "https://tracker.example/list")
    transport.handle_request(request)
    assert observed == [("1.1.1.1", "tracker.example", "tracker.example")]
    assert request.url.host == "tracker.example"
    monkeypatch.setattr(adapters.socket, "getaddrinfo", lambda *a, **k: [(2, 1, 6, "", ("1.1.1.1", 443)), (2, 1, 6, "", ("81.70.24.5", 443))])
    with pytest.raises(AdapterError):
        transport.handle_request(request)
    assert len(observed) == 1
    monkeypatch.setattr(adapters.socket, "getaddrinfo", lambda *a, **k: [(2, 1, 6, "", ("127.0.0.1", 443))])
    with pytest.raises(AdapterError):
        transport.handle_request(request)


@pytest.mark.parametrize("extra", [
    {"announce-list": [["https://81.70.24.5/announce"]]},
    {"announce-list": [["https://other.example/announce"]]},
    {"url-list": ["https://81.70.24.5/file"]},
    {"httpseeds": ["https://other.example/file"]},
    {"nodes": [["81.70.24.5", 6881]]},
])
def test_torrent_network_endpoints_are_bounded(extra):
    with pytest.raises(AdapterError):
        verify_torrent_networks(raw("zmpt.cc", **extra), BUILTIN_PROFILES["zmpt"])


def test_dynamic_transfer_uses_frozen_route_after_registry_and_rule_change(tmp_path, monkeypatch):
    store = Store(tmp_path / "controller.sqlite3")
    source, target = profile("alpha", "alpha.example"), profile("beta", "beta.example")
    for p in (source, target):
        store.import_site(p)
        store.set_site_credentials(p["id"], {"cookie": p["id"] + "-secret"})
    store.set_rule(Rule(source_site="alpha", target_site="beta").model_dump())
    item = {"source_id": "42", "title": "Example-official", "size_bytes": 2 * 1024 ** 3,
        "seeders": 12, "leechers": 3, "official": True, "category": "movie", "permission_verified": True,
        "restricted": False, "target_exists": False, "target_work_exists": False, "metadata": {},
        "detail_url": profile_url(source, "detail", id=42), "download_url": profile_url(source, "download", id=42),
        "published_at": datetime.now(timezone.utc).isoformat()}
    store.create_run()
    scan = store.claim("scanner")["job"]
    store.complete(scan["id"], "scanner", scan["lease_token"], True, {"items": [item]})
    store.enqueue([store.candidates()["items"][0]["id"]])
    changed = deepcopy(target)
    changed["paths"]["detail"] = "/new/{id}"
    store.import_site(changed, update=True)
    store.set_rule(Rule().model_dump())
    job = store.claim("nas")["job"]
    assert job["payload"]["route"]["target"]["paths"]["detail"] == "/torrent/{id}"
    calls = []
    class FakeTracker:
        def __init__(self, p, cookie):
            self.profile = p
            calls.append(("tracker", p["id"], cookie))
        def close(self): pass
        def inspect(self, candidate): return dict(candidate)
        def duplicates(self, candidate): return False, False
        def torrent(self, candidate): return raw(self.profile["announce_hosts"][0], self.profile["id"])
    class QB:
        def __init__(self, *a, **k): pass
        def close(self): pass
        def login(self): pass
        def add(self, data, path): return torrent_metadata(data)["hash"]
        def add_target(self, data, path, p):
            calls.append(("seed", p["id"]))
            return torrent_metadata(data)["hash"]
        def info(self, info_hash): return {"progress": 1, "amount_left": 0, "state": "stalledUP", "save_path": "/media"}
        def verify_files(self, *a): pass
        def recheck(self, *a): pass
        def start(self, *a): pass
        def trackers(self, *a): return [{"url": "https://beta.example/announce", "status": 2}]
    class Engine:
        def __init__(self, *a): pass
        def close(self): pass
        def login(self): pass
        def prepare(self, c, path, downloader, s, t):
            calls.append(("prepare", s["ptnexus_site"], t["ptnexus_site"]))
            return {}
        def publish(self, prepared, p):
            calls.append(("publish", p["id"]))
            return profile_url(p, "detail", id=99)
    class Controller:
        worker_id = "nas"
        def job(self, job, action, **fields):
            if action == "checkpoint":
                return store.checkpoint(job["id"], "nas", job["lease_token"], fields)
            return store.complete(job["id"], "nas", job["lease_token"], fields["success"], fields["result"])
    for name, cls in (("Tracker", FakeTracker), ("QBittorrent", QB), ("PTNexus", Engine)):
        monkeypatch.setattr(worker, name, cls)
    settings = {**store.route_settings(job["payload"]["route"]), "qb_url": "http://qb", "qb_username": "u", "qb_password": "p",
        "qb_save_path": "/media", "ptnexus_url": "http://engine", "ptnexus_password": "p", "ptnexus_downloader_id": "qb"}
    executor = worker.Executor(Controller(), tmp_path / "state", sleep=lambda _: None)
    executor.transfer(job, settings, SimpleNamespace(check=lambda: None))
    task = store.tasks()["items"][0]
    assert task["status"] == "succeeded"
    assert task["published_url"] == "https://beta.example/torrent/99"
    assert ("tracker", "alpha", "alpha-secret") in calls and ("tracker", "beta", "beta-secret") in calls
    assert ("prepare", "alpha", "beta") in calls and ("seed", "beta") in calls
