# Database Design

**Project:** GameRent
**Database:** PostgreSQL
**Version:** 1.0
**Status:** Approved

---

# 1. Purpose

## 1.1 Overview

Данный документ описывает структуру базы данных платформы **GameRent**.

Цель документа:

* определить модель хранения данных;
* описать все таблицы и связи между ними;
* определить ограничения целостности данных;
* определить стратегию индексации;
* зафиксировать бизнес-правила, реализуемые средствами PostgreSQL.

Данный документ является реализацией доменной модели, описанной в `domain-model.md`.

---

# 2. Database Overview

В качестве основной СУБД используется PostgreSQL.

PostgreSQL выбран благодаря:

* поддержке ACID-транзакций;
* высокой надёжности;
* богатому набору ограничений целостности;
* эффективной работе с индексами;
* поддержке JSONB;
* хорошей интеграции с Go (`pgx`);
* зрелой экосистеме.

Redis используется исключительно как кэш и не является источником истины.

---

# 3. Design Principles

При проектировании базы данных используются следующие принципы.

## 3.1 Source of Truth

Единственным источником истины является PostgreSQL.

Все изменения данных сначала фиксируются в PostgreSQL и только затем при необходимости кэшируются в Redis.

---

## 3.2 Third Normal Form (3NF)

Основные таблицы проектируются в соответствии с третьей нормальной формой.

Это позволяет:

* избежать дублирования данных;
* уменьшить вероятность аномалий обновления;
* повысить согласованность данных.

Допустимые случаи денормализации должны быть явно обоснованы.

---

## 3.3 Immutable History

Исторические данные не изменяются.

Например:

* завершённые аренды;
* завершённые платежи;
* события безопасности.

При необходимости создаются новые записи, а не изменяются существующие.

---

## 3.4 Explicit Constraints

Максимальное количество бизнес-правил должно контролироваться самой базой данных.

Используются:

* PRIMARY KEY;
* FOREIGN KEY;
* UNIQUE;
* CHECK;
* NOT NULL.

---

## 3.5 Soft Delete

Для большинства бизнес-сущностей физическое удаление не используется.

Вместо этого применяется поле:

```sql
deleted_at TIMESTAMP NULL
```

Это позволяет:

* восстановить данные;
* сохранить историю;
* исключить потерю связанной информации.

---

# 4. Entity Relationship Overview

Основные сущности системы:

```text
User
 │
 ├──────────────┐
 │              │
 ▼              ▼
Rental       Review
 │
 ▼
Payment

Account
 │
 ├───────────────┐
 │               │
 ▼               ▼
AccountGame     SecurityEvent

Game

User
 │
 ▼
RefreshToken

User
 │
 ▼
Notification

AuditLog
```

---

# 5. Naming Conventions

## Tables

Используются имена во множественном числе.

Примеры:

```text
users

accounts

games

rentals

payments

reviews
```

---

## Primary Keys

Во всех таблицах используется:

```sql
id BIGSERIAL PRIMARY KEY
```

---

## Foreign Keys

Все внешние ключи имеют вид

```text
user_id

account_id

rental_id

payment_id
```

---

## Timestamps

Во всех основных таблицах присутствуют поля

```sql
created_at TIMESTAMP NOT NULL

updated_at TIMESTAMP NOT NULL
```

При использовании Soft Delete добавляется

```sql
deleted_at TIMESTAMP NULL
```

---

# 6. Table: users

## Purpose

Хранит зарегистрированных пользователей платформы.

---

## Columns

