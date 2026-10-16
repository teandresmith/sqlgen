# models manifest

- **Dialect:** mysql
- **Generator:** sqlgen dev
- **Schema version:** 0.1.0

Features: none

## Entities

| Name | Table | Kind | File | Description |
| --- | --- | --- | --- | --- |
| Account | `accounts` | table | [account.md](account.md) | Customer accounts |
| Project | `projects` | table | [project.md](project.md) |  |
| ProjectSummary | `project_summaries` | view | [project_summary.md](project_summary.md) |  |
| Task | `tasks` | table | [task.md](task.md) |  |
| TaskLabel | `task_labels` | table | [task_label.md](task_label.md) |  |

See [conventions](_conventions.md) for package-wide error sentinels, pagination, comparators, soft-delete, CallOptions, and the omittable reference.
