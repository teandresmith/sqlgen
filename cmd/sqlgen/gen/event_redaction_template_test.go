package gen_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
)

// redactedAccountTable is a fixture table with one redacted required column
// (password_hash — internal), one redacted omittable column (recovery_code —
// write_only), and public columns that must survive redaction untouched.
func redactedAccountTable() gen.TableContext {
	return gen.TableContext{
		StructName:        "Account",
		TableName:         "accounts",
		TableNameConstant: "TableAccounts",
		Schema:            "public",
		Operations: gen.ResolvedOperations{
			Create: true, CreateMany: true, Upsert: true,
			Update: true, UpdateMany: true, UpdateWhere: true,
			Increment: true,
		},
		Columns: []gen.ColumnContext{
			{Name: "id", Access: "public", APIReadable: true},
			{Name: "email", Access: "public", APIReadable: true},
			{Name: "password_hash", Access: "internal", EventRedacted: true},
			{Name: "recovery_code", Access: "write_only", EventRedacted: true},
			{Name: "internal_score", FieldName: "InternalScore", Access: "internal", EventRedacted: true},
			{Name: "view_count", FieldName: "ViewCount", Access: "public"},
		},
		IncrementColumns: []gen.ColumnContext{
			{Name: "internal_score", FieldName: "InternalScore", Access: "internal", EventRedacted: true},
			{Name: "view_count", FieldName: "ViewCount", Access: "public"},
		},
		CreateInputFields: []gen.InputFieldContext{
			{FieldName: "Email", ColumnName: "email", Required: true},
			{FieldName: "PasswordHash", ColumnName: "password_hash", Required: true},
			{FieldName: "RecoveryCode", ColumnName: "recovery_code", Omittable: true},
		},
		UpdateInputFields: []gen.InputFieldContext{
			{FieldName: "Email", ColumnName: "email", Omittable: true},
			{FieldName: "PasswordHash", ColumnName: "password_hash", Omittable: true},
			{FieldName: "RecoveryCode", ColumnName: "recovery_code", Omittable: true},
		},
	}
}

// §32.3 / §28.6: a table with write_only / internal columns emits per-table
// redact helpers, and every create/update-shaped op routes Event.Input
// through them — single-op, batch (i-th element), and *Where alike.
func TestEventHooks_redactedTable_emitsRedactHelpers(t *testing.T) {
	cfg := &config.RootConfig{}
	cfg.Events = &config.EventConfig{Enabled: true}

	ctx := gen.BuildEventHooksContext([]gen.TableContext{redactedAccountTable()}, cfg, "db", "Client", gotype.UUIDIntegrationFor(""))
	if ctx == nil {
		t.Fatalf("BuildEventHooksContext returned nil with events enabled")
	}
	if !ctx.HasRedactedTables {
		t.Errorf("HasRedactedTables = false, want true")
	}

	out := renderEventHooks(t, ctx)

	wants := []string{
		// The shared in-place zeroing helper.
		"func redactZero[T any](p *T) {",
		// Per-table helpers clear exactly the redacted fields.
		"func redactAccountCreateInput(in *CreateAccountInput) *CreateAccountInput {",
		"func redactAccountUpdateInput(in *UpdateAccountInput) *UpdateAccountInput {",
		"redactZero(&out.PasswordHash)",
		"redactZero(&out.RecoveryCode)",
		// Single-op create/upsert routes through the clone.
		"case hook.OpCreate, hook.OpUpsert:",
		"inputVal = redactAccountCreateInput(in)",
		// Batch create redacts the i-th element.
		"inputVal = redactAccountCreateInput(batchCreateInputs[i])",
		// UpdateMany redacts the item's inner input while keeping the
		// Update<T>Item concrete type for the subscriber type-switch.
		"item := batchUpdateItems[i]",
		"item.Input = redactAccountUpdateInput(item.Input)",
		"inputVal = item",
		// Update + UpdateWhere share the update-shape redaction.
		"case hook.OpUpdate, hook.OpUpdateWhere:",
		"inputVal = redactAccountUpdateInput(in)",
		// Increment blanks the delta only for redacted target columns.
		"case hook.OpIncrement:",
		"inputVal = redactAccountIncrementInput(in)",
		"func redactAccountIncrementInput(in IncrementInput[AccountIncrementColumn]) IncrementInput[AccountIncrementColumn] {",
		"case AccountIncrementInternalScore:",
		"in.Amount = 0",
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("redacted event-hook output missing %q\nfull output:\n%s", w, out)
		}
	}

	// Public fields must never be cleared.
	if strings.Contains(out, "redactZero(&out.Email)") {
		t.Errorf("redact helper clears public field Email\nfull output:\n%s", out)
	}
	// A public incrementable column must not be in the blanking case set.
	if strings.Contains(out, "AccountIncrementViewCount:") {
		t.Errorf("increment redaction blanks the public ViewCount column\nfull output:\n%s", out)
	}
	// The redacted single-op arms replace the default-path leak: the raw
	// mc.Input must not be published for create/update ops on this table.
	if strings.Contains(out, "inputVal = batchCreateInputs[i]\n") {
		t.Errorf("CreateMany publishes unredacted batch element\nfull output:\n%s", out)
	}
}

