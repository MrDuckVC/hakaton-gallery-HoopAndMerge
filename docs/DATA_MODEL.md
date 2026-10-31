# Модель PostgreSQL

B01 реализует `users`, `sessions`, `player_states` в `migrations/00001_users_sessions_player_states.sql`; остальные таблицы ниже — проект для задач их подсистем. `goose_db_version` — служебная история миграций. Внешние ID сериализуются строками; UUID внутри допустим. Все timestamps timestamptz. SQL параметризован. Владельца брать из сессии.

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


## Начальная схема B01

Ключи users/sessions — UUID без расширений PostgreSQL, выдаются приложением. Username хранится lowercase с UNIQUE и CHECK `[a-z0-9_]{3,32}`; display_name 1–32 кодовые точки, avatar_color `#RRGGBB`. `password_hash` — непрозрачная непустая строка encoded KDF с параметрами/солью; B02 хранит `$pbkdf2-sha256$600000$<salt-base64>$<key-base64>`: HMAC-SHA-256, случайная соль 16 байт, ключ 32 байта. Token hash — 32-байтный SHA-256 в bytea с UNIQUE; plaintext-токены не хранятся. sessions дополнительно содержит created_at, expires_at > created_at, индексы user_id и expires_at; revoked_at nullable. FK не удаляют данные каскадно.

Player state хранит world_version/room_id, конечные x/z, y=0, yaw в [-pi,pi), pitch в [-pi/2,pi/2], updated_at. Поля epoch/ack_seq относятся к игровому соединению и пока не добавляются в постоянную схему. Команда `bin/migrate up` применяет SQL транзакционно, повторный запуск сохраняет данные; down не предоставляется. Backend читает точный набор применённых версий, включая начальную запись goose 0, и не инициализирует схему. Команды и отдельная тестовая БД — DEVELOPMENT.md.

## Операции B02

Существующая миграция B01 покрывает B02 и остаётся неизменной. Новые UUID v4 и cookie-токены генерирует приложение. Токен — 32 случайных байта в base64url без padding, lookup — SHA-256 от cookie-строки. Срок и время создания сессии задаются одной парой UTC timestamps с разницей 24 часа, чтение не продлевает срок.

Создание аккаунта+player_state+session и замена прежней cookie-сессии атомарны. Отзыв обновляет только строку с совпавшим token_hash, `revoked_at IS NULL` и `expires_at > now`; остальные сессии пользователя не меняются. Повторный logout безопасен. Ошибка UNIQUE username преобразуется в USERNAME_TAKEN, другие driver errors не раскрываются через API.

Отсутствующий player_state восстанавливается через INSERT ON CONFLICT DO NOTHING. Запись исправления выполняется в транзакции с разделяемой блокировкой строки действующей сессии; отзыв сериализуется с этой записью, срок повторно проверяется перед commit. Исправление существующей позиции сравнивает прочитанный `updated_at` и обновляет его через `GREATEST(clock_timestamp(), updated_at + interval '1 microsecond')`; конфликт требует перечитывания. Будущие записи позиции B05 также должны изменять ревизию при каждом сохранении, чтобы `/me` не затёр более новое состояние.
