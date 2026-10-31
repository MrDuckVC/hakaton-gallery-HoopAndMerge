import fs from 'node:fs';
import path from 'node:path';
const root=path.resolve(import.meta.dirname,'..');
const docs={
'AGENTS.md':`# Инструкции агенту backend

## Проект и источники
Отдельный репозиторий https://github.com/MrDuckVC/hakaton-gallery-HoopAndMerge. Go backend офлайн 3D-галереи для LAN: PostgreSQL, cookie-сессии, WebSocket, локальный Ollama. Frontend живёт отдельно. Не создавать монорепозиторий.
Сначала прочитай README.md, локальные инструкции, go.mod, Makefile, .golangci.yml, docker-compose.yml и .env.example. Затем docs/api/contract.md, conventions.md, openapi.yaml, websocket.md. Контракт пользователя в contract.md приоритетнее старых планов и примеров. Старые маршруты не поддерживать. При конфликте требований сообщи о нём и исправь производные схемы до кода.

## Стек и среда
Используй Go net/http, pgx/v5, coder/websocket, PostgreSQL и HTTP API Ollama. Точные версии брать из go.mod/go.sum и Compose, не выдумывать. Если go.mod отсутствует, сначала создай модуль с проверенным module path и стабильными совместимыми версиями. Новые зависимости — оригинальные upstream-пакеты с фиксацией версии; не добавляй ORM, Redis, фреймворки агентов без практической необходимости. Runtime полностью офлайн; всё скачать до отключения интернета.

## Сборка и проверки
Сверь команды с фактическим Makefile и CI. Не утверждай, что цель существует, пока не прочитан файл. После появления Go-модуля: go build ./..., go vet ./..., go test ./.... Для сетевых/конкурентных изменений go test -race ./... (нужен поддерживаемый toolchain/CGO). Форматировать изменённые .go через gofmt. Запускай настроенный в проекте golangci-lint. Для документов: node tools/build-contracts.mjs, node tools/validate-contracts.mjs. Node нужен только для этих инструментов.
Интеграционные тесты используют отдельную тестовую БД. Не выполнять очистку рабочей БД, migrate down, docker compose down -v или перезапись контента без прямого запроса. Реальные команды запуска и миграций держать в docs/DEVELOPMENT.md.

## Реализация
Работаем только по Clean Architecture: domain <- usecase <- adapters; cmd/server — composition root. Domain не зависит от usecase и инфраструктуры; usecase зависит только от domain и собственных портов. Интерфейсы определяет потребитель во внутреннем слое, внешние адаптеры реализуют их. Domain/usecase не импортируют net/http, WebSocket, pgx, Ollama SDK и filesystem API. Wire DTO и SQL rows преобразуются на границах. Бизнес-правила и переходы состояний — внутри; транспорт и хранение — снаружи. Запрещено обходить слои ради сроков MVP. Проверять граф импортов; правила — docs/ARCHITECTURE.md.
API-first; handlers вызывают usecase. Go mixedCaps, JSON snake_case. Применяй gofmt и https://go.dev/doc/effective_go. Принимай context.Context первым аргументом I/O; deadlines и отмена обязательны. Ошибки оборачивай с контекстом, наружу отдавай коды контракта. Не логируй токены, пароли, полные prompts и приватные диалоги. Используй параметризованный SQL и явные транзакции; миграции после применения не переписывай.
У каждого WS один writer. Поздние callback старого соединения проверяют connection ID и не меняют новое. Не держать mutex на SQL/LLM/сетевом I/O. Смена зала — сохранённая транзакция с epoch, движение не меняет зал. Пока world.ready не подтверждён, игровые команды отклоняются.
Локальная модель одна, очередь из БД; уникальность guide.ask и photo upload обеспечивается БД. Терминальная генерация неизменяема; partial output не память. Изоляция по user_id + guide_id. Ограничения безопасности, файлов и кеша: docs/api/contract.md.

## Тесты
Добавляй тесты на изменённые наблюдаемые правила, особенно сессии, ownership, epoch, коллизии, идемпотентность и гонки. Не ограничивайся happy path. Матрица в docs/TESTING.md. После исправления прогоняй связанные тесты и обязательные проверки. В отчёте перечисляй выполненные команды и результат; непроведённые проверки с причиной, без выдуманного успеха.

## Актуальность документов
Поддерживай документацию самостоятельно в том же изменении, что и код: HTTP -> OpenAPI; WS -> websocket.md/схемы; поля/лимиты -> contract.md и examples; SQL -> DATA_MODEL.md/миграции; config -> .env.example/DEVELOPMENT.md; команды -> README/AGENTS. Не менять утверждённую семантику только ради соответствия реализации. Обновляй затронутые документы, проверяй ссылки и fixtures. Существенные решения заноси в docs/DECISIONS.md. Не дублируй подробности здесь.
Максимум AGENTS.md — 6000 Unicode-символов, включая пробелы и переводы строк; цель 4000–5500. При росте переноси подробности в docs/ и оставляй ссылки. После изменения проверь размер. Не добавляй временные баги, текущие задачи, секреты и большие примеры кода.

## Коммиты и PR
Conventional Commits: feat:, fix:, docs:, test:, refactor:, chore:. Коммитить/пушить только в рамках запроса пользователя. PR описывает поведение, изменения контрактов/миграций и проверки; отмечай несовместимые изменения. Не включай .env, фото пользователей, модели, дампы, .idea и случайные артефакты. Не откатывай чужие изменения.
`,
'README.md':`# Gallery Backend

Разработка ведётся только по Clean Architecture: domain <- usecase <- adapters. Зависимости направлены внутрь, реализации связывает cmd/server. Требование обязательно и для MVP; обход слоёв ради сроков запрещён. Подробности — docs/ARCHITECTURE.md (для файлов в docs/ — ARCHITECTURE.md).

Backend многопользовательской 3D-галереи: посещение тематических залов, общий чат, фотографии и локальные ИИ-гиды с персональной памятью. Во время игры интернет не требуется; клиенты подключаются по LAN/Wi-Fi.

Репозиторий: https://github.com/MrDuckVC/hakaton-gallery-HoopAndMerge. Frontend разрабатывается отдельно.

## Стек
Go net/http, PostgreSQL, pgx/v5, coder/websocket, локальный Ollama. Версии фиксируются в go.mod/go.sum и docker-compose.yml. Один процесс Go обслуживает HTTP и WS, одна локальная модель — всех гидов.

## Запуск
Это базовый README для начала реализации: доступ к существующему коду репозитория не получен. Команды Makefile, имена Compose-сервисов и наличие cmd/server пока не подтверждены.
1. Установить Go и PostgreSQL согласно версиям проекта; скачать зависимости и локальную модель до офлайн-запуска.
2. Создать локальный .env по .env.example; настроить БД, Origin, путь мира и Ollama.
3. Запустить БД, применить миграции, подготовить/проверить контент.
4. Собрать и запустить Go entrypoint. Для выбранной структуры: go build -o bin/gallery ./cmd/server; это целевая команда после реализации cmd/server, а не подтверждённая существующая.
5. Настроить frontend proxy /api -> Go с поддержкой WS; при демонстрации возможно раздавать frontend dist сервером с одного origin.

Подробности и предлагаемые env: docs/DEVELOPMENT.md. Агент реализации обязан заменить этот раздел проверенными командами до первой рабочей сборки.

## API и документы
- docs/api/contract.md — нормативный контракт пользователя; имеет приоритет над старыми планами.
- docs/api/openapi.yaml — HTTP; docs/api/websocket.md — WS.
- docs/api/examples/ — готовые JSON-fixtures для параллельной разработки клиента.
- docs/ARCHITECTURE.md, docs/DATA_MODEL.md — устройство сервера.
- docs/IMPLEMENTATION_PLAN.md, docs/TESTING.md — порядок реализации и проверки.
- AGENTS.md — инструкции ИИ-агентам.

## Проверки
После появления Go-модуля: go build ./..., go vet ./..., go test ./.... Конкурентный код: go test -race ./.... Команды линтера сверяются с настройками репозитория.
Документация: node tools/build-contracts.mjs && node tools/validate-contracts.mjs. Это инструменты контракта, не runtime-зависимость сервера.

Комплект сейчас содержит документацию, не работающий сервер и не готовый пакет мира. Примеры используют демонстрационные ID и размер картинки; реальный asset не включён. Полный перечень комплекта — docs/INDEX.md.
`,
'docs/INDEX.md':`# Карта документации

contract.md в docs/api — точная редакция API пользователя, нормативный источник. conventions.md и websocket.md — навигация и уточнения, не альтернативный контракт. OpenAPI и WS JSON Schema описывают структуру; условия состояний, порядок событий, видимость и лимиты дополнительно нормативны в contract.md.

Порядок чтения: PROJECT_CONTEXT -> api/contract -> ARCHITECTURE -> DATA_MODEL -> DEVELOPMENT -> IMPLEMENTATION_PLAN -> TESTING -> DECISIONS. AGENTS.md — короткие инструкции, без дублирования этих файлов.

При изменении модели данных сверять SQL и DATA_MODEL. При изменении wire сверять contract/OpenAPI/WS schemas/examples. При изменении команды — README/AGENTS/DEVELOPMENT/Makefile. Источник генерации машинных файлов tools/build-contracts.mjs; после редактирования перегенерировать и валидировать. tools/write-docs.mjs — первоначальный scaffold, не запускать поверх отредактированных документов.
`,
'docs/api/conventions.md':`# Соглашения API

Нормативный источник — contract.md (редакция пользователя), разделы 1–3, 8–10. В отличие от первоначального эскиза: только один текущий зал; никаких available_rooms/world.rooms_available, соседнего prefetch, моделей или /messages. Успех без data; HTTP error содержит request_id, WS error — ErrorBody без обязательного retryable.

HTTP /api/v1, JSON UTF-8 snake_case. Cookie museum_session. Все HTTP-ответы no-store и X-Request-ID. Logout — явное исключение из общего требования сессии: идемпотентен и без cookie, но Origin обязателен. Origin для POST и upgrade проверяется точным совпадением. В URL ID кодировать как сегмент, request_id команды и photo Idempotency-Key — UUID v4.

Манифест: max_loaded_rooms=1, photo_reserve_bytes=3145728. Доступ к ресурсам только текущей сессии-владельцу активного WS, только текущий зал. Пока not-ready, скачивание пакета разрешено; игровые действия — нет. Размеры точных файлов и photo reserve проверяются до допуска мира. Переход через дверь, сохраняется до world.entered; epoch сбрасывает движение.

OpenAPI 3.1.0 выбран как формат инструмента, а не утверждение о latest. JSON Schema 2020-12 — структура событий; неоднозначности бизнес-правил решаются по contract.md. Строковые byte-лимиты пароля и multipart, trim, размер декодированных сообщений и условные поля статусов должны проверяться кодом сверх схем.

Стороны, лучи видимости, коллизии цилиндра, rate limits, тайм-ауты и порядок пагинации брать из contract.md без упрощения. Изображения — только неподвижный JPEG/WebP. До реализации выбрать поддерживаемый WebP decoder из upstream и проверить лицензию/поддержку статического изображения; не утверждать наличие decoder в stdlib.
`,
'docs/api/websocket.md':`# WS v1 — указатель реализации

Нормативный источник: contract.md, разделы 4–7 и 9. Машинные схемы: ws-client.schema.json и ws-server.schema.json. Примеры: examples/. Старые события не поддерживаются.

Соединение: authenticate + Origin -> upgrade -> согласованный world.snapshot -> загрузка текущего зала -> world.ready -> подтверждение world.ready -> движение/взаимодействия. На аккаунт один socket. Replacement close 4009; другим left затем joined. Connection ID защищает новое соединение от поздних действий старого.

Переход: world.enter(door_id,epoch) -> проверить дистанцию/LOS -> отменить вопрос -> транзакция позиции и epoch -> world.entered(self,current_room) -> выгрузить старый зал -> загрузить новый -> world.ready. Обычное движение не меняет room_id. world.entered обязательно опережает state с новой позицией.

События клиента: world.ready, world.enter, player.move, chat.send, exhibit.inspect, guide.ask, guide.cancel.
События сервера: world.snapshot, world.ready, world.entered, player.joined, player.left, players.state, chat.message, exhibit.inspected, guide.accepted, guide.started, guide.delta, guide.completed, guide.failed, guide.cancelled, error.

Полный Player в snapshot/joined/state включает display_name,avatar_color,movement_epoch,ack_seq. State полный, 10 Гц. До ready любые игровые команды кроме chat/cancel отклоняются; HTTP-истории доступны без WS. Пропущенные движения/чат не воспроизводятся.

Guide: accepted содержит Generation; затем started, delta*, completed/failed. Статусы queued/running/completed/failed. Cancellation — failed с CANCELLED, затем cancelled-ответ на команду отмены. Терминальные записи неизменяемы. После переподключения использовать generations и by-request, не повторять ask автоматически. Повтор ask того же UUID и payload возвращает accepted с текущей Generation; проверка DB-ключа раньше ready/дистанции/очереди.

Единственный writer, исходящая очередь <=1 MiB, сообщение <=256 KiB. Переполнение close 1013. Входящий message <=16 KiB, binary close 1003. Ping 20s/pong 10s. Полная таблица close codes, retry и limits — в contract.md; не использовать старую таблицу из переписки.
`,
'docs/ARCHITECTURE.md':`# Чистая архитектура backend

Clean Architecture обязательна для всего backend, включая MVP. Упрощать можно объём функций, но не направление зависимостей. Один Go-процесс, PostgreSQL и локальный Ollama; frontend в отдельном репозитории. Wire-контракт не меняется из-за организации кода.

## Слои и направление зависимостей

Импорты: adapters -> usecase -> domain. Адаптеры могут зависеть от обоих внутренних слоёв. Обратные зависимости запрещены. cmd/server создаёт реализации и внедряет их через интерфейсы; это единственный composition root.

| Слой | Ответственность | Ограничение |
|---|---|---|
| domain | Сущности, value objects, геометрические правила, состояния, инварианты | Нет usecase/adapters, сети, SQL, файлов, env |
| usecase | Сценарии входа, движения, переходов, общения, генерации и фото; порты | Нет adapters, net/http, pgx, websocket/LLM SDK, filesystem API |
| adapters/inbound | HTTP/WS, middleware, DTO, структурная валидация, преобразование ошибок | Нет SQL и бизнес-правил в handlers |
| adapters/outbound | PostgreSQL, Ollama HTTP, файлы, image codecs, clock/ID реализации | Не меняют бизнес-состояния в обход usecase |
| cmd/server | Config, DI, startup/shutdown | Не реализует сценарии |

Стандартные context/time/errors допустимы во внутренних слоях. Request, ResponseWriter, websocket.Conn, pgx.Tx, Ollama response и файловые дескрипторы через порты не передавать. JSON DTO в inbound, SQL rows в postgres; преобразования явные. Не отдавать entity или запись БД напрямую как wire response.

## Целевая структура

    cmd/server/                         # composition root
    internal/domain/
      auth/ player/ world/ guide/ photo/
    internal/usecase/
      auth/ game/ guide/ photo/          # сценарии и порты потребителей
    internal/adapters/
      inbound/httpapi/                   # HTTP DTO, handlers, Origin/cookie
      inbound/websocket/                 # WS DTO, reader/writer, network hub
      outbound/postgres/                 # repositories, transactions, queue
      outbound/ollama/                   # локальный HTTP/NDJSON
      outbound/files/                    # world catalog и photo staging
      outbound/images/                   # decoder/thumbnail encoder
    migrations/ content/ docs/ build/

Пакеты по функциям внутри слоёв допустимы при сохранении графа импортов. Интерфейс определяет потребитель в usecase; domain-интерфейс допустим только как собственный доменный контракт. Адаптер реализует его неявно. Не создавать BaseRepository/BaseService или интерфейс на каждый struct. Порты узкие: session store, player state store, generation store, model stream, photo store, world catalog, clock/ID generator.

## Транзакции, состояния и конкурентность

Ready/epoch/seq, ownership, правила очереди и терминальные переходы принадлежат domain/usecase. Game loop сериализует сценарии; network hub только управляет транспортом. Начальный epoch=1, дверь увеличивает его. Поздние callback проверяют connection ID/epoch, cleanup старого socket не меняет новое соединение.

Usecase задаёт атомарную операцию; PostgreSQL-адаптер реализует транзакцию через специализированный порт либо UnitOfWork с внутренними repository interfaces. Не принимать pgx.Tx в usecase и не держать SQL-транзакцию на LLM/file I/O. Завершение generation+memory атомарно. Фото FS+SQL согласуются сценарием компенсаций через порты staging/commit/delete.

Reader декодирует команды и вызывает usecase. Один writer пишет в WS, исходящая очередь ограничена байтами. Не держать mutex при SQL/LLM/сетевом I/O. Порядок snapshot до событий и entered до нового state обеспечивается последовательностью usecase и writer queue. Сетевая механика backpressure/close — в WS-адаптере.

## Локальный ИИ и lifecycle

Usecase гида собирает профиль, релевантные знания и личную память. Ollama-адаптер передаёт запрос и поток. Одна генерация, FIFO8 через порт PostgreSQL, без partial в памяти. При старте queued/running -> failed SERVER_RESTARTED до обслуживания. Output limit8000. Модель не исполняет произвольный SQL/инструменты.

cmd/server: config -> DB check -> world validation usecase -> recovery usecase -> listeners. Shutdown: остановить upgrade, отменить генерации, сохранить позиции, закрыть WS1012/HTTP с deadline. При недоступной модели прогулка остаётся доступна, вопрос завершается по контракту. Ошибка сохранения позиции -> close1011.

## Архитектурная проверка

Domain unit tests без БД/сети/файлов; usecase tests через fake ports; адаптеры проверяются отдельно integration tests. Контролировать импорты через go list и архитектурный тест либо depguard в фактической версии golangci-lint. PR с нарушением слоёв не принимается даже при проходящих функциональных тестах. Исключения требуют изменения архитектурного требования пользователем, а не записи о временном обходе.
`,
'docs/DATA_MODEL.md':`# Предлагаемая модель PostgreSQL

Это проект миграций, не утверждение о существующей БД. Внешние ID сериализуются строками; UUID внутри допустим. Все timestamps timestamptz. SQL параметризован. Владельца брать из сессии.

| Таблица | Основные поля / ограничения |
|---|---|
| users | id PK, username UNIQUE lowercase, display_name, avatar_color, password_hash с параметрами KDF, created_at |
| sessions | id PK, token_hash UNIQUE, user_id FK, expires_at, revoked_at nullable |
| player_states | user_id PK FK, world_version, room_id, x/y/z,yaw,pitch,updated_at; Y=0 |
| exhibit_inspections | user_id, world_version, exhibit_id composite PK, inspected_at обновляется |
| guide_memories | user_id,guide_id composite PK, summary, version,updated_at |
| guide_generations | id PK,user_id,request_id UUID,guide_id,exhibit_id nullable,normalized_payload_hash,status,user_text,user_message_id,user_created_at,assistant_text/message_id/created_at nullable,error_code/message/retryable nullable,created_at,started_at,finished_at,origin_connection_id |
| photos | id PK,user_id,idempotency_key UUID,request_fingerprint,world_version,room_id,movement_epoch,exhibit_id nullable,caption,published default false,original_path,thumbnail_path,image_bytes,thumbnail_bytes,width,height,created_at |
| memory_events | id PK,user_id,type,world_version,room_id,exhibit_id nullable,photo_id nullable,created_at |

Уникальные guide_generations(user_id,request_id), photos(user_id,idempotency_key). Частичный UNIQUE(user_id) WHERE status IN ('queued','running') обеспечивает один вопрос при гонке запросов. Индексы history(user_id,guide_id,created_at,id), album(user_id,created_at,id), sessions(token_hash), queue(status,created_at,id).

CHECK generation: queued/running -> assistant/error null; completed -> assistant заполнен,error null; failed -> assistant null,error заполнен. Терминальный переход только UPDATE ... WHERE status IN ('queued','running') RETURNING; победитель единственный. Failed user_message остаётся в истории. Полный Generation строить из записи, message role задаётся сервером. queued accepted формируется до старта worker; worker не может опередить постановку accepted в outgoing queue.

guide.ask: поиск существующего key -> сравнение fingerprint нормализованных text/guide_id/exhibit_id -> accepted(current) либо conflict; для нового проверить game/limits/queue -> транзакция insert+резерв очереди -> accepted -> worker. Ограничение 8 queued проверять с общей блокировкой scheduler/транзакции, а не COUNT без сериализации. Завершение: assistant+status+memory в одной транзакции. При потере DB не посылать success.

Photo fingerprint: SHA-256 проверенного исходного файла + канонически закодированные нормализованные поля, включая room_id/movement_epoch/exhibit_id/caption. Конкурентные повторы сериализуются advisory lock/резервацией по user/key. Повтор совпадающего ключа проверять до текущего room/ready, но после сессии, лимита и проверки содержимого. Вернуть существующее Photo200 даже после перехода.

Файловая система и SQL не имеют общей транзакции: staging -> decode+thumbnail -> atomic rename -> SQL commit -> доступ. При ошибке удалить собственные staged/final файлы; периодический cleanup сверяет references, не трогает чужие активные uploads. Фото становится видимо только после полного commit. memory_event PHOTO_CAPTURED создаётся в той же SQL транзакции. published=true фиксируется перед chat broadcast.

Positions: save раз в5s и disconnect; door transition сохранить до ACK. cursor history по (created_at,id), привязан к user+guide; album аналогично user. Подписанный opaque cursor либо строгая server-side lookup/валидация, не доверять переданному user_id. Мир файловый, FK на room/guide заменяется startup/service validation. Миграции append-only, down с потерей данных только по прямому запросу.
`,
'docs/DEVELOPMENT.md':`# Локальная разработка и конфигурация

Статус: существующие Makefile/Compose/AGENTS репозитория не прочитаны (GitHub-коннектор404). Ниже целевой интерфейс, агент должен сверить и обновить после получения кода. Не выполнять неизвестные make-цели наугад.

После создания go.mod: go version; go mod download (до offline); go build ./...; go vet ./...; go test ./...; go test -race ./... для конкуренции. Форматирование gofmt. Линтер использовать версии проекта. Entry cmd/server и Makefile цели run/build/test/lint/migrate-up/world-check — предложения, пока не реализованы.

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
`,
'docs/IMPLEMENTATION_PLAN.md':`# Реализация: 2 дня × 8 часов backend

Каждый этап реализуется только по Clean Architecture из ARCHITECTURE.md: сначала domain и usecase с портами, затем адаптеры и DI. Проверка графа импортов обязательна; бизнес-логику не переносить в handlers/SQL ради сроков.

День1: 0–1ч сверить repo/команды/контракты, config, миграции пользователей/сессий/position, запустить модель и измерить. 1–2ч register/login/me/logout, Origin, session checks. 2–3ч WS snapshot, ownership, presence, full state/chat, bounded writer. 3–4ч world package, ready, epoch, collision, doors и persistence transitions. 4–5ч registry knowledge/profiles, generations unique keys/history, queue. 5–6ч Ollama stream, completion/cancel/recovery, inspect память. 6–7ч интеграция frontend по fixtures, сохранение5s, rate limits. 7–8ч offline demo и исправления.

День2: 0–2ч photo multipart/idempotency/file consistency, thumbnail и access. 2–3ч album/chat publishing/фото события памяти. 3–4ч качество retrieval/memory, лимиты context/output. 4–6ч race/integration/security/slow client/collision tests. 6–7ч restart/потери ответов/upload race/контент бюджет. 7–8ч сборка, обновление docs, полный offline rehearsal.

Первый день демонстрирует обязательные пункты задания, фотография может оставаться скачиванием на клиенте. API фото реализуется во второй день без изменения контракта. При нехватке времени сокращать количество тщательно оформленных залов/декор, но не лимит кеша, sessions, память или локальность ИИ. Нельзя увеличивать знаменатель пустыми файлами.

Definition of done на изменение: wire/schema/examples согласованы; видимые правила покрыты тестами; изменённая конфигурация документирована; build/vet/tests пройдены или указана конкретная причина ограничения; offline поведение проверено. На финал — пункты10 contract.md, не только happy path.
`,
'docs/TESTING.md':`# Проверки backend

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
`,
'docs/DECISIONS.md':`# Архитектурные решения

| Решение | Основание | Последствие |
|---|---|---|
| Только Clean Architecture | Прямое требование команды | domain <- usecase <- adapters; DI в cmd/server; проверки импортов; без обхода слоёв для MVP |
| Отдельный backend repository | Требование команды | Frontend исходники/пакеты не добавлять |
| Один загруженный зал, двери | Бюджет10%, простота | ready+epoch, без prefetch и физических переходов |
| Cookie session + one origin | Браузеры в LAN | Exact Origin, server expiry, один WS/account |
| PostgreSQL | Выбранный командой стек | Unique idempotency, транзакции, durable queue |
| Один Ollama worker | Laptop VRAM | FIFO8, явные timeout/queue failures |
| Generation как история | Восстановление после потери события | /generations и by-request, не /messages |
| Photo original+thumbnail files | Размер и скорость | SQL/FS компенсации, приватные handlers |
| Wire schemas из генератора | Согласованность двух repo | Проверять Markdown,schemas,fixtures вместе |

Для новых решений добавлять причину и последствия, не временные баги. Действующая семантика v1 закреплена в api/contract.md; изменения обязательных полей/событий требуют новой версии, не скрытых aliases.
`
};
for(const [file,text]of Object.entries(docs)){const dest=path.join(root,file);fs.mkdirSync(path.dirname(dest),{recursive:true});fs.writeFileSync(dest,text);}
console.log('Written backend-only documentation; AGENTS chars:',Array.from(docs['AGENTS.md']).length);
