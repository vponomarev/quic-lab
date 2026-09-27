(() => {
 'use strict';
 const valid=['windows','arm64','amd64'];
 const defaults=()=>({platform:/Mac/.test(navigator.platform)?'arm64':'windows',helper:'',wireshark:''});
 const key=base=>'quiclab.capture.settings.v1:'+base;
 function load(base){try{const v=JSON.parse(localStorage.getItem(key(base))||'{}');return {platform:valid.includes(v.platform)?v.platform:defaults().platform,helper:typeof v.helper==='string'?v.helper:'',wireshark:typeof v.wireshark==='string'?v.wireshark:''};}catch(e){return defaults();}}
 function command(base,url){const v=load(base),win=v.platform==='windows';const quote=s=>win?"'"+s.replaceAll("'","''")+"'":"'"+s.replaceAll("'","'\"'\"'")+"'";const helper=v.helper||(win?'.\\quic-lab-capture-cli.exe':'./quic-lab-capture-darwin-'+v.platform);return (win?'& ':'')+quote(helper)+' -connect '+quote(url)+(v.wireshark?' -wireshark '+quote(v.wireshark):'');}
 window.QuicCaptureSettings={load,command};
 const form=document.getElementById('capture-settings-form');if(!form)return;
 const platform=document.getElementById('settings-platform'),helper=document.getElementById('settings-helper'),shark=document.getElementById('settings-wireshark'),status=document.getElementById('settings-status');
 const v=load(form.dataset.base);platform.value=v.platform;helper.value=v.helper;shark.value=v.wireshark;
 function hint(){helper.placeholder=platform.value==='windows'?'C:\\Tools\\quic-lab-capture-windows\\quic-lab-capture-cli.exe':'/Users/name/Downloads/quic-lab-capture-darwin-'+platform.value;shark.placeholder=platform.value==='windows'?'C:\\Program Files\\Wireshark\\Wireshark.exe':'/Applications/Wireshark.app/Contents/MacOS/Wireshark';}
 platform.onchange=hint;hint();
 form.onsubmit=e=>{e.preventDefault();try{const h=helper.value.trim(),w=shark.value.trim();if(/[\r\n\0]/.test(h+w))throw Error('Путь должен быть одной строкой.');if(/^['"]|['"]$/.test(h)||/^['"]|['"]$/.test(w))throw Error('Введите пути без внешних кавычек.');localStorage.setItem(key(form.dataset.base),JSON.stringify({platform:platform.value,helper:h,wireshark:w}));status.textContent='Сохранено для этого браузера';}catch(e){status.textContent=e.message;}};
})();
