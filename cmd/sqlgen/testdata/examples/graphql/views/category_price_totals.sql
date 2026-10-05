-- category_price_totals — the MATERIALIZED view fixture. PRD §26.4 states
-- a matview emits the identical read surface to a regular view and that
-- `Refresh` / `RefreshConcurrently` stay Go-client-only; before this fixture no
-- API-enabled example carried a matview, so neither half of that sentence was
-- tested. The postgres example has `order_totals` but no `api:` block.
--
-- It aggregates `products`, which is a SHARED table — deliberately, so the
-- matview isolates the "matview == view on the read surface" claim from the
-- tenancy claim `workspace_note_summary` carries.
--
-- Views have no primary-key fallback for cursor_keys (§4.13), and the
-- inherited default `id` does not exist here, so the connection query needs
-- the explicit `views.category_price_totals.cursor_keys` entry in sqlgen.yml.
--
-- @pk: category_id
-- @type product_count: int32

CREATE MATERIALIZED VIEW category_price_totals AS
SELECT
    p.category_id,
    COUNT(p.id) AS product_count,
    SUM(p.price) AS total_price,
    MAX(p.price) AS max_price
FROM products p
GROUP BY p.category_id;
