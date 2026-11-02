# Runtime Requirements: Database and Transactions

<!-- seeding: in-progress -->
<!-- packages: database database/mock database/pgx database/stdlib -->
<!-- matrix: transactions rows=feature cols=pgx,stdlib-postgres,mysql,sqlite -->
<!-- matrix: error-mapping rows=error-class cols=pgx,stdlib-postgres,mysql,sqlite -->

The database abstraction layer and its drivers (§19), transactions (§18), and constraint error
mapping (§22.2). See [README.md](../README.md) for the format.

## §18.6 Deferred Side Effects
<!-- prd: 18.6 sha256: 015391b1d84f058a -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| tx.callback-mode-option | The unified client exposes `WithCallbackMode(mode database.CallbackMode) ClientOption` | `unit: cmd/sqlgen/gen: TestUnifiedClientTemplate_clientOption` | all |  |

## §22.2 Constraint Errors
<!-- prd: 22.2 sha256: 79ed9d573708c0cf -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| error-mapping.mysql-not-null | The MySQL adapter maps `1048` and `1364` to a `ConstraintError` of `Type` `ConstraintNotNull` that unwraps to `ErrConstraintViolation`, with the column parsed from the message | `unit: database/stdlib: TestMapMySQLErrorConstraintCodes/1048 not null violation`; `unit: database/stdlib: TestMapMySQLErrorConstraintCodes/1364 insert omits a NOT NULL column with no default`; `unit: database/stdlib: TestMapMySQLErrorNotNullColumn/1048 names the column`; `unit: database/stdlib: TestMapMySQLErrorNotNullColumn/1364 names the column` | mysql | error-mapping: not-null × mysql |
| error-mapping.pgx-send-batch | The pgx adapter's `SendBatch` maps a constraint error read from its results (`Exec`, `Query`, `QueryRow`'s `Scan`, `Close`) to a structured `ConstraintError` that unwraps to `ErrConstraintViolation`, as it does for a statement | `integration: database/pgx: TestSendBatchMapsConstraintErrors`; `conformance: UpdateMany/a failed sub-batch rolls back its earlier items and keeps the sub-batches before it` | pgx | |
