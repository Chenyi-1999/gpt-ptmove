from datetime import datetime
import hashlib
import json

import httpx
import pytest

from ptmove.adapters import (AdapterError, PTNexus, QBittorrent, Tracker,
                            parse_listing, payload_signature, same_payload, torrent_metadata)


def bencode(value):
    if isinstance(value, int):
        return b"i" + str(value).encode() + b"e"
    if isinstance(value, bytes):
        return str(len(value)).encode() + b":" + value
    if isinstance(value, str):
        return bencode(value.encode())
    if isinstance(value, list):
        return b"l" + b"".join(map(bencode, value)) + b"e"
    return b"d" + b"".join(bencode(k) + bencode(v) for k, v in sorted(value.items())) + b"e"


def torrent(announce="https://zmpt.cc/announce.php?passkey=private", source="ZM", size=2 * 1024 ** 3):
    return bencode({"announce": announce, "info": {"name": "Example.2026.1080p-ZmWeb.mkv", "length": size,
                    "piece length": 1024 ** 2, "pieces": b"a" * 20 * ((size + 1024 ** 2 - 1) // (1024 ** 2)), "private": 1, "source": source}})


def test_info_hash_uses_exact_original_span_and_payload_comparison():
    raw = torrent()
    meta = torrent_metadata(raw)
    info = raw[raw.index(b"4:info") + 6:-1]
    assert meta["hash"] == hashlib.sha1(info).hexdigest()
    target = torrent_metadata(torrent("https://pt.novahd.top/announce.php?passkey=target", "NOVA"))
    assert meta["hash"] != target["hash"]
    assert same_payload(meta, target)
    assert not same_payload(meta, torrent_metadata(torrent(size=1)))


def test_reject_unsafe_or_invalid_torrent():
    for value in (b"<html>login</html>", bencode({"info": {"name": "../escape", "length": 1, "pieces": b"a" * 20, "piece length": 1}}), b"d4:infodeee"):
        with pytest.raises(AdapterError):
            torrent_metadata(value)


def test_listing_without_recognized_counts_does_not_invent_peers():
    html = '<table class="torrents"><tr><th>类型</th><th>名称</th><th>大小</th></tr><tr><td>电影</td><td><a href="details.php?id=123">Example-ZmWeb</a></td><td>2 GiB</td></tr></table>'
    item = parse_listing(html)[0]
    assert item["source_id"] == "123" and item["official"]
    assert item["seeders"] == -1 and not item["permission_verified"]
    with pytest.raises(AdapterError):
        parse_listing('<input type="password"><table class="torrents"></table>')


def test_search_requires_recognized_result_and_identifier_for_work_absence():
    html = ["<html>Unexpected upstream error</html>", "<p>没有找到种子</p>", "<p>没有找到种子</p>"]
    def handler(request):
        return httpx.Response(200, text=html.pop(0))
    tracker = Tracker("https://pt.novahd.top", "secret", httpx.Client(transport=httpx.MockTransport(handler)))
    candidate = {"title": "Example", "metadata": {"imdb": "tt1234"}}
    assert tracker.duplicates(candidate) == (None, None)
    assert tracker.duplicates(candidate) == (False, False)
    assert tracker.duplicates({"title": "Example"}) == (False, None)


def test_release_duplicate_uses_manifest_and_pieces_not_just_title():
    source = torrent()
    renamed_page_title = '<table class="torrents"><tr><td><a href="details.php?id=999">Different title formatting</a></td></tr></table>'
    def handler(request):
        if request.url.path == "/download.php":
            return httpx.Response(200, content=torrent("https://pt.novahd.top/announce.php?passkey=target", "NOVA"))
        return httpx.Response(200, text=renamed_page_title)
    tracker = Tracker("https://pt.novahd.top", "secret", httpx.Client(transport=httpx.MockTransport(handler)))
    item = {"title": "Original title", "metadata": {"imdb": "tt1234", "payload_signature": payload_signature(torrent_metadata(source))}}
    assert tracker.duplicates(item) == (True, True)
    item["metadata"].pop("payload_signature")
    assert tracker.duplicates(item) == (None, True)


def test_external_redirect_never_carries_cookie_to_another_host():
    calls = []
    def handler(request):
        calls.append(request.url.host)
        return httpx.Response(302, headers={"Location": "https://evil.test/"})
    tracker = Tracker("https://zmpt.cc", "private", httpx.Client(transport=httpx.MockTransport(handler)))
    with pytest.raises(AdapterError):
        tracker.get("https://zmpt.cc/torrents.php")
    assert calls == ["zmpt.cc"]
    with pytest.raises(AdapterError):
        tracker.get("https://evil.test/torrent")


def test_image_only_restriction_is_not_ignored():
    html = '<h1 id="top">Example-ZmWeb</h1><table><tr><td>基本信息</td><td>电影 2 GiB <span class="tag"><img alt="禁止转载" title="禁止转载"></span></td></tr></table><div id="kdescr">介绍</div>'
    tracker = Tracker("https://zmpt.cc", "secret", httpx.Client(transport=httpx.MockTransport(lambda _: httpx.Response(200, text=html))))
    result = tracker.inspect({"source_id": "1", "detail_url": "https://zmpt.cc/details.php?id=1", "metadata": {}})
    assert result["permission_verified"] and result["restricted"]


def test_source_detail_gets_exact_manifest_size_and_fingerprint():
    html = '<h1 id="top">Example.2026.1080p-ZmWeb</h1><table><tr><td>基本信息</td><td>类型: 电影 大小: 2.0 GiB</td></tr></table><div id="kdescr">合法简介 <a href="https://imdb.com/title/tt12345">IMDb</a></div><div id="peercount">做种: 12 下载: 6</div><a href="download.php?id=123&amp;passkey=private">下载种子</a>'
    def handler(request):
        return httpx.Response(200, content=torrent()) if request.url.path == '/download.php' else httpx.Response(200, text=html)
    tracker = Tracker("https://zmpt.cc", "secret", httpx.Client(transport=httpx.MockTransport(handler)))
    item = tracker.inspect({"source_id":"123", "detail_url":"https://zmpt.cc/details.php?id=123", "metadata":{}})
    assert item["permission_verified"] and not item["restricted"]
    assert item["size_bytes"] == 2 * 1024 ** 3 and item["seeders"] == 12 and item["leechers"] == 6
    assert item["metadata"]["imdb"] == 'tt12345' and len(item["metadata"]["payload_signature"]) == 64


def test_qb_full_selected_progress_does_not_mean_full_payload():
    meta = torrent_metadata(torrent())
    files = [{"name": meta["files"][0][0], "size": meta["size_bytes"], "progress": 0.5, "priority": 0}]
    qb = QBittorrent("http://qb:8080", "u", "p", httpx.Client(base_url="http://qb:8080", transport=httpx.MockTransport(lambda _: httpx.Response(200, json=files))))
    with pytest.raises(AdapterError, match="未完成或未选择"):
        qb.verify_files(meta["hash"], meta)


@pytest.mark.parametrize("version,start", [("v4.6.7", "resume"), ("v5.1.4", "start")])
def test_qb_login_and_version_appropriate_start(version, start):
    calls = []
    def handler(request):
        calls.append(request.url.path)
        if request.url.path.endswith("auth/login"):
            return httpx.Response(200, text="Ok.")
        if request.url.path.endswith("app/version"):
            return httpx.Response(200, text=version)
        if request.url.path.endswith("app/webapiVersion"):
            return httpx.Response(200, text="2.11.3")
        return httpx.Response(200)
    qb = QBittorrent("http://qb:8080", "user", "secret", httpx.Client(base_url="http://qb:8080", transport=httpx.MockTransport(handler)))
    assert qb.login()["version"] == version
    qb.start("a" * 40)
    assert calls[-1] == "/api/v2/torrents/" + start


def test_identical_info_hash_adds_only_verified_target_tracker():
    raw = torrent("https://pt.novahd.top/announce.php?passkey=target")
    info_hash = torrent_metadata(raw)["hash"]
    writes = []
    def handler(request):
        if request.url.path.endswith("torrents/info"):
            return httpx.Response(200, json=[{"hash": info_hash}])
        if request.url.path.endswith("torrents/trackers"):
            return httpx.Response(200, json=[{"url": "https://zmpt.cc/announce.php?passkey=source"}])
        writes.append((request.url.path, request.content))
        return httpx.Response(200)
    qb = QBittorrent("http://qb:8080", "user", "secret", httpx.Client(base_url="http://qb:8080", transport=httpx.MockTransport(handler)))
    assert qb.add_target(raw, "/media") == info_hash
    assert len(writes) == 1 and writes[0][0].endswith("addTrackers")
    assert b"target" in writes[0][1] and b"source" not in writes[0][1]
    with pytest.raises(AdapterError):
        qb.add_target(torrent("https://evil.test/announce"), "/media")
    assert len(writes) == 1


def test_ptnexus_timeout_is_not_retried_and_upstream_secret_is_not_exposed():
    calls = []
    def handler(request):
        calls.append(request.url.path)
        raise httpx.ReadTimeout("secret-cookie-private-passkey", request=request)
    engine = PTNexus("http://engine:5274", "admin", "private", httpx.Client(base_url="http://engine:5274", transport=httpx.MockTransport(handler)))
    with pytest.raises(AdapterError) as result:
        engine.publish({"task_id": "1"})
    assert len(calls) == 1
    assert "private" not in str(result.value)
