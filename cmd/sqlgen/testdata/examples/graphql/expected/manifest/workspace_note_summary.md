# WorkspaceNoteSummary

- **Table:** `workspace_note_summary` (schema `public`)
- **Kind:** view
- **Source:** `-- workspace_note_summary — the view proof fixture: a TENANTED view reached
-- over the real gqlgen server. It projects `workspace_id`, so §29.2.5's
-- detection rule scopes every one of its reads with no
-- `views.workspace_note_summary.tenancy` block; `@pk` makes the by-PK query
-- available, and `id` satisfies the inherited `cursor_keys` default so the
-- connection query is emitted too. All three read queries therefore exist,
-- which is what lets tests/view_tenancy_test.go prove isolation across the
-- whole read surface rather than one query of it.
--
-- `kind` (ENUM) and `labels` (JSONB) are projected so <V>Filter carries the
-- enum and JSONB comparator families — PRD §26.4 promises a view's filter is
-- the table's read half, and nothing else in the example tree tests that
-- claim on a view.
--
-- The LEFT JOIN onto documents is what makes this a real view rather than a
-- passthrough: `document_count` is an aggregate, so the object type carries a
-- column with no base table behind it. `documents` is the schema's polymorphic
-- child table — `entity_id` carries no FK precisely so several parents can
-- share it — and `spv` is the one member of document_entity_type_enum that is
-- NOT asset-scoped, so notes claim it without disturbing the §13.7
-- sub-categorized-polymorphism fixture the `asset.*` members serve.
--
-- @pk: id
-- @type document_count: int32

CREATE VIEW workspace_note_summary AS
SELECT
    n.id,
    n.workspace_id,
    n.kind,
    n.labels,
    n.body,
    n.pinned_order,
    COUNT(d.id) AS document_count
FROM workspace_notes n
LEFT JOIN documents d
       ON d.entity_id = n.id AND d.entity_type = 'spv'
GROUP BY n.id, n.workspace_id, n.kind, n.labels, n.body, n.pinned_order;
`

## Files

- `views_gen.go`

## Primary key

- Kind: single
- `id` — ID `uuid.UUID`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `id` | ID | `uuid.UUID` | `uuid` |  | yes |  |  | `comparator.ID` |  |
| `workspace_id` | WorkspaceID | `uuid.UUID` | `uuid` |  |  |  |  | `comparator.ID` |  |
| `kind` | Kind | `WorkspaceNoteKindEnum` | `workspace_note_kind_enum` |  |  |  |  | `comparator.Enum[WorkspaceNoteKindEnum]` |  |
| `labels` | Labels | `types.JSON` | `jsonb` |  |  |  |  | `comparator.JSONB` |  |
| `body` | Body | `string` | `text` |  |  |  |  | `comparator.String` |  |
| `pinned_order` | PinnedOrder | `int32` | `integer` |  |  |  |  | `comparator.Number[int32]` |  |
| `document_count` | DocumentCount | `int32` | `int32` |  |  |  |  | `comparator.Number[int32]` |  |

## Query methods

### Get

- Params: `id uuid.UUID`
- Returns: `*WorkspaceNoteSummary`, `error`
- Errors: `ErrNotFound`, `tenancy.ErrMissing`
- Notes: Generated from the view's @pk annotation.

Generated SQL:

postgres:

```sql
SELECT "body", "document_count", "id", "kind", "labels", "pinned_order", "workspace_id" FROM "public"."workspace_note_summary" WHERE "id" = $1 LIMIT 1
```

### GetMany

- Params: `input *GetWorkspaceNoteSummariesInput`
- Returns: `[]*WorkspaceNoteSummary`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT "body", "document_count", "id", "kind", "labels", "pinned_order", "workspace_id" FROM "public"."workspace_note_summary" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *WorkspaceNoteSummaryFilter`
- Returns: `int64`, `error`
- Errors: `tenancy.ErrMissing`

Generated SQL:

postgres:

```sql
SELECT COUNT(*) FROM "public"."workspace_note_summary" WHERE <filter>
```

### Paginate

- Params: `input PaginateInput[WorkspaceNoteSummaryFilter]`
- Returns: `*PaginateResult[WorkspaceNoteSummary]`, `error`
- Errors: `tenancy.ErrMissing`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[WorkspaceNoteSummaryFilter]`
- Returns: `*Connection[WorkspaceNoteSummary]`, `error`
- Errors: `ErrInvalidCursor`, `tenancy.ErrMissing`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

## Filter

Type `WorkspaceNoteSummaryFilter`.

| Field | Type |
| --- | --- |
| Body | `*comparator.String` |
| DocumentCount | `*comparator.Number[int32]` |
| ID | `*comparator.ID` |
| Kind | `*comparator.Enum[WorkspaceNoteKindEnum]` |
| Labels | `*comparator.JSONB` |
| PinnedOrder | `*comparator.Number[int32]` |
| WorkspaceID | `*comparator.ID` |
| And | `[]*WorkspaceNoteSummaryFilter` |
| Or | `[]*WorkspaceNoteSummaryFilter` |

## Sort

Type `WorkspaceNoteSummarySort`.

Fields: Body, DocumentCount, ID, Kind, Labels, PinnedOrder, WorkspaceID
