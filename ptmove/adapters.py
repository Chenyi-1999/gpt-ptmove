"""Small, independently implemented adapters for NexusPHP, qB and PTNexus.

Tracker HTML is untrusted. Missing markers/counts/search results are unknown,
never evidence that a resource is safe or absent. No tracker POST is retried.
"""
from __future__ import annotations

import hashlib
import ipaddress
import json
import re
import socket
import time
from datetime import datetime, timezone, timedelta
from typing import Any
from urllib.parse import parse_qs, urlencode, urljoin, urlsplit

import feedparser
import httpx
from bs4 import BeautifulSoup

SOURCE = "https://zmpt.cc"
TARGET = "https://pt.novahd.top"
MAX_HTML = 8 * 1024 * 1024
MAX_TORRENT = 16 * 1024 * 1024
RESTRICTED = re.compile(r"禁[止]?转|限[制]?转|禁止转载|谢绝转载|分集|禁止分流|不可转载|no[ -]?(?:repost|transfer)", re.I)
OFFICIAL_GROUPS = {"zmweb", "zmaudio", "zmpt"}


class AdapterError(RuntimeError):
    """Deliberately contains a public-safe message, never upstream response text."""


def safe_url(url: str, host: str | None = None) -> str:
    p = urlsplit(str(url))
    if p.scheme not in {"http", "https"} or not p.hostname or p.username or p.password or p.fragment:
        raise AdapterError("连接地址格式不正确")
    try:
        addr = ipaddress.ip_address(p.hostname)
        normalized = str(addr.ipv4_mapped or addr) if isinstance(addr, ipaddress.IPv6Address) else str(addr)
    except ValueError:
        try:
            normalized = socket.inet_ntoa(socket.inet_aton(p.hostname))
        except OSError:
            normalized = p.hostname
    if normalized == "81.70.24.5":
        raise AdapterError("受保护的服务器不允许作为连接目标")
    if host and (p.scheme != "https" or p.hostname.lower() != host or p.port not in (None, 443)):
        raise AdapterError("种子链接不属于指定站点")
    return url


def resolve_safe(url: str):
    """Block aliases/alternate IP spellings before sending any request."""
    safe_url(url)
    p = urlsplit(url)
    try:
        for _, _, _, _, addr in socket.getaddrinfo(p.hostname, p.port or (443 if p.scheme == "https" else 80), type=socket.SOCK_STREAM):
            safe_url("http://[" + addr[0] + "]" if ":" in addr[0] else "http://" + addr[0])
    except OSError as exc:
        raise AdapterError("连接地址无法解析") from exc


def size_bytes(text: str) -> int:
    m = re.search(r"([\d,.]+)\s*([KMGTPE]?i?B)\b", text, re.I)
    if not m:
        return 0
    unit = m[2].upper().replace("I", "")
    return int(float(m[1].replace(",", "")) * (1024 ** ["B", "KB", "MB", "GB", "TB", "PB", "EB"].index(unit)))


