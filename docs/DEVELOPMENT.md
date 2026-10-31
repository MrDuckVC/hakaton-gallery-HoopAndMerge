# Локальная разработка и конфигурация

B01: backend слушает HTTP, проверяет PostgreSQL/схему и обрабатывает SIGINT/SIGTERM. Игровые сценарии, WS, загрузка мира, раздача frontend и интеграция Ollama ещё не реализованы. Текущие HTTP-запросы возвращают JSON `404 NOT_FOUND` с `X-Request-ID` и `Cache-Control: no-store`.

## Подготовка и запуск

Go >=1.27.1, Docker Compose для локальной PostgreSQL. Версии pgx/v5 и goose закреплены в go.mod/go.sum. Если Go установлен в `/usr/local/go`, добавьте `/usr/local/go/bin` в PATH.

```sh
cp .env.example .env
# Замените пароли, origins, WORLD_VERSION и OLLAMA_MODEL.
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
| OLLAMA_MODEL | Обязательный заранее подготовленный tag без пробелов; выбрать по A01 |
| LOG_LEVEL | `info`; доступны `debug`, `info`, `warn`, `error` |

Явно пустые значения отклоняются. `.env.example` — шаблон; `change-me`, адреса и model tag замените. Настройки мира/фото/cookie/origins/Ollama предназначены и для следующих подсистем; B01 не реализует их игровые возможности. Нет startup-проверки Ollama и файлов мира. При реализации B03/B07 добавить world validation и recovery до listeners согласно ARCHITECTURE.md.

Compose использует `postgres:18.6-bookworm`, постоянный volume на `/var/lib/postgresql`, healthcheck и порт только `127.0.0.1:${POSTGRES_PORT:-5432}`. `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB` должны соответствовать `DATABASE_URL`. Изменение этих переменных не меняет credentials уже инициализированного volume.

Startup deadline 10 секунд; DB connect/query timeout 5 секунд; максимум 10 соединений. HTTP: headers 5 секунд, read 15, write 30, idle 60. Shutdown — общий deadline 15 секунд с независимым контекстом: остановить listener/новые запросы, lifecycle-порты отмены генераций/сохранения позиций/закрытия WS, HTTP drain, закрытие пула. При ошибке выполняется последующая очистка; по deadline последующие lifecycle cleanup-вызовы и закрытие пула предпринимаются best-effort без ожидания зависших участников, HTTP закрывается принудительно, процесс завершается с ненулевым кодом. Сейчас игровые участники отсутствуют. Не держать SQL-транзакции при сетевом/file/LLM I/O.

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

До отключения интернета: `go mod download`, `make lint` для установки закреплённого линтера, `docker compose pull postgres`, `make build`, `make docker-build`; отдельно подготовить модель и контент по A01/C01/I01. Runtime B01 использует только PostgreSQL, без внешних API. Не выполнять `tools/write-docs.mjs` поверх ручных изменений. `world-check` пока не реализован.

Frontend остаётся в отдельном репозитории. Proxy `/api` должен поддерживать WS и сохранять Origin. Bind `0.0.0.0` не является браузерным origin; адреса LAN и localhost задаются явно. Пакеты мира/фото и раздача dist появятся в следующих задачах.

## Требуемая топология демонстрации

Единственный сервер — ноутбук с RTX 4070 Laptop. PostgreSQL, backend, Ollama, модели, frontend dist, мир и фотографии размещаются на нём. На других ноутбуках — только браузер; установку серверных инструментов на них не требовать. Все runtime-ресурсы frontend должны входить в локальную сборку.

Подготовить оба origin: адрес сервера в LAN и localhost. Сначала показывать многопользовательский режим по Wi-Fi без интернета, затем выключить Wi-Fi на сервере и показать автономную игру с ИИ через localhost. Bind 0.0.0.0 не является origin браузера; exact Origin allowlist должен включать реальные адреса обоих режимов. Офлайн-комплект и реальные команды раздачи dist/запуска всех сервисов ещё предстоит реализовать в I01; сама эта запись не означает готовности запуска.

До выбора OLLAMA_MODEL выполнить A01 на реальном ноутбуке: измерить доступную память и время ответа, в том числе рядом с браузерной сценой; зафиксировать точный основной и резервный tags, квантование и контекст. Скачать оба до отключения интернета.
