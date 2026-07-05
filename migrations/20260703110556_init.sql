-- +goose Up
-- 1. Table: users
CREATE TABLE users (
    id BIGSERIAL PRIMARY KEY,
    email VARCHAR(255) NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    first_name VARCHAR(100),
    last_name VARCHAR(100),
    email_verified BOOLEAN NOT NULL DEFAULT FALSE,
    role VARCHAR(20) NOT NULL DEFAULT 'RENT' CHECK (role IN ('ADMIN', 'RENT')),
    trust_score INTEGER NOT NULL DEFAULT 300 CHECK (trust_score >= 0 AND trust_score <= 1000),
    is_blocked BOOLEAN NOT NULL DEFAULT FALSE,
    balance BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0),
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP NULL
);

CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_users_trust_score ON users(trust_score);

-- 2. Table: accounts
CREATE TABLE accounts (
    id BIGSERIAL PRIMARY KEY,
    steam_id64 VARCHAR(32) NOT NULL UNIQUE,
    login TEXT NOT NULL,
    encrypted_password BYTEA NOT NULL,
    steam_guard_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    inventory_verified BOOLEAN NOT NULL DEFAULT FALSE,
    last_security_check TIMESTAMP NULL,
    hourly_price BIGINT NOT NULL CHECK (hourly_price > 0),
    deposit_amount BIGINT NOT NULL CHECK (deposit_amount >= 0),
    status SMALLINT NOT NULL DEFAULT 0,
    profile_url TEXT,
    avatar_url TEXT,
    library_synced_at TIMESTAMP NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP NULL
);

CREATE INDEX idx_accounts_status ON accounts(status);
CREATE INDEX idx_accounts_library_synced_at ON accounts(library_synced_at);
CREATE INDEX idx_accounts_steam_id64 ON accounts(steam_id64);
CREATE INDEX idx_accounts_available ON accounts(id) WHERE status = 2;

