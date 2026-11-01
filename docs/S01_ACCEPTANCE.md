# S01 — сохраняемый runtime и административное редактирование

Реализация задачи [#37](https://github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/issues/37).
Контракт — [api/contract.md](api/contract.md) и [api/sunday.md](api/sunday.md).

Реализованы version-scoped SQL overlay, CAS, admin membership/rooms HTTP API,
effective guide placements, world.runtime, room.sync, admin grant и упаковка binary.
C01 и миграции 00001–00005 неизменны. Миграция 00006 применяется оператором отдельно.

## Проверенные наблюдаемые правила

- Два конкурентных HTTP PUT с expected_revision=1: один 200/revision=2, второй 409.
- Два owning not-ready WS получают snapshot перед runtime и одинаковый committed update.
- SQL failure даёт 503; следующие события обоим игрокам — коррелированные room.sync,
  без неподтверждённой рассылки. Новый runtime service восстанавливает overlay из SQL.
- Новый WORLD_VERSION не читает overlay прежней версии.
- guide.ask сначала OUT_OF_RANGE; после изменения placement через HTTP принимается
  через настоящий WS, публичный runtime содержит ту же effective позицию.
- Неизвестный COMMIT после фактической записи: usecase 503, оба WS 1011, invalidated cache;
  недоступное durable чтение запрещает reconnect, восстановленное читает revision2.
  Это детерминированный тест порта с потерянным ответом; сетевой разрыв реальной БД
  в момент COMMIT отдельно не инъецировался.
- Invalidation во время успешного door commit и reconnect до detach перечитывают
  сохранённую позу; поздний cleanup не меняет новое соединение.
- Проверены role/session на каждом HTTP запросе, Origin, media type, UTF-8,
  case-sensitive/duplicate/missing/null JSON, 65536/65537 байт, safe integers,
  общий admin PUT rate и room.sync rate/UUID при reconnect.
- Геометрия проверяет bounds, surfaces, installation footprint, slot/guide/door/spawn
  separation. Seed валиден для всех 24 C01 залов. Публичный wire не содержит connection ID.

## Команды и результаты

В среде Go добавлен в PATH из `/usr/local/go/bin`; cache перенесён в `/tmp`,
поскольку стандартный cache недоступен для записи в sandbox. Сетевые тесты
запускались с разрешением локальных TCP-сокетов. Отдельная временная PostgreSQL
18.6 работала на 127.0.0.1:55437 без volumes; каждый SQL-тест использует свою схему.
Рабочие БД/файлы не менялись.

| Проверка | Результат |
|---|---|
| go build ./...; make build | PASS, gallery/migrate/admin |
| go vet ./... | PASS |
| go test ./... с TEST_DATABASE_URL | PASS, включая SQL/HTTP/WS |
| go test -race ./... с TEST_DATABASE_URL | PASS |
| make lint | PASS, 0 issues |
| make coverage с TEST_DATABASE_URL | PASS, 86.11%, порог 60% |
| node tools/build-contracts.mjs | PASS, обновлены описания статуса S01 в OpenAPI, структура wire неизменна |
| node tools/validate-contracts.mjs | PASS |
| python3 -m unittest discover -s tools -p test_offline_bundle.py | PASS, 8 тестов |
| docker build --file build/main.Dockerfile --tag gallery-backend:issue37-test . | PASS |
| docker run --rm --network none --entrypoint /admin gallery-backend:issue37-test | usage, ожидаемый exit 1 без аргументов |
| tools/smoke-docker.sh gallery-backend:issue37-test | PASS, миграции 1–6, API 404, frontend 200, SIGTERM exit 0 |
| git diff --check | PASS |

Docker Desktop не предоставляет ожидаемый Linux host networking: первая попытка
smoke в host-режиме не достигла HTTP listener. Повтор с документированным
SMOKE_DOCKER_NETWORK в отдельной bridge-сети прошёл; код для обхода не менялся.

## Границы S01

Симуляция NPC относится к S05: `game.RoomSnapshots` подготовлен и проверен на порядок
runtime → NPC, но реальный NPC snapshot пока не выдаётся. Профили гидов, object.act,
guide.action и telemetry остаются S02–S06. Capability sunday_v1 не включена до S07.
Frontend/LAN/localhost показ с отключением Wi-Fi этим backend изменением не выполнялся.
Полный offline kit с моделями/изображениями заново не создавался; проверена упаковка
admin в генератор и его unit tests. Развёртывание/выдача роли рабочим аккаунтам не выполнялись.
