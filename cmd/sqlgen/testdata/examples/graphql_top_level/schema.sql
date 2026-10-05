-- graphql_top_level: minimal schema for the top-level GraphQL output-dir E2E
-- proof. Trimmed to two tables — just enough to emit a graph/
-- package (row types, a query/mutation surface, and one M2O relationship
-- resolver) and prove that root-relative schema_dir / resolver_dir (§26.5.8)
-- place graph at <root>/graph (a SIBLING of ./models) with import paths that
-- resolve and compile. Resolver / walker / connection behavior is exercised
-- by the sibling `graphql` example; this module only proves topology + import
-- resolution for the top-level layout.

-- categories: simple PK (BIGSERIAL → int64). Reverse target of products.
CREATE TABLE categories (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    description TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- products: UUID PK, M2O products → categories (BIGINT FK to BIGSERIAL PK),
-- numeric price (cat 4 Decimal via global override). The M2O relationship
-- emits one relationship resolver into the top-level graph/ package.
CREATE TABLE products (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    price NUMERIC(12, 2) NOT NULL,
    category_id BIGINT NOT NULL REFERENCES categories(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
