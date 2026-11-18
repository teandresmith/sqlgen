package reqcheck

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Locations the report joins against, relative to the repository root.
const (
	// ConformanceDir holds the suite module and one module per dialect.
	ConformanceDir = "cmd/sqlgen/testdata/conformance"
	// goldenDir holds goldenTest, which runs one subtest per example.
	goldenDir = "cmd/sqlgen"
)

const (
	// conformanceTest is the test each dialect module runs the suite from.
	conformanceTest = "TestConformance"
	goldenTest      = "TestE2EGoldenFiles"
)

// Status is the derived status of a test, a requirement, or a requirement on one target.
type Status int

// Statuses, least severe first: a requirement's status is the most severe of its targets'.
const (
	StatusNA Status = iota // the requirement does not apply to the target
	StatusPass
	StatusSkip
	StatusUntested
	StatusNotRun
	StatusFail
)

func (s Status) String() string {
	switch s {
	case StatusPass:
		return "pass"
	case StatusSkip:
		return "skip"
	case StatusUntested:
		return "untested"
	case StatusNotRun:
		return "not run"
	case StatusFail:
		return "fail"
	case StatusNA:
	}
	return "—"
}

// Outcome is a status and, for a skip or a test that did not run, why.
type Outcome struct {
	Status Status
	Msg    string
}

// Target is one conformance dialect module: one run of a suite.
type Target struct {
	Dir string // relative to the repository root
	// Suite is the root segment of the suite the module runs, empty for the core suite.
	Suite    string
	Dialect  string
	Driver   string
	Database string // what the harness logged; empty when the suite did not run
}

// Label names the target in the report; a variant suite's target is prefixed with the suite.
func (t Target) Label() string {
	if t.Suite != "" {
		return t.Suite + " " + t.Dialect + "/" + t.Driver
	}
	return t.Dialect + "/" + t.Driver
}

// Report is the registry joined with test results (CONFORMANCE.md §5.5).
type Report struct {
	Targets     []Target
	Files       []FileReport
	Runs        []Run
	Informative int
}

// FileReport is one registry file's part of the report.
type FileReport struct {
	Reg  *Registry
	Rows []RowReport
	Gaps []RowReport
	// Uncovered lists the owned PRD sections with no requirement rows.
	Uncovered []string
	// Conformance and Tests say which target columns the file's rows need.
	Conformance bool
	Tests       bool
}

// RowReport is a requirement, or a PRD gap's pinning tests, joined with results.
type RowReport struct {
	ID     string
	Anchor string
	Cell   string
	// Targets has one outcome per Report.Targets, for the row's conformance tests.
	Targets []Outcome
	// Tests is the outcome of the row's other tests (unit, integration, example, golden).
	Tests Outcome
	// Status is the most severe of the outcomes above that apply.
	Status Status
	// NoTarget is set when the row names conformance tests but no dialect module it applies to
	// exists, so they ran nowhere.
	NoTarget bool
}

// BuildReport checks the registry at root, then joins it with results. A registry that fails
// the static check is an error: its rows would not resolve.
func BuildReport(root string, results *Results) (*Report, error) {
	res, err := Check(root)
	if err != nil {
		return nil, err
	}
	if errs := res.Errors(); len(errs) > 0 {
		return nil, fmt.Errorf("the registry fails make req-check (%d error(s), first %s); fix it before reporting", len(errs), errs[0])
	}
	targets, err := loadTargets(root, results)
	if err != nil {
		return nil, err
	}
	rep := &Report{Targets: targets, Runs: results.Runs}
	for _, e := range res.Ownership.Entries {
		if e.Owner == Informative {
			rep.Informative++
		}
	}
	for _, reg := range res.Registries {
		fr := FileReport{Reg: reg, Uncovered: uncoveredSections(res.Ownership, reg)}
		for _, row := range reg.Rows {
			rr := joinRow(row.ID, row.Tests, row.AppliesTo, targets, results)
			rr.Anchor, rr.Cell = row.Anchor, row.Cell
			fr.Rows = append(fr.Rows, rr)
		}
		for _, g := range reg.Gaps {
			fr.Gaps = append(fr.Gaps, joinRow(g.ID, g.Tests, []string{"all"}, targets, results))
		}
		// An untested row names no test, so it asks for no column.
		for _, rr := range fr.Rows {
			if rr.Status == StatusUntested {
				continue
			}
			fr.Conformance = fr.Conformance || slices.ContainsFunc(rr.Targets, func(o Outcome) bool { return o.Status != StatusNA })
			fr.Tests = fr.Tests || rr.Tests.Status != StatusNA
		}
		rep.Files = append(rep.Files, fr)
	}
	return rep, nil
}

