(() => {
 'use strict';
 function wire(dialog) {
  dialog.querySelector('[data-close]').addEventListener('click', () => dialog.close());
  dialog.addEventListener('click', event => {
   if (event.target !== dialog) return;
   const b = dialog.getBoundingClientRect();
   if (event.clientX < b.left || event.clientX > b.right || event.clientY < b.top || event.clientY > b.bottom) dialog.close();
  });
 }
 document.querySelectorAll('.user-actions details').forEach(details => {
  const wrapper = document.createElement('div'); wrapper.className = details.className;
  const trigger = document.createElement('button'); trigger.type = 'button'; trigger.className = 'secondary modal-trigger';
  trigger.textContent = details.querySelector('summary').textContent;
  const dialog = document.createElement('dialog');
  const header = document.createElement('header'); header.className = 'modal-header';
  const title = document.createElement('h2'); title.textContent = trigger.textContent;
  title.id = 'modal-' + Math.random().toString(36).slice(2); dialog.setAttribute('aria-labelledby', title.id);
  const close = document.createElement('button'); close.type = 'button'; close.className = 'modal-close'; close.dataset.close = ''; close.setAttribute('aria-label', 'Закрыть'); close.textContent = '×';
  header.append(title, close);
  const body = document.createElement('div'); body.className = 'modal-body';
  const name = document.createElement('p'); name.className = 'client-name'; name.textContent = details.closest('tr').querySelector('[data-stat="name"]').textContent; body.append(name);
  details.querySelector('summary').remove();
  body.append(...details.childNodes); dialog.append(header, body); wrapper.append(trigger, dialog); details.replaceWith(wrapper);
  trigger.addEventListener('click', () => {name.textContent = wrapper.closest('tr').querySelector('[data-stat="name"]').textContent; dialog.showModal();});
  wire(dialog);
  const qrResult = document.createElement('div'); body.append(qrResult);
  let generation = 0;
  dialog.addEventListener('close', () => {generation++; qrResult.replaceChildren();});
  dialog.querySelectorAll('form[action$="/qr"], form[action$="/awg-qr"]').forEach(form => {
   form.addEventListener('submit', async event => {
    event.preventDefault(); const attempt = ++generation;
    const button = form.querySelector('button'); button.disabled = true; qrResult.textContent = 'Готовим QR…';
    try {
     const response = await fetch(form.action, {method:'POST', body:new URLSearchParams(new FormData(form)), credentials:'same-origin'});
     const doc = new DOMParser().parseFromString(await response.text(), 'text/html');
     const qr = doc.querySelector('.card.qr');
     if (!response.ok || !qr) throw new Error('Не удалось получить QR. Обновите страницу и проверьте вход.');
     if (attempt === generation && dialog.open) {qrResult.replaceChildren(document.importNode(qr, true)); qrResult.scrollIntoView({block:'nearest'});}
    } catch(error) {if(attempt === generation && dialog.open) qrResult.textContent = error.message;}
    finally {button.disabled = false;}
   });
  });
 });
 const help = document.getElementById('stats-help');
 if (help) {wire(help); document.querySelector('[data-help]').addEventListener('click', () => help.showModal());}
 const login = document.querySelector('.login-page');
 if(login) document.querySelector('main > h1').hidden = true;
})();
