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

Рабочая последовательность: login → `/api/v1/ws` → настоящий world.snapshot из B02/B03 → HTTP-пакет текущего зала. Активное владение проверяет user_id и session ID; оно не возникает от одного login. B05 реализует готовность, движение, переходы и полный players.state 10 Гц. До ready move/enter/inspect/ask возвращают GAME_NOT_READY; чат B06 доступен без ready; B07 подключает отмену и устойчивые вопросы гидов.

Транспорт отклоняет неизвестные/лишние/отсутствующие поля и null, проверяет UUID v4 и точные безопасные целые без округления float64. Читаемые UUID распознанных обычных команд учитываются в пределах соединения, включая отклонённые по payload и rate limit; UUID сравниваются без учёта регистра, исходный request_id сохраняется в ответе. guide.ask исключён из обычной дедупликации для будущей устойчивой идемпотентности.

Общий лимит 30/10 с учитывает также повторы и некорректные сообщения; при исчерпании окна ответ RATE_LIMITED имеет приоритет над ошибкой разбора/дедупликации. Дополнительные chat/new ask/move окна и серия нарушений move привязаны к user_id и переживают reconnect. Данные окон без активности удаляются после 60 секунд, когда все окна уже освобождены.

Исключение S03: client.metrics и telemetry.pong без request_id доступны до ready,
не входят в 30/10 с и 10000 UUID. У каждого отдельное скользящее окно 2/с/connection;
лишние валидные сообщения молча отбрасываются. Неверный payload распознанной telemetry
даёт VALIDATION_ERROR, не меняя измерений и обычных лимитов. Неоднозначный/неразбираемый
JSON обрабатывается как прежде. Сервер отправляет telemetry.probe только текущему
socket через общий writer, начиная через 5 с после snapshot/runtime; nonce погашается
одним ответом. Это не заменяет protocol ping 20 с / pong timeout 10 с.
FPS/ping доступны только GET /api/v1/admin/clients, не players.state или broadcast.

Некорректный upgrade (Connection/Upgrade, Sec-WebSocket-Version/Key) возвращает HTTP 400 VALIDATION_ERROR в общем JSON-формате с request_id/no-store, сохраняя необходимые Upgrade и Sec-WebSocket-Version headers. Валидацию handshake выполняет coder/websocket; адаптер нормализует его HTTP-ошибки до upgrade.

## Реализовано в B05

Готовность проверяет версию мира и epoch; повтор с новым UUID не сбрасывает серверное время движения. Move с устаревшим epoch/seq игнорируется, будущий epoch возвращает VALIDATION_ERROR. Структурно корректный move обновляет ack и время; запас расстояния пополняется 4 м/с до 1 м, первый ready начинает с 0 м, только принятое расстояние расходует запас; при превышении скорости или коллизии сохраняет прежнюю позицию, принимая допустимый поворот. Проверяется весь путь цилиндра, включая углы препятствий; касание пола разрешено.

Переход проверяет дверь текущего зала, XZ <=1.5 м и LOS от камеры к двери. Порт отмены вопроса вызывается до транзакционного сохранения; сейчас подключён NoQuestions, поскольку генерации ещё отсутствуют. Ошибка отмены оставляет игрока в прежнем зале. Ошибка записи не публикует entered и закрывает текущий WS с 1011 (при отозванной сессии — 4001): результат COMMIT может быть неопределённым. Новое подключение перечитывает фактическую позу из БД, прежде чем разрешить игру. После commit entered поставлен в очередь до состояния нового epoch, владение HTTP переключается одновременно с позицией.

Сохранение каждые 5 секунд, при disconnect и перед закрытием WS на shutdown использует CAS-ревизию updated_at. Ошибка периодической записи закрывает соединение с 1011. Replacement получает последнюю принятую позу и пакет её зала; epoch/ack нового соединения равны 1/0. Поздние команды и flush старого connection ID пропускаются. При disconnect владение и допуск команд отзываются до ожидания финального flush; отключённый игрок сразу исключён из state и snapshot. Если очередной save-тик пришёл во время предыдущего цикла, он объединяется в одну отложенную попытку сразу после завершения цикла, без пропуска дополнительного пятесекундного интервала.

## Реализовано в B06

`chat.send` принимает текст до 500 Unicode-кодовых точек после trim либо photo_id. Пустой текст без фото отклоняется. `chat.message` получают все активные игроки независимо от зала и ready; исходный request_id есть только у отправителя. Общий чат не сохраняется и не воспроизводится после reconnect. Дополнительный лимит — 5 команд за 10 секунд по user_id.

`exhibit.inspect` требует ready, экспонат текущего зала, расстояние XZ <=3 м и LOS от камеры к центру экспоната. Отсутствующий в текущем зале ID возвращает NOT_FOUND; расстояние/стена — OUT_OF_RANGE. `exhibit.inspected` отправляется только после атомарного сохранения осмотра и события памяти. SQL-сбой возвращает SERVICE_UNAVAILABLE, отозванная сессия закрывается с 4001. Результат старого/отключённого соединения не доставляется новому.

На (user_id,exhibit_id) приходится один осмотр и одно событие EXHIBIT_INSPECTED независимо от версии мира. Повтор обновляет время, версию и зал обеих записей, сохраняя ID события. Правило идентичности exhibit_id закреплено в contract.md. Личная память читается через внутренний порт с ограничением владельца и количества; публичного endpoint пока нет.