// §28.6 same-pointer guarantee: a table with no redacted column emits an
// event hook with no redaction machinery at all — no helpers, no redactZero,
// no clone; Event.Input stays the caller's pointer via the default arm. The
// E2E golden harness pins the byte-identity against committed goldens; this
// test pins the structural absence.
func TestEventHooks_unredactedTable_noRedactionMachinery(t *testing.T) {
	cfg := &config.RootConfig{}
	cfg.Events = &config.EventConfig{Enabled: true}

	table := redactedAccountTable()
	table.StructName = "Product"
	table.TableName = "products"
	table.TableNameConstant = "TableProducts"
	// Same shape, but nothing redacted.
	for i := range table.Columns {
		table.Columns[i].EventRedacted = false
		table.Columns[i].Access = "public"
	}
	for i := range table.IncrementColumns {
		table.IncrementColumns[i].EventRedacted = false
		table.IncrementColumns[i].Access = "public"
	}

	ctx := gen.BuildEventHooksContext([]gen.TableContext{table}, cfg, "db", "Client", gotype.UUIDIntegrationFor(""))
	if ctx == nil {
		t.Fatalf("BuildEventHooksContext returned nil with events enabled")
	}
	if ctx.HasRedactedTables {
		t.Errorf("HasRedactedTables = true, want false")
	}

	out := renderEventHooks(t, ctx)

	if strings.Contains(strings.ToLower(out), "redact") {
		t.Errorf("unredacted table output contains redaction machinery\nfull output:\n%s", out)
	}
	// The pre-§32 fanout arms must be intact: batch elements pass through
	// unwrapped and single-op/*Where ops flow through the default arm.
	wants := []string{
		"inputVal = batchCreateInputs[i]",
		"inputVal = batchUpdateItems[i]",
		"default:\n\t\t\t\t\tinputVal = mc.Input",
		"// mutations keep mc.Input unchanged.",
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("unredacted event-hook output missing %q\nfull output:\n%s", w, out)
		}
	}
}

// Ops gating: redacted fields without the corresponding input type (ops
// disabled) must not emit a helper referencing a type that does not exist.
func TestEventHooks_redactGating_opsDisabled(t *testing.T) {
	cfg := &config.RootConfig{}
	cfg.Events = &config.EventConfig{Enabled: true}

	table := redactedAccountTable()
	// Only update-shaped ops — the Create<T>Input type is never generated.
	table.Operations = gen.ResolvedOperations{Update: true, UpdateWhere: true}

	ctx := gen.BuildEventHooksContext([]gen.TableContext{table}, cfg, "db", "Client", gotype.UUIDIntegrationFor(""))
	if ctx == nil {
		t.Fatalf("BuildEventHooksContext returned nil")
	}

	out := renderEventHooks(t, ctx)

	if strings.Contains(out, "CreateAccountInput") {
		t.Errorf("update-only table references CreateAccountInput\nfull output:\n%s", out)
	}
	for _, w := range []string{
		"func redactAccountUpdateInput(",
		"case hook.OpUpdate, hook.OpUpdateWhere:",
	} {
		if !strings.Contains(out, w) {
			t.Errorf("update-only redacted table missing %q\nfull output:\n%s", w, out)
		}
	}
}

