-- +goose Up
CREATE TABLE IF NOT EXISTS financial_ledger_entries (
    id BIGSERIAL PRIMARY KEY,
    entry_type SMALLINT NOT NULL,
    user_id BIGINT NULL REFERENCES users(id) ON DELETE RESTRICT,
    rental_id BIGINT NULL REFERENCES rentals(id) ON DELETE RESTRICT,
    payment_id BIGINT NULL REFERENCES payments(id) ON DELETE RESTRICT,
    account_id BIGINT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    amount BIGINT NOT NULL CHECK (amount > 0),
    currency CHAR(3) NOT NULL CHECK (currency IN ('USD', 'EUR', 'RUB', 'TJS')),
    provider TEXT NULL,
    external_transaction_id TEXT NULL,
    idempotency_key TEXT NOT NULL UNIQUE,
    correlation_id TEXT NULL,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_financial_ledger_user_created
    ON financial_ledger_entries(user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_financial_ledger_rental_created
    ON financial_ledger_entries(rental_id, created_at);

CREATE INDEX IF NOT EXISTS idx_financial_ledger_payment
    ON financial_ledger_entries(payment_id);

CREATE INDEX IF NOT EXISTS idx_financial_ledger_provider_external_tx
    ON financial_ledger_entries(provider, external_transaction_id);

CREATE INDEX IF NOT EXISTS idx_financial_ledger_correlation
    ON financial_ledger_entries(correlation_id);

CREATE INDEX IF NOT EXISTS idx_financial_ledger_type_created
    ON financial_ledger_entries(entry_type, created_at);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION prevent_financial_ledger_mutation()
RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'financial_ledger_entries is append-only; UPDATE and DELETE are not allowed';
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS trg_financial_ledger_append_only ON financial_ledger_entries;

CREATE TRIGGER trg_financial_ledger_append_only
    BEFORE UPDATE OR DELETE ON financial_ledger_entries
    FOR EACH ROW
    EXECUTE FUNCTION prevent_financial_ledger_mutation();

CREATE TABLE IF NOT EXISTS deposit_holds (
    id BIGSERIAL PRIMARY KEY,
    rental_id BIGINT NOT NULL UNIQUE REFERENCES rentals(id) ON DELETE RESTRICT,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    payment_id BIGINT NULL REFERENCES payments(id) ON DELETE RESTRICT,
    amount BIGINT NOT NULL CHECK (amount > 0),
    currency CHAR(3) NOT NULL CHECK (currency IN ('USD', 'EUR', 'RUB', 'TJS')),
    status SMALLINT NOT NULL,
    held_at TIMESTAMP NULL,
    released_at TIMESTAMP NULL,
    forfeited_at TIMESTAMP NULL,
    idempotency_key TEXT NOT NULL UNIQUE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_deposit_holds_user_status
    ON deposit_holds(user_id, status);

CREATE INDEX IF NOT EXISTS idx_deposit_holds_status_updated
    ON deposit_holds(status, updated_at);

CREATE INDEX IF NOT EXISTS idx_deposit_holds_payment
    ON deposit_holds(payment_id);

-- +goose Down
DROP INDEX IF EXISTS idx_deposit_holds_payment;
DROP INDEX IF EXISTS idx_deposit_holds_status_updated;
DROP INDEX IF EXISTS idx_deposit_holds_user_status;
DROP TABLE IF EXISTS deposit_holds;

DROP TRIGGER IF EXISTS trg_financial_ledger_append_only ON financial_ledger_entries;
DROP FUNCTION IF EXISTS prevent_financial_ledger_mutation();

DROP INDEX IF EXISTS idx_financial_ledger_type_created;
DROP INDEX IF EXISTS idx_financial_ledger_correlation;
DROP INDEX IF EXISTS idx_financial_ledger_provider_external_tx;
DROP INDEX IF EXISTS idx_financial_ledger_payment;
DROP INDEX IF EXISTS idx_financial_ledger_rental_created;
DROP INDEX IF EXISTS idx_financial_ledger_user_created;
DROP TABLE IF EXISTS financial_ledger_entries;
