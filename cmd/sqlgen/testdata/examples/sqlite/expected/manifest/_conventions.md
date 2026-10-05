# models conventions

Package-wide reference. Per-entity files cross-reference this document rather than restating it.

## Client entry points

- Entity client: `client.<Entity>()` — one accessor per entity, carrying reads and writes alike. Tables use the plural struct name (`client.Users()`), views the singular one (`client.ActiveUser()`).
- `Get` returns nil on a missing row: 

## Error sentinels

| Name | GraphQL code | Package |
| --- | --- | --- |
| `ErrNotFound` | `NOT_FOUND` | models |
| `ErrEmptyFilter` | — | models |
| `ErrNilInput` | — | models |
| `ErrAmbiguousFilter` | — | models |
| `ErrInvalidCursor` | — | models |
| `ErrDeadlock` | — | models |
| `ErrConnectionFailed` | — | models |
| `ErrConstraintViolation` | — | models |
| `ErrAlreadyRelated` | `CONFLICT` | models |
| `ErrNestedVerbConflict` | `INVALID_INPUT` | models |

## Constraint error codes

Constraint failures arrive as one `*ConstraintError` (unwrapping to
`ErrConstraintViolation`); its `Type` selects the GraphQL code:

| ConstraintError.Type | GraphQL code |
| --- | --- |
| `check` | `INVALID_INPUT` |
| `foreign_key` | `BAD_REFERENCE` |
| `not_null` | `INVALID_INPUT` |
| `unique` | `CONFLICT` |

## Pagination

- Page type: `Page`
- List envelope suffix: `List`
- Cursor encoding: opaque-base64

## CallOptions

- Type: `CallOptions`
- Fields:
  - `SkipCache`
  - `SkipEvents`
  - `SkipHooks`
  - `FieldOptions`
  - `LockMode`
  - `AllowInTransaction`

## Soft delete

- Excluded from finds by default: yes
- Include via: `set <SoftDeleteColumn> comparator on filter`
- Hard-delete method suffix: `HardDelete`
- Restore method suffix: `Restore`

## Comparator

Package `comparator`.

Families: Bool, Enum, ID, JSON, JSONB, Number, Slice, String, Time

- All families share: Eq, Neq, In, NotIn, IsNull, IsNotNull

- Numeric families (Number, Time) add: Gt, Gte, Lt, Lte

- String adds: Like, ILike, NotLike, NotILike

- Slice (JSON columns, PostgreSQL arrays) adds: Contains, ContainedBy, Overlap

Composition:
- `and`: Filter.And: []*Filter — predicates AND'd together
- `nesting`: And and Or nest arbitrarily; default is AND across top-level fields
- `or`: Filter.Or:  []*Filter — entries OR'd together; each entry's own fields are AND'd, so a union is one entry per branch

Examples:
- `compound_or`: `<pkg>.UserFilter{Or: []*<pkg>.UserFilter{{Email: &comparator.String{Eq: new("a@x.com")}}, {Email: &comparator.String{Eq: new("b@x.com")}}}}`
- `field_filter`: `<pkg>.UserFilter{Email: &comparator.String{Eq: new("a@x.com")}}`
- `id_in`: `&comparator.ID{In: []string{"u1", "u2", "u3"}}`
- `is_null`: `&comparator.NullableString{Null: new(true)}`
- `numeric_range`: `&comparator.Number[int]{Between: &comparator.Range[int]{Start: 18, End: 65}}`
- `text_contains`: `&comparator.String{Contains: new("acme")}`
- `time_after`: `&comparator.Time{Gt: new(cutoff)}`

## Omittable

Package `omittable` — type `omittable.Value[T]`.

Distinguishes 'not set' from 'set to zero value'. Used in <Entity>Update structs and partial-update contexts.

Construction:
- `omittable.Set(value) — set to a value`
- `omittable.Omit[T]() — explicitly unset (zero state)`

Methods:
- `IsSet() bool`
- `IsZero() bool`
- `Get() (T, bool)`
- `MustGet() T`

JSON behavior: Marshals to the wrapped value when set; omitted when unset (omitzero semantics on the wrapper, via IsZero)
