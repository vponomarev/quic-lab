(() => {
 'use strict';
 const panels=[...document.querySelectorAll('.user-capture')];
 const entry=document.querySelector('[data-capture-open]');
 if(!entry)return;
 const base=new URL(entry.href).pathname.replace(/capture$/, '');
 const csrf=document.querySelector('input[name="csrf"]')?.value;
 const dialog=document.createElement('dialog');dialog.className='capture-dialog';dialog.setAttribute('aria-labelledby','capture-title');
 dialog.innerHTML=`<header class="modal-header"><h2 id="capture-title"></h2><button type="button" class="modal-close" aria-label="Закрыть">×</button></header><div class="modal-body">
 <p id="capture-description"></p><label id="capture-kind-label">Что захватывать <select id="capture-kind"><option value="vpn">VPN · QUIC + HTTPS</option><option value="echo">Echo · QUIC + HTTPS</option></select></label>
 <p id="capture-capability" class="muted"></p><p class="capture-status" role="status">Проверяем статус…</p><p role="alert"></p>
 <label id="capture-record-label">Ваши записи <select id="capture-record"></select></label>
 <div class="capture-controls"><button id="capture-start">Начать захват</button><button id="capture-link">Получить ссылку</button><button id="capture-stop" class="danger">Остановить</button><a id="capture-download">Скачать pcapng</a></div>
 <section id="capture-launch" hidden><strong>Запуск Wireshark на компьютере</strong><p>Ссылка одноразовая, действует 1 минуту. Передайте её capture-cli.</p><textarea id="capture-url" rows="3" readonly aria-label="Одноразовая ссылка"></textarea><div class="capture-controls"><button id="capture-copy-link" class="secondary">Копировать ссылку</button><a id="capture-settings" target="_blank" rel="noopener">Настройки путей ↗</a></div><label>Где будете запускать? <select id="capture-command-shell"><option value="windows">Windows · PowerShell</option><option value="windows-cmd">Windows · CMD</option><option value="arm64">macOS · Apple Silicon</option><option value="amd64">macOS · Intel</option></select></label><textarea id="capture-command" rows="3" readonly aria-label="Команда запуска"></textarea><div class="capture-controls"><button id="capture-copy-command" class="secondary">Копировать команду</button></div><p class="muted" id="capture-shell"></p></section>
 <p class="muted">Утилита: <a id="capture-windows">Windows ZIP</a> · <a id="capture-mac">macOS ZIP</a>. Установка и QR на телефоне не нужны.</p><small>До 10 минут / 32 MiB. Закрытие этого окна не останавливает запись. Используйте «Остановить».</small></div>`;
 document.body.append(dialog);
 const el=id=>dialog.querySelector('#capture-'+id);
 const status=dialog.querySelector('[role=status]'),error=dialog.querySelector('[role=alert]');
 el('windows').href=base+'download/capture/windows';el('mac').href=base+'download/capture/darwin';
 let user='',name='',selected='',sessions=[],busy=false,generation=0,link='',termination={};
 el('settings').href=base+'capture/settings';
 const api=async(path,values)=>{
  const r=await fetch(base+path,{method:values?'POST':'GET',credentials:'same-origin',cache:'no-store',headers:{Accept:'application/json'},...(values?{body:new URLSearchParams({csrf,...values})}:{})});
  if(r.redirected)throw Error('Сессия входа завершена. Обновите страницу и войдите снова.');
  if(!r.ok){const text=await r.text();throw Error(text.slice(0,240)||'Не удалось выполнить действие');}
  return r.json();
 };
 function command(){
  const platform=window.QuicCaptureSettings.load(base).platform;el('command-shell').value=platform;
  try{el('command').value=window.QuicCaptureSettings.command(base,link);el('copy-command').disabled=false;el('shell').textContent='Вставьте команду в '+(platform==='windows'?'PowerShell':platform==='windows-cmd'?'CMD (командная строка)':'Terminal')+'. Пути подставлены из настроек захвата.';}
  catch(e){el('command').value='';el('copy-command').disabled=true;el('shell').textContent=e.message;}
 }
 el('command-shell').onchange=()=>{try{window.QuicCaptureSettings.setPlatform(base,el('command-shell').value);command();}catch(e){error.textContent=e.message;}};

 function clearLink(){link='';el('launch').hidden=true;el('url').value='';el('command').value='';}
 function render(){
  const records=sessions.filter(s=>user?s.user===user:!s.user&&s.kind===el('kind').value);
  if(!records.some(s=>s.id===selected)){selected=(records.find(s=>s.state==='running')||records[0])?.id||'';clearLink();}
  el('record').replaceChildren(...records.map(s=>{const o=document.createElement('option');o.value=s.id;o.textContent=(s.state==='running'?'● Идёт запись':'Завершена')+' · до '+new Date(s.until).toLocaleTimeString()+' · '+Math.round(s.bytes/1024)+' KiB';return o;}));el('record').value=selected;el('record-label').hidden=!records.length;
  const s=records.find(s=>s.id===selected),running=s?.state==='running',active=records.some(s=>s.state==='running');
  status.textContent=s?(running?'● Захват включён':'Захват завершён')+' · '+Math.round(s.bytes/1024)+' KiB'+(running?' · до '+new Date(s.until).toLocaleTimeString():''):'Запись ещё не запущена';
  el('start').hidden=active;el('link').hidden=!running;el('stop').hidden=!running;el('download').hidden=!s;
  if(s)el('download').href=base+'capture/download?id='+encodeURIComponent(s.id);
  const cap=termination[el('kind').value]||[];
  el('capability').textContent=user?'QUIC/HTTPS: реконструированные TCP/IP-пакеты — не для измерения RTT и повторов. AWG: реальные внутренние пакеты.':'Расшифровка с сервера: QUIC '+(cap[0]?'доступна':'недоступна')+', HTTPS '+(cap[1]?'доступна':'недоступна')+'. AWG в этом режиме не расшифровывается.';
 }
 async function refresh(){const g=generation;const data=await api('capture/state');if(g!==generation||!dialog.open)return;sessions=data.sessions;termination=data.termination||{};render();}
 async function action(fn){if(busy)return;busy=true;error.textContent='';dialog.querySelectorAll('button,select').forEach(b=>b.disabled=true);try{await fn();await refresh();await refreshRows();}catch(e){error.textContent=e.message;}finally{busy=false;dialog.querySelectorAll('button,select').forEach(b=>b.disabled=false);}}
 async function launch(){const v=await api('capture/launch',{id:selected});link=v.link;el('url').value=link;command();el('launch').hidden=false;}
 function open(uid='',label=''){
  generation++;user=uid;name=label;selected='';sessions=[];clearLink();error.textContent='';el('title').textContent=user?'Внутренний захват · '+name:'Внешний захват · весь сервер';
  el('description').textContent=user?'Записывается только трафик этого пользователя после снятия VPN-обёртки. HTTPS самих приложений остаётся зашифрованным. Переподключать VPN не требуется.':'Записываются реальные пакеты всех клиентов выбранных слушателей и серверные ключи QUIC/TLS. После запуска начните новое Echo/VPN-соединение на телефоне.';
  el('kind-label').hidden=!!user;render();dialog.showModal();refresh().catch(e=>error.textContent=e.message);
 }
 document.querySelectorAll('[data-capture-open]').forEach(a=>a.addEventListener('click',e=>{e.preventDefault();open();}));
 dialog.querySelector('.modal-close').onclick=()=>dialog.close();dialog.addEventListener('close',()=>{generation++;clearLink();});
 dialog.addEventListener('cancel',e=>{if(busy)e.preventDefault();});
 el('kind').onchange=()=>{selected='';clearLink();render();};el('record').onchange=()=>{selected=el('record').value;clearLink();render();};window.addEventListener('storage',()=>{if(link)command();});window.addEventListener('focus',()=>{if(link)command();});
 el('start').onclick=()=>action(async()=>{const v=await api(user?'users/capture':'capture/start',user?{id:user,action:'start'}:{kind:el('kind').value});selected=v.id;if(v.link){link=v.link;el('url').value=link;command();el('launch').hidden=false;}else await launch();});
 el('link').onclick=()=>action(launch);el('stop').onclick=()=>action(async()=>{await api('capture/stop',{id:selected});clearLink();});
 async function copy(id,button){try{await navigator.clipboard.writeText(el(id).value);const old=button.textContent;button.textContent='Скопировано';setTimeout(()=>button.textContent=old,1500);}catch(e){el(id).focus();el(id).select();error.textContent='Скопируйте выделенный текст вручную.';}}
 el('copy-link').onclick=e=>copy('url',e.target);el('copy-command').onclick=e=>copy('command',e.target);
 panels.forEach(p=>p.querySelectorAll('form').forEach(f=>f.addEventListener('submit',e=>{e.preventDefault();open(p.dataset.user,p.closest('tr').querySelector('[data-stat=name]').textContent);}))); 
 async function refreshRows(){if(!panels.length)return;const states=await api('users/captures');panels.forEach(p=>{const s=states[p.dataset.user];p.querySelector('.capture-start').hidden=!!s;p.querySelector('.capture-active').hidden=!s;if(s){p.querySelector('.capture-label').textContent='● Запись · '+Math.round(s.bytes/1024)+' KiB'+(s.owned?'':' · другой вход');p.querySelectorAll('.capture-owned').forEach(f=>f.hidden=!s.owned);}});}
 // Row actions open the same management window; stopping remains an explicit action there.
 panels.forEach(p=>{const fs=p.querySelectorAll('.capture-owned');if(fs[0])fs[0].querySelector('button').textContent='Управление';if(fs[1])fs[1].remove();});
 refreshRows().catch(()=>{});setInterval(()=>{if(busy)return;refreshRows().catch(()=>{});if(dialog.open)refresh().catch(e=>error.textContent=e.message);},5000);
})();
