# WS v1 — указатель реализации

Нормативный источник: contract.md, разделы 4–7 и 9. Машинные схемы: ws-client.schema.json и ws-server.schema.json. Примеры: examples/. Старые события не поддерживаются.

Соединение: authenticate + Origin -> upgrade -> согласованный world.snapshot -> загрузка текущего зала -> world.ready -> подтверждение world.ready -> движение/взаимодействия. На аккаунт один socket. Replacement close 4009; другим left затем joined. Connection ID защищает новое соединение от поздних действий старого.

Переход: world.enter(door_id,epoch) -> проверить дистанцию/LOS -> отменить вопрос -> транзакция позиции и epoch -> world.entered(self,current_room) -> выгрузить старый зал -> загрузить новый -> world.ready. Обычное движение не меняет room_id. world.entered обязательно опережает state с новой позицией.

События клиента: world.ready, world.enter, player.move, chat.send, exhibit.inspect, guide.ask, guide.cancel.
События сервера: world.snapshot, world.ready, world.entered, player.joined, player.left, players.state, chat.message, exhibit.inspected, guide.accepted, guide.started, guide.delta, guide.completed, guide.failed, guide.cancelled, error.

Полный Player в snapshot/joined/state включает display_name,avatar_color,movement_epoch,ack_seq. State полный, 10 Гц. До ready любые игровые команды кроме chat/cancel отклоняются; HTTP-истории доступны без WS. Пропущенные движения/чат не воспроизводятся.

Guide: accepted содержит Generation; затем started, delta*, completed/failed. Статусы queued/running/completed/failed. Cancellation — failed с CANCELLED, затем cancelled-ответ на команду отмены. Терминальные записи неизменяемы. После переподключения использовать generations и by-request, не повторять ask автоматически. Повтор ask того же UUID и payload возвращает accepted с текущей Generation; проверка DB-ключа раньше ready/дистанции/очереди.

Единственный writer, исходящая очередь <=1 MiB, сообщение <=256 KiB. Переполнение close 1013. Входящий message <=16 KiB, binary close 1003. Ping 20s/pong 10s. Полная таблица close codes, retry и limits — в contract.md; не использовать старую таблицу из переписки.

## Реализовано в B04

Рабочая последовательность: login → `/api/v1/ws` → настоящий world.snapshot из B02/B03 → HTTP-пакет текущего зала. Активное владение проверяет user_id и session ID; оно не возникает от одного login. Готовность/движение/переходы/осмотр ожидают B05 и возвращают GAME_NOT_READY; чат и гиды до подключения обработчиков возвращают SERVICE_UNAVAILABLE. players.state пока не публикуется.

Транспорт отклоняет неизвестные/лишние/отсутствующие поля и null, проверяет UUID v4 и точные безопасные целые без округления float64. Читаемые UUID распознанных обычных команд учитываются в пределах соединения, включая отклонённые по payload и rate limit; UUID сравниваются без учёта регистра, исходный request_id сохраняется в ответе. guide.ask исключён из обычной дедупликации для будущей устойчивой идемпотентности.

Общий лимит 30/10 с учитывает также повторы и некорректные сообщения; при исчерпании окна ответ RATE_LIMITED имеет приоритет над ошибкой разбора/дедупликации. Дополнительные chat/new ask/move окна и серия нарушений move привязаны к user_id и переживают reconnect. Данные окон без активности удаляются после 60 секунд, когда все окна уже освобождены.

Некорректный upgrade (Connection/Upgrade, Sec-WebSocket-Version/Key) возвращает HTTP 400 VALIDATION_ERROR в общем JSON-формате с request_id/no-store, сохраняя необходимые Upgrade и Sec-WebSocket-Version headers. Валидацию handshake выполняет coder/websocket; адаптер нормализует его HTTP-ошибки до upgrade.
