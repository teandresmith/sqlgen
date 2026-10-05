-- sqlite: comprehensive SQLite E2E scenario
-- Tests all 5 type affinities (INTEGER, TEXT, REAL, BLOB, BOOLEAN),
-- relationships, RETURNING clause, ON CONFLICT upsert, and pagination

-- users: broad SQLite type coverage
CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    email TEXT NOT NULL UNIQUE,
    role TEXT NOT NULL DEFAULT 'viewer',
    age INTEGER,
    bio TEXT,
    is_active BOOLEAN NOT NULL DEFAULT 1,
    balance REAL NOT NULL DEFAULT 0.0,
    login_count INTEGER NOT NULL DEFAULT 0,
    avatar BLOB,
    score REAL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME DEFAULT (datetime('now'))
);

-- profiles: O2O with users
CREATE TABLE profiles (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL UNIQUE,
    website TEXT,
    github_handle TEXT,
    verified BOOLEAN NOT NULL DEFAULT 0,
    FOREIGN KEY (user_id) REFERENCES users(id)
);

-- categories
CREATE TABLE categories (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    description TEXT
);

-- products: O2M from categories
CREATE TABLE products (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    category_id INTEGER NOT NULL,
    title TEXT NOT NULL,
    price REAL NOT NULL,
    weight_kg REAL,
    in_stock BOOLEAN NOT NULL DEFAULT 1,
    sku TEXT NOT NULL UNIQUE,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (category_id) REFERENCES categories(id)
);

-- orders: O2M from users
CREATE TABLE orders (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    total REAL NOT NULL DEFAULT 0.0,
    notes TEXT,
    ordered_at DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (user_id) REFERENCES users(id)
);

-- order_items: junction-like
CREATE TABLE order_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id INTEGER NOT NULL,
    product_id INTEGER NOT NULL,
    quantity INTEGER NOT NULL,
    unit_price REAL NOT NULL,
    FOREIGN KEY (order_id) REFERENCES orders(id),
    FOREIGN KEY (product_id) REFERENCES products(id),
    CONSTRAINT order_items_amounts_positive CHECK (unit_price >= 0 AND quantity > 0)
);

-- user_categories: M2M junction
CREATE TABLE user_categories (
    user_id INTEGER NOT NULL,
    category_id INTEGER NOT NULL,
    PRIMARY KEY (user_id, category_id),
    FOREIGN KEY (user_id) REFERENCES users(id),
    FOREIGN KEY (category_id) REFERENCES categories(id)
);

-- articles: timestamp-based soft delete (deleted_at DATETIME)
CREATE TABLE articles (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    title TEXT NOT NULL,
    body TEXT,
    author TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    deleted_at DATETIME
);

-- tags: boolean-based soft delete (is_deleted BOOLEAN)
CREATE TABLE tags (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    is_deleted BOOLEAN NOT NULL DEFAULT 0
);

-- product_tag_labels: 3-column composite PK for testing composite key operations
CREATE TABLE product_tag_labels (
    product_id INTEGER NOT NULL,
    tag_name TEXT NOT NULL,
    label TEXT NOT NULL,
    description TEXT,
    PRIMARY KEY (product_id, tag_name, label),
    FOREIGN KEY (product_id) REFERENCES products(id)
);

