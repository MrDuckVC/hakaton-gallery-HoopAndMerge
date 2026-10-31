# Предлагаемая модель PostgreSQL

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
