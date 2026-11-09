const test = require('node:test');
const assert = require('node:assert/strict');
const {candidateCells, cellKey} = require('./mdm_track.js');
const fs = require('node:fs');
const vm = require('node:vm');

function openTrackPage(href,instant='2026-11-01T17:00:00Z') {
 const fields = new Map();
 const location = {href,reloads:0,reload() {this.reloads++;}};
 const history = {state: {other: 'preserved'}, replaceState(state, title, url) {this.state=state;location.href=String(url);}};
 const document = {getElementById(id) {
  if (!fields.has(id)) fields.set(id, {value:'',dataset:{},listeners:{},addEventListener(event,listener) {this.listeners[event]=listener;}});
  return fields.get(id);
 }};
 const now = Date.parse(instant);
 class Clock extends Date {constructor(...args) {super(...(args.length ? args : [now]));} static now() {return now;}}
 vm.runInNewContext(fs.readFileSync(require.resolve('./mdm_track.js'),'utf8'), {document,window:{location,history},URL,Date:Clock,Intl});
 return {start:fields.get('track-start'),end:fields.get('track-end'),refresh:fields.get('mdm-refresh'),location,history};
}

test('fresh map opens the last seven days in local time, including across DST',()=>{
 const originalTZ=process.env.TZ;
 try {
  for (const [zone,start,end] of [['Europe/Moscow','2026-10-25T20:00','2026-11-01T20:00'],['America/New_York','2026-10-25T13:00','2026-11-01T12:00']]) {
   process.env.TZ=zone;
   const page=openTrackPage('https://example.test/lab/mdm?device=one#cell-track');
   assert.equal(page.start.value,start);
   assert.equal(page.end.value,end);
  }
 } finally {if(originalTZ===undefined)delete process.env.TZ;else process.env.TZ=originalTZ;}
});

test('edited dates survive reload before submitting and preserve device, hash and history state',()=>{
 const page=openTrackPage('https://example.test/lab/mdm?device=one&other=keep#cell-track');
 page.start.value='2026-10-14T08:30';
 page.end.value='2026-10-18T23:45';
 page.start.listeners.input?.();
 page.end.listeners.input?.();
 const refreshed=openTrackPage(page.location.href);
 assert.equal(refreshed.start.value,'2026-10-14T08:30');
 assert.equal(refreshed.end.value,'2026-10-18T23:45');
 assert.equal(page.refresh.href,page.location.href);
 const url=new URL(page.location.href);
 assert.equal(url.searchParams.get('device'),'one');
 assert.equal(url.searchParams.get('other'),'keep');
 assert.equal(url.hash,'#cell-track');
 assert.deepEqual(page.history.state,{other:'preserved'});
});

test('fresh default remains within the API limit when its start falls in a repeated DST hour',()=>{
 const originalTZ=process.env.TZ;
 try {
  process.env.TZ='America/New_York';
  const page=openTrackPage('https://example.test/lab/mdm?device=one#cell-track','2026-11-08T06:30:00Z');
  const duration=new Date(page.end.value)-new Date(page.start.value);
  assert.ok(duration>0 && duration<=7*86400000,`default interval was ${duration/3600000} hours`);
 } finally {if(originalTZ===undefined)delete process.env.TZ;else process.env.TZ=originalTZ;}
});

test('Refresh reloads the document even when its URL includes the current map fragment',()=>{
 const page=openTrackPage('https://example.test/lab/mdm?device=one#cell-track');
 page.refresh.listeners.click?.({preventDefault() {}});
 assert.equal(page.location.reloads,1);
});
test('different radios never share cache identity; unsupported/stale cells stay unknown',()=>{
 const c={technology:'LTE',registered:true,mcc:'250',mnc:'99',tac:'42',ci:'123',ageMillis:0};
 assert.notEqual(cellKey(c),cellKey({...c,technology:'GSM'}));
 assert.equal(candidateCells([c,{...c,technology:'NR'},{...c,registered:false},{...c,ageMillis:300000}]).length,1);
});
