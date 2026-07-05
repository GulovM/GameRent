# Architecture

**Version:** 1.0  
**Status:** Approved  
**Last Updated:** July 2026

---

# 1. Purpose

Данный документ описывает архитектуру backend-приложения **GameRent**.

Цель архитектуры — обеспечить:

- высокую модульность;
- простоту сопровождения;
- безопасность;
- масштабируемость;
- тестируемость;
- независимость бизнес-логики от инфраструктуры.

Архитектура ориентирована на **Modular Monolith** с использованием принципов **Domain-Driven Design (DDD)** и **Clean Architecture**.

---

# 2. Architectural Goals

При проектировании системы были определены следующие архитектурные цели.

## Functional

- безопасная аренда Steam-аккаунтов;
- высокая производительность;
- быстрый поиск аккаунтов;
- синхронизация библиотеки Steam;
- централизованное управление аккаунтами.

## Non-Functional

- высокая читаемость кода;
- независимость бизнес-логики;
- простое тестирование;
- возможность горизонтального масштабирования;
- минимальная связанность модулей.

---

# 3. Technology Stack

## Backend

- Go

## Database

- PostgreSQL

## Cache

- Redis

## Logging

- Uber Zap

## Authentication

- JWT

## Configuration

- Environment Variables

## Containerization

- Docker
- Docker Compose

## External Systems

- Steam Web API
- Email Provider

---

# 4. Architectural Style

Проект реализуется как **Modular Monolith**.

Каждый бизнес-домен представляет собой самостоятельный модуль.

Модули взаимодействуют через публичные сервисы и интерфейсы.

Переход на микросервисную архитектуру возможен без переписывания бизнес-логики.

---

# 5. Design Principles

Архитектура строится на следующих принципах.

## SOLID

Используются все принципы SOLID.

Особое внимание уделяется:

- SRP
- DIP

---

## Domain-Oriented Design

Код организуется вокруг бизнес-доменов, а не технических слоёв.

Например:

- Accounts
- Rentals
- Payments
- Games

а не

- Controllers
- Services
- Repositories

---

## Explicit Dependencies

Все зависимости создаются явно.

Используется ручной Dependency Injection.

Пример:

```go
repo := repository.New(...)
service := service.New(repo)
handler := handler.New(service)
```

---

## Composition over Inheritance

Используется композиция структур.

---

## Fail Fast

Ошибки обнаруживаются максимально рано.

---

## Single Source of Truth

Источник истины:

- PostgreSQL

Redis является только кэшем.

Steam Web API является источником данных исключительно для синхронизации библиотеки игр.

---

# 6. High-Level Architecture

```
                   Client
                      │
                      ▼
                 HTTP Server
                      │
                Middleware Layer
                      │
                      ▼
                   Handlers
                      │
                      ▼
                   Services
          ┌───────────┼────────────┐
          ▼           ▼            ▼
     PostgreSQL     Redis     Steam Client
          │                        │
          ▼                        ▼
      Database              Steam Web API

                 ▲
                 │
      Background Workers
```

---

# 7. Application Layers

## Presentation Layer

Ответственность:

- HTTP
- JSON
- Validation
- Authentication
- Routing

Содержит:

- handlers
- middleware
- routes

---

## Application Layer

Ответственность:

- orchestration
- use cases
- бизнес-операции

Содержит:

- services

---

## Domain Layer

Ответственность:

- бизнес-модель;
- инварианты;
- доменные правила;
- агрегаты.

Этот слой не знает о PostgreSQL, Redis или HTTP.

---

## Infrastructure Layer

Ответственность:

- PostgreSQL
- Redis
- Steam API
- Email
- JWT
- Logger

---

# 8. Module Structure

Каждый модуль имеет одинаковую структуру.

```
accounts/

handler.go

service.go

repository.go

repository_postgres.go

dto.go

mapper.go

validator.go

routes.go

errors.go
```

Аналогично:

- auth
- rentals
- games
- payments
- notifications
- reviews
- admin

---

# 9. Dependency Rules

Допустимые зависимости

```
Handler

↓

Service

↓

Repository Interface

↓

Repository

↓

Database
```

Недопустимые зависимости

```
Repository → Handler

Repository → Service

Handler → Database

Service → HTTP
```

---

# 10. Middleware

Используются следующие middleware.

- Recovery
- Request ID
- Logger
- JWT Authentication
- RBAC
- CORS
- Rate Limiter

---

# 11. Repository Pattern

Каждый модуль имеет собственный Repository Interface.

Например

```
AccountRepository

RentalRepository

UserRepository
```

Это позволяет:

- писать unit-тесты;
- менять PostgreSQL без изменения бизнес-логики;
- использовать mock-объекты.

---

# 12. Dependency Injection

Используется исключительно ручной Dependency Injection.

Причины:

- отсутствие скрытой магии;
- простой жизненный цикл объектов;
- прозрачность зависимостей;
- высокая читаемость.

---

# 13. Caching

Redis используется исключительно как кэш.

Кэшируются:

- каталог игр;
- список популярных аккаунтов;
- результаты поиска;
- часто запрашиваемые профили.

Правило:

PostgreSQL всегда является источником истины.

---

# 14. Background Workers

HTTP-запросы никогда не выполняют длительные операции.

Для фоновых задач используются Worker-процессы.

На текущем этапе:

## Steam Synchronization Worker

Ответственность

- синхронизация библиотек игр;
- обновление каталога;
- обновление playtime;
- синхронизация новых игр.

В будущем:

- Email Worker
- Cleanup Worker
- Analytics Worker

---

# 15. Error Handling

Все ошибки разделяются на категории.

Business Errors

Validation Errors

Authentication Errors

Infrastructure Errors

Каждая ошибка преобразуется в единый API Response.

---

# 16. Logging

Используется **Uber Zap**.

Логи являются структурированными.

Обязательные поля:

- request_id
- user_id
- ip_address
- method
- path
- latency
- status_code

Ошибки логируются централизованно.

---

# 17. Configuration

Конфигурация осуществляется через переменные окружения.

Не допускается хранение секретов в исходном коде.

Конфигурация разделяется на:

- Development
- Testing
- Production

---

# 18. Security

Архитектура безопасности включает:

- JWT Authentication
- RBAC
- Password Hashing
- Refresh Token Rotation
- Audit Logging
- Rate Limiting
- Login Protection
- HTTPS
- Steam Guard Verification

Подробное описание находится в `13-security.md`.

---

# 19. External Integrations

## Steam Web API

Используется для:

- получения списка игр;
- синхронизации библиотек;
- обновления каталога.

---

## Email Provider

Используется для:

- подтверждения регистрации;
- уведомлений владельцев аккаунтов;
- уведомлений пользователей;
- уведомлений службы безопасности.

---

# 20. Scalability

При увеличении нагрузки могут быть выделены отдельные сервисы:

- Payments
- Notifications
- Steam Synchronization

Благодаря Modular Monolith это не потребует изменения бизнес-логики.

---

# 21. Testing Strategy

Предусмотрены следующие виды тестирования.

Unit Tests

Repository Tests

Integration Tests

API Tests

End-to-End Tests

---

# 22. Future Improvements

Возможные направления развития.

- OpenTelemetry
- Prometheus
- Grafana
- Distributed Tracing
- RabbitMQ
- NATS
- Kafka
- Kubernetes
- gRPC
- CQRS
- Event-Driven Architecture

Данные технологии не требуются для MVP, однако архитектура допускает их внедрение без значительной переработки существующего кода.