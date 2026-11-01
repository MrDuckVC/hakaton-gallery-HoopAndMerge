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

Аппаратные прогоны A01 запускаются отдельно по [tools/modelbench](../tools/modelbench/README.md).
`TestModelBenchmarkQueue` требует явного A01_OLLAMA_MODEL и отдельной TEST_DATABASE_URL;
без флага он пропускается. Обычные build/vet/test/race/coverage/lint не требуют
Ollama, GPU, браузера или интернета. Результаты и неподтверждённые критерии —
[MODEL_BENCHMARK.md](MODEL_BENCHMARK.md).

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

Domain-тесты проверяют sweep тонкой стены, округлые углы цилиндра, границы, касание пола, конечные координаты и LOS. Usecase-тесты с управляемым временем проверяют готовность, повтор ready без сброса времени/запаса расстояния, старый/будущий epoch, stale seq, ограничение скорости после простоя, ack/rotation при отклонённом перемещении и отсутствие изменений при неверных полях.

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


## B08 — Ollama, контекст и память

Fake HTTP/NDJSON tests проверяют побайтовый split UTF-8, конечный chunk, HTTP/JSON/stream errors, обрыв без done, предел 256 КиБ на запись, timeout/cancel и отказ callback. Usecase tests проверяют серверный контекст, только completed-пары, осмотр/фото, ограничения 16 000/4000 и отсутствие LLM-summary. Каталог проверяет строгие профили и ссылки материалов.

С отдельной TEST_DATABASE_URL выполняются атомарность/rollback памяти B07, последние четыре completed-пары, изоляция user+guide, новый экземпляр адаптера после restart и отсутствие изменений памяти после failed. Сквозной тест связывает настоящий WS → B07 → ContextReader → HTTP/NDJSON fake Ollama → PostgreSQL; проверяет включение осмотра и прошлого ответа, cancel/reconnect и сбой completion с close1011. `make coverage` включает эти тесты и race detector.

Opt-in тест двух fixture-личностей с настоящим Ollama, ручные LAN/localhost-сценарии и статус непроведённого реального показа — [GUIDES.md](GUIDES.md). Реальный tag и токенное окно определяет A01; unit/integration тесты GPU не требуют.


## B09 — идемпотентная загрузка фото

Domain проверяет нормализацию/UUID/fingerprint, caption в Unicode, epoch и ограничения файла. Кодеки тестируются настоящими JPEG/static WebP: предельные размеры, повреждения, анимация, декодируемая JPEG-миниатюра ≤320 px и ≤128 КиБ. HTTP проверяет обязательные/неизвестные/повторные части, единственный image и ключ, MIME, лимиты тела, ошибки и заголовки.

Usecase проверяет replay до текущего игрового контекста, conflict, конкурентное сохранение, ошибки Images/Files/SQL, поздний отказ проверки, успешный/откатившийся/неразрешённый COMMIT и сохранение файлов при неизвестном исходе. Fake clock проверяет 6/60 секунд и точное освобождение окна. Files проверяет атомарную пару, запрет перезаписи, path traversal, отмену и сохранность чужого staging. Реальные FS-отказы (запрет записи в PHOTO_DIR и удалённый каталог) проходят через HTTP-маппинг: ожидается 503 SERVICE_UNAVAILABLE без раскрытия пути. Операционные ошибки сохраняют исходную OS-ошибку для errors.Is/errors.As; VALIDATION_ERROR и отмена контекста остаются отдельными причинами.

PostgreSQL проверяет единственность photos+PHOTO_CREATED, изоляцию владельца, rollback при сбое памяти и финальной проверке, revoked/expired session. Сквозные сценарии с настоящим каталогом, HTTP/WS, БД и файлами проверяют concurrent POST → 201/200, replay после перехода, conflict, rate limit и смену epoch во время декодирования с очисткой собственных файлов. Все сценарии включены в race/coverage; интеграционные требуют отдельную тестовую БД.

## B10: альбом, чтение и публикация

Usecase/HTTP-тесты проверяют доступ без WS, private/owner/published, отсутствие сессии, точные байты и MIME, пустой альбом, nullable next_cursor, limit 1–6, неверные и повторные параметры. Файловый адаптер проверяется на отмену контекста, выход за корень, отсутствующий файл, каталог вместо файла и превышение байтового лимита.

