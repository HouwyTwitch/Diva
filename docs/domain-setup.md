# Настройка домена, DNS и TLS

Этот документ описывает настройку адреса **Docker-сервера сигнализации**. Его
домен и IP не обязаны совпадать с публичным IP управляемого Windows-компьютера.
После установления соединения видео идёт напрямую между браузером и Windows.

## Почему Caddy пишет `NXDOMAIN`

Сообщение вида:

```text
DNS problem: NXDOMAIN looking up A for remote.example.com
DNS problem: NXDOMAIN looking up AAAA for remote.example.com
```

означает, что публичные DNS-серверы считают имя несуществующим. Это не ошибка
Docker, Caddy или Diva. Caddy не может подтвердить контроль над именем и поэтому
Let's Encrypt не выдаёт сертификат. Создайте DNS-запись и дождитесь её
распространения **до** `docker compose up`.

## Термины и два разных публичных адреса

| Назначение | Пример | Где задаётся |
|---|---|---|
| HTTPS/WebSocket сервера | `remote.example.com → 203.0.113.20` | DNS и `DIVA_ADDRESS` |
| WebRTC Windows-агента | `198.51.100.40:50000/udp` | `-PublicIP` и port forwarding |

Первый адрес ведёт на Linux/VPS с Docker. Второй ведёт на Windows или её роутер.
Если всё работает на одной площадке, адреса могут совпадать, но TCP 80/443 нужно
направить на Docker-сервер, а UDP 50000 — на Windows.

## Какие DNS-записи создать

Откройте DNS-панель **авторитетного** провайдера зоны. Для домена `houwy.dev` и
адреса сайта `remote.houwy.dev` минимальная запись выглядит так:

| Type | Name/Host | Value/Content | TTL |
|---|---|---|---|
| `A` | `remote` | публичный IPv4 Docker-сервера | `300` или Auto |

Некоторые панели требуют полное имя `remote.houwy.dev`, другие — только
`remote`. Не добавляйте точку в конце, если панель этого явно не требует.

### IPv6 (`AAAA`)

Добавляйте `AAAA` только если у VPS действительно есть глобальный IPv6, Docker
host слушает TCP 80/443 по IPv6, а firewall пропускает эти подключения:

| Type | Name/Host | Value |
|---|---|---|
| `AAAA` | `remote` | глобальный IPv6 Docker-сервера |

Ошибочная `AAAA` часто приводит к тому, что проверяющий центр или браузер идёт
по неработающему IPv6. В этом случае удалите `AAAA`; отсутствие AAAA нормально.

### CNAME вместо A

Можно создать `CNAME remote → existing-host.example.net`, если конечное имя уже
имеет правильные A/AAAA записи. Нельзя одновременно иметь CNAME и A/AAAA для
одного и того же имени. Для простой и предсказуемой установки рекомендуется A.

### CAA

CAA не обязательна. Если в зоне уже есть ограничивающие CAA-записи, разрешите
Let's Encrypt:

```text
Type: CAA
Name: @
Flag: 0
Tag: issue
Value: letsencrypt.org
```

Проще не создавать CAA вообще. Неправильная CAA может запретить выпуск даже при
правильных A/AAAA. Проверка: `dig example.com CAA +short`.

## Cloudflare и другие DNS-прокси

Для первого запуска установите запись в режим **DNS only** (серое облако в
Cloudflare). Так A-запись отвечает реальным IP VPS, а Caddy сам получает и
обслуживает сертификат. После успешного запуска прокси можно включить, но тогда:

* SSL/TLS mode должен быть `Full (strict)`, не `Flexible`;
* WebSocket должен быть разрешён;
* origin TCP 80/443 всё равно должен быть корректно настроен;
* IP в `dig` станет адресом прокси — это ожидаемо;
* UDP 50000 WebRTC нельзя проксировать обычным Cloudflare DNS proxy: он должен
  идти напрямую на публичный адрес Windows/роутера.

