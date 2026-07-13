# GameRent / Ruflo — план действий после аудита Codex

**Recommended model for next strategic reviews:** Sol  
**Reasoning level:** high

**Статус проекта:** late-alpha.  
**Главный вывод:** базовый rental/payment/wallet/refund flow уже собран, но дальнейшие продуктовые фичи нужно временно остановить до закрытия P0-рисков по деньгам, авторизации, webhook security и lifecycle-инвариантам.

---

## 0. Контекст и принцип движения

Проект уже прошёл стадию «собрать MVP». Сейчас основная задача — не добавлять больше фич любой ценой, а стабилизировать домен. В GameRent есть зоны, где ошибка приводит не просто к багу интерфейса, а к финансовой рассинхронизации, некорректной выдаче доступа или небезопасным admin-действиям.

Поэтому дальнейшая разработка должна идти в таком порядке:

1. **P0 — закрыть production-blockers.**
2. **P1 — завершить core product flows.**
3. **P2 — снизить архитектурные и эксплуатационные риски.**
4. **P3 — polish, масштабирование, production hardening.**

До завершения P0 не стоит реализовывать новые продуктовые возможности вроде paid extension, trust score, provider refunds, favorites или production deployment.

---

## 1. Карта состояния проекта

| Область | Текущее состояние | Вывод |
|---|---|---|
| Rental creation | Реализована атомарная резервация, row locking, payment creation | Хорошая база |
| Payment activation | Реализованы wallet/provider activation flows | Логика сильная, но webhook security P0 |
| Wallet payment | Есть атомарный debit + ledger | В целом готово |
| Ledger | Append-only, есть защита от UPDATE/DELETE | Сильная часть, но не все balance mutations идут через ledger |
| Deposit holds | Hold/release/forfeit/refund частично готовы | Backend сильный, policy/UI не завершены |
| Refunds | Wallet full refund реализован | Готово для wallet case |
| Credentials | Owner-scoped endpoint есть | Есть критическая ошибка eligibility |
| Admin rental support | Есть summary/detail/refund/deposit review | Хорошее направление |
| Auth/JWT | Register/login/refresh/logout есть | Нужна защита от privilege/session gaps |
| Reviews | Есть endpoint, но слабые инварианты | Нельзя считать готовым |
| Notifications | Есть чтение/mark-read, producers почти отсутствуют | Частично |
| Catalog | Базово работает | Нужны server-side pagination/filtering |
| Redis/security/trust | В документации есть, в коде почти нет | Future work |
| CI/CD/prod | Описано в docs, но не реализовано полноценно | Позже |

---

## 2. P0 — обязательные блокеры перед дальнейшими фичами

### Контрольная точка 2026-07-13

