package cli

import (
	"go/ast"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// TestGenerate_SchemaQualifiedTypeResolves generates a PostgreSQL schema whose
// columns name an enum and a domain schema-qualified, from a table in the
// type's own schema and from a table in another. Every column resolves to the
// generated enum type or the domain's base type (PRD §7.1).
func TestGenerate_SchemaQualifiedTypeResolves(t *testing.T) {
	cfgPath := writeSchemaProject(t, config.DialectPostgres, `
		CREATE TYPE app.status_enum AS ENUM ('active', 'inactive');
		CREATE DOMAIN app.positive_int AS integer;
		CREATE TABLE app.accounts (
			id bigserial PRIMARY KEY,
			status app.status_enum NOT NULL,
			statuses app.status_enum[],
			score app.positive_int NOT NULL
		);
		CREATE TABLE users (
			id bigserial PRIMARY KEY,
			status app.status_enum NOT NULL,
			score app.positive_int NOT NULL
		);
	`)
	if _, stderr, err := executeCommand("generate", "--config", cfgPath, "--quiet"); err != nil {
		t.Fatalf("generate: unexpected error: %v\nstderr: %s", err, stderr)
	}
	files := parseGeneratedFiles(t, filepath.Join(filepath.Dir(cfgPath), "output"))

	for _, c := range []struct {
		typeName string
		want     map[string]string
	}{
		{typeName: "Account", want: map[string]string{"ID": "int64", "Status": "StatusEnum", "Statuses": "StatusEnumSlice", "Score": "int32"}},
		{typeName: "User", want: map[string]string{"ID": "int64", "Status": "StatusEnum", "Score": "int32"}},
	} {
		if diff := cmp.Diff(c.want, structFieldTypes(files, c.typeName)); diff != "" {
			t.Errorf("%s field types mismatch (-want +got):\n%s", c.typeName, diff)
		}
	}
}

// TestGenerate_SharedEnumNameResolvesInOwnSchema generates two PostgreSQL
// schemas that each declare an enum named status. A column written with the
// bare name resolves to the enum in its own table's schema, and a qualified
// one to the schema it names (PRD §7.1).
func TestGenerate_SharedEnumNameResolvesInOwnSchema(t *testing.T) {
	cfgPath := writeSchemaProject(t, config.DialectPostgres, `
		CREATE TYPE app.status AS ENUM ('a');
		CREATE TYPE billing.status AS ENUM ('b');
		CREATE TYPE billing.tags AS (s billing.status, labels billing.status[]);
		CREATE DOMAIN billing.status_d AS billing.status;
		CREATE TABLE app.accounts (
			id bigserial PRIMARY KEY,
			status app.status NOT NULL,
			statuses app.status[],
			other billing.status NOT NULL
		);
		CREATE TABLE billing.invoices (
			id bigserial PRIMARY KEY,
			status billing.status NOT NULL,
			own billing.status NOT NULL,
			via_domain billing.status_d NOT NULL
		);
	`)
	if _, stderr, err := executeCommand("generate", "--config", cfgPath, "--quiet"); err != nil {
		t.Fatalf("generate: unexpected error: %v\nstderr: %s", err, stderr)
	}
	files := parseGeneratedFiles(t, filepath.Join(filepath.Dir(cfgPath), "output"))

	for _, c := range []struct {
		typeName string
		want     map[string]string
	}{
		{typeName: "Account", want: map[string]string{"ID": "int64", "Status": "AppStatus", "Statuses": "AppStatusSlice", "Other": "BillingStatus"}},
		{typeName: "Invoice", want: map[string]string{"ID": "int64", "Status": "BillingStatus", "Own": "BillingStatus", "ViaDomain": "BillingStatus"}},
		{typeName: "Tags", want: map[string]string{"S": "BillingStatus", "Labels": "BillingStatusSlice"}},
	} {
		if diff := cmp.Diff(c.want, structFieldTypes(files, c.typeName)); diff != "" {
			t.Errorf("%s field types mismatch (-want +got):\n%s", c.typeName, diff)
		}
	}
}

// TestGenerate_AnnotationViewTypesFollowViewSchema generates PostgreSQL
// annotation-file views over tables in other schemas, with an enum name two
// schemas share. A view column copies its source column's type, and that type
// resolves to the source column's enum whatever schema the view is in (PRD §7.1).
func TestGenerate_AnnotationViewTypesFollowViewSchema(t *testing.T) {
	cfgPath := writeSchemaProject(t, config.DialectPostgres, `
		CREATE TYPE app.status AS ENUM ('a');
		CREATE TYPE billing.status AS ENUM ('b');
		CREATE TABLE app.accounts (id bigserial PRIMARY KEY, status app.status NOT NULL);
		CREATE TABLE billing.invoices (id bigserial PRIMARY KEY, status billing.status NOT NULL);
	`)
	viewDir := t.TempDir()
	for name, sql := range map[string]string{
		"v_app.sql":      "CREATE VIEW app.v_app AS SELECT a.id, a.status FROM app.accounts a;",
		"v_billing.sql":  "CREATE VIEW billing.v_billing AS SELECT a.id, a.status FROM app.accounts a;",
		"v_public.sql":   "CREATE VIEW v_public AS SELECT accounts.id, accounts.status FROM app.accounts;",
		"v_invoices.sql": "CREATE VIEW app.v_invoices AS SELECT i.id, i.status FROM billing.invoices i;",
	} {
		if err := os.WriteFile(filepath.Join(viewDir, name), []byte(sql), 0o600); err != nil {
			t.Fatalf("writing view file: %v", err)
		}
	}
	cfg, err := os.ReadFile(cfgPath) //nolint:gosec // test code, path is from controlled test dir
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}
	cfg = []byte(strings.Replace(string(cfg), "\noutput:", "\n  views:\n    - \""+filepath.ToSlash(viewDir)+"\"\noutput:", 1))
	if err := os.WriteFile(cfgPath, cfg, 0o600); err != nil { //nolint:gosec // test code, path is from controlled test dir
		t.Fatalf("writing config: %v", err)
	}
	if _, stderr, err := executeCommand("generate", "--config", cfgPath, "--quiet"); err != nil {
		t.Fatalf("generate: unexpected error: %v\nstderr: %s", err, stderr)
	}
	files := parseGeneratedFiles(t, filepath.Join(filepath.Dir(cfgPath), "output"))

	for _, c := range []struct {
		typeName string
		want     map[string]string
	}{
		{typeName: "VApp", want: map[string]string{"ID": "int64", "Status": "AppStatus"}},
		{typeName: "VBilling", want: map[string]string{"ID": "int64", "Status": "AppStatus"}},
		{typeName: "VPublic", want: map[string]string{"ID": "int64", "Status": "AppStatus"}},
		{typeName: "VInvoice", want: map[string]string{"ID": "int64", "Status": "BillingStatus"}},
	} {
		if diff := cmp.Diff(c.want, structFieldTypes(files, c.typeName)); diff != "" {
			t.Errorf("%s field types mismatch (-want +got):\n%s", c.typeName, diff)
		}
	}
}

