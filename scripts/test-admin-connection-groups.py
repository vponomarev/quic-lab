"""Run on Linux with Playwright; verifies production UI against a synthetic user row."""
from pathlib import Path
import sys
from playwright.sync_api import sync_playwright
root=Path(sys.argv[1])
form=lambda path: f'<form action="https://example.test/users/{path}"><input name="id" value="test"><input name="csrf" value="test"><input name="name" value="Test"><label>TTL<input name="ttl_hours" value="24"></label><label>Limit<input name="max_devices" value="5"></label><button>Submit</button></form>'
fixture='<main><section class="card"><table><thead><tr>'+''.join('<th>Column</th>' for _ in range(6))+'</tr></thead><tbody id="user-stats"><tr data-user-id="test" data-vless="true" data-measured="false"><td><strong data-stat="name">Test</strong></td><td data-stat="connections"></td><td data-stat="last"></td><td data-stat="traffic"></td><td>Expiry</td><td><div class="user-actions"><details class="export"><summary>Connect</summary>'+form('qr')+form('config')+'</details><details class="user-settings"><summary>Settings</summary>'+form('settings')+'</details></div></td></tr></tbody></table></section></main>'
with sync_playwright() as p:
 b=p.chromium.launch(headless=True,executable_path='/root/.cache/ms-playwright/chromium_headless_shell-1228/chrome-headless-shell-linux64/chrome-headless-shell',args=['--no-sandbox'])
 page=b.new_page();errors=[];page.on('pageerror',lambda e:errors.append(str(e)))
 page.set_content(fixture);page.add_script_tag(content=(root/'internal/admin/ui.js').read_text())
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
 assert not errors,errors
 print('Grouped connections, live updates, preserved expansion, offline PASS');b.close()
