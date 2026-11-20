package reqcheck_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/internal/reqcheck"
)

// TestParseReportRoundTrip reads a rendered report back to the statuses BuildReport derived,
// including a row whose cells all pass or don't apply but whose conformance tests ran nowhere.
func TestParseReportRoundTrip(t *testing.T) {
	rep, err := reqcheck.BuildReport(reportRepo(t), results(t, reportEvents()...))
	if err != nil {
		t.Fatalf("BuildReport() unexpected error: %v", err)
	}
	want := map[string]reqcheck.ReportRow{}
	for _, f := range rep.Files {
		for _, r := range f.Rows {
			want[r.ID] = reqcheck.ReportRow{File: f.Reg.Name, Status: r.Status}
		}
	}
	got, err := reqcheck.ParseReport(rep.Markdown(reqcheck.Header{Commit: "abc123", Date: "2026-10-24"}))
	if err != nil {
		t.Fatalf("ParseReport() unexpected error: %v", err)
	}
	if diff := cmp.Diff(want, got.Rows); diff != "" {
		t.Errorf("ParseReport() rows mismatch (-want +got):\n%s", diff)
	}
	wantIDs := []string{"config.keyed", "config.slash", "config.golden", "delete.empty", "delete.missing", "delete.untested", "delete.mysql"}
	if diff := cmp.Diff(wantIDs, got.IDs); diff != "" {
		t.Errorf("ParseReport() IDs mismatch (-want +got):\n%s", diff)
	}
}

// TestReportDeltaCommittedReport compares the committed report with itself: it parses, its
// totals match its Modules table, and nothing changed.
func TestReportDeltaCommittedReport(t *testing.T) {
	md, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "docs", "conformance", "REPORT.md"))
	if err != nil {
		t.Fatalf("reading REPORT.md: %v", err)
	}
	rows, err := reqcheck.ParseReport(string(md))
	if err != nil {
		t.Fatalf("ParseReport(REPORT.md) unexpected error: %v", err)
	}
	if len(rows.IDs) == 0 {
		t.Fatal("ParseReport(REPORT.md) found no rows")
	}
	d := reqcheck.DiffReports(rows, rows)
	if len(d.Changed) != 0 || len(d.Regressions()) != 0 {
		t.Errorf("DiffReports(REPORT.md, REPORT.md) = %d changed, %d regressions, want none", len(d.Changed), len(d.Regressions()))
	}
	if diff := cmp.Diff(d.Base, d.Head); diff != "" {
		t.Errorf("DiffReports(REPORT.md, REPORT.md) totals differ (-base +head):\n%s", diff)
	}
}

func TestDiffReports(t *testing.T) {
	const (
		pass     = reqcheck.StatusPass
		fail     = reqcheck.StatusFail
		skip     = reqcheck.StatusSkip
		untested = reqcheck.StatusUntested
	)
	report := func(rows ...reqcheck.ReportRow) *reqcheck.ReportRows {
		r := &reqcheck.ReportRows{Rows: map[string]reqcheck.ReportRow{}}
		for i, row := range rows {
			id := []string{"a", "b", "c"}[i]
			if row.File == "" {
				continue // absent from this report
			}
			r.IDs = append(r.IDs, id)
			r.Rows[id] = row
		}
		return r
	}
	row := func(s reqcheck.Status) reqcheck.ReportRow { return reqcheck.ReportRow{File: "client.md", Status: s} }
	absent := reqcheck.ReportRow{}
	tests := []struct {
		name            string
		base, head      *reqcheck.ReportRows
		wantChanged     []reqcheck.RowChange
		wantRegressions int
	}{
		{
			name: "unchanged",
			base: report(row(pass), row(untested)),
			head: report(row(pass), row(untested)),
		},
		{
			name:            "pass to untested",
			base:            report(row(pass), row(pass)),
			head:            report(row(pass), row(untested)),
			wantChanged:     []reqcheck.RowChange{{ID: "b", File: "client.md", From: pass, To: untested, InBase: true, InHead: true}},
			wantRegressions: 1,
		},
		{
			name:            "pass to fail",
			base:            report(row(pass)),
			head:            report(row(fail)),
			wantChanged:     []reqcheck.RowChange{{ID: "a", File: "client.md", From: pass, To: fail, InBase: true, InHead: true}},
			wantRegressions: 1,
		},
		{
			name:            "pass to skip",
			base:            report(row(pass)),
			head:            report(row(skip)),
			wantChanged:     []reqcheck.RowChange{{ID: "a", File: "client.md", From: pass, To: skip, InBase: true, InHead: true}},
			wantRegressions: 1,
		},
		{
			name:            "passing row removed",
			base:            report(row(pass), row(pass)),
			head:            report(row(pass), absent),
			wantChanged:     []reqcheck.RowChange{{ID: "b", File: "client.md", From: pass, InBase: true}},
			wantRegressions: 1,
		},
		{
			name:        "untested to pass",
			base:        report(row(untested)),
			head:        report(row(pass)),
			wantChanged: []reqcheck.RowChange{{ID: "a", File: "client.md", From: untested, To: pass, InBase: true, InHead: true}},
		},
		{
			name:        "row added",
			base:        report(row(pass), absent, absent),
			head:        report(row(pass), absent, row(untested)),
			wantChanged: []reqcheck.RowChange{{ID: "c", File: "client.md", To: untested, InHead: true}},
		},
		{
			name:        "untested row removed",
			base:        report(row(untested)),
			head:        report(absent),
			wantChanged: []reqcheck.RowChange{{ID: "a", File: "client.md", From: untested, InBase: true}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := reqcheck.DiffReports(tt.base, tt.head)
			if diff := cmp.Diff(tt.wantChanged, d.Changed); diff != "" {
				t.Errorf("DiffReports() changed mismatch (-want +got):\n%s", diff)
			}
			if got := len(d.Regressions()); got != tt.wantRegressions {
				t.Errorf("DiffReports().Regressions() = %d rows, want %d", got, tt.wantRegressions)
			}
		})
	}
}

