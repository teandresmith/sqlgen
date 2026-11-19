package reqcheck_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/internal/reqcheck"
)

const reportPRD = `# PRD

## 4. Configuration System

Config.

### 4.13 Config Validation Rules

pgx with MySQL is a validation error.

## 9. Generated Client Operations

### 9.3 Delete Operations

HardDeleteMany is a batch hard delete.

## Appendix C: Project Linting Configuration

Linters.
`

const reportMap = `# Registry

| PRD section | Owner |
|---|---|
| §4. Configuration System | cli.md |
| §4.13 Config Validation Rules | cli.md |
| §9. Generated Client Operations | client.md |
| §9.3 Delete Operations | client.md |
| §Appendix C: Project Linting Configuration | informative |
`

const reportTests = `package config

import "testing"

func TestLiteral(t *testing.T) {}

func TestTable(t *testing.T) {
	tests := []struct{ name string }{{name: "keyed case"}, {name: "through ./"}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {})
	}
}
`

const reportSuite = `package suite

import "testing"

func Run(t *testing.T) {
	t.Run("HardDeleteMany", func(t *testing.T) { hardDeleteMany(t) })
}

func hardDeleteMany(t *testing.T) {
	t.Run("empty key list deletes nothing", func(t *testing.T) {})
	t.Run("missing keys are not an error", func(t *testing.T) {})
}
`

// dialectTest is a conformance dialect module's test: the report reads only the suite it
// imports and the harness's Dialect and Driver literals.
func dialectTest(dialect, driver string) string {
	return `package ` + dialect + `

import (
	"testing"

	"` + reqcheck.RepoModule + "/" + reqcheck.ConformanceSuiteDir + `"
)

func TestConformance(t *testing.T) {
	suite.Run(t, &suite.Harness{Dialect: "` + dialect + `", Driver: "` + driver + `"})
}
`
}

// Package directories the fixture's results name.
const (
	pgDir     = "cmd/sqlgen/testdata/conformance/postgres"
	sqliteDir = "cmd/sqlgen/testdata/conformance/sqlite"
	cfgDir    = "pkg/config"
	goldenPkg = "cmd/sqlgen"
)

// reportRepo is a repository that passes the static check: two dialect modules, a client file
// whose rows name conformance tests, and a cli file whose rows name a unit test and a golden.
func reportRepo(t *testing.T) string {
	t.Helper()
	return writeRepo(t, reportFiles(t))
}