| Column         | Type           | Description              |
| -------------- | -------------- | ------------------------ |
| id             | BIGSERIAL      | Primary Key              |
| email          | VARCHAR(255)   | Email пользователя       |
| password_hash  | TEXT           | Хэш пароля               |
| first_name     | VARCHAR(100)   | Имя                      |
| last_name      | VARCHAR(100)   | Фамилия                  |
| email_verified | BOOLEAN        | Подтверждение email      |
| trust_score    | INTEGER        | Уровень доверия (0–1000) |
| is_blocked     | BOOLEAN        | Блокировка пользователя  |
| created_at     | TIMESTAMP      | Дата регистрации         |
| updated_at     | TIMESTAMP      | Дата изменения           |
| deleted_at     | TIMESTAMP NULL | Soft Delete              |

---

## Constraints

```sql
UNIQUE(email)

CHECK(trust_score >= 0)

CHECK(trust_score <= 1000)
```

---

## Indexes

```sql
INDEX(email)

INDEX(trust_score)
```

---

## Notes

Trust Level вычисляется приложением на основании `trust_score`.

База данных хранит только числовое значение.

---

# 7. Table: accounts

## Purpose

Хранит Steam-аккаунты, доступные для аренды.

---

## Columns

### Identity

| Column     | Type        |
| ---------- | ----------- |
| id         | BIGSERIAL   |
| steam_id64 | VARCHAR(32) |
| login      | TEXT        |

---

### Security

| Column              | Type      |
| ------------------- | --------- |
| encrypted_password  | BYTEA     |
| steam_guard_enabled | BOOLEAN   |
| inventory_verified  | BOOLEAN   |
| last_security_check | TIMESTAMP |

---

### Rental

| Column         | Type     |
| -------------- | -------- |
| hourly_price   | BIGINT   |
| deposit_amount | BIGINT   |
| status         | SMALLINT |

---

### Synchronization

| Column            | Type      |
| ----------------- | --------- |
| profile_url       | TEXT      |
| avatar_url        | TEXT      |
| library_synced_at | TIMESTAMP |

---

### Metadata

| Column     | Type           |
| ---------- | -------------- |
| created_at | TIMESTAMP      |
| updated_at | TIMESTAMP      |
| deleted_at | TIMESTAMP NULL |

---

## Constraints

```sql
UNIQUE(steam_id64)

CHECK(hourly_price > 0)

CHECK(deposit_amount >= 0)
```

---

## Status Values

| Value | Meaning     |
| ----- | ----------- |
| 0     | Created     |
| 1     | Verifying   |
| 2     | Available   |
| 3     | Reserved    |
| 4     | Rented      |
| 5     | Maintenance |
| 6     | Disabled    |

---

## Indexes

```sql
INDEX(status)

INDEX(library_synced_at)

INDEX(steam_id64)
```

---

## Partial Index

```sql
CREATE INDEX idx_accounts_available
ON accounts(id)
WHERE status = 2;
```

Это ускоряет поиск аккаунтов, доступных для аренды.

---

# 8. Table: games

## Purpose

Содержит единый каталог игр Steam.

Каждая игра хранится только один раз.

---

## Columns

| Column            | Type      |
| ----------------- | --------- |
| id                | BIGSERIAL |
| steam_app_id      | INTEGER   |
| name              | TEXT      |
| short_description | TEXT      |
| header_image      | TEXT      |
| release_date      | DATE NULL |
| developers        | JSONB     |
| publishers        | JSONB     |
| genres            | JSONB     |
| created_at        | TIMESTAMP |
| updated_at        | TIMESTAMP |

---

## Constraints

```sql
UNIQUE(steam_app_id)
```

---

## Indexes

```sql
INDEX(steam_app_id)

INDEX(name)
```

---

## Notes

Информация обновляется через Steam Web API.

Редактирование вручную не допускается.

---

# 9. Table: account_games

## Purpose

Связующая таблица между аккаунтами и играми.

Позволяет одному аккаунту содержать множество игр, а одной игре принадлежать множеству аккаунтов.

---

## Columns

| Column           | Type      |
| ---------------- | --------- |
| account_id       | BIGINT    |
| game_id          | BIGINT    |
| playtime_minutes | INTEGER   |
| created_at       | TIMESTAMP |

