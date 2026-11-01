# Локальная разработка и конфигурация

B01: backend слушает HTTP, проверяет PostgreSQL/схему и обрабатывает SIGINT/SIGTERM. B02 подключает API аккаунтов и cookie-сессии. B03 добавляет startup-валидацию локального мира и HTTP выдачу через порт игрового владения. B04 подключает WS, snapshot, presence и HTTP-владение текущим залом. B05 реализует ready/движение/двери и сохранение. B06 подключает чат, осмотр и личные события памяти. B07 сохраняет очередь/историю, B08 подключает локальный Ollama и персональный контекст. B09 подключает загрузку фото; B10 подключает альбом, защищённое чтение файлов и публикацию. I01 раздаёт отдельно собранный frontend dist из FRONTEND_DIST_DIR через Go HTTP-адаптер. Все ответы приложения имеют `X-Request-ID` и `Cache-Control: no-store`; неизвестные маршруты возвращают JSON `404 NOT_FOUND`.

## Подготовка и запуск

Go >=1.27.1, Docker Compose для локальной PostgreSQL. Версии pgx/v5 и goose закреплены в go.mod/go.sum. Если Go установлен в `/usr/local/go`, добавьте `/usr/local/go/bin` в PATH.

```sh
cp .env.example .env
# Замените пароли, origins, WORLD_VERSION и OLLAMA_MODEL.
sh tools/build-frontend.sh ./data/frontend
# Для API-only разработки удалите FRONTEND_DIST_DIR из .env.
# Подготовьте WORLD_DIR/catalog.json по WORLD_CONTENT.md.
# .env — доверенный shell-файл; backend сам его не загружает.
set -a
. ./.env
set +a
docker compose up -d --wait postgres
make build                         # bin/gallery, bin/migrate и bin/admin
make migrate-up                    # либо ./bin/migrate up
make migrate-status                # либо ./bin/migrate status
./bin/gallery
```

Миграции всегда выполняются до backend. Его startup не создаёт даже `goose_db_version`: проверяет Ping и точное соответствие истории встроенным миграциям. Отсутствующая/отстающая схема требует `make migrate-up`; более новая — соответствующего бинарника. Применённые миграции не переписывать, новые добавлять следующим номером. В административной команде доступны только `up` и `status`; служебную историю goose при необходимости инициализирует административная команда. `up` использует advisory lock и транзакционные SQL-миграции. Миграции имеют deadline 60 секунд, соединение 5 секунд, ожидание SQL-блокировки 5 секунд. Runtime-роль backend может не иметь DDL-прав; выдачу отдельных ролей администратор выполняет отдельно.

`make run` запускает через `go run`; для проверки сигналов используйте собранный `./bin/gallery`. Остановка: Ctrl+C или SIGTERM; затем `docker compose stop postgres`. Не удаляйте named volume. Фото в `PHOTO_DIR` и PostgreSQL должны оставаться на серверном ноутбуке.

## Настройки

| Переменная | Default / правило |
|---|---|
| HTTP_ADDR | `0.0.0.0:8080`, host:port, порт 1–65535 |
| DATABASE_URL | Обязательный PostgreSQL URL с host и database; не логируется |
| ALLOWED_ORIGINS | Обязательные точные HTTP(S) origins через запятую, без path/query/fragment/credentials; включить LAN и localhost |
| COOKIE_SECURE | `true`; локальный HTTP требует явного `false` |
| FRONTEND_DIST_DIR | Не задан: API-only. При заданном пути обязателен читаемый index.html до listener; шаблон: `./data/frontend` |
| COMPOSE_PROJECT_NAME | Шаблон: `backend`; сохранять прежнее значение при переносе комплекта для того же PostgreSQL volume |
| WORLD_DIR | `./content/world` |
| WORLD_VERSION | Обязательный ID, 1–128 печатных ASCII-символов без пробелов |
| PHOTO_DIR | `./data/photos` |
| OLLAMA_BASE_URL | `http://127.0.0.1:11434`, HTTP(S), без credentials/query/fragment |
| OLLAMA_MODEL | Обязательный заранее подготовленный tag; шаблон A01 — `gallery-guide-main:a01`, ручной резерв — `gallery-guide-reserve:a01` |
| LOG_LEVEL | `info`; доступны `debug`, `info`, `warn`, `error` |

Явно пустые значения отклоняются. `.env.example` — шаблон; `change-me`, адреса и model tag замените. Настройки cookie/origins используются B02; настройки фото предназначены для следующей подсистемы. OLLAMA_BASE_URL/OLLAMA_MODEL используются B08, recovery генераций выполняется до listener; B03 уже валидирует мир до открытия listener согласно ARCHITECTURE.md.

Compose использует `postgres:18.6-bookworm`, постоянный volume на `/var/lib/postgresql`, healthcheck и порт только `127.0.0.1:${POSTGRES_PORT:-5432}`. `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB` должны соответствовать `DATABASE_URL`. Изменение этих переменных не меняет credentials уже инициализированного volume.

Startup deadline 10 секунд; DB connect/query timeout 5 секунд; максимум 10 соединений. HTTP: headers 5 секунд, read 15, write 30, idle 60; контекст обработки запроса ограничен 10 секундами. Shutdown — общий deadline 15 секунд с независимым контекстом: остановить listener/новые запросы, lifecycle-порты отмены генераций/сохранения позиций/закрытия WS, HTTP drain, закрытие пула. При ошибке выполняется последующая очистка; по deadline последующие lifecycle cleanup-вызовы и закрытие пула предпринимаются best-effort без ожидания зависших участников, HTTP закрывается принудительно, процесс завершается с ненулевым кодом. B05 регистрирует сохранение позиций до закрытия WS и пула. Не держать SQL-транзакции при сетевом/file/LLM I/O.

