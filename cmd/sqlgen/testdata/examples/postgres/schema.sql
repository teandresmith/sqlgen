-- postgres: comprehensive PostgreSQL E2E scenario
-- Tests enums, composite types, domain types, arrays, JSONB, relationships, pagination,
-- and multi-schema disambiguation (audit.users collides with public.users)

-- Enums
CREATE TYPE user_role AS ENUM ('admin', 'editor', 'viewer');
CREATE TYPE order_status AS ENUM ('pending', 'processing', 'shipped', 'delivered', 'cancelled');

-- Composite type (generates Go struct with JSON Scan/Value)
CREATE TYPE address AS (
    street text,
    city text,
    state text,
    zip text,
    country text
);

-- Domain types (generate Go type aliases)
CREATE DOMAIN email AS text CHECK (VALUE ~ '^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$');
CREATE DOMAIN positive_int AS integer CHECK (VALUE > 0);
-- region_name: scoped domain so the warehouses table can override it to a
-- custom Optional[string] wrapper without affecting other VARCHAR columns
-- (notably warehouses.name, which stays a plain Go string). Exercises
-- user-declared `valid_field` / `underlying_field` on the custom wrapper,
-- separate from the built-in uuid.NullUUID path.
CREATE DOMAIN region_name AS VARCHAR(64);

-- users: enums, arrays, JSONB, domain types
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email email NOT NULL UNIQUE,
    name TEXT NOT NULL,
    role user_role NOT NULL DEFAULT 'viewer',
    tags TEXT[] DEFAULT '{}',
    metadata JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- profiles: O2O with users (via UNIQUE FK), composite type (JSONB), integer array
CREATE TABLE profiles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL UNIQUE REFERENCES users(id),
    bio TEXT,
    avatar_url TEXT,
    address JSONB,
    scores INTEGER[],
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- categories: standalone lookup table
CREATE TABLE categories (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL UNIQUE,
    description TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- products: O2M from categories, domain type, arrays, JSONB, boolean
CREATE TABLE products (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    description TEXT,
    price NUMERIC(10, 2) NOT NULL,
    quantity positive_int NOT NULL DEFAULT 1,
    is_active BOOLEAN NOT NULL DEFAULT true,
    category_id UUID NOT NULL REFERENCES categories(id),
    tags TEXT[] DEFAULT '{}',
    metadata JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- orders: O2M from users, enum status
CREATE TABLE orders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id),
    status order_status NOT NULL DEFAULT 'pending',
    total NUMERIC(10, 2) NOT NULL DEFAULT 0,
    notes TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- order_items: O2M from orders and products, domain type
CREATE TABLE order_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id UUID NOT NULL REFERENCES orders(id),
    product_id UUID NOT NULL REFERENCES products(id),
    quantity positive_int NOT NULL DEFAULT 1,
    unit_price NUMERIC(10, 2) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT order_items_amounts_positive CHECK (unit_price >= 0 AND quantity > 0)
);

-- user_categories: M2M junction between users and categories
CREATE TABLE user_categories (
    user_id UUID NOT NULL REFERENCES users(id),
    category_id UUID NOT NULL REFERENCES categories(id),
    PRIMARY KEY (user_id, category_id)
);

-- Enum comments
COMMENT ON TYPE user_role IS 'Access level assigned to a user account';
COMMENT ON TYPE order_status IS 'Lifecycle state of a customer order';

-- Composite type comments
COMMENT ON TYPE address IS 'Mailing address components';

-- Domain comments
COMMENT ON DOMAIN email IS 'RFC 5321 email address';
COMMENT ON DOMAIN positive_int IS 'Integer constrained to values greater than zero';

-- Table and column comments: users
COMMENT ON TABLE users IS 'Registered user accounts';
COMMENT ON COLUMN users.id IS 'Unique identifier';
COMMENT ON COLUMN users.email IS 'Login email address';
COMMENT ON COLUMN users.name IS 'Display name';
COMMENT ON COLUMN users.role IS 'Authorization level';
COMMENT ON COLUMN users.tags IS 'Freeform labels for filtering';
COMMENT ON COLUMN users.metadata IS 'Arbitrary key-value data';
COMMENT ON COLUMN users.created_at IS 'Row creation timestamp';
COMMENT ON COLUMN users.updated_at IS 'Last modification timestamp';

-- Table and column comments: profiles
COMMENT ON TABLE profiles IS 'Extended user profile information';
COMMENT ON COLUMN profiles.id IS 'Unique identifier';
COMMENT ON COLUMN profiles.user_id IS 'Owning user reference';
COMMENT ON COLUMN profiles.bio IS 'Short biography';
COMMENT ON COLUMN profiles.avatar_url IS 'Profile image URL';
COMMENT ON COLUMN profiles.address IS 'Mailing address stored as JSON';
COMMENT ON COLUMN profiles.scores IS 'Numeric score history';
COMMENT ON COLUMN profiles.created_at IS 'Row creation timestamp';

