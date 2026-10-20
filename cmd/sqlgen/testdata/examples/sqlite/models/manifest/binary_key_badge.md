# BinaryKeyBadge

- **Table:** `binary_key_badges`
- **Kind:** table

## Files

- `binary_key_badge_gen.go`

## Primary key

- Kind: composite
- Struct: `BinaryKeyBadgePK`
- `binary_key_id` — BinaryKeyID `[]byte`
- `badge_code` — BadgeCode `string`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `binary_key_id` | BinaryKeyID | `[]byte` | `blob` |  | yes |  |  | `comparator.Opaque[[]byte]` |  |
| `badge_code` | BadgeCode | `string` | `text` |  | yes |  |  | `comparator.ID` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `binary_key_badges_pkey` | binary_key_id, badge_code | yes | btree |  |

## Query methods

### Get

- Params: `pk BinaryKeyBadgePK`
- Returns: `*BinaryKeyBadge`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

sqlite:

```sql
SELECT "badge_code", "binary_key_id" FROM "binary_key_badges" WHERE "binary_key_id" = ? AND "badge_code" = ? LIMIT 1
```

### GetMany

- Params: `input *GetBinaryKeyBadgesInput`
- Returns: `[]*BinaryKeyBadge`, `error`

Generated SQL:

sqlite:

```sql
SELECT "badge_code", "binary_key_id" FROM "binary_key_badges" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *BinaryKeyBadgeFilter`
- Returns: `int64`, `error`

Generated SQL:

sqlite:

```sql
SELECT COUNT(*) FROM "binary_key_badges" WHERE <filter>
```

### Exists

- Params: `pk BinaryKeyBadgePK`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "binary_key_badges" WHERE "binary_key_id" = ? AND "badge_code" = ?)
```

### ExistsWhere

- Params: `filter *BinaryKeyBadgeFilter`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "binary_key_badges" WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[BinaryKeyBadgeFilter]`
- Returns: `*PaginateResult[BinaryKeyBadge]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[BinaryKeyBadgeFilter]`
- Returns: `*Connection[BinaryKeyBadge]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamBinaryKeyBadgesInput`
- Returns: `iter.Seq2[*BinaryKeyBadge, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

sqlite:

```sql
SELECT "badge_code", "binary_key_id" FROM "binary_key_badges" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateBinaryKeyBadgeInput`
- Returns: `*BinaryKeyBadge`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

sqlite:

```sql
INSERT INTO "binary_key_badges" (<columns>) VALUES (<values>)
```

### CreateMany

- Params: `inputs []*CreateBinaryKeyBadgeInput`
- Returns: `[]*BinaryKeyBadge`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "binary_key_badges" (<columns>) VALUES <values>
```

### Upsert

- Params: `input *CreateBinaryKeyBadgeInput`, `target BinaryKeyBadgeConflictTarget`
- Returns: `*BinaryKeyBadge`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated BinaryKeyBadgeConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

sqlite:

```sql
INSERT INTO "binary_key_badges" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### UpsertMany

- Params: `inputs []*CreateBinaryKeyBadgeInput`, `target BinaryKeyBadgeConflictTarget`
- Returns: `[]*BinaryKeyBadge`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same BinaryKeyBadgeConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "binary_key_badges" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `pk BinaryKeyBadgePK`, `input *UpdateBinaryKeyBadgeInput`
- Returns: `*BinaryKeyBadge`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

sqlite:

```sql
UPDATE "binary_key_badges" SET <set> WHERE "binary_key_id" = ? AND "badge_code" = ?
```

### UpdateMany

- Params: `items []UpdateBinaryKeyBadgeItem`
- Returns: `[]*BinaryKeyBadge`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

sqlite:

```sql
UPDATE "binary_key_badges" SET <set> WHERE "binary_key_id" = ? AND "badge_code" = ?
```

### UpdateWhere

- Params: `filter *BinaryKeyBadgeFilter`, `input *UpdateBinaryKeyBadgeInput`
- Returns: `[]*BinaryKeyBadge`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
UPDATE "binary_key_badges" SET <set> WHERE <filter> RETURNING "binary_key_id", "badge_code"
```

### HardDelete

- Params: `pk BinaryKeyBadgePK`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "binary_key_badges" WHERE "binary_key_id" = ? AND "badge_code" = ?
```

### HardDeleteMany

- Params: `pks []BinaryKeyBadgePK`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "binary_key_badges" WHERE ("binary_key_id", "badge_code") IN (<pks>)
```

### HardDeleteWhere

- Params: `filter *BinaryKeyBadgeFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
DELETE FROM "binary_key_badges" WHERE <filter> RETURNING "binary_key_id", "badge_code"
```

## Filter

Type `BinaryKeyBadgeFilter`.

| Field | Type |
| --- | --- |
| BadgeCode | `*comparator.ID` |
| BinaryKeyID | `*comparator.Opaque[[]byte]` |
| And | `[]*BinaryKeyBadgeFilter` |
| Or | `[]*BinaryKeyBadgeFilter` |

## Sort

Type `BinaryKeyBadgeSort`.

Fields: BadgeCode, BinaryKeyID
