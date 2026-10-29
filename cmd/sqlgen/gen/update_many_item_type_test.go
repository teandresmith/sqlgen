package gen_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// gnwMethodParams returns the parameter types of recv's method name in src.
func gnwMethodParams(t *testing.T, src, recv, name string) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "src.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing generated source: %v", err)
	}
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Recv == nil || fd.Name.Name != name || gnwExpr(fd.Recv.List[0].Type) != recv {
			continue
		}
		var params []string
		for _, p := range fd.Type.Params.List {
			for range max(len(p.Names), 1) {
				params = append(params, gnwExpr(p.Type))
			}
		}
		return params
	}
	t.Fatalf("generated source has no method (%s).%s", recv, name)
	return nil
}

// TestGenerate_UpdateManyItemNamesThePKAfterItsColumn generates, on every
// dialect, tables whose single-column primary keys are not named id, and checks
// Update<T>Item pairs a field named and typed after that key column with an
// *Update<T>Input, and that UpdateMany takes a slice of those items.
func TestGenerate_UpdateManyItemNamesThePKAfterItsColumn(t *testing.T) {
	tests := []struct {
		name      string
		dialect   config.Dialect
		ddl       string
		item      string
		recv      string
		wantField []string
	}{
		{
			name:      "postgres text key product_code",
			dialect:   config.DialectPostgres,
			ddl:       `CREATE TABLE products (product_code text PRIMARY KEY, name text NOT NULL);`,
			item:      "UpdateProductItem",
			recv:      "*productClient",
			wantField: []string{"ProductCode string", "Input *UpdateProductInput"},
		},
		{
			name:      "postgres uuid key ticket_ref",
			dialect:   config.DialectPostgres,
			ddl:       `CREATE TABLE tickets (ticket_ref uuid PRIMARY KEY DEFAULT gen_random_uuid(), title text NOT NULL);`,
			item:      "UpdateTicketItem",
			recv:      "*ticketClient",
			wantField: []string{"TicketRef uuid.UUID", "Input *UpdateTicketInput"},
		},
		{
			name:      "mysql varchar key product_code",
			dialect:   config.DialectMySQL,
			ddl:       "CREATE TABLE products (product_code VARCHAR(32) NOT NULL PRIMARY KEY, name VARCHAR(255) NOT NULL);",
			item:      "UpdateProductItem",
			recv:      "*productClient",
			wantField: []string{"ProductCode string", "Input *UpdateProductInput"},
		},
		{
			name:      "sqlite text key product_code",
			dialect:   config.DialectSQLite,
			ddl:       `CREATE TABLE products (product_code TEXT PRIMARY KEY, name TEXT NOT NULL);`,
			item:      "UpdateProductItem",
			recv:      "*productClient",
			wantField: []string{"ProductCode string", "Input *UpdateProductInput"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			models := readGenFile(t, gnwGenerate(t, tt.dialect, tt.ddl, ""), "models_gen.go")
			if diff := cmp.Diff(tt.wantField, gnwStructFields(t, models, tt.item)); diff != "" {
				t.Errorf("%s fields (-want +got):\n%s", tt.item, diff)
			}
			params := gnwMethodParams(t, models, tt.recv, "UpdateMany")
			if len(params) < 2 || params[1] != "[]"+tt.item {
				t.Errorf("UpdateMany params = %v, want the second to be []%s", params, tt.item)
			}
		})
	}
}
