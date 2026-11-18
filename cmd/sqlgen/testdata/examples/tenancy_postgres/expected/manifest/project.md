# Project

- **Table:** `projects` (schema `public`)
- **Kind:** table

## Files

- `models_gen.go`

## Primary key

- Kind: composite
- Struct: `ProjectPK`
- `workspace_id` — WorkspaceID `uuid.UUID`
- `code` — Code `string`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `workspace_id` | WorkspaceID | `uuid.UUID` | `uuid` |  | yes |  |  | `comparator.ID` |  |
| `code` | Code | `string` | `text` |  | yes |  |  | `comparator.ID` |  |
| `name` | Name | `string` | `text` |  |  |  |  | `comparator.String` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `projects_pkey` | workspace_id, code | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| tasks | o2m | Task | public.tasks.(workspace_id, project_code) |  |

## Query methods

### Get

- Params: `pk ProjectPK`
- Returns: `*Project`, `error`
- Errors: `ErrNotFound`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "code", "name", "workspace_id" FROM "public"."projects" WHERE "workspace_id" = $1 AND "code" = $2 LIMIT 1
```

### GetMany

- Params: `input *GetProjectsInput`
- Returns: `[]*Project`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT "code", "name", "workspace_id" FROM "public"."projects" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Exists

- Params: `pk ProjectPK`
- Returns: `bool`, `error`
- Errors: `tenancy.ErrMissing`, `tenancy.ErrMismatch`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."projects" WHERE "workspace_id" = $1 AND "code" = $2)
```

### ExistsWhere

- Params: `filter *ProjectFilter`
- Returns: `bool`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."projects" WHERE <filter>)
```

### Count

- Params: `filter *ProjectFilter`
- Returns: `int64`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."projects" WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[ProjectFilter]`
- Returns: `*PaginateResult[Project]`, `error`
- Errors: `tenancy.ErrMissing`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[ProjectFilter]`
- Returns: `*Connection[Project]`, `error`
- Errors: `ErrInvalidCursor`, `tenancy.ErrMissing`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamProjectsInput`
- Returns: `iter.Seq2[*Project, error]`, `error`
- Errors: `tenancy.ErrMissing`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "code", "name", "workspace_id" FROM "public"."projects" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateProjectInput`
- Returns: `*Project`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."projects" (<columns>) VALUES (<values>)
```

### CreateMany

- Params: `inputs []*CreateProjectInput`
- Returns: `[]*Project`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."projects" (<columns>) VALUES <values>
```

### Update

- Params: `pk ProjectPK`, `input *UpdateProjectInput`
- Returns: `*Project`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."projects" SET <set> WHERE "workspace_id" = $1 AND "code" = $2
```

### UpdateMany

- Params: `items []UpdateProjectItem`
- Returns: `[]*Project`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."projects" SET <set> WHERE "workspace_id" = $1 AND "code" = $2
```

### UpdateWhere

- Params: `filter *ProjectFilter`, `input *UpdateProjectInput`
- Returns: `[]*Project`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`, `tenancy.ErrMissing`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."projects" SET <set> WHERE <filter> RETURNING "workspace_id", "code"
```

### Upsert

- Params: `input *CreateProjectInput`, `target ProjectConflictTarget`
- Returns: `*Project`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: One method over the generated ProjectConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."projects" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### UpsertMany

- Params: `inputs []*CreateProjectInput`, `target ProjectConflictTarget`
- Returns: `[]*Project`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Batched in generation.batch_size chunks over the same ProjectConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."projects" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### HardDelete

- Params: `pk ProjectPK`
- Returns: `error`
- Errors: `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."projects" WHERE "workspace_id" = $1 AND "code" = $2
```

### HardDeleteMany

- Params: `pks []ProjectPK`
- Returns: `error`
- Errors: `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."projects" WHERE ("workspace_id", "code") IN (<pks>)
```

### HardDeleteWhere

- Params: `filter *ProjectFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`, `tenancy.ErrMissing`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."projects" WHERE <filter> RETURNING "workspace_id", "code"
```

## Filter

Type `ProjectFilter`.

| Field | Type |
| --- | --- |
| Code | `*comparator.ID` |
| Name | `*comparator.String` |
| WorkspaceID | `*comparator.ID` |
| And | `[]*ProjectFilter` |
| Or | `[]*ProjectFilter` |

## Sort

Type `ProjectSort`.

Fields: Code, Name, WorkspaceID

## Read examples

```go
row, err := client.Projects().Get(ctx, models.ProjectPK{WorkspaceID: uuid.NewV4(), Code: "example"})
rows, err := client.Projects().GetMany(ctx, &models.GetProjectsInput{Filter: &models.ProjectFilter{Name: &comparator.String{Eq: new("example")}}})
```

## Write examples

```go
created, err := client.Projects().Create(ctx, &models.CreateProjectInput{WorkspaceID: uuid.NewV4(), Code: "example", Name: "example"})
updated, err := client.Projects().Update(ctx, models.ProjectPK{WorkspaceID: uuid.NewV4(), Code: "example"}, &models.UpdateProjectInput{Name: omittable.Set("example")})
```
