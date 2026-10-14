package sqlite_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/parser/sqlite"
)

func TestCreateTableSQLiteTypes(t *testing.T) {
	sql := `CREATE TABLE test_types (
		t_integer INTEGER,
		t_int INT,
		t_tinyint TINYINT,
		t_smallint SMALLINT,
		t_bigint BIGINT,
		t_text TEXT,
		t_varchar VARCHAR(255),
		t_char CHAR(10),
		t_clob CLOB,
		t_real REAL,
		t_float FLOAT,
		t_double DOUBLE,
		t_blob BLOB,
		t_boolean BOOLEAN,
		t_numeric NUMERIC,
		t_decimal DECIMAL(10,2),
		t_date DATE,
		t_datetime DATETIME,
		t_timestamp TIMESTAMP
	);`

	p := sqlite.New()
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	schema := p.Schema()
	if len(schema.Tables) != 1 {
		t.Fatalf("Parse() tables = %d, want 1", len(schema.Tables))
	}
	table := schema.Tables[0]
	if table.Name != "test_types" {
		t.Errorf("table name = %q, want %q", table.Name, "test_types")
	}
	// SQLite tables should have no schema prefix.
	if table.Schema != "" {
		t.Errorf("table schema = %q, want empty", table.Schema)
	}

	wantTypes := map[string]string{
		"t_integer":   "integer",
		"t_int":       "int",
		"t_tinyint":   "tinyint",
		"t_smallint":  "smallint",
		"t_bigint":    "bigint",
		"t_text":      "text",
		"t_varchar":   "varchar(255)",
		"t_char":      "char(10)",
		"t_clob":      "clob",
		"t_real":      "real",
		"t_float":     "float",
		"t_double":    "double",
		"t_blob":      "blob",
		"t_boolean":   "boolean",
		"t_numeric":   "numeric",
		"t_decimal":   "decimal(10,2)",
		"t_date":      "date",
		"t_datetime":  "datetime",
		"t_timestamp": "timestamp",
	}

	for _, col := range table.Columns {
		want, ok := wantTypes[col.Name]
		if !ok {
			t.Errorf("unexpected column %q", col.Name)
			continue
		}
		if col.Type != want {
			t.Errorf("column %q type = %q, want %q", col.Name, col.Type, want)
		}
		if !col.Nullable {
			t.Errorf("column %q nullable = false, want true", col.Name)
		}
	}
}

func TestCreateTableConstraints(t *testing.T) {
	sql := `CREATE TABLE orders (
		id INTEGER PRIMARY KEY,
		email TEXT UNIQUE NOT NULL,
		amount REAL DEFAULT 0,
		user_id INTEGER REFERENCES users(id),
		CHECK (amount >= 0)
	);`

	p := sqlite.New()
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	table := p.Schema().Tables[0]
	cols := make(map[string]parser.Column)
	for _, c := range table.Columns {
		cols[c.Name] = c
	}

	// PK — INTEGER PRIMARY KEY is auto-increment (rowid alias).
	if c := cols["id"]; !c.PrimaryKey || c.Nullable || !c.AutoIncrement {
		t.Errorf("id: PrimaryKey=%v Nullable=%v AutoIncrement=%v, want PrimaryKey=true Nullable=false AutoIncrement=true",
			c.PrimaryKey, c.Nullable, c.AutoIncrement)
	}

	// UNIQUE NOT NULL
	if c := cols["email"]; !c.Unique || c.Nullable {
		t.Errorf("email: Unique=%v Nullable=%v, want Unique=true Nullable=false", c.Unique, c.Nullable)
	}

	// DEFAULT
	if c := cols["amount"]; c.Default != "0" {
		t.Errorf("amount: Default=%q, want %q", c.Default, "0")
	}

	// FK REFERENCES
	if c := cols["user_id"]; c.FKReference == nil {
		t.Error("user_id: FKReference is nil, want non-nil")
	} else if c.FKReference.Table != "users" || c.FKReference.Column != "id" {
		t.Errorf("user_id FK = %s.%s, want users.id", c.FKReference.Table, c.FKReference.Column)
	}

	// Table-level CHECK constraint.
	var hasCheck bool
	for _, c := range table.Constraints {
		if c.Type == parser.Check {
			hasCheck = true
			if c.CheckExpression == "" {
				t.Error("CHECK expression is empty, want non-empty")
			}
		}
	}
	if !hasCheck {
		t.Error("missing CHECK constraint")
	}
}

func TestTypeAffinityUnrecognized(t *testing.T) {
	sql := `CREATE TABLE test_affinity (
		a FOOBAR,
		b XYZZY(100),
		c,
		d BLOB,
		e TEXT
	);`

	p := sqlite.New()
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	table := p.Schema().Tables[0]
	cols := make(map[string]parser.Column)
	for _, c := range table.Columns {
		cols[c.Name] = c
	}

	// Unrecognized types default to "blob".
	if cols["a"].Type != "blob" {
		t.Errorf("column a type = %q, want %q", cols["a"].Type, "blob")
	}
	if cols["b"].Type != "blob" {
		t.Errorf("column b type = %q, want %q", cols["b"].Type, "blob")
	}
	// No type declared → "blob".
	if cols["c"].Type != "blob" {
		t.Errorf("column c (no type) type = %q, want %q", cols["c"].Type, "blob")
	}
	// Recognized types are preserved.
	if cols["d"].Type != "blob" {
		t.Errorf("column d type = %q, want %q", cols["d"].Type, "blob")
	}
	if cols["e"].Type != "text" {
		t.Errorf("column e type = %q, want %q", cols["e"].Type, "text")
	}
}

func TestIntegerPrimaryKeyAutoIncrement(t *testing.T) {
	sql := `CREATE TABLE users (
		id INTEGER PRIMARY KEY,
		name TEXT NOT NULL
	);`

	p := sqlite.New()
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	table := p.Schema().Tables[0]
	cols := make(map[string]parser.Column)
	for _, c := range table.Columns {
		cols[c.Name] = c
	}

	// INTEGER PRIMARY KEY is auto-increment (rowid alias).
	if !cols["id"].AutoIncrement {
		t.Error("id should have AutoIncrement=true")
	}
	if !cols["id"].PrimaryKey {
		t.Error("id should have PrimaryKey=true")
	}
	if cols["id"].Nullable {
		t.Error("id should have Nullable=false")
	}
	if cols["name"].AutoIncrement {
		t.Error("name should have AutoIncrement=false")
	}
}

// SQLite allows a sort order after a column PRIMARY KEY. It belongs to the
// key, so it must not surface as a column of its own, while a column that is
// really named desc still parses.
func TestPrimaryKeySortOrder(t *testing.T) {
	tests := []struct {
		name     string
		sql      string
		wantCols []string
		wantFK   string // a column that must carry the foreign key
	}{
		{
			name:     "DESC",
			sql:      `CREATE TABLE t (id INTEGER PRIMARY KEY DESC, v TEXT);`,
			wantCols: []string{"id", "v"},
		},
		{
			name:     "ASC",
			sql:      `CREATE TABLE t (id INTEGER PRIMARY KEY ASC, v TEXT);`,
			wantCols: []string{"id", "v"},
		},
		{
			name:     "last column",
			sql:      `CREATE TABLE t (v TEXT, id INTEGER PRIMARY KEY DESC);`,
			wantCols: []string{"v", "id"},
		},
		{
			// rqlite/sql positions count runes, not bytes.
			name:     "multibyte text before the key",
			sql:      "-- café ✓\nCREATE TABLE t (\"é\" TEXT DEFAULT 'ü', id INTEGER PRIMARY KEY DESC, v TEXT);",
			wantCols: []string{"é", "id", "v"},
		},
		{
			name:     "comment before the sort order",
			sql:      `CREATE TABLE t (id INTEGER PRIMARY KEY /* a, b */ DESC, v TEXT);`,
			wantCols: []string{"id", "v"},
		},
		{
			name:     "AUTOINCREMENT after the sort order",
			sql:      `CREATE TABLE t (id INTEGER PRIMARY KEY ASC AUTOINCREMENT, v TEXT);`,
			wantCols: []string{"id", "v"},
		},
		{
			name:     "ON CONFLICT after the sort order",
			sql:      `CREATE TABLE t (id INTEGER PRIMARY KEY DESC ON CONFLICT REPLACE, v TEXT);`,
			wantCols: []string{"id", "v"},
		},
		{
			name:     "constraints after the sort order stay on the key column",
			sql:      `CREATE TABLE t (id INTEGER PRIMARY KEY DESC REFERENCES p(id), v TEXT);`,
			wantCols: []string{"id", "v"},
			wantFK:   "id",
		},
		{
			name:     "column named desc",
			sql:      `CREATE TABLE t (id INTEGER PRIMARY KEY, desc TEXT);`,
			wantCols: []string{"id", "desc"},
		},
		{
			name:     "untyped column named desc",
			sql:      `CREATE TABLE t (id INTEGER PRIMARY KEY, desc);`,
			wantCols: []string{"id", "desc"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := sqlite.New()
			if err := p.Parse("test.sql", []byte(tt.sql)); err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			table := p.Schema().Tables[0]
			got := make([]string, 0, len(table.Columns))
			for _, c := range table.Columns {
				got = append(got, c.Name)
			}
			if diff := cmp.Diff(tt.wantCols, got); diff != "" {
				t.Errorf("columns mismatch (-want +got):\n%s", diff)
			}
			if tt.wantFK != "" {
				if c := parser.ColumnByName(&table, tt.wantFK); c == nil || c.FKReference == nil {
					t.Errorf("column %s: want FKReference, got %+v", tt.wantFK, c)
				}
			}
		})
	}
}

