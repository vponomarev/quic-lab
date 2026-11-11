#!/usr/bin/env python3
"""Build the native Linux desktop archive from pinned Go dependencies."""
import argparse
import hashlib
import io
import json
import os
from pathlib import Path
import re
import subprocess
import tarfile
import tempfile


def command(*args):
    return subprocess.check_output(args, cwd=ROOT, text=True,
                                   env={**os.environ, "CGO_ENABLED": "1"})


def modules_for_desktop():
    raw = command("go", "list", "-deps", "-json", "./cmd/quic-lab")
    decoder = json.JSONDecoder()
    modules = {}
    while raw.strip():
        package, end = decoder.raw_decode(raw.lstrip())
        raw = raw.lstrip()[end:]
        module = package.get("Module")
        if module and module["Path"] != "quiclab":
            modules[module["Path"]] = module
    return modules


def collect_licenses(destination):
    manifest = []
    for name, module in sorted(modules_for_desktop().items()):
        effective = module.get("Replace", module)
        source = Path(effective["Dir"])
        found = []
        for directory, dirs, names in os.walk(source):
            dirs[:] = [d for d in dirs if d not in (".git", "node_modules")]
            for filename in sorted(names):
                if not filename.upper().startswith(("LICENSE", "NOTICE", "COPYING", "COPYRIGHT", "OFL")):
                    continue
                path = Path(directory) / filename
                data = path.read_bytes()
                if b"\x00" in data[:4096]:
                    continue
                relative = path.relative_to(source)
                target = destination / name.replace("/", "__") / relative
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes(data)
                found.append(str(target.relative_to(destination)).replace("\\", "/"))
        if not found:
            raise RuntimeError("No license text found for " + name)
        manifest.append({"module": name, "version": effective.get("Version", ""),
                         "effective_module": effective["Path"],
                         "requested_version": module.get("Version", ""),
                         "local_replacement": "Replace" in module and not effective.get("Version"),
                         "licenses": sorted(found)})
    goroot = Path(command("go", "env", "GOROOT").strip())
    (destination / "Go-LICENSE.txt").write_bytes((goroot / "LICENSE").read_bytes())
    (destination / "MODULES.json").write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")


ROOT = Path(__file__).resolve().parent.parent


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", required=True)
    parser.add_argument("--revision", required=True)
    args = parser.parse_args()
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._-]*", args.version):
        parser.error("Invalid version")
    if not re.fullmatch(r"[0-9a-f]{7,40}", args.revision):
        parser.error("Revision must be a source commit SHA")
    if command("go", "env", "GOOS").strip() != "linux":
        parser.error("Build on Linux with the target graphics toolchain")
    arch = command("go", "env", "GOARCH").strip()
    name = f"quic-lab-client-{args.version}-linux-{arch}"
    output = ROOT / "artifacts" / "linux-client-release"
    output.mkdir(parents=True, exist_ok=True)
    archive = output / (name + ".tar.gz")
    with tempfile.TemporaryDirectory() as temporary:
        package = Path(temporary) / name
        package.mkdir()
        subprocess.run(["go", "build", "-trimpath", "-buildvcs=false", "-ldflags=-s -w",
                        "-o", str(package / "quic-lab"), "./cmd/quic-lab"], cwd=ROOT,
                       env={**os.environ, "CGO_ENABLED": "1"}, check=True)
        for source, target in (
            ("docs/linux-client-release.md", "README.md"),
            ("docs/linux-client-gui.md", "linux-client-gui.md"),
            ("docs/linux-client-preview.md", "linux-client-preview.md"),
            ("packaging/linux/quic-lab-client.service", "quic-lab-client.service"),
            ("packaging/linux/quic-lab-client-per-app.conf", "quic-lab-client-per-app.conf"),
            ("packaging/linux/client.example.json", "client.example.json"),
            ("packaging/linux/install-gui.sh", "install-gui.sh"),
            ("packaging/linux/uninstall-gui.sh", "uninstall-gui.sh"),
            ("THIRD_PARTY_NOTICES.md", "THIRD_PARTY_NOTICES.md"),
            ("internal/awg/netstack/LICENSE", "LICENSE-netstack.txt"),
        ):
            (package / target).write_bytes((ROOT / source).read_bytes().replace(b"\r\n", b"\n"))
        (package / "VERSION.txt").write_text(f"Linux client {args.version}\nSource: {args.revision}\n", encoding="utf-8")
        collect_licenses(package / "licenses")
        # Include ELF dependencies so users can assess their distro before install.
        (package / "SYSTEM-LIBRARIES.txt").write_text(command("ldd", str(package / "quic-lab")), encoding="utf-8")
        with tarfile.open(archive, "w:gz") as tar:
            for path in sorted(package.rglob("*")):
                if not path.is_file():
                    continue
                data = path.read_bytes()
                info = tarfile.TarInfo(str(path.relative_to(package.parent)).replace("\\", "/"))
                info.size, info.uid, info.gid = len(data), 0, 0
                info.mode = 0o755 if path.name == "quic-lab" or path.suffix == ".sh" else 0o644
                info.uname = info.gname = "root"
                tar.addfile(info, io.BytesIO(data))
    digest = hashlib.sha256(archive.read_bytes()).hexdigest()
    (output / "SHA256SUMS.txt").write_text(digest + "  " + archive.name + "\n", encoding="ascii")
    print(archive)
    print(digest)


if __name__ == "__main__":
    main()