Порт PhotoPublisher проверяет владельца и фиксирует published=true перед возвратом метаданных. B10 подключает photo.Library через outbound/photopublisher: чужое или отсутствующее фото возвращает NOT_FOUND; ошибка хранения — SERVICE_UNAVAILABLE без рассылки всего сообщения, включая текст. Повтор публикации с новым request_id разрешён и необратим. Фото без текста возвращает text="". Опубликованные оригинал и миниатюра доступны любой действующей сессии, в том числе без WS и после исчезновения сообщения из чата.

## Реализовано в B07

`guide.ask` проверяет сохранённый ключ раньше ready, дистанции и окна новых вопросов. Нормализованный payload включает guide_id, trim текста и необязательный exhibit_id. Регистр UUID не влияет на ключ; исходный request_id принятого вопроса сохраняется для started/delta/terminal, а replay accepted коррелирует с текущей командой.

Транзакционная FIFO допускает одного running и восемь queued; частичный индекс запрещает два pending на пользователя. Accepted ставится в единственную writer queue до захвата вопроса worker. Completed и память сохраняются атомарно; частичный текст существует только в worker и delta. Cancel выигрывает терминальную операцию через CAS и отправляет failed перед cancelled. Door, disconnect, replacement и отзыв сессии используют порт отмены с connection ID.

Тайм-аут queued — 120 секунд, running — 90 секунд; output limit — 8000 кодовых точек. До B08 модель явно недоступна: после accepted/started приходит failed с AI_UNAVAILABLE. При ошибке хранения результата WS закрывается 1011; восстановление переводит оставшиеся pending в SERVER_RESTARTED и уведомляет ещё подключённых владельцев. До открытия listener выполняется та же recovery-операция.


## Sunday v1: runtime, светильник и NPC

Нормативные правила: [sunday.md](sunday.md), типы [sunday.types.ts](sunday.types.ts).
Реализованы world.runtime, room.sync, object.act, object.acted, npc.state, guide.action и guide.action.completed.
До S07 capability sunday_v1 отсутствует.
Новые client events: room.sync, object.act, guide.action, client.metrics, telemetry.pong.
Новые server events: world.runtime, npc.state, object.acted, guide.action.completed, telemetry.probe.
Первым остаётся world.snapshot; runtime/NPC текущего зала идут после snapshot/entered.
Metrics/pong без request_id и вне обычного лимита команд; остальные новые команды
имеют UUID и обычную дедупликацию. Полные runtime snapshots + CAS revision, без новых
геометрических файлов/смены WORLD_VERSION при правке оформления.

S04: `object.act` принимает только union take/place/aim/power из Sunday §4. Все действия
требуют owning/ready/epoch, XZ ≤3 м и LOS, обычный UUID и дополнительный лимит
10/10 с/user, сохраняющийся при reconnect. Сначала объект/дистанция/LOS, затем
допустимость holder и revision: held aim/power/take и чужой place →OBJECT_BUSY;
при допустимом holder устаревшая revision →REVISION_CONFLICT. Ошибки не меняют revision.
После commit отправитель получает `object.acted {action,runtime}` с request_id,
остальные owning WS зала — `world.runtime` без request_id. No-op тоже повышает revision.
Release публикует обычный полный runtime; приватный connection ID не входит в DTO.

S02: смена имени/subtitle профиля атомарно повышает revisions всех залов этой
личности. После commit каждый owning WS, включая not-ready, получает полный
world.runtime с новой публичной идентичностью. Reconnect/room.sync читают SQL;
правки инструкций без смены имени/subtitle не создают runtime-событий.
Worker фиксирует профиль и revision в транзакции queued→running, поэтому текущий
ответ использует прежний снимок, следующий — новые настройки. Приватный снимок
и revision профиля не добавляются в публичные события генерации.

S05: сразу после snapshot/runtime и entered/runtime сервер передаёт полный
`npc.state {world_version,room_id,room_instance_id,seq,npcs}`. Затем owning WS,
включая not-ready, получают состояние зала 5 Гц; `room.sync` возвращает runtime,
затем NPC с тем же request_id. На остальных снимках request_id отсутствует.
Список содержит 0–10 элементов (`npc_00..npc_09`), при count=0 — `[]`; payload
не больше 24 КиБ. Состояния walking/idle/talking, speed_mps=0.6/0/0, phase∈[0,2π).
Общий снимок одной рассылки имеет одинаковый seq для всех получателей. Внутри
instance seq возрастает; после опустошения комнаты новый вход создаёт другой
instance и seq=1. Replacement при непрерывном владении сохраняет instance.
Клиент отбрасывает старые room/version/instance/seq, интерполирует 200 мс и
анимирует конечности локально. Единственный writer и очередь 1 МиБ сохраняются.


## S06: локальные действия гидов реализованы

`guide.action {guide_id,text,movement_epoch,expected_revision}` распознаёт только
девять фраз [Sunday §6](sunday.md#6-просьбы-к-гиду-guideaction). Текст после trim —
1–200 Unicode-символов без NUL; UUID обычный, лимит 3/10 с/user сохраняется при reconnect.
Owning/ready/epoch и effective guide placement с XZ ≤3 м/LOS проверяются до CAS.
Неизвестный текст возвращает UNSUPPORTED_ACTION. После commit отправитель получает
`guide.action.completed {guide_id,effect,runtime}` с request_id, остальные owning WS
зала — `world.runtime` без request_id; за ними следует обычный NPC-снимок.
Повтор show с новым UUID повышает revision без второго объекта; повтор UUID —
DUPLICATE_REQUEST. guide.ask продолжает только образовательный диалог с памятью.

S07: manifest объявляет `sunday_v1`; все перечисленные Sunday-события подключены в
`cmd/server`. Сквозные проверки и ограничения физической приёмки —
[../S07_ACCEPTANCE.md](../S07_ACCEPTANCE.md). Базовый protocol_version остаётся 1.
