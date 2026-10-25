package parser_test

import (
	"testing"

	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/parser/postgres"
)

// TestBuildView_UnqualifiedFromUsesInputSchema pins that an annotation-file
// view whose FROM names a table without a schema finds that table in
// input.schema: with a same-named table in two schemas, the view column
// copies the type of the one in input.schema.
func TestBuildView_UnqualifiedFromUsesInputSchema(t *testing.T) {
	const ddl = `
		CREATE SCHEMA app;
		CREATE SCHEMA billing;
		CREATE TABLE app.accounts (code text NOT NULL);
		CREATE TABLE billing.accounts (code integer NOT NULL);
	`
	const viewSQL = `CREATE VIEW account_codes AS SELECT code FROM accounts;`

	tests := []struct {
		name        string
		inputSchema string
		wantType    string
	}{
		{name: "input schema app", inputSchema: "app", wantType: "text"},
		{name: "input schema billing", inputSchema: "billing", wantType: "integer"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := postgres.New(tt.inputSchema)
			if err := p.Parse("schema.sql", []byte(ddl)); err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			schema := p.Schema()

			parsed, err := postgres.ParseViewSQL([]byte(viewSQL), tt.inputSchema)
			if err != nil {
				t.Fatalf("ParseViewSQL() error: %v", err)
			}
			view, err := parser.BuildView(parsed, viewSQL, &parser.ViewAnnotations{}, schema, postgres.SearchPath(tt.inputSchema))
			if err != nil {
				t.Fatalf("BuildView() error: %v", err)
			}
			if len(view.Columns) != 1 {
				t.Fatalf("view columns = %+v, want one column", view.Columns)
			}
			if got := view.Columns[0].Type; got != tt.wantType {
				t.Errorf("input.schema %q: FROM accounts: code Type = %q, want %q (the %s.accounts column)", tt.inputSchema, got, tt.wantType, tt.inputSchema)
			}
		})
	}
}
