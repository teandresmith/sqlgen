package gen_test

import (
	"strings"
	"testing"
	"text/template"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/sql"
)

// loadSharedTemplates parses all shared fragment templates.
func loadSharedTemplates(t *testing.T) *template.Template {
	t.Helper()
	tmpl, err := template.New("test").Funcs(gen.FuncMap(sql.NewPostgresDialect())).ParseGlob("templates/shared/_*.tmpl")
	if err != nil {
		t.Fatalf("parsing shared templates: %v", err)
	}
	return tmpl
}

// --- _doc.tmpl tests ---

func TestDocTemplate_withDescription(t *testing.T) {
	tmpl := loadSharedTemplates(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "shared/doc", "User account in the system."); err != nil {
		t.Fatalf("executing template: %v", err)
	}

	got := buf.String()
	want := "// User account in the system."
	if !strings.Contains(got, want) {
		t.Errorf("got %q, want to contain %q", got, want)
	}
}

func TestDocTemplate_emptyDescription(t *testing.T) {
	tmpl := loadSharedTemplates(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "shared/doc", ""); err != nil {
		t.Fatalf("executing template: %v", err)
	}

	got := buf.String()
	if strings.Contains(got, "//") {
		t.Errorf("expected no output for empty description, got %q", got)
	}
}

func TestDocTemplate_noCommentGenerated(t *testing.T) {
	// When no SQL comment and no config description, empty string is passed.
	tmpl := loadSharedTemplates(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "shared/doc", ""); err != nil {
		t.Fatalf("executing template: %v", err)
	}

	got := strings.TrimSpace(buf.String())
	if got != "" {
		t.Errorf("expected empty output, got %q", got)
	}
}

// --- _field.tmpl tests ---

func TestFieldTemplate_withDBAndJSONTags(t *testing.T) {
	tmpl := loadSharedTemplates(t)
	col := gen.ColumnContext{
		FieldName: "Name",
		GoType:    "string",
		DBTag:     "name",
		JSONTag:   "name",
	}

	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "shared/field", col); err != nil {
		t.Fatalf("executing template: %v", err)
	}

	got := buf.String()
	wantPatterns := []string{
		"Name string",
		`db:"name"`,
		`json:"name"`,
	}
	for _, want := range wantPatterns {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q in: %q", want, got)
		}
	}
}

func TestFieldTemplate_dbBeforeJSON(t *testing.T) {
	tmpl := loadSharedTemplates(t)
	col := gen.ColumnContext{
		FieldName: "ID",
		GoType:    "uuid.UUID",
		DBTag:     "id",
		JSONTag:   "id",
	}

	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "shared/field", col); err != nil {
		t.Fatalf("executing template: %v", err)
	}

	got := buf.String()
	dbIdx := strings.Index(got, `db:"`)
	jsonIdx := strings.Index(got, `json:"`)
	if dbIdx < 0 || jsonIdx < 0 {
		t.Fatalf("missing tags in output: %q", got)
	}
	if dbIdx >= jsonIdx {
		t.Errorf("db tag should come before json tag, got db at %d, json at %d", dbIdx, jsonIdx)
	}
}

func TestFieldTemplate_withDocComment(t *testing.T) {
	tmpl := loadSharedTemplates(t)
	col := gen.ColumnContext{
		FieldName:   "Price",
		GoType:      "int64",
		DBTag:       "price",
		JSONTag:     "price",
		Description: "Unit price in cents.",
	}

	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "shared/field", col); err != nil {
		t.Fatalf("executing template: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "// Price - Unit price in cents.") {
		t.Errorf("output missing doc comment in: %q", got)
	}
}

func TestFieldTemplate_configDescriptionOverridesSQL(t *testing.T) {
	// Config description is resolved at the context layer; template just
	// renders whatever Description is set. Verify it renders.
	tmpl := loadSharedTemplates(t)
	col := gen.ColumnContext{
		FieldName:   "SKU",
		GoType:      "string",
		DBTag:       "sku",
		JSONTag:     "sku",
		Description: "Config-provided description.",
	}

	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "shared/field", col); err != nil {
		t.Fatalf("executing template: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "// SKU - Config-provided description.") {
		t.Errorf("output missing config description in: %q", got)
	}
}

