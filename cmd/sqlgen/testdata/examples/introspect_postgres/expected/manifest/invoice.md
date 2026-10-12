# Invoice

- **Table:** `invoices` (schema `billing`)
- **Kind:** table

Invoices issued to an account

## Files

- `models_gen.go`

## Primary key

- Kind: single
- `id` — ID `int64`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `int64` | `bigint` |  | yes |  |  | `comparator.Number[int64]` |  |
| `account_id` | AccountID | `int64` | `bigint` |  |  |  |  | `comparator.Number[int64]` |  |
| `subtotal_cents` | SubtotalCents | `int32` | `integer` |  |  |  |  | `comparator.Number[int32]` |  |
| `tax_cents` | TaxCents | `int32` | `integer` |  |  |  |  | `comparator.Number[int32]` |  |
| `total_cents` | TotalCents | `*int32` | `integer` | yes |  |  |  | `comparator.NullableNumber[int32]` | Subtotal plus tax |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `invoices_pkey` | id | yes | btree |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*Invoice`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "account_id", "id", "subtotal_cents", "tax_cents", "total_cents" FROM "billing"."invoices" WHERE "id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetInvoicesInput`
- Returns: `[]*Invoice`, `error`

Generated SQL:

postgres:

```sql
SELECT "account_id", "id", "subtotal_cents", "tax_cents", "total_cents" FROM "billing"."invoices" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *InvoiceFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "billing"."invoices" WHERE <filter>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "billing"."invoices" WHERE "id" = $1)
```

### ExistsWhere

- Params: `filter *InvoiceFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "billing"."invoices" WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[InvoiceFilter]`
- Returns: `*PaginateResult[Invoice]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[InvoiceFilter]`
- Returns: `*Connection[Invoice]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamInvoicesInput`
- Returns: `iter.Seq2[*Invoice, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "account_id", "id", "subtotal_cents", "tax_cents", "total_cents" FROM "billing"."invoices" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateInvoiceInput`
- Returns: `*Invoice`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

postgres:

```sql
INSERT INTO "billing"."invoices" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateInvoiceInput`
- Returns: `[]*Invoice`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "billing"."invoices" (<columns>) VALUES <values> RETURNING "id"
```

### Upsert

- Params: `input *CreateInvoiceInput`, `target InvoiceConflictTarget`
- Returns: `*Invoice`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated InvoiceConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "billing"."invoices" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateInvoiceInput`, `target InvoiceConflictTarget`
- Returns: `[]*Invoice`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same InvoiceConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "billing"."invoices" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `id int64`, `input *UpdateInvoiceInput`
- Returns: `*Invoice`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "billing"."invoices" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdateInvoiceItem`
- Returns: `[]*Invoice`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "billing"."invoices" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *InvoiceFilter`, `input *UpdateInvoiceInput`
- Returns: `[]*Invoice`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "billing"."invoices" SET <set> WHERE <filter> RETURNING "id"
```

### Increment

- Params: `id int64`, `input IncrementInput[InvoiceIncrementColumn]`
- Returns: `error`
- Errors: `ErrNotFound`
- Notes: Atomic single-column arithmetic. The column is restricted to the generated InvoiceIncrementColumn enum — numeric, and identifying nothing: neither PK, FK, nor the tenant column. A negative amount decrements.

Generated SQL:

postgres:

```sql
UPDATE "billing"."invoices" SET <column> = <column> + $1 WHERE "id" = $2
```

### HardDelete

- Params: `id int64`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "billing"."invoices" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "billing"."invoices" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *InvoiceFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "billing"."invoices" WHERE <filter> RETURNING "id"
```

## Filter

Type `InvoiceFilter`.

| Field | Type |
| --- | --- |
| AccountID | `*comparator.Number[int64]` |
| ID | `*comparator.Number[int64]` |
| SubtotalCents | `*comparator.Number[int32]` |
| TaxCents | `*comparator.Number[int32]` |
| TotalCents | `*comparator.NullableNumber[int32]` |
| And | `[]*InvoiceFilter` |
| Or | `[]*InvoiceFilter` |

## Sort

Type `InvoiceSort`.

Fields: AccountID, ID, SubtotalCents, TaxCents, TotalCents