// TestGenerate_TypmodOverrideKey generates a PostgreSQL column declared
// numeric(12, 2) against an overrides.types key written numeric(12,2), the
// catalog's spelling. The key matches the parsed column (PRD §7.1).
func TestGenerate_TypmodOverrideKey(t *testing.T) {
	cfgPath := writeSchemaProject(t, config.DialectPostgres, `
		CREATE TABLE items (
			id bigserial PRIMARY KEY,
			price numeric(12, 2) NOT NULL,
			rate numeric(10,4) NOT NULL
		);
	`)
	appendConfig(t, cfgPath, `overrides:
  types:
    "numeric(12,2)":
      type: money.Amount
      import: example.com/money
      nullable: money.NullAmount
`)
	if _, stderr, err := executeCommand("generate", "--config", cfgPath, "--quiet"); err != nil {
		t.Fatalf("generate: unexpected error: %v\nstderr: %s", err, stderr)
	}
	files := parseGeneratedFiles(t, filepath.Join(filepath.Dir(cfgPath), "output"))
	want := map[string]string{"ID": "int64", "Price": "money.Amount", "Rate": "float64"}
	if diff := cmp.Diff(want, structFieldTypes(files, "Item")); diff != "" {
		t.Errorf("Item field types mismatch (-want +got):\n%s", diff)
	}
}

