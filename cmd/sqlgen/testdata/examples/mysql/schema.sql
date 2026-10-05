-- mysql: comprehensive MySQL E2E scenario
-- Tests inline enums, broad MySQL type coverage, relationships, JSON, unsigned integers,
-- ON DUPLICATE KEY UPDATE upsert, and pagination

-- users: broad MySQL type coverage
CREATE TABLE users (
    id BIGINT AUTO_INCREMENT PRIMARY KEY COMMENT 'Unique identifier',
    name VARCHAR(255) NOT NULL COMMENT 'Display name',
    email VARCHAR(255) NOT NULL UNIQUE COMMENT 'Login email address',
    role ENUM('admin', 'editor', 'viewer') NOT NULL DEFAULT 'viewer' COMMENT 'Authorization level',
    permissions SET('read', 'write', 'delete', 'admin') NOT NULL DEFAULT 'read' COMMENT 'Granted permission flags',
    age SMALLINT COMMENT 'User age in years',
    bio TEXT COMMENT 'Short biography',
    is_active BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'Whether the account is active',
    balance DECIMAL(12,2) NOT NULL COMMENT 'Account balance in default currency',
    login_count INT NOT NULL DEFAULT 0 COMMENT 'Total number of logins',
    avatar BLOB COMMENT 'Profile image binary data',
    metadata JSON COMMENT 'Arbitrary key-value data',
    rating FLOAT COMMENT 'Average user rating',
    score DOUBLE COMMENT 'Computed reputation score',
    tiny_flag TINYINT COMMENT 'Small integer flag',
    medium_val MEDIUMINT COMMENT 'Medium-range integer value',
    big_unsigned BIGINT UNSIGNED COMMENT 'Large unsigned counter',
    birth_date DATE COMMENT 'Date of birth',
    last_login DATETIME COMMENT 'Most recent login timestamp',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT 'Row creation timestamp',
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT 'Last modification timestamp'
) COMMENT='Registered user accounts';

-- profiles: O2O with users
CREATE TABLE profiles (
    id BIGINT AUTO_INCREMENT PRIMARY KEY COMMENT 'Unique identifier',
    user_id BIGINT NOT NULL UNIQUE COMMENT 'Owning user reference',
    website VARCHAR(500) COMMENT 'Personal website URL',
    github_handle VARCHAR(100) COMMENT 'GitHub username',
    verified BOOLEAN NOT NULL DEFAULT FALSE COMMENT 'Whether the profile has been verified',
    preferences JSON COMMENT 'User preference settings',
    FOREIGN KEY (user_id) REFERENCES users(id)
) COMMENT='Extended user profile information';

-- categories
CREATE TABLE categories (
    id INT AUTO_INCREMENT PRIMARY KEY COMMENT 'Unique identifier',
    name VARCHAR(255) NOT NULL UNIQUE COMMENT 'Category display name',
    description TEXT COMMENT 'Optional long-form description'
) COMMENT='Product classification categories';

-- products: O2M from categories
CREATE TABLE products (
    id BIGINT AUTO_INCREMENT PRIMARY KEY COMMENT 'Unique identifier',
    category_id INT NOT NULL COMMENT 'Parent category reference',
    title VARCHAR(255) NOT NULL COMMENT 'Product display name',
    price DECIMAL(10,2) NOT NULL COMMENT 'Unit price in default currency',
    weight_kg DOUBLE COMMENT 'Product weight in kilograms',
    in_stock BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'Whether the product is currently available',
    sku VARCHAR(50) NOT NULL UNIQUE COMMENT 'Stock keeping unit code',
    attributes JSON NOT NULL COMMENT 'Structured product attributes',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT 'Row creation timestamp',
    FOREIGN KEY (category_id) REFERENCES categories(id)
) COMMENT='Catalog of products available for sale';

