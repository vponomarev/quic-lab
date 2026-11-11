# QUIC Lab — Linux-клиент

Один Go-бинарник `quic-lab` содержит окно Fyne, VPN-движок, локальный контроллер
и встроенный сетевой helper. Python/Go/.NET для запуска не нужны.
Нужны Linux x86_64, systemd/journalctl, iproute2, `/dev/net/tun`, графический
сеанс X11/XWayland и системные библиотеки OpenGL/X11/Wayland-client.
Текущая сборка требует glibc 2.38 или новее; проверена Ubuntu 24.04.5.
Перед установкой выполните `ldd ./quic-lab`: строк `not found` быть не должно.
Архив содержит лицензии runtime-зависимостей, шрифтов и их `licenses/MODULES.json`.

## Обновление настроенного клиента

Закройте старое окно (VPN продолжит работу), затем из распакованного архива:

```sh
chmod +x ./quic-lab
sudo ./quic-lab install --user "$USER"
quic-lab
```

Работающий VPN кратковременно перезапускается. Профили, выбранное подключение,
VPN unit и политика автозапуска сохраняются. Установка проверяет готовность
и откатывается при ошибке. Резервные копии: `/var/backups/quic-lab-client/`.
Имена `quic-lab-client` и `quic-lab-gui` становятся ссылками на единый бинарник.
Распознанный старый DNS/IPv6 helper также заменяется ссылкой; чужой helper не
перезаписывается. Системные Python/GTK пакеты не удаляются.

## Первая установка

Это preview управления готовыми профилями, без редактора/импорта в окне.
Администратор сначала готовит профиль в формате Linux-клиента: см.
[форматы QUIC/HTTPS/AWG/VLESS и маршруты](linux-client-preview.md).
`client.example.json` содержит заглушки и не подключится без ваших параметров.
Не публикуйте готовый профиль: в нём приватные ключи.

После заполнения собственного `client.json` для QUIC проверьте и установите:

```sh
chmod 600 ./client.json
./quic-lab engine -validate -config ./client.json
sudo install -d -m 700 /etc/quic-lab /etc/quic-lab/profiles
sudo install -o root -g root -m 600 ./client.json /etc/quic-lab/profiles/quic.json
sudo ln -s profiles/quic.json /etc/quic-lab/client.json
sudo install -o root -g root -m 755 ./quic-lab /usr/local/bin/quic-lab-client
sudo install -o root -g root -m 644 ./quic-lab-client.service /etc/systemd/system/quic-lab-client.service
sudo systemctl daemon-reload
sudo ./quic-lab install --user "$USER"
quic-lab
```

Эти команды предназначены для новой установки: существующие файлы и unit не
заменяйте ими. Для других транспортов используйте соответствующее имя профиля
`https.json`, `awg.json` или `vless.json` и ссылку на него. Тип транспорта в JSON
должен совпадать с именем. Безопасные профили появятся в списке автоматически.

Свежая установка сама не включает VPN при загрузке. Подключитесь кнопкой в
окне и при необходимости включите флажок автозапуска. Системные DNS и IPv6
для новой машины требуют отдельной настройки администратором: поставочный
базовый unit не подключает ноутбучный helper и не меняет эти настройки.
Полная блокировка обходного трафика и IPv6 VPN в этот preview не входят.

## Управление и диагностика

[Памятка GUI, RTT и границы проверки](linux-client-gui.md).
Открывайте QUIC Lab из меню приложений или командой `quic-lab`, без sudo.
Закрытие окна не отключает VPN. Смена транспорта разрывает текущие соединения.
`quic-lab status` выводит безопасный статус; административный журнал:
`sudo journalctl -u quic-lab-client -n 60`.

Сборка из исходников: `scripts/build-linux-client-release.py --version VERSION
--revision COMMIT` на Linux с Go из go.mod, Python3 (только сборка), C-компилятором
и графическими заголовками, перечисленными в linux-client-gui.md.
