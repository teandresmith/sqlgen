# Zone

- **Table:** `zones` (schema `public`)
- **Kind:** table

## Files

- `models_gen.go`

## Primary key

- Kind: single
- `id` — ID `int64`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `int64` | `bigint` |  | yes |  |  | `comparator.Number[int64]` |  |
| `workspace_id` | WorkspaceID | `uuid.UUID` | `uuid` |  |  |  |  | `comparator.ID` |  |
| `region` | Region | `string` | `text` |  |  |  |  | `comparator.String` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `zones_pkey` | id | yes | btree |  |
| `zones_region_id_key` | region, id | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| plots | o2m | Plot | public.plots.(zone_region, zone_id) |  |
| sites | o2m | Site | public.sites.(zone_region, zone_id) |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*Zone`, `error`
- Errors: `ErrNotFound`, `tenancy.ErrMissing`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "id", "region", "workspace_id" FROM "public"."zones" WHERE "id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetZonesInput`
- Returns: `[]*Zone`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT "id", "region", "workspace_id" FROM "public"."zones" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."zones" WHERE "id" = $1)
```

### ExistsWhere

- Params: `filter *ZoneFilter`
- Returns: `bool`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."zones" WHERE <filter>)
```

### Count

- Params: `filter *ZoneFilter`
- Returns: `int64`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."zones" WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[ZoneFilter]`
- Returns: `*PaginateResult[Zone]`, `error`
- Errors: `tenancy.ErrMissing`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[ZoneFilter]`
- Returns: `*Connection[Zone]`, `error`
- Errors: `ErrInvalidCursor`, `tenancy.ErrMissing`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamZonesInput`
- Returns: `iter.Seq2[*Zone, error]`, `error`
- Errors: `tenancy.ErrMissing`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "id", "region", "workspace_id" FROM "public"."zones" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateZoneInput`
- Returns: `*Zone`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."zones" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateZoneInput`
- Returns: `[]*Zone`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."zones" (<columns>) VALUES <values> RETURNING "id"
```

### Update

- Params: `id int64`, `input *UpdateZoneInput`
- Returns: `*Zone`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."zones" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdateZoneItem`
- Returns: `[]*Zone`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."zones" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *ZoneFilter`, `input *UpdateZoneInput`
- Returns: `[]*Zone`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."zones" SET <set> WHERE <filter> RETURNING "id"
```

### Upsert

- Params: `input *CreateZoneInput`, `target ZoneConflictTarget`
- Returns: `*Zone`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: One method over the generated ZoneConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."zones" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateZoneInput`, `target ZoneConflictTarget`
- Returns: `[]*Zone`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Batched in generation.batch_size chunks over the same ZoneConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."zones" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### CreateWithRelated

- Params: `input *CreateZoneWithRelatedInput`
- Returns: `*Zone`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrAlreadyRelated`, `ErrNestedVerbConflict`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Create and, per eligible relationship (Plots, Sites), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### UpdateWithRelated

- Params: `id int64`, `input *UpdateZoneWithRelatedInput`
- Returns: `*Zone`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrAlreadyRelated`, `ErrNestedVerbConflict`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Update and, per eligible relationship (Plots, Sites), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### UpsertWithRelated

- Params: `input *UpsertZoneWithRelatedInput`, `target ZoneConflictTarget`
- Returns: `*Zone`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrAlreadyRelated`, `ErrNestedVerbConflict`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Upsert and, per eligible relationship (Plots, Sites), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### HardDelete

- Params: `id int64`
- Returns: `error`
- Errors: `tenancy.ErrMissing`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."zones" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Errors: `tenancy.ErrMissing`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."zones" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *ZoneFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`, `tenancy.ErrMissing`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."zones" WHERE <filter> RETURNING "id"
```

## Filter

Type `ZoneFilter`.

| Field | Type |
| --- | --- |
| ID | `*comparator.Number[int64]` |
| Region | `*comparator.String` |
| WorkspaceID | `*comparator.ID` |
| And | `[]*ZoneFilter` |
| Or | `[]*ZoneFilter` |

## Sort

Type `ZoneSort`.

Fields: ID, Region, WorkspaceID

## Read examples

```go
row, err := client.Zones().Get(ctx, 1)
rows, err := client.Zones().GetMany(ctx, &models.GetZonesInput{Filter: &models.ZoneFilter{Region: &comparator.String{Eq: new("example")}}})
```

## Write examples

```go
created, err := client.Zones().Create(ctx, &models.CreateZoneInput{Region: "example"})
updated, err := client.Zones().Update(ctx, 1, &models.UpdateZoneInput{WorkspaceID: omittable.Set(uuid.NewV4())})
```
