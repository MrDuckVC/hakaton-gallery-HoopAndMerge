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

B01 проверяет конфигурацию/defaults/ошибки без утечки credentials, HTTP 404/503 и request_id/no-store, порядок lifecycle и продолжение cleanup после ошибок, deadline даже при зависшем участнике, освобождение startup-ресурсов. PostgreSQL integration tests проверяют пустую и более новую схему, read-only startup check, транзакционные миграции, повторное применение с сохранением users/sessions/player_states, SQL constraints и FK. Регрессия shutdown удерживает listener зарегистрированным в `net/http` и вызывает `Shutdown` сразу после `StopAccepting`, без ожидания `Serve`: штатное закрытие не даёт ложный `net.ErrClosed`, реальные ошибки закрытия сохраняются. Игровые сценарии выше пока не реализованы.

Для SQL-тестов нужен отдельный `TEST_DATABASE_URL`, отличающийся от runtime `DATABASE_URL`, с правом CREATE SCHEMA; без него тесты явно SKIP. В CI PostgreSQL обязательна. Тесты владеют случайными schemas и удаляют только их, тесты команд применяют append-only миграции к тестовой БД. Подготовка и реальные команды — DEVELOPMENT.md. Проверка графа импортов запускает `go list`, поэтому Go должен быть в PATH.

`make coverage` сначала проверяет сам порог на профилях 60%, 100%, 59.99%, 0% и пустом профиле, затем выполняет `go test -race -covermode=atomic -coverpkg=./... -coverprofile=coverage.out ./...`. Проверка требует минимум 60% суммарного покрытия операторов всех Go-пакетов, включая cmd. Порог проверяется по исходным счётчикам операторов, без округления. Повторные блоки из профилей разных тестовых бинарников (`-coverpkg`) объединяются по расположению: оператор учитывается один раз и покрыт, если исполнялся хотя бы в одном тестовом бинарнике. Противоречивые счётчики одного блока отклоняются. Пустой профиль считается ошибкой. Отчёт можно открыть через `go tool cover -html=coverage.out`.

GitHub Actions также проверяет gofmt, go vet, golangci-lint, сборку Go и Docker-образа. Docker smoke-test применяет миграции на отдельной PostgreSQL, проверяет доступность HTTP и выход 0 после SIGTERM. Это проверка lifecycle, а не игровых возможностей.

## Обязательная проверка двух режимов

По REQUIREMENTS.md и задаче Q02 сначала проверить не менее двух браузерных клиентов по Wi-Fi без доступа в интернет: вход, синхронизацию, чат, загрузку залов, бюджет и персональную память гидов. Затем выключить Wi-Fi на серверном ноутбуке с RTX 4070 Laptop и повторить локальный вход через localhost, сцену, генерацию и восстановление памяти. Все сервисы остаются на сервере, клиентам не нужны установки кроме браузера. Проверять отсутствие CDN, удалённых шрифтов/аналитики и загрузок моделей. Записать фактическую VRAM/RAM, FPS, время загрузки зала и TTFT/скорость ответа при совместной нагрузке браузера и Ollama; значения не предполагать по названию GPU.

## Проверки B02

Domain/usecase tests проверяют trim Unicode White_Space, lowercase username, длины display_name в кодовых точках и пароля в байтах, отсутствие trim пароля, атомарные входные данные для регистрации, world unavailable, восстановление и повтор при конфликте ревизии, истёкшую сессию и повторную проверку после подписки. Криптографические тесты проверяют независимые соли, пароль с пробелами, dummy KDF и отмену контекста. Hub проверяется на session-scoped revoke, expiry, отмену и гонки.

HTTP tests проверяют точный Origin (включая отсутствующий/null/повторяющийся и upgrade), четыре маршрута, cookie-флаги, 204 без тела, no-store/request ID, строгие поля/JSON/UTF-8 и размер, общий sliding limit с fake clock и игнорирование X-Forwarded-For. Генератор контрактов не применяет ASCII ID pattern к паролям; validator содержит регрессии Unicode, пробелов и публичных полей до trim.

PostgreSQL tests используют тот же отдельный TEST_DATABASE_URL и собственные schemas: concurrent username conflict, account/state/session rollback, сохранение других сессий, идемпотентный отзыв и optimistic repair. Сквозной тест связывает настоящий HTTP adapter, usecase, KDF, hub и SQL, внедряя fixtureWorld только в тесте; проверяет cookie replacement, доставку уведомления, восстановление позы, expiry и 503 при world.Unavailable. Проверка реального WS close 4001 относится к B04, проверка геометрии настоящего каталога — к B03.

Регрессии ревью: Upgrade проверяется как список токенов во всех строках заголовка; lone UTF-16 surrogate в JSON отклоняется, валидная surrogate pair и literal U+FFFD сохраняются. `/me` повторно проверяет сессию после world validation; SQL-тест запрещает repair для отозванной/истёкшей сессии и проверяет восстановление отсутствующего state.