| Задача | Статус | Подтверждение |
|---|---|---|
| P0.1 — ledger-backed admin balance adjustment и current-admin authorization | ✅ ВЫПОЛНЕНО | Generic balance mutation удалена; отдельный идемпотентный adjustment flow атомарно пишет balance, immutable ledger, audit и security event; stale/demoted/blocked/deleted admin отклоняется. Backend, disposable PostgreSQL integration/E2E и frontend tests/build прошли. |
| P0.2 — payment webhook hardening | ✅ ВЫПОЛНЕНО | Обязательный HMAC-SHA256 exact-raw-body signature, no fallback secret, fail-fast config, 16 KiB limit, strict JSON, safe logs, transactional idempotency/replay guards. Focused, full backend и disposable PostgreSQL integration/E2E tests прошли. Новая migration не потребовалась. |
| P0.3 — auth/admin privilege gaps | ✅ ВЫПОЛНЕНО | Public registration всегда `RENT`; admin назначается явно и требует fresh `ADMIN` JWT; current user state проверяется на каждом protected request; block/demotion отзывает refresh sessions и audit-логируется; rotation/logout сериализованы row lock + conditional revoke; frontend использует single-flight refresh и защищён от session resurrection. Full backend, PostgreSQL integration/E2E, frontend tests/build, `go vet` и dependency audit прошли. |
| P0.4 — credential eligibility | ✅ ВЫПОЛНЕНО | Credential request использует abort + generation guards; stale responses после logout/session/view/rental/expiry/unmount не восстанавливают secrets. Backend блокирует rental/account/successful-payment и пишет secret-free issuance event в одной transaction до response; cleanup race сериализован. Full backend/frontend и disposable PostgreSQL integration/E2E validation прошли. |
| P0.5 — unsafe lifecycle mutations | ✅ ВЫПОЛНЕНО | Бесплатное extension отключено (`501 EXTENSION_NOT_SUPPORTED`) и удалено из UI; generic admin account PATCH стал pricing/deposit-only; status transitions ограничены guarded service/repository methods; VAC disable отклоняется для exclusive lifecycle; active cleanup транзакционен. Focused/full backend, disposable PostgreSQL integration/E2E, frontend tests/typecheck/build, `go vet`, Postman JSON и `git diff --check` прошли. |
| P0.6 — deposit labels и unknown state | ✅ ВЫПОЛНЕНО | User/admin labels покрывают `NONE/HELD/RELEASED/FORFEITED/REFUNDED/UNKNOWN`; corrupt/future backend code отображается как `UNKNOWN`, не `NONE`; добавлен reversible DB CHECK. Frontend/backend tests и disposable PostgreSQL migration/integration validation прошли. |

P0.1–P0.6 закрыты. Финальный P0 audit может переходить к P1 только при сохранении этих validation gates.

### P0.1. Убрать все generic balance mutations и заменить ledger-backed adjustment

**Recommended model:** Sol  
**Reasoning level:** high

**Статус: ✅ ВЫПОЛНЕНО (2026-07-12).** Definition of Done подтверждён полным backend suite, PostgreSQL integration/E2E в одноразовом контейнере и frontend tests/typecheck/build.

### Проблема

Сейчас есть операции, которые могут менять или перезаписывать `users.balance` без обязательной ledger-записи. Это ломает главный финансовый инвариант: баланс пользователя не должен существовать отдельно от истории движения денег.

### Цель

Сделать так, чтобы любое административное изменение баланса проходило только через отдельный use case:

- с блокировкой user row;
- с immutable ledger entry;
- с idempotency key;
- с reason/audit event;
- в одной PostgreSQL-транзакции;
- с защитой от отрицательного результата;
- с тестами на replay и concurrent updates.

### Backend scope

- Убрать `balance` из generic admin user PATCH.
- Проверить profile update, чтобы он не перезаписывал balance старым значением.
- Добавить отдельный admin-only endpoint, например:

```http
POST /api/v1/admin/users/{id}/balance-adjustments
```

- Request:

```json
{
  "amount": 5000,
  "reason_code": "MANUAL_COMPENSATION",
  "comment": "Compensation after support review",
  "idempotency_key": "admin-adjustment-..."
}
```

- Response должен возвращать:
  - user id;
  - previous balance;
  - new balance;
  - ledger entry id;
  - idempotency status.

### Frontend scope

- Убрать прямое поле `balance` из generic admin user edit form.
- Добавить отдельную форму adjustment:
  - сумма;
  - причина;
  - комментарий;
  - confirmation step;
  - disabled state от double-submit.

### DB impact

Возможна новая миграция:

- новый ledger entry type, например `ADMIN_BALANCE_ADJUSTMENT`;
- возможно отдельная таблица `admin_balance_adjustments`;
- уникальный ключ по `idempotency_key`.

Существующие миграции не переписывать.

### Tests required

- admin может сделать positive adjustment;
- admin может сделать negative adjustment, если итоговый баланс не отрицательный;
- negative result rejected;
- non-admin rejected;
- replay по тому же idempotency key не создаёт вторую ledger entry;
- concurrent adjustment безопасен;
- profile update не перетирает balance;
- frontend double-submit не создаёт дубль.

### Definition of Done

