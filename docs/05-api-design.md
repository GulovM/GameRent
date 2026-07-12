# API Design

## 1. Purpose

Данный документ описывает REST API платформы GameRent.

API предназначено для взаимодействия:

- Web Client
- Mobile Client (future)
- Admin Panel

Все запросы выполняются по HTTPS.

Базовый URL:

/api/v1

---

# 2. API Principles

API проектируется согласно REST.

Основные принципы:

- Resource-oriented
- Stateless
- JSON only
- HTTPS only
- Versioned
- JWT Authentication

---

# 3. Authentication

Используется JWT.

## Access Token

Назначение:

Авторизация запросов.

Время жизни:

15 минут.

Передается:

Authorization: Bearer <token>

---

## Refresh Token

Назначение:

Получение нового Access Token.

Время жизни:

30 дней.

Используется механизм Refresh Token Rotation. Предъявленный token блокируется и отзывается в PostgreSQL-транзакции до создания successor token; конкурентное повторное предъявление не может создать второй действующий successor.

Refresh Token хранится только в виде hash.

---

# 4. Request Standards

Content-Type:

application/json

Все даты передаются в ISO-8601 UTC.

Пример:

2026-07-04T18:30:00Z

Идентификаторы в текущей реализации используют PostgreSQL `BIGSERIAL` и передаются в API как JSON number / Go `int64` / TypeScript `number`.

Path parameters (`{accountId}`, `{rentalId}`, `{paymentId}`, `{userId}`) ожидают десятичные integer ID, а не UUID.

---

# 5. Response Standards

Успешный ответ:

{
    "success": true,
    "data": {}
}

Ошибки:

{
    "success": false,
    "error": {
        "code": "ACCOUNT_NOT_AVAILABLE",
        "message": "The account is currently unavailable."
    }
}

---

# 6. Error Codes

Общие ошибки:

INVALID_REQUEST

UNAUTHORIZED

FORBIDDEN

NOT_FOUND

VALIDATION_ERROR

INTERNAL_ERROR

Бизнес-ошибки:

ACCOUNT_NOT_AVAILABLE

ACCOUNT_ALREADY_RENTED

INSUFFICIENT_TRUST_LEVEL

PAYMENT_FAILED

INVALID_RENTAL_DURATION

STEAM_ACCOUNT_UNAVAILABLE

---

# 7. Pagination

Поддерживается для всех коллекций.

Параметры:

page

page_size

Ответ:

{
    "data": [],
    "pagination": {
        "page": 1,
        "page_size": 20,
        "total_items": 356,
        "total_pages": 18
    }
}

---

# 8. Filtering

Поддерживаются:

search

sort

order

status

game

min_price

max_price

trust_level

---

# 9. DTO Models

## User

- id
- email
- trust_level

---

## Account

- id
- steam_id64
- status
- price_per_hour
- security_deposit
- games

---

## Game

- id
- steam_app_id
- name
- header_image

---

## Rental

- id
- user_id
- account_id
- started_at
- expires_at
- payment_expires_at
- status
- rental_price
- security_deposit
- total_price

---

## Payment

- id
- rental_id
- amount
- currency
- status
- created_at

---

## RentalCredentials

- login
- password

Ответ выдаётся только после commit транзакции eligibility + security audit и содержит `Cache-Control: no-store`. Endpoint не возвращает account metadata, tokens или audit metadata.

---

# 10. Endpoints

## Authentication

POST /auth/register

POST /auth/login

POST /auth/logout

POST /auth/refresh

GET /auth/me
/me/rentals, GET /me/payments, GET /me/notifications — более естественные маршруты для данных текущего пользователя, чем обязательная фильтрация через общие коллекции.

---

## Accounts

GET /accounts

GET /accounts/{accountId}

GET /accounts/{accountId}/availability

POST /accounts/{id}/favorite и DELETE /accounts/{id}/favorite — если позже появится избранное.
---

## Games

GET /games

GET /games/{gameId}

---

## Rentals

POST /rentals

GET /rentals

GET /rentals/{rentalId}

GET /me/rentals/{rentalId}/credentials

POST /rentals/{rentalId}/cancel

POST /rentals/calculate

POST /rentals/{id}/extend — paid extension пока не реализован. Endpoint всегда возвращает `501 EXTENSION_NOT_SUPPORTED` и никогда не изменяет `end_at`. Полноценное продление требует отдельного payment/ledger/pricing/idempotency flow.

