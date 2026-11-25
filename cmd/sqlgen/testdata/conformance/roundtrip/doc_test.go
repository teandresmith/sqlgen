package roundtrip

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// results returns a core result for every target, each with the rows rows
// returns for it.
func results(rows func(Target) []Row) []Result {
	var out []Result
	for _, t := range Targets {
		out = append(out, Result{Suite: SuiteCore, Dialect: t.Dialect, Driver: t.Driver, Rows: rows(t)})
	}
	return out
}

func sampleRows(t Target) []Row {
	insert := Row{Group: "Writes", Op: "Create", Shape: "plain table", Statements: 1, Rounds: 1}
	if t.Dialect == "mysql" {
		insert.Statements, insert.Rounds = 2, 2
	}
	return []Row{
		{Group: "Reads", Op: "Get", Shape: "plain table", Statements: 1, Rounds: 1},
		{Group: "Reads", Op: "Get", Shape: "2 O2M", Grows: "one statement per relationship", Statements: 3, Rounds: 2},
		insert,
		{Group: "Writes", Op: "CreateWithRelated", Shape: "O2M create", Statements: 2, Rounds: 2, Tx: true},
	}
}

func TestRender(t *testing.T) {
	got, err := Render("# Title\n\nIntro.\n", results(sampleRows))
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	want := `# Title

Intro.

## Reads

| Operation | Shape | Postgres pgx | Postgres stdlib | MySQL | SQLite | Transaction | Grows with |
| --- | --- | --- | --- | --- | --- | --- | --- |
| ` + "`Get`" + ` | plain table | 1 / 1 | 1 / 1 | 1 / 1 | 1 / 1 | — | — |
| ` + "`Get`" + ` | 2 O2M | 3 / 2 | 3 / 2 | 3 / 2 | 3 / 2 | — | one statement per relationship |

## Writes

| Operation | Shape | Postgres pgx | Postgres stdlib | MySQL | SQLite | Transaction | Grows with |
| --- | --- | --- | --- | --- | --- | --- | --- |
| ` + "`Create`" + ` | plain table | 1 / 1 | 1 / 1 | 2 / 2 | 1 / 1 | — | — |
| ` + "`CreateWithRelated`" + ` | O2M create | 2 / 2 | 2 / 2 | 2 / 2 | 2 / 2 | yes | — |

` + DiffersIntro + `
- Writes: ` + "`Create`" + ` plain table: MySQL 2 / 2; 1 / 1 elsewhere.
`
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Render() (-want +got):\n%s", diff)
	}
}