---

## Primary Key

```sql
PRIMARY KEY(account_id, game_id)
```

---

## Foreign Keys

```sql
account_id
REFERENCES accounts(id)

game_id
REFERENCES games(id)
```

---

## Constraints

```sql
CHECK(playtime_minutes >= 0)
```

---

## Indexes

```sql
INDEX(game_id)

INDEX(account_id)
```

---

# 10. Current Relationships

На текущем этапе определены следующие связи:

* Один пользователь может иметь множество аренд (`users 1:N rentals`).
* Один Steam-аккаунт может участвовать во множестве аренд в разные моменты времени (`accounts 1:N rentals`).
* Один аккаунт содержит множество игр (`accounts N:M games` через `account_games`).
* Каждая игра может принадлежать множеству аккаунтов.
* Пользователь может получать множество уведомлений.
* Пользователь может иметь несколько refresh-токенов.
* Пользователь может создавать множество событий безопасности.

Следующие таблицы будут описаны в следующих разделах документа:

* `rentals`;
* `payments`;
* `reviews`;
* `security_events`;
* `audit_logs`;
* `refresh_tokens`;
* `notifications`.

---

# Database Design

## Part 2 — Core Business Tables

---

# 11. Table: rentals

## Purpose

Таблица `rentals` является центральной таблицей базы данных и отражает факт временного предоставления пользователю доступа к конкретному Steam-аккаунту.

Каждая запись представляет собой одну завершённую или текущую аренду.

История аренд никогда не удаляется.

---

## Columns

| Column              | Type           | Description              |
| ------------------- | -------------- | ------------------------ |
| id                  | BIGSERIAL      | Primary Key              |
| user_id             | BIGINT         | Пользователь             |
| account_id          | BIGINT         | Арендуемый аккаунт       |
| status              | SMALLINT       | Текущее состояние аренды |
| start_at            | TIMESTAMP      | Начало аренды            |
| end_at              | TIMESTAMP      | Окончание аренды         |
| rental_price        | BIGINT         | Стоимость аренды         |
| deposit_amount      | BIGINT         | Размер депозита          |
| payment_expires_at  | TIMESTAMP      | Дедлайн оплаты           |
| actual_finished_at  | TIMESTAMP NULL | Фактическое завершение   |
| cancellation_reason | TEXT NULL      | Причина отмены           |
| created_at          | TIMESTAMP      | Создание записи          |
| updated_at          | TIMESTAMP      | Последнее изменение      |

---

## Status Values

| Value | State          |
| ----: | -------------- |
|     0 | Created        |
|     1 | WaitingPayment |
|     2 | Active         |
|     3 | Expired        |
|     4 | Completed      |
|     5 | Cancelled      |

---

## Foreign Keys

```sql
FOREIGN KEY(user_id)
REFERENCES users(id)

FOREIGN KEY(account_id)
REFERENCES accounts(id)
```

---

## Constraints

```sql
CHECK(start_at < end_at)

CHECK(rental_price > 0)

CHECK(deposit_amount >= 0)
```

---

## Partial Unique Index

```sql
CREATE UNIQUE INDEX uq_rental_account_waiting_or_active
ON rentals(account_id)
WHERE status IN (1, 2);
```

Данный индекс гарантирует невозможность существования двух `WAITING_PAYMENT`/`ACTIVE` аренд одного аккаунта одновременно.

---

## Additional Indexes

```sql
INDEX(user_id)

INDEX(account_id)

INDEX(status)

INDEX(start_at)

INDEX(end_at)
```

---

# 12. Table: payments

## Purpose

Хранит все финансовые операции системы.

Одна аренда может иметь несколько платежей.

Например:

* оплата аренды;
* удержание депозита;
* возврат депозита;
* полный возврат средств.

---

## Columns

