(()=>{'use strict';
const zone=Intl.DateTimeFormat().resolvedOptions().timeZone;
for(const node of document.querySelectorAll('time[data-local-time]')){const date=new Date(node.dateTime);if(!Number.isFinite(date.getTime()))continue;node.textContent=date.toLocaleString(undefined,{year:'numeric',month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',second:'2-digit',hour12:false});node.title='Исходное время: '+node.dateTime;}
const note=document.querySelector('#research-timezone');if(note)note.textContent='Время — в часовом поясе браузера: '+zone+'. Исходные метки в JSONL и JSON — UTC.';
const pad=n=>String(n).padStart(2,'0');
for(const input of document.querySelectorAll('input[data-local-filter]')){const original=input.value,date=original?new Date(original):null;if(date&&!Number.isFinite(date.getTime()))continue;const hidden=document.createElement('input');hidden.type='hidden';hidden.name=input.name;hidden.value=original;input.removeAttribute('name');input.after(hidden);input.type='datetime-local';input.step='1';input.value=date?date.getFullYear()+'-'+pad(date.getMonth()+1)+'-'+pad(date.getDate())+'T'+pad(date.getHours())+':'+pad(date.getMinutes())+':'+pad(date.getSeconds()):'';const initial=input.value;input.previousElementSibling.textContent=(hidden.name==='from'?'С':'До')+' · местное время';input.form.addEventListener('submit',()=>{hidden.value=input.value===initial?original:input.value?new Date(input.value).toISOString():'';});}

const periods={'5m':300,'15m':900,'60m':3600,'6h':21600,'12h':43200,'24h':86400,'2d':172800,'7d':604800,'14d':1209600};
const picker=document.querySelector('#quick-periods'),filter=document.querySelector('input[data-local-filter]')?.form;
if(picker&&filter){
 const params=new URLSearchParams(location.search);let selected=params.get('period')||'';
 const from=Date.parse(params.get('from')||''),to=Date.parse(params.get('to')||'');if(!periods[selected]||!Number.isFinite(from)||!Number.isFinite(to)||Math.abs(to-from-periods[selected]*1000)>1000)selected='';
 const periodField=document.createElement('input');periodField.type='hidden';periodField.name='period';periodField.value=selected;filter.append(periodField);
 const refresh=document.querySelector('#refresh-period');
 function mark(){for(const button of picker.querySelectorAll('[data-period]'))button.setAttribute('aria-pressed',String(button.dataset.period===selected));refresh.hidden=!selected;}
 function choose(key){const values=new URLSearchParams(new FormData(filter));const now=new Date();values.delete('cursor');values.set('period',key);values.set('from',new Date(now.getTime()-periods[key]*1000).toISOString());values.set('to',now.toISOString());location.assign(filter.action+'?'+values.toString());}
 for(const button of picker.querySelectorAll('[data-period]'))button.onclick=()=>choose(button.dataset.period);
 refresh.onclick=()=>{if(selected)choose(selected);};
 for(const input of filter.querySelectorAll('input[data-local-filter]'))input.addEventListener('input',()=>{selected='';periodField.value='';mark();});
 mark();picker.hidden=false;

}
})();
