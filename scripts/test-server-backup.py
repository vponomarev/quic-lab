#!/usr/bin/env python3
"""Linux-only end-to-end backup acceptance against a disposable server."""
import argparse,hashlib,json,os,re,socket,subprocess,tempfile,time,urllib.request,urllib.parse
from pathlib import Path
p=argparse.ArgumentParser();p.add_argument('--binary',required=True);args=p.parse_args();binary=str(Path(args.binary).resolve())
def run(*cmd):return subprocess.run(cmd,check=True,text=True,capture_output=True)
with tempfile.TemporaryDirectory(prefix='quic-backup-acceptance-') as temp:
 root=Path(temp);conf=root/'etc/quic-lab';data=root/'var/lib/quic-lab';conf.mkdir(parents=True,mode=0o700);data.mkdir(parents=True,mode=0o700)
 password=root/'password';password.write_text('test-archive-passphrase\n');password.chmod(0o600)
 cert=conf/'cert.pem';key=conf/'key.pem';run('openssl','req','-x509','-newkey','rsa:2048','-nodes','-keyout',str(key),'-out',str(cert),'-days','1','-subj','/CN=backup.test')
 s=socket.socket();s.bind(('127.0.0.1',0));port=s.getsockname()[1];s.close()
 cfg={'listen':f'127.0.0.1:{port}','public_url':'https://backup.test/lab/','username':'admin','password':'test-admin-password','data_dir':str(data),'echo':{'endpoint':'backup.test:443','hostname':'backup.test'},'vpn':{'quic':'backup.test:443','https':'backup.test:443','hostname':'backup.test'}}
 (conf/'admin.json').write_text(json.dumps(cfg));(conf/'admin.json').chmod(0o600)
 udp=socket.socket(socket.AF_INET,socket.SOCK_DGRAM);udp.bind(('127.0.0.1',0));udp_port=udp.getsockname()[1];udp.close()
 opts={'listen':f'127.0.0.1:{udp_port}','cert':str(cert),'key':str(key),'admin_config':str(conf/'admin.json')};(conf/'server.json').write_text(json.dumps(opts))
 log=open(root/'server.log','wb');proc=None
 def start():
  global proc
  proc=subprocess.Popen([binary,'-config',str(conf/'server.json')],stdout=log,stderr=log)
  for _ in range(100):
   if proc.poll() is not None:raise RuntimeError('server exited: '+(root/'server.log').read_text())
   if (data/'backups/control.sock').exists():return
   time.sleep(.05)
  raise RuntimeError('server startup timeout')
 def stop():
  global proc
  if proc is not None and proc.poll() is None:proc.terminate();proc.wait(timeout=20)
 def request(path,form=None,cookie=None):
  headers={'Origin':'https://backup.test'}
  if cookie:headers['Cookie']=cookie
  req=urllib.request.Request(f'http://127.0.0.1:{port}'+path,data=None if form is None else urllib.parse.urlencode(form).encode(),headers=headers)
  class NoRedirect(urllib.request.HTTPRedirectHandler):
   def redirect_request(self,*a,**kw):return None
  try:return urllib.request.build_opener(NoRedirect()).open(req,timeout=15)
  except urllib.error.HTTPError as e:
   if e.code==303:return e
   raise
 try:
  start();login=request('/login',{'username':cfg['username'],'password':cfg['password']});cookie=login.headers['Set-Cookie'].split(';')[0];page=request('/backups',cookie=cookie).read().decode();csrf=re.search(r'name="csrf" value="([^"]+)"',page)[1]
  response=request('/users/add',{'csrf':csrf,'name':'restore-test'},cookie);assert response.status in (200,303)
  original=(data/'identities.json').read_bytes();assert len(json.loads(original)['users'])==1
  history=data/'client-diagnostics'/'device'
  history.mkdir(parents=True);(history/'batch.json').write_text('{"version":1,"records":[]}')
  (data/'downloads').mkdir();(data/'downloads/quic-lab.apk').write_bytes(b'not-a-real-apk')
  outputs={}
  for kind in ('config','full'):
   output=root/(kind+'.age');result=json.loads(run(binary,'backup','create','--type',kind,'--socket',str(data/'backups/control.sock'),'--output',str(output),'--password-file',str(password),'--json').stdout);assert result['size_bytes']==output.stat().st_size;assert result['sha256']==hashlib.sha256(output.read_bytes()).hexdigest()
   verified=run(binary,'backup','verify','--input',str(output),'--password-file',str(password),'--json');manifest=json.loads(verified.stdout);names=[x['path'] for x in manifest['entries']];assert 'data/identities.json' in names;assert 'data/downloads/quic-lab.apk' not in names;assert ('data/client-diagnostics/device/batch.json' in names)==(kind=='full');outputs[kind]=output
  # GUI creation and authenticated download use the same archive verifier.
  page=request('/backups',cookie=cookie).read().decode();reqid=re.search(r'name="request_id" value="([^"]+)"',page)[1]
  request('/backups/create',{'csrf':csrf,'request_id':reqid,'type':'config','password':'test-archive-passphrase','repeat':'test-archive-passphrase'},cookie)
  for _ in range(200):
   jobs=json.loads(request('/backups/state',cookie=cookie).read());ready=[j for j in jobs if j['state']=='ready']
   if ready:break
   time.sleep(.05)
  assert ready,'GUI backup failed';payload=request('/backups/'+ready[0]['id']+'/download',cookie=cookie).read();gui=root/'gui.age';gui.write_bytes(payload);run(binary,'backup','verify','--input',str(gui),'--password-file',str(password))
  # A live owner must prevent replacing the running server's data.
  blocked=subprocess.run([binary,'backup','restore','--input',str(outputs['full']),'--password-file',str(password),'--root',str(root),'--yes'],capture_output=True)
  assert blocked.returncode!=0,'restored a running server';assert (data/'identities.json').read_bytes()==original
  stop()
  result=json.loads(run(binary,'backup','restore','--input',str(outputs['full']),'--password-file',str(password),'--root',str(root),'--yes','--json').stdout)
  assert (data/'identities.json').read_bytes()==original;assert (history/'batch.json').exists();assert not (data/'downloads/quic-lab.apk').exists()
  start();stop() # restored config, certificates and identities start without reenrollment
  run(binary,'backup','rollback','--journal',str(Path(result['rollback_dir'])/'journal.json'))
  assert (data/'downloads/quic-lab.apk').read_bytes()==b'not-a-real-apk'
  run(binary,'backup','restore','--input',str(outputs['config']),'--password-file',str(password),'--root',str(root),'--yes')
  assert (data/'identities.json').read_bytes()==original;assert not (history/'batch.json').exists()
  print('PASS: CLI config/full, GUI download, authenticated verify, live-owner rejection, offline restore/start, rollback, history and APK scope')
 finally:stop();log.close()
