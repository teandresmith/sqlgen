# WorkspaceNote

- **Table:** `workspace_notes` (schema `public`)
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
| `workspace_id` | WorkspaceID | `uuid.UUID` | `uuid` |  |  |  |  | `comparator.ID` |  |
| `parent_id` | ParentID | `*uuid.UUID` | `uuid` | yes |  |  |  | `comparator.NullableID` |  |
| `body` | Body | `string` | `text` |  |  |  |  | `comparator.String` |  |
| `pinned_order` | PinnedOrder | `int32` | `integer` |  |  |  | `0` | `comparator.Number[int32]` |  |
| `kind` | Kind | `WorkspaceNoteKindEnum` | `workspace_note_kind_enum` |  |  |  | `'draft'` | `comparator.Enum[WorkspaceNoteKindEnum]` |  |
| `labels` | Labels | `types.JSON` | `jsonb` |  |  |  | `'{}'` | `comparator.JSONB` |  |
| `created_at` | CreatedAt | `time.Time` | `timestamptz` |  |  |  | `current_timestamp` | `comparator.Time` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `workspace_notes_pkey` | id | yes | btree |  |

## Relationships

| Name | Kind | Target | FK | Filter |
| --- | --- | --- | --- | --- |
| Children | o2m | WorkspaceNote | public.workspace_notes.parent_id |  |
| DraftChildren | o2m | WorkspaceNote | public.workspace_notes.parent_id | `kind = 'draft'` |

## Query methods

### Get

- Params: `id uuid.UUID`
- Returns: `*WorkspaceNote`, `error`
- Errors: `ErrNotFound`, `tenancy.ErrMissing`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

postgres:

```sql
SELECT "body", "created_at", "id", "kind", "labels", "parent_id", "pinned_order", "workspace_id" FROM "public"."workspace_notes" WHERE "id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetWorkspaceNotesInput`
- Returns: `[]*WorkspaceNote`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT "body", "created_at", "id", "kind", "labels", "parent_id", "pinned_order", "workspace_id" FROM "public"."workspace_notes" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *WorkspaceNoteFilter`
- Returns: `int64`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."workspace_notes" WHERE <filter>
```

### Exists

- Params: `id uuid.UUID`
- Returns: `bool`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."workspace_notes" WHERE "id" = $1)
```

### ExistsWhere

- Params: `filter *WorkspaceNoteFilter`
- Returns: `bool`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT EXISTS(SELECT 1 FROM "public"."workspace_notes" WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[WorkspaceNoteFilter]`
- Returns: `*PaginateResult[WorkspaceNote]`, `error`
- Errors: `tenancy.ErrMissing`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[WorkspaceNoteFilter]`
- Returns: `*Connection[WorkspaceNote]`, `error`
- Errors: `ErrInvalidCursor`, `tenancy.ErrMissing`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamWorkspaceNotesInput`
- Returns: `iter.Seq2[*WorkspaceNote, error]`, `error`
- Errors: `tenancy.ErrMissing`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

postgres:

```sql
SELECT "body", "created_at", "id", "kind", "labels", "parent_id", "pinned_order", "workspace_id" FROM "public"."workspace_notes" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateWorkspaceNoteInput`
- Returns: `*WorkspaceNote`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`

Generated SQL:

postgres:

```sql
INSERT INTO "public"."workspace_notes" (<columns>) VALUES (<values>) RETURNING "id"
```

### CreateMany

- Params: `inputs []*CreateWorkspaceNoteInput`
- Returns: `[]*WorkspaceNote`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."workspace_notes" (<columns>) VALUES <values> RETURNING "id"
```

### Upsert

- Params: `input *CreateWorkspaceNoteInput`, `target WorkspaceNoteConflictTarget`
- Returns: `*WorkspaceNote`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: One method over the generated WorkspaceNoteConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."workspace_notes" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded> RETURNING "id"
```

### UpsertMany

- Params: `inputs []*CreateWorkspaceNoteInput`, `target WorkspaceNoteConflictTarget`
- Returns: `[]*WorkspaceNote`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Batched in generation.batch_size chunks over the same WorkspaceNoteConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

postgres:

```sql
INSERT INTO "public"."workspace_notes" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `id uuid.UUID`, `input *UpdateWorkspaceNoteInput`
- Returns: `*WorkspaceNote`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

