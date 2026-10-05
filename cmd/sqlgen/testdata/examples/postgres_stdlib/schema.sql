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
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
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
COMMENT ON TABLE audit.events IS 'General system audit events';

-- ============================================================
-- Soft delete tables: test timestamp and boolean strategies
-- ============================================================

-- articles: timestamp-based soft delete (deleted_at TIMESTAMPTZ).
-- No DEFAULT on `id` — forces PKStrategyApp under PG auto-detection
-- (context_table.go:detectPostgresPK), so codegen calls `pkAutoGenType`
-- at Create time. Combined with no `uuid → uuid.UUID` override in this
-- example, the resolved PK GoType stays `string` and the
-- `uuid.NewString()` path is exercised end-to-end against a real
-- database.
CREATE TABLE articles (
    id UUID PRIMARY KEY,
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
    price NUMERIC(10, 2) NOT NULL,
    nullable_price NUMERIC(10, 2),
    related_ids UUID[],
    nullable_ref UUID,
    allowed_roles user_role[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

COMMENT ON TABLE warehouses IS 'Storage warehouses with type-overridden columns';
COMMENT ON COLUMN warehouses.id IS 'Unique identifier';
COMMENT ON COLUMN warehouses.external_id IS 'External reference ID (KSUID)';
COMMENT ON COLUMN warehouses.name IS 'Warehouse name';
COMMENT ON COLUMN warehouses.price IS 'Operational cost';
COMMENT ON COLUMN warehouses.nullable_price IS 'Optional secondary price';
COMMENT ON COLUMN warehouses.related_ids IS 'Related warehouse UUIDs';
COMMENT ON COLUMN warehouses.nullable_ref IS 'Optional reference to another warehouse';
COMMENT ON COLUMN warehouses.allowed_roles IS 'Permitted user roles for warehouse access';
COMMENT ON COLUMN warehouses.created_at IS 'Row creation timestamp';
