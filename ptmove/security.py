"""Password, session, encryption and redaction primitives; no tracker requests."""
import base64
import hashlib
import hmac
import ipaddress
import json
import os
from pathlib import Path
import re
import secrets
import socket
import time
from urllib.parse import urlsplit

from cryptography.fernet import Fernet

SECRET_SETTINGS = {"source_cookie", "target_cookie", "source_rss_url", "qb_password", "qb_api_key", "ptnexus_password"}
SETTING_KEYS = ("source_cookie", "target_cookie", "source_rss_url", "qb_url", "qb_username", "qb_password", "qb_api_key", "qb_save_path", "media_root", "ptnexus_url", "ptnexus_username", "ptnexus_downloader_id", "ptnexus_password")


class Vault:
    def __init__(self, directory):
        directory = Path(directory)
        directory.mkdir(parents=True, exist_ok=True)
        key_path = directory / "encryption.key"
        try:
            fd = os.open(key_path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        except FileExistsError:
            pass
        else:
            with os.fdopen(fd, "wb") as stream:
                stream.write(Fernet.generate_key())
        self.fernet = Fernet(key_path.read_bytes())

    def encrypt(self, value):
        return self.fernet.encrypt(value.encode()).decode()

    def decrypt(self, value):
        return self.fernet.decrypt(value.encode()).decode()


class Sessions:
    def __init__(self, password, key):
        self.salt = secrets.token_bytes(16)
        self.password_hash = self._hash(password) if password else None
        self.key = key.encode()

    def _hash(self, password):
        return hashlib.pbkdf2_hmac("sha256", password.encode(), self.salt, 310000)

    def verify_password(self, password):
        digest = self._hash(password)
        return self.password_hash is not None and hmac.compare_digest(digest, self.password_hash)

    def create(self):
        payload = {"exp": int(time.time()) + 43200, "csrf": secrets.token_urlsafe(32), "sid": secrets.token_urlsafe(24)}
        encoded = base64.urlsafe_b64encode(json.dumps(payload).encode()).decode()
        return encoded + "." + hmac.new(self.key, encoded.encode(), "sha256").hexdigest(), payload

    def read(self, cookie):
        try:
            encoded, signature = cookie.rsplit(".", 1)
            if not hmac.compare_digest(signature, hmac.new(self.key, encoded.encode(), "sha256").hexdigest()):
                return None
            payload = json.loads(base64.urlsafe_b64decode(encoded))
            return payload if payload["exp"] > time.time() else None
        except (ValueError, KeyError, TypeError):
            return None


def validate_url(value, rss=False):
    if not value:
        return value
    try:
        p = urlsplit(value)
        if p.scheme not in ("http", "https") or not p.hostname or p.username is not None or p.password is not None or p.fragment:
            raise ValueError("URL must be HTTP(S), without user info or fragment")
        _ = p.port
        hostname = p.hostname.lower().rstrip(".")
        try:
            ip = ipaddress.ip_address(hostname)
        except ValueError:
            ip = None
        # inet_aton recognizes legacy integer/octal/hex IP spellings without DNS.
        try:
            legacy_ip = str(ipaddress.IPv4Address(socket.inet_aton(hostname)))
        except OSError:
            legacy_ip = None
        if hostname == "81.70.24.5" or legacy_ip == "81.70.24.5" or (ip and str(ip).removeprefix("::ffff:") == "81.70.24.5"):
            raise ValueError("protected server is forbidden")
        if not rss and p.query:
            raise ValueError("service base URL cannot contain a query")
        if rss and (p.scheme != "https" or hostname != "zmpt.cc" or p.port not in (None, 443)):
            raise ValueError("RSS must use https://zmpt.cc")
        return value
    except (ValueError, OverflowError) as exc:
        raise ValueError(str(exc)) from None


def redact(message, known_secrets=()):
    text = str(message)
    for value in known_secrets:
        if value:
            text = text.replace(str(value), "[REDACTED]")
    text = re.sub(r"https?://[^\s<>\"']+", lambda m: m.group(0).split("?", 1)[0] + ("?[REDACTED]" if "?" in m.group(0) else ""), text)
    text = re.sub(r"(?i)(cookie|password|passkey|token|authorization|secret)(\s*[:=]\s*)[^\s,;]+", r"\1\2[REDACTED]", text)
    return text[:4096]
