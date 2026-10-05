# AuditUser

- **Table:** `users` (schema `audit`)
- **Kind:** table

Audit log of user account changes

## Files

- `models_gen.go`

## Primary key

- Kind: single
- `id` — ID `uuid.UUID`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `uuid.UUID` | `uuid` |  | yes |  | `gen_random_uuid()` | `comparator.ID` |  |
| `action` | Action | `string` | `text` |  |  |  |  | `comparator.String` |  |
| `details` | Details | `types.JSON` | `jsonb` | yes |  |  |  | `comparator.NullableJSONB` |  |
| `source_ip` | SourceIP | `*netip.Addr` | `inet` | yes |  |  |  | `comparator.NullableString` | Client address the change originated from |
| `created_at` | CreatedAt | `time.Time` | `timestamptz` |  |  |  | `current_timestamp` | `comparator.Time` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `users_pkey` | id | yes | btree |  |

## Query methods

### Get

- Params: `id uuid.UUID`
- Returns: `*AuditUser`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "action", "created_at", "details", "id", "source_ip" FROM "audit"."users" WHERE "id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetAuditUsersInput`
- Returns: `[]*AuditUser`, `error`

Generated SQL:

postgres:

```sql
SELECT "action", "created_at", "details", "id", "source_ip" FROM "audit"."users" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *AuditUserFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "audit"."users" WHERE <filter>
```

### Exists

- Params: `id uuid.UUID`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "audit"."users" WHERE "id" = $1)
```

### ExistsWhere

- Params: `filter *AuditUserFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "audit"."users" WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[AuditUserFilter]`
- Returns: `*PaginateResult[AuditUser]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[AuditUserFilter]`
- Returns: `*Connection[AuditUser]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamAuditUsersInput`
- Returns: `iter.Seq2[*AuditUser, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "action", "created_at", "details", "id", "source_ip" FROM "audit"."users" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateAuditUserInput`
- Returns: `*AuditUser`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

postgres:

```sql
INSERT INTO "audit"."users" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateAuditUserInput`
- Returns: `[]*AuditUser`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "audit"."users" (<columns>) VALUES <values> RETURNING "id"
```

### Upsert

- Params: `input *CreateAuditUserInput`, `target AuditUserConflictTarget`
- Returns: `*AuditUser`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated AuditUserConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "audit"."users" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateAuditUserInput`, `target AuditUserConflictTarget`
- Returns: `[]*AuditUser`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same AuditUserConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "audit"."users" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `id uuid.UUID`, `input *UpdateAuditUserInput`
- Returns: `*AuditUser`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "audit"."users" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdateAuditUserItem`
- Returns: `[]*AuditUser`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "audit"."users" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *AuditUserFilter`, `input *UpdateAuditUserInput`
- Returns: `[]*AuditUser`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "audit"."users" SET <set> WHERE <filter> RETURNING "id"
```

### HardDelete

- Params: `id uuid.UUID`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "audit"."users" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []uuid.UUID`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "audit"."users" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *AuditUserFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "audit"."users" WHERE <filter> RETURNING "id"
```

## Filter

Type `AuditUserFilter`.

| Field | Type |
| --- | --- |
| Action | `*comparator.String` |
| CreatedAt | `*comparator.Time` |
| Details | `*comparator.NullableJSONB` |
| ID | `*comparator.ID` |
| SourceIP | `*comparator.NullableString` |
| And | `[]*AuditUserFilter` |
| Or | `[]*AuditUserFilter` |

## Sort

Type `AuditUserSort`.

Fields: Action, CreatedAt, Details, ID, SourceIP