// loadTargets lists the conformance dialect modules: each directory under ConformanceDir with a
// go.mod, except the suites, the core suite's targets first. The dialect and driver are the
// string literals its test passes to the harness, so a target is known even when its suite did
// not run.
func loadTargets(root string, results *Results) ([]Target, error) {
	mods, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(ConformanceDir), "*", "go.mod"))
	if err != nil {
		return nil, fmt.Errorf("listing conformance modules: %w", err)
	}
	slices.Sort(mods)
	var targets []Target
	for _, mod := range mods {
		dir := ConformanceDir + "/" + filepath.Base(filepath.Dir(mod))
		if slices.ContainsFunc(Suites, func(s Suite) bool { return s.Dir == dir }) {
			continue
		}
		t, err := readTarget(root, dir)
		if err != nil {
			return nil, err
		}
		t.Database = results.database(dir)
		targets = append(targets, t)
	}
	slices.SortStableFunc(targets, func(a, b Target) int {
		return slices.IndexFunc(Suites, func(s Suite) bool { return s.Name == a.Suite }) -
			slices.IndexFunc(Suites, func(s Suite) bool { return s.Name == b.Suite })
	})
	return targets, nil
}

// readTarget reads the suite a dialect module's tests import and the Dialect and Driver fields
// they set on the harness.
func readTarget(root, dir string) (Target, error) {
	t := Target{Dir: dir}
	imported := false
	files, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(dir), "*_test.go"))
	if err != nil {
		return t, fmt.Errorf("listing %s tests: %w", dir, err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		f, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			return t, fmt.Errorf("parsing %s: %w", name, err)
		}
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			for _, s := range Suites {
				if p == RepoModule+"/"+s.Dir {
					t.Suite, imported = s.Name, true
				}
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			kv, ok := n.(*ast.KeyValueExpr)
			if !ok {
				return true
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok {
				return true
			}
			if v, ok := stringLit(kv.Value); ok {
				switch key.Name {
				case "Dialect":
					t.Dialect = v
				case "Driver":
					t.Driver = v
				}
			}
			return true
		})
	}
	if !imported {
		return t, fmt.Errorf("conformance module %s: its tests import no conformance suite (%s)", dir, suiteDirs())
	}
	if t.Dialect == "" || t.Driver == "" {
		return t, fmt.Errorf("conformance module %s: its tests set no string-literal Dialect and Driver on the harness", dir)
	}
	return t, nil
}

func suiteDirs() string {
	dirs := make([]string, len(Suites))
	for i, s := range Suites {
		dirs[i] = s.Dir
	}
	return strings.Join(dirs, ", ")
}

// joinRow joins a row's tests with the results. Conformance tests run once per target the row
// applies to; every other test runs once, in its own package.
func joinRow(id string, tests, appliesTo []string, targets []Target, results *Results) RowReport {
	rr := RowReport{ID: id, Targets: make([]Outcome, len(targets))}
	if len(tests) == 0 {
		for i := range rr.Targets {
			rr.Targets[i] = Outcome{Status: StatusUntested}
		}
		rr.Tests, rr.Status = Outcome{Status: StatusUntested}, StatusUntested
		return rr
	}
	perTarget := make([][]Outcome, len(targets))
	var others []Outcome
	conformance := false
	for _, raw := range tests {
		ref, err := ParseTestRef(raw)
		if err != nil { // Check has already rejected it
			continue
		}
		if ref.Home != HomeConformance {
			dir, name := refTest(ref)
			others = append(others, results.outcome(dir, name))
			continue
		}
		conformance = true
		suite := suiteFor(ref.Path).Name
		for i, t := range targets {
			if t.Suite == suite && applies(appliesTo, t) {
				perTarget[i] = append(perTarget[i], results.outcome(t.Dir, conformanceTest+"/"+goTestPath(ref.Path)))
			}
		}
	}
	rr.Tests = combine(others)
	targetApplies := false
	for i := range targets {
		rr.Targets[i] = combine(perTarget[i])
		if rr.Targets[i].Status != StatusNA {
			targetApplies = true
		}
	}
	for _, o := range append([]Outcome{rr.Tests}, rr.Targets...) {
		rr.Status = max(rr.Status, o.Status)
	}
	if conformance && !targetApplies {
		// Whatever its other tests did, the conformance tests ran nowhere.
		rr.NoTarget = true
		rr.Status = max(rr.Status, StatusNotRun)
	}
	return rr
}