-- assets / documents: sub-categorized polymorphism fixture (PRD §13.7).
-- SQLite has no native enum type, so the discriminator column is plain TEXT
-- and the dispatching predicates match against literal strings.
CREATE TABLE assets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE documents (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    entity_id INTEGER NOT NULL,
    entity_type TEXT NOT NULL,
    name TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

-- Junction table for m2m sub-categorized polymorphism.
CREATE TABLE asset_document_links (
    asset_id INTEGER NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    document_id INTEGER NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    PRIMARY KEY (asset_id, document_id)
);

-- warehouses: type override testing (real → decimal.Decimal, varchar → ksuid.KSUID)
CREATE TABLE warehouses (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    external_id VARCHAR(27) NOT NULL UNIQUE,
    name TEXT NOT NULL,
    price REAL NOT NULL,
    nullable_price REAL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

-- reserved_word_columns: PRD §8.5 (Reserved Word Handling) integration fixture.
-- Every non-PK column below is a Go keyword, predeclared identifier, or
-- generator-reserved local (`ctx`, `err`). Generated CRUD code must compile
-- and round-trip values for every column — the structural guarantee is
-- pinned by the `safeGoIdent` template helper that escapes any
-- column-derived local that would shadow a Go reserved word.
CREATE TABLE reserved_word_columns (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    type TEXT NOT NULL DEFAULT 'kind',
    interface TEXT NOT NULL DEFAULT 'iface',
    func TEXT NOT NULL DEFAULT 'callable',
    map TEXT NOT NULL DEFAULT 'lookup',
    new TEXT NOT NULL DEFAULT 'init',
    make TEXT NOT NULL DEFAULT 'build',
    len INTEGER NOT NULL DEFAULT 0,
    error TEXT NOT NULL DEFAULT '',
    ctx TEXT NOT NULL DEFAULT '',
    err TEXT NOT NULL DEFAULT ''
);

-- digit_leading_columns: PRD §8.5 "Digit-Leading Handling" integration
-- fixture. Every non-PK column begins with a digit, which is not a legal
-- leading character for Go identifiers. The toPascalCase / toCamelCase /
-- safeGoIdent guard prefixes every digit-leading name with `Col` (exposed)
-- or `col` (locals) so the generated CRUD code compiles. Mirrors the
-- reserved_word_columns shape — Create / Get / Update / CreateMany /
-- HardDelete must all round-trip values for every column.
CREATE TABLE digit_leading_columns (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    "2010_revenue" REAL NOT NULL DEFAULT 0.0,
    "2024_quota" INTEGER NOT NULL DEFAULT 0,
    "1st_place" TEXT NOT NULL DEFAULT 'unranked',
    "3d_model_url" TEXT NOT NULL DEFAULT ''
);

-- binary_keys / binary_key_events: the key-column fixture. A BLOB
-- primary key resolves to []byte, which now filters through
-- comparator.Opaque[[]byte] rather than comparator.String. The generated
-- client assigns its PK-filter expressions straight into the table's own
-- filter struct, so `pkFilterExpr` / `pkFilterInExpr` and
-- `resolveComparatorType` have to agree on the family -- and they are separate
-- functions that once derived it independently. When they disagreed
-- the package did not compile, at 10+ sites per table: GetByID, GetMany, the
-- Create / Update / Delete ID collections, and every relationship loader.
--
-- No example had a key column of any affected type, which is why the whole
-- class survived: tenancy overrides `blob` to uuid.UUID, the only postgres
-- INET column is overridden to netip.Addr, and the mysql / sqlite `avatar
-- BLOB` columns are ordinary nullable attributes. A unit test can pin the
-- expression's type; only a compiled module proves all 10+ sites, so the
-- child table below exists to pull in the O2M loader path as well.
CREATE TABLE binary_keys (
    id BLOB PRIMARY KEY,
    name TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE binary_key_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    binary_key_id BLOB NOT NULL REFERENCES binary_keys(id),
    label TEXT NOT NULL
);

-- Manifest extended-metadata fixtures (PRD §30.7). Composite
-- (non-unique) index + a partial index whose WHERE predicate round-trips into
-- the manifest index `where` field (SQLite supports partial indexes). Both are
-- non-unique, so they add no FindBy* method to the generated client.
CREATE INDEX idx_order_items_order_product ON order_items (order_id, product_id);
CREATE INDEX idx_articles_active ON articles (author) WHERE deleted_at IS NULL;

-- key_only_rows: the key is the table's only column and the database
-- generates it, so CreateKeyOnlyRowInput has no field and no INSERT can name a
-- writable column.
CREATE TABLE key_only_rows (id INTEGER PRIMARY KEY AUTOINCREMENT);

-- A has-one edge whose parent spells its PK differently from the
-- target. owners.OwnerProfile joins owner_profiles.owner_id to owners.owner_key,
-- not to the target's own `id`.
CREATE TABLE owners (
    owner_key INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL
);

CREATE TABLE owner_profiles (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    owner_id INTEGER NOT NULL UNIQUE,
    bio TEXT,
    FOREIGN KEY (owner_id) REFERENCES owners(owner_key)
);

-- slugged_rows: `slug` is UNIQUE with a constant DEFAULT and `read_only` on the
-- API, and every column has a DEFAULT, so a zero input supplies no column.
CREATE TABLE slugged_rows (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL DEFAULT '',
    slug TEXT NOT NULL UNIQUE DEFAULT 'x'
);