## Авторизация B02 и интеграция мира

Маршруты: `POST /api/v1/auth/register`, `POST /api/v1/auth/login`, `POST /api/v1/auth/logout`, `GET /api/v1/me`. Для POST обязательно передавать точный разрешённый `Origin`, в том числе при запросах curl. CORS для сторонних origin не включён. Отсутствующий/`null` Origin отклоняется; та же проверка подготовлена для будущего WS upgrade.

B03 подключает настоящий каталог к auth.World. Сервер запускается только с валидным WORLD_DIR; формат и проверки — [WORLD_CONTENT.md](WORLD_CONTENT.md). Register и `/me` используют проверенные позы. Manifest доступен любой действующей сессии, rooms/assets — только через активное игровое владение. До B04 production NoConnections отклоняет пакеты с 409 GAME_NOT_READY; fixtureWorld не используется в рабочем запуске. На чтение и проверку мира выделен отменяемый deadline 60 секунд, PostgreSQL и открытие listener ограничены отдельно десятью секундами.

Login/register делят скользящий лимит 10 попыток за 60 секунд по IP из TCP `RemoteAddr`; `X-Forwarded-For` не доверяется. Лимит хранится в процессе и не сбрасывается новым HTTP-соединением, cookie или logout; перезапуск процесса его очищает. Отказ Origin выполняется раньше учёта попытки, допустимые по Origin запросы учитываются до разбора JSON. При reverse proxy несколько клиентов с одним upstream IP делят лимит: прямой LAN-доступ не требует доверенных forwarding headers.

Cookie `museum_session` действует ровно 24 часа без продления, `HttpOnly; SameSite=Lax; Path=/; Max-Age=86400`. `Secure` включён по умолчанию; для локального HTTP явно задайте `COOKIE_SECURE=false`. Пароли и токены не логируются; PostgreSQL хранит encoded PBKDF2 verifier и SHA-256 токена. Замена отзывает только действующую сессию из переданной cookie, включая cookie другого аккаунта. Удаление cookie при logout происходит после успешного сохранения отзыва; ошибка БД возвращает 503.

Порт наблюдения `auth.Service.Observe(ctx, session)` предназначен для B04: использовать контекст жизни игрового соединения, отменять подписку при отключении. Он регистрирует наблюдателя до повторной проверки БД. Hub доставляет `revoked` после commit или `expired` по неизменному deadline; закрытый канал без события означает отменённое наблюдение. B04 должен сопоставить причину с close 4001, проверять connection ID и повторно проверять сессию перед изменяющими командами. B04 использует этот порт в контексте жизни WS и закрывает соединение с 4001.

## Проверки и отдельная тестовая БД

```sh
go build ./...
go vet ./...
go test ./...
go test -race ./...
make lint
make coverage
make contracts
```

Интеграционные тесты явно пропускаются без `TEST_DATABASE_URL`; в CI переменная обязательна и PostgreSQL предоставляется сервисом. Она должна указывать на отдельную тестовую БД, отличную от `DATABASE_URL`. Не используйте рабочую БД.

```sh
# На запущенной локальной PostgreSQL создайте отдельную БД для тестов один раз:
docker compose exec postgres sh -c 'createdb -U "$POSTGRES_USER" gallery_test'
export TEST_DATABASE_URL='postgres://gallery:change-me@127.0.0.1:5432/gallery_test?sslmode=disable'
make coverage
```

Тесты PostgreSQL создают случайные собственные schemas и удаляют только их. Тесты команд применяют обычные append-only миграции к тестовой БД без удаления существующих записей. Подключению для тестов нужны права CREATE SCHEMA. Повторный прогон допустим. Тесты сигналов используют дочерний процесс и временный loopback-порт. `go list` и depguard проверяют граф импортов. `make coverage` проверяет суммарные операторы всех Go-пакетов, включая cmd, с порогом >=60%, race detector и отчётом `coverage.out`. Node используется только генератором/валидатором контрактов.

Если sandbox запрещает запись в стандартный Go cache, задайте `GOCACHE=/tmp/gallery-go-cache`; для линтера аналогично `GOLANGCI_LINT_CACHE=/tmp/gallery-lint-cache`. Сетевым тестам нужен loopback. `make build GO=/usr/local/go/bin/go` позволяет указать путь Go; для архитектурного теста и линтера он также должен быть в PATH.

## Docker, CI и офлайн-подготовка

```sh
make docker-build
# На Linux, только с отдельной тестовой БД:
DATABASE_URL="$TEST_DATABASE_URL" sh tools/smoke-docker.sh gallery-backend:local
```

Multi-stage Dockerfile: `golang:1.27.1`, статические `/gallery`, `/migrate` и `/admin`, финальный `scratch`, пользователь 65532:65532. Миграции встроены в бинарники; `/admin` проверяет схему, но не применяет её. Smoke-test выполняет миграции, стартует контейнер на localhost:18080, проверяет HTTP API 404, frontend 200 и успешное завершение по SIGTERM. Использует host networking на Linux для доступа к тестовой БД и удаляет только свой контейнер. Для Docker Desktop/WSL можно задать `SMOKE_DOCKER_NETWORK` с именем уже созданной обычной bridge-сети (без `--internal`, чтобы работала публикация порта): тестовая PostgreSQL должна быть подключена к ней, а `DATABASE_URL` должен использовать её сетевой alias и порт 5432; HTTP-порт 18080 публикуется на loopback. Старый запуск с `--network none` без PostgreSQL теперь ожидаемо завершается с ошибкой.

