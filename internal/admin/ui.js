(() => {
 'use strict';
 const $ = (s, root=document) => root.querySelector(s);
 const $$ = (s, root=document) => [...root.querySelectorAll(s)];
 const el = (tag, cls='', text='') => {const n=document.createElement(tag);n.className=cls;n.textContent=text;return n;};
 const button = (text, cls='secondary') => {const b=el('button',cls,text);b.type='button';return b;};
 let serial=0;
 function load(key, fallback) {try{return JSON.parse(localStorage.getItem('portal.'+key)) ?? fallback;}catch{return fallback;}}
 function save(key, value) {try{localStorage.setItem('portal.'+key,JSON.stringify(value));}catch{}}
 function wire(dialog) {
  $('[data-close]',dialog)?.addEventListener('click',()=>dialog.close());
  dialog.addEventListener('click',e=>{if(e.target!==dialog)return;const b=dialog.getBoundingClientRect();if(e.clientX<b.left||e.clientX>b.right||e.clientY<b.top||e.clientY>b.bottom)dialog.close();});
 }
 function modal(title, subtitle, cls='') {
  const d=el('dialog',cls), h=el('header','modal-header'), t=el('h2','',title), x=button('×','modal-close'), body=el('div','modal-body');
  t.id='dialog-'+(++serial);d.setAttribute('aria-labelledby',t.id);x.dataset.close='';x.setAttribute('aria-label','Закрыть');h.append(t,x);d.append(h,body);
  if(subtitle)body.append(el('p','client-name',subtitle));wire(d);return {dialog:d,body,title:t};
 }
 function tabs(host, items) {
  const nav=el('div','tabs');nav.setAttribute('role','tablist');host.append(nav);
  const panels=new Map(),buttons=new Map();
  function select(key){for(const [k,p] of panels){p.hidden=k!==key;buttons.get(k).setAttribute('aria-selected',String(k===key));buttons.get(k).tabIndex=k===key?0:-1;}}
  items.forEach(([key,label])=>{const b=button(label),p=el('section','tab-panel');b.id='tab-'+(++serial);p.id='panel-'+serial;b.setAttribute('role','tab');b.setAttribute('aria-controls',p.id);p.setAttribute('role','tabpanel');p.setAttribute('aria-labelledby',b.id);b.addEventListener('click',()=>select(key));nav.append(b);host.append(p);panels.set(key,p);buttons.set(key,b);});
  nav.addEventListener('keydown',e=>{if(!['ArrowLeft','ArrowRight','Home','End'].includes(e.key))return;e.preventDefault();const bs=[...buttons.values()],ks=[...buttons.keys()],idx=bs.indexOf(document.activeElement);const n=e.key==='Home'?0:e.key==='End'?bs.length-1:(idx+(e.key==='ArrowRight'?1:-1)+bs.length)%bs.length;select(ks[n]);bs[n].focus();});
  select(items[0][0]);return {panels,select,nav};
 }
 function formAction(f){return new URL(f.action,location.href).pathname.split('/').pop();}
 async function post(form) {
  const r=await fetch(form.action,{method:'POST',body:new URLSearchParams(new FormData(form)),credentials:'same-origin'});
  const text=await r.text(),doc=new DOMParser().parseFromString(text,'text/html');
  if(new URL(r.url).pathname.endsWith('/login'))throw new Error('Сессия завершена. Откройте вход администратора в другой вкладке, затем повторите.');
  if(!r.ok)throw new Error($('.alert',doc)?.textContent || doc.body.textContent.trim() || 'Запрос не выполнен');
  return doc;
 }
 function qrForm(form, dialog, result) {
  let generation=0;
  dialog.addEventListener('close',()=>{generation++;result.replaceChildren();});
  dialog.addEventListener('change',()=>{generation++;result.replaceChildren();});
  dialog.addEventListener('qr-reset',()=>{generation++;result.replaceChildren();});
  form.addEventListener('submit',async e=>{
   e.preventDefault();const g=++generation,b=$('button',form);b.disabled=true;result.setAttribute('role','status');result.textContent='Готовим QR…';
   try {const doc=await post(form),qr=$('.card.qr',doc);if(!qr)throw new Error('Сервер не вернул QR-код.');if(g===generation&&dialog.open){result.replaceChildren(document.importNode(qr,true));const image=$('img',result);if(image){const download=el('a','download','Сохранить QR');download.href=image.src;download.download='connection-qr.png';result.append(download);}result.scrollIntoView({block:'nearest'});}}
   catch(err){if(g===generation&&dialog.open){result.setAttribute('role','alert');result.textContent=err.message;}}
   finally{b.disabled=false;}
  });
 }
 function settingsForm(form, dialog) {
  if(!form)return;
  const p=$('button',form).parentElement;p.className='form-actions';
  const status=el('p','status-message');status.setAttribute('role','status');form.append(status);
  form.addEventListener('submit',async e=>{e.preventDefault();const b=$('button',form);b.disabled=true;status.textContent='Сохраняем…';
   try{await post(form);save('reopen',{id:form.elements.id.value,tab:'access'});location.reload();}
   catch(err){status.setAttribute('role','alert');status.textContent=err.message;b.disabled=false;}
  });
 }
 function chooseDevice(panel, forms, devices, protocol) {
  const label=el('label','','Устройство'),select=el('select','device-select');select.setAttribute('aria-label','Устройство для '+protocol);
  devices.filter(d=>d.dataset.disabled!=='true'&&(protocol!=='AmneziaWG'||d.dataset.awg==='true')).forEach(d=>{const o=el('option','',$('strong',d).textContent);o.value=d.dataset.deviceId;select.append(o);});
  label.append(select);panel.append(label);
  const change=()=>{forms.forEach(f=>{f.elements.id.value=select.value;$('button',f).disabled=!select.value;});};select.addEventListener('change',change);change();
  if(!select.options.length)panel.append(el('p','field-note','Нет доступных устройств. Сначала зарегистрируйте устройство через приложение.'));
 }
 const table=$('#user-stats');
 const controllers=new Map();
 function setupRow(row) {
  const actions=$('.user-actions',row),name=$('[data-stat=name]',row),nameText=name.textContent;
  const devices=$$('.device',row), deviceDetails=$('.devices',row), enrollmentDetails=$('.enrollments',row),exportDetails=$('details.export',row),settingsDetails=$('details.user-settings',row);
  const openUser=button(nameText,'user-name');openUser.dataset.stat='name';name.replaceWith(openUser);
  const user=modal('Пользователь',nameText,'user-dialog');
  const sections=tabs(user.body,[['overview','Обзор'],['devices','Устройства · '+devices.length],['connections','Подключения'],['enrollments','Приглашения'],['access','Доступ'],['diagnostics','Диагностика']]);
  const overview=sections.panels.get('overview'),prefs=el('details','display-options');prefs.append(el('summary','','Показывать в обзоре'));const checks=el('div');prefs.append(checks);overview.append(prefs);const grid=el('div','overview-grid');overview.append(grid);
  const fields=[['access','Состояние доступа'],['connections','Активные подключения'],['traffic','Трафик · 10 минут'],['last','Последнее подключение'],['expires','Доступ до']];
  const stored=load('overview',{}),overviewItems=new Map();
  fields.forEach(([key,title])=>{const item=el('div','overview-item'),content=el('div');item.append(el('h3','',title),content);grid.append(item);overviewItems.set(key,content);const label=el('label'),input=el('input');input.type='checkbox';input.checked=stored[key]!==false;item.hidden=!input.checked;input.onchange=()=>{stored[key]=input.checked;save('overview',stored);item.hidden=!input.checked;};label.append(input,document.createTextNode(title));checks.append(label);});
  if(deviceDetails){$('summary',deviceDetails).remove();sections.panels.get('devices').append(...deviceDetails.childNodes);deviceDetails.remove();}
  if(!devices.length)sections.panels.get('devices').append(el('p','muted','Устройств пока нет.'));
  if(enrollmentDetails){$('summary',enrollmentDetails).remove();[...enrollmentDetails.children].forEach(x=>x.classList.add('enrollment-item'));sections.panels.get('enrollments').append(...enrollmentDetails.childNodes);enrollmentDetails.remove();}else sections.panels.get('enrollments').append(el('p','muted','Приглашений пока нет. Создайте новое приглашение кнопкой выше.'));
  const access=sections.panels.get('access');$('summary',settingsDetails).remove();access.append(...settingsDetails.childNodes);settingsDetails.remove();
  const toggle=$('form[action$="/toggle"]',actions);const accessState=toggle?.elements.disabled.value==='true'?'Доступ разрешён':'Доступ приостановлен';
  if(toggle){toggle.classList.add('subtle-danger');access.append(toggle);}
  const deletion=$('form[action$="/delete"]',access);
  const danger=el('div','danger-actions');if(toggle){toggle.classList.remove('subtle-danger');$('button',toggle).textContent=toggle.elements.disabled.value==='true'?'Приостановить доступ':'Возобновить доступ';danger.append(toggle);}if(deletion)danger.append(deletion);$$('hr',access).forEach(h=>h.remove());access.append(danger);
  const capture=$('.user-capture',actions);if(capture)sections.panels.get('diagnostics').append(capture);else sections.panels.get('diagnostics').append(el('p','muted','Захват трафика недоступен на этом сервере.'));
  settingsForm($('form[action$="/settings"]',access),user.dialog);
  const connectionsPanel=sections.panels.get('connections');connectionsPanel.classList.add('detail-connections');
  let liveConnections=[];
  function open(tab='overview'){user.body.querySelector('.client-name').textContent=openUser.textContent;sections.select(tab);user.dialog.showModal();}
  openUser.onclick=()=>open();const settingsButton=button('Настройки');settingsButton.onclick=()=>open('access');const more=button('⋯');more.setAttribute('aria-label','Устройства и диагностика: '+nameText);more.onclick=()=>open();
  // Reuse authenticated forms; do not duplicate credential-bearing exports in the DOM.
  const connect=modal('Подключить устройство',nameText,'connect-dialog');
  const forms=$$('form',exportDetails),enroll=forms.find(f=>formAction(f)==='qr'),vless=forms.find(f=>formAction(f)==='vless-qr'),awg=forms.find(f=>formAction(f)==='awg-qr'),configs=forms.filter(f=>formAction(f)==='config');
  const options=[['app','QUIC Lab']];if(vless)options.push(['vless','VLESS']);if(awg)options.push(['awg','AmneziaWG']);
  const methods=tabs(connect.body,options);
  methods.nav.addEventListener('click',()=>connect.dialog.dispatchEvent(new Event('qr-reset')));
  const app=methods.panels.get('app');app.append(el('h3','','Приглашение для регистрации'));
  const inputGrid=el('div','form-grid');$$('label',enroll).forEach(l=>inputGrid.append(l));enroll.prepend(inputGrid);
  const labels=$$('label',inputGrid);labels[0].firstChild.textContent='Действует, часов';labels[1].firstChild.textContent='Новых устройств';
  const eb=$('button',enroll);eb.textContent='Создать приглашение';const ef=el('div','form-actions');ef.append(eb);enroll.append(el('p','field-note','Срок приглашения ограничивает регистрацию. Уже подключённые устройства продолжат работать до окончания их доступа.'),ef);app.append(enroll);
  const appResult=el('div','qr-result');app.append(appResult);qrForm(enroll,connect.dialog,appResult);
  const legacy=el('details','export-note');legacy.append(el('summary','','Экспорт общего профиля'));
  legacy.append(el('p','field-note','Устройства с этим файлом используют общий профиль и не различаются сервером. Для нового устройства используйте приглашение выше.'));
  configs.filter(f=>!f.elements.format).forEach(f=>{const b=$('button',f);b.textContent='Скачать JSON';b.classList.add('secondary');legacy.append(f);});app.append(legacy);
  [['vless',vless,'VLESS'],['awg',awg,'AmneziaWG']].forEach(([key,form,label])=>{
   if(!form)return;const panel=methods.panels.get(key),group=[form,...(key==='awg'?configs.filter(f=>f.elements.format?.value==='awg'):[])];
   chooseDevice(panel,group,devices,label);panel.append(el('p','field-note','Профиль выбранного устройства для совместимого клиента. Срок приглашения к этому QR не относится.'));
   const controls=el('div','form-actions');group.forEach(f=>controls.append(f));$('button',form).textContent='Показать QR';panel.append(controls);const result=el('div','qr-result');panel.append(result);qrForm(form,connect.dialog,result);
   $('select',panel).addEventListener('change',()=>result.replaceChildren());
  });
  exportDetails.remove();
  function openConnect(method='app'){connect.body.querySelector('.client-name').textContent=openUser.textContent;methods.select(method);connect.dialog.dispatchEvent(new Event('qr-reset'));connect.dialog.showModal();}
  const connectButton=button('QR / подключение','');connectButton.onclick=()=>openConnect();
  const quick=el('div','user-quick-actions'),quickTitle=el('span','muted','Подключить клиента:');quick.append(quickTitle);
  [['app','QR QUIC Lab'],['vless','QR VLESS'],['awg','QR AmneziaWG']].forEach(([key,label])=>{if(!methods.panels.has(key))return;const b=button(label);b.onclick=()=>openConnect(key);quick.append(b);});
  user.body.querySelector('.client-name').after(quick);
  const invites=sections.panels.get('enrollments'),intro=el('div','invitation-intro');intro.append(el('h3','','Регистрация в QUIC Lab'),el('p','field-note','Эти приглашения добавляют устройства в приложение QUIC Lab. Для стороннего клиента используйте QR VLESS или QR AmneziaWG над вкладками.'),el('p','field-note','Старый QR повторно показать нельзя: сервер хранит только хеш его токена. Создайте новое приглашение и сохраните QR после создания. Старые приглашения продолжат действовать до истечения срока или отзыва.'));
  const createInvite=button('Создать новое приглашение','');createInvite.onclick=()=>openConnect('app');intro.append(createInvite);invites.prepend(intro);

  actions.append(connectButton,settingsButton,more,user.dialog,connect.dialog);
  $$('form[action$="/vless-qr"]',user.dialog).forEach(f=>{const result=el('div','qr-result');f.after(result);qrForm(f,user.dialog,result);});
  $$('form',user.dialog).filter(f=>['disable','revoke','delete'].includes(formAction(f))||formAction(f)==='toggle').forEach(f=>f.addEventListener('submit',e=>{
   if(formAction(f)==='toggle'&&f.elements.disabled.value==='false')return;
   const target=f.closest('.device')?.querySelector('strong')?.textContent || openUser.textContent;
   const message=formAction(f)==='revoke'?'Отозвать приглашение? Уже зарегистрированные устройства продолжат работать.':`Отключить доступ «${target}»? Активные подключения будут разорваны.`;
   if(!confirm(message))e.preventDefault();
  }));
  function refreshOverview(){const traffic=$('[data-stat=traffic]',row);if(row.dataset.measured!=='true')traffic.textContent='Не измеряется';else if(row.dataset.vless==='true')traffic.title='Счётчики QUIC / HTTPS / AWG. Трафик VLESS пока не измеряется.';overviewItems.get('access').textContent=accessState;overviewItems.get('expires').textContent=row.cells[4].textContent;for(const key of ['traffic','last'])overviewItems.get(key).textContent=$(`[data-stat="${key}"]`,row).textContent;overviewItems.get('connections').textContent=liveConnections.length?liveConnections.join('\n\n'):'Нет активных подключений';}
  function updateConnections(connections){
   liveConnections=connections;const cell=$('[data-stat=connections]',row);cell.replaceChildren();cell.className=connections.length?'online':'muted';
   if(!connections.length)cell.textContent='Нет подключений';else{const first=el('span','connection-primary',connections[0].split('\n')[0]);first.title=connections[0];cell.append(first);const state=el('span','connection-status',connections.some(c=>c.startsWith('AWG'))?'Активность AWG / подключения':'Подключений: '+connections.length);cell.append(state);if(connections.length>1){const b=button('Ещё '+(connections.length-1),'connection-more');b.onclick=()=>open('connections');cell.append(b);}}
   connectionsPanel.replaceChildren();if(!connections.length)connectionsPanel.append(el('p','muted','Нет активных подключений'));connections.forEach(c=>{const p=el('p','',c);p.style.whiteSpace='pre-line';connectionsPanel.append(p);});refreshOverview();
  }
  const initial=$$('p',$('[data-stat=connections]',row)).map(p=>p.innerText);updateConnections(initial);
  controllers.set(row.dataset.userId,{updateConnections,refreshOverview,open});
 }
 if(table){
  $$('#user-stats > tr[data-user-id]').forEach(setupRow);
  const tools=el('div','table-tools'),search=el('input');search.type='search';search.placeholder='Найти пользователя…';search.setAttribute('aria-label','Найти пользователя');tools.append(search);
  const columns=el('details','columns');columns.append(el('summary','','Столбцы'));const menu=el('div','columns-menu');columns.append(menu);tools.append(columns);table.closest('.card').before(tools);
  const stored=load('columns',{});$$('thead th',table.closest('table')).forEach((th,i)=>{if(i===0||i===5)return;const l=el('label'),c=el('input');c.type='checkbox';c.checked=stored[i]!==false;const apply=()=>{$$('tr',table.closest('table')).forEach(tr=>{if(tr.cells.length===6)tr.cells[i].hidden=!c.checked;});};c.onchange=()=>{stored[i]=c.checked;save('columns',stored);apply();};l.append(c,document.createTextNode(th.textContent));menu.append(l);apply();});
  search.oninput=()=>{$$('tr[data-user-id]',table).forEach(r=>{r.hidden=!$('[data-stat=name]',r).textContent.toLocaleLowerCase().includes(search.value.toLocaleLowerCase());});};
  const reopen=load('reopen',null);if(reopen){save('reopen',null);controllers.get(reopen.id)?.open(reopen.tab);}
 }
 window.AdminUI={update(row,user){if(user.last_at){const cell=$('[data-stat=last]',row),date=new Date(user.last_at);cell.replaceChildren(document.createTextNode(date.toLocaleString(undefined,{day:'2-digit',month:'2-digit',hour:'2-digit',minute:'2-digit'})),document.createElement('br'),el('small','',user.last.split('\n').slice(1).join(' ')));cell.title=date.toLocaleString(undefined,{timeZoneName:'short'});}controllers.get(row.dataset.userId)?.updateConnections(user.connections);controllers.get(row.dataset.userId)?.refreshOverview();}};
 const help=$('#stats-help');if(help){wire(help);$('[data-help]').onclick=()=>help.showModal();}
 if($('.login-page'))$('main > h1').hidden=true;
 document.addEventListener('click',async event=>{
  const button=event.target.closest('[data-copy-connection]');if(!button)return;
  const box=button.closest('.connection-link'),input=box.querySelector('[data-connection-link]'),status=box.querySelector('[data-copy-status]');
  try {await navigator.clipboard.writeText(input.value);status.textContent='Ссылка скопирована';}
  catch(_){input.focus();input.select();status.textContent='Скопируйте выделенную ссылку вручную';}
 });
 const tz=$('#visitor-timezone');if(tz)tz.textContent=Intl.DateTimeFormat().resolvedOptions().timeZone || 'Не определён';
})();
