# Site

- **Table:** `sites` (schema `public`)
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
| `zone_region` | ZoneRegion | `*string` | `text` | yes |  |  |  | `comparator.NullableString` |  |
| `zone_id` | ZoneID | `*int64` | `bigint` | yes |  |  |  | `comparator.NullableNumber[int64]` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `sites_pkey` | id | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| zone | o2o | Zone | public.sites.(zone_region, zone_id) |  |

## Query methods

### Get

- Params: `id int64`
- Returns: `*Site`, `error`
- Errors: `ErrNotFound`, `tenancy.ErrMissing`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "id", "workspace_id", "zone_id", "zone_region" FROM "public"."sites" WHERE "id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetSitesInput`
- Returns: `[]*Site`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT "id", "workspace_id", "zone_id", "zone_region" FROM "public"."sites" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Exists

- Params: `id int64`
- Returns: `bool`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."sites" WHERE "id" = $1)
```

### ExistsWhere

- Params: `filter *SiteFilter`
- Returns: `bool`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."sites" WHERE <filter>)
```

### Count

- Params: `filter *SiteFilter`
- Returns: `int64`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."sites" WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[SiteFilter]`
- Returns: `*PaginateResult[Site]`, `error`
- Errors: `tenancy.ErrMissing`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[SiteFilter]`
- Returns: `*Connection[Site]`, `error`
- Errors: `ErrInvalidCursor`, `tenancy.ErrMissing`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamSitesInput`
- Returns: `iter.Seq2[*Site, error]`, `error`
- Errors: `tenancy.ErrMissing`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "id", "workspace_id", "zone_id", "zone_region" FROM "public"."sites" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateSiteInput`
- Returns: `*Site`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."sites" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateSiteInput`
- Returns: `[]*Site`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."sites" (<columns>) VALUES <values> RETURNING "id"
```

### Update

- Params: `id int64`, `input *UpdateSiteInput`
- Returns: `*Site`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."sites" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdateSiteItem`
- Returns: `[]*Site`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."sites" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *SiteFilter`, `input *UpdateSiteInput`
- Returns: `[]*Site`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."sites" SET <set> WHERE <filter> RETURNING "id"
```

### Upsert

- Params: `input *CreateSiteInput`, `target SiteConflictTarget`
- Returns: `*Site`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: One method over the generated SiteConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."sites" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateSiteInput`, `target SiteConflictTarget`
- Returns: `[]*Site`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Batched in generation.batch_size chunks over the same SiteConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."sites" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### HardDelete

- Params: `id int64`
- Returns: `error`
- Errors: `tenancy.ErrMissing`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."sites" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []int64`
- Returns: `error`
- Errors: `tenancy.ErrMissing`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."sites" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *SiteFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`, `tenancy.ErrMissing`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."sites" WHERE <filter> RETURNING "id"
```

## Filter

Type `SiteFilter`.

| Field | Type |
| --- | --- |
| ID | `*comparator.Number[int64]` |
| WorkspaceID | `*comparator.ID` |
| ZoneID | `*comparator.NullableNumber[int64]` |
| ZoneRegion | `*comparator.NullableString` |
| And | `[]*SiteFilter` |
| Or | `[]*SiteFilter` |

## Sort

Type `SiteSort`.

Fields: ID, WorkspaceID, ZoneID, ZoneRegion

## Read examples

```go
row, err := client.Sites().Get(ctx, 1)
rows, err := client.Sites().GetMany(ctx, &models.GetSitesInput{Filter: &models.SiteFilter{WorkspaceID: &comparator.ID{Eq: new("123e4567-e89b-12d3-a456-426614174000")}}})
```

## Write examples

```go
created, err := client.Sites().Create(ctx, &models.CreateSiteInput{})
updated, err := client.Sites().Update(ctx, 1, &models.UpdateSiteInput{WorkspaceID: omittable.Set(uuid.NewV4())})
```
