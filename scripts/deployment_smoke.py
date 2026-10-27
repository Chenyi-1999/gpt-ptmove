"""Run on 152 to verify isolated controller auth and restart persistence.

No tracker/NAS requests; never enables schedules or creates transfer tasks.
Credentials remain in process memory and are never printed.
"""
import http.cookiejar
import json
from pathlib import Path
import subprocess
import time
import urllib.error
import urllib.request

root = Path(__file__).resolve().parents[1]
env = dict(line.split("=", 1) for line in (root / ".env").read_text().splitlines() if "=" in line and not line.startswith("#"))
base = "http://127.0.0.1:" + env.get("PTMOVE_PORT", "18120")
opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()), urllib.request.ProxyHandler({}))
csrf = ""


def request(path, body=None, method=None, headers=None):
    req = urllib.request.Request(base + path, data=json.dumps(body).encode() if body is not None else None,
                                 method=method, headers={"Content-Type": "application/json", "X-CSRF-Token": csrf, **(headers or {})})
    with opener.open(req, timeout=8) as response:
        return json.load(response)


assert request("/health")["status"] == "ok"
try:
    request("/api/dashboard")
    raise AssertionError("anonymous access allowed")
except urllib.error.HTTPError as exc:
    assert exc.code == 401
csrf = request("/api/auth/login", {"password": env["PTMOVE_ADMIN_PASSWORD"]})["csrf_token"]
before = request("/api/dashboard")
assert before["rule"]["enabled"] is False
original = before["rule"]
temporary = {**original, "monthly_limit": 7}
request("/api/rules", temporary, "PUT")
try:
    subprocess.run(["docker", "compose", "restart", "controller"], cwd=root, check=True)
    deadline = time.monotonic() + 40
    while True:
        try:
            if request("/health")["status"] == "ok":
                break
        except (OSError, ValueError):
            if time.monotonic() >= deadline:
                raise
            time.sleep(1)
    assert request("/api/rules")["monthly_limit"] == 7
    # Session signing survives a restart; secret fields never appear in settings.
    config = request("/api/settings")
    assert not config["source_cookie"] and not config["qb_password"]
    worker = request("/api/worker/config", headers={"Authorization": "Bearer " + env["PTMOVE_WORKER_TOKEN"]})
    assert worker["version"] == "0.1.0"
finally:
    request("/api/rules", original, "PUT")
after = request("/api/dashboard")
assert after["quota"] == before["quota"] and after["counts"] == before["counts"]
assert not after["rule"]["enabled"]
print(json.dumps({"health": "ok", "auth": "passed", "restart_persistence": "passed", "worker_auth": "passed", "quota_unchanged": True, "schedule_enabled": False}))
