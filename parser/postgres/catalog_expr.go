package postgres

import (
	"regexp"
	"slices"
	"strconv"
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v6"

	"github.com/teandresmith/sqlgen/parser"
)

// CatalogExpr rewrites an expression read from the PostgreSQL catalog
// (pg_get_expr, pg_get_constraintdef) in the form the parser writes the same
// expression from DDL. On top of NormalizeExpr's deparse it removes the casts
// the catalog adds on its own: the type label on every constant, and a cast of
// a column to a type PostgreSQL coerces it to implicitly. implicitCasts maps a
// column name ("value" in a domain constraint) to those types, written as the
// parser writes types and without modifiers.
func CatalogExpr(expr string, implicitCasts map[string][]string) (string, error) {
	node, err := parseExpr(expr)
	if err != nil {
		return "", err
	}
	walkExpr(node, func(n *pg_query.Node) {
		stripLiteralCast(n)
		stripImplicitColumnCast(n, implicitCasts)
	})
	return deparseExpr(node), nil
}

// TableImplicitCasts returns CatalogExpr's implicitCasts for an expression
// over the columns of table: each column maps to the types PostgreSQL casts it
// to on its own. domains holds the domain types the column types may name.
func TableImplicitCasts(table *parser.Table, domains []parser.DomainType) map[string][]string {
	casts := make(map[string][]string)
	for _, c := range table.Columns {
		if targets := implicitTargets(c.Type, table.Schema, domains); len(targets) > 0 {
			casts[c.Name] = targets
		}
	}
	return casts
}

// DomainImplicitCasts returns CatalogExpr's implicitCasts for a constraint of
// the domain dt, whose VALUE has dt's base type.
func DomainImplicitCasts(dt *parser.DomainType, domains []parser.DomainType) map[string][]string {
	return map[string][]string{"value": implicitTargets(dt.BaseType, dt.Schema, domains)}
}

// implicitTargets lists the types a value of type typ, written relative to
// schema, is cast to without being asked: a domain's base type, that type's
// base, and so on, and text for a character type.
func implicitTargets(typ, schema string, domains []parser.DomainType) []string {
	var targets []string
	for range len(domains) + 1 {
		elem, isArray := strings.CutSuffix(stripTypmods(typ), "[]")
		if elem == "varchar" || elem == "char" {
			if isArray {
				return append(targets, "text[]")
			}
			return append(targets, "text")
		}
		if isArray {
			return targets
		}
		d := findDomain(domains, elem, schema)
		if d == nil {
			return targets
		}
		typ, schema = d.BaseType, d.Schema
		targets = append(targets, stripTypmods(typ))
	}
	return targets
}

// findDomain returns the domain the type name typ, written relative to
// schema, names, or nil.
func findDomain(domains []parser.DomainType, typ, schema string) *parser.DomainType {
	if s, name, ok := strings.Cut(typ, "."); ok {
		schema, typ = s, name
	}
	for i := range domains {
		if domains[i].Schema == schema && domains[i].Name == typ {
			return &domains[i]
		}
	}
	return nil
}

// stripLiteralCast removes the cast from a constant: 'x'::text becomes 'x',
// (2)::numeric becomes 2. The catalog labels every constant whose type is not
// integer, numeric or boolean, whether or not the DDL wrote a cast. A number
// it prints quoted ('-1'::integer) becomes the number again.
func stripLiteralCast(n *pg_query.Node) {
	tc := n.GetTypeCast()
	if tc == nil || tc.Arg.GetAConst() == nil {
		return
	}
	c := tc.Arg.GetAConst()
	if s := c.GetSval(); s != nil && isNumericType(tc.TypeName) {
		if num := numericConst(s.Sval); num != nil {
			n.Node = num.Node
			return
		}
	}
	n.Node = tc.Arg.Node
}

// stripImplicitColumnCast removes a cast of a column to a type implicitCasts
// lists for it: a domain-typed column cast to its base type, as in
// (score)::integer, or a varchar column cast to text.
func stripImplicitColumnCast(n *pg_query.Node, implicitCasts map[string][]string) {
	tc := n.GetTypeCast()
	if tc == nil {
		return
	}
	ref := tc.Arg.GetColumnRef()
	if ref == nil {
		return
	}
	fields := extractStringList(ref.Fields)
	if len(fields) == 0 {
		return
	}
	target := stripTypmods(typeNameToString(tc.TypeName))
	if slices.Contains(implicitCasts[fields[len(fields)-1]], target) {
		n.Node = tc.Arg.Node
	}
}