---

## Payments

POST /payments

GET /payments

GET /payments/{paymentId}

POST /payments/webhook

POST /me/rentals/{rentalId}/pay-with-balance

---

### Payment provider webhook security contract

`POST /api/v1/payments/webhook` is public to the simulated provider but is not unsigned. Startup fails unless `PAYMENT_WEBHOOK_SECRET` is explicitly configured as a non-placeholder value of at least 32 bytes. Local development and tests must set their own deterministic or generated secret; there is no fallback.

The provider sends one `X-Payment-Signature` header containing exactly 64 lowercase hexadecimal characters. It is computed as `HMAC-SHA256(PAYMENT_WEBHOOK_SECRET, raw_request_body)`. Verification uses the exact bounded bytes received and constant-time digest comparison. Missing, empty, malformed, duplicated or incorrect signature headers return `401 UNAUTHORIZED` before payload decoding or transaction work.

The request body is limited to 16 KiB and must be `application/json`. It must contain exactly one JSON object with no unknown fields, duplicate fields or trailing data:

```json
{
  "payment_id": "123",
  "rental_id": "456",
  "external_transaction_id": "provider-transaction-789",
  "provider": "internal",
  "amount": 1500,
  "currency": "USD",
  "status": "success"
}
```

All identifiers are required and bounded. `payment_id` and `rental_id` must be positive decimal identifiers; `external_transaction_id` is 1..128 ASCII letters, digits, `.`, `_`, `:` or `-`; `amount` is a positive integer minor-unit amount; provider, amount and currency must exactly match the locked payment. Provider callbacks cannot process wallet-provider (`balance`) payments.

Invalid JSON and unsupported provider values return `400`; oversized input returns `413`; a non-JSON Content-Type returns `415`; unknown payment returns `404`; identifier, stored-provider, financial or lifecycle conflicts return `409`. Responses never expose signatures, secrets, raw payloads or internal database errors.

The successful transition and all ledger/deposit/security facts commit atomically. Exact signed redelivery returns idempotent success without another transition, ledger entry or deposit hold. Reusing an external transaction for a different payment, changing a completed payment's external transaction, or supplying mismatched financial facts is rejected.

---

## Reviews

POST /reviews

GET /accounts/{accountId}/reviews

---

## Notifications

GET /notifications

PATCH /notifications/{notificationId}/read

---

## Admin

GET /admin/accounts

POST /admin/accounts

PATCH /admin/accounts/{accountId}

POST /admin/accounts/{accountId}/sync

GET /admin/rentals

GET /admin/refund-reason-codes

POST /admin/rentals/{rentalId}/wallet-refund

GET /admin/users

PATCH /admin/users/{userId}

POST /admin/users/{userId}/balance-adjustments

GET /admin/audit-logs

---

### Current administrator authorization

All `/api/v1/admin/*` endpoints load the actor's current PostgreSQL state. The actor must exist, have no `deleted_at`, be unblocked and currently have role `ADMIN`; the signed access token must also contain `ADMIN`. Therefore stale demoted tokens and pre-promotion `RENT` tokens do not grant access. Public registration always issues `RENT`; promotion is an explicit provisioning/admin operation followed by a fresh login.

Admin account create/update requests use strict JSON decoding and validate the merged hourly-price/deposit configuration against the maximum supported 720-hour rental. Generic account PATCH accepts only `price_per_hour` and `security_deposit`; `status` and every other unknown field return `422 VALIDATION_ERROR`. Rental quote and creation arithmetic uses checked signed-64-bit multiplication and addition; configurations or requests whose integer-minor-unit total cannot be represented are rejected rather than wrapped.

Financial admin operations (`balance-adjustments`, `wallet-refund`, deposit `release` and deposit `forfeit`) repeat this authorization inside their PostgreSQL transaction before any idempotent replay response or mutation. A replay attempted after actor demotion, blocking or deletion returns `403 FORBIDDEN` without duplicating or changing financial records.

Admin account creation and pricing/deposit PATCH also revalidate inside their write transaction. Generic PATCH cannot change lifecycle status. Steam synchronization does not hold a database transaction across Steam network calls; immediately before persisting library state or disabling a VAC-banned account, its repository transaction revalidates the current administrator again. VAC disable locks the account and is rejected while a `WAITING_PAYMENT` or `ACTIVE` rental exists, so it cannot desynchronize account availability from rental state.