def torrent_metadata(raw: bytes) -> dict:
    """Hash the *original* bencoded info span, not a lossy re-serialization."""
    if not raw or len(raw) > MAX_TORRENT:
        raise AdapterError("种子文件为空或过大")
    info_span: list[tuple[int, int]] = []

    def read(pos: int, depth: int = 0) -> tuple[Any, int]:
        if depth > 40 or pos >= len(raw):
            raise ValueError("invalid bencoding")
        mark = raw[pos:pos + 1]
        if mark == b"i":
            end = raw.index(b"e", pos)
            return int(raw[pos + 1:end]), end + 1
        if mark in (b"l", b"d"):
            pos += 1
            result: Any = [] if mark == b"l" else {}
            while raw[pos:pos + 1] != b"e":
                if pos >= len(raw):
                    raise ValueError("unterminated container")
                key, pos = read(pos, depth + 1)
                if mark == b"l":
                    result.append(key)
                else:
                    if not isinstance(key, bytes) or key in result:
                        raise ValueError("invalid dictionary key")
                    start = pos
                    val, pos = read(pos, depth + 1)
                    result[key] = val
                    if depth == 0 and key == b"info":
                        info_span.append((start, pos))
            return result, pos + 1
        colon = raw.index(b":", pos)
        length = int(raw[pos:colon])
        end = colon + 1 + length
        if length < 0 or end > len(raw):
            raise ValueError("invalid string length")
        return raw[colon + 1:end], end

    try:
        obj, end = read(0)
        info = obj[b"info"]
        if end != len(raw) or len(info_span) != 1 or not isinstance(info, dict):
            raise ValueError("missing info")
        name = info[b"name"].decode("utf-8", "replace")
        files = []
        for f in info.get(b"files", [{b"path": [], b"length": info.get(b"length", 0)}]):
            path = [name] + [p.decode("utf-8", "replace") for p in f[b"path"]]
            if any(p in {".", "..", ""} or "/" in p or "\\" in p or "\x00" in p for p in path):
                raise ValueError("unsafe path")
            length = int(f[b"length"])
            if length < 0:
                raise ValueError("invalid length")
            files.append(("/".join(path), length))
        total = sum(n for _, n in files)
        pieces = info.get(b"pieces", b"")
        piece_length = int(info.get(b"piece length", 0))
        if total <= 0 or not pieces or len(pieces) % 20 or piece_length <= 0 or len(pieces) // 20 != (total + piece_length - 1) // piece_length:
            raise ValueError("unsupported v2-only/invalid torrent")
        start, stop = info_span[0]
        return {"hash": hashlib.sha1(raw[start:stop]).hexdigest(), "name": name,
                "size_bytes": total, "files": sorted(files), "pieces": pieces,
                "piece_length": piece_length, "private": bool(info.get(b"private")),
                "announce": obj.get(b"announce", b"").decode("utf-8", "replace")}
    except (KeyError, IndexError, TypeError, ValueError, OverflowError) as exc:
        raise AdapterError("种子文件无效或暂不支持纯 v2 格式") from exc


def same_payload(source: dict, target: dict) -> bool:
    return all(source.get(k) == target.get(k) for k in ("files", "pieces", "piece_length", "size_bytes"))


def payload_signature(metadata: dict) -> str:
    value = {"files": metadata["files"], "size_bytes": metadata["size_bytes"],
             "piece_length": metadata["piece_length"], "pieces_sha256": hashlib.sha256(metadata["pieces"]).hexdigest()}
    return hashlib.sha256(json.dumps(value, sort_keys=True, ensure_ascii=False).encode()).hexdigest()


def authenticated_html(html: str) -> BeautifulSoup:
    soup = BeautifulSoup(html, "html.parser")
    if soup.select_one('input[type="password"]') or re.search(r"cf-chl-|just a moment|验证您是真人", html, re.I):
        raise AdapterError("站点登录失效或需要人工验证")
    return soup


def _date(text: str) -> str | None:
    m = re.search(r"(20\d\d[-/]\d\d[-/]\d\d)[ T](\d\d:\d\d(?::\d\d)?)", text)
    if m:
        try:
            return datetime.fromisoformat(m[1].replace("/", "-") + "T" + m[2]).replace(tzinfo=timezone(timedelta(hours=8))).isoformat()
        except ValueError:
            pass
    return None


def _count(cell: Any) -> int:
    text = cell.get_text(" ", strip=True).replace(",", "") if cell else ""
    return int(text) if text.isdigit() else -1


