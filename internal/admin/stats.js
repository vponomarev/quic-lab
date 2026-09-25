(() => {
  'use strict';
  const table = document.getElementById('user-stats');
  const status = document.getElementById('live-status');
  if (!table || !status) return;
  const url = new URL(table.dataset.liveUrl, location.href);
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
  let socket, retry, leaving = false;
  function lines(element, text) {
    element.replaceChildren();
    text.split('\n').forEach((line, index) => {
      if (index) element.append(document.createElement('br'));
      element.append(document.createTextNode(line));
    });
  }
  function connect() {
    if (leaving) return;
    status.textContent = 'Подключение к обновлениям…';
    socket = new WebSocket(url);
    socket.onmessage = event => {
      const users = JSON.parse(event.data);
      const rows = new Map(Array.from(table.querySelectorAll('[data-user-id]'), row => [row.dataset.userId, row]));
      let changed = users.length !== rows.size;
      users.forEach(user => {
        const row = rows.get(user.id);
        if (!row) { changed = true; return; }
        row.querySelector('[data-stat="name"]').textContent = user.name;
        const editor = row.querySelector('details.rename');
        if (editor && !editor.open) editor.querySelector('input[name="name"]').value = user.name;
        const connections = row.querySelector('[data-stat="connections"]');
        lines(connections, user.connections.length ? 'Онлайн · ' + user.connections.length + '\n' + user.connections.join('\n\n') : 'Офлайн');
        connections.className = user.connections.length ? 'online' : 'muted';
        lines(row.querySelector('[data-stat="last"]'), user.last);
        lines(row.querySelector('[data-stat="traffic"]'), '↑ TX ' + user.tx + '\n↓ RX ' + user.rx + (user.rate ? '\n' + user.rate : ''));
      });
      status.textContent = changed ? 'Список пользователей изменился — обновите страницу' : 'Онлайн · каждые 5 секунд · обновлено ' + new Date().toLocaleTimeString();
    };
    socket.onclose = event => {
      if (leaving) return;
      if (event.code === 1008) {
        status.textContent = 'Сессия завершена — обновите страницу и войдите снова';
        return;
      }
      status.textContent = 'Нет связи с сервером — повторное подключение…';
      retry = setTimeout(connect, 3000);
    };
  }
  addEventListener('pagehide', () => { leaving = true; clearTimeout(retry); if (socket) socket.close(); });
  addEventListener('pageshow', event => { if (event.persisted) { leaving = false; connect(); } });
  connect();
})();
