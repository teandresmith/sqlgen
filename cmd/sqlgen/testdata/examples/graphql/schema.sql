-- graphql: spec-driven schema for the GraphQL E2E example.
--
-- Goals — exercise every PRD §26.4.1 scalar registry category 1/3/4 against
-- a real postgres column type, every relationship variant (O2O, M2O, O2M,
-- M2M), and a mix of PK strategies (BIGSERIAL on categories and user_badges,
-- UUID elsewhere, composite on the M2M junction).

-- categories: simple PK (BIGSERIAL → int64). No outbound FKs. Reverse target
-- of products and user_categories.
CREATE TABLE categories (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    description TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- users: UUID PK, soft-delete (deleted_at TIMESTAMPTZ), JSONB metadata
-- (cat 3 JSON — PRD §7.6 shape-polymorphic types.JSON). Also the §32
-- access-classified fixture: one column per non-public role.
-- The DEFAULT on password_hash keeps it out of the required-on-create set,
-- so `internal` passes §32.4 validation while the create API stays enabled.
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    metadata JSONB,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    password_hash TEXT NOT NULL DEFAULT '',   -- access: internal
    new_password TEXT,                        -- access: write_only
    last_login_at TIMESTAMPTZ,                -- access: read_only
    internal_score BIGINT,                    -- access: hidden
    ip_addr TEXT,                             -- column_map name: ClientIP
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMPTZ
);

-- profiles: O2O with users via UNIQUE FK. JSON column (cat 3 JSON via the
-- default types.JSON binding — PRD §7.6 shape-polymorphic types.JSON).
CREATE TABLE profiles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL UNIQUE REFERENCES users(id),
    bio TEXT,
    avatar_url TEXT,
    settings JSON,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- user_credentials: 1:1 extension table — the PK *is* the FK.
-- PRD §8.6: a single-column PK that is also a foreign key is caller-strategy
-- on every dialect, because its value must equal an existing users row and
-- neither the database nor the generated client may mint it. The column is
-- therefore a required field on both Create<T>Input surfaces, and is the one
-- shape that distinguishes the FK clause from plain type-based detection: a
-- bare `uuid` PK with no default would otherwise resolve to `app` and Create
-- would insert a freshly minted UUID that no parent row can satisfy.
--
-- Deliberately UUID with no DEFAULT — the exact combination that misclassified
-- before the fix. `profiles` above covers the ordinary surrogate-PK O2O.
CREATE TABLE user_credentials (
    user_id UUID PRIMARY KEY REFERENCES users(id),
    provider TEXT NOT NULL,
    external_id TEXT NOT NULL,
    last_login_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- user_sessions: the end-to-end target that `api.enabled: false` hides
-- (PRD §26.10, §9.9.4). Its FK gives users an FK-inferred
-- `UserSessions` edge, which the Go client keeps and which nested mutations
-- pick up too. The API drops the edge everywhere: the `type User` field and
-- the member in all three `*UserWithRelatedInput` wrappers. If either were
-- left in, it would name a type the schema never declares, and gqlgen would
-- reject the whole document. This is a new table rather than an existing one
-- hidden, because the tests query every other table through the API.
CREATE TABLE user_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id),
    token_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- user_badges: the has-one relink fixture, and the only has-one edge in the
-- tree over a NULLABLE FK into an integer-keyed target (`users.Badge`,
-- declared in sqlgen.yml — a UNIQUE FK infers no edge on the parent). Both
-- halves are needed to reach one translator arm. The nullable FK is what keeps
-- `connect` and `disconnect` on a has-one edge (PRD §9.9.4):
-- `assets.PrimaryDocument` and the mysql example's `users.Profile` sit on NOT
-- NULL FKs, so they are create-only. The BIGSERIAL key is what makes the
-- single id need a conversion: gqlgen binds `Int` to Go int, the model carries
-- int64. Without this table that arm was rendered into no golden and compiled
-- by nothing.
CREATE TABLE user_badges (
    id BIGSERIAL PRIMARY KEY,
    user_id UUID UNIQUE REFERENCES users(id),
    label TEXT NOT NULL
);

