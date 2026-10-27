"""Create a verified source release directly in the user's D: archive root."""
import argparse
from datetime import datetime, timezone, timedelta
import hashlib
import json
from pathlib import Path
import subprocess
import zipfile


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--image", type=Path, help="Optional existing Docker image archive already on D:")
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[1]
    now = datetime.now(timezone(timedelta(hours=8)))
    archive_root = Path("D:/A_GPT/GPT-BackUp/GPT-PTMOVE") / now.strftime("%Y-%m")
    if not Path("D:/").exists():
        raise SystemExit("D: is unavailable; refusing to fall back to C:")
    archive_root.mkdir(parents=True, exist_ok=True)
    target = archive_root / ("GPT-PTMOVE__" + now.strftime("%Y-%m-%d_%H%M%S") + "__历史发布__v0.1.0.zip")
    if target.exists():
        raise SystemExit("Archive already exists; rerun with a fresh timestamp")
    names = subprocess.check_output(["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z"], cwd=root).decode().split("\0")
    files = sorted(set(n for n in names if n and (root / n).is_file()))
    manifest = {}
    with zipfile.ZipFile(target, "x", zipfile.ZIP_DEFLATED, compresslevel=6) as z:
        for name in files:
            if name.startswith(("data/", ".local/", ".venv/", "output/")) or Path(name).name in {".env", ".env.nas"}:
                raise SystemExit("Unsafe tracked file in release: " + name)
            data = (root / name).read_bytes()
            manifest["source/" + name] = hashlib.sha256(data).hexdigest()
            z.writestr("source/" + name, data)
        if args.image:
            image = args.image.resolve()
            if image.drive.upper() != "D:":
                raise SystemExit("Image input must already be on D:")
            digest = hashlib.file_digest(image.open("rb"), "sha256").hexdigest()
            manifest["docker/" + image.name] = digest
            z.write(image, "docker/" + image.name)
        restore = {"project": "GPT-PTMOVE", "version": "0.1.0", "created_at": now.isoformat(), "source_path": str(root),
                   "scope": "Source and restore instructions; optional Docker image. No live database, credentials, NAS media or site accounts.",
                   "restore": "Extract source/, copy deployment .env and data/ (including encryption.key) from your separately secured live-data backup, then docker compose up -d --build. For NAS see source/docs/NAS_SETUP.md.",
                   "not_full_backup": True}
        z.writestr("RESTORE.json", json.dumps(restore, ensure_ascii=False, indent=2))
        z.writestr("SHA256.json", json.dumps(manifest, ensure_ascii=False, indent=2))
    with zipfile.ZipFile(target) as z:
        assert z.testzip() is None
        for name, digest in manifest.items():
            with z.open(name) as f:
                if hashlib.file_digest(f, "sha256").hexdigest() != digest:
                    raise RuntimeError("Archive integrity mismatch: " + name)
    index = archive_root.parent / "INDEX.jsonl"
    with index.open("a", encoding="utf-8") as f:
        f.write(json.dumps({"path": str(target), "created_at": now.isoformat(), "kind": "历史发布", "version": "0.1.0", "sha256": hashlib.file_digest(target.open("rb"), "sha256").hexdigest(), "scope": restore["scope"]}, ensure_ascii=False) + "\n")
    print(target)


if __name__ == "__main__":
    main()
