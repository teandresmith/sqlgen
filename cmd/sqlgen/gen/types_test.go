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

func loadTypesTemplate(t *testing.T) *template.Template {
	t.Helper()
	tmplPath := filepath.Join("templates", "types.go.tmpl")
	tmpl, err := template.New("types.go.tmpl").Funcs(gen.FuncMap(sql.NewPostgresDialect())).ParseFiles(tmplPath)
	if err != nil {
		t.Fatalf("parsing template: %v", err)
	}
	return tmpl
}

func executeTypesTemplate(t *testing.T, ctx gen.TypeFileContext) string {
	t.Helper()
	tmpl := loadTypesTemplate(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "types", ctx); err != nil {
		t.Fatalf("executing template: %v", err)
	}
	return string(gen.WrapWithPreamble(ctx.Package, []string{"database/sql/driver", "encoding/json", "fmt"}, []byte(buf.String())))
}

func testTypeFileContext() gen.TypeFileContext {
	return gen.TypeFileContext{
		Package: "db",
		Composites: []gen.CompositeTypeContext{
			{
				Name:       "address",
				Schema:     "public",
				GoTypeName: "Address",
				Fields: []gen.CompositeFieldContext{
					{FieldName: "Street", GoType: "string", JSONTag: "street"},
					{FieldName: "City", GoType: "string", JSONTag: "city"},
					{FieldName: "ZipCode", GoType: "string", JSONTag: "zip_code"},
				},
			},
		},
		Domains: []gen.DomainTypeContext{
			{
				Name:       "email",
				Schema:     "public",
				GoTypeName: "Email",
				BaseGoType: "string",
			},
		},
		Extras: []gen.ExtraTypeContext{
			{
				GoTypeName:  "MemeMetadata",
				Description: "Metadata for meme entities",
				Fields: []gen.ExtraFieldContext{
					{
						FieldName: "IsTopTen",
						GoType:    "bool",
						Tags:      []gen.TagPair{{Key: "json", Value: "is_top_ten"}},
					},
					{
						FieldName: "ViewCount",
						GoType:    "int",
						Tags:      []gen.TagPair{{Key: "json", Value: "view_count"}},
					},
				},
			},
		},
	}
}

func TestTypesTemplate_goldenFile(t *testing.T) {
	ctx := testTypeFileContext()
	output := executeTypesTemplate(t, ctx)

	formatted, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "types_gen.go")
	if err != nil {
		t.Fatalf("formatting: %v", err)
	}

	got := string(formatted)
	goldenPath := filepath.Join("testdata", "golden", "types_gen.go")

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
		t.Errorf("types_gen.go mismatch (-want +got):\n%s", diff)
	}
}

func TestTypesTemplate_compilesCleanly(t *testing.T) {
	ctx := testTypeFileContext()
	output := executeTypesTemplate(t, ctx)

	_, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "types_gen.go")
	if err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

func TestTypesTemplate_compositeStruct(t *testing.T) {
	ctx := gen.TypeFileContext{
		Package: "db",
		Composites: []gen.CompositeTypeContext{
			{
				Name:       "address",
				GoTypeName: "Address",
				Fields: []gen.CompositeFieldContext{
					{FieldName: "Street", GoType: "string", JSONTag: "street"},
					{FieldName: "City", GoType: "string", JSONTag: "city"},
				},
			},
		},
	}
	output := executeTypesTemplate(t, ctx)

	wantPatterns := []string{
		"type Address struct",
		`Street string ` + "`" + `json:"street"` + "`",
		`City string ` + "`" + `json:"city"` + "`",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q", want)
		}
	}
}

func TestTypesTemplate_compositeValueMarshalsJSON(t *testing.T) {
	ctx := gen.TypeFileContext{
		Package: "db",
		Composites: []gen.CompositeTypeContext{
			{
				Name:       "address",
				GoTypeName: "Address",
				Fields: []gen.CompositeFieldContext{
					{FieldName: "Street", GoType: "string", JSONTag: "street"},
				},
			},
		},
	}
	output := executeTypesTemplate(t, ctx)

	wantPatterns := []string{
		"func (v Address) Value() (driver.Value, error)",
		"return json.Marshal(v)",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q", want)
		}
	}
}

