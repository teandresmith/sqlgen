package gen_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// unmappedWarnings keeps the warnings about a SQL type that fell back to
// string, dropping every other notice the build raised.
func unmappedWarnings(warnings []string) []string {
	var out []string
	for _, w := range warnings {
		if strings.Contains(w, "built-in mapping resolves") {
			out = append(out, w)
		}
	}
	return out
}

// TestUnmappedSQLTypeWarnsOnTableColumn pins the warning a table column gets
// when no resolution step maps its SQL type: generation succeeds, the column
// is a string, and the warning names the column, the SQL type and the key to
// add.
func TestUnmappedSQLTypeWarnsOnTableColumn(t *testing.T) {
	cases := []struct {
		name       string
		tableCfg   config.TableConfig
		global     map[string]config.TypeOverride
		wantGoType string
		want       []string
	}{
		{
			name:       "unknown type warns and falls back to string",
			wantGoType: "string",
			want: []string{
				`column invoices.handle has SQL type "citext", which no override, integration, enum, SET, domain or built-in mapping resolves, so it is generated as string; add overrides.types.citext to choose its Go type`,
			},
		},
		{
			name:       "global override silences the warning",
			global:     map[string]config.TypeOverride{"citext": {Type: "string"}},
			wantGoType: "string",
		},
		{
			name:       "table override silences the warning",
			tableCfg:   config.TableConfig{Overrides: &config.OverrideConfig{Types: map[string]config.TypeOverride{"citext": {Type: "string"}}}},
			wantGoType: "string",
		},
		{
			name:       "type_map literal silences the warning",
			tableCfg:   config.TableConfig{TypeMap: map[string]string{"handle": "string"}},
			wantGoType: "string",
		},
		{
			name:     "excluded column does not warn",
			tableCfg: config.TableConfig{ExcludeColumns: []string{"handle"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := testInput(&parser.Schema{Tables: []parser.Table{{
				Name: "invoices",
				Columns: []parser.Column{
					{Name: "id", Type: "text", PrimaryKey: true},
					{Name: "handle", Type: "citext"},
				},
			}}})
			for k, v := range tc.global {
				input.Config.Overrides.Types[k] = v
			}
			input.Config.Tables["invoices"] = tc.tableCfg
			contexts, err := gen.BuildTableContexts(input, nil)
			if err != nil {
				t.Fatalf("BuildTableContexts() error = %v, want nil", err)
			}
			if diff := cmp.Diff(tc.want, unmappedWarnings(input.Warnings)); diff != "" {
				t.Errorf("warnings mismatch (-want +got):\n%s", diff)
			}
			if tc.wantGoType == "" {
				return
			}
			for _, col := range contexts[0].Columns {
				if col.Name == "handle" && col.GoType != tc.wantGoType {
					t.Errorf("handle GoType = %q, want %q", col.GoType, tc.wantGoType)
				}
			}
		})
	}
}

// TestUnmappedSQLTypeWarnsOnViewColumn pins the same warning for a view
// column, and that a mapped view column raises none.
func TestUnmappedSQLTypeWarnsOnViewColumn(t *testing.T) {
	input := testInput(&parser.Schema{Views: []parser.View{{
		Name:   "handles",
		Schema: "public",
		Columns: []parser.Column{
			{Name: "id", Type: "text"},
			{Name: "handle", Type: "citext", Nullable: true},
		},
	}}})
	if _, err := gen.BuildViewContexts(input, nil); err != nil {
		t.Fatalf("BuildViewContexts() error = %v, want nil", err)
	}
	want := []string{
		`view column public.handles.handle has SQL type "citext", which no override, integration, enum, SET, domain or built-in mapping resolves, so it is generated as *string; add overrides.types.citext to choose its Go type`,
	}
	if diff := cmp.Diff(want, unmappedWarnings(input.Warnings)); diff != "" {
		t.Errorf("warnings mismatch (-want +got):\n%s", diff)
	}
}

// TestUnmappedSQLTypeWarnsOnCompositeColumn pins how a PostgreSQL column of a
// composite type generates: it stays a string holding the tuple text, since
// the generated struct reads and writes JSON (PRD §8.3), and the warning names
// that struct instead of the generic unmapped-type text. A bare composite name
// is the column's own schema's composite, never another schema's enum.
func TestUnmappedSQLTypeWarnsOnCompositeColumn(t *testing.T) {
	schema := &parser.Schema{
		CompositeTypes: []parser.CompositeType{
			{Name: "postal_address", Schema: "public", Attributes: []parser.Attribute{{Name: "street", Type: "text"}}},
			{Name: "kind", Schema: "public", Attributes: []parser.Attribute{{Name: "x", Type: "integer"}}},
		},
		Enums: []parser.Enum{{Name: "kind", Schema: "app", Values: []string{"a", "b"}}},
		Tables: []parser.Table{{
			Name:   "accounts",
			Schema: "public",
			Columns: []parser.Column{
				{Name: "id", Type: "text", PrimaryKey: true},
				{Name: "address", Type: "postal_address", Nullable: true},
				{Name: "history", Type: "postal_address[]", Nullable: true},
				{Name: "k", Type: "kind", Nullable: true},
			},
		}},
		Views: []parser.View{{
			Name:    "addresses",
			Schema:  "public",
			Columns: []parser.Column{{Name: "address", Type: "postal_address"}},
		}},
	}
	cfg := testInput(schema).Config

	warnings, err := gen.ValidateGeneration(schema, cfg)
	if err != nil {
		t.Fatalf("ValidateGeneration() error = %v, want nil", err)
	}
	want := []string{
		`column public.accounts.address has composite type "postal_address", so it is generated as *string holding the tuple text; the generated PostalAddress reads and writes JSON (PRD §8.3), so to use it declare the column jsonb and map it with type_map, or add overrides.types.postal_address to choose its Go type`,
		`column public.accounts.history has composite type "postal_address[]", so it is generated as []string holding the tuple text; the generated PostalAddress reads and writes JSON (PRD §8.3), so to use it declare the column jsonb and map it with type_map, or add overrides.types.postal_address to choose its Go type`,
		`column public.accounts.k has composite type "kind", so it is generated as *string holding the tuple text; the generated Kind reads and writes JSON (PRD §8.3), so to use it declare the column jsonb and map it with type_map, or add overrides.types.kind to choose its Go type`,
		`view column public.addresses.address has composite type "postal_address", so it is generated as string holding the tuple text; the generated PostalAddress reads and writes JSON (PRD §8.3), so to use it declare the column jsonb and map it with type_map, or add overrides.types.postal_address to choose its Go type`,
	}
	if diff := cmp.Diff(want, compositeWarnings(warnings)); diff != "" {
		t.Errorf("warnings mismatch (-want +got):\n%s", diff)
	}

	contexts, err := gen.BuildTableContextsFromSchema(schema, cfg)
	if err != nil {
		t.Fatalf("BuildTableContextsFromSchema() error = %v, want nil", err)
	}
	wantTypes := map[string]string{"address": "*string", "history": "[]string", "k": "*string"}
	for _, col := range contexts[0].Columns {
		if want, ok := wantTypes[col.Name]; ok && col.GoType != want {
			t.Errorf("%s GoType = %q, want %q", col.Name, col.GoType, want)
		}
	}
}

// compositeWarnings keeps the warnings about a composite-typed column, and the
// generic unmapped-type warnings, which a composite column must not raise.
func compositeWarnings(warnings []string) []string {
	var out []string
	for _, w := range warnings {
		if strings.Contains(w, "composite type") || strings.Contains(w, "built-in mapping resolves") {
			out = append(out, w)
		}
	}
	return out
}