-- Table and column comments: categories
COMMENT ON TABLE categories IS 'Product classification categories';
COMMENT ON COLUMN categories.id IS 'Unique identifier';
COMMENT ON COLUMN categories.name IS 'Category display name';
COMMENT ON COLUMN categories.description IS 'Optional long-form description';
COMMENT ON COLUMN categories.created_at IS 'Row creation timestamp';

-- Table and column comments: products
COMMENT ON TABLE products IS 'Catalog of products available for sale';
COMMENT ON COLUMN products.id IS 'Unique identifier';
COMMENT ON COLUMN products.name IS 'Product display name';
COMMENT ON COLUMN products.description IS 'Optional long-form description';
COMMENT ON COLUMN products.price IS 'Unit price in the default currency';
COMMENT ON COLUMN products.quantity IS 'Available stock count';
COMMENT ON COLUMN products.is_active IS 'Whether the product is currently available';
COMMENT ON COLUMN products.category_id IS 'Parent category reference';
COMMENT ON COLUMN products.tags IS 'Freeform labels for filtering';
COMMENT ON COLUMN products.metadata IS 'Arbitrary key-value data';
COMMENT ON COLUMN products.created_at IS 'Row creation timestamp';
COMMENT ON COLUMN products.updated_at IS 'Last modification timestamp';

-- Table and column comments: orders
COMMENT ON TABLE orders IS 'Customer purchase orders';
COMMENT ON COLUMN orders.id IS 'Unique identifier';
COMMENT ON COLUMN orders.user_id IS 'Ordering user reference';
COMMENT ON COLUMN orders.status IS 'Current lifecycle state';
COMMENT ON COLUMN orders.total IS 'Order total in the default currency';
COMMENT ON COLUMN orders.notes IS 'Optional order notes from the customer';
COMMENT ON COLUMN orders.created_at IS 'Row creation timestamp';
COMMENT ON COLUMN orders.updated_at IS 'Last modification timestamp';

-- Table and column comments: order_items
COMMENT ON TABLE order_items IS 'Individual line items within an order';
COMMENT ON COLUMN order_items.id IS 'Unique identifier';
COMMENT ON COLUMN order_items.order_id IS 'Parent order reference';
COMMENT ON COLUMN order_items.product_id IS 'Purchased product reference';
COMMENT ON COLUMN order_items.quantity IS 'Number of units ordered';
COMMENT ON COLUMN order_items.unit_price IS 'Price per unit at time of purchase';
COMMENT ON COLUMN order_items.created_at IS 'Row creation timestamp';

-- Table and column comments: user_categories
COMMENT ON TABLE user_categories IS 'Junction table linking users to their preferred categories';
COMMENT ON COLUMN user_categories.user_id IS 'User side of the relationship';
COMMENT ON COLUMN user_categories.category_id IS 'Category side of the relationship';

-- ============================================================
-- Audit schema: tests multi-schema struct name disambiguation
-- "audit.users" collides with "public.users" → AuditUser vs PublicUser
-- ============================================================

CREATE SCHEMA IF NOT EXISTS audit;

-- audit.users: audit log snapshots of user changes (same table name, different schema)
CREATE TABLE audit.users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    action TEXT NOT NULL,
    details JSONB,
    -- source_ip is the fixture for `column_map.<col>.type` + `.import`:
    -- an external Go type (netip.Addr) that no other column in this file pulls
    -- in, so the generated import is emitted by the override or not at all.
    source_ip INET,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- audit.events: general system events (unique name, no prefix needed)
CREATE TABLE audit.events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    payload JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

COMMENT ON TABLE audit.users IS 'Audit log of user account changes';
COMMENT ON COLUMN audit.users.source_ip IS 'Client address the change originated from';
COMMENT ON TABLE audit.events IS 'General system audit events';

-- ============================================================
-- A same-named pair reached only through declared relationships
-- whose `table:` is schema-qualified (sqlgen.yml). There are no REFERENCES
-- clauses, so no FK-inferred edge names either table: the qualified spelling
-- is the only thing that picks the target. A bare `table: notes` is an error,
-- since both schemas declare the name (PRD §5.5).
-- ============================================================

-- public.notes: a user's own notes. The two audit.events has-one edges decide
-- which side holds the FK by looking for it on the target, and each FK column
-- exists in one schema only (source_event_id here, event_id on audit.notes).
-- Whichever `notes` a schema-blind lookup finds first, one edge's join flips.
CREATE TABLE notes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL,
    source_event_id UUID UNIQUE,
    body TEXT NOT NULL
);

