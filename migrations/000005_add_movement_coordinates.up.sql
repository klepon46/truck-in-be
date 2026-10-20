ALTER TABLE movement_transactions
    ADD COLUMN coordinates VARCHAR(100) NULL,
    ADD CONSTRAINT movement_transactions_coordinates_check CHECK (
        coordinates IS NULL OR CASE
            WHEN coordinates ~ '^[+-]?([0-9]+([.][0-9]*)?|[.][0-9]+),[+-]?([0-9]+([.][0-9]*)?|[.][0-9]+)$'
            THEN split_part(coordinates, ',', 1)::NUMERIC BETWEEN -90 AND 90
                AND split_part(coordinates, ',', 2)::NUMERIC BETWEEN -180 AND 180
            ELSE FALSE
        END
    );
