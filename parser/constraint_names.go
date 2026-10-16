package parser

import (
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// postgresNameLimit is the longest identifier PostgreSQL stores, in bytes
// (NAMEDATALEN - 1).
const postgresNameLimit = 63

// DerivePostgresConstraintName returns the name PostgreSQL gives an unnamed
// constraint of table: <table>_pkey, <table>_<cols>_key, <table>_<cols>_fkey,
// <table>_<col>_check (<table>_check when the CHECK references no column or
// several), or <table>_<cols>_idx for an index. taken reports whether a name is
// already in use where PostgreSQL looks for a clash; a clash moves the label to
// pkey1, pkey2, and so on. For an index, c.Columns holds the names PostgreSQL
// chooses for its columns (see PostgresIndexColumnNames).
func DerivePostgresConstraintName(table string, c Constraint, taken func(string) bool) string {
	switch c.Type {
	case PrimaryKey:
		return choosePostgresName(table, "", "pkey", taken)
	case Unique:
		return choosePostgresName(table, postgresNameAddition(c.Columns), "key", taken)
	case ForeignKey:
		return choosePostgresName(table, postgresNameAddition(c.Columns), "fkey", taken)
	case Check:
		var col string
		if len(c.Columns) == 1 {
			col = c.Columns[0]
		}
		return choosePostgresName(table, col, "check", taken)
	case Index:
		return choosePostgresName(table, postgresNameAddition(c.Columns), "idx", taken)
	}
	return ""
}

// DerivePostgresSequenceName returns the name PostgreSQL gives the sequence
// it creates for a serial or identity column: <table>_<column>_seq, or
// <table>_<column>_seq1, … when taken reports the name in use by a relation.
func DerivePostgresSequenceName(table, column string, taken func(string) bool) string {
	return choosePostgresName(table, column, "seq", taken)
}

// PostgresIndexColumnNames returns the names PostgreSQL uses for an index's
// columns when it names the index: a column that repeats an earlier one gets a
// numeric suffix, so (a, a) names as a_a1.
func PostgresIndexColumnNames(cols []string) []string {
	out := make([]string, 0, len(cols))
	for _, orig := range cols {
		name := orig
		for i := 1; slices.Contains(out, name); i++ {
			suffix := strconv.Itoa(i)
			name = clipUTF8(orig, postgresNameLimit-len(suffix)) + suffix
		}
		out = append(out, name)
	}
	return out
}

// choosePostgresName is PostgreSQL's ChooseRelationName / ChooseConstraintName:
// the first of name1_name2_label, name1_name2_label1, … that taken rejects.
func choosePostgresName(name1, name2, label string, taken func(string) bool) string {
	modLabel := label
	for pass := 1; ; pass++ {
		name := postgresObjectName(name1, name2, modLabel)
		if !taken(name) {
			return name
		}
		modLabel = label + strconv.Itoa(pass)
	}
}

// postgresObjectName is PostgreSQL's makeObjectName: name1, name2 and label
// joined with "_", with name1 and name2 cut, the longer one first, until the
// whole fits in 63 bytes. A cut never splits a multibyte character.
func postgresObjectName(name1, name2, label string) string {
	overhead := len(label) + 1
	if name2 != "" {
		overhead++
	}
	avail := postgresNameLimit - overhead
	n1, n2 := len(name1), len(name2)
	for n1+n2 > avail {
		if n1 > n2 {
			n1--
		} else {
			n2--
		}
	}
	var b strings.Builder
	b.WriteString(clipUTF8(name1, n1))
	if name2 != "" {
		b.WriteByte('_')
		b.WriteString(clipUTF8(name2, n2))
	}
	b.WriteByte('_')
	b.WriteString(label)
	return b.String()
}

// postgresNameAddition is PostgreSQL's ChooseIndexNameAddition: the column
// names joined with "_", stopping once 64 bytes are reached.
func postgresNameAddition(cols []string) string {
	var b strings.Builder
	for _, col := range cols {
		if b.Len() > 0 {
			b.WriteByte('_')
		}
		b.WriteString(clipUTF8(col, postgresNameLimit))
		if b.Len() > postgresNameLimit {
			break
		}
	}
	return b.String()
}

// clipUTF8 returns the longest prefix of s that is at most n bytes and ends on
// a character boundary.
func clipUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// DeriveMySQLConstraintName returns the name MySQL gives an unnamed constraint
// of table. A PRIMARY KEY is always PRIMARY. A FOREIGN KEY is <table>_ibfk_<n>
// and a CHECK <table>_chk_<n>, where n is one more than the highest n among
// existing names of that form. A UNIQUE or index takes its first column's name,
// or that name with _2, _3, … when an existing key name or PRIMARY already
// has it; key names compare case-insensitively.
//
// existing holds the names the database compares against at that moment.
// Within one CREATE TABLE that is the generated FOREIGN KEY or CHECK names so
// far, or every key name so far; an ALTER TABLE compares against every name of
// that kind the table already has.
func DeriveMySQLConstraintName(table string, c Constraint, existing []string) string {
	switch c.Type {
	case PrimaryKey:
		return "PRIMARY"
	case ForeignKey:
		return mysqlGeneratedName(table, "_ibfk_", existing)
	case Check:
		return mysqlGeneratedName(table, "_chk_", existing)
	case Unique, Index:
		if len(c.Columns) == 0 {
			return ""
		}
		return mysqlKeyName(c.Columns[0], existing)
	}
	return ""
}

// mysqlGeneratedName returns table+infix+n for the next n after the highest
// one among existing names of that form.
func mysqlGeneratedName(table, infix string, existing []string) string {
	prefix := table + infix
	highest := 0
	for _, name := range existing {
		digits, ok := strings.CutPrefix(name, prefix)
		if !ok {
			continue
		}
		if n, err := strconv.Atoi(digits); err == nil && n > highest && strconv.Itoa(n) == digits {
			highest = n
		}
	}
	return prefix + strconv.Itoa(highest+1)
}

// mysqlKeyName is MySQL's make_unique_key_name.
func mysqlKeyName(column string, existing []string) string {
	used := func(name string) bool {
		if strings.EqualFold(name, "PRIMARY") {
			return true
		}
		for _, e := range existing {
			if strings.EqualFold(e, name) {
				return true
			}
		}
		return false
	}
	if !used(column) {
		return column
	}
	for i := 2; ; i++ {
		if name := column + "_" + strconv.Itoa(i); !used(name) {
			return name
		}
	}
}

// RenameMySQLGeneratedName returns name with its table part replaced when name
// has the <oldTable>_ibfk_<n> or <oldTable>_chk_<n> form MySQL renames along
// with its table, and name unchanged otherwise.
func RenameMySQLGeneratedName(name, oldTable, newTable string) string {
	for _, infix := range []string{"_ibfk_", "_chk_"} {
		digits, ok := strings.CutPrefix(name, oldTable+infix)
		if !ok || digits == "" {
			continue
		}
		if strings.Trim(digits, "0123456789") == "" {
			return newTable + infix + digits
		}
	}
	return name
}
