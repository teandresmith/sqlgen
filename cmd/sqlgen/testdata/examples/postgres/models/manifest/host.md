# Host

- **Table:** `hosts` (schema `public`)
- **Kind:** table

## Files

- `models_gen.go`

## Primary key

- Kind: single
- `addr` — Addr `net.IP`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `addr` | Addr | `net.IP` | `inet` |  | yes |  |  | `comparator.Opaque[net.IP]` |  |
| `label` | Label | `string` | `text` |  |  |  |  | `comparator.String` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `hosts_pkey` | addr | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| fingerprints | m2m | Fingerprint | public.host_fingerprints (host_addr → fingerprint_digest) |  |

## Query methods

### Get

- Params: `id net.IP`
- Returns: `*Host`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "addr", "label" FROM "public"."hosts" WHERE "addr" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetHostsInput`
- Returns: `[]*Host`, `error`

Generated SQL:

postgres:

```sql
SELECT "addr", "label" FROM "public"."hosts" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *HostFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."hosts" WHERE <filter>
```

### Exists

- Params: `id net.IP`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."hosts" WHERE "addr" = $1)
```

### ExistsWhere

- Params: `filter *HostFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."hosts" WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[HostFilter]`
- Returns: `*PaginateResult[Host]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[HostFilter]`
- Returns: `*Connection[Host]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamHostsInput`
- Returns: `iter.Seq2[*Host, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "addr", "label" FROM "public"."hosts" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateHostInput`
- Returns: `*Host`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."hosts" (<columns>) VALUES (<values>) RETURNING "addr"
```

### CreateMany

- Params: `inputs []*CreateHostInput`
- Returns: `[]*Host`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."hosts" (<columns>) VALUES <values> RETURNING "addr"
```

### Upsert

- Params: `input *CreateHostInput`, `target HostConflictTarget`
- Returns: `*Host`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated HostConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."hosts" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "addr"
```

### UpsertMany

- Params: `inputs []*CreateHostInput`, `target HostConflictTarget`
- Returns: `[]*Host`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same HostConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."hosts" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `id net.IP`, `input *UpdateHostInput`
- Returns: `*Host`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."hosts" SET <set> WHERE "addr" = $1
```

### UpdateMany

- Params: `items []UpdateHostItem`
- Returns: `[]*Host`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."hosts" SET <set> WHERE "addr" = $1
```

### UpdateWhere

- Params: `filter *HostFilter`, `input *UpdateHostInput`
- Returns: `[]*Host`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."hosts" SET <set> WHERE <filter> RETURNING "addr"
```

### HardDelete

- Params: `id net.IP`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."hosts" WHERE "addr" = $1
```

### HardDeleteMany

- Params: `ids []net.IP`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."hosts" WHERE "addr" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *HostFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."hosts" WHERE <filter> RETURNING "addr"
```

## Filter

Type `HostFilter`.

| Field | Type |
| --- | --- |
| Addr | `*comparator.Opaque[net.IP]` |
| Label | `*comparator.String` |
| And | `[]*HostFilter` |
| Or | `[]*HostFilter` |

## Sort

Type `HostSort`.

Fields: Addr, Label
