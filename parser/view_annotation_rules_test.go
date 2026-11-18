package parser_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/parser/mysql"
	"github.com/teandresmith/sqlgen/parser/postgres"
	"github.com/teandresmith/sqlgen/parser/sqlite"
)

// qvwDDL is the schema every view annotation file below reads from. It is
// valid on all three dialects.
const qvwDDL = `
CREATE TABLE products (
    id integer NOT NULL PRIMARY KEY,
    tenant_id integer NOT NULL,
    name text NOT NULL,
    price integer NOT NULL,
    sku text NOT NULL,
    discount integer
);
CREATE TABLE reviews (
    id integer NOT NULL PRIMARY KEY,
    product_id integer NOT NULL,
    rating integer NOT NULL
);
`

// qvwDialects lists the three dialects an annotation file is read for.
var qvwDialects = []string{"postgres", "mysql", "sqlite"}

// qvwBuild reads a view annotation file the way input.views does for
// dialect: the DDL parser reads qvwDDL, then the file's directives and its
// CREATE VIEW are read and the view is built against that schema.
func qvwBuild(t *testing.T, dialect, file string) (*parser.View, *parser.ViewAnnotations, error) {
	t.Helper()
	var (
		schema     *parser.Schema
		parseView  func([]byte) (parser.ViewSQL, error)
		searchPath parser.SearchPath
	)
	switch dialect {
	case "postgres":
		p := postgres.New("public")
		if err := p.Parse("schema.sql", []byte(qvwDDL)); err != nil {
			t.Fatalf("postgres Parse() error: %v", err)
		}
		schema = p.Schema()
		parseView = func(sql []byte) (parser.ViewSQL, error) { return postgres.ParseViewSQL(sql, "public") }
		searchPath = postgres.SearchPath("public")
	case "mysql":
		p, err := mysql.New()
		if err != nil {
			t.Fatalf("mysql.New() error: %v", err)
		}
		if err := p.Parse("schema.sql", []byte(qvwDDL)); err != nil {
			t.Fatalf("mysql Parse() error: %v", err)
		}
		schema = p.Schema()
		parseView = mysql.ParseViewSQL
	case "sqlite":
		p := sqlite.New()
		if err := p.Parse("schema.sql", []byte(qvwDDL)); err != nil {
			t.Fatalf("sqlite Parse() error: %v", err)
		}
		schema = p.Schema()
		parseView = sqlite.ParseViewSQL
	default:
		t.Fatalf("unknown dialect %q", dialect)
	}

	annotations, err := parser.ParseViewAnnotations(file)
	if err != nil {
		return nil, nil, err
	}
	parsed, err := parseView([]byte(file))
	if err != nil {
		t.Fatalf("%s ParseViewSQL() error: %v", dialect, err)
	}
	view, err := parser.BuildView(parsed, file, annotations, schema, searchPath)
	return view, annotations, err
}

// qvwColumnFlags is the part of a view column the directives set.
type qvwColumnFlags struct {
	Name       string
	PrimaryKey bool
	Nullable   bool
}

func qvwFlags(view *parser.View) []qvwColumnFlags {
	out := make([]qvwColumnFlags, 0, len(view.Columns))
	for _, c := range view.Columns {
		out = append(out, qvwColumnFlags{Name: c.Name, PrimaryKey: c.PrimaryKey, Nullable: c.Nullable})
	}
	return out
}

