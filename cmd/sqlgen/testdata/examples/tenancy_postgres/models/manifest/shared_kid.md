# SharedKid

- **Table:** `shared_kids` (schema `public`)
- **Kind:** table

## Files

- `models_gen.go`

## Primary key

- Kind: single
- `id` — ID `int64`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `int64` | `bigint` |  | yes |  |  | `comparator.Number[int64]` |  |
| `workspace_id` | WorkspaceID | `uuid.UUID` | `uuid` |  |  |  |  | `comparator.ID` |  |
| `org` | Org | `*string` | `text` | yes |  |  |  | `comparator.NullableString` |  |
| `parent_id` | ParentID | `*int64` | `bigint` | yes |  |  |  | `comparator.NullableNumber[int64]` |  |
| `guardian_id` | GuardianID | `*int64` | `bigint` | yes |  |  |  | `comparator.NullableNumber[int64]` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `shared_kids_pkey` | id | yes | btree |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*SharedKid`, `error`
- Errors: `ErrNotFound`, `tenancy.ErrMissing`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "guardian_id", "id", "org", "parent_id", "workspace_id" FROM "public"."shared_kids" WHERE "id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetSharedKidsInput`
- Returns: `[]*SharedKid`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT "guardian_id", "id", "org", "parent_id", "workspace_id" FROM "public"."shared_kids" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."shared_kids" WHERE "id" = $1)
```

### ExistsWhere

- Params: `filter *SharedKidFilter`
- Returns: `bool`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."shared_kids" WHERE <filter>)
```

### Count

- Params: `filter *SharedKidFilter`
- Returns: `int64`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."shared_kids" WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[SharedKidFilter]`
- Returns: `*PaginateResult[SharedKid]`, `error`
- Errors: `tenancy.ErrMissing`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[SharedKidFilter]`
- Returns: `*Connection[SharedKid]`, `error`
- Errors: `ErrInvalidCursor`, `tenancy.ErrMissing`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamSharedKidsInput`
- Returns: `iter.Seq2[*SharedKid, error]`, `error`
- Errors: `tenancy.ErrMissing`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "guardian_id", "id", "org", "parent_id", "workspace_id" FROM "public"."shared_kids" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateSharedKidInput`
- Returns: `*SharedKid`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."shared_kids" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateSharedKidInput`
- Returns: `[]*SharedKid`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."shared_kids" (<columns>) VALUES <values> RETURNING "id"
```

### Update

- Params: `id int64`, `input *UpdateSharedKidInput`
- Returns: `*SharedKid`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."shared_kids" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdateSharedKidItem`
- Returns: `[]*SharedKid`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."shared_kids" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *SharedKidFilter`, `input *UpdateSharedKidInput`
- Returns: `[]*SharedKid`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."shared_kids" SET <set> WHERE <filter> RETURNING "id"
```

### Upsert

- Params: `input *CreateSharedKidInput`, `target SharedKidConflictTarget`
- Returns: `*SharedKid`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: One method over the generated SharedKidConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."shared_kids" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateSharedKidInput`, `target SharedKidConflictTarget`
- Returns: `[]*SharedKid`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Batched in generation.batch_size chunks over the same SharedKidConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."shared_kids" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### HardDelete

- Params: `id int64`
- Returns: `error`
- Errors: `tenancy.ErrMissing`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."shared_kids" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Errors: `tenancy.ErrMissing`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."shared_kids" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *SharedKidFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`, `tenancy.ErrMissing`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."shared_kids" WHERE <filter> RETURNING "id"
```

## Filter

Type `SharedKidFilter`.

| Field | Type |
| --- | --- |
| GuardianID | `*comparator.NullableNumber[int64]` |
| ID | `*comparator.Number[int64]` |
| Org | `*comparator.NullableString` |
| ParentID | `*comparator.NullableNumber[int64]` |
| WorkspaceID | `*comparator.ID` |
| And | `[]*SharedKidFilter` |
| Or | `[]*SharedKidFilter` |

## Sort

Type `SharedKidSort`.

Fields: GuardianID, ID, Org, ParentID, WorkspaceID

## Read examples

```go
row, err := client.SharedKids().Get(ctx, 1)
rows, err := client.SharedKids().GetMany(ctx, &models.GetSharedKidsInput{Filter: &models.SharedKidFilter{WorkspaceID: &comparator.ID{Eq: new("123e4567-e89b-12d3-a456-426614174000")}}})
```

## Write examples

```go
created, err := client.SharedKids().Create(ctx, &models.CreateSharedKidInput{})
updated, err := client.SharedKids().Update(ctx, 1, &models.UpdateSharedKidInput{WorkspaceID: omittable.Set(uuid.NewV4())})
```