// filterableRedactedTable is redactedAccountTable plus the filter surface and
// the delete/restore ops that make the §32.3 filter leg reachable: the *Where
// deletes publish the caller's filter as Event.Input (§28.6 shape table), and
// the Go filter carries the restricted columns the API filter surface drops.
//
// internal_score is deliberately `hidden`, not `internal` — §32.2 gives hidden
// a pass-through event payload, so it must survive filter redaction untouched.
func filterableRedactedTable() gen.TableContext {
	t := redactedAccountTable()
	t.Operations.SoftDelete = true
	t.Operations.HardDelete = true
	t.Operations.Restore = true
	for i := range t.Columns {
		if t.Columns[i].Name == "internal_score" {
			t.Columns[i].Access = "hidden"
			t.Columns[i].EventRedacted = false
		}
	}
	for i := range t.IncrementColumns {
		if t.IncrementColumns[i].Name == "internal_score" {
			t.IncrementColumns[i].Access = "hidden"
			t.IncrementColumns[i].EventRedacted = false
		}
	}
	t.FilterFields = []gen.FilterFieldContext{
		{FieldName: "Email", ColumnName: "email", ComparatorType: "*comparator.String", Filterable: true},
		{FieldName: "InternalScore", ColumnName: "internal_score", ComparatorType: "*comparator.NullableNumber[int64]", Filterable: true},
		{FieldName: "PasswordHash", ColumnName: "password_hash", ComparatorType: "*comparator.String", Filterable: true},
		{FieldName: "RecoveryCode", ColumnName: "recovery_code", ComparatorType: "*comparator.NullableString", Filterable: true},
	}
	return t
}

// §32.3 filter leg: a table whose filter can carry a redacted column publishes
// a redacted clone of it on the *Where delete / restore ops, and its redaction
// switch fails closed so a later-added op cannot leak by omission.
func TestEventHooks_redactedFilter_emitsFilterRedactor(t *testing.T) {
	cfg := &config.RootConfig{}
	cfg.Events = &config.EventConfig{Enabled: true}

	ctx := gen.BuildEventHooksContext([]gen.TableContext{filterableRedactedTable()}, cfg, "db", "Client", gotype.UUIDIntegrationFor(""))
	if ctx == nil {
		t.Fatalf("BuildEventHooksContext returned nil with events enabled")
	}

	want := []gen.FilterRedactorContext{{
		StructName:     "Account",
		RedactedFields: []string{"PasswordHash", "RecoveryCode"},
	}}
	if diff := cmp.Diff(want, ctx.FilterRedactors); diff != "" {
		t.Errorf("FilterRedactors mismatch (-want +got):\n%s", diff)
	}

	out := renderEventHooks(t, ctx)

	wants := []string{
		"func redactAccountFilter(f *AccountFilter) *AccountFilter {",
		// Exactly the write_only / internal comparators are cleared.
		"out.PasswordHash = nil",
		"out.RecoveryCode = nil",
		// And / Or recurse — a nested member is otherwise a one-line bypass.
		"and := make([]*AccountFilter, len(out.And))",
		"and[i] = redactAccountFilter(sub)",
		"or := make([]*AccountFilter, len(out.Or))",
		"or[i] = redactAccountFilter(sub)",
		// All three *Where delete / restore ops route through the clone.
		"case hook.OpSoftDeleteWhere, hook.OpHardDeleteWhere, hook.OpRestoreWhere:",
		"inputVal = redactAccountFilter(in)",
		// The default arm no longer passes the raw input through.
		"// Fail closed:",
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("filter-redacted event-hook output missing %q\nfull output:\n%s", w, out)
		}
	}

	// §32.2: hidden and public columns keep a pass-through event payload.
	for _, field := range []string{"out.InternalScore = nil", "out.Email = nil"} {
		if strings.Contains(out, field) {
			t.Errorf("filter redactor clears non-redacted field: %q\nfull output:\n%s", field, out)
		}
	}
	// The leak this fix closes: the raw filter must no longer reach Event.Input.
	if strings.Contains(out, "default:\n\t\t\t\t\tinputVal = mc.Input") {
		t.Errorf("redacted table still has a pass-through default arm\nfull output:\n%s", out)
	}
}

