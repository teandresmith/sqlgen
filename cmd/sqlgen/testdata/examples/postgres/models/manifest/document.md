# Document

- **Table:** `documents` (schema `public`)
- **Kind:** table

Polymorphic child rows; entity_type sub-categorizes per-parent relationship

## Files

- `models_gen.go`

## Primary key

- Kind: single
- `id` — ID `uuid.UUID`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `uuid.UUID` | `uuid` |  | yes |  | `gen_random_uuid()` | `comparator.ID` |  |
| `entity_id` | EntityID | `uuid.UUID` | `uuid` |  |  |  |  | `comparator.ID` |  |
| `entity_type` | EntityType | `DocumentEntityTypeEnum` | `document_entity_type_enum` |  |  |  |  | `comparator.Enum[DocumentEntityTypeEnum]` |  |
| `name` | Name | `string` | `text` |  |  |  |  | `comparator.String` |  |
| `created_at` | CreatedAt | `time.Time` | `timestamptz` |  |  |  | `current_timestamp` | `comparator.Time` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `documents_pkey` | id | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| assets | m2m | Asset | public.asset_document_links (document_id → asset_id) |  |

## Query methods

### Get

- Params: `id uuid.UUID`
- Returns: `*Document`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "created_at", "entity_id", "entity_type", "id", "name" FROM "public"."documents" WHERE "id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetDocumentsInput`
- Returns: `[]*Document`, `error`

Generated SQL:

postgres:

```sql
SELECT "created_at", "entity_id", "entity_type", "id", "name" FROM "public"."documents" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *DocumentFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."documents" WHERE <filter>
```

### Exists

- Params: `id uuid.UUID`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."documents" WHERE "id" = $1)
```

### ExistsWhere

- Params: `filter *DocumentFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."documents" WHERE <filter>)
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

postgres:

```sql
SELECT "created_at", "entity_id", "entity_type", "id", "name" FROM "public"."documents" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateDocumentInput`
- Returns: `*Document`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."documents" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateDocumentInput`
- Returns: `[]*Document`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."documents" (<columns>) VALUES <values> RETURNING "id"
```

### Upsert

- Params: `input *CreateDocumentInput`, `target DocumentConflictTarget`
- Returns: `*Document`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated DocumentConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."documents" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateDocumentInput`, `target DocumentConflictTarget`
- Returns: `[]*Document`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same DocumentConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."documents" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `id uuid.UUID`, `input *UpdateDocumentInput`
- Returns: `*Document`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."documents" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdateDocumentItem`
- Returns: `[]*Document`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."documents" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *DocumentFilter`, `input *UpdateDocumentInput`
- Returns: `[]*Document`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."documents" SET <set> WHERE <filter> RETURNING "id"
```

### HardDelete

- Params: `id uuid.UUID`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."documents" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []uuid.UUID`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."documents" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *DocumentFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."documents" WHERE <filter> RETURNING "id"
```

## Filter

Type `DocumentFilter`.

| Field | Type |
| --- | --- |
| CreatedAt | `*comparator.Time` |
| EntityID | `*comparator.ID` |
| EntityType | `*comparator.Enum[DocumentEntityTypeEnum]` |
| ID | `*comparator.ID` |
| Name | `*comparator.String` |
| And | `[]*DocumentFilter` |
| Or | `[]*DocumentFilter` |

## Sort

Type `DocumentSort`.

Fields: CreatedAt, EntityID, EntityType, ID, Name