def parse_listing(html: str, base: str = SOURCE, limit: int = 50) -> list[dict]:
    soup = authenticated_html(html)
    table = soup.select_one("table.torrents") or soup.select_one("table#torrenttable")
    if not table:
        raise AdapterError("无法识别种子列表，已停止选种")
    headings = table.find("tr")
    columns = []
    for cell in headings.find_all(["th", "td"], recursive=False) if headings else []:
        columns.append(" ".join([cell.get_text(" ", strip=True)] + [x.get("alt", "") + x.get("title", "") for x in cell.find_all("img")]).lower())
    def index(*labels):
        return next((i for i, label in enumerate(columns) if any(v in label for v in labels)), None)
    si, li, zi = index("seeders", "做种", "种子数"), index("leechers", "下载数", "下载者"), index("size", "大小", "体积")
    items, seen = [], set()
    for row in table.find_all("tr"):
        link = row.select_one('a[href*="details.php?id="]')
        if not link:
            continue
        url = urljoin(base, link.get("href", ""))
        try:
            safe_url(url, urlsplit(base).hostname)
            sid = parse_qs(urlsplit(url).query).get("id", [""])[0]
            if not sid.isdigit() or sid in seen:
                continue
            seen.add(sid)
            cells = row.find_all("td", recursive=False)
            text = row.get_text(" ", strip=True)
            title = link.get("title") or link.get_text(" ", strip=True)
            group = title.rsplit("-", 1)[-1].strip()
            size = size_bytes(cells[zi].get_text(" ", strip=True)) if zi is not None and zi < len(cells) else size_bytes(text)
            cat_cell = cells[0] if cells else row
            cat = " ".join([cat_cell.get_text(" ", strip=True)] + [x.get("alt", "") + x.get("title", "") for x in cat_cell.find_all("img")])
            category = "movie" if re.search(r"电影|movie", cat, re.I) else ("tv" if re.search(r"电视剧|tv series", cat, re.I) else "other")
            items.append({"source_id": sid, "title": title, "size_bytes": size,
                          "seeders": _count(cells[si]) if si is not None and si < len(cells) else -1,
                          "leechers": _count(cells[li]) if li is not None and li < len(cells) else -1,
                          "official": group.lower() in OFFICIAL_GROUPS or bool(re.search("官种|官方", text)),
                          "release_group": group, "category": category, "detail_url": f"{base}/details.php?id={sid}",
                          "download_url": f"{base}/download.php?id={sid}", "published_at": _date(text),
                          "restricted": bool(RESTRICTED.search(text)), "permission_verified": False,
                          "target_exists": None, "target_work_exists": None, "metadata": {}})
        except (AdapterError, ValueError):
            continue
        if len(items) >= limit:
            break
    return items