-- products: M2O products → categories (BIGINT FK to BIGSERIAL PK), soft-delete,
-- numeric (cat 4 Decimal via global override).
CREATE TABLE products (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    description TEXT,
    price NUMERIC(12, 2) NOT NULL,
    stock INTEGER NOT NULL DEFAULT 0,
    category_id BIGINT NOT NULL REFERENCES categories(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMPTZ
);

-- orders: M2O orders → users, soft-delete, numeric total.
CREATE TABLE orders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id),
    total NUMERIC(12, 2) NOT NULL DEFAULT 0,
    notes TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMPTZ
);

-- order_items: M2O order_items → orders / → products. Two FKs to test the
-- multi-FK relationship-loader path.
CREATE TABLE order_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id UUID NOT NULL REFERENCES orders(id),
    product_id UUID NOT NULL REFERENCES products(id),
    quantity INTEGER NOT NULL DEFAULT 1,
    unit_price NUMERIC(12, 2) NOT NULL,
    -- external_ref carries only a column_map `description:`, which is the
    -- narrowest form of that block — no retype, no import. The registry-scalar
    -- retype it used to carry moved to the graphql_null_wrappers example: this
    -- one binds the standard library, whose uuid.UUID has no sql.Scanner, so
    -- pgx cannot scan a TEXT column into it (PRD §7.4 — the codec is
    -- registered for the uuid OID). The two wrapper-backed integrations do
    -- ship Scan/Value, so the retype still works there.
    external_ref TEXT,
    -- source_ip is the consumer-declared scalar fixture and the sibling of
    -- external_ref above: column_map retypes it onto a Go type the built-in
    -- scalar registry does NOT cover, so the GraphQL field type has to come
    -- from a consumer declaration (api.graphql.scalars) instead. Without one
    -- the field falls through to `String` in front of a netip.Addr Go field,
    -- and gqlgen answers that with a panic("not implemented") field resolver
    -- plus an input translator that will not compile.
    --
    -- INET rather than TEXT because this column is scanned for real: pgx's
    -- InetCodec lists netip.Addr among its preferred Go types. Nullable so the
    -- pointer-nullability path (*netip.Addr on both the row struct and the
    -- gqlgen-generated input) is what runs — consumer scalars have no
    -- Null-wrapper pairing and do not need one.
    source_ip INET,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- user_categories: M2M junction with composite PK across mixed Go types
-- (UUID + int64).
CREATE TABLE user_categories (
    user_id UUID NOT NULL REFERENCES users(id),
    category_id BIGINT NOT NULL REFERENCES categories(id),
    PRIMARY KEY (user_id, category_id)
);

-- assets / documents: sub-categorized polymorphism fixture (PRD §13.7).
-- A single PG enum discriminator sub-categorizes documents per-parent; the
-- API walker emits one selectable field per relationship.
CREATE TYPE document_entity_type_enum AS ENUM (
    'asset.primary',
    'asset.attachment',
    'asset.invoice',
    'spv'
);

CREATE TABLE assets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    -- Exercises the enum-slice path through the GraphQL surface: the column
    -- types as `DocumentEntityTypeEnumSlice` in Go (a named slice), the
    -- GraphQL field surfaces as `[DocumentEntityTypeEnum!]!`, and the
    -- generated MarshalGQL / UnmarshalGQL on the slice variant bridges
    -- between the wire identifiers and the SQL literals on both directions.
    declared_categories document_entity_type_enum[] NOT NULL DEFAULT '{}',
    -- The nullable half of PRD §26.4 Rule 2 on an ENUM column. Every
    -- other enum column in the example tree (here and in the mysql example)
    -- is NOT NULL, so `NullableDocumentEntityTypeEnumComparator` and its
    -- translator would otherwise be emitted into no golden and compiled by
    -- nothing — a blind spot that once hid two non-compiling translators.
    -- It also makes `assets` a second table referencing the same enum
    -- comparator, which is what the one-input-per-enum sharing rule claims.
    primary_category document_entity_type_enum,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE documents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_id UUID NOT NULL,
    entity_type document_entity_type_enum NOT NULL,
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Junction table for m2m sub-categorized polymorphism coverage.
CREATE TABLE asset_document_links (
    asset_id UUID NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    document_id UUID NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    PRIMARY KEY (asset_id, document_id)
);

