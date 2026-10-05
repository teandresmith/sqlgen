# UserCredential

- **Table:** `user_credentials` (schema `public`)
- **Kind:** table

## Files

- `models_gen.go`

## Primary key

- Kind: single
- `user_id` — UserID `uuid.UUID`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `user_id` | UserID | `uuid.UUID` | `uuid` |  | yes |  |  | `comparator.ID` |  |
| `provider` | Provider | `string` | `text` |  |  |  |  | `comparator.String` |  |
| `external_id` | ExternalID | `string` | `text` |  |  |  |  | `comparator.String` |  |
| `last_login_at` | LastLoginAt | `*time.Time` | `timestamptz` | yes |  |  |  | `comparator.NullableTime` |  |
| `created_at` | CreatedAt | `time.Time` | `timestamptz` |  |  |  | `current_timestamp` | `comparator.Time` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `user_credentials_pkey` | user_id | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| users | o2o | User | public.user_credentials.user_id |  |

## Query methods

### Get

- Params: `id uuid.UUID`
- Returns: `*UserCredential`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "created_at", "external_id", "last_login_at", "provider", "user_id" FROM "public"."user_credentials" WHERE "user_id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetUserCredentialsInput`
- Returns: `[]*UserCredential`, `error`

Generated SQL:

postgres:

```sql
SELECT "created_at", "external_id", "last_login_at", "provider", "user_id" FROM "public"."user_credentials" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *UserCredentialFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."user_credentials" WHERE <filter>
```

### Exists

- Params: `id uuid.UUID`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."user_credentials" WHERE "user_id" = $1)
```

### ExistsWhere

- Params: `filter *UserCredentialFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."user_credentials" WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[UserCredentialFilter]`
- Returns: `*PaginateResult[UserCredential]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[UserCredentialFilter]`
- Returns: `*Connection[UserCredential]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamUserCredentialsInput`
- Returns: `iter.Seq2[*UserCredential, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "created_at", "external_id", "last_login_at", "provider", "user_id" FROM "public"."user_credentials" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateUserCredentialInput`
- Returns: `*UserCredential`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."user_credentials" (<columns>) VALUES (<values>) RETURNING "user_id"
```

### CreateMany

- Params: `inputs []*CreateUserCredentialInput`
- Returns: `[]*UserCredential`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."user_credentials" (<columns>) VALUES <values> RETURNING "user_id"
```

### Upsert

- Params: `input *CreateUserCredentialInput`, `target UserCredentialConflictTarget`
- Returns: `*UserCredential`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated UserCredentialConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."user_credentials" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "user_id"
```

### UpsertMany

- Params: `inputs []*CreateUserCredentialInput`, `target UserCredentialConflictTarget`
- Returns: `[]*UserCredential`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same UserCredentialConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."user_credentials" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `id uuid.UUID`, `input *UpdateUserCredentialInput`
- Returns: `*UserCredential`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."user_credentials" SET <set> WHERE "user_id" = $1
```

### UpdateMany

- Params: `items []UpdateUserCredentialItem`
- Returns: `[]*UserCredential`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."user_credentials" SET <set> WHERE "user_id" = $1
```

### UpdateWhere

- Params: `filter *UserCredentialFilter`, `input *UpdateUserCredentialInput`
- Returns: `[]*UserCredential`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."user_credentials" SET <set> WHERE <filter> RETURNING "user_id"
```

### HardDelete

- Params: `id uuid.UUID`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."user_credentials" WHERE "user_id" = $1
```

### HardDeleteMany

- Params: `ids []uuid.UUID`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."user_credentials" WHERE "user_id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *UserCredentialFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."user_credentials" WHERE <filter> RETURNING "user_id"
```

## Filter

Type `UserCredentialFilter`.

| Field | Type |
| --- | --- |
| CreatedAt | `*comparator.Time` |
| ExternalID | `*comparator.String` |
| LastLoginAt | `*comparator.NullableTime` |
| Provider | `*comparator.String` |
| UserID | `*comparator.ID` |
| And | `[]*UserCredentialFilter` |
| Or | `[]*UserCredentialFilter` |

## Sort

Type `UserCredentialSort`.

Fields: CreatedAt, ExternalID, LastLoginAt, Provider, UserID
