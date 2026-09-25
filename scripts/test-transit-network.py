#!/usr/bin/env python3
"""Run only in a disposable Linux network namespace/container with NET_ADMIN."""
import importlib.util,json,subprocess,sys,tempfile
from pathlib import Path
spec=importlib.util.spec_from_file_location('network',Path(__file__).with_name('transit-network.py'));n=importlib.util.module_from_spec(spec);spec.loader.exec_module(n)
real=n.run
n.run=lambda *args,**kw:None if args[0]=='sysctl' else real(*args,**kw)
def get(*args):return subprocess.run(['ip','-4','route','get',*args],capture_output=True,text=True)
with tempfile.TemporaryDirectory() as tmp:
    n.MARKER=Path(tmp)/'owned';cfg=Path(tmp)/'config.json'
    cfg.write_text(json.dumps(dict(transit=dict(source_ip='10.8.0.9',endpoint='192.0.2.1:52000'),ingress_subnet='10.77.0.0/24',front_ips=['192.0.2.2'])))
    before=get('1.1.1.1').stdout
    real('ip','link','add','ql-exit0','type','dummy');real('ip','address','add','10.8.0.9/32','dev','ql-exit0');real('ip','link','set','ql-exit0','up')
    sys.argv=['transit-network.py','start','--config',str(cfg)];n.main();n.main()
    assert get('1.1.1.1','from','10.8.0.9').returncode!=0,'direct fallback before uplink'
    real('ip','route','add','table',n.TABLE,'default','dev','ql-exit0','metric','10')
    assert 'ql-exit0' in get('1.1.1.1','from','10.8.0.9').stdout
    # Even the front-end's own IP must traverse the uplink, not terminate locally.
    assert 'ql-exit0' in get('127.0.0.1','from','10.8.0.9').stdout
    assert 'local 192.0.2.2' in get('192.0.2.2','from','10.8.0.9').stdout
    assert get('1.1.1.1').stdout==before,'host management route changed'
    real('ip','link','del','ql-exit0')
    assert get('1.1.1.1','from','10.77.0.2','iif','lo').returncode!=0,'ingress direct fallback'
    assert get('1.1.1.1').stdout==before,'host route changed after uplink loss'
    sys.argv=['transit-network.py','stop','--config',str(cfg)];n.main()
    rules=json.loads(subprocess.check_output(['ip','-j','rule','show']))
    assert len([r for r in rules if r.get('priority')==0])==1
    assert get('1.1.1.1').stdout==before
    sys.argv=['transit-network.py','start','--config',str(cfg)];n.main()
    sys.argv=['transit-network.py','stop','--config',str(cfg)];n.main()
print('Transit fail-closed routes, local destination routing, restart and explicit cleanup passed')
