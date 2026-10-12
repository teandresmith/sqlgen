package parser

import (
	"slices"
	"strings"
	"unicode"
)

// CanonicalizeConstraints gives a table the constraint shape every parser and
// introspector shares (PRD §5.2 "Constraints"). dialect is "postgres", "mysql"
// or "sqlite". Afterwards:
//   - every column PrimaryKey, Unique and FKReference flag has a matching table
//     constraint, and every constraint sets its column flags
//   - a UNIQUE or index with no Method is btree on PostgreSQL and MySQL
//   - a CHECK's Columns lists the table columns its expression references, in
//     table order
//   - a constraint spells each column as the table declares it, matching the
//     names as the dialect does (SameColumnName)
//
// It names nothing. A parser names an unnamed constraint when it creates it,
// because the name depends on what else exists at that moment.
func CanonicalizeConstraints(table *Table, dialect string) {
	for i := range table.Constraints {
		declaredColumnNames(table, dialect, table.Constraints[i].Columns)
	}
	addFlagConstraints(table)
	for i := range table.Constraints {
		c := &table.Constraints[i]
		switch c.Type {
		case Unique, Index:
			if c.Method == "" && dialect != "sqlite" {
				c.Method = "btree"
			}
		case Check:
			if len(c.Columns) == 0 {
				c.Columns = CheckColumns(c.CheckExpression, table.Columns, dialect)
			}
		case PrimaryKey, ForeignKey:
		}
	}
	ApplyTableConstraints(table)
}

// declaredColumnNames rewrites each name in cols to the spelling of the table
// column it names in dialect (SameColumnName).
func declaredColumnNames(table *Table, dialect string, cols []string) {
	for i, name := range cols {
		cols[i] = declaredColumnName(table, dialect, name)
	}
}

// addFlagConstraints adds the constraint a column flag implies when the table
// has none: a PRIMARY KEY over every PrimaryKey column, a UNIQUE for a Unique
// column, and a FOREIGN KEY for a schema-declared FKReference.
func addFlagConstraints(table *Table) {
	var pkCols []string
	for _, col := range table.Columns {
		if col.PrimaryKey {
			pkCols = append(pkCols, col.Name)
		}
	}
	if len(pkCols) > 0 && !slices.ContainsFunc(table.Constraints, func(c Constraint) bool { return c.Type == PrimaryKey }) {
		table.Constraints = append(table.Constraints, Constraint{Type: PrimaryKey, Columns: pkCols})
	}
	for _, col := range table.Columns {
		if col.Unique && !hasConstraintOn(table, Unique, col.Name) {
			table.Constraints = append(table.Constraints, Constraint{Type: Unique, Columns: []string{col.Name}})
		}
		ref := col.FKReference
		if ref != nil && !ref.Synthetic && !hasConstraintOn(table, ForeignKey, col.Name) {
			c := Constraint{
				Type:            ForeignKey,
				Columns:         []string{col.Name},
				ReferenceTable:  ref.Table,
				ReferenceSchema: ref.Schema,
			}
			if ref.Column != "" {
				c.ReferenceColumns = []string{ref.Column}
			}
			table.Constraints = append(table.Constraints, c)
		}
	}
}

// hasConstraintOn reports whether the table has a constraint of kind over
// exactly the one column col. A partial UNIQUE does not count.
func hasConstraintOn(table *Table, kind ConstraintType, col string) bool {
	return slices.ContainsFunc(table.Constraints, func(c Constraint) bool {
		return c.Type == kind && c.Where == "" && len(c.Columns) == 1 && c.Columns[0] == col
	})
}

// CheckColumns returns the columns of cols that the CHECK expression expr
// references, in column order. Names match as SameColumnName matches them:
// MySQL ignores case, SQLite ignores ASCII case only, and PostgreSQL folds an
// unquoted name to lower case and then compares exactly. A function name, a type name
// after ::, a string literal and a charset introducer (_utf8mb4'x') are not
// column references.
func CheckColumns(expr string, cols []Column, dialect string) []string {
	refs := checkIdentifiers(expr, dialect)
	var out []string
	for _, col := range cols {
		if slices.ContainsFunc(refs, func(ref string) bool {
			return SameColumnName(dialect, ref, col.Name)
		}) {
			out = append(out, col.Name)
		}
	}
	return out
}

