package roundtrip

import (
	"bufio"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// DocPath is docs/round-trips.md relative to a conformance dialect module,
// the directory its tests run in.
const DocPath = "../../../../../docs/round-trips.md"

// Target is one dialect and driver the report has a column for.
type Target struct {
	Dialect string
	Driver  string
	// Column is the target's column heading.
	Column string
}

// Targets are the report's columns, in order.
var Targets = []Target{
	{Dialect: "postgres", Driver: "pgx", Column: "Postgres pgx"},
	{Dialect: "postgres", Driver: "stdlib", Column: "Postgres stdlib"},
	{Dialect: "mysql", Driver: "stdlib", Column: "MySQL"},
	{Dialect: "sqlite", Driver: "stdlib", Column: "SQLite"},
}

func targetIndex(dialect, driver string) int {
	return slices.IndexFunc(Targets, func(t Target) bool { return t.Dialect == dialect && t.Driver == driver })
}

// Result is one suite's measurements on one target: the fragment a dialect
// module writes for `make round-trips`.
type Result struct {
	Suite   string `json:"suite"`
	Dialect string `json:"dialect"`
	Driver  string `json:"driver"`
	Rows    []Row  `json:"rows"`
}

// Row is one operation and shape, measured.
type Row struct {
	Group      string `json:"group"`
	Op         string `json:"op"`
	Shape      string `json:"shape"`
	Grows      string `json:"grows,omitempty"`
	Statements int    `json:"statements"`
	Rounds     int    `json:"rounds"`
	Tx         bool   `json:"tx"`
	// SQL is what was sent, for a reader of the fragment; the report leaves
	// it out.
	SQL []string `json:"sql,omitempty"`
}

func (r Row) key() string { return r.Group + "\x00" + r.Op + "\x00" + r.Shape }

func (r Row) cell() string { return strconv.Itoa(r.Statements) + " / " + strconv.Itoa(r.Rounds) }

// header is the header row of every report table.
func header() []string {
	h := []string{"Operation", "Shape"}
	for _, t := range Targets {
		h = append(h, t.Column)
	}
	return append(h, "Transaction", "Grows with")
}

// docRow is one row of a report table: the measured cells by target, and
// which targets open a transaction.
type docRow struct {
	group, op, shape, grows string
	cells                   []string // by Targets index; "n/a" where the target has no row
	tx                      []bool
	line                    int
}

func (d docRow) key() string { return d.group + "\x00" + d.op + "\x00" + d.shape }

// Render writes the report: intro, then one table per group, groups and rows
// in the order the results list them. Every target of every suite must be
// present.
func Render(intro string, results []Result) (string, error) {
	var suites []string
	bySuite := map[string][]*Result{}
	for i := range results {
		r := &results[i]
		if targetIndex(r.Dialect, r.Driver) < 0 {
			return "", fmt.Errorf("result for %s/%s: not a report target", r.Dialect, r.Driver)
		}
		if !slices.Contains(suites, r.Suite) {
			suites = append(suites, r.Suite)
		}
		bySuite[r.Suite] = append(bySuite[r.Suite], r)
	}
	slices.SortStableFunc(suites, func(a, b string) int { return suiteOrder(a) - suiteOrder(b) })

	var groups []string
	rows := map[string]*docRow{}
	var order []string
	for _, s := range suites {
		rs := bySuite[s]
		slices.SortFunc(rs, func(a, b *Result) int {
			return targetIndex(a.Dialect, a.Driver) - targetIndex(b.Dialect, b.Driver)
		})
		for i, t := range Targets {
			if i >= len(rs) || rs[i].Dialect != t.Dialect || rs[i].Driver != t.Driver {
				return "", fmt.Errorf("suite %s: no result, or more than one, for %s/%s", s, t.Dialect, t.Driver)
			}
		}
		for _, r := range rs {
			ti := targetIndex(r.Dialect, r.Driver)
			for _, row := range r.Rows {
				if err := checkCells(row.Group, row.Op, row.Shape, row.Grows); err != nil {
					return "", fmt.Errorf("suite %s, %s/%s: %w", s, r.Dialect, r.Driver, err)
				}
				d, ok := rows[row.key()]
				if !ok {
					d = &docRow{group: row.Group, op: row.Op, shape: row.Shape, grows: row.Grows, cells: make([]string, len(Targets)), tx: make([]bool, len(Targets))}
					for i := range d.cells {
						d.cells[i] = "n/a"
					}
					rows[row.key()] = d
					order = append(order, row.key())
					if !slices.Contains(groups, row.Group) {
						groups = append(groups, row.Group)
					}
				} else if d.grows != row.Grows {
					return "", fmt.Errorf("%s %s: %s/%s says it grows with %q, another target %q", row.Op, row.Shape, r.Dialect, r.Driver, row.Grows, d.grows)
				}
				if d.cells[ti] != "n/a" {
					return "", fmt.Errorf("%s %s: measured twice on %s/%s", row.Op, row.Shape, r.Dialect, r.Driver)
				}
				d.cells[ti], d.tx[ti] = row.cell(), row.Tx
			}
		}
	}

	var b strings.Builder
	b.WriteString(strings.TrimRight(intro, "\n"))
	b.WriteString("\n")
	for _, g := range groups {
		fmt.Fprintf(&b, "\n## %s\n\n", g)
		writeRow(&b, header())
		sep := make([]string, len(header()))
		for i := range sep {
			sep[i] = "---"
		}
		writeRow(&b, sep)
		for _, k := range order {
			if d := rows[k]; d.group == g {
				writeRow(&b, d.cellsOut())
			}
		}
	}
	var differs []string
	for _, k := range order {
		if line := rows[k].difference(); line != "" {
			differs = append(differs, line)
		}
	}
	if len(differs) > 0 {
		b.WriteString("\n")
		b.WriteString(strings.TrimRight(DiffersIntro, "\n"))
		b.WriteString("\n\n")
		for _, line := range differs {
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	return b.String(), nil
}

// difference is the list item naming how d's targets differ, or "" when
// they agree. Each cell that differs from the most common one is named;
// a transaction some targets open and others do not names the ones that do.
func (d docRow) difference() string {
	counts := map[string]int{}
	common := d.cells[0]
	for _, c := range d.cells {
		counts[c]++
		if counts[c] > counts[common] {
			common = c
		}
	}
	var parts []string
	var odd []string
	for i, c := range d.cells {
		if c != common {
			odd = append(odd, Targets[i].Column+" "+c)
		}
	}
	if len(odd) > 0 {
		parts = append(parts, strings.Join(odd, ", ")+"; "+common+" elsewhere")
	}
	if tx := txCell(d.tx); tx != "yes" && tx != "—" {
		parts = append(parts, "a transaction on "+tx+" only")
	}
	if len(parts) == 0 {
		return ""
	}
	return fmt.Sprintf("- %s: `%s` %s: %s.", d.group, d.op, d.shape, strings.Join(parts, "; "))
}

// suiteOrder puts the core suite first.
func suiteOrder(s string) int {
	if s == SuiteCore {
		return 0
	}
	return 1
}

func checkCells(cells ...string) error {
	for _, c := range cells {
		if strings.ContainsAny(c, "|`\n") || strings.TrimSpace(c) != c {
			return fmt.Errorf("cell %q holds a pipe, a backquote, a newline or surrounding space", c)
		}
	}
	return nil
}

func (d docRow) cellsOut() []string {
	out := []string{"`" + d.op + "`", d.shape}
	out = append(out, d.cells...)
	out = append(out, txCell(d.tx), dash(d.grows))
	return out
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func undash(s string) string {
	if s == "—" {
		return ""
	}
	return s
}

// txCell is "yes" when every target opens a transaction, "—" when none does,
// otherwise the columns of those that do.
func txCell(tx []bool) string {
	var cols []string
	for i, t := range tx {
		if t {
			cols = append(cols, Targets[i].Column)
		}
	}
	switch len(cols) {
	case 0:
		return "—"
	case len(Targets):
		return "yes"
	}
	return strings.Join(cols, ", ")
}

func parseTxCell(s string) ([]bool, error) {
	tx := make([]bool, len(Targets))
	switch s {
	case "—":
		return tx, nil
	case "yes":
		for i := range tx {
			tx[i] = true
		}
		return tx, nil
	}
	for col := range strings.SplitSeq(s, ", ") {
		i := slices.IndexFunc(Targets, func(t Target) bool { return t.Column == col })
		if i < 0 {
			return nil, fmt.Errorf("transaction cell %q names %q, which is not a column", s, col)
		}
		tx[i] = true
	}
	return tx, nil
}

func writeRow(b *strings.Builder, cells []string) {
	b.WriteString("| ")
	b.WriteString(strings.Join(cells, " | "))
	b.WriteString(" |\n")
}

// parse reads the report's tables back into rows.
func parse(doc string) ([]docRow, error) {
	want := strings.Join(header(), "|")
	var (
		rows  []docRow
		group string
		inTbl bool
	)
	sc := bufio.NewScanner(strings.NewReader(doc))
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if g, ok := strings.CutPrefix(line, "## "); ok {
			group, inTbl = g, false
			continue
		}
		if !strings.HasPrefix(line, "|") {
			inTbl = false
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		switch {
		case strings.Join(cells, "|") == want:
			inTbl = true
			continue
		case !inTbl:
			// A table that is not a report table, such as one in the intro.
			continue
		case strings.Trim(strings.Join(cells, ""), "-") == "":
			continue
		case len(cells) != len(header()):
			return nil, fmt.Errorf("line %d: %d cells, want %d", n, len(cells), len(header()))
		}
		tx, err := parseTxCell(cells[len(cells)-2])
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		rows = append(rows, docRow{
			group: group,
			op:    strings.Trim(cells[0], "`"),
			shape: cells[1],
			cells: cells[2 : 2+len(Targets)],
			tx:    tx,
			grows: undash(cells[len(cells)-1]),
			line:  n,
		})
	}
	return rows, sc.Err()
}

// compare lists how the report's column for result's target differs from
// result: a row whose cell, transaction or growth note differs, a measured
// row the report lacks, and a report row in one of result's groups that was
// not measured.
func compare(doc string, result Result) ([]string, error) {
	ti := targetIndex(result.Dialect, result.Driver)
	if ti < 0 {
		return nil, fmt.Errorf("%s/%s is not a report target", result.Dialect, result.Driver)
	}
	rows, err := parse(doc)
	if err != nil {
		return nil, err
	}
	byKey := map[string]docRow{}
	for _, d := range rows {
		if prev, dup := byKey[d.key()]; dup {
			return nil, fmt.Errorf("line %d repeats %s %s from line %d", d.line, d.op, d.shape, prev.line)
		}
		byKey[d.key()] = d
	}
	col := Targets[ti].Column
	var diffs []string
	measured := map[string]bool{}
	groups := map[string]bool{}
	for _, r := range result.Rows {
		measured[r.key()], groups[r.Group] = true, true
		d, ok := byKey[r.key()]
		if !ok {
			diffs = append(diffs, fmt.Sprintf("%s: %s %s measured %s, the report has no row", r.Group, r.Op, r.Shape, r.cell()))
			continue
		}
		if d.cells[ti] != r.cell() {
			diffs = append(diffs, fmt.Sprintf("%s: %s %s measured %s statements / rounds, the report says %s (line %d)", r.Group, r.Op, r.Shape, r.cell(), d.cells[ti], d.line))
		}
		if d.tx[ti] != r.Tx {
			diffs = append(diffs, fmt.Sprintf("%s: %s %s %s a transaction, the report's %s cell says otherwise (line %d)", r.Group, r.Op, r.Shape, opens(r.Tx), col, d.line))
		}
		if d.grows != r.Grows {
			diffs = append(diffs, fmt.Sprintf("%s: %s %s grows with %q, the report says %q (line %d)", r.Group, r.Op, r.Shape, r.Grows, d.grows, d.line))
		}
	}
	for _, d := range rows {
		if groups[d.group] && !measured[d.key()] {
			diffs = append(diffs, fmt.Sprintf("%s: %s %s is in the report (line %d) but no longer measured", d.group, d.op, d.shape, d.line))
		}
	}
	return diffs, nil
}

func opens(tx bool) string {
	if tx {
		return "opens"
	}
	return "does not open"
}