- Ни один generic endpoint больше не меняет balance напрямую.
- Все balance updates проходят через финансовый service/use case.
- `go test ./...` проходит.
- PostgreSQL integration tests проходят.
- Frontend tests/typecheck проходят.

---

### P0.2. Harden payment webhooks

**Recommended model:** Sol  
**Reasoning level:** high

**Статус: ✅ ВЫПОЛНЕНО (2026-07-12).** Unsigned/invalid/replay-abusive/malformed/oversized requests не могут достичь business mutation; exact legitimate replay остаётся идемпотентным. Контракт и local secret setup документированы.

### Проблема

Webhook сейчас может принять пустую подпись или fallback secret. Это делает payment activation небезопасной: внешний запрос потенциально может активировать rental без реальной оплаты.

### Цель

Сделать webhook строго проверяемым и безопасным по умолчанию.

### Backend scope

- Fail startup, если webhook secret отсутствует в production-like mode.
- Запретить пустую signature.
- Убрать predictable fallback secret.
- Не логировать invalid signature value.
- Ограничить размер body.
- Сохранить idempotency для duplicate webhook delivery.
- Проверить replay behavior.

### Frontend scope

Нет.

### DB impact

Скорее всего нет.

### Tests required

- missing secret;
- empty signature rejected;
- invalid signature rejected;
- valid signature accepted;
- duplicate webhook idempotent;
- oversized payload rejected;
- malformed payload rejected.

### Definition of Done

- Невозможно активировать payment без валидной подписи.
- Все webhook tests проходят.
- Локальный dev flow документирован: какой secret использовать и как подписывать тестовый webhook.

---

### P0.3. Закрыть auth/admin privilege gaps

**Recommended model:** Sol  
**Reasoning level:** high

**Статус: ✅ ВЫПОЛНЕНО (2026-07-12).** Выбран безопасный вариант manual/provisioned admin: public registration всегда создаёт `RENT`, а повышение выполняется явно и начинает давать privileged access только после нового логина. Refresh rotation и logout сериализованы PostgreSQL row lock и conditional revoke; `token_hash` защищён unique index. Block/demotion в одной транзакции отзывает refresh sessions и пишет audit event, а stale access JWT теряет privileged access через live user-state check. Frontend выполняет single-flight refresh, один retry, корректно обрабатывает revoke/block/demotion и не восстанавливает session после logout. Definition of Done подтверждён full backend suite, PostgreSQL integration/E2E, frontend tests/typecheck/build, `go vet`, `git diff --check` и `npm audit`.

**Техдолг:** P0 отсутствует. P1 — полноценный email verification (`email_verified` пока отражает текущий legacy immediate-login flow) явно отложен до появления email delivery/verification UX; refresh-token family/reuse detection и управление отдельными устройствами отложены до требования global/device-scoped logout. Эти пункты не позволяют public registration получить `ADMIN` и не ослабляют немедленный block/demotion.

### Проблема

Есть риск, что пользователь может получить `ADMIN` через регистрацию email из `ADMIN_EMAILS`, даже если владение email не подтверждено. Также refresh-token rotation не защищён от race condition, а активные JWT продолжают жить после block/demotion.

### Цель

Сделать admin privilege и session lifecycle управляемыми и отзывными.

### Backend scope

- Убрать автоматическую выдачу ADMIN при public registration до email verification.
- Выбрать один безопасный вариант:
  1. отдельный seed/provisioning admin;
  2. email verification before role assignment;
  3. ручное назначение admin уже существующим admin.
- Добавить row lock / conditional update для refresh token rotation.
- Определить стратегию отзыва access token:
  - короткий TTL;
  - session version;
  - token blacklist;
  - или проверка user status на critical admin routes.
- Убедиться, что blocked/demoted user теряет доступ к privileged actions.

### Frontend scope

- Single-flight refresh.
- Корректная обработка 401/403.
- Logout/clear session после revoke/block.

### DB impact

Возможно:

- `email_verified_at`;
- `session_version`;
- дополнительные поля refresh sessions.

### Tests required

