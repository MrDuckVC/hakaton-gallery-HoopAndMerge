# Gallery Backend

Разработка ведётся только по Clean Architecture: domain <- usecase <- adapters. Зависимости направлены внутрь, реализации связывает cmd/server. Требование обязательно и для MVP; обход слоёв ради сроков запрещён. Подробности — docs/ARCHITECTURE.md (для файлов в docs/ — ARCHITECTURE.md).

Backend многопользовательской 3D-галереи: посещение тематических залов, общий чат, фотографии и локальные ИИ-гиды с персональной памятью. Во время игры интернет не требуется; клиенты подключаются по LAN/Wi-Fi.

Репозиторий: https://github.com/MrDuckVC/hakaton-gallery-HopeAndMerge. Frontend разрабатывается отдельно.

Единственный сервер — ноутбук с RTX 4070 Laptop: backend, PostgreSQL, Ollama и контент находятся на нём. На остальных ноутбуках нужен только браузер. Обязательны два показа: несколько игроков по Wi-Fi без интернета, затем игра с локальным ИИ в браузере на серверном ноутбуке с выключенным Wi-Fi. Модель выбирается по замерам на этом ноутбуке. Требования и приёмка — docs/REQUIREMENTS.md; задачи — docs/ISSUE_BACKLOG.md.

## Стек
Go net/http, PostgreSQL, pgx/v5, coder/websocket, локальный Ollama. Версии фиксируются в go.mod/go.sum и docker-compose.yml. Один процесс Go обслуживает HTTP и WS, одна локальная модель — всех гидов.

## Запуск

Go-модуль `github.com/MrDuckVC/hakaton-gallery-HopeAndMerge`, Go >=1.27.1; pgx/v5 и goose закреплены в go.mod/go.sum.

```sh
export PATH="/usr/local/go/bin:$PATH" # если требуется
cp .env.example .env
# Отредактируйте .env: пароли, origins, версия мира и заранее подготовленный model tag.
# Сначала соберите frontend: sh tools/build-frontend.sh ./data/frontend
# Для API-only разработки удалите FRONTEND_DIST_DIR из .env.
# Готовый мир C01 уже в content/world; WORLD_VERSION=gallery_c01_v1.
set -a
. ./.env
set +a
docker compose up -d --wait postgres
make build
make migrate-up
./bin/gallery
```

Сервер проверяет конфигурацию, соединение с PostgreSQL и точный набор применённых миграций, затем слушает `HTTP_ADDR`. Схему при старте не меняет. SIGINT/SIGTERM запускает завершение с общим deadline 15 секунд. `make migrate-status` показывает миграции; повторный `make migrate-up` сохраняет данные. Compose хранит PostgreSQL в постоянном named volume.

B01 реализует запуск и lifecycle; B02 — аккаунты и cookie-сессии; B03 — проверяемый локальный каталог и `/api/v1/world/manifest`, `/api/v1/world/rooms/{room_id}`, `/api/v1/world/assets/{asset_id}`. До открытия HTTP listener проверяются геометрия, ссылки, spawns, точные размеры, бюджет с резервом фото и максимальный snapshot. Без валидного WORLD_DIR запуск завершается ошибкой. Формат контента — docs/WORLD_CONTENT.md.

Manifest доступен действующей сессии. Для rooms/assets нужны version и активный owning WS с совпадающими пользователем и сессией; login сам по себе владения не даёт. Каталог уже обеспечивает настоящий spawn для register и `/me`. B04 подключает `/api/v1/ws`, snapshot, presence и замену соединения. B08 подключает локальный Ollama; I01 раздаёт отдельно собранный frontend dist через тот же Go HTTP listener.

Команды миграций, отдельная тестовая БД, Docker smoke-test и подготовка офлайн-работы — в docs/DEVELOPMENT.md. Docker-образ собирается через `make docker-build` и содержит `/gallery`, административные `/migrate` и `/admin`.

## Офлайн-показ I01

