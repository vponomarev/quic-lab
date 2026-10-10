"""Checks production backup poller under the same restrictive script CSP."""
from pathlib import Path
from playwright.sync_api import sync_playwright,expect
import json,sys
root=Path(sys.argv[1]);state={'id':'fixture','state':'packing','size_bytes':0}
html='''<meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'self'; connect-src 'self'"><main data-backup-state="/lab/backups/state"><table><tr data-job="fixture"><td class="state"></td><td><a class="download" hidden>Download</a><button>Delete</button></td></tr></table></main><script src="/lab/backups/poll.js"></script>'''
with sync_playwright() as p:
 b=p.chromium.launch(headless=True,executable_path='/root/.cache/ms-playwright/chromium_headless_shell-1228/chrome-headless-shell-linux64/chrome-headless-shell',args=['--no-sandbox'])
 page=b.new_page();errors=[];page.on('pageerror',lambda e:errors.append(str(e)))
 page.route('https://backup.test/lab/backups',lambda r:r.fulfill(body=html,content_type='text/html'))
 page.route('https://backup.test/lab/backups/poll.js',lambda r:r.fulfill(body=(root/'internal/admin/backup_poll.js').read_text(),content_type='text/javascript'))
 page.route('https://backup.test/lab/backups/state',lambda r:r.fulfill(body=json.dumps([state]),content_type='application/json'))
 page.goto('https://backup.test/lab/backups');expect(page.locator('button')).to_be_disabled();expect(page.locator('.download')).to_be_hidden()
 state.update(state='ready',size_bytes=1234);expect(page.locator('.download')).to_be_visible();expect(page.locator('button')).to_be_enabled();expect(page.locator('.state')).to_contain_text('1234')
 assert not errors,errors;b.close()
print('PASS backup polling under CSP: packing -> ready, download enabled without reload')