-- workspace_settings: tenancy + cache fixture. Composite-PK table
-- with the tenant column IN the PK so PRD §29.7's verify-match rule is
-- reachable end-to-end from the GraphQL surface (callers supply
-- workspaceID via the typed PK args; a mismatch against the resolver
-- short-circuits with tenancy.ErrMismatch → FORBIDDEN).
--
-- Three entities in this example carry `workspace_id` and are therefore
-- tenanted by §29.2.3 / §29.2.5 detection: this table, `workspace_notes`
-- below (the non-PK-tenant shape), and the `workspace_note_summary` VIEW
-- built over it. Every other table lacks the column and auto-detects
-- as shared.
CREATE TABLE workspace_settings (
    workspace_id UUID NOT NULL,
    key TEXT NOT NULL,
    value TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (workspace_id, key)
);

-- events: dedicated table whose timestamptz columns are overridden to
-- types.DateTime / types.NullDateTime (cat 3 DateTime + paired Null variant —
-- PRD §26.4.1). Other tables keep the default time.Time binding
-- (cat 1 Time).
--
-- user_id is nullable to exercise the nullable-UUID field path: this example
-- binds the standard library, which ships no NullUUID, so the field is a
-- nullable `UUID` rather than a `NullUUID` (PRD §7.4, §26.4.1) — paired with
-- the non-null UUID columns elsewhere, e.g. orders.user_id. The NullUUID
-- scalar itself lives in the graphql_null_wrappers example, on gofrs.
-- processed_at is nullable to exercise the NullDateTime scalar path
-- (paired with the non-null occurred_at).
-- adjustment is nullable to exercise the NullDecimal scalar path (paired
-- with the non-null products.price / orders.total).
-- retry_count is nullable to exercise TWO paths that no other example reaches:
-- the NullableNumericComparator twin (PRD §26.4 Rule 2), and the nullable
-- sized-int input translation — gqlgen binds the GraphQL Int scalar to Go
-- `int` while the model narrows to `int32`, so the create/update translator
-- must convert through a local instead of threading the pointer. Without a
-- nullable integer column anywhere, that generated the non-compiling
-- `omittable.Set(in.RetryCount)` for every consumer with one.
-- succeeded is nullable for the NullableBooleanComparator twin, the other
-- family with no golden coverage before this.
CREATE TABLE events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID REFERENCES users(id),
    action TEXT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    processed_at TIMESTAMPTZ,
    adjustment NUMERIC(12, 2),
    retry_count INTEGER,
    succeeded BOOLEAN,
    -- Acronym-parity fixture. Plain TEXT / INTEGER / BOOLEAN on
    -- purpose: these columns test naming, not type mapping. Each one is a
    -- token where sqlgen's acronym set and gqlgen's initialism list disagree.
    -- The set is sqlgen-owned and deliberately not shared with gqlgen (PRD
    -- §8.5); the Go names are dictated field by field instead, as the
    -- line2_id note below spells out. The generated filter and input
    -- translators reference gqlgen-emitted struct fields by Go name, so a
    -- divergence here does not produce subtly wrong output — it fails to
    -- compile. Keeping them in an example is what makes that a build failure
    -- in this repo rather than in a consumer's.
    --
    -- sqlgen uppercased these and gqlgen did not:
    mac TEXT,
    os_name TEXT,
    io_count INTEGER,
    pk_val TEXT,
    fk_val TEXT,
    -- gqlgen uppercased these and sqlgen did not:
    ascii_art TEXT,
    csv_data TEXT,
    tcp_port INTEGER,
    -- Digit boundaries: neither side may form an acronym across one, so these
    -- resolve to Utf8Body and HTTP2Flag respectively.
    utf8_body TEXT,
    http2_flag BOOLEAN,
    -- An acronym directly following a digit-terminated word. Camelization is
    -- lossy here — the emitted GraphQL field `line2ID` gives gqlgen's word
    -- walker no seam before the acronym, so left to itself it produces
    -- `Line2id` while sqlgen produces `Line2ID`. No acronym list can close
    -- that; it is closed by sqlgen dictating the Go field name outright
    -- (PRD §26.5.6 "Go field naming"). `line2_id` / `address2_id` are ordinary
    -- postal-address columns, which is why this shape earns an example.
    line2_id TEXT,
    address2_id TEXT,
    s3_url TEXT
);

-- workspace_note_kind_enum: the enum a tenanted VIEW projects. Distinct
-- from document_entity_type_enum above, which sub-categorizes documents per
-- parent and would read as a different fixture on a workspace note.
CREATE TYPE workspace_note_kind_enum AS ENUM (
    'draft',
    'published',
    'archived'
);

