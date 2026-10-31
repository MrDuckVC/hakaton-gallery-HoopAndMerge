# Локальная разработка и конфигурация

Статус: Go-модуль и `cmd/server` инициализированы. Go >=1.27.1. Точка входа выводит сообщение и завершается; HTTP/WS, PostgreSQL, Ollama и миграции ещё не реализованы. Внешних Go-зависимостей нет, поэтому `go.sum` пока не требуется.

```sh
# Если go отсутствует в PATH, но установлен в /usr/local/go:
export PATH="/usr/local/go/bin:$PATH"
make build                  # bin/gallery
make run                    # запуск каркаса, затем завершение
make test
make coverage               # race + порог покрытия 60%, coverage.out
make vet
make fmt
make lint                   # первая установка требует интернета
make contracts              # генерация и проверка схем API
```

Прямые проверки: `go build ./...`, `go vet ./...`, `go test ./...`. Для конкурентных изменений: `go test -race ./...`. Реальных тестов бизнес-правил пока нет. Makefile использует `GO ?= go`; при необходимости путь можно передать как `make build GO=/usr/local/go/bin/go`. Для линтера каталог Go также должен быть в PATH.

Цели `migrate-up` и `world-check` пока не реализованы. `.env.example` пуст, Compose содержит только заготовку `services:`; запуск PostgreSQL/Ollama через Compose ещё не настроен.

Предлагаемые .env.example ключи без секретных значений:
- HTTP_ADDR=0.0.0.0:8080
- DATABASE_URL=postgres://gallery:change-me@localhost:5432/gallery?sslmode=disable (только local)
- ALLOWED_ORIGINS=http://localhost:5173,http://SERVER_LAN_IP:8080
- COOKIE_SECURE=false (явный local HTTP; HTTPS=true)
- WORLD_DIR=./content/world; WORLD_VERSION=world_v1
- PHOTO_DIR=./data/photos
- OLLAMA_BASE_URL=http://127.0.0.1:11434
- OLLAMA_MODEL=<заранее скачанный проверенный tag>
- LOG_LEVEL=info

Значения с placeholder заменить до запуска. Длины/тайм-ауты/лимиты contract.md фиксированы v1; не менять незаметно через env. Пароль/KDF параметры и connection pool настраиваются отдельно с документированными defaults.

БД можно Compose если уже установлен Docker; имя сервиса прочитать в docker-compose.yml. Не использовать down -v для обычной остановки. PostgreSQL volume и photo directory постоянные. Ollama на хосте Windows/WSL/GPU: проверить доступ Go-процесса отдельно; localhost внутри контейнера не равен хосту. API Ollama не открывать в LAN без необходимости. Подготовить модель, drivers, Go deps, образ PostgreSQL и весь контент до отключения интернета.

Frontend — отдельный checkout. В dev его proxy /api -> backend поддерживает WS и сохраняет cookie Origin semantics. Для показа один origin: backend может раздавать полученный dist, но frontend source не переносить в этот репозиторий. Адрес origin должен совпадать с адресом браузера, не bind0.0.0.0. Проверить firewall и отсутствие Wi-Fi client isolation.

Контент: отдельные неизменяемые room JSON+JPEG/WebP; серверные door spawn, profiles и knowledge не отдавать клиенту. Сборщик вычисляет descriptor_bytes, assets.bytes,total, budget. Startup проверяет связность дверей и безопасные spawns/LOS/коллизии; каждый room package+3MiB помещается в10%. Примеры docs/api/examples не содержат image asset и не запускаются как готовый world.

Документы: node tools/build-contracts.mjs; node tools/validate-contracts.mjs. tools/write-docs.mjs — первоначальное создание Markdown, не применять поверх ручных правок. Миграции выполнять выбранным в проекте инструментом; точную команду добавить после интеграции. Тестовую DB задавать отдельно, не переиспользовать рабочую.

## Docker и CI

```sh
make docker-build
# Эквивалент:
docker build -f build/main.Dockerfile -t gallery-backend:local .
docker run --rm --network none gallery-backend:local
```

Multi-stage Dockerfile использует официальный `golang:1.27.1` и финальный `scratch`, статическую сборку без CGO и пользователя 65532:65532. Точка входа пока только логирует статус и завершается; это ожидаемо и в контейнере. Docker Desktop должен быть доступен из WSL. Для сборки заранее нужны образ Go и, после появления зависимостей, доступ к ним. `.dockerignore` допускает только Go-модуль, cmd, internal и Dockerfile; при добавлении embed-ресурсов обновлять список.

Workflow `.github/workflows/ci.yml` предназначен для корня отдельного backend-репозитория. Срабатывает на push, pull_request и workflow_dispatch. Версия Go берётся из go.mod; версия golangci-lint — из Makefile. После форматирования, vet, race-тестов с покрытием >=60%, линтера и сборки выполняются docker build и запуск контейнера без сети. Публикации или деплоя нет, secrets не требуются. Проверка покрытия включает cmd/server и все будущие Go-пакеты (`-coverpkg=./...`); исключений нет.
