# Diva Remote — удалённый Windows в браузере

Diva — self-hosted система удалённого рабочего стола: на Windows работает
агент, а оператору нужен только современный браузер. H.264-видео и ввод идут
**напрямую по WebRTC/UDP** между браузером и Windows. Docker-сервер передаёт
только служебные SDP/ICE-сообщения и раздаёт веб-интерфейс — видеопоток через
него не проходит.

Сейчас это пригодный для личного использования MVP, а не полный клон Parsec.
Есть захват всего виртуального рабочего стола или выбранной прямоугольной
области/монитора, 60+ FPS, программный и аппаратный H.264, клавиатура, мышь и
полноэкранный режим. Пока нет звука, геймпада, буфера обмена, передачи файлов,
TURN, адаптивного битрейта и работы на заблокированном экране Windows.

## 1. Что потребуется

### Сервер сигнализации

* Linux/VPS с Docker Engine и Docker Compose v2;
* домен, например `remote.example.com`, с A-записью на публичный IP сервера;
* открытые TCP 80 и TCP/UDP 443 (UDP 443 нужен Caddy для HTTP/3, не для видео).

Сервер может находиться где угодно и почти не расходует трафик. Caddy в составе
проекта автоматически получает и обновляет TLS-сертификат. Доступ по голому IP
не рекомендуется: публичный доверенный TLS-сертификат обычно выдаётся на домен.

> Ошибка Caddy `NXDOMAIN looking up A/AAAA` означает, что указанного имени ещё
> нет в публичной DNS. До запуска контейнеров обязательно выполните отдельную
> инструкцию **[Настройка домена и DNS](docs/domain-setup.md)**. Там приведены
> точные A/AAAA/CNAME/CAA записи, настройка Cloudflare, firewall, команды проверки
> и разбор ошибок ACME/Let's Encrypt.

Если в логе одновременно видны Cloudflare IP `2606:4700:...`, HTTP status `522`
и ошибка `Cannot negotiate ALPN protocol "acme-tls/1"`, запись включена в режим
Cloudflare Proxy, но Cloudflare не может подключиться к origin. Самое быстрое
исправление — переключить запись `remote` в **DNS only**. Если proxy необходимо
оставить, используйте описанный в DNS-гайде режим DNS-01 с API token.

### Управляемый Windows-компьютер

* Windows 10/11 x64, Go 1.23+ для сборки и FFmpeg с H.264 encoder;
* прямой публичный IPv4 **или** проброс одного UDP-порта с роутера;
* незаблокированная интерактивная пользовательская сессия;
* NVIDIA/Intel/AMD encoder желателен, но `libx264` тоже работает.

Проверить внешний IPv4 можно на любом сервисе «мой IP». Если адрес WAN в
настройках роутера отличается от внешнего адреса или принадлежит диапазонам
`10.0.0.0/8`, `100.64.0.0/10`, `172.16.0.0/12`, `192.168.0.0/16`, у вас CGNAT.
В таком случае запросите белый IP у провайдера: TURN в этой версии отсутствует.

## 2. Установка Docker-сервера

```bash
git clone <URL-ЭТОГО-РЕПОЗИТОРИЯ> diva
cd diva
cp .env.example .env
openssl rand -hex 32
```

Откройте `.env`, укажите домен и вставьте сгенерированную строку:

```dotenv
DIVA_ADDRESS=remote.example.com
DIVA_TOKEN=64_случайных_шестнадцатеричных_символа
```

Не копируйте `remote.example.com` буквально: это пример. Значение
`DIVA_ADDRESS` должно полностью совпадать с созданной DNS-записью. Например, для
зоны `houwy.dev` сначала создайте запись `A` с именем `remote`, дождитесь ответа
`dig remote.houwy.dev A`, и лишь затем укажите `DIVA_ADDRESS=remote.houwy.dev`.

Запустите автоматическую предварительную проверку DNS (второй аргумент —
ожидаемый публичный IPv4 VPS):

```bash
./scripts/check-domain.sh remote.example.com 203.0.113.20
```

Запустите и проверьте контейнеры:

```bash
docker compose pull
docker compose up -d --build
docker compose ps
docker compose logs -f --tail=100
```

Файлы `go.mod` и `go.sum` уже находятся в репозитории. Не удаляйте `go.sum`:
Docker-сборка использует `-mod=readonly` и проверяет зафиксированные контрольные
суммы зависимостей перед компиляцией.

