# Profile

- **Table:** `profiles`
- **Kind:** table

Extended user profile information

## Files

- `models_gen.go`

## Primary key

- Kind: single
- `id` — ID `int64`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `int64` | `bigint` |  | yes |  |  | `comparator.Number[int64]` | Unique identifier |
| `user_id` | UserID | `int64` | `bigint` |  |  | yes |  | `comparator.Number[int64]` | Owning user reference |
| `website` | Website | `*string` | `varchar(500)` | yes |  |  |  | `comparator.NullableString` | Personal website URL |
| `github_handle` | GithubHandle | `*string` | `varchar(100)` | yes |  |  |  | `comparator.NullableString` | GitHub username |
| `verified` | Verified | `bool` | `boolean` |  |  |  | `false` | `comparator.Bool` | Whether the profile has been verified |
| `preferences` | Preferences | `types.JSON` | `json` | yes |  |  |  | `comparator.NullableJSON` | User preference settings |

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

mysql:

```sql
SELECT `github_handle`, `id`, `preferences`, `user_id`, `verified`, `website` FROM `profiles` WHERE `id` = ? LIMIT 1
```

### GetMany

- Params: `input *GetProfilesInput`
- Returns: `[]*Profile`, `error`

Generated SQL:

mysql:

```sql
SELECT `github_handle`, `id`, `preferences`, `user_id`, `verified`, `website` FROM `profiles` WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *ProfileFilter`
- Returns: `int64`, `error`

Generated SQL:

mysql:

```sql
SELECT COUNT(*) FROM `profiles` WHERE <filter>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`

Generated SQL:

mysql:

```sql
SELECT EXISTS(SELECT 1 FROM `profiles` WHERE `id` = ?)
```

### ExistsWhere

- Params: `filter *ProfileFilter`
- Returns: `bool`, `error`

Generated SQL:

mysql:

```sql
SELECT EXISTS(SELECT 1 FROM `profiles` WHERE <filter>)
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

mysql:

```sql
SELECT `github_handle`, `id`, `preferences`, `user_id`, `verified`, `website` FROM `profiles` WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateProfileInput`
- Returns: `*Profile`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

mysql:

```sql
INSERT INTO `profiles` (<columns>) VALUES (<values>)
```

### CreateMany

- Params: `inputs []*CreateProfileInput`
- Returns: `[]*Profile`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

mysql:

```sql
INSERT INTO `profiles` (<columns>) VALUES <values>
```

### Upsert

- Params: `input *CreateProfileInput`, `target ProfileConflictTarget`
- Returns: `*Profile`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated ProfileConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

mysql:

```sql
INSERT INTO `profiles` (<columns>) VALUES (<values>) ON DUPLICATE KEY UPDATE <excluded>
```

### UpsertMany

- Params: `inputs []*CreateProfileInput`, `target ProfileConflictTarget`
- Returns: `[]*Profile`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same ProfileConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

mysql:

```sql
INSERT INTO `profiles` (<columns>) VALUES <values> ON DUPLICATE KEY UPDATE <excluded>
```

### Update

- Params: `id int64`, `input *UpdateProfileInput`
- Returns: `*Profile`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

mysql:

```sql
UPDATE `profiles` SET <set> WHERE `id` = ?
```

### UpdateMany

- Params: `items []UpdateProfileItem`
- Returns: `[]*Profile`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

mysql:

```sql
UPDATE `profiles` SET <set> WHERE `id` = ?
```

### UpdateWhere

- Params: `filter *ProfileFilter`, `input *UpdateProfileInput`
- Returns: `[]*Profile`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

mysql:

```sql
UPDATE `profiles` SET <set> WHERE <filter>
```

### HardDelete

- Params: `id int64`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

mysql:

```sql
DELETE FROM `profiles` WHERE `id` = ?
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

mysql:

```sql
DELETE FROM `profiles` WHERE `id` IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *ProfileFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

mysql:

```sql
DELETE FROM `profiles` WHERE <filter>
```

## Filter

Type `ProfileFilter`.

| Field | Type |
| --- | --- |
| GithubHandle | `*comparator.NullableString` |
| ID | `*comparator.Number[int64]` |
| Preferences | `*comparator.NullableJSON` |
| UserID | `*comparator.Number[int64]` |
| Verified | `*comparator.Bool` |
| Website | `*comparator.NullableString` |
| And | `[]*ProfileFilter` |
| Or | `[]*ProfileFilter` |

## Sort

Type `ProfileSort`.

Fields: GithubHandle, ID, Preferences, UserID, Verified, Website