// §32.3 closure propagation: a relationship member reaches another table's
// redacted comparators, so a parent with nothing redacted of its own still
// needs a helper — &ParentFilter{Child: &ChildFilter{Secret: …}} otherwise
// publishes the child's restricted value through the parent's event.
func TestEventHooks_filterRedactor_followsRelationshipMembers(t *testing.T) {
	cfg := &config.RootConfig{}
	cfg.Events = &config.EventConfig{Enabled: true}

	child := filterableRedactedTable()
	parent := gen.TableContext{
		StructName:        "Workspace",
		TableName:         "workspaces",
		TableNameConstant: "TableWorkspaces",
		Schema:            "public",
		Operations:        gen.ResolvedOperations{Create: true, HardDelete: true},
		Columns: []gen.ColumnContext{
			{Name: "id", Access: "public", APIReadable: true},
			{Name: "name", Access: "public", APIReadable: true},
		},
		FilterFields: []gen.FilterFieldContext{
			{FieldName: "Name", ColumnName: "name", ComparatorType: "*comparator.String", Filterable: true},
		},
		RelationshipFilters: []gen.RelationshipFilterContext{
			{FieldName: "Accounts", TargetStructName: "Account", TargetTable: "accounts", TargetSchema: "public"},
		},
	}

	ctx := gen.BuildEventHooksContext([]gen.TableContext{parent, child}, cfg, "db", "Client", gotype.UUIDIntegrationFor(""))
	if ctx == nil {
		t.Fatalf("BuildEventHooksContext returned nil")
	}

	want := []gen.FilterRedactorContext{
		{StructName: "Account", RedactedFields: []string{"PasswordHash", "RecoveryCode"}},
		{
			StructName:          "Workspace",
			RelationshipMembers: []gen.FilterRedactorMember{{FieldName: "Accounts", TargetStructName: "Account"}},
		},
	}
	if diff := cmp.Diff(want, ctx.FilterRedactors); diff != "" {
		t.Errorf("FilterRedactors mismatch (-want +got):\n%s", diff)
	}

	out := renderEventHooks(t, ctx)
	for _, w := range []string{
		"func redactWorkspaceFilter(f *WorkspaceFilter) *WorkspaceFilter {",
		"out.Accounts = redactAccountFilter(out.Accounts)",
		// The parent's own *Where delete publishes the redacted clone.
		"inputVal = redactWorkspaceFilter(in)",
	} {
		if !strings.Contains(out, w) {
			t.Errorf("relationship-closure output missing %q\nfull output:\n%s", w, out)
		}
	}
}

// The closure is not "every redacted table": a redacted column with no
// comparator never reaches a filter, and a helper nothing calls would be dead
// generated code. Both shapes must emit no filter redactor at all.
func TestEventHooks_filterRedactor_notEmittedWhenUnreachable(t *testing.T) {
	tests := []struct {
		name  string
		table func() gen.TableContext
	}{
		{
			// Redacted columns, *Where deletes — but no filter surface, so
			// nothing restricted can ride out on a filter.
			name: "redacted columns are not filterable",
			table: func() gen.TableContext {
				tc := filterableRedactedTable()
				tc.FilterFields = []gen.FilterFieldContext{
					{FieldName: "Email", ColumnName: "email", ComparatorType: "*comparator.String", Filterable: true},
				}
				return tc
			},
		},
		{
			// Filterable redacted columns, but no *Where delete op publishes a
			// filter and no parent points here — the helper would be uncalled.
			name: "no *Where delete op and no inbound member",
			table: func() gen.TableContext {
				tc := filterableRedactedTable()
				tc.Operations.SoftDelete = false
				tc.Operations.HardDelete = false
				tc.Operations.Restore = false
				return tc
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.RootConfig{}
			cfg.Events = &config.EventConfig{Enabled: true}

			ctx := gen.BuildEventHooksContext([]gen.TableContext{tt.table()}, cfg, "db", "Client", gotype.UUIDIntegrationFor(""))
			if ctx == nil {
				t.Fatalf("BuildEventHooksContext returned nil")
			}
			if len(ctx.FilterRedactors) != 0 {
				t.Errorf("FilterRedactors = %+v, want none", ctx.FilterRedactors)
			}
			out := renderEventHooks(t, ctx)
			if strings.Contains(out, "redactAccountFilter") {
				t.Errorf("emitted an uncalled filter redactor\nfull output:\n%s", out)
			}
		})
	}
}

