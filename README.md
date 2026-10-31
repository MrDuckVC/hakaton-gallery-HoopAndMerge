# Gallery Backend

Разработка ведётся только по Clean Architecture: domain <- usecase <- adapters. Зависимости направлены внутрь, реализации связывает cmd/server. Требование обязательно и для MVP; обход слоёв ради сроков запрещён. Подробности — docs/ARCHITECTURE.md (для файлов в docs/ — ARCHITECTURE.md).

Backend многопользовательской 3D-галереи: посещение тематических залов, общий чат, фотографии и локальные ИИ-гиды с персональной памятью. Во время игры интернет не требуется; клиенты подключаются по LAN/Wi-Fi.

Репозиторий: https://github.com/MrDuckVC/hakaton-gallery-HoopAndMerge. Frontend разрабатывается отдельно.

## Стек
Go net/http, PostgreSQL, pgx/v5, coder/websocket, локальный Ollama. Версии фиксируются в go.mod/go.sum и docker-compose.yml. Один процесс Go обслуживает HTTP и WS, одна локальная модель — всех гидов.

## Запуск

Инициализирован Go-модуль `github.com/MrDuckVC/hakaton-gallery-HoopAndMerge`, минимальная версия Go — 1.27.1. Внешних Go-зависимостей пока нет.

```sh
make build
make run
```

`cmd/server` пока сообщает об инициализации и завершается. HTTP/WS API, подключение PostgreSQL и Ollama ещё не реализованы; сетевой порт не открывается. `make build` создаёт `bin/gallery`.

Если Go установлен в `/usr/local/go`, добавьте его в PATH:

```sh
export PATH="/usr/local/go/bin:$PATH"
```

Docker-образ собирается командой `make docker-build` (см. docs/DEVELOPMENT.md). Compose и `.env.example` пока остаются заготовками. Параметры будущего запуска описаны в docs/DEVELOPMENT.md. Перед офлайн-демонстрацией потребуется подготовить БД, зависимости, модель и контент.

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

Также доступны `make test`, `make vet`, `make fmt`. Для конкурентного кода — `go test -race ./...`. Тестируется сообщение точки входа о недоступности API. `make coverage` запускает тесты с race detector и требует суммарное покрытие всех Go-пакетов не ниже 60%; отчёт — `coverage.out`. Тестов бизнес-логики пока нет. `make lint` при первом запуске скачивает golangci-lint версии из Makefile; для офлайн-работы установите его заранее.

`make contracts` генерирует и проверяет схемы API; Node нужен только этим инструментам.

Проект содержит Go-каркас и документацию, но ещё не работающий игровой сервер и не готовый пакет мира. Примеры используют демонстрационные ID и размер картинки; реальный asset не включён. Карта документации — docs/INDEX.md.

GitHub Actions (`.github/workflows/ci.yml`) запускается на push, pull_request и вручную: форматирование, vet, race-тесты с порогом 60%, golangci-lint и Go-сборка. После успешных проверок собирается Docker-образ и проверяется запуск контейнера без сети. Образ не публикуется в registry.