// checkIdentifiers returns the identifiers in expr that can name a column.
func checkIdentifiers(expr, dialect string) []string {
	sc := checkScanner{runes: []rune(expr), dialect: dialect}
	for sc.i < len(sc.runes) {
		sc.step()
	}
	return sc.idents
}

// checkScanner walks a CHECK expression and collects the identifiers in it.
type checkScanner struct {
	runes     []rune
	i         int
	dialect   string
	afterCast bool // the previous token was ::, so this one names a type
	idents    []string
}

func (sc *checkScanner) step() {
	r := sc.runes[sc.i]
	switch {
	case r == '\'':
		sc.i = skipQuoted(sc.runes, sc.i, '\'', sc.dialect == "mysql")
	case r == '"' || r == '`' || (r == '[' && sc.dialect == "sqlite"):
		sc.quoted(r)
	case sc.atCast():
		sc.afterCast = true
		sc.i += 2
	case unicode.IsDigit(r):
		sc.number()
	case r == '_' || unicode.IsLetter(r):
		sc.word()
	default:
		sc.afterCast = sc.afterCast && unicode.IsSpace(r)
		sc.i++
	}
}

func (sc *checkScanner) atCast() bool {
	return sc.runes[sc.i] == ':' && sc.i+1 < len(sc.runes) && sc.runes[sc.i+1] == ':'
}

// number reads a numeric literal, 1.5e3 included.
func (sc *checkScanner) number() {
	for sc.i < len(sc.runes) && (isCheckIdentPart(sc.runes[sc.i]) || sc.runes[sc.i] == '.') {
		sc.i++
	}
}

// quoted reads a quoted identifier that opens with open.
func (sc *checkScanner) quoted(open rune) {
	closing := open
	if open == '[' {
		closing = ']'
	}
	start := sc.i + 1
	sc.i = skipQuoted(sc.runes, sc.i, closing, false)
	end := max(start, sc.i-1)
	if !sc.afterCast {
		doubled := string(closing) + string(closing)
		sc.idents = append(sc.idents, strings.ReplaceAll(string(sc.runes[start:end]), doubled, string(closing)))
	}
	sc.afterCast = false
}

// word reads a bare word. A function name, a type name after :: and a
// charset introducer (_utf8mb4'x') do not name a column.
func (sc *checkScanner) word() {
	start := sc.i
	for sc.i < len(sc.runes) && isCheckIdentPart(sc.runes[sc.i]) {
		sc.i++
	}
	word := string(sc.runes[start:sc.i])
	next := sc.i
	for next < len(sc.runes) && unicode.IsSpace(sc.runes[next]) {
		next++
	}
	isCall := next < len(sc.runes) && sc.runes[next] == '('
	isIntroducer := sc.i < len(sc.runes) && sc.runes[sc.i] == '\''
	if !sc.afterCast && !isCall && !isIntroducer {
		if sc.dialect == "postgres" {
			word = strings.ToLower(word)
		}
		sc.idents = append(sc.idents, word)
	}
	sc.afterCast = false
}

// skipQuoted returns the index just past the quoted run that opens at
// runes[i] and closes at the next unescaped closing rune. A doubled closing
// rune is an escaped one; backslash also escapes when backslashEscapes is set.
func skipQuoted(runes []rune, i int, closing rune, backslashEscapes bool) int {
	for i++; i < len(runes); i++ {
		switch {
		case backslashEscapes && runes[i] == '\\':
			i++
		case runes[i] == closing:
			if i+1 < len(runes) && runes[i+1] == closing {
				i++
				continue
			}
			return i + 1
		}
	}
	return i
}

func isCheckIdentPart(r rune) bool {
	return r == '_' || r == '$' || unicode.IsLetter(r) || unicode.IsDigit(r)
}
