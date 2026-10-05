package gen_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// typeOverrideSchema returns a single-table schema whose columns cover the
// shapes a `column_map.<col>.type` override has to handle: a plain NOT NULL
// column, a nullable one, and a column that also carries a `type_map` entry.
//
// `amount` is `numeric` rather than `text` because the cases below retype it
// to `int32` / `decimal.Decimal`, and an arithmetic Go type over a
// non-arithmetic SQL column is rejected by validateIncrementSQLTypes. These
// cases are about which override route wins, not about arithmetic legality, so
// the column sits on a SQL type that makes them legal.
func typeOverrideSchema() *parser.Schema {
	return &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "sessions",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "ip_addr", Type: "text"},
					{Name: "last_seen_ip", Type: "text", Nullable: true},
					{Name: "payload", Type: "text", Nullable: true},
					{Name: "amount", Type: "numeric"},
				},
			},
		},
	}
}

func buildTypeOverrideTable(t *testing.T, tableCfg config.TableConfig) gen.TableContext {
	t.Helper()
	input := testInput(typeOverrideSchema())
	input.Config.Tables["sessions"] = tableCfg
	contexts, err := gen.BuildTableContexts(input, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() error: %v", err)
	}
	if len(contexts) != 1 {
		t.Fatalf("BuildTableContexts() returned %d contexts, want 1", len(contexts))
	}
	return contexts[0]
}

// TestColumnTypeOverride pins the resolution half of type overrides: a
// `column_map.<col>.type` override reaches the column's Go type, and its
// sibling `.import` reaches the generated file's import block. Both fields
// must actually be read, not merely declared.
func TestColumnTypeOverride(t *testing.T) {
	tests := []struct {
		name       string
		tableCfg   config.TableConfig
		column     string
		wantGoType string
		wantImport string
	}{
		{
			name: "external type with explicit import",
			tableCfg: config.TableConfig{ColumnMap: map[string]config.ColumnOverride{
				"ip_addr": {Type: "netip.Addr", Import: "net/netip"},
			}},
			column:     "ip_addr",
			wantGoType: "netip.Addr",
			wantImport: "net/netip",
		},
		{
			name: "known literal resolves its own import",
			tableCfg: config.TableConfig{ColumnMap: map[string]config.ColumnOverride{
				"ip_addr": {Type: "time.Time"},
			}},
			column:     "ip_addr",
			wantGoType: "time.Time",
			wantImport: "time",
		},
		{
			name: "explicit import beats the known-literal default",
			tableCfg: config.TableConfig{ColumnMap: map[string]config.ColumnOverride{
				"ip_addr": {Type: "time.Time", Import: "example.com/shim/time"},
			}},
			column:     "ip_addr",
			wantGoType: "time.Time",
			wantImport: "example.com/shim/time",
		},
		{
			name: "same-package type needs no import",
			tableCfg: config.TableConfig{ColumnMap: map[string]config.ColumnOverride{
				"ip_addr": {Type: "Address"},
			}},
			column:     "ip_addr",
			wantGoType: "Address",
			wantImport: "",
		},
		{
			name: "column_map beats type_map for the same column",
			tableCfg: config.TableConfig{
				TypeMap:   map[string]string{"amount": "int32"},
				ColumnMap: map[string]config.ColumnOverride{"amount": {Type: "decimal.Decimal", Import: "github.com/shopspring/decimal"}},
			},
			column:     "amount",
			wantGoType: "decimal.Decimal",
			wantImport: "github.com/shopspring/decimal",
		},
		{
			name: "type_map still applies to a column column_map only renames",
			tableCfg: config.TableConfig{
				TypeMap:   map[string]string{"amount": "int32"},
				ColumnMap: map[string]config.ColumnOverride{"amount": {Name: "Amount"}},
			},
			column:     "amount",
			wantGoType: "int32",
			wantImport: "",
		},
		{
			name: "nullable column takes the pointer form",
			tableCfg: config.TableConfig{ColumnMap: map[string]config.ColumnOverride{
				"last_seen_ip": {Type: "netip.Addr", Import: "net/netip"},
			}},
			column:     "last_seen_ip",
			wantGoType: "*netip.Addr",
			wantImport: "net/netip",
		},
		{
			name: "nullable natively-nilable type stays bare",
			tableCfg: config.TableConfig{ColumnMap: map[string]config.ColumnOverride{
				"payload": {Type: "json.RawMessage"},
			}},
			column:     "payload",
			wantGoType: "json.RawMessage",
			wantImport: "encoding/json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := buildTypeOverrideTable(t, tt.tableCfg)
			col := columnByName(t, tc.Columns, tt.column)
			if col.GoType != tt.wantGoType {
				t.Errorf("column %q GoType = %q, want %q", tt.column, col.GoType, tt.wantGoType)
			}
			if col.Import != tt.wantImport {
				t.Errorf("column %q Import = %q, want %q", tt.column, col.Import, tt.wantImport)
			}
			if tt.wantImport != "" && !slices.Contains(tc.Imports, tt.wantImport) {
				t.Errorf("TableContext.Imports = %v, want it to contain %q", tc.Imports, tt.wantImport)
			}
		})
	}
}

