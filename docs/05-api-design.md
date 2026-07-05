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

UUID используется во всех идентификаторах.

---

# 5. Response Standards

Успешный ответ:

{
    "data": {}
}

Ошибки:

{
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
- account
- user
- started_at
- expires_at
- status
- total_price

---

## Payment

- id
- amount
- currency
- status

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

POST /rentals/{rentalId}/cancel

POST /rentals/calculate

POST /rentals/{id}/extend — продление аренды (если аккаунт не забронирован следующим пользователем).

---

## Payments

POST /payments

GET /payments

GET /payments/{paymentId}

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

DELETE /admin/accounts/{accountId}

POST /admin/accounts/{accountId}/sync

GET /admin/users

PATCH /admin/users/{userId}

GET /admin/audit-logs

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