// refTest returns the package directory and `go test` name of a non-conformance test reference.
func refTest(ref TestRef) (dir, name string) {
	if ref.Home == HomeGolden {
		return goldenDir, goldenTest + "/" + goTestName(path.Base(ref.Dir))
	}
	return ref.Dir, goTestPath(ref.Path)
}

func goTestPath(segs []string) string {
	return goTestName(strings.Join(segs, "/"))
}

// applies reports whether a row's Applies-to admits a target: `all`, or each dimension it
// names (dialects, drivers) includes the target's.
func applies(appliesTo []string, t Target) bool {
	if slices.Contains(appliesTo, "all") {
		return true
	}
	var dialects, drivers []string
	for _, a := range appliesTo {
		if a == "pgx" || a == "stdlib" {
			drivers = append(drivers, a)
		} else {
			dialects = append(dialects, a)
		}
	}
	return (len(dialects) == 0 || slices.Contains(dialects, t.Dialect)) &&
		(len(drivers) == 0 || slices.Contains(drivers, t.Driver))
}

// combine folds the outcomes of several tests on one target. A failure or a test that did not
// run decides it; otherwise one pass is enough, since a sibling may skip for a capability another
// test covers. All skips make a skip, with each distinct message.
func combine(outs []Outcome) Outcome {
	if len(outs) == 0 {
		return Outcome{Status: StatusNA}
	}
	for _, s := range []Status{StatusFail, StatusNotRun, StatusPass} {
		if i := slices.IndexFunc(outs, func(o Outcome) bool { return o.Status == s }); i >= 0 {
			return outs[i]
		}
	}
	var msgs []string
	for _, o := range outs {
		if o.Msg != "" && !slices.Contains(msgs, o.Msg) {
			msgs = append(msgs, o.Msg)
		}
	}
	return Outcome{Status: StatusSkip, Msg: strings.Join(msgs, "; ")}
}

// Header is what the report records about the run, the only part that changes between runs
// of the same tree and results.
type Header struct {
	Commit string
	Date   string
}

// Markdown renders the report.
func (rep *Report) Markdown(h Header) string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }

	w("# Conformance Report\n\n")
	w("<!-- Generated by `make conformance` (docs/design/CONFORMANCE.md §5.5). Do not edit. -->\n\n")
	w("Commit `%s`, run %s. The registry in this directory joined with the `go test -json` results of every registered test home.\n\n", h.Commit, h.Date)
	if len(rep.Targets) > 0 {
		w("| Conformance target | Module | Database |\n|---|---|---|\n")
		for _, t := range rep.Targets {
			w("| %s | `%s` | %s |\n", t.Label(), t.Dir, cmp.Or(t.Database, "not run"))
		}
		w("\n")
	}
	w("Statuses: `pass`; `fail`; `skip`, with the test's skip message; `not run`, the test has no result " +
		"(its package did not build or run, or a parent test skipped or failed); `untested`, the row names " +
		"no test; `—`, the row does not apply. A row's status is its most severe target. On one target, a " +
		"failure or a test that did not run decides; otherwise one pass is enough.\n\n")

	rep.writeModules(w)
	rep.writeAttention(w)
	rep.writeRuns(w)
	rep.writeRequirements(w)
	rep.writeMatrices(w)
	rep.writeCoverage(w)
	rep.writeGaps(w)
	return b.String()
}

type writer func(format string, args ...any)

func (rep *Report) writeModules(w writer) {
	w("## Modules\n\n| File | Seeding | Rows | Pass | Fail | Skip | Not run | Untested |\n|---|---|---|---|---|---|---|---|\n")
	for _, f := range rep.Files {
		n := map[Status]int{}
		for _, r := range f.Rows {
			n[r.Status]++
		}
		w("| %s | %s | %d | %d | %d | %d | %d | %d |\n", f.Reg.Name, f.Reg.Seeding, len(f.Rows),
			n[StatusPass], n[StatusFail], n[StatusSkip], n[StatusNotRun], n[StatusUntested])
	}
	w("\n")
}

// attention is one line of the failing and not-run list.
type attention struct{ file, id, where, status string }