class Tracker:
    def __init__(self, base: str, cookie: str, client: httpx.Client | None = None):
        self.base = base.rstrip("/")
        self.host = urlsplit(base).hostname
        self.resolve = client is None
        self.last_request = 0.0
        self.client = client or httpx.Client(timeout=45, follow_redirects=False, trust_env=False, transport=httpx.HTTPTransport(retries=2, trust_env=False),
                                             headers={"User-Agent": "PTMove/0.1 (personal tracker migration)", "Cookie": cookie})

    def close(self):
        self.client.close()

    def get(self, url: str, limit: int = MAX_HTML) -> bytes:
        safe_url(url, self.host)
        if self.resolve:
            resolve_safe(url)
            delay = 2 - (time.monotonic() - self.last_request)
            if delay > 0:
                time.sleep(delay)
            self.last_request = time.monotonic()
        try:
            with self.client.stream("GET", url) as r:
                if r.is_redirect:
                    raise AdapterError("站点跳转需要重新确认登录或域名")
                r.raise_for_status()
                buf = bytearray()
                for part in r.iter_bytes():
                    buf.extend(part)
                    if len(buf) > limit:
                        raise AdapterError("站点响应超出大小限制")
                return bytes(buf)
        except httpx.HTTPError as exc:
            raise AdapterError("站点请求失败，请检查登录和网络") from exc

    def candidates(self, rss_url: str, limit: int) -> list[dict]:
        if not rss_url:
            return parse_listing(self.get(self.base + "/torrents.php?sort=4&type=desc").decode("utf-8", "replace"), self.base, limit)
        feed = feedparser.parse(self.get(rss_url))
        if feed.bozo or not feed.entries:
            raise AdapterError("RSS 无法解析或没有条目")
        out = []
        for entry in feed.entries[:limit]:
            urls = [entry.get("link", "")] + [v.get("href", "") for v in entry.get("links", [])]
            sid = next((parse_qs(urlsplit(u).query).get("id", [""])[0] for u in urls if parse_qs(urlsplit(u).query).get("id")), "")
            if not sid.isdigit():
                continue
            # RSS supplies IDs; authoritative classification/counts come from fresh detail HTML.
            parsed = entry.get("published_parsed")
            date = datetime(*parsed[:6], tzinfo=timezone.utc).isoformat() if parsed else None
            out.append({"source_id": sid, "title": entry.get("title", ""), "size_bytes": 0,
                        "seeders": -1, "leechers": -1, "official": False, "category": "other",
                        "detail_url": f"{self.base}/details.php?id={sid}", "download_url": f"{self.base}/download.php?id={sid}",
                        "published_at": date, "restricted": False, "permission_verified": False,
                        "target_exists": None, "target_work_exists": None, "metadata": {}})
        return out

    def inspect(self, candidate: dict) -> dict:
        out = dict(candidate)
        html = self.get(out["detail_url"]).decode("utf-8", "replace")
        soup = authenticated_html(html)
        title = soup.select_one("h1#top") or soup.select_one("h1")
        description = soup.select_one("#kdescr") or soup.select_one("#description")
        # Restriction verification needs both detail identity and a recognizable metadata table.
        if not title or not description:
            raise AdapterError("详情页结构无法确认，已跳过该资源")
        out["title"] = title.get_text(" ", strip=True)
        group = out["title"].rsplit("-", 1)[-1].strip()
        out["release_group"] = group
        rows = soup.find_all("tr")
        labels = {}
        for row in rows:
            cells = row.find_all(["td", "th"], recursive=False)
            if len(cells) == 2:
                labels[cells[0].get_text(" ", strip=True).rstrip(":：")] = cells[1].get_text(" ", strip=True)
        metadata_text = " ".join(f"{k}: {v}" for k, v in labels.items() if k in {"基本信息", "种子信息", "标签", "类型", "副标题", "大小", "体积", "文件大小", "种子名称"})
        def evidence(node):
            return " ".join([node.get_text(" ", strip=True)] + [str(x.get(k, "")) for x in [node, *node.find_all(True)] for k in ("alt", "title", "data-tag")])
        tagged = " ".join(evidence(x) for x in soup.select(".tag, .tags, .torrent-tag, .label"))
        # Some NexusPHP styles render forbidden labels as bare images in detail rows.
        metadata_text += " " + " ".join(evidence(row) for row in rows if row.find(["td", "th"]) and re.search("基本信息|种子信息|标签|副标题|种子名称", row.find(["td", "th"]).get_text(" ", strip=True)))
        check_text = " ".join([out["title"], metadata_text, tagged, description.get_text(" ", strip=True)])
        out["restricted"] = bool(RESTRICTED.search(check_text))
        out["restriction_reason"] = "详情页含禁转、限转或分集标记" if out["restricted"] else ""
        out["permission_verified"] = bool(metadata_text)  # parser failure never grants permission
        out["official"] = group.lower() in OFFICIAL_GROUPS or bool(re.search("官种|官方", tagged))
        out["size_bytes"] = size_bytes(metadata_text) or out.get("size_bytes", 0)
        for field, pattern in (("seeders", r"(?:做种者?|seeders?)\D{0,6}(\d+)"), ("leechers", r"(?:下载者?|leechers?)\D{0,6}(\d+)")):
            peer_text = " ".join(x.get_text(" ", strip=True) for x in soup.select("#peercount, #peers, .peers")) or soup.get_text(" ", strip=True)
            match = re.search(pattern, peer_text, re.I)
            if match:
                out[field] = int(match[1])
        cat = labels.get("类型", "") + labels.get("基本信息", "")
        if re.search(r"电影|movie", cat, re.I):
            out["category"] = "movie"
        elif re.search(r"电视剧|tv series", cat, re.I):
            out["category"] = "tv"
        out["published_at"] = out.get("published_at") or _date(" ".join(labels.values()))
        imdb = re.search(r"imdb\.com/title/(tt\d+)", html)
        douban = re.search(r"douban\.com/subject/(\d+)", html)
        out["metadata"] = {"imdb": imdb[1] if imdb else "", "douban": douban[1] if douban else ""}
        # Authorized download link is fetched fresh and kept on the worker only; no passkey in UI.
        links = soup.select('a[href*="download.php"]')
        for a in links:
            u = urljoin(self.base, a.get("href", ""))
            if parse_qs(urlsplit(u).query).get("id", [""])[0] == str(out["source_id"]):
                safe_url(u, self.host)
                out["download_url"] = u
                break
        if out["permission_verified"] and not out["restricted"]:
            metadata = torrent_metadata(self.torrent(out))
            out["size_bytes"] = metadata["size_bytes"]
            out["info_hash"] = metadata["hash"]
            out["metadata"]["payload_signature"] = payload_signature(metadata)
        return out

    def duplicates(self, candidate: dict) -> tuple[bool | None, bool | None]:
        # Search by work identifier when available, then compare exact normalized release.
        metadata = candidate.get("metadata", {})
        work_key = metadata.get("imdb") or metadata.get("douban")
        query = work_key or candidate["title"]
        html = self.get(self.base + "/torrents.php?" + urlencode({"search": query, "search_area": "4" if metadata.get("imdb") else "0", "search_mode": "0"})).decode("utf-8", "replace")
        soup = authenticated_html(html)
        table = soup.select_one("table.torrents") or soup.select_one("table#torrenttable")
        no_results = bool(re.search(r"没有种子|没有找到|未找到|无匹配|no torrents|nothing found|no results", soup.get_text(" ", strip=True), re.I))
        if not table and not no_results:
            return None, None
        links = table.select('a[href*="details.php?id="]') if table else []
        def release(t):
            return re.sub(r"[^\w]", "", t.lower())
        same = any(release(a.get("title") or a.get_text(" ", strip=True)) == release(candidate["title"]) for a in links)
        if same:
            return True, True
        if not links:
            # A title-only empty search proves release absence but not work absence.
            return False, False if work_key else None
        # Different title spelling is not proof that this release is absent.
        # Compare authenticated torrent manifests and piece hashes for a bounded
        # set of versions. Pagination/large results remain unknown, never absent.
        signature = metadata.get("payload_signature")
        if not work_key or not signature or len(links) > 5 or soup.select('a[href*="page="]'):
            return None, True
        checked = set()
        for a in links:
            url = urljoin(self.base, a.get("href", ""))
            safe_url(url, self.host)
            if url in checked:
                continue
            checked.add(url)
            try:
                existing = torrent_metadata(self.torrent(url))
            except AdapterError:
                return None, True
            if payload_signature(existing) == signature:
                return True, True
        return False, True

    def torrent(self, candidate_or_url: dict | str) -> bytes:
        if isinstance(candidate_or_url, dict):
            url = candidate_or_url["download_url"]
        else:
            p = urlsplit(candidate_or_url)
            safe_url(candidate_or_url, self.host)
            sid = parse_qs(p.query).get("id", [""])[0]
            if not sid.isdigit():
                raise AdapterError("发布详情链接缺少有效种子编号")
            url = self.base + "/download.php?" + urlencode({"id": sid})
        raw = self.get(url, MAX_TORRENT)
        torrent_metadata(raw)
        return raw