-- workspace_notes: the non-PK-tenant fixture (PRD §29.4.2). workspace_settings
-- above carries its tenant column INSIDE the composite PK, which the update
-- input already drops via the PK rule — so before this table no example in the
-- repo produced the shape where a tenant column is an ordinary updatable
-- column. That gap is why the "tenant column offered on Update<T>Input but
-- unable to take effect" defect was invisible to the suite and only surfaced
-- in a consumer app.
--
-- The tenant type matches workspace_settings.workspace_id (uuid) as §29.2.4's
-- uniform-tenant-type rule requires. `pinned_order` is a plain incrementable
-- column so the `_inc` / `_dec` operators stay covered on a table that also
-- has a tenant column; the tenant column itself is uuid and therefore not
-- incrementable, so the integer-tenant escape is pinned by a gen unit test
-- instead (a bigint tenant cannot coexist with uuid under §29.2.4).
--
-- `kind` and `labels` exist so the tenanted view built over this table
-- (views/workspace_note_summary.sql) can project an ENUM and a JSONB column.
-- PRD §26.4's view row promises a view's <V>Filter carries the same comparator
-- families a table's does; before these two columns the only tenanted table in
-- this example was uuid/text/int only, so a view over it could not reach the
-- enum family or the JSONB family at all.
--
-- Both are NOT NULL, and a DEFAULT does NOT excuse a column from the create
-- input: every NOT NULL non-generated column is required on Create<T>Input
-- (`kind: WorkspaceNoteKindEnum!` / `labels: JSON!`), the same rule that makes
-- `pinned_order` and `created_at` required above. The two createWorkspaceNote
-- call sites in tests/tenant_update_input_test.go pass both.
--
-- `parent_id` is a self-referential FK, and it is the one column that
-- makes two otherwise-unreachable GraphQL shapes testable in this example.
--
--   1. A relationship filter whose TARGET is TENANTED. `workspace_notes` is
--      the only tenanted table here that anything can point a list
--      relationship at (`workspace_settings` has a composite PK, so PRD §11.1
--      gives it no filter members at all), and pointing an untenanted parent
--      at it would flip that parent to "must resolve a tenant" under
--      `tenancy.required: true` — breaking every existing unscoped read of it.
--      Pointing the table at itself adds the shape and moves nothing: this
--      table already resolves a tenant on every call.
--   2. A SELF-REFERENTIAL recursive filter input (§13.5). `input
--      WorkspaceNoteFilter { children: WorkspaceNoteFilter }` is legal GraphQL
--      and its translator recurses into itself; both are asserted end to end
--      rather than only in a hand-built context.
--
-- Nullable so every pre-existing workspace-note fixture still creates without
-- naming a parent, and so the column doubles as another nullable-UUID field
-- site (a nullable `UUID`, not a `NullUUID` — see events.user_id above).
CREATE TABLE workspace_notes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    parent_id UUID REFERENCES workspace_notes(id),
    body TEXT NOT NULL,
    pinned_order INTEGER NOT NULL DEFAULT 0,
    kind workspace_note_kind_enum NOT NULL DEFAULT 'draft',
    labels JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- scalar_probes: the scalar registry fixture. Every one of the twelve Go
