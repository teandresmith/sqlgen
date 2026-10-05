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
    STRING_AGG(p.name, ', ' ORDER BY p.name) AS product_names,
    BOOL_OR(p.is_active) AS has_active_product,
    JSON_AGG(p.name) AS product_names_json,
    -- ARRAY_AGG resolves its element type from the source column (text -> []string).
    -- FILTER is what keeps an empty LEFT JOIN group from producing {NULL}, which has
    -- no non-pointer scan target; with it, an empty group yields SQL NULL -> nil slice.
    ARRAY_AGG(p.name) FILTER (WHERE p.name IS NOT NULL) AS product_name_list
FROM categories c
LEFT JOIN products p ON p.category_id = c.id
GROUP BY c.name;