-- audit.notes: reviewer notes about a public user. user_id holds public.users
-- ids, and audit.users is the same-named decoy for the belongs-to edge. The key
-- is a BIGSERIAL, not a UUID, so an M2M edge wired against public.notes' key
-- would not compile.
CREATE TABLE audit.notes (
    id BIGSERIAL PRIMARY KEY,
    user_id UUID NOT NULL,
    event_id UUID UNIQUE,
    severity TEXT NOT NULL
);

-- audit.note_watchers: the junction for public.users ↔ audit.notes.
CREATE TABLE audit.note_watchers (
    user_id UUID NOT NULL,
    note_id BIGINT NOT NULL,
    PRIMARY KEY (user_id, note_id)
);

-- ============================================================
-- Soft delete tables: test timestamp and boolean strategies
-- ============================================================

-- articles: timestamp-based soft delete (deleted_at TIMESTAMPTZ)
CREATE TABLE articles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title TEXT NOT NULL,
    body TEXT,
    author TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMPTZ
);

-- tags: boolean-based soft delete (is_deleted BOOLEAN)
CREATE TABLE tags (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL UNIQUE,
    is_deleted BOOLEAN NOT NULL DEFAULT FALSE
);

COMMENT ON TABLE articles IS 'Blog articles with timestamp soft delete';
COMMENT ON TABLE tags IS 'Content tags with boolean soft delete';

-- ============================================================
-- Composite PK: 3-column composite primary key for testing
-- ============================================================

-- product_tag_labels: associates a product with a tag and label (3-column composite PK)
CREATE TABLE product_tag_labels (
    product_id UUID NOT NULL REFERENCES products(id),
    tag_name TEXT NOT NULL,
    label TEXT NOT NULL,
    description TEXT,
    PRIMARY KEY (product_id, tag_name, label)
);

COMMENT ON TABLE product_tag_labels IS 'Product tag labels with 3-column composite primary key';
COMMENT ON COLUMN product_tag_labels.product_id IS 'Product reference';
COMMENT ON COLUMN product_tag_labels.tag_name IS 'Tag name';
COMMENT ON COLUMN product_tag_labels.label IS 'Label value';
COMMENT ON COLUMN product_tag_labels.description IS 'Optional description';

-- ============================================================
-- Type override table: tests table-level overrides.types config
-- uuid → uuid.UUID, numeric → decimal.Decimal, text → ksuid.KSUID
-- ============================================================

