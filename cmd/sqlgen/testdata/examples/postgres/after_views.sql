-- Runs after views/. The unique index backing order_totals' @pk annotation,
-- which REFRESH MATERIALIZED VIEW CONCURRENTLY requires.
CREATE UNIQUE INDEX order_totals_product_id_idx ON order_totals (product_id);
