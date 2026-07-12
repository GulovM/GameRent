-- +goose Up
ALTER TABLE deposit_holds
    ADD CONSTRAINT chk_deposit_holds_status_known
    CHECK (status IN (1, 2, 3, 4));

-- +goose Down
ALTER TABLE deposit_holds
    DROP CONSTRAINT IF EXISTS chk_deposit_holds_status_known;
