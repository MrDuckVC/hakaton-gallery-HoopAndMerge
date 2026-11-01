# S07 — интеграция Sunday и автономный комплект

Дата: 01.11.2026. Задача [#43](https://github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/issues/43),
дополнение фактической приёмки №20/21. Backend base `4f39306`; frontend
`ManInTheCoat/Hope-Merge-Frontend main@c7dbfdc62057fabf06d50bce2a0c3e8e0f386150`.
Go 1.27.1, Bun 1.4.4, Node 24.19.0, golangci-lint 2.13.2,
PostgreSQL 18.6-bookworm, Ollama 0.40.13, WSL/RTX 4070 Laptop.

## Изменения и наблюдаемые правила

S01–S06 уже были подключены в cmd/server. S07 объявляет `extensions:["sunday_v1"]`
в manifest, сохраняя базовые поля и protocol_version=1. HTTP/WS wire-форма
расширения не меняется. Нормативный максимум **10 NPC на зал** по уточнению владельца
#38; старые 40 в #36/#43 не возвращаются. Дополнительно по поручению пользователя исправлены рывки ходьбы и heartbeat CI.
Новых миграций, env-настроек production,
ассетов backend и зависимостей нет. Миграции 00006–00008 применяются оператором
отдельно до запуска; admin grant работает только для существующего аккаунта.

Новый unit-тест capability сначала упал на отсутствующем extensions, затем прошёл
после изменения HTTP DTO. Сквозной TestSundayCompositionIntegration проверяет
настоящий composition root и две cookie/WS-сессии: admin 401/403/grant, manifest,
snapshot → runtime → NPC, room PUT/CAS, GAME_NOT_READY, chat/metrics до ready,
10 NPC, take/OBJECT_BUSY/place/aim/power, guide.action, профиль,
probe/pong/admin clients, restart с held lamp, сохранение настроек и room.sync.
Каждый запуск создаёт собственную SQL-схему и удаляет только её.

Первый отдельный прогон использовал полный C01 и production dist — PASS.
Первый полный race-прогон выявил превышение 60-секундного startup deadline
при декодировании большого C01 под инструментированием (не data race).
CI-тест переведён на валидный синтетический мир с гидом; production deadline
не менялся. Для отдельной проверки C01 предусмотрен SUNDAY_TEST_WORLD_DIR;
SUNDAY_TEST_FRONTEND_DIST включает реальный dist вместо временной HTML-страницы.

## Автоматические проверки

Все SQL-прогоны использовали отдельную gallery_test в специально созданной
PostgreSQL на 127.0.0.1:55443, без рабочих volumes. Рабочая БД не использовалась.

| Команда / сценарий | Результат |
|---|---|
| `go build ./...`, `make build`, `go vet ./...` | PASS; gallery/migrate/admin |
| `go test ./...` | PASS с SQL/HTTP/WS интеграцией |
| `go test -race ./...` | PASS после исправления зависимости CI-теста от C01; data races не обнаружены |
| `make lint` | PASS, 0 issues; включая depguard/архитектурные ограничения |
| `make coverage` | PASS после всех правок: **87,31%**, race detector, порог ≥60% |
| `node tools/build-contracts.mjs`, `node tools/validate-contracts.mjs` | PASS; references/fixtures/лимиты/согласованность wire |
| `bun install --frozen-lockfile`, `bun test`, `VITE_USE_MOCK=false bun run build` в отдельном frontend checkout | PASS: 497 tests, 0 fail; tsc и Vite build |
| `tsc --noEmit --strict --target ES2020 --moduleResolution bundler --module ESNext` на временных type assertions frontend/backend | PASS: runtime/profile в обе стороны, clients/NPC к клиенту, object/action/metrics к серверу |
| `python3 -B -m unittest discover -s tools -p 'test_*.py'` | PASS: 14 tests; включая executable admin, hash, подмену/удаление и отказ перезаписи |
| `sh -n tools/build-frontend.sh`; повтор сборки в существующий dist | PASS; повтор отклонён до checkout/install |
| `make world-check` | PASS: C01, 24 зала, 317806538 bytes; лимит кеша 31780653, резерв фото 3145728 |
| `make docker-build`; `sh tools/smoke-docker.sh gallery-backend:local` | PASS: отдельный Docker bridge/test DB, миграции 1–8, frontend/API, SIGTERM exit 0 |
| `offline-bundle.py prepare/verify`; повтор prepare в существующий output | PASS; 297 артефактов, существующий комплект не перезаписан |

Frontend build предупреждает о chunk >500 kB; сборка успешна. Bun/tsc работают
только при подготовке frontend; runtime не запускает build/install. Сам frontend
source остаётся вне backend. Проверка TS дополнительно подтвердила совместимость
типов; она не заменяет проверку семантики состояний в Go-тестах.

Полная существующая матрица №2–14/S01–S06 выполняется вместе с новыми тестами:
ownership/session/epoch, переходы entered до runtime/NPC, profile snapshot,
идемпотентность очереди/фото, reconnect/rates и slow consumer/backpressure.
Это автоматические сценарии, не общий браузерный показ Sunday §8.

## Реальный холодный запуск без внешней сети

В отдельном `unshare --user --map-root-user --net` включён только loopback;
таблица IPv4-маршрутов пуста. PostgreSQL запущен отдельно с `--network none`,
соединение через Unix socket. Это проверочная топология; штатный показ сохраняет
host backend+Ollama и Compose PostgreSQL по [DEMO.md](DEMO.md).

Из нового комплекта `data/offline/s07`, без build/pull/create/download, выполнены:

- `bin/migrate up/status`, startup настоящего gallery с C01 и production frontend;
- manifest с sunday_v1, WS snapshot/runtime/10 NPC/ready, загрузка описания зала;
- загрузка синтетического JPEG через настоящий photo API;
- настоящий guide.completed основной модели через backend queue/Ollama;
- `bin/admin grant --username s07offline` для созданного тестового аккаунта;
- штатный restart backend, затем login, admin/me, альбом/оригинал фото и completed-история.

Ollama из комплекта показал основную модель **100% GPU**, context=8192.
Это smoke основной модели, не повтор полной матрицы A01; резервный tag включён
и проверен по hashes, но отдельная генерация резервной модели в S07 не выполнялась.
Первый запуск тестовой PostgreSQL потребовал исправления прав только временного
socket-каталога и создания тестовой БД после прерванной инициализации.
Рабочие аккаунты/фото/память, сеть ноутбука и существующий Ollama daemon не менялись.
Приватные ответы, prompts и cookie в отчёт не включены.

## Комплект и ограничения приёмки

Frontend закреплён в tools/build-frontend.sh; сборка помещена в `data/frontend-s07`.
Промежуточный комплект `data/offline/s07` проверен и использован в cold-start smoke.
Финальный комплект с актуальной документацией — `data/offline/s07-final`;
его hashes проверяются до runtime-записей в каталог.
Оба каталога исключены из git. Рабочие .env/БД/фотографии в комплект не копируются;
для существующей БД сохраняются прежние PHOTO_DIR и Compose project/volume.
Административный binary действительно упакован и использован в cold-start smoke.

**Не проведены:** совместный показ в двух настоящих браузерах/на двух ноутбуках,
физический LAN без WAN и localhost с физически выключенным Wi-Fi, проверка
Windows firewall/роутера, визуальных анимаций, реального rAF FPS/ping под сценой,
внешних запросов через DevTools и фактического клиентского бюджета 10% на переходах
с фото. Серверная проверка C01/резерва и автоматические frontend/cache-тесты
не подтверждают эти ручные пункты. Рестарт PostgreSQL/Ollama и резервная модель
в этой итерации отдельно не проверялись; старые I01-результаты не выдаются за S07.

По решению пользователя PR в issue-1 создаётся с этими ограничениями. Полная
физическая приёмка №20/21 остаётся незавершённой; namespace не считается показом.
Ручной маршрут и команды — [DEMO.md](DEMO.md), [DEVELOPMENT.md](DEVELOPMENT.md).

## Дополнительные исправления по поручению пользователя

Строгая проверка 4*dt отклоняла шаг 0,36 м после сжатого интервала доставки
60 мс. Детерминированный тест сначала воспроизвёл отказ второго шага, затем
прошёл с накоплением неиспользованного расстояния (4 м/с, cap1 м). Начальный запас
**0 м**, reset epoch/replacement также 0: повторные соединения не создают бонус.
Тест recycling сначала воспроизвёл 48 м за 10 с с ошибочным стартовым credit,
после его удаления проходит. Стены проверяются sweep, постоянные 5 м/с запрещены.
Wire-поля не меняются; нормативная семантика contract.md обновлена до кода.

На настоящем gallery/C01 в отдельной gallery_s07_jitter проведены 200 отправок
по 0,36 м каждые ~100 мс: 200 observed ack states, **0 откатов**. Повторный прогон после удаления
стартового credit также дал 200/200 подтверждений без откатов.
Это сетевой replay, не субъективная проверка камеры/анимации браузера.

В [CI job124844311412](https://github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/actions/runs/41305284103/job/124844311412)
missing-pong тест упал на ожидании удаления peer. В coder/websocket CloseNow
может присоединиться к уже начатому graceful Close и ждать handshake. Новый
управляемый тест сначала воспроизвёл зависание; после underlying net.Conn abort
проходит. Исходный heartbeat + regression: 50 повторов с race/coverpkg — PASS.
Производственные ping/pong интервалы и секундный предел старого теста не увеличены.

## Исправление первого CI после публикации

Оба запуска618799b прошли heartbeat, но cmd/server достиг общего test timeout600с
в JPEG Decode (TestPrepareWorld). Локальные87,31% выше не означали зелёный GitHub CI.
Оптимизирован только синтетический fixture: grayscale896px/quality100, настоящий
JPEG и прежние правила бюджета; C01 и production validator не изменены. Дублирующий
полный decode ради wrong-version удалён из composition smoke; отрицательный кейс
сохранён в usecase/world. По следующему поручению владельца — точечные тесты с
race/coverage и lint, затем отдельный коммит и немедленный push; без полного
повторного локального прогона. Старый offline-kit содержит прежние документы;
production-бинарники этим test-only исправлением не меняются.

Точечная проверка: `go test -race -covermode=atomic -coverpkg=./... ./cmd/server
./internal/testworld ./internal/usecase/world -run
'^(TestPrepareWorld|TestSundayCompositionIntegration|TestFixtureBoundsDecodedPixels|TestLoadFailures)$'
-count=1` — PASS: cmd/server67,323с, fixture10,333с, usecase/world1,183с.
`make lint` — PASS, 0 issues; `git diff --check` — PASS.
