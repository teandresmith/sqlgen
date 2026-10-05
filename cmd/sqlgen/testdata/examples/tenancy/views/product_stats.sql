-- product_stats — a view-tenancy regression fixture: a view that projects the tenant
-- column and is therefore tenant-scoped by detection alone, with no
-- `views.product_stats.tenancy` block (PRD §29.2.5). @pk makes Get available,
-- so all five read methods are exercised.
--
-- @pk: id
-- @type order_line_count: int64

CREATE VIEW product_stats AS
SELECT
    p.id,
    p.workspace_id,
    p.name,
    p.price,
    COUNT(oi.product_id) AS order_line_count
FROM products p
LEFT JOIN order_items oi
       ON oi.product_id = p.id AND oi.workspace_id = p.workspace_id
GROUP BY p.id, p.workspace_id, p.name, p.price;