- unverified ADMIN_EMAILS не получает admin;
- verified/provisioned admin получает admin корректно;
- concurrent refresh не создаёт два valid successor tokens;
- blocked user теряет доступ;
- demoted admin больше не вызывает admin endpoint;
- logout revokes refresh token.

### Definition of Done

- Public registration не может выдать admin без доказательства владения email.
- Refresh rotation безопасен при concurrency.
- Block/demotion не оставляет активные privileged sessions.

---

### P0.4. Исправить credential eligibility

**Статус: ✅ ВЫПОЛНЕНО (2026-07-13).** Frontend stale-response races закрыты abort + generation checks. Backend eligibility, local decrypt и secret-free issuance audit выполняются в одной transaction с row locks, а cleanup concurrency проверена реальными PostgreSQL race tests. Definition of Done подтверждён полным backend suite, disposable PostgreSQL integration/E2E, frontend tests/typecheck/build и `go vet`.

### Проблема

Credentials сейчас завязаны на `payment_expires_at`, из-за чего активная многочасовая аренда может потерять доступ примерно через 15 минут после создания payment window.

### Цель

Credentials должны быть доступны в течение оплаченной активной аренды, но недоступны после expiry/refund/cancel.

### Backend scope

- Проверять:
  - rental belongs to requester;
  - rental status is `ACTIVE`;
  - payment status is `SUCCESS`;
  - `rental.end_at > now`;
  - no blocking refund/cancellation state;
  - credentials audit event created successfully.
- Не использовать `payment_expires_at` как ограничитель доступа к credentials.
- Добавить `Cache-Control: no-store`.

### Frontend scope

- Чистить credentials:
  - при переходе между экранами;
  - при logout;
  - при expiry;
  - при смене rental;
  - при unmount.
- Не сохранять credentials в localStorage/sessionStorage.

### DB impact

Нет ожидаемого.

### Tests required

- credentials доступны в active rental после истечения payment deadline;
- credentials недоступны после rental end;
- credentials недоступны после cancel/refund;
- чужой пользователь не получает credentials;
- audit failure не должен silently pass;
- response contains no-store headers.

### Definition of Done

- Активная оплаченная аренда не теряет credentials из-за payment window.
- Credentials не кэшируются браузером.
- Audit обязателен.

---

### P0.5. Остановить или перестроить unsafe lifecycle mutations

**Recommended model:** Sol  
**Reasoning level:** high

**Статус: ✅ ВЫПОЛНЕНО (2026-07-13).** Бесплатное extension отключено без mutation, generic account PATCH больше не принимает status, live VAC-disable и cleanup защищают rental/account tuple, unsafe legacy repository methods удалены. Definition of Done подтверждён focused/full backend suite, disposable PostgreSQL integration/E2E, frontend tests/typecheck/build, `go vet`, `git diff --check` и повторным mutation-аудитом.

### Проблема

Rental extension может давать бесплатное продление, а admin account status patch может нарушить lifecycle: например, сделать rented account available.

### Цель

До реализации полноценного paid extension и account state machine — запретить опасные transitions.

### Backend scope

- Временно отключить extension endpoint или вернуть explicit `501 Not Implemented`.
- Убрать возможность менять account status generic PATCH-ем.
- Ввести service-level transition methods:
  - `MarkAvailable`;
  - `ReserveForRental`;
  - `MarkRented`;
  - `ExpireRental`;
  - `SuspendAccount`;
  - `VerifyAccount`.
- Проверять текущий статус и rental linkage перед transition.
- Добавить row locking.

### Frontend scope

- Скрыть/отключить extension button.
- Убрать unsafe account status controls из admin UI или ограничить список безопасных действий.

### DB impact

Возможна новая миграция с `CHECK` constraints для status columns после аудита существующих данных.

### Tests required

- нельзя сделать rented account available через admin patch;
- нельзя продлить rental бесплатно;
- invalid status transitions rejected;
- active rental/account race safe;
- cleanup jobs не конфликтуют с admin actions.

### Definition of Done

- Нет bypass-эндпоинта для account/rental lifecycle.
- Все изменения статусов проходят через доменный service.

