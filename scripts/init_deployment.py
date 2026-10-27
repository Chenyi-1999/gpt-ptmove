"""Run on 152 inside /opt/gpt-ptmove before docker compose build/up.

Does not print secrets or overwrite an existing deployment's credentials.
"""
import os
from pathlib import Path
import secrets

root = Path(__file__).resolve().parents[1]
env = root / ".env"
if not env.exists():
    content = "\n".join([
        "PTMOVE_ADMIN_PASSWORD=" + secrets.token_urlsafe(24),
        "PTMOVE_SECRET_KEY=" + secrets.token_urlsafe(48),
        "PTMOVE_WORKER_TOKEN=" + secrets.token_urlsafe(48),
        "PTMOVE_PORT=18120", "",
    ])
    fd = os.open(env, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w", encoding="utf-8") as f:
        f.write(content)
    print("Created deployment credentials (not displayed)")
data = root / "data"
data.mkdir(exist_ok=True)
if hasattr(os, "chown"):
    os.chown(data, 10001, 10001)
data.chmod(0o700)
print("Deployment directory initialized")
