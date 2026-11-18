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