// SQLite keeps a quoted type name in a form the parser does not reproduce,
// so a column declared with one is an error; an unquoted type of several
// words, or with a modifier, reads as written.
func TestCreateTable_QuotedTypeName(t *testing.T) {
	tests := []struct {
		name     string
		sql      string
		wantErr  string
		wantType string
	}{
		{
			name:    "double-quoted type name",
			sql:     `CREATE TABLE t (id INTEGER PRIMARY KEY, f "my type");`,
			wantErr: "table t: column f: 1:43: a quoted type name cannot be read",
		},
		{
			name:    "backquoted type name",
			sql:     "CREATE TABLE t (id INTEGER PRIMARY KEY, f `my type` NOT NULL, g TEXT);",
			wantErr: "table t: column f: 1:43: a quoted type name cannot be read",
		},
		{
			name:    "string type name",
			sql:     `CREATE TABLE t (id INTEGER PRIMARY KEY, f 'text');`,
			wantErr: "table t: column f: 1:43: a quoted type name cannot be read",
		},
		{
			name:    "bracketed type name",
			sql:     `CREATE TABLE t (id INTEGER PRIMARY KEY, f [text]);`,
			wantErr: "table t: column f: 1:43: a quoted type name cannot be read",
		},
		{
			name:    "quoted type name with a modifier",
			sql:     `CREATE TABLE t (id INTEGER PRIMARY KEY, f "varchar"(10));`,
			wantErr: "table t: column f: 1:43: a quoted type name cannot be read",
		},
		{
			name:    "quoted word after a type name",
			sql:     `CREATE TABLE t (id INTEGER PRIMARY KEY, f INT "x");`,
			wantErr: "table t: column f: 1:43: a quoted type name cannot be read",
		},
		{
			name:    "added column",
			sql:     `CREATE TABLE t (id INTEGER PRIMARY KEY); ALTER TABLE t ADD COLUMN f "text";`,
			wantErr: "table t: column f: 1:69: a quoted type name cannot be read",
		},
		{
			name:     "words",
			sql:      `CREATE TABLE t (id INTEGER PRIMARY KEY, f unsigned  big /* x */ int NOT NULL);`,
			wantType: "unsigned big int",
		},
		{
			name:     "modifier",
			sql:      `CREATE TABLE t (id INTEGER PRIMARY KEY, f varchar (10) DEFAULT 'x');`,
			wantType: "varchar(10)",
		},
		{
			name:     "quoted column name",
			sql:      `CREATE TABLE t (id INTEGER PRIMARY KEY, "f" TEXT COLLATE "NOCASE");`,
			wantType: "text",
		},
		{
			name:     "comments holding commas between columns",
			sql:      "CREATE TABLE t (id INTEGER PRIMARY KEY, -- a, b\n /* c, d */ f TEXT);",
			wantType: "text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := sqlite.New()
			err := p.Parse("test.sql", []byte(tt.sql))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Parse(%q) error = %v, want it to contain %q", tt.sql, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q) unexpected error: %v", tt.sql, err)
			}
			table := p.Schema().Tables[0]
			if got := columnNamesOf(table); !cmp.Equal(got, []string{"id", "f"}) {
				t.Fatalf("Parse(%q) columns = %v, want [id f]", tt.sql, got)
			}
			if got := parser.ColumnByName(&table, "f").Type; got != tt.wantType {
				t.Errorf("Parse(%q) f type = %q, want %q", tt.sql, got, tt.wantType)
			}
		})
	}
}

func columnNamesOf(table parser.Table) []string {
	names := make([]string, 0, len(table.Columns))
	for _, c := range table.Columns {
		names = append(names, c.Name)
	}
	return names
}

