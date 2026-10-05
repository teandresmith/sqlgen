# models manifest

- **Dialect:** postgres
- **Generator:** sqlgen dev
- **Schema version:** 0.1.0

Features: audit_columns, cache, events, graphql, soft_delete, tenancy, views

## Entities

| Name | Table | Kind | File | Description |
| --- | --- | --- | --- | --- |
| Asset | `assets` | table | [asset.md](asset.md) |  |
| AssetDocumentLink | `asset_document_links` | table | [asset_document_link.md](asset_document_link.md) |  |
| Category | `categories` | table | [category.md](category.md) | Product classification categories |
| CategoryPriceTotal | `category_price_totals` | view | [category_price_total.md](category_price_total.md) |  |
| Document | `documents` | table | [document.md](document.md) |  |
| Event | `events` | table | [event.md](event.md) |  |
| FilterProbe | `filter_probes` | table | [filter_probe.md](filter_probe.md) |  |
| KeyedCode | `keyed_codes` | table | [keyed_code.md](keyed_code.md) |  |
| KeyedCodeEntry | `keyed_code_entries` | table | [keyed_code_entry.md](keyed_code_entry.md) |  |
| MaskedLabel | `masked_labels` | table | [masked_label.md](masked_label.md) |  |
| NumericWidth | `numeric_widths` | table | [numeric_width.md](numeric_width.md) |  |
| Order | `orders` | table | [order.md](order.md) |  |
| OrderItem | `order_items` | table | [order_item.md](order_item.md) |  |
| Product | `products` | table | [product.md](product.md) |  |
| Profile | `profiles` | table | [profile.md](profile.md) |  |
| ScalarProbe | `scalar_probes` | table | [scalar_probe.md](scalar_probe.md) |  |
| User | `users` | table | [user.md](user.md) |  |
| UserBadge | `user_badges` | table | [user_badge.md](user_badge.md) |  |
| UserCategory | `user_categories` | table | [user_category.md](user_category.md) |  |
| UserCredential | `user_credentials` | table | [user_credential.md](user_credential.md) |  |
| UserSession | `user_sessions` | table | [user_session.md](user_session.md) |  |
| WorkspaceNote | `workspace_notes` | table | [workspace_note.md](workspace_note.md) |  |
| WorkspaceNoteSummary | `workspace_note_summary` | view | [workspace_note_summary.md](workspace_note_summary.md) |  |
| WorkspaceSetting | `workspace_settings` | table | [workspace_setting.md](workspace_setting.md) |  |

See [conventions](_conventions.md) for package-wide error sentinels, pagination, comparators, soft-delete, CallOptions, and the omittable reference.
