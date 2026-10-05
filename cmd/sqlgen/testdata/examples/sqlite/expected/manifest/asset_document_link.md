# AssetDocumentLink

- **Table:** `asset_document_links`
- **Kind:** table

## Files

- `asset_document_link_gen.go`

## Primary key

- Kind: composite
- Struct: `AssetDocumentLinkPK`
- `asset_id` — AssetID `int64`
- `document_id` — DocumentID `int64`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `asset_id` | AssetID | `int64` | `integer` |  | yes |  |  | `comparator.Number[int64]` |  |
| `document_id` | DocumentID | `int64` | `integer` |  | yes |  |  | `comparator.Number[int64]` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `asset_document_links_pkey` | asset_id, document_id | yes | btree |  |

## Query methods

### Get

- Params: `pk AssetDocumentLinkPK`
- Returns: `*AssetDocumentLink`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

sqlite:

```sql
SELECT "asset_id", "document_id" FROM "asset_document_links" WHERE "asset_id" = ? AND "document_id" = ? LIMIT 1
```

### GetMany

- Params: `input *GetAssetDocumentLinksInput`
- Returns: `[]*AssetDocumentLink`, `error`

Generated SQL:

sqlite:

```sql
SELECT "asset_id", "document_id" FROM "asset_document_links" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *AssetDocumentLinkFilter`
- Returns: `int64`, `error`

Generated SQL:

sqlite:

```sql
SELECT COUNT(*) FROM "asset_document_links" WHERE <filter>
```

### Exists

- Params: `pk AssetDocumentLinkPK`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "asset_document_links" WHERE "asset_id" = ? AND "document_id" = ?)
```

### ExistsWhere

- Params: `filter *AssetDocumentLinkFilter`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "asset_document_links" WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[AssetDocumentLinkFilter]`
- Returns: `*PaginateResult[AssetDocumentLink]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[AssetDocumentLinkFilter]`
- Returns: `*Connection[AssetDocumentLink]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamAssetDocumentLinksInput`
- Returns: `iter.Seq2[*AssetDocumentLink, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

sqlite:

```sql
SELECT "asset_id", "document_id" FROM "asset_document_links" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateAssetDocumentLinkInput`
- Returns: `*AssetDocumentLink`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

sqlite:

```sql
INSERT INTO "asset_document_links" (<columns>) VALUES (<values>)
```

### CreateMany

- Params: `inputs []*CreateAssetDocumentLinkInput`
- Returns: `[]*AssetDocumentLink`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "asset_document_links" (<columns>) VALUES <values>
```

### Upsert

- Params: `input *CreateAssetDocumentLinkInput`, `target AssetDocumentLinkConflictTarget`
- Returns: `*AssetDocumentLink`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated AssetDocumentLinkConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

sqlite:

```sql
INSERT INTO "asset_document_links" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### UpsertMany

- Params: `inputs []*CreateAssetDocumentLinkInput`, `target AssetDocumentLinkConflictTarget`
- Returns: `[]*AssetDocumentLink`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same AssetDocumentLinkConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "asset_document_links" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `pk AssetDocumentLinkPK`, `input *UpdateAssetDocumentLinkInput`
- Returns: `*AssetDocumentLink`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

sqlite:

```sql
UPDATE "asset_document_links" SET <set> WHERE "asset_id" = ? AND "document_id" = ?
```

### UpdateMany

- Params: `items []UpdateAssetDocumentLinkItem`
- Returns: `[]*AssetDocumentLink`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

sqlite:

```sql
UPDATE "asset_document_links" SET <set> WHERE "asset_id" = ? AND "document_id" = ?
```

### UpdateWhere

- Params: `filter *AssetDocumentLinkFilter`, `input *UpdateAssetDocumentLinkInput`
- Returns: `[]*AssetDocumentLink`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
UPDATE "asset_document_links" SET <set> WHERE <filter> RETURNING "asset_id", "document_id"
```

### HardDelete

- Params: `pk AssetDocumentLinkPK`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "asset_document_links" WHERE "asset_id" = ? AND "document_id" = ?
```

### HardDeleteMany

- Params: `pks []AssetDocumentLinkPK`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "asset_document_links" WHERE ("asset_id", "document_id") IN (<pks>)
```

### HardDeleteWhere

- Params: `filter *AssetDocumentLinkFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
DELETE FROM "asset_document_links" WHERE <filter> RETURNING "asset_id", "document_id"
```

## Filter

Type `AssetDocumentLinkFilter`.

| Field | Type |
| --- | --- |
| AssetID | `*comparator.Number[int64]` |
| DocumentID | `*comparator.Number[int64]` |
| And | `[]*AssetDocumentLinkFilter` |
| Or | `[]*AssetDocumentLinkFilter` |

## Sort

Type `AssetDocumentLinkSort`.

Fields: AssetID, DocumentID
