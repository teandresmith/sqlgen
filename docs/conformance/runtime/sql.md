# Runtime Requirements: SQL Builders, Comparators and Omittable

<!-- seeding: in-progress -->
<!-- packages: sql comparator omittable -->
<!-- matrix: sql-builders rows=builder cols=basic,returning,empty-input,composite-key,quoted-identifier -->
<!-- matrix: comparators rows=operator cols=value,nil,empty-list,escaping -->
<!-- matrix: condition-composition rows=shape cols=placeholder-numbering,nesting -->
<!-- matrix: lock-modes rows=mode cols=postgres,mysql,sqlite -->
<!-- matrix: omittable rows=state cols=value,json-round-trip -->

The SQL builders and dialects (Appendix A, Appendix B), the comparators and condition composition
(§11.2–§11.5), the sort type (§15.1), and the `omittable` package (§10). See
[README.md](../README.md) for the format.

## §10.4 JSON Serialization
<!-- prd: 10.4 sha256: 104cc937927c7e3c -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| omittable.json-omitzero | A generated input field of type `omittable.Value[T]` carries the `omitzero` option in its `json` tag | `unit: cmd/sqlgen/gen: TestCreateInputTemplate_dbStrategyUUIDPK_omittable`; `unit: cmd/sqlgen/gen: TestInputTemplate_goldenFile_products`; `unit: cmd/sqlgen/gen: TestInputTemplate_goldenFile_orderItems` | all |  |

## §11.2 Comparator Types
<!-- prd: 11.2 sha256: cfdcc0e3a93d5e56 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| comparator.pg-only.never-selected | The generator never selects the `JSONB` or `Slice` comparators for a MySQL or SQLite column: a `jsonb` column takes `JSON` on MySQL and is non-filterable on SQLite, and no array type, `type_map` entry or type override to a Go slice makes a column a slice | `unit: cmd/sqlgen/gen: TestComparatorTypeSelection/mysql jsonb (types.JSON) → JSON, never JSONB`; `unit: cmd/sqlgen/gen: TestJSONComparator_sqliteModelSideNonFilterable`; `unit: cmd/sqlgen/gotype: TestArrayTypesAreSlicesOnlyOnPostgres` | mysql, sqlite |  |
