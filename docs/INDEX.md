# Карта документации

contract.md в docs/api — точная редакция API пользователя, нормативный источник. conventions.md и websocket.md — навигация и уточнения, не альтернативный контракт. OpenAPI и WS JSON Schema описывают структуру; условия состояний, порядок событий, видимость и лимиты дополнительно нормативны в contract.md.

Порядок чтения: PROJECT_CONTEXT -> api/contract -> ARCHITECTURE -> DATA_MODEL -> DEVELOPMENT -> IMPLEMENTATION_PLAN -> TESTING -> DECISIONS. AGENTS.md — короткие инструкции, без дублирования этих файлов.

REQUIREMENTS.md — исходное задание и уточнение организатора от 31.10.2026, топология, два режима показа и матрица приёмки. ISSUE_BACKLOG.md — разделение работ по областям, критерии готовности и ссылки на GitHub issues.

При изменении модели данных сверять SQL и DATA_MODEL. При изменении wire сверять contract/OpenAPI/WS schemas/examples. При изменении команды — README/AGENTS/DEVELOPMENT/Makefile. Источник генерации машинных файлов tools/build-contracts.mjs; после редактирования перегенерировать и валидировать. tools/write-docs.mjs — первоначальный scaffold, не запускать поверх отредактированных документов.

- [GUIDES.md](GUIDES.md) — B08: профили, локальный Ollama, контекст, память и реальная проверка.
- [MODEL_BENCHMARK.md](MODEL_BENCHMARK.md) — A01: оборудование, измерения и статус выбора моделей.

- [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) — лицензия WebP decoder и масштабирования миниатюр B09.

content/README.md — готовая галерея C01, её маршрут, источники/права, профили и пересборка; content/budget.json — точные размеры, hashes и запасы всех пакетов; content/ACCEPTANCE.md — проведённые проверки и ограничения показа.
