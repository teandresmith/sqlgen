package gen_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
)

// buildStreamTableContext builds a TableContext suitable for exercising the
// stream template. It mirrors the lock-mode test fixture but ensures
// Operations.Stream is on by default, with a relationships field set so we
// can prove the StreamFieldOptions struct never carries relationship fields
// even when the regular FieldOptions does. Stream emission does not branch on
// dialect, so the fixture is fixed to postgres — dialect-specific stream
// behavior (none, currently) would warrant a per-dialect variant.
func buildStreamTableContext() gen.TableContext {
	ctx := buildLockGuardTableContext("postgres")
	ctx.Operations.Stream = true
	ctx.Relationships = []gen.RelationshipContext{
		{
			Name:             "company",
			TargetTable:      "companies",
			TargetStructName: "Company",
			FieldName:        "Company",
			GoType:           "*Company",
			JSONTag:          "company",
		},
	}
	return ctx
}

// TestStream_TypesEmitted pins the Stream-method-side type emissions:
// Stream{Table}Input has Filter + Sorts only (no Limit/Offset, no relationships),
// Stream{Table}FieldOptions has scalar bool fields only (no relationship fields),
// and the FieldOptions methods (Columns, HasSelectedColumns) are emitted.
func TestStream_TypesEmitted(t *testing.T) {
	tmpl := loadTableTemplates(t, "stream.go.tmpl")
	ctx := buildStreamTableContext()
	out := renderTableTemplate(t, tmpl, "table/stream", ctx)

	mustContainAll(
		t, out,
		"type StreamProductsInput struct {",
		"Filter *ProductFilter",
		"Sorts  []sql.Sort",
		"type StreamProductFieldOptions struct {",
		`ID bool `+"`json:\"id\"`",
		`Name bool `+"`json:\"name\"`",
		"func (fo *StreamProductFieldOptions) Columns() []string {",
		"slices.Sort(cols)",
		"func (fo *StreamProductFieldOptions) HasSelectedColumns() bool {",
	)
}

// TestStream_InputHasNoLimitOrOffset enforces the type-level constraint
// from PRD §9.4a: StreamXInput must not contain Limit / Offset fields.
// A drift adding them would silently re-enable bounded reads through Stream.
func TestStream_InputHasNoLimitOrOffset(t *testing.T) {
	tmpl := loadTableTemplates(t, "stream.go.tmpl")
	ctx := buildStreamTableContext()
	out := renderTableTemplate(t, tmpl, "table/stream", ctx)

	// Scope to the StreamProductsInput type body to avoid false positives
	// from method bodies that legitimately mention "Limit" elsewhere.
	const startMarker = "type StreamProductsInput struct {"
	start := strings.Index(out, startMarker)
	if start < 0 {
		t.Fatalf("StreamProductsInput type missing from output:\n%s", out)
	}
	end := strings.Index(out[start:], "}")
	if end < 0 {
		t.Fatalf("StreamProductsInput type body unterminated")
	}
	body := out[start : start+end]
	for _, forbidden := range []string{"Limit", "Offset"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("StreamProductsInput must not contain %q field; body:\n%s", forbidden, body)
		}
	}
}

// TestStream_FieldOptionsScalarOnly enforces the topology constraint:
// StreamXFieldOptions cannot represent relationship fields. A drift that
// included relationships here would silently allow JOIN loading on Stream,
// breaking the §9.4a contract.
func TestStream_FieldOptionsScalarOnly(t *testing.T) {
	tmpl := loadTableTemplates(t, "stream.go.tmpl")
	ctx := buildStreamTableContext()
	out := renderTableTemplate(t, tmpl, "table/stream", ctx)

	const startMarker = "type StreamProductFieldOptions struct {"
	start := strings.Index(out, startMarker)
	if start < 0 {
		t.Fatalf("StreamProductFieldOptions type missing from output:\n%s", out)
	}
	end := strings.Index(out[start:], "}")
	if end < 0 {
		t.Fatalf("StreamProductFieldOptions type body unterminated")
	}
	body := out[start : start+end]
	for _, forbidden := range []string{
		"Company",              // relationship field name
		"*CompanyFieldOptions", // O2O relationship type
		"RelationshipOptions",  // O2M / M2M relationship type
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("StreamProductFieldOptions must not contain relationship reference %q; body:\n%s", forbidden, body)
		}
	}
}

