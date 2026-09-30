ALTER TABLE unit_snapshot DROP CONSTRAINT unit_snapshot_current_transaction_fk;
DROP TABLE movement_transactions;
DROP TABLE unit_snapshot;
DROP TABLE unit_sync_runs;
DROP FUNCTION reject_movement_transaction_mutation();
