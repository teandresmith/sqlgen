# LedgerEntry

- **Table:** `ledger_entries` (schema `public`)
- **Kind:** table

## Files

- `models_gen.go`

## Primary key

- Kind: composite
- Struct: `LedgerEntryPK`
- `workspace_id` — WorkspaceID `uuid.UUID`
- `entry_id` — EntryID `int64`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `workspace_id` | WorkspaceID | `uuid.UUID` | `uuid` |  | yes |  |  | `comparator.ID` |  |
| `entry_id` | EntryID | `int64` | `bigint` |  | yes |  |  | `comparator.Number[int64]` |  |
| `amount` | Amount | `int64` | `bigint` |  |  |  |  | `comparator.Number[int64]` |  |
| `ref` | Ref | `*string` | `text` | yes |  | yes |  | `comparator.NullableString` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `ledger_entries_pkey` | workspace_id, entry_id | yes | btree |  |
| `ledger_entries_ref_key` | ref | yes | btree |  |

## Query methods

### Get

- Params: `pk LedgerEntryPK`
- Returns: `*LedgerEntry`, `error`
- Errors: `ErrNotFound`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "amount", "entry_id", "ref", "workspace_id" FROM "public"."ledger_entries" WHERE "workspace_id" = $1 AND "entry_id" = $2 LIMIT 1
```

### GetMany

- Params: `input *GetLedgerEntriesInput`
- Returns: `[]*LedgerEntry`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT "amount", "entry_id", "ref", "workspace_id" FROM "public"."ledger_entries" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Exists

- Params: `pk LedgerEntryPK`
- Returns: `bool`, `error`
- Errors: `tenancy.ErrMissing`, `tenancy.ErrMismatch`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."ledger_entries" WHERE "workspace_id" = $1 AND "entry_id" = $2)
```

### ExistsWhere

- Params: `filter *LedgerEntryFilter`
- Returns: `bool`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."ledger_entries" WHERE <filter>)
```

### Count

- Params: `filter *LedgerEntryFilter`
- Returns: `int64`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."ledger_entries" WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[LedgerEntryFilter]`
- Returns: `*PaginateResult[LedgerEntry]`, `error`
- Errors: `tenancy.ErrMissing`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[LedgerEntryFilter]`
- Returns: `*Connection[LedgerEntry]`, `error`
- Errors: `ErrInvalidCursor`, `tenancy.ErrMissing`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamLedgerEntriesInput`
- Returns: `iter.Seq2[*LedgerEntry, error]`, `error`
- Errors: `tenancy.ErrMissing`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "amount", "entry_id", "ref", "workspace_id" FROM "public"."ledger_entries" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateLedgerEntryInput`
- Returns: `*LedgerEntry`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."ledger_entries" (<columns>) VALUES (<values>)
```

### CreateMany

- Params: `inputs []*CreateLedgerEntryInput`
- Returns: `[]*LedgerEntry`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."ledger_entries" (<columns>) VALUES <values>
```

### Update

- Params: `pk LedgerEntryPK`, `input *UpdateLedgerEntryInput`
- Returns: `*LedgerEntry`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."ledger_entries" SET <set> WHERE "workspace_id" = $1 AND "entry_id" = $2
```

### UpdateMany

- Params: `items []UpdateLedgerEntryItem`
- Returns: `[]*LedgerEntry`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."ledger_entries" SET <set> WHERE "workspace_id" = $1 AND "entry_id" = $2
```

### UpdateWhere

- Params: `filter *LedgerEntryFilter`, `input *UpdateLedgerEntryInput`
- Returns: `[]*LedgerEntry`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`, `tenancy.ErrMissing`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."ledger_entries" SET <set> WHERE <filter> RETURNING "workspace_id", "entry_id"
```

### Upsert

- Params: `input *CreateLedgerEntryInput`, `target LedgerEntryConflictTarget`
- Returns: `*LedgerEntry`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: One method over the generated LedgerEntryConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."ledger_entries" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### UpsertMany

- Params: `inputs []*CreateLedgerEntryInput`, `target LedgerEntryConflictTarget`
- Returns: `[]*LedgerEntry`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Batched in generation.batch_size chunks over the same LedgerEntryConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."ledger_entries" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Increment

- Params: `pk LedgerEntryPK`, `input IncrementInput[LedgerEntryIncrementColumn]`
- Returns: `error`
- Errors: `ErrNotFound`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Atomic single-column arithmetic. The column is restricted to the generated LedgerEntryIncrementColumn enum — numeric, and identifying nothing: neither PK, FK, nor the tenant column. A negative amount decrements.

Generated SQL:

postgres:

```sql
UPDATE "public"."ledger_entries" SET <column> = <column> + $1 WHERE "workspace_id" = $2 AND "entry_id" = $3
```

### HardDelete

- Params: `pk LedgerEntryPK`
- Returns: `error`
- Errors: `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."ledger_entries" WHERE "workspace_id" = $1 AND "entry_id" = $2
```

### HardDeleteMany

- Params: `pks []LedgerEntryPK`
- Returns: `error`
- Errors: `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."ledger_entries" WHERE ("workspace_id", "entry_id") IN (<pks>)
```

### HardDeleteWhere

- Params: `filter *LedgerEntryFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`, `tenancy.ErrMissing`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."ledger_entries" WHERE <filter> RETURNING "workspace_id", "entry_id"
```

## Filter

Type `LedgerEntryFilter`.

| Field | Type |
| --- | --- |
| Amount | `*comparator.Number[int64]` |
| EntryID | `*comparator.Number[int64]` |
| Ref | `*comparator.NullableString` |
| WorkspaceID | `*comparator.ID` |
| And | `[]*LedgerEntryFilter` |
| Or | `[]*LedgerEntryFilter` |

## Sort

Type `LedgerEntrySort`.

Fields: Amount, EntryID, Ref, WorkspaceID

## Read examples

```go
row, err := client.LedgerEntries().Get(ctx, models.LedgerEntryPK{WorkspaceID: uuid.NewV4(), EntryID: 1})
rows, err := client.LedgerEntries().GetMany(ctx, &models.GetLedgerEntriesInput{Filter: &models.LedgerEntryFilter{Amount: &comparator.Number[int64]{Eq: new(int64(1))}}})
```

## Write examples

```go
created, err := client.LedgerEntries().Create(ctx, &models.CreateLedgerEntryInput{WorkspaceID: uuid.NewV4(), EntryID: 1, Amount: 1})
updated, err := client.LedgerEntries().Update(ctx, models.LedgerEntryPK{WorkspaceID: uuid.NewV4(), EntryID: 1}, &models.UpdateLedgerEntryInput{Amount: omittable.Set(int64(1))})
```
