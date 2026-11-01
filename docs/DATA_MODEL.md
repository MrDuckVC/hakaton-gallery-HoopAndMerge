# Модель PostgreSQL

B01 реализует `users`, `sessions`, `player_states` в `migrations/00001_users_sessions_player_states.sql`; B06 добавляет `exhibit_inspections` и `memory_events` миграцией 00002. B07 добавляет guide_generations, guide_memories и guide_scheduler миграциями 00003–00004. B09 добавляет photos миграцией 00005. `goose_db_version` — служебная история миграций. Внешние ID сериализуются строками; UUID внутри допустим. Все timestamps timestamptz. SQL параметризован. Владельца брать из сессии.

| Таблица | Основные поля / ограничения |
|---|---|
| users | id PK, username UNIQUE lowercase, display_name, avatar_color, password_hash с параметрами KDF, created_at |
| sessions | id PK, token_hash UNIQUE, user_id FK, expires_at, revoked_at nullable |
| player_states | user_id PK FK, world_version, room_id, x/y/z,yaw,pitch,updated_at; Y=0 |
| exhibit_inspections | user_id, exhibit_id composite PK; world_version,room_id,inspected_at обновляются атомарно с событием памяти |
| guide_memories | user_id,guide_id composite PK, summary, version,updated_at |
| guide_generations | id PK,user_id,request_id UUID,request_correlation_id,guide_id,exhibit_id nullable,normalized_payload_hash,status,user_text,user_message_id,user_created_at,assistant_text/message_id/created_at nullable,error_code/message/retryable nullable,created_at,started_at,finished_at,origin_connection_id |
| photos | id PK,user_id,idempotency_key UUID,request_fingerprint,world_version,room_id,movement_epoch,exhibit_id nullable,caption,published default false,content_type,original_path,thumbnail_path,image_bytes,thumbnail_bytes,width,height,created_at |
| memory_events | id PK,user_id FK,type,world_version,room_id,exhibit_id nullable,photo_id nullable,created_at; UNIQUE(user_id,exhibit_id) WHERE type=EXHIBIT_INSPECTED |

Уникальные guide_generations(user_id,request_id), photos(user_id,idempotency_key). Частичный UNIQUE(user_id) WHERE status IN ('queued','running') обеспечивает один вопрос при гонке запросов. Индексы history(user_id,guide_id,created_at,id), album(user_id,created_at,id), sessions(token_hash), queue(status,created_at,id).

CHECK generation: queued/running -> assistant/error null; completed -> assistant заполнен,error null; failed -> assistant null,error заполнен. Терминальный переход только UPDATE ... WHERE status IN ('queued','running') RETURNING; победитель единственный. Failed user_message остаётся в истории. Полный Generation строить из записи, message role задаётся сервером. queued accepted формируется до старта worker; worker не может опередить постановку accepted в outgoing queue.

guide.ask: поиск существующего key -> сравнение fingerprint нормализованных text/guide_id/exhibit_id -> accepted(current) либо conflict; для нового проверить game/limits/queue -> транзакция insert+резерв очереди -> accepted -> worker. Ограничение 8 queued проверять с общей блокировкой scheduler/транзакции, а не COUNT без сериализации. Завершение: assistant+status+memory в одной транзакции. При потере DB не посылать success.

Photo fingerprint: SHA-256 проверенного исходного файла + канонически закодированные нормализованные поля, включая room_id/movement_epoch/exhibit_id/caption. Конкурентные повторы сериализуются advisory lock/резервацией по user/key. Повтор совпадающего ключа проверять до текущего room/ready, но после сессии, лимита и структурной проверки запроса. Вернуть существующее Photo200 даже после перехода.

Файловая система и SQL не имеют общей транзакции: decode+thumbnail -> staging -> atomic rename каталога пары -> SQL commit -> доступ. При подтверждённом rollback удалить только собственные staged/final файлы. При неопределённом COMMIT сначала перечитать ключ под новой блокировкой; пока исход не установлен, файлы сохраняются. Массового/периодического cleanup в B09 нет. Фото становится видимо только после полного commit. memory_event PHOTO_CREATED создаётся в той же SQL транзакции. published=true фиксируется перед chat broadcast.

Positions: save раз в5s и disconnect; door transition сохранить до ACK. cursor history по (created_at,id), привязан к user+guide; album аналогично user. Подписанный opaque cursor либо строгая server-side lookup/валидация, не доверять переданному user_id. Мир файловый, FK на room/guide заменяется startup/service validation. Миграции append-only, down с потерей данных только по прямому запросу.