[docs/DEMO.md](docs/DEMO.md) содержит подготовку комплекта, холодный запуск,
LAN-показ и localhost с выключенным Wi-Fi. Go раздаёт frontend, API и WS на
одном origin; PostgreSQL в Compose, Ollama с GPU на хосте. Подготовка frontend
из отдельного репозитория закреплена на commit ветки `fix/day2-first-demo`.
`python3 tools/offline-bundle.py prepare --frontend-dist ./data/frontend --output ./data/offline/i01`
собирает комплект без рабочих секретов/фото/БД, `verify` проверяет его hashes.
Статус проверок — [docs/I01_ACCEPTANCE.md](docs/I01_ACCEPTANCE.md).

## API и документы
- docs/api/contract.md — нормативный контракт пользователя; имеет приоритет над старыми планами.
- docs/api/openapi.yaml — HTTP; docs/api/websocket.md — WS.
- docs/api/examples/ — готовые JSON-fixtures для параллельной разработки клиента.
- docs/ARCHITECTURE.md, docs/DATA_MODEL.md — устройство сервера.
- docs/MODEL_BENCHMARK.md — реальные замеры A01, модели, контекст и статус офлайн-проверки.
- docs/IMPLEMENTATION_PLAN.md, docs/TESTING.md — порядок реализации и проверки.
- AGENTS.md — инструкции ИИ-агентам.

## Проверки

```sh
go build ./...
go vet ./...
go test ./...
make lint
make coverage
make contracts
```

B04: login → `/api/v1/ws` → `world.snapshot` → загрузка room/assets. Первое событие всегда snapshot; замена вкладки закрывает старую с 4009. B05 подтверждает `world.ready`, проверяет движение и переходы и рассылает полный `players.state` 10 Гц. До ready игровые действия возвращают `GAME_NOT_READY`; B06 подключает чат до ready и осмотр после ready; B07 сохраняет очередь и историю гидов, cancel доступен до ready; B08 выполняет принятые вопросы через локальный Ollama с личной памятью. Позиции сохраняются каждые 5 секунд, при отключении и shutdown; ошибка периодического save закрывает WS с 1011.

Также доступны `make test`, `make vet`, `make fmt`. Для конкурентного кода — `go test -race ./...`. Тестируются каталог/бюджет/выдача файлов B03, конфигурация, ошибки запуска, lifecycle, HTTP-контракт, миграции и сценарии B02: KDF, сессии, Origin, лимиты и восстановление положения через порт мира. `make coverage` запускает тесты с race detector и требует суммарное покрытие всех Go-пакетов не ниже 60%; отчёт — `coverage.out`. Интеграционные тесты требуют отдельный `TEST_DATABASE_URL`; без него они явно пропускаются. `make lint` при первом запуске скачивает golangci-lint версии из Makefile; для офлайн-работы установите его заранее.

`make contracts` генерирует и проверяет схемы API; Node нужен только этим инструментам.

Проект содержит запускаемый backend с PostgreSQL и каталогом, с WS-входом, загрузкой залов, авторитетным движением и сохранением. B06 подключает общий чат и сохранение осмотра с личной памятью; B07 подключает очередь и историю гидов, B08 — Ollama и личный контекст; B09 подключает загрузку фото; B10 подключает альбом, защищённое чтение файлов и публикацию в чат; готовый образовательный пакет C01 включён в `content/world`: 24 зала, 72 музейные репродукции и три художника-гида. API-примеры остаются независимыми демонстрационными fixtures. Подготовка, права, бюджет и приёмка — [docs/content/README.md](docs/content/README.md). Карта документации — docs/INDEX.md.

GitHub Actions (`.github/workflows/ci.yml`) запускается на push, pull_request и вручную: форматирование, vet, race-тесты с порогом 60%, golangci-lint и Go-сборка. После успешных проверок собирается Docker-образ и проверяется запуск контейнера с отдельной тестовой PostgreSQL и штатное завершение по SIGTERM. Образ не публикуется в registry.

