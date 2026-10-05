# Profile

- **Table:** `profiles`
- **Kind:** table

## Files

- `profile_gen.go`

## Primary key

- Kind: single
- `id` — ID `int64`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `int64` | `integer` |  | yes |  |  | `comparator.Number[int64]` |  |
| `user_id` | UserID | `int64` | `integer` |  |  | yes |  | `comparator.Number[int64]` |  |
| `website` | Website | `*string` | `text` | yes |  |  |  | `comparator.NullableString` |  |
| `github_handle` | GithubHandle | `*string` | `text` | yes |  |  |  | `comparator.NullableString` |  |
| `verified` | Verified | `bool` | `boolean` |  |  |  | `0` | `comparator.Bool` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `profiles_pkey` | id | yes | btree |  |
| `profiles_user_id_key` | user_id | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| users | o2o | User | profiles.user_id |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*Profile`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

sqlite:

```sql
SELECT "github_handle", "id", "user_id", "verified", "website" FROM "profiles" WHERE "id" = ? LIMIT 1
```

### GetMany

- Params: `input *GetProfilesInput`
- Returns: `[]*Profile`, `error`

Generated SQL:

sqlite:

```sql
SELECT "github_handle", "id", "user_id", "verified", "website" FROM "profiles" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *ProfileFilter`
- Returns: `int64`, `error`

Generated SQL:

sqlite:

```sql
SELECT COUNT(*) FROM "profiles" WHERE <filter>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "profiles" WHERE "id" = ?)
```

### ExistsWhere

- Params: `filter *ProfileFilter`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "profiles" WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[ProfileFilter]`
- Returns: `*PaginateResult[Profile]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[ProfileFilter]`
- Returns: `*Connection[Profile]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamProfilesInput`
- Returns: `iter.Seq2[*Profile, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

sqlite:

```sql
SELECT "github_handle", "id", "user_id", "verified", "website" FROM "profiles" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateProfileInput`
- Returns: `*Profile`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

sqlite:

```sql
INSERT INTO "profiles" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateProfileInput`
- Returns: `[]*Profile`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "profiles" (<columns>) VALUES <values> RETURNING "id"
```

### Upsert

- Params: `input *CreateProfileInput`, `target ProfileConflictTarget`
- Returns: `*Profile`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated ProfileConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

sqlite:

```sql
INSERT INTO "profiles" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateProfileInput`, `target ProfileConflictTarget`
- Returns: `[]*Profile`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same ProfileConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "profiles" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `id int64`, `input *UpdateProfileInput`
- Returns: `*Profile`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

sqlite:

```sql
UPDATE "profiles" SET <set> WHERE "id" = ?
```

### UpdateMany

- Params: `items []UpdateProfileItem`
- Returns: `[]*Profile`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

sqlite:

```sql
UPDATE "profiles" SET <set> WHERE "id" = ?
```

### UpdateWhere

- Params: `filter *ProfileFilter`, `input *UpdateProfileInput`
- Returns: `[]*Profile`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
UPDATE "profiles" SET <set> WHERE <filter> RETURNING "id"
```

### HardDelete

- Params: `id int64`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "profiles" WHERE "id" = ?
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "profiles" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *ProfileFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
DELETE FROM "profiles" WHERE <filter> RETURNING "id"
```

## Filter

Type `ProfileFilter`.

| Field | Type |
| --- | --- |
| GithubHandle | `*comparator.NullableString` |
| ID | `*comparator.Number[int64]` |
| UserID | `*comparator.Number[int64]` |
| Verified | `*comparator.Bool` |
| Website | `*comparator.NullableString` |
| And | `[]*ProfileFilter` |
| Or | `[]*ProfileFilter` |

## Sort

Type `ProfileSort`.

Fields: GithubHandle, ID, UserID, Verified, Website
