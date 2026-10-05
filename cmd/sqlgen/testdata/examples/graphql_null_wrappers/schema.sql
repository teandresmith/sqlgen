-- graphql_null_wrappers: the module-wide `overrides.use_pointers: false`
-- fixture, plus the gqlgen-bundled-scalar route.
--
-- Under `use_pointers: false` every nullable column of a `gotype.nullSQL` type
-- resolves to a `database/sql` wrapper instead of `*T`. The `graphql` example's
-- `scalar_probes` table reaches the same seven Go types through a table-scoped
-- `overrides.types` block, but it is a STANDALONE table — no FK, no
-- relationship, no soft delete, no cursor keys — so it cannot reach the paths
-- the module-wide flag actually changes. This schema is built for those:
--
--   * `accounts.parent_id`  — a NULLABLE self-FK, so the M2O/O2M relationship
--     loaders take sql.NullInt64 on the join column (PRD §11).
--   * `accounts.deleted_at` — soft delete landing on sql.NullTime, the only
--     dialect where it does (SQLite claims datetime for types.DateTime).
--   * `accounts.balance` / `tenant_ref` — the paired Null variants that come
--     from an integration rather than database/sql (decimal.NullDecimal,
--     uuid.NullUUID) sitting alongside the stdlib wrappers.
--
-- The `Int64` half rides on `id` / `external_ref` / `entries.account_id`: with
-- `Int64: {go_type: int64, marshaling: builtin}` declared, every int64 column
-- routes onto gqlgen's own bundled Int64 marshaler instead of `Int`.

CREATE TABLE accounts (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    external_ref BIGINT NOT NULL,

    -- One column per database/sql wrapper the flag can produce.
    nickname TEXT,                  -- sql.NullString  → NullString
    flag BOOLEAN,                   -- sql.NullBool    → NullBool
    small_count SMALLINT,           -- sql.NullInt16   → NullInt16
    mid_count INTEGER,              -- sql.NullInt32   → NullInt32
    big_count BIGINT,               -- sql.NullInt64   → NullInt64
    ratio DOUBLE PRECISION,         -- sql.NullFloat64 → NullFloat64
    observed_at TIMESTAMPTZ,        -- sql.NullTime    → NullTime

    -- Integration-owned Null variants, unaffected by use_pointers.
    balance NUMERIC(12, 2),         -- decimal.NullDecimal → NullDecimal
    tenant_ref UUID,                -- uuid.NullUUID       → NullUUID

    -- Nullable self-FK: the relationship-loader path scalar_probes cannot reach.
    parent_id BIGINT REFERENCES accounts(id),

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    -- Soft delete on a wrapper column.
    deleted_at TIMESTAMPTZ
);

-- entries: the O2M target, so the loader runs with a wrapper-typed parent.
CREATE TABLE entries (
    id BIGSERIAL PRIMARY KEY,
    account_id BIGINT NOT NULL REFERENCES accounts(id),
    memo TEXT,
    amount NUMERIC(12, 2) NOT NULL,
    -- upstream_ref is the column_map retype fixture: a TEXT column nothing in
    -- the resolution chain maps to uuid.UUID, retyped by column_map.<col>.type +
    -- .import onto a type the built-in scalar registry owns, so the override
    -- has to survive the gqlgen handoff and not just the row struct.
    --
    -- It lives on this example rather than on `graphql` because the retype
    -- only works under a wrapper-backed integration: gofrs's uuid.UUID ships
    -- sql.Scanner / driver.Valuer, which is what lets pgx scan a TEXT column
    -- into it. The standard library's uuid.UUID has neither, and pgx keys its
    -- UUIDCodec on the uuid OID, so the same retype fails at scan time there
    -- (PRD §7.4).
    upstream_ref TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