На отдельной PostgreSQL проверяются сортировка с одинаковым created_at, owner-scoped cursor, новые вставки между страницами, неизвестная/чужая граница, rollback публикации, concurrent/repeat publish, отозванная/истёкшая сессия, сохранность после пересоздания адаптера и единственность PHOTO_CREATED. Сквозной тест с реальными HTTP/WS/БД/файлами проверяет JPEG/WebP, точное совпадение bytes DTO с файлами, доступ после закрытия WS, запрет чужой публикации, отсутствие broadcast при SQL-сбое и немедленное чтение другим игроком после chat.message. Публикация тестируется на новом соединении до ready; корреляция возвращается только отправителю. Все проверки входят в штатные test/race/coverage.

## C01 — поставляемый мир

`go test ./tools/worldcheck` загружает реальные JPEG и строгий каталог C01 штатным валидатором. Проверяются отчёт размеров/hash, бюджет с резервом фото, метаданные/права, отсутствие дублирующих изображений, материалы каждого экспоната и три профиля, безопасные spawns, свободные пути и LOS к каждой двери/картине/гиду. Тесты не используют сеть или SQL; полное декодирование 72 изображений занимает заметное время. Опциональный `TestLocalModelGallery` проверяет настоящую модель на подготовленных профилях и не запускается без C01_OLLAMA_MODEL. Команды и ручные критерии — [content/README.md](content/README.md), фактическая приёмка — [content/ACCEPTANCE.md](content/ACCEPTANCE.md).

## I01 — frontend и офлайн-комплект

HTTP-тесты проверяют корень, GET/HEAD, MIME, SPA navigation, отсутствие assets,
защиту hidden/traversal/symlink, API 404 и отдельную маршрутизацию WS. Config
проверяет отсутствие FRONTEND_DIST_DIR и ошибки явно заданного пути; startup
проверяет dist до БД/listener. Интеграция использует отдельную тестовую PostgreSQL.
`python3 -B -m unittest discover -s tools -p test_offline_bundle.py` проверяет
отказ перезаписи существующего комплекта, порчу/отсутствие/добавление файлов,
недостающие/повреждённые веса и symlinks за пределы комплекта. CI выполняет эти
проверки вместе с существующими build/vet/race/coverage/lint/contracts/Docker smoke.
Ручные сценарии и фактические ограничения — [DEMO.md](DEMO.md),
[I01_ACCEPTANCE.md](I01_ACCEPTANCE.md).

## Sunday: матрица приёмки

S01–S07 интегрированы. Итоговый прогон и границы — [S07_ACCEPTANCE.md](S07_ACCEPTANCE.md);
исторические проверки светильника — [S04_ACCEPTANCE.md](S04_ACCEPTANCE.md). Матрица:
- admin: 401/403, точный Origin для PUT, 64 КиБ, неизвестные поля, role только из БД;
- два конкурентных room/profile PUT: один commit, второй REVISION_CONFLICT, без partial broadcast;
- effective guide placement/profile: distance/LOS и следующий running используют override,
  уже running остаётся со старым snapshot, чужая память не читается;
- лампа: два take, другой holder, старый epoch/connection, place за стеной, CAS, disconnect/
  replacement/door/SQL-сбой/неопределённый commit/restart, lazy cleanup без сброса нового owner;
- telemetry: fake clock 2/5/6 с, visible=false, неверный nonce, stale, старый callback,
  без участия в обычном лимите/UUID, SQL и других метриках;
- NPC: fake clock/seed, 10/0, остановка/чат/3 с, стены, отсутствие LLM/SQL tick,
  полные snapshots, новый instance при новом lifecycle комнаты;
- guide.action: только перечисленные фразы, без LLM/SQL из текста, CAS/LOS/ready,
  повтор show без новых объектов, обычный ask работает независимо;
- контрактные fixtures/TS, сохранность базовых HTTP/WS/10% и сборки офлайн-комплекта.
Реальные LAN/Wi-Fi-off/WebGL проверки дополняют №20/21, не заменяются unit tests.