GitHub Actions: gofmt, vet, race/coverage с PostgreSQL integration tests, lint, build, генерация и проверка отсутствия drift контрактов, затем Docker build и smoke-test с отдельной PostgreSQL. Workflow не публикует образ и не выполняет deploy.

До отключения интернета: `go mod download`, `make lint` для установки закреплённого линтера, `docker compose pull postgres`, `make build`, `make docker-build`; отдельно подготовить модель и контент по A01/C01/I01. Runtime использует PostgreSQL, локальные файлы мира и стандартную криптографию Go, без внешних API. Не выполнять `tools/write-docs.mjs` поверх ручных изменений. `make world-check` проверяет поставляемый C01 штатным валидатором.

Frontend остаётся в отдельном репозитории. При разработке через Vite proxy `/api` должен поддерживать WS и сохранять Origin; добавьте точный dev origin `http://localhost:5173` в ALLOWED_ORIGINS. В production dist, API и WS обслуживает Go на одном origin. Bind `0.0.0.0` не является браузерным origin; адреса LAN и localhost задаются явно. Пакеты мира доступны через B03; B04 уже подключает игровые соединения. B05 подключает движение/готовность/переходы и полный state 10 Гц. I01 раздаёт dist из FRONTEND_DIST_DIR; исходники frontend не копируются в backend.

## Требуемая топология демонстрации

Единственный сервер — ноутбук с RTX 4070 Laptop. PostgreSQL, backend, Ollama, модели, frontend dist, мир и фотографии размещаются на нём. На других ноутбуках — только браузер; установку серверных инструментов на них не требовать. Все runtime-ресурсы frontend должны входить в локальную сборку.

Подготовить оба origin: адрес сервера в LAN и localhost. Сначала показывать многопользовательский режим по Wi-Fi без интернета, затем выключить Wi-Fi на сервере и показать автономную игру с ИИ через localhost. Bind 0.0.0.0 не является origin браузера; exact Origin allowlist должен включать реальные адреса обоих режимов. Подготовка офлайн-комплекта, реальные команды запуска и два режима показа описаны в [DEMO.md](DEMO.md); фактический статус проверок — [I01_ACCEPTANCE.md](I01_ACCEPTANCE.md).

До выбора OLLAMA_MODEL выполнить A01 на реальном ноутбуке: измерить доступную память и время ответа, в том числе рядом с браузерной сценой; зафиксировать точный основной и резервный tags, квантование и контекст. Скачать оба до отключения интернета.

## Тестовый мир и Docker

`go run ./tools/worldfixture --dir /tmp/gallery-smoke-world --version smoke_v1` создаёт синтетический мир только в пустом каталоге. Это fixture для проверок, не образовательный контент и не fallback сервера. Генератор пишет настоящие JPEG и 40 связанных комнат, проходящих тот же бюджет. Каталог содержит около 50 МиБ; его можно удалить после теста.

Docker smoke предоставляет непривилегированному процессу временный PHOTO_DIR=/photos через tmpfs (UID/GID 65532), сам создаёт отдельный временный мир, монтирует его read-only как `/world`, задаёт WORLD_DIR/WORLD_VERSION и удаляет свои файлы при завершении. Нужны Go из go.mod, Docker и curl. Runtime Docker-образ не содержит fixture. Для реального запуска смонтируйте подготовленный WORLD_DIR read-only с правом чтения UID 65532; не включайте пользовательский контент в образ.

## WebSocket B04

Зависимость `github.com/coder/websocket v1.8.15` закреплена в go.mod/go.sum; скачайте её до офлайн-показа. Клиент отправляет cookie `museum_session` и точный разрешённый Origin при подключении к `/api/v1/ws`. Отсутствующий/пустой/null/повторный Origin отклоняется до upgrade. Для LAN добавьте origin браузера в ALLOWED_ORIGINS; для показа без Wi-Fi оставьте localhost origin.

Проверяемый сценарий: register/login → WS → world.snapshot → manifest → room/assets с version из snapshot. Для пакета должны совпадать активный WS, user_id и session ID; готовность для скачивания не нужна. Новая login-сессия получает владение только после собственного WS upgrade. Замена WS закрывает старый с 4009 и публикует остальным left/joined. Logout/expiry закрывает соответствующий WS с 4001. Не воспроизводите накопленные команды после reconnect.

B05 подключает ready/player.move/world.enter, периодический players.state и сохранение перемещений; B06 — chat.send/exhibit.inspect. Ошибки структуры, дедупликации и лимитов проверяются до обработчика. Гиды пока возвращают SERVICE_UNAVAILABLE после проверки готовности, cancel — без требования ready.

WS живёт независимо от десятисекундного HTTP deadline. Handshake, проверки БД и вызовы обработчиков ограничены 10 секундами; исходящая запись — 10 секундами. Ping — 20 секунд, pong deadline — 10 секунд. При shutdown upgrade/команды останавливаются, hijacked WS явно закрываются с 1012 в рамках общего deadline 15 секунд.

