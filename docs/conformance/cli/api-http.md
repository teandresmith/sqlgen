# CLI Requirements: API Generation — REST, HTTP Headers and gRPC

<!-- seeding: in-progress -->

REST (§26.8), per-call options via HTTP headers (§26.11) and gRPC (§26.13). The code and tests live in `cmd/sqlgen/gen`, whose orphan check stays with [`gen.md`](gen.md), so this file lists no package of its own. REST and gRPC are not generated yet (REST is deferred and gRPC is future work), so their rows record the PRD's promises as untested planned work, not defects.

## §26.8 REST API (Supplementary)
<!-- prd: 26.8 sha256: 76a7d06cc3d95798 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| api-http.rest.net-http-handlers | REST endpoints are generated as `net/http` stdlib handlers | | all | |
| api-http.rest.shares-walker | REST handlers share the schema-traversal walker shape the GraphQL pipeline produces | | all | |
| api-http.rest.shares-translators | REST handlers share the GraphQL pipeline's filter and comparator translators | | all | |
| api-http.rest.shares-error-mapping | REST handlers share the GraphQL pipeline's error-mapping helper | | all | |
| api-http.rest.endpoint-per-operation | A table gets one REST endpoint per client method the schema allows | | all | |
| api-http.rest.operations-mask | The per-table REST endpoints are narrowed by `api.operations` (§26.5.1) | | all | |
| api-http.rest.endpoint.get | `get` is served at `GET /{table}/{id}` with no request body | | all | |
| api-http.rest.endpoint.get-many | `get_many` is served at `POST /{table}/query` with filter/pagination/sort as a JSON body | | all | |
| api-http.rest.endpoint.create | `create` is served at `POST /{table}` with a `CreateInput` JSON body | | all | |
| api-http.rest.endpoint.create-many | `create_many` is served at `POST /{table}/batch` with a `[CreateInput]` JSON body | | all | |
| api-http.rest.endpoint.update | `update` is served at `PATCH /{table}/{id}` with an `UpdateInput` JSON body | | all | |
| api-http.rest.endpoint.upsert | `upsert` is served at `PUT /{table}` with an `{input, conflict_keys}` JSON body | | all | |
| api-http.rest.endpoint.soft-delete | `soft_delete` is served at `DELETE /{table}/{id}` with no request body | | all | |
| api-http.rest.endpoint.hard-delete | `hard_delete` is served at `DELETE /{table}/{id}?hard=true` with no request body | | all | |
| api-http.rest.endpoint.exists | `exists` is served at `HEAD /{table}/{id}` with no request body | | all | |
| api-http.rest.endpoint.count | `count` is served at `POST /{table}/count` with a filter JSON body | | all | |
| api-http.rest.complex-queries-post-json | Complex queries use `POST` with a JSON body, not query-parameter encoding of the filter | | all | |
| api-http.rest.simple-lookups-get | Simple lookups use `GET` | | all | |
| api-http.rest.new-api-handler | `<pkg>.NewAPIHandler(client, opts...)` builds the REST API handler from the unified client | | all | |
| api-http.rest.with-api-middleware | `<pkg>.WithAPIMiddleware(mw)` is an option to `NewAPIHandler` that installs middleware | | all | |
| api-http.rest.with-api-error-handler | `<pkg>.WithAPIErrorHandler(h)` is an option to `NewAPIHandler` that installs a custom error handler | | all | |
| api-http.rest.handler-method | `api.Handler()` returns an `http.Handler` mountable on an `http.ServeMux` (e.g. at `/api/v1/`) | | all | |
| api-http.rest.response.single | A single-entity response is `{ "data": { … } }` | | all | |
| api-http.rest.response.list-data | A list response carries the rows as `"data": [ … ]` | | all | |
| api-http.rest.response.list-meta | A list response carries `"meta": { "total", "limit", "offset" }` | | all | |
| api-http.rest.response.error | An error response is `{ "error": { "code", "message" } }` (e.g. `"code": "NOT_FOUND"`) | | all | |

