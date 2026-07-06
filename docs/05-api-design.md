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

Используется механизм Refresh Token Rotation.

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

POST /rentals/{id}/extend — продление аренды (если аккаунт не забронирован следующим пользователем).

---

## Payments

POST /payments

GET /payments

GET /payments/{paymentId}

POST /payments/webhook

POST /me/rentals/{rentalId}/pay-with-balance

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

GET /admin/users

PATCH /admin/users/{userId}

GET /admin/audit-logs

---

# 10.1 Rental and Payment Contract

Фактический lifecycle аренды и платежа:

1. `POST /rentals` создаёт `rental.status = WAITING_PAYMENT`, `payment.status = PENDING` и переводит account `AVAILABLE -> RESERVED` в одной PostgreSQL transaction.
2. Успешный `POST /payments/webhook` переводит `payment PENDING -> SUCCESS`, `rental WAITING_PAYMENT -> ACTIVE`, `account RESERVED -> RENTED`.
3. `POST /me/rentals/{rentalId}/pay-with-balance` для владельца rental подтверждает тот же существующий `PENDING` payment из внутреннего `users.balance`: `payment PENDING -> SUCCESS`, `rental WAITING_PAYMENT -> ACTIVE`, `account RESERVED -> RENTED`.
4. `POST /rentals`, wallet payment и webhook не возвращают Steam credentials.
5. Credentials доступны только владельцу активной оплаченной неистёкшей аренды через `GET /api/v1/me/rentals/{rentalId}/credentials`.
6. `POST /rentals/{rentalId}/cancel` отменяет только `WAITING_PAYMENT` аренду: `rental -> CANCELLED`, `payment PENDING -> FAILED`, `account RESERVED -> AVAILABLE`.
7. Expiration worker переводит истёкшую `ACTIVE` аренду в `EXPIRED`, освобождает account `RENTED -> AVAILABLE` и не изменяет `SUCCESS` payment.
8. Waiting-payment cleanup переводит просроченную неоплаченную аренду `WAITING_PAYMENT -> EXPIRED`, payment `PENDING -> FAILED`, account `RESERVED -> AVAILABLE`.

Для одного account база запрещает одновременно более одной аренды в статусе `WAITING_PAYMENT` или `ACTIVE`.

Webhook idempotency:

- повтор webhook с тем же `external_transaction_id` для уже `SUCCESS/ACTIVE/RENTED` состояния возвращает успешный ответ без повторной активации;
- конфликтующий `external_transaction_id` не принимается;
- webhook без `payment_id` и без `external_transaction_id` отклоняется;
- первая успешная обработка требует `external_transaction_id`;
- `provider` и `external_transaction_id` сохраняются в `payments` как provider metadata.

Refund/deposit ledger в текущем backend не реализован. `security_deposit` хранится в rental/payment суммах, но отдельного ledger для refund/deposit release нет.

---

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
