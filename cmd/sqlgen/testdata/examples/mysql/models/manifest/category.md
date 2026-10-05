# Category

- **Table:** `categories`
- **Kind:** table

Product classification categories

## Files

- `models_gen.go`

## Primary key

- Kind: single
- `id` — ID `int32`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `int32` | `int` |  | yes |  |  | `comparator.Number[int32]` | Unique identifier |
| `name` | Name | `string` | `varchar(255)` |  |  | yes |  | `comparator.String` | Category display name |
| `description` | Description | `*string` | `text` | yes |  |  |  | `comparator.NullableString` | Optional long-form description |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `categories_name_key` | name | yes | btree |  |
| `categories_pkey` | id | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| products | o2m | Product | products.category_id |  |
| users | m2m | User | user_categories (category_id → user_id) |  |

## Query methods

### Get

- Params: `id int32`
- Returns: `*Category`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

mysql:

```sql
SELECT `description`, `id`, `name` FROM `categories` WHERE `id` = ? LIMIT 1
```

### GetMany

- Params: `input *GetCategoriesInput`
- Returns: `[]*Category`, `error`

Generated SQL:

mysql:

```sql
SELECT `description`, `id`, `name` FROM `categories` WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *CategoryFilter`
- Returns: `int64`, `error`

Generated SQL:

mysql:

```sql
SELECT COUNT(*) FROM `categories` WHERE <filter>
```

### Exists

- Params: `id int32`
- Returns: `bool`, `error`

Generated SQL:

mysql:

```sql
SELECT EXISTS(SELECT 1 FROM `categories` WHERE `id` = ?)
```

### ExistsWhere

- Params: `filter *CategoryFilter`
- Returns: `bool`, `error`

Generated SQL:

mysql:

```sql
SELECT EXISTS(SELECT 1 FROM `categories` WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[CategoryFilter]`
- Returns: `*PaginateResult[Category]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[CategoryFilter]`
- Returns: `*Connection[Category]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamCategoriesInput`
- Returns: `iter.Seq2[*Category, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

mysql:

```sql
SELECT `description`, `id`, `name` FROM `categories` WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateCategoryInput`
- Returns: `*Category`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

mysql:

```sql
INSERT INTO `categories` (<columns>) VALUES (<values>)
```

### CreateMany

- Params: `inputs []*CreateCategoryInput`
- Returns: `[]*Category`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

mysql:

```sql
INSERT INTO `categories` (<columns>) VALUES <values>
```

### Upsert

- Params: `input *CreateCategoryInput`, `target CategoryConflictTarget`
- Returns: `*Category`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated CategoryConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

mysql:

```sql
INSERT INTO `categories` (<columns>) VALUES (<values>) ON DUPLICATE KEY UPDATE <excluded>
```

### UpsertMany

- Params: `inputs []*CreateCategoryInput`, `target CategoryConflictTarget`
- Returns: `[]*Category`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same CategoryConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

mysql:

```sql
INSERT INTO `categories` (<columns>) VALUES <values> ON DUPLICATE KEY UPDATE <excluded>
```

### Update

- Params: `id int32`, `input *UpdateCategoryInput`
- Returns: `*Category`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

mysql:

```sql
UPDATE `categories` SET <set> WHERE `id` = ?
```

### UpdateMany

- Params: `items []UpdateCategoryItem`
- Returns: `[]*Category`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

mysql:

```sql
UPDATE `categories` SET <set> WHERE `id` = ?
```

### UpdateWhere

- Params: `filter *CategoryFilter`, `input *UpdateCategoryInput`
- Returns: `[]*Category`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

mysql:

```sql
UPDATE `categories` SET <set> WHERE <filter>
```

### HardDelete

- Params: `id int32`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

mysql:

```sql
DELETE FROM `categories` WHERE `id` = ?
```

### HardDeleteMany

- Params: `ids []int32`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

mysql:

```sql
DELETE FROM `categories` WHERE `id` IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *CategoryFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

mysql:

```sql
DELETE FROM `categories` WHERE <filter>
```

### CreateWithRelated

- Params: `input *CreateCategoryWithRelatedInput`
- Returns: `*Category`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Create and, per eligible relationship (Products), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### UpdateWithRelated

- Params: `id int32`, `input *UpdateCategoryWithRelatedInput`
- Returns: `*Category`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrConstraintViolation`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Update and, per eligible relationship (Products), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### UpsertWithRelated

- Params: `input *UpsertCategoryWithRelatedInput`, `target CategoryConflictTarget`
- Returns: `*Category`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Upsert and, per eligible relationship (Products), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

## Filter

Type `CategoryFilter`.

| Field | Type |
| --- | --- |
| Description | `*comparator.NullableString` |
| ID | `*comparator.Number[int32]` |
| Name | `*comparator.String` |
| And | `[]*CategoryFilter` |
| Or | `[]*CategoryFilter` |

## Sort

Type `CategorySort`.

Fields: Description, ID, Name
