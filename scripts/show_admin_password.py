"""Explicit local/SSH administrator action; shows only the console password."""
from pathlib import Path

env = Path(__file__).resolve().parents[1] / ".env"
for line in env.read_text("utf-8").splitlines():
    if line.startswith("PTMOVE_ADMIN_PASSWORD="):
        print(line.split("=", 1)[1])
        break
else:
    raise SystemExit("Deployment administrator password is not configured")
