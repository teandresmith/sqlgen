# Runtime Requirements

<!-- seeding: not-started -->
<!-- packages: sql comparator omittable database database/mock database/pgx database/stdlib cache cache/memory cache/redis cache/msgpack tenancy event event/memorybus event/natsbus hook types manifest metrics/otel -->
<!-- matrix: sql-builders rows=builder cols=basic,returning,empty-input,composite-key,quoted-identifier -->
<!-- matrix: comparators rows=operator cols=value,nil,empty-list,escaping -->
<!-- matrix: condition-composition rows=shape cols=placeholder-numbering,nesting -->
<!-- matrix: lock-modes rows=mode cols=postgres,mysql,sqlite -->
<!-- matrix: transactions rows=feature cols=pgx,stdlib-postgres,mysql,sqlite -->
<!-- matrix: error-mapping rows=error-class cols=pgx,stdlib-postgres,mysql,sqlite -->
<!-- matrix: cache-keys rows=key-shape cols=single-pk,composite-pk,schema,no-schema,tenanted -->
<!-- matrix: hooks rows=hook-point cols=fires,order,skip,panic-recovery -->
<!-- matrix: omittable rows=state cols=value,json-round-trip -->

No requirements yet. See [README.md](README.md) for the format.
