# KeyedCode

- **Table:** `keyed_codes` (schema `public`)
- **Kind:** table

## Files

- `models_gen.go`

## Primary key

- Kind: single
- `id` — ID `uuid.UUID`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `uuid.UUID` | `uuid` |  | yes |  |  | `comparator.ID` |  |
| `code` | Code | `string` | `text` |  |  | yes |  | `comparator.String` |  |
| `name` | Name | `string` | `text` |  |  |  |  | `comparator.String` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `keyed_codes_code_key` | code | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| Entries | o2m | KeyedCodeEntry | public.keyed_code_entries.keyed_code_id |  |

## Query methods

### Get

- Params: `id uuid.UUID`
- Returns: `*KeyedCode`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "code", "id", "name" FROM "public"."keyed_codes" WHERE "id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetKeyedCodesInput`
- Returns: `[]*KeyedCode`, `error`

Generated SQL:

postgres:

```sql
SELECT "code", "id", "name" FROM "public"."keyed_codes" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *KeyedCodeFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."keyed_codes" WHERE <filter>
```

### Exists

- Params: `id uuid.UUID`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."keyed_codes" WHERE "id" = $1)
```

### ExistsWhere

- Params: `filter *KeyedCodeFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."keyed_codes" WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[KeyedCodeFilter]`
- Returns: `*PaginateResult[KeyedCode]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[KeyedCodeFilter]`
- Returns: `*Connection[KeyedCode]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamKeyedCodesInput`
- Returns: `iter.Seq2[*KeyedCode, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "code", "id", "name" FROM "public"."keyed_codes" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateKeyedCodeInput`
- Returns: `*KeyedCode`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."keyed_codes" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateKeyedCodeInput`
- Returns: `[]*KeyedCode`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."keyed_codes" (<columns>) VALUES <values> RETURNING "id"
```

### Upsert

- Params: `input *CreateKeyedCodeInput`, `target KeyedCodeConflictTarget`
- Returns: `*KeyedCode`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated KeyedCodeConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."keyed_codes" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateKeyedCodeInput`, `target KeyedCodeConflictTarget`
- Returns: `[]*KeyedCode`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same KeyedCodeConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."keyed_codes" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `id uuid.UUID`, `input *UpdateKeyedCodeInput`
- Returns: `*KeyedCode`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."keyed_codes" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdateKeyedCodeItem`
- Returns: `[]*KeyedCode`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."keyed_codes" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *KeyedCodeFilter`, `input *UpdateKeyedCodeInput`
- Returns: `[]*KeyedCode`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."keyed_codes" SET <set> WHERE <filter> RETURNING "id"
```

### HardDelete

- Params: `id uuid.UUID`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."keyed_codes" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []uuid.UUID`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."keyed_codes" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *KeyedCodeFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."keyed_codes" WHERE <filter> RETURNING "id"
```

### CreateWithRelated

- Params: `input *CreateKeyedCodeWithRelatedInput`
- Returns: `*KeyedCode`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Create and, per eligible relationship (Entries), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### UpdateWithRelated

- Params: `id uuid.UUID`, `input *UpdateKeyedCodeWithRelatedInput`
- Returns: `*KeyedCode`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrConstraintViolation`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Update and, per eligible relationship (Entries), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### UpsertWithRelated

- Params: `input *UpsertKeyedCodeWithRelatedInput`, `target KeyedCodeConflictTarget`
- Returns: `*KeyedCode`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Upsert and, per eligible relationship (Entries), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

## Filter

Type `KeyedCodeFilter`.

| Field | Type |
| --- | --- |
| Code | `*comparator.String` |
| ID | `*comparator.ID` |
| Name | `*comparator.String` |
| And | `[]*KeyedCodeFilter` |
| Or | `[]*KeyedCodeFilter` |

## Sort

Type `KeyedCodeSort`.

Fields: Code, ID, Name