CREATE TABLE warehouses (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    external_id TEXT NOT NULL UNIQUE,
    name VARCHAR(255) NOT NULL,
    region region_name,
    price NUMERIC(10, 2) NOT NULL,
    nullable_price NUMERIC(10, 2),
    related_ids UUID[],
    nullable_ref UUID REFERENCES warehouses(id),
    allowed_roles user_role[] NOT NULL DEFAULT '{}',
    tag_ids TEXT[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

COMMENT ON TABLE warehouses IS 'Storage warehouses with type-overridden columns';
COMMENT ON COLUMN warehouses.id IS 'Unique identifier';
COMMENT ON COLUMN warehouses.external_id IS 'External reference ID (KSUID)';
COMMENT ON COLUMN warehouses.name IS 'Warehouse name';
COMMENT ON COLUMN warehouses.region IS 'Optional regional designation (custom Optional[string] override)';
COMMENT ON COLUMN warehouses.price IS 'Operational cost';
COMMENT ON COLUMN warehouses.nullable_price IS 'Optional secondary price';
COMMENT ON COLUMN warehouses.related_ids IS 'Related warehouse UUIDs';
COMMENT ON COLUMN warehouses.nullable_ref IS 'Optional reference to another warehouse';
COMMENT ON COLUMN warehouses.allowed_roles IS 'Permitted user roles for warehouse access';
COMMENT ON COLUMN warehouses.tag_ids IS 'Array of KSUID tag references';
COMMENT ON COLUMN warehouses.created_at IS 'Row creation timestamp';

-- ============================================================
-- Sub-categorized polymorphism (PRD §13.7): a single parent declares
-- multiple relationships into the same child table, distinguished by a
-- raw-SQL filter on a discriminator column.
-- ============================================================

CREATE TYPE document_entity_type_enum AS ENUM (
    'asset.primary',
    'asset.attachment',
    'asset.invoice',
    'spv'
);

CREATE TABLE assets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE documents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_id UUID NOT NULL,
    entity_type document_entity_type_enum NOT NULL,
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Junction table for m2m sub-categorized polymorphism (coverage of the
-- m2m loader's sql.Raw(filter) target-query WHERE path).
CREATE TABLE asset_document_links (
    asset_id UUID NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    document_id UUID NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    PRIMARY KEY (asset_id, document_id)
);

COMMENT ON TABLE assets IS 'Polymorphic parent for sub-categorized document relationships';
COMMENT ON TABLE documents IS 'Polymorphic child rows; entity_type sub-categorizes per-parent relationship';
COMMENT ON TABLE asset_document_links IS 'Junction table for m2m sub-categorized polymorphism';

-- ============================================================
-- No-PK tables: primary_key.columns override path
-- ============================================================
-- Both tables below have no inline PRIMARY KEY clause and would be skipped
-- from generation with a warning (PRD §9.4b) unless an override is supplied
-- via tables.<name>.primary_key.columns in sqlgen.yml. The example wires
-- those overrides for both tables so the full PK-keyed client surface is
-- emitted and exercised end-to-end (cmd/sqlgen/testdata/examples/postgres/
-- tests/no_pk_test.go).

-- counters: post-CREATE UNIQUE on (key). The override in sqlgen.yml
-- promotes `key` to the PK position — exercises the SINGLE-column override
-- path. Without the override, the table would be skipped with a warning.
CREATE TABLE counters (
    key TEXT NOT NULL,
    slot TEXT,
    count BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
ALTER TABLE counters ADD CONSTRAINT counters_key_uq UNIQUE (key);
ALTER TABLE counters ADD CONSTRAINT counters_slot_uq UNIQUE (slot);

COMMENT ON TABLE counters IS 'Counter rows keyed via primary_key.columns override (single-column PK)';
COMMENT ON COLUMN counters.key IS 'Logical counter identity (post-ALTER UNIQUE, promoted to PK via override)';
COMMENT ON COLUMN counters.slot IS 'Second UNIQUE that does NOT cover the primary key — the only example shape where a caller-known key still has to be read back after an upsert (PRD 9.5). Nullable so existing rows leave it unset; NULL never matches under a unique index.';
COMMENT ON COLUMN counters.count IS 'Current counter value';
COMMENT ON COLUMN counters.updated_at IS 'Last modification timestamp';

-- rate_limits: post-CREATE UNIQUE on (org_id, bucket). The override in
-- sqlgen.yml promotes those columns to the PK position — exercises the
-- COMPOSITE override path. Without the override, the table would be
-- skipped with a warning.
CREATE TABLE rate_limits (
    org_id BIGINT NOT NULL,
    bucket TEXT NOT NULL,
    count INTEGER NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
ALTER TABLE rate_limits ADD CONSTRAINT rate_limits_org_bucket_uq UNIQUE (org_id, bucket);

COMMENT ON TABLE rate_limits IS 'Per-org rate-limit buckets keyed via primary_key.columns override (composite PK)';
COMMENT ON COLUMN rate_limits.org_id IS 'Tenant identifier';
COMMENT ON COLUMN rate_limits.bucket IS 'Bucket key (e.g. minute / hour window)';
COMMENT ON COLUMN rate_limits.count IS 'Observed request count';
COMMENT ON COLUMN rate_limits.updated_at IS 'Last modification timestamp';

-- Manifest extended-metadata fixtures (PRD §30.7). Composite
-- (non-unique) index surfaces as an idx_* entry in the entity's indexes[]
-- (unique:false, method:btree); being non-unique it adds no FindBy* method to
-- the generated client. The partial index's WHERE predicate round-trips into
-- the manifest index `where` field (PG supports partial indexes).
CREATE INDEX idx_order_items_order_product ON order_items (order_id, product_id);
CREATE INDEX idx_articles_active ON articles (author) WHERE deleted_at IS NULL;

-- default_only_rows: every writable column carries a DEFAULT, so a zero
-- CreateDefaultOnlyRowInput supplies no column at all and the INSERT takes
-- the dialect's all-defaults form.
CREATE TABLE default_only_rows (
    id BIGSERIAL PRIMARY KEY,
    label TEXT NOT NULL DEFAULT 'unset',
    hits INTEGER NOT NULL DEFAULT 0,
    active BOOLEAN NOT NULL DEFAULT TRUE
);

-- key_only_rows: the key is the table's only column and the database
-- generates it, so CreateKeyOnlyRowInput has no field and no INSERT can name a
-- writable column.
CREATE TABLE key_only_rows (id BIGSERIAL PRIMARY KEY);

-- slugged_rows: `slug` is UNIQUE with a constant DEFAULT and `read_only` on the
-- API, and every column has a DEFAULT, so a zero input supplies no column.
CREATE TABLE slugged_rows (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL DEFAULT '',
    slug TEXT NOT NULL UNIQUE DEFAULT 'x'
);