// reportFiles are reportRepo's files.
func reportFiles(t *testing.T) map[string]string {
	t.Helper()
	prd, err := reqcheck.ParsePRD(reportPRD)
	if err != nil {
		t.Fatalf("ParsePRD() unexpected error: %v", err)
	}
	hash := func(key string) string {
		s, _ := prd.Section(key)
		return s.Hash
	}
	return map[string]string{
		"docs/PRD.md":                reportPRD,
		"docs/conformance/README.md": reportMap,
		"docs/conformance/client.md": `# Client

<!-- seeding: in-progress -->
<!-- matrix: client-methods rows=method:Get,HardDeleteMany cols=happy-path,not-found,lock-mode -->

## §9.3 Delete Operations
<!-- prd: 9.3 sha256: ` + hash("9.3") + ` -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| delete.empty | An empty key list deletes nothing | ` + "`conformance: HardDeleteMany/empty key list deletes nothing`" + ` | all | client-methods: HardDeleteMany × happy-path |
| delete.missing | A missing key is not an error | ` + "`conformance: HardDeleteMany/missing keys are not an error`" + ` | sqlite | |
| delete.untested | A listed key that is absent deletes nothing | | all | client-methods: HardDeleteMany × not-found |
| delete.mysql | MySQL reports the missing key | ` + "`conformance: HardDeleteMany/missing keys are not an error`; `unit: pkg/config: TestLiteral`" + ` | mysql | |

| Matrix cell | Reason | Accepted |
|---|---|---|
| client-methods: HardDeleteMany × lock-mode | §9.3 deletes take no lock mode | |
`,
		"docs/conformance/cli.md": `# CLI

<!-- seeding: in-progress -->
<!-- packages: pkg/config -->
<!-- matrix: naming rows=rule cols=struct -->

## §4.13 Config Validation Rules
<!-- prd: 4.13 sha256: ` + hash("4.13") + ` -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| config.keyed | pgx with MySQL is a validation error | ` + "`unit: pkg/config: TestTable/keyed case`" + ` | all | |
| config.slash | A name ending in a slash | ` + "`unit: pkg/config: TestTable/through ./`" + ` | all | |
| config.golden | The generated example matches | ` + "`golden: postgres`" + ` | all | |

## PRD gaps

| Gap | Behavior | Tests | Section | Ruling |
|---|---|---|---|---|
| gap.config.literal | An unstated behavior | ` + "`unit: pkg/config: TestLiteral`" + ` | §4.13 | |
| gap.config.ruled | Another | | §4.13 | drop-pin 2026-10-24 |
`,
		cfgDir + "/config_test.go":                         reportTests,
		"cmd/sqlgen/testdata/examples/postgres/sqlgen.yml": "version: 1\n",
		reqcheck.ConformanceSuiteDir + "/go.mod":           "module suite\n",
		reqcheck.ConformanceSuiteDir + "/run.go":           reportSuite,
		pgDir + "/go.mod":                                  "module postgres\n",
		pgDir + "/conformance_test.go":                     dialectTest("postgres", "pgx"),
		sqliteDir + "/go.mod":                              "module sqlite\n",
		sqliteDir + "/conformance_test.go":                 dialectTest("sqlite", "stdlib"),
	}
}

const featSqliteDir = "cmd/sqlgen/testdata/conformance/features_sqlite"

const reportFeaturesSuite = `package featuressuite

import "testing"

func Run(t *testing.T) {
	t.Run("Features", func(t *testing.T) {
		t.Run("Events", func(t *testing.T) {
			t.Run("publishes one event", func(t *testing.T) {})
		})
	})
}
`

// featuresDialectTest is a variant module's test: it runs the Features suite, which its import
// names.
const featuresDialectTest = `package features_sqlite

import (
	"testing"

	featuressuite "` + reqcheck.RepoModule + `/cmd/sqlgen/testdata/conformance/features_suite"
)

func TestConformance(t *testing.T) {
	featuressuite.Run(t, &featuressuite.Harness{Dialect: "sqlite", Driver: "stdlib"})
}
`

// featuresRepo is reportRepo with a Features suite, one Features target and a row citing it.
func featuresRepo(t *testing.T) string {
	t.Helper()
	return writeRepo(t, featuresFiles(t))
}

// featuresFiles are featuresRepo's files.
func featuresFiles(t *testing.T) map[string]string {
	t.Helper()
	f := reportFiles(t)
	edit(f, "docs/conformance/client.md", "| mysql | |\n",
		"| mysql | |\n| features.publish | A create publishes one event | `conformance: Features/Events/publishes one event` | all | |\n")
	f[reqcheck.FeaturesSuiteDir+"/go.mod"] = "module featuressuite\n"
	f[reqcheck.FeaturesSuiteDir+"/run.go"] = reportFeaturesSuite
	f[featSqliteDir+"/go.mod"] = "module features_sqlite\n"
	f[featSqliteDir+"/conformance_test.go"] = featuresDialectTest
	return f
}

// event is one go test -json line; dir is the package's repository directory.
type event struct {
	action, dir, test, output, failedBuild string
}

