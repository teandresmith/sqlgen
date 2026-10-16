-- Runs after views/. category_price_totals asserts `@pk: category_id`, which
-- makes the matview concurrently refreshable (PRD §16.5.2), so the generated
-- client emits RefreshConcurrently. That statement needs a real UNIQUE index;
-- the annotation alone does not create one.
CREATE UNIQUE INDEX category_price_totals_category_id_idx ON category_price_totals (category_id);