// writeAttention lists the requirements and PRD-gap pinning tests that failed or did not run,
// with where and why.
func (rep *Report) writeAttention(w writer) {
	var lines []attention
	for _, f := range rep.Files {
		for _, r := range f.Rows {
			lines = rep.attend(lines, f.Reg.Name, r)
			if r.NoTarget {
				lines = append(lines, attention{f.Reg.Name, r.ID, "—", "not run: no conformance module it applies to"})
			}
		}
		for _, g := range f.Gaps {
			lines = rep.attend(lines, f.Reg.Name, g)
		}
	}
	w("## Failing and not-run rows\n\nRequirements and PRD-gap pinning tests.\n\n")
	if len(lines) == 0 {
		w("None.\n\n")
		return
	}
	w("| File | ID | Where | Status |\n|---|---|---|---|\n")
	for _, l := range lines {
		w("| %s | %s | %s | %s |\n", l.file, l.id, l.where, l.status)
	}
	w("\n")
}

// attend appends a line for each target, and the other tests, on which r failed or did not run.
func (rep *Report) attend(lines []attention, file string, r RowReport) []attention {
	add := func(where string, o Outcome) {
		if o.Status == StatusFail || o.Status == StatusNotRun {
			lines = append(lines, attention{file, r.ID, where, cellText(o, true)})
		}
	}
	for i, t := range rep.Targets {
		add(t.Label(), r.Targets[i])
	}
	add("tests", r.Tests)
	return lines
}

func (rep *Report) writeRuns(w writer) {
	w("## Test runs\n\nOne `go test -json` run per module, example and conformance module. Failed counts packages with a failing test, a failed build, or a test binary that stopped early.\n\n")
	w("| Run | Packages | Passed | Failed | No tests |\n|---|---|---|---|---|\n")
	for _, r := range rep.Runs {
		w("| %s | %d | %d | %d | %d |\n", r.Name, r.Packages, r.Passed, r.Failed, r.NoTests)
	}
	w("\n")
}

func (rep *Report) writeRequirements(w writer) {
	w("## Requirements\n\n")
	for _, f := range rep.Files {
		w("### %s\n\n", f.Reg.Name)
		if len(f.Rows) == 0 {
			w("No rows (seeding: %s).\n\n", f.Reg.Seeding)
			continue
		}
		cols := []string{"Section", "ID"}
		if f.Conformance {
			for _, t := range rep.Targets {
				cols = append(cols, t.Label())
			}
		}
		if f.Tests || !f.Conformance {
			cols = append(cols, "tests")
		}
		w("| %s |\n|%s\n", strings.Join(cols, " | "), strings.Repeat("---|", len(cols)))
		for _, r := range f.Rows {
			cells := []string{"§" + r.Anchor, r.ID}
			if f.Conformance {
				for _, o := range r.Targets {
					cells = append(cells, cellText(o, true))
				}
			}
			if f.Tests || !f.Conformance {
				cells = append(cells, cellText(r.Tests, true))
			}
			w("| %s |\n", strings.Join(cells, " | "))
		}
		w("\n")
	}
}

func (rep *Report) writeMatrices(w writer) {
	w("## Matrices\n\nEach cell shows the status of the row that claims it, `n/a` when declared not applicable, or `empty`.\n\n")
	for _, f := range rep.Files {
		decls := make([]Matrix, 0, len(f.Reg.Matrices))
		for _, m := range f.Reg.Matrices {
			decls = append(decls, m)
		}
		slices.SortFunc(decls, func(a, b Matrix) int { return cmp.Compare(a.Line, b.Line) })
		for _, m := range decls {
			w("### %s: %s\n\n", f.Reg.Name, m.Name)
			rows, cells := f.matrixCells(m.Name)
			if m.Rows != nil {
				rows = m.Rows
			}
			if len(rows) == 0 {
				w("No rows yet.\n\n")
				continue
			}
			w("| %s | %s |\n|---|%s\n", m.RowUnit, strings.Join(m.Cols, " | "), strings.Repeat("---|", len(m.Cols)))
			for _, row := range rows {
				out := []string{row}
				for _, col := range m.Cols {
					out = append(out, cmp.Or(cells[row+"\x00"+col], "empty"))
				}
				w("| %s |\n", strings.Join(out, " | "))
			}
			w("\n")
		}
	}
}

