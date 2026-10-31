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

B01 проверяет конфигурацию/defaults/ошибки без утечки credentials, HTTP 404/503 и request_id/no-store, порядок lifecycle и продолжение cleanup после ошибок, deadline даже при зависшем участнике, освобождение startup-ресурсов. PostgreSQL integration tests проверяют пустую и более новую схему, read-only startup check, транзакционные миграции, повторное применение с сохранением users/sessions/player_states, SQL constraints и FK. Регрессия shutdown удерживает listener зарегистрированным в `net/http` и вызывает `Shutdown` сразу после `StopAccepting`, без ожидания `Serve`: штатное закрытие не даёт ложный `net.ErrClosed`, реальные ошибки закрытия сохраняются. Игровые сценарии реализуются и проверяются соответствующими задачами; B05 описан ниже.

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

U+0000 в display_name отклоняется с 400 VALIDATION_ERROR до world/KDF/SQL. Domain tests проверяют NUL в начале, середине и конце имени; usecase test проверяет приоритет валидации над недоступным миром. Сквозной HTTP/SQL-тест подтверждает отсутствие новых users/sessions/player_states и сохранение прежней cookie-сессии после отказа. Пароль с U+0000 регистрируется и проверяется без удаления символа; вход с тем же паролем без NUL отклоняется. Validator проверяет запрет NUL только для display_name.

## Проверки B03

Domain tests проверяют цилиндр/углы/конечные координаты, касание пола, bounds, ID, ссылки, направленные spawns и связность; budget tests — точную границу с резервом 3145728 и safe integers. Usecase tests через fake ports проверяют дедупликацию общего asset между залами и внутри пакета, неизменяемость файлов/резервов/spawns, ошибки источника и snapshot, version, active ownership, переход и отмену.

Файловые tests используют temp dirs: строгий JSON, UTF-8/surrogates, реальные JPEG и статический WebP, запрет анимации/повреждений, traversal, symlink escape и hardlink identity. Linux-регрессия подаёт FIFO без писателя вместо индекса/комнаты/изображения/профиля; короткий context и ограниченный subprocess проверяют своевременный отказ и отсутствие открытых world-дескрипторов. HTTP tests проверяют версии/ошибки, композицию с auth, непрозрачные URL IDs, точные исходные байты и Content-Length/no-store/request ID/nosniff. Сквозной тест генерирует отдельный синтетический мир и связывает Files → usecase → HTTP, сверяя total с размерами файлов и проверяя неизменность после перезаписи диска.

Startup tests проверяют отсутствующий/повреждённый мир, несовпадение версии и отмену. SQL startup integration проверяет world failure до открытия даже занятого listener; SIGTERM и Docker smoke используют тот же валидный тестовый формат. Никаких production fallback и фиктивного WS-владения.

## B04: соединения и транспорт

Usecase-тесты через fake Sink проверяют snapshot первым, left/joined при замене, защиту от старых callbacks, user/session ownership, резервации 32 мест, остановку admission, дедупликацию/10000/1012, sliding limits и серию move нарушений через reconnect. Парсер проверяет ошибки JSON/type/fields/null/Unicode, UUID, точные integer boundaries и дроби, округляемые float64.

Реальные loopback WS-тесты покрывают auth/Origin до upgrade, snapshot/presence/replacement4009, 33-й аккаунт503 и замену при полной вместимости, expiry/revoke4001, binary1003/oversize1009, heartbeat с pong и без него, shutdown1012. Детерминированно остановленный data writer использует настоящий WS для close1013; здоровые клиенты продолжают получать presence. Проверяется байтовый бюджет очереди. Ошибочные команды и повторы ограничены общим окном, которое переживает reconnect.

`TestWebSocketWorldAndLogoutIntegration` требует отдельную TEST_DATABASE_URL и связывает настоящие HTTP/WS, PostgreSQL, B02 и B03. Проверяет login → snapshot → room/assets, отсутствие ownership от login, смену owning session при replacement и logout4001. Синтетический каталог подготавливается до начала DB deadline; runtime не использует fixtures. Все конкурентные сценарии запускаются под `go test -race ./...` и `make coverage`.

Регрессии review B04: несколько молчащих WS-клиентов и shutdown deadline 100 мс с проверкой освобождения workers; матрица отсутствующих/неверных Connection, Upgrade, Sec-WebSocket-Version, Sec-WebSocket-Key и повторного Key с HTTP JSON/заголовками; ошибочный payload → исправленный payload с тем же UUID даёт DUPLICATE_REQUEST. Дополнительно проверены исключения guide.ask/player.move/unknown и отсутствие request_id у неверного move.

