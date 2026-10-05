package mysql_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/parser/mysql"
)

func newParser(t *testing.T) *mysql.Parser {
	t.Helper()
	p, err := mysql.New()
	if err != nil {
		t.Fatalf("mysql.New() error: %v", err)
	}
	return p
}

// findConstraint returns the first constraint matching both name and type, or
// nil. MySQL names an FK's synthesized backing index after the FK constraint,
// so a name can map to more than one constraint of different types.
func findConstraint(constraints []parser.Constraint, name string, typ parser.ConstraintType) *parser.Constraint {
	for i := range constraints {
		if constraints[i].Name == name && constraints[i].Type == typ {
			return &constraints[i]
		}
	}
	return nil
}

func TestCreateTableMySQLTypes(t *testing.T) {
	sql := `CREATE TABLE test_types (
		t_tinyint TINYINT,
		t_smallint SMALLINT,
		t_int INT,
		t_bigint BIGINT,
		t_float FLOAT,
		t_double DOUBLE,
		t_decimal DECIMAL(10,2),
		t_varchar VARCHAR(255),
		t_text TEXT,
		t_datetime DATETIME,
		t_timestamp TIMESTAMP,
		t_date DATE,
		t_json JSON,
		t_blob BLOB,
		t_bool BOOL,
		t_enum ENUM('a', 'b', 'c'),
		t_set SET('x', 'y', 'z')
	);`

	p := newParser(t)
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
	// MySQL tables should have no schema prefix.
	if table.Schema != "" {
		t.Errorf("table schema = %q, want empty", table.Schema)
	}

	wantTypes := map[string]string{
		"t_tinyint":   "tinyint",
		"t_smallint":  "smallint",
		"t_int":       "int",
		"t_bigint":    "bigint",
		"t_float":     "float",
		"t_double":    "double",
		"t_decimal":   "decimal(10,2)",
		"t_varchar":   "varchar(255)",
		"t_text":      "text",
		"t_datetime":  "datetime",
		"t_timestamp": "timestamp",
		"t_date":      "date",
		"t_json":      "json",
		"t_blob":      "blob",
		"t_bool":      "bool",
		"t_enum":      "test_types_t_enum_enum",
		"t_set":       "test_types_t_set_set",
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

func TestCreateTableInlineConstraints(t *testing.T) {
	sql := `CREATE TABLE orders (
		id INT PRIMARY KEY,
		email VARCHAR(255) UNIQUE NOT NULL,
		amount DECIMAL(10,2) DEFAULT 0,
		user_id INT REFERENCES users(id)
	);`

	p := newParser(t)
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	table := p.Schema().Tables[0]
	cols := make(map[string]parser.Column)
	for _, c := range table.Columns {
		cols[c.Name] = c
	}

	// PK
	if c := cols["id"]; !c.PrimaryKey || c.Nullable {
		t.Errorf("id: PrimaryKey=%v Nullable=%v, want PrimaryKey=true Nullable=false", c.PrimaryKey, c.Nullable)
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
}

func TestInlineEnumExtraction(t *testing.T) {
	sql := `CREATE TABLE users (
		id INT PRIMARY KEY AUTO_INCREMENT,
		status ENUM('active', 'inactive', 'pending') NOT NULL DEFAULT 'active'
	);`

	p := newParser(t)
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	schema := p.Schema()

	// Column type should match the derived enum name, not generic "enum".
	table := schema.Tables[0]
	cols := make(map[string]parser.Column)
	for _, c := range table.Columns {
		cols[c.Name] = c
	}
	if cols["status"].Type != "users_status_enum" {
		t.Errorf("status type = %q, want %q", cols["status"].Type, "users_status_enum")
	}

	// An Enum entry should be created for the inline ENUM.
	if len(schema.Enums) != 1 {
		t.Fatalf("enums = %d, want 1", len(schema.Enums))
	}
	wantValues := []string{"active", "inactive", "pending"}
	if diff := cmp.Diff(wantValues, schema.Enums[0].Values); diff != "" {
		t.Errorf("enum values mismatch (-want +got):\n%s", diff)
	}
}

func TestInlineSetExtraction(t *testing.T) {
	sql := `CREATE TABLE users (
		id INT PRIMARY KEY AUTO_INCREMENT,
		permissions SET('read', 'write', 'admin') NOT NULL DEFAULT 'read'
	);`

	p := newParser(t)
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	schema := p.Schema()

	// Column type should match the derived set name, not generic "set".
	table := schema.Tables[0]
	cols := make(map[string]parser.Column)
	for _, c := range table.Columns {
		cols[c.Name] = c
	}
	if cols["permissions"].Type != "users_permissions_set" {
		t.Errorf("permissions type = %q, want %q", cols["permissions"].Type, "users_permissions_set")
	}

	// A Set entry should be created for the inline SET.
	if len(schema.Sets) != 1 {
		t.Fatalf("sets = %d, want 1", len(schema.Sets))
	}
	if schema.Sets[0].Name != "users_permissions_set" {
		t.Errorf("set name = %q, want %q", schema.Sets[0].Name, "users_permissions_set")
	}
	wantValues := []string{"read", "write", "admin"}
	if diff := cmp.Diff(wantValues, schema.Sets[0].Values); diff != "" {
		t.Errorf("set values mismatch (-want +got):\n%s", diff)
	}

	// No Enum entries should be created for SET.
	if len(schema.Enums) != 0 {
		t.Errorf("enums = %d, want 0 (SET should not create Enum entries)", len(schema.Enums))
	}
}

// TestRedefineColumnReplacesInlineEnumOrSet covers MODIFY and CHANGE COLUMN on
// inline ENUM and SET columns: the new definition replaces the column's entry
// instead of registering a second one under the same name, which fails
// generation on the duplicate Go type, and a column redefined to another type
// or renamed leaves no entry behind.
func TestRedefineColumnReplacesInlineEnumOrSet(t *testing.T) {
	tests := []struct {
		name      string
		sql       string
		wantType  string
		wantEnums []parser.Enum
		wantSets  []parser.Set
	}{
		{
			name:      "MODIFY widening an ENUM",
			sql:       "CREATE TABLE users (id INT PRIMARY KEY, role ENUM('a','b') NOT NULL); ALTER TABLE users MODIFY role ENUM('a','b','c') NOT NULL;",
			wantType:  "users_role_enum",
			wantEnums: []parser.Enum{{Name: "users_role_enum", Values: []string{"a", "b", "c"}}},
		},
		{
			name:      "CHANGE COLUMN widening an ENUM without a rename",
			sql:       "CREATE TABLE users (id INT PRIMARY KEY, role ENUM('a','b') NOT NULL); ALTER TABLE users CHANGE COLUMN role role ENUM('a','b','c') NOT NULL;",
			wantType:  "users_role_enum",
			wantEnums: []parser.Enum{{Name: "users_role_enum", Values: []string{"a", "b", "c"}}},
		},
		{
			name:      "CHANGE COLUMN renaming an ENUM column",
			sql:       "CREATE TABLE users (id INT PRIMARY KEY, role ENUM('a','b') NOT NULL); ALTER TABLE users CHANGE COLUMN role kind ENUM('a','b','c') NOT NULL;",
			wantType:  "users_kind_enum",
			wantEnums: []parser.Enum{{Name: "users_kind_enum", Values: []string{"a", "b", "c"}}},
		},
		{
			name:     "MODIFY from ENUM to VARCHAR",
			sql:      "CREATE TABLE users (id INT PRIMARY KEY, role ENUM('a','b') NOT NULL); ALTER TABLE users MODIFY role VARCHAR(10) NOT NULL;",
			wantType: "varchar(10)",
		},
		{
			name:      "MODIFY from VARCHAR to ENUM",
			sql:       "CREATE TABLE users (id INT PRIMARY KEY, role VARCHAR(10) NOT NULL); ALTER TABLE users MODIFY role ENUM('a','b') NOT NULL;",
			wantType:  "users_role_enum",
			wantEnums: []parser.Enum{{Name: "users_role_enum", Values: []string{"a", "b"}}},
		},
		{
			name:     "MODIFY widening a SET",
			sql:      "CREATE TABLE users (id INT PRIMARY KEY, role SET('a','b') NOT NULL); ALTER TABLE users MODIFY role SET('a','b','c') NOT NULL;",
			wantType: "users_role_set",
			wantSets: []parser.Set{{Name: "users_role_set", Values: []string{"a", "b", "c"}}},
		},
		{
			name:     "MODIFY from ENUM to SET",
			sql:      "CREATE TABLE users (id INT PRIMARY KEY, role ENUM('a','b') NOT NULL); ALTER TABLE users MODIFY role SET('a','b') NOT NULL;",
			wantType: "users_role_set",
			wantSets: []parser.Set{{Name: "users_role_set", Values: []string{"a", "b"}}},
		},
		{
			name:      "MODIFY on one ENUM column leaves another table's ENUM column alone",
			sql:       "CREATE TABLE users (id INT PRIMARY KEY, role ENUM('a','b') NOT NULL); CREATE TABLE admins (id INT PRIMARY KEY, role ENUM('x') NOT NULL); ALTER TABLE users MODIFY role ENUM('a','b','c') NOT NULL;",
			wantType:  "users_role_enum",
			wantEnums: []parser.Enum{{Name: "admins_role_enum", Values: []string{"x"}}, {Name: "users_role_enum", Values: []string{"a", "b", "c"}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newParser(t)
			if err := p.Parse("test.sql", []byte(tt.sql)); err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			schema := p.Schema()
			users := schema.Tables[0]
			if got := users.Columns[len(users.Columns)-1].Type; got != tt.wantType {
				t.Errorf("Parse(%q) column type = %q, want %q", tt.sql, got, tt.wantType)
			}
			if diff := cmp.Diff(tt.wantEnums, schema.Enums, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Parse(%q) enums mismatch (-want +got):\n%s", tt.sql, diff)
			}
			if diff := cmp.Diff(tt.wantSets, schema.Sets, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Parse(%q) sets mismatch (-want +got):\n%s", tt.sql, diff)
			}
		})
	}
}

// TestRedefineColumnKeepsSharedInlineEnumName covers MODIFY on an ENUM column
// whose inline name another column also carries, which RENAME COLUMN and
// RENAME TABLE make possible by keeping the name derived from the old names.
// Both entries survive, so generation still fails loudly on the duplicate Go
// type instead of resolving one column to the other's values.
func TestRedefineColumnKeepsSharedInlineEnumName(t *testing.T) {
	tests := []struct {
		name      string
		sql       string
		wantEnums []parser.Enum
	}{
		{
			name: "RENAME COLUMN then ADD COLUMN under the old name",
			sql:  "CREATE TABLE users (id INT PRIMARY KEY, role ENUM('a','b') NOT NULL); ALTER TABLE users RENAME COLUMN role TO kind; ALTER TABLE users ADD COLUMN role ENUM('x') NOT NULL; ALTER TABLE users MODIFY kind ENUM('a','b','c') NOT NULL;",
			wantEnums: []parser.Enum{
				{Name: "users_kind_enum", Values: []string{"a", "b", "c"}},
				{Name: "users_role_enum", Values: []string{"a", "b"}},
				{Name: "users_role_enum", Values: []string{"x"}},
			},
		},
		{
			name: "RENAME TABLE then CREATE TABLE under the old name",
			sql:  "CREATE TABLE users (id INT PRIMARY KEY, role ENUM('a','b') NOT NULL); RENAME TABLE users TO accounts; CREATE TABLE users (id INT PRIMARY KEY, role ENUM('x') NOT NULL); ALTER TABLE users MODIFY role ENUM('x','y') NOT NULL;",
			wantEnums: []parser.Enum{
				{Name: "users_role_enum", Values: []string{"a", "b"}},
				{Name: "users_role_enum", Values: []string{"x"}},
				{Name: "users_role_enum", Values: []string{"x", "y"}},
			},
		},
	}
	sortEnums := cmpopts.SortSlices(func(a, b parser.Enum) bool {
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return strings.Join(a.Values, ",") < strings.Join(b.Values, ",")
	})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newParser(t)
			if err := p.Parse("test.sql", []byte(tt.sql)); err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			if diff := cmp.Diff(tt.wantEnums, p.Schema().Enums, sortEnums); diff != "" {
				t.Errorf("Parse(%q) enums mismatch (-want +got):\n%s", tt.sql, diff)
			}
		})
	}
}

// TestRedefineColumnReplacesSeededInlineEnum covers `input.source: both`: a
// MODIFY in the DDL files replaces the introspected column's inline ENUM entry
// instead of registering a second one.
func TestRedefineColumnReplacesSeededInlineEnum(t *testing.T) {
	p := newParser(t)
	p.Seed(&parser.Schema{
		Tables: []parser.Table{{
			Name: "users",
			Columns: []parser.Column{
				{Name: "id", Type: "int", PrimaryKey: true},
				{Name: "role", Type: "users_role_enum"},
			},
		}},
		Enums: []parser.Enum{{Name: "users_role_enum", Values: []string{"a", "b"}}},
	})
	sql := "ALTER TABLE users MODIFY role ENUM('a','b','c') NOT NULL;"
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	want := []parser.Enum{{Name: "users_role_enum", Values: []string{"a", "b", "c"}}}
	if diff := cmp.Diff(want, p.Schema().Enums); diff != "" {
		t.Errorf("Parse(%q) after Seed enums mismatch (-want +got):\n%s", sql, diff)
	}
}

func TestAutoIncrementDetection(t *testing.T) {
	sql := `CREATE TABLE users (
		id INT PRIMARY KEY AUTO_INCREMENT,
		name VARCHAR(255) NOT NULL
	);`

	p := newParser(t)
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	table := p.Schema().Tables[0]
	cols := make(map[string]parser.Column)
	for _, c := range table.Columns {
		cols[c.Name] = c
	}

	if !cols["id"].AutoIncrement {
		t.Error("id should have AutoIncrement=true")
	}
	if cols["id"].Nullable {
		t.Error("id should have Nullable=false (AUTO_INCREMENT implies NOT NULL)")
	}
	if cols["name"].AutoIncrement {
		t.Error("name should have AutoIncrement=false")
	}
}

func TestUnsignedIntegerTypes(t *testing.T) {
	sql := `CREATE TABLE counters (
		small_val INT UNSIGNED,
		big_val BIGINT UNSIGNED
	);`

	p := newParser(t)
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	table := p.Schema().Tables[0]
	cols := make(map[string]parser.Column)
	for _, c := range table.Columns {
		cols[c.Name] = c
	}

	if cols["small_val"].Type != "int unsigned" {
		t.Errorf("small_val type = %q, want %q", cols["small_val"].Type, "int unsigned")
	}
	if cols["big_val"].Type != "bigint unsigned" {
		t.Errorf("big_val type = %q, want %q", cols["big_val"].Type, "bigint unsigned")
	}
}

func TestInlineComment(t *testing.T) {
	sql := `CREATE TABLE users (
		id INT PRIMARY KEY COMMENT 'Primary key',
		name VARCHAR(255) COMMENT 'User display name'
	) COMMENT='User accounts table';`

	p := newParser(t)
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	table := p.Schema().Tables[0]

	// Table comment.
	if table.Comment != "User accounts table" {
		t.Errorf("table comment = %q, want %q", table.Comment, "User accounts table")
	}

	// Column comments.
	cols := make(map[string]parser.Column)
	for _, c := range table.Columns {
		cols[c.Name] = c
	}
	if cols["id"].Comment != "Primary key" {
		t.Errorf("id comment = %q, want %q", cols["id"].Comment, "Primary key")
	}
	if cols["name"].Comment != "User display name" {
		t.Errorf("name comment = %q, want %q", cols["name"].Comment, "User display name")
	}
}

func TestAlterTableAddDropModifyColumn(t *testing.T) {
	sql := `
		CREATE TABLE users (
			id INT PRIMARY KEY AUTO_INCREMENT,
			name VARCHAR(255) NOT NULL
		);
		ALTER TABLE users ADD COLUMN email VARCHAR(255) NOT NULL;
		ALTER TABLE users DROP COLUMN name;
		ALTER TABLE users MODIFY COLUMN email VARCHAR(512) NOT NULL;
	`

	p := newParser(t)
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
		t.Errorf("columns after ALTER mismatch (-want +got):\n%s", diff)
	}

	// Verify MODIFY updated the type.
	for _, c := range table.Columns {
		if c.Name == "email" {
			if c.Type != "varchar(512)" {
				t.Errorf("email type after MODIFY = %q, want %q", c.Type, "varchar(512)")
			}
			if c.Nullable {
				t.Error("email should be NOT NULL after MODIFY")
			}
		}
	}
}

