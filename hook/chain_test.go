package hook_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/hook"
)

// trackingMutationHook returns a MutationHook that appends its label to the
// order slice before and after calling next.
func trackingMutationHook(label string, order *[]string) hook.MutationHook {
	return func(next hook.MutationHandler) hook.MutationHandler {
		return func(ctx context.Context, m *hook.MutationContext) (any, error) {
			*order = append(*order, label+":before")
			result, err := next(ctx, m)
			*order = append(*order, label+":after")
			return result, err
		}
	}
}

// trackingQueryHook returns a QueryHook that appends its label to the
// order slice before and after calling next.
func trackingQueryHook(label string, order *[]string) hook.QueryHook {
	return func(next hook.QueryHandler) hook.QueryHandler {
		return func(ctx context.Context, q *hook.QueryContext) (any, error) {
			*order = append(*order, label+":before")
			result, err := next(ctx, q)
			*order = append(*order, label+":after")
			return result, err
		}
	}
}

func TestBuildMutationChain_executionOrder(t *testing.T) {
	var order []string
	hooks := []hook.MutationHook{
		trackingMutationHook("f", &order),
		trackingMutationHook("g", &order),
		trackingMutationHook("h", &order),
	}

	terminal := func(_ context.Context, _ *hook.MutationContext) (any, error) {
		order = append(order, "terminal")
		return "ok", nil
	}

	chain := hook.BuildMutationChain(nil, hooks, terminal)
	result, err := chain(context.Background(), &hook.MutationContext{
		Op:     hook.OpCreate,
		Table:  "products",
		Schema: "public",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ok" {
		t.Errorf("result = %v, want %q", result, "ok")
	}

	// Before logic: f→g→h (registration order)
	// After logic: h→g→f (reverse order)
	wantOrder := []string{
		"f:before", "g:before", "h:before",
		"terminal",
		"h:after", "g:after", "f:after",
	}
	if len(order) != len(wantOrder) {
		t.Fatalf("order length = %d, want %d: %v", len(order), len(wantOrder), order)
	}
	for i, want := range wantOrder {
		if order[i] != want {
			t.Errorf("order[%d] = %q, want %q", i, order[i], want)
		}
	}
}

func TestBuildQueryChain_executionOrder(t *testing.T) {
	var order []string
	hooks := []hook.QueryHook{
		trackingQueryHook("f", &order),
		trackingQueryHook("g", &order),
		trackingQueryHook("h", &order),
	}

	terminal := func(_ context.Context, _ *hook.QueryContext) (any, error) {
		order = append(order, "terminal")
		return 42, nil
	}

	chain := hook.BuildQueryChain(nil, hooks, terminal)
	result, err := chain(context.Background(), &hook.QueryContext{
		Op:     hook.OpGetMany,
		Table:  "products",
		Schema: "public",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != 42 {
		t.Errorf("result = %v, want %d", result, 42)
	}

	wantOrder := []string{
		"f:before", "g:before", "h:before",
		"terminal",
		"h:after", "g:after", "f:after",
	}
	if len(order) != len(wantOrder) {
		t.Fatalf("order length = %d, want %d: %v", len(order), len(wantOrder), order)
	}
	for i, want := range wantOrder {
		if order[i] != want {
			t.Errorf("order[%d] = %q, want %q", i, order[i], want)
		}
	}
}

func TestBuildMutationChain_panicInHook(t *testing.T) {
	panicHook := func(_ hook.MutationHandler) hook.MutationHandler {
		return func(_ context.Context, _ *hook.MutationContext) (any, error) {
			panic("hook exploded")
		}
	}

	terminal := func(_ context.Context, _ *hook.MutationContext) (any, error) {
		t.Fatal("terminal should not be reached")
		return nil, nil
	}

	chain := hook.BuildMutationChain(nil, []hook.MutationHook{panicHook}, terminal)
	result, err := chain(context.Background(), &hook.MutationContext{
		Op:     hook.OpCreate,
		Table:  "products",
		Schema: "public",
	})

	if result != nil {
		t.Errorf("result = %v, want nil", result)
	}
	if err == nil {
		t.Fatal("expected error from panic recovery, got nil")
	}
	if !strings.Contains(err.Error(), "sqlgen: panic in create public.products: hook exploded") {
		t.Errorf("error = %q, want message containing panic details", err.Error())
	}
}

func TestBuildMutationChain_panicInTerminal(t *testing.T) {
	terminal := func(_ context.Context, _ *hook.MutationContext) (any, error) {
		panic("db exploded")
	}

	chain := hook.BuildMutationChain(nil, nil, terminal)
	result, err := chain(context.Background(), &hook.MutationContext{
		Op:     hook.OpUpdate,
		Table:  "users",
		Schema: "public",
	})

	if result != nil {
		t.Errorf("result = %v, want nil", result)
	}
	if err == nil {
		t.Fatal("expected error from panic recovery, got nil")
	}
	if !strings.Contains(err.Error(), "sqlgen: panic in update public.users: db exploded") {
		t.Errorf("error = %q, want message containing panic details", err.Error())
	}
}

func TestBuildQueryChain_panicInTerminal(t *testing.T) {
	terminal := func(_ context.Context, _ *hook.QueryContext) (any, error) {
		panic("query exploded")
	}

	chain := hook.BuildQueryChain(nil, nil, terminal)
	result, err := chain(context.Background(), &hook.QueryContext{
		Op:     hook.OpGet,
		Table:  "products",
		Schema: "public",
	})

	if result != nil {
		t.Errorf("result = %v, want nil", result)
	}
	if err == nil {
		t.Fatal("expected error from panic recovery, got nil")
	}
	if !strings.Contains(err.Error(), "sqlgen: panic in get public.products: query exploded") {
		t.Errorf("error = %q, want message containing panic details", err.Error())
	}
}

func TestBuildMutationChain_customPanicHandler(t *testing.T) {
	var gotCtx context.Context
	var gotR any
	var gotTable, gotOp string

	customHandler := func(ctx context.Context, r any, table hook.TableName, op string) error {
		gotCtx = ctx
		gotR = r
		gotTable = string(table)
		gotOp = op
		return fmt.Errorf("custom: %v on %s %s", r, op, table)
	}

	terminal := func(_ context.Context, _ *hook.MutationContext) (any, error) {
		panic("boom")
	}

	//nolint:staticcheck // context value used for test verification
	ctx := context.WithValue(context.Background(), "test-key", "test-value")
	chain := hook.BuildMutationChain(customHandler, nil, terminal)
	_, err := chain(ctx, &hook.MutationContext{
		Op:    hook.OpHardDelete,
		Table: "users",
	})

	if err == nil {
		t.Fatal("expected error from custom panic handler")
	}
	if err.Error() != "custom: boom on hard_delete users" {
		t.Errorf("error = %q, want %q", err.Error(), "custom: boom on hard_delete users")
	}
	if gotCtx != ctx {
		t.Error("custom handler did not receive the original context")
	}
	if gotR != "boom" {
		t.Errorf("recovered value = %v, want %q", gotR, "boom")
	}
	if gotTable != "users" {
		t.Errorf("table = %q, want %q", gotTable, "users")
	}
	if gotOp != "hard_delete" {
		t.Errorf("op = %q, want %q", gotOp, "hard_delete")
	}
}

func TestBuildQueryChain_customPanicHandler(t *testing.T) {
	var gotCtx context.Context
	var gotR any
	var gotTable hook.TableName
	var gotOp string
	customErr := errors.New("custom query panic")
	customHandler := func(ctx context.Context, r any, table hook.TableName, op string) error {
		gotCtx, gotR, gotTable, gotOp = ctx, r, table, op
		return customErr
	}

	terminal := func(_ context.Context, _ *hook.QueryContext) (any, error) {
		panic("query boom")
	}

	//nolint:staticcheck // context value used for test verification
	ctx := context.WithValue(context.Background(), "test-key", "test-value")
	chain := hook.BuildQueryChain(customHandler, nil, terminal)
	_, err := chain(ctx, &hook.QueryContext{
		Op:    hook.OpGetMany,
		Table: "products",
	})

	// The handler's error is what the caller sees, as-is: not wrapped.
	if err != customErr { //nolint:errorlint // identity is the property under test
		t.Errorf("error = %v, want the handler's error itself", err)
	}
	if gotCtx != ctx {
		t.Error("custom handler did not receive the original context")
	}
	if gotR != "query boom" {
		t.Errorf("recovered value = %v, want %q", gotR, "query boom")
	}
	if gotTable != "products" {
		t.Errorf("table = %q, want %q", gotTable, "products")
	}
	if gotOp != "get_many" {
		t.Errorf("op = %q, want %q", gotOp, "get_many")
	}
}

func TestBuildMutationChain_emptyHooks(t *testing.T) {
	terminalCalled := false
	terminal := func(_ context.Context, _ *hook.MutationContext) (any, error) {
		terminalCalled = true
		return "direct", nil
	}

	// No hooks — chain should call terminal directly (wrapped in panic recovery only).
	chain := hook.BuildMutationChain(nil, nil, terminal)
	result, err := chain(context.Background(), &hook.MutationContext{
		Op:    hook.OpCreate,
		Table: "products",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !terminalCalled {
		t.Error("terminal was not called")
	}
	if result != "direct" {
		t.Errorf("result = %v, want %q", result, "direct")
	}
}

func TestBuildQueryChain_emptyHooks(t *testing.T) {
	terminalCalled := false
	terminal := func(_ context.Context, _ *hook.QueryContext) (any, error) {
		terminalCalled = true
		return 99, nil
	}

	chain := hook.BuildQueryChain(nil, nil, terminal)
	result, err := chain(context.Background(), &hook.QueryContext{
		Op:    hook.OpGet,
		Table: "users",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !terminalCalled {
		t.Error("terminal was not called")
	}
	if result != 99 {
		t.Errorf("result = %v, want %d", result, 99)
	}
}

func TestBuildMutationChain_onlyGlobalHooks(t *testing.T) {
	var order []string
	hooks := []hook.MutationHook{
		trackingMutationHook("g1", &order),
		trackingMutationHook("g2", &order),
	}

	terminal := func(_ context.Context, _ *hook.MutationContext) (any, error) {
		order = append(order, "terminal")
		return "ok", nil
	}

	chain := hook.BuildMutationChain(nil, hooks, terminal)
	_, err := chain(context.Background(), &hook.MutationContext{
		Op:    hook.OpCreate,
		Table: "products",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantOrder := []string{"g1:before", "g2:before", "terminal", "g2:after", "g1:after"}
	if len(order) != len(wantOrder) {
		t.Fatalf("order length = %d, want %d: %v", len(order), len(wantOrder), order)
	}
	for i, want := range wantOrder {
		if order[i] != want {
			t.Errorf("order[%d] = %q, want %q", i, order[i], want)
		}
	}
}

func TestBuildMutationChain_skipHooksBypassesAllHooks(t *testing.T) {
	// When SkipHooks is true, the caller passes nil hooks.
	// Only panic recovery wraps the terminal handler.
	var order []string
	hooks := []hook.MutationHook{
		trackingMutationHook("h1", &order),
		trackingMutationHook("h2", &order),
	}

	terminal := func(_ context.Context, _ *hook.MutationContext) (any, error) {
		order = append(order, "terminal")
		return "result", nil
	}

	// Simulate SkipHooks=true by passing nil hooks
	_ = hooks // registered but not used when skipping
	chain := hook.BuildMutationChain(nil, nil, terminal)
	result, err := chain(context.Background(), &hook.MutationContext{
		Op:    hook.OpCreate,
		Table: "products",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "result" {
		t.Errorf("result = %v, want %q", result, "result")
	}

	// Only terminal should have run — no hooks
	wantOrder := []string{"terminal"}
	if len(order) != len(wantOrder) {
		t.Fatalf("order length = %d, want %d: %v", len(order), len(wantOrder), order)
	}
	if order[0] != "terminal" {
		t.Errorf("order[0] = %q, want %q", order[0], "terminal")
	}
}

func TestBuildMutationChain_skipHooksPreservesPanicRecovery(t *testing.T) {
	// Even with nil hooks (SkipHooks=true), panic recovery still wraps the terminal.
	terminal := func(_ context.Context, _ *hook.MutationContext) (any, error) {
		panic("boom in skipped mode")
	}

	chain := hook.BuildMutationChain(nil, nil, terminal)
	_, err := chain(context.Background(), &hook.MutationContext{
		Op:     hook.OpCreate,
		Table:  "products",
		Schema: "public",
	})

	if err == nil {
		t.Fatal("expected error from panic recovery, got nil")
	}
	if !strings.Contains(err.Error(), "sqlgen: panic in create public.products: boom in skipped mode") {
		t.Errorf("error = %q, want message containing panic details", err.Error())
	}
}

func TestBuildQueryChain_skipHooksBypassesAllHooks(t *testing.T) {
	var order []string
	hooks := []hook.QueryHook{
		trackingQueryHook("h1", &order),
	}

	terminal := func(_ context.Context, _ *hook.QueryContext) (any, error) {
		order = append(order, "terminal")
		return 42, nil
	}

	// Simulate SkipHooks=true by passing nil hooks
	_ = hooks
	chain := hook.BuildQueryChain(nil, nil, terminal)
	result, err := chain(context.Background(), &hook.QueryContext{
		Op:    hook.OpGet,
		Table: "products",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != 42 {
		t.Errorf("result = %v, want %d", result, 42)
	}
	if len(order) != 1 || order[0] != "terminal" {
		t.Errorf("order = %v, want [terminal]", order)
	}
}

func TestBuildQueryChain_skipHooksPreservesPanicRecovery(t *testing.T) {
	terminal := func(_ context.Context, _ *hook.QueryContext) (any, error) {
		panic("query panic in skipped mode")
	}

	chain := hook.BuildQueryChain(nil, nil, terminal)
	_, err := chain(context.Background(), &hook.QueryContext{
		Op:     hook.OpGet,
		Table:  "products",
		Schema: "public",
	})

	if err == nil {
		t.Fatal("expected error from panic recovery, got nil")
	}
	if !strings.Contains(err.Error(), "sqlgen: panic in get public.products: query panic in skipped mode") {
		t.Errorf("error = %q, want message containing panic details", err.Error())
	}
}

func TestBuildMutationChain_callOptionsPropagate(t *testing.T) {
	// Verify that CallOptions set on the context are accessible to hooks.
	type testOptions struct {
		SkipCache bool
	}

	var gotOptions any
	inspectHook := func(next hook.MutationHandler) hook.MutationHandler {
		return func(ctx context.Context, m *hook.MutationContext) (any, error) {
			gotOptions = m.CallOptions
			return next(ctx, m)
		}
	}

	terminal := func(_ context.Context, _ *hook.MutationContext) (any, error) {
		return "ok", nil
	}

	opts := testOptions{SkipCache: true}
	chain := hook.BuildMutationChain(nil, []hook.MutationHook{inspectHook}, terminal)
	_, err := chain(context.Background(), &hook.MutationContext{
		Op:          hook.OpCreate,
		Table:       "products",
		CallOptions: opts,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotOptions == nil {
		t.Fatal("hook did not receive CallOptions")
	}
	typedOpts, ok := gotOptions.(testOptions)
	if !ok {
		t.Fatalf("CallOptions type = %T, want testOptions", gotOptions)
	}
	if !typedOpts.SkipCache {
		t.Error("CallOptions.SkipCache = false, want true")
	}
}

func TestBuildQueryChain_callOptionsPropagate(t *testing.T) {
	type testOptions struct {
		SkipEvents bool
	}

	var gotOptions any
	inspectHook := func(next hook.QueryHandler) hook.QueryHandler {
		return func(ctx context.Context, q *hook.QueryContext) (any, error) {
			gotOptions = q.CallOptions
			return next(ctx, q)
		}
	}

	terminal := func(_ context.Context, _ *hook.QueryContext) (any, error) {
		return 42, nil
	}

	opts := testOptions{SkipEvents: true}
	chain := hook.BuildQueryChain(nil, []hook.QueryHook{inspectHook}, terminal)
	_, err := chain(context.Background(), &hook.QueryContext{
		Op:          hook.OpGet,
		Table:       "products",
		CallOptions: opts,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotOptions == nil {
		t.Fatal("hook did not receive CallOptions")
	}
	typedOpts, ok := gotOptions.(testOptions)
	if !ok {
		t.Fatalf("CallOptions type = %T, want testOptions", gotOptions)
	}
	if !typedOpts.SkipEvents {
		t.Error("CallOptions.SkipEvents = false, want true")
	}
}

func TestBuildMutationChain_onlyEntityHooks(t *testing.T) {
	// Entity hook that only fires for "products" table.
	var order []string
	entityHook := hook.ForMutation(
		hook.TableName("products"),
		func(ctx context.Context, m *hook.MutationContext, input string, next func(context.Context) (string, error)) (string, error) {
			order = append(order, "entity:before")
			result, err := next(ctx)
			order = append(order, "entity:after")
			return result, err
		},
	)

	terminal := func(_ context.Context, _ *hook.MutationContext) (any, error) {
		order = append(order, "terminal")
		return "created", nil
	}

	chain := hook.BuildMutationChain(nil, []hook.MutationHook{entityHook}, terminal)

	// Call for matching table.
	_, err := chain(context.Background(), &hook.MutationContext{
		Op:    hook.OpCreate,
		Table: "products",
		Input: "input",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantOrder := []string{"entity:before", "terminal", "entity:after"}
	if len(order) != len(wantOrder) {
		t.Fatalf("order length = %d, want %d: %v", len(order), len(wantOrder), order)
	}
	for i, want := range wantOrder {
		if order[i] != want {
			t.Errorf("order[%d] = %q, want %q", i, order[i], want)
		}
	}

	// Call for non-matching table — entity hook should pass through.
	order = nil
	_, err = chain(context.Background(), &hook.MutationContext{
		Op:    hook.OpCreate,
		Table: "users",
		Input: "input",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantPassthrough := []string{"terminal"}
	if len(order) != len(wantPassthrough) {
		t.Fatalf("passthrough order length = %d, want %d: %v", len(order), len(wantPassthrough), order)
	}
	if order[0] != "terminal" {
		t.Errorf("order[0] = %q, want %q", order[0], "terminal")
	}
}

// TestDefaultPanicError_QualifiedTable pins the default recovery error for
// every Table form a context can carry. A generated client stamps Table with
// the TableXxx constant's value, which already carries the schema on
// PostgreSQL (PRD §5.5), so the message must not print it twice
// ("panic in create audit.audit.users").
func TestDefaultPanicError_QualifiedTable(t *testing.T) {
	tests := []struct {
		name   string
		schema string
		table  hook.TableName
		want   string
	}{
		{name: "qualified value", schema: "public", table: "public.users", want: "public.users"},
		{name: "second schema", schema: "audit", table: "audit.users", want: "audit.users"},
		{name: "schema named like the table", schema: "users", table: "users.users", want: "users.users"},
		{name: "bare value with schema", schema: "public", table: "users", want: "public.users"},
		{name: "schemaless", schema: "", table: "users", want: "users"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mutation := hook.BuildMutationChain(nil, nil, func(context.Context, *hook.MutationContext) (any, error) {
				panic("boom")
			})
			_, err := mutation(context.Background(), &hook.MutationContext{Op: hook.OpCreate, Table: tt.table, Schema: tt.schema})
			if want := "sqlgen: panic in create " + tt.want + ": boom"; err == nil || err.Error() != want {
				t.Errorf("mutation panic error = %v, want %q", err, want)
			}

			query := hook.BuildQueryChain(nil, nil, func(context.Context, *hook.QueryContext) (any, error) {
				panic("boom")
			})
			_, err = query(context.Background(), &hook.QueryContext{Op: hook.OpGet, Table: tt.table, Schema: tt.schema})
			if want := "sqlgen: panic in get " + tt.want + ": boom"; err == nil || err.Error() != want {
				t.Errorf("query panic error = %v, want %q", err, want)
			}
		})
	}
}

// entityMutationHook returns a ForMutation hook on products that records its
// label before and after calling next.
func entityMutationHook(order *[]string) hook.MutationHook {
	return hook.ForMutation(
		hook.TableName("products"),
		func(ctx context.Context, _ *hook.MutationContext, _ string, next func(context.Context) (string, error)) (string, error) {
			*order = append(*order, "entity:before")
			result, err := next(ctx)
			*order = append(*order, "entity:after")
			return result, err
		},
	)
}

// entityQueryHook returns a ForQuery hook on products that records its label
// before and after calling next.
func entityQueryHook(order *[]string) hook.QueryHook {
	return hook.ForQuery(
		hook.TableName("products"),
		func(ctx context.Context, _ *hook.QueryContext, _ string, next func(context.Context) (string, error)) (string, error) {
			*order = append(*order, "entity:before")
			result, err := next(ctx)
			*order = append(*order, "entity:after")
			return result, err
		},
	)
}

// TestBuildMutationChain_registrationOrderAcrossHookKinds pins that the chain
// orders hooks by registration alone: an entity hook registered before a
// global hook runs outside it.
func TestBuildMutationChain_registrationOrderAcrossHookKinds(t *testing.T) {
	t.Run("global registered first", func(t *testing.T) {
		var order []string
		hooks := []hook.MutationHook{trackingMutationHook("global", &order), entityMutationHook(&order)}
		runMutationOrder(t, hooks, &order, []string{"global:before", "entity:before", "terminal", "entity:after", "global:after"})
	})
	t.Run("entity registered first", func(t *testing.T) {
		var order []string
		hooks := []hook.MutationHook{entityMutationHook(&order), trackingMutationHook("global", &order)}
		runMutationOrder(t, hooks, &order, []string{"entity:before", "global:before", "terminal", "global:after", "entity:after"})
	})
}

func runMutationOrder(t *testing.T, hooks []hook.MutationHook, order *[]string, want []string) {
	t.Helper()
	chain := hook.BuildMutationChain(nil, hooks, func(context.Context, *hook.MutationContext) (any, error) {
		*order = append(*order, "terminal")
		return "created", nil
	})
	if _, err := chain(context.Background(), &hook.MutationContext{Op: hook.OpCreate, Table: "products", Input: "in"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if diff := cmp.Diff(want, *order); diff != "" {
		t.Errorf("order mismatch (-want +got):\n%s", diff)
	}
}

// TestBuildQueryChain_registrationOrderAcrossHookKinds is the query-chain
// counterpart of TestBuildMutationChain_registrationOrderAcrossHookKinds.
func TestBuildQueryChain_registrationOrderAcrossHookKinds(t *testing.T) {
	t.Run("global registered first", func(t *testing.T) {
		var order []string
		hooks := []hook.QueryHook{trackingQueryHook("global", &order), entityQueryHook(&order)}
		runQueryOrder(t, hooks, &order, []string{"global:before", "entity:before", "terminal", "entity:after", "global:after"})
	})
	t.Run("entity registered first", func(t *testing.T) {
		var order []string
		hooks := []hook.QueryHook{entityQueryHook(&order), trackingQueryHook("global", &order)}
		runQueryOrder(t, hooks, &order, []string{"entity:before", "global:before", "terminal", "global:after", "entity:after"})
	})
}

func runQueryOrder(t *testing.T, hooks []hook.QueryHook, order *[]string, want []string) {
	t.Helper()
	chain := hook.BuildQueryChain(nil, hooks, func(context.Context, *hook.QueryContext) (any, error) {
		*order = append(*order, "terminal")
		return "rows", nil
	})
	if _, err := chain(context.Background(), &hook.QueryContext{Op: hook.OpGetMany, Table: "products", Input: "in"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if diff := cmp.Diff(want, *order); diff != "" {
		t.Errorf("order mismatch (-want +got):\n%s", diff)
	}
}

type payload struct{ Name string }

// TestBuildMutationChain_hookCapabilities pins what a mutation hook can do to
// the operation it wraps: change the input before next, change the result
// after it, return a result without calling next, or return an error without
// calling next.
func TestBuildMutationChain_hookCapabilities(t *testing.T) {
	t.Run("modify before", func(t *testing.T) {
		replaced := &payload{Name: "sanitized"}
		var seen any
		h := func(next hook.MutationHandler) hook.MutationHandler {
			return func(ctx context.Context, m *hook.MutationContext) (any, error) {
				m.Input = replaced
				return next(ctx, m)
			}
		}
		chain := hook.BuildMutationChain(nil, []hook.MutationHook{h}, func(_ context.Context, m *hook.MutationContext) (any, error) {
			seen = m.Input
			return nil, nil
		})
		if _, err := chain(context.Background(), &hook.MutationContext{Op: hook.OpCreate, Table: "products", Input: &payload{Name: "raw"}}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if seen != replaced {
			t.Errorf("terminal saw Input %v, want the hook's replacement", seen)
		}
	})
	t.Run("modify after", func(t *testing.T) {
		enriched := &payload{Name: "enriched"}
		h := func(next hook.MutationHandler) hook.MutationHandler {
			return func(ctx context.Context, m *hook.MutationContext) (any, error) {
				if _, err := next(ctx, m); err != nil {
					return nil, err
				}
				return enriched, nil
			}
		}
		chain := hook.BuildMutationChain(nil, []hook.MutationHook{h}, func(context.Context, *hook.MutationContext) (any, error) {
			return &payload{Name: "stored"}, nil
		})
		got, err := chain(context.Background(), &hook.MutationContext{Op: hook.OpCreate, Table: "products"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != enriched {
			t.Errorf("result = %v, want the hook's replacement", got)
		}
	})
	t.Run("short-circuit", func(t *testing.T) {
		cached := &payload{Name: "cached"}
		h := func(hook.MutationHandler) hook.MutationHandler {
			return func(context.Context, *hook.MutationContext) (any, error) {
				return cached, nil
			}
		}
		chain := hook.BuildMutationChain(nil, []hook.MutationHook{h}, func(context.Context, *hook.MutationContext) (any, error) {
			t.Error("terminal ran after the hook short-circuited")
			return nil, nil
		})
		got, err := chain(context.Background(), &hook.MutationContext{Op: hook.OpCreate, Table: "products"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != cached {
			t.Errorf("result = %v, want the hook's result", got)
		}
	})
	t.Run("abort", func(t *testing.T) {
		denied := errors.New("unauthorized")
		h := func(hook.MutationHandler) hook.MutationHandler {
			return func(context.Context, *hook.MutationContext) (any, error) {
				return nil, denied
			}
		}
		chain := hook.BuildMutationChain(nil, []hook.MutationHook{h}, func(context.Context, *hook.MutationContext) (any, error) {
			t.Error("terminal ran after the hook aborted")
			return "written", nil
		})
		got, err := chain(context.Background(), &hook.MutationContext{Op: hook.OpCreate, Table: "products"})
		if err != denied { //nolint:errorlint // identity is the property under test
			t.Errorf("error = %v, want the hook's error itself", err)
		}
		if got != nil {
			t.Errorf("result = %v, want nil", got)
		}
	})
}

// TestBuildQueryChain_hookCapabilities is the query-chain counterpart of
// TestBuildMutationChain_hookCapabilities.
func TestBuildQueryChain_hookCapabilities(t *testing.T) {
	t.Run("modify before", func(t *testing.T) {
		replaced := &payload{Name: "limited"}
		var seen any
		h := func(next hook.QueryHandler) hook.QueryHandler {
			return func(ctx context.Context, q *hook.QueryContext) (any, error) {
				q.Input = replaced
				return next(ctx, q)
			}
		}
		chain := hook.BuildQueryChain(nil, []hook.QueryHook{h}, func(_ context.Context, q *hook.QueryContext) (any, error) {
			seen = q.Input
			return nil, nil
		})
		if _, err := chain(context.Background(), &hook.QueryContext{Op: hook.OpGetMany, Table: "products", Input: &payload{Name: "raw"}}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if seen != replaced {
			t.Errorf("terminal saw Input %v, want the hook's replacement", seen)
		}
	})
	t.Run("modify after", func(t *testing.T) {
		enriched := &payload{Name: "enriched"}
		h := func(next hook.QueryHandler) hook.QueryHandler {
			return func(ctx context.Context, q *hook.QueryContext) (any, error) {
				if _, err := next(ctx, q); err != nil {
					return nil, err
				}
				return enriched, nil
			}
		}
		chain := hook.BuildQueryChain(nil, []hook.QueryHook{h}, func(context.Context, *hook.QueryContext) (any, error) {
			return &payload{Name: "stored"}, nil
		})
		got, err := chain(context.Background(), &hook.QueryContext{Op: hook.OpGetMany, Table: "products"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != enriched {
			t.Errorf("result = %v, want the hook's replacement", got)
		}
	})
	t.Run("short-circuit", func(t *testing.T) {
		cached := &payload{Name: "cached"}
		h := func(hook.QueryHandler) hook.QueryHandler {
			return func(context.Context, *hook.QueryContext) (any, error) {
				return cached, nil
			}
		}
		chain := hook.BuildQueryChain(nil, []hook.QueryHook{h}, func(context.Context, *hook.QueryContext) (any, error) {
			t.Error("terminal ran after the hook short-circuited")
			return nil, nil
		})
		got, err := chain(context.Background(), &hook.QueryContext{Op: hook.OpGetMany, Table: "products"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != cached {
			t.Errorf("result = %v, want the hook's result", got)
		}
	})
	t.Run("abort", func(t *testing.T) {
		denied := errors.New("unauthorized")
		h := func(hook.QueryHandler) hook.QueryHandler {
			return func(context.Context, *hook.QueryContext) (any, error) {
				return nil, denied
			}
		}
		chain := hook.BuildQueryChain(nil, []hook.QueryHook{h}, func(context.Context, *hook.QueryContext) (any, error) {
			t.Error("terminal ran after the hook aborted")
			return "rows", nil
		})
		got, err := chain(context.Background(), &hook.QueryContext{Op: hook.OpGetMany, Table: "products"})
		if err != denied { //nolint:errorlint // identity is the property under test
			t.Errorf("error = %v, want the hook's error itself", err)
		}
		if got != nil {
			t.Errorf("result = %v, want nil", got)
		}
	})
}

func TestBuildQueryChain_panicInHook(t *testing.T) {
	panicHook := func(hook.QueryHandler) hook.QueryHandler {
		return func(context.Context, *hook.QueryContext) (any, error) {
			panic("hook exploded")
		}
	}

	chain := hook.BuildQueryChain(nil, []hook.QueryHook{panicHook}, func(context.Context, *hook.QueryContext) (any, error) {
		t.Fatal("terminal should not be reached")
		return nil, nil
	})
	result, err := chain(context.Background(), &hook.QueryContext{
		Op:     hook.OpCount,
		Table:  "products",
		Schema: "public",
	})

	if result != nil {
		t.Errorf("result = %v, want nil", result)
	}
	if want := "sqlgen: panic in count public.products: hook exploded"; err == nil || err.Error() != want {
		t.Errorf("error = %v, want %q", err, want)
	}
}

// TestBuildMutationChain_panicRecoveryIsOutermost pins that recovery wraps the
// whole chain rather than each hook: a panic in an inner hook unwinds through
// the outer hook, whose after-logic never runs, and the caller gets the
// recovery error.
func TestBuildMutationChain_panicRecoveryIsOutermost(t *testing.T) {
	outerAfterRan := false
	outer := func(next hook.MutationHandler) hook.MutationHandler {
		return func(ctx context.Context, m *hook.MutationContext) (any, error) {
			result, err := next(ctx, m)
			outerAfterRan = true
			return result, err
		}
	}
	inner := func(hook.MutationHandler) hook.MutationHandler {
		return func(context.Context, *hook.MutationContext) (any, error) {
			panic("inner exploded")
		}
	}

	chain := hook.BuildMutationChain(nil, []hook.MutationHook{outer, inner}, terminalMutation("ok"))
	_, err := chain(context.Background(), &hook.MutationContext{Op: hook.OpUpdate, Table: "products"})

	if want := "sqlgen: panic in update products: inner exploded"; err == nil || err.Error() != want {
		t.Errorf("error = %v, want %q", err, want)
	}
	if outerAfterRan {
		t.Error("the outer hook's after-logic ran: recovery sits inside it, not outside the chain")
	}
}

// TestBuildQueryChain_panicRecoveryIsOutermost is the query-chain counterpart
// of TestBuildMutationChain_panicRecoveryIsOutermost.
func TestBuildQueryChain_panicRecoveryIsOutermost(t *testing.T) {
	outerAfterRan := false
	outer := func(next hook.QueryHandler) hook.QueryHandler {
		return func(ctx context.Context, q *hook.QueryContext) (any, error) {
			result, err := next(ctx, q)
			outerAfterRan = true
			return result, err
		}
	}
	inner := func(hook.QueryHandler) hook.QueryHandler {
		return func(context.Context, *hook.QueryContext) (any, error) {
			panic("inner exploded")
		}
	}

	chain := hook.BuildQueryChain(nil, []hook.QueryHook{outer, inner}, terminalQuery("ok"))
	_, err := chain(context.Background(), &hook.QueryContext{Op: hook.OpGet, Table: "products"})

	if want := "sqlgen: panic in get products: inner exploded"; err == nil || err.Error() != want {
		t.Errorf("error = %v, want %q", err, want)
	}
	if outerAfterRan {
		t.Error("the outer hook's after-logic ran: recovery sits inside it, not outside the chain")
	}
}

type ctxKey string

// TestBuildMutationChain_hookContextReachesTerminal pins that the chain hands
// the caller's context in and that the context a hook passes to next is the
// one every inner layer and the terminal receive, which is how a tracing hook's
// span becomes the parent of the driver's spans.
func TestBuildMutationChain_hookContextReachesTerminal(t *testing.T) {
	var innerSaw, terminalSaw context.Context
	tracing := func(next hook.MutationHandler) hook.MutationHandler {
		return func(ctx context.Context, m *hook.MutationContext) (any, error) {
			return next(context.WithValue(ctx, ctxKey("span"), "operation"), m)
		}
	}
	inner := func(next hook.MutationHandler) hook.MutationHandler {
		return func(ctx context.Context, m *hook.MutationContext) (any, error) {
			innerSaw = ctx
			return next(ctx, m)
		}
	}
	chain := hook.BuildMutationChain(nil, []hook.MutationHook{tracing, inner}, func(ctx context.Context, _ *hook.MutationContext) (any, error) {
		terminalSaw = ctx
		return nil, nil
	})

	caller := context.WithValue(context.Background(), ctxKey("request"), "caller")
	if _, err := chain(caller, &hook.MutationContext{Op: hook.OpCreate, Table: "products"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if terminalSaw != innerSaw {
		t.Error("the terminal did not receive the context the inner hook passed to next")
	}
	if terminalSaw.Value(ctxKey("span")) != "operation" {
		t.Error("the terminal's context lacks the value the tracing hook added")
	}
	if terminalSaw.Value(ctxKey("request")) != "caller" {
		t.Error("the terminal's context does not descend from the caller's")
	}
}

// TestBuildQueryChain_hookContextReachesTerminal is the query-chain
// counterpart of TestBuildMutationChain_hookContextReachesTerminal.
func TestBuildQueryChain_hookContextReachesTerminal(t *testing.T) {
	var innerSaw, terminalSaw context.Context
	tracing := func(next hook.QueryHandler) hook.QueryHandler {
		return func(ctx context.Context, q *hook.QueryContext) (any, error) {
			return next(context.WithValue(ctx, ctxKey("span"), "operation"), q)
		}
	}
	inner := func(next hook.QueryHandler) hook.QueryHandler {
		return func(ctx context.Context, q *hook.QueryContext) (any, error) {
			innerSaw = ctx
			return next(ctx, q)
		}
	}
	chain := hook.BuildQueryChain(nil, []hook.QueryHook{tracing, inner}, func(ctx context.Context, _ *hook.QueryContext) (any, error) {
		terminalSaw = ctx
		return nil, nil
	})

	caller := context.WithValue(context.Background(), ctxKey("request"), "caller")
	if _, err := chain(caller, &hook.QueryContext{Op: hook.OpGetMany, Table: "products"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if terminalSaw != innerSaw {
		t.Error("the terminal did not receive the context the inner hook passed to next")
	}
	if terminalSaw.Value(ctxKey("span")) != "operation" {
		t.Error("the terminal's context lacks the value the tracing hook added")
	}
	if terminalSaw.Value(ctxKey("request")) != "caller" {
		t.Error("the terminal's context does not descend from the caller's")
	}
}
