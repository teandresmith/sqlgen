# SQLGen

Generate a complete, type-safe Go data layer — and the services around it — from your SQL schema.

SQLGen reads your schema (from `.sql` files, a live database, or both) for PostgreSQL, MySQL or
SQLite and generates idiomatic Go: structs, a typed client per entity, filters, pagination,
relationship loading, and nested writes. No ORM, no reflection, no `interface{}` round-trips —
everything is generated Go you can read, and the core runtime depends only on the standard library.

## Features

- **Typed client**: CRUD, upsert, increment, cursor and offset pagination, streaming, typed filters,
  field selection, relationship loading (O2O / O2M / M2M), transactions and lock modes.
- **Nested mutations** (opt-in): write a row and its related rows in one transaction.
- **GraphQL API**: `.graphqls` schemas and gqlgen resolvers wired to the client.
- **MCP server**: serves the generated surface to AI agents.
- **Caching**: in-memory or Redis, with invalidation and a circuit breaker.
- **Events**: mutation events over an in-memory bus or NATS JetStream.
- **Multi-tenancy**: tenant isolation enforced in the generated SQL.
- **Column access control, hooks, OpenTelemetry metrics** and a machine-readable **manifest**.

Everything beyond the client is opt-in.

## Quick start

Install a pre-built binary from [GitHub Releases](https://github.com/teandresmith/sqlgen/releases),
or build from source (requires CGO and a C compiler):

```bash
go install github.com/teandresmith/sqlgen/cmd/sqlgen@latest
```

Then:

```bash
sqlgen init        # write a starter sqlgen.yml
sqlgen generate    # generate code from your schema
sqlgen diff        # verify generated code is up to date (CI-friendly)
```

## Documentation

| Document | Purpose |
|----------|---------|
| [Getting started](./docs/getting-started.md) | From a schema to a generated client, with usage examples |
| [Configuration](./docs/configuration.md) | Full `sqlgen.yml` reference, including editor/IDE setup |
| [Dialect support](./docs/dialects.md) | How PostgreSQL, MySQL and SQLite differ |
| [Database round trips](./docs/round-trips.md) | How many statements, and sequential rounds of them, each client method sends, measured on every dialect |
| [`sqlgen manifest` CLI](./docs/cli-manifest.md) | Manifest validate / diff tooling |
| [sqlgen-example](https://github.com/teandresmith/sqlgen-example) | Reference application: a multi-tenant GraphQL API built on a generated client |
| [Per-feature test projects](./cmd/sqlgen/testdata/examples/) | Complete, tested projects for every dialect and feature |

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md).

## License

[MIT](./LICENSE)
