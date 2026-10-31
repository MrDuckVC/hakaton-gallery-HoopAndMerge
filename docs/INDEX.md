# Карта документации

contract.md в docs/api — точная редакция API пользователя, нормативный источник. conventions.md и websocket.md — навигация и уточнения, не альтернативный контракт. OpenAPI и WS JSON Schema описывают структуру; условия состояний, порядок событий, видимость и лимиты дополнительно нормативны в contract.md.

Порядок чтения: PROJECT_CONTEXT -> api/contract -> ARCHITECTURE -> DATA_MODEL -> DEVELOPMENT -> IMPLEMENTATION_PLAN -> TESTING -> DECISIONS. AGENTS.md — короткие инструкции, без дублирования этих файлов.

При изменении модели данных сверять SQL и DATA_MODEL. При изменении wire сверять contract/OpenAPI/WS schemas/examples. При изменении команды — README/AGENTS/DEVELOPMENT/Makefile. Источник генерации машинных файлов tools/build-contracts.mjs; после редактирования перегенерировать и валидировать. tools/write-docs.mjs — первоначальный scaffold, не запускать поверх отредактированных документов.
