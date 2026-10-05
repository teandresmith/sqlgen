# models manifest

- **Dialect:** postgres
- **Generator:** sqlgen dev
- **Schema version:** 0.1.0

Features: audit_columns, cache, events, soft_delete, views

## Entities

| Name | Table | Kind | File | Description |
| --- | --- | --- | --- | --- |
| Article | `articles` | table | [article.md](article.md) | Blog articles with timestamp soft delete |
| Asset | `assets` | table | [asset.md](asset.md) | Polymorphic parent for sub-categorized document relationships |
| AssetDocumentLink | `asset_document_links` | table | [asset_document_link.md](asset_document_link.md) | Junction table for m2m sub-categorized polymorphism |
| AuditNote | `notes` | table | [audit_note.md](audit_note.md) |  |
| AuditUser | `users` | table | [audit_user.md](audit_user.md) | Audit log of user account changes |
| Category | `categories` | table | [category.md](category.md) | Product classification categories |
| CategoryStat | `category_stats` | view | [category_stat.md](category_stat.md) |  |
| Counter | `counters` | table | [counter.md](counter.md) | Counter rows keyed via primary_key.columns override (single-column PK) |
| DefaultOnlyRow | `default_only_rows` | table | [default_only_row.md](default_only_row.md) |  |
| Document | `documents` | table | [document.md](document.md) | Polymorphic child rows; entity_type sub-categorizes per-parent relationship |
| Event | `events` | table | [event.md](event.md) | General system audit events |
| KeyOnlyRow | `key_only_rows` | table | [key_only_row.md](key_only_row.md) |  |
| NoteWatcher | `note_watchers` | table | [note_watcher.md](note_watcher.md) |  |
| Order | `orders` | table | [order.md](order.md) | Customer purchase orders |
| OrderItem | `order_items` | table | [order_item.md](order_item.md) | Individual line items within an order |
| OrderTotal | `order_totals` | view | [order_total.md](order_total.md) |  |
| Product | `products` | table | [product.md](product.md) | Catalog of products available for sale |
| ProductSummary | `product_summary` | view | [product_summary.md](product_summary.md) |  |
| ProductTagLabel | `product_tag_labels` | table | [product_tag_label.md](product_tag_label.md) | Product tag labels with 3-column composite primary key |
| Profile | `profiles` | table | [profile.md](profile.md) | Extended user profile information |
| PublicNote | `notes` | table | [public_note.md](public_note.md) |  |
| PublicUser | `users` | table | [public_user.md](public_user.md) | Registered user accounts |
| RateLimit | `rate_limits` | table | [rate_limit.md](rate_limit.md) | Per-org rate-limit buckets keyed via primary_key.columns override (composite PK) |
| SluggedRow | `slugged_rows` | table | [slugged_row.md](slugged_row.md) |  |
| Tag | `tags` | table | [tag.md](tag.md) | Content tags with boolean soft delete |
| UserCategory | `user_categories` | table | [user_category.md](user_category.md) | Junction table linking users to their preferred categories |
| Warehouse | `warehouses` | table | [warehouse.md](warehouse.md) | Storage warehouses with type-overridden columns |

See [conventions](_conventions.md) for package-wide error sentinels, pagination, comparators, soft-delete, CallOptions, and the omittable reference.