| Column                  | Type           |
| ----------------------- | -------------- |
| id                      | BIGSERIAL      |
| rental_id               | BIGINT         |
| user_id                 | BIGINT         |
| payment_type            | SMALLINT       |
| provider                | TEXT           |
| status                  | SMALLINT       |
| amount                  | BIGINT         |
| currency                | CHAR(3)        |
| external_transaction_id | TEXT NULL      |
| created_at              | TIMESTAMP      |
| processed_at            | TIMESTAMP NULL |

---

## Payment Types

| Value | Meaning         |
| ----: | --------------- |
|     0 | Rental Payment  |
|     1 | Deposit Hold    |
|     2 | Deposit Release |
|     3 | Refund          |

---

## Status

| Value | Meaning    |
| ----: | ---------- |
|     0 | Created    |
|     1 | Pending    |
|     2 | Success    |
|     3 | Failed     |

В текущем rental lifecycle используются `Pending`, `Success` и `Failed`. Отдельный refund/deposit ledger пока не реализован.

---

## Constraints

```sql
CHECK(amount > 0)

CHECK(currency IN ('USD','EUR','RUB','TJS'))

UNIQUE(provider, external_transaction_id)
WHERE external_transaction_id IS NOT NULL
```

---

## Foreign Keys

```sql
FOREIGN KEY(rental_id)
REFERENCES rentals(id)

FOREIGN KEY(user_id)
REFERENCES users(id)
```

---

## Indexes

```sql
INDEX(rental_id)

INDEX(user_id)

INDEX(status)

INDEX(payment_type)
```

`provider` и `external_transaction_id` используются для идемпотентной обработки payment webhook.

---

# 13. Table: reviews

## Purpose

Отзывы пользователей после завершения аренды.

---

## Columns

| Column     | Type      |
| ---------- | --------- |
| id         | BIGSERIAL |
| rental_id  | BIGINT    |
| user_id    | BIGINT    |
| account_id | BIGINT    |
| rating     | SMALLINT  |
| comment    | TEXT      |
| created_at | TIMESTAMP |

---

## Constraints

```sql
CHECK(rating BETWEEN 1 AND 5)
```

---

```sql
UNIQUE(rental_id)
```

Одна аренда — один отзыв.

---

## Foreign Keys

```sql
FOREIGN KEY(rental_id)
REFERENCES rentals(id)

FOREIGN KEY(user_id)
REFERENCES users(id)

FOREIGN KEY(account_id)
REFERENCES accounts(id)
```

---

## Indexes

```sql
INDEX(account_id)

INDEX(user_id)

INDEX(rating)
```

---

# 14. Table: security_events

## Purpose

Фиксирует события безопасности.

Таблица используется для:

* расследования инцидентов;
* антифрод-аналитики;
* расчёта Trust Score;
* уведомления владельцев аккаунтов;
* внутреннего аудита.

История никогда не удаляется.

---

## Columns

| Column     | Type              |
| ---------- | ----------------- |
| id         | BIGSERIAL         |
| user_id    | BIGINT NULL       |
| account_id | BIGINT NULL       |
| rental_id  | BIGINT NULL       |
| event_type | SMALLINT          |
| ip_address | INET              |
| user_agent | TEXT              |
| country    | VARCHAR(100) NULL |
| city       | VARCHAR(100) NULL |
| success    | BOOLEAN           |
| metadata   | JSONB             |
| created_at | TIMESTAMP         |

---

## Event Types

| Value | Description          |
| ----: | -------------------- |
|     0 | Login Attempt        |
|     1 | Steam Guard Request  |
|     2 | Rental Started       |
|     3 | Rental Finished      |
|     4 | Suspicious Activity  |
|     5 | Account Verification |
|     6 | Security Incident    |

---

## Foreign Keys

```sql
FOREIGN KEY(user_id)
REFERENCES users(id)

FOREIGN KEY(account_id)
REFERENCES accounts(id)

FOREIGN KEY(rental_id)
REFERENCES rentals(id)
```

