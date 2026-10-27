"""Project-scoped deployment helper. Run only on 152 in /opt/gpt-ptmove-nexus.

Reads the existing PTMove administrator password only in server memory and passes
it to the initialization container through stdin. Never prints configuration secrets.
"""
import argparse
import json
import os
from pathlib import Path
import secrets
import subprocess
import time
import urllib.request


def command(args, **kwargs):
    return subprocess.run(args, check=True, **kwargs)


def compose(root, kind, *args):
    env_file = ".env.canary" if kind == "canary" else ".env.ptmove"
    project = "ptmove-nexus-canary" if kind == "canary" else "ptmove-nexus"
    command(["docker", "compose", "--project-name", project, "--env-file", env_file,
             "-f", "compose.ptmove.yaml", *args], cwd=root)


def prepare(root, kind):
    canary = kind == "canary"
    data = root / ("canary-data" if canary else "data")
    data.mkdir(mode=0o700, exist_ok=True)
    data.chmod(0o700)
    env_file = root / (".env.canary" if canary else ".env.ptmove")
    if not env_file.exists():
        content = "\n".join([
            "JWT_SECRET=" + secrets.token_urlsafe(48),
            "PTMOVE_ENV_FILE=" + env_file.name,
            "PTMOVE_DATA_DIR=./" + data.name,
            "PTMOVE_BIND=0.0.0.0",
            "PTMOVE_PORT=" + ("18121" if canary else "18120"), "",
        ])
        with os.fdopen(os.open(env_file, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600), "w") as stream:
            stream.write(content)
    env_file.chmod(0o600)
    config_file = data / "config.json"
    initialized = config_file.exists() and bool(json.loads(config_file.read_text()).get("auth", {}).get("password_hash"))
    if not initialized:
        old_env = Path("/opt/gpt-ptmove/.env")
        values = dict(line.split("=", 1) for line in old_env.read_text().splitlines() if "=" in line and not line.startswith("#"))
        password = values.get("PTMOVE_ADMIN_PASSWORD")
        if not password:
            raise SystemExit("Existing administrator password unavailable; no credentials changed")
        command(["docker", "run", "--rm", "--network=none", "--memory=128m", "--cpus=0.25", "-i",
                 "-v", f"{data}:/app/data", "ptmove-nexus:0.3.0", "--initialize-admin"],
                input=json.dumps({"username": "admin", "password": password}), text=True)
    compose(root, kind, "config", "--quiet")
    print(f"Prepared {kind}: independent runtime data; existing administrator password preserved")


def health(port):
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    for _ in range(20):
        try:
            with opener.open(f"http://127.0.0.1:{port}/health", timeout=2) as response:
                data = json.load(response)
                if data.get("version") == "0.3.0" and data.get("service") == "ptmove-nexus":
                    return data
        except Exception:
            pass
        time.sleep(2)
    raise RuntimeError("Native health did not report the expected version")


def cutover(root):
    # Canary readiness is necessary; actual API/browser acceptance is performed separately.
    health(18121)
    old = json.loads(subprocess.check_output(["docker", "inspect", "gpt-ptmove-controller-1"]))[0]
    labels = old["Config"].get("Labels", {})
    if labels.get("com.docker.compose.project") != "gpt-ptmove" or labels.get("com.docker.compose.service") != "controller":
        raise SystemExit("Old container ownership mismatch; cutover refused")
    prepare(root, "production")
    command(["docker", "stop", old["Id"]], stdout=subprocess.DEVNULL)
    try:
        compose(root, "production", "up", "-d", "--no-build")
        health(18120)
    except Exception:
        compose(root, "production", "stop")
        command(["docker", "start", old["Id"]], stdout=subprocess.DEVNULL)
        raise
    print("Cutover healthy on18120; old container, environment and data retained for rollback")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("action", choices=["prepare-canary", "start-canary", "prepare-production", "cutover", "stop-canary"])
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[1]
    if root != Path("/opt/gpt-ptmove-nexus") or os.name != "posix":
        raise SystemExit("Refusing deployment outside the exact152 project directory")
    if args.action == "prepare-canary":
        prepare(root, "canary")
    elif args.action == "prepare-production":
        prepare(root, "production")
    elif args.action == "start-canary":
        prepare(root, "canary")
        compose(root, "canary", "up", "-d", "--no-build")
        print(json.dumps(health(18121), ensure_ascii=False))
    elif args.action == "stop-canary":
        compose(root, "canary", "down")
    elif args.action == "cutover":
        cutover(root)


if __name__ == "__main__":
    main()