// A table can redact an input while its redacted columns are not filterable —
// a jsonb[] column, or json on SQLite (§11.2). It then fails closed without
// joining the filter closure, and its *Where deletes must still publish the
// caller's filter: nil there is the §28.6 shape-table violation this fix
// rejected, and is indistinguishable from a single-row delete.
func TestEventHooks_failClosed_whereDeletesKeepTheirFilter(t *testing.T) {
	cfg := &config.RootConfig{}
	cfg.Events = &config.EventConfig{Enabled: true}

	table := redactedAccountTable()
	table.Operations.SoftDelete = true
	table.Operations.HardDelete = true
	table.Operations.Restore = true
	// Redacted in the create/update inputs, but with no filter surface at all.
	table.FilterFields = []gen.FilterFieldContext{
		{FieldName: "Email", ColumnName: "email", ComparatorType: "*comparator.String", Filterable: true},
	}

	ctx := gen.BuildEventHooksContext([]gen.TableContext{table}, cfg, "db", "Client", gotype.UUIDIntegrationFor(""))
	if ctx == nil {
		t.Fatalf("BuildEventHooksContext returned nil")
	}
	if len(ctx.FilterRedactors) != 0 {
		t.Errorf("FilterRedactors = %+v, want none — no redacted column is filterable", ctx.FilterRedactors)
	}
	if !ctx.Tables[0].FailClosedDefault {
		t.Error("FailClosedDefault = false, want true — the table redacts its inputs")
	}
	want := []string{"OpSoftDeleteWhere", "OpHardDeleteWhere", "OpRestoreWhere"}
	if diff := cmp.Diff(want, ctx.Tables[0].PassThroughOps); diff != "" {
		t.Errorf("PassThroughOps mismatch (-want +got):\n%s", diff)
	}

	out := renderEventHooks(t, ctx)
	if !strings.Contains(out, "case hook.OpSoftDeleteWhere, hook.OpHardDeleteWhere, hook.OpRestoreWhere:") {
		t.Errorf("*Where deletes are not enumerated, so a fail-closed default publishes nil for them\nfull output:\n%s", out)
	}
}

// A fail-closed default must not swallow an op whose §28.6 payload is real but
// carries nothing to redact on this table — OpIncrement on a table whose
// redacted columns are not incrementable would otherwise degrade to nil and
// contradict the shape table's `Increment | IncrementInput[…]` row.
func TestEventHooks_failClosed_keepsUnredactedOpsExplicit(t *testing.T) {
	cfg := &config.RootConfig{}
	cfg.Events = &config.EventConfig{Enabled: true}

	table := filterableRedactedTable()
	// Redacted columns exist, but none of them is incrementable and neither
	// input shape carries one, so only the filter leg redacts anything.
	table.IncrementColumns = []gen.ColumnContext{{Name: "view_count", FieldName: "ViewCount", Access: "public"}}
	table.CreateInputFields = []gen.InputFieldContext{{FieldName: "Email", ColumnName: "email", Required: true}}
	table.UpdateInputFields = []gen.InputFieldContext{{FieldName: "Email", ColumnName: "email", Omittable: true}}

	ctx := gen.BuildEventHooksContext([]gen.TableContext{table}, cfg, "db", "Client", gotype.UUIDIntegrationFor(""))
	if ctx == nil {
		t.Fatalf("BuildEventHooksContext returned nil")
	}
	want := []string{"OpCreate", "OpUpsert", "OpUpdate", "OpUpdateWhere", "OpIncrement"}
	if diff := cmp.Diff(want, ctx.Tables[0].PassThroughOps); diff != "" {
		t.Errorf("PassThroughOps mismatch (-want +got):\n%s", diff)
	}

	out := renderEventHooks(t, ctx)
	for _, w := range []string{
		"case hook.OpCreate, hook.OpUpsert, hook.OpUpdate, hook.OpUpdateWhere, hook.OpIncrement:",
		"// Fail closed:",
	} {
		if !strings.Contains(out, w) {
			t.Errorf("fail-closed output missing %q\nfull output:\n%s", w, out)
		}
	}
}
