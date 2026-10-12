# ProjectMember

- **Table:** `project_members`
- **Kind:** table

## Files

- `models_gen.go`

## Primary key

- Kind: composite
- Struct: `ProjectMemberPK`
- `project_id` — ProjectID `int64`
- `account_id` — AccountID `int64`

## Columns

| Name | Go field | Go type | DB type | Null | PK | Unique | Default | Comparator | Comment |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `project_id` | ProjectID | `int64` | `integer` |  | yes |  |  | `comparator.Number[int64]` |  |
| `account_id` | AccountID | `int64` | `integer` |  | yes |  |  | `comparator.Number[int64]` |  |
| `role` | Role | `string` | `text` |  |  |  |  | `comparator.String` |  |

## Indexes

| Name | Columns | Unique | Method | Where |
| --- | --- | --- | --- | --- |
| `project_members_pkey` | project_id, account_id | yes | btree |  |

## Query methods

### Get

- Params: `pk ProjectMemberPK`
- Returns: `*ProjectMember`, `error`
- Errors: `ErrNotFound`
- Notes: Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.

Generated SQL:

sqlite:

```sql
SELECT "account_id", "project_id", "role" FROM "project_members" WHERE "project_id" = ? AND "account_id" = ? LIMIT 1
```

### GetMany

- Params: `input *GetProjectMembersInput`
- Returns: `[]*ProjectMember`, `error`

Generated SQL:

sqlite:

```sql
SELECT "account_id", "project_id", "role" FROM "project_members" WHERE <filter> ORDER BY <sort> LIMIT <limit>
```

### Count

- Params: `filter *ProjectMemberFilter`
- Returns: `int64`, `error`

Generated SQL:

sqlite:

```sql
SELECT COUNT(*) FROM "project_members" WHERE <filter>
```

### Exists

- Params: `pk ProjectMemberPK`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "project_members" WHERE "project_id" = ? AND "account_id" = ?)
```

### ExistsWhere

- Params: `filter *ProjectMemberFilter`
- Returns: `bool`, `error`

Generated SQL:

sqlite:

```sql
SELECT EXISTS(SELECT 1 FROM "project_members" WHERE <filter>)
```

### Paginate

- Params: `input PaginateInput[ProjectMemberFilter]`
- Returns: `*PaginateResult[ProjectMember]`, `error`
- Notes: Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Connection

- Params: `input ConnectionInput[ProjectMemberFilter]`
- Returns: `*Connection[ProjectMember]`, `error`
- Errors: `ErrInvalidCursor`
- Notes: Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.

### Stream

- Params: `input *StreamProjectMembersInput`
- Returns: `iter.Seq2[*ProjectMember, error]`, `error`
- Notes: Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.

Generated SQL:

sqlite:

```sql
SELECT "account_id", "project_id", "role" FROM "project_members" WHERE <filter> ORDER BY <sort>
```

## Mutation methods

### Create

- Params: `input *CreateProjectMemberInput`
- Returns: `*ProjectMember`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`

Generated SQL:

sqlite:

```sql
INSERT INTO "project_members" (<columns>) VALUES (<values>)
```

### CreateMany

- Params: `inputs []*CreateProjectMemberInput`
- Returns: `[]*ProjectMember`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "project_members" (<columns>) VALUES <values>
```

### Upsert

- Params: `input *CreateProjectMemberInput`, `target ProjectMemberConflictTarget`
- Returns: `*ProjectMember`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: One method over the generated ProjectMemberConflictTarget enum — the target argument selects the conflict columns at call time.

Generated SQL:

sqlite:

```sql
INSERT INTO "project_members" (<columns>) VALUES (<values>) ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### UpsertMany

- Params: `inputs []*CreateProjectMemberInput`, `target ProjectMemberConflictTarget`
- Returns: `[]*ProjectMember`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Batched in generation.batch_size chunks over the same ProjectMemberConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.

Generated SQL:

sqlite:

```sql
INSERT INTO "project_members" (<columns>) VALUES <values> ON CONFLICT (<conflict_target>) DO UPDATE SET <excluded>
```

### Update

- Params: `pk ProjectMemberPK`, `input *UpdateProjectMemberInput`
- Returns: `*ProjectMember`, `error`
- Errors: `ErrNotFound`, `ErrNilInput`, `ErrConstraintViolation`
- Notes: Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.

Generated SQL:

sqlite:

```sql
UPDATE "project_members" SET <set> WHERE "project_id" = ? AND "account_id" = ?
```

### UpdateMany

- Params: `items []UpdateProjectMemberItem`
- Returns: `[]*ProjectMember`, `error`
- Errors: `ErrNilInput`, `ErrConstraintViolation`
- Notes: Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.

Generated SQL:

sqlite:

```sql
UPDATE "project_members" SET <set> WHERE "project_id" = ? AND "account_id" = ?
```

### UpdateWhere

- Params: `filter *ProjectMemberFilter`, `input *UpdateProjectMemberInput`
- Returns: `[]*ProjectMember`, `error`
- Errors: `ErrNilInput`, `ErrEmptyFilter`, `ErrConstraintViolation`
- Notes: Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
UPDATE "project_members" SET <set> WHERE <filter> RETURNING "project_id", "account_id"
```

### HardDelete

- Params: `pk ProjectMemberPK`
- Returns: `error`
- Notes: Idempotent — a primary key that does not exist is not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "project_members" WHERE "project_id" = ? AND "account_id" = ?
```

### HardDeleteMany

- Params: `pks []ProjectMemberPK`
- Returns: `error`
- Notes: Idempotent — primary keys that do not exist are not an error.

Generated SQL:

sqlite:

```sql
DELETE FROM "project_members" WHERE ("project_id", "account_id") IN (<pks>)
```

### HardDeleteWhere

- Params: `filter *ProjectMemberFilter`
- Returns: `error`
- Errors: `ErrEmptyFilter`
- Notes: Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.

Generated SQL:

sqlite:

```sql
DELETE FROM "project_members" WHERE <filter> RETURNING "project_id", "account_id"
```

## Filter

Type `ProjectMemberFilter`.

| Field | Type |
| --- | --- |
| AccountID | `*comparator.Number[int64]` |
| ProjectID | `*comparator.Number[int64]` |
| Role | `*comparator.String` |
| And | `[]*ProjectMemberFilter` |
| Or | `[]*ProjectMemberFilter` |

## Sort

Type `ProjectMemberSort`.

Fields: AccountID, ProjectID, Role
