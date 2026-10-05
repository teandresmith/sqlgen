package gen_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/sql"
)

func loadErrorTemplate(t *testing.T) *template.Template {
	t.Helper()
	tmplPath := filepath.Join("templates", "error.go.tmpl")
	tmpl, err := template.New("error.go.tmpl").Funcs(gen.FuncMap(sql.NewPostgresDialect())).ParseFiles(tmplPath)
	if err != nil {
		t.Fatalf("parsing template: %v", err)
	}
	return tmpl
}

func executeErrorTemplate(t *testing.T, ctx gen.ErrorFileContext) string {
	t.Helper()
	tmpl := loadErrorTemplate(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "error", ctx); err != nil {
		t.Fatalf("executing template: %v", err)
	}
	return string(gen.WrapWithPreamble(ctx.Package, []string{"github.com/teandresmith/sqlgen/database"}, []byte(buf.String())))
}

func testErrorFileContext() gen.ErrorFileContext {
	return gen.ErrorFileContext{
		Package: "db",
	}
}

func TestErrorTemplate_goldenFile(t *testing.T) {
	ctx := testErrorFileContext()
	output := executeErrorTemplate(t, ctx)

	formatted, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "errors_gen.go")
	if err != nil {
		t.Fatalf("formatting: %v", err)
	}

	got := string(formatted)
	goldenPath := filepath.Join("testdata", "golden", "errors_gen.go")

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
		t.Errorf("errors_gen.go mismatch (-want +got):\n%s", diff)
	}
}

func TestErrorTemplate_compilesCleanly(t *testing.T) {
	ctx := testErrorFileContext()
	output := executeErrorTemplate(t, ctx)

	_, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "errors_gen.go")
	if err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

func TestErrorTemplate_sentinelErrors(t *testing.T) {
	ctx := testErrorFileContext()
	output := executeErrorTemplate(t, ctx)

	wantPatterns := []string{
		"ErrNotFound = database.ErrNotFound",
		"ErrEmptyFilter = database.ErrEmptyFilter",
		"ErrNilInput = database.ErrNilInput",
		"ErrAmbiguousFilter = database.ErrAmbiguousFilter",
		"ErrInvalidCursor = database.ErrInvalidCursor",
		"ErrDeadlock = database.ErrDeadlock",
		"ErrConnectionFailed = database.ErrConnectionFailed",
		"ErrConstraintViolation = database.ErrConstraintViolation",
		"ErrAlreadyRelated = database.ErrAlreadyRelated",
		"ErrNestedVerbConflict = database.ErrNestedVerbConflict",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing sentinel: %q", want)
		}
	}
}

func TestErrorTemplate_constraintTypes(t *testing.T) {
	ctx := testErrorFileContext()
	output := executeErrorTemplate(t, ctx)

	wantPatterns := []string{
		"type ConstraintError = database.ConstraintError",
		"type ConstraintType = database.ConstraintType",
		"type NestedMutationError = database.NestedMutationError",
		"ConstraintUnique     = database.ConstraintUnique",
		"ConstraintForeignKey = database.ConstraintForeignKey",
		"ConstraintCheck      = database.ConstraintCheck",
		"ConstraintNotNull    = database.ConstraintNotNull",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing constraint type: %q", want)
		}
	}
}
