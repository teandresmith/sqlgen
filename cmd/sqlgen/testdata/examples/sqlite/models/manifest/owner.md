# Owner

- **Table:** `owners`
- **Kind:** table

## Files

- `owner_gen.go`

## Primary key

- Kind: single
- `owner_key` — OwnerKey `int64`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `owner_key` | OwnerKey | `int64` | `integer` |  | yes |  |  | `comparator.Number[int64]` |  |
| `name` | Name | `string` | `text` |  |  |  |  | `comparator.String` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `owners_pkey` | owner_key | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| OwnerProfile | o2o | OwnerProfile | owner_profiles.owner_id |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*Owner`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

sqlite:

```sql
SELECT "name", "owner_key" FROM "owners" WHERE "owner_key" = ? LIMIT 1
```

### GetMany

- Params: `input *GetOwnersInput`
- Returns: `[]*Owner`, `error`

Generated SQL:

sqlite:

```sql
SELECT "name", "owner_key" FROM "owners" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *OwnerFilter`
- Returns: `int64`, `error`

Generated SQL:

sqlite:

```sql
SELECT COUNT(*) FROM "owners" WHERE <filter>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "owners" WHERE "owner_key" = ?)
```

### ExistsWhere

- Params: `filter *OwnerFilter`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "owners" WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[OwnerFilter]`
- Returns: `*PaginateResult[Owner]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[OwnerFilter]`
- Returns: `*Connection[Owner]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamOwnersInput`
- Returns: `iter.Seq2[*Owner, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

sqlite:

```sql
SELECT "name", "owner_key" FROM "owners" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateOwnerInput`
- Returns: `*Owner`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

sqlite:

```sql
INSERT INTO "owners" (<columns>) VALUES (<values>) RETURNING "owner_key"
```

### CreateMany

- Params: `inputs []*CreateOwnerInput`
- Returns: `[]*Owner`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "owners" (<columns>) VALUES <values> RETURNING "owner_key"
```

### Upsert

- Params: `input *CreateOwnerInput`, `target OwnerConflictTarget`
- Returns: `*Owner`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated OwnerConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

sqlite:

```sql
INSERT INTO "owners" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "owner_key"
```

### UpsertMany

- Params: `inputs []*CreateOwnerInput`, `target OwnerConflictTarget`
- Returns: `[]*Owner`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same OwnerConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "owners" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `id int64`, `input *UpdateOwnerInput`
- Returns: `*Owner`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

sqlite:

```sql
UPDATE "owners" SET <set> WHERE "owner_key" = ?
```

### UpdateMany

- Params: `items []UpdateOwnerItem`
- Returns: `[]*Owner`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

sqlite:

```sql
UPDATE "owners" SET <set> WHERE "owner_key" = ?
```

### UpdateWhere

- Params: `filter *OwnerFilter`, `input *UpdateOwnerInput`
- Returns: `[]*Owner`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
UPDATE "owners" SET <set> WHERE <filter> RETURNING "owner_key"
```

### HardDelete

- Params: `id int64`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "owners" WHERE "owner_key" = ?
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "owners" WHERE "owner_key" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *OwnerFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
DELETE FROM "owners" WHERE <filter> RETURNING "owner_key"
```

## Filter

Type `OwnerFilter`.

| Field | Type |
| --- | --- |
| Name | `*comparator.String` |
| OwnerKey | `*comparator.Number[int64]` |
| And | `[]*OwnerFilter` |
| Or | `[]*OwnerFilter` |

## Sort

Type `OwnerSort`.

Fields: Name, OwnerKey
