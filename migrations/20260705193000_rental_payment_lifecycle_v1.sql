-- +goose Up
ALTER TABLE rentals
    ADD COLUMN IF NOT EXISTS payment_expires_at TIMESTAMP NOT NULL DEFAULT (NOW() + INTERVAL '15 minutes');

ALTER TABLE rentals
    ADD CONSTRAINT chk_rentals_payment_expires_after_created CHECK (payment_expires_at > created_at);

ALTER TABLE payments
    ADD COLUMN IF NOT EXISTS provider TEXT NOT NULL DEFAULT 'internal';

DROP INDEX IF EXISTS uq_active_account_rental;
CREATE UNIQUE INDEX uq_rental_account_waiting_or_active
    ON rentals(account_id)
    WHERE status IN (1, 2);

CREATE UNIQUE INDEX uq_payments_provider_external_transaction
    ON payments(provider, external_transaction_id)
    WHERE external_transaction_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS uq_payments_provider_external_transaction;
DROP INDEX IF EXISTS uq_rental_account_waiting_or_active;

ALTER TABLE payments
    DROP COLUMN IF EXISTS provider;

ALTER TABLE rentals
    DROP CONSTRAINT IF EXISTS chk_rentals_payment_expires_after_created;

ALTER TABLE rentals
    DROP COLUMN IF EXISTS payment_expires_at;

CREATE UNIQUE INDEX uq_active_account_rental
    ON rentals(account_id)
    WHERE status = 2;
