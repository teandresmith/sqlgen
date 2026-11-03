package hook_test

import (
	"context"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/hook"
)

// test types for ForMutation/ForQuery type-safe helpers.
type createInput struct {
	Name string
}

type product struct {
	ID   int
	Name string
}

type getInput struct {
	ID int
}

type updateInput struct {
	Name string
}

const (
	tableProducts hook.TableName = "products"
	tableOrders   hook.TableName = "orders"
	tableUsers    hook.TableName = "users"
)

// terminalMutation returns a MutationHandler that returns the given result.
func terminalMutation(result any) hook.MutationHandler {
	return func(_ context.Context, _ *hook.MutationContext) (any, error) {
		return result, nil
	}
}

// terminalQuery returns a QueryHandler that returns the given result.
func terminalQuery(result any) hook.QueryHandler {
	return func(_ context.Context, _ *hook.QueryContext) (any, error) {
		return result, nil
	}
}

func TestForMutation(t *testing.T) {
	t.Run("passes through when table does not match", func(t *testing.T) {
		hookCalled := false
		h := hook.ForMutation[*createInput, *product](
			tableProducts,
			func(ctx context.Context, m *hook.MutationContext, input *createInput, next func(ctx context.Context) (*product, error)) (*product, error) {
				hookCalled = true
				return next(ctx)
			},
		)

		terminal := terminalMutation(&product{ID: 1, Name: "widget"})
		handler := h(terminal)

		got, err := handler(context.Background(), &hook.MutationContext{
			Op:    hook.OpCreate,
			Table: tableOrders, // different table
			Input: &createInput{Name: "order"},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if hookCalled {
			t.Error("hook should not have been called for non-matching table")
		}
		p, ok := got.(*product)
		if !ok {
			t.Fatalf("result type = %T, want *product", got)
		}
		if p.ID != 1 {
			t.Errorf("result.ID = %d, want 1", p.ID)
		}
	})

	t.Run("executes typed fn when table matches", func(t *testing.T) {
		var capturedInput *createInput
		h := hook.ForMutation[*createInput, *product](
			tableProducts,
			func(ctx context.Context, m *hook.MutationContext, input *createInput, next func(ctx context.Context) (*product, error)) (*product, error) {
				capturedInput = input
				return next(ctx)
			},
		)

		input := &createInput{Name: "widget"}
		terminal := terminalMutation(&product{ID: 1, Name: "widget"})
		handler := h(terminal)

		got, err := handler(context.Background(), &hook.MutationContext{
			Op:    hook.OpCreate,
			Table: tableProducts,
			Input: input,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if capturedInput != input {
			t.Error("hook did not receive the expected typed input")
		}
		p, ok := got.(*product)
		if !ok {
			t.Fatalf("result type = %T, want *product", got)
		}
		if p.ID != 1 {
			t.Errorf("result.ID = %d, want 1", p.ID)
		}
	})

	t.Run("an input of another type returns an error without running fn or next", func(t *testing.T) {
		handler := mutationHelperProbe(t)
		_, err := handler(context.Background(), &hook.MutationContext{
			Op:    hook.OpUpdate,
			Table: tableProducts,
			Input: &updateInput{Name: "widget"},
		})
		want := "hook: type assertion failed for update products: expected *hook_test.createInput, got *hook_test.updateInput"
		if err == nil || err.Error() != want {
			t.Errorf("error = %v, want %q", err, want)
		}
	})

	t.Run("a nil input returns an error without running fn or next", func(t *testing.T) {
		handler := mutationHelperProbe(t)
		_, err := handler(context.Background(), &hook.MutationContext{
			Op:    hook.OpHardDelete,
			Table: tableProducts,
			PK:    1,
		})
		want := "hook: type assertion failed for hard_delete products: expected *hook_test.createInput, got <nil>"
		if err == nil || err.Error() != want {
			t.Errorf("error = %v, want %q", err, want)
		}
	})

	t.Run("a result of another type returns an error after next ran", func(t *testing.T) {
		nextRan := false
		h := hook.ForMutation[*createInput, *product](
			tableProducts,
			func(ctx context.Context, m *hook.MutationContext, input *createInput, next func(ctx context.Context) (*product, error)) (*product, error) {
				return next(ctx)
			},
		)
		handler := h(func(context.Context, *hook.MutationContext) (any, error) {
			nextRan = true
			return []*product{{ID: 1}}, nil
		})

		_, err := handler(context.Background(), &hook.MutationContext{
			Op:    hook.OpCreateMany,
			Table: tableProducts,
			Input: &createInput{Name: "widget"},
		})
		want := "hook: result type assertion failed for create_many products: expected *hook_test.product, got []*hook_test.product"
		if err == nil || err.Error() != want {
			t.Errorf("error = %v, want %q", err, want)
		}
		if !nextRan {
			t.Error("next did not run before the result assertion")
		}
	})

	t.Run("a nil result reaches fn as the zero value", func(t *testing.T) {
		var gotResult *product
		var gotErr error
		called := false
		h := hook.ForMutation[*createInput, *product](
			tableProducts,
			func(ctx context.Context, m *hook.MutationContext, input *createInput, next func(ctx context.Context) (*product, error)) (*product, error) {
				called = true
				gotResult, gotErr = next(ctx)
				return gotResult, gotErr
			},
		)
		handler := h(terminalMutation(nil))

		if _, err := handler(context.Background(), &hook.MutationContext{
			Op:    hook.OpCreate,
			Table: tableProducts,
			Input: &createInput{Name: "widget"},
		}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !called {
			t.Fatal("fn did not run")
		}
		if gotResult != nil || gotErr != nil {
			t.Errorf("typed next = (%v, %v), want (nil, nil)", gotResult, gotErr)
		}
	})

	t.Run("the terminal sees the input fn modified", func(t *testing.T) {
		var seen any
		h := hook.ForMutation[*createInput, *product](
			tableProducts,
			func(ctx context.Context, m *hook.MutationContext, input *createInput, next func(ctx context.Context) (*product, error)) (*product, error) {
				input.Name = strings.TrimSpace(input.Name)
				return next(ctx)
			},
		)
		handler := h(func(_ context.Context, m *hook.MutationContext) (any, error) {
			seen = m.Input
			return &product{ID: 1}, nil
		})

		input := &createInput{Name: "  widget  "}
		if _, err := handler(context.Background(), &hook.MutationContext{
			Op:    hook.OpCreate,
			Table: tableProducts,
			Input: input,
		}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if seen != input || input.Name != "widget" {
			t.Errorf("terminal saw %v with Name %q, want the caller's input with Name %q", seen, input.Name, "widget")
		}
	})
}

// mutationHelperProbe returns a ForMutation[*createInput, *product] handler on
// products whose fn and terminal each fail the test if they run.
func mutationHelperProbe(t *testing.T) hook.MutationHandler {
	t.Helper()
	h := hook.ForMutation[*createInput, *product](
		tableProducts,
		func(ctx context.Context, m *hook.MutationContext, input *createInput, next func(ctx context.Context) (*product, error)) (*product, error) {
			t.Error("fn ran on a mismatched input")
			return next(ctx)
		},
	)
	return h(func(context.Context, *hook.MutationContext) (any, error) {
		t.Error("next ran on a mismatched input")
		return &product{ID: 1}, nil
	})
}

// queryHelperProbe returns a ForQuery[*getInput, *product] handler on products
// whose fn and terminal each fail the test if they run.
func queryHelperProbe(t *testing.T) hook.QueryHandler {
	t.Helper()
	h := hook.ForQuery[*getInput, *product](
		tableProducts,
		func(ctx context.Context, q *hook.QueryContext, input *getInput, next func(ctx context.Context) (*product, error)) (*product, error) {
			t.Error("fn ran on a mismatched input")
			return next(ctx)
		},
	)
	return h(func(context.Context, *hook.QueryContext) (any, error) {
		t.Error("next ran on a mismatched input")
		return &product{ID: 1}, nil
	})
}

func TestForQuery(t *testing.T) {
	t.Run("passes through when table does not match", func(t *testing.T) {
		hookCalled := false
		h := hook.ForQuery[*getInput, *product](
			tableProducts,
			func(ctx context.Context, q *hook.QueryContext, input *getInput, next func(ctx context.Context) (*product, error)) (*product, error) {
				hookCalled = true
				return next(ctx)
			},
		)

		terminal := terminalQuery(&product{ID: 1, Name: "widget"})
		handler := h(terminal)

		got, err := handler(context.Background(), &hook.QueryContext{
			Op:    hook.OpGet,
			Table: tableOrders, // different table
			Input: &getInput{ID: 1},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if hookCalled {
			t.Error("hook should not have been called for non-matching table")
		}
		p, ok := got.(*product)
		if !ok {
			t.Fatalf("result type = %T, want *product", got)
		}
		if p.ID != 1 {
			t.Errorf("result.ID = %d, want 1", p.ID)
		}
	})

	t.Run("executes typed fn when table matches", func(t *testing.T) {
		var capturedInput *getInput
		h := hook.ForQuery[*getInput, *product](
			tableProducts,
			func(ctx context.Context, q *hook.QueryContext, input *getInput, next func(ctx context.Context) (*product, error)) (*product, error) {
				capturedInput = input
				return next(ctx)
			},
		)

		input := &getInput{ID: 42}
		terminal := terminalQuery(&product{ID: 42, Name: "widget"})
		handler := h(terminal)

		got, err := handler(context.Background(), &hook.QueryContext{
			Op:    hook.OpGet,
			Table: tableProducts,
			Input: input,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if capturedInput != input {
			t.Error("hook did not receive the expected typed input")
		}
		p, ok := got.(*product)
		if !ok {
			t.Fatalf("result type = %T, want *product", got)
		}
		if p.ID != 42 {
			t.Errorf("result.ID = %d, want 42", p.ID)
		}
	})

	t.Run("an input of another type returns an error without running fn or next", func(t *testing.T) {
		handler := queryHelperProbe(t)
		_, err := handler(context.Background(), &hook.QueryContext{
			Op:    hook.OpCount,
			Table: tableProducts,
			Input: &createInput{},
		})
		want := "hook: type assertion failed for count products: expected *hook_test.getInput, got *hook_test.createInput"
		if err == nil || err.Error() != want {
			t.Errorf("error = %v, want %q", err, want)
		}
	})

	t.Run("a nil input returns an error without running fn or next", func(t *testing.T) {
		handler := queryHelperProbe(t)
		_, err := handler(context.Background(), &hook.QueryContext{
			Op:    hook.OpGet,
			Table: tableProducts,
			PK:    1,
		})
		want := "hook: type assertion failed for get products: expected *hook_test.getInput, got <nil>"
		if err == nil || err.Error() != want {
			t.Errorf("error = %v, want %q", err, want)
		}
	})

	t.Run("a result of another type returns an error after next ran", func(t *testing.T) {
		nextRan := false
		h := hook.ForQuery[*getInput, *product](
			tableProducts,
			func(ctx context.Context, q *hook.QueryContext, input *getInput, next func(ctx context.Context) (*product, error)) (*product, error) {
				return next(ctx)
			},
		)
		handler := h(func(context.Context, *hook.QueryContext) (any, error) {
			nextRan = true
			return []*product{{ID: 1}}, nil
		})

		_, err := handler(context.Background(), &hook.QueryContext{
			Op:    hook.OpGetMany,
			Table: tableProducts,
			Input: &getInput{},
		})
		want := "hook: result type assertion failed for get_many products: expected *hook_test.product, got []*hook_test.product"
		if err == nil || err.Error() != want {
			t.Errorf("error = %v, want %q", err, want)
		}
		if !nextRan {
			t.Error("next did not run before the result assertion")
		}
	})

	t.Run("a nil result reaches fn as the zero value", func(t *testing.T) {
		var gotResult *product
		var gotErr error
		called := false
		h := hook.ForQuery[*getInput, *product](
			tableProducts,
			func(ctx context.Context, q *hook.QueryContext, input *getInput, next func(ctx context.Context) (*product, error)) (*product, error) {
				called = true
				gotResult, gotErr = next(ctx)
				return gotResult, gotErr
			},
		)
		handler := h(terminalQuery(nil))

		if _, err := handler(context.Background(), &hook.QueryContext{
			Op:    hook.OpGetMany,
			Table: tableProducts,
			Input: &getInput{},
		}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !called {
			t.Fatal("fn did not run")
		}
		if gotResult != nil || gotErr != nil {
			t.Errorf("typed next = (%v, %v), want (nil, nil)", gotResult, gotErr)
		}
	})

	t.Run("fn can return a result without calling next", func(t *testing.T) {
		cached := &product{ID: 7, Name: "cached"}
		h := hook.ForQuery[*getInput, *product](
			tableProducts,
			func(ctx context.Context, q *hook.QueryContext, input *getInput, next func(ctx context.Context) (*product, error)) (*product, error) {
				return cached, nil
			},
		)
		handler := h(func(context.Context, *hook.QueryContext) (any, error) {
			t.Error("next ran after fn short-circuited")
			return nil, nil
		})

		got, err := handler(context.Background(), &hook.QueryContext{
			Op:    hook.OpGetMany,
			Table: tableProducts,
			Input: &getInput{},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != cached {
			t.Errorf("result = %v, want fn's result", got)
		}
	})
}

// TestHelpersMatchTheTableValueAlone pins that the table helpers compare the
// whole TableName value, so a hook on public.users never fires for
// audit.users.
func TestHelpersMatchTheTableValueAlone(t *testing.T) {
	const (
		publicUsers hook.TableName = "public.users"
		auditUsers  hook.TableName = "audit.users"
	)

	t.Run("ForMutation", func(t *testing.T) {
		fired := false
		h := hook.ForMutation[*createInput, *product](publicUsers,
			func(ctx context.Context, _ *hook.MutationContext, _ *createInput, next func(context.Context) (*product, error)) (*product, error) {
				fired = true
				return next(ctx)
			})
		handler := h(terminalMutation(&product{ID: 1}))
		for _, tc := range []struct {
			table hook.TableName
			want  bool
		}{{auditUsers, false}, {publicUsers, true}} {
			fired = false
			if _, err := handler(context.Background(), &hook.MutationContext{Op: hook.OpCreate, Table: tc.table, Input: &createInput{}}); err != nil {
				t.Fatalf("%s: unexpected error: %v", tc.table, err)
			}
			if fired != tc.want {
				t.Errorf("%s: fired = %v, want %v", tc.table, fired, tc.want)
			}
		}
	})

	t.Run("ForQuery", func(t *testing.T) {
		fired := false
		h := hook.ForQuery[*getInput, *product](publicUsers,
			func(ctx context.Context, _ *hook.QueryContext, _ *getInput, next func(context.Context) (*product, error)) (*product, error) {
				fired = true
				return next(ctx)
			})
		handler := h(terminalQuery(&product{ID: 1}))
		for _, tc := range []struct {
			table hook.TableName
			want  bool
		}{{auditUsers, false}, {publicUsers, true}} {
			fired = false
			if _, err := handler(context.Background(), &hook.QueryContext{Op: hook.OpGetMany, Table: tc.table, Input: &getInput{}}); err != nil {
				t.Fatalf("%s: unexpected error: %v", tc.table, err)
			}
			if fired != tc.want {
				t.Errorf("%s: fired = %v, want %v", tc.table, fired, tc.want)
			}
		}
	})

	t.Run("ForTable", func(t *testing.T) {
		fired := false
		inner := hook.MutationHook(func(next hook.MutationHandler) hook.MutationHandler {
			return func(ctx context.Context, m *hook.MutationContext) (any, error) {
				fired = true
				return next(ctx, m)
			}
		})
		handler := hook.ForTable(inner, publicUsers)(terminalMutation("ok"))
		for _, tc := range []struct {
			table hook.TableName
			want  bool
		}{{auditUsers, false}, {publicUsers, true}} {
			fired = false
			if _, err := handler(context.Background(), &hook.MutationContext{Op: hook.OpCreate, Table: tc.table}); err != nil {
				t.Fatalf("%s: unexpected error: %v", tc.table, err)
			}
			if fired != tc.want {
				t.Errorf("%s: fired = %v, want %v", tc.table, fired, tc.want)
			}
		}
	})
}

func TestOnMutation(t *testing.T) {
	tests := []struct {
		name       string
		filterOps  []hook.MutationOp
		actualOp   hook.MutationOp
		wantCalled bool
	}{
		{
			name:       "fires on matching op",
			filterOps:  []hook.MutationOp{hook.OpCreate, hook.OpUpdate},
			actualOp:   hook.OpCreate,
			wantCalled: true,
		},
		{
			name:       "fires on second matching op",
			filterOps:  []hook.MutationOp{hook.OpCreate, hook.OpUpdate},
			actualOp:   hook.OpUpdate,
			wantCalled: true,
		},
		{
			name:       "passes through on non-matching op",
			filterOps:  []hook.MutationOp{hook.OpCreate, hook.OpUpdate},
			actualOp:   hook.OpHardDelete,
			wantCalled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hookCalled := false
			inner := hook.MutationHook(func(next hook.MutationHandler) hook.MutationHandler {
				return func(ctx context.Context, m *hook.MutationContext) (any, error) {
					hookCalled = true
					return next(ctx, m)
				}
			})

			h := hook.OnMutation(inner, tt.filterOps...)
			handler := h(terminalMutation("ok"))

			got, err := handler(context.Background(), &hook.MutationContext{
				Op:    tt.actualOp,
				Table: tableProducts,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if hookCalled != tt.wantCalled {
				t.Errorf("hook called = %v, want %v", hookCalled, tt.wantCalled)
			}
			if got != "ok" {
				t.Errorf("result = %v, want the next handler's %q", got, "ok")
			}
		})
	}
}

func TestOnQuery(t *testing.T) {
	tests := []struct {
		name       string
		filterOps  []hook.QueryOp
		actualOp   hook.QueryOp
		wantCalled bool
	}{
		{
			name:       "fires on matching op",
			filterOps:  []hook.QueryOp{hook.OpGetMany, hook.OpCount},
			actualOp:   hook.OpGetMany,
			wantCalled: true,
		},
		{
			name:       "fires on second matching op",
			filterOps:  []hook.QueryOp{hook.OpGetMany, hook.OpCount},
			actualOp:   hook.OpCount,
			wantCalled: true,
		},
		{
			name:       "passes through on non-matching op",
			filterOps:  []hook.QueryOp{hook.OpGetMany, hook.OpCount},
			actualOp:   hook.OpGet,
			wantCalled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hookCalled := false
			inner := hook.QueryHook(func(next hook.QueryHandler) hook.QueryHandler {
				return func(ctx context.Context, q *hook.QueryContext) (any, error) {
					hookCalled = true
					return next(ctx, q)
				}
			})

			h := hook.OnQuery(inner, tt.filterOps...)
			handler := h(terminalQuery("ok"))

			got, err := handler(context.Background(), &hook.QueryContext{
				Op:    tt.actualOp,
				Table: tableProducts,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if hookCalled != tt.wantCalled {
				t.Errorf("hook called = %v, want %v", hookCalled, tt.wantCalled)
			}
			if got != "ok" {
				t.Errorf("result = %v, want the next handler's %q", got, "ok")
			}
		})
	}
}

func TestForTable(t *testing.T) {
	tests := []struct {
		name        string
		filterTbls  []hook.TableName
		actualTable hook.TableName
		wantCalled  bool
	}{
		{
			name:        "fires on matching table",
			filterTbls:  []hook.TableName{tableProducts, tableOrders},
			actualTable: tableProducts,
			wantCalled:  true,
		},
		{
			name:        "fires on second matching table",
			filterTbls:  []hook.TableName{tableProducts, tableOrders},
			actualTable: tableOrders,
			wantCalled:  true,
		},
		{
			name:        "passes through on non-matching table",
			filterTbls:  []hook.TableName{tableProducts, tableOrders},
			actualTable: tableUsers,
			wantCalled:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hookCalled := false
			inner := hook.MutationHook(func(next hook.MutationHandler) hook.MutationHandler {
				return func(ctx context.Context, m *hook.MutationContext) (any, error) {
					hookCalled = true
					return next(ctx, m)
				}
			})

			h := hook.ForTable(inner, tt.filterTbls...)
			handler := h(terminalMutation("ok"))

			got, err := handler(context.Background(), &hook.MutationContext{
				Op:    hook.OpCreate,
				Table: tt.actualTable,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if hookCalled != tt.wantCalled {
				t.Errorf("hook called = %v, want %v", hookCalled, tt.wantCalled)
			}
			if got != "ok" {
				t.Errorf("result = %v, want the next handler's %q", got, "ok")
			}
		})
	}
}

func TestRejectMutation(t *testing.T) {
	t.Run("returns error on matching op", func(t *testing.T) {
		h := hook.RejectMutation(hook.OpHardDelete)
		handler := h(func(context.Context, *hook.MutationContext) (any, error) {
			t.Error("next ran for a rejected op")
			return "ok", nil
		})

		got, err := handler(context.Background(), &hook.MutationContext{
			Op:    hook.OpHardDelete,
			Table: "public.products",
		})
		if want := "hook: operation hard_delete is not allowed on public.products"; err == nil || err.Error() != want {
			t.Errorf("error = %v, want %q", err, want)
		}
		if got != nil {
			t.Errorf("result = %v, want nil", got)
		}
	})

	t.Run("rejects every listed op on every table", func(t *testing.T) {
		h := hook.RejectMutation(hook.OpHardDelete, hook.OpHardDeleteMany)
		handler := h(func(context.Context, *hook.MutationContext) (any, error) {
			t.Error("next ran for a rejected op")
			return "ok", nil
		})

		for _, table := range []hook.TableName{tableProducts, tableOrders} {
			for _, op := range []hook.MutationOp{hook.OpHardDelete, hook.OpHardDeleteMany} {
				_, err := handler(context.Background(), &hook.MutationContext{Op: op, Table: table})
				if want := "hook: operation " + string(op) + " is not allowed on " + string(table); err == nil || err.Error() != want {
					t.Errorf("%s %s: error = %v, want %q", op, table, err, want)
				}
			}
		}
	})

	t.Run("passes through on non-matching op", func(t *testing.T) {
		h := hook.RejectMutation(hook.OpHardDelete)
		handler := h(terminalMutation("ok"))

		got, err := handler(context.Background(), &hook.MutationContext{
			Op:    hook.OpCreate,
			Table: tableProducts,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "ok" {
			t.Errorf("result = %v, want %q", got, "ok")
		}
	})
}

func TestComposedHelpers(t *testing.T) {
	// OnMutation(ForTable(hook, tables...), ops...) should filter by both table and op.
	hookCalled := false
	inner := hook.MutationHook(func(next hook.MutationHandler) hook.MutationHandler {
		return func(ctx context.Context, m *hook.MutationContext) (any, error) {
			hookCalled = true
			return next(ctx, m)
		}
	})

	composed := hook.OnMutation(
		hook.ForTable(inner, tableProducts),
		hook.OpCreate, hook.OpUpdate,
	)

	tests := []struct {
		name       string
		table      hook.TableName
		op         hook.MutationOp
		wantCalled bool
	}{
		{
			name:       "matching table and op",
			table:      tableProducts,
			op:         hook.OpCreate,
			wantCalled: true,
		},
		{
			name:       "matching table wrong op",
			table:      tableProducts,
			op:         hook.OpHardDelete,
			wantCalled: false,
		},
		{
			name:       "wrong table matching op",
			table:      tableOrders,
			op:         hook.OpCreate,
			wantCalled: false,
		},
		{
			name:       "wrong table and op",
			table:      tableOrders,
			op:         hook.OpHardDelete,
			wantCalled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hookCalled = false
			handler := composed(terminalMutation("ok"))

			_, err := handler(context.Background(), &hook.MutationContext{
				Op:    tt.op,
				Table: tt.table,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if hookCalled != tt.wantCalled {
				t.Errorf("hook called = %v, want %v", hookCalled, tt.wantCalled)
			}
		})
	}
}
