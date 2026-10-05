# OwnerProfile

- **Table:** `owner_profiles`
- **Kind:** table

## Files

- `owner_profile_gen.go`

## Primary key

- Kind: single
- `id` — ID `int64`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `int64` | `integer` |  | yes |  |  | `comparator.Number[int64]` |  |
| `owner_id` | OwnerID | `int64` | `integer` |  |  | yes |  | `comparator.Number[int64]` |  |
| `bio` | Bio | `*string` | `text` | yes |  |  |  | `comparator.NullableString` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `owner_profiles_owner_id_key` | owner_id | yes | btree |  |
| `owner_profiles_pkey` | id | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| owners | o2o | Owner | owner_profiles.owner_id |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*OwnerProfile`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

sqlite:

```sql
SELECT "bio", "id", "owner_id" FROM "owner_profiles" WHERE "id" = ? LIMIT 1
```

### GetMany

- Params: `input *GetOwnerProfilesInput`
- Returns: `[]*OwnerProfile`, `error`

Generated SQL:

sqlite:

```sql
SELECT "bio", "id", "owner_id" FROM "owner_profiles" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *OwnerProfileFilter`
- Returns: `int64`, `error`

Generated SQL:

sqlite:

```sql
SELECT COUNT(*) FROM "owner_profiles" WHERE <filter>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "owner_profiles" WHERE "id" = ?)
```

### ExistsWhere

- Params: `filter *OwnerProfileFilter`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "owner_profiles" WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[OwnerProfileFilter]`
- Returns: `*PaginateResult[OwnerProfile]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[OwnerProfileFilter]`
- Returns: `*Connection[OwnerProfile]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamOwnerProfilesInput`
- Returns: `iter.Seq2[*OwnerProfile, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

sqlite:

```sql
SELECT "bio", "id", "owner_id" FROM "owner_profiles" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateOwnerProfileInput`
- Returns: `*OwnerProfile`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

sqlite:

```sql
INSERT INTO "owner_profiles" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateOwnerProfileInput`
- Returns: `[]*OwnerProfile`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "owner_profiles" (<columns>) VALUES <values> RETURNING "id"
```

### Upsert

- Params: `input *CreateOwnerProfileInput`, `target OwnerProfileConflictTarget`
- Returns: `*OwnerProfile`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated OwnerProfileConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

sqlite:

```sql
INSERT INTO "owner_profiles" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateOwnerProfileInput`, `target OwnerProfileConflictTarget`
- Returns: `[]*OwnerProfile`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same OwnerProfileConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "owner_profiles" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `id int64`, `input *UpdateOwnerProfileInput`
- Returns: `*OwnerProfile`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

sqlite:

```sql
UPDATE "owner_profiles" SET <set> WHERE "id" = ?
```

### UpdateMany

- Params: `items []UpdateOwnerProfileItem`
- Returns: `[]*OwnerProfile`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

sqlite:

```sql
UPDATE "owner_profiles" SET <set> WHERE "id" = ?
```

### UpdateWhere

- Params: `filter *OwnerProfileFilter`, `input *UpdateOwnerProfileInput`
- Returns: `[]*OwnerProfile`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
UPDATE "owner_profiles" SET <set> WHERE <filter> RETURNING "id"
```

### HardDelete

- Params: `id int64`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "owner_profiles" WHERE "id" = ?
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "owner_profiles" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *OwnerProfileFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
DELETE FROM "owner_profiles" WHERE <filter> RETURNING "id"
```

## Filter

Type `OwnerProfileFilter`.

| Field | Type |
| --- | --- |
| Bio | `*comparator.NullableString` |
| ID | `*comparator.Number[int64]` |
| OwnerID | `*comparator.Number[int64]` |
| And | `[]*OwnerProfileFilter` |
| Or | `[]*OwnerProfileFilter` |

## Sort

Type `OwnerProfileSort`.

Fields: Bio, ID, OwnerID
