# Parser Requirements

<!-- seeding: in-progress -->
<!-- packages: parser parser/postgres parser/mysql parser/sqlite parser/introspect -->
<!-- matrix: schema-model rows=element cols=postgres-files,postgres-database,postgres-both,mysql-files,mysql-database,mysql-both,sqlite-files,sqlite-database,sqlite-both -->
<!-- matrix: relationship-detection rows=fk-shape cols=postgres-files,postgres-database,postgres-both,mysql-files,mysql-database,mysql-both,sqlite-files,sqlite-database,sqlite-both -->
<!-- matrix: soft-delete-detection rows=column cols=postgres,mysql,sqlite -->
<!-- matrix: qualification rows=case cols=postgres,mysql,sqlite -->
<!-- matrix: determinism rows=entry-point cols=shuffled-input -->
<!-- matrix: introspection-filtering rows=rule cols=postgres,mysql,sqlite -->

No requirements yet beyond the §5.5 config-key rows below. See [README.md](README.md) for the format.

## §5.5 Schema Qualification & Table Naming
<!-- prd: 5.5 sha256: 9e695eea25c76dd7 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| parser.qualify.config-key-ambiguous | A bare `tables:` key naming a table that exists in more than one schema is a validation error naming the key | `unit: cmd/sqlgen/config: TestValidatePostParse_AmbiguousBareTableName` | postgres |  |
| parser.qualify.config-key-bare-matches | A bare `tables:` key matches the table regardless of schema when only one schema has it | `unit: cmd/sqlgen/config: TestValidatePostParse_BareTableNameSingleSchema` | postgres |  |
| parser.qualify.config-key-mysql-never-ambiguous | On MySQL and SQLite a bare config key never raises the ambiguity error | `unit: cmd/sqlgen/config: TestValidatePostParse_MySQLBareKeysNoAmbiguity` | mysql, sqlite |  |
