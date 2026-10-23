package reqcheck_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/internal/reqcheck"
)

func TestParseRegistry_Tables(t *testing.T) {
	text := `# CLI

<!-- seeding: in-progress -->
<!-- packages: cmd/sqlgen/config cmd/sqlgen/gen -->
<!-- matrix: config-validation rows=rule cols=fires,message -->

## §4.13 Config Validation Rules
<!-- prd: 4.13 sha256: 0123456789abcdef -->

| ID | Requirement | Tests | Applies to | Matrix cell |
|---|---|---|---|---|
| config.driver-mismatch | pgx with MySQL is an error | ` + "`unit: cmd/sqlgen/config: TestA`; `unit: cmd/sqlgen/config: TestB/case one`" + ` | all | config-validation: driver × fires |
| config.untested | A pipe \| in text | | postgres, mysql | |

| Matrix cell | Reason | Accepted |
|---|---|---|
| config-validation: driver × message | §4.13 states no message | |

| Unclaimed test | Reason |
|---|---|
| cmd/sqlgen/gen: TestHelper | Helper |
`
	r, findings := reqcheck.ParseRegistry(text, "docs/conformance/cli.md", "cli.md")
	if len(findings) != 0 {
		t.Fatalf("ParseRegistry() findings = %v, want none", findings)
	}
	if r.Seeding != reqcheck.SeedingInProgress {
		t.Errorf("ParseRegistry().Seeding = %q, want %q", r.Seeding, reqcheck.SeedingInProgress)
	}
	if diff := cmp.Diff([]string{"cmd/sqlgen/config", "cmd/sqlgen/gen"}, r.Packages); diff != "" {
		t.Errorf("ParseRegistry().Packages mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"fires", "message"}, r.Matrices["config-validation"].Cols); diff != "" {
		t.Errorf("ParseRegistry() matrix cols mismatch (-want +got):\n%s", diff)
	}
	wantRows := []reqcheck.Row{
		{
			ID:          "config.driver-mismatch",
			Requirement: "pgx with MySQL is an error",
			Tests:       []string{"unit: cmd/sqlgen/config: TestA", "unit: cmd/sqlgen/config: TestB/case one"},
			AppliesTo:   []string{"all"},
			Cell:        "config-validation: driver × fires",
			Anchor:      "4.13",
			Line:        12,
		},
		{
			ID:          "config.untested",
			Requirement: "A pipe | in text",
			AppliesTo:   []string{"postgres", "mysql"},
			Anchor:      "4.13",
			Line:        13,
		},
	}
	if diff := cmp.Diff(wantRows, r.Rows); diff != "" {
		t.Errorf("ParseRegistry().Rows mismatch (-want +got):\n%s", diff)
	}
	if len(r.NA) != 1 || r.NA[0].Cell != "config-validation: driver × message" {
		t.Errorf("ParseRegistry().NA = %+v, want one n/a cell", r.NA)
	}
	if len(r.Unclaimed) != 1 || r.Unclaimed[0].Test != "cmd/sqlgen/gen: TestHelper" {
		t.Errorf("ParseRegistry().Unclaimed = %+v, want one entry", r.Unclaimed)
	}
}

func TestParseRegistry_FormatErrors(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{
			name: "missing seeding declaration",
			text: "# X\n",
			want: "no `<!-- seeding: … -->` declaration",
		},
		{
			name: "unknown seeding state",
			text: "<!-- seeding: done -->\n",
			want: `seeding "done" is not one of`,
		},
		{
			name: "requirement outside an anchor",
			text: "<!-- seeding: in-progress -->\n\n| ID | Requirement | Tests | Applies to | Matrix cell |\n|---|---|---|---|---|\n| a.b | x | | all | |\n",
			want: "is not under a `<!-- prd: … -->` anchor",
		},
		{
			name: "unrecognized table",
			text: "<!-- seeding: in-progress -->\n\n| Rule | Note |\n|---|---|\n",
			want: `unrecognized table "Rule|Note"`,
		},
		{
			name: "row with the wrong cell count",
			text: "<!-- seeding: in-progress -->\n\n| Unclaimed test | Reason |\n|---|---|\n| only one |\n",
			want: "row has 1 cells, the table header has 2",
		},
		{
			name: "malformed matrix declaration",
			text: "<!-- seeding: in-progress -->\n<!-- matrix: naming cols=a,b -->\n",
			want: "is not `<name> rows=<unit> cols=<a,b,…>`",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, findings := reqcheck.ParseRegistry(tt.text, "f.md", "f.md")
			if !anyContains(findings, tt.want) {
				t.Errorf("ParseRegistry(%q) findings = %v, want one containing %q", tt.text, findings, tt.want)
			}
		})
	}
}

func TestParseTestRef(t *testing.T) {
	tests := []struct {
		in      string
		want    reqcheck.TestRef
		wantErr string
	}{
		{
			in:   "conformance: HardDeleteMany/empty key list deletes nothing",
			want: reqcheck.TestRef{Home: "conformance", Dir: reqcheck.ConformanceSuiteDir, Path: []string{"HardDeleteMany", "empty key list deletes nothing"}},
		},
		{
			in:   "unit: cmd/sqlgen/config/: TestValidate/nullable column",
			want: reqcheck.TestRef{Home: "unit", Dir: "cmd/sqlgen/config", Path: []string{"TestValidate", "nullable column"}},
		},
		{
			in:   "example: postgres: TestCRUD",
			want: reqcheck.TestRef{Home: "example", Dir: reqcheck.ExamplesDir + "/postgres/tests", Path: []string{"TestCRUD"}},
		},
		{
			in:   "golden: sqlite",
			want: reqcheck.TestRef{Home: "golden", Dir: reqcheck.ExamplesDir + "/sqlite"},
		},
		{in: "TestA", wantErr: "has no `<home>:` prefix"},
		{in: "unit: TestA", wantErr: "needs `unit: <dir>: <TestName/…>`"},
		{in: "smoke: x: TestA", wantErr: `home "smoke" is not one of`},
		{in: "golden: a: b", wantErr: "a golden home names one example"},
		{in: "conformance: ", wantErr: "names no test"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := reqcheck.ParseTestRef(tt.in)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("ParseTestRef(%q) error = %v, want one containing %q", tt.in, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseTestRef(%q) unexpected error: %v", tt.in, err)
			}
			tt.want.Raw = tt.in
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ParseTestRef(%q) mismatch (-want +got):\n%s", tt.in, diff)
			}
		})
	}
}

func anyContains(findings []reqcheck.Finding, sub string) bool {
	for _, f := range findings {
		if strings.Contains(f.Msg, sub) {
			return true
		}
	}
	return false
}