Неверные заголовки WS handshake возвращают контрактный HTTP 400 VALIDATION_ERROR JSON; технические сообщения библиотеки наружу не передаются. При истечении shutdown deadline транспорт закрывает исходные hijacked TCP-сокеты, прерывая ожидание ответного Close без дополнительного пятисекундного ожидания coder/websocket.

## Игровой цикл B05

После snapshot загрузите current_room, отправьте world.ready с world_version и movement_epoch из snapshot и дождитесь подтверждения. Игровой цикл рассылает полный players.state каждые 100 мс. Движение ограничено сервером; смена зала возможна только командой world.enter по доступной двери. После entered загрузите новый пакет и подтвердите новый epoch. B06 подключает чат и осмотр; гиды пока не подключены.

Периодический save запускается каждые 5 секунд, disconnect и shutdown сохраняют позиции дополнительно. Ошибка периодического хранения закрывает WS с 1011. При рестарте используются сохранённые координаты, epoch/ack начинаются заново. Для проверки сохранения не очищайте БД и не удаляйте volume.

## Чат и осмотр B06

Перед запуском выполните `make migrate-up`: миграция 00002 добавляет exhibit_inspections/memory_events. Startup требует точный набор применённых миграций; история миграции 00001 не меняется. Новых env и runtime-зависимостей нет.

Проверка: два аккаунта подключают WS и обмениваются chat.send до ready; отправитель получает свой request_id, остальные — chat.message без него. После ready осмотр доступен только в текущем зале на расстоянии до 3 м при свободном LOS. Повтор обновляет время осмотра и существующего события памяти; новая версия мира не создаёт повторное достижение при сохранённом exhibit_id. Внутренний MemoryReader возвращает 1–100 последних событий конкретного пользователя по (created_at,id) DESC.

Фото до B10 не публикуются: chat.send с photo_id возвращает SERVICE_UNAVAILABLE без рассылки. Текстовый чат не сохраняется. Неподключённые гиды остаются отдельными задачами.

## B07: генерации

Перед запуском после обновления выполните `make migrate-up`: миграции 00003–00004 добавляют очередь и память гидов. Backend проверяет полный набор миграций и выполняет recovery pending до открытия HTTP listener. B08 использует настройки Ollama из .env.example. При недоступной модели сохранённый вопрос получает failed/AI_UNAVAILABLE, прогулка и история продолжают работать. Формат профилей, пределы памяти, настройка модели A01 и opt-in проверка реальной модели — [GUIDES.md](GUIDES.md).

Проверки B07 входят в `go test ./...`, `go test -race ./...` и `make coverage`. Для SQL и сквозных HTTP/WS сценариев требуется отдельный TEST_DATABASE_URL по инструкции выше. Тесты создают собственные schemas; рабочие генерации не очищаются. Тестовые модель/clock управляются через порты и не требуют GPU или интернета.


## B08 — локальный Ollama

Запустите заранее подготовленный Ollama на серверном ноутбуке, задайте OLLAMA_BASE_URL и точный OLLAMA_MODEL из A01. Backend не загружает модели и не блокирует startup при недоступном Ollama. Новые миграции B08 не нужны; штатный `make migrate-up` сохраняется. Все профили мира теперь обязаны соответствовать [GUIDES.md](GUIDES.md); старый произвольный JSON-профиль не принимается. Перед обновлением подготовьте профили и новую WORLD_VERSION. Сквозные автоматические проверки используют только TEST_DATABASE_URL отдельной тестовой БД; ручной двухрежимный показ и его текущий статус описаны там же.

## A01 — подготовка Ollama в WSL Ubuntu

Измерения и границы подтверждённого выбора — [MODEL_BENCHMARK.md](MODEL_BENCHMARK.md).
Обычную нагрузку приложений сохранять, режим питания/GPU записывать; не подменять
фактическую доступную VRAM паспортными характеристиками. Команды ниже выполнять
из корня backend до отключения интернета. Нужны curl, sha256sum, tar с zstd;
распаковщик zstd установить заранее средствами Ubuntu. Во время выполнения A01
архив распакован через системную libzstd, без изменения установленных пакетов.

```sh
mkdir -p bin/ollama-runtime data/ollama/models
curl -fL --retry 3 \
  https://github.com/ollama/ollama/releases/download/v0.40.13/ollama-linux-amd64.tar.zst \
  -o /tmp/ollama-v0.40.13.tar.zst
echo 'c9490e0f520be21271d969e6703f012268b1d39f8b70dd58854e5bfef7a75bb6  /tmp/ollama-v0.40.13.tar.zst' | sha256sum -c -
tar --zstd -xf /tmp/ollama-v0.40.13.tar.zst -C bin/ollama-runtime
export PATH="$PWD/bin/ollama-runtime/bin:$PATH"
export OLLAMA_HOST=127.0.0.1:11434
export OLLAMA_MODELS="$PWD/data/ollama/models"
export OLLAMA_NO_CLOUD=1
export OLLAMA_NUM_PARALLEL=1
export OLLAMA_MAX_LOADED_MODELS=1
ollama serve
```

Запускать Ollama в отдельном терминале. Не устанавливать NVIDIA Linux-драйвер
в WSL поверх Windows-драйвера. Проверить `nvidia-smi`, `ollama --version`,
`curl --fail http://127.0.0.1:11434/api/version`. Настройки OLLAMA_* процесса
serve не читаются автоматически из backend .env. Ollama слушает только loopback;
для клиентов LAN открывается backend, прямой доступ к Ollama не нужен.