## Начальная схема B01

Ключи users/sessions — UUID без расширений PostgreSQL, выдаются приложением. Username хранится lowercase с UNIQUE и CHECK `[a-z0-9_]{3,32}`; display_name 1–32 кодовые точки, avatar_color `#RRGGBB`. `password_hash` — непрозрачная непустая строка encoded KDF с параметрами/солью; B02 хранит `$pbkdf2-sha256$600000$<salt-base64>$<key-base64>`: HMAC-SHA-256, случайная соль 16 байт, ключ 32 байта. Token hash — 32-байтный SHA-256 в bytea с UNIQUE; plaintext-токены не хранятся. sessions дополнительно содержит created_at, expires_at > created_at, индексы user_id и expires_at; revoked_at nullable. FK не удаляют данные каскадно.

Player state хранит world_version/room_id, конечные x/z, y=0, yaw в [-pi,pi), pitch в [-pi/2,pi/2], updated_at. Поля epoch/ack_seq относятся к игровому соединению и пока не добавляются в постоянную схему. Команда `bin/migrate up` применяет SQL транзакционно, повторный запуск сохраняет данные; down не предоставляется. Backend читает точный набор применённых версий, включая начальную запись goose 0, и не инициализирует схему. Команды и отдельная тестовая БД — DEVELOPMENT.md.

## Операции B02

Существующая миграция B01 покрывает B02 и остаётся неизменной. Новые UUID v4 и cookie-токены генерирует приложение. Токен — 32 случайных байта в base64url без padding, lookup — SHA-256 от cookie-строки. Срок и время создания сессии задаются одной парой UTC timestamps с разницей 24 часа, чтение не продлевает срок.

Создание аккаунта+player_state+session и замена прежней cookie-сессии атомарны. Отзыв обновляет только строку с совпавшим token_hash, `revoked_at IS NULL` и `expires_at > now`; остальные сессии пользователя не меняются. Повторный logout безопасен. Ошибка UNIQUE username преобразуется в USERNAME_TAKEN, другие driver errors не раскрываются через API.

Отсутствующий player_state восстанавливается через INSERT ON CONFLICT DO NOTHING. Запись исправления выполняется в транзакции с разделяемой блокировкой строки действующей сессии; отзыв сериализуется с этой записью, срок повторно проверяется перед commit. Исправление существующей позиции сравнивает прочитанный `updated_at` и обновляет его через `GREATEST(clock_timestamp(), updated_at + interval '1 microsecond')`; конфликт требует перечитывания. Записи позиции B05 изменяют ревизию при каждом сохранении, чтобы `/me` не затёр более новое состояние.

## Сохранение позиции B05

GameStore использует существующую player_states без новой миграции. SaveState выполняет CAS по updated_at в явной транзакции и возвращает новую монотонную ревизию. Для перехода разделяемая блокировка сессии и повторная проверка срока перед commit исключают изменение отозванной сессией. Обычный/final flush сохраняет уже авторизованную серверную позу; отзыв сессии не мешает сохранить последнюю принятую позицию. Конкурентные записи одного пользователя сериализует game usecase. CAS-конфликт не считается успехом и не перезаписывает более новую строку; ошибка периодической записи закрывает соответствующий WS с 1011. Ошибка записи перехода также закрывает WS: потеря ответа COMMIT не доказывает rollback, поэтому играть в прежнем зале до восстановления нельзя. Следующий snapshot использует перечитанную SQL-позу.

Epoch/ack_seq не сохраняются в SQL: начальные значения нового игрового соединения всегда 1/0. Исправление недопустимой восстановленной позиции остаётся транзакционным сценарием auth.Me до допуска WS.

## Осмотр B06

Миграция 00002 добавляет exhibit_inspections и memory_events. Осмотр и событие EXHIBIT_INSPECTED сохраняются одной транзакцией с проверкой действующей сессии. Повтор сохраняет ID события, обновляет время и контекст обеих записей; уникальность по (user_id,exhibit_id) действует между версиями мира. ID экспоната нельзя переиспользовать для другого по смыслу экспоната. Чтение памяти всегда ограничено user_id, сортируется по (created_at,id) DESC и имеет ограниченный limit. Индекс memory_events(user_id,created_at DESC,id DESC) обслуживает личную память. B09 добавляет FK photo_id → photos.id и единственное событие PHOTO_CREATED на фото. История общего чата не сохраняется.

