package gen_test

import (
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/sql"
)

func executeSetTemplate(t *testing.T, ctx gen.SetFileContext) string {
	t.Helper()
	tmplPath := filepath.Join("templates", "set.go.tmpl")
	tmpl, err := template.New("set.go.tmpl").Funcs(gen.FuncMap(sql.NewMySQLDialect())).ParseFiles(tmplPath)
	if err != nil {
		t.Fatalf("parsing template: %v", err)
	}
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "set", ctx); err != nil {
		t.Fatalf("executing template: %v", err)
	}
	return string(gen.WrapWithPreamble(ctx.Package, ctx.Imports, []byte(buf.String())))
}

func testSetFileContext() gen.SetFileContext {
	return gen.SetFileContext{
		Package: "db",
		Imports: []string{"database/sql/driver", "fmt", "io", "strings"},
		Sets: []gen.SetContext{{
			Name:            "users_permissions_set",
			GoTypeName:      "UsersPermissionsSet",
			ValueGoTypeName: "UsersPermissionsSetValue",
			// `multi_word_value` pins that SET identifiers route through the
			// same gqlEnumIdent helper enums use rather than a second spelling.
			Values: []string{"read", "multi_word_value"},
		}},
	}
}

func TestSetTemplate_compilesCleanly(t *testing.T) {
	output := executeSetTemplate(t, testSetFileContext())
	if _, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "sets_gen.go"); err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

// TestSetTemplate_gqlMarshalers is the SET template pin.
//
// A SET column projects onto a GraphQL enum list (PRD §26.4), which needs
// MarshalGQL / UnmarshalGQL on BOTH emitted types, for different reasons:
//
//   - The VALUE type is what each list element binds to, so it carries the
//     wire identifier round-trip exactly as a schema enum does.
//   - The NAMED SLICE is the column's own Go type, and gqlgen's generated list
//     marshaler for a named-slice binding is literally `return v` typed as
//     graphql.Marshaler. Without MarshalGQL on the slice the emitted server
//     does not compile — this is not a symmetry nicety.
//
// Before the fix neither existed, which is why a SET column had no sound
// GraphQL binding at all.
func TestSetTemplate_gqlMarshalers(t *testing.T) {
	output := executeSetTemplate(t, testSetFileContext())

	want := []string{
		// Value type — the element binding.
		"func (s UsersPermissionsSetValue) MarshalGQL(w io.Writer) {",
		"func (s *UsersPermissionsSetValue) UnmarshalGQL(v any) error {",
		// Named slice — the column's Go type; required for the list marshaler.
		"func (s UsersPermissionsSet) MarshalGQL(w io.Writer) {",
		"func (s *UsersPermissionsSet) UnmarshalGQL(v any) error {",
		// SCREAMING_SNAKE_CASE wire identifiers, via gqlEnumIdent.
		`name = "READ"`,
		`name = "MULTI_WORD_VALUE"`,
		`case "MULTI_WORD_VALUE":`,
		// The slice delegates per element so both directions round-trip
		// through one spelling of the identifier.
		"v.MarshalGQL(w)",
		"if err := elem.UnmarshalGQL(item); err != nil {",
	}
	for _, w := range want {
		if !strings.Contains(output, w) {
			t.Errorf("generated sets file missing %q", w)
		}
	}
}

// TestSetTemplate_databaseRoundTripUnchanged guards the half of the SET
// surface the GraphQL projection must not disturb: the comma-separated form
// is what MySQL stores, and MarshalGQL is an additional face on the type, not
// a replacement for String / Value / Scan.
func TestSetTemplate_databaseRoundTripUnchanged(t *testing.T) {
	output := executeSetTemplate(t, testSetFileContext())

	want := []string{
		"func (s UsersPermissionsSet) String() string {",
		"func (s UsersPermissionsSet) Value() (driver.Value, error) {",
		"func (s *UsersPermissionsSet) Scan(src any) error {",
		`strings.Join(parts, ",")`,
		`parts := strings.Split(str, ",")`,
		// The Go constant keeps the raw SQL literal; only the wire identifier
		// is SCREAMING-cased.
		`UsersPermissionsSetValueMultiWordValue UsersPermissionsSetValue = "multi_word_value"`,
	}
	for _, w := range want {
		if !strings.Contains(output, w) {
			t.Errorf("generated sets file missing %q", w)
		}
	}
}
