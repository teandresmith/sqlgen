-- @pk: id
-- @type stock: int32

CREATE VIEW product_summary AS
SELECT
    p.id,
    p.name,
    p.price,
    p.stock
FROM products p;
