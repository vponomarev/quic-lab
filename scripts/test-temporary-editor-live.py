"""Linux-only acceptance against the disposable HTTPS admin fixture (no production data)."""
from pathlib import Path
from urllib.parse import urlparse,parse_qs
from urllib.request import Request,urlopen
import json,ssl
from playwright.sync_api import sync_playwright,expect
origin='https://192.168.5.214:18449'
ctx=ssl.create_default_context(cafile='artifacts/editor-live/cert.pem')
def device(op,token,body):
 req=Request(origin+'/api/editor/v1/'+op,data=json.dumps(body).encode(),headers={'Authorization':'Bearer '+token,'Content-Type':'application/json'})
 with urlopen(req,context=ctx) as r:return json.load(r)
doc={'schema':1,'profiles':[{'id':'default','name':'Synthetic','settings':{'transport':'quic'}}],'currentProfileId':'default','enabledProfileIds':[],'multiple':False,'globalApps':[],'dns':{'mode':'system','profileId':'default'},'budget':{'limitBytes':0},'diagnostics':{'enabled':True,'detailed':False}}
with sync_playwright() as p:
 b=p.chromium.launch(headless=True,executable_path='/root/.cache/ms-playwright/chromium_headless_shell-1228/chrome-headless-shell-linux64/chrome-headless-shell',args=['--no-sandbox'])
 page=b.new_page(ignore_https_errors=True);errors=[];page.on('pageerror',lambda e:errors.append(str(e)))
 page.goto(origin+'/lab/login');page.locator('[name=username]').fill('fixture');page.locator('[name=password]').fill('editor-disposable-fixture-only');page.locator('button').first.click()
 page.goto(origin+'/lab/editor');page.locator('#create-session').click();expect(page.locator('#pair-details')).to_be_visible()
 fields=parse_qs(urlparse(page.locator('#pair-link').input_value()).fragment);sid=fields['id'][0]
 grant=device('pair',fields['token'][0],{'id':sid,'generation':0,'configuration':doc});secret=grant['deviceToken']
 page.locator('#reload').click();expect(page.locator('#apply')).to_be_enabled();page.locator('[data-tab=config]').click();page.get_by_text('Импорт и экспорт',exact=True).click()
 for checkbox in page.locator('#transfer-sections input').all():checkbox.uncheck()
 page.locator('#transfer-sections [value=diagnostics]').check()
 with page.expect_download() as download:page.locator('#export-config').click()
 file=download.value.path();bundle=json.loads(Path(file).read_text());assert bundle['credentials']=='omitted';assert set(bundle['payload'])=={'diagnostics'}
 bundle['payload']['diagnostics']['detailed']=True
 page.locator('#import-file').set_input_files({'name':'diagnostics.json','mimeType':'application/json','buffer':json.dumps(bundle).encode()})
 page.locator('#import-mode').select_option('update');page.locator('#import-config').click();expect(page.locator('#message')).to_contain_text('Черновик подготовлен')
 assert 'pending' not in device('poll',secret,{'id':sid}) # Import cannot publish.
 page.on('dialog',lambda d:d.accept());page.locator('#apply').click();expect(page.locator('#message')).to_contain_text('Отправлено')
 pending=device('poll',secret,{'id':sid})['pending'];assert pending['document']['diagnostics']['detailed']
 device('ack',secret,{'id':sid,'revision':pending['revision'],'generation':1,'configuration':pending['document'],'status':'applied'})
 page.locator('[data-tab=overview]').click();page.locator('#reload').click();expect(page.locator('#status')).to_contain_text('подтвердил применение')
 page.locator('[data-tab=events]').click();expect(page.locator('#event-list')).to_contain_text('Применено')
 # A local phone edit races an existing browser draft; server rejects stale Apply.
 page.locator('[data-tab=config]').click();page.get_by_label('Название подключения',exact=True).fill('stale');page.get_by_label('Название подключения',exact=True).dispatch_event('change')
 latest=pending['document'];latest['profiles'][0]['name']='local'
 device('report',secret,{'id':sid,'generation':2,'configuration':latest})
 page.locator('#apply').click();expect(page.locator('#message')).to_contain_text('Конфликт')
 assert 'pending' not in device('poll',secret,{'id':sid})
 device('revoke',secret,{'id':sid});page.locator('[data-tab=overview]').click();expect(page.locator('#create-session')).to_be_enabled(timeout=15000);expect(page.locator('#create-session')).to_be_enabled()
 assert not errors,errors;b.close()
print('PASS real browser export/import draft, apply/ACK/audit, local edit conflict, revoke')
