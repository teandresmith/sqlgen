package gen_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// incrementGuardSchema returns a schema covering every shape the
// arithmetic guard has to separate: a non-arithmetic column an override can
// retype (`note`), an arithmetic one it legitimately can (`total`), a column
// whose SQL type the classifier cannot place (`payload`), the two identifying
// columns increment already excludes — the PK and an FK — where an arithmetic
// override must stay legal because no increment surface is emitted for them,
// and a domain over each side (`email` over text, `positive_int` over integer)
// so the guard is pinned to classify the base rather than the domain name.
func incrementGuardSchema() *parser.Schema {
	return &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "users",
				Columns: []parser.Column{
					{Name: "id", Type: "text", PrimaryKey: true},
				},
			},
			{
				Name: "invoices",
				Columns: []parser.Column{
					{Name: "id", Type: "text", PrimaryKey: true},
					{Name: "note", Type: "text"},
					{Name: "total", Type: "numeric"},
					{Name: "payload", Type: "jsonb", Nullable: true},
					{Name: "email_addr", Type: "email"},
					{Name: "positive_qty", Type: "positive_int"},
					{
						Name:        "owner_ref",
						Type:        "text",
						FKReference: &parser.FKReference{Table: "users", Column: "id"},
					},
				},
			},
		},
	}
}

func buildIncrementGuardTable(t *testing.T, tableCfg config.TableConfig) (gen.TableContext, []string) {
	t.Helper()
	input := testInput(incrementGuardSchema())
	// Mirrors registerSchemaTypes, which runs before BuildTableContexts on
	// both real entry points (orchestrate.go:67, :1786).
	input.Resolver.RegisterDomain("email", "text")
	input.Resolver.RegisterDomain("positive_int", "integer")
	input.Config.Tables["invoices"] = tableCfg
	contexts, err := gen.BuildTableContexts(input, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() error = %v, want nil — the arithmetic rule warns, it does not reject", err)
	}
	for _, tc := range contexts {
		if tc.TableName == "invoices" {
			return tc, input.Warnings
		}
	}
	t.Fatalf("BuildTableContexts() returned no context for invoices")
	return gen.TableContext{}, nil
}

func incrementColumnNames(tc gen.TableContext) []string {
	out := make([]string, 0, len(tc.IncrementColumns))
	for _, col := range tc.IncrementColumns {
		out = append(out, col.Name)
	}
	slices.Sort(out)
	return out
}

// TestIncrementSQLTypeGuardWarns pins the guard: a type override that resolves a
// non-arithmetic column to an arithmetic Go type would enroll it in the
// Increment surface, emitting `SET "col" = "col" + $1` against a column the
// database will not do arithmetic on — an error on PostgreSQL, a silent
// coercion on MySQL, and a silent rewrite of the stored value on SQLite.
//
// The column is dropped from the surface and a warning is emitted; the config
// still generates, because everything else the override does is correct. This
// is what keeps `text` + `decimal.Decimal` — the only exact-decimal shape
// SQLite offers, since NUMERIC converts to REAL on insert — expressible.
// Every override route reaches the same rule, and the warning names the route
// so the config line is findable.
func TestIncrementSQLTypeGuardWarns(t *testing.T) {
	tests := []struct {
		name        string
		tableCfg    config.TableConfig
		column      string
		wantWarning string
		wantSQLType string
	}{
		{
			name: "column_map type override",
			tableCfg: config.TableConfig{ColumnMap: map[string]config.ColumnOverride{
				"note": {Type: "decimal.Decimal", Import: "github.com/shopspring/decimal"},
			}},
			column:      "note",
			wantWarning: `tables.invoices.column_map.note.type: "decimal.Decimal" is an arithmetic Go type`,
			wantSQLType: `has SQL type "text",`,
		},
		{
			name:        "type_map override",
			tableCfg:    config.TableConfig{TypeMap: map[string]string{"note": "int32"}},
			column:      "note",
			wantWarning: `tables.invoices.type_map.note: "int32" is an arithmetic Go type`,
			wantSQLType: `has SQL type "text",`,
		},
		{
			name: "table-level overrides.types override",
			tableCfg: config.TableConfig{Overrides: &config.OverrideConfig{
				Types: map[string]config.TypeOverride{"text": {Type: "int64"}},
			}},
			column:      "note",
			wantWarning: `tables.invoices.overrides.types.text: "int64" is an arithmetic Go type`,
			wantSQLType: `has SQL type "text",`,
		},
		{
			name:        "domain over a non-arithmetic base",
			tableCfg:    config.TableConfig{TypeMap: map[string]string{"email_addr": "int32"}},
			column:      "email_addr",
			wantWarning: `tables.invoices.type_map.email_addr: "int32" is an arithmetic Go type`,
			wantSQLType: `has SQL type "email" (a domain over "text"),`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc, warnings := buildIncrementGuardTable(t, tt.tableCfg)

			if slices.Contains(incrementColumnNames(tc), tt.column) {
				t.Errorf("IncrementColumns = %v, want %q dropped", incrementColumnNames(tc), tt.column)
			}

			var matched string
			for _, w := range warnings {
				if strings.Contains(w, tt.wantWarning) {
					matched = w
					break
				}
			}
			if matched == "" {
				t.Fatalf("warnings = %v, want one containing %q", warnings, tt.wantWarning)
			}
			if !strings.Contains(matched, tt.wantSQLType) {
				t.Errorf("warning = %q, want it to name the column's SQL type (%s)", matched, tt.wantSQLType)
			}
			if !strings.Contains(matched, "is not offered for increment") {
				t.Errorf("warning = %q, want it to say the column is not offered for increment", matched)
			}
		})
	}
}