class QBittorrent:
    def __init__(self, url: str, username: str, password: str, client: httpx.Client | None = None, api_key: str | None = None):
        self.url = safe_url(url).rstrip("/")
        self.username, self.password = username, password
        self.key_mode = bool(api_key)
        if self.key_mode:
            if not re.fullmatch(r"qbt_[A-Za-z0-9]{28}", api_key):
                raise AdapterError("qB API Token 格式无效")
        self.resolve = client is None
        self.client = client or httpx.Client(base_url=self.url, timeout=45, follow_redirects=False, trust_env=False, transport=httpx.HTTPTransport(retries=2, trust_env=False),
                                             headers={"Referer": self.url + "/", "Origin": self.url})
        if self.key_mode:
            self.client.headers["Authorization"] = "Bearer " + api_key
        self.major = 4

    def close(self):
        self.client.close()

    def request(self, method: str, path: str, **kwargs) -> httpx.Response:
        if self.resolve:
            resolve_safe(self.url)
        failed = False
        try:
            r = self.client.request(method, "/api/v2/" + path, **kwargs)
            r.raise_for_status()
            if r.is_redirect:
                raise AdapterError("qB 地址发生跳转")
            return r
        except httpx.HTTPError:
            # Do not retain an HTTP exception whose attached Request can
            # contain the Authorization header carrying the API key.
            failed = True
        if failed:
            raise AdapterError("qB API 请求失败，请检查连接和权限")

    def login(self) -> dict:
        if not self.key_mode:
            r = self.request("POST", "auth/login", data={"username": self.username, "password": self.password})
            if r.text.strip() != "Ok.":
                raise AdapterError("qB 登录失败")
        version = self.request("GET", "app/version").text.strip()
        api = self.request("GET", "app/webapiVersion").text.strip()
        version_match = re.match(r"v?(\d+)\.(\d+)(?:\.(\d+))?", version)
        version_numbers = tuple(int(part or 0) for part in version_match.groups()) if version_match else ()
        version_numbers = version_numbers + (0,) * (3 - len(version_numbers))
        api_match = re.match(r"(\d+)\.(\d+)\.(\d+)", api)
        if self.key_mode:
            if (not version_match or version_numbers < (5, 2, 0)
                    or not api_match or tuple(map(int, api_match.groups())) < (2, 14, 1)):
                raise AdapterError("qB API Token 需要 qBittorrent 5.2.0+ 和 Web API 2.14.1+")
        elif (not version_match or version_numbers[:2] < (4, 1)
              or not api.startswith("2.")):
            raise AdapterError("需要 qBittorrent 4.1+ 和 Web API v2")
        self.major = int(version_match[1])
        return {"version": version, "api_version": api, "auth_method": "api_key" if self.key_mode else "password"}

    def info(self, info_hash: str) -> dict | None:
        items = self.request("GET", "torrents/info", params={"hashes": info_hash}).json()
        return next((v for v in items if v.get("hash", "").lower() == info_hash.lower()), None)

    def add(self, raw: bytes, save_path: str, paused: bool = False):
        metadata = torrent_metadata(raw)
        if self.info(metadata["hash"]):
            return metadata["hash"]
        result = self.request("POST", "torrents/add", files={"torrents": ("migration.torrent", raw, "application/x-bittorrent")},
                              data={"savepath": save_path, "autoTMM": "false", "paused": str(paused).lower(),
                                    "stopped": str(paused).lower(), "skip_checking": "false"})
        if result.text.strip() not in {"Ok.", ""}:
            raise AdapterError("qB 未接受种子文件")
        return metadata["hash"]

    def add_target(self, raw: bytes, save_path: str) -> str:
        """Cross-seed an identical info hash without losing the source tracker.

        Only the authenticated target torrent's verified announce is added; no
        user-provided tracker list or unrelated torrent is modified.
        """
        metadata = torrent_metadata(raw)
        safe_url(metadata["announce"], "pt.novahd.top")
        if self.info(metadata["hash"]):
            trackers = self.trackers(metadata["hash"])
            if not any(t.get("url") == metadata["announce"] for t in trackers):
                self.request("POST", "torrents/addTrackers", data={"hash": metadata["hash"], "urls": metadata["announce"]})
            return metadata["hash"]
        return self.add(raw, save_path, paused=True)

    def recheck(self, info_hash: str):
        self.request("POST", "torrents/recheck", data={"hashes": info_hash})

    def start(self, info_hash: str):
        self.request("POST", "torrents/start" if self.major >= 5 else "torrents/resume", data={"hashes": info_hash})

    def trackers(self, info_hash: str) -> list[dict]:
        return self.request("GET", "torrents/trackers", params={"hash": info_hash}).json()

    def verify_files(self, info_hash: str, metadata: dict):
        files = self.request("GET", "torrents/files", params={"hash": info_hash}).json()
        actual = sorted((f.get("name", ""), int(f.get("size", -1))) for f in files)
        if actual != metadata["files"] or any(float(f.get("progress", 0)) < 1 for f in files):
            raise AdapterError("源资源存在未完成或未选择的文件，不能发布")


