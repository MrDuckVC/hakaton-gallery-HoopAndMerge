# Локальная разработка и конфигурация

B01: backend слушает HTTP, проверяет PostgreSQL/схему и обрабатывает SIGINT/SIGTERM. B02 подключает API аккаунтов и cookie-сессии. B03 добавляет startup-валидацию локального мира и HTTP выдачу через порт игрового владения. B04 подключает WS, snapshot, presence и HTTP-владение текущим залом. B05 реализует ready/движение/двери и сохранение. B06 подключает чат, осмотр и личные события памяти. B07 сохраняет очередь/историю, B08 подключает локальный Ollama и персональный контекст. B09 подключает загрузку фото; B10 подключает альбом, защищённое чтение файлов и публикацию. Раздача frontend ещё не реализована. Все ответы приложения имеют `X-Request-ID` и `Cache-Control: no-store`; неизвестные маршруты возвращают JSON `404 NOT_FOUND`.

## Подготовка и запуск

Go >=1.27.1, Docker Compose для локальной PostgreSQL. Версии pgx/v5 и goose закреплены в go.mod/go.sum. Если Go установлен в `/usr/local/go`, добавьте `/usr/local/go/bin` в PATH.

```sh
cp .env.example .env
# Замените пароли, origins, WORLD_VERSION и OLLAMA_MODEL.
# Подготовьте WORLD_DIR/catalog.json по WORLD_CONTENT.md.
# .env — доверенный shell-файл; backend сам его не загружает.
set -a
. ./.env
set +a
docker compose up -d --wait postgres
make build                         # bin/gallery и bin/migrate
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

Multi-stage Dockerfile: `golang:1.27.1`, статические `/gallery` и `/migrate`, финальный `scratch`, пользователь 65532:65532. Миграции встроены в оба бинарника. Smoke-test выполняет миграции, стартует контейнер на localhost:18080, проверяет HTTP 404 и успешное завершение по SIGTERM. Использует host networking на Linux для доступа к тестовой БД и удаляет только свой контейнер. Для Docker Desktop/WSL можно задать `SMOKE_DOCKER_NETWORK` с именем уже созданной обычной bridge-сети (без `--internal`, чтобы работала публикация порта): тестовая PostgreSQL должна быть подключена к ней, а `DATABASE_URL` должен использовать её сетевой alias и порт 5432; HTTP-порт 18080 публикуется на loopback. Старый запуск с `--network none` без PostgreSQL теперь ожидаемо завершается с ошибкой.

GitHub Actions: gofmt, vet, race/coverage с PostgreSQL integration tests, lint, build, генерация и проверка отсутствия drift контрактов, затем Docker build и smoke-test с отдельной PostgreSQL. Workflow не публикует образ и не выполняет deploy.

До отключения интернета: `go mod download`, `make lint` для установки закреплённого линтера, `docker compose pull postgres`, `make build`, `make docker-build`; отдельно подготовить модель и контент по A01/C01/I01. Runtime использует PostgreSQL, локальные файлы мира и стандартную криптографию Go, без внешних API. Не выполнять `tools/write-docs.mjs` поверх ручных изменений. `world-check` пока не реализован.

Frontend остаётся в отдельном репозитории. Proxy `/api` должен поддерживать WS и сохранять Origin. Bind `0.0.0.0` не является браузерным origin; адреса LAN и localhost задаются явно. Пакеты мира доступны через B03; B04 уже подключает игровые соединения. B05 подключает движение/готовность/переходы и полный state 10 Гц; фото и раздача dist появятся в следующих задачах.

## Требуемая топология демонстрации

Единственный сервер — ноутбук с RTX 4070 Laptop. PostgreSQL, backend, Ollama, модели, frontend dist, мир и фотографии размещаются на нём. На других ноутбуках — только браузер; установку серверных инструментов на них не требовать. Все runtime-ресурсы frontend должны входить в локальную сборку.

Подготовить оба origin: адрес сервера в LAN и localhost. Сначала показывать многопользовательский режим по Wi-Fi без интернета, затем выключить Wi-Fi на сервере и показать автономную игру с ИИ через localhost. Bind 0.0.0.0 не является origin браузера; exact Origin allowlist должен включать реальные адреса обоих режимов. Офлайн-комплект и реальные команды раздачи dist/запуска всех сервисов ещё предстоит реализовать в I01; сама эта запись не означает готовности запуска.

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
