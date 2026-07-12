# Project Structure

**Project:** GameRent

**Language:** Go 1.25+

**Architecture:** Clean Architecture + Domain-Driven Design (DDD)

**Database:** PostgreSQL

**Cache:** Redis

**API:** REST

**Deployment:** Local Go run + Docker Compose dependencies

**Logging:** Zap

**Migrations:** Goose

**Configuration:** .env

---

# 1. Purpose

## 1.1 Goal

Настоящий документ определяет структуру исходного кода проекта GameRent.

Его цель — обеспечить:

* единообразную организацию кода;
* слабую связанность компонентов;
* высокую тестируемость;
* простоту сопровождения;
* масштабируемость проекта.

Структура проекта должна позволять без существенных изменений добавлять новые доменные модули, сервисы и внешние интеграции.

---

# 2. Architectural Principles

При организации проекта используются следующие принципы.

## 2.1 Layered Architecture

Приложение разделяется на независимые слои.

```text
HTTP

↓

Application

↓

Domain

↓

Infrastructure
```

Каждый слой имеет собственную область ответственности.

---

## 2.2 Dependency Rule

Зависимости всегда направлены внутрь системы.

```text
HTTP

↓

Application

↓

Domain

↑

Infrastructure
```

Domain Layer не знает о существовании:

* PostgreSQL;
* Redis;
* HTTP;
* Docker;
* JWT;
* Zap;
* gRPC.

Он содержит исключительно бизнес-логику.

---

## 2.3 High Cohesion

Каждый пакет отвечает только за одну бизнес-задачу.

Например:

```text
internal/rental
```

не должен содержать код пользователей.

---

## 2.4 Low Coupling

Связь между пакетами осуществляется исключительно через интерфейсы.

Запрещается прямой импорт инфраструктурных реализаций.

---

## 2.5 Composition Root

Все зависимости создаются в одном месте приложения.

Для GameRent таким местом является:

```text
cmd/server/main.go
```

Именно здесь создаются:

* PostgreSQL;
* Redis;
* Logger;
* Config;
* HTTP Server;
* Repositories;
* Services;
* Handlers.

---

# 3. Repository Structure

```text
gamerent/

├── cmd/
│
├── internal/
│
├── pkg/
│
├── api/
│
├── configs/
│
├── deployments/
│
├── migrations/
│
├── scripts/
│
├── docs/
│
├── test/
│
├── .github/
│
├── docker-compose.yml
├── Dockerfile
├── Makefile
├── go.mod
└── go.sum
```

---

# 4. Root Directories

## cmd/

Содержит точки входа приложения.

```text
cmd/

    server/

        main.go
```

В будущем допускается добавление:

```text
cmd/

    migrate/

    worker/

    seeder/
```

---

## internal/

Основной исходный код приложения.

Всё, что находится внутри `internal`, не может импортироваться внешними проектами.

Именно здесь располагается вся бизнес-логика.

---

## pkg/

Переиспользуемые компоненты, не зависящие от бизнес-домена.

Например:

```text
pkg/

    validator/

    crypto/

    pagination/

    pointer/

    money/

    errors/
```

Код из `pkg` может использоваться любым модулем приложения.

---

## api/

Хранит спецификацию API.

```text
api/

    openapi.yaml
```

При необходимости могут добавляться:

```text
swagger.json

swagger.yaml
```

---

## configs/

Конфигурационные файлы.

Например:

```text
configs/

    config.yaml

    config.dev.yaml

    config.prod.yaml
```

Все чувствительные параметры передаются через переменные окружения.

---

## deployments/

Файлы развёртывания.

Например:

```text
deployments/

    docker/

    compose/

    nginx/
```

В дальнейшем здесь могут размещаться:

* Kubernetes-манифесты;
* Helm Charts;
* Terraform-конфигурации.

---

## migrations/

SQL-миграции PostgreSQL.

Пример:

```text
000001_create_users.up.sql

000001_create_users.down.sql

000002_create_accounts.up.sql

000002_create_accounts.down.sql
```

