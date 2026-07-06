-- +goose Up
-- Refuse to create the one-payment-per-rental constraint if legacy duplicates exist.
-- +goose StatementBegin
DO $$
DECLARE
    duplicate_ids TEXT;
BEGIN
    SELECT string_agg(rental_id::text, ', ')
    INTO duplicate_ids
    FROM (
        SELECT rental_id
        FROM payments
        GROUP BY rental_id
        HAVING COUNT(*) > 1
        ORDER BY rental_id
        LIMIT 20
    ) dup;

    IF duplicate_ids IS NOT NULL THEN
        RAISE EXCEPTION 'cannot enforce single payment per rental; duplicate payments exist for rental_id(s): %', duplicate_ids;
    END IF;
END $$;
-- +goose StatementEnd

DROP INDEX IF EXISTS idx_payments_rental_id;

CREATE UNIQUE INDEX IF NOT EXISTS uq_payments_rental_id
    ON payments(rental_id);

-- +goose Down
DROP INDEX IF EXISTS uq_payments_rental_id;

CREATE INDEX IF NOT EXISTS idx_payments_rental_id
    ON payments(rental_id);
