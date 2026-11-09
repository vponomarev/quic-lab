/* Cell-derived positions are approximate estimates, never GPS fixes. */
(function () {
  'use strict';
  const cellKey = c => [c.technology,c.mcc,c.mnc,c.tac,c.ci].join(':');
  const candidateCells = cells => (cells || []).filter(c => ['LTE','GSM','WCDMA'].includes(c.technology) && c.registered && c.ageMillis <= 120000 && ['mcc','mnc','tac','ci'].every(k => /^\d+$/.test(c[k] || '')));
  if (typeof module !== 'undefined') module.exports = {candidateCells,cellKey};
  if (typeof document === 'undefined') return;
  const root=document.getElementById('cell-track'); if (!root) return;
  const el=id=>document.getElementById('track-'+id);
  const start=el('start'),end=el('end'),status=el('status'),slider=el('time'),details=el('details');
  const local=t=>new Date(t.getTime()-t.getTimezoneOffset()*60000).toISOString().slice(0,16);
  const params=new URL(window.location.href).searchParams;
  const defaultEnd=+new Date(local(new Date()));
  let defaultStart=defaultEnd-7*86400000;
  // datetime-local chooses the earlier repeated DST hour; keep the default within the API's seven-day limit.
  defaultStart+=Math.max(0,defaultStart-+new Date(local(new Date(defaultStart))));
  const savedDate=(key,fallback)=>{
    const value=params.get(key),date=new Date(value || '');
    return value && /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/.test(value) && Number.isFinite(+date) && local(date)===value ? value : local(new Date(fallback));
  };
  start.value=savedDate('trackStart',defaultStart);end.value=savedDate('trackEnd',defaultEnd);
  function saveRange() {
    const url=new URL(window.location.href);
    url.searchParams.set('trackStart',start.value);url.searchParams.set('trackEnd',end.value);
    window.history.replaceState(window.history.state,'',url);
    const refresh=document.getElementById('mdm-refresh');if(refresh)refresh.href=url.href;
  }
  start.addEventListener('input',saveRange);end.addEventListener('input',saveRange);
  saveRange();
  document.getElementById('mdm-refresh')?.addEventListener('click',event=>{
    if(event.ctrlKey||event.metaKey||event.shiftKey||event.altKey)return;
    event.preventDefault();window.location.reload();
  });
  el('zone').textContent=Intl.DateTimeFormat().resolvedOptions().timeZone;
  let map,layer,selection,points=[],controller;
  const stamp=at=>new Date(at).toLocaleString();
  const positionOf=row=>row.cells.map(c=>({cell:c,position:locations.get(cellKey(c))})).find(p=>p.position);
  let locations=new Map();
  function ensureMap() {
    if(map)return;
    if(!window.L)throw new Error('Не удалось загрузить библиотеку карты. Обновите страницу.');
    map=L.map(el('map'),{preferCanvas:true}).setView([56,36],5);
    L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png',{maxZoom:19,attribution:'&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a>'}).on('tileerror',()=>{el('tiles').textContent='Подложка карты недоступна. Точки и шкала времени остаются доступны.';}).addTo(map);
    layer=L.layerGroup().addTo(map);selection=L.layerGroup().addTo(map);
  }
  function select(index,pan=false) {
    const p=points[index];selection.clearLayers();el('history').hidden=true;if(!p){details.textContent='Нет измерений за выбранный период.';return;}
    slider.value=index;
    if(!p.position){details.textContent=stamp(p.at)+' · Координаты не определены';return;}
    const c=p.cell,v=p.position;
    el('history').href=root.dataset.base+'mdm/cell-db/history?'+new URLSearchParams({technology:c.technology,mcc:c.mcc,mnc:c.mnc,tac:c.tac,ci:c.ci});el('history').hidden=false;
    details.textContent=stamp(p.at)+' · '+c.technology+' '+c.mcc+'/'+c.mnc+' · TAC/LAC '+c.tac+' · CI '+c.ci+' · '+v.lat.toFixed(5)+', '+v.lon.toFixed(5)+' · радиус оценки '+(v.range/1000).toFixed(1)+' км · источник: '+(v.source || 'неизвестен')+' · получено: '+(v.fetchedAt ? stamp(v.fetchedAt) : '—');
    L.circle([v.lat,v.lon],{radius:v.range,color:'#b85e00',fillOpacity:0.12,weight:2}).addTo(selection);
    L.circleMarker([v.lat,v.lon],{radius:7,color:'#b85e00',fillOpacity:1}).addTo(selection);
    if(pan)map.panTo([v.lat,v.lon]);
  }
  function draw(rows,fit=false) {
    points=rows.map(row=>({...row,...positionOf(row)}));layer.clearLayers();
    // One clickable marker per unique location; timeline retains every observation.
    const marked=new Set();points.forEach((p,i)=>{
      if(!p.position)return;const key=p.position.lat+','+p.position.lon;if(marked.has(key))return;marked.add(key);
      L.circleMarker([p.position.lat,p.position.lon],{radius:4,color:'#087e8b',fillOpacity:0.8}).on('click',()=>select(i)).addTo(layer);
    });
    const found=points.filter(p=>p.position);
    el('coverage').textContent='Измерений: '+points.length+' · с координатами: '+found.length+' · без координат: '+(points.length-found.length)+' · точек на карте: '+marked.size;
    slider.disabled=!points.length;slider.max=Math.max(0,points.length-1);
    select(Math.min(Number(slider.value),Math.max(0,points.length-1)));
    if(fit&&found.length)map.fitBounds(found.map(p=>[p.position.lat,p.position.lon]),{padding:[35,35],maxZoom:11});
  }
  async function post(path,values,signal) {
    const response=await fetch(root.dataset.base+path,{method:'POST',body:new URLSearchParams({csrf:root.dataset.csrf,...values}),signal,credentials:'same-origin'});
    if(response.redirected)throw new Error('Сессия завершена. Войдите в админку заново.');
    if(!response.ok)throw new Error(await response.text());
    return response.json();
  }
  el('form').addEventListener('submit',async event=>{
    event.preventDefault();if(controller)return;
    const from=new Date(start.value),to=new Date(end.value);
    if(!Number.isFinite(+from)||!Number.isFinite(+to)||to<=from||to-from>7*86400000){status.textContent='Выберите период от одной минуты до 7 суток.';return;}
    controller=new AbortController();const signal=controller.signal;
    el('build').disabled=true;el('stop').disabled=false;start.disabled=true;end.disabled=true;
    let rows=[];
    try {
      ensureMap();locations=new Map();draw([]);status.textContent='Загрузка истории…';
      rows=await post('mdm/track',{id:root.dataset.device,start:from.toISOString(),end:to.toISOString()},signal);
      rows=rows.map(row=>({...row,cells:candidateCells(row.cells)}));
      const unique=new Map();rows.forEach(row=>row.cells.forEach(c=>unique.set(cellKey(c),c)));
      if(unique.size>1000)throw new Error('В периоде более 1000 уникальных сот. Выберите меньший период.');
      draw(rows);let first=true;
      do {
        const saved=await post('mdm/cell-db/positions',{cells:JSON.stringify([...unique.values()])},signal);
        locations=new Map(Object.entries(saved.positions));
        draw(rows,first);first=false;
        const found=[...locations.values()].filter(Boolean).length;
        status.textContent='База: '+found+' / '+unique.size+' сот с координатами · ожидают фоновой обработки: '+saved.pending;
        if(!saved.pending)break;
        await new Promise((resolve,reject)=>{
          const cancel=()=>{clearTimeout(timer);reject(new Error('Остановлено'));};
          const timer=setTimeout(()=>{signal.removeEventListener('abort',cancel);resolve();},5000);
          signal.addEventListener('abort',cancel,{once:true});
          if(signal.aborted)cancel();
        });
      } while(!signal.aborted);
      if(!rows.length)status.textContent='Нет измерений за выбранный период.';
    } catch(error) {if(map)draw(rows,true);status.textContent=signal.aborted?'Автообновление остановлено. Фоновая обработка на сервере продолжается.':String(error.message || error);}
    finally {controller=null;el('build').disabled=false;el('stop').disabled=true;start.disabled=false;end.disabled=false;}
  });
  el('stop').addEventListener('click',()=>controller?.abort());
  slider.addEventListener('input',()=>select(Number(slider.value),true));
  el('fit').addEventListener('click',()=>{const found=points.filter(p=>p.position);if(map&&found.length)map.fitBounds(found.map(p=>[p.position.lat,p.position.lon]),{padding:[35,35],maxZoom:11});});
})();
