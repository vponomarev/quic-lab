"""Run on Linux with Playwright; verifies production UI against a synthetic user row."""
from pathlib import Path
import sys
from playwright.sync_api import sync_playwright
root=Path(sys.argv[1])
form=lambda path: f'<form action="https://example.test/users/{path}"><input name="id" value="test"><input name="csrf" value="test"><input name="name" value="Test"><label>TTL<input name="ttl_hours" value="24"></label><label>Limit<input name="max_devices" value="5"></label><button>Submit</button></form>'
fixture='<main><section class="card"><table><thead><tr>'+''.join('<th>Column</th>' for _ in range(6))+'</tr></thead><tbody id="user-stats"><tr data-user-id="test" data-vless="true" data-measured="false"><td><strong data-stat="name">Test</strong></td><td data-stat="connections"></td><td data-stat="last"></td><td data-stat="traffic"></td><td>Expiry</td><td><div class="user-actions"><details class="export"><summary>Connect</summary>'+form('qr')+form('config')+'</details><details class="user-settings"><summary>Settings</summary>'+form('settings')+'</details></div></td></tr></tbody></table></section></main>'
fixture=fixture.replace('<details class="export">','<details class="devices"><summary>Устройства</summary><div class="device" data-device-id="device-one" data-disabled="false" data-legacy="false" data-created="2026-10-03T10:00:00Z" data-last-report="2026-10-07T08:00:00Z"><strong>Xiaomi</strong><small>Регистрация: 03.10.2026 · ID: device-o</small></div><div class="device" data-device-id="device-two" data-disabled="false" data-legacy="false" data-created="2026-10-07T10:00:00Z" data-last-report=""><strong>Xiaomi</strong><small>Регистрация: 07.10.2026 · ID: device-t</small></div></details><details class="export">')
fixture=fixture.replace('</small></div>', '</small><form class="inline" action="https://example.test/devices/disable"><input type="hidden" name="id" value="device-one"><button class="danger">Отозвать устройство</button></form></div>', 1)
fixture=fixture.replace('<details class="export">','<details class="enrollments"><summary>Invites</summary><div data-expires="2020-01-01T00:00:00Z" data-used="1" data-limit="5" data-revoked="false"><small>Old invitation</small><form action="https://example.test/enrollments/revoke"><input type="hidden" name="id" value="invite-one"><button>Revoke</button></form></div></details><details class="export">')
fixture=fixture.replace('<button class="danger">Отозвать устройство</button></form>','<button class="danger">Отозвать устройство</button></form><form class="inline" action="https://example.test/users/awg-qr"><input type="hidden" name="id" value="device-one"><button>QR AWG</button></form>')
with sync_playwright() as p:
 b=p.chromium.launch(headless=True,executable_path='/root/.cache/ms-playwright/chromium_headless_shell-1228/chrome-headless-shell-linux64/chrome-headless-shell',args=['--no-sandbox'])
 page=b.new_page();errors=[];page.on('pageerror',lambda e:errors.append(str(e)))
 page.set_content(fixture);page.add_script_tag(content=(root/'internal/admin/ui.js').read_text())
 assert not errors,errors
 revoke=page.locator('.device-table form[action$="/disable"]')
 assert revoke.count()==1
 assert revoke.locator('[name=id]').input_value()=='device-one'
 assert revoke.locator('xpath=ancestor::tr[1]').get_attribute('data-device-id')=='device-one'
 assert page.locator('.enrollment-table tbody tr').count()==1
 assert 'Истекло' in page.locator('.enrollment-table').inner_text()
 assert page.locator('.enrollment-table form [name=id]').input_value()=='invite-one'
 assert page.locator('.device-table form[action$="/awg-qr"] [name=id]').input_value()=='device-one'
 links=page.locator('a',has_text='Журналы устройств пользователя');assert links.count()==2
 for link in links.all(): assert link.get_attribute('href')=='diagnostics/clients?user=test'
 def update(values):
  page.evaluate('(connections)=>AdminUI.update(document.querySelector("[data-user-id]"),{connections})',values)
 peers=[f'vless · 198.51.100.1:{1000+i}\nС 04.10 10:00:00 UTC' for i in range(18)]
 update(peers)
 cell=page.locator('[data-stat=connections]')
 assert '18 TCP' in cell.inner_text(),cell.inner_text()
 assert '198.51.100.1:1000' not in cell.inner_text()
 cell.locator('button').click()
 detail=page.locator('.detail-connections details');assert detail.count()==1
 detail.locator('summary').click();assert detail.evaluate('(e)=>e.open')
 assert '198.51.100.1:1000' in detail.inner_text()
 update(peers[:-1]);assert detail.evaluate('(e)=>e.open');assert '17 TCP' in detail.locator('summary').inner_text()
 update(peers+['vless · 203.0.113.4:2000\nС now','QUIC · 198.51.100.1\nС now'])
 assert page.locator('.detail-connections details').count()==3
 update([]);assert 'Нет подключений' in cell.inner_text()
 assert page.locator('.device-registrations').count()==0
 assert page.locator('.device-table tbody tr').count()==2
 assert page.locator('.device-table th').all_text_contents()==['Устройство / ключ','Регистрация','Последний отчёт','Действия']
 assert 'Нет данных' in page.locator('.device-table').inner_text()
 assert page.locator('.device-report').count()==2
 assert '2026' in page.locator('.device-report').first.inner_text()
 assert 'Отчётов пока нет' in page.locator('.device-report').last.inner_text()
 assert page.locator('.device-report a').first.get_attribute('href').endswith('&device=device-one')
 assert page.locator('a.action-link',has_text='Журналы устройств пользователя').count()==2
 page.get_by_role('tab',name='Устройства · 2',exact=True).click()
 page.add_style_tag(content=(root/'internal/admin/web.html').read_text().split('<style>',1)[1].split('</style>',1)[0])
 page.add_style_tag(content=(root/'internal/admin/ui.css').read_text())
 page.screenshot(path='/tmp/quic-diagnostics-ui.png')
 assert not errors,errors
 print('Grouped connections, live updates, preserved expansion, offline PASS');b.close()
