-- A regular view over products with a declared key. Its WHERE leaves rows out,
-- so a read that ignores the view's query is observable.
--
-- @pk: id

CREATE VIEW premium_products AS
SELECT id, name, price
FROM products
WHERE price >= 200;