postgres:

```sql
UPDATE "public"."workspace_notes" SET <set> WHERE "id" = $1
```

### UpdateMany

- Params: `items []UpdateWorkspaceNoteItem`
- Returns: `[]*WorkspaceNote`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

postgres:

```sql
UPDATE "public"."workspace_notes" SET <set> WHERE "id" = $1
```

### UpdateWhere

- Params: `filter *WorkspaceNoteFilter`, `input *UpdateWorkspaceNoteInput`
- Returns: `[]*WorkspaceNote`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
UPDATE "public"."workspace_notes" SET <set> WHERE <filter> RETURNING "id"
```

### Increment

- Params: `id uuid.UUID`, `input IncrementInput[WorkspaceNoteIncrementColumn]`
- Returns: `error`
- Errors: `ErrNotFound`, `tenancy.ErrMissing`
- Notes: Atomic single-column arithmetic. The column is restricted to the generated WorkspaceNoteIncrementColumn enum — numeric, and identifying nothing: neither PK, FK, nor the tenant column. A negative amount decrements.

Generated SQL:

postgres:

```sql
UPDATE "public"."workspace_notes" SET <column> = <column> + $1 WHERE "id" = $2
```

### HardDelete

- Params: `id uuid.UUID`
- Returns: `error`
- Errors: `tenancy.ErrMissing`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."workspace_notes" WHERE "id" = $1
```

### HardDeleteMany

- Params: `ids []uuid.UUID`
- Returns: `error`
- Errors: `tenancy.ErrMissing`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."workspace_notes" WHERE "id" IN (<ids>)
```

### HardDeleteWhere

- Params: `filter *WorkspaceNoteFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`, `tenancy.ErrMissing`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

postgres:

```sql
DELETE FROM "public"."workspace_notes" WHERE <filter> RETURNING "id"
```

### CreateWithRelated

- Params: `input *CreateWorkspaceNoteWithRelatedInput`
- Returns: `*WorkspaceNote`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrAlreadyRelated`, `ErrNestedVerbConflict`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Create and, per eligible relationship (Children, DraftChildren), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### UpdateWithRelated

- Params: `id uuid.UUID`, `input *UpdateWorkspaceNoteWithRelatedInput`
- Returns: `*WorkspaceNote`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrAlreadyRelated`, `ErrNestedVerbConflict`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Update and, per eligible relationship (Children, DraftChildren), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

### UpsertWithRelated

- Params: `input *UpsertWorkspaceNoteWithRelatedInput`, `target WorkspaceNoteConflictTarget`
- Returns: `*WorkspaceNote`, `error`
- Errors: `ErrNilInput`, `ErrNotFound`, `ErrAlreadyRelated`, `ErrNestedVerbConflict`, `ErrConstraintViolation`, `tenancy.ErrMissing`, `tenancy.ErrMismatch`
- Notes: Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. Issues no statement of its own: it composes Upsert and, per eligible relationship (Children, DraftChildren), the target's and junction's own client methods, so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none.

## Filter

Type `WorkspaceNoteFilter`.

| Field | Type |
| --- | --- |
| Body | `*comparator.String` |
| CreatedAt | `*comparator.Time` |
| ID | `*comparator.ID` |
| Kind | `*comparator.Enum[WorkspaceNoteKindEnum]` |
| Labels | `*comparator.JSONB` |
| ParentID | `*comparator.NullableID` |
| PinnedOrder | `*comparator.Number[int32]` |
| WorkspaceID | `*comparator.ID` |
| And | `[]*WorkspaceNoteFilter` |
| Or | `[]*WorkspaceNoteFilter` |

## Sort

Type `WorkspaceNoteSort`.

Fields: Body, CreatedAt, ID, Kind, Labels, ParentID, PinnedOrder, WorkspaceID
