-- +goose Up
ALTER TABLE rentals
    ADD COLUMN deposit_review_deadline_at TIMESTAMP NULL,
    ADD COLUMN completed_at TIMESTAMP NULL;

ALTER TABLE deposit_holds
    ADD COLUMN settlement_source SMALLINT NULL,
    ADD COLUMN settled_by_user_id BIGINT NULL REFERENCES users(id) ON DELETE RESTRICT,
    ADD COLUMN settlement_reason_code VARCHAR(64) NULL,
    ADD COLUMN settlement_evidence_ref VARCHAR(255) NULL;

-- Preserve the historical meaning of actual_finished_at as the usage-end time.
-- Legacy COMPLETED rows are closed using their latest persisted lifecycle timestamp.
UPDATE rentals
SET completed_at = COALESCE(updated_at, actual_finished_at, end_at)
WHERE status = 4
  AND completed_at IS NULL;

-- Give only internally consistent legacy paid/expired/held rentals a fresh review
-- window. Inconsistent positive-deposit rows remain NULL and therefore fail closed.
UPDATE rentals AS r
SET deposit_review_deadline_at = NOW() + INTERVAL '24 hours'
FROM payments AS p
JOIN deposit_holds AS d
  ON d.payment_id = p.id
 AND d.rental_id = p.rental_id
 AND d.user_id = p.user_id
WHERE p.rental_id = r.id
  AND p.user_id = r.user_id
  AND p.status = 2
  AND r.status = 3
  AND r.deposit_amount > 0
  AND r.actual_finished_at IS NOT NULL
  AND r.actual_finished_at <= NOW() + INTERVAL '24 hours'
  AND r.end_at <= NOW()
  AND d.status = 1
  AND d.amount = r.deposit_amount
  AND d.currency = p.currency
  AND r.deposit_review_deadline_at IS NULL;

ALTER TABLE rentals
    ADD CONSTRAINT chk_rentals_review_deadline_after_usage_end
        CHECK (
            deposit_review_deadline_at IS NULL
            OR actual_finished_at IS NULL
            OR deposit_review_deadline_at >= actual_finished_at
        ),
    ADD CONSTRAINT chk_rentals_completed_at_present
        CHECK (status <> 4 OR completed_at IS NOT NULL);

ALTER TABLE deposit_holds
    ADD CONSTRAINT chk_deposit_holds_settlement_source_known
        CHECK (settlement_source IS NULL OR settlement_source IN (1, 2, 3, 4)),
    ADD CONSTRAINT chk_deposit_holds_settlement_metadata
        CHECK (
            settlement_source IS NULL
            OR (
                settlement_source = 1
                AND status = 2
                AND settled_by_user_id IS NOT NULL
            )
            OR (
                settlement_source = 2
                AND status = 3
                AND settled_by_user_id IS NOT NULL
                AND settlement_reason_code IS NOT NULL
                AND BTRIM(settlement_reason_code) <> ''
                AND settlement_evidence_ref IS NOT NULL
                AND BTRIM(settlement_evidence_ref) <> ''
            )
            OR (
                settlement_source = 3
                AND status = 2
                AND settled_by_user_id IS NULL
            )
            OR (
                settlement_source = 4
                AND status = 4
                AND settled_by_user_id IS NOT NULL
            )
        );

CREATE INDEX idx_rentals_active_expiry_queue
    ON rentals(end_at, id)
    WHERE status = 2;

CREATE INDEX idx_rentals_expired_finalization_queue
    ON rentals(deposit_review_deadline_at, id)
    WHERE status = 3;

-- +goose Down
DROP INDEX IF EXISTS idx_rentals_expired_finalization_queue;
DROP INDEX IF EXISTS idx_rentals_active_expiry_queue;

ALTER TABLE deposit_holds
    DROP CONSTRAINT IF EXISTS chk_deposit_holds_settlement_metadata,
    DROP CONSTRAINT IF EXISTS chk_deposit_holds_settlement_source_known;

ALTER TABLE rentals
    DROP CONSTRAINT IF EXISTS chk_rentals_completed_at_present,
    DROP CONSTRAINT IF EXISTS chk_rentals_review_deadline_after_usage_end;

ALTER TABLE deposit_holds
    DROP COLUMN IF EXISTS settlement_evidence_ref,
    DROP COLUMN IF EXISTS settlement_reason_code,
    DROP COLUMN IF EXISTS settled_by_user_id,
    DROP COLUMN IF EXISTS settlement_source;

ALTER TABLE rentals
    DROP COLUMN IF EXISTS completed_at,
    DROP COLUMN IF EXISTS deposit_review_deadline_at;