`PATCH /api/v1/admin/users/{userId}` applies the same transactional authorization. It accepts only `trust_score`, `is_blocked` and `role`, rejects deleted targets, and rejects every self-targeted admin update with `409 ADMIN_USER_UPDATE_FORBIDDEN`. A role/block change revokes every active refresh token of the target and writes `USER_SECURITY_STATE_UPDATED` to the audit log in the same transaction. The current API does not support user deletion or undeletion. Because an active administrator cannot demote or block itself, a successful privilege update always leaves its currently authorized actor active.

---

# 10.1 Rental and Payment Contract

Фактический lifecycle аренды и платежа:

1. `POST /rentals` создаёт `rental.status = WAITING_PAYMENT`, `payment.status = PENDING` и переводит account `AVAILABLE -> RESERVED` в одной PostgreSQL transaction.
2. Успешный `POST /payments/webhook` переводит `payment PENDING -> SUCCESS`, `rental WAITING_PAYMENT -> ACTIVE`, `account RESERVED -> RENTED`.
3. `POST /me/rentals/{rentalId}/pay-with-balance` для владельца rental подтверждает тот же существующий `PENDING` payment из внутреннего `users.balance`: `payment PENDING -> SUCCESS`, `rental WAITING_PAYMENT -> ACTIVE`, `account RESERVED -> RENTED`.
4. `POST /rentals`, wallet payment и webhook не возвращают Steam credentials.
5. Credentials доступны только владельцу активной оплаченной неистёкшей аренды через `GET /api/v1/me/rentals/{rentalId}/credentials`. Endpoint в одной PostgreSQL transaction блокирует rental/account/successful payment, повторно проверяет eligibility, локально дешифрует пароль и пишет secret-free security event; audit failure или конкурирующий cleanup закрывают выдачу fail-closed. Response отправляется только после commit и содержит no-store headers.
6. `POST /rentals/{rentalId}/cancel` отменяет только `WAITING_PAYMENT` аренду: `rental -> CANCELLED`, `payment PENDING -> FAILED`, `account RESERVED -> AVAILABLE`.
7. Expiration worker переводит истёкшую `ACTIVE` аренду в `EXPIRED`, освобождает account `RENTED -> AVAILABLE` и не изменяет `SUCCESS` payment.
8. Waiting-payment cleanup переводит просроченную неоплаченную аренду `WAITING_PAYMENT -> EXPIRED`, payment `PENDING -> FAILED`, account `RESERVED -> AVAILABLE`.
9. Успешный provider webhook пишет immutable `financial_ledger_entries`: `PROVIDER_PAYMENT_RECEIVED` и, при ненулевом депозите, `DEPOSIT_HELD`.
10. `POST /api/v1/admin/rentals/{rentalId}/wallet-refund` доступен только `ADMIN`, принимает `reason_code`, работает только для wallet-paid `SUCCESS` payment и rental в `EXPIRED` или `COMPLETED`. Backend сам рассчитывает full refund, не переводит `payment.status` в отдельный `REFUNDED` status и не меняет `rental.status` из-за refund.
11. Wallet refund кредитует `users.balance` идемпотентно и пишет отдельные ledger entries `BALANCE_REFUND_CREDIT` и `DEPOSIT_REFUND_CREDIT`. Если `deposit_hold` уже `RELEASED` или `FORFEITED`, депозит повторно не кредитуется.
12. Rental extension отключён: endpoint возвращает `501 EXTENSION_NOT_SUPPORTED`, UI не предлагает действие, `end_at` остаётся неизменным.
13. Generic admin account PATCH не принимает lifecycle status. Account status меняется только согласованными rental/payment/cleanup/idle-disable операциями.

Для одного account база запрещает одновременно более одной аренды в статусе `WAITING_PAYMENT` или `ACTIVE`.

Webhook idempotency:

