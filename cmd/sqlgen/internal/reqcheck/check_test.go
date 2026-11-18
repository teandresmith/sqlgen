package reqcheck_test

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/internal/reqcheck"
)

const checkPRD = `# PRD

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

const checkMap = `# Registry

| PRD section | Owner |
|---|---|
| §4. Configuration System | cli.md |
| §4.13 Config Validation Rules | cli.md |
| §9. Generated Client Operations | client.md |
| §9.3 Delete Operations | client.md |
| §Appendix C: Project Linting Configuration | informative |
`

const checkTests = `package config

import (
	"fmt"
	"testing"
)

func TestLiteral(t *testing.T) {
	t.Run("driver mismatch", func(t *testing.T) {
		t.Run("nested case", func(t *testing.T) {})
	})
}

func TestTable(t *testing.T) {
	tests := []struct {
		name string
		in   int
	}{
		{name: "keyed case", in: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {})
	}
}

func TestPositional(t *testing.T) {
	for _, tc := range []struct {
		desc string
		n    int
	}{{"positional case", 1}} {
		t.Run(tc.desc, func(t *testing.T) {})
	}
}

func TestMap(t *testing.T) {
	cases := map[string]int{"map case": 1}
	for name := range cases {
		t.Run(name, func(t *testing.T) {})
	}
}

func TestHelper(t *testing.T) { runShared(t) }

func runShared(t *testing.T) { t.Run("from helper", func(t *testing.T) {}) }

func TestComputed(t *testing.T) {
	for i := range 2 {
		t.Run(fmt.Sprint(i), func(t *testing.T) {})
	}
}

func TestSlash(t *testing.T) { t.Run("from/to", func(t *testing.T) {}) }

func TestEnclosingTable(t *testing.T) {
	tests := []struct{ name string }{{name: "outer table case"}}
	t.Run("group", func(t *testing.T) {
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {})
		}
	})
}

type base struct{}

func TestEmbedded(t *testing.T) {
	for _, tc := range []struct {
		base
		desc string
	}{{base{}, "embedded case"}} {
		t.Run(tc.desc, func(t *testing.T) {})
	}
}
`

const checkSuite = `package suite

import "testing"

func Run(t *testing.T) {
	t.Run("HardDeleteMany", func(t *testing.T) { hardDeleteMany(t) })
}

func hardDeleteMany(t *testing.T) {
	t.Run("empty key list deletes nothing", func(t *testing.T) {})
	t.Run("missing keys are not an error", func(t *testing.T) {})
}
`

// baseRepo is a repository the check passes on: every test style resolves, and the anchor
// hash is current.
func baseRepo(t *testing.T) map[string]string {
	t.Helper()
	prd, err := reqcheck.ParsePRD(checkPRD)
	if err != nil {
		t.Fatalf("ParsePRD() unexpected error: %v", err)
	}
	s, _ := prd.Section("4.13")
	tests := strings.Join([]string{
		"unit: pkg/config: TestLiteral/driver mismatch/nested case",
		"unit: pkg/config: TestTable/keyed case",
		"unit: pkg/config: TestTable/keyed_case",
		"unit: pkg/config: TestPositional/positional case",
		"unit: pkg/config: TestMap/map case",
		"unit: pkg/config: TestHelper/from helper",
		"unit: pkg/config: TestComputed",
		"unit: pkg/config: TestSlash/from/to",
		"unit: pkg/config: TestEnclosingTable/group/outer table case",
		"unit: pkg/config: TestEmbedded/embedded case",
	}, "; ")
	return map[string]string{
		"docs/PRD.md":                checkPRD,
		"docs/conformance/README.md": checkMap,
		"docs/conformance/cli.md": `# CLI

<!-- seeding: in-progress -->
<!-- packages: pkg/config -->
<!-- matrix: config-validation rows=rule cols=fires,message -->

