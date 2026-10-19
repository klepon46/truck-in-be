DROP INDEX movement_transactions_work_order_number_uidx;
CREATE UNIQUE INDEX movement_transactions_work_order_number_uidx
    ON movement_transactions (work_order_number) WHERE work_order_number IS NOT NULL;

ALTER TABLE movement_transactions DROP CONSTRAINT movement_transactions_shape_check;

ALTER TABLE movement_transactions
    ADD CHECK (
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
    );
