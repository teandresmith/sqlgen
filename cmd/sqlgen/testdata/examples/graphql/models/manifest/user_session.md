# UserSession

- **Table:** `user_sessions` (schema `public`)
- **Kind:** table

## Files

- `models_gen.go`

## Primary key

- Kind: single
- `id` — ID `uuid.UUID`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `uuid.UUID` | `uuid` |  | yes |  | `gen_random_uuid()` | `comparator.ID` |  |
| `user_id` | UserID | `uuid.UUID` | `uuid` |  |  |  |  | `comparator.ID` |  |
| `token_hash` | TokenHash | `string` | `text` |  |  |  |  | `comparator.String` |  |
| `created_at` | CreatedAt | `time.Time` | `timestamptz` |  |  |  | `current_timestamp` | `comparator.Time` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `user_sessions_pkey` | id | yes | btree |  |

## Query methods

### Get

- Params: `id uuid.UUID`
- Returns: `*UserSession`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "created_at", "id", "token_hash", "user_id" FROM "public"."user_sessions" WHERE "id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetUserSessionsInput`
- Returns: `[]*UserSession`, `error`

Generated SQL:

postgres:

```sql
SELECT "created_at", "id", "token_hash", "user_id" FROM "public"."user_sessions" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *UserSessionFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."user_sessions" WHERE <filter>
```

### Exists

- Params: `id uuid.UUID`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."user_sessions" WHERE "id" = $1)
```

### ExistsWhere

- Params: `filter *UserSessionFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."user_sessions" WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[UserSessionFilter]`
- Returns: `*PaginateResult[UserSession]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[UserSessionFilter]`
- Returns: `*Connection[UserSession]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamUserSessionsInput`
- Returns: `iter.Seq2[*UserSession, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "created_at", "id", "token_hash", "user_id" FROM "public"."user_sessions" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateUserSessionInput`
- Returns: `*UserSession`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."user_sessions" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateUserSessionInput`
- Returns: `[]*UserSession`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."user_sessions" (<columns>) VALUES <values> RETURNING "id"
```

### Upsert

- Params: `input *CreateUserSessionInput`, `target UserSessionConflictTarget`
- Returns: `*UserSession`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated UserSessionConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."user_sessions" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateUserSessionInput`, `target UserSessionConflictTarget`
- Returns: `[]*UserSession`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same UserSessionConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."user_sessions" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `id uuid.UUID`, `input *UpdateUserSessionInput`
- Returns: `*UserSession`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."user_sessions" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdateUserSessionItem`
- Returns: `[]*UserSession`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."user_sessions" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *UserSessionFilter`, `input *UpdateUserSessionInput`
- Returns: `[]*UserSession`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."user_sessions" SET <set> WHERE <filter> RETURNING "id"
```

### HardDelete

- Params: `id uuid.UUID`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."user_sessions" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []uuid.UUID`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."user_sessions" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *UserSessionFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."user_sessions" WHERE <filter> RETURNING "id"
```

## Filter

Type `UserSessionFilter`.

| Field | Type |
| --- | --- |
| CreatedAt | `*comparator.Time` |
| ID | `*comparator.ID` |
| TokenHash | `*comparator.String` |
| UserID | `*comparator.ID` |
| And | `[]*UserSessionFilter` |
| Or | `[]*UserSessionFilter` |

## Sort

Type `UserSessionSort`.

Fields: CreatedAt, ID, TokenHash, UserID
