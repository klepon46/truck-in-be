LOCK TABLE movement_transactions IN ACCESS EXCLUSIVE MODE;
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM movement_transactions WHERE spp_departure_policy_version = 7
    ) THEN
        RAISE EXCEPTION 'cannot downgrade SPP reuse policy while version-7 departure audit exists';
    END IF;
END;
$$;

-- Restore the version-6 trigger, without modifying its historical migration file.
CREATE OR REPLACE FUNCTION derive_spp_service_resumption()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    current_id BIGINT;
    expected_source BIGINT;
    source_customer TEXT;
BEGIN
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

CREATE UNIQUE INDEX movement_transactions_filling_shed_spp_number_uidx
    ON movement_transactions (spp_number)
    WHERE out_destination = 'FILLING_SHED_KUIN'
      AND resumed_from_service_transaction_id IS NULL;
DROP FUNCTION unitlog_spp_used_by_other_unit(TEXT, BIGINT);
ALTER TABLE movement_transactions
    DROP CONSTRAINT movement_transactions_spp_policy_check,
    DROP COLUMN spp_departure_policy_version;
