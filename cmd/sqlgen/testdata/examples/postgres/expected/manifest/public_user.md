# PublicUser

- **Table:** `users` (schema `public`)
- **Kind:** table

Registered user accounts

## Files

- `models_gen.go`

## Primary key

- Kind: single
- `id` — ID `uuid.UUID`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `uuid.UUID` | `uuid` |  | yes |  | `gen_random_uuid()` | `comparator.ID` | Unique identifier |
| `email` | Email | `string` | `email` |  |  | yes |  | `comparator.String` | Login email address |
| `name` | Name | `string` | `text` |  |  |  |  | `comparator.String` | Display name |
| `role` | Role | `UserRole` | `user_role` |  |  |  | `'viewer'` | `comparator.Enum[UserRole]` | Authorization level |
| `tags` | Tags | `[]string` | `text[]` | yes |  |  | `'{}'` | `comparator.NullableSlice[string]` | Freeform labels for filtering |
| `metadata` | Metadata | `types.JSON` | `jsonb` | yes |  |  |  | `comparator.NullableJSONB` | Arbitrary key-value data |
| `created_at` | CreatedAt | `time.Time` | `timestamptz` |  |  |  | `current_timestamp` | `comparator.Time` | Row creation timestamp |
| `updated_at` | UpdatedAt | `*time.Time` | `timestamptz` | yes |  |  | `current_timestamp` | `comparator.NullableTime` | Last modification timestamp |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `users_email_key` | email | yes | btree |  |
| `users_pkey` | id | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| AuditNotes | o2m | AuditNote | audit.notes.user_id |  |
| Notes | o2m | PublicNote | public.notes.user_id |  |
| Profile | o2o | Profile | public.profiles.user_id |  |
| WatchedAuditNotes | m2m | AuditNote | audit.note_watchers (user_id → note_id) |  |
| categories | m2m | Category | public.user_categories (user_id → category_id) |  |
| orders | o2m | Order | public.orders.user_id |  |

## Query methods

### Get

- Params: `id uuid.UUID`
- Returns: `*PublicUser`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "created_at", "email", "id", "metadata", "name", "role", "tags", "updated_at" FROM "public"."users" WHERE "id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetPublicUsersInput`
- Returns: `[]*PublicUser`, `error`

Generated SQL:

postgres:

```sql
SELECT "created_at", "email", "id", "metadata", "name", "role", "tags", "updated_at" FROM "public"."users" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *PublicUserFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."users" WHERE <filter>
```

### Exists

- Params: `id uuid.UUID`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."users" WHERE "id" = $1)
```

### ExistsWhere

- Params: `filter *PublicUserFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."users" WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[PublicUserFilter]`
- Returns: `*PaginateResult[PublicUser]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[PublicUserFilter]`
- Returns: `*Connection[PublicUser]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamPublicUsersInput`
- Returns: `iter.Seq2[*PublicUser, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "created_at", "email", "id", "metadata", "name", "role", "tags", "updated_at" FROM "public"."users" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreatePublicUserInput`
- Returns: `*PublicUser`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."users" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreatePublicUserInput`
- Returns: `[]*PublicUser`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."users" (<columns>) VALUES <values> RETURNING "id"
```

### Upsert

- Params: `input *CreatePublicUserInput`, `target PublicUserConflictTarget`
- Returns: `*PublicUser`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated PublicUserConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."users" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreatePublicUserInput`, `target PublicUserConflictTarget`
- Returns: `[]*PublicUser`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same PublicUserConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."users" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `id uuid.UUID`, `input *UpdatePublicUserInput`
- Returns: `*PublicUser`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."users" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdatePublicUserItem`
- Returns: `[]*PublicUser`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."users" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *PublicUserFilter`, `input *UpdatePublicUserInput`
- Returns: `[]*PublicUser`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."users" SET <set> WHERE <filter> RETURNING "id"
```

### HardDelete

- Params: `id uuid.UUID`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."users" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []uuid.UUID`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."users" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *PublicUserFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."users" WHERE <filter> RETURNING "id"
```

## Filter

Type `PublicUserFilter`.

| Field | Type |
| --- | --- |
| CreatedAt | `*comparator.Time` |
| Email | `*comparator.String` |
| ID | `*comparator.ID` |
| Metadata | `*comparator.NullableJSONB` |
| Name | `*comparator.String` |
| Role | `*comparator.Enum[UserRole]` |
| Tags | `*comparator.NullableSlice[string]` |
| UpdatedAt | `*comparator.NullableTime` |
| And | `[]*PublicUserFilter` |
| Or | `[]*PublicUserFilter` |

## Sort

Type `PublicUserSort`.

Fields: CreatedAt, Email, ID, Metadata, Name, Role, Tags, UpdatedAt