В другом терминале экспортировать тот же PATH и подготовить модели. Размеры
до скачивания: 2 620 788 260 + 1 929 912 432 + 986 061 892 байта для трёх
кандидатов; архив Ollama — 1 525 346 571 байт, распакованный runtime около 2,4 ГиБ.
Локальные производные tags используют общие blobs, не копируют веса повторно.

```sh
ollama pull qwen3:4b-q4_K_M
ollama pull qwen2.5:3b-instruct-q4_K_M
ollama pull qwen2.5:1.5b-instruct-q4_K_M
ollama list
curl --fail http://127.0.0.1:11434/api/tags
# Сверить базовые digest с MODEL_BENCHMARK.md до создания производных tags.
ollama create gallery-guide-main:a01 -f configs/ollama/main.Modelfile
ollama create gallery-guide-reserve:a01 -f configs/ollama/reserve.Modelfile
ollama show gallery-guide-main:a01 --parameters
ollama show gallery-guide-reserve:a01 --parameters
```

При другом digest повторить матрицу A01 до использования результатов этого отчёта.

Для воспроизведения сравнения создать аналогичные tags с num_ctx 8192 и 16384
для каждой базовой модели. Важны явный tag, digest, Q4_K_M, контекст и temperature;
не использовать latest. Команды измерений — [tools/modelbench](../tools/modelbench/README.md).
Проверять размещение и фактическое окно через `ollama ps` во время генерации.

Офлайн-переключение не скачивает модель: штатно остановить backend, выполнить
`ollama stop gallery-guide-main:a01`, задать
`OLLAMA_MODEL=gallery-guide-reserve:a01` в экспортируемом окружении/.env и снова
запустить backend. Для возврата поменять tags местами. При новом запуске .env
должен быть заново экспортирован. Backend не перечитывает настройку на лету,
не выбирает резерв автоматически; незавершённые генерации обрабатываются штатным
shutdown/recovery. Контекст/temperature из Modelfile применяются и к backend.

До отключения интернета проверить оба tags командой
`B08_OLLAMA_MODEL=<tag> go test ./internal/adapters/outbound/ollama -run '^TestLocalModelFixtures$' -count=1 -v`.
Повторить после реального отключения интернета и после выключения Wi-Fi
через localhost; сохранить результаты и состояние сети. Для доступа из WSL
достаточно локального API; URL B08_OLLAMA_URL при необходимости задаётся отдельно.
Наличие OLLAMA_NO_CLOUD=1 или локального URL не заменяет офлайн-показ.


## Фотографии B09

Перед обновлением выполните `make migrate-up` (новая миграция 00005). PHOTO_DIR должен находиться на локальной записываемой файловой системе сервера; он создаётся до listener, права новых каталогов 0700, файлов 0600. Для контейнера смонтируйте отдельный записываемый каталог с владельцем UID 65532. Оригинал и JPEG-миниатюра находятся в `<photo_id>/image` и `<photo_id>/thumbnail.jpg`; каталог публикуется атомарным rename в пределах PHOTO_DIR. Раздача статикой этого каталога запрещена: доступ должен проверять будущий B10 по метаданным БД.

POST /api/v1/photos подключён; GET альбома/файлов и публикация относятся к B10. Передача фото в chat.send по-прежнему возвращает SERVICE_UNAVAILABLE. Клиент повторяет потерянный ответ с тем же UUID и тем же multipart-содержимым. Каждый POST учитывается в лимите 6/60 секунд на пользователя, независимо от переподключений.