-- types the registry binds for these columns is reachable from an ordinary
-- column, and until this table none was reachable from an example: the two
-- modules with a GraphQL surface carry deliberately narrow schemas (uuid /
-- text / numeric / jsonb / timestamptz / enum), while the modules that DO have
-- `bytea`, `inet` and `blob` columns -- postgres, mysql, sqlite, tenancy --
-- have no api: block, so their columns never reach a scalar binding at all.
-- That split is why missing bindings for these types once went unnoticed.
--
-- Shape A (payload … mac_n) are the five stdlib types with a GraphQL
-- binding: Bytes, Duration, IP, CIDR, MacAddr. Both nullabilities are
-- present because they take different paths through gqlgen -- a Nilable
-- binding stays bare when the field is nullable, a non-nilable one is
-- pointer-wrapped -- and the input translator emits a deref for exactly one of
-- them.
--
-- Shape B (note … observed_at) are ordinary nullable columns that resolve to
-- the `database/sql` wrappers, via the table-scoped overrides in sqlgen.yml.
-- `overrides.use_pointers: false` is what produces them in production, but it
-- is module-wide and would retype every nullable column in this example; the
-- scalar registry is keyed on the resolved Go type, so the two routes are
-- indistinguishable downstream.
--
-- `label` and `created_at` are NOT NULL siblings of overridden SQL types,
-- pinning that the override's non-null branch still resolves to exactly what
-- the built-in dialect mapping gives (`String!`, `Time!`).
--
-- The shape-A columns above are also the opaque-filter fixture. Each of the
-- five (bytea, interval, inet, cidr, macaddr) filtered through
-- `StringComparator` until the comparator.Opaque[T] / Duration families
-- landed, so the generated filter compared a binary, network or duration
-- column against a text parameter. The shape-B `sql.NullX` columns are NOT
-- part of that set -- they already route to their real families.
-- tests/scalar_filters_test.go exercises the five through the live handler;
-- `addr` doubles as the pin for the IPv4 4-byte normalization UnmarshalIP
-- applies (§26.4.1).
CREATE TABLE scalar_probes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    label TEXT NOT NULL,

    payload BYTEA NOT NULL,         -- []byte      → Bytes!
    signature BYTEA,                -- []byte      → Bytes
    dur INTERVAL NOT NULL,          -- time.Duration → Duration!
    dur_n INTERVAL,                 -- *time.Duration → Duration
    addr INET NOT NULL,             -- net.IP      → IP!
    addr_n INET,                    -- net.IP      → IP
    net_block CIDR NOT NULL,        -- net.IPNet   → CIDR!
    net_block_n CIDR,               -- *net.IPNet  → CIDR
    mac MACADDR NOT NULL,           -- net.HardwareAddr → MacAddr!
    mac_n MACADDR,                  -- net.HardwareAddr → MacAddr

    note TEXT,                      -- sql.NullString  → NullString
    flag BOOLEAN,                   -- sql.NullBool    → NullBool
    small_count SMALLINT,           -- sql.NullInt16   → NullInt16
    mid_count INTEGER,              -- sql.NullInt32   → NullInt32
    big_count BIGINT,               -- sql.NullInt64   → NullInt64
    ratio DOUBLE PRECISION,         -- sql.NullFloat64 → NullFloat64
    observed_at TIMESTAMPTZ,        -- sql.NullTime    → NullTime

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- numeric_widths: the numeric-width fixture. Two axes, both invisible before
-- this table, and both silent rather than loud.
--
-- READ. gqlgen binds the `Int` scalar to Go int / int32 / int64 and `Float` to
-- float64 — and nothing else. `codegen/config/binder.go` compares basic kinds
-- EXACTLY, so a row-struct field of any other width matches no entry, the
-- binder error is swallowed into `f.IsResolver = true`, and gqlgen answers the
-- field with `panic("not implemented")`. No error, no log line, and outside the
-- reach of the wrapper's Query/Mutation stub rewriter. `small` and `ratio` are
-- the bare int16 / float32 columns that reach it; before this table no module
-- exposed either to the API, because the only two API-enabled examples are
-- PostgreSQL and their sole SMALLINT is retyped onto sql.NullInt16.
--
-- WRITE. gqlgen types a GENERATED list field as `[]Model[0]` — `[]int` for
-- `[Int!]!` — while the model carries the column's native width. Go has no
-- conversion between slices of differing element types, so this used to fail
-- generation outright. sqlgen now dictates the field's Go type through
-- `models.<Input>.fields.<f>.type` and the disagreement is gone at its source.
--
-- Both nullabilities on every width: the nullable scalar takes the
-- deref-convert-readdress local, the nullable array stays a bare nilable slice,
-- and the two are different translator arms.
--
-- No table-scoped overrides on purpose. scalar_probes retypes its nullable
-- columns onto the database/sql wrappers, which is what hid the int16 case
-- there; these columns must reach the bare Go widths.
CREATE TABLE numeric_widths (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    small SMALLINT NOT NULL,        -- int16    → Int!   (gqlgen graphql.Int16)
    small_n SMALLINT,               -- *int16   → Int
    ratio REAL NOT NULL,            -- float32  → Float! (sqlgen-emitted Float32)
    ratio_n REAL,                   -- *float32 → Float

    smalls SMALLINT[] NOT NULL DEFAULT '{}',  -- []int16   → [Int!]!
    scores INTEGER[] NOT NULL DEFAULT '{}',   -- []int32   → [Int!]!
    bigs BIGINT[] NOT NULL DEFAULT '{}',      -- []int64   → [Int!]!
    ratios REAL[] NOT NULL DEFAULT '{}',      -- []float32 → [Float!]!
    scores_n INTEGER[],                       -- []int32   → [Int!]

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- filter_probes: the JSON / JSONB / Slice comparator families (PRD §26.4).
-- Every shape here was previously advertised as `StringComparator` with no
-- translator behind it, so the server answered the filter with the
-- UNFILTERED set.
--
-- Each column exists for a projection the rest of the tree cannot reach:
--
--   doc      NOT NULL jsonb — JSONBComparator. Every other jsonb column in an
--            API-enabled example is nullable, so the NOT NULL half of Rule 2
--            would be emitted into no golden and compiled by nothing (the
--            blind spot assets.primary_category closes for enums).
--   tags     text[] — StringSliceComparator, the identity-copy arm, and the
--            one array whose operands are CALLER-SUPPLIED strings. That is
--            the surface the array-literal quoting protects: a value
--            containing a comma used to match a different set silently.
--   tags_n   nullable text[] — Nullable<Elem>SliceComparator with `isNull`.
--   flags    boolean[] — BooleanSliceComparator, the Boolean element binding.
--   weights  double precision[] — FloatSliceComparator with NO width cast
--            (`float64` is gqlgen's own Float binding), the counterpart to
--            numeric_widths.ratios, which is `float32` and does cast.
--
-- No NOT NULL `json` column: the NOT NULL half of the JSON family earns its
-- golden in the MySQL example (products.attributes, JSON NOT NULL).
-- profiles.settings covers the nullable form here, and it is filterable at
-- request time on PostgreSQL too — `comparator.JSON` casts to
-- jsonb, so `@>` / `?` reach a `json` column instead of raising SQLSTATE
-- 42883. TestJSONComparator_NarrowsOverHTTP is what pins that.
CREATE TABLE filter_probes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    doc JSONB NOT NULL DEFAULT '{}',
    tags TEXT[] NOT NULL DEFAULT '{}',
    tags_n TEXT[],
    flags BOOLEAN[] NOT NULL DEFAULT '{}',
    weights DOUBLE PRECISION[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Two `api.operations` mask shapes that unit tests covered and no example
-- compiled. gqlgen compiling the generated graph package is the pin.
--
-- masked_labels: the Go client generates Upsert, and its schema-declared key
-- emits MaskedLabelConflictPK, but the table's mask takes `upsert` off the API
-- (sqlgen.yml). No flat upsertMaskedLabel, while createMaskedLabel(s) still
-- consume CreateMaskedLabelInput.
CREATE TABLE masked_labels (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    label TEXT NOT NULL
);

-- keyed_codes: the key is declared only through `primary_key.columns`, so no
-- constraint backs it and no KeyedCodeConflictPK is emitted (PRD §9.5). The
-- UNIQUE `code` is the one conflict target, and the mask leaves upsert as the
-- only write. The table's whole mutation surface is therefore
-- upsertKeyedCodeWithRelated with a REQUIRED `conflictTarget` and no default
-- (§26.5.1), and there is no flat `extend type Mutation` block. The Entries
-- edge is declared in sqlgen.yml; with no constraint on `id` there is no
-- foreign key to infer it from.
CREATE TABLE keyed_codes (
    id UUID NOT NULL,
    code TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL
);

CREATE TABLE keyed_code_entries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    keyed_code_id UUID NOT NULL,
    note TEXT NOT NULL
);

-- Manifest + GraphQL description fixtures. COMMENT ON exercises the §26.4
-- .graphqls description wiring end-to-end AND the manifest
-- comment/Column.comment fields, which no example previously covered. The
-- composite (non-unique) index surfaces in the manifest indexes[].
COMMENT ON TABLE categories IS 'Product classification categories';
COMMENT ON COLUMN categories.name IS 'Unique category display name';
COMMENT ON COLUMN categories.description IS 'Optional long-form description';
CREATE INDEX idx_order_items_order_product ON order_items (order_id, product_id);