---

## Indexes

```sql
INDEX(user_id)

INDEX(account_id)

INDEX(event_type)

INDEX(created_at)

INDEX(ip_address)
```

---

# 15. Table: audit_logs

## Purpose

Хранит историю административных действий.

В отличие от `security_events`, эта таблица отражает изменения данных, выполненные администраторами или системой.

---

## Columns

| Column        | Type         |
| ------------- | ------------ |
| id            | BIGSERIAL    |
| actor_user_id | BIGINT NULL  |
| entity_type   | VARCHAR(50)  |
| entity_id     | BIGINT       |
| action        | VARCHAR(100) |
| old_values    | JSONB        |
| new_values    | JSONB        |
| created_at    | TIMESTAMP    |

---

## Indexes

```sql
INDEX(actor_user_id)

INDEX(entity_type)

INDEX(entity_id)

INDEX(created_at)
```

---

# 16. Table: refresh_tokens

## Purpose

Хранит Refresh Token пользователей.

Используется механизм ротации токенов (Refresh Token Rotation).

---

## Columns

| Column     | Type           |
| ---------- | -------------- |
| id         | BIGSERIAL      |
| user_id    | BIGINT         |
| token_hash | TEXT           |
| expires_at | TIMESTAMP      |
| revoked_at | TIMESTAMP NULL |
| created_at | TIMESTAMP      |

---

## Constraints

```sql
CHECK(expires_at > created_at)
```

---

## Foreign Keys

```sql
FOREIGN KEY(user_id)
REFERENCES users(id)
ON DELETE CASCADE
```

---

## Indexes

```sql
INDEX(user_id)

INDEX(expires_at)
```

---

# 17. Table: notifications

## Purpose

Хранит пользовательские уведомления.

Система позволяет отправлять уведомления через различные каналы (например, Email, WebSocket, Push). Таблица хранит сам факт уведомления и его статус доставки.

---

## Columns

| Column     | Type           |
| ---------- | -------------- |
| id         | BIGSERIAL      |
| user_id    | BIGINT         |
| type       | SMALLINT       |
| title      | VARCHAR(255)   |
| body       | TEXT           |
| is_read    | BOOLEAN        |
| sent_at    | TIMESTAMP NULL |
| created_at | TIMESTAMP      |

---

## Notification Types

| Value | Description         |
| ----: | ------------------- |
|     0 | Email Verification  |
|     1 | Rental Activated    |
|     2 | Rental Completed    |
|     3 | Deposit Released    |
|     4 | Security Alert      |
|     5 | System Notification |

---

## Foreign Keys

```sql
FOREIGN KEY(user_id)
REFERENCES users(id)
ON DELETE CASCADE
```

---

## Indexes

```sql
INDEX(user_id)

INDEX(is_read)

INDEX(created_at)
```

---

# 18. Transaction Boundaries

Следующие операции всегда выполняются внутри транзакции PostgreSQL:

* создание аренды;
* изменение статуса аккаунта;
* создание платежа;
* активация аренды;
* завершение аренды;
* возврат депозита.

Это гарантирует атомарность (выполнение "всё или ничего") и предотвращает появление частично сохранённых данных.

---

# 19. Data Retention Policy

Разные категории данных имеют различный жизненный цикл.

| Entity          | Retention Policy                                              |
| --------------- | ------------------------------------------------------------- |
| Rentals         | Никогда не удаляются                                          |
| Payments        | Никогда не удаляются                                          |
| Security Events | Никогда не удаляются                                          |
| Audit Logs      | Никогда не удаляются                                          |
| Reviews         | Никогда не удаляются                                          |
| Refresh Tokens  | Удаляются после истечения срока действия по фоновому процессу |
| Notifications   | Могут архивироваться после заданного периода хранения         |
 
---

# Database Design

## Part 3 — Constraints, Performance and Operational Guidelines

---

# 20. Complete Entity Relationship Diagram

