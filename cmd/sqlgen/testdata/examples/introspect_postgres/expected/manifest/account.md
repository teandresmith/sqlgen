# Account

- **Table:** `accounts` (schema `public`)
- **Kind:** table

Customer accounts

## Files

- `models_gen.go`

## Primary key

- Kind: single
- `id` — ID `int64`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `int64` | `bigint` |  | yes |  |  | `comparator.Number[int64]` |  |
| `email` | Email | `string` | `email_address` |  |  |  |  | `comparator.String` | Login email address |
| `display_name` | DisplayName | `string` | `text` |  |  |  |  | `comparator.String` | Name shown to other users |
| `status` | Status | `AccountStatus` | `account_status` |  |  |  | `'active'` | `comparator.Enum[AccountStatus]` |  |
| `address` | Address | `*string` | `postal_address` | yes |  |  |  | `comparator.NullableString` |  |
| `created_at` | CreatedAt | `time.Time` | `timestamptz` |  |  |  | `now()` | `comparator.Time` |  |
| `nickname` | Nickname | `*string` | `text` | yes |  |  |  | `comparator.NullableString` |  |
| `locale` | Locale | `string` | `text` |  |  |  | `'en'` | `comparator.String` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `accounts_pkey` | id | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| invoices | o2m | Invoice | billing.invoices.account_id |  |
| projects | o2m | Project | public.projects.account_id |  |
| tasks | o2m | Task | public.tasks.assignee_id |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*Account`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "address", "created_at", "display_name", "email", "id", "locale", "nickname", "status" FROM "public"."accounts" WHERE "id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetAccountsInput`
- Returns: `[]*Account`, `error`

Generated SQL:

postgres:

```sql
SELECT "address", "created_at", "display_name", "email", "id", "locale", "nickname", "status" FROM "public"."accounts" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *AccountFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."accounts" WHERE <filter>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."accounts" WHERE "id" = $1)
```

### ExistsWhere

- Params: `filter *AccountFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."accounts" WHERE <filter>)
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

postgres:

```sql
SELECT "address", "created_at", "display_name", "email", "id", "locale", "nickname", "status" FROM "public"."accounts" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateAccountInput`
- Returns: `*Account`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."accounts" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateAccountInput`
- Returns: `[]*Account`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."accounts" (<columns>) VALUES <values> RETURNING "id"
```

### Upsert

- Params: `input *CreateAccountInput`, `target AccountConflictTarget`
- Returns: `*Account`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated AccountConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."accounts" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateAccountInput`, `target AccountConflictTarget`
- Returns: `[]*Account`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same AccountConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."accounts" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `id int64`, `input *UpdateAccountInput`
- Returns: `*Account`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."accounts" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdateAccountItem`
- Returns: `[]*Account`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."accounts" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *AccountFilter`, `input *UpdateAccountInput`
- Returns: `[]*Account`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."accounts" SET <set> WHERE <filter> RETURNING "id"
```

### HardDelete

- Params: `id int64`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."accounts" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."accounts" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *AccountFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."accounts" WHERE <filter> RETURNING "id"
```

## Filter

Type `AccountFilter`.

| Field | Type |
| --- | --- |
| Address | `*comparator.NullableString` |
| CreatedAt | `*comparator.Time` |
| DisplayName | `*comparator.String` |
| Email | `*comparator.String` |
| ID | `*comparator.Number[int64]` |
| Locale | `*comparator.String` |
| Nickname | `*comparator.NullableString` |
| Status | `*comparator.Enum[AccountStatus]` |
| And | `[]*AccountFilter` |
| Or | `[]*AccountFilter` |

## Sort

Type `AccountSort`.

Fields: Address, CreatedAt, DisplayName, Email, ID, Locale, Nickname, Status
