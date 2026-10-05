package hook

import (
	"context"
	"fmt"
	"strings"
)

// PanicHandler is called when a panic is recovered during hook chain execution.
// It receives the context, recovered value, table name, and operation, and
// returns the error that the caller will see.
type PanicHandler func(ctx context.Context, r any, table TableName, op string) error

// BuildMutationChain composes the full mutation hook chain.
// Hooks are applied in registration order (first registered = outermost).
// Panic recovery wraps the entire chain as the outermost layer.
// If panicHandler is nil, the default handler returns a descriptive error.
func BuildMutationChain(panicHandler PanicHandler, hooks []MutationHook, terminal MutationHandler) MutationHandler {
	handler := terminal
	for i := len(hooks) - 1; i >= 0; i-- {
		handler = hooks[i](handler)
	}
	return recoverMutation(panicHandler, handler)
}

// BuildQueryChain composes the full query hook chain.
// Hooks are applied in registration order (first registered = outermost).
// Panic recovery wraps the entire chain as the outermost layer.
// If panicHandler is nil, the default handler returns a descriptive error.
func BuildQueryChain(panicHandler PanicHandler, hooks []QueryHook, terminal QueryHandler) QueryHandler {
	handler := terminal
	for i := len(hooks) - 1; i >= 0; i-- {
		handler = hooks[i](handler)
	}
	return recoverQuery(panicHandler, handler)
}

func recoverMutation(panicHandler PanicHandler, next MutationHandler) MutationHandler {
	return func(ctx context.Context, m *MutationContext) (result any, err error) {
		defer func() {
			if r := recover(); r != nil {
				if panicHandler != nil {
					err = panicHandler(ctx, r, m.Table, string(m.Op))
				} else {
					err = fmt.Errorf("sqlgen: panic in %s %s: %v", m.Op, panicTableName(m.Schema, m.Table), r)
				}
			}
		}()
		return next(ctx, m)
	}
}

func recoverQuery(panicHandler PanicHandler, next QueryHandler) QueryHandler {
	return func(ctx context.Context, q *QueryContext) (result any, err error) {
		defer func() {
			if r := recover(); r != nil {
				if panicHandler != nil {
					err = panicHandler(ctx, r, q.Table, string(q.Op))
				} else {
					err = fmt.Errorf("sqlgen: panic in %s %s: %v", q.Op, panicTableName(q.Schema, q.Table), r)
				}
			}
		}()
		return next(ctx, q)
	}
}

// panicTableName renders the table for the default panic error. A generated
// client stamps Table with the TableXxx constant's value, which already
// carries the schema whenever the table has one ("public.products" on
// PostgreSQL, "products" on MySQL / SQLite — PRD §5.5), so the schema is
// prefixed only to a value that lacks it and is never printed twice.
// An empty schema adds nothing.
func panicTableName(schema string, table TableName) string {
	name := string(table)
	if schema != "" && !strings.HasPrefix(name, schema+".") {
		name = schema + "." + name
	}
	return name
}