Все изменения схемы базы данных выполняются только через миграции.

---

## scripts/

Вспомогательные скрипты.

Например:

```text
scripts/

    seed.sh

    backup.sh

    restore.sh
```

---

## docs/

Проектная документация.

```text
docs/

    vision.md

    srs.md

    architecture.md

    api-design.md

    database-design.md

    domain-model.md

    security.md

    project-structure.md
```

---

## test/

Интеграционные и end-to-end тесты.

Например:

```text
test/

    integration/

    e2e/

    fixtures/
```

---

## .github/

Конфигурация GitHub.

```text
.github/

    workflows/

        ci.yml
```

Здесь располагаются пайплайны непрерывной интеграции.

---

# 5. Internal Structure

Каталог `internal` организован по бизнес-доменам.

```text
internal/

    account/

    auth/

    user/

    rental/

    payment/

    review/

    notification/

    security/

    game/

    shared/
```

Каждый модуль полностью инкапсулирует собственную бизнес-логику.

Модули взаимодействуют только через публичные интерфейсы.

---

# 6. Standard Module Layout

Каждый доменный модуль имеет одинаковую внутреннюю структуру.

Пример:

```text
internal/

    rental/

        dto.go

        entity.go

        service.go

        repository.go

        repository_postgres.go

        handler.go

        mapper.go

        validator.go

        errors.go

        routes.go
```

---

## entity.go

Содержит доменные сущности.

Пример:

```go
type Rental struct {
    ...
}
```

В сущностях отсутствуют зависимости от PostgreSQL, HTTP и Redis.

---

## dto.go

Содержит DTO (Data Transfer Objects).

DTO используются:

* HTTP Handler;
* Application Service;
* Mapper.

Доменные сущности не передаются напрямую наружу.

---

## service.go

Содержит бизнес-логику модуля.

Именно сервисы реализуют сценарии использования (Use Cases).

Сервисы не работают напрямую с PostgreSQL — только через интерфейсы репозиториев.

---

## repository.go

Определяет интерфейс репозитория.

Например:

```go
type Repository interface {
    GetByID(...)
    Create(...)
    Update(...)
}
```

Интерфейс принадлежит доменному модулю, а не инфраструктуре.

---

## repository_postgres.go

Реализация репозитория для PostgreSQL с использованием `pgx`.

Здесь располагаются:

* SQL-запросы;
* работа с транзакциями;
* преобразование строк БД в доменные сущности.

Бизнес-логика в репозитории отсутствует.

---

## handler.go

HTTP-обработчики.

Отвечают только за:

* чтение HTTP-запроса;
* вызов сервиса;
* формирование HTTP-ответа.

Любая бизнес-логика в обработчиках запрещена.

---

## mapper.go

Преобразует:

* DTO → Entity;
* Entity → DTO.

Это позволяет изолировать доменную модель от транспортного слоя.

---

## validator.go

Содержит проверки входных данных.

Проверяется:

* обязательность полей;
* диапазоны значений;
* формат данных.

Бизнес-правила здесь не размещаются.

---

## errors.go

Определяет ошибки модуля.

Например:

```go
var (
    ErrRentalNotFound
    ErrRentalExpired
    ErrRentalAlreadyActive
)
```

Ошибки одного модуля не должны зависеть от другого.

---

## routes.go

Регистрирует HTTP-маршруты модуля.

Маршрутизация сосредоточена внутри самого модуля, что упрощает масштабирование и подключение новых компонентов.

---
# Project Structure

## Part 2 — Shared Infrastructure, Configuration and Common Components

---

# 7. Shared Module

Каталог `internal/shared` содержит компоненты, используемые несколькими доменными модулями, но относящиеся к внутренней архитектуре приложения.

```text
internal/

    shared/

        config/

        database/

        cache/

        logger/

        middleware/

        auth/

        response/

        errors/

        validator/

        transaction/

        scheduler/

        events/

        mail/

        storage/

        clock/
```

Каждый пакет отвечает только за одну инфраструктурную задачу.