При диагностике временно верните `DNS only`, чтобы исключить влияние прокси.

### Ошибки Cloudflare `522` и `acme-tls/1`

Комбинация сообщений:

```text
2606:4700:... Invalid response ...: 522
Cannot negotiate ALPN protocol "acme-tls/1"
```

однозначно показывает, что Let's Encrypt приходит к Cloudflare, а не напрямую к
Caddy. `522` означает, что Cloudflare не установил соединение с origin по HTTP;
TLS-ALPN challenge через обычный Cloudflare proxy также не доходит до Caddy.

Есть два поддерживаемых решения. Выберите только одно.

#### Вариант A — DNS only (рекомендуется и проще)

1. В Cloudflare → DNS найдите запись `remote`.
2. Нажмите оранжевое облако, чтобы статус стал **DNS only** / серое облако.
3. Убедитесь, что Content записи — публичный IPv4 VPS.
4. Удалите `AAAA`, если на origin нет рабочего IPv6.
5. Разрешите входящие TCP 80 и 443 в firewall VPS и панели хостинга.
6. Дождитесь, пока `dig remote.houwy.dev A +short` вернёт IP VPS, а не адреса
   Cloudflare, затем выполните:

```bash
docker compose restart caddy
docker compose logs -f caddy
```

#### Вариант B — Cloudflare proxy + DNS-01

DNS-01 не требует, чтобы центр сертификации подключался к origin по HTTP или
договаривался об `acme-tls/1`: Caddy временно создаёт TXT-запись через Cloudflare
API. В репозитории имеется отдельная конфигурация для этого режима.

1. Cloudflare → My Profile → API Tokens → Create Token.
2. Создайте scoped token со следующими разрешениями:
   * `Zone / Zone / Read`;
   * `Zone / DNS / Edit`;
   * Zone Resources → Include → Specific zone → ваш домен.
3. Не используйте Global API Key. Не вставляйте token в Caddyfile или Git.
4. Добавьте в `.env`:

```dotenv
CADDYFILE_PATH=./Caddyfile.cloudflare
CF_API_TOKEN=<секретный scoped token Cloudflare>
```

5. Запустите Compose с override, который собирает Caddy с официальным DNS
   provider module:

```bash
docker compose -f docker-compose.yml -f docker-compose.cloudflare.yml up -d --build
docker compose -f docker-compose.yml -f docker-compose.cloudflare.yml logs -f caddy
```

6. После выпуска сертификата удалять token нельзя: он понадобится Caddy для
   автоматического продления. Защитите `.env` командой `chmod 600 .env`.

В этом режиме запись может оставаться оранжевой. Cloudflare всё равно должен
достигать origin по TCP 443 для обычного пользовательского трафика. Разрешите
Cloudflare IP ranges в firewall либо временно откройте 443 для всех. WebSocket
должен быть включён, SSL/TLS mode — `Full (strict)`.

## Firewall, NAT и port forwarding сервера

Разрешите на VPS и во внешнем firewall/security group:

| Protocol | Port | Назначение |
|---|---:|---|
| TCP | 80 | ACME HTTP challenge и redirect на HTTPS |
| TCP | 443 | HTTPS и WebSocket (`wss://`) |
| UDP | 443 | HTTP/3 Caddy; опционален для базовой работы |

Если Docker-сервер находится за роутером, пробросьте TCP 80 и 443 на его LAN IP.
Не направляйте UDP 50000 на Docker: этот порт относится к Windows-агенту.

Пример для Ubuntu с UFW:

```bash
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp
sudo ufw allow 443/udp
sudo ufw status
```

Убедитесь, что порты не заняты другим nginx/Apache/Caddy:

```bash
sudo ss -lntup | grep -E ':(80|443)\b'
```

## Пошаговая проверка до запуска Caddy

### 1. Узнайте публичный IP VPS

В панели хостинга найдите primary/public IPv4. На самом VPS для дополнительной
проверки можно выполнить:

```bash
curl -4 https://api.ipify.org; echo
```

