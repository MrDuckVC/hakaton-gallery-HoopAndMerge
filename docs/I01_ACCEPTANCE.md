# I01: фактическая проверка инфраструктуры

Дата: 01.11.2026. WSL Ubuntu на ноутбуке с RTX 4070 Laptop, Docker Desktop
29.8.2 / Compose 5.6.0, Go 1.27.1, Bun 1.4.4, Ollama 0.40.13,
PostgreSQL 18.6-bookworm. Frontend:
`038074dd56c719ca5a9d636019e392a365d4cbb6` (`fix/day2-first-demo`).

## Автоматические проверки

| Команда / сценарий | Результат |
|---|---|
| `sh tools/build-frontend.sh ./data/frontend` | PASS: frozen lockfile, production dist, mocks выключены; Vite предупреждает о chunk >500 kB |
| `go build ./...`, `make build`, `go vet ./...`, `make lint` | PASS; lint: 0 issues |
| `go test ./...` с отдельной TEST_DATABASE_URL | PASS, включая HTTP/WS/SQL интеграции |
| `make coverage` с отдельной TEST_DATABASE_URL | PASS: race detector, 85,99% при пороге ≥60% |
| `go test -race ./...` с отдельной TEST_DATABASE_URL | PASS, включая новый same-origin тест |
| `python3 -B -m unittest discover -s tools -p 'test_*.py'` | PASS: 12 тестов, включая целостность/отказ перезаписи, manifests-v2, runtime version, абсолютный путь фото |
| `node tools/build-contracts.mjs`, `node tools/validate-contracts.mjs` | PASS; производные API не изменились |
| `make docker-build` | PASS: scratch image с непривилегированным пользователем |
| `sh tools/smoke-docker.sh gallery-backend:local` | PASS через отдельную bridge-сеть Docker Desktop: миграции, API 404, mounted frontend 200, SIGTERM exit 0 |
| `python3 tools/offline-bundle.py prepare ...`, `verify ...` | PASS: комплект около 7,1 ГиБ; проверен состав и hashes |

Для SQL использовалась созданная специально для I01 БД `gallery_test`, не рабочая
БД. Docker smoke на Linux CI использует host networking; на Docker Desktop
прогон выполнен с `SMOKE_DOCKER_NETWORK` и DATABASE_URL на тестовый bridge-контейнер.
Первая попытка host networking в Docker Desktop не подключилась к WSL loopback;
bridge-прогон прошёл. Обычные Go-тесты требуют разрешения sandbox на sockets.

## Холодный старт без внешней сети

В отдельном `unshare --user --map-root-user --net` namespace поднят только `lo`
с 127.0.0.1/::1, таблица маршрутов пуста. Внешний HTTPS-запрос не прошёл.
Сеть самого ноутбука не отключалась. PostgreSQL запущен отдельно с `--network none`;
backend соединялся с ним через локальный Unix socket. Это проверочная топология;
штатный demo использует Compose loopback-порт по DEMO.md.

Из собранного комплекта, без build/pull/create/download, проверены:

- отдельные `migrate up/status`, startup настоящего backend с миром C01 и dist;
- frontend 200, регистрация/cookie, WS snapshot/ready, manifest, room и JPEG asset;
- загрузка JPEG-фото через API и настоящий `guide.completed` основного гида;
- ответ резервной модели через локальный Ollama API после выгрузки основной;
- GPU-размещение обоих tags: main `size_vram=4001072414`, reserve `2411577999` байт,
  оба с `context_length=8192`; это smoke, не повторение полной матрицы A01;
- штатный рестарт backend и повторные миграции;
- затем рестарт тестовой PostgreSQL и новый backend в namespace: login, альбом,
  чтение сохранённого оригинала фото и completed-история остались доступны.

После рестарта PostgreSQL сохранились 1 аккаунт, 1 фото, 1 completed-генерация
и 1 непустая guide memory. Рабочая БД/фотографии не очищались и не использовались.
Приватные ответы, сессионные cookie и prompts в этот отчёт не включены.

## Непроведённые аппаратные сценарии

Физический браузерный показ на нескольких ноутбуках по Wi-Fi и показ на сервере
с **физически выключенным Wi-Fi** не проведены. Namespace smoke не подтверждает
настройки Windows firewall/роутера, отсутствие client isolation, WebGL/FPS или
внешние запросы в реальном браузере. Проверены локальная production-сборка и
доставка файлов; DevTools-проверка браузерного runtime остаётся частью показа.

Полный игровой маршрут, качество ответов и производительность при открытой сцене
относятся к Q02/A01. Реальные инструкции для обоих режимов — [DEMO.md](DEMO.md).
