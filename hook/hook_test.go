package hook_test

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/hook"
)

func TestMutationOpConstants(t *testing.T) {
	want := []hook.MutationOp{
		"create",
		"create_many",
		"update",
		"update_many",
		"update_where",
		"upsert",
		"upsert_many",
		"soft_delete",
		"soft_delete_many",
		"soft_delete_where",
		"restore",
		"restore_many",
		"restore_where",
		"hard_delete",
		"hard_delete_many",
		"hard_delete_where",
		"increment",
	}

	got := []hook.MutationOp{
		hook.OpCreate,
		hook.OpCreateMany,
		hook.OpUpdate,
		hook.OpUpdateMany,
		hook.OpUpdateWhere,
		hook.OpUpsert,
		hook.OpUpsertMany,
		hook.OpSoftDelete,
		hook.OpSoftDeleteMany,
		hook.OpSoftDeleteWhere,
		hook.OpRestore,
		hook.OpRestoreMany,
		hook.OpRestoreWhere,
		hook.OpHardDelete,
		hook.OpHardDeleteMany,
		hook.OpHardDeleteWhere,
		hook.OpIncrement,
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("MutationOp constants mismatch (-want +got):\n%s", diff)
	}
}

func TestQueryOpConstants(t *testing.T) {
	want := []hook.QueryOp{
		"get",
		"get_many",
		"exists",
		"count",
		"paginate",
		"connection",
		"stream",
		"refresh",
	}

	got := []hook.QueryOp{
		hook.OpGet,
		hook.OpGetMany,
		hook.OpExists,
		hook.OpCount,
		hook.OpPaginate,
		hook.OpConnection,
		hook.OpStream,
		hook.OpRefresh,
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("QueryOp constants mismatch (-want +got):\n%s", diff)
	}
}

