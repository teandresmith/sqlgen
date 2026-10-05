package sqlite_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

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

func TestAlterTableRenameColumn(t *testing.T) {
	sql := `
		CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);
		ALTER TABLE users RENAME COLUMN name TO display_name;
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

	wantCols := []string{"id", "display_name"}
	if diff := cmp.Diff(wantCols, colNames); diff != "" {
		t.Errorf("columns after RENAME COLUMN mismatch (-want +got):\n%s", diff)
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
				"posts FOREIGN KEY [user_id] -> users[user_id]",
				"post_tags.post_id -> posts(id)",
				"post_tags PRIMARY KEY [post_id tag_id]",
				"members UNIQUE [org_id email]",
			},
		},
		{
			name:  "junction key column",
			alter: "ALTER TABLE post_tags RENAME COLUMN post_id TO article_id;",
			want: []string{
				"users PRIMARY KEY [id]",
				"posts.user_id -> users(id)",
				"posts FOREIGN KEY [user_id] -> users[id]",
				"post_tags.article_id -> posts(id)",
				"post_tags PRIMARY KEY [article_id tag_id]",
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
				"posts FOREIGN KEY [user_id] -> users[id]",
				"post_tags.post_id -> posts(id)",
				"post_tags PRIMARY KEY [post_id tag_id]",
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