func results(t *testing.T, events ...event) *reqcheck.Results {
	t.Helper()
	var b strings.Builder
	for _, e := range events {
		pkg := reqcheck.RepoModule + "/" + e.dir
		line, err := json.Marshal(map[string]string{
			"Action": e.action, "Package": pkg, "Test": e.test, "Output": e.output, "FailedBuild": e.failedBuild,
		})
		if err != nil {
			t.Fatalf("marshaling event: %v", err)
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	res := reqcheck.NewResults()
	if err := res.Add("run", strings.NewReader(b.String())); err != nil {
		t.Fatalf("Add() unexpected error: %v", err)
	}
	return res
}

// passed is a test that ran and passed, with its package.
func passed(dir, test string) []event {
	return []event{{action: "run", dir: dir, test: test}, {action: "pass", dir: dir, test: test}}
}

func skipped(dir, test, msg string) []event {
	return []event{
		{action: "run", dir: dir, test: test},
		{action: "output", dir: dir, test: test, output: "=== RUN   " + test + "\n"},
		{action: "output", dir: dir, test: test, output: "    harness.go:12: " + msg + "\n"},
		{action: "output", dir: dir, test: test, output: "--- SKIP: " + test + " (0.00s)\n"},
		{action: "skip", dir: dir, test: test},
	}
}

func join(groups ...[]event) []event {
	var out []event
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

const (
	emptyCase   = "TestConformance/HardDeleteMany/empty_key_list_deletes_nothing"
	missingCase = "TestConformance/HardDeleteMany/missing_keys_are_not_an_error"
)

var (
	pass     = reqcheck.Outcome{Status: reqcheck.StatusPass}
	fail     = reqcheck.Outcome{Status: reqcheck.StatusFail}
	na       = reqcheck.Outcome{Status: reqcheck.StatusNA}
	untested = reqcheck.Outcome{Status: reqcheck.StatusUntested}
)

func notRun(msg string) reqcheck.Outcome {
	return reqcheck.Outcome{Status: reqcheck.StatusNotRun, Msg: msg}
}

func TestBuildReportJoin(t *testing.T) {
	root := reportRepo(t)
	emptyRow := func(pg, sqlite reqcheck.Outcome, status reqcheck.Status) reqcheck.RowReport {
		return reqcheck.RowReport{
			ID: "delete.empty", Anchor: "9.3", Cell: "client-methods: HardDeleteMany × happy-path",
			Targets: []reqcheck.Outcome{pg, sqlite}, Tests: na, Status: status,
		}
	}
	tests := []struct {
		name   string
		events []event
		id     string
		want   reqcheck.RowReport
	}{
		{
			name:   "pass on every target",
			events: join(passed(pgDir, emptyCase), passed(sqliteDir, emptyCase)),
			id:     "delete.empty",
			want:   emptyRow(pass, pass, reqcheck.StatusPass),
		},
		{
			name: "fail on one target",
			events: join(passed(sqliteDir, emptyCase),
				[]event{{action: "run", dir: pgDir, test: emptyCase}, {action: "fail", dir: pgDir, test: emptyCase}}),
			id:   "delete.empty",
			want: emptyRow(fail, pass, reqcheck.StatusFail),
		},
		{
			name:   "skip carries the capability message",
			events: join(skipped(pgDir, emptyCase, "needs capability RowLocks"), skipped(sqliteDir, emptyCase, "needs capability RowLocks")),
			id:     "delete.empty",
			want: emptyRow(
				reqcheck.Outcome{Status: reqcheck.StatusSkip, Msg: "needs capability RowLocks"},
				reqcheck.Outcome{Status: reqcheck.StatusSkip, Msg: "needs capability RowLocks"},
				reqcheck.StatusSkip),
		},
		{
			name:   "ran on one dialect, its parent skipped on the other",
			events: join(skipped(pgDir, "TestConformance", "needs Docker"), passed(sqliteDir, emptyCase)),
			id:     "delete.empty",
			want:   emptyRow(notRun("TestConformance skipped: needs Docker"), pass, reqcheck.StatusNotRun),
		},
		{
			name: "parent failed before the subtest ran",
			events: join(passed(sqliteDir, emptyCase),
				[]event{{action: "fail", dir: pgDir, test: "TestConformance"}, {action: "fail", dir: pgDir}}),
			id:   "delete.empty",
			want: emptyRow(notRun("TestConformance failed before it ran"), pass, reqcheck.StatusNotRun),
		},
		{
			name:   "a dialect module missing from the results is not run, never pass",
			events: passed(sqliteDir, emptyCase),
			id:     "delete.empty",
			want:   emptyRow(notRun("its package is not in the results"), pass, reqcheck.StatusNotRun),
		},
		{
			name:   "a target outside applies-to is not applicable",
			events: join(passed(pgDir, missingCase), passed(sqliteDir, missingCase)),
			id:     "delete.missing",
			want: reqcheck.RowReport{
				ID: "delete.missing", Anchor: "9.3",
				Targets: []reqcheck.Outcome{na, pass}, Tests: na, Status: reqcheck.StatusPass,
			},
		},
		{
			name:   "conformance tests no dialect module runs are not run, whatever the other tests did",
			events: join(passed(cfgDir, "TestLiteral"), passed(pgDir, missingCase), passed(sqliteDir, missingCase)),
			id:     "delete.mysql",
			want: reqcheck.RowReport{
				ID: "delete.mysql", Anchor: "9.3",
				Targets: []reqcheck.Outcome{na, na}, Tests: pass, Status: reqcheck.StatusNotRun, NoTarget: true,
			},
		},
		{
			name:   "a row with no tests is untested",
			events: passed(sqliteDir, emptyCase),
			id:     "delete.untested",
			want: reqcheck.RowReport{
				ID: "delete.untested", Anchor: "9.3", Cell: "client-methods: HardDeleteMany × not-found",
				Targets: []reqcheck.Outcome{untested, untested}, Tests: untested, Status: reqcheck.StatusUntested,
			},
		},
		{
			name:   "a unit subtest with spaces matches go test's underscores",
			events: passed(cfgDir, "TestTable/keyed_case"),
			id:     "config.keyed",
			want: reqcheck.RowReport{
				ID: "config.keyed", Anchor: "4.13",
				Targets: []reqcheck.Outcome{na, na}, Tests: pass, Status: reqcheck.StatusPass,
			},
		},
		{
			name:   "a subtest name ending in a slash keeps it, as go test reports it",
			events: passed(cfgDir, "TestTable/through_./"),
			id:     "config.slash",
			want: reqcheck.RowReport{
				ID: "config.slash", Anchor: "4.13",
				Targets: []reqcheck.Outcome{na, na}, Tests: pass, Status: reqcheck.StatusPass,
			},
		},
		{
			name:   "a subtest with no result whose parent passed is not run",
			events: passed(cfgDir, "TestTable"),
			id:     "config.keyed",
			want: reqcheck.RowReport{
				ID: "config.keyed", Anchor: "4.13",
				Targets: []reqcheck.Outcome{na, na}, Tests: notRun("no result"), Status: reqcheck.StatusNotRun,
			},
		},
		{
			name:   "a package that failed to build is not run",
			events: []event{{action: "fail", dir: cfgDir, failedBuild: reqcheck.RepoModule + "/" + cfgDir + " [build failed]"}},
			id:     "config.keyed",
			want: reqcheck.RowReport{
				ID: "config.keyed", Anchor: "4.13",
				Targets: []reqcheck.Outcome{na, na}, Tests: notRun("its package failed to build"), Status: reqcheck.StatusNotRun,
			},
		},
		{
			name:   "a golden reference is its example's subtest of the golden test",
			events: passed(goldenPkg, "TestE2EGoldenFiles/postgres"),
			id:     "config.golden",
			want: reqcheck.RowReport{
				ID: "config.golden", Anchor: "4.13",
				Targets: []reqcheck.Outcome{na, na}, Tests: pass, Status: reqcheck.StatusPass,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rep, err := reqcheck.BuildReport(root, results(t, tt.events...))
			if err != nil {
				t.Fatalf("BuildReport() unexpected error: %v", err)
			}
			var got *reqcheck.RowReport
			for _, f := range rep.Files {
				for i := range f.Rows {
					if f.Rows[i].ID == tt.id {
						got = &f.Rows[i]
					}
				}
			}
			if got == nil {
				t.Fatalf("BuildReport() has no row %s", tt.id)
			}
			if diff := cmp.Diff(tt.want, *got); diff != "" {
				t.Errorf("row %s mismatch (-want +got):\n%s", tt.id, diff)
			}
		})
	}

	t.Run("a suite-rooted citation joins only that suite's targets", func(t *testing.T) {
		const featuresCase = "TestConformance/Features/Events/publishes_one_event"
		// Every target reports both names, so only the routing decides which result a row takes.
		var events []event
		for _, dir := range []string{pgDir, sqliteDir, featSqliteDir} {
			events = join(events, passed(dir, emptyCase), passed(dir, featuresCase))
		}
		rep, err := reqcheck.BuildReport(featuresRepo(t), results(t, events...))
		if err != nil {
			t.Fatalf("BuildReport() unexpected error: %v", err)
		}
		got := map[string]reqcheck.RowReport{}
		for _, f := range rep.Files {
			for _, r := range f.Rows {
				got[r.ID] = r
			}
		}
		want := map[string]reqcheck.RowReport{
			"delete.empty": {
				ID: "delete.empty", Anchor: "9.3", Cell: "client-methods: HardDeleteMany × happy-path",
				Targets: []reqcheck.Outcome{pass, pass, na}, Tests: na, Status: reqcheck.StatusPass,
			},
			"features.publish": {
				ID: "features.publish", Anchor: "9.3",
				Targets: []reqcheck.Outcome{na, na, pass}, Tests: na, Status: reqcheck.StatusPass,
			},
		}
		for id, w := range want {
			if diff := cmp.Diff(w, got[id]); diff != "" {
				t.Errorf("row %s mismatch (-want +got):\n%s", id, diff)
			}
		}
	})
}

func TestBuildReportTargets(t *testing.T) {
	root := reportRepo(t)
	res := results(t, event{
		action: "output", dir: sqliteDir, test: "TestConformance",
		output: "    harness.go:40: conformance harness: database SQLite 3.51.3\n",
	})
	rep, err := reqcheck.BuildReport(root, res)
	if err != nil {
		t.Fatalf("BuildReport() unexpected error: %v", err)
	}
	want := []reqcheck.Target{
		{Dir: pgDir, Dialect: "postgres", Driver: "pgx"},
		{Dir: sqliteDir, Dialect: "sqlite", Driver: "stdlib", Database: "SQLite 3.51.3"},
	}
	if diff := cmp.Diff(want, rep.Targets); diff != "" {
		t.Errorf("Targets mismatch (-want +got):\n%s", diff)
	}

	t.Run("a module that imports no suite is an error", func(t *testing.T) {
		f := reportFiles(t)
		edit(f, pgDir+"/conformance_test.go", "\t\""+reqcheck.RepoModule+"/"+reqcheck.ConformanceSuiteDir+"\"\n", "")
		_, err := reqcheck.BuildReport(writeRepo(t, f), results(t))
		if err == nil || !strings.Contains(err.Error(), pgDir+": its tests import no conformance suite") {
			t.Errorf("BuildReport() error = %v, want one naming %s and the missing suite import", err, pgDir)
		}
	})

	t.Run("a module that imports two suites is an error", func(t *testing.T) {
		f := featuresFiles(t)
		core := "\t\"" + reqcheck.RepoModule + "/" + reqcheck.ConformanceSuiteDir + "\"\n"
		edit(f, featSqliteDir+"/conformance_test.go", "\"testing\"\n", "\"testing\"\n\n"+core)
		_, err := reqcheck.BuildReport(writeRepo(t, f), results(t))
		want := []string{featSqliteDir + ": its tests import more than one conformance suite", reqcheck.ConformanceSuiteDir, reqcheck.FeaturesSuiteDir}
		for _, w := range want {
			if err == nil || !strings.Contains(err.Error(), w) {
				t.Errorf("BuildReport() error = %v, want one containing %q", err, w)
			}
		}
	})

	t.Run("variant modules are targets of their suite", func(t *testing.T) {
		res := results(t, event{
			action: "output", dir: featSqliteDir, test: "TestConformance",
			output: "    harness.go:30: conformance harness: database SQLite 3.51.3\n",
		})
		rep, err := reqcheck.BuildReport(featuresRepo(t), res)
		if err != nil {
			t.Fatalf("BuildReport() unexpected error: %v", err)
		}
		// The core targets come first, then each variant suite's.
		want := []reqcheck.Target{
			{Dir: pgDir, Dialect: "postgres", Driver: "pgx"},
			{Dir: sqliteDir, Dialect: "sqlite", Driver: "stdlib"},
			{Dir: featSqliteDir, Suite: "Features", Dialect: "sqlite", Driver: "stdlib", Database: "SQLite 3.51.3"},
		}
		if diff := cmp.Diff(want, rep.Targets); diff != "" {
			t.Errorf("Targets mismatch (-want +got):\n%s", diff)
		}
		labels := make([]string, 0, len(rep.Targets))
		for _, tg := range rep.Targets {
			labels = append(labels, tg.Label())
		}
		if diff := cmp.Diff([]string{"postgres/pgx", "sqlite/stdlib", "Features sqlite/stdlib"}, labels); diff != "" {
			t.Errorf("Label() mismatch (-want +got):\n%s", diff)
		}
	})
}

func TestBuildReportRejectsFailingRegistry(t *testing.T) {
	root := reportRepo(t)
	// The tests the rows claim vanish.
	if err := os.WriteFile(filepath.Join(root, cfgDir, "config_test.go"), []byte("package config\n"), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	_, err := reqcheck.BuildReport(root, results(t))
	if err == nil || !strings.Contains(err.Error(), "fails make req-check") {
		t.Errorf("BuildReport() error = %v, want the registry's req-check failure", err)
	}
}

func TestResultsAddRejectsNonJSON(t *testing.T) {
	err := reqcheck.NewResults().Add("run", strings.NewReader("ok  \tpkg\t0.1s\n"))
	if err == nil || !strings.Contains(err.Error(), "run line 1") {
		t.Errorf("Add() error = %v, want a line-numbered error", err)
	}
}

// reportEvents is a run with one of each status: postgres skipped for want of Docker, sqlite
// passing one subtest and skipping the other for a capability, the unit subtest passing, the
// golden test failing.
func reportEvents() []event {
	return join(
		skipped(pgDir, "TestConformance", "needs Docker"),
		[]event{{action: "pass", dir: pgDir}},
		[]event{{action: "output", dir: sqliteDir, test: "TestConformance", output: "    harness.go:40: conformance harness: database SQLite 3.51.3\n"}},
		passed(sqliteDir, emptyCase),
		skipped(sqliteDir, missingCase, "needs capability | RowLocks"),
		[]event{{action: "pass", dir: sqliteDir}},
		passed(cfgDir, "TestTable/keyed_case"),
		passed(cfgDir, "TestTable/through_./"),
		passed(cfgDir, "TestLiteral"),
		[]event{{action: "pass", dir: cfgDir}},
		[]event{{action: "run", dir: goldenPkg, test: "TestE2EGoldenFiles/postgres"}, {action: "fail", dir: goldenPkg, test: "TestE2EGoldenFiles/postgres"}, {action: "fail", dir: goldenPkg}},
	)
}

const wantReport = "# Conformance Report\n\n" +
	"<!-- Generated by `make conformance` (docs/design/CONFORMANCE.md §5.5). Do not edit. -->\n\n" +
	"Commit `abc123`, run 2026-10-24. The registry in this directory joined with the `go test -json` results of every registered test home.\n\n" +
	`| Conformance target | Module | Database |
|---|---|---|
| postgres/pgx | ` + "`" + pgDir + "`" + ` | not run |
| sqlite/stdlib | ` + "`" + sqliteDir + "`" + ` | SQLite 3.51.3 |

Statuses: ` + "`pass`; `fail`; `skip`, with the test's skip message; `not run`, the test has no result (its package did not build or run, or a parent test skipped or failed); `untested`, the row names no test; `—`, the row does not apply." +
	` A row's status is its most severe target. On one target, a failure or a test that did not run decides; otherwise one pass is enough.

## Modules

| File | Seeding | Rows | Pass | Fail | Skip | Not run | Untested |
|---|---|---|---|---|---|---|---|
| cli.md | in-progress | 3 | 2 | 1 | 0 | 0 | 0 |
| client.md | in-progress | 4 | 0 | 0 | 1 | 2 | 1 |

## Failing and not-run rows

Requirements and PRD-gap pinning tests.

| File | ID | Where | Status |
|---|---|---|---|
| cli.md | config.golden | tests | **fail** |
| client.md | delete.empty | postgres/pgx | not run: TestConformance skipped: needs Docker |
| client.md | delete.mysql | — | not run: no conformance module it applies to |

## Test runs

One ` + "`go test -json`" + ` run per module, example and conformance module. Failed counts packages with a failing test, a failed build, or a test binary that stopped early.

| Run | Packages | Passed | Failed | No tests |
|---|---|---|---|---|
| run | 4 | 3 | 1 | 0 |

## Requirements

### cli.md

| Section | ID | tests |
|---|---|---|
| §4.13 | config.keyed | pass |
| §4.13 | config.slash | pass |
| §4.13 | config.golden | **fail** |

### client.md

| Section | ID | postgres/pgx | sqlite/stdlib | tests |
|---|---|---|---|---|
| §9.3 | delete.empty | not run: TestConformance skipped: needs Docker | pass | — |
| §9.3 | delete.missing | — | skip: needs capability \| RowLocks | — |
| §9.3 | delete.untested | untested | untested | untested |
| §9.3 | delete.mysql | — | — | pass |

## Matrices

Each cell shows the status of the row that claims it, ` + "`n/a`" + ` when declared not applicable, or ` + "`empty`" + `.

### cli.md: naming

No rows yet.

### client.md: client-methods

| method | happy-path | not-found | lock-mode |
|---|---|---|---|
| Get | empty | empty | empty |
| HardDeleteMany | not run | untested | n/a |

## PRD coverage

PRD sections marked ` + "`informative`" + `: 1. Owned sections with no requirement rows:

| File | Sections | Owned sections with no rows |
|---|---|---|
| cli.md | 0 | — |
| client.md | 0 | — |

## PRD gaps

1 open, 1 ruled and waiting to be applied.

| File | Gap | Section | Ruling | Pinning tests |
|---|---|---|---|---|
| cli.md | gap.config.literal | §4.13 | open | pass |
| cli.md | gap.config.ruled | §4.13 | drop-pin 2026-10-24 | untested |

`

func TestReportMarkdown(t *testing.T) {
	root := reportRepo(t)
	rep, err := reqcheck.BuildReport(root, results(t, reportEvents()...))
	if err != nil {
		t.Fatalf("BuildReport() unexpected error: %v", err)
	}
	got := rep.Markdown(reqcheck.Header{Commit: "abc123", Date: "2026-10-24"})
	if diff := cmp.Diff(wantReport, got); diff != "" {
		t.Errorf("Markdown() mismatch (-want +got):\n%s", diff)
	}
	// Rendering again from fresh results gives the same bytes: nothing depends on map order.
	again, err := reqcheck.BuildReport(root, results(t, reportEvents()...))
	if err != nil {
		t.Fatalf("BuildReport() unexpected error: %v", err)
	}
	if diff := cmp.Diff(got, again.Markdown(reqcheck.Header{Commit: "abc123", Date: "2026-10-24"})); diff != "" {
		t.Errorf("Markdown() is not deterministic (-first +second):\n%s", diff)
	}
}

func TestSkipMessage(t *testing.T) {
	// TestLiteral alone pins gap.config.literal, so the gap carries the skip.
	const test = "TestLiteral"
	tests := []struct {
		name   string
		output []string
		want   string
	}{
		{
			name:   "one line",
			output: []string{"=== RUN   TestLiteral\n", "    a_test.go:5: needs capability RowLocks\n", "--- SKIP: TestLiteral (0.00s)\n"},
			want:   "needs capability RowLocks",
		},
		{
			name:   "continuation lines join the message",
			output: []string{"=== RUN   TestLiteral\n", "    a_test.go:5: needs cap\n", "        second line\n", "--- SKIP: TestLiteral (0.00s)\n"},
			want:   "needs cap second line",
		},
		{
			name:   "a bare skip has no message",
			output: []string{"=== RUN   TestLiteral\n", "    a_test.go:8: \n", "--- SKIP: TestLiteral (0.00s)\n"},
			want:   "",
		},
		{
			name:   "stray process output before a bare skip is not its message",
			output: []string{"=== RUN   TestLiteral\n", "container started\n", "    a_test.go:8: \n", "--- SKIP: TestLiteral (0.00s)\n"},
			want:   "",
		},
		{
			name:   "process output after the message is not part of it",
			output: []string{"=== RUN   TestLiteral\n", "    a_test.go:5: needs capability RowLocks\n", "Terminating container abc\n", "--- SKIP: TestLiteral (0.00s)\n"},
			want:   "needs capability RowLocks",
		},
		{
			name:   "an earlier log line is not the skip message",
			output: []string{"=== RUN   TestLiteral\n", "    a_test.go:4: setting up\n", "    a_test.go:5: needs Docker\n", "--- SKIP: TestLiteral (0.00s)\n"},
			want:   "needs Docker",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events := []event{{action: "run", dir: cfgDir, test: test}}
			for _, o := range tt.output {
				events = append(events, event{action: "output", dir: cfgDir, test: test, output: o})
			}
			events = append(events, event{action: "skip", dir: cfgDir, test: test})
			rep, err := reqcheck.BuildReport(reportRepo(t), results(t, events...))
			if err != nil {
				t.Fatalf("BuildReport() unexpected error: %v", err)
			}
			var got reqcheck.Outcome
			for _, f := range rep.Files {
				for _, g := range f.Gaps {
					if g.ID == "gap.config.literal" {
						got = g.Tests
					}
				}
			}
			want := reqcheck.Outcome{Status: reqcheck.StatusSkip, Msg: tt.want}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("skip outcome mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestResultsRuns(t *testing.T) {
	res := results(t,
		event{action: "pass", dir: cfgDir},
		event{action: "output", dir: "pkg/empty", output: "?   \tpkg/empty\t[no test files]\n"},
		event{action: "skip", dir: "pkg/empty"},
		event{action: "fail", dir: "pkg/broken", failedBuild: "pkg/broken [build failed]"},
		event{action: "start", dir: "pkg/stopped"},
	)
	want := []reqcheck.Run{{Name: "run", Packages: 4, Passed: 1, Failed: 2, NoTests: 1}}
	if diff := cmp.Diff(want, res.Runs); diff != "" {
		t.Errorf("Runs mismatch (-want +got):\n%s", diff)
	}
}

func TestLoadResultsEmptyDir(t *testing.T) {
	_, err := reqcheck.LoadResults(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "no go test -json results") {
		t.Errorf("LoadResults() error = %v, want a no-results error", err)
	}
}
