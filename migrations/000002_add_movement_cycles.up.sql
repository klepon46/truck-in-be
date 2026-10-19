CREATE TABLE movement_cycles (
    cycle_id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    lambung_unit_id BIGINT NOT NULL REFERENCES unit_snapshot(lambung_unit_id),
    out_transaction_id BIGINT NOT NULL UNIQUE REFERENCES movement_transactions(transaction_id),
    in_transaction_id BIGINT NULL UNIQUE REFERENCES movement_transactions(transaction_id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ NULL,
    CHECK (in_transaction_id IS NULL OR in_transaction_id <> out_transaction_id),
    CHECK (
        (in_transaction_id IS NULL AND completed_at IS NULL)
        OR (in_transaction_id IS NOT NULL AND completed_at IS NOT NULL)
    )
);

CREATE UNIQUE INDEX movement_cycles_one_open_per_unit_uidx
    ON movement_cycles (lambung_unit_id) WHERE in_transaction_id IS NULL;

INSERT INTO movement_cycles (lambung_unit_id, out_transaction_id, created_at)
SELECT u.lambung_unit_id, current_transaction.transaction_id, current_transaction.occurred_at
FROM unit_snapshot u
JOIN movement_transactions current_transaction ON current_transaction.transaction_id = u.current_transaction_id
WHERE current_transaction.direction = 'OUT'
ON CONFLICT (out_transaction_id) DO NOTHING;