- повтор webhook с тем же `external_transaction_id` для уже `SUCCESS/ACTIVE/RENTED` состояния возвращает успешный ответ без повторной активации;
- конфликтующий `external_transaction_id` не принимается;
- webhook без `payment_id` и без `external_transaction_id` отклоняется;
- первая успешная обработка требует `external_transaction_id`;
- `provider` и `external_transaction_id` сохраняются в `payments` как provider metadata;
- wallet payment использует существующий `PENDING` payment и provider `balance`;
- повторный wallet refund по тому же rental должен возвращать идемпотентный результат без второго balance credit;
- refund history public API пока отсутствует.

### Admin wallet refund contract

`GET /api/v1/admin/refund-reason-codes` returns the backend-owned safe catalog for wallet refund reasons and should be used as the frontend source of truth:

```json
{
  "success": true,
  "data": {
    "reason_codes": [
      { "code": "SERVICE_UNAVAILABLE", "label": "Service unavailable" },
      { "code": "ACCOUNT_INVALID", "label": "Account invalid" },
      { "code": "ADMIN_CORRECTION", "label": "Admin correction" }
    ]
  }
}
```

`POST /api/v1/admin/rentals/{rentalId}/wallet-refund`

Request body:

```json
{
  "reason_code": "SERVICE_UNAVAILABLE"
}
```

Validation and behavior:

- доступ только для `ADMIN`;
- `reason_code` обязателен, короткий и ограничен буквами, цифрами, `_` и `-`;
- endpoint работает только для wallet-paid payment (`provider = balance`);
- payment должен быть `SUCCESS`;
- rental должен быть `EXPIRED` или `COMPLETED`;
- backend сам рассчитывает `principal_amount`, `deposit_amount` и `total_amount`;
- credentials, ledger metadata, idempotency keys, provider details и внутренние correlation IDs в response не возвращаются.

Success response (`200 OK`, стандартный response envelope):

```json
{
  "success": true,
  "data": {
    "changed": true,
    "idempotent": false,
    "status": "COMPLETED",
    "principal_amount": 500,
    "deposit_amount": 700,
    "total_amount": 1200,
    "deposit_status": "REFUNDED"
  }
}
```

Known limitations of the current contract:

- provider refund ещё не реализован;
- partial refund ещё не реализован;
- self-service refund ещё не реализован;
- отдельный public refund history endpoint ещё не реализован;
- отдельный authenticated internal system caller surface пока не реализован.

---

### Admin balance adjustment contract

`POST /api/v1/admin/users/{userId}/balance-adjustments` is the only administrative API allowed to change `users.balance`.

Request body:

```json
{
  "amount": 5000,
  "currency": "USD",
  "reason_code": "MANUAL_COMPENSATION",
  "comment": "Support-approved balance correction",
  "idempotency_key": "admin-balance-adjustment-4ddbd591"
}
```

Rules:

- only `ADMIN` may call the endpoint; the transaction revalidates that the actor is still an existing, unblocked `ADMIN`, including for idempotent replays;
- `amount` is a signed, non-zero integer in minor units: positive credits and negative debits;
- the current wallet is single-currency, so only `USD` is accepted;
- the resulting balance must remain non-negative and must not overflow `BIGINT`;
- `reason_code` and a caller-owned idempotency key are required;
- the target user row is locked and the balance, immutable ledger entry, audit log and security event are committed in one PostgreSQL transaction;
- ledger `amount` remains positive; entry types `ADMIN_BALANCE_CREDIT` and `ADMIN_BALANCE_DEBIT` encode direction;
- a matching replay returns the original result without another balance or ledger mutation;
- reusing a key for different request data returns `409 BALANCE_ADJUSTMENT_FAILED`;
- `PATCH /api/v1/admin/users/{userId}` does not accept `balance`.

Success response (`201 Created`; exact replay uses `200 OK`):

```json
{
  "success": true,
  "data": {
    "adjustment_id": 123,
    "user_id": 456,
    "previous_balance": 10000,
    "new_balance": 15000,
    "amount": 5000,
    "currency": "USD",
    "ledger_entry_id": 123,
    "idempotency_key": "admin-balance-adjustment-4ddbd591",
    "idempotent_replay": false,
    "created_at": "2026-07-11T09:00:00Z"
  }
}
```

The adjustment is represented by its immutable ledger entry, therefore `adjustment_id` and `ledger_entry_id` are the same identifier.

---

### Admin rentals summary contract

`GET /api/v1/admin/rentals` supports validated server-side filters:

