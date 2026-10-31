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

`sessionevents.Hub` хранит только активные подписки, освобождает таймеры при отмене/отзыве и доставляет терминальную причину однократно. Observe регистрируется синхронно, затем usecase перечитывает сессию, исключая потерю отзыва при подключении. Транспорт будущего B04 обязан применять причину к тому же session/connection ID. В рабочем composition root используется `world.Unavailable` до B03: отсутствие каталога явно даёт 503 для сценариев, которым нужна поза. Fixtures не используются в рабочем запуске.