func TestAlterTableAddDropIndex(t *testing.T) {
	sql := `
		CREATE TABLE users (
			id INT PRIMARY KEY AUTO_INCREMENT,
			email VARCHAR(255) NOT NULL
		);
		ALTER TABLE users ADD INDEX idx_email (email);
	`

	p := newParser(t)
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	table := p.Schema().Tables[0]

	// Find the idx_email constraint.
	var found bool
	for _, c := range table.Constraints {
		if c.Name == "idx_email" && c.Type == parser.Index {
			found = true
			if diff := cmp.Diff([]string{"email"}, c.Columns); diff != "" {
				t.Errorf("idx_email columns mismatch (-want +got):\n%s", diff)
			}
		}
	}
	if !found {
		t.Error("idx_email constraint not found after ADD INDEX")
	}

	// Now drop it.
	sql2 := `ALTER TABLE users DROP INDEX idx_email;`
	if err := p.Parse("test2.sql", []byte(sql2)); err != nil {
		t.Fatalf("Parse() error on DROP INDEX: %v", err)
	}

	table = p.Schema().Tables[0]
	for _, c := range table.Constraints {
		if c.Name == "idx_email" {
			t.Error("idx_email should be removed after DROP INDEX")
		}
	}
}

func TestAlterTableAddDropConstraint(t *testing.T) {
	sql := `
		CREATE TABLE orders (
			id INT PRIMARY KEY AUTO_INCREMENT,
			user_id INT,
			amount DECIMAL(10,2)
		);
		ALTER TABLE orders ADD CONSTRAINT fk_user FOREIGN KEY (user_id) REFERENCES users(id);
		ALTER TABLE orders ADD CONSTRAINT chk_amount CHECK (amount > 0);
	`

	p := newParser(t)
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	table := p.Schema().Tables[0]

	// FK constraint. MySQL also materializes a backing index named after the
	// FK constraint (see synthesizeForeignKeyIndexes), so fk_user names both a
	// ForeignKey and an Index — look the FK up by type, not name alone.
	fk := findConstraint(table.Constraints, "fk_user", parser.ForeignKey)
	if fk == nil {
		t.Fatal("fk_user foreign key constraint not found")
	}
	if fk.ReferenceTable != "users" {
		t.Errorf("FK reference table = %q, want %q", fk.ReferenceTable, "users")
	}
	if diff := cmp.Diff([]string{"id"}, fk.ReferenceColumns); diff != "" {
		t.Errorf("FK reference columns mismatch (-want +got):\n%s", diff)
	}

	// The synthesized backing index carries the FK's referencing column.
	fkIdx := findConstraint(table.Constraints, "fk_user", parser.Index)
	if fkIdx == nil {
		t.Fatal("fk_user backing index not synthesized")
	}
	if diff := cmp.Diff([]string{"user_id"}, fkIdx.Columns); diff != "" {
		t.Errorf("FK backing index columns mismatch (-want +got):\n%s", diff)
	}

	// FK should be propagated to column.
	cols := make(map[string]parser.Column)
	for _, c := range table.Columns {
		cols[c.Name] = c
	}
	if cols["user_id"].FKReference == nil {
		t.Error("user_id FKReference should be set after ADD CONSTRAINT FK")
	}

	// CHECK constraint.
	chk := findConstraint(table.Constraints, "chk_amount", parser.Check)
	if chk == nil {
		t.Fatal("chk_amount constraint not found")
	}
	if chk.CheckExpression == "" {
		t.Error("CHECK expression is empty, want non-empty")
	}

	// Drop the FK.
	sql2 := `ALTER TABLE orders DROP FOREIGN KEY fk_user;`
	if err := p.Parse("test2.sql", []byte(sql2)); err != nil {
		t.Fatalf("Parse() error on DROP: %v", err)
	}

	table = p.Schema().Tables[0]
	if findConstraint(table.Constraints, "fk_user", parser.ForeignKey) != nil {
		t.Error("fk_user foreign key should be removed after DROP FOREIGN KEY")
	}
	// MySQL keeps the backing index after DROP FOREIGN KEY (a separate
	// DROP INDEX is required), so it must survive here too.
	if findConstraint(table.Constraints, "fk_user", parser.Index) == nil {
		t.Error("fk_user backing index should survive DROP FOREIGN KEY")
	}

	// FK reference should be removed from column.
	cols = make(map[string]parser.Column)
	for _, c := range table.Columns {
		cols[c.Name] = c
	}
	if cols["user_id"].FKReference != nil {
		t.Error("user_id FKReference should be nil after DROP CONSTRAINT FK")
	}

	// Drop the CHECK.
	sql3 := `ALTER TABLE orders DROP CHECK chk_amount;`
	if err := p.Parse("test3.sql", []byte(sql3)); err != nil {
		t.Fatalf("Parse() error on DROP CHECK: %v", err)
	}

	table = p.Schema().Tables[0]
	for _, c := range table.Constraints {
		if c.Name == "chk_amount" {
			t.Error("chk_amount should be removed after DROP CHECK")
		}
	}
}