- `rental_status`: `WAITING_PAYMENT | ACTIVE | EXPIRED | CANCELLED | COMPLETED`
- `payment_status`: `PENDING | SUCCESS | FAILED`
- `payment_provider`: `balance | internal`
- `deposit_status`: `NONE | HELD | RELEASED | FORFEITED | REFUNDED | UNKNOWN`
- `refund_status`: `NONE | REQUESTED | COMPLETED | FAILED`
- `eligible_wallet_refund`: `true | false`
- `user_id`: exact `int64`
- `rental_id`: exact `int64`
- `page`: `> 0`
- `page_size`: `> 0` and `<= 100`

Invalid filter values return the standard validation envelope with `422 VALIDATION_ERROR`.

`NONE` означает только нулевой депозит или отсутствие hold. Неизвестный ненулевой код `deposit_holds.status` сериализуется как `UNKNOWN`, никогда как `NONE`; frontend показывает warning/review label. База дополнительно ограничивает persisted hold statuses значениями `1..4` через `chk_deposit_holds_status_known`.

The response includes a server-side `summary` alongside `rentals` and `pagination` so admin KPI values, `eligible_wallet_refund_count`, and `total_items` are all computed from the same filtered result set rather than from the loaded page slice:

```json
{
  "success": true,
  "data": {
    "summary": {
      "total_count": 120,
      "eligible_wallet_refund_count": 7,
      "rental_status_counts": {
        "WAITING_PAYMENT": 10,
        "ACTIVE": 20,
        "EXPIRED": 30,
        "COMPLETED": 40,
        "CANCELLED": 20
      },
      "payment_status_counts": {
        "PENDING": 10,
        "SUCCESS": 90,
        "FAILED": 15,
        "CANCELLED": 5
      },
      "refund_status_counts": {
        "NONE": 113,
        "REQUESTED": 0,
        "COMPLETED": 7,
        "FAILED": 0
      }
    }
  }
}
```

### Admin rental detail contract

`GET /api/v1/admin/rentals/{rentalId}` is a read-only `ADMIN` endpoint for support and refund review.

Validation and behavior:

- `rentalId` must be a positive integer;
- invalid path value returns `422 VALIDATION_ERROR`;
- unknown rental returns `404 NOT_FOUND`;
- non-admin callers receive the standard forbidden response.

Safe response sections:

- `rental`: `id`, `user_id`, `account_id`, `status`, `start_at`, `end_at`, `rental_price`, `deposit_amount`, `payment_expires_at`, `created_at`, `updated_at`
- `payment`: latest safe payment snapshot with `id`, `status`, `provider`, `amount`, `currency`, `created_at`
- `deposit`: `amount`, `currency`, public `status`, `held_at`, `released_at`, `forfeited_at`, `refunded_at`
- `refund_summary`: `count`, `latest_refund_status`, `total_refunded_principal`, `total_refunded_deposit`, `latest_processed_at`
- `ledger_summary`: `counts_by_display_type`, `totals_by_display_type`, latest 5 safe entries with `id`, `display_type`, `amount`, `currency`, `created_at`
- `support_flags`: `eligible_wallet_refund`, `refund_ineligible_reason`, `has_active_credentials_access`, `payment_window_expired`

Explicitly redacted fields:

- Steam login / password / encrypted credentials
- JWT / refresh tokens
- webhook signatures and provider raw payloads
- ledger or refund `metadata`
- `idempotency_key`
- internal `correlation_id`
- audit/security raw payloads
- credential access payloads

`external_transaction_id` is intentionally omitted from this endpoint.

# 11. HTTP Status Codes

200 OK

201 Created

204 No Content

400 Bad Request

401 Unauthorized

403 Forbidden

404 Not Found

409 Conflict

422 Unprocessable Entity

500 Internal Server Error

---

# 12. Security

Все защищённые эндпоинты требуют JWT.

Refresh Token никогда не используется для доступа к ресурсам.

Все запросы логируются.

Rate Limiting применяется к:

- login
- register
- refresh
- payment

---

# 13. Versioning

Все версии API имеют префикс:

/api/v1

Новые несовместимые изменения публикуются в:

/api/v2

Без нарушения совместимости предыдущей версии.

---

# 14. Future Extensions

API допускает расширение:

- WebSocket уведомления
- Public API
- Steam OAuth
- GraphQL Gateway
- gRPC Internal API