// numericTypes are the types a quoted catalog constant is a number in.
var numericTypes = []string{"smallint", "integer", "bigint", "numeric", "decimal", "real", "double precision"}

func isNumericType(tn *pg_query.TypeName) bool {
	return slices.Contains(numericTypes, stripTypmods(typeNameToString(tn)))
}

// numericConst returns the constant the parser makes of the number s: an
// integer when it fits in 32 bits, otherwise a float holding s as written, as
// PostgreSQL's lexer does. It returns nil when s is not a decimal number.
func numericConst(s string) *pg_query.Node {
	if !isDecimalNumber(s) {
		return nil
	}
	if i, err := strconv.ParseInt(s, 10, 32); err == nil {
		return pg_query.MakeAConstIntNode(i, -1)
	}
	return &pg_query.Node{Node: &pg_query.Node_AConst{AConst: &pg_query.A_Const{
		Val: &pg_query.A_Const_Fval{Fval: &pg_query.Float{Fval: s}},
	}}}
}

// decimalNumber matches an optionally negative decimal number with an optional
// fraction and exponent, such as -1, 2.5 or 1e10.
var decimalNumber = regexp.MustCompile(`^-?([0-9]+\.?[0-9]*|\.[0-9]+)([eE][-+]?[0-9]+)?$`)

func isDecimalNumber(s string) bool {
	return decimalNumber.MatchString(s)
}

// stripTypmods drops a type's modifiers and keeps an array suffix:
// varchar(10)[] becomes varchar[].
func stripTypmods(t string) string {
	open := -1
	for i := range len(t) {
		if t[i] == '(' {
			open = i
			break
		}
	}
	if open < 0 {
		return t
	}
	closing := open
	for closing < len(t) && t[closing] != ')' {
		closing++
	}
	if closing == len(t) {
		return t
	}
	return t[:open] + t[closing+1:]
}

// walkExpr calls visit on every node of an expression tree, children before
// their parent, so a parent sees its children already rewritten. It descends
// through the node kinds a column default, CHECK or generated expression is
// built from; any other node is visited but not entered.
func walkExpr(n *pg_query.Node, visit func(*pg_query.Node)) {
	if n == nil {
		return
	}
	for _, c := range exprOperands(n) {
		walkExpr(c, visit)
	}
	visit(n)
}

// exprOperands returns the operands of an operator, function call, cast,
// array, CASE or list node. A nil entry is an operand the node lacks.
func exprOperands(n *pg_query.Node) []*pg_query.Node {
	switch x := n.Node.(type) {
	case *pg_query.Node_TypeCast:
		return []*pg_query.Node{x.TypeCast.Arg}
	case *pg_query.Node_AExpr:
		return []*pg_query.Node{x.AExpr.Lexpr, x.AExpr.Rexpr}
	case *pg_query.Node_BoolExpr:
		return x.BoolExpr.Args
	case *pg_query.Node_FuncCall:
		return x.FuncCall.Args
	case *pg_query.Node_AArrayExpr:
		return x.AArrayExpr.Elements
	case *pg_query.Node_CaseExpr:
		return append([]*pg_query.Node{x.CaseExpr.Arg, x.CaseExpr.Defresult}, x.CaseExpr.Args...)
	case *pg_query.Node_CaseWhen:
		return []*pg_query.Node{x.CaseWhen.Expr, x.CaseWhen.Result}
	case *pg_query.Node_List:
		return x.List.Items
	}
	return wrapperOperands(n)
}

// wrapperOperands returns the operands of the nodes that wrap a value list or
// a single value: COALESCE, GREATEST / LEAST, ROW, IS [NOT] NULL, IS [NOT]
// TRUE, COLLATE and subscripting.
func wrapperOperands(n *pg_query.Node) []*pg_query.Node {
	switch x := n.Node.(type) {
	case *pg_query.Node_CoalesceExpr:
		return x.CoalesceExpr.Args
	case *pg_query.Node_MinMaxExpr:
		return x.MinMaxExpr.Args
	case *pg_query.Node_RowExpr:
		return x.RowExpr.Args
	case *pg_query.Node_NullTest:
		return []*pg_query.Node{x.NullTest.Arg}
	case *pg_query.Node_BooleanTest:
		return []*pg_query.Node{x.BooleanTest.Arg}
	case *pg_query.Node_CollateClause:
		return []*pg_query.Node{x.CollateClause.Arg}
	case *pg_query.Node_AIndirection:
		return []*pg_query.Node{x.AIndirection.Arg}
	}
	return nil
}
