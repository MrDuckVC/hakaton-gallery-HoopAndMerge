# WS v1 — указатель реализации

Нормативный источник: contract.md, разделы 4–7 и 9. Машинные схемы: ws-client.schema.json и ws-server.schema.json. Примеры: examples/. Старые события не поддерживаются.

Соединение: authenticate + Origin -> upgrade -> согласованный world.snapshot -> загрузка текущего зала -> world.ready -> подтверждение world.ready -> движение/взаимодействия. На аккаунт один socket. Replacement close 4009; другим left затем joined. Connection ID защищает новое соединение от поздних действий старого.

Переход: world.enter(door_id,epoch) -> проверить дистанцию/LOS -> отменить вопрос -> транзакция позиции и epoch -> world.entered(self,current_room) -> выгрузить старый зал -> загрузить новый -> world.ready. Обычное движение не меняет room_id. world.entered обязательно опережает state с новой позицией.

События клиента: world.ready, world.enter, player.move, chat.send, exhibit.inspect, guide.ask, guide.cancel.
События сервера: world.snapshot, world.ready, world.entered, player.joined, player.left, players.state, chat.message, exhibit.inspected, guide.accepted, guide.started, guide.delta, guide.completed, guide.failed, guide.cancelled, error.

Полный Player в snapshot/joined/state включает display_name,avatar_color,movement_epoch,ack_seq. State полный, 10 Гц. До ready любые игровые команды кроме chat/cancel отклоняются; HTTP-истории доступны без WS. Пропущенные движения/чат не воспроизводятся.

Guide: accepted содержит Generation; затем started, delta*, completed/failed. Статусы queued/running/completed/failed. Cancellation — failed с CANCELLED, затем cancelled-ответ на команду отмены. Терминальные записи неизменяемы. После переподключения использовать generations и by-request, не повторять ask автоматически. Повтор ask того же UUID и payload возвращает accepted с текущей Generation; проверка DB-ключа раньше ready/дистанции/очереди.

Единственный writer, исходящая очередь <=1 MiB, сообщение <=256 KiB. Переполнение close 1013. Входящий message <=16 KiB, binary close 1003. Ping 20s/pong 10s. Полная таблица close codes, retry и limits — в contract.md; не использовать старую таблицу из переписки.
