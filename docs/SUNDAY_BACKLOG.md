# Sunday: новые backend-задачи

Согласованный объём: [api/sunday.md](api/sunday.md). S01 реализует runtime залов и admin rooms; результаты и границы — [S01_ACCEPTANCE.md](S01_ACCEPTANCE.md). S02 добавляет профили гидов; проверки — [S02_ACCEPTANCE.md](S02_ACCEPTANCE.md).
Основа №2–14 выполнена; I01 из issue-14 используется как есть, review исправлен.
Frontend берётся из main отдельного репозитория, задачи frontend по этому поручению не создаются.

Эпик с опубликованным полным контрактом и TypeScript для frontend: https://github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/issues/36

| Ключ | Задача | Зависимости | Ориентир backend |
|---|---|---|---|
| S01 | [#37 Backend. Добавить сохраняемый runtime зала и административное редактирование](https://github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/issues/37) | №2–14 | 1–1,25 ч |
| S02 | [#38 Backend. Добавить редактирование поведения гидов через admin API](https://github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/issues/38) | #37 | 0,75 ч |
| S03 | [#39 Backend. Собирать только FPS и ping подключённых клиентов для админки](https://github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/issues/39) | #37 | 0,5 ч |
| S04 | [#40 Backend. Реализовать перенос и управление составным светильником](https://github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/issues/40) | #37 | 1 ч |
| S05 | [#41 Backend. Добавить массовых NPC-посетителей с реакциями на игроков](https://github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/issues/41) | #37 | 0,75 ч |
| S06 | [#42 Backend. Научить гидов менять экспозицию по локальному классификатору](https://github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/issues/42) | #37 | 0,5 ч |
| S07 | [#43 Backend. Интегрировать Sunday-контракт и обновить автономный комплект](https://github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/issues/43) | #37, #38, #39, #40, #41, #42 | 0,75 ч + общий показ |

Порядок: S01 → S04 → S03 → S02 → S05 → S06 → S07. После S01 остальные
S02–S06 независимы по интерфейсам, но на одном backend-разработчике выполняются последовательно.
Frontend работает сразу по TS/fixtures; S07 ждёт реальных реализаций обеих сторон.
Все 7 задач — GitHub sub-issues эпика; Blocked by проверены фактически.
Ранее существующие #15–21/#27 не изменялись/не закрывались и не дублируются.
S07 дополняет #20/#21 проверкой расширения и обновлением offline kit/закреплённого frontend SHA.

## Передача фронту

1. Прочитать [api/sunday.md](api/sunday.md) или полный текст в эпике GitHub.
2. Скопировать [api/sunday.types.ts](api/sunday.types.ts) в отдельный frontend; объединить
   SundayClientEvent/SundayServerEvent с существующими union types.
3. Использовать [api/examples/sunday](api/examples/sunday) для mocks: runtime,10 NPC,4 lamp
   actions, admin PUT, guide.action и FPS/ping. S07 объявляет capability sunday_v1.
4. Сцена: full runtime после snapshot/entered, dispose прежнего зала, дождаться runtime+assets
   до ready. Анимация частей гида/лампы/NPC локальная; серверных костей нет.
5. Админка проверяет GET /admin/me, редактирует config/profile с expected_revision,
   опрашивает только FPS/ping2с. Конфликт → перечитать, не перезаписывать автоматически.
6. Согласовать production SHA main и проверить оба режима в S07; одного mock-показа недостаточно.

Этот объём рассчитан на оставшиеся 7 часов **при параллельном frontend/backend**;
оценки ориентировочные. Последние 2 часа — интеграция/показ, без новых функций.
Локальные контракты подготовлены без commit/push; опубликованный эпик доступен уже сейчас.

S07: результаты интеграции и непроведённая физическая приёмка — [S07_ACCEPTANCE.md](S07_ACCEPTANCE.md).
