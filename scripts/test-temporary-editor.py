"""Browser regression for the shared temporary-editor form; Linux Playwright."""
from pathlib import Path
import re,json,sys
from playwright.sync_api import sync_playwright
root=Path(sys.argv[1]);html=(root/'internal/admin/mdm_editor.html').read_text()
html=re.sub(r'<script[^>]*>.*?</script>','',html,flags=re.S)
html=html.replace('{{.Base}}','https://editor.test/lab/').replace('{{.Temporary}}','true').replace('{{.CSRF}}','csrf')
html=re.sub(r'{{.*?}}','',html)
doc={'schema':1,'profiles':[{'id':'default','name':'Phone','settings':{'transport':'quic'}}],'currentProfileId':'default','enabledProfileIds':['default'],'multiple':False,'globalApps':['org.telegram.messenger'],'dns':{'mode':'system','profileId':'default'},'budget':{'limitBytes':0},'diagnostics':{'enabled':True,'detailed':False}}
with sync_playwright() as p:
 b=p.chromium.launch(headless=True,executable_path='/root/.cache/ms-playwright/chromium_headless_shell-1228/chrome-headless-shell-linux64/chrome-headless-shell',args=['--no-sandbox'])
 page=b.new_page();errors=[];page.on('pageerror',lambda e:errors.append(str(e)))
 state={'paired':False,'generation':0,'status':'ready','expiresAt':'2099-01-01T00:00:00Z'}
 def route(r):
  op=r.request.url.rsplit('/',1)[-1]
  if op=='create':out={'id':'a'*64,'browserToken':'b'*64,'pairToken':'c'*64,'link':'quiclab://editor/pair#test','qr':'data:image/png;base64,'}
  elif op=='state':
   if state.get('revoked'):r.fulfill(status=403,body='Forbidden');return
   out=state
  elif op=='submit':
   from urllib.parse import parse_qs
   data=parse_qs(r.request.post_data);assert data['generation']==['0'];assert json.loads(data['document'][0])['profiles'][0]['name']=='Changed'
   state['pending']={'revision':1};state['status']='pending';out={'revision':1}
  elif op=='revoke':out={'accepted':True}
  else:raise AssertionError(op)
  r.fulfill(status=200,content_type='application/json',body=json.dumps(out))
 page.route('https://editor.test/lab/editor/**',route)
 page.route('https://editor.test/lab/editor',lambda r:r.fulfill(status=200,content_type='text/html',body=html));page.goto('https://editor.test/lab/editor');page.add_script_tag(content=(root/'internal/admin/mdm_editor.js').read_text())
 assert not errors,errors
 page.locator('#create-session').click();page.wait_for_function("!document.querySelector('#pair-details').hidden")
 assert page.locator('#apply').is_disabled()
 state.update(paired=True,configuration=doc)
 page.locator('#reload').click();page.wait_for_function("!document.querySelector('#apply').disabled")
 page.locator('[data-tab=config]').click();page.get_by_label('Название подключения',exact=True).fill('Changed');page.get_by_label('Название подключения',exact=True).dispatch_event('change')
 page.on('dialog',lambda d:d.accept())
 page.locator('#apply').click();page.wait_for_function("document.querySelector('#apply').disabled")
 page.locator('[data-tab=overview]').click();assert 'ожидаем' in page.locator('#status').inner_text().lower()
 page.locator('#end-session').click();page.wait_for_function("!document.querySelector('#create-session').disabled")
 assert page.locator('#apply').is_disabled();assert not errors,errors
 state.clear();state.update(paired=False,generation=0,status='waiting',expiresAt='2099-01-01T00:00:00Z')
 page.locator('#create-session').click();page.wait_for_function("!document.querySelector('#end-session').disabled")
 state['revoked']=True;page.locator('#reload').click();page.wait_for_function("!document.querySelector('#create-session').disabled",timeout=2000)
 b.close()
print('PASS temporary editor browser: pairing, shared form, pending acknowledgement, revoke')
