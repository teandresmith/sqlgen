# Warehouse

- **Table:** `warehouses` (schema `public`)
- **Kind:** table

Storage warehouses with type-overridden columns

## Files

- `models_gen.go`

## Primary key

- Kind: single
- `id` — ID `uuid.UUID`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `uuid.UUID` | `uuid` |  | yes |  | `gen_random_uuid()` | `comparator.ID` | Unique identifier |
| `external_id` | ExternalID | `ksuid.KSUID` | `text` |  |  | yes |  | `comparator.String` | External reference ID (KSUID) |
| `name` | Name | `string` | `varchar(255)` |  |  |  |  | `comparator.String` | Warehouse name |
| `region` | Region | `customtypes.OptionalString` | `region_name` | yes |  |  |  | `comparator.NullableString` | Optional regional designation (custom Optional[string] override) |
| `price` | Price | `decimal.Decimal` | `numeric(10, 2)` |  |  |  |  | `comparator.String` | Operational cost |
| `nullable_price` | NullablePrice | `decimal.NullDecimal` | `numeric(10, 2)` | yes |  |  |  | `comparator.NullableString` | Optional secondary price |
| `related_ids` | RelatedIDS | `[]uuid.UUID` | `uuid[]` | yes |  |  |  | `comparator.NullableSlice[uuid.UUID]` | Related warehouse UUIDs |
| `nullable_ref` | NullableRef | `*uuid.UUID` | `uuid` | yes |  |  |  | `comparator.NullableID` | Optional reference to another warehouse |
| `allowed_roles` | AllowedRoles | `UserRoleSlice` | `user_role[]` |  |  |  | `'{}'` | `comparator.Slice[UserRole]` | Permitted user roles for warehouse access |
| `tag_ids` | TagIDS | `[]ksuid.KSUID` | `text[]` |  |  |  | `'{}'` | `comparator.Slice[ksuid.KSUID]` | Array of KSUID tag references |
| `created_at` | CreatedAt | `time.Time` | `timestamptz` |  |  |  | `current_timestamp` | `comparator.Time` | Row creation timestamp |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `warehouses_external_id_key` | external_id | yes | btree |  |
| `warehouses_pkey` | id | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| warehouses | o2m | Warehouse | public.warehouses.nullable_ref |  |

## Query methods

### Get

- Params: `id uuid.UUID`
- Returns: `*Warehouse`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "allowed_roles", "created_at", "external_id", "id", "name", "nullable_price", "nullable_ref", "price", "region", "related_ids", "tag_ids" FROM "public"."warehouses" WHERE "id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetWarehousesInput`
- Returns: `[]*Warehouse`, `error`

Generated SQL:

postgres:

```sql
SELECT "allowed_roles", "created_at", "external_id", "id", "name", "nullable_price", "nullable_ref", "price", "region", "related_ids", "tag_ids" FROM "public"."warehouses" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *WarehouseFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."warehouses" WHERE <filter>
```

### Exists

- Params: `id uuid.UUID`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."warehouses" WHERE "id" = $1)
```

### ExistsWhere

- Params: `filter *WarehouseFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."warehouses" WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[WarehouseFilter]`
- Returns: `*PaginateResult[Warehouse]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[WarehouseFilter]`
- Returns: `*Connection[Warehouse]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamWarehousesInput`
- Returns: `iter.Seq2[*Warehouse, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "allowed_roles", "created_at", "external_id", "id", "name", "nullable_price", "nullable_ref", "price", "region", "related_ids", "tag_ids" FROM "public"."warehouses" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateWarehouseInput`
- Returns: `*Warehouse`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."warehouses" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateWarehouseInput`
- Returns: `[]*Warehouse`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."warehouses" (<columns>) VALUES <values> RETURNING "id"
```

### Upsert

- Params: `input *CreateWarehouseInput`, `target WarehouseConflictTarget`
- Returns: `*Warehouse`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated WarehouseConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."warehouses" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateWarehouseInput`, `target WarehouseConflictTarget`
- Returns: `[]*Warehouse`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same WarehouseConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."warehouses" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `id uuid.UUID`, `input *UpdateWarehouseInput`
- Returns: `*Warehouse`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."warehouses" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdateWarehouseItem`
- Returns: `[]*Warehouse`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."warehouses" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *WarehouseFilter`, `input *UpdateWarehouseInput`
- Returns: `[]*Warehouse`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."warehouses" SET <set> WHERE <filter> RETURNING "id"
```

### Increment

- Params: `id uuid.UUID`, `input IncrementInput[WarehouseIncrementColumn]`
- Returns: `error`
- Errors: `ErrNotFound`
- Notes: Atomic single-column arithmetic. The column is restricted to the generated WarehouseIncrementColumn enum — numeric, and identifying nothing: neither PK, FK, nor the tenant column. A negative amount decrements.

Generated SQL:

postgres:

```sql
UPDATE "public"."warehouses" SET <column> = <column> + $1 WHERE "id" = $2
```

### HardDelete

- Params: `id uuid.UUID`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."warehouses" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []uuid.UUID`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."warehouses" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *WarehouseFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."warehouses" WHERE <filter> RETURNING "id"
```

## Filter

Type `WarehouseFilter`.

| Field | Type |
| --- | --- |
| AllowedRoles | `*comparator.Slice[UserRole]` |
| CreatedAt | `*comparator.Time` |
| ExternalID | `*comparator.String` |
| ID | `*comparator.ID` |
| Name | `*comparator.String` |
| NullablePrice | `*comparator.NullableString` |
| NullableRef | `*comparator.NullableID` |
| Price | `*comparator.String` |
| Region | `*comparator.NullableString` |
| RelatedIDS | `*comparator.NullableSlice[uuid.UUID]` |
| TagIDS | `*comparator.Slice[ksuid.KSUID]` |
| And | `[]*WarehouseFilter` |
| Or | `[]*WarehouseFilter` |

## Sort

Type `WarehouseSort`.

Fields: AllowedRoles, CreatedAt, ExternalID, ID, Name, NullablePrice, NullableRef, Price, Region, RelatedIDS, TagIDS
