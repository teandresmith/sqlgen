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
| LedgerEntry | `ledger_entries` | table | [ledger_entry.md](ledger_entry.md) |  |
| LineItem | `line_items` | table | [line_item.md](line_item.md) |  |
| Milestone | `milestones` | table | [milestone.md](milestone.md) |  |
| Moment | `moments` | table | [moment.md](moment.md) |  |
| MomentOwner | `moment_owners` | table | [moment_owner.md](moment_owner.md) |  |
| MomentTag | `moment_tags` | table | [moment_tag.md](moment_tag.md) |  |
| MomentTagLink | `moment_tag_links` | table | [moment_tag_link.md](moment_tag_link.md) |  |
| Plot | `plots` | table | [plot.md](plot.md) |  |
| Project | `projects` | table | [project.md](project.md) |  |
| Reviewer | `reviewers` | table | [reviewer.md](reviewer.md) |  |
| SharedGuardian | `shared_guardians` | table | [shared_guardian.md](shared_guardian.md) |  |
| SharedKid | `shared_kids` | table | [shared_kid.md](shared_kid.md) |  |
| SharedParent | `shared_parents` | table | [shared_parent.md](shared_parent.md) |  |
| Site | `sites` | table | [site.md](site.md) |  |
| Sprint | `sprints` | table | [sprint.md](sprint.md) |  |
| Stamp | `stamps` | table | [stamp.md](stamp.md) |  |
| Task | `tasks` | table | [task.md](task.md) |  |
| TaskBadge | `task_badges` | table | [task_badge.md](task_badge.md) |  |
| TaskLabel | `task_labels` | table | [task_label.md](task_label.md) |  |
| TaskReviewer | `task_reviewers` | table | [task_reviewer.md](task_reviewer.md) |  |
| Template | `templates` | table | [template.md](template.md) |  |
| TemplateLabel | `template_labels` | table | [template_label.md](template_label.md) |  |
| TemplateNote | `template_notes` | table | [template_note.md](template_note.md) |  |
| TemplateStamp | `template_stamps` | table | [template_stamp.md](template_stamp.md) |  |
| TemplateStep | `template_steps` | table | [template_step.md](template_step.md) |  |
| Workspace | `workspaces` | table | [workspace.md](workspace.md) |  |
| WorkspaceLineTotal | `workspace_line_totals` | view | [workspace_line_total.md](workspace_line_total.md) |  |
| WorkspaceNote | `workspace_notes` | table | [workspace_note.md](workspace_note.md) |  |
| Zone | `zones` | table | [zone.md](zone.md) |  |

See [conventions](_conventions.md) for package-wide error sentinels, pagination, comparators, soft-delete, CallOptions, and the omittable reference.