-- orders: O2M from users
CREATE TABLE orders (
    id BIGINT AUTO_INCREMENT PRIMARY KEY COMMENT 'Unique identifier',
    user_id BIGINT NOT NULL COMMENT 'Ordering user reference',
    status ENUM('pending', 'confirmed', 'shipped', 'delivered', 'cancelled') NOT NULL DEFAULT 'pending' COMMENT 'Current lifecycle state',
    total DECIMAL(12,2) NOT NULL COMMENT 'Order total in default currency',
    notes TEXT COMMENT 'Optional order notes from the customer',
    ordered_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT 'Order placement timestamp',
    FOREIGN KEY (user_id) REFERENCES users(id)
) COMMENT='Customer purchase orders';

-- order_items: junction-like
CREATE TABLE order_items (
    id BIGINT AUTO_INCREMENT PRIMARY KEY COMMENT 'Unique identifier',
    order_id BIGINT NOT NULL COMMENT 'Parent order reference',
    product_id BIGINT NOT NULL COMMENT 'Purchased product reference',
    quantity INT NOT NULL COMMENT 'Number of units ordered',
    unit_price DECIMAL(10,2) NOT NULL COMMENT 'Price per unit at time of purchase',
    FOREIGN KEY (order_id) REFERENCES orders(id),
    FOREIGN KEY (product_id) REFERENCES products(id),
    CONSTRAINT order_items_amounts_positive CHECK (unit_price >= 0 AND quantity > 0)
) COMMENT='Individual line items within an order';

-- user_categories: M2M junction
CREATE TABLE user_categories (
    user_id BIGINT NOT NULL COMMENT 'User side of the relationship',
    category_id INT NOT NULL COMMENT 'Category side of the relationship',
    slot INT COMMENT 'Display position of this category for this user. The UNIQUE below does NOT cover the primary key, which makes this the only MySQL table where a caller-known composite key still has to be read back after an upsert (PRD 9.5). Nullable so existing rows leave it unset; MySQL, like the other two dialects, allows repeated NULLs under a unique index.',
    PRIMARY KEY (user_id, category_id),
    UNIQUE KEY user_categories_user_slot_uq (user_id, slot),
    FOREIGN KEY (user_id) REFERENCES users(id),
    FOREIGN KEY (category_id) REFERENCES categories(id)
) COMMENT='Junction table linking users to their preferred categories';

-- user_events: O2M from users on a NULLABLE foreign key. Every
-- other FK in this schema is NOT NULL, and PRD §9.9.4 leaves an O2M or
-- has-one edge on a NOT NULL FK create-only, so without this table the nested
-- adoption path (connect, disconnect, clear) is unreachable on MySQL. A new
-- table rather than a widened orders.user_id, so no existing test changes
-- meaning. The mirror of the graphql example's events.user_id.
CREATE TABLE user_events (
    id BIGINT AUTO_INCREMENT PRIMARY KEY COMMENT 'Unique identifier',
    user_id BIGINT COMMENT 'Owning user reference; NULL while the event is unparented',
    action VARCHAR(255) NOT NULL COMMENT 'What happened',
    FOREIGN KEY (user_id) REFERENCES users(id)
) COMMENT='User activity events; the one nullable-FK child table in this schema';

-- articles: timestamp-based soft delete (deleted_at DATETIME)
CREATE TABLE articles (
    id BIGINT AUTO_INCREMENT PRIMARY KEY COMMENT 'Unique identifier',
    title VARCHAR(255) NOT NULL COMMENT 'Article title',
    body TEXT COMMENT 'Article body content',
    author VARCHAR(255) NOT NULL COMMENT 'Author name',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT 'Row creation timestamp',
    deleted_at DATETIME COMMENT 'Soft delete timestamp'
) COMMENT='Blog articles with timestamp soft delete';

-- tags: boolean-based soft delete (is_deleted BOOLEAN)
CREATE TABLE tags (
    id INT AUTO_INCREMENT PRIMARY KEY COMMENT 'Unique identifier',
    name VARCHAR(255) NOT NULL UNIQUE COMMENT 'Tag name',
    is_deleted BOOLEAN NOT NULL DEFAULT FALSE COMMENT 'Whether the tag is soft-deleted'
) COMMENT='Content tags with boolean soft delete';

