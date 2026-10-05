package gen_test

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
)

var update = flag.Bool("update", false, "update golden files")

func TestSharedTypesTemplate_goldenFile(t *testing.T) {
	// Load the template.
	tmplPath := filepath.Join("templates", "shared_types.go.tmpl")
	tmpl, err := template.New("shared_types.go.tmpl").ParseFiles(tmplPath)
	if err != nil {
		t.Fatalf("parsing template: %v", err)
	}

	// Build the context.
	ctx := gen.BuildSharedTypesContext("db", false, "", "", false, false, false)

	// Execute the template.
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "shared-types", ctx); err != nil {
		t.Fatalf("executing template: %v", err)
	}

	output := string(gen.WrapWithPreamble(ctx.Package, nil, []byte(buf.String())))

	// Format through the pipeline.
	formatted, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "shared_types_gen.go")
	if err != nil {
		t.Fatalf("formatting: %v", err)
	}

	got := string(formatted)

	goldenPath := filepath.Join("testdata", "golden", "shared_types_gen.go")

	if *update {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o750); err != nil { //nolint:gosec // test helper, golden file dir
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
		t.Errorf("shared_types_gen.go mismatch (-want +got):\n%s", diff)
	}
}

func TestSharedTypesTemplate_compilesCleanly(t *testing.T) {
	// Load the template.
	tmplPath := filepath.Join("templates", "shared_types.go.tmpl")
	tmpl, err := template.New("shared_types.go.tmpl").ParseFiles(tmplPath)
	if err != nil {
		t.Fatalf("parsing template: %v", err)
	}

	// Build the context.
	ctx := gen.BuildSharedTypesContext("db", false, "", "", false, false, false)

	// Execute the template.
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "shared-types", ctx); err != nil {
		t.Fatalf("executing template: %v", err)
	}

	// Wrap with package preamble and format through the pipeline — goimports validates syntax.
	wrapped := gen.WrapWithPreamble(ctx.Package, ctx.Imports, []byte(buf.String()))
	_, err = gen.FormatOnly(wrapped, "v0.0.0-test", "shared_types_gen.go")
	if err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

func TestSharedTypesTemplate_tenancyEnabled_emitsSkipTenancy(t *testing.T) {
	tmplPath := filepath.Join("templates", "shared_types.go.tmpl")
	tmpl, err := template.New("shared_types.go.tmpl").ParseFiles(tmplPath)
	if err != nil {
		t.Fatalf("parsing template: %v", err)
	}

	ctx := gen.BuildSharedTypesContext("db", true, "uuid.UUID", "github.com/google/uuid", false, false, false)

	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "shared-types", ctx); err != nil {
		t.Fatalf("executing template: %v", err)
	}

	wrapped := gen.WrapWithPreamble(ctx.Package, ctx.Imports, []byte(buf.String()))
	formatted, err := gen.FormatOnly(wrapped, "v0.0.0-test", "shared_types_gen.go")
	if err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
	output := string(formatted)

	if !strings.Contains(output, "SkipTenancy") {
		t.Errorf("tenancy-enabled output missing SkipTenancy identifier, got:\n%s", output)
	}
	if !strings.Contains(output, `json:"skip_tenancy"`) {
		t.Errorf("tenancy-enabled output missing skip_tenancy JSON tag, got:\n%s", output)
	}
}

func TestSharedTypesTemplate_tenancyDisabled_omitsSkipTenancy(t *testing.T) {
	tmplPath := filepath.Join("templates", "shared_types.go.tmpl")
	tmpl, err := template.New("shared_types.go.tmpl").ParseFiles(tmplPath)
	if err != nil {
		t.Fatalf("parsing template: %v", err)
	}

	ctx := gen.BuildSharedTypesContext("db", false, "", "", false, false, false)

	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "shared-types", ctx); err != nil {
		t.Fatalf("executing template: %v", err)
	}

	wrapped := gen.WrapWithPreamble(ctx.Package, ctx.Imports, []byte(buf.String()))
	formatted, err := gen.FormatOnly(wrapped, "v0.0.0-test", "shared_types_gen.go")
	if err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
	output := string(formatted)

	if strings.Contains(output, "SkipTenancy") {
		t.Errorf("tenancy-disabled output must not mention SkipTenancy, got:\n%s", output)
	}
	if strings.Contains(output, "skip_tenancy") {
		t.Errorf("tenancy-disabled output must not mention skip_tenancy JSON tag, got:\n%s", output)
	}
}

func TestSharedTypesTemplate_containsAllTypes(t *testing.T) {
	// Load the template.
	tmplPath := filepath.Join("templates", "shared_types.go.tmpl")
	tmpl, err := template.New("shared_types.go.tmpl").ParseFiles(tmplPath)
	if err != nil {
		t.Fatalf("parsing template: %v", err)
	}

	ctx := gen.BuildSharedTypesContext("db", false, "", "", false, false, false)

	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "shared-types", ctx); err != nil {
		t.Fatalf("executing template: %v", err)
	}

	output := buf.String()

	wantTypes := []string{
		"type CallOptions[FO any] struct",
		"type IncrementInput[C ~string] struct",
	}

	for _, want := range wantTypes {
		if !strings.Contains(output, want) {
			t.Errorf("output missing type declaration: %q", want)
		}
	}

	wantHelpers := []string{
		"func resolveCallOptions[FO any]",
		"func toAnySlice[T any]",
	}

	for _, want := range wantHelpers {
		if !strings.Contains(output, want) {
			t.Errorf("output missing helper function: %q", want)
		}
	}
}