// TestColumnTypeOverrideRejectsUnimportableLiteral pins the guardrail half of
// type overrides. A package-qualified literal outside gotype's known registry has no
// import unless `column_map.<col>.import` supplies one; `type_map` cannot
// supply one at all. Either shape used to generate a file referencing a
// package nothing imports — it compiled only when an unrelated column
// happened to pull the same package in.
func TestColumnTypeOverrideRejectsUnimportableLiteral(t *testing.T) {
	tests := []struct {
		name     string
		tableCfg config.TableConfig
		wantErr  string
	}{
		{
			name:     "type_map cannot carry an import",
			tableCfg: config.TableConfig{TypeMap: map[string]string{"amount": "decimal.Decimal"}},
			wantErr:  "tables.sessions.type_map.amount",
		},
		{
			name:     "type_map slice of an external type",
			tableCfg: config.TableConfig{TypeMap: map[string]string{"amount": "[]decimal.Decimal"}},
			wantErr:  "tables.sessions.type_map.amount",
		},
		{
			name: "column_map type without an import",
			tableCfg: config.TableConfig{ColumnMap: map[string]config.ColumnOverride{
				"ip_addr": {Type: "netip.Addr"},
			}},
			wantErr: "tables.sessions.column_map.ip_addr.type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := testInput(typeOverrideSchema())
			input.Config.Tables["sessions"] = tt.tableCfg
			_, err := gen.BuildTableContexts(input, nil)
			if err == nil {
				t.Fatalf("BuildTableContexts() error = nil, want one mentioning %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("BuildTableContexts() error = %q, want it to mention %q", err, tt.wantErr)
			}
		})
	}
}

// TestColumnTypeOverrideAcceptsImportableLiteral is the negative control for
// the guardrail: the same literals are fine once an import is declared, and
// registry members / same-package types never needed one.
func TestColumnTypeOverrideAcceptsImportableLiteral(t *testing.T) {
	tableCfgs := map[string]config.TableConfig{
		"type_map builtin":        {TypeMap: map[string]string{"amount": "int32"}},
		"type_map same package":   {TypeMap: map[string]string{"amount": "Address"}},
		"type_map known registry": {TypeMap: map[string]string{"payload": "json.RawMessage"}},
		"column_map with import": {ColumnMap: map[string]config.ColumnOverride{
			"ip_addr": {Type: "netip.Addr", Import: "net/netip"},
		}},
		"column_map known registry": {ColumnMap: map[string]config.ColumnOverride{
			"ip_addr": {Type: "time.Time"},
		}},
	}

	for name, tableCfg := range tableCfgs {
		t.Run(name, func(t *testing.T) {
			input := testInput(typeOverrideSchema())
			input.Config.Tables["sessions"] = tableCfg
			if _, err := gen.BuildTableContexts(input, nil); err != nil {
				t.Errorf("BuildTableContexts() error = %v, want nil", err)
			}
		})
	}
}