// DDL the parser reads as SQLite does: a clause that does not change the
// model (ON CONFLICT, MATCH), bracket-quoted names, and a sort order or
// collation on a table-level key column.
func TestCreateTable_ClausesReadAsSQLite(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want []parser.Constraint
	}{
		{
			name: "ON CONFLICT",
			sql:  `CREATE TABLE t (a INTEGER PRIMARY KEY ON CONFLICT REPLACE, b TEXT NOT NULL ON CONFLICT IGNORE UNIQUE ON CONFLICT ABORT, c TEXT, UNIQUE (c) ON CONFLICT FAIL);`,
			want: []parser.Constraint{
				{Type: parser.PrimaryKey, Columns: []string{"a"}},
				{Type: parser.Unique, Columns: []string{"b"}},
				{Type: parser.Unique, Columns: []string{"c"}},
			},
		},
		{
			name: "table-level ON CONFLICT",
			sql:  `CREATE TABLE t (a INTEGER, b TEXT, PRIMARY KEY (a, b) ON CONFLICT ROLLBACK);`,
			want: []parser.Constraint{{Type: parser.PrimaryKey, Columns: []string{"a", "b"}}},
		},
		{
			name: "MATCH",
			sql:  `CREATE TABLE p (id INTEGER PRIMARY KEY); CREATE TABLE t (a INTEGER REFERENCES p(id) MATCH SIMPLE, b INTEGER, FOREIGN KEY (b) REFERENCES p(id) MATCH FULL);`,
			want: []parser.Constraint{
				{Type: parser.ForeignKey, Columns: []string{"a"}, ReferenceTable: "p", ReferenceColumns: []string{"id"}},
				{Type: parser.ForeignKey, Columns: []string{"b"}, ReferenceTable: "p", ReferenceColumns: []string{"id"}},
			},
		},
		{
			name: "bracket-quoted names",
			sql:  `CREATE TABLE [p] ([Y] INTEGER PRIMARY KEY); CREATE TABLE [t] ([a b] TEXT, [x] INTEGER REFERENCES [p]([Y]));`,
			want: []parser.Constraint{{Type: parser.ForeignKey, Columns: []string{"x"}, ReferenceTable: "p", ReferenceColumns: []string{"Y"}}},
		},
		{
			name: "table-level key sort order and collation",
			sql:  `CREATE TABLE t (a TEXT, b TEXT, PRIMARY KEY (a DESC, b COLLATE NOCASE));`,
			want: []parser.Constraint{{Type: parser.PrimaryKey, Columns: []string{"a", "b"}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := sqlite.New()
			if err := p.Parse("test.sql", []byte(tt.sql)); err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			tables := p.Schema().Tables
			got := tables[len(tables)-1].Constraints
			if diff := cmp.Diff(tt.want, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("constraints mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// SQLite rejects an expression in a PRIMARY KEY; read as written it would be
// a key over no columns.
func TestCreateTable_PrimaryKeyExpression(t *testing.T) {
	for _, sql := range []string{
		`CREATE TABLE t (a TEXT, PRIMARY KEY (lower(a)));`,
		`CREATE TABLE t (a INTEGER, PRIMARY KEY (a, a + 1));`,
	} {
		err := sqlite.New().Parse("test.sql", []byte(sql))
		const want = "parsing test.sql: table t: PRIMARY KEY has an expression part: expressions prohibited in PRIMARY KEY"
		if err == nil || err.Error() != want {
			t.Errorf("Parse(%q) error = %v, want %q", sql, err, want)
		}
	}
}

func TestIntegerPrimaryKeyWithoutRowID(t *testing.T) {
	sql := `CREATE TABLE settings (
		key TEXT PRIMARY KEY,
		value TEXT
	) WITHOUT ROWID;`

	p := sqlite.New()
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	table := p.Schema().Tables[0]
	if !table.WithoutRowID {
		t.Error("table should have WithoutRowID=true")
	}

	cols := make(map[string]parser.Column)
	for _, c := range table.Columns {
		cols[c.Name] = c
	}

	// In WITHOUT ROWID tables, PRIMARY KEY is NOT auto-increment.
	if cols["key"].AutoIncrement {
		t.Error("key in WITHOUT ROWID table should have AutoIncrement=false")
	}
	if !cols["key"].PrimaryKey {
		t.Error("key should have PrimaryKey=true")
	}

	// Also verify with an INTEGER PRIMARY KEY in WITHOUT ROWID.
	sql2 := `CREATE TABLE counters (
		id INTEGER PRIMARY KEY,
		count INTEGER
	) WITHOUT ROWID;`

	p2 := sqlite.New()
	if err := p2.Parse("test.sql", []byte(sql2)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	table2 := p2.Schema().Tables[0]
	cols2 := make(map[string]parser.Column)
	for _, c := range table2.Columns {
		cols2[c.Name] = c
	}

	// INTEGER PRIMARY KEY in WITHOUT ROWID is NOT auto-increment.
	if cols2["id"].AutoIncrement {
		t.Error("INTEGER PRIMARY KEY in WITHOUT ROWID table should have AutoIncrement=false")
	}
}

// Only a primary key that aliases the rowid gets an automatic value, and it is
// the only one SQLite builds no index for, so a UNIQUE on it is kept.
func TestRowIDAlias(t *testing.T) {
	tests := []struct {
		name          string
		sql           string
		wantAutoInc   bool
		wantUniqueCnt int
	}{
		{name: "column key", sql: `CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT);`, wantAutoInc: true},
		{name: "lower-case type", sql: `CREATE TABLE t (id integer primary key, v TEXT);`, wantAutoInc: true},
		{name: "ascending key", sql: `CREATE TABLE t (id INTEGER PRIMARY KEY ASC, v TEXT);`, wantAutoInc: true},
		{name: "AUTOINCREMENT", sql: `CREATE TABLE t (id INTEGER PRIMARY KEY AUTOINCREMENT, v TEXT);`, wantAutoInc: true},
		{name: "table-level key", sql: `CREATE TABLE t (id INTEGER, v TEXT, PRIMARY KEY (id));`, wantAutoInc: true},
		{name: "ascending AUTOINCREMENT", sql: `CREATE TABLE t (id INTEGER PRIMARY KEY ASC AUTOINCREMENT, v TEXT);`, wantAutoInc: true},
		{name: "ON CONFLICT", sql: `CREATE TABLE t (id INTEGER PRIMARY KEY ON CONFLICT REPLACE, v TEXT);`, wantAutoInc: true},
		{name: "descending key", sql: `CREATE TABLE t (id INTEGER PRIMARY KEY DESC, v TEXT);`},
		{name: "descending key with ON CONFLICT", sql: `CREATE TABLE t (id INTEGER PRIMARY KEY DESC ON CONFLICT FAIL, v TEXT);`},
		// Only the column form is the exception; a table-level key aliases
		// the rowid whatever its sort order.
		{name: "table-level descending key", sql: `CREATE TABLE t (id INTEGER, v TEXT, PRIMARY KEY (id DESC));`, wantAutoInc: true},
		{name: "WITHOUT ROWID", sql: `CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT) WITHOUT ROWID;`},
		{name: "INT is not INTEGER", sql: `CREATE TABLE t (id INT PRIMARY KEY, v TEXT);`},
		{name: "composite key", sql: `CREATE TABLE t (id INTEGER, v INTEGER, PRIMARY KEY (id, v));`},
		{name: "UNIQUE on a rowid alias is kept", sql: `CREATE TABLE t (id INTEGER PRIMARY KEY UNIQUE, v TEXT);`, wantAutoInc: true, wantUniqueCnt: 1},
		{name: "UNIQUE on a descending key is dropped", sql: `CREATE TABLE t (id INTEGER PRIMARY KEY DESC UNIQUE, v TEXT);`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := sqlite.New()
			if err := p.Parse("test.sql", []byte(tt.sql)); err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			table := p.Schema().Tables[0]
			if got := parser.ColumnByName(&table, "id").AutoIncrement; got != tt.wantAutoInc {
				t.Errorf("id AutoIncrement = %t, want %t", got, tt.wantAutoInc)
			}
			if got := len(filterUniques(table.Constraints)); got != tt.wantUniqueCnt {
				t.Errorf("UNIQUE constraints = %d, want %d: %+v", got, tt.wantUniqueCnt, table.Constraints)
			}
		})
	}
}

func filterUniques(cs []parser.Constraint) []parser.Constraint {
	var out []parser.Constraint
	for _, c := range cs {
		if c.Type == parser.Unique {
			out = append(out, c)
		}
	}
	return out
}

func TestGeneratedColumnStored(t *testing.T) {
	sql := `CREATE TABLE products (
		price REAL,
		qty INTEGER,
		total REAL GENERATED ALWAYS AS (price * qty) STORED
	);`

	p := sqlite.New()
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	table := p.Schema().Tables[0]
	cols := make(map[string]parser.Column)
	for _, c := range table.Columns {
		cols[c.Name] = c
	}

	total := cols["total"]
	if total.GeneratedExpr == "" {
		t.Error("total GeneratedExpr is empty, want non-empty")
	}
	if total.GeneratedStorage != "STORED" {
		t.Errorf("total GeneratedStorage = %q, want %q", total.GeneratedStorage, "STORED")
	}

	// Non-generated columns should have empty generated fields.
	if cols["price"].GeneratedExpr != "" {
		t.Error("price should not have GeneratedExpr")
	}
	if cols["price"].GeneratedStorage != "" {
		t.Error("price should not have GeneratedStorage")
	}
}

func TestGeneratedColumnVirtualShorthand(t *testing.T) {
	sql := `CREATE TABLE products (
		price REAL,
		qty INTEGER,
		total REAL AS (price * qty)
	);`

	p := sqlite.New()
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	table := p.Schema().Tables[0]
	cols := make(map[string]parser.Column)
	for _, c := range table.Columns {
		cols[c.Name] = c
	}

	total := cols["total"]
	if total.GeneratedExpr == "" {
		t.Error("total GeneratedExpr is empty, want non-empty")
	}
	// Shorthand AS defaults to VIRTUAL.
	if total.GeneratedStorage != "VIRTUAL" {
		t.Errorf("total GeneratedStorage = %q, want %q", total.GeneratedStorage, "VIRTUAL")
	}
}

func TestStrictTableModifier(t *testing.T) {
	sql := `CREATE TABLE strict_types (
		id INTEGER PRIMARY KEY,
		name TEXT NOT NULL
	) STRICT;`

	p := sqlite.New()
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	table := p.Schema().Tables[0]
	if !table.Strict {
		t.Error("table should have Strict=true")
	}
	if table.WithoutRowID {
		t.Error("table should have WithoutRowID=false")
	}
}

func TestWithoutRowIDTableModifier(t *testing.T) {
	sql := `CREATE TABLE kv (
		key TEXT PRIMARY KEY,
		value BLOB
	) WITHOUT ROWID;`

	p := sqlite.New()
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	table := p.Schema().Tables[0]
	if !table.WithoutRowID {
		t.Error("table should have WithoutRowID=true")
	}
	if table.Strict {
		t.Error("table should have Strict=false")
	}
}

func TestAlterTableAddColumn(t *testing.T) {
	sql := `
		CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);
		ALTER TABLE users ADD COLUMN email TEXT;
	`

	p := sqlite.New()
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	table := p.Schema().Tables[0]
	colNames := make([]string, 0, len(table.Columns))
	for _, c := range table.Columns {
		colNames = append(colNames, c.Name)
	}

	wantCols := []string{"id", "name", "email"}
	if diff := cmp.Diff(wantCols, colNames); diff != "" {
		t.Errorf("columns after ADD COLUMN mismatch (-want +got):\n%s", diff)
	}

	// Verify added column properties.
	for _, c := range table.Columns {
		if c.Name == "email" {
			if c.Type != "text" {
				t.Errorf("email type = %q, want %q", c.Type, "text")
			}
			if !c.Nullable {
				t.Error("email should be nullable (no NOT NULL specified)")
			}
		}
	}
}

func TestAlterTableDropColumn(t *testing.T) {
	sql := `
		CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT);
		ALTER TABLE users DROP COLUMN name;
	`

	p := sqlite.New()
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	table := p.Schema().Tables[0]
	colNames := make([]string, 0, len(table.Columns))
	for _, c := range table.Columns {
		colNames = append(colNames, c.Name)
	}

	wantCols := []string{"id", "email"}
	if diff := cmp.Diff(wantCols, colNames); diff != "" {
		t.Errorf("columns after DROP COLUMN mismatch (-want +got):\n%s", diff)
	}
}

// TestAlterTableRenameColumn pins that RENAME COLUMN renames the column and
// every reference to it in the table's CHECK, generated-column and
// partial-index expressions, as SQLite rewrites its stored DDL (measured on
// 3.51). Function, type, collation and table names, and string literals, are
// left alone.
func TestAlterTableRenameColumn(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want []string
	}{
		{
			name: "column only",
			sql: `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);
				ALTER TABLE users RENAME COLUMN name TO display_name;`,
			want: []string{"column id", "column display_name"},
		},
		{
			name: "check expressions",
			sql: `CREATE TABLE t (a INTEGER CHECK (a > 0), b TEXT, CHECK (a < length(b)), CONSTRAINT lit CHECK (b <> 'a'));
				ALTER TABLE t RENAME COLUMN a TO qty;`,
			want: []string{
				"column qty",
				"column b",
				`CHECK [qty] "qty" > 0`,
				`CHECK [qty b] "qty" < length("b")`,
				`CHECK lit [b] "b" != 'a'`,
			},
		},
		{
			name: "generated column expressions",
			sql: `CREATE TABLE g (a INTEGER, d INTEGER AS (a * 2), e INTEGER GENERATED ALWAYS AS (abs(a) + 1) STORED);
				ALTER TABLE g RENAME COLUMN a TO qty;`,
			want: []string{"column qty", `column d AS "qty" * 2`, `column e AS abs("qty") + 1`},
		},
		{
			name: "partial index predicate",
			sql: `CREATE TABLE p (a INTEGER, b INTEGER);
				CREATE INDEX p_b ON p (b) WHERE a > 0 AND a IS NOT NULL AND b IS NULL;
				ALTER TABLE p RENAME COLUMN a TO qty;`,
			want: []string{"column qty", "column b", `INDEX p_b [b] WHERE "qty" > 0 AND "qty" NOT NULL AND "b" IS NULL`},
		},
		{
			name: "names match without regard to case",
			sql: `CREATE TABLE k (A INTEGER CHECK ("a" > 0), b INTEGER AS (a + 1));
				ALTER TABLE k RENAME COLUMN A TO qty;`,
			want: []string{"column qty", `column b AS "qty" + 1`, `CHECK [qty] "qty" > 0`},
		},
		{
			name: "only ASCII letters fold",
			sql: `CREATE TABLE u ("É" INTEGER, "é" INTEGER, g INTEGER AS ("é" + 1));
				ALTER TABLE u RENAME COLUMN "É" TO qty;`,
			want: []string{"column qty", "column é", `column g AS "é" + 1`},
		},
		{
			name: "table-qualified reference",
			sql: `CREATE TABLE q (a INTEGER, CHECK (q.a > 0));
				ALTER TABLE q RENAME COLUMN a TO qty;`,
			want: []string{"column qty", `CHECK [qty] "q"."qty" > 0`},
		},
		{
			name: "function type and collation names are not columns",
			sql: `CREATE TABLE f (upper TEXT, CHECK (upper(upper) <> CAST(upper AS TEXT) COLLATE NOCASE));
				ALTER TABLE f RENAME COLUMN upper TO label;`,
			want: []string{"column label", `CHECK [label] upper("label") != CAST("label" AS TEXT) COLLATE "NOCASE"`},
		},
		{
			name: "time keyword is not the column it spells",
			sql: `CREATE TABLE c (x TEXT, "current_date" TEXT, CHECK (x > CURRENT_DATE AND "current_date" <> ''));
				ALTER TABLE c RENAME COLUMN "current_date" TO d;`,
			want: []string{"column x", "column d", `CHECK [x d] "x" > CURRENT_DATE AND "d" != ''`},
		},
		{
			name: "other column's name left alone",
			sql: `CREATE TABLE o (a INTEGER, b INTEGER CHECK (b > 0));
				ALTER TABLE o RENAME COLUMN a TO qty;`,
			want: []string{"column qty", "column b", `CHECK [b] "b" > 0`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := sqlite.New()
			if err := p.Parse("test.sql", []byte(tt.sql)); err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			table := p.Schema().Tables[0]
			var got []string
			for _, c := range table.Columns {
				line := "column " + c.Name
				if c.GeneratedExpr != "" {
					line += " AS " + c.GeneratedExpr
				}
				got = append(got, line)
			}
			for _, c := range table.Constraints {
				switch {
				case c.Type == parser.Check:
					got = append(got, strings.TrimSpace(fmt.Sprintf("CHECK %s", c.Name))+fmt.Sprintf(" %v %s", c.Columns, c.CheckExpression))
				case c.Where != "":
					got = append(got, fmt.Sprintf("%s %s %v WHERE %s", c.Type, c.Name, c.Columns, c.Where))
				}
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("schema after RENAME COLUMN mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAlterTableRenameTable(t *testing.T) {
	sql := `
		CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);
		ALTER TABLE users RENAME TO accounts;
	`

	p := sqlite.New()
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	schema := p.Schema()
	if len(schema.Tables) != 1 {
		t.Fatalf("tables = %d, want 1", len(schema.Tables))
	}
	if schema.Tables[0].Name != "accounts" {
		t.Errorf("table name = %q, want %q", schema.Tables[0].Name, "accounts")
	}
}

// TestAlterTableRenameTableCarriesForeignKeys pins that a foreign key into a
// renamed table follows it to the new name, as SQLite does (measured on 3.51:
// sqlite_master rewrites the reference to REFERENCES "accounts"). Left on the
// old name, relationship detection drops the edge.
func TestAlterTableRenameTableCarriesForeignKeys(t *testing.T) {
	sql := `
		CREATE TABLE users (id INTEGER PRIMARY KEY);
		CREATE TABLE posts (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id));
		CREATE TABLE comments (id INTEGER PRIMARY KEY, user_id INTEGER, FOREIGN KEY (user_id) REFERENCES users(id));
		ALTER TABLE users RENAME TO accounts;
	`

	p := sqlite.New()
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	var got []string
	for _, tbl := range p.Schema().Tables {
		for _, col := range tbl.Columns {
			if col.FKReference != nil {
				got = append(got, tbl.Name+"."+col.Name+" -> "+col.FKReference.Table)
			}
		}
		for _, c := range tbl.Constraints {
			if c.Type == parser.ForeignKey {
				got = append(got, tbl.Name+" constraint -> "+c.ReferenceTable)
			}
		}
	}
	want := []string{
		"posts.user_id -> accounts",
		"posts constraint -> accounts",
		"comments.user_id -> accounts",
		"comments constraint -> accounts",
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("FK targets after RENAME TO mismatch (-want +got):\n%s", diff)
	}
}

// TestAlterTableRenameColumnCarriesReferences pins that RENAME COLUMN carries
// the constraints and foreign keys naming the column along, as SQLite does
// (measured on 3.51: sqlite_schema rewrites PRIMARY KEY (user_id) and
// REFERENCES users(user_id) after the rename).
func TestAlterTableRenameColumnCarriesReferences(t *testing.T) {
	const tables = `CREATE TABLE users (id INTEGER, PRIMARY KEY (id));
		CREATE TABLE posts (id INTEGER PRIMARY KEY, user_id INTEGER, FOREIGN KEY (user_id) REFERENCES users(id));
		CREATE TABLE post_tags (post_id INTEGER REFERENCES posts(id), tag_id INTEGER, PRIMARY KEY (post_id, tag_id));
		CREATE TABLE members (id INTEGER PRIMARY KEY, org_id INTEGER, email TEXT, UNIQUE (org_id, email));
	`
	tests := []struct {
		name  string
		alter string
		want  []string
	}{
		{
			name:  "referenced primary key column",
			alter: "ALTER TABLE users RENAME COLUMN id TO user_id;",
			want: []string{
				"users PRIMARY KEY [user_id]",
				"posts.user_id -> users(user_id)",
				"posts PRIMARY KEY [id]",
				"posts FOREIGN KEY [user_id] -> users[user_id]",
				"post_tags.post_id -> posts(id)",
				"post_tags FOREIGN KEY [post_id] -> posts[id]",
				"post_tags PRIMARY KEY [post_id tag_id]",
				"members PRIMARY KEY [id]",
				"members UNIQUE [org_id email]",
			},
		},
		{
			name:  "junction key column",
			alter: "ALTER TABLE post_tags RENAME COLUMN post_id TO article_id;",
			want: []string{
				"users PRIMARY KEY [id]",
				"posts.user_id -> users(id)",
				"posts PRIMARY KEY [id]",
				"posts FOREIGN KEY [user_id] -> users[id]",
				"post_tags.article_id -> posts(id)",
				"post_tags FOREIGN KEY [article_id] -> posts[id]",
				"post_tags PRIMARY KEY [article_id tag_id]",
				"members PRIMARY KEY [id]",
				"members UNIQUE [org_id email]",
			},
		},
		{
			// A composite UNIQUE left on the old name makes the generated
			// upsert conflict target name a column that no longer exists.
			name:  "composite unique member column",
			alter: "ALTER TABLE members RENAME COLUMN email TO login;",
			want: []string{
				"users PRIMARY KEY [id]",
				"posts.user_id -> users(id)",
				"posts PRIMARY KEY [id]",
				"posts FOREIGN KEY [user_id] -> users[id]",
				"post_tags.post_id -> posts(id)",
				"post_tags FOREIGN KEY [post_id] -> posts[id]",
				"post_tags PRIMARY KEY [post_id tag_id]",
				"members PRIMARY KEY [id]",
				"members UNIQUE [org_id login]",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := sqlite.New()
			if err := p.Parse("test.sql", []byte(tables+tt.alter)); err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			var got []string
			for _, tbl := range p.Schema().Tables {
				for _, col := range tbl.Columns {
					if ref := col.FKReference; ref != nil {
						got = append(got, fmt.Sprintf("%s.%s -> %s(%s)", tbl.Name, col.Name, ref.Table, ref.Column))
					}
				}
				for _, c := range tbl.Constraints {
					s := fmt.Sprintf("%s %s %v", tbl.Name, c.Type, c.Columns)
					if c.Type == parser.ForeignKey {
						s += fmt.Sprintf(" -> %s%v", c.ReferenceTable, c.ReferenceColumns)
					}
					got = append(got, s)
				}
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("references after RENAME COLUMN mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTableWithNoPK(t *testing.T) {
	sql := `CREATE TABLE logs (
		message TEXT,
		created_at DATETIME
	);`

	p := sqlite.New()
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	table := p.Schema().Tables[0]
	for _, col := range table.Columns {
		if col.PrimaryKey {
			t.Errorf("column %q should not be PrimaryKey", col.Name)
		}
		if col.AutoIncrement {
			t.Errorf("column %q should not be AutoIncrement", col.Name)
		}
	}

	for _, c := range table.Constraints {
		if c.Type == parser.PrimaryKey {
			t.Error("table should have no PrimaryKey constraint")
		}
	}
}

func TestUnsupportedAlterTableOperation(t *testing.T) {
	// rqlite/sql only parses the 4 supported ALTER TABLE operations.
	// Anything else should fail at the parser level.
	sql := `ALTER TABLE users MODIFY COLUMN name VARCHAR(512);`

	p := sqlite.New()
	err := p.Parse("test.sql", []byte(sql))
	if err == nil {
		t.Error("Parse() should error on unsupported ALTER TABLE operation")
	}
}

func TestParserReentrant(t *testing.T) {
	sql1 := `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);`
	sql2 := `CREATE TABLE posts (id INTEGER PRIMARY KEY, user_id INTEGER, title TEXT);`

	p := sqlite.New()
	if err := p.Parse("001_users.sql", []byte(sql1)); err != nil {
		t.Fatalf("Parse(001) error: %v", err)
	}
	if err := p.Parse("002_posts.sql", []byte(sql2)); err != nil {
		t.Fatalf("Parse(002) error: %v", err)
	}

	schema := p.Schema()
	if len(schema.Tables) != 2 {
		t.Fatalf("tables = %d, want 2", len(schema.Tables))
	}
	if schema.Tables[0].Name != "users" {
		t.Errorf("table[0] = %q, want %q", schema.Tables[0].Name, "users")
	}
	if schema.Tables[1].Name != "posts" {
		t.Errorf("table[1] = %q, want %q", schema.Tables[1].Name, "posts")
	}
}

// A foreign key written with no column list references its parent's primary
// key, as SQLite reads it, whichever file defines the parent.
func TestImplicitForeignKeyTarget(t *testing.T) {
	tests := []struct {
		name    string
		files   []string
		table   string
		want    []string // the foreign key's ReferenceColumns
		wantRef string   // the FKReference.Column of column c; "" wants no FKReference
	}{
		{
			name:    "column REFERENCES",
			files:   []string{`CREATE TABLE p (id INTEGER PRIMARY KEY); CREATE TABLE t (c INTEGER REFERENCES p);`},
			table:   "t",
			want:    []string{"id"},
			wantRef: "id",
		},
		{
			name:    "table FOREIGN KEY",
			files:   []string{`CREATE TABLE p (id TEXT PRIMARY KEY); CREATE TABLE t (c TEXT, FOREIGN KEY (c) REFERENCES p);`},
			table:   "t",
			want:    []string{"id"},
			wantRef: "id",
		},
		{
			name:  "composite key in key order",
			files: []string{`CREATE TABLE p (a INT, b INT, PRIMARY KEY (b, a)); CREATE TABLE t (x INT, y INT, FOREIGN KEY (x, y) REFERENCES p);`},
			table: "t",
			want:  []string{"b", "a"},
		},
		{
			name:    "self reference",
			files:   []string{`CREATE TABLE t (id INTEGER PRIMARY KEY, c INTEGER REFERENCES t);`},
			table:   "t",
			want:    []string{"id"},
			wantRef: "id",
		},
		{
			name:    "parent defined later",
			files:   []string{`CREATE TABLE t (c INTEGER REFERENCES p);`, `CREATE TABLE p (id INTEGER PRIMARY KEY);`},
			table:   "t",
			want:    []string{"id"},
			wantRef: "id",
		},
		{
			name:    "parent key renamed",
			files:   []string{`CREATE TABLE p (id INTEGER PRIMARY KEY); CREATE TABLE t (c INTEGER REFERENCES p);`, `ALTER TABLE p RENAME COLUMN id TO pid;`},
			table:   "t",
			want:    []string{"pid"},
			wantRef: "pid",
		},
		{
			name:  "parent without a primary key",
			files: []string{`CREATE TABLE p (id INTEGER); CREATE TABLE t (c INTEGER REFERENCES p);`},
			table: "t",
		},
		{
			name:  "key of another size",
			files: []string{`CREATE TABLE p (a INT, b INT, PRIMARY KEY (a, b)); CREATE TABLE t (c INTEGER REFERENCES p);`},
			table: "t",
		},
		{
			name:  "parent not defined",
			files: []string{`CREATE TABLE t (c INTEGER REFERENCES p);`},
			table: "t",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := sqlite.New()
			for i, sql := range tt.files {
				if err := p.Parse(fmt.Sprintf("%d.sql", i), []byte(sql)); err != nil {
					t.Fatalf("Parse(%q) error: %v", sql, err)
				}
			}
			var table *parser.Table
			for i := range p.Schema().Tables {
				if p.Schema().Tables[i].Name == tt.table {
					table = &p.Schema().Tables[i]
				}
			}
			var fks []parser.Constraint
			for _, c := range table.Constraints {
				if c.Type == parser.ForeignKey {
					fks = append(fks, c)
				}
			}
			if len(fks) != 1 {
				t.Fatalf("foreign keys = %+v, want 1", fks)
			}
			if diff := cmp.Diff(tt.want, fks[0].ReferenceColumns, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("ReferenceColumns mismatch (-want +got):\n%s", diff)
			}
			var ref *parser.FKReference
			if c := parser.ColumnByName(table, "c"); c != nil {
				ref = c.FKReference
			}
			switch {
			case tt.wantRef == "" && ref != nil:
				t.Errorf("FKReference = %+v, want nil", *ref)
			case tt.wantRef != "" && (ref == nil || ref.Column != tt.wantRef):
				t.Errorf("FKReference = %+v, want column %q", ref, tt.wantRef)
			}
		})
	}
}

func TestTableLevelConstraints(t *testing.T) {
	sql := `CREATE TABLE order_items (
		order_id INTEGER,
		product_id INTEGER,
		quantity INTEGER,
		PRIMARY KEY (order_id, product_id),
		UNIQUE (order_id, quantity),
		FOREIGN KEY (order_id) REFERENCES orders(id),
		CHECK (quantity > 0)
	);`

	p := sqlite.New()
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	table := p.Schema().Tables[0]

	// Composite PK columns should be marked.
	cols := make(map[string]parser.Column)
	for _, c := range table.Columns {
		cols[c.Name] = c
	}
	if !cols["order_id"].PrimaryKey || !cols["product_id"].PrimaryKey {
		t.Error("composite PK columns should have PrimaryKey=true")
	}

	// FK should be propagated to column.
	if cols["order_id"].FKReference == nil {
		t.Error("order_id FKReference should be set from table-level FK constraint")
	}

	// Verify constraint types exist.
	var hasPK, hasUnique, hasFK, hasCheck bool
	for _, c := range table.Constraints {
		switch c.Type {
		case parser.PrimaryKey:
			hasPK = true
			if diff := cmp.Diff([]string{"order_id", "product_id"}, c.Columns); diff != "" {
				t.Errorf("PK columns mismatch (-want +got):\n%s", diff)
			}
		case parser.Unique:
			hasUnique = true
		case parser.ForeignKey:
			hasFK = true
			if c.ReferenceTable != "orders" {
				t.Errorf("FK reference table = %q, want %q", c.ReferenceTable, "orders")
			}
		case parser.Check:
			hasCheck = true
			if c.CheckExpression == "" {
				t.Error("CHECK expression is empty")
			}
		case parser.Index:
			// Not tested here.
		}
	}

	if !hasPK {
		t.Error("missing PRIMARY KEY constraint")
	}
	if !hasUnique {
		t.Error("missing UNIQUE constraint")
	}
	if !hasFK {
		t.Error("missing FOREIGN KEY constraint")
	}
	if !hasCheck {
		t.Error("missing CHECK constraint")
	}
}

func TestDuplicateCreateTable(t *testing.T) {
	p := sqlite.New()
	sql := `
		CREATE TABLE users (id INTEGER PRIMARY KEY);
		CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);
	`
	err := p.Parse("test.sql", []byte(sql))
	if err == nil {
		t.Fatal("expected error for duplicate CREATE TABLE, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should mention 'already exists', got: %v", err)
	}
}

// SQLite matches table names case-insensitively, folding ASCII letters only.
// Each table is summarized as its name, its columns, and its named
// constraints and foreign-key targets.
func TestTableNameCase(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		want    []string
		wantErr string
	}{
		{
			name: "CREATE INDEX",
			sql:  `CREATE TABLE Users (id INTEGER PRIMARY KEY, a TEXT); CREATE UNIQUE INDEX ux2 ON users (a);`,
			want: []string{"Users(id, a) ux2"},
		},
		{
			name: "ALTER TABLE ADD COLUMN",
			sql:  `CREATE TABLE Users (id INTEGER PRIMARY KEY); ALTER TABLE USERS ADD COLUMN b TEXT;`,
			want: []string{"Users(id, b)"},
		},
		{
			name: "ALTER TABLE DROP COLUMN",
			sql:  `CREATE TABLE Users (id INTEGER PRIMARY KEY, b TEXT); ALTER TABLE users DROP COLUMN b;`,
			want: []string{"Users(id)"},
		},
		{
			name: "ALTER TABLE RENAME COLUMN",
			sql:  `CREATE TABLE Users (id INTEGER PRIMARY KEY, b TEXT); ALTER TABLE users RENAME COLUMN b TO c;`,
			want: []string{"Users(id, c)"},
		},
		{
			name: "RENAME TO carries the foreign keys",
			sql: `CREATE TABLE Users (id INTEGER PRIMARY KEY);
				CREATE TABLE posts (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES Users(id));
				ALTER TABLE users RENAME TO Accounts;
				ALTER TABLE ACCOUNTS ADD COLUMN email TEXT;`,
			want: []string{"Accounts(id, email)", "posts(id, user_id) ->Accounts"},
		},
		{
			name: "DROP TABLE",
			sql:  `CREATE TABLE Users (id INTEGER PRIMARY KEY); CREATE TABLE t (id INTEGER PRIMARY KEY); DROP TABLE users;`,
			want: []string{"t(id)"},
		},
		{
			name: "DROP TABLE referenced by a foreign key",
			sql: `CREATE TABLE Users (id INTEGER PRIMARY KEY);
				CREATE TABLE posts (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES Users(id));
				DROP TABLE users;`,
			wantErr: "drop table users: cannot drop table Users",
		},
		{
			name:    "duplicate CREATE TABLE",
			sql:     `CREATE TABLE Users (id INTEGER PRIMARY KEY); CREATE TABLE users (id INTEGER PRIMARY KEY);`,
			wantErr: "table users already exists",
		},
		{
			name: "CREATE TABLE IF NOT EXISTS",
			sql:  `CREATE TABLE Users (id INTEGER PRIMARY KEY); CREATE TABLE IF NOT EXISTS users (x INTEGER PRIMARY KEY);`,
			want: []string{"Users(id)"},
		},
		{
			name: "non-ASCII letters keep their case",
			sql:  `CREATE TABLE "Äb" (id INTEGER PRIMARY KEY); CREATE TABLE "äb" (id INTEGER PRIMARY KEY); CREATE INDEX ix ON "äb" (id);`,
			want: []string{"Äb(id)", "äb(id) ix"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := sqlite.New()
			err := p.Parse("test.sql", []byte(tt.sql))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("Parse() error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			var got []string
			for _, tbl := range p.Schema().Tables {
				s := tbl.Name + "("
				for i, c := range tbl.Columns {
					if i > 0 {
						s += ", "
					}
					s += c.Name
				}
				s += ")"
				for _, c := range tbl.Constraints {
					switch {
					case c.Type == parser.ForeignKey:
						s += " ->" + c.ReferenceTable
					case c.Name != "":
						s += " " + c.Name
					}
				}
				got = append(got, s)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Parse() tables mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestIdentifierCase pins that the parser matches table and column names as
// SQLite does, ignoring ASCII case only, and refuses what SQLite refuses
// (measured with modernc.org/sqlite, legacy_alter_table off).
func TestIdentifierCase(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		want    []string
		wantErr string
	}{
		{
			name: "RENAME TO carries a reference written in another case",
			sql: `CREATE TABLE Users (id INTEGER PRIMARY KEY);
				CREATE TABLE posts (uid INTEGER REFERENCES users(id));
				ALTER TABLE Users RENAME TO Accounts;`,
			want: []string{"Accounts(id) PK[id]", "posts(uid->Accounts.id) FK[uid]->Accounts[id]"},
		},
		{
			name: "DROP TABLE referenced in another case",
			sql: `CREATE TABLE Users (id INTEGER PRIMARY KEY);
				CREATE TABLE posts (uid INTEGER REFERENCES users(id));
				DROP TABLE Users;`,
			wantErr: "cannot drop table Users",
		},
		{
			name: "RENAME COLUMN carries a reference written in another case",
			sql: `CREATE TABLE u (b INTEGER PRIMARY KEY);
				CREATE TABLE c (x INTEGER REFERENCES u(B));
				ALTER TABLE u RENAME COLUMN b TO c;`,
			want: []string{"u(c) PK[c]", "c(x->u.c) FK[x]->u[c]"},
		},
		{
			name: "DROP COLUMN in another case",
			sql:  `CREATE TABLE t (b INTEGER, x INTEGER); ALTER TABLE t DROP COLUMN B;`,
			want: []string{"t(x)"},
		},
		{
			name:    "DROP COLUMN of a missing column",
			sql:     `CREATE TABLE t (a INTEGER, x INTEGER); ALTER TABLE t DROP COLUMN zz;`,
			wantErr: "table t: no such column: zz",
		},
		{
			name: "RENAME COLUMN in another case",
			sql:  `CREATE TABLE t (A INTEGER, x INTEGER); ALTER TABLE t RENAME COLUMN a TO c;`,
			want: []string{"t(c, x)"},
		},
		{
			name:    "RENAME COLUMN onto another column's name in another case",
			sql:     `CREATE TABLE t (a INTEGER, B INTEGER); ALTER TABLE t RENAME COLUMN a TO b;`,
			wantErr: "column b already exists in table t",
		},
		{
			name: "RENAME COLUMN to its own name in another case",
			sql:  `CREATE TABLE t (a INTEGER, B INTEGER); ALTER TABLE t RENAME COLUMN a TO A;`,
			want: []string{"t(A, B)"},
		},
		{
			name:    "ADD COLUMN in another case",
			sql:     `CREATE TABLE t (a INTEGER); ALTER TABLE t ADD COLUMN A INTEGER;`,
			wantErr: "table t: duplicate column name: A",
		},
		{
			name: "ADD COLUMN differing in a non-ASCII letter",
			sql:  `CREATE TABLE t ("é" INTEGER); ALTER TABLE t ADD COLUMN "É" INTEGER;`,
			want: []string{"t(é, É)"},
		},
		{
			name:    "RENAME TO an existing table",
			sql:     `CREATE TABLE a (id INTEGER); CREATE TABLE b (id INTEGER); ALTER TABLE a RENAME TO b;`,
			wantErr: "table b already exists",
		},
		{
			name:    "RENAME TO an existing table in another case",
			sql:     `CREATE TABLE a (id INTEGER); CREATE TABLE b (id INTEGER); ALTER TABLE a RENAME TO B;`,
			wantErr: "table B already exists",
		},
		{
			name:    "RENAME TO its own name in another case",
			sql:     `CREATE TABLE Users (id INTEGER); ALTER TABLE Users RENAME TO users;`,
			wantErr: "table users already exists",
		},
		{
			name: "UNIQUE on é beside É",
			sql:  `CREATE TABLE t ("É" INTEGER, "é" INTEGER, UNIQUE ("é"));`,
			want: []string{"t(É, é) U[é]"},
		},
		{
			name: "UNIQUE on É and on é are two indexes",
			sql:  `CREATE TABLE t ("É" INTEGER UNIQUE, "é" INTEGER UNIQUE);`,
			want: []string{"t(É, é) U[É] U[é]"},
		},
		{
			name: "CHECK on é beside É",
			sql:  `CREATE TABLE t ("é" INTEGER, "É" INTEGER, CHECK ("é" > 0));`,
			want: []string{"t(é, É) CHECK[é]"},
		},
		{
			name:    "CREATE TABLE with the same column twice",
			sql:     `CREATE TABLE t (id INTEGER PRIMARY KEY, a INT, a TEXT);`,
			wantErr: "table t: duplicate column name: a",
		},
		{
			name:    "CREATE TABLE with a column in another case",
			sql:     `CREATE TABLE t (id INTEGER PRIMARY KEY, a INT, A TEXT);`,
			wantErr: "table t: duplicate column name: A",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := sqlite.New()
			err := p.Parse("test.sql", []byte(tt.sql))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("Parse() error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			if diff := cmp.Diff(tt.want, describeTables(p.Schema().Tables)); diff != "" {
				t.Errorf("Parse() tables mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// describeTables renders each table as name(columns) and its PRIMARY KEY,
// UNIQUE, CHECK and FOREIGN KEY constraints, with each column's FKReference.
func describeTables(tables []parser.Table) []string {
	out := make([]string, 0, len(tables))
	for _, tbl := range tables {
		cols := make([]string, 0, len(tbl.Columns))
		for _, c := range tbl.Columns {
			col := c.Name
			if ref := c.FKReference; ref != nil {
				col += "->" + ref.Table + "." + ref.Column
			}
			cols = append(cols, col)
		}
		s := tbl.Name + "(" + strings.Join(cols, ", ") + ")"
		for _, c := range tbl.Constraints {
			switch c.Type {
			case parser.PrimaryKey:
				s += fmt.Sprintf(" PK%v", c.Columns)
			case parser.Unique:
				s += fmt.Sprintf(" U%v", c.Columns)
			case parser.Check:
				s += fmt.Sprintf(" CHECK%v", c.Columns)
			case parser.ForeignKey:
				s += fmt.Sprintf(" FK%v->%s%v", c.Columns, c.ReferenceTable, c.ReferenceColumns)
			case parser.Index:
			}
		}
		out = append(out, s)
	}
	return out
}

func TestDropTable(t *testing.T) {
	p := sqlite.New()
	sql := `
		CREATE TABLE users (id INTEGER PRIMARY KEY);
		CREATE TABLE products (id INTEGER PRIMARY KEY);
		DROP TABLE products;
	`
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	schema := p.Schema()
	if len(schema.Tables) != 1 {
		t.Fatalf("expected 1 table after DROP, got %d", len(schema.Tables))
	}
	if schema.Tables[0].Name != "users" {
		t.Errorf("remaining table = %q, want %q", schema.Tables[0].Name, "users")
	}
}

func TestDropTableWithFKReference(t *testing.T) {
	p := sqlite.New()
	sql := `
		CREATE TABLE users (id INTEGER PRIMARY KEY);
		CREATE TABLE orders (
			id INTEGER PRIMARY KEY,
			user_id INTEGER REFERENCES users(id)
		);
		DROP TABLE users;
	`
	err := p.Parse("test.sql", []byte(sql))
	if err == nil {
		t.Fatal("expected error when dropping table referenced by FK, got nil")
	}
}

func TestDropTableCrossFile(t *testing.T) {
	p := sqlite.New()
	if err := p.Parse("001.sql", []byte(`
		CREATE TABLE users (id INTEGER PRIMARY KEY);
		CREATE TABLE orders (
			id INTEGER PRIMARY KEY,
			user_id INTEGER REFERENCES users(id)
		);
	`)); err != nil {
		t.Fatalf("Parse(001.sql) error: %v", err)
	}
	if err := p.Parse("002.sql", []byte(`DROP TABLE users;`)); err == nil {
		t.Fatal("expected error when dropping cross-file table referenced by FK, got nil")
	}
}

func TestDropTableNoReferences(t *testing.T) {
	p := sqlite.New()
	sql := `
		CREATE TABLE users (id INTEGER PRIMARY KEY);
		CREATE TABLE legacy (id INTEGER PRIMARY KEY);
		DROP TABLE legacy;
	`
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}
	if len(p.Schema().Tables) != 1 {
		t.Errorf("expected 1 table, got %d", len(p.Schema().Tables))
	}
}

func TestDropTableIfExists(t *testing.T) {
	p := sqlite.New()
	sql := `DROP TABLE IF EXISTS nonexistent;`
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() unexpected error for DROP IF EXISTS on nonexistent table: %v", err)
	}
}

func TestParseViewSQL(t *testing.T) {
	sqlStr := `CREATE VIEW product_summary AS
SELECT
    p.id,
    p.name,
    COUNT(r.id) AS review_count
FROM products p
LEFT JOIN reviews r ON r.product_id = p.id
GROUP BY p.id, p.name;`

	got, err := sqlite.ParseViewSQL([]byte(sqlStr))
	if err != nil {
		t.Fatalf("ParseViewSQL() error: %v", err)
	}

	if got.Name != "product_summary" {
		t.Errorf("Name = %q, want %q", got.Name, "product_summary")
	}

	if len(got.Columns) != 3 {
		t.Fatalf("columns = %d, want 3", len(got.Columns))
	}

	// Verify qualified column reference.
	if got.Columns[0].Alias != "id" || got.Columns[0].SourceTable != "p" || got.Columns[0].SourceColumn != "id" {
		t.Errorf("col[0] = %+v, want alias=id sourceTable=p sourceColumn=id", got.Columns[0])
	}

	// Verify aggregate.
	if got.Columns[2].Aggregate != "COUNT" {
		t.Errorf("col[2].Aggregate = %q, want %q", got.Columns[2].Aggregate, "COUNT")
	}

	// Verify table aliases.
	if got.TableAliases["p"] != "products" {
		t.Errorf("alias p = %q, want %q", got.TableAliases["p"], "products")
	}
	if got.TableAliases["r"] != "reviews" {
		t.Errorf("alias r = %q, want %q", got.TableAliases["r"], "reviews")
	}
}

func TestCreateViewSkippedGracefully(t *testing.T) {
	sqlStr := `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);
CREATE VIEW active_users AS SELECT id, name FROM users WHERE id > 0;`

	p := sqlite.New()
	if err := p.Parse("test.sql", []byte(sqlStr)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	schema := p.Schema()

	if len(schema.Tables) != 1 {
		t.Fatalf("tables = %d, want 1", len(schema.Tables))
	}
	if len(schema.Views) != 0 {
		t.Errorf("views = %d, want 0", len(schema.Views))
	}
	if len(schema.Warnings) == 0 {
		t.Error("expected a warning for CREATE VIEW, got none")
	}
	foundViewWarning := false
	for _, w := range schema.Warnings {
		if strings.Contains(w, "CREATE VIEW") && strings.Contains(w, "active_users") {
			foundViewWarning = true
		}
	}
	if !foundViewWarning {
		t.Errorf("expected warning mentioning CREATE VIEW active_users, got %v", schema.Warnings)
	}
}

func TestCreateVirtualTableSkipped(t *testing.T) {
	sqlStr := `CREATE TABLE docs (id INTEGER PRIMARY KEY, body TEXT);
CREATE VIRTUAL TABLE docs_fts USING fts5(body);
CREATE VIRTUAL TABLE "Boxes" USING rtree(id, minx, maxx);`

	p := sqlite.New()
	if err := p.Parse("test.sql", []byte(sqlStr)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	schema := p.Schema()
	if len(schema.Tables) != 1 || schema.Tables[0].Name != "docs" {
		t.Errorf("tables = %+v, want only docs", schema.Tables)
	}
	wantWarnings := []string{
		"CREATE VIRTUAL TABLE docs_fts skipped — virtual tables are not generated",
		"CREATE VIRTUAL TABLE Boxes skipped — virtual tables are not generated",
	}
	if diff := cmp.Diff(wantWarnings, schema.Warnings); diff != "" {
		t.Errorf("warnings mismatch (-want +got):\n%s", diff)
	}
}

// A collation on an indexed column is not part of the column's name, which
// is the name the catalog reports for the index.
func TestIndexedColumnCollation(t *testing.T) {
	sqlStr := `CREATE TABLE t (email TEXT, name TEXT, UNIQUE (email COLLATE NOCASE));
CREATE INDEX t_name ON t (name COLLATE NOCASE DESC);`

	p := sqlite.New()
	if err := p.Parse("test.sql", []byte(sqlStr)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	want := []parser.Constraint{
		{Type: parser.Unique, Columns: []string{"email"}},
		{Name: "t_name", Type: parser.Index, Columns: []string{"name"}, Method: "btree"},
	}
	if diff := cmp.Diff(want, p.Schema().Tables[0].Constraints); diff != "" {
		t.Errorf("constraints mismatch (-want +got):\n%s", diff)
	}
}

// An index part in parentheses names its column, as SQLite indexes it, and so
// does a string literal, which SQLite reads as a column name. An index with an
// expression part covers no column list and is skipped, as the introspector
// skips it (PRD §6.1, §30.7).
func TestIndexedColumnExpressions(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want []parser.Constraint
	}{
		{
			name: "parenthesized column",
			sql:  `CREATE INDEX t_ab ON t ((a), ((b) COLLATE NOCASE));`,
			want: []parser.Constraint{{Name: "t_ab", Type: parser.Index, Columns: []string{"a", "b"}, Method: "btree"}},
		},
		{
			name: "parenthesized UNIQUE constraint column",
			sql:  `CREATE TABLE u (a TEXT, UNIQUE ((a)));`,
			want: []parser.Constraint{{Type: parser.Unique, Columns: []string{"a"}}},
		},
		{
			name: "string literal",
			sql:  `CREATE INDEX t_a ON t ('a');`,
			want: []parser.Constraint{{Name: "t_a", Type: parser.Index, Columns: []string{"a"}, Method: "btree"}},
		},
		{
			name: "string literal UNIQUE constraint column",
			sql:  `CREATE TABLE u (a TEXT, b TEXT, UNIQUE ((('a') COLLATE NOCASE), b));`,
			want: []parser.Constraint{{Type: parser.Unique, Columns: []string{"a", "b"}}},
		},
		{
			name: "string literal PRIMARY KEY column",
			sql:  `CREATE TABLE u (a TEXT, PRIMARY KEY ('a'));`,
			want: []parser.Constraint{{Type: parser.PrimaryKey, Columns: []string{"a"}}},
		},
		{
			name: "string literal under two COLLATEs in a PRIMARY KEY",
			sql:  `CREATE TABLE u (a TEXT, PRIMARY KEY (('a' COLLATE NOCASE) COLLATE BINARY));`,
			want: []parser.Constraint{{Type: parser.PrimaryKey, Columns: []string{"a"}}},
		},
		{name: "string literal under two COLLATEs in an index", sql: `CREATE INDEX t_a ON t (('a' COLLATE NOCASE) COLLATE BINARY);`},
		{name: "expression-only unique index", sql: `CREATE UNIQUE INDEX t_lower_a ON t (lower(a));`},
		{name: "expression-only index", sql: `CREATE INDEX t_lower_a ON t (lower(a));`},
		{name: "column and expression", sql: `CREATE INDEX t_a_lower_b ON t (a, lower(b));`},
		{name: "unique column and expression", sql: `CREATE UNIQUE INDEX t_a_b1 ON t (a, b + 1);`},
		{name: "partial expression index", sql: `CREATE INDEX t_lower_a ON t (lower(a)) WHERE b IS NOT NULL;`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := sqlite.New()
			if err := p.Parse("test.sql", []byte(`CREATE TABLE t (a TEXT, b TEXT);`+tt.sql)); err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			var got []parser.Constraint
			for _, table := range p.Schema().Tables {
				got = append(got, table.Constraints...)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("constraints mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// SQLite rejects an expression in a UNIQUE constraint; read as written it
// would be a UNIQUE over no columns.
func TestCreateTable_UniqueExpression(t *testing.T) {
	for _, sql := range []string{
		`CREATE TABLE t (a TEXT, UNIQUE (lower(a)));`,
		`CREATE TABLE t (a INTEGER, UNIQUE (a, a + 1));`,
		`CREATE TABLE t (a TEXT, CONSTRAINT t_a UNIQUE ((a || 'x') COLLATE NOCASE));`,
		`CREATE TABLE t (a TEXT, UNIQUE (+a));`,
		`CREATE TABLE t (a TEXT, UNIQUE (1));`,
		`CREATE TABLE t (a TEXT, UNIQUE (('a' COLLATE NOCASE) COLLATE BINARY));`,
	} {
		err := sqlite.New().Parse("test.sql", []byte(sql))
		const want = "parsing test.sql: table t: UNIQUE has an expression part: expressions prohibited in UNIQUE"
		if err == nil || err.Error() != want {
			t.Errorf("Parse(%q) error = %v, want %q", sql, err, want)
		}
	}
}

// SQLite builds one index per set of columns and collations, so a UNIQUE is a
// duplicate only when its columns are indexed under the same collations. The
// sort order does not count. Each want is the number of UNIQUE indexes SQLite
// builds for the table.
func TestUniqueDuplicatesFollowCollation(t *testing.T) {
	tests := []struct {
		name        string
		sql         string
		wantUniques int
	}{
		{name: "explicit collation differs", sql: `CREATE TABLE t (a TEXT, UNIQUE (a), UNIQUE (a COLLATE NOCASE));`, wantUniques: 2},
		{name: "collation inside parentheses", sql: `CREATE TABLE t (a TEXT, UNIQUE (a), UNIQUE ((a COLLATE NOCASE)));`, wantUniques: 2},
		{name: "collation outside parentheses", sql: `CREATE TABLE t (a TEXT, UNIQUE (a), UNIQUE ((a) COLLATE NOCASE));`, wantUniques: 2},
		{name: "collation differs from the key", sql: `CREATE TABLE t (id TEXT PRIMARY KEY, UNIQUE (id COLLATE NOCASE));`, wantUniques: 1},
		{name: "sort order does not count", sql: `CREATE TABLE t (a TEXT, UNIQUE (a), UNIQUE (a DESC));`, wantUniques: 1},
		{name: "explicit equals the column's", sql: `CREATE TABLE t (a TEXT COLLATE NOCASE, UNIQUE (a), UNIQUE (a COLLATE nocase));`, wantUniques: 1},
		{name: "explicit overrides the column's", sql: `CREATE TABLE t (a TEXT COLLATE NOCASE UNIQUE, UNIQUE (a COLLATE BINARY));`, wantUniques: 2},
		{name: "key on a collated column", sql: `CREATE TABLE t (id TEXT COLLATE NOCASE PRIMARY KEY, UNIQUE (id));`},
		{name: "explicit BINARY is the default", sql: `CREATE TABLE t (a TEXT, b TEXT, UNIQUE (a, b), UNIQUE (a COLLATE BINARY, b));`, wantUniques: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := sqlite.New()
			if err := p.Parse("test.sql", []byte(tt.sql)); err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			table := p.Schema().Tables[0]
			if got := len(filterUniques(table.Constraints)); got != tt.wantUniques {
				t.Errorf("UNIQUE constraints = %d, want %d: %+v", got, tt.wantUniques, table.Constraints)
			}
		})
	}
}

func TestNormalizeType(t *testing.T) {
	tests := []struct {
		name     string
		declared string
		want     string
	}{
		{name: "empty is blob", declared: "", want: "blob"},
		{name: "case folding", declared: "INTEGER", want: "integer"},
		{name: "mixed case", declared: "Text", want: "text"},
		{name: "varchar passthrough", declared: "VARCHAR(255)", want: "varchar(255)"},
		{name: "numeric passthrough", declared: "NUMERIC(10,2)", want: "numeric(10,2)"},
		{name: "spaces inside the modifier", declared: "NUMERIC( 10, 2 )", want: "numeric(10,2)"},
		{name: "space before the modifier", declared: "VARCHAR (255)", want: "varchar(255)"},
		{name: "multi-word type", declared: "DOUBLE  PRECISION", want: "double precision"},
		{name: "unrecognized is blob", declared: "uuid", want: "blob"},
		{name: "unrecognized with a modifier is blob", declared: "XYZZY(100)", want: "blob"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sqlite.NormalizeType(tt.declared); got != tt.want {
				t.Errorf("NormalizeType(%q) = %q, want %q", tt.declared, got, tt.want)
			}
		})
	}
}

func TestDropViewSkippedGracefully(t *testing.T) {
	sqlStr := `CREATE TABLE users (id INTEGER PRIMARY KEY);
DROP VIEW IF EXISTS old_view;`

	p := sqlite.New()
	if err := p.Parse("test.sql", []byte(sqlStr)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	schema := p.Schema()
	if len(schema.Tables) != 1 {
		t.Fatalf("tables = %d, want 1", len(schema.Tables))
	}
}

func TestCreateIndex_PartialWhere(t *testing.T) {
	sqlStr := `CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    email TEXT NOT NULL,
    deleted_at TEXT
);
CREATE UNIQUE INDEX users_active_email_idx ON users (email) WHERE deleted_at IS NULL;
CREATE INDEX users_email_idx ON users (email);`

	p := sqlite.New()
	if err := p.Parse("test.sql", []byte(sqlStr)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	schema := p.Schema()
	if len(schema.Tables) != 1 {
		t.Fatalf("tables = %d, want 1", len(schema.Tables))
	}
	table := schema.Tables[0]

	byName := make(map[string]parser.Constraint, len(table.Constraints))
	for _, c := range table.Constraints {
		byName[c.Name] = c
	}

	partial, ok := byName["users_active_email_idx"]
	if !ok {
		t.Fatalf("missing users_active_email_idx; constraints=%+v", table.Constraints)
	}
	if partial.Type != parser.Unique {
		t.Errorf("partial: Type=%v, want Unique", partial.Type)
	}
	if partial.Method != "btree" {
		t.Errorf("partial: Method=%q, want %q", partial.Method, "btree")
	}
	if partial.Where == "" || !strings.Contains(partial.Where, "deleted_at") {
		t.Errorf("partial: Where=%q, want non-empty containing deleted_at", partial.Where)
	}

	plain, ok := byName["users_email_idx"]
	if !ok {
		t.Fatalf("missing users_email_idx; constraints=%+v", table.Constraints)
	}
	if plain.Type != parser.Index || plain.Method != "btree" || plain.Where != "" {
		t.Errorf("plain: %+v, want Type=Index Method=btree Where=\"\"", plain)
	}

	for _, col := range table.Columns {
		if col.Name == "email" && col.Unique {
			t.Errorf("email column unexpectedly marked Unique=true from partial UNIQUE index")
		}
	}
}

func TestIndexWhere(t *testing.T) {
	tests := []struct {
		name    string
		ddl     string
		want    string
		wantErr string
	}{
		{name: "not partial", ddl: `CREATE INDEX ix ON t (a)`},
		{name: "IS NOT NULL", ddl: `CREATE UNIQUE INDEX ux ON t (a) WHERE a IS NOT NULL`, want: `"a" NOT NULL`},
		{name: "comparison", ddl: `create index ix on t (b) where b>0 AND a<>'x'`, want: `"b" > 0 AND "a" != 'x'`},
		{name: "WHERE in a comment", ddl: "CREATE INDEX ix ON t (a) -- WHERE b = 1\nWHERE a > 0", want: `"a" > 0`},
		{name: "WHERE in an identifier", ddl: `CREATE INDEX ix ON t ("a WHERE b") WHERE "a WHERE b" > 0`, want: `"a WHERE b" > 0`},
		{name: "IS NOT DISTINCT FROM", ddl: `CREATE INDEX ix ON t (a) WHERE a IS NOT DISTINCT FROM b`, want: `"a" IS NOT DISTINCT FROM "b"`},
		{name: "LIKE ESCAPE", ddl: `CREATE INDEX ix ON t (a) WHERE a LIKE 'x%' ESCAPE '\'`, want: `"a" LIKE 'x%' ESCAPE '\'`},
		{name: "unreadable predicate", ddl: `CREATE INDEX ix ON t (a) WHERE a IS`, wantErr: "parsing CREATE INDEX: 1:35: expected expression, found 'EOF'"},
		{name: "not an index", ddl: `CREATE TABLE t (a)`, wantErr: "parsing CREATE INDEX: got *sql.CreateTableStatement"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sqlite.IndexWhere(tt.ddl)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("IndexWhere(%q) error = %v, want %q", tt.ddl, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("IndexWhere(%q) error: %v", tt.ddl, err)
			}
			if got != tt.want {
				t.Errorf("IndexWhere(%q) = %q, want %q", tt.ddl, got, tt.want)
			}
		})
	}
}

// TestParse_TimeKeywords pins that CURRENT_TIME, CURRENT_DATE and
// CURRENT_TIMESTAMP in an expression render as the keyword, as written.
// rqlite/sql parses them as identifiers there, and a quoted identifier is what
// SQLite reads as a string: DEFAULT ("CURRENT_TIMESTAMP") is the text
// CURRENT_TIMESTAMP. A quoted name stays a name.
func TestParse_TimeKeywords(t *testing.T) {
	tests := []struct {
		name        string
		ddl         string
		wantDefault string
		wantCheck   string
	}{
		{
			name:      "column CHECK",
			ddl:       `CREATE TABLE t (y TEXT CHECK (y < CURRENT_TIMESTAMP))`,
			wantCheck: `"y" < CURRENT_TIMESTAMP`,
		},
		{
			name:      "table CHECK, as written, in nested operands",
			ddl:       `CREATE TABLE t (y TEXT, CHECK ((y BETWEEN current_date AND datetime(CURRENT_TIME, '+1 day')) OR CASE WHEN y IS NULL THEN Current_Timestamp END IS NOT NULL))`,
			wantCheck: `("y" BETWEEN current_date AND datetime(CURRENT_TIME, '+1 day')) OR CASE WHEN "y" IS NULL THEN Current_Timestamp END NOT NULL`,
		},
		{
			name:      "added column CHECK",
			ddl:       `CREATE TABLE t (id INTEGER); ALTER TABLE t ADD COLUMN y TEXT CHECK (y < CURRENT_DATE)`,
			wantCheck: `"y" < CURRENT_DATE`,
		},
		{
			name:        "parenthesized DEFAULT",
			ddl:         `CREATE TABLE t (y TEXT DEFAULT (CURRENT_TIMESTAMP))`,
			wantDefault: `CURRENT_TIMESTAMP`,
		},
		{
			name:        "DEFAULT function argument",
			ddl:         `CREATE TABLE t (y TEXT DEFAULT (datetime(CURRENT_TIMESTAMP, '+1 day')))`,
			wantDefault: `datetime(CURRENT_TIMESTAMP, '+1 day')`,
		},
		{
			name:      "quoted name",
			ddl:       `CREATE TABLE t (y TEXT, CHECK (y < "CURRENT_TIMESTAMP"))`,
			wantCheck: `"y" < "CURRENT_TIMESTAMP"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := sqlite.New()
			if err := p.Parse("schema.sql", []byte(tt.ddl)); err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			table := p.Schema().Tables[0]
			var gotDefault, gotCheck string
			for _, c := range table.Columns {
				if c.Name == "y" {
					gotDefault = c.Default
				}
			}
			for _, c := range table.Constraints {
				if c.Type == parser.Check {
					gotCheck = c.CheckExpression
				}
			}
			if gotDefault != tt.wantDefault {
				t.Errorf("y Default = %q, want %q", gotDefault, tt.wantDefault)
			}
			if gotCheck != tt.wantCheck {
				t.Errorf("CHECK = %q, want %q", gotCheck, tt.wantCheck)
			}
		})
	}
}

// LIKE … ESCAPE and IS [NOT] DISTINCT FROM read and render wherever an
// expression is kept.
func TestParse_EscapeAndDistinctExpressions(t *testing.T) {
	tests := []struct {
		name string
		ddl  string
		get  func(*parser.Schema) string
		want string
	}{
		{
			name: "column CHECK",
			ddl:  `CREATE TABLE t (a TEXT CHECK (a LIKE 'x%' ESCAPE '\'))`,
			get:  func(s *parser.Schema) string { return s.Tables[0].Constraints[0].CheckExpression },
			want: `"a" LIKE 'x%' ESCAPE '\'`,
		},
		{
			name: "table CHECK",
			ddl:  `CREATE TABLE t (a TEXT, CHECK (a NOT LIKE 'x%' ESCAPE '\'))`,
			get:  func(s *parser.Schema) string { return s.Tables[0].Constraints[0].CheckExpression },
			want: `"a" NOT LIKE 'x%' ESCAPE '\'`,
		},
		{
			name: "DEFAULT",
			ddl:  `CREATE TABLE t (a INTEGER DEFAULT ('x' LIKE 'y' ESCAPE '\'))`,
			get:  func(s *parser.Schema) string { return s.Tables[0].Columns[0].Default },
			want: `'x' LIKE 'y' ESCAPE '\'`,
		},
		{
			name: "generated column",
			ddl:  `CREATE TABLE t (a TEXT, b INTEGER GENERATED ALWAYS AS (a IS DISTINCT FROM 'x'))`,
			get:  func(s *parser.Schema) string { return s.Tables[0].Columns[1].GeneratedExpr },
			want: `"a" IS DISTINCT FROM 'x'`,
		},
		{
			name: "CHECK after a PRIMARY KEY sort order",
			ddl:  `CREATE TABLE t (id INTEGER PRIMARY KEY DESC CHECK (id LIKE 'x%' ESCAPE '\'))`,
			get:  func(s *parser.Schema) string { return s.Tables[0].Constraints[1].CheckExpression },
			want: `"id" LIKE 'x%' ESCAPE '\'`,
		},
		{
			name: "added column CHECK",
			ddl:  `CREATE TABLE t (id INTEGER); ALTER TABLE t ADD COLUMN a TEXT CHECK (a IS NOT DISTINCT FROM 'x')`,
			get:  func(s *parser.Schema) string { return s.Tables[0].Constraints[0].CheckExpression },
			want: `"a" IS NOT DISTINCT FROM 'x'`,
		},
		{
			name: "partial index predicate",
			ddl:  `CREATE TABLE t (a TEXT); CREATE INDEX ix ON t (a) WHERE a LIKE 'x%' ESCAPE '\'`,
			get:  func(s *parser.Schema) string { return s.Tables[0].Constraints[0].Where },
			want: `"a" LIKE 'x%' ESCAPE '\'`,
		},
		{
			name: "renamed column",
			ddl:  `CREATE TABLE t (a TEXT, b TEXT CHECK (b LIKE a ESCAPE '\' OR b IS DISTINCT FROM a)); ALTER TABLE t RENAME COLUMN a TO z`,
			get:  func(s *parser.Schema) string { return s.Tables[0].Constraints[0].CheckExpression },
			want: `"b" LIKE "z" ESCAPE '\' OR "b" IS DISTINCT FROM "z"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := sqlite.New()
			if err := p.Parse("schema.sql", []byte(tt.ddl)); err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			if got := tt.get(p.Schema()); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseViewSQL_MaterializedRejected(t *testing.T) {
	tests := []struct {
		name string
		sql  string
	}{
		{
			name: "bare statement",
			sql:  "CREATE MATERIALIZED VIEW order_totals AS SELECT customer_id FROM orders;",
		},
		{
			name: "leading annotation comments",
			sql: `-- @pk: customer_id

CREATE MATERIALIZED VIEW order_totals AS SELECT customer_id FROM orders;`,
		},
		{
			name: "lowercase keywords",
			sql:  "create materialized view order_totals as select customer_id from orders;",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := sqlite.ParseViewSQL([]byte(tt.sql))
			if err == nil {
				t.Fatal("ParseViewSQL() error = nil, want hard error (materialized views are PostgreSQL-only)")
			}
			if !strings.Contains(err.Error(), "not supported for dialect sqlite") {
				t.Errorf("error = %q, want mention of 'not supported for dialect sqlite'", err)
			}
		})
	}
}

func TestParseViewSQL_OuterAliases(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want map[string]bool
	}{
		{name: "inner join", sql: `CREATE VIEW v AS SELECT a.id, b.id AS b_id FROM a JOIN b ON b.a_id = a.id;`, want: map[string]bool{}},
		{name: "comma join", sql: `CREATE VIEW v AS SELECT a.id, b.id AS b_id FROM a, b;`, want: map[string]bool{}},
		{name: "left join", sql: `CREATE VIEW v AS SELECT a.id, b.id AS b_id FROM a LEFT JOIN b ON b.a_id = a.id;`, want: map[string]bool{"b": true}},
		{name: "left outer join with aliases", sql: `CREATE VIEW v AS SELECT x.id, y.id AS y_id FROM a x LEFT OUTER JOIN b y ON y.a_id = x.id;`, want: map[string]bool{"y": true}},
		{name: "inner then left", sql: `CREATE VIEW v AS SELECT a.id FROM a JOIN b ON b.a_id = a.id LEFT JOIN c ON c.b_id = b.id;`, want: map[string]bool{"c": true}},
		{name: "join nested under the outer side", sql: `CREATE VIEW v AS SELECT a.id FROM a LEFT JOIN (b JOIN c ON c.b_id = b.id) ON b.a_id = a.id;`, want: map[string]bool{"b": true, "c": true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sqlite.ParseViewSQL([]byte(tt.sql))
			if err != nil {
				t.Fatalf("ParseViewSQL() error: %v", err)
			}
			if diff := cmp.Diff(tt.want, got.OuterAliases); diff != "" {
				t.Errorf("OuterAliases mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestParseViewSQL_CompoundSelectRejected(t *testing.T) {
	for _, sql := range []string{
		"CREATE VIEW v AS SELECT id FROM a UNION SELECT id FROM b;",
		"CREATE VIEW v AS SELECT id FROM a UNION ALL SELECT id FROM b;",
		"CREATE VIEW v AS SELECT id FROM a EXCEPT SELECT id FROM b;",
	} {
		_, err := sqlite.ParseViewSQL([]byte(sql))
		if err == nil || !strings.Contains(err.Error(), "not a simple SELECT") {
			t.Errorf("ParseViewSQL(%q) error = %v, want not a simple SELECT", sql, err)
		}
		// The error names the view, so an annotation file for a view the
		// database defines can still overlay it.
		if qe, ok := errors.AsType[*parser.ViewQueryError](err); !ok || qe.Name != "v" {
			t.Errorf("ParseViewSQL(%q) error = %#v, want a ViewQueryError naming v", sql, err)
		}
	}
}

func TestParseViewSQL_RightAndFullJoinRejected(t *testing.T) {
	for _, sql := range []string{
		"CREATE VIEW v AS SELECT a.id FROM a RIGHT JOIN b ON b.a_id = a.id;",
		"CREATE VIEW v AS SELECT a.id FROM a full JOIN b ON b.a_id = a.id;",
		"CREATE VIEW v AS SELECT a.id FROM c JOIN a FULL JOIN b ON b.a_id = a.id;",
		"CREATE VIEW v AS SELECT a.id FROM a RIGHT OUTER JOIN b ON b.a_id = a.id;",
		"CREATE VIEW v AS SELECT a.id FROM a NATURAL FULL OUTER JOIN b;",
		"CREATE VIEW v AS SELECT a.id FROM (a RIGHT JOIN b ON b.a_id = a.id) JOIN c ON c.id = a.id;",
	} {
		_, err := sqlite.ParseViewSQL([]byte(sql))
		if err == nil || !strings.Contains(err.Error(), "RIGHT and FULL joins are not supported") {
			t.Errorf("ParseViewSQL(%q) error = %v, want it to reject the join", sql, err)
		}
		if _, ok := errors.AsType[*parser.ViewQueryError](err); !ok {
			t.Errorf("ParseViewSQL(%q) error = %v, want a ViewQueryError", sql, err)
		}
	}
}

// TestParseViewSQL_ColumnNames checks that every view column has a name (PRD
// §16.1): an unaliased expression with no column name, * and t.* are
// ViewQueryErrors, and a column list names the columns in order. SQLite
// requires the list to name every column.
func TestParseViewSQL_ColumnNames(t *testing.T) {
	tests := []struct {
		name, sql string
		want      []string
		wantErr   string
	}{
		{name: "unaliased expression", sql: "CREATE VIEW v AS SELECT id, a + b FROM t", wantErr: `view v selects "a" + "b" with no alias and no column name`},
		{name: "unaliased aggregate", sql: "CREATE VIEW v AS SELECT id, COUNT(*) FROM t GROUP BY id", wantErr: "view v selects COUNT(*) with no alias"},
		{name: "unaliased LIKE ESCAPE", sql: `CREATE VIEW v AS SELECT id, a LIKE 'x%' ESCAPE '\' FROM t`, wantErr: `view v selects "a" LIKE 'x%' ESCAPE '\' with no alias and no column name; name it with AS`},
		{name: "unaliased aggregate over LIKE ESCAPE", sql: `CREATE VIEW v AS SELECT id, COUNT(a LIKE 'x%' ESCAPE '\') FROM t GROUP BY id`, wantErr: `view v selects COUNT("a" LIKE 'x%' ESCAPE '\') with no alias and no column name; name it with AS`},
		{name: "aliased LIKE ESCAPE", sql: `CREATE VIEW v AS SELECT id, a LIKE 'x%' ESCAPE '\' AS m FROM t`, want: []string{"id", "m"}},
		{name: "star", sql: "CREATE VIEW v AS SELECT * FROM t", wantErr: "view v selects *, whose columns the view parser cannot list"},
		{name: "table star", sql: "CREATE VIEW v AS SELECT id, t.* FROM t", wantErr: `view v selects "t".*, whose columns`},
		{name: "column list", sql: "CREATE VIEW v (x, total) AS SELECT id, a + b FROM t", want: []string{"x", "total"}},
		{name: "short column list", sql: "CREATE VIEW v (x) AS SELECT id, name FROM t", wantErr: "view v names 1 columns in its column list but selects 2"},
		{name: "long column list", sql: "CREATE VIEW v (x, y) AS SELECT id FROM t", wantErr: "view v names 2 columns in its column list but selects 1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sqlite.ParseViewSQL([]byte(tt.sql))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ParseViewSQL() error = %v, want it to contain %q", err, tt.wantErr)
				}
				// An annotation file for a view the database defines can
				// still overlay it (PRD §16.2).
				if qe, ok := errors.AsType[*parser.ViewQueryError](err); !ok || qe.Name != "v" {
					t.Errorf("ParseViewSQL() error = %#v, want a ViewQueryError naming v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseViewSQL() error: %v", err)
			}
			names := make([]string, 0, len(got.Columns))
			for _, c := range got.Columns {
				names = append(names, c.Alias)
			}
			if diff := cmp.Diff(tt.want, names); diff != "" {
				t.Errorf("column names (-want +got):\n%s", diff)
			}
		})
	}
}
