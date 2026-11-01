[![Backend CI](https://github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/actions/workflows/ci.yml/badge.svg?branch=issue-1)](https://github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/actions/workflows/ci.yml)
![Go 1.27.1](https://img.shields.io/badge/Go-1.27.1-00ADD8?logo=go)
![PostgreSQL 18.6](https://img.shields.io/badge/PostgreSQL-18.6-4169E1?logo=postgresql)
![Ollama 0.40.13](https://img.shields.io/badge/Ollama-0.40.13-black)

# Hope & Merge — Gallery Backend

Многопользовательская 3D-галерея для локальной сети: посетители гуляют по залам,
обсуждают картины в чате, снимают фотографии и разговаривают с локальными
ИИ-гидами. Личная память гида сохраняет контекст посещений. После подготовки
комплекта интернет не требуется: один ноутбук обслуживает браузеры по LAN/Wi-Fi,
а также позволяет играть локально с выключенным Wi-Fi.

Демонстрационный мир C01: **24 пронумерованных зала, 72 музейные репродукции,
три художника-гида и 5 NPC в каждом зале по умолчанию**. Администратор может
настроить 0–10 NPC. Движение и реакции посетителей выполняет сервер.
Frontend развивается в [отдельном репозитории](https://github.com/ManInTheCoat/Hope-Merge-Frontend).

## Стек и версии

Версии берутся из `go.mod`, Compose, Makefile и подготовленного офлайн-комплекта.

| Компонент | Версия / назначение |
|---|---|
| Go / стандартный `net/http` | 1.27.1; та же версия в Docker builder |
| PostgreSQL | `postgres:18.6-bookworm`, постоянный named volume |
| pgx/v5 | v5.11.0, драйвер PostgreSQL |
| coder/websocket | v1.8.15 |
| goose/v3 | v3.29.0, SQL-миграции |
| golang.org/x/image | v0.47.0, обработка изображений |
| golangci-lint | v2.13.2 |
| Ollama | 0.40.13 для проверенного офлайн-комплекта; HTTP API без SDK |
| Основная модель | `gallery-guide-main:a01`, база `qwen3:4b-q4_K_M` |
| Резервная модель | `gallery-guide-reserve:a01`, база `qwen2.5:3b-instruct-q4_K_M` |
| Контекст моделей | 8192 токена, temperature 0.2; параметры в `configs/ollama/` |
| Node.js | 24 в CI, только генерация/валидация контрактов |
| Python | 3.9+ для контента; 3.11+ для офлайн-комплекта |
| Docker / Compose | Compose v2; точные версии CLI не закреплены |
| Bun | только подготовка отдельного frontend; версия CLI не закреплена |

Транзитивные Go-зависимости (`go.mod`; контрольные суммы — `go.sum`):

```text
github.com/jackc/pgpassfile       v1.0.0
github.com/jackc/pgservicefile    v0.0.0-20240606120523-5a60cdf6a761
github.com/jackc/puddle/v2        v2.2.3
github.com/mfridman/interpolate   v0.0.2
github.com/sethvargo/go-retry     v0.5.0
go.uber.org/multierr             v1.11.0
golang.org/x/sync                v0.24.0
golang.org/x/text                v0.43.0
```

Model tags не заменяют hashes: точные digests подготовленных моделей фиксирует
manifest офлайн-комплекта. Замеры GPU и выбор модели — [MODEL_BENCHMARK.md](docs/MODEL_BENCHMARK.md).

## Запуск

Нужны Go, Docker с Compose и локальный Ollama. До отключения интернета скачайте
зависимости, образ PostgreSQL, Ollama и модели. Подготовка Ollama и GPU подробно
описана в [DEVELOPMENT.md](docs/DEVELOPMENT.md). Ollama запускается отдельно
на `127.0.0.1:11434`; настройки из `.env` он автоматически не читает.

```sh
# Только для новой установки; существующий .env сохраняется.
[ -e .env ] || cp .env.example .env
# Отредактируйте .env: POSTGRES_PASSWORD, DATABASE_URL, origins и постоянный PHOTO_DIR.
# Для поставляемого мира: WORLD_DIR=./content/world, WORLD_VERSION=gallery_c01_v2.
# Подготовьте frontend (нужен Bun); output должен быть новым каталогом:
sh tools/build-frontend.sh ./data/frontend
# Для API-only разработки удалите FRONTEND_DIST_DIR из .env.
set -a
. ./.env
set +a
docker compose up -d --wait postgres
make build
make migrate-up
make world-check
./bin/gallery
```

Откройте `http://localhost:8080`; для LAN добавьте фактический origin сервера
в `ALLOWED_ORIGINS`. Go раздаёт frontend, HTTP API и WebSocket на одном origin.
Frontend закреплён на commit в `tools/build-frontend.sh`, сборка отключает mocks.
Сервер проверяет схему БД и мир до открытия listener; миграции при старте не применяет.
`make migrate-status` показывает их состояние; повторный `migrate-up` сохраняет данные.

**Обновление существующей галереи:** остановите backend и выполните
[инструкцию перехода на v2](docs/DEVELOPMENT.md#обновление-демогалереи-v2-задача-52).
Миграция 00009 переносит настройки залов/гидов и устанавливает 5 NPC без очистки
БД. Сохраните прежние volume, DATABASE_URL и PHOTO_DIR. После миграции обновите
WORLD_VERSION в существующем окружении. Постоянного названия зала в HUD
закреплённого frontend пока нет; сервер передаёт номер в Room.title.

## Структура проекта

```text
cmd/server/          composition root: конфигурация, HTTP/WS, lifecycle
cmd/migrate/         применение и статус миграций
cmd/admin/           административный CLI
internal/domain/     бизнес-типы и правила
internal/usecase/    сценарии приложения и порты
internal/adapters/   HTTP/WS, PostgreSQL, Ollama, файлы
internal/config/     конфигурация окружения
migrations/          встроенные последовательные SQL-миграции
content/gallery.json авторский источник C01
content/world/       готовые комнаты, изображения и профили
configs/ollama/      Modelfile основной и резервной моделей
tools/               контракты, подготовка контента, валидатор, офлайн-комплект
build/               Dockerfile
docs/               контракты, архитектура, разработка и показ
```

Clean Architecture обязательна: `domain <- usecase <- adapters`; зависимости
направлены внутрь, интерфейсы определяет потребитель. Правила —
[ARCHITECTURE.md](docs/ARCHITECTURE.md), схема БД — [DATA_MODEL.md](docs/DATA_MODEL.md).

## Проверки и документация

```sh
go build ./...
go vet ./...
go test ./...
go test -race ./...
make lint
make coverage     # race и порог суммарного покрытия ≥60%
make contracts    # генерация и валидация HTTP/WS схем и примеров
make world-check  # геометрия, ссылки, точные байты и бюджет кеша 10%
```

Интеграционные тесты используют отдельную `TEST_DATABASE_URL`; без неё явно
пропускаются. Рабочую БД для тестов не используйте. CI проверяет форматирование,
vet, race/coverage, lint, сборку, контракты, воспроизводимость контента и Docker smoke.

- [Нормативный API-контракт](docs/api/contract.md), [OpenAPI](docs/api/openapi.yaml), [WebSocket](docs/api/websocket.md).
- [Разработка и миграции](docs/DEVELOPMENT.md), [матрица тестов](docs/TESTING.md).
- [Контент C01 и воспроизводимая сборка](docs/content/README.md).
- [Подготовка и сценарий офлайн-показа](docs/DEMO.md).
- [Требования](docs/REQUIREMENTS.md), [карта документации](docs/INDEX.md).
