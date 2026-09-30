CREATE TABLE unit_snapshot (
    lambung_unit_id BIGINT PRIMARY KEY,
    no_lambung VARCHAR(100) NOT NULL UNIQUE,
    no_polisi VARCHAR(100) NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    synced_at TIMESTAMPTZ NOT NULL,
    current_transaction_id BIGINT NULL
);

CREATE TABLE movement_transactions (
    transaction_id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    idempotency_key UUID NOT NULL UNIQUE,
    request_hash BYTEA NOT NULL,
    lambung_unit_id BIGINT NOT NULL REFERENCES unit_snapshot(lambung_unit_id),
    no_lambung_snapshot VARCHAR(100) NOT NULL,
    no_polisi_snapshot VARCHAR(100) NOT NULL,
    direction VARCHAR(3) NOT NULL CHECK (direction IN ('IN', 'OUT')),
    in_category VARCHAR(50) NULL,
    out_destination VARCHAR(50) NULL,
    driver_id BIGINT NULL,
    driver_name_snapshot VARCHAR(255) NULL,
    work_order_number VARCHAR(100) NULL,
    spp_number VARCHAR(100) NULL,
    customer_id VARCHAR(100) NULL,
    customer_name_snapshot VARCHAR(255) NULL,
    note TEXT NULL,
    actor_id VARCHAR(255) NOT NULL,
    actor_name_snapshot VARCHAR(255) NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (
        (direction = 'IN'
            AND in_category IN ('WAITING_FOR_ASSIGNMENT', 'WAITING_FOR_DELIVERY_TO_CUSTOMER', 'SERVICE_AND_REPAIR_MAINTENANCE')
            AND out_destination IS NULL AND driver_id IS NULL AND driver_name_snapshot IS NULL
            AND work_order_number IS NULL AND spp_number IS NULL AND customer_id IS NULL
            AND customer_name_snapshot IS NULL AND note IS NULL)
        OR (direction = 'OUT' AND in_category IS NULL AND driver_name_snapshot IS NOT NULL AND (
            (out_destination = 'BENGKEL_LUAR' AND work_order_number IS NOT NULL AND spp_number IS NULL)
            OR (out_destination IN ('FILLING_SHED_KUIN', 'CUSTOMER') AND spp_number IS NOT NULL AND work_order_number IS NULL)
            OR (out_destination = 'OTHER' AND driver_id IS NOT NULL AND note IS NOT NULL
                AND work_order_number IS NULL AND spp_number IS NULL)
        ))
    )
);

ALTER TABLE unit_snapshot
    ADD CONSTRAINT unit_snapshot_current_transaction_fk
    FOREIGN KEY (current_transaction_id)
    REFERENCES movement_transactions(transaction_id);

CREATE TABLE unit_sync_runs (
    sync_run_id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    started_at TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ NULL,
    status VARCHAR(20) NOT NULL CHECK (status IN ('RUNNING', 'SUCCEEDED', 'FAILED')),
    units_read INTEGER NOT NULL DEFAULT 0,
    error_message TEXT NULL
);

CREATE INDEX movement_transactions_lambung_unit_occurred_at_idx
    ON movement_transactions (lambung_unit_id, occurred_at DESC);
CREATE INDEX movement_transactions_spp_number_idx
    ON movement_transactions (spp_number) WHERE spp_number IS NOT NULL;
CREATE UNIQUE INDEX movement_transactions_work_order_number_uidx
    ON movement_transactions (work_order_number) WHERE work_order_number IS NOT NULL;
CREATE UNIQUE INDEX movement_transactions_filling_shed_spp_number_uidx
    ON movement_transactions (spp_number) WHERE out_destination = 'FILLING_SHED_KUIN';
CREATE INDEX unit_snapshot_current_transaction_id_idx
    ON unit_snapshot (current_transaction_id) WHERE current_transaction_id IS NOT NULL;

CREATE FUNCTION reject_movement_transaction_mutation()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'movement_transactions is append-only';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER movement_transactions_append_only
    BEFORE UPDATE OR DELETE ON movement_transactions
    FOR EACH ROW EXECUTE FUNCTION reject_movement_transaction_mutation();