## §26.11 Per-Call Options via HTTP Headers
<!-- prd: 26.11 sha256: 500051ab877103d2 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| api-http.headers.graphql-translates | The GraphQL API translates HTTP headers into `CallOptions` before passing the request to the unified client | `example: graphql: TestCache_NoCacheHeaderBypasses` | all | |
| api-http.headers.rest-translates | The REST API translates the same HTTP headers into `CallOptions` before passing the request to the unified client | | all | |
| api-http.headers.one-convention | One header convention is shared across protocols (GraphQL and REST use the same header names and values) | | all | |
| api-http.headers.cache-control.sets-skip-cache | `Cache-Control: no-cache` sets `SkipCache = true` for the request | `unit: cmd/sqlgen/gen: TestMiddleware_CacheControlNoCache`; `example: graphql: TestCache_NoCacheHeaderBypasses` | all | |
| api-http.headers.cache-control.exact-value | Only the exact value `no-cache` sets `SkipCache` (`r.Header.Get("Cache-Control") == "no-cache"`) | `unit: cmd/sqlgen/gen: TestMiddleware_CacheControlNoCache` | all | |
| api-http.headers.cache-control.bypasses-read-through | `Cache-Control: no-cache` bypasses cache read-through: the request reaches the database and the cache is not probed although a cached entity exists | `example: graphql: TestCache_NoCacheHeaderBypasses` | all | |
| api-http.headers.cache-control.bypasses-write-through | `Cache-Control: no-cache` bypasses cache write-through for the request | `example: graphql: TestCallOptionHeaders_NoCacheBypassesWriteThrough/a read with Cache-Control no-cache writes nothing to the cache`; `example: graphql: TestCallOptionHeaders_NoCacheBypassesWriteThrough/a mutation with Cache-Control no-cache writes nothing to the cache` | all |  |
| api-http.headers.skip-events.sets-skip-events | `X-Skip-Events: true` sets `SkipEvents = true` for the request | `unit: cmd/sqlgen/gen: TestMiddleware_XSkipEvents` | all | |
| api-http.headers.skip-events.exact-value | Only the exact value `true` sets `SkipEvents` (`r.Header.Get("X-Skip-Events") == "true"`) | `unit: cmd/sqlgen/gen: TestMiddleware_XSkipEvents` | all | |
| api-http.headers.skip-events.suppresses-publishing | `X-Skip-Events: true` suppresses event publishing for the mutations in the request | `example: graphql: TestCallOptionHeaders_SkipEventsSuppressesPublishing/a mutation with X-Skip-Events true publishes nothing` | all |  |
| api-http.headers.skip-hooks.sets-skip-hooks | `X-Skip-Hooks: true` sets `SkipHooks = true` for the request | `unit: cmd/sqlgen/gen: TestMiddleware_XSkipHooks` | all | |
| api-http.headers.skip-hooks.exact-value | Only the exact value `true` sets `SkipHooks` (`r.Header.Get("X-Skip-Hooks") == "true"`) | `unit: cmd/sqlgen/gen: TestMiddleware_XSkipHooks` | all | |
| api-http.headers.skip-hooks.bypasses-hooks | `X-Skip-Hooks: true` bypasses the registered hooks for the request | `example: graphql: TestCallOptionHeaders_SkipHooksBypassesHooks/a mutation with X-Skip-Hooks true runs no hook` | all |  |
| api-http.headers.skip-hooks.keeps-panic-recovery | Under `X-Skip-Hooks: true` panic recovery still runs |  | all |  |
| api-http.headers.skip-hooks.implies-skip-cache | `X-Skip-Hooks: true` implies `SkipCache` for the request | `example: graphql: TestCallOptionHeaders_SkipHooksImpliesSkipCacheAndEvents/a mutation with only X-Skip-Hooks true touches no cache and publishes nothing` | all |  |
| api-http.headers.skip-hooks.implies-skip-events | `X-Skip-Hooks: true` implies `SkipEvents` for the request | `example: graphql: TestCallOptionHeaders_SkipHooksImpliesSkipCacheAndEvents/a mutation with only X-Skip-Hooks true touches no cache and publishes nothing` | all |  |
| api-http.middleware.file | The GraphQL middleware pieces are emitted in `graph/sqlgenresolver/middleware_gen.go` | `golden: graphql` | all | |
| api-http.middleware.signature | The generated middleware is `WithCallOptionsMiddleware(next http.Handler) http.Handler` | `unit: cmd/sqlgen/gen: TestCallOptionsFromHTTP_RoundTrip`; `golden: graphql` | all | |
| api-http.middleware.wraps-gqlgen-handler | `WithCallOptionsMiddleware` wraps the gqlgen handler at server-bootstrap time (`graph/server.go` returns `sqlgenresolver.WithCallOptionsMiddleware(srv)`) | `unit: cmd/sqlgen/gen: TestGenerate_GraphQLQueryLimits/server.go is written when absent` | all | |
| api-http.middleware.calls-next | The middleware serves the wrapped handler with the request carrying the new context (`next.ServeHTTP(w, r.WithContext(ctx))`) | `unit: cmd/sqlgen/gen: TestCallOptionsFromHTTP_RoundTrip` | all | |
| api-http.middleware.flag-struct | The middleware records the headers in a per-request `httpCallOptions{SkipCache, SkipEvents, SkipHooks bool}` flag struct | `unit: cmd/sqlgen/gen: TestCallOptionsFromHTTP_RoundTrip`; `golden: graphql` | all | |
| api-http.middleware.unexported-key | The flag struct is stashed in the request context under an unexported `callOptionsKey` context key | `unit: cmd/sqlgen/gen: TestCallOptionsFromHTTP_RoundTrip` | all | |
| api-http.middleware.records-explicit-skip-hooks-only | The middleware records only the explicit `SkipHooks` flag for `X-Skip-Hooks`; it does not itself set `SkipCache` / `SkipEvents` | `golden: graphql` | all | |
| api-http.middleware.implication-in-runtime | The `SkipHooks ⇒ SkipCache + SkipEvents` implication is enforced by the runtime's `resolveCallOptions` (§9.6) | | all | |
| api-http.middleware.no-skip-hooks-restriction | The generated middleware does not restrict `X-Skip-Hooks` to internal/admin traffic; protecting it is the consumer's responsibility | `golden: graphql` | all | |
| api-http.read.signature | The read side is `callOptionsFromHTTP[FO any](ctx context.Context) []func(*<pkg>.CallOptions[FO])` | `unit: cmd/sqlgen/gen: TestCallOptionsFromHTTP_RoundTrip` | all | |
| api-http.read.nil-without-middleware | `callOptionsFromHTTP` returns nil when the middleware did not run (no flag struct in the context) | `golden: graphql` | all |  |
| api-http.read.projects-truthy-flags | `callOptionsFromHTTP` projects each truthy flag into a typed setter on `CallOptions[FO]` (`o.SkipCache = true`, `o.SkipEvents = true`, `o.SkipHooks = true`) | `unit: cmd/sqlgen/gen: TestMiddleware_CacheControlNoCache`; `unit: cmd/sqlgen/gen: TestMiddleware_XSkipEvents`; `unit: cmd/sqlgen/gen: TestMiddleware_XSkipHooks` | all | |
| api-http.read.setter-order | The setters are appended in header order: Cache-Control, X-Skip-Events, X-Skip-Hooks | `unit: cmd/sqlgen/gen: TestMiddleware_AllThreeHeaders` | all | |
| api-http.resolver.applies-in-setter | Every generated resolver applies `callOptionsFromHTTP` inside the per-call `CallOptions[FO]` setter it passes to the client | `golden: graphql` | all | |
| api-http.resolver.explicit-type-arg | Every call site spells the explicit `[<pkg>.<Table>FieldOptions]` type argument; the bare `callOptionsFromHTTP(ctx)` form never appears | `unit: cmd/sqlgen/gen: TestResolvers_CallOptionsFromHTTP_ExplicitTypeArg` | all | |
| api-http.resolver.covers-relationship-loads | The header overrides cover the primary client call and the relationship loads triggered by the FieldOptions tree: under `X-Skip-Hooks: true` no query hook runs for either read | `example: graphql: TestCallOptionHeaders_SkipHooksCoversRelationshipLoads/a relationship query with X-Skip-Hooks true runs no hook for either read` | all |  |
| api-http.rest.non-generic-projection | The REST API reuses the same flag-struct stash with a non-generic projection onto the route's concrete `CallOptions[<Table>FieldOptions]` | | all | |

