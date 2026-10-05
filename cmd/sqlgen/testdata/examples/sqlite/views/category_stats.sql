-- @type product_count: int32
-- @nullable: category_name

CREATE VIEW category_stats AS
SELECT
    c.name AS category_name,
    COUNT(p.id) AS product_count,
    SUM(p.price) AS total_price,
    AVG(p.price) AS avg_price,
    MIN(p.price) AS min_price,
    MAX(p.price) AS max_price,
    GROUP_CONCAT(p.title, ', ') AS product_names
FROM categories c
LEFT JOIN products p ON p.category_id = c.id
GROUP BY c.name;