## B05: состояние, движение и сохранение

Domain-тесты проверяют sweep тонкой стены, округлые углы цилиндра, границы, касание пола, конечные координаты и LOS. Usecase-тесты с управляемым временем проверяют готовность, повтор ready без сброса dt, старый/будущий epoch, stale seq, ограничение скорости после простоя, ack/rotation при отклонённом перемещении и отсутствие изменений при неверных полях.

Блокируемый fake store проверяет, что SQL-переход не останавливает публикацию state, replacement ждёт commit и получает фактическую позу/пакет, старый cleanup не пишет в БД. Ошибка отмены сохраняет старый зал; ошибка записи перехода и периодического save закрывает только текущий WS с 1011. Финальный flush сохраняет принятый move.

Loopback WS-тесты проверяют реальные payload ready/entered/state, корреляцию request_id, ack и коррекцию позиции, готовность и порядок entered перед новым state. PostgreSQL-тесты проверяют монотонность ревизий, конкурентный CAS с одним победителем, rollback, проверки сессии и flush после отзыва. Сквозной TestGameWorldPersistenceAndTransitionIntegration связывает настоящий каталог, auth, HTTP/WS и SQL: движение, save, новый game service после рестарта, дверь, переключение HTTP-доступа и logout с сохранением. Все эти сценарии входят в race/coverage.

Регрессии ревью B05 блокируют disconnect flush и проверяют немедленный запрет Current/HTTP ownership и исключение из state/snapshot. Fake с успешным commit и потерянным ответом проверяет запрет старых move/HTTP-загрузок и восстановление фактического зала новым соединением. Управляемые каналы тиков воспроизводят следующий save-тик до завершения медленного соседа: сразу после завершения цикла здоровый игрок сохраняет новую принятую позицию, без ожидания ещё одного тика. Одновременно остаётся только один цикл, накопленные тики объединяются; публикация state не блокируется SQL.

## B06 — чат, осмотр и личная память

Usecase-тесты проверяют чат до ready, корреляцию только отправителю, Unicode trim/500 символов, отсутствие рассылки при ошибке публикации фото; осмотр проверяет ready, границу 3 м, стену/чужой зал, SQL-сбой и disconnect во время записи. Wire-тесты проверяют payload chat.message/exhibit.inspected и nullable photo.

PostgreSQL integration проверяет атомарный rollback обеих записей, изоляцию владельца, повтор с сохранением ID и обновлением времени/контекста между версиями, конкурентные повторы, лимит и сортировку личных событий, недействующие сессии. Сценарий двух аккаунтов по настоящему WS проверяет chat до ready, inspect после ready, сохранение до ответа и отсутствие success при реальном SQL-сбое. Используется только отдельная TEST_DATABASE_URL.

## B07: устойчивая генерация

Unit-тесты проверяют fingerprint/trim/Unicode, replay до ready и нового лимита, исходную UUID-корреляцию, cancel/terminal replay, output limit 8000/8001, queue deadline 120 с и model deadline 90 с через fake clock/model. Проверяются отмена при shutdown/disconnect, отсутствие partial в памяти и recovery после ошибки чтения уже claimed записи, включая уведомления всех pending-владельцев.

PostgreSQL-тесты проверяют concurrent admission восьми queued, один running плюс восемь queued, FIFO при одинаковом created_at, единственного победителя cancel/completion, rollback ответа при ошибке памяти, terminal immutability через Store, владельца и границы history, revoked sessions и restart recovery. Сквозной HTTP/WS-тест проверяет accepted → started → delta → completed, replay, приватность by-request, корреляцию failed/cancelled, потерю accepted при обрыве, восстановление без ready и SQL-сбой завершения с close1011 и SERVER_RESTARTED. Все SQL-сценарии используют отдельную TEST_DATABASE_URL и входят в race/coverage.

Регрессия ревью B07 проверяет disconnect после принятого движения при тайм-ауте, ошибке и успешной отмене вопроса. `testing/synctest` доводит отмену до deadline без реального ожидания; context-aware PositionStore подтверждает свежий ограниченный бюджет и сохранение последней позы. Повторный cleanup старого connection ID не отменяет вопрос и не сохраняет позицию нового соединения.
