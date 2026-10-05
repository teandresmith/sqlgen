package hook

import (
	"context"
	"fmt"
	"slices"
)

// ForMutation creates a type-safe mutation hook scoped to a specific table.
// When the table matches, it performs type assertions on Input and Result and
// calls fn with typed values. When the table does not match, it passes through
// to next without executing fn.
func ForMutation[Input any, Result any](table TableName, fn func(ctx context.Context, m *MutationContext, input Input, next func(ctx context.Context) (Result, error)) (Result, error)) MutationHook {
	return func(next MutationHandler) MutationHandler {
		return func(ctx context.Context, m *MutationContext) (any, error) {
			if m.Table != table {
				return next(ctx, m)
			}
			input, ok := m.Input.(Input)
			if !ok {
				var zero Input
				return nil, fmt.Errorf("hook: type assertion failed for %s %s: expected %T, got %T", m.Op, m.Table, zero, m.Input)
			}
			typedNext := func(ctx context.Context) (Result, error) {
				result, err := next(ctx, m)
				if err != nil {
					var zero Result
					return zero, err
				}
				if result == nil {
					var zero Result
					return zero, nil
				}
				typed, ok := result.(Result)
				if !ok {
					var zero Result
					return zero, fmt.Errorf("hook: result type assertion failed for %s %s: expected %T, got %T", m.Op, m.Table, zero, result)
				}
				return typed, nil
			}
			return fn(ctx, m, input, typedNext)
		}
	}
}

// ForQuery creates a type-safe query hook scoped to a specific table.
// When the table matches, it performs type assertions on Input and Result and
// calls fn with typed values. When the table does not match, it passes through
// to next without executing fn.
func ForQuery[Input any, Result any](table TableName, fn func(ctx context.Context, q *QueryContext, input Input, next func(ctx context.Context) (Result, error)) (Result, error)) QueryHook {
	return func(next QueryHandler) QueryHandler {
		return func(ctx context.Context, q *QueryContext) (any, error) {
			if q.Table != table {
				return next(ctx, q)
			}
			input, ok := q.Input.(Input)
			if !ok {
				var zero Input
				return nil, fmt.Errorf("hook: type assertion failed for %s %s: expected %T, got %T", q.Op, q.Table, zero, q.Input)
			}
			typedNext := func(ctx context.Context) (Result, error) {
				result, err := next(ctx, q)
				if err != nil {
					var zero Result
					return zero, err
				}
				if result == nil {
					var zero Result
					return zero, nil
				}
				typed, ok := result.(Result)
				if !ok {
					var zero Result
					return zero, fmt.Errorf("hook: result type assertion failed for %s %s: expected %T, got %T", q.Op, q.Table, zero, result)
				}
				return typed, nil
			}
			return fn(ctx, q, input, typedNext)
		}
	}
}

// OnMutation wraps a mutation hook so it only executes when the operation
// matches one of the specified ops. Non-matching operations pass through
// to next without executing the hook.
func OnMutation(hook MutationHook, ops ...MutationOp) MutationHook {
	return func(next MutationHandler) MutationHandler {
		wrapped := hook(next)
		return func(ctx context.Context, m *MutationContext) (any, error) {
			if slices.Contains(ops, m.Op) {
				return wrapped(ctx, m)
			}
			return next(ctx, m)
		}
	}
}

// OnQuery wraps a query hook so it only executes when the operation
// matches one of the specified ops. Non-matching operations pass through
// to next without executing the hook.
func OnQuery(hook QueryHook, ops ...QueryOp) QueryHook {
	return func(next QueryHandler) QueryHandler {
		wrapped := hook(next)
		return func(ctx context.Context, q *QueryContext) (any, error) {
			if slices.Contains(ops, q.Op) {
				return wrapped(ctx, q)
			}
			return next(ctx, q)
		}
	}
}

// ForTable wraps a mutation hook so it only executes when the table
// matches one of the specified tables. Non-matching tables pass through
// to next without executing the hook.
func ForTable(hook MutationHook, tables ...TableName) MutationHook {
	return func(next MutationHandler) MutationHandler {
		wrapped := hook(next)
		return func(ctx context.Context, m *MutationContext) (any, error) {
			if slices.Contains(tables, m.Table) {
				return wrapped(ctx, m)
			}
			return next(ctx, m)
		}
	}
}

// RejectMutation returns a mutation hook that blocks operations matching
// any of the specified ops by returning an error. Non-matching operations
// pass through to next.
func RejectMutation(ops ...MutationOp) MutationHook {
	return func(next MutationHandler) MutationHandler {
		return func(ctx context.Context, m *MutationContext) (any, error) {
			if slices.Contains(ops, m.Op) {
				return nil, fmt.Errorf("hook: operation %s is not allowed on %s", m.Op, m.Table)
			}
			return next(ctx, m)
		}
	}
}
