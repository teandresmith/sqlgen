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
	}

	got := []hook.QueryOp{
		hook.OpGet,
		hook.OpGetMany,
		hook.OpExists,
		hook.OpCount,
		hook.OpPaginate,
		hook.OpConnection,
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

func TestMutationContextFields(t *testing.T) {
	m := &hook.MutationContext{
		Op:     hook.OpUpdate,
		Table:  "products",
		Schema: "public",
		PK:     123,
		Input:  "some-input",
	}

	if m.Op != hook.OpUpdate {
		t.Errorf("Op = %q, want %q", m.Op, hook.OpUpdate)
	}
	if m.Table != "products" {
		t.Errorf("Table = %q, want %q", m.Table, "products")
	}
	if m.Schema != "public" {
		t.Errorf("Schema = %q, want %q", m.Schema, "public")
	}
	if m.PK != 123 {
		t.Errorf("PK = %v, want %d", m.PK, 123)
	}
	if m.Input != "some-input" {
		t.Errorf("Input = %v, want %q", m.Input, "some-input")
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
	q := &hook.QueryContext{
		Op:     hook.OpGetMany,
		Table:  "users",
		Schema: "",
		PK:     nil,
		Input:  "filter",
	}

	if q.Op != hook.OpGetMany {
		t.Errorf("Op = %q, want %q", q.Op, hook.OpGetMany)
	}
	if q.Table != "users" {
		t.Errorf("Table = %q, want %q", q.Table, "users")
	}
	if q.Schema != "" {
		t.Errorf("Schema = %q, want empty", q.Schema)
	}
	if q.PK != nil {
		t.Errorf("PK = %v, want nil", q.PK)
	}
	if q.Input != "filter" {
		t.Errorf("Input = %v, want %q", q.Input, "filter")
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
