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

func loadEnumTemplate(t *testing.T) *template.Template {
	t.Helper()
	tmplPath := filepath.Join("templates", "enum.go.tmpl")
	tmpl, err := template.New("enum.go.tmpl").Funcs(gen.FuncMap(sql.NewPostgresDialect())).ParseFiles(tmplPath)
	if err != nil {
		t.Fatalf("parsing template: %v", err)
	}
	return tmpl
}

func executeEnumTemplate(t *testing.T, ctx gen.EnumFileContext) string {
	t.Helper()
	tmpl := loadEnumTemplate(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "enum", ctx); err != nil {
		t.Fatalf("executing template: %v", err)
	}
	return string(gen.WrapWithPreamble(ctx.Package, []string{"database/sql/driver", "fmt"}, []byte(buf.String())))
}

func testEnumFileContext() gen.EnumFileContext {
	return gen.EnumFileContext{
		Package: "db",
		Enums: []gen.EnumContext{
			{
				Name:       "order_status",
				Schema:     "public",
				GoTypeName: "OrderStatus",
				Values:     []string{"pending", "shipped", "delivered"},
			},
			{
				Name:       "user_role",
				Schema:     "public",
				GoTypeName: "UserRole",
				Values:     []string{"admin", "member"},
			},
		},
	}
}

func TestEnumTemplate_goldenFile(t *testing.T) {
	ctx := testEnumFileContext()
	output := executeEnumTemplate(t, ctx)

	formatted, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "enums_gen.go")
	if err != nil {
		t.Fatalf("formatting: %v", err)
	}

	got := string(formatted)
	goldenPath := filepath.Join("testdata", "golden", "enums_gen.go")

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
		t.Errorf("enums_gen.go mismatch (-want +got):\n%s", diff)
	}
}

func TestEnumTemplate_compilesCleanly(t *testing.T) {
	ctx := testEnumFileContext()
	output := executeEnumTemplate(t, ctx)

	_, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "enums_gen.go")
	if err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

func TestEnumTemplate_typeAndConstants(t *testing.T) {
	ctx := gen.EnumFileContext{
		Package: "db",
		Enums: []gen.EnumContext{
			{
				Name:       "order_status",
				GoTypeName: "OrderStatus",
				Values:     []string{"pending", "shipped", "delivered"},
			},
		},
	}
	output := executeEnumTemplate(t, ctx)

	wantPatterns := []string{
		"type OrderStatus string",
		`OrderStatusPending OrderStatus = "pending"`,
		`OrderStatusShipped OrderStatus = "shipped"`,
		`OrderStatusDelivered OrderStatus = "delivered"`,
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q", want)
		}
	}
}

func TestEnumTemplate_stringMethod(t *testing.T) {
	ctx := gen.EnumFileContext{
		Package: "db",
		Enums: []gen.EnumContext{
			{
				Name:       "color",
				GoTypeName: "Color",
				Values:     []string{"red", "green", "blue"},
			},
		},
	}
	output := executeEnumTemplate(t, ctx)

	if !strings.Contains(output, "func (s Color) String() string { return string(s) }") {
		t.Error("output missing String() method")
	}
}

func TestEnumTemplate_validateFunction(t *testing.T) {
	ctx := gen.EnumFileContext{
		Package: "db",
		Enums: []gen.EnumContext{
			{
				Name:       "color",
				GoTypeName: "Color",
				Values:     []string{"red", "green", "blue"},
			},
		},
	}
	output := executeEnumTemplate(t, ctx)

	wantPatterns := []string{
		"func ValidateColor(s string) (Color, error)",
		"case ColorRed, ColorGreen, ColorBlue:",
		`return "", fmt.Errorf("invalid Color: %q", s)`,
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q", want)
		}
	}
}

func TestEnumTemplate_valueReturnsString(t *testing.T) {
	ctx := gen.EnumFileContext{
		Package: "db",
		Enums: []gen.EnumContext{
			{
				Name:       "color",
				GoTypeName: "Color",
				Values:     []string{"red"},
			},
		},
	}
	output := executeEnumTemplate(t, ctx)

	wantPatterns := []string{
		"func (s Color) Value() (driver.Value, error)",
		"return string(s), nil",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q", want)
		}
	}
}

func TestEnumTemplate_allVar(t *testing.T) {
	ctx := gen.EnumFileContext{
		Package: "db",
		Enums: []gen.EnumContext{
			{
				Name:       "order_status",
				GoTypeName: "OrderStatus",
				Values:     []string{"pending", "shipped", "delivered"},
			},
		},
	}
	output := executeEnumTemplate(t, ctx)

	wantPatterns := []string{
		"var AllOrderStatus = []OrderStatus{",
		"OrderStatusPending,",
		"OrderStatusShipped,",
		"OrderStatusDelivered,",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q", want)
		}
	}
}

func TestEnumTemplate_marshalText(t *testing.T) {
	ctx := gen.EnumFileContext{
		Package: "db",
		Enums: []gen.EnumContext{
			{
				Name:       "color",
				GoTypeName: "Color",
				Values:     []string{"red"},
			},
		},
	}
	output := executeEnumTemplate(t, ctx)

	wantPatterns := []string{
		"func (s Color) MarshalText() ([]byte, error)",
		"return []byte(s), nil",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q", want)
		}
	}
}

func TestEnumTemplate_unmarshalTextValidates(t *testing.T) {
	ctx := gen.EnumFileContext{
		Package: "db",
		Enums: []gen.EnumContext{
			{
				Name:       "color",
				GoTypeName: "Color",
				Values:     []string{"red"},
			},
		},
	}
	output := executeEnumTemplate(t, ctx)

	wantPatterns := []string{
		"func (s *Color) UnmarshalText(b []byte) error",
		"v, err := ValidateColor(string(b))",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q", want)
		}
	}
}

func TestEnumTemplate_scanHandlesStringAndBytes(t *testing.T) {
	ctx := gen.EnumFileContext{
		Package: "db",
		Enums: []gen.EnumContext{
			{
				Name:       "color",
				GoTypeName: "Color",
				Values:     []string{"red"},
			},
		},
	}
	output := executeEnumTemplate(t, ctx)

	wantPatterns := []string{
		"func (s *Color) Scan(src any) error",
		"case string:",
		"*s = Color(v)",
		"case []byte:",
		`return fmt.Errorf("scanning %T into Color", src)`,
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q", want)
		}
	}
}
