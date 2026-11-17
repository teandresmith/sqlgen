package sqlite_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/parser/sqlite"
)

// A SQLite view has no schema: CREATE VIEW names the bare view, and a
// qualifier on its name is dropped.
func TestParseViewSQL_ViewNameIsBare(t *testing.T) {
	tests := []struct {
		name string
		sql  string
	}{
		{name: "unqualified", sql: "CREATE VIEW v AS SELECT p.id FROM products p;"},
		{name: "main qualifier", sql: "CREATE VIEW main.v AS SELECT p.id FROM products p;"},
		{name: "other schema qualifier", sql: "CREATE VIEW app.v AS SELECT p.id FROM products p;"},
		{name: "quoted qualifier", sql: `CREATE VIEW "main"."v" AS SELECT p.id FROM products p;`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sqlite.ParseViewSQL([]byte(tt.sql))
			if err != nil {
				t.Fatalf("ParseViewSQL(%q) error: %v", tt.sql, err)
			}
			if got.Schema != "" || got.Name != "v" {
				t.Errorf("ParseViewSQL(%q) = %q.%q, want the bare view v", tt.sql, got.Schema, got.Name)
			}
		})
	}
}

// A view annotation file is read as a view: its name, its columns typed from
// the tables it reads, and its annotations.
func TestViewFileParsedAsView(t *testing.T) {
	schema := mswParse(t, `CREATE TABLE products (id INTEGER PRIMARY KEY, name TEXT NOT NULL, price REAL);
		CREATE TABLE reviews (id INTEGER PRIMARY KEY, product_id INTEGER NOT NULL REFERENCES products (id), rating INTEGER NOT NULL);`)
	const file = `-- @pk: id
-- @type review_count: int32

CREATE VIEW product_summary AS
SELECT p.id, p.name, p.price, COUNT(r.id) AS review_count
FROM products p
LEFT JOIN reviews r ON r.product_id = p.id
GROUP BY p.id, p.name, p.price;`
	annotations, err := parser.ParseViewAnnotations(file)
	if err != nil {
		t.Fatalf("ParseViewAnnotations() error: %v", err)
	}
	parsed, err := sqlite.ParseViewSQL([]byte(file))
	if err != nil {
		t.Fatalf("ParseViewSQL() error: %v", err)
	}
	view, err := parser.BuildView(parsed, file, annotations, schema, parser.SearchPath{})
	if err != nil {
		t.Fatalf("BuildView() error: %v", err)
	}
	if view.Schema != "" || view.Name != "product_summary" || view.Materialized {
		t.Errorf("view = %q.%q materialized=%v, want the plain view product_summary", view.Schema, view.Name, view.Materialized)
	}
	type col struct {
		Name, Type, GoType string
		Nullable, PK       bool
	}
	got := make([]col, 0, len(view.Columns))
	for _, c := range view.Columns {
		got = append(got, col{c.Name, c.Type, c.GoTypeLiteral, c.Nullable, c.PrimaryKey})
	}
	want := []col{
		{Name: "id", Type: "integer", PK: true},
		{Name: "name", Type: "text"},
		{Name: "price", Type: "real", Nullable: true},
		{Name: "review_count", Type: "int64", GoType: "int32"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("view columns mismatch (-want +got):\n%s", diff)
	}
}

// SQLite has no CREATE OR REPLACE VIEW, so a view file using it is a parse
// error, while the same view written CREATE VIEW parses.
func TestParseViewSQL_OrReplaceRejected(t *testing.T) {
	const query = " VIEW v AS SELECT p.id FROM products p;"
	if _, err := sqlite.ParseViewSQL([]byte("CREATE" + query)); err != nil {
		t.Fatalf("ParseViewSQL(CREATE VIEW) error: %v", err)
	}
	got, err := sqlite.ParseViewSQL([]byte("CREATE OR REPLACE" + query))
	if err == nil {
		t.Fatalf("ParseViewSQL(CREATE OR REPLACE VIEW) = %+v, want a parse error", got)
	}
	if !strings.Contains(err.Error(), "parsing view SQL") {
		t.Errorf("ParseViewSQL(CREATE OR REPLACE VIEW) error = %v, want a view parse error", err)
	}
}

// A view query using INTERSECT is a parse error naming the view.
func TestParseViewSQL_IntersectRejected(t *testing.T) {
	tests := []struct {
		name string
		sql  string
	}{
		{name: "intersect", sql: "CREATE VIEW v AS SELECT id FROM a INTERSECT SELECT id FROM b;"},
		{name: "intersect on a qualified view", sql: "CREATE VIEW main.v AS SELECT a.id FROM a INTERSECT SELECT b.id FROM b;"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := sqlite.ParseViewSQL([]byte(tt.sql))
			if err == nil || !strings.Contains(err.Error(), "not a simple SELECT") {
				t.Fatalf("ParseViewSQL(%q) error = %v, want not a simple SELECT", tt.sql, err)
			}
			qe, ok := errors.AsType[*parser.ViewQueryError](err)
			if !ok || qe.Schema != "" || qe.Name != "v" {
				t.Errorf("ParseViewSQL(%q) error = %#v, want a ViewQueryError naming the bare view v", tt.sql, err)
			}
		})
	}
}
