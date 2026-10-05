package gen_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
)

func loadPaginationTemplate(t *testing.T) *template.Template {
	t.Helper()
	tmplPath := filepath.Join("templates", "pagination.go.tmpl")
	tmpl, err := template.New("pagination.go.tmpl").ParseFiles(tmplPath)
	if err != nil {
		t.Fatalf("parsing pagination template: %v", err)
	}
	return tmpl
}

func executePaginationTemplate(t *testing.T, ctx gen.PaginationContext) string {
	t.Helper()
	tmpl := loadPaginationTemplate(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "pagination", ctx); err != nil {
		t.Fatalf("executing template: %v", err)
	}
	return string(gen.WrapWithPreamble(ctx.Package, nil, []byte(buf.String())))
}

// --- Test: PaginateInput generic type ---

func TestPaginationTemplate_paginateInputType(t *testing.T) {
	ctx := gen.PaginationContext{Package: "db", Imports: []string{"github.com/teandresmith/sqlgen/sql"}}
	output := executePaginationTemplate(t, ctx)

	wantPatterns := []string{
		"type PaginateInput[F any] struct",
		`Filter *F         ` + "`" + `json:"filter"` + "`",
		`Sort   []sql.Sort ` + "`" + `json:"sort"` + "`",
		`Limit  int        ` + "`" + `json:"limit"` + "`",
		`Offset int        ` + "`" + `json:"offset"` + "`",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// --- Test: PaginateResult generic type ---

func TestPaginationTemplate_paginateResultType(t *testing.T) {
	ctx := gen.PaginationContext{Package: "db", Imports: []string{"github.com/teandresmith/sqlgen/sql"}}
	output := executePaginationTemplate(t, ctx)

	wantPatterns := []string{
		"type PaginateResult[T any] struct",
		"Items      []*T",
		"TotalCount int64",
		"Offset     int",
		"Limit      int",
		"HasMore    bool",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// --- Test: All 5 fields present ---

func TestPaginationTemplate_resultFields(t *testing.T) {
	ctx := gen.PaginationContext{Package: "db", Imports: []string{"github.com/teandresmith/sqlgen/sql"}}
	output := executePaginationTemplate(t, ctx)

	wantFields := []string{"Items", "TotalCount", "Offset", "Limit", "HasMore"}
	for _, field := range wantFields {
		if !strings.Contains(output, field) {
			t.Errorf("PaginateResult missing field %q", field)
		}
	}

	// Dropped fields must not appear.
	droppedFields := []string{"TotalPages", "PageSize", "CurrentPage", "HasNextPage", "HasPreviousPage"}
	for _, field := range droppedFields {
		if strings.Contains(output, field) {
			t.Errorf("PaginateResult should not contain dropped field %q", field)
		}
	}
}

// --- Test: Compiles cleanly ---

func TestPaginationTemplate_compilesCleanly(t *testing.T) {
	ctx := gen.PaginationContext{Package: "db", Imports: []string{"github.com/teandresmith/sqlgen/sql"}}
	output := executePaginationTemplate(t, ctx)

	_, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "pagination_gen.go")
	if err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

// --- Test: Golden file ---

func TestPaginationTemplate_goldenFile(t *testing.T) {
	ctx := gen.PaginationContext{Package: "db", Imports: []string{"github.com/teandresmith/sqlgen/sql"}}
	output := executePaginationTemplate(t, ctx)

	formatted, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "pagination_gen.go")
	if err != nil {
		t.Fatalf("formatting: %v", err)
	}

	got := string(formatted)
	goldenPath := filepath.Join("testdata", "golden", "pagination_gen.go")

	if *update {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o750); err != nil { //nolint:gosec // test helper
			t.Fatalf("creating golden dir: %v", err)
		}
		if err := os.WriteFile(goldenPath, formatted, 0o600); err != nil {
			t.Fatalf("writing golden file: %v", err)
		}
		t.Log("golden file updated")
		return
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // golden file path is not user-controlled
	if err != nil {
		t.Fatalf("reading golden file (run with -update to create): %v", err)
	}

	if diff := cmp.Diff(string(want), got); diff != "" {
		t.Errorf("pagination_gen.go mismatch (-want +got):\n%s", diff)
	}
}