func TestFieldTemplate_noDocCommentWhenEmpty(t *testing.T) {
	tmpl := loadSharedTemplates(t)
	col := gen.ColumnContext{
		FieldName: "ID",
		GoType:    "int64",
		DBTag:     "id",
		JSONTag:   "id",
	}

	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "shared/field", col); err != nil {
		t.Fatalf("executing template: %v", err)
	}

	got := buf.String()
	if strings.Contains(got, "//") {
		t.Errorf("expected no doc comment for empty description, got %q", got)
	}
}

func TestFieldTemplate_nullableType(t *testing.T) {
	tmpl := loadSharedTemplates(t)
	col := gen.ColumnContext{
		FieldName: "DeletedAt",
		GoType:    "*time.Time",
		DBTag:     "deleted_at",
		JSONTag:   "deleted_at",
		Nullable:  true,
	}

	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "shared/field", col); err != nil {
		t.Fatalf("executing template: %v", err)
	}

	got := buf.String()
	wantPatterns := []string{
		"DeletedAt *time.Time",
		`db:"deleted_at"`,
		`json:"deleted_at"`,
	}
	for _, want := range wantPatterns {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q in: %q", want, got)
		}
	}
}

// --- _imports.tmpl tests ---

func TestImportsTemplate_deduplication(t *testing.T) {
	tmpl := loadSharedTemplates(t)
	imports := []string{
		"time",
		"github.com/google/uuid",
		"time",
		"github.com/google/uuid",
	}

	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "shared/imports", gen.UniqueImports(imports)); err != nil {
		t.Fatalf("executing template: %v", err)
	}

	got := buf.String()
	// Each import should appear exactly once.
	if strings.Count(got, `"time"`) != 1 {
		t.Errorf("expected 'time' once, got %d in: %q", strings.Count(got, `"time"`), got)
	}
	if strings.Count(got, `"github.com/google/uuid"`) != 1 {
		t.Errorf("expected uuid once, got %d in: %q", strings.Count(got, `"github.com/google/uuid"`), got)
	}
}

func TestImportsTemplate_emptySlice(t *testing.T) {
	tmpl := loadSharedTemplates(t)

	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "shared/imports", gen.UniqueImports(nil)); err != nil {
		t.Fatalf("executing template: %v", err)
	}

	got := strings.TrimSpace(buf.String())
	if got != "" {
		t.Errorf("expected no output for empty imports, got %q", got)
	}
}

func TestImportsTemplate_filtersEmptyStrings(t *testing.T) {
	tmpl := loadSharedTemplates(t)
	imports := []string{"", "time", "", "fmt", ""}

	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "shared/imports", gen.UniqueImports(imports)); err != nil {
		t.Fatalf("executing template: %v", err)
	}

	got := buf.String()
	if strings.Count(got, `""`) > 0 {
		t.Errorf("output contains empty import path in: %q", got)
	}
	if !strings.Contains(got, `"fmt"`) {
		t.Errorf("output missing fmt import in: %q", got)
	}
	if !strings.Contains(got, `"time"`) {
		t.Errorf("output missing time import in: %q", got)
	}
}

func TestImportsTemplate_sorted(t *testing.T) {
	tmpl := loadSharedTemplates(t)
	imports := []string{"time", "fmt", "context"}

	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "shared/imports", gen.UniqueImports(imports)); err != nil {
		t.Fatalf("executing template: %v", err)
	}

	got := buf.String()
	ctxIdx := strings.Index(got, `"context"`)
	fmtIdx := strings.Index(got, `"fmt"`)
	timeIdx := strings.Index(got, `"time"`)
	if ctxIdx < 0 || fmtIdx < 0 || timeIdx < 0 {
		t.Fatalf("missing imports in output: %q", got)
	}
	if ctxIdx >= fmtIdx || fmtIdx >= timeIdx {
		t.Errorf("imports not sorted: context=%d, fmt=%d, time=%d", ctxIdx, fmtIdx, timeIdx)
	}
}