WebP декодируется уже закреплённым golang.org/x/image v0.47.0 (BSD-3-Clause, https://pkg.go.dev/golang.org/x/image/webp@v0.47.0). Текст лицензии для передачи бинарного backend — [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md). Новых библиотек и внешних runtime-запросов нет. После аварийного завершения либо неразрешённого COMMIT могут остаться недоступные файлы без метаданных: автоматическая массовая очистка не выполняется. Не удаляйте staging/final каталоги работающего сервера вручную; очистку после остановки согласовывайте с состоянием БД.

SQL/HTTP/WS тесты B09 входят в штатные `go test ./...`, `go test -race ./...` и `make coverage`, используют отдельный TEST_DATABASE_URL и собственные случайные schemas; изображения и PHOTO_DIR создаются во временных каталогах. GPU/Ollama для проверки загрузки не требуются.

## B10: альбом и публикация

Новых миграций, зависимостей и переменных окружения нет: используются таблица photos и PHOTO_DIR из B09. Файлы и БД нужно сохранять вместе; публичные URL никогда не раскрывают пути на диске. GET альбома и обоих файлов требует действующей cookie, но не WS/ready. Публикация через chat.send доступна до ready; только владелец может открыть фото остальным вошедшим игрокам, отмены публикации в v1 нет. При недоступном/повреждённом файле сервер возвращает SERVICE_UNAVAILABLE без частичного HTTP-успеха.

Автоматические проверки B10 входят в обычные test/race/coverage. Они используют отдельную TEST_DATABASE_URL, случайные собственные schemas, временный PHOTO_DIR и тестовый каталог; Ollama/GPU не требуются. Ручной показ по Wi-Fi и с выключенным Wi-Fi остаётся отдельной проверкой оборудования.

## C01 — готовая офлайн-галерея

В `.env.example` задано `WORLD_DIR=./content/world`, `WORLD_VERSION=gallery_c01_v2`. Каталог, 72 JPEG и три профиля уже включены в репозиторий; перед штатным запуском выполните `make world-check`. Скачивание контента при старте не требуется. Полная подготовка, отдельный output без перезаписи рабочего мира, измерение размеров и правила обновления — [content/README.md](content/README.md). Для пересборки JSON нужен только Python 3 stdlib; Go runtime и клиенты Python не используют.

## I01 — frontend и переносимый комплект

По уточнению владельца Go раздаёт frontend напрямую, без отдельного reverse proxy.
`sh tools/build-frontend.sh ./data/frontend` собирает закреплённый commit внешнего
frontend с frozen lockfile и отключёнными mocks; нужен Bun, Git и интернет только
при подготовке. Существующий dist скрипт не перезаписывает. FRONTEND_DIST_DIR
подключается до listener; без переменной доступен API-only режим. Статика разрешает
GET/HEAD, выдаёт MIME/no-store/X-Request-ID, HTML-навигация без расширения возвращает
index.html. Отсутствующие assets и неизвестный `/api` возвращают 404, листинг,
скрытые файлы и symlinks недоступны. PHOTO_DIR/WORLD_DIR никогда не монтировать
внутрь dist. Раздача не требует сессии; API сохраняет свои проверки авторизации.

`python3 tools/offline-bundle.py prepare --frontend-dist ./data/frontend --output ./data/offline/i01`
создаёт новый каталог комплекта, проверяет клиентский runtime Ollama 0.40.13 и
записывает абсолютный исходный PHOTO_DIR в шаблон .env, не копируя фотографии.
Для другого постоянного пути используйте `--photo-dir /absolute/path/to/photos`. `python3 tools/offline-bundle.py verify ./data/offline/i01`
проверяет состав и hashes без Docker/сети. Проверку проводить до создания runtime
файлов внутри комплекта. Python 3.11+ нужен для подготовки/проверки, не для runtime;
Node нужен backend только для tools/build-contracts.mjs и validate-contracts.mjs.
Команды запуска из комплекта, firewall, WSL NAT/mirrored networking и сохранение
существующего volume/фото — [DEMO.md](DEMO.md). Go runtime lifecycle не изменяется.

## Sunday

Контракты — [api/sunday.md](api/sunday.md), задачи — [SUNDAY_BACKLOG.md](SUNDAY_BACKLOG.md).
S01–S07 интегрированы, manifest объявляет sunday_v1. Production frontend закреплён
на проверенном main SHA. Проверки и границы — [S07_ACCEPTANCE.md](S07_ACCEPTANCE.md).

## S01: runtime залов и административная роль

После обновления программы примените **новую** миграцию 00006 отдельной командой;
сервер и команда выдачи роли схему не изменяют. Миграции 00001–00005 и C01 неизменны.
Сначала зарегистрируйте обычный аккаунт через API, затем выполните:

```sh
make build
./bin/migrate up
./bin/admin grant --username NAME
./bin/gallery
```

`DATABASE_URL` должен быть экспортирован. Повтор grant безопасен; неизвестный username
даёт ненулевой exit, пользователей команда не создаёт. В Docker команда имеет вид
`docker compose run --rm --entrypoint /admin <backend-service> grant --username NAME`,
где `<backend-service>` — сервис вашего deployment: штатный Compose содержит только PostgreSQL.
Можно вызвать `/admin` через `docker run --rm --network ... --env DATABASE_URL --entrypoint /admin gallery-backend:local grant --username NAME`.
Offline kit включает `bin/admin`; уже собранный старый kit нужно пересобрать в новом каталоге.

`GET /api/v1/admin/me`, `/admin/rooms`, `/admin/rooms/{room_id}` и PUT последнего
требуют cookie и сохранённую роль на каждом запросе, игровой WS не нужен.
PUT принимает полный `{world_version,expected_revision,config}`, точный настроенный Origin,
JSON UTF-8 не более 65536 байт; лимит всех admin PUT — 10 за 10 секунд на пользователя.
Неизвестные, повторные, отсутствующие и null-поля запрещены. Ревизия начинается с 1;
успешная запись повышает её на 1. При 409 перечитайте runtime и повторите осознанную правку.
Редактируются только существующие гиды и допустимые позиции; стены, двери, C01 и версия
не меняются. Полный runtime ограничен 32768 байтами до сохранения.

Overlay хранится отдельно по `(world_version,room_id)`. Reconnect/restart читает БД,
новая WORLD_VERSION получает отдельный seed. До listener проверяются seed и текущие
сохранённые overlays всех залов. Неподходящий другому миру seed останавливает startup;
подготовка собственного seed требует отдельного изменения кода, fallback не применяется.
После SQL commit owning WS зала получают полный runtime, включая ещё not-ready.
При неизвестном исходе COMMIT HTTP возвращает 503, затронутые WS закрываются с 1011;
новое подключение перечитывает БД, RAM rollback не выполняется.

Первое событие — snapshot, затем runtime; переход — entered, затем runtime.
`room.sync {}` требует обычный UUID, owning connection, но допускается до ready;
лимит 2 за 10 секунд на user сохраняется при reconnect. Ответ runtime содержит request_id.
S05 добавляет полный NPC snapshot после runtime в ту же очередь; room.sync
коррелирует оба события. S07 включает capability `sunday_v1`.
Обычные guide.ask и игровые проверки используют effective placement runtime.

SQL-интеграции запускайте только с отдельным `TEST_DATABASE_URL`:

```sh
TEST_DATABASE_URL='postgres://USER:PASSWORD@127.0.0.1:55437/gallery_test?sslmode=disable' go test ./internal/adapters/outbound/postgres
```

Тесты создают и удаляют собственные схемы этой тестовой БД, рабочие БД/файлы не очищают.

## S04: эксплуатация светильника

`object.act`/`object.acted` используют уже применённую миграцию 00006, новые env и
миграции не нужны. Перед listener сервер освобождает stale holders текущего мира;
ошибка чтения/release блокирует запуск. После потери соединения, replacement/logout
или перехода holder возвращается на последний slot. Дверь подтверждается только
после release; ошибка release закрывает соединение 1011 и не подтверждает переход.

Один worker повторяет неуспешный cleanup с задержками 1/2/5 секунд, затем каждые 5 секунд.
Перед take/attach действует дополнительный lazy cleanup. Новая команда с revision,
устаревшей после cleanup, получает конфликт: клиент перечитывает `room.sync` с новым UUID.
Неизвестный результат COMMIT закрывает owning WS зала 1011 и разрешается чтением БД;
не повторяйте автоматически неподтверждённые действия. Shutdown освобождает объекты
в пределах существующего общего deadline 15 секунд; при недоступной БД окончательное
восстановление произойдёт на следующем успешном startup. Не очищайте holder SQL вручную.

Целевой прогон (после задания отдельного TEST_DATABASE_URL из раздела проверок):

```sh
go test -race ./internal/domain/runtime ./internal/usecase/runtime ./internal/usecase/game
go test ./internal/adapters/outbound/postgres -run TestLamp -v
```

Фактические результаты и ограничения — [S04_ACCEPTANCE.md](S04_ACCEPTANCE.md).

## S03 — административная телеметрия

Новых миграций и переменных окружения нет. С действующей admin cookie запрос
`GET /api/v1/admin/clients` возвращает только текущие FPS/ping и состояние образца;
WS у администратора не требуется. Клиент присылает `client.metrics` раз в 2 с и
немедленно отвечает `telemetry.pong` на nonce серверного probe. Оба события без
request_id и доступны до ready; остальные правила — [api/sunday.md](api/sunday.md), §7.
UI polling раз в 2 с находится в отдельном frontend. FPS заявлен браузером;
RTT относится к обмену с браузером, а не скорости SQL/Ollama. При disconnect/restart
измерения исчезают. Автоматические проверки — штатные test/race/coverage/contracts;
реальный LAN/rAF/фоновый браузер проверяется при S07.

## S02 — редактирование профилей гидов

Перед запуском обновлённого бинарника оператор отдельно применяет forward-only
миграцию 00007: `make migrate-up`, затем проверяет `make migrate-status`.
Startup только проверяет точную схему; рабочая БД агентом не мигрируется.
Новых env/зависимостей нет. Право admin назначается существующей командой S01.

GET `/api/v1/admin/guides` и GET/PUT `/api/v1/admin/guides/{guide_id}` доступны
admin cookie без игрового WS. PUT использует exact Origin, JSON ≤65536 байт и
общее с admin rooms окно 10/10 с/user. Пример — api/examples/sunday; PUT содержит
world_version, expected_revision и полный settings. При 409 перечитать профиль,
показать конфликт и не перезаписывать чужую правку автоматически. При 503 после
неизвестного COMMIT также перечитать; затронутые игровые соединения закрываются 1011.

Администратор выбирает только существующие материалы своей личности. Тексты знаний,
URL, uploads и фиксированные системные инструкции не редактируются. Память не
очищается, запущенные ответы сохраняют снимок; новые running получают актуальный
профиль. Данные старой WORLD_VERSION не переносятся. Проверки — [S02_ACCEPTANCE.md](S02_ACCEPTANCE.md).

## Лимит NPC: 10 посетителей на зал

В рамках #38 максимум и seed снижены с 40 до 10; admin PUT разрешает 0–10,
11 и выше возвращают VALIDATION_ERROR. Сам лимит внесён в #38; симуляция
добавлена в S05 (#41). S07 включает capability sunday_v1.

Оператор отдельно выполняет `make migrate-up`, затем `make migrate-status` до
запуска обновлённого backend. Новая forward-only миграция 00008 уменьшает все
сохранённые npc_count>10 до 10 во всех WORLD_VERSION, повышая revisions этих
комнат на 1; остальные config/lamp/holder не меняются. Значения 0–10 и их revisions
сохраняются. Если revision нельзя увеличить в пределах safe integer, миграция
откатывается целиком; автоматического сброса revision нет. Startup требует новую
схему и не меняет её. Рабочую БД агент не мигрирует; новых env нет.

## S05 — симуляция NPC

В каждом зале с owning WS работают до 10 посетителей из config.npc_count. При
startup проверяются маршруты неизменяемой геометрии всех залов; ошибка подготовки
запрещает listener. Существующий игровой цикл выполняет NPC-шаг раз в 200 мс,
без SQL/Ollama и без добавления препятствий игрокам. При задержке scheduler
пропущенные шаги не догоняются, чтобы не создавать скачки и залпы сообщений.

После потери последнего owning WS симуляция удаляется до SQL-cleanup. Следующий
вход начинает новый instance с seq=1; непрерывный replacement сохраняет instance.
Принятый текст чата вызывает talking на 3 с у посетителей того же зала в радиусе
3 м; фото без текста не вызывает реакцию. NPC-позиции и реакции не сохраняются.
Новых env/миграций/ассетов нет; для лимита 10 схема уже должна включать 00008.
Анимации и интерполяция относятся к отдельному frontend. Проверки:

```sh
go test ./internal/domain/npc ./internal/usecase/game
go test ./cmd/server -run TestNPCPlansForAllC01Rooms
go test -race ./internal/adapters/inbound/websocket -run TestNPC
# С отдельной TEST_DATABASE_URL, как описано выше:
go test ./internal/adapters/outbound/postgres -run TestRuntimeHTTPWebSocketDurabilityIntegration
```


## S06 — изменение экспозиции через гида

Новые миграции, переменные окружения и ассеты не нужны. После world.ready отправьте
`guide.action` по [Sunday §6](api/sunday.md#6-просьбы-к-гиду-guideaction) с текущими
movement_epoch и expected_revision. Для повторного show нужен новый UUID и новая
revision. После ошибки перечитайте runtime через room.sync; не повторяйте автоматически
команду после reconnect. Учебный стенд остаётся одним объектом «Цвет и свет» в (6,0,3).
Проверки на отдельной тестовой БД (TEST_DATABASE_URL по разделу проверок):

```sh
go test ./internal/domain/runtime ./internal/usecase/runtime ./internal/usecase/game ./internal/adapters/inbound/websocket -run TestGuideAction
go test ./internal/adapters/outbound/postgres -run TestGuideActionWebSocketDurabilityAndConversationIntegration -v
```

Полный test/race/coverage прогон включает эти проверки и регрессии B07/B08.

## S07 — обновление существующего офлайн-запуска

Подготовка не перезаписывает существующий dist/комплект. После `make build`:

```sh
sh tools/build-frontend.sh ./data/frontend-s07
python3 tools/offline-bundle.py prepare --frontend-dist ./data/frontend-s07 --output ./data/offline/s07-final
python3 tools/offline-bundle.py verify ./data/offline/s07-final
```

Для повторной подготовки выбирайте новые имена выходных каталогов. В комплекте
обязательны исполняемые `bin/gallery`, `bin/migrate`, `bin/admin`; hashes каждого
фиксируются в bundle-manifest.json. Миграции 00006–00008 поставляются как есть,
новой миграции S07 нет. В корне комплекта после настройки и экспорта доверенного .env:

```sh
./bin/migrate up
./bin/migrate status
# Существующий зарегистрированный аккаунт; команда не создаёт пользователя.
./bin/admin grant --username NAME
./bin/gallery
```

Миграции/выдачу роли оператор выполняет отдельно до игрового запуска. При обновлении
сохраните прежние Compose project/volume и абсолютный PHOTO_DIR; порядок Ollama,
LAN и localhost — [DEMO.md](DEMO.md). Рабочие данные в комплект не копируются.
Для сквозного теста с реальным dist дополнительно задайте абсолютный
`SUNDAY_TEST_FRONTEND_DIST` вместе с отдельным `TEST_DATABASE_URL`; без dist тест
использует временную HTML-страницу, а без тестовой БД явно пропускается.
По умолчанию мир синтетический; `SUNDAY_TEST_WORLD_DIR=/absolute/content/world`
включает отдельную uninstrumented проверку C01. Каждый запуск владеет случайной
SQL-схемой и удаляет только её.

## Обновление демогалереи v2 (задача №52)

Поставляемый мир `gallery_c01_v2` содержит 24 зала с номерами `01.`–`24.`;
обычный seed — 5 NPC, административный диапазон остаётся 0–10.
Перед обновлением сохраните резервную копию БД и остановите backend.
Соберите новые бинарники (`make build`), установите поставляемый каталог мира,
сохраните прежние DATABASE_URL, PHOTO_DIR и Compose volume. Выполните:

```sh
# С существующим окружением: не перезаписывайте .env примером.
make migrate-up
make migrate-status
# Обновите WORLD_VERSION=gallery_c01_v2 в существующем .env и перечитайте окружение.
set -a
. ./.env
set +a
make world-check
./bin/gallery
```

Миграция 00009 переносит overlays 24 C01-залов из v1 в v2, сохраняя прочие
поля config и лампы; количество посетителей выставляется в 5. Миграция 00008
перед этим приводит исторические 40 к 10. При наличии overlay v2 сохраняются
его настройки, меняются только npc_count и revision. Старые overlays остаются;
другие миры и пользовательские данные не затрагиваются миграцией 00009.
Overrides трёх гидов копируются с прежними settings/revision, существующие v2
не перезаписываются. Переполнение revision откатывает миграцию; revisions
не сбрасываются. После применения повторный migrate-up сохраняет даже новое
административное количество NPC. Залы без сохранённого overlay используют seed=5.
Обычная очистка владельца лампы после рестарта работает прежним способом.

Проверьте 24 зала: 5 посетителей, движение и реакции на чат, переход и рестарт,
доступ к картинам/гидам и прежним фото/памяти. Это ручная приёмка на демосреде.
Закреплённый frontend выводит Room.title при возвращении в зал, но постоянной
подписи текущего зала в HUD нет. Её добавление — отдельная frontend-задача;
сервер передаёт полный номер в title, клиент не должен вычислять его повторно.