Не используйте Docker bridge address (`172.x.x.x`) или приватный `10.x.x.x` /
`192.168.x.x` в публичной A-записи.

### 2. Проверьте авторитетные nameserver'ы

```bash
dig houwy.dev NS +short
```

DNS-запись нужно создавать именно у провайдера, чьи NS показала команда. Запись,
созданная в панели регистратора при делегировании на другие NS, не действует.

### 3. Проверьте запись через несколько публичных resolver'ов

```bash
dig remote.houwy.dev A +short
dig remote.houwy.dev AAAA +short
dig @1.1.1.1 remote.houwy.dev A +short
dig @8.8.8.8 remote.houwy.dev A +short
```

Первая и две последние команды должны вернуть ожидаемый IPv4. Пустой результат
или `status: NXDOMAIN` означает, что запускать Caddy пока рано. AAAA может быть
пустой. Удалите старый неправильный AAAA, если он указывает не на ваш VPS.

Также в репозитории есть проверочный скрипт:

```bash
./scripts/check-domain.sh remote.houwy.dev 203.0.113.20
```

### 4. Дождитесь DNS propagation

Изменение обычно видно после TTL, но кэши с прежним NXDOMAIN могут жить дольше.
Проверяйте публичные resolver'ы, а не только локальный роутер. Не перезапускайте
Caddy каждую секунду: центры сертификации имеют rate limits.

### 5. Запустите стек и следите за сертификатом

```bash
cp .env.example .env
# Отредактируйте DIVA_ADDRESS и DIVA_TOKEN
docker compose up -d --build
docker compose logs -f caddy
```

Успешный лог содержит получение сертификата, после чего проверки должны вернуть
redirect по HTTP и успешный HTTPS:

```bash
curl -I http://remote.houwy.dev
curl -I https://remote.houwy.dev
docker compose ps
```

## Значение `.env`

```dotenv
DIVA_ADDRESS=remote.houwy.dev
DIVA_TOKEN=<результат openssl rand -hex 32>
```

В `DIVA_ADDRESS` нельзя писать `https://`, путь `/ws`, порт, кавычки внутри
значения или случайный пробел. Допустимо только DNS-имя, соответствующее записи.
Caddyfile использует это имя для сайта и автоматического TLS.

После изменения `.env` пересоздайте контейнеры:

```bash
docker compose up -d --force-recreate
```

## Таблица типичных ошибок

| Ошибка | Причина | Исправление |
|---|---|---|
| `NXDOMAIN looking up A/AAAA` | имя не создано или создано не у авторитетного DNS | создать A, проверить NS и дождаться propagation |
| `no valid A records found` | A отсутствует или содержит приватный IP | указать публичный IPv4 VPS |
| `connection refused` | порт закрыт или контейнер не запущен | открыть 80/443, проверить `docker compose ps` |
| `i/o timeout` | firewall/NAT/security group блокирует challenge | разрешить и пробросить TCP 80/443 |
| Cloudflare `522` | proxy не достигает origin | DNS only либо исправить origin firewall/routing |
| Cloudflare `525/526` | proxy не может установить/проверить TLS до origin | сначала DNS only и выпустить origin certificate, затем Full (strict) |
| `Cannot negotiate ALPN protocol "acme-tls/1"` | TLS-ALPN остановился на proxy | DNS only либо режим Cloudflare DNS-01 |
| ошибка только по IPv6 | неправильная AAAA/IPv6 routing | исправить IPv6 или удалить AAAA |
| `CAA record does not allow` | CAA запрещает issuer | разрешить `letsencrypt.org` или удалить CAA |
| сертификат есть, но `/ws` не работает | внешний proxy не пропускает WebSocket | включить WebSocket и режим Full (strict) |

Полезные официальные материалы: [Automatic HTTPS в Caddy](https://caddyserver.com/docs/automatic-https)
и [типы challenge Let's Encrypt](https://letsencrypt.org/docs/challenge-types/).