---

### P0.6. Исправить deposit labels

**Recommended model:** Luna  
**Reasoning level:** low

**Статус: ✅ ВЫПОЛНЕНО (2026-07-13).** Добавлены отдельные user/admin labels и безопасный `UNKNOWN`; backend больше не превращает неожиданный hold status в `NONE`; новая reversible migration ограничивает persisted codes. Full frontend/backend и disposable PostgreSQL integration/E2E validation прошли.

### Проблема

`depositStatus.ts` показывает literal question marks вместо понятных финансовых статусов. Это не ломает деньги технически, но ломает понимание пользователя и администратора.

### Scope

- Вернуть корректные labels.
- Проверить UTF-8.
- Добавить frontend tests/snapshots на все public states.

### Definition of Done

- Для каждого deposit status есть понятный label.
- Нет `???` или placeholder text.
- Tests проходят.

---

## 3. P1 — завершение core product

P1 начинать только после закрытия P0.

---

### P1.1. Завершить rental lifecycle: `ACTIVE → EXPIRED → COMPLETED`

**Recommended model:** Sol  
**Reasoning level:** high

### Цель

Сейчас rental может истечь, но полноценный completion/deposit settlement policy не завершён. Нужно определить, когда аренда считается завершённой, как обрабатывается депозит и какие уведомления создаются.

### Scope

- Добавить явный transition в `COMPLETED`, если он остаётся частью модели.
- Определить policy:
  - auto-release deposit;
  - manual admin review;
  - timed review window;
  - dispute/forfeit.
- Связать lifecycle с deposit holds.
- Добавить notifications/audit events.
- Обновить frontend rental states.

### Tests

- active rental expires;
- deposit moves to review/released/forfeited по policy;
- no double settlement;
- cleanup idempotent;
- user/admin UI корректно показывает статус.

---

### P1.2. Review invariants

**Recommended model:** Terra  
**Reasoning level:** medium

### Цель

Отзывы должны быть доступны только по реальной завершённой аренде.

### Scope

- Проверить:
  - rental belongs to renter;
  - rental is `COMPLETED`;
  - account belongs to rental;
  - one review per rental/account;
  - rating bounds.
- Добавить aggregation для account/game rating.
- Добавить frontend review form.

### Tests

- нельзя оставить review без rental;
- нельзя оставить review за чужую rental;
- нельзя оставить review до completion;
- нельзя оставить duplicate review.

---

### P1.3. Server-driven catalog

**Recommended model:** Terra  
**Reasoning level:** medium

### Цель

Каталог должен фильтроваться и пагинироваться на сервере, а не через client-side ограничение.

### Scope

- Accurate `total_count`.
- Pagination.
- Search/filter contract.
- Sorting.
- Account detail endpoint.
- Frontend pagination.
- Использовать `/rentals/calculate` вместо локального pricing preview.

### Tests

- filters;
- pagination;
- total count;
- empty state;
- loading/error states.

---

### P1.4. Safe admin operations

**Recommended model:** Sol  
**Reasoning level:** high

### Цель

Админка должна стать не набором прямых patch-операций, а набором безопасных доменных действий.

### Scope

- Audited user changes.
- Self-demotion safeguards.
- Self-block safeguards.
- Account verification controls.
- Deposit settlement UI.
- Real pagination.
- KPIs from authoritative queries.

### Tests

- admin cannot accidentally block/demote self without explicit flow;
- deposit settlement idempotent;
- account verification transitions safe;
- stale admin filter responses do not overwrite newer ones.

---

### P1.5. Notification producers

**Recommended model:** Terra  
**Reasoning level:** medium

### Цель

Если notification API уже есть, нужно создавать события из реальных бизнес-операций.

### Events

- payment success;
- rental activated;
- rental expiring soon;
- rental expired;
- deposit released;
- deposit forfeited;
- refund completed;
- admin action requiring attention.

### Tests

- event created once;
- duplicate operations do not duplicate notifications;
- mark-read works;
- user sees only own notifications.

