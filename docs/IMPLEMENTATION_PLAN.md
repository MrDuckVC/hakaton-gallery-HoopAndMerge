# Реализация: 2 дня × 8 часов backend

Каждый этап реализуется только по Clean Architecture из ARCHITECTURE.md: сначала domain и usecase с портами, затем адаптеры и DI. Проверка графа импортов обязательна; бизнес-логику не переносить в handlers/SQL ради сроков.

День1: 0–1ч сверить repo/команды/контракты, config, миграции пользователей/сессий/position, запустить модель и измерить. 1–2ч register/login/me/logout, Origin, session checks. 2–3ч WS snapshot, ownership, presence, full state/chat, bounded writer. 3–4ч world package, ready, epoch, collision, doors и persistence transitions. 4–5ч registry knowledge/profiles, generations unique keys/history, queue. 5–6ч Ollama stream, completion/cancel/recovery, inspect память. 6–7ч интеграция frontend по fixtures, сохранение5s, rate limits. 7–8ч offline demo и исправления.

День2: 0–2ч photo multipart/idempotency/file consistency, thumbnail и access. 2–3ч album/chat publishing/фото события памяти. 3–4ч качество retrieval/memory, лимиты context/output. 4–6ч race/integration/security/slow client/collision tests. 6–7ч restart/потери ответов/upload race/контент бюджет. 7–8ч сборка, обновление docs, полный offline rehearsal.

Первый день демонстрирует обязательные пункты задания, фотография может оставаться скачиванием на клиенте. API фото реализуется во второй день без изменения контракта. При нехватке времени сокращать количество тщательно оформленных залов/декор, но не лимит кеша, sessions, память или локальность ИИ. Нельзя увеличивать знаменатель пустыми файлами.

Definition of done на изменение: wire/schema/examples согласованы; видимые правила покрыты тестами; изменённая конфигурация документирована; build/vet/tests пройдены или указана конкретная причина ограничения; offline поведение проверено. На финал — пункты10 contract.md, не только happy path.
