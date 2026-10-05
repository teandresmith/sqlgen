# ProductTagLabel

- **Table:** `product_tag_labels`
- **Kind:** table

## Files

- `product_tag_label_gen.go`

## Primary key

- Kind: composite
- Struct: `ProductTagLabelPK`
- `product_id` — ProductID `int64`
- `tag_name` — TagName `string`
- `label` — Label `string`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `product_id` | ProductID | `int64` | `integer` |  | yes |  |  | `comparator.Number[int64]` |  |
| `tag_name` | TagName | `string` | `text` |  | yes |  |  | `comparator.ID` |  |
| `label` | Label | `string` | `text` |  | yes |  |  | `comparator.ID` |  |
| `description` | Description | `*string` | `text` | yes |  |  |  | `comparator.NullableString` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `product_tag_labels_pkey` | product_id, tag_name, label | yes | btree |  |

## Query methods

### Get

- Params: `pk ProductTagLabelPK`
- Returns: `*ProductTagLabel`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

sqlite:

```sql
SELECT "description", "label", "product_id", "tag_name" FROM "product_tag_labels" WHERE "product_id" = ? AND "tag_name" = ? AND "label" = ? LIMIT 1
```

### GetMany

- Params: `input *GetProductTagLabelsInput`
- Returns: `[]*ProductTagLabel`, `error`

Generated SQL:

sqlite:

```sql
SELECT "description", "label", "product_id", "tag_name" FROM "product_tag_labels" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *ProductTagLabelFilter`
- Returns: `int64`, `error`

Generated SQL:

sqlite:

```sql
SELECT COUNT(*) FROM "product_tag_labels" WHERE <filter>
```

### Exists

- Params: `pk ProductTagLabelPK`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "product_tag_labels" WHERE "product_id" = ? AND "tag_name" = ? AND "label" = ?)
```

### ExistsWhere

- Params: `filter *ProductTagLabelFilter`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "product_tag_labels" WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[ProductTagLabelFilter]`
- Returns: `*PaginateResult[ProductTagLabel]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[ProductTagLabelFilter]`
- Returns: `*Connection[ProductTagLabel]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamProductTagLabelsInput`
- Returns: `iter.Seq2[*ProductTagLabel, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

sqlite:

```sql
SELECT "description", "label", "product_id", "tag_name" FROM "product_tag_labels" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateProductTagLabelInput`
- Returns: `*ProductTagLabel`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

sqlite:

```sql
INSERT INTO "product_tag_labels" (<columns>) VALUES (<values>)
```

### CreateMany

- Params: `inputs []*CreateProductTagLabelInput`
- Returns: `[]*ProductTagLabel`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "product_tag_labels" (<columns>) VALUES <values>
```

### Upsert

- Params: `input *CreateProductTagLabelInput`, `target ProductTagLabelConflictTarget`
- Returns: `*ProductTagLabel`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated ProductTagLabelConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

sqlite:

```sql
INSERT INTO "product_tag_labels" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### UpsertMany

- Params: `inputs []*CreateProductTagLabelInput`, `target ProductTagLabelConflictTarget`
- Returns: `[]*ProductTagLabel`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same ProductTagLabelConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "product_tag_labels" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `pk ProductTagLabelPK`, `input *UpdateProductTagLabelInput`
- Returns: `*ProductTagLabel`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

sqlite:

```sql
UPDATE "product_tag_labels" SET <set> WHERE "product_id" = ? AND "tag_name" = ? AND "label" = ?
```

### UpdateMany

- Params: `items []UpdateProductTagLabelItem`
- Returns: `[]*ProductTagLabel`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

sqlite:

```sql
UPDATE "product_tag_labels" SET <set> WHERE "product_id" = ? AND "tag_name" = ? AND "label" = ?
```

### UpdateWhere

- Params: `filter *ProductTagLabelFilter`, `input *UpdateProductTagLabelInput`
- Returns: `[]*ProductTagLabel`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
UPDATE "product_tag_labels" SET <set> WHERE <filter> RETURNING "product_id", "tag_name", "label"
```

### HardDelete

- Params: `pk ProductTagLabelPK`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "product_tag_labels" WHERE "product_id" = ? AND "tag_name" = ? AND "label" = ?
```

### HardDeleteMany

- Params: `pks []ProductTagLabelPK`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "product_tag_labels" WHERE ("product_id", "tag_name", "label") IN (<pks>)
```

### HardDeleteWhere

- Params: `filter *ProductTagLabelFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
DELETE FROM "product_tag_labels" WHERE <filter> RETURNING "product_id", "tag_name", "label"
```

## Filter

Type `ProductTagLabelFilter`.

| Field | Type |
| --- | --- |
| Description | `*comparator.NullableString` |
| Label | `*comparator.ID` |
| ProductID | `*comparator.Number[int64]` |
| TagName | `*comparator.ID` |
| And | `[]*ProductTagLabelFilter` |
| Or | `[]*ProductTagLabelFilter` |

## Sort

Type `ProductTagLabelSort`.

Fields: Description, Label, ProductID, TagName
