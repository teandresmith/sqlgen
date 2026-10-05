# ScalarProbe

- **Table:** `scalar_probes` (schema `public`)
- **Kind:** table

## Files

- `models_gen.go`

## Primary key

- Kind: single
- `id` — ID `uuid.UUID`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `uuid.UUID` | `uuid` |  | yes |  | `gen_random_uuid()` | `comparator.ID` |  |
| `label` | Label | `string` | `text` |  |  |  |  | `comparator.String` |  |
| `payload` | Payload | `[]byte` | `bytea` |  |  |  |  | `comparator.Opaque[[]byte]` |  |
| `signature` | Signature | `[]byte` | `bytea` | yes |  |  |  | `comparator.NullableOpaque[[]byte]` |  |
| `dur` | Dur | `time.Duration` | `interval` |  |  |  |  | `comparator.Number[time.Duration]` |  |
| `dur_n` | DurN | `*time.Duration` | `interval` | yes |  |  |  | `comparator.NullableNumber[time.Duration]` |  |
| `addr` | Addr | `net.IP` | `inet` |  |  |  |  | `comparator.Opaque[net.IP]` |  |
| `addr_n` | AddrN | `net.IP` | `inet` | yes |  |  |  | `comparator.NullableOpaque[net.IP]` |  |
| `net_block` | NetBlock | `net.IPNet` | `cidr` |  |  |  |  | `comparator.Opaque[net.IPNet]` |  |
| `net_block_n` | NetBlockN | `*net.IPNet` | `cidr` | yes |  |  |  | `comparator.NullableOpaque[net.IPNet]` |  |
| `mac` | MAC | `net.HardwareAddr` | `macaddr` |  |  |  |  | `comparator.Opaque[net.HardwareAddr]` |  |
| `mac_n` | MACN | `net.HardwareAddr` | `macaddr` | yes |  |  |  | `comparator.NullableOpaque[net.HardwareAddr]` |  |
| `note` | Note | `sql.NullString` | `text` | yes |  |  |  | `comparator.NullableString` |  |
| `flag` | Flag | `sql.NullBool` | `boolean` | yes |  |  |  | `comparator.NullableBool` |  |
| `small_count` | SmallCount | `sql.NullInt16` | `smallint` | yes |  |  |  | `comparator.NullableNumber[int16]` |  |
| `mid_count` | MidCount | `sql.NullInt32` | `integer` | yes |  |  |  | `comparator.NullableNumber[int32]` |  |
| `big_count` | BigCount | `sql.NullInt64` | `bigint` | yes |  |  |  | `comparator.NullableNumber[int64]` |  |
| `ratio` | Ratio | `sql.NullFloat64` | `double precision` | yes |  |  |  | `comparator.NullableNumber[float64]` |  |
| `observed_at` | ObservedAt | `sql.NullTime` | `timestamptz` | yes |  |  |  | `comparator.NullableTime` |  |
| `created_at` | CreatedAt | `time.Time` | `timestamptz` |  |  |  | `current_timestamp` | `comparator.Time` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `scalar_probes_pkey` | id | yes | btree |  |

## Query methods

### Get

- Params: `id uuid.UUID`
- Returns: `*ScalarProbe`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "addr", "addr_n", "big_count", "created_at", "dur", "dur_n", "flag", "id", "label", "mac", "mac_n", "mid_count", "net_block", "net_block_n", "note", "observed_at", "payload", "ratio", "signature", "small_count" FROM "public"."scalar_probes" WHERE "id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetScalarProbesInput`
- Returns: `[]*ScalarProbe`, `error`

Generated SQL:

postgres:

```sql
SELECT "addr", "addr_n", "big_count", "created_at", "dur", "dur_n", "flag", "id", "label", "mac", "mac_n", "mid_count", "net_block", "net_block_n", "note", "observed_at", "payload", "ratio", "signature", "small_count" FROM "public"."scalar_probes" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *ScalarProbeFilter`
- Returns: `int64`, `error`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."scalar_probes" WHERE <filter>
```

### Exists

- Params: `id uuid.UUID`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."scalar_probes" WHERE "id" = $1)
```

### ExistsWhere