---

# 8. Configuration

## internal/shared/config

Отвечает за загрузку конфигурации приложения.

```text
config/

    config.go

    loader.go

    validation.go
```

Используется библиотека **Viper**.

Поддерживаются три источника конфигурации:

* YAML-файлы;
* переменные окружения;
* значения по умолчанию.

Приоритет:

```text
Environment Variables

↓

Configuration File

↓

Default Values
```

Все секреты (JWT Secret, ключ шифрования Steam-аккаунтов, SMTP-пароли и т.п.) должны передаваться исключительно через переменные окружения.

---

# 9. PostgreSQL

## internal/shared/database

Отвечает за подключение к PostgreSQL.

```text
database/

    postgres.go

    migrations.go

    transaction.go
```

Используется:

* pgxpool
* pgx
* golang-migrate

В приложении создаётся один общий пул соединений.

Повторное создание пула запрещено.

---

## Transaction Manager

Все сервисы получают интерфейс управления транзакциями.

Пример:

```go
type TxManager interface {
    WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}
```

Это позволяет скрыть детали работы с PostgreSQL и упростить тестирование.

---

# 10. Redis

## internal/shared/cache

Отвечает за работу с Redis.

```text
cache/

    redis.go

    keys.go

    cache.go
```

Redis используется только как кэш.

Запрещается хранить данные, потеря которых приводит к нарушению бизнес-логики.

Кэшируются:

* карточки аккаунтов;
* список популярных игр;
* результаты поиска;
* публичные профили аккаунтов;
* часто используемые справочники.

---

# 11. Logger

## internal/shared/logger

Используется библиотека **Zap**.

```text
logger/

    logger.go
```

Создаётся один экземпляр логгера на всё приложение.

Логгер передаётся через dependency injection.

---

## Logging Levels

Используются уровни:

```text
DEBUG

INFO

WARN

ERROR

FATAL
```

---

## Structured Logging

Все сообщения записываются в структурированном формате.

Пример:

```go
logger.Info(
    "rental created",
    zap.Int64("rental_id", rental.ID),
    zap.Int64("user_id", rental.UserID),
)
```

Использование форматированных строк (`fmt.Sprintf`) внутри логов не рекомендуется.

---

# 12. Middleware

## internal/shared/middleware

```text
middleware/

    auth.go

    logging.go

    recovery.go

    request_id.go

    rate_limit.go

    cors.go

    timeout.go
```

---

## Auth Middleware

Проверяет:

* JWT;
* срок действия токена;
* существование пользователя.

---

## Request ID Middleware

Каждому запросу присваивается уникальный идентификатор.

Идентификатор передаётся:

* в логах;
* в ответах API;
* во внутренние сервисы.

Это упрощает трассировку запросов.

---

## Recovery Middleware

Перехватывает panic.

Возвращает HTTP 500.

Записывает стек вызовов в лог.

---

## Logging Middleware

Логирует:

* метод;
* путь;
* статус ответа;
* длительность обработки;
* Request ID;
* IP клиента.

---

## Timeout Middleware

Каждый HTTP-запрос выполняется с ограничением времени.

По умолчанию:

```text
30 seconds
```

---

## Rate Limiter

Используется Token Bucket.

Ограничиваются:

* авторизация;
* регистрация;
* восстановление пароля;
* публичные API.

---

# 13. Authentication

## internal/shared/auth

```text
auth/

    jwt.go

    password.go

    claims.go

    token.go
```

---

Используются:

* Access Token;
* Refresh Token Rotation.

---

Пароли пользователей:

```text
Argon2id
```

или

```text
bcrypt
```

(предпочтительно Argon2id).

---

JWT содержит только минимальный набор данных:

* UserID;
* TokenID;
* Expiration Time.

Никакая бизнес-информация в JWT не хранится.

---

# 14. Error Handling

## internal/shared/errors

```text
errors/

    app_error.go

    mapper.go
```

Все ошибки приложения наследуются от общей модели.

Пример:

```go
type AppError struct {
    Code    string
    Message string
}
```

---

Ошибки разделяются на категории:

* Validation
* Unauthorized
* Forbidden
* NotFound
* Conflict
* Internal

Это позволяет единообразно преобразовывать их в HTTP-ответы.

---

# 15. API Response

## internal/shared/response

Все HTTP-ответы имеют единый формат.

Успешный ответ:

```json
{
    "data": {}
}
```

Ошибка:

```json
{
    "error": {
        "code": "ACCOUNT_NOT_FOUND",
        "message": "Account not found"
    }
}
```

Это обеспечивает единообразие API.

---

# 16. Validation

## internal/shared/validator

Валидация выполняется до передачи данных в сервисный слой.

Проверяются:

* обязательность полей;
* формат email;
* длина строк;
* диапазоны значений;
* integer path parameters (`BIGSERIAL` / `int64` IDs);
* даты.

Бизнес-правила валидатор не проверяет.

---

# 17. Mail

## internal/shared/mail

```text
mail/

    sender.go

    template.go

    smtp.go
```

Отвечает за отправку:

* подтверждения email;
* уведомлений владельцу Steam-аккаунта;
* уведомлений о завершении аренды;
* уведомлений безопасности;
* восстановления пароля.

Шаблоны писем хранятся отдельно от Go-кода.

---

# 18. Scheduler

## internal/shared/scheduler

```text
scheduler/

    scheduler.go

    jobs.go
```

Фоновые задачи:

* завершение просроченных аренд;
* очистка Refresh Token;
* очистка кэша;
* синхронизация библиотеки Steam;
* повторная проверка аккаунтов;
* отправка отложенных уведомлений.

Все задачи должны быть идемпотентными (повторный запуск не должен приводить к некорректному состоянию системы).

---

# 19. Clock

## internal/shared/clock

```text
clock/

    clock.go
```

Вместо прямого вызова `time.Now()` используется интерфейс:

```go
type Clock interface {
    Now() time.Time
}
```

Это значительно упрощает тестирование логики, зависящей от времени.

---

# 20. Storage

## internal/shared/storage

Пакет предназначен для работы с внешними файлами.

В MVP используется только для хранения:

* аватаров игр;
* временных файлов;
* экспортируемых отчётов.

Архитектура позволяет в будущем подключить:

* S3;
* MinIO;
* Google Cloud Storage.

Без изменения бизнес-логики.

---

# 21. Events

## internal/shared/events

На текущем этапе Event Bus не используется.

Пакет содержит только определения доменных событий и интерфейсы.

Это позволит в будущем подключить:

* Kafka;
* RabbitMQ;
* NATS;

без изменения доменной модели и сервисов.

---

# 22. Dependency Injection

Создание зависимостей происходит только в Composition Root.

Запрещается:

* создавать PostgreSQL внутри сервисов;
* создавать Redis внутри обработчиков;
* создавать Logger внутри модулей;
* использовать глобальные переменные.

Все зависимости передаются через конструкторы.

Пример:

```go
func NewRentalService(
    repo Repository,
    payment PaymentService,
    security SecurityService,
    tx TxManager,
    logger *zap.Logger,
) *Service
```

Это делает зависимости явными и облегчает тестирование.

---

# Project Structure

## Part 3 — Business Modules, Testing, Deployment and Development Standards

---

# 23. Business Modules

Каждый бизнес-модуль представляет собой законченную функциональную область (Bounded Context) и содержит всё необходимое для своей работы.

Модули взаимодействуют только через публичные интерфейсы.

---

# 24. Auth Module

```text
internal/

    auth/

        dto.go
        entity.go
        service.go
        repository.go
        repository_postgres.go
        handler.go
        mapper.go
        validator.go
        routes.go
        errors.go
```

## Responsibilities

Отвечает за:

* регистрацию пользователей;
* авторизацию;
* обновление Access Token;
* отзыв Refresh Token;
* подтверждение электронной почты;
* восстановление пароля;
* смену пароля.

Не отвечает за:

* управление аккаунтами Steam;
* аренды;
* платежи.

---

# 25. User Module

```text
internal/

    user/
```

## Responsibilities

Отвечает за:

* профиль пользователя;
* Trust Score;
* получение истории аренд;
* изменение личных данных.

---

# 26. Account Module

```text
internal/

    account/
```

## Responsibilities

Отвечает за:

* регистрацию Steam-аккаунтов;
* проверку безопасности;
* синхронизацию библиотеки игр;
* публикацию аккаунтов;
* изменение стоимости аренды;
* изменение депозита;
* вывод аккаунта из публикации.

Не отвечает за:

* оплату;
* JWT;
* авторизацию.

---

# 27. Rental Module

```text
internal/

    rental/
```

Центральный модуль проекта.

## Responsibilities

* создание аренды;
* активация аренды;
* запрет неоплаченного продления и future paid-extension boundary;
* завершение аренды;
* отмена аренды;
* расчёт длительности;
* проверка доступности аккаунта.

Практически вся бизнес-логика проекта сосредоточена именно здесь.

---

# 28. Payment Module

```text
internal/

    payment/
```

Отвечает исключительно за финансовые операции.

Функции:

* создание платежа;
* подтверждение оплаты;
* удержание депозита;
* возврат депозита;
* возврат средств;
* история платежей.

---

# 29. Review Module

```text
internal/

    review/
```

Отвечает за:

* публикацию отзывов;
* получение отзывов;
* расчёт среднего рейтинга аккаунта.

---

# 30. Notification Module

```text
internal/

    notification/
```

Отвечает за создание уведомлений.

Каналы доставки:

* Email;
* WebSocket;
* Push (будущее развитие).

---

# 31. Security Module

```text
internal/

    security/
```

Один из ключевых модулей системы.

## Responsibilities

* журналирование событий безопасности;
* расчёт Trust Score;
* обнаружение подозрительной активности;
* фиксация входов;
* отправка уведомлений владельцам Steam-аккаунтов;
* ведение истории безопасности.

Этот модуль не принимает решений об авторизации — только анализирует события и предоставляет данные другим сервисам.

---

# 32. Game Module

```text
internal/

    game/
```

Отвечает за:

* каталог игр;
* синхронизацию Steam AppID;
* поиск игр;
* получение карточек игр.

---

# 33. Import Rules

Для сохранения слабой связанности устанавливаются следующие правила.

Разрешены зависимости:

```text
handler
    ↓
service
    ↓
repository interface
```

```text
repository_postgres
    ↓
pgx
```

```text
service
    ↓
other service interfaces
```

Запрещены зависимости:

```text
handler
    ↓
repository_postgres
```

```text
entity
    ↓
postgres
```

```text
entity
    ↓
redis
```

```text
entity
    ↓
http
```

Нарушение этих правил считается архитектурной ошибкой.

---

# 34. Testing Strategy

Проект покрывается несколькими уровнями тестирования.

## Unit Tests

Проверяют:

* бизнес-логику;
* доменные сущности;
* сервисы;
* валидаторы.

Используются mock-реализации интерфейсов.

---

## Integration Tests

Проверяют:

* PostgreSQL;
* Redis;
* репозитории;
* транзакции.

Тесты выполняются в отдельных Docker-контейнерах.

---

## API Tests

Проверяют:

* HTTP-маршруты;
* middleware;
* сериализацию;
* авторизацию.

---

## End-to-End Tests

Проверяют полный пользовательский сценарий.

Например:

```text
Регистрация

↓

Подтверждение Email

↓

Авторизация

↓

Создание аренды

↓

Оплата

↓

Завершение аренды
```

---

# 35. Docker Structure

```text
Dockerfile

docker-compose.yml
```

Контейнеры MVP:

```text
app

postgres

redis
```

Дополнительно могут быть добавлены:

```text
mailhog

adminer

prometheus

grafana
```

Все сервисы объединяются одной внутренней Docker-сетью.

---

# 36. Makefile