Ниже представлена логическая схема взаимосвязей основных таблиц.

```text
                    users
                      │
        ┌─────────────┼─────────────┐
        │             │             │
        ▼             ▼             ▼
   refresh_tokens   rentals    notifications
                        │
        ┌───────────────┼────────────────┐
        ▼               ▼                ▼
    payments         reviews      security_events
                        │
                        ▼
                    accounts
                        │
             ┌──────────┴──────────┐
             ▼                     ▼
       account_games            audit_logs
             │
             ▼
           games
```

---

# 21. Foreign Key Strategy

Все связи между сущностями реализуются через внешние ключи.

Основные правила:

* запись не может ссылаться на несуществующую сущность;
* история должна сохраняться;
* удаление родительских записей ограничено.

## ON DELETE CASCADE

Используется только там, где потеря дочерних данных безопасна.

Например:

```text
users
    ↓
refresh_tokens
```

или

```text
users
    ↓
notifications
```

---

## ON DELETE RESTRICT

Используется для бизнес-критичных данных.

Например:

```text
users
    ↓
rentals
```

Пользователь не может быть удалён, если существует история аренд.

---

# 22. Business Rules Enforced by Database

Помимо проверки в Go-коде часть инвариантов обеспечивается средствами PostgreSQL.

| Business Rule                      | Database Mechanism   |
| ---------------------------------- | -------------------- |
| Email пользователя уникален        | UNIQUE               |
| SteamID64 уникален                 | UNIQUE               |
| Стоимость аренды > 0               | CHECK                |
| Депозит ≥ 0                        | CHECK                |
| Trust Score в диапазоне 0–1000     | CHECK                |
| Оценка отзыва 1–5                  | CHECK                |
| Дата окончания аренды позже начала | CHECK                |
| Один отзыв на аренду               | UNIQUE(rental_id)    |
| Одна активная аренда аккаунта      | Partial UNIQUE INDEX |
| Все связи между сущностями валидны | FOREIGN KEY          |

Это означает, что даже при ошибке в приложении база данных не позволит нарушить фундаментальные бизнес-правила.

---

# 23. Transaction Strategy

Все изменения критически важных сущностей выполняются в рамках транзакций PostgreSQL.

## Create Rental

В одной транзакции выполняются:

1. Проверка доступности аккаунта.
2. Создание записи аренды.
3. Изменение статуса аккаунта на `Reserved`.
4. Создание записи платежа.

Если любой шаг завершается ошибкой, транзакция полностью откатывается.

---

## Activate Rental

В одной транзакции:

1. Подтверждение оплаты.
2. Изменение статуса аренды.
3. Изменение статуса аккаунта.
4. Запись события безопасности.

---

## Complete Rental

В одной транзакции:

1. Завершение аренды.
2. Освобождение аккаунта.
3. Создание операции возврата депозита.
4. Запись Audit Log.

---

# 24. Concurrency Control

Для предотвращения состояния гонки (Race Condition) используются механизмы PostgreSQL.

## Row-Level Locking

При создании аренды запись аккаунта блокируется:

```sql
SELECT *
FROM accounts
WHERE id = $1
FOR UPDATE;
```

Это предотвращает ситуацию, когда два пользователя одновременно арендуют один аккаунт.

---

## Optimistic Concurrency

Для сущностей, где потеря обновления возможна, рекомендуется использовать поле:

```text
version BIGINT
```

Поле увеличивается при каждом обновлении записи.

Это позволит реализовать оптимистическую блокировку при необходимости.

---

# 25. Indexing Strategy

Индексы создаются только для реально используемых сценариев.

## B-Tree Indexes

Используются для:

* email;
* SteamID;
* статусов;
* внешних ключей;
* временных полей.

---

## Partial Indexes

Используются для:

* доступных аккаунтов;
* активных аренд;
* непрочитанных уведомлений.