// TestIncrementSQLTypeGuardAccepts pins what the rule must NOT touch, and that
// nothing it leaves alone warns. The check is about arithmetic, not about type
// identity: retyping a column to a non-arithmetic external type is unaffected,
// and so is an arithmetic override on a column the database can actually add
// to. An unclassifiable SQL type is trusted rather than guessed at, matching
// the §29.2.4 tenancy guard.
func TestIncrementSQLTypeGuardAccepts(t *testing.T) {
	tests := []struct {
		name          string
		tableCfg      config.TableConfig
		wantIncrement []string
	}{
		{
			name: "arithmetic override on an arithmetic column",
			tableCfg: config.TableConfig{ColumnMap: map[string]config.ColumnOverride{
				"total": {Type: "decimal.Decimal", Import: "github.com/shopspring/decimal"},
			}},
			wantIncrement: []string{"positive_qty", "total"},
		},
		{
			name: "non-arithmetic override on a non-arithmetic column",
			tableCfg: config.TableConfig{ColumnMap: map[string]config.ColumnOverride{
				"note": {Type: "uuid.UUID", Import: "github.com/google/uuid"},
			}},
			wantIncrement: []string{"positive_qty", "total"},
		},
		{
			name:          "unclassifiable SQL type is trusted",
			tableCfg:      config.TableConfig{TypeMap: map[string]string{"payload": "int32"}},
			wantIncrement: []string{"payload", "positive_qty", "total"},
		},
		{
			name:          "primary key is never enrolled",
			tableCfg:      config.TableConfig{TypeMap: map[string]string{"id": "int64"}},
			wantIncrement: []string{"positive_qty", "total"},
		},
		{
			name:          "foreign key is never enrolled",
			tableCfg:      config.TableConfig{TypeMap: map[string]string{"owner_ref": "int64"}},
			wantIncrement: []string{"positive_qty", "total"},
		},
		{
			name:          "domain over an arithmetic base is enrolled, not rejected",
			tableCfg:      config.TableConfig{TypeMap: map[string]string{"positive_qty": "int64"}},
			wantIncrement: []string{"positive_qty", "total"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc, warnings := buildIncrementGuardTable(t, tt.tableCfg)
			if got := incrementColumnNames(tc); !slices.Equal(got, tt.wantIncrement) {
				t.Errorf("IncrementColumns = %v, want %v", got, tt.wantIncrement)
			}
			if len(warnings) != 0 {
				t.Errorf("warnings = %v, want none", warnings)
			}
		})
	}
}

// TestAPIIncrementSurfaceFollowsOperations pins the guard's second half:
// the GraphQL `_inc` / `_dec` operators are part of the increment surface, so
// they follow the resolved Increment flag like the client method does.
//
// They dispatch to the client's generated `Increment` method and the
// `<T>IncrementColumn` enum (api/resolvers.go.tmpl), both of which
// table/increment.go.tmpl and table/client.go.tmpl emit only under
// `.Operations.Increment`. The original failing shape was a client toggle turning
// Increment off under an enabled API; the toggle is gone (PRD §4.6), so the
// flag is now the incrementable-column fact itself and this pins that the API
// still reads it.
func TestAPIIncrementSurfaceFollowsOperations(t *testing.T) {
	tests := []struct {
		name          string
		wantOps       bool
		wantEnumType  string
		wantHasColumn bool
	}{
		{
			name:    "incrementable columns present",
			wantOps: true, wantEnumType: "InvoiceIncrementColumn", wantHasColumn: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := apiTestInput(t, incrementGuardSchema())
			input.Resolver.RegisterDomain("email", "text")
			input.Resolver.RegisterDomain("positive_int", "integer")

			tables, err := gen.BuildTableContexts(input, nil)
			if err != nil {
				t.Fatalf("BuildTableContexts() error = %v", err)
			}
			apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, input.Config)
			if err != nil {
				t.Fatalf("BuildAPIContext() error = %v", err)
			}

			var invoices *gen.APITableContext
			for i := range apiCtx.Tables {
				if apiCtx.Tables[i].StructName == "Invoice" {
					invoices = &apiCtx.Tables[i]
				}
			}
			if invoices == nil {
				t.Fatalf("BuildAPIContext() produced no table context for invoices")
			}

			if got := len(invoices.UpdateOps) > 0; got != tt.wantOps {
				t.Errorf("len(UpdateOps) > 0 = %v, want %v (ops = %+v)", got, tt.wantOps, invoices.UpdateOps)
			}
			if invoices.IncrementEnumType != tt.wantEnumType {
				t.Errorf("IncrementEnumType = %q, want %q", invoices.IncrementEnumType, tt.wantEnumType)
			}
			if invoices.HasIncrementColumns != tt.wantHasColumn {
				t.Errorf("HasIncrementColumns = %v, want %v", invoices.HasIncrementColumns, tt.wantHasColumn)
			}

			// The `_inc` / `_dec` schema fields ride APIFieldContext.Numeric.
			var numeric []string
			for _, f := range invoices.Fields {
				if f.Numeric {
					numeric = append(numeric, f.SQLName)
				}
			}
			if got := len(numeric) > 0; got != tt.wantOps {
				t.Errorf("numeric fields = %v, want any = %v", numeric, tt.wantOps)
			}
		})
	}
}
