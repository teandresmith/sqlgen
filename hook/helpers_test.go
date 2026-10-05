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

	t.Run("type assertion failure returns error", func(t *testing.T) {
		h := hook.ForMutation[*createInput, *product](
			tableProducts,
			func(ctx context.Context, m *hook.MutationContext, input *createInput, next func(ctx context.Context) (*product, error)) (*product, error) {
				return next(ctx)
			},
		)

		terminal := terminalMutation(&product{ID: 1})
		handler := h(terminal)

		_, err := handler(context.Background(), &hook.MutationContext{
			Op:    hook.OpCreate,
			Table: tableProducts,
			Input: "wrong-type", // string instead of *createInput
		})
		if err == nil {
			t.Fatal("expected error for type assertion failure, got nil")
		}
		if !strings.Contains(err.Error(), "type assertion failed") {
			t.Errorf("error = %q, want it to contain %q", err.Error(), "type assertion failed")
		}
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

			_, err := handler(context.Background(), &hook.MutationContext{
				Op:    tt.actualOp,
				Table: tableProducts,
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

			_, err := handler(context.Background(), &hook.QueryContext{
				Op:    tt.actualOp,
				Table: tableProducts,
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

			_, err := handler(context.Background(), &hook.MutationContext{
				Op:    hook.OpCreate,
				Table: tt.actualTable,
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

func TestRejectMutation(t *testing.T) {
	t.Run("returns error on matching op", func(t *testing.T) {
		h := hook.RejectMutation(hook.OpHardDelete)
		handler := h(terminalMutation("ok"))

		_, err := handler(context.Background(), &hook.MutationContext{
			Op:    hook.OpHardDelete,
			Table: tableProducts,
		})
		if err == nil {
			t.Fatal("expected error for rejected operation, got nil")
		}
		if !strings.Contains(err.Error(), "not allowed") {
			t.Errorf("error = %q, want it to contain %q", err.Error(), "not allowed")
		}
		if !strings.Contains(err.Error(), string(hook.OpHardDelete)) {
			t.Errorf("error = %q, want it to contain op %q", err.Error(), hook.OpHardDelete)
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
