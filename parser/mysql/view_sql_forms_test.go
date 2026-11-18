package mysql_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/parser/mysql"
)

// CREATE OR REPLACE VIEW reads as CREATE VIEW does.
func TestParseViewSQL_OrReplaceAccepted(t *testing.T) {
	const query = " VIEW product_names AS SELECT p.id, p.name AS label FROM products p;"
	plain, err := mysql.ParseViewSQL([]byte("CREATE" + query))
	if err != nil {
		t.Fatalf("ParseViewSQL(CREATE VIEW) error: %v", err)
	}
	got, err := mysql.ParseViewSQL([]byte("CREATE OR REPLACE" + query))
	if err != nil {
		t.Fatalf("ParseViewSQL(CREATE OR REPLACE VIEW) error: %v", err)
	}
	if got.Schema != "" || got.Name != "product_names" {
		t.Errorf("view = %q.%q, want the bare view product_names", got.Schema, got.Name)
	}
	want := []parser.ViewSelectColumn{
		{Alias: "id", SourceTable: "p", SourceColumn: "id"},
		{Alias: "label", SourceTable: "p", SourceColumn: "name"},
	}
	if diff := cmp.Diff(want, got.Columns); diff != "" {
		t.Errorf("columns mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(plain, got); diff != "" {
		t.Errorf("CREATE OR REPLACE VIEW differs from CREATE VIEW (-plain +or replace):\n%s", diff)
	}
}

// A view column takes its name from an explicit alias, else from the column a
// bare or qualified reference names.
func TestParseViewSQL_ColumnNameSources(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want []parser.ViewSelectColumn
	}{
		{
			name: "explicit alias on a column",
			sql:  "CREATE VIEW v AS SELECT p.name AS label FROM products p;",
			want: []parser.ViewSelectColumn{{Alias: "label", SourceTable: "p", SourceColumn: "name"}},
		},
		{
			name: "explicit alias on an aggregate",
			sql:  "CREATE VIEW v AS SELECT AVG(r.rating) AS avg_rating FROM reviews r;",
			want: []parser.ViewSelectColumn{{Alias: "avg_rating", SourceTable: "r", SourceColumn: "rating", Aggregate: "AVG"}},
		},
		{
			name: "explicit alias without AS",
			sql:  "CREATE VIEW v AS SELECT price total FROM products;",
			want: []parser.ViewSelectColumn{{Alias: "total", SourceColumn: "price"}},
		},
		{
			name: "bare column reference",
			sql:  "CREATE VIEW v AS SELECT id, sku FROM products;",
			want: []parser.ViewSelectColumn{{Alias: "id", SourceColumn: "id"}, {Alias: "sku", SourceColumn: "sku"}},
		},
		{
			name: "bare reference beside a qualified one",
			sql:  "CREATE VIEW v AS SELECT sku, p.id FROM products p;",
			want: []parser.ViewSelectColumn{{Alias: "sku", SourceColumn: "sku"}, {Alias: "id", SourceTable: "p", SourceColumn: "id"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mysql.ParseViewSQL([]byte(tt.sql))
			if err != nil {
				t.Fatalf("ParseViewSQL(%q) error: %v", tt.sql, err)
			}
			if diff := cmp.Diff(tt.want, got.Columns); diff != "" {
				t.Errorf("ParseViewSQL(%q) columns mismatch (-want +got):\n%s", tt.sql, diff)
			}
		})
	}
}

// A view query using INTERSECT or EXCEPT is a parse error, though each of its
// arms reads as a view query.
func TestParseViewSQL_IntersectAndExceptRejected(t *testing.T) {
	if _, err := mysql.ParseViewSQL([]byte("CREATE VIEW v AS SELECT id FROM a;")); err != nil {
		t.Fatalf("ParseViewSQL(one arm) error: %v", err)
	}
	tests := []struct {
		name    string
		sql     string
		wantErr string
	}{
		{
			name:    "intersect",
			sql:     "CREATE VIEW v AS SELECT id FROM a INTERSECT SELECT id FROM b;",
			wantErr: "parsing view SQL: syntax error at position 51 near 'SELECT'",
		},
		{
			name:    "intersect all",
			sql:     "CREATE VIEW v AS SELECT id FROM a INTERSECT ALL SELECT id FROM b;",
			wantErr: "parsing view SQL: syntax error at position 48 near 'ALL'",
		},
		{
			name:    "except",
			sql:     "CREATE VIEW v AS SELECT id FROM a EXCEPT SELECT id FROM b;",
			wantErr: "parsing view SQL: syntax error at position 48 near 'SELECT'",
		},
		{
			name:    "except all",
			sql:     "CREATE VIEW v AS SELECT id FROM a EXCEPT ALL SELECT id FROM b;",
			wantErr: "parsing view SQL: syntax error at position 45 near 'ALL'",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mysql.ParseViewSQL([]byte(tt.sql))
			if err == nil {
				t.Fatalf("ParseViewSQL(%q) = %+v, want a parse error", tt.sql, got)
			}
			if err.Error() != tt.wantErr {
				t.Errorf("ParseViewSQL(%q) error = %q, want %q", tt.sql, err, tt.wantErr)
			}
		})
	}
}
