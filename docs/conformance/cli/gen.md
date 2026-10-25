# CLI Requirements: Code Generation

<!-- seeding: in-progress -->
<!-- packages: cmd/sqlgen/gen -->
<!-- matrix: naming rows=rule cols=struct,field,type-name,file-stem -->
<!-- matrix: generated-artifacts rows=artifact cols=single-pk,composite-pk,no-pk,soft-delete,tenanted,view,materialized-view -->
<!-- matrix: relationships rows=type cols=struct-field,loader,filter,nested-mutation -->
<!-- matrix: access-control rows=role cols=go-client,api-in,api-out,filter,sort,event-payload,cache,manifest -->
<!-- matrix: api-generation rows=element cols=single-pk,composite-pk,no-pk,soft-delete,tenanted,view -->

## PRD gaps

Behavior the code has, most of it pinned by tests, that no PRD text states. Each gap waits for
the user's ruling: amend the PRD, which turns the gap into requirement rows, or drop the pins.

| Gap | Behavior | Tests | Section | Ruling |
|---|---|---|---|---|
| gap.gen.nested-eligibility-reported-once | Under `sqlgen validate`, a listed ineligible nested-mutation edge is reported once, not twice | `unit: cmd/sqlgen/gen: TestNestedEligibility_ValidateReportsEachViolationOnce` | §4.13 | |
