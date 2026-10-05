# models manifest

- **Dialect:** sqlite
- **Generator:** sqlgen dev
- **Schema version:** 0.1.0

Features: audit_columns, graphql, soft_delete

## Entities

| Name | Table | Kind | File | Description |
| --- | --- | --- | --- | --- |
| Article | `articles` | table | [article.md](article.md) |  |
| Asset | `assets` | table | [asset.md](asset.md) |  |
| AssetDocumentLink | `asset_document_links` | table | [asset_document_link.md](asset_document_link.md) |  |
| BinaryKey | `binary_keys` | table | [binary_key.md](binary_key.md) |  |
| BinaryKeyEvent | `binary_key_events` | table | [binary_key_event.md](binary_key_event.md) |  |
| Category | `categories` | table | [category.md](category.md) |  |
| CategoryStat | `category_stats` | view | [category_stat.md](category_stat.md) |  |
| DigitLeadingColumn | `digit_leading_columns` | table | [digit_leading_column.md](digit_leading_column.md) |  |
| Document | `documents` | table | [document.md](document.md) |  |
| KeyOnlyRow | `key_only_rows` | table | [key_only_row.md](key_only_row.md) |  |
| Order | `orders` | table | [order.md](order.md) |  |
| OrderItem | `order_items` | table | [order_item.md](order_item.md) |  |
| Owner | `owners` | table | [owner.md](owner.md) |  |
| OwnerProfile | `owner_profiles` | table | [owner_profile.md](owner_profile.md) |  |
| Product | `products` | table | [product.md](product.md) |  |
| ProductSummary | `product_summary` | view | [product_summary.md](product_summary.md) |  |
| ProductTagLabel | `product_tag_labels` | table | [product_tag_label.md](product_tag_label.md) |  |
| Profile | `profiles` | table | [profile.md](profile.md) |  |
| ReservedWordColumn | `reserved_word_columns` | table | [reserved_word_column.md](reserved_word_column.md) |  |
| SluggedRow | `slugged_rows` | table | [slugged_row.md](slugged_row.md) |  |
| Tag | `tags` | table | [tag.md](tag.md) |  |
| User | `users` | table | [user.md](user.md) |  |
| UserCategory | `user_categories` | table | [user_category.md](user_category.md) |  |
| Warehouse | `warehouses` | table | [warehouse.md](warehouse.md) |  |

See [conventions](_conventions.md) for package-wide error sentinels, pagination, comparators, soft-delete, CallOptions, and the omittable reference.
