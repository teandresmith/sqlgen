# UserCategory

- **Table:** `user_categories`
- **Kind:** table

Junction table linking users to their preferred categories

## Files

- `models_gen.go`

## Primary key

- Kind: composite
- Struct: `UserCategoryPK`
- `user_id` — UserID `int64`
- `category_id` — CategoryID `int32`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `user_id` | UserID | `int64` | `bigint` |  | yes |  |  | `comparator.Number[int64]` | User side of the relationship |
| `category_id` | CategoryID | `int32` | `int` |  | yes |  |  | `comparator.Number[int32]` | Category side of the relationship |
| `slot` | Slot | `*int32` | `int` | yes |  |  |  | `comparator.NullableNumber[int32]` | Display position of this category for this user. The UNIQUE below does NOT cover the primary key, which makes this the only MySQL table where a caller-known composite key still has to be read back after an upsert (PRD 9.5). Nullable so existing rows leave it unset; MySQL, like the other two dialects, allows repeated NULLs under a unique index. |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `category_id` | category_id |  | btree |  |
| `user_categories_pkey` | user_id, category_id | yes | btree |  |
| `user_categories_user_slot_uq` | user_id, slot | yes | btree |  |

## Query methods

### Get

- Params: `pk UserCategoryPK`
- Returns: `*UserCategory`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

mysql:

```sql
SELECT `category_id`, `slot`, `user_id` FROM `user_categories` WHERE `user_id` = ? AND `category_id` = ? LIMIT 1
```

### GetMany

- Params: `input *GetUserCategoriesInput`
- Returns: `[]*UserCategory`, `error`

Generated SQL:

mysql:

```sql
SELECT `category_id`, `slot`, `user_id` FROM `user_categories` WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *UserCategoryFilter`
- Returns: `int64`, `error`

Generated SQL:

mysql:

```sql
SELECT COUNT(*) FROM `user_categories` WHERE <filter>
```

### Exists

- Params: `pk UserCategoryPK`
- Returns: `bool`, `error`

Generated SQL:

mysql:

```sql
SELECT EXISTS(SELECT 1 FROM `user_categories` WHERE `user_id` = ? AND `category_id` = ?)
```

### ExistsWhere

- Params: `filter *UserCategoryFilter`
- Returns: `bool`, `error`

Generated SQL:

mysql:

```sql
SELECT EXISTS(SELECT 1 FROM `user_categories` WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[UserCategoryFilter]`
- Returns: `*PaginateResult[UserCategory]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[UserCategoryFilter]`
- Returns: `*Connection[UserCategory]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamUserCategoriesInput`
- Returns: `iter.Seq2[*UserCategory, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

mysql:

```sql
SELECT `category_id`, `slot`, `user_id` FROM `user_categories` WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateUserCategoryInput`
- Returns: `*UserCategory`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

mysql:

```sql
INSERT INTO `user_categories` (<columns>) VALUES (<values>)
```

### CreateMany

- Params: `inputs []*CreateUserCategoryInput`
- Returns: `[]*UserCategory`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

mysql:

```sql
INSERT INTO `user_categories` (<columns>) VALUES <values>
```

### Upsert

- Params: `input *CreateUserCategoryInput`, `target UserCategoryConflictTarget`
- Returns: `*UserCategory`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated UserCategoryConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

mysql:

```sql
INSERT INTO `user_categories` (<columns>) VALUES (<values>) ON DUPLICATE KEY UPDATE <excluded>
```

### UpsertMany

- Params: `inputs []*CreateUserCategoryInput`, `target UserCategoryConflictTarget`
- Returns: `[]*UserCategory`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same UserCategoryConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

mysql:

```sql
INSERT INTO `user_categories` (<columns>) VALUES <values> ON DUPLICATE KEY UPDATE <excluded>
```

### Update

- Params: `pk UserCategoryPK`, `input *UpdateUserCategoryInput`
- Returns: `*UserCategory`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

mysql:

```sql
UPDATE `user_categories` SET <set> WHERE `user_id` = ? AND `category_id` = ?
```

### UpdateMany

- Params: `items []UpdateUserCategoryItem`
- Returns: `[]*UserCategory`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

mysql:

```sql
UPDATE `user_categories` SET <set> WHERE `user_id` = ? AND `category_id` = ?
```

### UpdateWhere

- Params: `filter *UserCategoryFilter`, `input *UpdateUserCategoryInput`
- Returns: `[]*UserCategory`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

mysql:

```sql
UPDATE `user_categories` SET <set> WHERE <filter>
```

### Increment

- Params: `pk UserCategoryPK`, `input IncrementInput[UserCategoryIncrementColumn]`
- Returns: `error`
- Errors: `ErrNotFound`
- Notes: Atomic single-column arithmetic. The column is restricted to the generated UserCategoryIncrementColumn enum — numeric, and identifying nothing: neither PK, FK, nor the tenant column. A negative amount decrements.

Generated SQL:

mysql:

```sql
UPDATE `user_categories` SET <column> = <column> + ? WHERE `user_id` = ? AND `category_id` = ?
```

### HardDelete

- Params: `pk UserCategoryPK`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

mysql:

```sql
DELETE FROM `user_categories` WHERE `user_id` = ? AND `category_id` = ?
```

### HardDeleteMany

- Params: `pks []UserCategoryPK`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

mysql:

```sql
DELETE FROM `user_categories` WHERE <pks>
```

### HardDeleteWhere

- Params: `filter *UserCategoryFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

mysql:

```sql
DELETE FROM `user_categories` WHERE <filter>
```

## Filter

Type `UserCategoryFilter`.

| Field | Type |
| --- | --- |
| CategoryID | `*comparator.Number[int32]` |
| Slot | `*comparator.NullableNumber[int32]` |
| UserID | `*comparator.Number[int64]` |
| And | `[]*UserCategoryFilter` |
| Or | `[]*UserCategoryFilter` |

## Sort

Type `UserCategorySort`.

Fields: CategoryID, Slot, UserID
