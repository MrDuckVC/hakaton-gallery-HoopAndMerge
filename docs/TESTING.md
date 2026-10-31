# Проверки backend

Проверка Clean Architecture обязательна: domain не импортирует usecase/adapters, usecase не импортирует adapters и инфраструктурные библиотеки. Проверять go list, архитектурным тестом или depguard. Unit domain — без внешних ресурсов, usecase — через fake ports, адаптеры — integration tests.

Unit: password boundary bytes/Unicode trim; Origin exact match; finite coordinates/yaw/pitch; safe ints; package budget/reservations; cylinder sweep и floor touching; LOS; cursor ordering/owner; rate sliding windows с fake clock; generation FSM/idempotency. SQL integration — реальная отдельная PostgreSQL, LLM transport — fake NDJSON server (split UTF-8 chunks, errors, timeout,cancel). GPU benchmark отдельно, не требовать в обычном go test.

Обязательные сценарии:
1. Register/login заменяют только cookie-session; logout без cookie204 при разрешённом Origin, другие origin403. /me401 без сессии. Password128 bytes принимается,129 нет. Соль и KDF, никаких plaintext.
2. WS upgrade без auth401/Origin403, snapshot первое, max32, replacement4009/left+joined; cleanup старого socket не меняет новое. Expiry/logout4001. Binary1003/oversize1009. Slow writer queue1MiB ->1013, hub работает дальше.
3. До ready move/inspect/ask/photo GAME_NOT_READY; chat/cancel/history доступны. ready повторный не сбрасывает dt. Epoch old ignore для move, future error; seq stale ignore. Недопустимая позиция не меняется, ack обновляется, допустимый rotation принимается. Y всегда0.
4. Дверь current+radius+LOS, atomic save before entered, entered до state, epoch+1 ack0 not-ready. DB failure не меняет room. HTTP asset доступ только active owning session/current room. Запрос начат до transition может завершиться; клиент отбрасывает старый пакет.
5. inspect XZ/LOS и repeat inspected_at. Изоляция user memory. История guide только владельца, guide shared across rooms. Один running+8queued, очередь120s, running90s, output8000, no partial in memory.
6. Ask same UUID+payload один generation, different conflict, existing lookup до ready/distance/queue. Потеря accepted/completed -> by-request/history, без нового turn. Cancel/completion race один terminal; cancel terminal безопасен. Cancel correlation failed ask UUID затем cancelled cancel UUID. Restart pending -> failed SERVER_RESTARTED.
7. Фото 2MiB/multipart2MiB+64KiB, dimensions/JPEG/WebP/malformed/animated, exactly one image, duplicate fields reject. Thumbnail<=320px/128KiB. Commit recheck room+epoch+owning socket. Same idempotency key+hash returns200 even after transition; conflict409, concurrent repeats one photo. No visible metadata before both files saved; failure cleanup.
8. Photo private other404, published authenticated200; caption/no exhibit defaults; irreversible publication saved before chat broadcast. Album max6, keyset no shifts after insert, wrong cursor400 INVALID_CURSOR.
9. Sliding limits survive reconnect, retry_after_ms и HTTP Retry-After; moves лишние без ack и5sec->1008. UUID request retention10000 then1012.
10. Офлайн2 клиента: весь раздел10 contract.md. Позиция каждые5s, ошибку save не скрывать, close1011. Неправильная world version409; package+photo reserve нарушение блокирует startup.

Команды после появления модуля: go test ./..., go test -race ./..., go vet ./..., go build ./.... Тесты должны проверять наблюдаемое поведение, не повторять implementation. Не запускать деструктивные integration cleanup на рабочей БД.

Документы: validate-contracts проверяет синтаксис/refs/examples и критичные отсутствующие legacy routes. Это локальная структурная проверка, не полная сертификация OpenAPI и не проверка Go runtime. При доступном выбранном validator дополнительно прогнать OpenAPI3.1/JSON Schema2020-12.

## Текущие тесты и порог CI

`cmd/server/main_test.go` проверяет, что запуск каркаса сообщает на INFO об отсутствии реализации API. Сейчас это единственное поведение Go-программы; его покрытие не означает готовности игровых сценариев выше.

`make coverage` сначала проверяет сам порог на профилях 60%, 100%, 59.99%, 0% и пустом профиле, затем выполняет `go test -race -covermode=atomic -coverpkg=./... -coverprofile=coverage.out ./...`. Проверка требует минимум 60% суммарного покрытия операторов всех Go-пакетов, включая cmd. Порог проверяется по исходным счётчикам операторов, без округления. Пустой профиль считается ошибкой. Отчёт можно открыть через `go tool cover -html=coverage.out`.

GitHub Actions также проверяет gofmt, go vet, golangci-lint, сборку Go и Docker-образа. Контейнер запускается без сети; пока ожидается успешное завершение каркаса, а не работа HTTP-сервера.

## Обязательная проверка двух режимов

По REQUIREMENTS.md и задаче Q02 сначала проверить не менее двух браузерных клиентов по Wi-Fi без доступа в интернет: вход, синхронизацию, чат, загрузку залов, бюджет и персональную память гидов. Затем выключить Wi-Fi на серверном ноутбуке с RTX 4070 Laptop и повторить локальный вход через localhost, сцену, генерацию и восстановление памяти. Все сервисы остаются на сервере, клиентам не нужны установки кроме браузера. Проверять отсутствие CDN, удалённых шрифтов/аналитики и загрузок моделей. Записать фактическую VRAM/RAM, FPS, время загрузки зала и TTFT/скорость ответа при совместной нагрузке браузера и Ollama; значения не предполагать по названию GPU.
