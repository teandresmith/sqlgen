# models manifest

- **Dialect:** postgres
- **Generator:** sqlgen 0.42.0
- **Schema version:** 0.1.0

Features: audit_columns, cache, events, views

## Entities

| Name | Table | Kind | File | Description |
| --- | --- | --- | --- | --- |
| User | `users` | table | [users.md](users.md) | Application users. Soft-deleted on account closure. |
| ActiveUser | `active_users` | view | [active_users.md](active_users.md) | Non-deleted users. |

See [conventions](_conventions.md) for package-wide error sentinels, pagination, comparators, soft-delete, CallOptions, and the omittable reference.
