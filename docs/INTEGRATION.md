# Перенос в backend-репозиторий

Цель: https://github.com/MrDuckVC/hakaton-gallery-HoopAndMerge. Комплект подготовлен отдельно от checkout: GitHub-коннектор вернул404. Существующие AGENTS.md, Makefile, Compose и исходники не проверены и не изменены. Публикации/коммитов в GitHub не было.

1. Открыть отдельный backend checkout, прочитать действующий AGENTS.md и git status.
2. Сопоставить новый AGENTS.md с существующим: сохранить действующие применимые правила; объединить инструкции без автоматической перезаписи. Не превысить6000 Unicode-символов.
3. Перенести docs/ и tools/build-contracts.mjs, tools/validate-contracts.mjs, проверить конфликты имён. README.md — базовая заготовка для объединения с текущим README.
4. Сверить фактические Go/module path, зависимости, entrypoint, env/Compose и Makefile. Заменить пометки «предлагаемое» проверенными командами. Не копировать frontend в backend.
5. Проверить node tools/build-contracts.mjs и node tools/validate-contracts.mjs. Node — утилита документов; runtime не зависит от Node.
6. Реализовывать по IMPLEMENTATION_PLAN, не создавать legacy aliases. contract.md — пользовательская редакция; изменения семантики сначала согласовать в контракте и синхронизировать с frontend-командой.

Не запускать tools/write-docs.mjs поверх изменённых Markdown: это первоначальный scaffold. Архив не содержит Go реализации, миграций или world assets. Проверка схем не означает проверки игрового сервера.

## B03 → B04

Каталог внедряется в auth.World и предоставляет `Package(roomID)` и `Spawn(doorID)`. B04 использует общий wire package `internal/adapters/inbound/worldwire` для RoomPackage и snapshot, чтобы соответствовать startup size bound. Порт `usecase/world.Ownership` проверяет sessionID+userID активного владельца и атомарно возвращает текущий подтверждённый room, в том числе до ready. Заменить NoConnections в composition root после подключения игры. Поздний cleanup старого connection ID не должен менять этот снимок. До B04 manifest/register/me работают с каталогом, доступ к пакетам не открывается одним login.
