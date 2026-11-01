# S04 — управление составным светильником

Реализация [#40](https://github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/issues/40)
по [api/sunday.md](api/sunday.md), разделы 4 и 8.

## Наблюдаемые правила

- Строгий union take/place/aim/power, ordinary UUID, дополнительный rate 10/10 с/user
  с сохранением при reconnect. Неизвестные/лишние/null поля отклоняются.
- Owning/ready/epoch и повторный допуск перед CAS; XZ ≤3 м, LOS, installed-only aim/power,
  owner-only place. Границы углов, stale revision и no-op с повышением revision.
- Два SQL-адаптера конкурируют за первый take: один durable победитель. Старый connection
  не может снять holder даже при совпадающей revision. Release не создаёт отсутствующую строку.
- Два настоящих WS-клиента видят одно состояние: object.acted отправителю с UUID,
  world.runtime остальным. Повтор UUID и held aim не меняют состояние; no-op power
  повышает revision. DTO не содержит holder connection ID.
- Реальный SQL CHECK временно отклоняет release после disconnect. После снятия fault
  worker освобождает лампу, reconnect получает canonical revision. Рабочая БД не участвует.
- Управляемое время проверяет backoff 1/2/5/5 с, сохранение задания при invalidation,
  lazy release и защиту нового live owner. Dirty work объединяется по комнате.
- Детерминированно остановленный CAS проверяет disconnect/replacement и shutdown
  во время commit. Старый socket не получает success; новый holder не сбрасывается.
- Door не сохраняет переход при ошибке release; lifecycle освобождает объекты до close.
  Startup удаляет stale holder, release reread повторяет CAS только для прежней пары.
- Неизвестный COMMIT проверен через порт: committed, rollback, более поздняя revision,
  недоступное чтение и подтверждение cleanup перечитыванием. Команда не подтверждается,
  затронутые WS закрываются 1011. Перечитывание сохраняет caller cancellation/deadline.

Отрицательные проверки воспроизведены до реализации; review-regression на приоритет
геометрии перед holder прошёл RED→GREEN. Независимое ревью не обнаружило других
ошибок ownership/CAS/deadlock; отмеченные пробелы тестов закрыты.

## Среда и проверки

Go 1.27.1 из `/usr/local/go/bin`, Node v24.19.0, golangci-lint v2.13.2.
Go/lint caches перенесены в `/tmp`. SQL/HTTP/WS тесты используют отдельную временную
PostgreSQL 18.6 на 127.0.0.1:55440, без рабочих volumes, с отдельной схемой на тест.
Сокеты требуют разрешения loopback вне sandbox. Первый широкий sandbox-прогон
остановился на `socket: operation not permitted`; повтор выполнен с разрешением.

| Команда | Результат |
|---|---|
| go build ./...; make build | PASS, gallery/migrate/admin |
| go vet ./... | PASS |
| go test ./... | PASS, включая отдельную SQL/HTTP/WS интеграцию |
| go test -race ./... | PASS |
| make lint | PASS, 0 issues |
| make coverage | PASS, 86.33%, порог 60% |
| node tools/build-contracts.mjs | PASS, согласованная wire-форма сохранена |
| node tools/validate-contracts.mjs | PASS |
| gofmt -l затронутых Go-пакетов | пусто |
| git diff --check | PASS |

## Границы

Новые миграции, env, зависимости и ассеты не нужны. Используется схема 00006;
C01, коллизии и player.move не изменены. `sunday_v1` остаётся выключенной до S07.
При недоступной БД на исчерпанном shutdown deadline stale holder восстанавливается
следующим успешным startup. Старые WORLD_VERSION overlays не удаляются.

Инъекция потери COMMIT-ответа детерминированная через store port; реальный сетевой
разрыв PostgreSQL в момент COMMIT отдельно не проводился. Frontend, WebGL/LAN и
показ с выключенным Wi-Fi не проверялись. Docker image/offline kit заново не собирались;
S04 не меняет их зависимости или упаковку. Старые ручные проверки S01 не выдаются за S04.
