CREATE TABLE movement_status_durations (
    transaction_id BIGINT PRIMARY KEY REFERENCES movement_transactions(transaction_id),
    duration_seconds BIGINT NOT NULL CHECK (duration_seconds >= 0),
    completed_at TIMESTAMPTZ NOT NULL
);

CREATE FUNCTION reject_movement_status_duration_mutation()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'movement status duration is immutable';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER movement_status_durations_append_only
    BEFORE UPDATE OR DELETE ON movement_status_durations
    FOR EACH ROW EXECUTE FUNCTION reject_movement_status_duration_mutation();

INSERT INTO movement_status_durations (transaction_id, duration_seconds, completed_at)
SELECT movement.transaction_id,
       GREATEST(0, EXTRACT(EPOCH FROM next_movement.occurred_at - movement.occurred_at)::BIGINT),
       next_movement.occurred_at
FROM movement_transactions movement
JOIN LATERAL (
    SELECT subsequent.occurred_at
    FROM movement_transactions subsequent
    WHERE subsequent.lambung_unit_id = movement.lambung_unit_id
      AND subsequent.transaction_id > movement.transaction_id
    ORDER BY subsequent.transaction_id ASC
    LIMIT 1
) next_movement ON TRUE
ON CONFLICT (transaction_id) DO NOTHING;