func TestTableWithNoPK(t *testing.T) {
	sql := `CREATE TABLE logs (
		message TEXT,
		created_at DATETIME
	);`

	p := newParser(t)
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	table := p.Schema().Tables[0]
	for _, col := range table.Columns {
		if col.PrimaryKey {
			t.Errorf("column %q should not be PrimaryKey", col.Name)
		}
	}

	// No PK or other constraints should exist.
	for _, c := range table.Constraints {
		if c.Type == parser.PrimaryKey {
			t.Error("table should have no PrimaryKey constraint")
		}
	}
}

// TestDropPrimaryKey checks that ALTER TABLE … DROP PRIMARY KEY leaves no
// column flagged as a key however the key was declared, and that the former
// key columns stay NOT NULL, as MySQL leaves them.
func TestDropPrimaryKey(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want []string
	}{
		{
			name: "inline primary key",
			sql:  "CREATE TABLE users (id INT PRIMARY KEY, name TEXT); ALTER TABLE users DROP PRIMARY KEY;",
			want: []string{"users.id int", "users.name text null"},
		},
		{
			name: "table-level primary key",
			sql:  "CREATE TABLE users (id INT, name TEXT, PRIMARY KEY (id)); ALTER TABLE users DROP PRIMARY KEY;",
			want: []string{"users.id int", "users.name text null"},
		},
		{
			name: "composite primary key",
			sql:  "CREATE TABLE post_tags (post_id INT, tag_id INT, PRIMARY KEY (post_id, tag_id)); ALTER TABLE post_tags DROP PRIMARY KEY;",
			want: []string{"post_tags.post_id int", "post_tags.tag_id int"},
		},
		{
			name: "primary key added inline by MODIFY",
			sql:  "CREATE TABLE users (id INT, name TEXT); ALTER TABLE users MODIFY id BIGINT PRIMARY KEY; ALTER TABLE users DROP PRIMARY KEY;",
			want: []string{"users.id bigint", "users.name text null"},
		},
		{
			name: "inline primary key carried over a MODIFY",
			sql:  "CREATE TABLE users (id INT PRIMARY KEY, name TEXT); ALTER TABLE users MODIFY id BIGINT; ALTER TABLE users DROP PRIMARY KEY;",
			want: []string{"users.id bigint", "users.name text null"},
		},
		{
			name: "primary key added by ADD PRIMARY KEY",
			sql:  "CREATE TABLE users (id INT, name TEXT); ALTER TABLE users ADD PRIMARY KEY (id); ALTER TABLE users DROP PRIMARY KEY;",
			want: []string{"users.id int", "users.name text null"},
		},
		{
			name: "inline primary key replaced by a key on another column",
			sql:  "CREATE TABLE users (id INT PRIMARY KEY, code INT NOT NULL); ALTER TABLE users DROP PRIMARY KEY, ADD PRIMARY KEY (code);",
			want: []string{"users.id int", "users.code int pk"},
		},
		{
			name: "table-level primary key replaced with ADD before DROP",
			sql:  "CREATE TABLE users (id INT, code INT NOT NULL, PRIMARY KEY (id)); ALTER TABLE users ADD PRIMARY KEY (code), DROP PRIMARY KEY;",
			want: []string{"users.id int", "users.code int pk"},
		},
		{
			name: "inline primary key replaced with ADD before DROP",
			sql:  "CREATE TABLE users (id INT PRIMARY KEY, code INT NOT NULL); ALTER TABLE users ADD PRIMARY KEY (code), DROP PRIMARY KEY;",
			want: []string{"users.id int", "users.code int pk"},
		},
		{
			name: "inline primary key replaced by MODIFY before DROP",
			sql:  "CREATE TABLE users (id INT PRIMARY KEY, code INT NOT NULL); ALTER TABLE users MODIFY code INT NOT NULL PRIMARY KEY, DROP PRIMARY KEY;",
			want: []string{"users.id int", "users.code int pk"},
		},
		{
			name: "inline primary key replaced by ADD COLUMN before DROP",
			sql:  "CREATE TABLE users (id INT PRIMARY KEY, name TEXT); ALTER TABLE users ADD COLUMN code INT NOT NULL PRIMARY KEY, DROP PRIMARY KEY;",
			want: []string{"users.id int", "users.name text null", "users.code int pk"},
		},
		{
			name: "former key column redefined nullable in the same ALTER",
			sql:  "CREATE TABLE users (id INT PRIMARY KEY, name TEXT); ALTER TABLE users MODIFY id BIGINT, DROP PRIMARY KEY;",
			want: []string{"users.id bigint null", "users.name text null"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newParser(t)
			if err := p.Parse("test.sql", []byte(tt.sql)); err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			var got []string
			for _, tbl := range p.Schema().Tables {
				for _, col := range tbl.Columns {
					s := tbl.Name + "." + col.Name + " " + col.Type
					if col.PrimaryKey {
						s += " pk"
					}
					if col.Nullable {
						s += " null"
					}
					got = append(got, s)
				}
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Parse(%q) columns mismatch (-want +got):\n%s", tt.sql, diff)
			}
		})
	}
}

