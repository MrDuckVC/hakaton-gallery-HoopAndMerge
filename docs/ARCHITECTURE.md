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

`adapters/inbound/websocket` отвечает за Origin, wire DTO/строгую структурную валидацию, coder/websocket, единственный data writer, byte-bounded очередь и control frames. Дедупликация и sliding windows остаются в usecase, сериализация — в адаптере. B05 подключает game.Service как Handler; PendingHandler остаётся только явной заглушкой для изолированных тестов транспорта. Порт AdmitNewAsk предназначен для вызова после устойчивой проверки идемпотентности будущего guide subsystem; пока генераций нет, stub-вопросы считаются новыми.

Composition root связывает настоящие accounts/catalog/ownership и регистрирует transport в lifecycle. WS observer имеет отдельный контекст жизни, связанный с handshake deadline только до upgrade; disconnect освобождает наблюдение и все workers. Сетевые операции не выполняются под registry mutex. Протокольные ping/pong/close обрабатывает библиотека; application data пишет только один worker.

WS handshake ResponseWriter нормализует pre-upgrade ошибки coder/websocket в HTTP JSON и сохраняет исходный hijacked net.Conn. При shutdown deadline закрывается этот сокет, а не ожидающий чужой close handshake метод Conn.CloseNow; это прерывает reader/writer и освобождает workers без продления deadline. Wire-ошибки распознанных обычных команд сохраняют читаемый UUID в дедупликации даже при отклонении payload; guide.ask и player.move остаются исключениями.

## Авторитетное состояние B05

Game usecase хранит ready/epoch/ack_seq и время последнего обработанного move. Domain world содержит конечные углы, точный sweep цилиндра и LOS; каталог выдаёт независимые копии геометрии и направленные spawns. WS adapter только декодирует команды и сериализует события; runtime связывает GameStore и запускает таймеры.

Отменяемый per-user gate последовательно допускает команды, Attach/Detach/End и записи позиции; mutex реестра удерживается только при работе с памятью и ограниченной enqueue. SQL и порт отмены вопроса выполняются вне mutex. Рассылка полного состояния каждые 100 мс независима от SQL, одновременно работает один цикл периодического save. Тики во время цикла объединяются в одну отложенную попытку, запускаемую сразу после его завершения, а не отбрасываются. Переход сначала сохраняется, затем поза, пакет, epoch и HTTP-владение публикуются под одной блокировкой вместе с entered. Replacement использует актуальное состояние, а не предварительный Prepare; после ошибки хранения перечитывает каноническую позу из БД. Новое соединение получает epoch=1/ack=0.

Порт Questions отменяет незавершённый вопрос до перехода/замены; в B07 подключён usecase гидов. Его ошибка запрещает переход. Если отмена прошла, но SQL не сохранился, отмена не откатывается. При disconnect admission/HTTP-владение отзываются немедленно под registry mutex до ожидания gate и SQL; завершённые соединения исключаются из snapshot/state. Ожидание gate и отмена вопроса при disconnect имеют общий бюджет 5 секунд; затем финальный flush получает новый независимый бюджет 5 секунд, даже после ошибки или тайм-аута отмены. Финальный flush использует отдельный контекст и сохраняет только ранее авторизованную серверную позу, в том числе после logout; CAS и сериализация не позволяют старому cleanup перезаписать нового владельца.

Ошибка SaveState при переходе закрывает текущий WS с 1011 (ошибка сессии — 4001), поскольку порт не гарантирует, что COMMIT не состоялся. Старые команды/HTTP-владение сразу запрещены; новое соединение читает каноническую позицию. До успешной записи world.entered не публикуется.

## Генерации B07

`domain/guide` задаёт нормализованный payload, fingerprint и состояния; `usecase/guide` владеет admission, одним worker, тайм-аутами, отменой, history и recovery. Порты Store/Model/Events/World/Sessions/Clock не передают транспорт или SQL во внутренние слои. Game передаёт серверный снимок игрока под пользовательским gate; события гида возвращаются через connection-scoped неблокирующий Events без повторного захвата пользовательского gate.

PostgreSQL-адаптер сериализует очередь через отдельную scheduler-строку и сохраняет completion+memory одной транзакцией. Общий gate гида согласует accepted с worker; вызов модели идёт вне gate. Независимый sweep завершает просроченные queued, даже когда модель занята. Worker ждёт остановки отменённой модели до следующего вызова. Неопределённый результат SQL блокирует новое выполнение до recovery, закрывает затронутый WS с 1011; recovery уведомляет остальных владельцев pending.