## §26.13 gRPC API Generation
<!-- prd: 26.13 sha256: 7d8db687b7a34471 -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| api-http.grpc.map.table-message | A table maps to a protobuf `message` (the entity) | | all | |
| api-http.grpc.map.table-service | A table maps to a protobuf `service` (its operations) | | all | |
| api-http.grpc.map.column | A column maps to a message field with a typed scalar | | all | |
| api-http.grpc.map.enum | An enum type maps to a protobuf `enum` | | all | |
| api-http.grpc.map.o2o | An O2O relationship maps to a nested message field | | all | |
| api-http.grpc.map.o2m | An O2M relationship maps to a `repeated` message field | | all | |
| api-http.grpc.map.filter | The filter struct maps to a request message with optional filter fields | | all | |
| api-http.grpc.map.pagination | Pagination maps to request/response messages with cursor or offset fields | | all | |
| api-http.grpc.map.inputs | Create/Update inputs map to request messages | | all | |
| api-http.grpc.artifact.proto | A table gets `{table}.proto`, a protobuf service definition with RPCs for all enabled operations | | all | |
| api-http.grpc.artifact.service-go | A table gets `{table}_grpc_gen.go`, a Go service implementation delegating to the unified client | | all | |
| api-http.grpc.artifact.buf-gen | `buf.gen.yaml` is generated as the buf/protoc code-generation configuration | | all | |
| api-http.grpc.proto.example-shape | The generated `.proto` follows the shape of the §26.13 example: `proto3`, a `package`, the entity message, `Get` / `List` / `Create` request and response messages, and a `service <T>Service` with the CRUD RPCs |  | all |  |
| api-http.grpc.service.translates-request | The Go service translates protobuf request messages into the unified client's input types | | all | |
| api-http.grpc.service.delegates | The Go service delegates to the unified client | | all | |
| api-http.grpc.service.converts-result | The Go service converts the client result back to protobuf response messages | | all | |
| api-http.grpc.field-mask | `FieldOptions` can be derived from `google.protobuf.FieldMask` to prevent overfetching, mirroring the GraphQL field-selection translation |  | all |  |
| api-http.grpc.metadata.cache-control | gRPC metadata `cache-control: no-cache` sets `CallOptions.SkipCache` | | all | |
| api-http.grpc.metadata.skip-events | gRPC metadata `x-skip-events: true` sets `CallOptions.SkipEvents` | | all | |
| api-http.grpc.metadata.skip-hooks | gRPC metadata `x-skip-hooks: true` sets `CallOptions.SkipHooks` | | all | |
