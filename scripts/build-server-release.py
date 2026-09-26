#!/usr/bin/env python3
"""Build self-contained Linux server archives (Go is needed only here)."""
import argparse
import hashlib
import io
import os
from pathlib import Path
import re
import subprocess
import tarfile
import tempfile

root = Path(__file__).resolve().parent.parent
p = argparse.ArgumentParser(description=__doc__)
p.add_argument("--version", required=True)
p.add_argument("--arch", choices=("amd64", "arm64", "all"), default="all")
a = p.parse_args()
if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._-]*", a.version):
    p.error("Invalid version")
out = root / "artifacts" / "server-release"
out.mkdir(parents=True, exist_ok=True)
checksums = []
for arch in (["amd64", "arm64"] if a.arch == "all" else [a.arch]):
    name = f"quic-lab-server-{a.version}-linux-{arch}"
    with tempfile.TemporaryDirectory() as tmp:
        binary = Path(tmp) / "quic-lab-server"
        subprocess.run(["go", "build", "-trimpath", "-o", str(binary), "./cmd/server"], cwd=root,
                       env={**os.environ, "GOOS": "linux", "GOARCH": arch, "CGO_ENABLED": "0"}, check=True)
        worker = Path(tmp) / "quic-lab-awg"
        subprocess.run(["go", "build", "-trimpath", "-o", str(worker), "./cmd/awg-server"], cwd=root,
                       env={**os.environ, "GOOS": "linux", "GOARCH": arch, "CGO_ENABLED": "0"}, check=True)
        transit = Path(tmp) / "quic-lab-transit"
        subprocess.run(["go", "build", "-trimpath", "-o", str(transit), "./cmd/awg-transit"], cwd=root,
                       env={**os.environ, "GOOS": "linux", "GOARCH": arch, "CGO_ENABLED": "0"}, check=True)
        archive = out / (name + ".tar.gz")
        files = [(transit, "quic-lab-transit", 0o755),
                 (root / "scripts/install-transit.py", "install-transit.py", 0o755),
                 (root / "scripts/transit-network.py", "transit-network.py", 0o755), (binary, "quic-lab-server", 0o755), (worker, "quic-lab-awg", 0o755),
                 (root / "scripts/install-awg.py", "install-awg.py", 0o755),
                 (root / "scripts/awg-network.py", "awg-network.py", 0o755),
                 (root / "scripts/install-server.py", "install-server.py", 0o755),
                 (root / "scripts/publish-apk.sh", "publish-apk.sh", 0o755),
                 (root / "docs/install-server.md", "README.md", 0o644),
                 (root / "THIRD_PARTY_NOTICES.md", "THIRD_PARTY_NOTICES.md", 0o644)]
        files.extend((source, "licenses/" + source.name, 0o644)
                     for source in sorted((root / "android/app/src/main/assets/licenses").glob("*.txt")))
        with tarfile.open(archive, "w:gz") as tar:
            for source, target, mode in files:
                info = tar.gettarinfo(str(source), arcname=name + "/" + target)
                info.mode, info.uid, info.gid, info.uname, info.gname = mode, 0, 0, "root", "root"
                data = source.read_bytes()
                if source not in (binary, worker, transit):
                    data = data.replace(b"\r\n", b"\n")
                info.size = len(data)
                tar.addfile(info, io.BytesIO(data))
        checksums.append(hashlib.sha256(archive.read_bytes()).hexdigest() + "  " + archive.name)
        print(archive)
(out / "SHA256SUMS").write_text("\n".join(checksums) + "\n", encoding="utf-8")
