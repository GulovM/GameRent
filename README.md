# GameRent

GameRent - full-stack приложение для аренды игровых аккаунтов. Backend написан на Go, данные хранятся в PostgreSQL, Redis используется для вспомогательных runtime-задач, frontend реализован на React/Vite и в production отдается через Nginx.

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
- Темный адаптивный web-интерфейс для desktop/mobile.

## Стек

- Go `1.25.7`
- PostgreSQL `17-alpine3.24`
- Redis `7.2.14-alpine`
- React `19`
- Vite `6`
- Nginx `1.27-alpine`
- Docker Compose

## Быстрый запуск в Docker

Создать `.env` из примера:

```powershell
make setup
```

Поднять весь стек:

```powershell
make up
```

После запуска:

```text
Frontend: http://localhost:5173
API:      http://localhost:8080
API via frontend proxy: http://localhost:5173/api/v1
```

Проверить контейнеры:

```powershell
make ps
```

Посмотреть логи:

```powershell
make logs
```

Остановить стек:

```powershell
make down
```

## Локальная разработка

Поднять только инфраструктуру:

```powershell
make infra-up
```

Запустить API локально:

```powershell
make run
```

Запустить frontend локально в другом терминале:

```powershell
make web-deps
make web-dev
```

Vite dev server проксирует `/api` и `/healthz` на `http://localhost:8080`.

## Makefile

```text
make setup             create .env from .env.example if missing
make deps              download Go modules and install web deps
make web-deps          install frontend dependencies
make up                start full Docker stack: web, API, PostgreSQL, Redis
make infra-up          start PostgreSQL and Redis only
make down              stop Docker services
make restart           restart full Docker stack
make run               run API locally
make dev               start DB/Redis and run API locally
make web-dev           run frontend locally with Vite
make build             build API binary and frontend
make build-api         build API binary into bin/
make build-web         build frontend bundle
make docker-build      build API and web Docker images
make test              run all tests that do not require external services
make test-integration  run DB and E2E tests with local services
make fmt               format Go code
make tidy              clean go.mod and go.sum
make clean             remove local build/runtime artifacts
make logs              follow Docker service logs
make ps                show Docker Compose services
```

## Конфигурация

Все runtime-настройки лежат в `.env`. YAML-конфигов нет.

Основные переменные:

```env
HTTP_ADDR=0.0.0.0:8080
HTTP_SHUTDOWN_TIMEOUT=15s

POSTGRES_HOST=localhost
POSTGRES_PORT=5433
POSTGRES_USER=postgres
POSTGRES_PASSWORD=postgres
POSTGRES_DB=game_rental
POSTGRES_TIMEOUT=10s

REDIS_HOST=localhost
REDIS_PORT=6379
REDIS_PASSWORD=

JWT_SECRET=your-jwt-token-signing-secret-key-here
JWT_TTL=24h
ADMIN_EMAILS=admin@example.com
ENCRYPTION_KEY=your-32-byte-aes-encryption-key-here

STEAM_API_KEY=
STEAM_BASE_URL=https://api.steampowered.com
PAYMENT_WEBHOOK_SECRET=local-payment-webhook-secret
```

Для Docker Compose `POSTGRES_HOST`, `POSTGRES_PORT`, `REDIS_HOST`, `REDIS_PORT` и `GOOSE_DBSTRING` переопределяются внутри `docker-compose.yml`, потому что контейнеры общаются друг с другом по service name: `postgres`, `redis`, `api`.

Если `STEAM_API_KEY` пустой, приложение использует `FakeSteamClient` для локальной разработки.

## Роли

- `RENT` - клиент. Может регистрироваться, логиниться, смотреть каталог, арендовать аккаунты, создавать платежи, оставлять отзывы и смотреть свои данные.
- `ADMIN` - администратор. Может создавать и менять игровые аккаунты, управлять пользователями, смотреть audit logs и административные списки.

При регистрации роль выбирается так:

- если email есть в `ADMIN_EMAILS`, пользователь получает `ADMIN`;
- все остальные получают `RENT`.

Пример:

```env
ADMIN_EMAILS=admin@example.com,owner@example.com
```

## Сборка

Локальная сборка API и frontend:

```powershell
make build
```

Только API:

```powershell
make build-api
```

Только frontend:

```powershell
make build-web
```

Docker-образы:

```powershell
make docker-build
```

## Проверка

Обычные тесты:

```powershell
make test
```

Полный прогон с PostgreSQL/Redis:

```powershell
make test-integration
```

Health endpoints:

```text
GET http://localhost:8080/healthz
GET http://localhost:8080/health/live
GET http://localhost:8080/health/ready
GET http://localhost:8080/metrics
```