Все часто используемые команды должны быть доступны через `make`.

Минимальный набор целей:

```text
make build

make run

make test

make test-integration

make up

make infra-up

make down

make docker-build

make fmt

make clean

make logs

make ps
```

Разработчик не должен вручную вводить длинные команды.

---

# 37. Static Analysis

Перед каждым коммитом выполняются:

* gofmt;
* goimports;
* golangci-lint;
* go test.

Код, не прошедший проверки, не должен попадать в основную ветку.

---

# 38. Git Workflow

Основная ветка:

```text
main
```

Разработка ведётся через feature-ветки.

Пример:

```text
feature/auth

feature/rental

feature/payment

feature/reviews
```

Каждое изменение проходит Pull Request и код-ревью.

---

# 39. Continuous Integration

При каждом Pull Request автоматически выполняются:

1. Проверка форматирования.
2. Сборка проекта.
3. Запуск линтера.
4. Запуск Unit-тестов.
5. Запуск Integration-тестов.
6. Проверка миграций.
7. Проверка покрытия тестами.

Только после успешного прохождения всех этапов изменения могут быть объединены с основной веткой.

---

# 40. Environment Variables

Конфигурация приложения должна полностью поддерживать запуск через переменные окружения.

Пример:

```text
APP_ENV

SERVER_PORT

POSTGRES_HOST
POSTGRES_PORT
POSTGRES_DB
POSTGRES_USER
POSTGRES_PASSWORD

REDIS_HOST
REDIS_PORT

JWT_SECRET

STEAM_CREDENTIALS_KEY

SMTP_HOST
SMTP_PORT
SMTP_USERNAME
SMTP_PASSWORD

LOG_LEVEL
```

Никакие секреты не должны храниться в репозитории.

---

# 41. Graceful Shutdown

При завершении приложения необходимо:

1. Прекратить приём новых HTTP-запросов.
2. Дождаться завершения текущих запросов.
3. Остановить фоновые задачи.
4. Закрыть соединение с Redis.
5. Закрыть пул соединений PostgreSQL.
6. Синхронно записать оставшиеся сообщения логгера (`logger.Sync()`).
7. Завершить работу процесса.

Это предотвращает потерю данных и обеспечивает корректное завершение транзакций.

---

# 42. Code Style

Для проекта устанавливаются следующие правила.

* Используется стандартное форматирование Go (`gofmt`).
* Имена пакетов — короткие, существительные, в нижнем регистре.
* Один пакет — одна ответственность.
* Интерфейсы объявляются там, где они используются.
* Предпочтение отдаётся композиции, а не наследованию.
* Ошибки возвращаются как значения (`error`), а не через исключения.
* Контекст (`context.Context`) передаётся первым аргументом во все публичные методы, выполняющие ввод-вывод.

---

# 43. Package Design Guidelines

Каждый пакет должен быть максимально автономным.

Допустимо добавлять новые файлы без изменения существующей структуры.

При росте функциональности допускается создание подпакетов только в случае явной необходимости. Преждевременное дробление структуры не рекомендуется.

---

# 44. Scalability

Текущая структура позволяет без рефакторинга добавить:

* gRPC API;
* GraphQL API;
* WebSocket Gateway;
* административную панель;
* мобильный Backend;
* очереди сообщений (Kafka, RabbitMQ, NATS);
* отдельные фоновые воркеры;
* микросервисную архитектуру (при необходимости).

Все изменения будут затрагивать инфраструктурный слой, не нарушая доменную модель и бизнес-логику.

---

# 45. Conclusion

Структура проекта GameRent разработана с учётом принципов Clean Architecture и Domain-Driven Design.

Она обеспечивает:

* чёткое разделение ответственности между слоями;
* независимость бизнес-логики от инфраструктуры;
* высокую тестируемость;
* удобство сопровождения;
* предсказуемую организацию исходного кода;
* готовность к масштабированию.

Любые новые функции должны разрабатываться в рамках данной структуры. Изменение архитектурных правил допускается только после обновления соответствующей проектной документации.
