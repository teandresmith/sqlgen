# Document

- **Table:** `documents`
- **Kind:** table

## Files

- `document_gen.go`

## Primary key

- Kind: single
- `id` — ID `int64`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `int64` | `integer` |  | yes |  |  | `comparator.Number[int64]` |  |
| `entity_id` | EntityID | `int64` | `integer` |  |  |  |  | `comparator.Number[int64]` |  |
| `entity_type` | EntityType | `string` | `text` |  |  |  |  | `comparator.String` |  |
| `name` | Name | `string` | `text` |  |  |  |  | `comparator.String` |  |
| `created_at` | CreatedAt | `types.DateTime` | `datetime` |  |  |  | `datetime('now')` | `comparator.Time` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `documents_pkey` | id | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| assets | m2m | Asset | asset_document_links (document_id → asset_id) |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*Document`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

sqlite:

```sql
SELECT "created_at", "entity_id", "entity_type", "id", "name" FROM "documents" WHERE "id" = ? LIMIT 1
```

### GetMany

- Params: `input *GetDocumentsInput`
- Returns: `[]*Document`, `error`

Generated SQL:

sqlite:

```sql
SELECT "created_at", "entity_id", "entity_type", "id", "name" FROM "documents" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *DocumentFilter`
- Returns: `int64`, `error`

Generated SQL:

sqlite:

```sql
SELECT COUNT(*) FROM "documents" WHERE <filter>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "documents" WHERE "id" = ?)
```

### ExistsWhere

- Params: `filter *DocumentFilter`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "documents" WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[DocumentFilter]`
- Returns: `*PaginateResult[Document]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[DocumentFilter]`
- Returns: `*Connection[Document]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamDocumentsInput`
- Returns: `iter.Seq2[*Document, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

sqlite:

```sql
SELECT "created_at", "entity_id", "entity_type", "id", "name" FROM "documents" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateDocumentInput`
- Returns: `*Document`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

sqlite:

```sql
INSERT INTO "documents" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateDocumentInput`
- Returns: `[]*Document`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "documents" (<columns>) VALUES <values> RETURNING "id"
```

### Upsert

- Params: `input *CreateDocumentInput`, `target DocumentConflictTarget`
- Returns: `*Document`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated DocumentConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

sqlite:

```sql
INSERT INTO "documents" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateDocumentInput`, `target DocumentConflictTarget`
- Returns: `[]*Document`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same DocumentConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "documents" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `id int64`, `input *UpdateDocumentInput`
- Returns: `*Document`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

sqlite:

```sql
UPDATE "documents" SET <set> WHERE "id" = ?
```

### UpdateMany

- Params: `items []UpdateDocumentItem`
- Returns: `[]*Document`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

sqlite:

```sql
UPDATE "documents" SET <set> WHERE "id" = ?
```

### UpdateWhere

- Params: `filter *DocumentFilter`, `input *UpdateDocumentInput`
- Returns: `[]*Document`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
UPDATE "documents" SET <set> WHERE <filter> RETURNING "id"
```

### HardDelete

- Params: `id int64`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "documents" WHERE "id" = ?
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "documents" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *DocumentFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
DELETE FROM "documents" WHERE <filter> RETURNING "id"
```

## Filter

Type `DocumentFilter`.

| Field | Type |
| --- | --- |
| CreatedAt | `*comparator.Time` |
| EntityID | `*comparator.Number[int64]` |
| EntityType | `*comparator.String` |
| ID | `*comparator.Number[int64]` |
| Name | `*comparator.String` |
| And | `[]*DocumentFilter` |
| Or | `[]*DocumentFilter` |

## Sort

Type `DocumentSort`.

Fields: CreatedAt, EntityID, EntityType, ID, Name