Через frontend proxy:

```text
GET http://localhost:5173/healthz
GET http://localhost:5173/api/v1/games
```

## Frontend

Исходники frontend находятся в `web/`.

Основные файлы:

- `web/src/App.tsx` - UI приложения.
- `web/src/api.ts` - typed API client для существующих backend endpoints.
- `web/src/mock.ts` - demo-data fallback, если backend недоступен.
- `web/src/styles.css` - темная адаптивная UI-тема.
- `web/nginx.conf` - production Nginx config для SPA и proxy `/api`.

Frontend использует реальные backend endpoints. Для админского отключения аккаунта используется `PATCH /admin/accounts/{accountId}` со статусом отключения, потому что `DELETE /admin/accounts/{accountId}` в backend сейчас не реализован.

Mock fallback в runtime-фронтенде не используется: если backend недоступен или БД возвращает пустой список, UI показывает ошибку или empty state, а не demo-data.

## Логи в Docker

API пишет логи в `LOGGER_FOLDER`. По умолчанию:

```env
LOGGER_FOLDER=out/logs
```

В Docker Compose папка проброшена так:

```text
./out:/app/out
```

Поэтому логи API из контейнера появляются локально в `out/logs`.

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

## API

Все бизнес-ручки находятся под `/api/v1`.

### Auth

```http
POST {{apiUrl}}/auth/register
POST {{apiUrl}}/auth/login
POST {{apiUrl}}/auth/refresh
POST {{apiUrl}}/auth/logout
GET  {{apiUrl}}/auth/me
```

### Users

```http
GET   {{apiUrl}}/users/{id}
PATCH {{apiUrl}}/users/{id}
```

`RENT` может читать и редактировать только себя. `ADMIN` может работать с другими пользователями.

### Games

```http
GET {{apiUrl}}/games?page=1&page_size=20
GET {{apiUrl}}/games?search=counter&page=1&page_size=20
GET {{apiUrl}}/games/{gameId}
```

### Accounts

```http
GET    {{apiUrl}}/accounts?page=1&page_size=20
GET    {{apiUrl}}/accounts?search=counter&min_price=100&max_price=1000&page=1&page_size=20
GET    {{apiUrl}}/accounts/{accountId}
GET    {{apiUrl}}/accounts/{accountId}/availability
GET    {{apiUrl}}/accounts/{accountId}/reviews
POST   {{apiUrl}}/accounts/{id}/favorite
DELETE {{apiUrl}}/accounts/{id}/favorite
```

### Rentals

```http
POST {{apiUrl}}/rentals
GET  {{apiUrl}}/rentals
GET  {{apiUrl}}/me/rentals
GET  {{apiUrl}}/rentals/{rentalId}
POST {{apiUrl}}/rentals/{rentalId}/cancel
POST {{apiUrl}}/rentals/calculate
POST {{apiUrl}}/rentals/{id}/extend
```

Создание аренды:

```json
{
  "account_id": 1,
  "duration_hours": 2
}
```

### Payments

```http
POST {{apiUrl}}/payments
GET  {{apiUrl}}/payments
GET  {{apiUrl}}/me/payments
GET  {{apiUrl}}/payments/{paymentId}
POST {{apiUrl}}/payments/webhook
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
GET  {{apiUrl}}/accounts/{accountId}/reviews
```

Создание отзыва:

```json
{
  "account_id": 1,
  "rental_id": 1,
  "rating": 5,
  "comment": "Все работает"
}
```

### Notifications

```http
GET   {{apiUrl}}/notifications
GET   {{apiUrl}}/me/notifications
PATCH {{apiUrl}}/notifications/{notificationId}/read
```

### Admin

Нужен access token пользователя с ролью `ADMIN`.

```http
GET   {{apiUrl}}/admin/accounts
POST  {{apiUrl}}/admin/accounts
PATCH {{apiUrl}}/admin/accounts/{accountId}
POST  {{apiUrl}}/admin/accounts/{accountId}/sync
GET   {{apiUrl}}/admin/users
PATCH {{apiUrl}}/admin/users/{userId}
GET   {{apiUrl}}/admin/audit-logs
```

Создание аккаунта:

```json
{
  "steam_id64": "76561198000000001",
  "steam_login": "steam_login",
  "steam_password": "steam_password",
  "price_per_hour": 150,
  "security_deposit": 500
}
```

Назначить пользователю роль:

```json
{
  "role": "ADMIN"
}
```

## Runtime logs

Сообщение ниже не является ошибкой для локального запуска:

```text
STEAM_API_KEY is not set. Falling back to FakeSteamClient for local testing.
```
