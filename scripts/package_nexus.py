"""Archive the native fork directly on D:, with per-file integrity verification."""
import argparse
from datetime import datetime, timedelta, timezone
import hashlib
import json
import os
from pathlib import Path
import subprocess
import zipfile


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--image", type=Path)
    parser.add_argument("--extra-image", type=Path, action="append", default=[], help="Additional verified Docker archives, e.g. NAS amd64/arm64")
    parser.add_argument("--release", action="store_true")
    args = parser.parse_args()
    if args.release and not args.image:
        parser.error("a release archive requires the verified Docker image; use source snapshot otherwise")
    if args.extra_image and not args.image:
        parser.error("additional Docker archives require --image")
    root = Path(__file__).resolve().parents[1]
    archive_root = Path("D:/A_GPT/GPT-BackUp/GPT-PTMOVE")
    if not Path("D:/").exists():
        raise SystemExit("D: unavailable; no C: fallback")
    now = datetime.now(timezone(timedelta(hours=8)))
    destination = archive_root / now.strftime("%Y-%m")
    destination.mkdir(parents=True, exist_ok=True)
    version = (root / "nexus/VERSION").read_text("utf-8").strip()
    kind = "历史发布" if args.release else "源码快照"
    target = destination / f"GPT-PTMOVE__{now:%Y-%m-%d_%H%M%S}__{kind}__Nexus-v{version}.zip"
    names = subprocess.check_output(["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z"], cwd=root).decode("utf-8").split("\0")
    required = {"nexus/server/cmd/server/main.go", "nexus/server/cmd/server/admin_init.go", "nexus/Dockerfile.ptmove"}
    if not required.issubset(set(names)):
        raise SystemExit("Required native entry points are ignored or missing; source archive refused")
    index_entries = subprocess.check_output(["git", "ls-files", "--stage", "-z"], cwd=root).decode("utf-8").split("\0")
    modes = {entry.split("\t", 1)[1]: entry.split(" ", 1)[0] for entry in index_entries if "\t" in entry}
    manifest = {}
    image = args.image.resolve() if args.image else None
    images = ([image] if image else []) + [path.resolve() for path in args.extra_image]
    if any(path.drive.upper() != "D:" or not path.is_file() for path in images):
        raise SystemExit("Docker archive must already exist on D:")
    if len({path.name for path in images}) != len(images):
        raise SystemExit("Docker archive names must be unique")
    with zipfile.ZipFile(target, "x", zipfile.ZIP_DEFLATED, compresslevel=6) as archive:
        for name in sorted(set(filter(None, names))):
            source = root / name
            if not source.is_file() and not source.is_symlink():
                continue
            parts = Path(name).parts
            if "dist" in parts or name == "nexus/proxy/pt-nexus-box-proxy":
                continue  # Rebuildable Git delivery products are supplied in the Docker archives.
            if any(p in {".local", "data", "node_modules", ".venv", "dist", "__pycache__"} for p in parts) or source.name.startswith(".env") and source.name != ".env.example" or source.suffix in {".key", ".pem", ".torrent"}:
                raise SystemExit("Unsafe source archive entry: " + name)
            data = os.readlink(source).encode("utf-8") if source.is_symlink() else source.read_bytes()
            manifest["source/" + name] = hashlib.sha256(data).hexdigest()
            if modes.get(name) == "120000" or source.is_symlink():
                info = zipfile.ZipInfo("source/" + name, now.timetuple()[:6])
                info.create_system = 3
                info.external_attr = 0o120777 << 16
                archive.writestr(info, data)
            else:
                archive.writestr("source/" + name, data)
        for docker_image in images:
            with docker_image.open("rb") as stream:
                manifest["docker/" + docker_image.name] = hashlib.file_digest(stream, "sha256").hexdigest()
            archive.write(docker_image, "docker/" + docker_image.name)
        restore = {"version": version, "created_at": now.isoformat(), "source_path": str(root), "scope": "Tracked and non-ignored source, including uncommitted changes; no live credentials, databases, SDK/cache or media", "docker_included": bool(image), "release_verified": bool(args.release), "restore": "Restore source/; use nexus/compose.ptmove.yaml with --env-file .env.ptmove. Runtime .env.ptmove and data/ (including machine-registry.key and internal-api.key) require a separate secured backup. Python0.2 data cannot be used as native Go data. A source snapshot is not a Docker release."}
        restore["docker_archives"] = [path.name for path in images]
        restore["source_excluded_build_products"] = ["nexus/webui/dist", "nexus/proxy/pt-nexus-box-proxy"]
        restore["docker_restore"] = "Load only the NAS archive matching the NAS architecture; use compose.nas.yaml for NAS and compose.ptmove.yaml for controller. Configure independent tokens/instance settings; archives contain no live credentials."
        archive.writestr("RESTORE.json", json.dumps(restore, ensure_ascii=False, indent=2))
        archive.writestr("SHA256SUMS.json", json.dumps(manifest, ensure_ascii=False, indent=2))
    with zipfile.ZipFile(target) as archive:
        if archive.testzip():
            raise SystemExit("ZIP CRC verification failed")
        for name, expected in manifest.items():
            with archive.open(name) as stream:
                actual = hashlib.file_digest(stream, "sha256").hexdigest()
            if actual != expected:
                raise SystemExit("Archive hash mismatch: " + name)
    with target.open("rb") as stream:
        digest = hashlib.file_digest(stream, "sha256").hexdigest()
    record = {"project": "GPT-PTMOVE", "type": kind, "created_at": now.isoformat(), "path": str(target), "sha256": digest, "files": len(manifest), "scope": restore["scope"], "docker_included": bool(image), "verification": "ZIP CRC and every manifest SHA256 passed"}
    with (archive_root / "INDEX.jsonl").open("a", encoding="utf-8") as index:
        index.write(json.dumps(record, ensure_ascii=False) + "\n")
    print(json.dumps(record, ensure_ascii=False))


if __name__ == "__main__":
    main()