func TestTypesTemplate_compositeScanUnmarshalsJSON(t *testing.T) {
	ctx := gen.TypeFileContext{
		Package: "db",
		Composites: []gen.CompositeTypeContext{
			{
				Name:       "address",
				GoTypeName: "Address",
				Fields: []gen.CompositeFieldContext{
					{FieldName: "Street", GoType: "string", JSONTag: "street"},
				},
			},
		},
	}
	output := executeTypesTemplate(t, ctx)

	wantPatterns := []string{
		"func (v *Address) Scan(src any) error",
		"case []byte:",
		"return json.Unmarshal(data, v)",
		"case string:",
		"return json.Unmarshal([]byte(data), v)",
		`return fmt.Errorf("scanning %T into Address", src)`,
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q", want)
		}
	}
}

func TestTypesTemplate_domainTypeAlias(t *testing.T) {
	ctx := gen.TypeFileContext{
		Package: "db",
		Domains: []gen.DomainTypeContext{
			{
				Name:       "email",
				GoTypeName: "Email",
				BaseGoType: "string",
			},
		},
	}
	output := executeTypesTemplate(t, ctx)

	if !strings.Contains(output, "type Email = string") {
		t.Error("output missing domain type alias")
	}

	// Domain types must NOT have Value()/Scan() methods.
	if strings.Contains(output, "func (v Email) Value()") {
		t.Error("domain type should not have Value() method")
	}
	if strings.Contains(output, "func (v *Email) Scan(") {
		t.Error("domain type should not have Scan() method")
	}
}

func TestTypesTemplate_extraTypeStruct(t *testing.T) {
	ctx := gen.TypeFileContext{
		Package: "db",
		Extras: []gen.ExtraTypeContext{
			{
				GoTypeName:  "MemeMetadata",
				Description: "Metadata for meme entities",
				Fields: []gen.ExtraFieldContext{
					{
						FieldName: "ViewCount",
						GoType:    "int",
						Tags:      []gen.TagPair{{Key: "json", Value: "view_count"}},
					},
				},
			},
		},
	}
	output := executeTypesTemplate(t, ctx)

	wantPatterns := []string{
		"type MemeMetadata struct",
		`ViewCount int`,
		`json:"view_count"`,
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q", want)
		}
	}
}

func TestTypesTemplate_extraTypeValueMarshalsJSON(t *testing.T) {
	ctx := gen.TypeFileContext{
		Package: "db",
		Extras: []gen.ExtraTypeContext{
			{
				GoTypeName: "MemeMetadata",
				Fields: []gen.ExtraFieldContext{
					{FieldName: "ViewCount", GoType: "int"},
				},
			},
		},
	}
	output := executeTypesTemplate(t, ctx)

	wantPatterns := []string{
		"func (v MemeMetadata) Value() (driver.Value, error)",
		"return json.Marshal(v)",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q", want)
		}
	}
}

func TestTypesTemplate_extraTypeScanUnmarshalsJSON(t *testing.T) {
	ctx := gen.TypeFileContext{
		Package: "db",
		Extras: []gen.ExtraTypeContext{
			{
				GoTypeName: "MemeMetadata",
				Fields: []gen.ExtraFieldContext{
					{FieldName: "ViewCount", GoType: "int"},
				},
			},
		},
	}
	output := executeTypesTemplate(t, ctx)

	wantPatterns := []string{
		"func (v *MemeMetadata) Scan(src any) error",
		"case []byte:",
		"return json.Unmarshal(data, v)",
		"case string:",
		"return json.Unmarshal([]byte(data), v)",
		`return fmt.Errorf("scanning %T into MemeMetadata", src)`,
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q", want)
		}
	}
}