## S03 — FPS и ping

Fake clock проверяет начальные null, samples раз в 2 с, границу stale 6 с,
первый/последующие probe и timeout 5 с, неизвестный/повторный/просроченный nonce.
Проверяются отдельные окна 2/с, независимость от 30/10 с и 10000 UUID,
невалидные FPS/фон, отсутствие mutable ссылок, replacement, прежний connection ID,
немедленное исключение disconnect до освобождения persistence gate и конкурентные
обновления/чтения. HTTP проверяет роль каждый запрос, доступ без игрового WS,
пустой массив и только восемь разрешённых полей. WS проверяет обработку до ready,
тихий discard, VALIDATION_ERROR и сохранение session revocation. Существующие
транспортные тесты проверяют Origin, binary/size/queue limits и protocol heartbeat.
Все сценарии входят в test/race/coverage; новых SQL-сценариев у телеметрии нет.
Отдельная регрессия задерживает session authentication на 5 с после получения pong:
RTT остаётся 12 мс; смена challenge за время SQL и позднее завершение старого ответа
не теряют следующий nonce и не переписывают новый RTT. До успешной аутентификации
публичные измерения не меняются.
Реальное измерение rAF и polling 2 с проверяются в отдельном frontend/S07.

## S02 — административные профили

Domain/usecase проверяют Unicode trim/границы, суммарные 4000 кодовых точек,
собственные уникальные материалы, seed со всеми материалами, неоднозначные имена,
конкурентный CAS, смену имени во всех залах и отказ без broadcast при SQL/unknown COMMIT.
HTTP проверяет сессию/роль, DTO/excerpt и отсутствие sources, строгий JSON и общий
лимит PUT для rooms/guides. Сборщик контекста использует private Claim snapshot.
SQL-интеграции проверяют CAS, version isolation, rollback уже записанной комнаты
при конфликте следующей, снимок до/после running и сохранение/изоляцию памяти.
Сквозной HTTP/WS-тест проверяет not-ready broadcast, rollback без лишних событий,
room.sync, replacement и восстановление профиля новым runtime service.
Все SQL-тесты используют только отдельный TEST_DATABASE_URL и свои schemas.

Лимит посетителей: seed=10; domain/admin PUT допускают npc_count=0/10 и
отклоняют 11/40. Контрактный валидатор проверяет тот же диапазон и NPC snapshot
из 10 записей; 11-я запись запрещена. SQL-тест миграции 00008 стартует со схемы
00007, проверяет counts 0/10/11/40, разные WORLD_VERSION, сохранение config/lamp/holder,
повышение revisions только изменённых комнат, повторный up и startup после миграции.

## S05: NPC-посетители

Domain fake-clock tests проверяют детерминированные маршруты, 10/0, движение 0.12 м
за шаг 200 мс, idle, talking/3 с/продление, радиус 3 м и tie-break по user_id,
стены и области дверей, полный шаг при развороте и строгую границу idle <1 м.
Отдельный тест подготавливает маршруты всех 24 залов C01.
Game tests покрывают initial/sync ordering до ready, 5 Гц, общий broadcast seq,
count→0, пустые комнаты, reconnect/replacement/late detach, переход между залами,
неуспешный/пустой/photo-only chat и отсутствие реакции до окончания публикации.
Блокированный photo I/O не блокирует NPC tick; invalidation отменяет поздний chat.
Движение игроков и detached snapshots проверяются независимо от NPC mutations.
Исчерпание seq закрывает владельцев с 1012 на tick/sync/runtime/attach, не выдавая
небезопасных JSON-чисел.

Настоящий WS проверяет initial/runtime/NPC, sync request_id и tick без request_id.
Wire tests проверяют 10/0, snake_case, полный список и close1011 при payload>24 КиБ.
RuntimeHTTPWebSocketDurabilityIntegration на отдельной TEST_DATABASE_URL проверяет
initial 10, admin CAS→0, одинаковую доставку [] owning WS и sync после SQL rollback.
Штатные race/lint/coverage и contract tools остаются обязательными; физический
LAN/offline показ и frontend-анимации этой автоматизацией не подтверждаются.