- Params: `filter *ScalarProbeFilter`
- Returns: `bool`, `error`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."scalar_probes" WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[ScalarProbeFilter]`
- Returns: `*PaginateResult[ScalarProbe]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[ScalarProbeFilter]`
- Returns: `*Connection[ScalarProbe]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamScalarProbesInput`
- Returns: `iter.Seq2[*ScalarProbe, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "addr", "addr_n", "big_count", "created_at", "dur", "dur_n", "flag", "id", "label", "mac", "mac_n", "mid_count", "net_block", "net_block_n", "note", "observed_at", "payload", "ratio", "signature", "small_count" FROM "public"."scalar_probes" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateScalarProbeInput`
- Returns: `*ScalarProbe`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."scalar_probes" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateScalarProbeInput`
- Returns: `[]*ScalarProbe`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."scalar_probes" (<columns>) VALUES <values> RETURNING "id"
```

### Upsert

- Params: `input *CreateScalarProbeInput`, `target ScalarProbeConflictTarget`
- Returns: `*ScalarProbe`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated ScalarProbeConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."scalar_probes" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateScalarProbeInput`, `target ScalarProbeConflictTarget`
- Returns: `[]*ScalarProbe`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same ScalarProbeConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."scalar_probes" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `id uuid.UUID`, `input *UpdateScalarProbeInput`
- Returns: `*ScalarProbe`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."scalar_probes" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdateScalarProbeItem`
- Returns: `[]*ScalarProbe`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."scalar_probes" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *ScalarProbeFilter`, `input *UpdateScalarProbeInput`
- Returns: `[]*ScalarProbe`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."scalar_probes" SET <set> WHERE <filter> RETURNING "id"
```

### Increment

- Params: `id uuid.UUID`, `input IncrementInput[ScalarProbeIncrementColumn]`
- Returns: `error`
- Errors: `ErrNotFound`
- Notes: Atomic single-column arithmetic. The column is restricted to the generated ScalarProbeIncrementColumn enum — numeric, and identifying nothing: neither PK, FK, nor the tenant column. A negative amount decrements.

Generated SQL:

postgres:

```sql
UPDATE "public"."scalar_probes" SET <column> = <column> + $1 WHERE "id" = $2
```

### HardDelete

- Params: `id uuid.UUID`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."scalar_probes" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []uuid.UUID`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."scalar_probes" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *ScalarProbeFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."scalar_probes" WHERE <filter> RETURNING "id"
```

## Filter

Type `ScalarProbeFilter`.

| Field | Type |
| --- | --- |
| Addr | `*comparator.Opaque[net.IP]` |
| AddrN | `*comparator.NullableOpaque[net.IP]` |
| BigCount | `*comparator.NullableNumber[int64]` |
| CreatedAt | `*comparator.Time` |
| Dur | `*comparator.Number[time.Duration]` |
| DurN | `*comparator.NullableNumber[time.Duration]` |
| Flag | `*comparator.NullableBool` |
| ID | `*comparator.ID` |
| Label | `*comparator.String` |
| MAC | `*comparator.Opaque[net.HardwareAddr]` |
| MACN | `*comparator.NullableOpaque[net.HardwareAddr]` |
| MidCount | `*comparator.NullableNumber[int32]` |
| NetBlock | `*comparator.Opaque[net.IPNet]` |
| NetBlockN | `*comparator.NullableOpaque[net.IPNet]` |
| Note | `*comparator.NullableString` |
| ObservedAt | `*comparator.NullableTime` |
| Payload | `*comparator.Opaque[[]byte]` |
| Ratio | `*comparator.NullableNumber[float64]` |
| Signature | `*comparator.NullableOpaque[[]byte]` |
| SmallCount | `*comparator.NullableNumber[int16]` |
| And | `[]*ScalarProbeFilter` |
| Or | `[]*ScalarProbeFilter` |

## Sort

Type `ScalarProbeSort`.

Fields: Addr, AddrN, BigCount, CreatedAt, Dur, DurN, Flag, ID, Label, MAC, MACN, MidCount, NetBlock, NetBlockN, Note, ObservedAt, Payload, Ratio, Signature, SmallCount
