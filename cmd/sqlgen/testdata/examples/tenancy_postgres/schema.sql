-- tenancy_postgres: pgx leg for tenant capture (uuid tenant type,
-- native RETURNING). Covers the single-numeric-PK and composite-PK
-- (tenant outside the PK) shapes; the pgx-batch UpdateMany capture is the
-- path this module exists to compile and run.

CREATE TABLE articles (
    id BIGSERIAL PRIMARY KEY,
    workspace_id UUID NOT NULL,
    title TEXT NOT NULL,
    deleted_at TIMESTAMPTZ
);

CREATE TABLE line_items (
    order_id BIGINT NOT NULL,
    product_id BIGINT NOT NULL,
    workspace_id UUID NOT NULL,
    quantity BIGINT NOT NULL,
    PRIMARY KEY (order_id, product_id)
);
