-- @pk: id
-- @type order_count: int32
-- @nullable: title

CREATE VIEW product_summary AS
SELECT
    p.id,
    p.title,
    p.price,
    c.name AS category_name,
    COUNT(oi.id) AS order_count
FROM products p
JOIN categories c ON c.id = p.category_id
LEFT JOIN order_items oi ON oi.product_id = p.id
GROUP BY p.id, p.title, p.price, c.name;