func TestParserReentrant(t *testing.T) {
	sql1 := `CREATE TABLE users (id INT PRIMARY KEY AUTO_INCREMENT, name VARCHAR(255));`
	sql2 := `CREATE TABLE posts (id INT PRIMARY KEY AUTO_INCREMENT, user_id INT, title VARCHAR(255));`

	p := newParser(t)
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
		order_id INT,
		product_id INT,
		quantity INT,
		PRIMARY KEY (order_id, product_id),
		UNIQUE KEY uq_order_qty (order_id, quantity),
		CONSTRAINT order_items_order_fk FOREIGN KEY (order_id) REFERENCES orders(id),
		CONSTRAINT order_items_qty_check CHECK (quantity > 0)
	);`

	p := newParser(t)
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
			// Index constraints are not checked in this test.
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

func TestForeignKeyBackingIndexSynthesis(t *testing.T) {
	type wantIdx struct {
		Name    string
		Columns []string
	}
	tests := []struct {
		name string
		ddl  string
		want []wantIdx
	}{
		{
			name: "unnamed FK synthesizes index named after column",
			ddl: `CREATE TABLE orders (
				id INT PRIMARY KEY,
				user_id INT,
				FOREIGN KEY (user_id) REFERENCES users(id)
			);`,
			want: []wantIdx{{Name: "user_id", Columns: []string{"user_id"}}},
		},
		{
			name: "named FK synthesizes index named after constraint",
			ddl: `CREATE TABLE orders (
				id INT PRIMARY KEY,
				user_id INT,
				CONSTRAINT fk_user FOREIGN KEY (user_id) REFERENCES users(id)
			);`,
			want: []wantIdx{{Name: "fk_user", Columns: []string{"user_id"}}},
		},
		{
			name: "single-column PK covers FK, no index",
			ddl: `CREATE TABLE memberships (
				user_id INT PRIMARY KEY,
				FOREIGN KEY (user_id) REFERENCES users(id)
			);`,
			want: nil,
		},
		{
			name: "explicit KEY covers FK, no duplicate index",
			ddl: `CREATE TABLE orders (
				id INT PRIMARY KEY,
				user_id INT,
				KEY idx_user (user_id),
				FOREIGN KEY (user_id) REFERENCES users(id)
			);`,
			want: []wantIdx{{Name: "idx_user", Columns: []string{"user_id"}}},
		},
		{
			name: "FK on leftmost prefix of composite PK, no index",
			ddl: `CREATE TABLE order_items (
				order_id INT,
				product_id INT,
				PRIMARY KEY (order_id, product_id),
				FOREIGN KEY (order_id) REFERENCES orders(id)
			);`,
			want: nil,
		},
		{
			name: "composite FK synthesizes composite index",
			ddl: `CREATE TABLE line_refs (
				id INT PRIMARY KEY,
				order_id INT,
				product_id INT,
				CONSTRAINT fk_line FOREIGN KEY (order_id, product_id) REFERENCES order_items(order_id, product_id)
			);`,
			want: []wantIdx{{Name: "fk_line", Columns: []string{"order_id", "product_id"}}},
		},
		{
			name: "two FKs on same column share one backing index",
			ddl: `CREATE TABLE audit (
				id INT PRIMARY KEY,
				user_id INT,
				CONSTRAINT fk_a FOREIGN KEY (user_id) REFERENCES users(id),
				CONSTRAINT fk_b FOREIGN KEY (user_id) REFERENCES admins(id)
			);`,
			want: []wantIdx{{Name: "fk_a", Columns: []string{"user_id"}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newParser(t)
			if err := p.Parse("test.sql", []byte(tt.ddl)); err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			table := p.Schema().Tables[0]

			var got []wantIdx
			for _, c := range table.Constraints {
				if c.Type == parser.Index {
					got = append(got, wantIdx{Name: c.Name, Columns: c.Columns})
				}
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("index constraints mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDuplicateCreateTable(t *testing.T) {
	p := newParser(t)
	sql := `
		CREATE TABLE users (id INT PRIMARY KEY);
		CREATE TABLE users (id INT PRIMARY KEY, name VARCHAR(255));
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
	p := newParser(t)
	sql := `
		CREATE TABLE users (id INT PRIMARY KEY);
		CREATE TABLE products (id INT PRIMARY KEY);
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
	p := newParser(t)
	sql := `
		CREATE TABLE users (id INT PRIMARY KEY);
		CREATE TABLE orders (
			id INT PRIMARY KEY,
			user_id INT,
			FOREIGN KEY (user_id) REFERENCES users(id)
		);
		DROP TABLE users;
	`
	err := p.Parse("test.sql", []byte(sql))
	if err == nil {
		t.Fatal("expected error when dropping table referenced by FK, got nil")
	}
}

func TestDropTableCrossFile(t *testing.T) {
	p := newParser(t)
	if err := p.Parse("001.sql", []byte(`
		CREATE TABLE users (id INT PRIMARY KEY);
		CREATE TABLE orders (
			id INT PRIMARY KEY,
			user_id INT,
			FOREIGN KEY (user_id) REFERENCES users(id)
		);
	`)); err != nil {
		t.Fatalf("Parse(001.sql) error: %v", err)
	}
	if err := p.Parse("002.sql", []byte(`DROP TABLE users;`)); err == nil {
		t.Fatal("expected error when dropping cross-file table referenced by FK, got nil")
	}
}

func TestDropTableNoReferences(t *testing.T) {
	p := newParser(t)
	sql := `
		CREATE TABLE users (id INT PRIMARY KEY);
		CREATE TABLE legacy (id INT PRIMARY KEY);
		DROP TABLE legacy;
	`
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}
	if len(p.Schema().Tables) != 1 {
		t.Errorf("expected 1 table, got %d", len(p.Schema().Tables))
	}
}

func TestRenameColumn(t *testing.T) {
	p := newParser(t)
	sql := `
		CREATE TABLE users (id INT PRIMARY KEY, name VARCHAR(100));
		ALTER TABLE users RENAME COLUMN name TO full_name;
	`
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	table := p.Schema().Tables[0]
	colNames := make([]string, len(table.Columns))
	for i, c := range table.Columns {
		colNames[i] = c.Name
	}
	want := []string{"id", "full_name"}
	if diff := cmp.Diff(want, colNames); diff != "" {
		t.Errorf("column names mismatch (-want +got):\n%s", diff)
	}
}

func TestRenameColumnNotFound(t *testing.T) {
	p := newParser(t)
	sql := `
		CREATE TABLE users (id INT PRIMARY KEY);
		ALTER TABLE users RENAME COLUMN nonexistent TO something;
	`
	err := p.Parse("test.sql", []byte(sql))
	if err == nil {
		t.Fatal("expected error renaming nonexistent column")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

func TestRenameTable(t *testing.T) {
	p := newParser(t)
	sql := `
		CREATE TABLE users (id INT PRIMARY KEY, name VARCHAR(100));
		ALTER TABLE users RENAME TO people;
	`
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	schema := p.Schema()
	if len(schema.Tables) != 1 {
		t.Fatalf("expected 1 table, got %d", len(schema.Tables))
	}
	if schema.Tables[0].Name != "people" {
		t.Errorf("table name = %q, want %q", schema.Tables[0].Name, "people")
	}
}

// TestRenameTableCarriesForeignKeys pins that a foreign key into a renamed
// table follows it to the new name, as InnoDB does (measured on MySQL 8:
// SHOW CREATE TABLE prints REFERENCES `zz_people` after the rename). Left on
// the old name, relationship detection drops the edge.
func TestRenameTableCarriesForeignKeys(t *testing.T) {
	p := newParser(t)
	sql := `
		CREATE TABLE users (id INT PRIMARY KEY);
		CREATE TABLE posts (id INT PRIMARY KEY, user_id INT REFERENCES users(id));
		CREATE TABLE comments (id INT PRIMARY KEY, user_id INT, FOREIGN KEY (user_id) REFERENCES users(id));
		ALTER TABLE users RENAME TO accounts;
	`
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

// TestRenameTableStatement pins the standalone RENAME TABLE statement: each
// pair renames through the same path as ALTER TABLE ... RENAME, left to
// right, and keys that reference a renamed table follow it, as InnoDB does
// (measured on MySQL 8: SHOW CREATE TABLE prints REFERENCES `accounts`).
func TestRenameTableStatement(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		want    []string
		wantErr string
	}{
		{
			name: "renames the table and its references follow",
			sql: `CREATE TABLE users (id INT PRIMARY KEY);
				CREATE TABLE posts (id INT PRIMARY KEY, user_id INT, FOREIGN KEY (user_id) REFERENCES users(id));
				RENAME TABLE users TO accounts;`,
			want: []string{"accounts", "posts", "posts.user_id -> accounts", "posts constraint -> accounts"},
		},
		{
			name: "pairs apply left to right, so a swap works",
			sql: `CREATE TABLE a (id INT PRIMARY KEY);
				CREATE TABLE b (id INT PRIMARY KEY, a_id INT, FOREIGN KEY (a_id) REFERENCES a(id));
				RENAME TABLE a TO tmp, b TO a, tmp TO b;`,
			want: []string{"b", "a", "a.a_id -> b", "a constraint -> b"},
		},
		{
			name:    "a missing table is an error",
			sql:     `RENAME TABLE users TO accounts;`,
			wantErr: "table users not found",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newParser(t)
			err := p.Parse("test.sql", []byte(tt.sql))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Parse() error = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			var got []string
			for _, tbl := range p.Schema().Tables {
				got = append(got, tbl.Name)
			}
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
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("tables and FK targets after RENAME TABLE mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestRenameColumnCarriesReferences pins that renaming a column, by RENAME
// COLUMN or CHANGE COLUMN, carries the constraints and foreign keys naming
// it along, as InnoDB does (measured on MySQL 8: SHOW CREATE TABLE prints
// REFERENCES `accounts` (`account_id`) after the rename).
func TestRenameColumnCarriesReferences(t *testing.T) {
	const tables = `CREATE TABLE users (id INT, PRIMARY KEY (id));
		CREATE TABLE posts (id INT PRIMARY KEY, user_id INT, FOREIGN KEY (user_id) REFERENCES users(id));
		CREATE TABLE post_tags (post_id INT, tag_id INT, PRIMARY KEY (post_id, tag_id), FOREIGN KEY (post_id) REFERENCES posts(id));
	`
	tests := []struct {
		name  string
		alter string
		want  []string
	}{
		{
			name:  "RENAME COLUMN on a referenced primary key",
			alter: "ALTER TABLE users RENAME COLUMN id TO user_id;",
			want: []string{
				"users PRIMARY KEY [user_id]",
				"posts.user_id -> users(user_id)",
				"posts FOREIGN KEY [user_id] -> users[user_id]",
				"posts INDEX [user_id]",
				"post_tags.post_id -> posts(id)",
				"post_tags PRIMARY KEY [post_id tag_id]",
				"post_tags FOREIGN KEY [post_id] -> posts[id]",
			},
		},
		{
			name:  "CHANGE COLUMN on a referenced primary key",
			alter: "ALTER TABLE users CHANGE COLUMN id user_id INT NOT NULL;",
			want: []string{
				"users PRIMARY KEY [user_id]",
				"posts.user_id -> users(user_id)",
				"posts FOREIGN KEY [user_id] -> users[user_id]",
				"posts INDEX [user_id]",
				"post_tags.post_id -> posts(id)",
				"post_tags PRIMARY KEY [post_id tag_id]",
				"post_tags FOREIGN KEY [post_id] -> posts[id]",
			},
		},
		{
			name:  "RENAME COLUMN on a junction key column",
			alter: "ALTER TABLE post_tags RENAME COLUMN post_id TO article_id;",
			want: []string{
				"users PRIMARY KEY [id]",
				"posts.user_id -> users(id)",
				"posts FOREIGN KEY [user_id] -> users[id]",
				"posts INDEX [user_id]",
				"post_tags.article_id -> posts(id)",
				"post_tags PRIMARY KEY [article_id tag_id]",
				"post_tags FOREIGN KEY [article_id] -> posts[id]",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newParser(t)
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
				t.Errorf("references after the rename mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestRedefineColumnKeepsKeys pins that MODIFY and CHANGE COLUMN keep the
// PRIMARY KEY, UNIQUE and FOREIGN KEY flags of the column they redefine, inline
// or table-level, as MySQL does (measured on MySQL 8.0: SHOW CREATE TABLE still
// prints every key after the ALTER, and a primary-key column redefined without
// NOT NULL comes back NOT NULL). Losing the PK flag drops the table out of
// generation.
func TestRedefineColumnKeepsKeys(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want []string
	}{
		{
			name: "MODIFY on a table-level primary key",
			sql:  "CREATE TABLE users (id INT, name TEXT, PRIMARY KEY (id)); ALTER TABLE users MODIFY id BIGINT;",
			want: []string{"users.id bigint pk", "users.name text null"},
		},
		{
			name: "CHANGE on a table-level primary key without a rename",
			sql:  "CREATE TABLE users (id INT, name TEXT, PRIMARY KEY (id)); ALTER TABLE users CHANGE COLUMN id id BIGINT;",
			want: []string{"users.id bigint pk", "users.name text null"},
		},
		{
			name: "CHANGE on a table-level primary key with a rename",
			sql:  "CREATE TABLE users (id INT, name TEXT, PRIMARY KEY (id)); ALTER TABLE users CHANGE COLUMN id user_id BIGINT;",
			want: []string{"users.user_id bigint pk", "users.name text null"},
		},
		{
			name: "MODIFY on a composite primary key member",
			sql:  "CREATE TABLE post_tags (post_id INT, tag_id INT, PRIMARY KEY (post_id, tag_id)); ALTER TABLE post_tags MODIFY post_id BIGINT;",
			want: []string{"post_tags.post_id bigint pk", "post_tags.tag_id int pk"},
		},
		{
			name: "CHANGE with a rename on a composite primary key member",
			sql:  "CREATE TABLE post_tags (post_id INT, tag_id INT, PRIMARY KEY (post_id, tag_id)); ALTER TABLE post_tags CHANGE COLUMN tag_id label_id BIGINT;",
			want: []string{"post_tags.post_id int pk", "post_tags.label_id bigint pk"},
		},
		{
			name: "MODIFY on an inline primary key",
			sql:  "CREATE TABLE users (id INT PRIMARY KEY, name TEXT); ALTER TABLE users MODIFY id BIGINT;",
			want: []string{"users.id bigint pk", "users.name text null"},
		},
		{
			name: "MODIFY to NULL on a primary key column, which MySQL rejects, keeps it NOT NULL",
			sql:  "CREATE TABLE users (id INT, name TEXT, PRIMARY KEY (id)); ALTER TABLE users MODIFY id BIGINT NULL;",
			want: []string{"users.id bigint pk", "users.name text null"},
		},
		{
			name: "MODIFY on a table-level UNIQUE column",
			sql:  "CREATE TABLE users (id INT PRIMARY KEY, email VARCHAR(10), UNIQUE KEY uq_email (email)); ALTER TABLE users MODIFY email VARCHAR(20);",
			want: []string{"users.id int pk", "users.email varchar(20) null unique"},
		},
		{
			name: "CHANGE with a rename on a table-level UNIQUE column",
			sql:  "CREATE TABLE users (id INT PRIMARY KEY, email VARCHAR(10), UNIQUE KEY uq_email (email)); ALTER TABLE users CHANGE COLUMN email mail VARCHAR(20);",
			want: []string{"users.id int pk", "users.mail varchar(20) null unique"},
		},
		{
			name: "MODIFY on an inline UNIQUE column",
			sql:  "CREATE TABLE users (id INT PRIMARY KEY, email VARCHAR(10) UNIQUE); ALTER TABLE users MODIFY email VARCHAR(20);",
			want: []string{"users.id int pk", "users.email varchar(20) null unique"},
		},
		{
			name: "MODIFY on an inline UNIQUE column keeps it unique after its table-level UNIQUE KEY is dropped",
			sql:  "CREATE TABLE users (id INT PRIMARY KEY, email VARCHAR(10) UNIQUE, UNIQUE KEY uq (email)); ALTER TABLE users MODIFY email VARCHAR(20); ALTER TABLE users DROP INDEX uq;",
			want: []string{"users.id int pk", "users.email varchar(20) null unique"},
		},
		{
			name: "MODIFY on a table-level foreign key column",
			sql:  "CREATE TABLE users (id INT PRIMARY KEY); CREATE TABLE posts (id INT PRIMARY KEY, user_id INT, FOREIGN KEY (user_id) REFERENCES users(id)); ALTER TABLE posts MODIFY user_id INT NOT NULL;",
			want: []string{"users.id int pk", "posts.id int pk", "posts.user_id int -> users(id)"},
		},
		{
			name: "CHANGE with a rename on a table-level foreign key column",
			sql:  "CREATE TABLE users (id INT PRIMARY KEY); CREATE TABLE posts (id INT PRIMARY KEY, user_id INT, FOREIGN KEY (user_id) REFERENCES users(id)); ALTER TABLE posts CHANGE COLUMN user_id author_id INT;",
			want: []string{"users.id int pk", "posts.id int pk", "posts.author_id int null -> users(id)"},
		},
		{
			name: "inline UNIQUE on the new definition marks a plain column",
			sql:  "CREATE TABLE users (id INT PRIMARY KEY, email VARCHAR(10)); ALTER TABLE users MODIFY email VARCHAR(20) UNIQUE;",
			want: []string{"users.id int pk", "users.email varchar(20) null unique"},
		},
		{
			name: "inline PRIMARY KEY on the new definition marks a plain column",
			sql:  "CREATE TABLE users (id INT, name TEXT); ALTER TABLE users MODIFY id BIGINT PRIMARY KEY;",
			want: []string{"users.id bigint pk", "users.name text null"},
		},
		{
			name: "a column with no key still takes its nullability from the new definition",
			sql:  "CREATE TABLE users (id INT PRIMARY KEY, name VARCHAR(10) NOT NULL); ALTER TABLE users MODIFY name VARCHAR(20);",
			want: []string{"users.id int pk", "users.name varchar(20) null"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newParser(t)
			if err := p.Parse("test.sql", []byte(tt.sql)); err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			var got []string
			for _, tbl := range p.Schema().Tables {
				for _, col := range tbl.Columns {
					s := tbl.Name + "." + col.Name + " " + col.Type
					if col.PrimaryKey {
						s += " pk"
					}
					if col.Nullable {
						s += " null"
					}
					if col.Unique {
						s += " unique"
					}
					if ref := col.FKReference; ref != nil {
						s += fmt.Sprintf(" -> %s(%s)", ref.Table, ref.Column)
					}
					got = append(got, s)
				}
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Parse(%q) columns mismatch (-want +got):\n%s", tt.sql, diff)
			}
		})
	}
}

func TestChangeColumn(t *testing.T) {
	p := newParser(t)
	sql := `
		CREATE TABLE users (id INT PRIMARY KEY, name VARCHAR(50));
		ALTER TABLE users CHANGE COLUMN name full_name VARCHAR(100) NOT NULL;
	`
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	table := p.Schema().Tables[0]
	if len(table.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(table.Columns))
	}

	col := table.Columns[1]
	if col.Name != "full_name" {
		t.Errorf("column name = %q, want %q", col.Name, "full_name")
	}
	if col.Type != "varchar(100)" {
		t.Errorf("column type = %q, want %q", col.Type, "varchar(100)")
	}
	if col.Nullable {
		t.Error("column should not be nullable after CHANGE with NOT NULL")
	}
}

func TestChangeColumnNotFound(t *testing.T) {
	p := newParser(t)
	sql := `
		CREATE TABLE users (id INT PRIMARY KEY);
		ALTER TABLE users CHANGE COLUMN nonexistent new_name INT;
	`
	err := p.Parse("test.sql", []byte(sql))
	if err == nil {
		t.Fatal("expected error changing nonexistent column")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

func TestMigrationSequence(t *testing.T) {
	p := newParser(t)

	if err := p.Parse("001_init.sql", []byte(`
		CREATE TABLE users (
			id INT AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(100) NOT NULL,
			email VARCHAR(255)
		);
	`)); err != nil {
		t.Fatalf("001: %v", err)
	}

	if err := p.Parse("002_modify.sql", []byte(`
		ALTER TABLE users ADD COLUMN status VARCHAR(20) DEFAULT 'active';
		ALTER TABLE users MODIFY COLUMN email VARCHAR(255) NOT NULL;
	`)); err != nil {
		t.Fatalf("002: %v", err)
	}

	if err := p.Parse("003_rename.sql", []byte(`
		ALTER TABLE users RENAME COLUMN name TO full_name;
		ALTER TABLE users CHANGE COLUMN status state VARCHAR(30) NOT NULL;
	`)); err != nil {
		t.Fatalf("003: %v", err)
	}

	table := p.Schema().Tables[0]
	cols := make(map[string]parser.Column)
	for _, c := range table.Columns {
		cols[c.Name] = c
	}

	if len(cols) != 4 {
		t.Fatalf("expected 4 columns, got %d", len(cols))
	}

	if _, ok := cols["full_name"]; !ok {
		t.Error("expected 'full_name' column (renamed from 'name')")
	}
	if cols["email"].Nullable {
		t.Error("email should be NOT NULL after MODIFY")
	}
	if cols["state"].Type != "varchar(30)" {
		t.Errorf("state type = %q, want %q", cols["state"].Type, "varchar(30)")
	}
	if cols["state"].Nullable {
		t.Error("state should be NOT NULL after CHANGE")
	}
}

func TestParseViewSQL(t *testing.T) {
	sql := `CREATE VIEW product_summary AS
