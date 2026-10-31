# Перенос в backend-репозиторий

Цель: https://github.com/MrDuckVC/hakaton-gallery-HoopAndMerge. Комплект подготовлен отдельно от checkout: GitHub-коннектор вернул404. Существующие AGENTS.md, Makefile, Compose и исходники не проверены и не изменены. Публикации/коммитов в GitHub не было.

1. Открыть отдельный backend checkout, прочитать действующий AGENTS.md и git status.
2. Сопоставить новый AGENTS.md с существующим: сохранить действующие применимые правила; объединить инструкции без автоматической перезаписи. Не превысить6000 Unicode-символов.
3. Перенести docs/ и tools/build-contracts.mjs, tools/validate-contracts.mjs, проверить конфликты имён. README.md — базовая заготовка для объединения с текущим README.
4. Сверить фактические Go/module path, зависимости, entrypoint, env/Compose и Makefile. Заменить пометки «предлагаемое» проверенными командами. Не копировать frontend в backend.
5. Проверить node tools/build-contracts.mjs и node tools/validate-contracts.mjs. Node — утилита документов; runtime не зависит от Node.
6. Реализовывать по IMPLEMENTATION_PLAN, не создавать legacy aliases. contract.md — пользовательская редакция; изменения семантики сначала согласовать в контракте и синхронизировать с frontend-командой.

Не запускать tools/write-docs.mjs поверх изменённых Markdown: это первоначальный scaffold. Архив не содержит Go реализации, миграций или world assets. Проверка схем не означает проверки игрового сервера.
