# models manifest

- **Dialect:** postgres
- **Generator:** sqlgen dev
- **Schema version:** 0.2.0

Features: cache, events, soft_delete, tenancy, views

## Entities

| Name | Table | Kind | File | Description |
| --- | --- | --- | --- | --- |
| Article | `articles` | table | [article.md](article.md) |  |
| ArticleStat | `article_stats` | view | [article_stat.md](article_stat.md) |  |
| Badge | `badges` | table | [badge.md](badge.md) |  |
| Checkpoint | `checkpoints` | table | [checkpoint.md](checkpoint.md) |  |
| Cycle | `cycles` | table | [cycle.md](cycle.md) |  |
| Label | `labels` | table | [label.md](label.md) |  |
| LineItem | `line_items` | table | [line_item.md](line_item.md) |  |
| Milestone | `milestones` | table | [milestone.md](milestone.md) |  |
| Project | `projects` | table | [project.md](project.md) |  |
| Site | `sites` | table | [site.md](site.md) |  |
| Sprint | `sprints` | table | [sprint.md](sprint.md) |  |
| Task | `tasks` | table | [task.md](task.md) |  |
| TaskBadge | `task_badges` | table | [task_badge.md](task_badge.md) |  |
| TaskLabel | `task_labels` | table | [task_label.md](task_label.md) |  |
| Template | `templates` | table | [template.md](template.md) |  |
| TemplateLabel | `template_labels` | table | [template_label.md](template_label.md) |  |
| TemplateStep | `template_steps` | table | [template_step.md](template_step.md) |  |
| Workspace | `workspaces` | table | [workspace.md](workspace.md) |  |
| WorkspaceLineTotal | `workspace_line_totals` | view | [workspace_line_total.md](workspace_line_total.md) |  |
| WorkspaceNote | `workspace_notes` | table | [workspace_note.md](workspace_note.md) |  |
| Zone | `zones` | table | [zone.md](zone.md) |  |

See [conventions](_conventions.md) for package-wide error sentinels, pagination, comparators, soft-delete, CallOptions, and the omittable reference.