## §4.13 Config Validation Rules
<!-- prd: 4.13 sha256: ` + s.Hash + ` -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| config.driver-mismatch | pgx with MySQL is a validation error | ` + tests + ` | all | config-validation: driver × fires |
`,
		"docs/conformance/client.md": "# Client\n\n<!-- seeding: not-started -->\n",
		"pkg/config/config_test.go":  checkTests,
	}
}

func writeRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatalf("creating %s: %v", name, err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	return root
}

// edit replaces old in a fixture file and panics when old is absent, so a stale mutation fails
// loudly instead of leaving the fixture unchanged.
func edit(files map[string]string, name, old, replacement string) {
	if !strings.Contains(files[name], old) {
		panic(fmt.Sprintf("edit %s: %q not found", name, old))
	}
	files[name] = strings.Replace(files[name], old, replacement, 1)
}

func TestCheck(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(files map[string]string)
		wantErr   string // empty: the check passes
		wantNote  string
		wantNoErr bool
	}{
		{
			name:      "baseline resolves every subtest style",
			mutate:    func(map[string]string) {},
			wantNoErr: true,
		},
		{
			name: "missing subtest",
			mutate: func(f map[string]string) {
				edit(f, "docs/conformance/cli.md", "TestTable/keyed case;", "TestTable/other case;")
			},
			wantErr: `TestTable has no subtest "other case"`,
		},
		{
			name: "part of a subtest name containing a slash",
			mutate: func(f map[string]string) {
				edit(f, "docs/conformance/cli.md", "TestSlash/from/to;", "TestSlash/from;")
			},
			wantErr: `"unit: pkg/config: TestSlash/from" names only part of a subtest name`,
		},
		{
			name: "a subtest name ending in a slash without its slash",
			mutate: func(f map[string]string) {
				edit(f, "pkg/config/config_test.go", `t.Run("from/to", func(t *testing.T) {})`, `t.Run("through ./", func(t *testing.T) {})`)
				edit(f, "docs/conformance/cli.md", "TestSlash/from/to;", "TestSlash/through .;")
			},
			wantErr: `"unit: pkg/config: TestSlash/through ." names only part of a subtest name`,
		},
		{
			name: "a subtest name ending in a slash resolves with it",
			mutate: func(f map[string]string) {
				edit(f, "pkg/config/config_test.go", `t.Run("from/to", func(t *testing.T) {})`, `t.Run("through ./", func(t *testing.T) {})`)
				edit(f, "docs/conformance/cli.md", "TestSlash/from/to;", "TestSlash/through ./;")
			},
			wantNoErr: true,
		},
		{
			name: "computed subtest names cannot be verified",
			mutate: func(f map[string]string) {
				edit(f, "docs/conformance/cli.md", "TestComputed;", "TestComputed/0;")
			},
			wantErr: "subtest names under TestComputed are computed",
		},
		{
			name: "missing test function",
			mutate: func(f map[string]string) {
				edit(f, "docs/conformance/cli.md", "TestComputed;", "TestGone;")
			},
			wantErr: "pkg/config has no test function TestGone",
		},
		{
			name: "missing package directory",
			mutate: func(f map[string]string) {
				edit(f, "docs/conformance/cli.md", "unit: pkg/config: TestComputed", "unit: pkg/gone: TestComputed")
			},
			wantErr: "directory pkg/gone does not exist",
		},
		{
			name: "PRD section changed under its anchor",
			mutate: func(f map[string]string) {
				edit(f, "docs/PRD.md", "pgx with MySQL is a validation error.", "pgx with MySQL or SQLite is a validation error.")
			},
			wantErr: "PRD §4.13 Config Validation Rules changed since its requirements were reviewed",
		},
		{
			name: "anchor without a reviewed hash",
			mutate: func(f map[string]string) {
				f["docs/conformance/cli.md"] = regexpReplaceHash(f["docs/conformance/cli.md"], "pending")
			},
			wantErr: "anchor §4.13 has no reviewed hash",
		},
		{
			name: "anchor names no PRD section",
			mutate: func(f map[string]string) {
				edit(f, "docs/conformance/cli.md", "<!-- prd: 4.13 ", "<!-- prd: 4.99 ")
			},
			wantErr: "anchor §4.99 is not a PRD section heading",
		},
		{
			name: "anchor on an informative section",
			mutate: func(f map[string]string) {
				edit(f, "docs/conformance/cli.md", "<!-- prd: 4.13 ", "<!-- prd: Appendix C ")
			},
			wantErr: "anchor §Appendix C is an informative section",
		},
		{
			name: "new PRD section has no owner",
			mutate: func(f map[string]string) {
				edit(f, "docs/PRD.md", "## 9. Generated Client Operations\n", "## 9. Generated Client Operations\n\n### 9.4 Pagination\n\nPages.\n")
			},
			wantErr: "§9.4 Pagination (PRD line",
		},
		{
			name: "section mapped twice",
			mutate: func(f map[string]string) {
				f["docs/conformance/README.md"] += "| §9.3 Delete Operations | cli.md |\n"
			},
			wantErr: "§9.3 is mapped twice",
		},
		{
			name: "unknown owner",
			mutate: func(f map[string]string) {
				edit(f, "docs/conformance/README.md", "| §9.3 Delete Operations | client.md |", "| §9.3 Delete Operations | api.md |")
			},
			wantErr: `owned by "api.md", which is neither a registry file`,
		},
		{
			name: "map lists a level-4 heading",
			mutate: func(f map[string]string) {
				edit(f, "docs/PRD.md", "Config.\n", "Config.\n\n#### 4.0.1 Deep\n\nDeep.\n")
				f["docs/conformance/README.md"] += "| §4.0.1 Deep | cli.md |\n"
			},
			wantErr: "§4.0.1 is a level-4 heading",
		},
		{
			name: "id reused across files",
			mutate: func(f map[string]string) {
				f["docs/conformance/client.md"] = "# Client\n\n<!-- seeding: in-progress -->\n\n## §9.3\n<!-- prd: 9.3 sha256: pending -->\n\n| ID | Requirement | Tests | Applies to | Matrix cell |\n|---|---|---|---|---|\n| config.driver-mismatch | Dup | | all | |\n"
			},
			wantErr: "id config.driver-mismatch is also used at docs/conformance/cli.md",
		},
		{
			name: "id is not a slug",
			mutate: func(f map[string]string) {
				edit(f, "docs/conformance/cli.md", "| config.driver-mismatch |", "| Config_Driver |")
			},
			wantErr: `id "Config_Driver" is not a lower-case slug`,
		},
		{
			name: "unknown applies-to value",
			mutate: func(f map[string]string) {
				edit(f, "docs/conformance/cli.md", "| all | config-validation", "| oracle | config-validation")
			},
			wantErr: `applies to "oracle", which is not one of`,
		},
		{
			name: "cell names an undeclared matrix",
			mutate: func(f map[string]string) {
				edit(f, "docs/conformance/cli.md", "config-validation: driver × fires |", "naming: driver × fires |")
			},
			wantErr: `names matrix "naming", which this file does not declare`,
		},
		{
			name: "cell names an undeclared column",
			mutate: func(f map[string]string) {
				edit(f, "docs/conformance/cli.md", "driver × fires |", "driver × severity |")
			},
			wantErr: `names column "severity"; config-validation declares fires, message`,
		},
		{
			name: "n/a without a PRD citation or acceptance",
			mutate: func(f map[string]string) {
				f["docs/conformance/cli.md"] += "\n| Matrix cell | Reason | Accepted |\n|---|---|---|\n| config-validation: driver × message | not meaningful | |\n"
			},
			wantErr: "cites no PRD section; cite one (§…) or record the user's acceptance date",
		},
		{
			name: "n/a acceptance is not a date",
			mutate: func(f map[string]string) {
				f["docs/conformance/cli.md"] += "\n| Matrix cell | Reason | Accepted |\n|---|---|---|\n| config-validation: driver × message | not meaningful | yes |\n"
			},
			wantErr: `accepted "yes" is not a YYYY-MM-DD date`,
		},
		{
			name: "n/a cell a requirement also claims",
			mutate: func(f map[string]string) {
				f["docs/conformance/cli.md"] += "\n| Matrix cell | Reason | Accepted |\n|---|---|---|\n| config-validation: driver × fires | §4.13 | |\n"
			},
			wantErr: "is declared n/a but requirement on line",
		},
		{
			name: "not-started file with rows",
			mutate: func(f map[string]string) {
				edit(f, "docs/conformance/cli.md", "seeding: in-progress", "seeding: not-started")
			},
			wantErr: "seeding is not-started but the file has rows",
		},
		{
			name:     "in-progress empty cell is a note",
			mutate:   func(map[string]string) {},
			wantNote: "matrix config-validation: cell driver × message is empty",
		},
		{
			name: "complete file fails on an empty cell",
			mutate: func(f map[string]string) {
				edit(f, "docs/conformance/cli.md", "seeding: in-progress", "seeding: complete")
			},
			wantErr: "matrix config-validation: cell driver × message is empty",
		},
		{
			name: "not-started file with a declared matrix has no findings",
			mutate: func(f map[string]string) {
				f["docs/conformance/client.md"] = "# Client\n\n<!-- seeding: not-started -->\n<!-- matrix: client-methods rows=method cols=happy-path -->\n"
			},
			wantNoErr: true,
		},
		{
			name: "cell names a row a closed matrix does not declare",
			mutate: func(f map[string]string) {
				edit(f, "docs/conformance/cli.md", "config-validation rows=rule cols", "config-validation rows=rule:schema cols")
			},
			wantErr: `names row "driver", which config-validation does not declare`,
		},
		{
			name: "declared row with no cells is a note while seeding",
			mutate: func(f map[string]string) {
				edit(f, "docs/conformance/cli.md", "config-validation rows=rule cols", "config-validation rows=rule:driver,schema cols")
			},
			wantNote: "matrix config-validation: cell schema × fires is empty",
		},
		{
			name: "complete file fails on a declared row with no cells",
			mutate: func(f map[string]string) {
				edit(f, "docs/conformance/cli.md", "config-validation rows=rule cols", "config-validation rows=rule:driver,schema cols")
				edit(f, "docs/conformance/cli.md", "seeding: in-progress", "seeding: complete")
			},
			wantErr: "matrix config-validation: cell schema × fires is empty",
		},
		{
			name: "declared matrix with no rows is a note while seeding",
			mutate: func(f map[string]string) {
				edit(f, "docs/conformance/cli.md", "<!-- matrix: config-validation", "<!-- matrix: naming rows=rule cols=struct,field -->\n<!-- matrix: config-validation")
			},
			wantNote: "matrix naming has no rows",
		},
		{
			name: "complete file fails on a declared matrix with no rows",
			mutate: func(f map[string]string) {
				edit(f, "docs/conformance/cli.md", "<!-- matrix: config-validation", "<!-- matrix: naming rows=rule cols=struct,field -->\n<!-- matrix: config-validation")
				edit(f, "docs/conformance/cli.md", "seeding: in-progress", "seeding: complete")
			},
			wantErr: "matrix naming has no rows",
		},
		{
			name: "complete file fails on an unclaimed test",
			mutate: func(f map[string]string) {
				edit(f, "docs/conformance/cli.md", "seeding: in-progress", "seeding: complete")
			},
			wantErr: "test pkg/config: TestRelatedHelperless is claimed by no requirement",
		},
		{
			name: "unclaimed entry naming a missing test",
			mutate: func(f map[string]string) {
				f["docs/conformance/cli.md"] += "\n| Unclaimed test | Reason |\n|---|---|\n| pkg/config: TestGone | helper |\n"
			},
			wantErr: "pkg/config has no test function TestGone",
		},
		{
			name: "owned section with no rows is a note while seeding",
			mutate: func(f map[string]string) {
				edit(f, "docs/PRD.md", "## 9. Generated Client Operations\n", "### 4.14 Schema\n\nSchema.\n\n## 9. Generated Client Operations\n")
				f["docs/conformance/README.md"] += "| §4.14 Schema | cli.md |\n"
			},
			wantNote: "owned PRD §4.14 Schema has no requirement rows",
		},
		{
			name: "owned section with no rows fails when complete",
			mutate: func(f map[string]string) {
				edit(f, "docs/PRD.md", "## 9. Generated Client Operations\n", "### 4.14 Schema\n\nSchema.\n\n## 9. Generated Client Operations\n")
				f["docs/conformance/README.md"] += "| §4.14 Schema | cli.md |\n"
				edit(f, "docs/conformance/cli.md", "seeding: in-progress", "seeding: complete")
			},
			wantErr: "owned PRD §4.14 Schema has no requirement rows",
		},
		{
			name: "map names a section the PRD lacks",
			mutate: func(f map[string]string) {
				f["docs/conformance/README.md"] += "| §4.99 Ghost | cli.md |\n"
			},
			wantErr: "§4.99 is not a PRD section heading",
		},
		{
			name: "n/a cites a section the PRD lacks",
			mutate: func(f map[string]string) {
				f["docs/conformance/cli.md"] += "\n| Matrix cell | Reason | Accepted |\n|---|---|---|\n| config-validation: driver × message | §99 states nothing | |\n"
			},
			wantErr: "cites §99, which is not a PRD section",
		},
		{
			name: "test directory no registry file lists",
			mutate: func(f map[string]string) {
				f["pkg/other/other_test.go"] = "package other\n"
			},
			wantErr: "pkg/other has tests but no registry file lists it",
		},
		{
			name: "unregistered test directory passes",
			mutate: func(f map[string]string) {
				f["pkg/other/other_test.go"] = "package other\n"
				f["docs/conformance/README.md"] += "\n<!-- unregistered: pkg/other -->\n"
			},
			wantNoErr: true,
		},
		{
			name: "computed suite subtest names cannot be claimed",
			mutate: func(f map[string]string) {
				f["cmd/sqlgen/testdata/conformance/suite/suite.go"] = strings.Replace(checkSuite, `import "testing"`, "import (\n\t\"fmt\"\n\t\"testing\"\n)", 1) +
					"\nfunc init() { _ = fmt.Sprint }\n"
				edit(f, "cmd/sqlgen/testdata/conformance/suite/suite.go", `t.Run("missing keys are not an error", func(t *testing.T) {})`, `for i := range 2 {
		t.Run(fmt.Sprint(i), func(t *testing.T) {})
	}`)
				f["docs/conformance/client.md"] = "# Client\n\n<!-- seeding: in-progress -->\n\n## §9.3\n<!-- prd: 9.3 sha256: " + sectionHash(t, "9.3") + " -->\n\n| ID | Requirement | Tests | Applies to | Matrix cell |\n|---|---|---|---|---|\n| delete.empty | Empty deletes nothing | conformance: HardDeleteMany/empty key list deletes nothing | all | |\n"
			},
			wantErr: "subtest names under HardDeleteMany are computed, so they cannot be claimed",
		},
		{
			name: "conformance subtest claimed by no requirement",
			mutate: func(f map[string]string) {
				f["cmd/sqlgen/testdata/conformance/suite/suite.go"] = checkSuite
				f["docs/conformance/client.md"] = "# Client\n\n<!-- seeding: in-progress -->\n\n## §9.3\n<!-- prd: 9.3 sha256: " + sectionHash(t, "9.3") + " -->\n\n| ID | Requirement | Tests | Applies to | Matrix cell |\n|---|---|---|---|---|\n| delete.empty | Empty deletes nothing | conformance: HardDeleteMany/empty key list deletes nothing | all | |\n"
			},
			wantErr: "conformance subtest HardDeleteMany/missing keys are not an error is claimed by no requirement",
		},
		{
			name: "gap tests count as claimed",
			mutate: func(f map[string]string) {
				edit(f, "docs/conformance/cli.md", "seeding: in-progress", "seeding: complete")
				f["docs/conformance/cli.md"] += "\n| Matrix cell | Reason | Accepted |\n|---|---|---|\n| config-validation: driver × message | §4.13 states no message | |\n" +
					"\n| Gap | Behavior | Tests | Section | Ruling |\n|---|---|---|---|---|\n| gap.config.helperless | Something the PRD does not say | unit: pkg/config: TestRelatedHelperless | §4.13 | |\n"
			},
			wantNoErr: true,
		},
		{
			name: "open gap is a note",
			mutate: func(f map[string]string) {
				f["docs/conformance/cli.md"] += "\n| Gap | Behavior | Tests | Section | Ruling |\n|---|---|---|---|---|\n| gap.config.helperless | Something the PRD does not say | | §4.13 | |\n"
			},
			wantNote: "PRD gap gap.config.helperless is open",
		},
		{
			name: "ruled gap is a note until applied",
			mutate: func(f map[string]string) {
				f["docs/conformance/cli.md"] += "\n| Gap | Behavior | Tests | Section | Ruling |\n|---|---|---|---|---|\n| gap.config.helperless | Something the PRD does not say | | §4.13 | drop-pin 2026-10-23 |\n"
			},
			wantNote: "PRD gap gap.config.helperless was ruled drop-pin on 2026-10-23",
		},
		{
			name: "gap ruling outside the closed set",
			mutate: func(f map[string]string) {
				f["docs/conformance/cli.md"] += "\n| Gap | Behavior | Tests | Section | Ruling |\n|---|---|---|---|---|\n| gap.config.helperless | Something the PRD does not say | | §4.13 | keep |\n"
			},
			wantErr: "ruling \"keep\" is not `amend-prd YYYY-MM-DD` or `drop-pin YYYY-MM-DD`",
		},
		{
			name: "gap id without the gap prefix",
			mutate: func(f map[string]string) {
				f["docs/conformance/cli.md"] += "\n| Gap | Behavior | Tests | Section | Ruling |\n|---|---|---|---|---|\n| config.helperless | Something the PRD does not say | | §4.13 | |\n"
			},
			wantErr: "gap id \"config.helperless\" is not a lower-case slug starting with `gap.`",
		},
		{
			name: "gap with no PRD section",
			mutate: func(f map[string]string) {
				f["docs/conformance/cli.md"] += "\n| Gap | Behavior | Tests | Section | Ruling |\n|---|---|---|---|---|\n| gap.config.helperless | Something the PRD does not say | | config | |\n"
			},
			wantErr: "gap gap.config.helperless names no PRD section",
		},
		{
			name: "gap cites a section the PRD lacks",
			mutate: func(f map[string]string) {
				f["docs/conformance/cli.md"] += "\n| Gap | Behavior | Tests | Section | Ruling |\n|---|---|---|---|---|\n| gap.config.helperless | Something the PRD does not say | | §99 | |\n"
			},
			wantErr: "gap gap.config.helperless cites §99, which is not a PRD section",
		},
		{
			name: "gap naming a missing test",
			mutate: func(f map[string]string) {
				f["docs/conformance/cli.md"] += "\n| Gap | Behavior | Tests | Section | Ruling |\n|---|---|---|---|---|\n| gap.config.helperless | Something the PRD does not say | unit: pkg/config: TestGone | §4.13 | |\n"
			},
			wantErr: "pkg/config has no test function TestGone",
		},
		{
			name: "gap id reused across files",
			mutate: func(f map[string]string) {
				f["docs/conformance/cli.md"] += "\n| Gap | Behavior | Tests | Section | Ruling |\n|---|---|---|---|---|\n| gap.config.dup | Something the PRD does not say | | §4.13 | |\n"
				f["docs/conformance/client.md"] += "\n| Gap | Behavior | Tests | Section | Ruling |\n|---|---|---|---|---|\n| gap.config.dup | Something else | | §9.3 | |\n"
			},
			wantErr: "id gap.config.dup is also used at docs/conformance/cli.md",
		},
		{
			name: "requirement id with the gap prefix",
			mutate: func(f map[string]string) {
				edit(f, "docs/conformance/cli.md", "| config.driver-mismatch |", "| gap.config.driver-mismatch |")
			},
			wantErr: "requirement id \"gap.config.driver-mismatch\" starts with `gap.`",
		},
		{
			name: "gap with no behavior",
			mutate: func(f map[string]string) {
				f["docs/conformance/cli.md"] += "\n| Gap | Behavior | Tests | Section | Ruling |\n|---|---|---|---|---|\n| gap.config.helperless | | | §4.13 | |\n"
			},
			wantErr: "gap gap.config.helperless has no behavior",
		},
		{
			name: "gap with one good and one bad citation",
			mutate: func(f map[string]string) {
				f["docs/conformance/cli.md"] += "\n| Gap | Behavior | Tests | Section | Ruling |\n|---|---|---|---|---|\n| gap.config.helperless | Something the PRD does not say | | §4.13, §99 | |\n"
			},
			wantErr: "gap gap.config.helperless cites §99, which is not a PRD section",
		},
		{
			name: "gap in another file claims a test in a complete file's package",
			mutate: func(f map[string]string) {
				edit(f, "docs/conformance/cli.md", "seeding: in-progress", "seeding: complete")
				f["docs/conformance/cli.md"] += "\n| Matrix cell | Reason | Accepted |\n|---|---|---|\n| config-validation: driver × message | §4.13 states no message | |\n"
				f["docs/conformance/client.md"] += "\n| Gap | Behavior | Tests | Section | Ruling |\n|---|---|---|---|---|\n| gap.client.helperless | Something the PRD does not say | unit: pkg/config: TestRelatedHelperless | §9.3 | |\n"
			},
			wantNoErr: true,
		},
		{
			name: "conformance subtest claimed by a gap",
			mutate: func(f map[string]string) {
				f["cmd/sqlgen/testdata/conformance/suite/suite.go"] = checkSuite
				f["docs/conformance/client.md"] = "# Client\n\n<!-- seeding: in-progress -->\n\n## §9.3\n<!-- prd: 9.3 sha256: " + sectionHash(t, "9.3") + " -->\n\n| ID | Requirement | Tests | Applies to | Matrix cell |\n|---|---|---|---|---|\n| delete.empty | Empty deletes nothing | conformance: HardDeleteMany/empty key list deletes nothing | all | |\n" +
					"\n| Gap | Behavior | Tests | Section | Ruling |\n|---|---|---|---|---|\n| gap.delete.missing | Missing keys are not an error | conformance: HardDeleteMany/missing keys are not an error | §9.3 | |\n"
			},
			wantNoErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := maps.Clone(baseRepo(t))
			files["pkg/config/extra_test.go"] = "package config\n\nimport \"testing\"\n\nfunc TestRelatedHelperless(t *testing.T) {}\n"
			tt.mutate(files)
			res, err := reqcheck.Check(writeRepo(t, files))
			if err != nil {
				t.Fatalf("Check() unexpected error: %v", err)
			}
			errs := res.Errors()
			switch {
			case tt.wantNoErr:
				if len(errs) != 0 {
					t.Errorf("Check() errors = %v, want none", errs)
				}
			case tt.wantErr != "":
				if !anyContains(errs, tt.wantErr) {
					t.Errorf("Check() errors = %v, want one containing %q", errs, tt.wantErr)
				}
			}
			if tt.wantNote != "" {
				if len(errs) != 0 {
					t.Errorf("Check() errors = %v, want none", errs)
				}
				if !anyContains(res.Notes(), tt.wantNote) {
					t.Errorf("Check() notes = %v, want one containing %q", res.Notes(), tt.wantNote)
				}
			}
		})
	}
}

func TestRehash(t *testing.T) {
	files := baseRepo(t)
	edit(files, "docs/PRD.md", "pgx with MySQL is a validation error.", "pgx with MySQL or SQLite is a validation error.")

	tests := []struct {
		name        string
		sections    []string
		wantErr     string
		wantChanged int
		wantPass    bool
	}{
		{name: "a section with no anchor is an error", sections: []string{"9.3"}, wantErr: "§9.3 names no registry anchor"},
		{name: "the changed section is acknowledged", sections: []string{"4.13"}, wantChanged: 1, wantPass: true},
		{name: "every anchor", wantChanged: 1, wantPass: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := writeRepo(t, files)
			changed, err := reqcheck.Rehash(root, tt.sections)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("Rehash(%v) error = %v, want one containing %q", tt.sections, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Rehash(%v) unexpected error: %v", tt.sections, err)
			}
			if len(changed) != tt.wantChanged {
				t.Errorf("Rehash(%v) changed %v, want %d anchor(s)", tt.sections, changed, tt.wantChanged)
			}
			res, err := reqcheck.Check(root)
			if err != nil {
				t.Fatalf("Check() unexpected error: %v", err)
			}
			if pass := len(res.Errors()) == 0; pass != tt.wantPass {
				t.Errorf("Check() after Rehash(%v) errors = %v, want pass = %v", tt.sections, res.Errors(), tt.wantPass)
			}
		})
	}
}

func sectionHash(t *testing.T, key string) string {
	t.Helper()
	prd, err := reqcheck.ParsePRD(checkPRD)
	if err != nil {
		t.Fatalf("ParsePRD() unexpected error: %v", err)
	}
	s, ok := prd.Section(key)
	if !ok {
		t.Fatalf("Section(%q) not found", key)
	}
	return s.Hash
}

func regexpReplaceHash(text, hash string) string {
	start := strings.Index(text, "sha256: ")
	end := strings.Index(text[start:], " -->")
	return text[:start] + "sha256: " + hash + text[start+end:]
}
