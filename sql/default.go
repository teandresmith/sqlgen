package sql

// defaultSentinel is the unexported type backing the Default sentinel value.
// It cannot be constructed outside this package.
type defaultSentinel struct{}

// Default is a sentinel value for use in multi-row INSERT value rows.
// When BuildMultiInsert encounters Default in a value position, it emits the
// SQL DEFAULT keyword instead of a placeholder, letting the database apply the
// column's default expression. This enables multi-row INSERTs where omittable
// fields differ per row while keeping all rows in the same column list.
var Default = defaultSentinel{}

// DefaultExpr is a sentinel value that carries a raw SQL default expression.
// When BuildMultiInsert encounters a DefaultExpr, it emits the expression
// inline instead of a placeholder. This is used for dialects that do not
// support the DEFAULT keyword in VALUES clauses (e.g., SQLite).
type DefaultExpr struct {
	Expr string
}

// NewDefaultExpr creates a DefaultExpr sentinel with the given SQL expression.
func NewDefaultExpr(expr string) DefaultExpr {
	return DefaultExpr{Expr: expr}
}

// isDefault reports whether v is the Default sentinel value.
func isDefault(v any) bool {
	_, ok := v.(defaultSentinel)
	return ok
}

// isDefaultExpr reports whether v is a DefaultExpr sentinel and returns it.
func isDefaultExpr(v any) (DefaultExpr, bool) {
	de, ok := v.(DefaultExpr)
	return de, ok
}
