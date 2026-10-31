# Чистая архитектура backend

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


## Реализованная основа B01

`internal/config` — внешний конфигурационный пакет composition root. `cmd/server` собирает pgxpool, read-only schema check и HTTP-адаптер; `cmd/migrate` — отдельная административная точка входа для forward-only goose. SQL встроен из `migrations/`. Domain/usecase не импортируют config, migrations или библиотеки SQL/HTTP.

`internal/usecase/lifecycle` определяет порты StopAccepting, CancelGenerations, SavePositions, CloseConnections. Участники обязаны соблюдать context deadline; координатор продолжает cleanup после ошибок и ограничивает ожидание каждого участника общим контекстом. По истечении deadline последующие cleanup-вызовы запускаются best-effort с отменённым контекстом; процесс не ждёт зависшие реализации. Composition root ограничивает весь shutdown, включая HTTP drain/закрытие пула, 15 секундами и принудительно закрывает HTTP по deadline. Пока игровые участники не зарегистрированы; B03/B07 добавят world validation и recovery до listeners. Это не заменяет будущие сценарии сохранения и отмены пустыми реализациями.

## Аккаунты и сессии B02

`internal/domain/auth` задаёт нормализацию credentials, публичного пользователя, приватный verifier и валидность сессии; `internal/domain/player` — сохраняемую позу с ревизией. `internal/usecase/auth` определяет порты Store, Security, World, Events и сценарии register/login/logout/me. HTTP DTO и сопоставление ошибок остаются в inbound; SQL rows, транзакции и параметры драйвера — в postgres; KDF/случайные значения — в security. Новых библиотек не добавлено.

Register сохраняет account+spawn+session и отзыв старой cookie-сессии одной транзакцией. Login сохраняет новую сессию и отзыв старой одной транзакцией; KDF выполняется до открытия транзакции. Уведомление публикуется после commit. `/me` проверяет позу через порт мира и сохраняет исправление сравнением ревизии; при конфликте перечитывает состояние, максимум три попытки. Перед исправлением usecase повторно проверяет сессию; SQL-адаптер проверяет её под FOR SHARE и повторно проверяет expiry перед commit, исключая запись после отзыва/истечения. Вызовы каталога не держат SQL-транзакцию.

`sessionevents.Hub` хранит только активные подписки, освобождает таймеры при отмене/отзыве и доставляет терминальную причину однократно. Observe регистрируется синхронно, затем usecase перечитывает сессию, исключая потерю отзыва при подключении. Транспорт B04 применяет причину к тому же session/connection ID. В B03 composition root подключает проверенный каталог вместо `world.Unavailable`; отсутствие корректного каталога блокирует startup. Fixtures не используются в рабочем запуске.

## Каталог B03

`domain/world` задаёт геометрию, связность, ссылки, безопасные spawns и пакетный бюджет. `usecase/world` определяет Source, SnapshotSizer, Sessions и Ownership, валидирует полную загрузку и владеет неизменяемыми копиями файлов. Каталог реализует auth.World и предоставляет Package/Spawn будущему game usecase. Размер полного snapshot измеряет внешний inbound/worldwire с общими DTO; внутренние слои не сериализуют HTTP/WS.

Файловый адаптер outbound/world читает через os.Root, проверяет JSON/изображения, выявляет aliases по файловой идентичности и преобразует DTO в domain. HTTP получает исходные JSON/JPEG/WebP байты через сценарий с проверкой persisted session и атомарным снимком Ownership. Ready не является условием загрузки. B04 подключает game.Service вместо NoConnections; БД-поза не подменяет активного владельца. Формат контента — WORLD_CONTENT.md.

## Транспорт B04

`usecase/game.Service` владеет резервациями/активными connection ID, ownership и упорядочиванием snapshot/presence. Подготовка Identity вызывает B02 Me (с проверкой/исправлением сохранённой позы) и B03 Package до захвата registry lock. Reserve ограничивает активные и резервируемые аккаунты числом 32; один незавершённый handshake на аккаунт. Attach заменяет владельца только после успешного upgrade и постановки snapshot первым событием. Порты Sink.Enqueue/Close выполняют только ограниченные операции с памятью, без сетевого ожидания. Поздние команды/события/cleanup проверяют connection ID. HTTP world.Ownership реализует тот же реестр; SQL-поза не даёт доступа без активного WS.

`adapters/inbound/websocket` отвечает за Origin, wire DTO/строгую структурную валидацию, coder/websocket, единственный data writer, byte-bounded очередь и control frames. Дедупликация и sliding windows остаются в usecase, сериализация — в адаптере. Неподключённые игровые сценарии представлены явным PendingHandler; B05 заменит его, сохранив контракт и обязательные проверки connection ID при фиксации результатов. Порт AdmitNewAsk предназначен для вызова после устойчивой проверки идемпотентности будущего guide subsystem; пока генераций нет, stub-вопросы считаются новыми.

Composition root связывает настоящие accounts/catalog/ownership и регистрирует transport в lifecycle. WS observer имеет отдельный контекст жизни, связанный с handshake deadline только до upgrade; disconnect освобождает наблюдение и все workers. Сетевые операции не выполняются под registry mutex. Протокольные ping/pong/close обрабатывает библиотека; application data пишет только один worker.

WS handshake ResponseWriter нормализует pre-upgrade ошибки coder/websocket в HTTP JSON и сохраняет исходный hijacked net.Conn. При shutdown deadline закрывается этот сокет, а не ожидающий чужой close handshake метод Conn.CloseNow; это прерывает reader/writer и освобождает workers без продления deadline. Wire-ошибки распознанных обычных команд сохраняют читаемый UUID в дедупликации даже при отклонении payload; guide.ask и player.move остаются исключениями.