---

### P1.6. Интеграционные тесты и CI

**Recommended model:** Sol  
**Reasoning level:** high

### Цель

Сейчас часть важных PostgreSQL/E2E tests skipped. Для финансового проекта это риск.

### Scope

- Isolated PostgreSQL/Redis test environment.
- `RUN_INTEGRATION_TESTS=1` target.
- Migration up/down validation.
- Backend + frontend test target.
- GitHub Actions or local CI script.
- Test data cleanup.

### Definition of Done

- Один top-level command валидирует backend, frontend, migrations и integration tests.
- CI падает на нарушении финансовых/rental invariants.

---

## 4. P2 — снижение архитектурного и эксплуатационного риска

---

### P2.1. Redis caching только там, где есть измеримая польза

**Recommended model:** Terra  
**Reasoning level:** medium

Не включать Redis «потому что он есть в docs». Сначала выбрать конкретные кейсы:

- catalog cache;
- game metadata cache;
- session/security rate limiting;
- idempotency/replay helper, если оправдано.

Для каждого cache должен быть explicit invalidation и fallback to DB.

---

### P2.2. Security/trust services

**Recommended model:** Sol  
**Reasoning level:** high

Добавлять после стабилизации core flows:

- security event correlation;
- trust recalculation;
- suspicious activity flags;
- immutable security events;
- admin review queues;
- IP/region risk rules.

---

### P2.3. Provider refunds / partial refunds / self-service refund requests

**Recommended model:** Sol  
**Reasoning level:** high

Делать только после стабильного wallet refund и ledger-backed balance operations.

Scope:

- refund request by user;
- admin review;
- partial refund semantics;
- provider refund integration;
- ledger mapping;
- deposit interaction.

---

### P2.4. Постепенно разгрузить monolithic handler.go

**Recommended model:** Terra  
**Reasoning level:** medium

Не делать big rewrite. Разделять только при изменении домена:

- auth handlers;
- rental handlers;
- payment handlers;
- admin handlers;
- review handlers;
- notification handlers.

Каждый extraction должен идти вместе с тестами.

---

### P2.5. Frontend resilience

**Recommended model:** Terra  
**Reasoning level:** medium

Scope:

- abort controllers;
- request version guards;
- isolated section loading;
- automatic refresh;
- better 401/403 handling;
- browser E2E;
- accessibility checks.

---

## 5. P3 — polish и production hardening

---

### P3.1. Production deployment

**Recommended model:** Sol  
**Reasoning level:** high

Scope:

- production Compose override or Kubernetes;
- TLS;
- CSP/security headers;
- secrets manager;
- backups;
- restore drills;
- monitoring;
- logs/metrics/tracing.

---

### P3.2. Persistent favorites

**Recommended model:** Luna  
**Reasoning level:** medium

Сейчас favorites misleading, потому что success есть, persistence нет. Либо убрать stub, либо реализовать нормально.

---

### P3.3. Multiple platforms / real payment gateways

**Recommended model:** Sol  
**Reasoning level:** high

Добавлять только после стабилизации Steam-only flow.

---

### P3.4. Localization and UX polish

**Recommended model:** Luna  
**Reasoning level:** low

Scope:

- consistent Russian labels;
- UTF-8 cleanup;
- empty states;
- accessibility;
- visual consistency.

---

## 6. Рекомендуемая очередность Codex-сессий

| Шаг | Задача | Модель | Reasoning | Тип промпта |
|---|---|---:|---:|---|
| 1 | Ledger-backed admin balance adjustment | Sol | high | implementation |
| 2 | Review результата balance adjustment | Sol | high | review |
| 3 | Webhook hardening | Sol | high | implementation |
| 4 | Review webhook security | Sol | high | review |
| 5 | Auth/admin/session hardening | Sol | high | implementation |
| 6 | Credential eligibility fix | Sol | high | implementation |
| 7 | Disable unsafe lifecycle mutations | Sol | high | implementation |
| 8 | Deposit labels cleanup | Luna | low | implementation |
| 9 | Full P0 regression audit | Sol | high | audit/review |
| 10 | Rental completion/deposit policy design | Sol | high | architecture |
| 11 | Rental completion implementation | Sol | high | implementation |
| 12 | Reviews invariants | Terra | medium | implementation |
| 13 | Server-driven catalog | Terra | medium | implementation |
| 14 | Integration tests + CI | Sol | high | implementation |