После выпуска сертификата откройте `https://remote.example.com`. Обновление:

```bash
git pull
docker compose up -d --build
```

Данные сертификата находятся в named volume `caddy_data`. Веб-приложение не
использует БД. Для резервной копии достаточно сохранить `.env`; токен нельзя
публиковать или коммитить.

## 3. Подготовка Windows

1. Установите [Go](https://go.dev/dl/) и FFmpeg. Убедитесь, что существуют
   `go.exe` и `C:\ffmpeg\bin\ffmpeg.exe`.
2. Скопируйте/клонируйте репозиторий на Windows.
3. В обычном PowerShell соберите агент:

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\scripts\build-agent.ps1
```

4. Проверьте доступные H.264-кодировщики:

```powershell
C:\ffmpeg\bin\ffmpeg.exe -hide_banner -encoders | Select-String h264
```

Рекомендуется `-Encoder auto`: агент последовательно проверит `h264_amf`,
`h264_nvenc`, `h264_qsv` и гарантированно доступный программный `libx264`.
Явно выбранный аппаратный encoder при ошибке также автоматически переключается
на `libx264`. Наличие имени в `ffmpeg -encoders` ещё не означает, что GPU и
драйвер смогут его инициализировать.

## 4. Сеть Windows

По умолчанию агент слушает WebRTC на UDP 50000. Если Windows подключена прямо к
Интернету, установочный скрипт сам добавит правило Windows Firewall. Если между
ней и Интернетом есть роутер, создайте port forwarding:

```text
protocol: UDP
external port: 50000
internal IP: постоянный LAN-адрес Windows, например 192.168.1.50
internal port: 50000
```

Параметру `PublicIP` всё равно передаётся **внешний белый IPv4 роутера**, а не
`192.168.x.x`. Закрепите LAN-адрес компьютера через DHCP reservation.

Diva также использует STUN `stun.cloudflare.com:3478` на агенте и в браузере.
STUN не принимает видеопоток и не является relay: он только сообщает peer'ам
видимый NAT-адрес. Его можно отключить пустым `-STUN ""` и пустым
`DIVA_STUN_URL`, но для компьютера за роутером это не рекомендуется.

## 5. Установка и автозапуск агента

Сначала убедитесь, что используется актуальный installer v2:

```powershell
git pull --ff-only
.\scripts\diagnose-windows.ps1
```

В выводе должно быть `Installer version: 2.0.0`. Если выводится
`LEGACY/UNKNOWN`, локальная копия устарела (это особенно часто происходит при
повторном использовании ранее скачанного ZIP). Скачайте репозиторий заново либо
выполните `git pull --ff-only` в правильной папке.

Запустите PowerShell в каталоге проекта. Если у текущего процесса нет elevated
administrator token, установщик сам покажет стандартный UAC-запрос и продолжит
работу в новом окне. Это также устраняет ситуацию, когда пользователь состоит в
группе Administrators, но PowerShell работает с ограниченным UAC-токеном:

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\scripts\install-agent.ps1 `
  -Server "wss://remote.example.com/ws" `
  -Room "my-gaming-pc" `
  -Token "ТОТ_ЖЕ_ТОКЕН_ИЗ_ENV" `
  -PublicIP "203.0.113.10" `
  -Encoder "auto" `
  -FPS 60 `
  -Bitrate 20000
```

После запуска должна появиться строка `Elevated administrator token confirmed`.
Если UAC-запрос отменить, установка остановится с понятным сообщением. Проверить
текущий процесс вручную можно командой:

```powershell
([Security.Principal.WindowsPrincipal]::new(
  [Security.Principal.WindowsIdentity]::GetCurrent()
)).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
```

Команда должна вывести `True`. Надпись «Администратор» в свойствах учётной записи
не гарантирует, что именно текущее окно PowerShell получило elevated token.

### Если по-прежнему видно старое `Run PowerShell as Administrator.`

Это сообщение отсутствует в installer v2. Если ошибка указывает ровно на строку
17 и содержит `throw 'Run PowerShell as Administrator.'`, PowerShell запускает
старый файл, независимо от прав текущего окна. Проверьте это командами:

```powershell
Resolve-Path .\scripts\install-agent.ps1
Select-String .\scripts\install-agent.ps1 -Pattern 'Diva-Installer-Version'
Select-String .\scripts\install-agent.ps1 -Pattern 'Run PowerShell as Administrator'
git status
git pull --ff-only
```

В актуальном файле первая `Select-String` показывает `2.0.0`, а вторая ничего не
находит. После обновления закройте старое окно PowerShell, откройте новое в
обновлённой папке и повторите команду установки. Installer сам запросит UAC.

Скрипт копирует бинарник в `%LOCALAPPDATA%\Diva`, открывает UDP 50000 и создаёт
задачу «Diva Remote Agent», запускаемую при входе текущего пользователя. Задача
работает с повышенными правами и перезапускается после ошибки. Проверка:

```powershell
Get-ScheduledTask -TaskName "Diva Remote Agent"
Get-NetUDPEndpoint -LocalPort 50000
```

Для удаления:

```powershell
.\scripts\uninstall-agent.ps1
```

### Несколько мониторов

Без дополнительных параметров FFmpeg передаёт весь виртуальный desktop — все
мониторы одним широким кадром. Чтобы передавать только один монитор, задайте его
координаты и размер. Например, для правого Full HD монитора:

```powershell
.\scripts\install-agent.ps1 <ОБЯЗАТЕЛЬНЫЕ ПАРАМЕТРЫ КАК ВЫШЕ> `
  -X 1920 -Y 0 -Width 1920 -Height 1080
```

Для монитора слева `X` может быть отрицательным. Координаты видны в Windows:
«Параметры → Система → Дисплей». После изменения конфигурации повторно запустите
`install-agent.ps1`: задача будет заменена.

## 6. Подключение

1. Не блокируйте Windows и убедитесь, что задача агента запущена.
2. Откройте `https://remote.example.com` в Chrome, Edge или Firefox.
3. Введите `Room` (`my-gaming-pc`) и значение `DIVA_TOKEN`.
4. Нажмите «Подключиться», затем кликните по видео для управления клавиатурой.
5. Кнопка «На весь экран» включает fullscreen.

Одновременно разрешён один браузерный клиент. Это исключает конфликт управления
и ошибочную рассылку одного WebRTC offer нескольким peer connection.

## 7. Настройка качества и задержки

Начните с 60 FPS и 20 Мбит/с. Для 1080p обычно достаточно 10–20 Мбит/с, для
1440p — 20–35, для 4K — 35–70. Повышение битрейта улучшает детали, но при
переполнении upload-канала резко увеличивает задержку. Оставляйте 20–30% запаса.

Для диагностики откройте `chrome://webrtc-internals` или `about:webrtc`.
Проверяйте packet loss, jitter, RTT и выбранную candidate pair. Соединение должно
использовать UDP и публичный адрес Windows на порту 50000.

## 8. Диагностика

### Сайт не открывается

Проверьте DNS, firewall VPS и логи:

```bash
dig +short remote.example.com
curl -I https://remote.example.com
docker compose logs caddy diva --tail=200
```

### Браузер показывает `failed` / видео не появляется

* убедитесь, что `PublicIP` — реальный внешний IPv4 Windows/роутера;
* проверьте UDP port forwarding и правило Windows Firewall;
* исключите CGNAT и корпоративную сеть, блокирующую UDP;
* проверьте, что room и token совпадают и агент запущен после входа пользователя;
* временно используйте `-Encoder libx264`, чтобы исключить проблему драйвера GPU.

Состояние `peer: failed` через примерно 30 секунд означает **ICE/UDP**, а не
ошибку FFmpeg: encoder уже работает, но браузер и агент не нашли доступный
сетевой маршрут. В новой версии агент печатает все local/remote ICE candidates и
отдельное состояние `ICE`. Выполните на Windows во время работы агента:

```powershell
Get-NetUDPEndpoint -LocalPort 50000
Get-NetFirewallRule -DisplayName "Diva WebRTC UDP" |
  Format-List Enabled,Direction,Action,Profile
(Invoke-RestMethod https://api.ipify.org)
```

Последняя команда должна совпасть с `-public-ip`. Если Windows находится за
роутером, одного белого IP недостаточно: пробросьте **UDP 50000** на LAN IPv4
этого компьютера и закрепите его DHCP reservation. Не создавайте TCP forwarding
50000. В CGNAT входящий port forwarding невозможен — нужен белый IP провайдера.

После пересборки запускайте с STUN явно, чтобы исключить старый бинарник:

```powershell
.\scripts\build-agent.ps1
.\dist\diva-agent.exe `
  -server "wss://remote.houwy.dev/ws" -room "main-pc" -token "TOKEN" `
  -public-ip "37.113.250.197" -udp-port 50000 `
  -stun "stun:stun.cloudflare.com:3478" -encoder auto `
  -ffmpeg "C:\ffmpeg\bin\ffmpeg.exe"
```

В логе должен появиться хотя бы один `local ICE candidate` с публичным адресом и
портом. Если видны только `host` candidates с `192.168.*`, `10.*` или `172.16-31.*`,
не сработали `-public-ip`/STUN. Если публичный candidate есть, но ICE всё равно
переходит в `failed`, почти всегда неверен UDP forwarding или firewall.

### FFmpeg завершается

Запустите команду агента вручную из PowerShell: stderr FFmpeg будет виден в
консоли. Проверьте encoder командой из раздела 3. Захват экрана не работает в
Windows service Session 0, поэтому Diva намеренно использует интерактивную
задачу при входе пользователя.

Ошибка AMF вида `encoder->Init() failed with error 5` означает, что FFmpeg знает
про `h264_amf`, но AMD runtime/GPU отклонил инициализацию. Частые причины:

* компьютер не использует поддерживаемую AMD GPU или установлен старый driver;
* виртуальный desktop шире максимального разрешения encoder (часто при нескольких
  мониторах); задайте `-X/-Y/-Width/-Height` для одного монитора;
* FFmpeg-сборка несовместима с установленным AMD AMF runtime;
* захват имеет нечётную ширину/высоту — агент теперь автоматически дополняет кадр
  до чётного размера.

Начиная с текущей версии это не завершает агент: после ошибки `h264_amf` он пишет
`trying fallback encoder libx264` и продолжает с программным H.264. Для
получения этого исправления обязательно пересоберите бинарник, а затем повторно
установите задачу с `-Encoder auto`:

```powershell
.\scripts\build-agent.ps1
.\scripts\install-agent.ps1 <остальные обязательные параметры> -Encoder auto
```

Проверить AMF отдельно можно так:

```powershell
C:\ffmpeg\bin\ffmpeg.exe -f gdigrab -framerate 60 -i desktop `
  -t 5 -an -vf "pad=ceil(iw/2)*2:ceil(ih/2)*2" `
  -c:v h264_amf -b:v 20M -f null NUL
```

Если эта независимая команда завершается на `Init() error 5`, проблема находится
в GPU/driver/FFmpeg, а не в WebRTC или сервере Diva. Используйте `auto` или
`libx264` и обновите AMD Adrenalin driver.

### Картинка тормозит

Снизьте `Bitrate`, затем FPS/разрешение. Используйте Ethernet вместо Wi-Fi,
аппаратный encoder и ближайший маршрут между браузером и Windows. Docker-сервер
на задержку видеопотока не влияет после установления peer-to-peer соединения.

## 9. Безопасность и ограничения

* Используйте только `https://`/`wss://` и длинный уникальный токен.
* Не публикуйте `.env`; регулярно обновляйте ОС, Docker, браузер, FFmpeg и GPU
  driver. Ограничьте UDP 50000 по source IP, если адрес клиента постоянный.
* Токен является общим для сервера и всех комнат. Для нескольких недоверяющих
  друг другу пользователей нужен отдельный deployment или будущая система
  per-device keys.
* WebRTC шифрует media/data DTLS-SRTP, но этот проект ещё не проходил внешний
  security audit. Не используйте его для критической инфраструктуры.
* Ctrl+Alt+Del, UAC secure desktop, вход до пользовательской сессии и управление
  заблокированным экраном недоступны обычному `SendInput`/`gdigrab`.

## 10. Что нужно для уровня Parsec

Следующие этапы: Windows Desktop Duplication API вместо GDI, автоматический
NVENC/AMF/QSV, frame pacing и bitrate feedback, Opus loopback audio, relative
raw mouse, gamepad, clipboard, per-device credentials, signed installer,
self-hosted coturn и монитор selector. Текущая версия сознательно оптимизирована
под заданный сценарий с прямым белым IP и браузером без установки клиента.