-- product_tag_labels: 3-column composite PK for testing composite key operations
CREATE TABLE product_tag_labels (
    product_id BIGINT NOT NULL COMMENT 'Product reference',
    tag_name VARCHAR(255) NOT NULL COMMENT 'Tag name',
    label VARCHAR(255) NOT NULL COMMENT 'Label value',
    description TEXT COMMENT 'Optional description',
    PRIMARY KEY (product_id, tag_name, label),
    FOREIGN KEY (product_id) REFERENCES products(id)
) COMMENT='Product tag labels with 3-column composite primary key';

-- assets / documents: sub-categorized polymorphism fixture (PRD §13.7).
-- MySQL uses inline ENUM(...) for the discriminator column since it has no
-- standalone CREATE TYPE syntax.
CREATE TABLE assets (
    id BIGINT AUTO_INCREMENT PRIMARY KEY COMMENT 'Unique identifier',
    name VARCHAR(255) NOT NULL COMMENT 'Asset name',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT 'Row creation timestamp'
) COMMENT='Polymorphic parent for sub-categorized document relationships';

CREATE TABLE documents (
    id BIGINT AUTO_INCREMENT PRIMARY KEY COMMENT 'Unique identifier',
    entity_id BIGINT NOT NULL COMMENT 'Parent row identifier (polymorphic FK)',
    entity_type ENUM('asset.primary', 'asset.attachment', 'asset.invoice', 'spv') NOT NULL COMMENT 'Polymorphic discriminator',
    name VARCHAR(255) NOT NULL COMMENT 'Document display name',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT 'Row creation timestamp'
) COMMENT='Polymorphic child rows; entity_type sub-categorizes per-parent relationship';

-- Junction table for m2m sub-categorized polymorphism.
CREATE TABLE asset_document_links (
    asset_id BIGINT NOT NULL,
    document_id BIGINT NOT NULL,
    PRIMARY KEY (asset_id, document_id),
    FOREIGN KEY (asset_id) REFERENCES assets(id) ON DELETE CASCADE,
    FOREIGN KEY (document_id) REFERENCES documents(id) ON DELETE CASCADE
) COMMENT='Junction table for m2m sub-categorized polymorphism';

-- warehouses: type override testing (decimal → decimal.Decimal, text → ksuid.KSUID)
CREATE TABLE warehouses (
    id BIGINT AUTO_INCREMENT PRIMARY KEY COMMENT 'Unique identifier',
    external_id TEXT NOT NULL COMMENT 'External reference ID (KSUID)',
    name VARCHAR(255) NOT NULL COMMENT 'Warehouse name',
    price DECIMAL(10, 2) NOT NULL COMMENT 'Operational cost',
    nullable_price DECIMAL(10, 2) COMMENT 'Optional secondary price',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT 'Row creation timestamp'
) COMMENT='Storage warehouses with type-overridden columns';

-- Manifest extended-metadata fixtures (PRD §30.7). Composite
-- (non-unique) index; MySQL has no partial-index grammar so there is no WHERE
-- clause. Placed after the CREATE TABLE it targets (vitess rewrites a
-- standalone CREATE INDEX into an ALTER, which requires the table to already
-- be defined). Non-unique, so it adds no FindBy* method.
CREATE INDEX idx_order_items_order_product ON order_items (order_id, product_id);

-- default_only_rows: every writable column carries a DEFAULT, so a zero
-- CreateDefaultOnlyRowInput supplies no column at all and the INSERT takes
-- the dialect's all-defaults form.
CREATE TABLE default_only_rows (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    label VARCHAR(32) NOT NULL DEFAULT 'unset',
    hits INT NOT NULL DEFAULT 0,
    active BOOLEAN NOT NULL DEFAULT TRUE
);

-- key_only_rows: the key is the table's only column and the database
-- generates it, so CreateKeyOnlyRowInput has no field and no INSERT can name a
-- writable column.
CREATE TABLE key_only_rows (id BIGINT AUTO_INCREMENT PRIMARY KEY);

-- slugged_rows: `slug` is UNIQUE with a constant DEFAULT and `read_only` on the
-- API, and every column has a DEFAULT, so a zero input supplies no column.
CREATE TABLE slugged_rows (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(64) NOT NULL DEFAULT '',
    slug VARCHAR(64) NOT NULL UNIQUE DEFAULT 'x'
);
