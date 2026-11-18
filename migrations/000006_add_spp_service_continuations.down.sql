LOCK TABLE movement_transactions IN ACCESS EXCLUSIVE MODE;
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM movement_transactions
        WHERE resumed_from_service_transaction_id IS NOT NULL
    ) THEN
        RAISE EXCEPTION 'cannot remove SPP continuation migration while continuation audit links exist';
    END IF;
END;
$$;

DROP TRIGGER movement_transactions_service_resumption ON movement_transactions;
DROP FUNCTION derive_spp_service_resumption();
DROP INDEX movement_transactions_service_resumption_uidx;
DROP INDEX movement_transactions_filling_shed_spp_number_uidx;
CREATE UNIQUE INDEX movement_transactions_filling_shed_spp_number_uidx
    ON movement_transactions (spp_number) WHERE out_destination = 'FILLING_SHED_KUIN';
ALTER TABLE movement_transactions
    DROP CONSTRAINT movement_transactions_service_resumption_check,
    DROP COLUMN resumed_from_service_transaction_id;
DROP FUNCTION unitlog_spp_completed(TEXT);
DROP VIEW movement_spp_return_context;
DROP TRIGGER movement_cycles_completed_immutable ON movement_cycles;
DROP FUNCTION reject_completed_cycle_mutation();
