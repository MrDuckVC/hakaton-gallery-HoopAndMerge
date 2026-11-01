# I01: автономная демонстрация на одном ноутбуке

Топология по уточнению владельца: Go backend, Ollama с RTX 4070 Laptop и frontend
работают в WSL Ubuntu одного серверного ноутбука; PostgreSQL — в Docker Compose.
Go раздаёт готовый frontend dist и `/api/v1`, включая WS, на одном порту 8080.
Отдельного reverse proxy нет. На клиентах требуется только браузер.
Frontend source остаётся в отдельном репозитории.

## Подготовка при доступном интернете

Все команды из корня backend, если явно не указан корень комплекта. Проверенные
версии: Go 1.27.1, Bun 1.4.4, PostgreSQL `18.6-bookworm`, Ollama `0.40.13`;
на сервере также нужны Docker Engine/Compose, Python 3.11+, curl и NVIDIA Windows
драйвер с поддержкой WSL CUDA. Node/Bun, Git и Go нужны при подготовке, но не для
запуска собранного комплекта. `nvidia-smi` должен видеть RTX 4070 Laptop в WSL.
Не устанавливайте Linux NVIDIA driver поверх Windows driver.

1. Подготовьте Ollama runtime по [DEVELOPMENT.md](DEVELOPMENT.md#a01--подготовка-ollama-в-wsl-ubuntu).
   Используйте существующий `bin/ollama-runtime`, если версия уже совпадает.
   Запустите Ollama и заранее скачайте две модели:

   ```sh
   export PATH="$PWD/bin/ollama-runtime/bin:/usr/local/go/bin:$PATH"
   export OLLAMA_HOST=127.0.0.1:11434
   export OLLAMA_MODELS="$PWD/data/ollama/models"
   export OLLAMA_NO_CLOUD=1 OLLAMA_NUM_PARALLEL=1 OLLAMA_MAX_LOADED_MODELS=1
   ollama serve
   ```

   В другом терминале с теми же PATH/OLLAMA_HOST/OLLAMA_MODELS:

   ```sh
   ollama pull qwen3:4b-q4_K_M
   ollama pull qwen2.5:3b-instruct-q4_K_M
   ollama create gallery-guide-main:a01 -f configs/ollama/main.Modelfile
   ollama create gallery-guide-reserve:a01 -f configs/ollama/reserve.Modelfile
   ollama show gallery-guide-main:a01 --parameters
   ollama show gallery-guide-reserve:a01 --parameters
   ```

   Сверьте base digests и контекст 8192 с [MODEL_BENCHMARK.md](MODEL_BENCHMARK.md).
   Не запускайте два экземпляра Ollama на одном порту.

2. Соберите frontend и backend, сохраните образ PostgreSQL локально:

   ```sh
   export PATH="/usr/local/go/bin:$PATH"
   sh tools/build-frontend.sh ./data/frontend
   make build
   docker pull postgres:18.6-bookworm
   python3 tools/offline-bundle.py prepare \
     --frontend-dist ./data/frontend --output ./data/offline/i01
   python3 tools/offline-bundle.py verify ./data/offline/i01
   ```

   Скрипт собирает frontend из `ManInTheCoat/Hope-Merge-Frontend`, ветка
   `main`, commit `c7dbfdc62057fabf06d50bce2a0c3e8e0f386150`,
   с lockfile и `VITE_USE_MOCK=false`. Для новой сборки выбирайте новый выходной
   каталог; существующий dist не перезаписывается. Исходники временно клонируются
   вне backend и удаляются после сборки. Замена frontend требует явного обновления
   закреплённого commit в скрипте и проверки сборки.

3. Комплект включает исполняемые `gallery`/`migrate`/`admin`, Ollama runtime с библиотеками,
   только основной и резервный tags с их blobs, frontend с локальными JS/CSS,
   мир C01, SQL-миграции (также встроены в migrate), Compose и документацию.
   `bundle-manifest.json` фиксирует hashes, ссылки runtime, frontend SHA,
   backend base SHA/diff hash, model digests, world version и Docker image ID.
   Бинарники проверяются по SHA-256; base SHA/diff hash не заменяют hash бинарника.
   Подготовка проверяет версию client binary Ollama 0.40.13 и целостность весов; отсутствие prerequisites останавливает
   её. Существующий выходной каталог не изменяется. Незавершённый комплект без
   manifest использовать нельзя.

Рабочие `.env`, БД, фото, приватные диалоги и история модели в комплект не копируются.
Он предназначен для уже подготовленного Linux/WSL x86_64 с совместимыми системными
библиотеками, драйвером и установленным Docker, а не для установки ОС с нуля.
Не обновляйте runtime/драйвер перед показом без повторной проверки GPU.

## Холодный запуск без интернета

Отключите интернет, сохранив Wi-Fi/LAN. Завершите предыдущий backend и Ollama
штатно; PostgreSQL можно остановить через `docker compose stop postgres` из
старого каталога. Volume не удалять. Не запускайте одновременно две копии backend
на одной БД. Перейдите в комплект на локальной Linux-файловой системе WSL:

```sh
cd data/offline/i01
# Проверять до создания .env/фото и других runtime-файлов в комплекте.
python3 verify-bundle.py verify .
docker image load --input postgres.tar
# Только при первом запуске: создать .env без перезаписи существующего.
[ -e .env ] || cp .env.example .env
```

Отредактируйте `.env`: одинаковый пароль в POSTGRES_PASSWORD и DATABASE_URL
(URL-encode специальные символы в URL), реальные origins, путь фото и volume
проверенного проекта. Не используйте `change-me` на показе.

```dotenv
HTTP_ADDR=0.0.0.0:8080
ALLOWED_ORIGINS=http://localhost:8080,http://127.0.0.1:8080,http://192.168.1.50:8080
COOKIE_SECURE=false
FRONTEND_DIST_DIR=./frontend
WORLD_DIR=./content/world
WORLD_VERSION=gallery_c01_v1
PHOTO_DIR=/absolute/path/to/persistent/photos
OLLAMA_BASE_URL=http://127.0.0.1:11434
OLLAMA_MODEL=gallery-guide-main:a01
COMPOSE_PROJECT_NAME=backend
```

Замените `192.168.1.50` реальным LAN IPv4 Windows, который вводят в браузере
клиенты. Без wildcard, пути и завершающего `/`. `0.0.0.0` — адрес bind, не origin.
Localhost остаётся в allowlist всегда.
**При использовании существующей БД обязательно сохраните прежний абсолютный путь
PHOTO_DIR.** До смены каталога определите его через `realpath -m ./data/photos`
(или ваш настроенный путь). Генератор комплекта по умолчанию записывает абсолютный
`data/photos` исходного backend; для другого расположения передайте
`--photo-dir /absolute/path/to/existing/photos`. Фотографии не копируются.
Для новой установки выберите постоянный абсолютный путь; при переносе на другой
ноутбук отредактируйте его. Не подставляйте относительный путь нового комплекта
при сохранении старой БД: существующие photo IDs должны находить прежние файлы.
 Cookie привязаны к hostname: вход через
LAN IP и localhost выполняется отдельно, а сохранённые данные аккаунта общие.

`COMPOSE_PROJECT_NAME=backend` сохраняет стандартный volume `backend_postgres_data`
при смене каталога с репозитория на комплект. Если старый запуск использовал иной
project name, сохраните именно его; предварительно проверьте `docker volume ls`
и mounts старого контейнера. Не подменяйте проект новым и не удаляйте volume.
Параметры POSTGRES_* не меняют пароль уже существующей БД.

```sh
# .env — доверенный shell-файл, backend сам его не читает.
set -a
. ./.env
set +a
docker compose up --pull never -d --wait postgres
./bin/migrate up
./bin/migrate status
# Только для существующего аккаунта будущего администратора:
./bin/admin grant --username NAME
```

В отдельном терминале из того же корня комплекта:

```sh
export OLLAMA_HOST=127.0.0.1:11434
export OLLAMA_MODELS="$PWD/models"
export OLLAMA_NO_CLOUD=1 OLLAMA_NUM_PARALLEL=1 OLLAMA_MAX_LOADED_MODELS=1
./ollama-runtime/bin/ollama serve
```

В основном терминале:

```sh
curl --fail http://127.0.0.1:11434/api/version
./ollama-runtime/bin/ollama list
./bin/gallery
```

На startup backend проверяет dist, БД, точный набор миграций, мир и восстанавливает
терминальные состояния генераций. Миграции автоматически не применяются.
Недоступная модель не запрещает прогулку, но для показа ИИ оба tags должны быть
проверены. Во время demo не выполняются build/install/pull/create/download.

## LAN: несколько ноутбуков, интернет отключён

- На роутере отключите AP/client isolation и guest isolation; все ноутбуки должны
  быть в одной доступной друг другу сети. Отключите WAN/интернет, не локальный Wi-Fi.
- Узнайте Windows LAN IPv4 через `ipconfig`. Для WSL NAT настройте публикацию
  только игрового порта. В PowerShell администратора, подставив реальный IP:

  ```powershell
  $lanIp = '192.168.1.50'
  $wslIp = ((wsl.exe hostname -I).Trim() -split '\s+')[0]
  netsh interface portproxy add v4tov4 listenaddress=$lanIp listenport=8080 connectaddress=$wslIp connectport=8080
  New-NetFirewallRule -DisplayName 'Gallery demo 8080 LAN' -Direction Inbound -Action Allow -Protocol TCP -LocalPort 8080 -RemoteAddress LocalSubnet -Profile Private
  ```

  LAN portproxy адрес не используется для localhost. Windows localhost forwarding
  в WSL должен быть включён (default); проверьте `http://localhost:8080` отдельно.
  WSL IPv4 после перезапуска может измениться: перед LAN-показом обновите правило
  и проверьте `netsh interface portproxy show v4tov4`. Если используется mirrored
  networking, portproxy не нужен; разрешите порт 8080 в Windows/Hyper-V firewall
  для WSL, например `New-NetFirewallHyperVRule -Name GalleryDemo8080 -DisplayName
  'Gallery demo 8080' -Direction Inbound -VMCreatorId
  '{40E0AC32-46A5-438A-A0B2-2B479E8F2E90}' -Protocol TCP -LocalPorts 8080`.
  Профиль Wi-Fi Windows должен быть Private. Linux firewall, если включён, также
  должен разрешать TCP 8080 из LAN. БД 5432 и Ollama 11434 клиентам не открывать.
- На двух браузерных клиентах откройте `http://<LAN-IP>:8080`, зарегистрируйте
  разные аккаунты. Проверьте вход, сцену, presence/движение, чат, вопрос гиду и
  персональную память. В DevTools Network включите Disable cache, перезагрузите
  страницу и убедитесь, что запросы идут только на этот origin, WS получает 101,
  нет CDN/удалённых шрифтов/аналитики/моделей.

## Localhost: Wi-Fi выключен

Завершите LAN-показ. Выключите Wi-Fi **на серверном ноутбуке**, не только WAN.
Не меняйте конфигурацию сервисов и не подставляйте LAN IP вместо loopback.
В браузере Windows на сервере откройте `http://localhost:8080`, войдите тем же
аккаунтом, загрузите сцену и задайте вопрос гиду. Проверьте историю, сохранённую
память и отсутствие внешних запросов с отключённым кешем. Подключение клиентов
после выключения Wi-Fi не требуется. Если localhost недоступен, исправьте WSL
localhost forwarding до демонстрации; работа только по LAN IP приёмку не выполняет.

## Рестарт, данные и резерв

Проверьте фото/альбом, осмотр и completed-ответ гида. Штатно остановите backend
через Ctrl+C, Ollama — Ctrl+C, PostgreSQL — `docker compose stop postgres`.
Повторите холодный запуск (без повторного копирования .env). После входа проверьте
сохранённую позицию, фотографии и историю/память. Не менять PHOTO_DIR и Compose
project между запусками. Для резервной копии БД и фотографий останавливайте запись
и сохраняйте их совместно. Комплект программы сам по себе не является backup данных.

Ручной резерв без сети: остановите backend, выполните
`./ollama-runtime/bin/ollama stop gallery-guide-main:a01`, поменяйте OLLAMA_MODEL
на `gallery-guide-reserve:a01` в .env, заново экспортируйте .env и запустите backend.
Для возврата поменяйте tags местами. Автоматического выбора или скачивания нет;
одновременно загружена одна модель. Проверьте реальный ответ и размещение на GPU
через `./ollama-runtime/bin/ollama ps` во время генерации для обоих tags.

## Запись приёмки

Фактические результаты I01 записываются в [I01_ACCEPTANCE.md](I01_ACCEPTANCE.md).
Автоматическая проверка HTTP/WS и network namespace не заменяет два физических
браузерных показа. Полный игровой маршрут и показатели качества относятся к Q02.

## Дополнение Sunday (§8)

Перед стартом отдельно примените миграции вплоть до 00008; S07 не добавляет SQL.
Для первого показа зарегистрируйте аккаунт, штатно остановите backend, выдайте
ему admin через `bin/admin`, затем снова запустите backend. Уже существующему
аккаунту роль можно выдать на этапе холодного запуска выше.

В двух браузерах с реальным dist и без mocks проверьте admin-оформление/профиль,
FPS+ping обоих клиентов; 10 NPC на зал, реакции на чат и локальные анимации;
лампу take → конфликт второго клиента → place → aim → power, затем guide.action.
Переподключитесь и перезапустите backend: оформление/профиль/лампа сохранены,
holder не завис. Проверьте переходы, фото/альбом и бюджет 10% с резервом фото.
Повторите сценарий сначала в LAN без WAN, затем на localhost с физически
выключенным Wi-Fi. Результаты и непроведённые пункты фиксируйте отдельно в
[S07_ACCEPTANCE.md](S07_ACCEPTANCE.md); unit/mock/namespace не заменяют показ.
