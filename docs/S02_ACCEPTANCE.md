# S02 — административные профили гидов

Реализация [#38](https://github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/issues/38)
по [api/sunday.md](api/sunday.md), §3. Реализованы GET списка/профиля и PUT настроек,
version-scoped overrides с CAS, snapshot при Claim и согласованные публичные имена.
Миграция 00007 применяется оператором отдельно; рабочие БД/C01 не изменялись.

## Наблюдаемые правила

- Settings нормализуются Unicode trim; проверяются индивидуальные пределы и сумма
  инструкций ≤4000 кодовых точек. Чужие/повторные материалы отклоняются. Seed
  revision=1 выбирает все материалы; неоднозначное имя/subtitle запрещено на startup.
- Конкурентный CAS даёт одного победителя. Смена имени/subtitle повышает revisions
  всех залов личности, изменение только инструкций не создаёт room revisions.
- SQL-транзакция сохраняет profile+все room revisions; поздний room CAS failure
  откатывает ранее записанные строки. Существующий config/lamp/holder сохраняется.
- Снимок effective profile+revision фиксируется при queued→running под тем же
  scheduler lock, что profile update. Running сохраняет прежние инструкции,
  следующий Claim получает новые. Сборщик контекста использует снимок и прежние
  fixed instructions/16000 codepoints; память и завершённые пары сохранены/изолированы.
- HTTP проверяет сессию/роль и Origin, strict JSON/UTF-8/duplicate/null/unknown keys;
  rooms и guides делят одно PUT-окно. available_materials не содержит sources и
  полного текста, excerpt обрезается по Unicode до 200. Private snapshots не в DTO.
- Сквозной тест с настоящими HTTP/WS/SQL: два not-ready игрока получают новое имя
  и subtitle, SQL-ошибка не публикует частичный результат, room.sync/replacement
  и новый runtime service восстанавливают прежний успешный commit.
- Неопределённый COMMIT детерминированно моделируется портом: 503, инвалидирование
  всех затронутых комнат и закрытие owning connections 1011. Реальный сетевой обрыв
  PostgreSQL точно в момент COMMIT отдельно не инъецировался.

## Первоначальные проверки S02 (bfc114c)

Go 1.27.1 из /usr/local/go/bin, golangci-lint 2.13.2. Для sandbox кеши перенесены
в /tmp; сетевые тесты запущены с разрешёнными loopback-сокетами. Отдельный временный
PostgreSQL 18.6 на 127.0.0.1:55438, tmpfs без рабочих volumes. SQL-тесты создают и
удаляют только собственные schemas. Фактические результаты общего прогона ниже. Первый параллельный запуск race/coverage
столкнулся с коллизиями имён тестовых schemas; итоговые полные прогоны выполнены
последовательно, каждый на новой отдельной БД этого же временного контейнера.

| Проверка | Результат |
|---|---|
| go build ./...; make build | PASS |
| go vet ./... | PASS |
| make lint | PASS, 0 issues |
| node tools/build-contracts.mjs | PASS; обновлён статус S02, wire shape сохранён |
| node tools/validate-contracts.mjs | PASS |
| Целевые S02 SQL/HTTP/WS | PASS |
| go test ./... | PASS, включая SQL/HTTP/WS |
| go test -race ./... | PASS, включая SQL/HTTP/WS |
| make coverage (порог ≥60%) | PASS, 86,65% |

Независимое ревью реализации не выявило critical/important замечаний; отдельный
race-прогон domain/guide, usecase/runtime и usecase/guide прошёл.

Frontend UI, физический LAN/Wi-Fi-off показ и настоящий Ollama со сменой профиля
не проверялись в этой задаче. Детерминированные тесты используют fake model/контекст;
GPU/LLM-интеграцию и capability sunday_v1 завершает S07. Новых env, зависимостей,
assets, uploads знаний или дополнительного LLM-summary нет.

## Дополнение #38: максимум 10 посетителей

По поручению владельца максимум и seed NPC снижены до 10 на зал; npc_count=0–10,
NPC IDs npc_00..npc_09. Обновлены domain, HTTP, OpenAPI/WS-схемы и fixtures.
Новая forward-only миграция 00008 отдельно применяется оператором: значения >10
во всех сохранённых версиях мира уменьшаются до 10 с revision+1. Counts 0–10
не меняются. Другие config/lamp/holder сохраняются; переполнение safe revision
откатывает всю миграцию. Повторный up не повышает revisions повторно.

Новые domain/admin/schema проверки сначала воспроизвели прежний предел 40 и
принятие 11; после изменения прошли. SQL-тест начинает со схемы 00007, проверяет
0/10/11/40, разные WORLD_VERSION, holder, повтор миграции и startup; отдельно
проверяется отсутствие частичного изменения при переполнении revision.
Рабочая БД не менялась. Независимое ревью дополнения не выявило существенных замечаний.

| Проверка дополнения | Результат |
|---|---|
| go build ./...; make build; go vet ./... | PASS |
| go test ./... | PASS, отдельная чистая тестовая БД |
| go test -race ./... | PASS, отдельная чистая тестовая БД |
| make lint | PASS, 0 issues |
| node tools/build-contracts.mjs; node tools/validate-contracts.mjs | PASS |
| make coverage (порог ≥60%) | PASS, 86,68% |