// TestStream_MethodBodyEmission pins the Stream method shape: returns
// iter.Seq2, forces SkipCache, references hook.OpStream, calls scan{Table}Row,
// and yields rows in the iterator closure.
func TestStream_MethodBodyEmission(t *testing.T) {
	tmpl := loadTableTemplates(t, "stream.go.tmpl")
	ctx := buildStreamTableContext()
	out := renderTableTemplate(t, tmpl, "table/stream", ctx)

	mustContainAll(
		t, out,
		"func (c *productClient) Stream(",
		"iter.Seq2[*Product, error]",
		"options.SkipCache = true",
		"hook.OpStream",
		"scanProductRow(rows, columns)",
		"for rows.Next() {",
		"if !emit(",
	)
}

// TestStream_NoLimitOffsetInSelect ensures the generated BuildSelect call
// inside Stream does NOT pass Limit or Offset. The streaming SELECT must be
// unbounded — all client-side memory bounds come from the iterator pattern.
func TestStream_NoLimitOffsetInSelect(t *testing.T) {
	tmpl := loadTableTemplates(t, "stream.go.tmpl")
	ctx := buildStreamTableContext()
	out := renderTableTemplate(t, tmpl, "table/stream", ctx)

	// Locate the BuildSelect call and inspect the literal that follows.
	const marker = "sql.BuildSelect(c.dialect, c.table, sql.SelectOptions{"
	start := strings.Index(out, marker)
	if start < 0 {
		t.Fatalf("Stream method body missing BuildSelect call:\n%s", out)
	}
	end := strings.Index(out[start:], "})")
	if end < 0 {
		t.Fatalf("Stream BuildSelect literal unterminated")
	}
	body := out[start : start+end]
	for _, forbidden := range []string{"Limit:", "Offset:", "LockMode:"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("Stream BuildSelect literal must not include %q; body:\n%s", forbidden, body)
		}
	}
}

// TestStream_OpStreamReferenced confirms hook.OpStream is the Op constant
// passed into the QueryContext. A drift to OpGetMany would silently route
// stream calls through the wrong hook dispatch path.
func TestStream_OpStreamReferenced(t *testing.T) {
	tmpl := loadTableTemplates(t, "stream.go.tmpl")
	ctx := buildStreamTableContext()
	out := renderTableTemplate(t, tmpl, "table/stream", ctx)

	if !strings.Contains(out, "Op: hook.OpStream") {
		t.Errorf("Stream QueryContext must use hook.OpStream; output:\n%s", out)
	}
}

// TestStream_ScanRowHelperEmitted pins the scan{Table}Row helper shape:
// single-row variant of scan{Table}s, takes (rows, columns), returns
// (*Entity, error). Stream depends on this helper to yield one row at a time.
func TestStream_ScanRowHelperEmitted(t *testing.T) {
	tmpl := loadTableTemplates(t, "stream.go.tmpl")
	ctx := buildStreamTableContext()
	out := renderTableTemplate(t, tmpl, "table/stream", ctx)

	mustContainAll(
		t, out,
		"func scanProductRow(rows database.Rows, columns []string) (*Product, error) {",
		`case "id":`,
		`case "name":`,
		"if err := rows.Scan(targets...); err != nil {",
		"return p, nil",
	)
	mustNotContain(
		t, out,
		"for rows.Next() {\n\t\tp := &Product{}", // scan{Table}Row must NOT loop
	)
}

// TestStream_OperationsStreamFalseSuppresses confirms operations.stream:false
// elides every Stream-related emission: no Stream method, no input type, no
// field-options type, no scan{Table}Row helper.
func TestStream_OperationsStreamFalseSuppresses(t *testing.T) {
	tmpl := loadTableTemplates(t, "stream.go.tmpl")
	ctx := buildStreamTableContext()
	ctx.Operations.Stream = false
	out := renderTableTemplate(t, tmpl, "table/stream", ctx)

	for _, forbidden := range []string{
		"StreamProductsInput",
		"StreamProductFieldOptions",
		"scanProductRow",
		"iter.Seq2",
		"hook.OpStream",
	} {
		if strings.Contains(out, forbidden) {
			t.Errorf("operations.stream:false must suppress %q; output:\n%s", forbidden, out)
		}
	}
}