SELECT
    p.id,
    p.name,
    AVG(r.rating) AS avg_rating,
    COUNT(r.id) AS review_count
FROM products p
LEFT JOIN reviews r ON r.product_id = p.id
GROUP BY p.id, p.name;`

	got, err := mysql.ParseViewSQL([]byte(sql))
	if err != nil {
		t.Fatalf("ParseViewSQL() error: %v", err)
	}

	if got.Name != "product_summary" {
		t.Errorf("Name = %q, want %q", got.Name, "product_summary")
	}

	if len(got.Columns) != 4 {
		t.Fatalf("columns = %d, want 4", len(got.Columns))
	}

	// Verify first column.
	if got.Columns[0].Alias != "id" || got.Columns[0].SourceTable != "p" {
		t.Errorf("col[0] = %+v, want alias=id sourceTable=p", got.Columns[0])
	}

	// Verify aggregate columns.
	if got.Columns[2].Aggregate != "AVG" {
		t.Errorf("col[2].Aggregate = %q, want %q", got.Columns[2].Aggregate, "AVG")
	}
	if got.Columns[3].Aggregate != "COUNT" {
		t.Errorf("col[3].Aggregate = %q, want %q", got.Columns[3].Aggregate, "COUNT")
	}

	// Verify table aliases.
	if got.TableAliases["p"] != "products" {
		t.Errorf("alias p = %q, want %q", got.TableAliases["p"], "products")
	}
}

func TestCreateViewSkippedGracefully(t *testing.T) {
	sql := `CREATE TABLE users (id INT AUTO_INCREMENT PRIMARY KEY, name VARCHAR(255));
