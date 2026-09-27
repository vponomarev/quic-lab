#!/usr/bin/env python3
"""Build Windows and macOS capture helper archives without requiring Go on teacher PCs."""
import os
from pathlib import Path
import subprocess
import tempfile
import zipfile
root=Path(__file__).resolve().parent.parent
out=root/'artifacts'/'capture'
out.mkdir(parents=True,exist_ok=True)
with tempfile.TemporaryDirectory() as temp:
    tmp=Path(temp)
    for target,arches in [('windows',['amd64']),('darwin',['amd64','arm64'])]:
        binaries=[]
        for arch in arches:
            name='quic-lab-capture.exe' if target=='windows' else f'quic-lab-capture-darwin-{arch}'
            binary=tmp/name
            command=['go','build','-trimpath']
            if target=='windows': command+=['-ldflags=-H=windowsgui']
            command+=['-o',str(binary),'./cmd/capture']
            subprocess.run(command,cwd=root,env={**os.environ,'GOOS':target,'GOARCH':arch,'CGO_ENABLED':'0'},check=True)
            binaries.append(binary)
        files=binaries+([root/'desktop/capture/install-windows.ps1'] if target=='windows' else [root/'desktop/capture/install-macos.sh',root/'desktop/capture/launcher.applescript'])
        files.append(root/'docs/wireshark-capture.md')
        archive=out/f'quic-lab-capture-{target}.zip'
        with zipfile.ZipFile(archive,'w',zipfile.ZIP_DEFLATED) as z:
            for f in files:
                info=zipfile.ZipInfo(f.name);info.create_system=3;info.external_attr=(0o100755 if f in binaries or f.suffix=='.sh' else 0o100644)<<16;info.compress_type=zipfile.ZIP_DEFLATED
                z.writestr(info,f.read_bytes())
        print(archive)
