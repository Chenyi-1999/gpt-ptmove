"""Offline declarative NexusPHP profiles; no credentials or executable code.

Runtime clients must also validate resolved DNS addresses before sending requests.
Profiles are copied into durable runs/tasks so edits cannot redirect old work.
"""
from copy import deepcopy
import hashlib
import ipaddress
import json
import re
import socket
from string import Formatter
from typing import Literal
from urllib.parse import parse_qsl, quote, unquote, urlsplit

from pydantic import BaseModel, ConfigDict, Field, field_validator, model_validator
import soupsieve

SLUG = r"^[a-z][a-z0-9_-]{0,63}$"
SECRET_QUERY = re.compile(r"passkey|token|password|cookie|secret|auth|key", re.I)


def public_hostname(value):
    """Reject literal/alternate IP spellings and local names without DNS I/O."""
    if not isinstance(value, str) or not value or value != value.lower() or value.endswith("."):
        raise ValueError("host must be a lowercase public DNS name")
    try:
        ip = ipaddress.ip_address(value)
    except ValueError:
        try:
            ip = ipaddress.IPv4Address(socket.inet_aton(value))
        except OSError:
            ip = None
    if ip is not None:
        raise ValueError("literal IP addresses are forbidden")
    if len(value) > 253 or "." not in value or value.endswith((".localhost", ".local", ".internal", ".lan", ".home", ".invalid")):
        raise ValueError("host must be a public DNS name")
    if any(not re.fullmatch(r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?", label) for label in value.split(".")):
        raise ValueError("invalid DNS hostname")
    return value


class ProfilePaths(BaseModel):
    model_config = ConfigDict(extra="forbid", strict=True)
    listing: str = "/torrents.php"
    detail: str = "/details.php?id={id}"
    download: str = "/download.php?id={id}"
    search: str = "/torrents.php?search={query}"

    @model_validator(mode="after")
    def safe_templates(self):
        for key, value in self.model_dump().items():
            if len(value) > 1024 or not value.startswith("/") or value.startswith("//") or "\\" in value or any(ord(c) < 32 for c in value):
                raise ValueError("paths must be bounded relative paths starting with /")
            try:
                parts = list(Formatter().parse(value))
            except ValueError:
                raise ValueError("invalid path template") from None
            fields = [name for _, name, _, _ in parts if name is not None]
            allowed = {"id"} if key in ("detail", "download") else {"query"} if key == "search" else set()
            if set(fields) != allowed or len(fields) != len(allowed) or any(spec or conversion for _, name, spec, conversion in parts if name is not None):
                raise ValueError("detail/download require {id}, search requires {query}; no formatting")
            parsed = urlsplit(value)
            decoded = unquote(parsed.path)
            if parsed.scheme or parsed.netloc or parsed.fragment or ".." in decoded.split("/") or "\\" in decoded or decoded.startswith("//"):
                raise ValueError("unsafe relative path")
            if any(SECRET_QUERY.search(k) for k, _ in parse_qsl(parsed.query)):
                raise ValueError("credentials are forbidden in profiles")
        return self


class ProfileSelectors(BaseModel):
    model_config = ConfigDict(extra="forbid", strict=True)
    table: str = "table.torrents, table#torrenttable"
    detail_link: str = 'a[href*="details.php?id="]'
    title: str = "h1, #top, .torrent-title"
    description: str = "#kdescr, #description, .description"
    download_link: str = 'a[href*="download.php"]'
    tags: str = ".tag, .tags, .torrent-tag, .label"
    peers: str = "#peercount, #peers, .peers"

    @field_validator("*", mode="after")
    @classmethod
    def css(cls, value):
        if not value or len(value) > 512 or any(ord(c) < 32 for c in value) or value.count(",") > 15 or value.count(":has(") > 2:
            raise ValueError("CSS selector exceeds limits")
        try:
            soupsieve.compile(value)
        except Exception:
            raise ValueError("invalid CSS selector") from None
        return value


class SiteProfile(BaseModel):
    model_config = ConfigDict(extra="forbid", strict=True)
    schema_version: Literal[1] = 1
    id: str = Field(pattern=SLUG)
    name: str = Field(min_length=1, max_length=100)
    base_url: str = Field(max_length=512)
    roles: list[Literal["source", "target"]] = Field(min_length=1, max_length=2)
    engine: Literal["nexusphp"] = "nexusphp"
    ptnexus_site: str | None = Field(None, min_length=1, max_length=64, pattern=SLUG)
    official_groups: list[str] = Field(default_factory=list, max_length=64)
    announce_hosts: list[str] = Field(default_factory=list, max_length=32)
    paths: ProfilePaths = Field(default_factory=ProfilePaths)
    selectors: ProfileSelectors = Field(default_factory=ProfileSelectors)

    @field_validator("schema_version", mode="before")
    @classmethod
    def exact_version(cls, value):
        if type(value) is not int or value != 1:
            raise ValueError("schema_version must be integer 1")
        return value

    @field_validator("base_url")
    @classmethod
    def origin(cls, value):
        try:
            p = urlsplit(value)
            if p.scheme != "https" or not p.hostname or p.username is not None or p.password is not None or p.port not in (None, 443) or p.path not in ("", "/") or p.query or p.fragment:
                raise ValueError("base_url must be an HTTPS origin")
            public_hostname(p.hostname)
            if p.netloc not in (p.hostname, p.hostname + ":443"):
                raise ValueError("invalid origin")
            return "https://" + p.hostname
        except ValueError:
            raise ValueError("base_url must be a public HTTPS origin") from None

    @field_validator("name")
    @classmethod
    def clean_name(cls, value):
        if not value.strip() or any(ord(c) < 32 for c in value):
            raise ValueError("invalid site name")
        return value

    @field_validator("roles")
    @classmethod
    def distinct_roles(cls, value):
        if len(set(value)) != len(value):
            raise ValueError("duplicate roles")
        return value

    @field_validator("announce_hosts")
    @classmethod
    def hosts(cls, value):
        return list(dict.fromkeys(public_hostname(v) for v in value))

    @field_validator("official_groups")
    @classmethod
    def groups(cls, value):
        if any(not v or len(v) > 80 or any(ord(c) < 32 for c in v) for v in value):
            raise ValueError("invalid official group")
        return list(dict.fromkeys(v.lower() for v in value))


BUILTIN_PROFILES = {
    "zmpt": SiteProfile(id="zmpt", name="织梦", base_url="https://zmpt.cc", roles=["source", "target"], ptnexus_site="zmpt", official_groups=["zmweb", "zmaudio", "zmpt"], announce_hosts=["zmpt.cc"]).model_dump(),
    "novahd": SiteProfile(id="novahd", name="NovaHD", base_url="https://pt.novahd.top", roles=["source", "target"], ptnexus_site="novahd", announce_hosts=["pt.novahd.top"]).model_dump(),
}


def builtin_profiles():
    return deepcopy(list(BUILTIN_PROFILES.values()))


def _profile(value):
    return value.model_dump() if isinstance(value, SiteProfile) else value


def profile_digest(profile):
    return hashlib.sha256(json.dumps(_profile(profile), sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode()).hexdigest()


def route_digest(route):
    return profile_digest(route)


def profile_url(profile, key, **variables):
    p = _profile(profile)
    template = p["paths"][key]
    fields = {name for _, name, _, _ in Formatter().parse(template) if name is not None}
    if set(variables) != fields:
        raise ValueError("incorrect path template variables")
    if "id" in variables and not re.fullmatch(r"[0-9]+", str(variables["id"])):
        raise ValueError("torrent id must be numeric")
    return p["base_url"] + template.format(**{k: quote(str(v), safe="") for k, v in variables.items()})


def profile_for_base(base):
    normalized = str(base).rstrip("/")
    return next((deepcopy(p) for p in BUILTIN_PROFILES.values() if p["base_url"] == normalized), None)


def same_origin(url, profile):
    try:
        p, origin = urlsplit(str(url)), urlsplit(_profile(profile)["base_url"])
        return p.scheme == "https" and p.hostname == origin.hostname and p.port in (None, 443) and p.username is None and p.password is None and not p.fragment and not any(ord(c) < 32 for c in str(url)) and "\\" not in str(url)
    except (ValueError, TypeError, KeyError):
        return False


def validate_rss(value, profile):
    if value and (not isinstance(value, str) or len(value) > 4096 or not same_origin(value, profile)):
        raise ValueError("RSS URL must use the site's HTTPS origin")
    return value


def published_url_valid(value, profile):
    if not isinstance(value, str) or len(value) > 4096 or not same_origin(value, profile):
        return False
    template = _profile(profile)["paths"]["detail"]
    pattern = re.escape(_profile(profile)["base_url"] + template).replace(re.escape("{id}"), r"[0-9]+")
    return re.fullmatch(pattern, value) is not None