CREATE VIEW active_users AS SELECT id, name FROM users WHERE id > 0;`

	p, err := mysql.New()
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
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
	sql := `CREATE TABLE users (id INT AUTO_INCREMENT PRIMARY KEY);
DROP VIEW IF EXISTS old_view;`

	p, err := mysql.New()
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	schema := p.Schema()
	if len(schema.Tables) != 1 {
		t.Fatalf("tables = %d, want 1", len(schema.Tables))
	}
}

func TestInlineIndex_UsingMethod(t *testing.T) {
	sql := `CREATE TABLE users (
    id BIGINT PRIMARY KEY,
    email VARCHAR(255) NOT NULL,
    INDEX idx_email_hash (email) USING HASH,
    INDEX idx_email_default (email)
);`

	p := newParser(t)
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
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

	hashIdx, ok := byName["idx_email_hash"]
	if !ok {
		t.Fatalf("missing idx_email_hash; constraints=%+v", table.Constraints)
	}
	if hashIdx.Type != parser.Index {
		t.Errorf("hash: Type=%v, want Index", hashIdx.Type)
	}
	if hashIdx.Method != "hash" {
		t.Errorf("hash: Method=%q, want %q", hashIdx.Method, "hash")
	}

	// Without USING, MySQL parser leaves Method empty — the manifest builder
	// falls back to "btree" at serialization time.
	defaultIdx, ok := byName["idx_email_default"]
	if !ok {
		t.Fatalf("missing idx_email_default; constraints=%+v", table.Constraints)
	}
	if defaultIdx.Method != "" {
		t.Errorf("default: Method=%q, want empty (manifest fills in btree)", defaultIdx.Method)
	}
}

// TestDropIndexRecomputesColumnUnique pins that dropping a table-
// level UNIQUE index must re-derive col.Unique from the remaining state
// (other unconditional UNIQUEs + the column's InlineUnique flag) rather
// than leaving the column flag desynchronized from the constraint set.
// MySQL has no partial-UNIQUE concept, so the partial-UNIQUE branch of
// the bug applies only to PostgreSQL / SQLite; the inline + table-level
// case and the lone-UNIQUE positive control are exercised here.
func TestDropIndexRecomputesColumnUnique(t *testing.T) {
	tests := []struct {
		name        string
		setupSQL    string
		dropSQL     string
		column      string
		wantUnique  bool
		wantComment string
	}{
		{
			name: "inline UNIQUE plus table-level UNIQUE — drop table-level keeps Unique",
			setupSQL: `
				CREATE TABLE users (
					id INT PRIMARY KEY,
					email VARCHAR(255) UNIQUE,
					UNIQUE KEY uk_email_extra (email)
				);
			`,
			dropSQL:     `ALTER TABLE users DROP INDEX uk_email_extra;`,
			column:      "email",
			wantUnique:  true,
			wantComment: "inline UNIQUE remains in force",
		},
		{
			name: "two table-level UNIQUEs — drop one keeps Unique",
			setupSQL: `
				CREATE TABLE users (
					id INT PRIMARY KEY,
					email VARCHAR(255) NOT NULL,
					UNIQUE KEY uk_email_a (email),
					UNIQUE KEY uk_email_b (email)
				);
			`,
			dropSQL:     `ALTER TABLE users DROP INDEX uk_email_b;`,
			column:      "email",
			wantUnique:  true,
			wantComment: "second table-level UNIQUE still covers the column",
		},
		{
			name: "single table-level UNIQUE — drop it clears Unique",
			setupSQL: `
				CREATE TABLE users (
					id INT PRIMARY KEY,
					email VARCHAR(255) NOT NULL,
					UNIQUE KEY uk_email (email)
				);
			`,
			dropSQL:     `ALTER TABLE users DROP INDEX uk_email;`,
			column:      "email",
			wantUnique:  false,
			wantComment: "no remaining UNIQUE and no inline UNIQUE",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newParser(t)
			if err := p.Parse("setup.sql", []byte(tt.setupSQL)); err != nil {
				t.Fatalf("Parse(setup) error: %v", err)
			}
			if err := p.Parse("drop.sql", []byte(tt.dropSQL)); err != nil {
				t.Fatalf("Parse(drop) error: %v", err)
			}

			schema := p.Schema()
			if len(schema.Tables) == 0 {
				t.Fatalf("no tables in schema")
			}
			var col *parser.Column
			for i := range schema.Tables[0].Columns {
				if schema.Tables[0].Columns[i].Name == tt.column {
					col = &schema.Tables[0].Columns[i]
					break
				}
			}
			if col == nil {
				t.Fatalf("column %s not found", tt.column)
			}
			if col.Unique != tt.wantUnique {
				t.Errorf("col.Unique = %v, want %v (%s)", col.Unique, tt.wantUnique, tt.wantComment)
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
			_, err := mysql.ParseViewSQL([]byte(tt.sql))
			if err == nil {
				t.Fatal("ParseViewSQL() error = nil, want hard error (materialized views are PostgreSQL-only)")
			}
			if !strings.Contains(err.Error(), "not supported for dialect mysql") {
				t.Errorf("error = %q, want mention of 'not supported for dialect mysql'", err)
			}
		})
	}
}
