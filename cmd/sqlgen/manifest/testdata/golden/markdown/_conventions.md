# models conventions

Package-wide reference. Per-entity files cross-reference this document rather than restating it.

## Client entry points

- Entity client: `client.<Entity>()` — one accessor per entity, carrying reads and writes alike. Tables use the plural struct name (`client.Users()`), views the singular one (`client.ActiveUser()`).
- `Get` returns nil on a missing row: yes

## Error sentinels

| Name | GraphQL code | Package |
| --- | --- | --- |
| `ErrNotFound` | `NOT_FOUND` | models |
| `ErrConstraintViolation` | — | models |
| `ErrMissing` | `UNAUTHENTICATED` | tenancy |

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

- Type: `CallOption`
- Fields:
  - `WithTx`
  - `IncludeSoftDeleted`

## Soft delete

- Excluded from finds by default: yes
- Include via: `WithSoftDeleted()`
- Hard-delete method suffix: `HardDelete`
- Restore method suffix: `Restore`

## Comparator

Package `comparator`.

Families: Bool, Enum, ID, JSON, JSONB, Number, Slice, String, Time

- Every family exposes Eq/Neq; ordered families add Gt/Gte/Lt/Lte.

Composition:
- `and`: all conditions must match
- `or`: any condition matches

Examples:
- `eq`: `comparator.String.Eq("alice")`

## Omittable

Package `omittable` — type `omittable.Value[T]`.

Distinguishes 'not set' from 'set to zero value'.

Construction:
- `omittable.Set(value)`
- `omittable.Omit[T]()`

Methods:
- `IsSet() bool`
- `IsZero() bool`
- `Get() (T, bool)`
- `MustGet() T`

JSON behavior: Marshals to the wrapped value when set; omitted when unset.