// TestReportDeltaPassToUntested edits a rendered report so one passing row names no test, as a
// citation dropped by mistake would, and checks the delta reports it and the totals move.
func TestReportDeltaPassToUntested(t *testing.T) {
	base, err := reqcheck.ParseReport(wantReport)
	if err != nil {
		t.Fatalf("ParseReport() unexpected error: %v", err)
	}
	edited := strings.NewReplacer(
		"| cli.md | in-progress | 3 | 2 | 1 | 0 | 0 | 0 |", "| cli.md | in-progress | 3 | 1 | 1 | 0 | 0 | 1 |",
		"| §4.13 | config.keyed | pass |", "| §4.13 | config.keyed | untested |",
	).Replace(wantReport)
	head, err := reqcheck.ParseReport(edited)
	if err != nil {
		t.Fatalf("ParseReport(edited) unexpected error: %v", err)
	}
	d := reqcheck.DiffReports(base, head)
	want := []reqcheck.RowChange{{
		ID: "config.keyed", File: "cli.md", From: reqcheck.StatusPass, To: reqcheck.StatusUntested, InBase: true, InHead: true,
	}}
	if diff := cmp.Diff(want, d.Regressions()); diff != "" {
		t.Errorf("DiffReports().Regressions() mismatch (-want +got):\n%s", diff)
	}
	if got, want := d.Head[reqcheck.StatusPass], d.Base[reqcheck.StatusPass]-1; got != want {
		t.Errorf("DiffReports() head pass total = %d, want %d", got, want)
	}
	if got, want := d.Head[reqcheck.StatusUntested], d.Base[reqcheck.StatusUntested]+1; got != want {
		t.Errorf("DiffReports() head untested total = %d, want %d", got, want)
	}
}

func TestParseReportErrors(t *testing.T) {
	tests := []struct {
		name    string
		old     string
		new     string
		wantErr string
	}{
		{
			name:    "totals disagree with rows",
			old:     "| cli.md | in-progress | 3 | 2 | 1 | 0 | 0 | 0 |",
			new:     "| cli.md | in-progress | 3 | 3 | 0 | 0 | 0 | 0 |",
			wantErr: "cli.md",
		},
		{
			name:    "unknown status",
			old:     "| §4.13 | config.slash | pass |",
			new:     "| §4.13 | config.slash | passed |",
			wantErr: `"passed"`,
		},
		{
			name:    "duplicate id",
			old:     "| §4.13 | config.slash | pass |",
			new:     "| §4.13 | config.keyed | pass |",
			wantErr: "config.keyed",
		},
		{
			name:    "no requirements section",
			old:     "## Requirements",
			new:     "## Rows",
			wantErr: "Requirements",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(wantReport, tt.old) {
				t.Fatalf("fixture has no %q", tt.old)
			}
			_, err := reqcheck.ParseReport(strings.Replace(wantReport, tt.old, tt.new, 1))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ParseReport() error = %v, want one naming %s", err, tt.wantErr)
			}
		})
	}
}