func TestRenderErrors(t *testing.T) {
	tests := []struct {
		name    string
		results []Result
		wantErr string
	}{
		{
			name:    "a target is missing",
			results: results(sampleRows)[:3],
			wantErr: "suite core: no result, or more than one, for sqlite/stdlib",
		},
		{
			name: "targets disagree on a growth note",
			results: results(func(t Target) []Row {
				rows := sampleRows(t)
				if t.Dialect == "sqlite" {
					rows[1].Grows = "something else"
				}
				return rows
			}),
			wantErr: `says it grows with "something else"`,
		},
		{
			name: "a cell holds a pipe",
			results: results(func(t Target) []Row {
				rows := sampleRows(t)
				rows[0].Shape = "a | b"
				return rows
			}),
			wantErr: "holds a pipe",
		},
		{
			name:    "a result for a target the report has no column for",
			results: append(results(sampleRows), Result{Suite: SuiteCore, Dialect: "oracle", Driver: "stdlib"}),
			wantErr: "oracle/stdlib: not a report target",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Render("# Title\n", tt.results)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Render() error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestRenderTransactionCell(t *testing.T) {
	doc, err := Render("# Title\n", results(func(t Target) []Row {
		return []Row{{Group: "Writes", Op: "Create", Shape: "plain table", Statements: 2, Rounds: 2, Tx: t.Dialect == "mysql" || t.Dialect == "sqlite"}}
	}))
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if !strings.Contains(doc, "| 2 / 2 | 2 / 2 | 2 / 2 | 2 / 2 | MySQL, SQLite | — |") {
		t.Errorf("Render() = %s, want the transaction cell to name MySQL and SQLite", doc)
	}
	if want := "- Writes: `Create` plain table: a transaction on MySQL, SQLite only.\n"; !strings.HasSuffix(doc, want) {
		t.Errorf("Render() = %s, want it to end with %q", doc, want)
	}
	for _, target := range Targets {
		measured := Result{Suite: SuiteCore, Dialect: target.Dialect, Driver: target.Driver, Rows: []Row{
			{Group: "Writes", Op: "Create", Shape: "plain table", Statements: 2, Rounds: 2, Tx: target.Dialect == "mysql" || target.Dialect == "sqlite"},
		}}
		if diffs, err := compare(doc, measured); err != nil || len(diffs) > 0 {
			t.Errorf("compare(%s/%s) = %q, %v, want no difference", target.Dialect, target.Driver, diffs, err)
		}
	}
}

func TestCompare(t *testing.T) {
	doc, err := Render("# Title\n\n| Legend | Meaning |\n|---|---|\n| 1 / 1 | one |\n", results(sampleRows))
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	mysql := func(edit func([]Row) []Row) Result {
		return Result{Suite: SuiteCore, Dialect: "mysql", Driver: "stdlib", Rows: edit(sampleRows(Targets[2]))}
	}
	tests := []struct {
		name   string
		result Result
		want   []string
	}{
		{name: "measured as committed", result: mysql(func(r []Row) []Row { return r })},
		{
			name:   "a count changed",
			result: mysql(func(r []Row) []Row { r[1].Statements = 4; return r }),
			want:   []string{"Reads: Get 2 O2M measured 4 / 2 statements / rounds, the report says 3 / 2 (line 12)"},
		},
		{
			name:   "a transaction appeared",
			result: mysql(func(r []Row) []Row { r[2].Tx = true; return r }),
			want:   []string{"Writes: Create plain table opens a transaction, the report's MySQL cell says otherwise (line 18)"},
		},
		{
			name:   "a growth note changed",
			result: mysql(func(r []Row) []Row { r[1].Grows = "per parent"; return r }),
			want:   []string{`Reads: Get 2 O2M grows with "per parent", the report says "one statement per relationship" (line 12)`},
		},
		{
			name: "a new case",
			result: mysql(func(r []Row) []Row {
				return append(r, Row{Group: "Reads", Op: "Count", Shape: "plain table", Statements: 1, Rounds: 1})
			}),
			want: []string{"Reads: Count plain table measured 1 / 1, the report has no row"},
		},
		{
			name:   "a case removed",
			result: mysql(func(r []Row) []Row { return r[1:] }),
			want:   []string{"Reads: Get plain table is in the report (line 11) but no longer measured"},
		},
		{
			name: "rows of another suite's groups are not this suite's",
			result: Result{Suite: SuiteFeatures, Dialect: "mysql", Driver: "stdlib", Rows: []Row{
				{Group: "Cache", Op: "Get", Shape: "hit", Statements: 0, Rounds: 0},
			}},
			want: []string{"Cache: Get hit measured 0 / 0, the report has no row"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := compare(doc, tt.result)
			if err != nil {
				t.Fatalf("compare() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("compare() (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCompareMalformed(t *testing.T) {
	head := "## Reads\n\n| Operation | Shape | Postgres pgx | Postgres stdlib | MySQL | SQLite | Transaction | Grows with |\n|---|---|---|---|---|---|---|---|\n"
	tests := []struct {
		name    string
		doc     string
		wantErr string
	}{
		{name: "a row short of cells", doc: head + "| `Get` | plain | 1 / 1 |\n", wantErr: "line 5: 3 cells, want 8"},
		{name: "a transaction cell naming no column", doc: head + "| `Get` | plain | 1 / 1 | 1 / 1 | 1 / 1 | 1 / 1 | Oracle | — |\n", wantErr: `names "Oracle"`},
		{
			name:    "a repeated row",
			doc:     head + "| `Get` | plain | 1 / 1 | 1 / 1 | 1 / 1 | 1 / 1 | — | — |\n| `Get` | plain | 1 / 1 | 1 / 1 | 1 / 1 | 1 / 1 | — | — |\n",
			wantErr: "line 6 repeats Get plain from line 5",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := compare(tt.doc, Result{Suite: SuiteCore, Dialect: "sqlite", Driver: "stdlib"})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("compare() error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestSettled(t *testing.T) {
	row := func(stmts, rounds int) Row { return Row{Statements: stmts, Rounds: rounds} }
	tests := []struct {
		name    string
		writing bool
		attempt int
		known   bool
		want    string
		got     Row
		settled bool
	}{
		{name: "writing, first attempt", writing: true, attempt: 0, got: row(3, 2), settled: false},
		{name: "writing, second attempt", writing: true, attempt: 1, got: row(3, 2), settled: true},
		{name: "checking a case the report lacks", got: row(3, 2), settled: true},
		{name: "checking, as committed", known: true, want: "3 / 2", got: row(3, 2), settled: true},
		{name: "checking, a wave split", known: true, want: "3 / 2", got: row(3, 3), settled: false},
		{name: "checking, fewer rounds is a real change", known: true, want: "3 / 2", got: row(3, 1), settled: true},
		{name: "checking, other statements are a real change", known: true, want: "3 / 2", got: row(4, 3), settled: true},
		{name: "checking, the target has no cell", known: true, want: "n/a", got: row(1, 1), settled: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := settled(tt.writing, tt.attempt, tt.known, tt.want, tt.got); got != tt.settled {
				t.Errorf("settled(%v, %d, %v, %q, %+v) = %v, want %v", tt.writing, tt.attempt, tt.known, tt.want, tt.got, got, tt.settled)
			}
		})
	}
}
