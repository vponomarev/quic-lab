#!/usr/bin/env python3
"""Publish a validated certificate pair atomically and signal QUIC Lab (Linux/root)."""
import argparse, fcntl, os, pwd, ssl, subprocess, tempfile
from pathlib import Path
p=argparse.ArgumentParser(description=__doc__)
p.add_argument('--cert',type=Path,required=True);p.add_argument('--key',type=Path,required=True)
p.add_argument('--directory',type=Path,default=Path('/var/lib/quic-lab/tls'))
p.add_argument('--owner',default='quic-lab');p.add_argument('--hostname',required=True)
p.add_argument('--service',default='quic-lab.service');p.add_argument('--no-signal',action='store_true')
a=p.parse_args()
if os.geteuid()!=0: p.error('Run as root')
u=pwd.getpwnam(a.owner)
# Copy the inputs once, then validate exactly the bytes to be installed.
cert,key=a.cert.read_bytes(),a.key.read_bytes()
a.directory.mkdir(mode=0o700,parents=True,exist_ok=True)
os.chown(a.directory,u.pw_uid,u.pw_gid);a.directory.chmod(0o700)
lock=os.open(a.directory/'.reload.lock',os.O_CREAT|os.O_WRONLY,0o600)
with os.fdopen(lock,'w') as locked:
 fcntl.flock(locked,fcntl.LOCK_EX)
 stage=Path(tempfile.mkdtemp(prefix='generation-',dir=a.directory))
 os.chown(stage,u.pw_uid,u.pw_gid)
 try:
  for name,data in [('cert.pem',cert),('key.pem',key)]:
   path=stage/name
   with path.open('xb') as f: os.fchmod(f.fileno(),0o600);f.write(data)
   os.chown(path,u.pw_uid,u.pw_gid)
  ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER).load_cert_chain(stage/'cert.pem',stage/'key.pem')
  subprocess.run(['openssl','x509','-in',str(stage/'cert.pem'),'-noout','-checkhost',a.hostname],check=True)
  subprocess.run(['openssl','x509','-in',str(stage/'cert.pem'),'-noout','-checkend','0'],check=True)
 except BaseException:
  for name in ('cert.pem','key.pem'): (stage/name).unlink(missing_ok=True)
  stage.rmdir();raise
 pending=a.directory/'current.next';pending.unlink(missing_ok=True);pending.symlink_to(stage.name)
 os.replace(pending,a.directory/'current')
 if not a.no_signal:
  subprocess.run(['systemctl','kill','--kill-whom=main','--signal=HUP',a.service],check=True)
 print('Certificate pair published; old generations retained for rollback.')