B06: `chat.send` рассылает общий чат без хранения истории. `exhibit.inspect` проверяет текущий зал, дистанцию и LOS, затем атомарно сохраняет осмотр и единственное событие памяти на пользователя/экспонат. Повтор обновляет время и контекст между версиями мира. Перед запуском примените новую миграцию через `make migrate-up`. B10 публикует `photo_id` владельца транзакционно до рассылки.

B07: перед запуском примените миграции 00003–00004 через `make migrate-up`. Вопрос сохраняется до `guide.accepted`; повторы UUID не создают дубликаты, результаты восстанавливаются через `/api/v1/guides/{guide_id}/generations` и `generations/by-request/{request_id}`. B08 использует настроенный Ollama; при его недоступности принятый вопрос завершается `failed / AI_UNAVAILABLE`, прогулка продолжает работать. Fake-модель используется только в тестах. Рестарт завершает pending как `SERVER_RESTARTED` до открытия listener.


B08: серверные профили теперь имеют строгий формат. Подготовьте профили и новую WORLD_VERSION до обновления; см. [docs/GUIDES.md](docs/GUIDES.md). Там же — две демонстрационные личности, предел входа 16 000 кодовых точек, детерминированная память и проверка реальной модели. Подтверждение GPU-производительности/офлайн-показа требует A01 и C01; fake-тесты его не заменяют.


B09: `POST /api/v1/photos` сохраняет JPEG/static WebP и JPEG-миниатюру с устойчивой идемпотентностью по user+UUID. Повтор после перехода возвращает прежнее фото; все POST учитываются в лимите 6/60 секунд. Примените миграцию 00005 через `make migrate-up`, подготовьте записываемый PHOTO_DIR по [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md). B10 подключает альбом, чтение файлов и публикацию.

B10: `GET /api/v1/photos` возвращает личный альбом с keyset-пагинацией (limit 1–6, по умолчанию 6). Оригинал `/photos/{photo_id}/image` и JPEG-миниатюра `/thumbnail` доступны владельцу; после публикации через `chat.send` — любому вошедшему игроку. Чужое приватное фото возвращает 404. Чтение не требует игрового WS. Публикация необратима, повтор разрешён; новые миграции и настройки не нужны.

C01: `make world-check` проверяет поставляемый мир штатным валидатором и печатает точные размеры. При любых изменениях файлов или профилей нужна новая WORLD_VERSION и перезапуск. Все изображения и знания локальные; Python 3 нужен только для необязательной пересборки JSON авторским инструментом.

## Согласованное расширение Sunday

Целевые контракты для параллельной разработки: [api/sunday.md](docs/api/sunday.md),
[TypeScript DTO](docs/api/sunday.types.ts), [JSON-примеры](docs/api/examples/sunday/).
S01 реализует сохраняемый runtime залов, редактирование комнат через admin API и
назначение роли командой `./bin/admin grant --username NAME` после отдельного применения
миграции 00006. `world.runtime` следует за snapshot/entered; `room.sync` перечитывает БД.
S04 реализует интерактивные команды светильника и восстановление holder после сбоев;
новые миграции/config не нужны. S03 реализует client.metrics, telemetry.probe/pong и GET /api/v1/admin/clients только для admin. Измерения живут в RAM текущего соединения; новые миграции/config не нужны. S02 реализует admin guides и снимок профиля при старте running; перед запуском отдельно примените новую миграцию 00007. S05 реализует NPC-посетителей 5 Гц в активных залах с реакциями на игроков/чат, без SQL и Ollama на tick. guide.action остаётся задачей S06. `sunday_v1` не объявляется до интеграции S07.
Основа №2–14 не переписывается. Задачи и порядок — [SUNDAY_BACKLOG.md](docs/SUNDAY_BACKLOG.md).

Максимум и seed NPC-посетителей — **10 на зал**, admin npc_count=0–10. Перед
запуском версии с этим лимитом оператор отдельно применяет миграцию 00008 через
`make migrate-up`: старые значения >10 уменьшаются до 10 с повышением room revision.
