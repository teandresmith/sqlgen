package roundtrip

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Suites a Result belongs to.
const (
	SuiteCore     = "core"
	SuiteFeatures = "features"
)

// OutEnv names the environment variable `make round-trips` sets to a
// directory. With it set, Run writes its Result there as JSON instead of
// comparing it with the committed report.
const OutEnv = "SQLGEN_ROUND_TRIPS_OUT"

// Case is one operation on one shape of data.
type Case struct {
	// Group is the report section the row sits in. Each suite's groups are its
	// own: Run treats every report row in them as one of its rows.
	Group string
	// Op is the client method, Shape what the call reads or writes.
	Op, Shape string
	// Grows says what the counts scale with, when they do.
	Grows string
	// Setup seeds what the operation needs, with nothing recorded, and returns
	// the operation. A failed setup calls t.Fatal.
	Setup func(t *testing.T) func(ctx context.Context) error
}

// Config is the wiring Run measures through.
type Config struct {
	Suite   string
	Dialect string
	Driver  string
	// Recorder is the recorder the client's driver reports to.
	Recorder *Recorder
	// Reset empties every table the cases touch.
	Reset func(t *testing.T)
}

// attempts bounds how often a case is measured. The delay keeps a wave
// together, but a goroutine the scheduler starts late can still split one,
// which only ever adds a round: the fewest rounds seen is the measurement.
// Statement counts must agree across attempts.
const attempts = 3

// Run measures every case and checks the committed report, or with OutEnv
// set writes the measurements out. The report is checked once every case has
// run, so one run lists every row that changed.
func Run(t *testing.T, cfg Config, cases []Case) {
	t.Helper()
	if targetIndex(cfg.Dialect, cfg.Driver) < 0 {
		t.Fatalf("roundtrip: %s/%s is not a report target", cfg.Dialect, cfg.Driver)
	}
	seen := map[string]bool{}
	for _, c := range cases {
		row := Row{Group: c.Group, Op: c.Op, Shape: c.Shape}
		if seen[row.key()] {
			t.Fatalf("roundtrip: two cases are %s %s in %s", c.Op, c.Shape, c.Group)
		}
		seen[row.key()] = true
	}

	out := os.Getenv(OutEnv)
	var expect map[string]docRow
	doc, docErr := os.ReadFile(DocPath)
	if out == "" && docErr == nil {
		if rows, err := parse(string(doc)); err == nil {
			expect = map[string]docRow{}
			for _, d := range rows {
				expect[d.key()] = d
			}
		}
	}
	ti := targetIndex(cfg.Dialect, cfg.Driver)

	result := Result{Suite: cfg.Suite, Dialect: cfg.Dialect, Driver: cfg.Driver}
	for _, c := range cases {
		t.Run(c.Op+" "+c.Shape, func(t *testing.T) {
			row := Row{Group: c.Group, Op: c.Op, Shape: c.Shape, Grows: c.Grows}
			var wantCell string
			want, known := expect[row.key()]
			if known {
				wantCell = want.cells[ti]
			}
			for i := range attempts {
				m := measure(t, cfg, c)
				if i > 0 && m.Statements != row.Statements {
					t.Fatalf("%s %s sent %d statements, then %d: the count must not vary\nfirst: %q\nnow:   %q", c.Op, c.Shape, row.Statements, m.Statements, row.SQL, m.SQL)
				}
				if i == 0 || m.Rounds < row.Rounds {
					row.Statements, row.Rounds, row.Tx, row.SQL = m.Statements, m.Rounds, m.Tx, m.SQL
				}
				if settled(out != "", i, known, wantCell, row) {
					break
				}
			}
			t.Logf("%s statements / rounds, tx %v: %q", row.cell(), row.Tx, row.SQL)
			result.Rows = append(result.Rows, row)
		})
	}
	if t.Failed() {
		return
	}

	if out != "" {
		writeResult(t, out, result)
		return
	}
	if docErr != nil {
		t.Fatalf("read the round-trip report: %v\nrun `make round-trips` (requires Docker) to generate docs/round-trips.md", docErr)
	}
	diffs, err := compare(string(doc), result)
	if err != nil {
		t.Fatalf("read the round-trip report: %v\nrun `make round-trips` (requires Docker) to regenerate docs/round-trips.md", err)
	}
	if len(diffs) > 0 {
		t.Errorf("docs/round-trips.md is out of date for %s/%s (%s suite):\n  %s\nrun `make round-trips` (requires Docker) to regenerate it, and commit the result",
			cfg.Dialect, cfg.Driver, cfg.Suite, strings.Join(diffs, "\n  "))
	}
}

// settled reports whether attempt i of a case is its last. Writing the
// report, a case is measured twice, so a wave split once is not committed.
// Checking it, a case is measured again only while it sends the statements
// the report states in more rounds than it states: a split wave only ever
// adds a round, so fewer rounds or other statements are a real change.
func settled(writing bool, i int, known bool, want string, got Row) bool {
	if writing {
		return i == 1
	}
	if !known || want == got.cell() {
		return true
	}
	var stmts, rounds int
	if _, err := fmt.Sscanf(want, "%d / %d", &stmts, &rounds); err != nil {
		return true
	}
	return stmts != got.Statements || got.Rounds < rounds
}

// measure runs one attempt of c: reset, seed, then the operation with the
// recorder on.
func measure(t *testing.T, cfg Config, c Case) Measurement {
	t.Helper()
	cfg.Reset(t)
	op := c.Setup(t)
	ctx := context.Background()
	cfg.Recorder.Start()
	err := op(ctx)
	m := cfg.Recorder.Stop()
	if err != nil {
		t.Fatalf("%s %s: %v", c.Op, c.Shape, err)
	}
	return m
}

func writeResult(t *testing.T, dir string, r Result) {
	t.Helper()
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ResultFile(r.Suite, r.Dialect, r.Driver))
	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s", path)
}

// ResultFile names the file a Result is written to.
func ResultFile(suite, dialect, driver string) string {
	return suite + "-" + dialect + "-" + driver + ".json"
}

// ReadResults reads every Result in dir, as `make round-trips` left them.
func ReadResults(dir string) ([]Result, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var results []Result
	var errs []error
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		var r Result
		if err := json.Unmarshal(b, &r); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", e.Name(), err))
			continue
		}
		if e.Name() != ResultFile(r.Suite, r.Dialect, r.Driver) {
			errs = append(errs, fmt.Errorf("%s holds the %s result for %s/%s", e.Name(), r.Suite, r.Dialect, r.Driver))
			continue
		}
		results = append(results, r)
	}
	slices.SortFunc(results, func(a, b Result) int { return strings.Compare(a.Suite+a.Dialect+a.Driver, b.Suite+b.Dialect+b.Driver) })
	return results, errors.Join(errs...)
}