// TestGenerate_QualifiedOverrideKey generates a PostgreSQL schema against
// overrides.types keys written schema-qualified. A key reaches the columns of
// its type's own schema, where the type is written bare, as well as columns in
// other schemas, where it is written qualified, and never another schema's
// type of the same name (PRD §7.1).
func TestGenerate_QualifiedOverrideKey(t *testing.T) {
	cfgPath := writeSchemaProject(t, config.DialectPostgres, `
		CREATE TYPE app.status_enum AS ENUM ('active', 'inactive');
		CREATE TYPE billing.status_enum AS ENUM ('open', 'paid');
		CREATE DOMAIN app.positive_int AS integer;
		CREATE TABLE app.accounts (
			id bigserial PRIMARY KEY,
			status app.status_enum NOT NULL,
			score app.positive_int NOT NULL
		);
		CREATE TABLE users (
			id bigserial PRIMARY KEY,
			status app.status_enum NOT NULL,
			score app.positive_int NOT NULL
		);
		CREATE TABLE billing.invoices (
			id bigserial PRIMARY KEY,
			status billing.status_enum NOT NULL
		);
	`)
	appendConfig(t, cfgPath, `overrides:
  types:
    "app.status_enum":
      type: ext.Status
      import: example.com/ext
    "app.positive_int":
      type: ext.Score
      import: example.com/ext
`)
	if _, stderr, err := executeCommand("generate", "--config", cfgPath, "--quiet"); err != nil {
		t.Fatalf("generate: unexpected error: %v\nstderr: %s", err, stderr)
	}
	files := parseGeneratedFiles(t, filepath.Join(filepath.Dir(cfgPath), "output"))

	for _, c := range []struct {
		typeName string
		want     map[string]string
	}{
		{typeName: "Account", want: map[string]string{"ID": "int64", "Status": "ext.Status", "Score": "ext.Score"}},
		{typeName: "User", want: map[string]string{"ID": "int64", "Status": "ext.Status", "Score": "ext.Score"}},
		{typeName: "Invoice", want: map[string]string{"ID": "int64", "Status": "BillingStatusEnum"}},
	} {
		if diff := cmp.Diff(c.want, structFieldTypes(files, c.typeName)); diff != "" {
			t.Errorf("%s field types mismatch (-want +got):\n%s", c.typeName, diff)
		}
	}
}

func appendConfig(t *testing.T, cfgPath, yaml string) {
	t.Helper()
	f, err := os.OpenFile(cfgPath, os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // test code, path is from controlled test dir
	if err != nil {
		t.Fatalf("opening config: %v", err)
	}
	defer f.Close() //nolint:errcheck // test cleanup; the write error is checked
	if _, err := f.WriteString(yaml); err != nil {
		t.Fatalf("appending to config: %v", err)
	}
}

// structFieldTypes returns the type of each named field of the struct type
// typeName, as written in the source.
func structFieldTypes(files []*ast.File, typeName string) map[string]string {
	fields := make(map[string]string)
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			spec, ok := n.(*ast.TypeSpec)
			if !ok || spec.Name.Name != typeName {
				return true
			}
			if st, ok := spec.Type.(*ast.StructType); ok {
				for _, field := range st.Fields.List {
					for _, name := range field.Names {
						fields[name.Name] = types.ExprString(field.Type)
					}
				}
			}
			return false
		})
	}
	return fields
}