## S06: локальные действия гидов

Domain tests покрывают девять точных фраз, Unicode lower/trim/пробелы/пунктуацию,
неподдерживаемые просьбы, длину/NUL, effective placement, границу 3 м, LOS,
CAS/overflow, detached input и сохранность holder лампы. Runtime tests проверяют
commit до callback, отказ guard/SQL/CAS/size, неопределённый commit, отмену,
повтор show и конкуренцию action/admin/lamp за одну revision.
Game/wire tests проверяют owning/ready/epoch, поздний callback, корреляцию и
broadcast, строгий payload, UUID-дедупликацию и окно 3/10 с после reconnect.
TestGuideActionWebSocketDurabilityAndConversationIntegration на отдельной
TEST_DATABASE_URL связывает настоящий WS/SQL с двумя игроками: все эффекты,
повтор show/UUID, failed SQL без broadcast, reconnect и новый runtime service.
Отсутствие generations/memory events/summary и вызовов модели проверяется отдельно;
та же фраза в guide.ask проходит прежнюю очередь/модель и сохраняет память.
Физический LAN/offline показ и frontend-отрисовка остаются ручной проверкой S07.

## S07: composition root и комплект

`TestSundayCompositionIntegration` запускает настоящий cmd/server с валидным синтетическим миром/гидом, отдельной
тестовой БД и двумя настоящими WS: capability/base protocol, snapshot → runtime → NPC,
admin 401/403/grant, room CAS, ready, chat/metrics до ready, лампа/конфликт,
guide.action, профиль, probe/pong/admin clients, рестарт с held lamp и room.sync.
`SUNDAY_TEST_FRONTEND_DIST=/absolute/dist` включает настоящий production dist;
проверка его раздачи не подтверждает WebGL/rAF/визуальные анимации.
`SUNDAY_TEST_WORLD_DIR=/absolute/content/world` включает отдельный прогон с C01
и gallery_c01_v1; тяжёлая загрузка C01 под race/coverage может превысить production
startup deadline, поэтому в instrumented CI используется testworld, deadline не увеличивается.
Kit-тест успешной подготовки проверяет наличие, executable mode и hash admin,
отказ verify после его подмены/удаления; существующие тесты защищают от перезаписи.
Физические режимы и бюджет в настоящем браузере проверяются по DEMO, отдельно.

## S07: рывки при ходьбе и регрессия heartbeat CI

MovementDeliveryJitter воспроизводит 200 шагов по 0,36 м с интервалами доставки
140/60/100/180/20/100 мс: каждый принятый state совпадает с запрошенным.
Тот же короткий сценарий проверяется через настоящий WS. Отдельные отрицательные
тесты запрещают постоянные 5 м/с, накопление больше 1 м после простоя, обход
стены, повторный ready и добычу запаса частыми replacement. Стартовый запас 0,
пополнение только реальным серверным временем; расход только принятой дистанции.

Heartbeat regression моделирует CloseNow, который ждёт уже начатый graceful close:
heartbeat обязан вызвать underlying network abort до ожидания CloseNow. Исходный
TestTransportHeartbeat и regression повторены 50 раз под race/coverage, без
увеличения production интервалов ping20с/pong10с. CI failure #41305284103 был
воспроизведён отдельным RED-тестом, затем исправлен; это runtime fix, не увеличение
допустимого времени ожидания теста.

## S07: стоимость fixture под instrumentation

После первого push оба CI-прогона упёрлись в общий 10-минутный timeout cmd/server
во время JPEG-декодирования. Синтетический testworld использует настоящие
896×896 grayscale JPEG quality100 вместо1024×1024 RGB quality95: бюджет с резервом
фото по-прежнему проверяется production-валидатором, padding не добавлен.
TestFixtureBoundsDecodedPixels ограничивает объём декодируемых пикселей. Из
TestPrepareWorld убран повтор полного decode ради version mismatch: отрицательный
кейс остаётся в usecase/world TestLoadFailures/version. Production C01, loader,
валидация изображений и тайм-ауты не изменены. По поручению владельца повторяются
только затронутые PrepareWorld/Sunday/fixture/version тесты и lint.
