#!/usr/bin/env python3
"""Build and package a STARPT WASI plugin written in Go."""

import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import zipfile


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("source", type=Path, help="Directory with plugin.json and main.go")
    parser.add_argument("--output-dir", type=Path, required=True)
    parser.add_argument("--go", default="go", help="Go 1.23 or later executable")
    args = parser.parse_args()

    source = args.source.resolve(strict=True)
    manifest = source / "plugin.json"
    metadata = json.loads(manifest.read_text(encoding="utf-8-sig"))
    identifier = metadata.get("id", "")
    if metadata.get("schema_version") != 2 or not isinstance(identifier, str) or not re.fullmatch(r"[a-z0-9][a-z0-9-]{0,39}", identifier):
        parser.error("plugin.json requires schema_version 2 and a valid id")
    args.output_dir.mkdir(parents=True, exist_ok=True)
    target = args.output_dir / (identifier + ".zip")

    env = dict(os.environ, GOOS="wasip1", GOARCH="wasm", CGO_ENABLED="0", GOWORK="off")
    with tempfile.TemporaryDirectory(prefix=".starpt-plugin-", dir=args.output_dir) as directory:
        wasm = Path(directory).resolve() / "plugin.wasm"
        subprocess.run([args.go, "build", "-trimpath", "-ldflags=-s -w", "-o", str(wasm), "main.go"],
                       cwd=source, env=env, check=True)
        package = wasm.with_suffix(".zip")
        with zipfile.ZipFile(package, "w", zipfile.ZIP_DEFLATED) as archive:
            archive.write(manifest, "plugin.json")
            archive.write(wasm, "plugin.wasm")
        os.replace(package, target)
    print(target)


if __name__ == "__main__":
    main()
