package tests

// TestWrapperRewriter pins the post-gqlgen panic-stub rewriter that
// converts gqlgen-emitted `panic("not implemented: …")` stubs into
// delegations into the sqlgenresolver helper sub-package.
//
// The regression this guards: when an existing sqlgen-managed table
// gains a new operation between two `sqlgen graphql gen` runs (e.g.
// flipping `api.operations` from `read_only` to include soft-delete),
// sqlgen's seed step skips files that already exist on disk (gqlgen
// owns them after the first run). gqlgen then sees a schema field
// without a method body and emits a panic stub. Without the rewriter,
// the package compiles but `softDelete<Table>(...)` panics at runtime.
// The rewriter closes that gap by AST-rewriting the panic body into
// `return r.M.SoftDelete<Table>(...)` (the same delegation shape the
// seed step would have written on a clean run).
//
// **Why no live-handler query:** the end-to-end shape would query the
// newly-added softDelete<Table> mutation via the live GraphQL handler.
// The example's main_test.go boots a live handler bound to the IN-TREE
// example's models package (because Go's runtime loader cannot re-bind
// to a separately-staged copy of the same import path); staging a fresh
// consumer fixture into t.TempDir produces a different filesystem
// location but the SAME import path, so the test process cannot import
// the staged resolver/handler at runtime. Booting a second live handler
// against the staged fixture would require spawning a subprocess
// `main.go` that runs an HTTP server, which adds substantial harness
// machinery for a check that's already pinned by simpler artifact-level
// inspection:
//
//	1. After the read_only → soft-delete-enabled flip, the per-table
//	   `*_gen.graphqls` for that table declares `softDelete<Table>`.
//	2. The corresponding resolver method body in
//	   `*_gen.resolvers.go` is the rewriter's delegation shape
//	   (`return r.M.SoftDelete<Table>(...)`), NOT the panic-stub
//	   shape (`panic("not implemented: …")`).
//	3. `go build ./...` succeeds against the staged fixture (the
//	   delegation has the right argument shape — a regression in
//	   the rewriter's signature handling would surface as a
//	   compile error).
//
// Together these three checks prove the rewriter fired correctly. The
// runtime correctness of `r.M.SoftDelete<Table>` itself is covered by
// the existing curated_surface_test.go's soft-delete tests (which run
// against the in-tree fixture where the operation is enabled from the
// outset). The rewriter-specific concern — does the panic-stub →
// delegation flip happen on the second run — is artifact-level
// verifiable, so the dedicated handler boot is omitted.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWrapperRewriter_PanicStubFlipToDelegation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping wrapper rewriter test in short mode")
	}

	stage := stageGraphQLExample(t)

	// Target table: `events` — has no soft-delete column in the
	// example schema, so we use `products` instead. Products has the
	// `deleted_at` column wired and the example's checked-in config
	// already enables soft_delete + restore on it. We need a table
	// that:
	//   1. Has a `deleted_at` column (so soft-delete is reachable)
	//   2. Is currently writing seeded resolver files we can later
	//      delete + force gqlgen to re-emit panic stubs for
	//
	// The whole-table flip approach uses `api.operations` at the
	// table level to scope down to read_only, run sqlgen graphql gen,
	// then flip to soft-delete-enabled and re-run. We assert the
	// flipped run's resolver file has delegation bodies (not panic
	// stubs) for the newly-added mutation.
	//
	// `orders` is the chosen table — it has soft-delete columns, no
	// composite PK complications, and a manageable column set so the
	// failure messages stay readable.
	const targetTable = "orders"  // DB table name (plural) — keys sqlgen.yml `tables:` block
	const targetStruct = "Order"  // PascalCase struct name — half of the resolver method names
	const targetSnakeFn = "order" // singular snake — filename stem for `<stem>_gen.resolvers.go`

	// Step 1: scope `orders` to read_only and re-run. The
	// products_gen.resolvers.go etc. are left alone; only orders is
	// affected. The full `sqlgen generate` command regenerates the
	// per-table `*_gen.graphqls` (so `order_gen.graphqls` loses the
	// soft-delete mutation), then chains into `sqlgen graphql gen`,
	// which writes the seed + invokes gqlgen + runs
	// the rewriter. We use `sqlgen generate` (not bare
	// `sqlgen graphql gen`) because operation scoping has to flow
	// through schema regeneration to take effect.
	//
	// Implementation note: the example's seeded resolver files
	// already include soft-delete operations for `orders`. To start
	// from the "read-only seeded" state, we delete the existing
	// `order_gen.resolvers.go` file so the seed step will rewrite it
	// from scratch under the read_only constraint.
	graphDir := filepath.Join(stage, "models", "graph")
	if err := os.Remove(filepath.Join(graphDir, targetSnakeFn+"_gen.resolvers.go")); err != nil {
		t.Fatalf("removing existing %s resolver: %v", targetTable, err)
	}
	addTableReadOnly(t, filepath.Join(stage, "sqlgen.yml"), targetTable)

	if out, err := runSqlgen(t, stage, "generate", "--config", "./sqlgen.yml"); err != nil {
		t.Fatalf("first `sqlgen graphql gen` (read_only) failed: %v\n%s", err, out)
	}

	// Verify the read_only run produced a resolver file WITHOUT a
	// SoftDelete<Table> method. If this fails, the test setup is wrong
	// — we should not proceed to the flip until the baseline is
	// confirmed.
	resolverPath := filepath.Join(graphDir, targetSnakeFn+"_gen.resolvers.go")
	roBody := string(readFile(t, resolverPath))
	softDeleteFn := "SoftDelete" + targetStruct
	if strings.Contains(roBody, "func (r *mutationResolver) "+softDeleteFn+"(") {
		t.Fatalf("baseline read_only run unexpectedly emitted SoftDelete%s in %s — test setup is wrong; resolver body:\n%s", targetStruct, resolverPath, roBody)
	}

	// Step 2: flip operations to allow soft-delete (preset read_only
	// + explicit soft_delete: true + restore: true) and re-run.
	// The existing `order_gen.resolvers.go` is now gqlgen-owned —
	// sqlgen's seed step will skip it. gqlgen will see the new
	// soft_delete field in the regenerated schema and emit panic
	// stubs. The post-gqlgen rewriter MUST convert those to
	// delegations.
	flipTableToAllowSoftDelete(t, filepath.Join(stage, "sqlgen.yml"), targetTable)

	if out, err := runSqlgen(t, stage, "generate", "--config", "./sqlgen.yml"); err != nil {
		t.Fatalf("second `sqlgen graphql gen` (post-flip) failed: %v\n%s", err, out)
	}

	// Assertion 1: the per-table schema gained the soft-delete field.
	schemaPath := filepath.Join(graphDir, targetSnakeFn+"_gen.graphqls")
	schemaBody := string(readFile(t, schemaPath))
	wantSchemaField := "softDelete" + targetStruct + "("
	if !strings.Contains(schemaBody, wantSchemaField) {
		t.Errorf("expected schema field %q in %s after flip, body:\n%s", wantSchemaField, schemaPath, schemaBody)
	}

	// Assertion 2: the resolver file has the rewriter's delegation
	// body for SoftDelete<Table>, NOT a panic stub. The check is
	// structural — the rewriter replaces a `panic(...)` block with a
	// `return r.M.<Field>(...)` block. We look for the receiver call
	// inside the function body to confirm.
	roBody = string(readFile(t, resolverPath))
	wantFnDecl := "func (r *mutationResolver) " + softDeleteFn + "("
	if !strings.Contains(roBody, wantFnDecl) {
		t.Errorf("expected resolver method %q in %s after flip, body:\n%s", wantFnDecl, resolverPath, roBody)
	}
	if !strings.Contains(roBody, "r.M."+softDeleteFn+"(") {
		t.Errorf("expected delegation `r.M.%s(...)` in %s — panic-stub rewriter did NOT fire", softDeleteFn, resolverPath)
	}
	// Negative check: no panic stub for the rewritten method (other
	// consumer-authored panics in the file would still be present,
	// but the example has none).
	bodyLines := strings.Split(roBody, "\n")
	inSoftDeleteFn := false
	braceDepth := 0
	for _, line := range bodyLines {
		if strings.Contains(line, wantFnDecl) {
			inSoftDeleteFn = true
		}
		if inSoftDeleteFn {
			braceDepth += strings.Count(line, "{") - strings.Count(line, "}")
			if strings.Contains(line, `panic("not implemented`) || strings.Contains(line, "panic(fmt.Errorf") {
				t.Errorf("rewriter left a panic stub in SoftDelete%s body: %q", targetStruct, line)
			}
			if braceDepth <= 0 {
				inSoftDeleteFn = false
			}
		}
	}

	// Assertion 3: the staged fixture compiles. A regression in the
	// rewriter's argument-list reconstruction (e.g. wrong number of
	// args, mis-typed receiver) would surface here as a compile error.
	if out, err := runGoBuildAll(t, stage); err != nil {
		t.Fatalf("`go build ./...` failed after flip: %v\n%s", err, out)
	}
}

