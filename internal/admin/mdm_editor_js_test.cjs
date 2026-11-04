const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
class Element {
 constructor(tag='div'){this.tag=tag;this.children=[];this.value='';this.textContent='';this.dataset={};this.disabled=false;this.hidden=false;this.className='';}
 append(...items){this.children.push(...items);if(this.tag==='select'&&!this.value)this.value=items[0]?.value||'';}
 prepend(...items){this.children.unshift(...items);}
 replaceChildren(...items){this.children=[];if(this.tag==='select')this.value='';this.append(...items);}
 setAttribute(){} addEventListener(){} get options(){return this.children;}
}
const html=fs.readFileSync(__dirname+'/mdm_phone.html','utf8');
const elements=Object.fromEntries([...html.matchAll(/<(\w+)[^>]*\sid="([^"]+)"/g)].map(m=>[m[2],new Element(m[1])]));
elements['mdm-editor'].dataset={phone:'true',base:'/'};elements.telemetry.hidden=true;
const config={version:1,profiles:[{id:'default',name:'Original',settings:{transport:'quic',mode:0}}],currentProfileId:'default',enabledProfileIds:['default'],multiple:false,dns:{mode:'system',profileId:'default'},budget:{limitBytes:0},diagnostics:{enabled:false,detailed:false},globalApps:[]};
let state={binding:{active:true,grantedRights:{config:true}},userConfigEdit:{enabled:true,revision:2},editable:true,report:{configuration:config,inventory:{entries:[{packageId:'org.example.app',label:'Example'}],complete:true},capabilities:['web-control-v1'],configGeneration:9,appliedRevision:0}},reload,failSave=false,failImport=false,seq=0;
const calls=[],flatten=e=>[e,...e.children.flatMap(flatten)],text=e=>e.textContent+e.children.map(text).join(' ');
const controls=()=>[...flatten(elements.configuration),...flatten(elements['app-list'])].filter(e=>['input','select','textarea','button'].includes(e.tag));
const window={addEventListener(){},MdmNative:{request(id,op,raw){const body=JSON.parse(raw);calls.push({op,body});let result;if(op==='read')result={ok:true,value:state};else if(op==='import'){const candidate=JSON.parse(JSON.stringify(body.document));candidate.profiles[0].name='From bundle';result=failImport?{ok:false,error:'Import canceled'}:{ok:true,value:{candidate,missingCredentials:[],sections:['profiles']}};}else result=failSave?{ok:false,error:'Connection lost'}:{ok:true,value:{revision:1}};queueMicrotask(()=>window.mdmEditorResult(id,JSON.stringify(result)));}}};
const context={window,document:{querySelector:s=>elements[s.slice(1)],querySelectorAll:s=>s==='[data-tab]'?[]:controls(),createElement:t=>new Element(t),createTextNode:t=>{const n=new Element();n.textContent=t;return n;}},crypto:{randomUUID:()=>`request-${++seq}`},confirm:()=>true,setInterval:fn=>reload=fn,Date,URLSearchParams,fetch:()=>{throw Error('Phone must not fetch directly');}};
vm.runInNewContext(fs.readFileSync(__dirname+'/mdm_editor.js','utf8'),context);
const tick=()=>new Promise(setImmediate);
const editName=value=>{const label=flatten(elements.configuration).find(e=>e.tag==='label'&&e.textContent==='Название подключения');label.children[0].value=value;label.children[0].onchange();};
(async()=>{
 await tick();assert.equal(elements.apply.disabled,false);assert.doesNotMatch(text(elements.configuration),/Режим конфигурации|Разрешить пользователю выбирать/);
 editName('Phone draft');assert.equal(window.mdmEditorDirty(),true);assert.equal(config.profiles[0].name,'Original');
 failSave=true;await elements.apply.onclick();const first=calls.at(-1).body;assert.equal(first.policyRevision,2);assert.equal(first.expectedGeneration,9);assert.equal(first.document.profiles[0].name,'Phone draft');assert.equal(first.bindingId,undefined);assert.equal(first.mode,undefined);
 assert.equal(window.mdmEditorDirty(),true);assert.equal(elements.apply.disabled,false);
 failSave=false;await elements.apply.onclick();assert.equal(calls.at(-1).body.requestId,first.requestId,'lost reply uses same request');assert.equal(elements.apply.disabled,true);assert.match(elements.status.textContent,/Ожидаем применения/);assert.equal(config.profiles[0].name,'Original');
 state={...state,desired:{revision:1,document:first.document},report:{...state.report,configGeneration:10,appliedRevision:1}};reload();await tick();assert.equal(elements.apply.disabled,false);assert.match(elements.status.textContent,/Применена ревизия 1/);
 editName('Stale draft');state={...state,editable:false,userConfigEdit:{enabled:false,revision:3}};reload();await tick();assert.equal(elements.apply.disabled,true);assert.ok(controls().every(c=>c.disabled));
 elements['app-search'].value='';elements['app-search'].oninput();assert.ok(controls().every(c=>c.disabled),'app list rebuild must preserve read-only permission');
 state={...state,editable:true,userConfigEdit:{enabled:true,revision:4}};reload();await tick();failSave=true;await elements.apply.onclick();assert.equal(calls.at(-1).body.policyRevision,2,'regrant never silently blesses old draft');
 // A clean draft opened before a grant must use the new permission revision.
 elements.reload.onclick();await tick();state={...state,userConfigEdit:{enabled:true,revision:5}};reload();await tick();editName('Fresh permitted edit');await elements.apply.onclick();assert.equal(calls.at(-1).body.policyRevision,5,'clean draft must adopt current grant');
 const add=flatten(elements.configuration).find(e=>e.tag==='button'&&e.textContent==='Добавить подключение');add.onclick();
 const keyLabels=flatten(elements.configuration).filter(e=>e.tag==='label'&&e.textContent==='Закрытый ключ PEM');const key=keyLabels.at(-1).children[0];key.value='PRIVATE-KEY-MARKER';key.onchange();elements.preview.onclick();assert.doesNotMatch(elements.diff.textContent,/PRIVATE-KEY-MARKER/,'new profile preview must redact identity recursively');
 const saves=calls.filter(x=>x.op==='save').length;await elements['phone-import'].onclick();assert.equal(calls.filter(x=>x.op==='save').length,saves,'import must not save');assert.equal(window.mdmEditorDirty(),true);assert.match(elements.diff.textContent,/From bundle/);assert.doesNotMatch(elements.diff.textContent,/PRIVATE-KEY-MARKER/);
 failImport=true;await elements['phone-import'].onclick();assert.equal(elements.apply.disabled,false);assert.match(elements.message.textContent,/Import canceled/);await elements.apply.onclick();assert.equal(calls.at(-1).body.document.profiles[0].name,'From bundle','cancel preserves draft');assert.equal(calls.at(-1).body.policyRevision,5);
 console.log('PASS phone forms, isolated draft, CAS, idempotent retry, applied status, revoke/regrant fencing');
})().catch(e=>{console.error(e);process.exitCode=1});
