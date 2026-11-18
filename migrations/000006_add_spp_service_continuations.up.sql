ALTER TABLE movement_transactions
    ADD COLUMN resumed_from_service_transaction_id BIGINT NULL
        REFERENCES movement_transactions(transaction_id),
    ADD CONSTRAINT movement_transactions_service_resumption_check CHECK (
        resumed_from_service_transaction_id IS NULL OR (
            direction = 'OUT'
            AND out_destination IN ('FILLING_SHED_KUIN', 'CUSTOMER')
            AND spp_number IS NOT NULL
            AND work_order_number IS NULL AND driver_id IS NULL
            AND resumed_from_service_transaction_id <> transaction_id
        )
    );

-- A return is trustworthy only through its own completed, same-unit SPP cycle.
-- Nullable legacy arrival snapshots can use the trusted departure, but contradictions cannot.
CREATE VIEW movement_spp_return_context AS
SELECT arrival.transaction_id AS in_transaction_id, arrival.lambung_unit_id,
       arrival.in_category, departure.transaction_id AS out_transaction_id,
       departure.spp_number, departure.customer_id
FROM movement_cycles cycle
JOIN movement_transactions arrival
  ON arrival.transaction_id = cycle.in_transaction_id
 AND arrival.lambung_unit_id = cycle.lambung_unit_id AND arrival.direction = 'IN'
JOIN movement_transactions departure
  ON departure.transaction_id = cycle.out_transaction_id
 AND departure.lambung_unit_id = cycle.lambung_unit_id AND departure.direction = 'OUT'
 AND departure.out_destination IN ('FILLING_SHED_KUIN', 'CUSTOMER')
 AND departure.work_order_number IS NULL AND departure.driver_id IS NULL
WHERE cycle.completed_at IS NOT NULL
  AND arrival.in_category IN ('WAITING_FOR_DELIVERY_TO_CUSTOMER', 'SERVICE_AND_REPAIR_MAINTENANCE')
  AND NULLIF(BTRIM(departure.spp_number), '') IS NOT NULL
  AND NULLIF(BTRIM(departure.customer_id), '') IS NOT NULL
  AND arrival.work_order_number IS NULL AND arrival.driver_id IS NULL
  AND (arrival.spp_number IS NULL OR arrival.spp_number = departure.spp_number)
  AND (arrival.customer_id IS NULL OR arrival.customer_id = departure.customer_id);

CREATE FUNCTION unitlog_spp_completed(p_spp_number TEXT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
SELECT EXISTS (
    SELECT 1 FROM movement_transactions history
    LEFT JOIN movement_cycles history_cycle
      ON history_cycle.out_transaction_id = history.transaction_id
     AND history_cycle.lambung_unit_id = history.lambung_unit_id
    LEFT JOIN movement_transactions returned
      ON returned.transaction_id = history_cycle.in_transaction_id
     AND returned.lambung_unit_id = history.lambung_unit_id AND returned.direction = 'IN'
    WHERE history.spp_number = p_spp_number AND (
        (history.direction = 'IN' AND history.in_category <> 'WAITING_FOR_DELIVERY_TO_CUSTOMER'
         AND NOT (
             history.in_category = 'SERVICE_AND_REPAIR_MAINTENANCE'
             AND EXISTS (
                 SELECT 1 FROM movement_spp_return_context valid
                 WHERE valid.in_transaction_id = history.transaction_id
                   AND valid.spp_number = p_spp_number
             )
         ))
        OR (history.direction = 'OUT' AND history.out_destination IN ('FILLING_SHED_KUIN', 'CUSTOMER')
            AND returned.in_category <> 'WAITING_FOR_DELIVERY_TO_CUSTOMER'
            AND NOT (
                returned.in_category = 'SERVICE_AND_REPAIR_MAINTENANCE'
                AND EXISTS (
                    SELECT 1 FROM movement_spp_return_context valid
                    WHERE valid.in_transaction_id = returned.transaction_id
                      AND valid.spp_number = p_spp_number
                )
            ))
    )
);
$$;

CREATE FUNCTION derive_spp_service_resumption()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    current_id BIGINT;
    expected_source BIGINT;
    source_customer TEXT;
BEGIN
    -- Match application lock order: unit first, canonical SPP second.
    IF NEW.spp_number IS NOT NULL THEN
        PERFORM 1 FROM unit_snapshot
        WHERE lambung_unit_id = NEW.lambung_unit_id FOR UPDATE;
        PERFORM pg_advisory_xact_lock(8105, hashtext(NEW.spp_number));
    END IF;
    IF NEW.direction = 'OUT' AND NEW.spp_number IS NOT NULL
       AND NEW.out_destination IN ('FILLING_SHED_KUIN', 'CUSTOMER') THEN
        SELECT current_transaction_id INTO current_id
        FROM unit_snapshot
        WHERE lambung_unit_id = NEW.lambung_unit_id AND is_active = TRUE
        FOR UPDATE;

        SELECT in_transaction_id, customer_id INTO expected_source, source_customer
        FROM movement_spp_return_context
        WHERE in_transaction_id = current_id AND lambung_unit_id = NEW.lambung_unit_id
          AND in_category = 'SERVICE_AND_REPAIR_MAINTENANCE'
          AND spp_number = NEW.spp_number;

        IF FOUND AND (NEW.customer_id IS DISTINCT FROM source_customer
                      OR unitlog_spp_completed(NEW.spp_number)) THEN
            RAISE EXCEPTION USING ERRCODE = '23514',
                CONSTRAINT = 'movement_transactions_service_resumption_check',
                MESSAGE = 'invalid SPP service continuation';
        END IF;
    END IF;

    IF NEW.resumed_from_service_transaction_id IS NOT NULL
       AND NEW.resumed_from_service_transaction_id IS DISTINCT FROM expected_source THEN
        RAISE EXCEPTION USING ERRCODE = '23514',
            CONSTRAINT = 'movement_transactions_service_resumption_check',
            MESSAGE = 'invalid SPP service continuation source';
    END IF;
    NEW.resumed_from_service_transaction_id := expected_source;
    RETURN NEW;
END;
$$;

CREATE TRIGGER movement_transactions_service_resumption
    BEFORE INSERT ON movement_transactions
    FOR EACH ROW EXECUTE FUNCTION derive_spp_service_resumption();

-- Keep one normal Filling Shed departure; only validated resumptions are excepted.
DROP INDEX movement_transactions_filling_shed_spp_number_uidx;
CREATE UNIQUE INDEX movement_transactions_filling_shed_spp_number_uidx
    ON movement_transactions (spp_number)
    WHERE out_destination = 'FILLING_SHED_KUIN'
      AND resumed_from_service_transaction_id IS NULL;

CREATE UNIQUE INDEX movement_transactions_service_resumption_uidx
    ON movement_transactions (resumed_from_service_transaction_id)
    WHERE resumed_from_service_transaction_id IS NOT NULL;

-- Completed correlation is audit evidence for future Service resumptions.
CREATE FUNCTION reject_completed_cycle_mutation()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.completed_at IS NOT NULL THEN
        RAISE EXCEPTION 'completed movement cycle is immutable';
    END IF;
    IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER movement_cycles_completed_immutable
    BEFORE UPDATE OR DELETE ON movement_cycles
    FOR EACH ROW EXECUTE FUNCTION reject_completed_cycle_mutation();
