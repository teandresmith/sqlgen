# Account

- **Table:** `accounts`
- **Kind:** table

Customer accounts

## Files

- `models_gen.go`

## Primary key

- Kind: single
- `id` — ID `uint64`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `uint64` | `bigint unsigned` |  | yes |  |  | `comparator.Number[uint64]` |  |
| `email` | Email | `string` | `varchar(255)` |  |  |  |  | `comparator.String` | Login email address |
| `display_name` | DisplayName | `string` | `varchar(100)` |  |  |  |  | `comparator.String` | Name shown to other users |
| `is_active` | IsActive | `bool` | `tinyint(1)` |  |  |  | `1` | `comparator.Bool` |  |
| `created_at` | CreatedAt | `time.Time` | `datetime` |  |  |  | `current_timestamp()` | `comparator.Time` |  |
| `nickname` | Nickname | `*string` | `varchar(100)` | yes |  |  |  | `comparator.NullableString` |  |
| `locale` | Locale | `string` | `varchar(10)` |  |  |  | `'en'` | `comparator.String` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `PRIMARY` | id | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| projects | o2m | Project | projects.account_id |  |
| tasks | o2m | Task | tasks.assignee_id |  |

## Query methods

### Get

- Params: `id uint64`
- Returns: `*Account`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

mysql:

```sql
SELECT `created_at`, `display_name`, `email`, `id`, `is_active`, `locale`, `nickname` FROM `accounts` WHERE `id` = ? LIMIT 1
```

### GetMany

- Params: `input *GetAccountsInput`
- Returns: `[]*Account`, `error`

Generated SQL:

mysql:

```sql
SELECT `created_at`, `display_name`, `email`, `id`, `is_active`, `locale`, `nickname` FROM `accounts` WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *AccountFilter`
- Returns: `int64`, `error`

Generated SQL:

mysql:

```sql
SELECT COUNT(*) FROM `accounts` WHERE <filter>
```

### Exists

- Params: `id uint64`
- Returns: `bool`, `error`

Generated SQL:

mysql:

```sql
SELECT EXISTS(SELECT 1 FROM `accounts` WHERE `id` = ?)
```

### ExistsWhere

- Params: `filter *AccountFilter`
- Returns: `bool`, `error`

Generated SQL:

mysql:

```sql
SELECT EXISTS(SELECT 1 FROM `accounts` WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[AccountFilter]`
- Returns: `*PaginateResult[Account]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[AccountFilter]`
- Returns: `*Connection[Account]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamAccountsInput`
- Returns: `iter.Seq2[*Account, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

mysql:

```sql
SELECT `created_at`, `display_name`, `email`, `id`, `is_active`, `locale`, `nickname` FROM `accounts` WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateAccountInput`
- Returns: `*Account`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

mysql:

```sql
INSERT INTO `accounts` (<columns>) VALUES (<values>)
```

### CreateMany

- Params: `inputs []*CreateAccountInput`
- Returns: `[]*Account`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

mysql:

```sql
INSERT INTO `accounts` (<columns>) VALUES <values>
```

### Upsert

- Params: `input *CreateAccountInput`, `target AccountConflictTarget`
- Returns: `*Account`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated AccountConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

mysql:

```sql
INSERT INTO `accounts` (<columns>) VALUES (<values>) ON DUPLICATE KEY UPDATE <excluded>
```

### UpsertMany

- Params: `inputs []*CreateAccountInput`, `target AccountConflictTarget`
- Returns: `[]*Account`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same AccountConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

mysql:

```sql
INSERT INTO `accounts` (<columns>) VALUES <values> ON DUPLICATE KEY UPDATE <excluded>
```

### Update

- Params: `id uint64`, `input *UpdateAccountInput`
- Returns: `*Account`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

mysql:

```sql
UPDATE `accounts` SET <set> WHERE `id` = ?
```

### UpdateMany

- Params: `items []UpdateAccountItem`
- Returns: `[]*Account`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

mysql:

```sql
UPDATE `accounts` SET <set> WHERE `id` = ?
```

### UpdateWhere

- Params: `filter *AccountFilter`, `input *UpdateAccountInput`
- Returns: `[]*Account`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

mysql:

```sql
UPDATE `accounts` SET <set> WHERE <filter>
```

### HardDelete

- Params: `id uint64`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

mysql:

```sql
DELETE FROM `accounts` WHERE `id` = ?
```

### HardDeleteMany

- Params: `ids []uint64`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

mysql:

```sql
DELETE FROM `accounts` WHERE `id` IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *AccountFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

mysql:

```sql
DELETE FROM `accounts` WHERE <filter>
```

## Filter

Type `AccountFilter`.

| Field | Type |
| --- | --- |
| CreatedAt | `*comparator.Time` |
| DisplayName | `*comparator.String` |
| Email | `*comparator.String` |
| ID | `*comparator.Number[uint64]` |
| IsActive | `*comparator.Bool` |
| Locale | `*comparator.String` |
| Nickname | `*comparator.NullableString` |
| And | `[]*AccountFilter` |
| Or | `[]*AccountFilter` |

## Sort

Type `AccountSort`.

Fields: CreatedAt, DisplayName, Email, ID, IsActive, Locale, Nickname
