"""Fetch upstream Linux helpers at a pinned commit and verify Git blob identities."""
import hashlib
import pathlib
import sys
import urllib.request

COMMIT = "53eee1fed09121867f956c798c158c13a475d51f"
ASSETS = {
    "BDInfo": (73097275, "b2bcf18f9732ab7fa78b1fa7cd0f335970b911eb"),
    "BDInfoDataSubstractor": (71185080, "f449aec4dbd2878a06966dfba0613f15d748341c"),
    "liblzfse.so": (76640, "364490eacd0d7aff634faec70fd937b8e9891f0b"),
}


def main():
    destination = pathlib.Path(sys.argv[1])
    destination.mkdir(parents=True, exist_ok=True)
    for name, (size, blob_id) in ASSETS.items():
        digest = hashlib.sha1(f"blob {size}\0".encode())
        path = destination / name
        if path.is_file() and path.stat().st_size == size:
            with path.open("rb") as cached:
                while chunk := cached.read(1024 * 1024):
                    digest.update(chunk)
            if digest.hexdigest() == blob_id:
                path.chmod(0o755)
                print(f"Verified cached pinned helper: {name}", flush=True)
                continue
        digest = hashlib.sha1(f"blob {size}\0".encode())
        received = 0
        url = f"https://raw.githubusercontent.com/sqing33/PTNexus/{COMMIT}/server/bdinfo/linux/{name}"
        with urllib.request.urlopen(url, timeout=90) as response, path.open("wb") as output:
            while chunk := response.read(1024 * 1024):
                received += len(chunk)
                if received > size:
                    raise RuntimeError(f"Pinned upstream asset exceeds expected size: {name}")
                digest.update(chunk)
                output.write(chunk)
        if received != size or digest.hexdigest() != blob_id:
            path.unlink(missing_ok=True)
            raise RuntimeError(f"Pinned upstream asset verification failed: {name}")
        path.chmod(0o755)
        print(f"Verified pinned helper: {name}", flush=True)


if __name__ == "__main__":
    main()
