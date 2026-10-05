-- @pk: product_id
-- @type total_quantity: int64

CREATE MATERIALIZED VIEW order_totals AS
SELECT
    oi.product_id,
    COUNT(oi.id) AS line_count,
    SUM(oi.quantity) AS total_quantity
FROM order_items oi
GROUP BY oi.product_id;