---

## 7. Что не делать сейчас

Не стоит сейчас:

- добавлять новые payment providers;
- делать partial refunds;
- включать Redis caching без конкретного кейса;
- разворачивать production;
- добавлять trust score;
- делать paid extension;
- добавлять новые платформы кроме Steam;
- делать большой refactor всей архитектуры;
- расширять admin UI, пока не закрыты unsafe mutations.

Причина простая: эти задачи увеличат площадь риска, пока базовые финансовые и security-инварианты ещё не защищены.

---

## 8. Первый следующий промпт для Codex

**Recommended model:** Sol  
**Reasoning level:** high

```text
You are working in the existing GameRent repository.

Goal:
Remove all generic balance mutations and implement a safe, ledger-backed admin balance adjustment flow.

Important:
This is a P0 financial-integrity task. Do not expand the scope beyond balance mutation safety.

Requirements:
1. Read AGENTS.md and the relevant financial, API, database, security, and domain documentation.
2. Inspect the current dirty worktree and preserve unrelated user changes.
3. Find every place where `users.balance` can be written or overwritten.
4. Remove `balance` from the generic admin user PATCH contract.
5. Ensure profile/user updates cannot overwrite balance with stale data.
6. Add a dedicated ADMIN-only balance adjustment use case.
7. The operation must:
   - use integer minor units;
   - lock the user row;
   - reject a resulting negative balance;
   - update `users.balance`;
   - create exactly one immutable financial ledger entry;
   - use an idempotency key;
   - create an audit/security event;
   - perform all state changes in one PostgreSQL transaction;
   - be safe under replay and concurrent requests.
8. Preserve existing response envelopes and unrelated public routes.
9. Add a new reversible Goose migration only if required. Do not modify existing migrations.
10. Update the typed frontend API and add an explicit confirmation form with a required reason code.
11. Add tests:
   - service unit tests;
   - PostgreSQL concurrency/idempotency tests;
   - endpoint authorization/E2E tests;
   - frontend double-submit/error tests.
12. Run:
   - formatting;
   - go test ./...;
   - integration tests with isolated PostgreSQL/Redis if available;
   - go vet ./...;
   - frontend tests;
   - TypeScript/build validation.

Before implementation:
Report the exact transaction design, ledger entry semantics, API request/response contract, and migration impact.

Then implement and validate the change.

Final response:
- changed files;
- new API contract;
- migration details;
- tests run and results;
- remaining risks.
```

---

## 9. Контрольная точка после P0

После закрытия всех P0 задач нужно снова запустить audit prompt:

**Recommended model:** Sol  
**Reasoning level:** high

Цель повторного аудита:

- проверить, что balance/ledger invariant больше не нарушается;
- проверить webhook security;
- проверить admin privilege flow;
- проверить credential access;
- проверить lifecycle transitions;
- проверить, что тесты реально покрывают PostgreSQL/E2E cases;
- обновить roadmap перед переходом к P1.

---

## 10. Итоговая стратегия

GameRent уже имеет хорошую базу: rental creation, wallet payment, ledger, deposit holds, refunds и admin review не выглядят как черновик. Но проект перешёл в фазу, где главная ценность — не скорость добавления фич, а сохранение инвариантов.

Правильный путь сейчас:

1. Закрыть P0.
2. Проверить P0 отдельным review.
3. Завершить lifecycle и deposit policy.
4. Усилить tests/CI.
5. Потом расширять продукт.

Если придерживаться этой очередности, проект будет двигаться не хаотично, а как нормальная финансово-чувствительная система: сначала безопасность и консистентность, затем продуктовые возможности.
