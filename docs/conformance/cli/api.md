# CLI Requirements: API Generation

<!-- seeding: in-progress -->
<!-- matrix: api-generation rows=element cols=single-pk,composite-pk,no-pk,soft-delete,tenanted,view -->

API generation (§26) is emitted by `cmd/sqlgen/gen`, whose orphan check stays with [`gen.md`](gen.md), so this file lists no package of its own. Its rows claim `gen`, `cli`, golden and example tests.
