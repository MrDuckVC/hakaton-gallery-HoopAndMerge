# Gallery Backend

Разработка ведётся только по Clean Architecture: domain <- usecase <- adapters. Зависимости направлены внутрь, реализации связывает cmd/server. Требование обязательно и для MVP; обход слоёв ради сроков запрещён. Подробности — docs/ARCHITECTURE.md (для файлов в docs/ — ARCHITECTURE.md).

Backend многопользовательской 3D-галереи: посещение тематических залов, общий чат, фотографии и локальные ИИ-гиды с персональной памятью. Во время игры интернет не требуется; клиенты подключаются по LAN/Wi-Fi.

Репозиторий: https://github.com/MrDuckVC/hakaton-gallery-HoopAndMerge. Frontend разрабатывается отдельно.

Единственный сервер — ноутбук с RTX 4070 Laptop: backend, PostgreSQL, Ollama и контент находятся на нём. На остальных ноутбуках нужен только браузер. Обязательны два показа: несколько игроков по Wi-Fi без интернета, затем игра с локальным ИИ в браузере на серверном ноутбуке с выключенным Wi-Fi. Модель выбирается по замерам на этом ноутбуке. Требования и приёмка — docs/REQUIREMENTS.md; задачи — docs/ISSUE_BACKLOG.md.

## Стек
Go net/http, PostgreSQL, pgx/v5, coder/websocket, локальный Ollama. Версии фиксируются в go.mod/go.sum и docker-compose.yml. Один процесс Go обслуживает HTTP и WS, одна локальная модель — всех гидов.

## Запуск

Go-модуль `github.com/MrDuckVC/hakaton-gallery-HoopAndMerge`, Go >=1.27.1; pgx/v5 и goose закреплены в go.mod/go.sum.

```sh
export PATH="/usr/local/go/bin:$PATH" # если требуется
cp .env.example .env
# Отредактируйте .env: пароли, origins, версия мира и заранее подготовленный model tag.
# Подготовьте WORLD_DIR/catalog.json и файлы по docs/WORLD_CONTENT.md.
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

Manifest доступен действующей сессии. Для rooms/assets нужны version и активный owning WS с совпадающими пользователем и сессией; login сам по себе владения не даёт. Каталог уже обеспечивает настоящий spawn для register и `/me`. B04 подключает `/api/v1/ws`, snapshot, presence и замену соединения. Ollama и раздача frontend dist ещё не подключены.

Команды миграций, отдельная тестовая БД, Docker smoke-test и подготовка офлайн-работы — в docs/DEVELOPMENT.md. Docker-образ собирается через `make docker-build` и содержит `/gallery` и административный `/migrate`.

## API и документы
- docs/api/contract.md — нормативный контракт пользователя; имеет приоритет над старыми планами.
- docs/api/openapi.yaml — HTTP; docs/api/websocket.md — WS.
- docs/api/examples/ — готовые JSON-fixtures для параллельной разработки клиента.
- docs/ARCHITECTURE.md, docs/DATA_MODEL.md — устройство сервера.
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

B04: login → `/api/v1/ws` → `world.snapshot` → загрузка room/assets. Первое событие всегда snapshot; замена вкладки закрывает старую с 4009. B05 подтверждает `world.ready`, проверяет движение и переходы и рассылает полный `players.state` 10 Гц. До ready игровые действия возвращают `GAME_NOT_READY`; B06 подключает чат до ready и осмотр после ready; B07 сохраняет очередь и историю гидов, cancel доступен до ready; до B08 принятый вопрос завершается `AI_UNAVAILABLE`. Позиции сохраняются каждые 5 секунд, при отключении и shutdown; ошибка периодического save закрывает WS с 1011.

Также доступны `make test`, `make vet`, `make fmt`. Для конкурентного кода — `go test -race ./...`. Тестируются каталог/бюджет/выдача файлов B03, конфигурация, ошибки запуска, lifecycle, HTTP-контракт, миграции и сценарии B02: KDF, сессии, Origin, лимиты и восстановление положения через порт мира. `make coverage` запускает тесты с race detector и требует суммарное покрытие всех Go-пакетов не ниже 60%; отчёт — `coverage.out`. Интеграционные тесты требуют отдельный `TEST_DATABASE_URL`; без него они явно пропускаются. `make lint` при первом запуске скачивает golangci-lint версии из Makefile; для офлайн-работы установите его заранее.

`make contracts` генерирует и проверяет схемы API; Node нужен только этим инструментам.

Проект содержит запускаемый backend с PostgreSQL и каталогом, с WS-входом, загрузкой залов, авторитетным движением и сохранением. B06 подключает общий чат и сохранение осмотра с личной памятью; B07 подключает очередь и историю гидов; фото ещё не подключены; готовый образовательный пакет мира в репозиторий не входит. Примеры используют демонстрационные ID и размер картинки; реальный asset не включён. Карта документации — docs/INDEX.md.

GitHub Actions (`.github/workflows/ci.yml`) запускается на push, pull_request и вручную: форматирование, vet, race-тесты с порогом 60%, golangci-lint и Go-сборка. После успешных проверок собирается Docker-образ и проверяется запуск контейнера с отдельной тестовой PostgreSQL и штатное завершение по SIGTERM. Образ не публикуется в registry.

B06: `chat.send` рассылает общий чат без хранения истории. `exhibit.inspect` проверяет текущий зал, дистанцию и LOS, затем атомарно сохраняет осмотр и единственное событие памяти на пользователя/экспонат. Повтор обновляет время и контекст между версиями мира. Перед запуском примените новую миграцию через `make migrate-up`. Публикация `photo_id` до B10 возвращает `SERVICE_UNAVAILABLE`.

B07: перед запуском примените миграции 00003–00004 через `make migrate-up`. Вопрос сохраняется до `guide.accepted`; повторы UUID не создают дубликаты, результаты восстанавливаются через `/api/v1/guides/{guide_id}/generations` и `generations/by-request/{request_id}`. До подключения Ollama в B08 production завершает принятые вопросы `failed / AI_UNAVAILABLE`; fake-модель используется только в тестах. Рестарт завершает pending как `SERVER_RESTARTED` до открытия listener.