Composition root выполняет recovery до listener, подключает порты отмены и lifecycle Generations. B08 связывает Ollama HTTP/NDJSON, типизированные профили каталога и ContextReader PostgreSQL. Usecase собирает ModelRequest; адаптер модели не читает БД и не выбирает личность. Детерминированные выдержки вычисляет usecase-функция внутри атомарного Finish. Прямого импорта game в guide нет. Поддельных ответов в runtime нет. Пределы и правила — [GUIDES.md](GUIDES.md).


## Загрузка фото B09

В domain/photo находятся Photo, нормализация и fingerprint; usecase/photo определяет порты Sessions, Images, Files, Game и Repository. HTTP строго разбирает ограниченный multipart и формирует DTO; images выполняет DecodeConfig/Decode и JPEG thumbnail, photofiles публикует каталог с парой файлов; PostgreSQL реализует сериализацию ключа и транзакцию фото+память. Domain/usecase не знают файловых путей, pgx или HTTP.

Идемпотентный replay проверяется до текущего ready/room. Capture запоминает исходный connection ID, а WithCapture использует пользовательский gate B05. После SQL-записей непосредственно перед COMMIT повторно проверяются owning session/socket, ready, room, epoch и inspect distance/LOS. Проверка — точка принятия загрузки: detach/shutdown до неё отклоняет снимок, последующее отключение может пересекаться с завершением COMMIT. Общий mutex не удерживается на I/O; gate исключает движение/переход/replacement в финализации. Файлы публикуются до начала SQL-транзакции. Неопределённый результат COMMIT разрешается отдельным чтением под новой блокировкой ключа; до установления исхода компенсация запрещена.

## B10: библиотека фото

usecase/photo.Library определяет узкие LibraryStore и Reader для личного альбома, защищённого чтения и необратимой публикации. Авторизация чтения выполняется до файлового I/O. Проверка владельца при публикации передаётся в специализированную атомарную операцию хранилища и выполняется над заблокированным фото; адаптер PostgreSQL обеспечивает транзакцию и повторную проверку сессии. Domain/usecase не получают пути или файловые дескрипторы.

outbound/photopublisher связывает photo.Library с потребительским портом game.PhotoPublisher, преобразуя успешно зафиксированное фото в метаданные чата. Зависимости между usecase-пакетами не добавляются. HTTP формирует DTO и отдаёт проверенные байты; photofiles ограничивает чтение PHOTO_DIR, фиксированными именами, типом файла и лимитами размера. Composition root подключает реальную публикацию вместо заглушки.

## I01 — публичный frontend dist

Раздача готовых файлов — внешний HTTP-адаптер `httpapi.Frontend`, связанный в
cmd/server. os.Root ограничивает файловый доступ каталогом сборки; hidden/symlink
и directory listing запрещены. Проверка index.html происходит до открытия listener,
root закрывается после HTTP drain. Domain/usecase не знают о frontend/filesystem.
API и WS сохраняют маршрутизацию и авторизацию; SPA fallback не обрабатывает `/api`.
Исходники frontend остаются в отдельном репозитории. Отдельный reverse proxy из
исходного I01 заменён прямой раздачей Go по уточнению владельца.


## S01 — runtime поверх неизменяемого каталога

`domain/runtime` задаёт RoomConfig, Overlay, lamp, seed и геометрические ограничения.
`usecase/runtime` определяет потребительские порты Catalog/Store/RoleStore/Publisher/Sizer;
SQL rows и публичные DTO преобразуют postgres/runtime и inbound/runtimewire.
Размер 32 КиБ проверяется внешним wire Sizer до записи, без JSON внутри usecase.

Контекстный канал на каждый зал сериализует durable read/CAS/publication. Cache mutex
держится только на копировании памяти. Game определяет порт Runtime.WithRoom:
SQL выполняется вне game mutex, затем callback ставит snapshot/entered и runtime
в единственную очередь, удерживая room gate. Поэтому конкурентный commit не теряется
между чтением и подключением/переходом. Порядок: user gate → room gate → game mutex;
runtime publisher берёт только game mutex. S05 добавляет полный NPC-снимок
после runtime в той же очереди; игровая usecase владеет симуляцией.

