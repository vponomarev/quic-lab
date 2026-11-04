const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
class Element {
 constructor(tag='div'){this.tag=tag;this.children=[];this.value='';this.textContent='';this.dataset={};this.disabled=false;this.hidden=false;this.className='';}
 append(...items){this.children.push(...items);if(this.tag==='select'&&!this.value)this.value=items[0]?.value||'';}
 replaceChildren(...items){this.children=[];if(this.tag==='select')this.value='';this.append(...items);}
}
const html=fs.readFileSync(__dirname+'/mdm_probe.html','utf8');
const elements=Object.fromEntries([...html.matchAll(/<(\w+)[^>]*\sid=\x22([^\x22]+)\x22/g)].map(m=>[m[2],new Element(m[1])]));
elements.probe.dataset={base:'/',id:'device',csrf:'fixture'};elements.filter.value='all';elements.sort.value='order';
const row={id:'node',name:'Node',endpoint:'example.org:443',outcome:'success',relayOutcome:'not_applicable'};
let state={binding:{active:true,grantedRights:{vpn:true,telemetry:true}},report:{capabilities:['vless_probe_v1','vless_probe_phases_v2'],probeSources:[{id:'source',name:'Source',fingerprint:'fp'}]},probes:{source:{schema:2,runId:'run',network:'cell',initiator:'local',state:'finished',startedAt:'2026-11-04T00:00:00Z',phases:{wifi:{network:'wifi',state:'finished',rows:[row]},cell:{network:'cell',state:'skipped',reason:'permission_denied',rows:[{...row,outcome:'permission_denied'}]}}}}};
let reload;
const context={document:{querySelector:s=>elements[s.slice(1)],createElement:t=>new Element(t),addEventListener(){},hidden:false},fetch:async()=>({ok:true,json:async()=>state}),setInterval:fn=>{reload=fn},URLSearchParams,crypto:{randomUUID:()=> 'request'},Date};
vm.runInNewContext(fs.readFileSync(__dirname+'/mdm_probe.js','utf8'),context);
const text=e=>e.textContent+e.children.map(text).join(' ');
(async()=>{
 await new Promise(setImmediate);
 assert.equal(elements.network.children.length,2,'two network choices');
 assert.equal(elements.rows.children.length,1,'selected skipped phase visible');
 assert.match(elements.notice.textContent,/Нет разрешения/);
 assert.equal(elements.rows.children[0].className,'','skip must be neutral');
 assert.equal(elements.retry.disabled,true,'v1 retry cannot address local v2 run');
 elements.network.value='wifi';elements.network.onchange();assert.match(text(elements.rows),/Работает/);
 await reload();assert.equal(elements.network.value,'wifi','selection survives refresh');
 elements.network.value='cell';elements.network.onchange();elements.filter.value='failed';elements.filter.onchange();assert.equal(elements.rows.children.length,0,'permission skip is not failed node');
 elements.filter.value='pending';elements.filter.onchange();assert.equal(elements.rows.children.length,1,'permission skip is untested');
 state.probes.source.state='running';await reload();assert.equal(elements.cancel.disabled,true,'v1 cancel cannot address v2 local run');
 state.probes.source={runId:'legacy',state:'finished',network:'wifi',rows:[row]};elements.filter.value='all';await reload();assert.equal(elements.network.hidden,true);assert.equal(elements.retry.disabled,false);assert.match(text(elements.rows),/Работает/);
 console.log('PASS phase selection, refresh, neutral filters, command scope and legacy rendering');
})().catch(e=>{console.error(e);process.exitCode=1});