class PTNexus:
    def __init__(self, url: str, username: str, password: str, client: httpx.Client | None = None):
        self.url = safe_url(url).rstrip("/")
        self.username, self.password = username or "admin", password
        self.resolve = client is None
        self.client = client or httpx.Client(base_url=self.url, timeout=300, follow_redirects=False, trust_env=False, transport=httpx.HTTPTransport(retries=2, trust_env=False))

    def close(self):
        self.client.close()

    def call(self, method: str, path: str, **kwargs) -> dict:
        if self.resolve:
            resolve_safe(self.url)
        try:
            r = self.client.request(method, path, **kwargs)
            r.raise_for_status()
            result = r.json()
            if not isinstance(result, dict) or result.get("success") is False:
                raise AdapterError("PTNexus 拒绝操作，请在其界面检查任务和站点配置")
            return result
        except (httpx.HTTPError, ValueError) as exc:
            raise AdapterError("PTNexus 请求失败或响应格式不兼容") from exc

    def login(self):
        result = self.call("POST", "/api/auth/login", json={"username": self.username, "password": self.password})
        if not result.get("token"):
            raise AdapterError("PTNexus 未返回访问令牌")
        self.client.headers["Authorization"] = "Bearer " + result["token"]

    def prepare(self, candidate: dict, save_path: str, downloader_id: str) -> dict:
        if not downloader_id:
            raise AdapterError("请配置 PTNexus 中 NAS 下载器的 ID")
        self.call("POST", "/api/migrate/fetch_and_store", json={
            "sourceSite": "zmpt", "searchTerm": str(candidate["source_id"]),
            "torrentName": candidate["title"], "savePath": save_path,
            "downloaderId": downloader_id, "screenshotReviewMode": "background"})
        result = self.call("GET", "/api/migrate/get_db_seed_info", params={"torrent_id": candidate["source_id"], "site_name": "zmpt"})
        data = result.get("data")
        if not isinstance(data, dict) or not result.get("task_id"):
            raise AdapterError("PTNexus 发布参数尚未准备完成")
        review = str(data.get("screenshot_review_status", "")).lower()
        if review in {"pending", "pending_review", "waiting_review", "rejected"}:
            raise AdapterError("截图需要人工确认，请先在 PTNexus 完成审核")
        tag_text = str(data.get("tags", "")) + str(data.get("standardized_params", {}).get("tags", ""))
        if RESTRICTED.search(tag_text):
            raise AdapterError("PTNexus 检测到不可转种标签")
        return {"task_id": result["task_id"], "upload_data": data, "targetSite": "novahd",
                "sourceSite": "zmpt", "savePath": save_path, "downloaderId": downloader_id,
                "auto_add_to_downloader": False, "auto_update_existing_torrent": False,
                "auto_add_existing_to_downloader": False, "publish_scene": "multi_torrent"}

    def publish(self, payload: dict) -> str:
        # Exactly one external POST. A timeout must be reconciled by the operator.
        result = self.call("POST", "/api/migrate/publish", json=payload)
        if result.get("is_existing_torrent"):
            raise AdapterError("发布时发现目标站已有该资源，请人工核实")
        url = result.get("url", "")
        safe_url(url, "pt.novahd.top")
        if not parse_qs(urlsplit(url).query).get("id", [""])[0].isdigit():
            raise AdapterError("PTNexus 未返回可确认的目标种子详情链接")
        return url
