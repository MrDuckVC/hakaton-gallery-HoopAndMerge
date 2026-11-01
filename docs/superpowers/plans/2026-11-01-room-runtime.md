# S01 Room runtime implementation plan

**Goal:** implement issue #37 using docs/api/contract.md and sunday.md, without enabling sunday_v1.
**Architecture:** domain/runtime owns overlay and geometry validation; usecase/runtime owns catalog/store/role/publication/size ports and per-room channel serialization. PostgreSQL CAS is authoritative; wire mapping stays in adapters. Existing game and guide consumers use effective geometry.
**Tech stack:** existing Go, net/http, pgx, PostgreSQL, coder/websocket; no new dependencies.

## Constraints and review focus
- Leave C01 and migrations 00001–00005 unchanged; migration 00006 is forward only and applied separately.
- Persist before publication; uncertain commit closes affected sockets 1011 and forces durable reread. No game mutex on SQL.
- Two competing revision-1 writes produce one revision-2 result and one conflict; old world overlays never apply.
- Reject malformed/duplicate/missing/null JSON fields, role/Origin violations, unsafe geometry and oversized runtime.
- Snapshot/entered precedes runtime; room.sync needs owning connection, permits not-ready and limits 2/10s/user across reconnect.
- Existing public User/session/registration and world files remain unchanged. NPC implementation belongs to S05; supply optional snapshot publication hook in game.

## Tasks
- [x] 1. Core domain/usecase: negative geometry/CAS/SQL-failure tests, domain runtime, consumer ports, durable reads, effective geometry and room serialization. Files internal/domain/runtime, internal/usecase/runtime; catalog room/spawn enumeration.
- [x] 2. PostgreSQL/CLI: new 00006 admin+overlay migration, durable CAS and commit-unknown classification; role lookup/grant; SQL integration tests; cmd/admin and Makefile/Docker/offline packaging.
- [x] 3. HTTP/wire: runtime DTO mapping and size adapter; admin endpoints, strict 64KiB decoder, cookie/role/Origin/rate validation; HTTP negative tests.
- [x] 4. Game/WS/composition: runtime on attach/enter/sync and publication/close adapter; effective guide geometry, per-user sync limits, decoder and event tests.
- [x] 5. Documentation/verification/review: DATA_MODEL/DEVELOPMENT/README/decisions and architecture checks; build/vet/test/race/lint/coverage and contract tools. SQL only on isolated test database. Commit and push issue-37; PR targeting issue-1 as explicitly authorized.

Interfaces shared by tasks are recorded in implementation briefs before dispatch. Work happens in existing issue-37 checkout; unrelated .serena stays untouched. The supplied issue and Sunday contract are the approved design; no additional approval gate is needed.

Verification evidence and scoped limitations: [S01_ACCEPTANCE](../../S01_ACCEPTANCE.md). Independent final review found a transition/invalidation pose race; negative regression reproduced it, reloadPose fix passed scoped re-review. Commit/push/PR follow the user’s explicit instruction; merge is excluded.
