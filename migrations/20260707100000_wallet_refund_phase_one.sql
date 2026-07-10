-- +goose Up
CREATE TABLE IF NOT EXISTS refunds (
    id BIGSERIAL PRIMARY KEY,
    payment_id BIGINT NOT NULL REFERENCES payments(id) ON DELETE RESTRICT,
    rental_id BIGINT NOT NULL REFERENCES rentals(id) ON DELETE RESTRICT,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    account_id BIGINT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    source_type SMALLINT NOT NULL,
    refund_kind SMALLINT NOT NULL,
    status SMALLINT NOT NULL,
    reason_code TEXT NOT NULL,
    requested_by_user_id BIGINT NULL REFERENCES users(id) ON DELETE RESTRICT,
    requested_by_role TEXT NOT NULL,
    amount_principal BIGINT NOT NULL CHECK (amount_principal >= 0),
    amount_deposit BIGINT NOT NULL CHECK (amount_deposit >= 0),
    amount_total BIGINT NOT NULL CHECK (amount_total > 0),
    currency CHAR(3) NOT NULL CHECK (currency IN ('USD', 'EUR', 'RUB', 'TJS')),
    idempotency_key TEXT NOT NULL UNIQUE,
    correlation_id TEXT NULL,
    metadata JSONB NOT NULL DEFAULT '{}',
    processed_at TIMESTAMP NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_refunds_amount_total
        CHECK (amount_total = amount_principal + amount_deposit)
);

CREATE INDEX IF NOT EXISTS idx_refunds_user_created
    ON refunds(user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_refunds_payment_created
    ON refunds(payment_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_refunds_rental_created
    ON refunds(rental_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_refunds_status_updated
    ON refunds(status, updated_at);

ALTER TABLE deposit_holds
    ADD COLUMN IF NOT EXISTS refunded_at TIMESTAMP NULL;

ALTER TABLE deposit_holds
    ADD COLUMN IF NOT EXISTS refund_id BIGINT NULL;

ALTER TABLE deposit_holds
    ADD CONSTRAINT fk_deposit_holds_refund_id
    FOREIGN KEY (refund_id) REFERENCES refunds(id) ON DELETE RESTRICT;

CREATE INDEX IF NOT EXISTS idx_deposit_holds_refund
    ON deposit_holds(refund_id);

-- +goose Down
DROP INDEX IF EXISTS idx_deposit_holds_refund;

ALTER TABLE deposit_holds
    DROP CONSTRAINT IF EXISTS fk_deposit_holds_refund_id;

ALTER TABLE deposit_holds
    DROP COLUMN IF EXISTS refund_id;

ALTER TABLE deposit_holds
    DROP COLUMN IF EXISTS refunded_at;

DROP INDEX IF EXISTS idx_refunds_status_updated;
DROP INDEX IF EXISTS idx_refunds_rental_created;
DROP INDEX IF EXISTS idx_refunds_payment_created;
DROP INDEX IF EXISTS idx_refunds_user_created;
DROP TABLE IF EXISTS refunds;