// addTableReadOnly inserts a `tables.<name>.api.operations: read_only` entry
// into the staged sqlgen.yml. The example's existing `tables:` block
// already has entries for some tables; we append a new sub-mapping
// without disturbing those.
func addTableReadOnly(t *testing.T, path, table string) {
	t.Helper()
	data := readFile(t, path)
	body := string(data)
	// Append a new table entry under the existing tables: block. The
	// example's checked-in sqlgen.yml ends with `events:` overrides;
	// adding a new entry at the end keeps the YAML structure intact.
	if !strings.Contains(body, "\ntables:\n") {
		t.Fatalf("addTableReadOnly: no `tables:` block in %s", path)
	}
	// Find the trailing `api:` block and inject the table entry just
	// before it — keeping `tables:` contiguous.
	apiIdx := strings.Index(body, "\napi:\n")
	if apiIdx < 0 {
		t.Fatalf("addTableReadOnly: no `api:` block in %s", path)
	}
	injection := "  " + table + ":\n    api:\n      operations: read_only\n"
	updated := body[:apiIdx+1] + injection + body[apiIdx+1:]
	if err := os.WriteFile(path, []byte(updated), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

// flipTableToAllowSoftDelete rewrites the table's `api.operations: read_only`
// entry to enable hard_delete + soft_delete + restore. Both delete ops
// must be on to produce the disambiguated `softDelete<Table>` mutation
// name the spec asserts on — with only one enabled the schema template
// (gen/templates/api/schema.graphqls.tmpl §line 124-128) emits the
// unqualified `delete<Table>` instead. The flip stays scoped to PK-only
// signatures (`hardDelete<T>`, `softDelete<T>`, `restore<T>`) so the
// rewriter's verbatim arg-list copy is sufficient — Create/Update
// mutations would require gqlgen-input-to-model translation calls the
// rewriter doesn't synthesize, so they're deliberately not added.
func flipTableToAllowSoftDelete(t *testing.T, path, table string) {
	t.Helper()
	data := readFile(t, path)
	body := string(data)
	const need = "api:\n      operations: read_only"
	tableHeader := "\n  " + table + ":\n    " + need + "\n"
	if !strings.Contains(body, tableHeader) {
		t.Fatalf("flipTableToAllowSoftDelete: cannot find `%s` block in %s", tableHeader, path)
	}
	replacement := "\n  " + table + ":\n    api:\n      operations:\n        preset: read_only\n        soft_delete: true\n        hard_delete: true\n        restore: true\n"
	updated := strings.Replace(body, tableHeader, replacement, 1)
	if err := os.WriteFile(path, []byte(updated), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}