## Очередь и история B07

Строка guide_scheduler(id=1) блокируется FOR UPDATE в операциях admission, claim, finish и recovery. Поэтому проверка количества queued и вставка атомарны с изменением running; индекс guide_one_running дополнительно запрещает два running. Транзакции не охватывают модель или сеть. Канальный gate usecase удерживает порядок create → enqueue accepted → claim без mutex на I/O.

request_id хранится как UUID для нечувствительного к регистру ключа; request_correlation_id сохраняет исходное написание и CHECK проверяет их равенство как UUID. Fingerprint — SHA-256 от полей guide_id, trim текста, exhibit_id с длинами полей; отсутствие exhibit_id нормализуется в пустое внутреннее значение и null на wire.

Finish обновляет только pending, одновременно записывая completed-ответ и guide_memories. Проигравшая терминальная операция читает уже сохранённый результат. B08 сохраняет детерминированные выдержки последних четырёх completed-пар, до 4000 кодовых точек; версия увеличивается только при успешном завершении. Failed/partial память не меняют. Выдержки вычисляет чистая функция usecase из completed-записей, прочитанных в той же транзакции. ReadContext читает последние четыре completed-пары и память по user_id+guide_id, а также 20 личных событий по user_id в read-only repeatable-read транзакции. Схема БД не меняется. Пределы и совместимость памяти B07 — [GUIDES.md](GUIDES.md).

History выбирает limit+1 последних записей по (created_at,id), возвращая страницу в обратном порядке. Opaque before — base64url UUID её первой записи; граница читается из БД с проверкой user_id и guide_id. Неизвестный, чужой или неверно кодированный cursor даёт INVALID_CURSOR. Записи не удаляются при reconnect/restart.


## Фотографии B09

photos.id — UUID v4 в каноническом text, совместимом с memory_events.photo_id; user_id и idempotency_key — uuid. Проверки user/key сериализуются session advisory lock на выделенном соединении. Первичный lookup освобождает соединение до ожидания игрового gate; финализация захватывает gate → соединение → key lock. Во время кодеков и файловых операций соединение не удерживается, SQL-транзакция не открыта. Уникальность дополнительно обеспечена UNIQUE(user_id,idempotency_key). Закрытое соединение не возвращается в пул с удержанной блокировкой.

Фото и PHOTO_CREATED вставляются одной короткой транзакцией; сессия проверяется под FOR SHARE и повторно перед COMMIT. original_path=id/image, thumbnail_path=id/thumbnail.jpg; MIME оригинала хранится отдельно. Обе записи остаются приватными (published=false); B10 подключает раздачу, альбом и публикацию. Файлы без метаданных не являются доступными объектами.

## B10: альбом и доступ

Альбом использует существующий photos_album_idx и порядок (created_at,id) DESC. Курсор — канонический base64url без padding от ID последнего элемента страницы; граница перечитывается только с user_id текущей сессии. Неверное кодирование, неизвестный или чужой ID дают INVALID_CURSOR. SQL выбирает limit+1 строк строго старше границы; вставка нового фото не сдвигает следующие страницы. В конце next_cursor=null.

Публикация блокирует строку фото FOR UPDATE в короткой транзакции, вызывает проверку владельца из usecase, устанавливает published=true и возвращает метаданные только после COMMIT. Действующая сессия проверяется под блокировкой до операции и повторно перед COMMIT. Повтор уже опубликованного фото успешен; новая память и история чата не создаются. Ошибка COMMIT не разрешает broadcast, даже если итог транзакции неопределён; повтор безопасен. Обратного сценария публикации нет. Схема B09 остаётся без изменений.

## Планируемая Sunday-схема (миграция ещё не создана/не применена)

Нормативная семантика — [api/sunday.md](api/sunday.md). Добавить forward-only миграцию
после 00005: membership администратора по user_id; room overlays с ключом
(world_version,room_id), revision/config/lamp и приватным holder connection ID;
guide overrides с ключом (world_version,guide_id), revision/settings. Обновления CAS,
profile+revision затронутых комнат атомарны. FK пользователей для membership/holder;
валидность room/guide/material IDs проверяет usecase по загруженному миру.
NPC/FPS/ping не сохраняются в SQL. При startup убрать stale holders до listener.
Это описание будущей модели, не утверждение о наличии таблиц.