// TestViewAnnotationFile_DirectiveRules reads one annotation file per case on
// every dialect and checks the primary key and nullability its directives
// give each view column.
func TestViewAnnotationFile_DirectiveRules(t *testing.T) {
	tests := []struct {
		name         string
		file         string
		wantPK       []string
		wantNullable []string
		want         []qvwColumnFlags
	}{
		{
			// The comment inside the statement looks like a directive but
			// comes after CREATE VIEW, so price stays NOT NULL and the key
			// stays id.
			name: "directives come from the comments before CREATE VIEW",
			file: `-- product rollup
-- @pk: id
-- @nullable: name

CREATE VIEW product_rollup AS
-- @nullable: price
-- @pk: sku
SELECT id, name, price, sku FROM products;
`,
			wantPK:       []string{"id"},
			wantNullable: []string{"name"},
			want: []qvwColumnFlags{
				{Name: "id", PrimaryKey: true},
				{Name: "name", Nullable: true},
				{Name: "price"},
				{Name: "sku"},
			},
		},
		{
			name: "@pk takes a comma-separated list",
			file: `-- @pk: tenant_id, id
CREATE VIEW tenant_products AS
SELECT tenant_id, id, name FROM products;
`,
			wantPK: []string{"tenant_id", "id"},
			want: []qvwColumnFlags{
				{Name: "tenant_id", PrimaryKey: true},
				{Name: "id", PrimaryKey: true},
				{Name: "name"},
			},
		},
		{
			name: "@pk list without spaces after the commas",
			file: `-- @pk: tenant_id,id,sku
CREATE VIEW tenant_skus AS
SELECT tenant_id, id, sku, name FROM products;
`,
			wantPK: []string{"tenant_id", "id", "sku"},
			want: []qvwColumnFlags{
				{Name: "tenant_id", PrimaryKey: true},
				{Name: "id", PrimaryKey: true},
				{Name: "sku", PrimaryKey: true},
				{Name: "name"},
			},
		},
		{
			name: "several @nullable lines are merged",
			file: `-- @pk: id
-- @nullable: name
-- @nullable: price, sku
CREATE VIEW loose_products AS
SELECT id, name, price, sku, tenant_id FROM products;
`,
			wantPK:       []string{"id"},
			wantNullable: []string{"name", "price", "sku"},
			want: []qvwColumnFlags{
				{Name: "id", PrimaryKey: true},
				{Name: "name", Nullable: true},
				{Name: "price", Nullable: true},
				{Name: "sku", Nullable: true},
				{Name: "tenant_id"},
			},
		},
		{
			name: "directives name the column list's names",
			file: `-- @pk: product_id
-- @nullable: label
CREATE VIEW product_labels (product_id, label) AS
SELECT id, name FROM products;
`,
			wantPK:       []string{"product_id"},
			wantNullable: []string{"label"},
			want: []qvwColumnFlags{
				{Name: "product_id", PrimaryKey: true},
				{Name: "label", Nullable: true},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, dialect := range qvwDialects {
				view, annotations, err := qvwBuild(t, dialect, tt.file)
				if err != nil {
					t.Errorf("%s: error = %v, want none", dialect, err)
					continue
				}
				if diff := cmp.Diff(tt.wantPK, annotations.PKColumns); diff != "" {
					t.Errorf("%s: PKColumns mismatch (-want +got):\n%s", dialect, diff)
				}
				if diff := cmp.Diff(tt.wantNullable, annotations.NullableColumns); diff != "" {
					t.Errorf("%s: NullableColumns mismatch (-want +got):\n%s", dialect, diff)
				}
				if diff := cmp.Diff(tt.want, qvwFlags(view)); diff != "" {
					t.Errorf("%s: view columns mismatch (-want +got):\n%s", dialect, diff)
				}
			}
		})
	}
}

// TestViewAnnotationFile_NullableOnNullableAggregate checks that @nullable on
// an AVG column, which is already nullable, leaves it as it is: the
// *float64 Go type literal, nullable, exactly as without the directive.
func TestViewAnnotationFile_NullableOnNullableAggregate(t *testing.T) {
	const query = `CREATE VIEW product_ratings AS
SELECT p.id, AVG(r.rating) AS avg_rating
FROM products p
LEFT JOIN reviews r ON r.product_id = p.id
GROUP BY p.id;
`
	tests := []struct {
		name string
		file string
	}{
		{name: "without @nullable", file: "-- @pk: id\n" + query},
		{name: "with @nullable", file: "-- @pk: id\n-- @nullable: avg_rating\n" + query},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, dialect := range qvwDialects {
				view, _, err := qvwBuild(t, dialect, tt.file)
				if err != nil {
					t.Errorf("%s: error = %v, want none", dialect, err)
					continue
				}
				if len(view.Columns) != 2 {
					t.Errorf("%s: columns = %+v, want id and avg_rating", dialect, view.Columns)
					continue
				}
				got := view.Columns[1]
				want := parser.Column{Name: "avg_rating", Type: "*float64", GoTypeLiteral: "*float64", Nullable: true}
				if diff := cmp.Diff(want, got); diff != "" {
					t.Errorf("%s: avg_rating mismatch (-want +got):\n%s", dialect, diff)
				}
			}
		})
	}
}

// TestViewAnnotationFile_UnknownColumnErrors checks that a directive naming a
// column the view does not have is an error naming the directive and the
// column, on every dialect.
func TestViewAnnotationFile_UnknownColumnErrors(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		wantErr string
	}{
		{
			name: "@type names a column not in the view",
			file: `-- @type rating_total: int64
CREATE VIEW product_names AS
SELECT id, name FROM products;
`,
			wantErr: `@type references unknown column "rating_total"`,
		},
		{
			name: "@nullable names a column not in the view",
			file: `-- @nullable: name, discount
CREATE VIEW product_names AS
SELECT id, name FROM products;
`,
			wantErr: `@nullable references unknown column "discount"`,
		},
		{
			// With a column list the SELECT names are not the view's names.
			name: "@pk names the SELECT column a column list renames",
			file: `-- @pk: id
CREATE VIEW product_labels (product_id, label) AS
SELECT id, name FROM products;
`,
			wantErr: `@pk references unknown column "id"`,
		},
		{
			name: "@nullable names the SELECT column a column list renames",
			file: `-- @nullable: name
CREATE VIEW product_labels (product_id, label) AS
SELECT id, name FROM products;
`,
			wantErr: `@nullable references unknown column "name"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, dialect := range qvwDialects {
				_, _, err := qvwBuild(t, dialect, tt.file)
				if err == nil || err.Error() != tt.wantErr {
					t.Errorf("%s: error = %v, want %q", dialect, err, tt.wantErr)
				}
			}
		})
	}
}