// TestStream_TenancyAndSoftDeleteApplied verifies the streaming SELECT picks
// up the same tenancy + soft-delete WHERE injections that GetMany applies.
// Stream rides the same hook chain (OpStream gets condition-injecting hooks),
// but the in-method composition must also fire so cross-tenant reads are
// safely scoped without relying on out-of-band hook configuration.
func TestStream_TenancyAndSoftDeleteApplied(t *testing.T) {
	tmpl := loadTableTemplates(t, "stream.go.tmpl")
	ctx := buildStreamTableContext()
	ctx.SoftDelete = &gen.SoftDeleteContext{Column: "deleted_at", FieldName: "DeletedAt", Strategy: "timestamp"}
	ctx.ExcludeDeleted = true
	ctx.Tenancy = &gen.TableTenancyContext{
		Tenanted:  true,
		Column:    "workspace_id",
		FieldName: "WorkspaceID",
		GoType:    "string",
	}
	out := renderTableTemplate(t, tmpl, "table/stream", ctx)

	mustContainAll(
		t, out,
		"if c.excludeDeleted",
		`sql.Where(c.dialect.QuoteIdentifier("deleted_at")).IsNull()`,
		"if !options.SkipTenancy {",
		`sql.Where(c.dialect.QuoteIdentifier("workspace_id")).Eq(resolvedTenant)`,
	)
}

// TestStream_InTransactionGuardEmitted pins the PRD §9.4a precondition: Stream
// refuses an active transaction unless CallOptions.AllowInTransaction is set.
// The generated error must name both the alternative (Connection in a loop) and
// the obligation the opt-in commits the caller to.
func TestStream_InTransactionGuardEmitted(t *testing.T) {
	tmpl := loadTableTemplates(t, "stream.go.tmpl")
	ctx := buildStreamTableContext()
	out := renderTableTemplate(t, tmpl, "table/stream", ctx)

	mustContainAll(
		t, out,
		"if database.InTransaction(ctx) && !options.AllowInTransaction {",
		`yield(nil, fmt.Errorf("stream products: unsupported inside a transaction — use Connection in a loop, "+`,
		`"or set AllowInTransaction for a snapshot-consistent stream that issues nothing else on this txCtx"))`,
	)

	// The rule travels on the method's own doc comment, so a consumer reads it
	// at the call site rather than in the PRD.
	docEnd := strings.Index(out, "func (c *productClient) Stream(")
	if docEnd < 0 {
		t.Fatalf("Stream method missing from output:\n%s", out)
	}
	if !strings.Contains(out[:docEnd], "// CallOptions.AllowInTransaction") {
		t.Errorf("Stream doc comment does not carry the AllowInTransaction rule; doc:\n%s", out[:docEnd])
	}
}

// TestStream_GuardPrecedesHookChain enforces the half of §9.4a that makes the
// refusal observable: the guard returns before the hook chain is built, so a
// refused Stream issues no SQL and fires no hooks at all — global authorization
// included. Placing it inside the executeQuery closure would still refuse the
// call, but only after the chain had run.
func TestStream_GuardPrecedesHookChain(t *testing.T) {
	tmpl := loadTableTemplates(t, "stream.go.tmpl")
	ctx := buildStreamTableContext()
	out := renderTableTemplate(t, tmpl, "table/stream", ctx)

	guard := strings.Index(out, "if database.InTransaction(ctx) && !options.AllowInTransaction {")
	executor := strings.Index(out, "c.executeQuery(ctx, options.SkipHooks,")
	resolve := strings.Index(out, "options := resolveCallOptions(opts)")
	if guard < 0 || executor < 0 || resolve < 0 {
		t.Fatalf("guard=%d executor=%d resolve=%d; output:\n%s", guard, executor, resolve, out)
	}
	if guard < resolve {
		t.Error("in-transaction guard renders before resolveCallOptions, so it reads an unresolved AllowInTransaction")
	}
	if guard > executor {
		t.Error("in-transaction guard renders after executeQuery, so a refused Stream would still fire the hook chain")
	}
}
