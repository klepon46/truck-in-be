ALTER TABLE movement_transactions
    ADD COLUMN spp_departure_policy_version SMALLINT NULL,
    ADD CONSTRAINT movement_transactions_spp_policy_check CHECK (
        spp_departure_policy_version IS NULL OR (
            spp_departure_policy_version = 7 AND direction = 'OUT'
            AND out_destination IN ('FILLING_SHED_KUIN', 'CUSTOMER')
            AND spp_number IS NOT NULL
            AND work_order_number IS NULL AND driver_id IS NULL
        )
    );

-- Same-unit reuse is permitted; contradictory unit history remains fail-closed.
CREATE FUNCTION unitlog_spp_used_by_other_unit(p_spp_number TEXT, p_unit_id BIGINT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
    SELECT EXISTS (
        SELECT 1 FROM movement_transactions
        WHERE spp_number = p_spp_number AND lambung_unit_id <> p_unit_id
    );
$$;

CREATE OR REPLACE FUNCTION derive_spp_service_resumption()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    current_id BIGINT;
    expected_source BIGINT;
BEGIN
    IF NEW.spp_number IS NOT NULL THEN
        PERFORM 1 FROM unit_snapshot
        WHERE lambung_unit_id = NEW.lambung_unit_id FOR UPDATE;
        PERFORM pg_advisory_xact_lock(8105, hashtext(NEW.spp_number));
    END IF;

    IF NEW.direction = 'OUT' AND NEW.spp_number IS NOT NULL
       AND NEW.out_destination IN ('FILLING_SHED_KUIN', 'CUSTOMER') THEN
        IF unitlog_spp_used_by_other_unit(NEW.spp_number, NEW.lambung_unit_id) THEN
            RAISE EXCEPTION USING ERRCODE = '23514',
                CONSTRAINT = 'movement_transactions_spp_unit_check',
                MESSAGE = 'SPP history belongs to another unit';
        END IF;
        IF NEW.spp_departure_policy_version IS NOT NULL
           AND NEW.spp_departure_policy_version <> 7 THEN
            RAISE EXCEPTION USING ERRCODE = '23514',
                CONSTRAINT = 'movement_transactions_spp_policy_check',
                MESSAGE = 'invalid SPP departure policy version';
        END IF;
        NEW.spp_departure_policy_version := 7;

        SELECT current_transaction_id INTO current_id
        FROM unit_snapshot
        WHERE lambung_unit_id = NEW.lambung_unit_id AND is_active = TRUE
        FOR UPDATE;

        -- Retain genuine Service provenance, but no longer require it for reuse.
        -- Changed customer/document snapshots do not fabricate a Service link.
        SELECT in_transaction_id INTO expected_source
        FROM movement_spp_return_context
        WHERE in_transaction_id = current_id AND lambung_unit_id = NEW.lambung_unit_id
          AND in_category = 'SERVICE_AND_REPAIR_MAINTENANCE'
          AND spp_number = NEW.spp_number AND customer_id = NEW.customer_id;
    ELSIF NEW.spp_departure_policy_version IS NOT NULL THEN
        RAISE EXCEPTION USING ERRCODE = '23514',
            CONSTRAINT = 'movement_transactions_spp_policy_check',
            MESSAGE = 'SPP departure policy marker requires an SPP OUT';
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

-- Do not rewrite old movements or drop any provenance/immutability protections.
DROP INDEX movement_transactions_filling_shed_spp_number_uidx;
