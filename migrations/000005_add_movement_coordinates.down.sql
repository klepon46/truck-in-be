DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM movement_transactions WHERE coordinates IS NOT NULL) THEN
        RAISE EXCEPTION 'cannot roll back 000005: movement coordinates are immutable audit data';
    END IF;
END;
$$;

ALTER TABLE movement_transactions
    DROP CONSTRAINT movement_transactions_coordinates_check,
    DROP COLUMN coordinates;
