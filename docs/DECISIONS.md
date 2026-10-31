# Архитектурные решения

| Решение | Основание | Последствие |
|---|---|---|
| Только Clean Architecture | Прямое требование команды | domain <- usecase <- adapters; DI в cmd/server; проверки импортов; без обхода слоёв для MVP |
| Отдельный backend repository | Требование команды | Frontend исходники/пакеты не добавлять |
| Один загруженный зал, двери | Бюджет10%, простота | ready+epoch, без prefetch и физических переходов |
| Cookie session + one origin | Браузеры в LAN | Exact Origin, server expiry, один WS/account |
| PostgreSQL | Выбранный командой стек | Unique idempotency, транзакции, durable queue |
| Один Ollama worker | Laptop VRAM | FIFO8, явные timeout/queue failures |
| Generation как история | Восстановление после потери события | /generations и by-request, не /messages |
| Photo original+thumbnail files | Размер и скорость | SQL/FS компенсации, приватные handlers |
| Wire schemas из генератора | Согласованность двух repo | Проверять Markdown,schemas,fixtures вместе |

Для новых решений добавлять причину и последствия, не временные баги. Действующая семантика v1 закреплена в api/contract.md; изменения обязательных полей/событий требуют новой версии, не скрытых aliases.