// matrixCells returns a matrix's rows in the order the file first names them, and each filled
// cell's text: the most severe status of the rows claiming it, or n/a.
func (f FileReport) matrixCells(matrix string) ([]string, map[string]string) {
	type claim struct {
		line   int
		cell   string
		status Status
		na     bool
	}
	claims := make([]claim, 0, len(f.Rows)+len(f.Reg.NA))
	for i, r := range f.Rows {
		claims = append(claims, claim{line: f.Reg.Rows[i].Line, cell: r.Cell, status: r.Status})
	}
	for _, n := range f.Reg.NA {
		claims = append(claims, claim{line: n.Line, cell: n.Cell, na: true})
	}
	slices.SortFunc(claims, func(a, b claim) int { return cmp.Compare(a.line, b.line) })
	var rows []string
	worst := map[string]Status{}
	text := map[string]string{}
	for _, c := range claims {
		m, row, col, ok := parseCell(c.cell)
		if !ok || m != matrix {
			continue
		}
		if !slices.Contains(rows, row) {
			rows = append(rows, row)
		}
		key := row + "\x00" + col
		if c.na {
			text[key] = "n/a"
			continue
		}
		if s, seen := worst[key]; !seen || c.status > s {
			worst[key] = c.status
			text[key] = cellText(Outcome{Status: c.status}, false)
		}
	}
	return rows, text
}

func (rep *Report) writeCoverage(w writer) {
	w("## PRD coverage\n\nPRD sections marked `informative`: %d. Owned sections with no requirement rows:\n\n", rep.Informative)
	w("| File | Sections | Owned sections with no rows |\n|---|---|---|\n")
	for _, f := range rep.Files {
		list := "—"
		if len(f.Uncovered) > 0 {
			list = "§" + strings.Join(f.Uncovered, ", §")
		}
		w("| %s | %d | %s |\n", f.Reg.Name, len(f.Uncovered), list)
	}
	w("\n")
}

func (rep *Report) writeGaps(w writer) {
	open, ruled := 0, 0
	for _, f := range rep.Files {
		for _, g := range f.Reg.Gaps {
			if g.Ruling == "" {
				open++
			} else {
				ruled++
			}
		}
	}
	w("## PRD gaps\n\n%d open, %d ruled and waiting to be applied.\n\n", open, ruled)
	if open+ruled == 0 {
		return
	}
	w("| File | Gap | Section | Ruling | Pinning tests |\n|---|---|---|---|---|\n")
	for _, f := range rep.Files {
		for i, g := range f.Reg.Gaps {
			w("| %s | %s | %s | %s | %s |\n", f.Reg.Name, g.ID, g.Section, cmp.Or(g.Ruling, "open"), cellText(Outcome{Status: f.Gaps[i].Status}, false))
		}
	}
	w("\n")
}

// cellText renders an outcome for a table cell; detail adds a skip's or not-run's reason.
func cellText(o Outcome, detail bool) string {
	s := o.Status.String()
	if o.Status == StatusFail {
		s = "**fail**"
	}
	if detail && o.Msg != "" && (o.Status == StatusSkip || o.Status == StatusNotRun) {
		s += ": " + escapeCell(o.Msg)
	}
	return s
}

func escapeCell(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "|", `\|`), "\n", " ")
}

// uncoveredSections returns the sections a registry file owns that have no requirement rows: an
// owned section with no owned descendant needs a row anchored at or below it.
func uncoveredSections(own *Ownership, r *Registry) []string {
	var owned []string
	for _, e := range own.Entries {
		if e.Owner == r.Name {
			owned = append(owned, e.Key)
		}
	}
	var out []string
	for _, key := range owned {
		if slices.ContainsFunc(owned, func(k string) bool { return strings.HasPrefix(k, key+".") }) {
			continue
		}
		covered := slices.ContainsFunc(r.Rows, func(row Row) bool {
			return row.Anchor == key || strings.HasPrefix(row.Anchor, key+".")
		})
		if !covered {
			out = append(out, key)
		}
	}
	return out
}

// WriteReport writes the rendered report to docs/conformance/REPORT.md under root.
func WriteReport(root string, rep *Report, h Header) error {
	p := filepath.Join(root, filepath.FromSlash(RegistryDir), ReportFile)
	if err := os.WriteFile(p, []byte(rep.Markdown(h)), 0o644); err != nil { //nolint:gosec // a committed docs file, world-readable like its siblings
		return fmt.Errorf("writing %s/%s: %w", RegistryDir, ReportFile, err)
	}
	return nil
}
