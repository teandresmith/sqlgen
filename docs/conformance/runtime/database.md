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
<!-- prd: 22.2 sha256: d4f5441b0dd75250 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| error-mapping.mysql-not-null | The MySQL adapter maps `1048` and `1364` to a `ConstraintError` of `Type` `ConstraintNotNull` that unwraps to `ErrConstraintViolation`, with the column parsed from the message | `unit: database/stdlib: TestMapMySQLErrorConstraintCodes/1048 not null violation`; `unit: database/stdlib: TestMapMySQLErrorConstraintCodes/1364 insert omits a NOT NULL column with no default`; `unit: database/stdlib: TestMapMySQLErrorNotNullColumn/1048 names the column`; `unit: database/stdlib: TestMapMySQLErrorNotNullColumn/1364 names the column` | mysql | error-mapping: not-null × mysql |
| error-mapping.pgx-send-batch | The pgx adapter's `SendBatch` maps a constraint error read from its results (`Exec`, `Query`, `QueryRow`'s `Scan`, `Close`) to a structured `ConstraintError` that unwraps to `ErrConstraintViolation`, as it does for a statement | `integration: database/pgx: TestSendBatchMapsConstraintErrors`; `conformance: UpdateMany/a failed sub-batch rolls back its earlier items and keeps the sub-batches before it` | pgx | |
| error-mapping.transport-failure | A failure to reach the database or keep the connection is `ErrConnectionFailed` wrapping the driver's error, on every driver and at `Begin` as at every statement: a refused connect, a connect timeout the driver enforces while the caller's context is live, a broken socket, `driver.ErrBadConn`, MySQL's `ErrInvalidConn`. A rejected password at connect is not, and once the caller's context has ended nothing is | `unit: database/pgx: TestRefusedConnectionIsConnectionFailed/exec`; `unit: database/pgx: TestRefusedConnectionIsConnectionFailed/begin`; `unit: database/pgx: TestMapErrorTransportFailures/a broken socket read`; `unit: database/pgx: TestMapErrorCtxTransportFailures/a driver's dial timeout while the caller waits is a connection failure`; `unit: database/pgx: TestMapErrorCtxTransportFailures/a broken socket after the caller's context ended is not a connection failure`; `unit: database/pgx: TestConnectTimeoutIsConnectionFailedOnlyWhileTheCallerWaits/the driver's connect timeout is a connection failure`; `unit: database/pgx: TestConnectTimeoutIsConnectionFailedOnlyWhileTheCallerWaits/the caller's deadline is not a connection failure`; `unit: database/pgx: TestMapErrorConnectionFailed/28P01 a rejected password at connect is not a connection failure`; `integration: database/pgx: TestRejectedPasswordIsNotConnectionFailed`; `unit: database/stdlib: TestRefusedConnectionIsConnectionFailed/mysql exec`; `unit: database/stdlib: TestRefusedConnectionIsConnectionFailed/mysql begin`; `unit: database/stdlib: TestRefusedConnectionIsConnectionFailed/postgres exec`; `unit: database/stdlib: TestRefusedConnectionIsConnectionFailed/postgres begin`; `unit: database/stdlib: TestMapErrorTransportFailures/a broken socket read`; `unit: database/stdlib: TestMapErrorTransportFailures/a bad connection the pool gave up on`; `unit: database/stdlib: TestMapErrorTransportFailures/a connection the MySQL driver gave up on`; `unit: database/stdlib: TestMapErrorCtxTransportFailures/a driver's dial timeout while the caller waits is a connection failure`; `unit: database/stdlib: TestMapErrorCtxTransportFailures/a broken socket after the caller's context ended is not a connection failure`; `unit: database/stdlib: TestConnectTimeoutIsConnectionFailedOnlyWhileTheCallerWaits/mysql handshake read timeout is a connection failure`; `unit: database/stdlib: TestConnectTimeoutIsConnectionFailedOnlyWhileTheCallerWaits/postgres handshake timeout is a connection failure`; `unit: database/stdlib: TestConnectTimeoutIsConnectionFailedOnlyWhileTheCallerWaits/the caller's deadline is not a connection failure`; `unit: database/stdlib: TestMapPqErrorConnectionFailed/28P01 a rejected password is not a connection failure`; `unit: database/stdlib: TestMapMySQLErrorConnectionFailed/1045 a rejected password is not a connection failure` | all | |