Game и guide получают effective Geometry от runtime service. Immutable Package/Spawn,
файловые downloads и бюджет остаются у базового каталога; public guide positions
строятся из того же overlay, что серверные distance/LOS. Неопределённый COMMIT
удаляет cached geometry, отзывает owning room WS и закрывает их 1011. WithRoom
на новом подключении читает канонический overlay до публикации состояния.

## S04: составной светильник

`domain/runtime.ApplyObject` проверяет геометрию взаимодействия и переходы состояния.
`usecase/game.Objects` — порт действий/release/reconciliation; game владеет ready/epoch,
user gates, UUID/rate и живыми connection ID. Runtime владеет room gates и `LampStore`:
SQL CAS сравнивает revision и старую пару holder. Порядок user gate → room gate →
краткий registry mutex; callbacks runtime не захватывают пользовательские gates и не
выполняют I/O. Команда повторно проверяет допуск непосредственно перед CAS.

Game содержит один cleanup worker и map грязных комнат. Каждая попытка перечитывает
БД и проверяет live owner; generation записи предотвращает потерю работы при новых
закрытиях во время I/O. Ошибки сохраняют backoff даже при concurrent invalidation.
Startup выполняет recovery до listener, lifecycle освобождает объекты до закрытия WS
и storage. При аварийном завершении следующая startup recovery снимает stale holder.

## S03 — телеметрия подключений

`usecase/game` хранит FPS, visible, sampled_at, outstanding nonce, RTT и два
скользящих окна внутри owning connection. Clock внедрён; `time.Now` сохраняет
монотонную составляющую для интервалов. Существующий игровой tick планирует probe
через bounded Sink, а HTTP consumer-owned порт читает копии публичных измерений.
Arrival pong и соответствующий nonce фиксируются до SQL-аутентификации; только после
её успеха и повторной проверки connection ID обновляется RTT. Задержка проверки роли/
сессии в БД в RTT не входит, позднее завершение не погашает следующий nonce и не
переписывает более новый результат.
JSON DTO остаются в inbound-адаптерах; auth/admin membership проверяются прежними
портами. Телеметрия не требует SQL или пользовательского persistence gate.
Ended соединение исключается из admin snapshot до завершающего SQL-cleanup;
replacement создаёт новое состояние, старые callbacks проверяют connection ID.

## S02 — профили и публичная идентичность

`domain/guide` задаёт Settings, Override и detached Snapshot, нормализацию и выбор
материалов из собственного профиля. `usecase/runtime` определяет ProfileCatalog/
ProfileStore и координирует profile CAS с существующими gates затронутых комнат.
SQL-транзакция адаптера сохраняет override и room revisions вместе; после commit
кеши effective identity обновляются до публикации runtime. Все room/lamp/geometry
пути используют общий resolver публичных имени/subtitle, не C01 напрямую.

GuideStore.Claim под scheduler SQL lock читает текущий override и возвращает
Generation с private ProfileSnapshot. Context.Build использует этот снимок;
публичные inbound DTO его исключают. Fixed instructions остаются в usecase,
prepared materials — в файловом каталоге, terminal+memory — в прежнем Finish.
Composition root подключает оба profile store/reader порта до Initialize/listener.
Подробности выбора и recovery — [DECISIONS.md](DECISIONS.md), раздел S02.

## S05 — детерминированные посетители

`domain/npc` содержит FSM и prevalidated patrol plans. Подготовка ограниченной
сетки использует существующий world.Sweep и исключает door areas; композиция
передаёт неизменяемые Rooms и генератор instance ID в game.ConfigureNPCs до listener.
`usecase/game` владеет активными симуляциями под прежним registry mutex: изменение
ownership и удаление пустой комнаты атомарны, поздний callback сверяет connection ID.
SQL/LLM и room gates в NPC tick не вызываются; движение идёт по готовым отрезкам.
Никаких reverse callbacks/дополнительных mutex симуляции нет.

Типизированный game.NPCSnapshot содержит detached NPC states. Inbound websocket
создаёт wire DTO и проверяет payload ≤24 КиБ, затем использует прежний bounded Sink
и единственный writer. Полный runtime и соответствующий NPC snapshot ставятся
последовательно под game mutex. Принятый текст chat регистрирует реакцию только
после успешного фото-I/O и повторной проверки текущего подключения.
