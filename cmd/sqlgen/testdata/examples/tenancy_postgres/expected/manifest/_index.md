# models manifest

- **Dialect:** postgres
- **Generator:** sqlgen dev
- **Schema version:** 0.1.0

Features: cache, events, soft_delete, tenancy, views

## Entities

| Name | Table | Kind | File | Description |
| --- | --- | --- | --- | --- |
| Article | `articles` | table | [article.md](article.md) |  |
| ArticleStat | `article_stats` | view | [article_stat.md](article_stat.md) |  |
| LineItem | `line_items` | table | [line_item.md](line_item.md) |  |
| WorkspaceLineTotal | `workspace_line_totals` | view | [workspace_line_total.md](workspace_line_total.md) |  |

See [conventions](_conventions.md) for package-wide error sentinels, pagination, comparators, soft-delete, CallOptions, and the omittable reference.