Это уменьшает размер индекса и ускоряет поиск.

---

## Composite Indexes

Планируется использование составных индексов для наиболее частых запросов.

Например:

```sql
(user_id, created_at DESC)
```

для получения истории пользователя.

Или:

```sql
(status, hourly_price)
```

для поиска доступных аккаунтов по цене.

---

# 26. Performance Considerations

При проектировании учитываются следующие требования.

## Минимизация JOIN

Часто используемые данные располагаются таким образом, чтобы уменьшить количество соединений таблиц.

---

## Использование Redis

Redis используется только для ускорения чтения.

Кэшируются:

* каталог игр;
* популярные аккаунты;
* результаты поиска;
* публичные карточки аккаунтов.

Источник истины всегда PostgreSQL.

---

## Pagination

Все списки возвращаются постранично.

Используется `LIMIT` и `OFFSET`.

При больших объёмах данных рекомендуется перейти на курсорную пагинацию (Keyset Pagination).

---

## JSONB

JSONB применяется только там, где структура данных может изменяться.

Например:

* metadata в `security_events`;
* old_values и new_values в `audit_logs`;
* списки разработчиков и жанров игр.

Основные бизнес-сущности не должны храниться в JSON.

---

# 27. Migration Strategy

Для управления схемой базы данных используется система миграций.

Основные правила:

* одна миграция — одно логическое изменение;
* миграции необратимо фиксируются в журнале;
* запрещается изменять уже применённую миграцию;
* все изменения схемы выполняются только через миграции.

Имена миграций рекомендуется оформлять следующим образом:

```text
000001_create_users.sql

000002_create_accounts.sql

000003_create_games.sql
```

---

# 28. Backup and Recovery

База данных должна регулярно резервироваться.

Рекомендуемая стратегия:

* ежедневный полный резервный снимок;
* периодическое архивирование WAL (Write-Ahead Log);
* регулярная проверка восстановления резервных копий.

Восстановление должно быть протестировано до выхода системы в промышленную эксплуатацию.

---

# 29. Monitoring

Необходимо контролировать следующие показатели PostgreSQL:

* количество активных соединений;
* длительные запросы;
* блокировки;
* использование индексов;
* размер таблиц;
* размер WAL;
* скорость роста базы данных.

Для анализа запросов рекомендуется использовать расширение:

```text
pg_stat_statements
```

---

# 30. Security

Пароли пользователей хранятся только в виде криптографического хэша.

Учётные данные Steam-аккаунтов должны храниться в зашифрованном виде с использованием современного алгоритма симметричного шифрования. Ключ шифрования не хранится в базе данных и передаётся приложению через переменные окружения или специализированное хранилище секретов.

Доступ к базе данных осуществляется по принципу минимально необходимых привилегий (Principle of Least Privilege).

---

# 31. Future Database Evolution

Архитектура базы данных допускает дальнейшее развитие без нарушения существующей модели.

Возможные направления:

* поддержка нескольких игровых платформ;
* несколько валют;
* система промокодов;
* подписки;
* динамическое ценообразование;
* антифрод-модуль;
* аналитическое хранилище (Data Warehouse);
* партиционирование крупных таблиц (`rentals`, `payments`, `security_events`) по времени.

Все новые сущности должны соответствовать общим принципам проектирования, изложенным в данном документе.

---

# 32. Conclusion

База данных GameRent спроектирована как централизованное и согласованное хранилище данных платформы.

При проектировании были соблюдены следующие принципы:

* нормализация данных;
* защита бизнес-инвариантов средствами PostgreSQL;
* поддержка ACID-транзакций;
* масштабируемость;
* безопасность хранения чувствительных данных;
* разделение оперативных и исторических данных;
* готовность к дальнейшему развитию проекта.

Данная модель является эталонной реализацией доменной модели (`domain-model.md`) и служит основой для разработки SQL-миграций, репозиториев и сервисного слоя приложения.