-- 3. Table: games
CREATE TABLE games (
    id BIGSERIAL PRIMARY KEY,
    steam_app_id INTEGER NOT NULL UNIQUE,
    name TEXT NOT NULL,
    short_description TEXT,
    header_image TEXT,
    release_date DATE NULL,
    developers JSONB,
    publishers JSONB,
    genres JSONB,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_games_steam_app_id ON games(steam_app_id);
CREATE INDEX idx_games_name ON games(name);

-- 4. Table: account_games
CREATE TABLE account_games (
    account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    game_id BIGINT NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    playtime_minutes INTEGER NOT NULL DEFAULT 0 CHECK (playtime_minutes >= 0),
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    PRIMARY KEY(account_id, game_id)
);

CREATE INDEX idx_account_games_game_id ON account_games(game_id);
CREATE INDEX idx_account_games_account_id ON account_games(account_id);

-- 5. Table: rentals
CREATE TABLE rentals (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    status SMALLINT NOT NULL DEFAULT 0,
    start_at TIMESTAMP NOT NULL,
    end_at TIMESTAMP NOT NULL,
    rental_price BIGINT NOT NULL CHECK (rental_price > 0),
    deposit_amount BIGINT NOT NULL CHECK (deposit_amount >= 0),
    actual_finished_at TIMESTAMP NULL,
    cancellation_reason TEXT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_rentals_dates CHECK (start_at < end_at)
);

CREATE UNIQUE INDEX uq_active_account_rental ON rentals(account_id) WHERE status = 2;
CREATE INDEX idx_rentals_user_id ON rentals(user_id);
CREATE INDEX idx_rentals_account_id ON rentals(account_id);
CREATE INDEX idx_rentals_status ON rentals(status);
CREATE INDEX idx_rentals_start_at ON rentals(start_at);
CREATE INDEX idx_rentals_end_at ON rentals(end_at);

-- 6. Table: payments
CREATE TABLE payments (
    id BIGSERIAL PRIMARY KEY,
    rental_id BIGINT NOT NULL REFERENCES rentals(id) ON DELETE RESTRICT,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    payment_type SMALLINT NOT NULL,
    status SMALLINT NOT NULL DEFAULT 0,
    amount BIGINT NOT NULL CHECK (amount > 0),
    currency CHAR(3) NOT NULL CHECK (currency IN ('USD', 'EUR', 'RUB', 'TJS')),
    external_transaction_id TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    processed_at TIMESTAMP NULL
);

CREATE INDEX idx_payments_rental_id ON payments(rental_id);
CREATE INDEX idx_payments_user_id ON payments(user_id);
CREATE INDEX idx_payments_status ON payments(status);
CREATE INDEX idx_payments_payment_type ON payments(payment_type);

-- 7. Table: reviews
CREATE TABLE reviews (
    id BIGSERIAL PRIMARY KEY,
    rental_id BIGINT NOT NULL UNIQUE REFERENCES rentals(id) ON DELETE RESTRICT,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    rating SMALLINT NOT NULL CHECK (rating BETWEEN 1 AND 5),
    comment TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_reviews_account_id ON reviews(account_id);
CREATE INDEX idx_reviews_user_id ON reviews(user_id);
CREATE INDEX idx_reviews_rating ON reviews(rating);

-- 8. Table: security_events
CREATE TABLE security_events (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    account_id BIGINT NULL REFERENCES accounts(id) ON DELETE SET NULL,
    rental_id BIGINT NULL REFERENCES rentals(id) ON DELETE SET NULL,
    event_type SMALLINT NOT NULL,
    ip_address INET,
    user_agent TEXT,
    country VARCHAR(100) NULL,
    city VARCHAR(100) NULL,
    success BOOLEAN,
    metadata JSONB,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_security_events_user_id ON security_events(user_id);
CREATE INDEX idx_security_events_account_id ON security_events(account_id);
CREATE INDEX idx_security_events_event_type ON security_events(event_type);
CREATE INDEX idx_security_events_created_at ON security_events(created_at);
CREATE INDEX idx_security_events_ip_address ON security_events(ip_address);

-- 9. Table: audit_logs
CREATE TABLE audit_logs (
    id BIGSERIAL PRIMARY KEY,
    actor_user_id BIGINT NULL,
    entity_type VARCHAR(50) NOT NULL,
    entity_id BIGINT NOT NULL,
    action VARCHAR(100) NOT NULL,
    old_values JSONB,
    new_values JSONB,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_audit_logs_actor_user_id ON audit_logs(actor_user_id);
CREATE INDEX idx_audit_logs_entity_type ON audit_logs(entity_type);
CREATE INDEX idx_audit_logs_entity_id ON audit_logs(entity_id);
CREATE INDEX idx_audit_logs_created_at ON audit_logs(created_at);

-- 10. Table: refresh_tokens
CREATE TABLE refresh_tokens (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    revoked_at TIMESTAMP NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_refresh_tokens_expires CHECK (expires_at > created_at)
);

CREATE INDEX idx_refresh_tokens_user_id ON refresh_tokens(user_id);
CREATE INDEX idx_refresh_tokens_expires ON refresh_tokens(expires_at);

-- 11. Table: notifications
CREATE TABLE notifications (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type SMALLINT NOT NULL,
    title VARCHAR(255) NOT NULL,
    body TEXT NOT NULL,
    is_read BOOLEAN NOT NULL DEFAULT FALSE,
    sent_at TIMESTAMP NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_notifications_user_id ON notifications(user_id);
CREATE INDEX idx_notifications_is_read ON notifications(is_read);
CREATE INDEX idx_notifications_created_at ON notifications(created_at);


-- +goose Down
DROP TABLE IF EXISTS notifications CASCADE;
DROP TABLE IF EXISTS refresh_tokens CASCADE;
DROP TABLE IF EXISTS audit_logs CASCADE;
DROP TABLE IF EXISTS security_events CASCADE;
DROP TABLE IF EXISTS reviews CASCADE;
DROP TABLE IF EXISTS payments CASCADE;
DROP TABLE IF EXISTS rentals CASCADE;
DROP TABLE IF EXISTS account_games CASCADE;
DROP TABLE IF EXISTS games CASCADE;
DROP TABLE IF EXISTS accounts CASCADE;
DROP TABLE IF EXISTS users CASCADE;
