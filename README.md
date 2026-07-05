# GameRent

GameRent - учебный backend на Go для сервиса аренды игровых аккаунтов. Приложение запускается локально, а PostgreSQL и Redis поднимаются через Docker Compose.

## Возможности

- Регистрация и логин пользователей.
- Роли пользователей: `ADMIN` и `RENT`.
- JWT access token и refresh token rotation.
- Хранение refresh token в БД только в виде SHA-256 хэша.
- Каталог игр и игровых аккаунтов.
- Поиск аккаунтов по названию игры, SteamID и логину.
- Аренда аккаунта клиентом.
- Создание платежа и обработка payment webhook.
- Отзывы, уведомления, admin endpoints.
- Фоновая очистка истекших аренд.
- Фоновая синхронизация Steam-библиотек.
- Health endpoints и Prometheus metrics.

## Роли

- `RENT` - клиент. Может регистрироваться, логиниться, смотреть каталог, арендовать аккаунты, создавать платежи, оставлять отзывы и смотреть свои данные.
- `ADMIN` - администратор. Может создавать игровые аккаунты, менять аккаунты, управлять пользователями, смотреть audit logs и административные списки.

При регистрации роль выбирается так:

- если email есть в `ADMIN_EMAILS`, пользователь получает `ADMIN`;
- все остальные получают `RENT`.

Пример:

```env
ADMIN_EMAILS=admin@example.com,owner@example.com
```

## Запуск

Создать `.env`:

```powershell
make setup
```

Поднять PostgreSQL и Redis:

```powershell
make up
```

Запустить API:

```powershell
make run
```

API:

```text
http://localhost:8080
```

## Конфигурация

Все runtime-настройки лежат в `.env`. YAML-конфигов нет.

Основные переменные:

- `HTTP_ADDR=0.0.0.0:8080`
- `POSTGRES_HOST=localhost`
- `POSTGRES_PORT=5433`
- `POSTGRES_USER=postgres`
- `POSTGRES_PASSWORD=postgres`
- `POSTGRES_DB=game_rental`
- `REDIS_HOST=localhost`
- `REDIS_PORT=6379`
- `JWT_SECRET`
- `JWT_TTL=24h`
- `ADMIN_EMAILS=admin@example.com`
- `ENCRYPTION_KEY` - 32 байта для AES-256.
- `STEAM_API_KEY` - можно оставить пустым для FakeSteamClient.
- `PAYMENT_WEBHOOK_SECRET`

## Проверка

Обычные тесты:

```powershell
make test
```

Полный прогон с PostgreSQL/Redis:

```powershell
make test-integration
```

Сборка:

```powershell
make build
```

## Postman

Создай переменные:

```text
baseUrl = http://localhost:8080
apiUrl = http://localhost:8080/api/v1
accessToken =
refreshToken =
```

После регистрации или логина сохрани токены:

```javascript
const json = pm.response.json();
pm.environment.set("accessToken", json.data.access_token);
pm.environment.set("refreshToken", json.data.refresh_token);
```

Для защищенных ручек:

```text
Authorization: Bearer {{accessToken}}
Content-Type: application/json
```

### Auth

```http
POST {{apiUrl}}/auth/register
Content-Type: application/json

{
  "email": "rent@example.com",
  "password": "secret123",
  "first_name": "Ivan",
  "last_name": "Petrov"
}
```

```http
POST {{apiUrl}}/auth/login
Content-Type: application/json

{
  "email": "rent@example.com",
  "password": "secret123"
}
```

```http
GET {{apiUrl}}/auth/me
Authorization: Bearer {{accessToken}}
```

```http
POST {{apiUrl}}/auth/refresh
Content-Type: application/json

{
  "refresh_token": "{{refreshToken}}"
}
```

```http
POST {{apiUrl}}/auth/logout
Content-Type: application/json

{
  "refresh_token": "{{refreshToken}}"
}
```

### User

`RENT` может читать и редактировать только себя. `ADMIN` может работать с другими пользователями.

```http
GET {{apiUrl}}/users/1
Authorization: Bearer {{accessToken}}
```

```http
PATCH {{apiUrl}}/users/1
Authorization: Bearer {{accessToken}}
Content-Type: application/json

{
  "first_name": "Ivan",
  "last_name": "Ivanov"
}
```

### Games

```http
GET {{apiUrl}}/games?page=1&page_size=20
GET {{apiUrl}}/games?search=counter&page=1&page_size=20
GET {{apiUrl}}/games/1
```

### Accounts

```http
GET {{apiUrl}}/accounts?page=1&page_size=20
GET {{apiUrl}}/accounts?search=counter&min_price=100&max_price=1000&page=1&page_size=20
GET {{apiUrl}}/accounts/1
GET {{apiUrl}}/accounts/1/availability
```

### Rentals

```http
POST {{apiUrl}}/rentals
Authorization: Bearer {{accessToken}}
Content-Type: application/json

{
  "account_id": 1,
  "duration_hours": 2
}
```

```http
GET {{apiUrl}}/rentals
GET {{apiUrl}}/me/rentals
GET {{apiUrl}}/rentals/1
POST {{apiUrl}}/rentals/1/cancel
```

```http
POST {{apiUrl}}/rentals/calculate
Content-Type: application/json

{
  "account_id": 1,
  "duration_hours": 2
}
```

### Payments

```http
POST {{apiUrl}}/payments
Authorization: Bearer {{accessToken}}
Content-Type: application/json

{
  "rental_id": 1
}
```

```http
GET {{apiUrl}}/payments
GET {{apiUrl}}/me/payments
GET {{apiUrl}}/payments/1
```

Webhook:

```http
POST {{apiUrl}}/payments/webhook
Content-Type: application/json
X-Payment-Signature: <hmac_sha256_hex>

{
  "payment_id": "1",
  "external_transaction_id": "local-tx-1",
  "status": "success"
}
```

### Reviews

```http
POST {{apiUrl}}/reviews
Authorization: Bearer {{accessToken}}
Content-Type: application/json

{
  "account_id": 1,
  "rental_id": 1,
  "rating": 5,
  "comment": "Все работает"
}
```

```http
GET {{apiUrl}}/accounts/1/reviews
```

### Notifications

```http
GET {{apiUrl}}/notifications
GET {{apiUrl}}/me/notifications
PATCH {{apiUrl}}/notifications/1/read
```

### Admin

Нужен access token пользователя с ролью `ADMIN`.

```http
GET {{apiUrl}}/admin/accounts
POST {{apiUrl}}/admin/accounts
PATCH {{apiUrl}}/admin/accounts/1
POST {{apiUrl}}/admin/accounts/1/sync
GET {{apiUrl}}/admin/users
PATCH {{apiUrl}}/admin/users/1
GET {{apiUrl}}/admin/audit-logs
```

Создание аккаунта:

```http
POST {{apiUrl}}/admin/accounts
Authorization: Bearer {{accessToken}}
Content-Type: application/json

{
  "steam_id64": "76561198000000001",
  "steam_login": "steam_login",
  "steam_password": "steam_password",
  "price_per_hour": 150,
  "security_deposit": 500
}
```

Назначить пользователю роль:

```http
PATCH {{apiUrl}}/admin/users/2
Authorization: Bearer {{accessToken}}
Content-Type: application/json

{
  "role": "ADMIN"
}
```

## Логи

`STEAM_API_KEY is not set. Falling back to FakeSteamClient for local testing.` - это не ошибка. Для локального учебного запуска это нормальное поведение.