func TestMutationHookSignature(t *testing.T) {
	// Verify the middleware pattern compiles: hook wraps handler to produce handler.
	called := false
	var h hook.MutationHook = func(next hook.MutationHandler) hook.MutationHandler {
		return func(ctx context.Context, m *hook.MutationContext) (any, error) {
			called = true
			return next(ctx, m)
		}
	}

	terminal := func(_ context.Context, _ *hook.MutationContext) (any, error) {
		return "result", nil
	}

	wrapped := h(terminal)
	got, err := wrapped(context.Background(), &hook.MutationContext{
		Op:    hook.OpCreate,
		Table: "products",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("hook was not called")
	}
	if got != "result" {
		t.Errorf("got %v, want %q", got, "result")
	}
}

func TestQueryHookSignature(t *testing.T) {
	// Verify the middleware pattern compiles: hook wraps handler to produce handler.
	called := false
	var h hook.QueryHook = func(next hook.QueryHandler) hook.QueryHandler {
		return func(ctx context.Context, q *hook.QueryContext) (any, error) {
			called = true
			return next(ctx, q)
		}
	}

	terminal := func(_ context.Context, _ *hook.QueryContext) (any, error) {
		return 42, nil
	}

	wrapped := h(terminal)
	got, err := wrapped(context.Background(), &hook.QueryContext{
		Op:    hook.OpGet,
		Table: "users",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("hook was not called")
	}
	if got != 42 {
		t.Errorf("got %v, want %d", got, 42)
	}
}

// pinAny compiles only when every argument points at a field declared as any.
func pinAny(_ ...*any) {}

func TestMutationContextFields(t *testing.T) {
	// The positional literal below fails to compile if a field is renamed or a
	// named-type field changes type; the pointer assignments pin the any fields,
	// which accept any value.
	type fields struct {
		Op              hook.MutationOp
		Table           hook.TableName
		Schema          string
		PK              any
		Input           any
		CallOptions     any
		AffectedPKs     []any
		AffectedTenants []any
		Tenant          any
	}
	m := &hook.MutationContext{
		Op:              hook.OpUpdate,
		Table:           "public.products",
		Schema:          "public",
		PK:              123,
		Input:           "some-input",
		CallOptions:     "some-options",
		AffectedPKs:     []any{123},
		AffectedTenants: []any{"acme"},
		Tenant:          "acme",
	}

	pinAny(&m.PK, &m.Input, &m.CallOptions, &m.Tenant)
	// An unkeyed literal of the struct itself fails to compile if a field is
	// added or removed.
	_ = hook.MutationContext{hook.OpUpdate, "", "", nil, nil, nil, nil, nil, nil}

	got := fields{m.Op, m.Table, m.Schema, m.PK, m.Input, m.CallOptions, m.AffectedPKs, m.AffectedTenants, m.Tenant}
	want := fields{hook.OpUpdate, "public.products", "public", 123, "some-input", "some-options", []any{123}, []any{"acme"}, "acme"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("MutationContext fields mismatch (-want +got):\n%s", diff)
	}
}

func TestMutationContextAffectedPKs(t *testing.T) {
	m := &hook.MutationContext{
		Op:    hook.OpCreate,
		Table: "products",
	}

	// Zero value — nil slice
	if m.AffectedPKs != nil {
		t.Errorf("AffectedPKs zero value = %v, want nil", m.AffectedPKs)
	}

	// Terminal populates after successful mutation
	m.AffectedPKs = []any{42}
	if len(m.AffectedPKs) != 1 {
		t.Fatalf("AffectedPKs length = %d, want 1", len(m.AffectedPKs))
	}
	if m.AffectedPKs[0] != 42 {
		t.Errorf("AffectedPKs[0] = %v, want 42", m.AffectedPKs[0])
	}

	// Batch mutation — multiple PKs
	m.AffectedPKs = []any{1, 2, 3}
	if diff := cmp.Diff([]any{1, 2, 3}, m.AffectedPKs); diff != "" {
		t.Errorf("AffectedPKs mismatch (-want +got):\n%s", diff)
	}
}

func TestMutationContextAffectedPKsVisibleToOuterHook(t *testing.T) {
	// Verify that the terminal setting AffectedPKs is visible to the outer hook
	// (the event hook pattern).
	var capturedPKs []any

	outerHook := func(next hook.MutationHandler) hook.MutationHandler {
		return func(ctx context.Context, m *hook.MutationContext) (any, error) {
			result, err := next(ctx, m)
			if err != nil {
				return result, err
			}
			capturedPKs = m.AffectedPKs
			return result, nil
		}
	}

	terminal := func(_ context.Context, m *hook.MutationContext) (any, error) {
		m.AffectedPKs = []any{10, 20, 30}
		return "ok", nil
	}

	chain := hook.BuildMutationChain(nil, []hook.MutationHook{outerHook}, terminal)
	_, err := chain(context.Background(), &hook.MutationContext{
		Op:    hook.OpCreateMany,
		Table: "products",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if diff := cmp.Diff([]any{10, 20, 30}, capturedPKs); diff != "" {
		t.Errorf("outer hook AffectedPKs mismatch (-want +got):\n%s", diff)
	}
}

func TestQueryContextFields(t *testing.T) {
	// The positional literal below fails to compile if a field is renamed or a
	// named-type field changes type; the pointer assignments pin the any fields,
	// which accept any value.
	type fields struct {
		Op          hook.QueryOp
		Table       hook.TableName
		Schema      string
		PK          any
		Input       any
		CallOptions any
		Tenant      any
	}
	q := &hook.QueryContext{
		Op:          hook.OpGet,
		Table:       "users",
		Schema:      "",
		PK:          7,
		Input:       "filter",
		CallOptions: "some-options",
		Tenant:      "acme",
	}

	pinAny(&q.PK, &q.Input, &q.CallOptions, &q.Tenant)
	_ = hook.QueryContext{hook.OpGet, "", "", nil, nil, nil, nil}

	got := fields{q.Op, q.Table, q.Schema, q.PK, q.Input, q.CallOptions, q.Tenant}
	want := fields{hook.OpGet, "users", "", 7, "filter", "some-options", "acme"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("QueryContext fields mismatch (-want +got):\n%s", diff)
	}
}

func TestTableNameType(t *testing.T) {
	// Verify TableName is a string type that supports comparison.
	var tn hook.TableName = "products"
	if tn != "products" {
		t.Errorf("TableName = %q, want %q", tn, "products")
	}
	if string(tn) != "products" {
		t.Errorf("string(TableName) = %q, want %q", string(tn), "products")
	}
}
