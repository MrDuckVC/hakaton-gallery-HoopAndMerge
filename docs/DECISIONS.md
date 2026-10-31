# Архитектурные решения

| Решение | Основание | Последствие |
|---|---|---|
| Только Clean Architecture | Прямое требование команды | domain <- usecase <- adapters; DI в cmd/server; проверки импортов; без обхода слоёв для MVP |
| Отдельный backend repository | Требование команды | Frontend исходники/пакеты не добавлять |
| Один загруженный зал, двери | Бюджет10%, простота | ready+epoch, без prefetch и физических переходов |
| Cookie session + one origin | Браузеры в LAN | Exact Origin, server expiry, один WS/account |
| PostgreSQL | Выбранный командой стек | Unique idempotency, транзакции, durable queue |
| Один Ollama worker | Laptop VRAM | FIFO8, явные timeout/queue failures |
| Один серверный ноутбук, клиенты только браузер | Уточнение организатора 31.10.2026 11:47 | Backend/БД/ИИ/контент на RTX 4070 Laptop; frontend dist без внешних runtime-ресурсов |
| Два режима офлайн-показа | Прямое требование организатора | LAN/Wi-Fi без интернета, затем localhost на сервере с выключенным Wi-Fi |
| Выбор model tag по замерам на сервере | Уточнение пользователя о RTX 4070 Laptop | Измерить реальную VRAM и совместную нагрузку с Three.js; проверить основной и резервный варианты |
| Generation как история | Восстановление после потери события | /generations и by-request, не /messages |
| Photo original+thumbnail files | Размер и скорость | SQL/FS компенсации, приватные handlers |
| Wire schemas из генератора | Согласованность двух repo | Проверять Markdown,schemas,fixtures вместе |

Для новых решений добавлять причину и последствия, не временные баги. Действующая семантика v1 закреплена в api/contract.md; изменения обязательных полей/событий требуют новой версии, не скрытых aliases.
