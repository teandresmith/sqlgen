# models manifest

- **Dialect:** mysql
- **Generator:** sqlgen dev
- **Schema version:** 0.1.0

Features: audit_columns, graphql, soft_delete

## Entities

| Name | Table | Kind | File | Description |
| --- | --- | --- | --- | --- |
| Article | `articles` | table | [article.md](article.md) | Blog articles with timestamp soft delete |
| Asset | `assets` | table | [asset.md](asset.md) | Polymorphic parent for sub-categorized document relationships |
| AssetDocumentLink | `asset_document_links` | table | [asset_document_link.md](asset_document_link.md) | Junction table for m2m sub-categorized polymorphism |
| Category | `categories` | table | [category.md](category.md) | Product classification categories |
| CategoryStat | `category_stats` | view | [category_stat.md](category_stat.md) |  |
| DefaultOnlyRow | `default_only_rows` | table | [default_only_row.md](default_only_row.md) |  |
| Document | `documents` | table | [document.md](document.md) | Polymorphic child rows; entity_type sub-categorizes per-parent relationship |
| KeyOnlyRow | `key_only_rows` | table | [key_only_row.md](key_only_row.md) |  |
| Order | `orders` | table | [order.md](order.md) | Customer purchase orders |
| OrderItem | `order_items` | table | [order_item.md](order_item.md) | Individual line items within an order |
| Product | `products` | table | [product.md](product.md) | Catalog of products available for sale |
| ProductSummary | `product_summary` | view | [product_summary.md](product_summary.md) |  |
| ProductTagLabel | `product_tag_labels` | table | [product_tag_label.md](product_tag_label.md) | Product tag labels with 3-column composite primary key |
| Profile | `profiles` | table | [profile.md](profile.md) | Extended user profile information |
| SluggedRow | `slugged_rows` | table | [slugged_row.md](slugged_row.md) |  |
| Tag | `tags` | table | [tag.md](tag.md) | Content tags with boolean soft delete |
| User | `users` | table | [user.md](user.md) | Registered user accounts |
| UserCategory | `user_categories` | table | [user_category.md](user_category.md) | Junction table linking users to their preferred categories |
| UserEvent | `user_events` | table | [user_event.md](user_event.md) | User activity events; the one nullable-FK child table in this schema |
| Warehouse | `warehouses` | table | [warehouse.md](warehouse.md) | Storage warehouses with type-overridden columns |

See [conventions](_conventions.md) for package-wide error sentinels, pagination, comparators, soft-delete, CallOptions, and the omittable reference.
