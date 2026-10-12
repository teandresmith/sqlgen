package mysql_test

import (
	"errors"
	"fmt"
	"slices"
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
		"t_bool":      "tinyint(1)",
		"t_enum":      "test_types__t_enum_enum",
		"t_set":       "test_types__t_set_set",
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
	if cols["status"].Type != "users__status_enum" {
		t.Errorf("status type = %q, want %q", cols["status"].Type, "users__status_enum")
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

func TestInlineEnumEdgeValues(t *testing.T) {
	sql := `CREATE TABLE users (
		status ENUM('active', 'in,active', 'it''s', '', 'b\\c', 't\%u'),
		perms SET('it''s', 'r w')
	);`

	p := newParser(t)
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	schema := p.Schema()
	if len(schema.Enums) != 1 || len(schema.Sets) != 1 {
		t.Fatalf("enums = %d, sets = %d, want 1 each", len(schema.Enums), len(schema.Sets))
	}
	// MySQL keeps the backslash in \% and \_, so 't\%u' stores t\%u.
	wantEnum := []string{"active", "in,active", "it's", "", `b\c`, `t\%u`}
	if diff := cmp.Diff(wantEnum, schema.Enums[0].Values); diff != "" {
		t.Errorf("enum values mismatch (-want +got):\n%s", diff)
	}
	wantSet := []string{"it's", "r w"}
	if diff := cmp.Diff(wantSet, schema.Sets[0].Values); diff != "" {
		t.Errorf("set values mismatch (-want +got):\n%s", diff)
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
	if cols["permissions"].Type != "users__permissions_set" {
		t.Errorf("permissions type = %q, want %q", cols["permissions"].Type, "users__permissions_set")
	}

	// A Set entry should be created for the inline SET.
	if len(schema.Sets) != 1 {
		t.Fatalf("sets = %d, want 1", len(schema.Sets))
	}
	if schema.Sets[0].Name != "users__permissions_set" {
		t.Errorf("set name = %q, want %q", schema.Sets[0].Name, "users__permissions_set")
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

// TestInlineTypeNamesSeparateTableFromColumn covers two columns that a single
// underscore would give one name: table a, column b_c and table a_b, column c.
func TestInlineTypeNamesSeparateTableFromColumn(t *testing.T) {
	sql := `CREATE TABLE a (id INT PRIMARY KEY, b_c ENUM('1','2') NOT NULL, y_z SET('r'));
		CREATE TABLE a_b (id INT PRIMARY KEY, c ENUM('3','4') NOT NULL);
		CREATE TABLE a_y (id INT PRIMARY KEY, z SET('w'));`

	p := newParser(t)
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	schema := p.Schema()

	wantEnums := []parser.Enum{
		{Name: "a__b_c_enum", Values: []string{"1", "2"}},
		{Name: "a_b__c_enum", Values: []string{"3", "4"}},
	}
	if diff := cmp.Diff(wantEnums, schema.Enums); diff != "" {
		t.Errorf("enums mismatch (-want +got):\n%s", diff)
	}
	wantSets := []parser.Set{
		{Name: "a__y_z_set", Values: []string{"r"}},
		{Name: "a_y__z_set", Values: []string{"w"}},
	}
	if diff := cmp.Diff(wantSets, schema.Sets); diff != "" {
		t.Errorf("sets mismatch (-want +got):\n%s", diff)
	}
	types := map[string]string{}
	for _, tbl := range schema.Tables {
		for _, c := range tbl.Columns {
			types[tbl.Name+"."+c.Name] = c.Type
		}
	}
	wantTypes := map[string]string{
		"a.id": "int", "a.b_c": "a__b_c_enum", "a.y_z": "a__y_z_set",
		"a_b.id": "int", "a_b.c": "a_b__c_enum",
		"a_y.id": "int", "a_y.z": "a_y__z_set",
	}
	if diff := cmp.Diff(wantTypes, types); diff != "" {
		t.Errorf("column types mismatch (-want +got):\n%s", diff)
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
			wantType:  "users__role_enum",
			wantEnums: []parser.Enum{{Name: "users__role_enum", Values: []string{"a", "b", "c"}}},
		},
		{
			name:      "CHANGE COLUMN widening an ENUM without a rename",
			sql:       "CREATE TABLE users (id INT PRIMARY KEY, role ENUM('a','b') NOT NULL); ALTER TABLE users CHANGE COLUMN role role ENUM('a','b','c') NOT NULL;",
			wantType:  "users__role_enum",
			wantEnums: []parser.Enum{{Name: "users__role_enum", Values: []string{"a", "b", "c"}}},
		},
		{
			name:      "CHANGE COLUMN renaming an ENUM column",
			sql:       "CREATE TABLE users (id INT PRIMARY KEY, role ENUM('a','b') NOT NULL); ALTER TABLE users CHANGE COLUMN role kind ENUM('a','b','c') NOT NULL;",
			wantType:  "users__kind_enum",
			wantEnums: []parser.Enum{{Name: "users__kind_enum", Values: []string{"a", "b", "c"}}},
		},
		{
			name:     "MODIFY from ENUM to VARCHAR",
			sql:      "CREATE TABLE users (id INT PRIMARY KEY, role ENUM('a','b') NOT NULL); ALTER TABLE users MODIFY role VARCHAR(10) NOT NULL;",
			wantType: "varchar(10)",
		},
		{
			name:      "MODIFY from VARCHAR to ENUM",
			sql:       "CREATE TABLE users (id INT PRIMARY KEY, role VARCHAR(10) NOT NULL); ALTER TABLE users MODIFY role ENUM('a','b') NOT NULL;",
			wantType:  "users__role_enum",
			wantEnums: []parser.Enum{{Name: "users__role_enum", Values: []string{"a", "b"}}},
		},
		{
			name:     "MODIFY widening a SET",
			sql:      "CREATE TABLE users (id INT PRIMARY KEY, role SET('a','b') NOT NULL); ALTER TABLE users MODIFY role SET('a','b','c') NOT NULL;",
			wantType: "users__role_set",
			wantSets: []parser.Set{{Name: "users__role_set", Values: []string{"a", "b", "c"}}},
		},
		{
			name:     "MODIFY from ENUM to SET",
			sql:      "CREATE TABLE users (id INT PRIMARY KEY, role ENUM('a','b') NOT NULL); ALTER TABLE users MODIFY role SET('a','b') NOT NULL;",
			wantType: "users__role_set",
			wantSets: []parser.Set{{Name: "users__role_set", Values: []string{"a", "b"}}},
		},
		{
			name:      "MODIFY on one ENUM column leaves another table's ENUM column alone",
			sql:       "CREATE TABLE users (id INT PRIMARY KEY, role ENUM('a','b') NOT NULL); CREATE TABLE admins (id INT PRIMARY KEY, role ENUM('x') NOT NULL); ALTER TABLE users MODIFY role ENUM('a','b','c') NOT NULL;",
			wantType:  "users__role_enum",
			wantEnums: []parser.Enum{{Name: "users__role_enum", Values: []string{"a", "b", "c"}}, {Name: "admins__role_enum", Values: []string{"x"}}},
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

// inlineColumnTypes returns the type of every column of schema that is not an
// int, keyed by table.column.
func inlineColumnTypes(schema *parser.Schema) map[string]string {
	types := make(map[string]string)
	for _, table := range schema.Tables {
		for _, c := range table.Columns {
			if c.Type != "int" {
				types[table.Name+"."+c.Name] = c.Type
			}
		}
	}
	return types
}

// sortEnumsByNameAndValues orders enums by name, then values, so a test can
// list two entries of one name in a fixed order.
var sortEnumsByNameAndValues = cmpopts.SortSlices(func(a, b parser.Enum) bool {
	if a.Name != b.Name {
		return a.Name < b.Name
	}
	return strings.Join(a.Values, ",") < strings.Join(b.Values, ",")
})

// TestInlineEnumOrSetFollowsRenameAndDrop covers RENAME and DROP on inline
// ENUM and SET columns. The entry takes the name built from the current table
// and column names, as introspection names it, and goes with its column or
// table, so a column re-added under the old name registers the only entry of
// that name.
func TestInlineEnumOrSetFollowsRenameAndDrop(t *testing.T) {
	tests := []struct {
		name      string
		sql       string
		wantTypes map[string]string
		wantEnums []parser.Enum
		wantSets  []parser.Set
	}{
		{
			name:      "RENAME COLUMN",
			sql:       "CREATE TABLE users (id INT PRIMARY KEY, role ENUM('a','b') NOT NULL, tags SET('p','q')); ALTER TABLE users RENAME COLUMN role TO kind, RENAME COLUMN tags TO labels;",
			wantTypes: map[string]string{"users.kind": "users__kind_enum", "users.labels": "users__labels_set"},
			wantEnums: []parser.Enum{{Name: "users__kind_enum", Values: []string{"a", "b"}}},
			wantSets:  []parser.Set{{Name: "users__labels_set", Values: []string{"p", "q"}}},
		},
		{
			name:      "RENAME COLUMN then ADD COLUMN under the old name, then MODIFY the renamed column",
			sql:       "CREATE TABLE users (id INT PRIMARY KEY, role ENUM('a','b') NOT NULL); ALTER TABLE users RENAME COLUMN role TO kind; ALTER TABLE users ADD COLUMN role ENUM('x') NOT NULL; ALTER TABLE users MODIFY kind ENUM('a','b','c') NOT NULL;",
			wantTypes: map[string]string{"users.kind": "users__kind_enum", "users.role": "users__role_enum"},
			wantEnums: []parser.Enum{{Name: "users__kind_enum", Values: []string{"a", "b", "c"}}, {Name: "users__role_enum", Values: []string{"x"}}},
		},
		{
			name:      "RENAME COLUMN swapping two ENUM columns",
			sql:       "CREATE TABLE users (id INT PRIMARY KEY, a ENUM('1') NOT NULL, b ENUM('2') NOT NULL); ALTER TABLE users RENAME COLUMN a TO tmp, RENAME COLUMN b TO a, RENAME COLUMN tmp TO b;",
			wantTypes: map[string]string{"users.a": "users__a_enum", "users.b": "users__b_enum"},
			wantEnums: []parser.Enum{{Name: "users__a_enum", Values: []string{"2"}}, {Name: "users__b_enum", Values: []string{"1"}}},
		},
		{
			name:      "CHANGE COLUMN renaming to VARCHAR, then ADD COLUMN under the old name",
			sql:       "CREATE TABLE users (id INT PRIMARY KEY, role ENUM('a','b') NOT NULL); ALTER TABLE users CHANGE COLUMN role kind VARCHAR(10) NOT NULL; ALTER TABLE users ADD COLUMN role ENUM('x') NOT NULL;",
			wantTypes: map[string]string{"users.kind": "varchar(10)", "users.role": "users__role_enum"},
			wantEnums: []parser.Enum{{Name: "users__role_enum", Values: []string{"x"}}},
		},
		{
			name:      "ALTER TABLE RENAME TO",
			sql:       "CREATE TABLE users (id INT PRIMARY KEY, role ENUM('a','b') NOT NULL, tags SET('p','q')); ALTER TABLE users RENAME TO people;",
			wantTypes: map[string]string{"people.role": "people__role_enum", "people.tags": "people__tags_set"},
			wantEnums: []parser.Enum{{Name: "people__role_enum", Values: []string{"a", "b"}}},
			wantSets:  []parser.Set{{Name: "people__tags_set", Values: []string{"p", "q"}}},
		},
		{
			name:      "ALTER TABLE RENAME TO its own name",
			sql:       "CREATE TABLE users (id INT PRIMARY KEY, role ENUM('a','b') NOT NULL); ALTER TABLE users RENAME TO users;",
			wantTypes: map[string]string{"users.role": "users__role_enum"},
			wantEnums: []parser.Enum{{Name: "users__role_enum", Values: []string{"a", "b"}}},
		},
		{
			name:      "RENAME TABLE then CREATE TABLE under the old name, then MODIFY",
			sql:       "CREATE TABLE users (id INT PRIMARY KEY, role ENUM('a','b') NOT NULL); RENAME TABLE users TO people; CREATE TABLE users (id INT PRIMARY KEY, role ENUM('x') NOT NULL); ALTER TABLE users MODIFY role ENUM('x','y') NOT NULL;",
			wantTypes: map[string]string{"people.role": "people__role_enum", "users.role": "users__role_enum"},
			wantEnums: []parser.Enum{{Name: "people__role_enum", Values: []string{"a", "b"}}, {Name: "users__role_enum", Values: []string{"x", "y"}}},
		},
		{
			name: "DROP COLUMN",
			sql:  "CREATE TABLE users (id INT PRIMARY KEY, role ENUM('a','b') NOT NULL, tags SET('p','q')); ALTER TABLE users DROP COLUMN role, DROP COLUMN tags;",
		},
		{
			name:      "DROP COLUMN then ADD COLUMN under the same name",
			sql:       "CREATE TABLE users (id INT PRIMARY KEY, role ENUM('a','b') NOT NULL); ALTER TABLE users DROP COLUMN role; ALTER TABLE users ADD COLUMN role ENUM('x','y') NOT NULL;",
			wantTypes: map[string]string{"users.role": "users__role_enum"},
			wantEnums: []parser.Enum{{Name: "users__role_enum", Values: []string{"x", "y"}}},
		},
		{
			name:      "DROP COLUMN then RENAME COLUMN a VARCHAR column to its name",
			sql:       "CREATE TABLE users (id INT PRIMARY KEY, role ENUM('a','b') NOT NULL, label VARCHAR(10)); ALTER TABLE users DROP COLUMN role; ALTER TABLE users RENAME COLUMN label TO role;",
			wantTypes: map[string]string{"users.role": "varchar(10)"},
		},
		{
			name: "DROP TABLE",
			sql:  "CREATE TABLE users (id INT PRIMARY KEY, role ENUM('a','b') NOT NULL, tags SET('p','q')); DROP TABLE users;",
		},
		{
			name:      "DROP TABLE then CREATE TABLE under the same name",
			sql:       "CREATE TABLE users (id INT PRIMARY KEY, role ENUM('a','b') NOT NULL, tags SET('p','q')); DROP TABLE users; CREATE TABLE users (id INT PRIMARY KEY, role ENUM('x') NOT NULL);",
			wantTypes: map[string]string{"users.role": "users__role_enum"},
			wantEnums: []parser.Enum{{Name: "users__role_enum", Values: []string{"x"}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newParser(t)
			if err := p.Parse("test.sql", []byte(tt.sql)); err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			schema := p.Schema()
			if diff := cmp.Diff(tt.wantTypes, inlineColumnTypes(schema), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Parse(%q) column types mismatch (-want +got):\n%s", tt.sql, diff)
			}
			if diff := cmp.Diff(tt.wantEnums, schema.Enums, sortEnumsByNameAndValues, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Parse(%q) enums mismatch (-want +got):\n%s", tt.sql, diff)
			}
			if diff := cmp.Diff(tt.wantSets, schema.Sets, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Parse(%q) sets mismatch (-want +got):\n%s", tt.sql, diff)
			}
		})
	}
}

// TestInlineEnumNameCollision covers two columns whose inline ENUM names
// collide: a.b_c and a_b.c share the Go type name ABCEnum, and a.b__c and
// a__b.c share the name a__b__c_enum. Each column keeps its own values through
// MODIFY, and renaming or dropping one of them resolves the collision.
func TestInlineEnumNameCollision(t *testing.T) {
	const (
		goName  = "CREATE TABLE a (id INT PRIMARY KEY, b_c ENUM('1','2') NOT NULL); CREATE TABLE a_b (id INT PRIMARY KEY, c ENUM('3','4') NOT NULL); "
		sqlName = "CREATE TABLE a (id INT PRIMARY KEY, b__c ENUM('1') NOT NULL); CREATE TABLE a__b (id INT PRIMARY KEY, c ENUM('2') NOT NULL); "
	)
	tests := []struct {
		name      string
		sql       string
		wantTypes map[string]string
		wantEnums []parser.Enum
	}{
		{
			name:      "Go name: RENAME COLUMN",
			sql:       goName + "ALTER TABLE a_b RENAME COLUMN c TO d;",
			wantTypes: map[string]string{"a.b_c": "a__b_c_enum", "a_b.d": "a_b__d_enum"},
			wantEnums: []parser.Enum{{Name: "a__b_c_enum", Values: []string{"1", "2"}}, {Name: "a_b__d_enum", Values: []string{"3", "4"}}},
		},
		{
			name:      "Go name: DROP COLUMN",
			sql:       goName + "ALTER TABLE a_b DROP COLUMN c;",
			wantTypes: map[string]string{"a.b_c": "a__b_c_enum"},
			wantEnums: []parser.Enum{{Name: "a__b_c_enum", Values: []string{"1", "2"}}},
		},
		{
			name:      "SQL name: MODIFY",
			sql:       sqlName + "ALTER TABLE a MODIFY b__c ENUM('1','3') NOT NULL;",
			wantTypes: map[string]string{"a.b__c": "a__b__c_enum", "a__b.c": "a__b__c_enum"},
			wantEnums: []parser.Enum{{Name: "a__b__c_enum", Values: []string{"1", "3"}}, {Name: "a__b__c_enum", Values: []string{"2"}}},
		},
		{
			name:      "SQL name: RENAME COLUMN",
			sql:       sqlName + "ALTER TABLE a__b RENAME COLUMN c TO d;",
			wantTypes: map[string]string{"a.b__c": "a__b__c_enum", "a__b.d": "a__b__d_enum"},
			wantEnums: []parser.Enum{{Name: "a__b__c_enum", Values: []string{"1"}}, {Name: "a__b__d_enum", Values: []string{"2"}}},
		},
		{
			name:      "SQL name: DROP COLUMN",
			sql:       sqlName + "ALTER TABLE a DROP COLUMN b__c;",
			wantTypes: map[string]string{"a__b.c": "a__b__c_enum"},
			wantEnums: []parser.Enum{{Name: "a__b__c_enum", Values: []string{"2"}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newParser(t)
			if err := p.Parse("test.sql", []byte(tt.sql)); err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			schema := p.Schema()
			if diff := cmp.Diff(tt.wantTypes, inlineColumnTypes(schema)); diff != "" {
				t.Errorf("Parse(%q) column types mismatch (-want +got):\n%s", tt.sql, diff)
			}
			if diff := cmp.Diff(tt.wantEnums, schema.Enums, sortEnumsByNameAndValues); diff != "" {
				t.Errorf("Parse(%q) enums mismatch (-want +got):\n%s", tt.sql, diff)
			}
		})
	}
}

// TestSeededInlineEnumOrSetFollowsRenameAndDrop covers `input.source: both`:
// DDL that renames or drops an introspected inline ENUM or SET column updates
// its entry as it does a parsed column's, and an entry no table column
// declares, such as a view's own, stays.
func TestSeededInlineEnumOrSetFollowsRenameAndDrop(t *testing.T) {
	seed := func() *parser.Schema {
		return &parser.Schema{
			Tables: []parser.Table{
				{Name: "a", Columns: []parser.Column{{Name: "id", Type: "int"}, {Name: "b__c", Type: "a__b__c_enum"}}},
				{Name: "a__b", Columns: []parser.Column{{Name: "id", Type: "int"}, {Name: "c", Type: "a__b__c_enum"}}},
				{Name: "users", Columns: []parser.Column{{Name: "id", Type: "int"}, {Name: "role", Type: "users__role_enum"}, {Name: "tags", Type: "users__tags_set"}}},
			},
			Enums: []parser.Enum{
				{Name: "a__b__c_enum", Values: []string{"1"}},
				{Name: "a__b__c_enum", Values: []string{"2"}},
				{Name: "users__role_enum", Values: []string{"a", "b"}},
				{Name: "v__role_enum", Values: []string{"a"}},
			},
			Sets: []parser.Set{{Name: "users__tags_set", Values: []string{"p", "q"}}},
		}
	}
	tests := []struct {
		name      string
		sql       string
		wantTypes map[string]string
		wantEnums []parser.Enum
		wantSets  []parser.Set
	}{
		{
			name:      "RENAME COLUMN",
			sql:       "ALTER TABLE users RENAME COLUMN role TO kind, RENAME COLUMN tags TO labels;",
			wantTypes: map[string]string{"a.b__c": "a__b__c_enum", "a__b.c": "a__b__c_enum", "users.kind": "users__kind_enum", "users.labels": "users__labels_set"},
			wantEnums: []parser.Enum{
				{Name: "a__b__c_enum", Values: []string{"1"}},
				{Name: "a__b__c_enum", Values: []string{"2"}},
				{Name: "users__kind_enum", Values: []string{"a", "b"}},
				{Name: "v__role_enum", Values: []string{"a"}},
			},
			wantSets: []parser.Set{{Name: "users__labels_set", Values: []string{"p", "q"}}},
		},
		{
			name:      "RENAME TABLE and DROP COLUMN on a shared name",
			sql:       "RENAME TABLE users TO people; ALTER TABLE a DROP COLUMN b__c;",
			wantTypes: map[string]string{"a__b.c": "a__b__c_enum", "people.role": "people__role_enum", "people.tags": "people__tags_set"},
			wantEnums: []parser.Enum{
				{Name: "a__b__c_enum", Values: []string{"2"}},
				{Name: "people__role_enum", Values: []string{"a", "b"}},
				{Name: "v__role_enum", Values: []string{"a"}},
			},
			wantSets: []parser.Set{{Name: "people__tags_set", Values: []string{"p", "q"}}},
		},
		{
			name:      "DROP TABLE",
			sql:       "DROP TABLE users; ALTER TABLE a__b RENAME COLUMN c TO d;",
			wantTypes: map[string]string{"a.b__c": "a__b__c_enum", "a__b.d": "a__b__d_enum"},
			wantEnums: []parser.Enum{
				{Name: "a__b__c_enum", Values: []string{"1"}},
				{Name: "a__b__d_enum", Values: []string{"2"}},
				{Name: "v__role_enum", Values: []string{"a"}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newParser(t)
			p.Seed(seed())
			if err := p.Parse("test.sql", []byte(tt.sql)); err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			schema := p.Schema()
			if diff := cmp.Diff(tt.wantTypes, inlineColumnTypes(schema)); diff != "" {
				t.Errorf("Parse(%q) after Seed column types mismatch (-want +got):\n%s", tt.sql, diff)
			}
			if diff := cmp.Diff(tt.wantEnums, schema.Enums, sortEnumsByNameAndValues); diff != "" {
				t.Errorf("Parse(%q) after Seed enums mismatch (-want +got):\n%s", tt.sql, diff)
			}
			if diff := cmp.Diff(tt.wantSets, schema.Sets, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Parse(%q) after Seed sets mismatch (-want +got):\n%s", tt.sql, diff)
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
				{Name: "role", Type: "users__role_enum"},
			},
		}},
		Enums: []parser.Enum{{Name: "users__role_enum", Values: []string{"a", "b"}}},
	})
	sql := "ALTER TABLE users MODIFY role ENUM('a','b','c') NOT NULL;"
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	want := []parser.Enum{{Name: "users__role_enum", Values: []string{"a", "b", "c"}}}
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

func TestGeneratedColumns(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want parser.Column
	}{
		{
			name: "stored generated column",
			sql:  "CREATE TABLE t (qty INT, total INT GENERATED ALWAYS AS (qty * 2) STORED);",
			want: parser.Column{Name: "total", Type: "int", Nullable: true, GeneratedExpr: "qty * 2", GeneratedStorage: "STORED"},
		},
		{
			name: "virtual generated column",
			sql:  "CREATE TABLE t (qty INT, total INT GENERATED ALWAYS AS (qty * 2) VIRTUAL);",
			want: parser.Column{Name: "total", Type: "int", Nullable: true, GeneratedExpr: "qty * 2", GeneratedStorage: "VIRTUAL"},
		},
		{
			name: "storage defaults to virtual",
			sql:  "CREATE TABLE t (qty INT, total INT GENERATED ALWAYS AS (qty + 1));",
			want: parser.Column{Name: "total", Type: "int", Nullable: true, GeneratedExpr: "qty + 1", GeneratedStorage: "VIRTUAL"},
		},
		{
			name: "shorthand AS",
			sql:  "CREATE TABLE t (qty INT, total INT AS (qty + 1));",
			want: parser.Column{Name: "total", Type: "int", Nullable: true, GeneratedExpr: "qty + 1", GeneratedStorage: "VIRTUAL"},
		},
		{
			name: "shorthand AS stored not null",
			sql:  "CREATE TABLE t (qty INT, total INT AS (abs(qty)) STORED NOT NULL);",
			want: parser.Column{Name: "total", Type: "int", GeneratedExpr: "abs(qty)", GeneratedStorage: "STORED"},
		},
		{
			name: "generated column added by alter table",
			sql: `CREATE TABLE t (qty INT);
				ALTER TABLE t ADD COLUMN total INT AS (qty * 2) STORED;`,
			want: parser.Column{Name: "total", Type: "int", Nullable: true, GeneratedExpr: "qty * 2", GeneratedStorage: "STORED"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newParser(t)
			if err := p.Parse("test.sql", []byte(tt.sql)); err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			got := parser.ColumnByName(&p.Schema().Tables[0], "total")
			if got == nil {
				t.Fatal("column total not found")
			}
			if diff := cmp.Diff(tt.want, *got); diff != "" {
				t.Errorf("column total mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestNormalizeExpr(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		want    string
		wantErr bool
	}{
		{name: "parentheses and backticks dropped", expr: "(`qty` * 2)", want: "qty * 2"},
		{name: "function call", expr: "abs(`qty`)", want: "abs(qty)"},
		{name: "reserved word keeps its backticks", expr: "(`order` + 1)", want: "`order` + 1"},
		{name: "already normalized", expr: "qty + 1", want: "qty + 1"},
		{name: "charset introducer dropped", expr: "concat(`s`,_latin1'it\\'s')", want: "concat(s, 'it\\'s')"},
		{name: "introducer on a lone literal", expr: "_utf8mb4'{}'", want: "'{}'"},
		{name: "unary minus in parentheses", expr: "((`price` > 1) or (`price` < -(1)))", want: "price > 1 or price < -1"},
		{name: "keyword function", expr: "CURRENT_TIMESTAMP", want: "current_timestamp()"},
		{name: "keyword function with precision", expr: "CURRENT_TIMESTAMP(3)", want: "current_timestamp(3)"},
		{name: "now", expr: "NOW()", want: "current_timestamp()"},
		{name: "now with precision", expr: "now(3)", want: "current_timestamp(3)"},
		{name: "localtime", expr: "LOCALTIME", want: "current_timestamp()"},
		{name: "localtimestamp with precision", expr: "LOCALTIMESTAMP(6)", want: "current_timestamp(6)"},
		{name: "now inside an expression", expr: "(now() + interval 1 day)", want: "current_timestamp() + interval 1 day"},
		{name: "curdate", expr: "curdate()", want: "current_date()"},
		{name: "current_date", expr: "CURRENT_DATE", want: "current_date()"},
		{name: "curtime with precision", expr: "CURTIME(2)", want: "current_time(2)"},
		{name: "utc_timestamp is not a synonym", expr: "UTC_TIMESTAMP", want: "utc_timestamp()"},
		{name: "bit literal", expr: "b'101'", want: "0b101"},
		{name: "json operators", expr: "json_unquote(json_extract(`doc`,_utf8mb4'$.a'))", want: "json_unquote(json_extract(doc, '$.a'))"},
		{name: "incomplete expression", expr: "qty *", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mysql.NormalizeExpr(tt.expr)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NormalizeExpr(%q) error = %v, wantErr %t", tt.expr, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("NormalizeExpr(%q) = %q, want %q", tt.expr, got, tt.want)
			}
		})
	}
}

// MySQL 8.0.19+ stores an integer type without its display width, except
// tinyint(1) and a ZEROFILL type, which is also UNSIGNED.
func TestIntegerDisplayWidth(t *testing.T) {
	tests := []struct {
		declared string
		want     string
	}{
		{declared: "INT(11)", want: "int"},
		{declared: "BIGINT(20) UNSIGNED", want: "bigint unsigned"},
		{declared: "SMALLINT(6)", want: "smallint"},
		{declared: "MEDIUMINT(9)", want: "mediumint"},
		{declared: "TINYINT(4)", want: "tinyint"},
		{declared: "TINYINT(1)", want: "tinyint(1)"},
		{declared: "TINYINT(1) UNSIGNED", want: "tinyint unsigned"},
		{declared: "INT UNSIGNED", want: "int unsigned"},
		{declared: "INT(5) ZEROFILL", want: "int(5) unsigned zerofill"},
		{declared: "INT(5) UNSIGNED ZEROFILL", want: "int(5) unsigned zerofill"},
		{declared: "INT ZEROFILL", want: "int(10) unsigned zerofill"},
		{declared: "TINYINT ZEROFILL", want: "tinyint(3) unsigned zerofill"},
		{declared: "BIGINT ZEROFILL", want: "bigint(20) unsigned zerofill"},
		{declared: "DECIMAL(10,2)", want: "decimal(10,2)"},
		{declared: "VARCHAR(20)", want: "varchar(20)"},
		{declared: "BIT(8)", want: "bit(8)"},
		{declared: "DOUBLE ZEROFILL", want: "double unsigned zerofill"},
		// A synonym takes the type the catalog stores it as.
		{declared: "BOOL", want: "tinyint(1)"},
		{declared: "BOOLEAN", want: "tinyint(1)"},
		{declared: "INTEGER", want: "int"},
		{declared: "INTEGER(11) UNSIGNED", want: "int unsigned"},
		{declared: "INTEGER ZEROFILL", want: "int(10) unsigned zerofill"},
		{declared: "NUMERIC(8,3)", want: "decimal(8,3)"},
		{declared: "NUMERIC", want: "decimal(10,0)"},
		{declared: "DECIMAL", want: "decimal(10,0)"},
		{declared: "DECIMAL(5)", want: "decimal(5,0)"},
		{declared: "REAL", want: "double"},
		{declared: "REAL(8,2)", want: "double(8,2)"},
		{declared: "FLOAT(24)", want: "float"},
		{declared: "FLOAT(25)", want: "double"},
		{declared: "FLOAT(7,3)", want: "float(7,3)"},
		{declared: "FLOAT4", want: "float"},
		{declared: "FLOAT8", want: "double"},
		{declared: "CHAR", want: "char(1)"},
		{declared: "BINARY", want: "binary(1)"},
		{declared: "BIT", want: "bit(1)"},
		{declared: "YEAR(4)", want: "year"},
		// ZEROFILL implies UNSIGNED on every numeric type.
		{declared: "DOUBLE UNSIGNED ZEROFILL", want: "double unsigned zerofill"},
		{declared: "DECIMAL(5,2) ZEROFILL", want: "decimal(5,2) unsigned zerofill"},
		{declared: "FLOAT(7,3) UNSIGNED ZEROFILL", want: "float(7,3) unsigned zerofill"},
		{declared: "REAL UNSIGNED ZEROFILL", want: "double unsigned zerofill"},
	}
	for _, tt := range tests {
		t.Run(tt.declared, func(t *testing.T) {
			p := newParser(t)
			if err := p.Parse("test.sql", []byte("CREATE TABLE t (c "+tt.declared+");")); err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			if got := p.Schema().Tables[0].Columns[0].Type; got != tt.want {
				t.Errorf("type of %s = %q, want %q", tt.declared, got, tt.want)
			}
		})
	}
}

// NormalizeType reads INFORMATION_SCHEMA.COLUMNS.COLUMN_TYPE, which a server
// older than MySQL 8.0.19 writes with display widths.
func TestNormalizeType(t *testing.T) {
	tests := []struct {
		columnType string
		want       string
	}{
		{columnType: "int", want: "int"},
		{columnType: "int(11)", want: "int"},
		{columnType: "bigint(20) unsigned", want: "bigint unsigned"},
		{columnType: "tinyint(1)", want: "tinyint(1)"},
		{columnType: "tinyint(1) unsigned", want: "tinyint unsigned"},
		{columnType: "tinyint(4)", want: "tinyint"},
		{columnType: "int(5) unsigned zerofill", want: "int(5) unsigned zerofill"},
		{columnType: "INT(11) UNSIGNED", want: "int unsigned"},
		{columnType: "decimal(10,2)", want: "decimal(10,2)"},
		{columnType: "VarChar(255)", want: "varchar(255)"},
		{columnType: "bit(1)", want: "bit(1)"},
		// The catalog's own forms are kept.
		{columnType: "decimal(10,0)", want: "decimal(10,0)"},
		{columnType: "double unsigned zerofill", want: "double unsigned zerofill"},
		{columnType: "float(7,3) unsigned zerofill", want: "float(7,3) unsigned zerofill"},
		{columnType: "char(1)", want: "char(1)"},
		{columnType: "year", want: "year"},
		{columnType: "double", want: "double"},
	}
	for _, tt := range tests {
		t.Run(tt.columnType, func(t *testing.T) {
			if got := mysql.NormalizeType(tt.columnType); got != tt.want {
				t.Errorf("NormalizeType(%q) = %q, want %q", tt.columnType, got, tt.want)
			}
		})
	}
}

// The parser writes a default, CHECK or generated expression as the
// normalized catalog text reads, so both sources agree.
func TestExpressionText(t *testing.T) {
	const ddl = `CREATE TABLE t (
		a int DEFAULT NULL,
		b varchar(10) DEFAULT _utf8mb4'x',
		c varchar(20) AS (concat(_latin1'n-', b)),
		d varchar(10) NOT NULL DEFAULT '',
		CONSTRAINT t_b CHECK (b <> _latin1'q')
	);`
	p := newParser(t)
	if err := p.Parse("test.sql", []byte(ddl)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	table := &p.Schema().Tables[0]

	defaults := make(map[string]string)
	for _, c := range table.Columns {
		defaults[c.Name] = c.Default
	}
	wantDefaults := map[string]string{"a": "", "b": "'x'", "c": "", "d": "''"}
	if diff := cmp.Diff(wantDefaults, defaults); diff != "" {
		t.Errorf("defaults mismatch (-want +got):\n%s", diff)
	}
	if got := parser.ColumnByName(table, "c").GeneratedExpr; got != "concat('n-', b)" {
		t.Errorf("c GeneratedExpr = %q, want %q", got, "concat('n-', b)")
	}
	if c := findConstraint(table.Constraints, "t_b", parser.Check); c == nil || c.CheckExpression != "b != 'q'" {
		t.Errorf("CHECK t_b = %+v, want CheckExpression %q", c, "b != 'q'")
	}
}

func TestQuoteLiteral(t *testing.T) {
	tests := []struct {
		value string
		want  string
	}{
		{value: "", want: "''"},
		{value: "active", want: "'active'"},
		{value: "it's", want: `'it\'s'`},
		{value: `a\b`, want: `'a\\b'`},
		{value: "r,w", want: "'r,w'"},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			if got := mysql.QuoteLiteral(tt.value); got != tt.want {
				t.Errorf("QuoteLiteral(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
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
		t.Error("user_id FKReference should be nil after DROP FOREIGN KEY")
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
// key columns stay NOT NULL, as MySQL leaves them. DROP INDEX or DROP KEY
// `PRIMARY`, in any letter case, drops the key the same way.
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
		{
			name: "table-level primary key dropped by DROP INDEX PRIMARY",
			sql:  "CREATE TABLE users (id INT, name TEXT, PRIMARY KEY (id)); ALTER TABLE users DROP INDEX `PRIMARY`;",
			want: []string{"users.id int", "users.name text null"},
		},
		{
			name: "inline primary key dropped by DROP KEY in lower case",
			sql:  "CREATE TABLE users (id INT PRIMARY KEY, name TEXT); ALTER TABLE users DROP KEY `primary`;",
			want: []string{"users.id int", "users.name text null"},
		},
		{
			name: "composite primary key dropped by DROP INDEX PRIMARY",
			sql:  "CREATE TABLE post_tags (post_id INT, tag_id INT, PRIMARY KEY (post_id, tag_id)); ALTER TABLE post_tags DROP INDEX `PRIMARY`;",
			want: []string{"post_tags.post_id int", "post_tags.tag_id int"},
		},
		{
			name: "primary key dropped by standalone DROP INDEX PRIMARY ON",
			sql:  "CREATE TABLE users (id INT, name TEXT, PRIMARY KEY (id)); DROP INDEX `PRIMARY` ON users;",
			want: []string{"users.id int", "users.name text null"},
		},
		{
			name: "primary key replaced with ADD before DROP INDEX PRIMARY",
			sql:  "CREATE TABLE users (id INT PRIMARY KEY, code INT NOT NULL); ALTER TABLE users ADD PRIMARY KEY (code), DROP INDEX `PRIMARY`;",
			want: []string{"users.id int", "users.code int pk"},
		},
		{
			name: "former key column redefined nullable before DROP INDEX PRIMARY",
			sql:  "CREATE TABLE users (id INT PRIMARY KEY, name TEXT); ALTER TABLE users MODIFY id BIGINT, DROP INDEX `PRIMARY`;",
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
				var flagged, keyed []string
				for _, col := range tbl.Columns {
					s := tbl.Name + "." + col.Name + " " + col.Type
					if col.PrimaryKey {
						s += " pk"
						flagged = append(flagged, col.Name)
					}
					if col.Nullable {
						s += " null"
					}
					got = append(got, s)
				}
				for _, c := range tbl.Constraints {
					if c.Type == parser.PrimaryKey {
						keyed = append(keyed, c.Columns...)
					}
				}
				// The PRIMARY KEY constraint goes with the column flags.
				slices.Sort(flagged)
				slices.Sort(keyed)
				if diff := cmp.Diff(flagged, keyed); diff != "" {
					t.Errorf("Parse(%q) table %s primary-key constraint columns differ from pk columns (-flagged +constraint):\n%s", tt.sql, tbl.Name, diff)
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
			// Measured on MySQL 8.0: of two identical foreign key indexes the
			// later one stays.
			name: "two FKs on same column share one backing index",
			ddl: `CREATE TABLE audit (
				id INT PRIMARY KEY,
				user_id INT,
				CONSTRAINT fk_a FOREIGN KEY (user_id) REFERENCES users(id),
				CONSTRAINT fk_b FOREIGN KEY (user_id) REFERENCES admins(id)
			);`,
			want: []wantIdx{{Name: "fk_b", Columns: []string{"user_id"}}},
		},
		{
			// Measured on MySQL 8.0.
			name: "renaming the table to its own name keeps the FK index replaceable",
			ddl: `CREATE TABLE orders (user_id INT, FOREIGN KEY (user_id) REFERENCES users(id));
				ALTER TABLE orders RENAME TO orders;
				ALTER TABLE orders ADD KEY ix (user_id);`,
			want: []wantIdx{{Name: "ix", Columns: []string{"user_id"}}},
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

// TestRenameOntoExistingName pins that ALTER TABLE refuses a table or column
// name already taken, as MySQL 8.0 does (ERROR 1050, ERROR 1060), while a
// table renamed to its own name and a column renamed to its own name in
// another case are accepted.
func TestRenameOntoExistingName(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		want    []string
		wantErr string
	}{
		{
			name:    "a table name another table has",
			sql:     `CREATE TABLE a (id INT); CREATE TABLE b (id INT); ALTER TABLE a RENAME TO b;`,
			wantErr: "table b already exists",
		},
		{
			name: "the table's own name",
			sql:  `CREATE TABLE a (id INT); ALTER TABLE a RENAME TO a;`,
			want: []string{"a(id)"},
		},
		{
			name:    "a column name another column has in another case",
			sql:     `CREATE TABLE t (a INT, b INT); ALTER TABLE t RENAME COLUMN a TO B;`,
			wantErr: "column B already exists in table t",
		},
		{
			name: "a column's own name in another case",
			sql:  `CREATE TABLE t (a INT, b INT); ALTER TABLE t RENAME COLUMN a TO A;`,
			want: []string{"t(A, b)"},
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
				var cols []string
				for _, c := range tbl.Columns {
					cols = append(cols, c.Name)
				}
				got = append(got, tbl.Name+"("+strings.Join(cols, ", ")+")")
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("tables after the rename mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestReferenceColumnInAnotherCase pins that a foreign key naming its
// referenced column in another case follows a rename of that column, as MySQL
// does (measured on 8.0: REFERENCES rp (B) is stored as `rp` (`b`), and
// becomes `rp` (`z`) after RENAME COLUMN b TO z).
func TestReferenceColumnInAnotherCase(t *testing.T) {
	p := newParser(t)
	sql := `CREATE TABLE rp (b INT PRIMARY KEY);
		CREATE TABLE rc (x INT, FOREIGN KEY (x) REFERENCES rp (B));
		ALTER TABLE rp RENAME COLUMN b TO z;`
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	rc := p.Schema().Tables[1]
	if ref := rc.Columns[0].FKReference; ref == nil || ref.Column != "z" {
		t.Errorf("rc.x FKReference = %+v, want column z", ref)
	}
	for _, c := range rc.Constraints {
		if c.Type == parser.ForeignKey {
			if diff := cmp.Diff([]string{"z"}, c.ReferenceColumns); diff != "" {
				t.Errorf("rc FOREIGN KEY reference columns mismatch (-want +got):\n%s", diff)
			}
		}
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
		"posts constraint -> accounts",
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
		{
			name:    "a name another table has is an error",
			sql:     `CREATE TABLE a (id INT); CREATE TABLE b (id INT); RENAME TABLE a TO b;`,
			wantErr: "table b already exists",
		},
		{
			name:    "the table's own name is an error",
			sql:     `CREATE TABLE a (id INT); RENAME TABLE a TO a;`,
			wantErr: "table a already exists",
		},
		{
			name: "a name in another case is another table",
			sql:  `CREATE TABLE a (id INT); RENAME TABLE a TO A;`,
			want: []string{"A"},
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
				"posts PRIMARY KEY [id]",
				"posts INDEX [user_id]",
				"posts FOREIGN KEY [user_id] -> users[user_id]",
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
				"posts PRIMARY KEY [id]",
				"posts INDEX [user_id]",
				"posts FOREIGN KEY [user_id] -> users[user_id]",
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
				"posts PRIMARY KEY [id]",
				"posts INDEX [user_id]",
				"posts FOREIGN KEY [user_id] -> users[id]",
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

// MySQL column names are case-insensitive, and MODIFY COLUMN, like CHANGE
// COLUMN, gives the column the spelling it names, carried into the keys and
// foreign keys on it.
func TestAlterColumnNamedInAnotherCase(t *testing.T) {
	type column struct{ Name, Type, Ref string }
	type key struct {
		Name    string
		Columns []string
		Ref     []string
	}
	tests := []struct {
		name     string
		sql      string
		table    string
		wantCols []column
		wantKeys []key
	}{
		{
			name:     "DROP COLUMN",
			sql:      "CREATE TABLE t (id int PRIMARY KEY, b int, UNIQUE KEY uk (id, b)); ALTER TABLE t DROP COLUMN B",
			table:    "t",
			wantCols: []column{{"id", "int", ""}},
			wantKeys: []key{{Name: "PRIMARY", Columns: []string{"id"}}, {Name: "uk", Columns: []string{"id"}}},
		},
		{
			name:     "MODIFY COLUMN",
			sql:      "CREATE TABLE t (id int PRIMARY KEY, b int, UNIQUE KEY uk (b)); ALTER TABLE t MODIFY COLUMN B bigint NOT NULL",
			table:    "t",
			wantCols: []column{{"id", "int", ""}, {"B", "bigint", ""}},
			wantKeys: []key{{Name: "PRIMARY", Columns: []string{"id"}}, {Name: "uk", Columns: []string{"B"}}},
		},
		{
			name:     "MODIFY COLUMN of an inline ENUM",
			sql:      "CREATE TABLE t (id int PRIMARY KEY, b enum('x','y')); ALTER TABLE t MODIFY COLUMN B enum('x','y','z')",
			table:    "t",
			wantCols: []column{{"id", "int", ""}, {"B", "t__B_enum", ""}},
			wantKeys: []key{{Name: "PRIMARY", Columns: []string{"id"}}},
		},
		{
			name: "MODIFY COLUMN of a referenced column",
			sql: `CREATE TABLE p (id int PRIMARY KEY, b int, UNIQUE KEY uk (b));
				CREATE TABLE c (id int PRIMARY KEY, p_b int, CONSTRAINT fk_c FOREIGN KEY (p_b) REFERENCES p (b));
				ALTER TABLE p MODIFY COLUMN B int`,
			table:    "c",
			wantCols: []column{{"id", "int", ""}, {"p_b", "int", "B"}},
			wantKeys: []key{
				{Name: "PRIMARY", Columns: []string{"id"}},
				{Name: "fk_c", Columns: []string{"p_b"}},
				{Name: "fk_c", Columns: []string{"p_b"}, Ref: []string{"B"}},
			},
		},
		{
			name:     "CHANGE COLUMN",
			sql:      "CREATE TABLE t (id int PRIMARY KEY, b int, UNIQUE KEY uk (b)); ALTER TABLE t CHANGE COLUMN B c bigint",
			table:    "t",
			wantCols: []column{{"id", "int", ""}, {"c", "bigint", ""}},
			wantKeys: []key{{Name: "PRIMARY", Columns: []string{"id"}}, {Name: "uk", Columns: []string{"c"}}},
		},
		{
			name:     "RENAME COLUMN",
			sql:      "CREATE TABLE t (id int PRIMARY KEY, b enum('x','y'), UNIQUE KEY uk (b)); ALTER TABLE t RENAME COLUMN B TO c",
			table:    "t",
			wantCols: []column{{"id", "int", ""}, {"c", "t__c_enum", ""}},
			wantKeys: []key{{Name: "PRIMARY", Columns: []string{"id"}}, {Name: "uk", Columns: []string{"c"}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newParser(t)
			if err := p.Parse("test.sql", []byte(tt.sql)); err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			var table *parser.Table
			for i, tb := range p.Schema().Tables {
				if tb.Name == tt.table {
					table = &p.Schema().Tables[i]
				}
			}
			if table == nil {
				t.Fatalf("table %s not found", tt.table)
			}
			gotCols := make([]column, 0, len(table.Columns))
			for _, c := range table.Columns {
				col := column{Name: c.Name, Type: c.Type}
				if c.FKReference != nil {
					col.Ref = c.FKReference.Column
				}
				gotCols = append(gotCols, col)
			}
			if diff := cmp.Diff(tt.wantCols, gotCols); diff != "" {
				t.Errorf("columns mismatch (-want +got):\n%s", diff)
			}
			gotKeys := make([]key, 0, len(table.Constraints))
			for _, c := range table.Constraints {
				gotKeys = append(gotKeys, key{Name: c.Name, Columns: c.Columns, Ref: c.ReferenceColumns})
			}
			if diff := cmp.Diff(tt.wantKeys, gotKeys, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("constraints mismatch (-want +got):\n%s", diff)
			}
		})
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

	// Without USING, the method is btree, as information_schema reports it.
	defaultIdx, ok := byName["idx_email_default"]
	if !ok {
		t.Fatalf("missing idx_email_default; constraints=%+v", table.Constraints)
	}
	if defaultIdx.Method != "btree" {
		t.Errorf("default: Method=%q, want %q", defaultIdx.Method, "btree")
	}
}

// TestDropIndexRecomputesColumnUnique pins that dropping a UNIQUE index
// re-derives col.Unique from the UNIQUEs that remain, an inline one
// included, rather than leaving the column flag desynchronized from the
// constraint set.
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
			name: "inline UNIQUE dropped by its derived name clears Unique",
			setupSQL: `
				CREATE TABLE users (
					id INT PRIMARY KEY,
					email VARCHAR(255) UNIQUE
				);
			`,
			dropSQL:     `ALTER TABLE users DROP INDEX email;`,
			column:      "email",
			wantUnique:  false,
			wantComment: "the inline UNIQUE is the key named email",
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

// TestDropConstraintIgnoresNameCase pins that ALTER TABLE drops a key or
// constraint whose name differs from the dropped name only in case, as MySQL
// 8.0 does, and reverses its column flags.
func TestDropConstraintIgnoresNameCase(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want []string // every constraint of v, as "TYPE name [columns]", then flagged columns
	}{
		{
			name: "DROP INDEX on a non-unique key",
			sql:  `CREATE TABLE v (a int, KEY A (a)); ALTER TABLE v DROP INDEX a;`,
		},
		{
			name: "DROP KEY on a unique key clears Unique",
			sql:  `CREATE TABLE v (a int, UNIQUE KEY A (a)); ALTER TABLE v DROP KEY a;`,
		},
		{
			name: "standalone DROP INDEX ON",
			sql:  `CREATE TABLE v (a int, KEY a (a)); DROP INDEX A ON v;`,
		},
		{
			name: "DROP FOREIGN KEY keeps its index and clears the reference",
			sql: `CREATE TABLE p (id int PRIMARY KEY);
				CREATE TABLE v (a int, CONSTRAINT Fk FOREIGN KEY (a) REFERENCES p (id));
				ALTER TABLE v DROP FOREIGN KEY fk;`,
			want: []string{"INDEX Fk [a]"},
		},
		{
			name: "DROP CHECK",
			sql:  `CREATE TABLE v (a int, CONSTRAINT Ck CHECK (a > 0)); ALTER TABLE v DROP CHECK ck;`,
		},
		{
			name: "DROP CONSTRAINT on a check",
			sql:  `CREATE TABLE v (a int, CONSTRAINT Ck CHECK (a > 0)); ALTER TABLE v DROP CONSTRAINT ck;`,
		},
		{
			name: "a dropped foreign key index no longer gives way to a wider key",
			sql: `CREATE TABLE p (id int PRIMARY KEY);
				CREATE TABLE v (a int, b int, CONSTRAINT fk1 FOREIGN KEY (a) REFERENCES p (id));
				ALTER TABLE v DROP FOREIGN KEY FK1;
				ALTER TABLE v DROP INDEX FK1;
				ALTER TABLE v ADD KEY fk1 (a);
				ALTER TABLE v ADD KEY (a, b);`,
			want: []string{"INDEX a [a b]", "INDEX fk1 [a]"},
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
				if tbl.Name != "v" {
					continue
				}
				for _, c := range tbl.Constraints {
					got = append(got, fmt.Sprintf("%s %s %v", c.Type, c.Name, c.Columns))
				}
				for _, c := range tbl.Columns {
					if c.Unique || c.FKReference != nil {
						got = append(got, fmt.Sprintf("column %s unique=%v fk=%v", c.Name, c.Unique, c.FKReference != nil))
					}
				}
			}
			slices.Sort(got)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Parse(%q) constraints mismatch (-want +got):\n%s", tt.sql, diff)
			}
		})
	}
}

func TestParseViewSQL_CompoundSelectRejected(t *testing.T) {
	tests := []struct{ sql, schema string }{
		{"CREATE VIEW v AS SELECT id FROM a UNION SELECT id FROM b;", ""},
		{"CREATE VIEW app.v AS SELECT id FROM a UNION ALL SELECT id FROM b;", "app"},
	}
	for _, tt := range tests {
		_, err := mysql.ParseViewSQL([]byte(tt.sql))
		if err == nil || !strings.Contains(err.Error(), "not a simple SELECT") {
			t.Errorf("ParseViewSQL(%q) error = %v, want not a simple SELECT", tt.sql, err)
		}
		// The error names the view, so an annotation file for a view the
		// database defines can still overlay it.
		qe, ok := errors.AsType[*parser.ViewQueryError](err)
		if !ok || qe.Schema != tt.schema || qe.Name != "v" {
			t.Errorf("ParseViewSQL(%q) error = %#v, want a ViewQueryError naming %q.v", tt.sql, err, tt.schema)
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

// TestNamesFollowDeclarationOrder pins that unnamed keys and foreign keys are
// numbered in the order the CREATE TABLE declares them, wherever a column,
// a key, a FOREIGN KEY or a CHECK falls in its definition list.
func TestNamesFollowDeclarationOrder(t *testing.T) {
	const parent = `CREATE TABLE p (id int PRIMARY KEY, b int, c int, d int, KEY (b, c), KEY (b, c, d));`
	tests := []struct {
		name string
		sql  string
		want []string
	}{
		{
			name: "a foreign key's index before a later key",
			sql:  `CREATE TABLE o2 (b int, c int, FOREIGN KEY (b, c) REFERENCES p (b, c), KEY (b));`,
			want: []string{"FOREIGN KEY o2_ibfk_1 [b c]", "INDEX b [b c]", "INDEX b_2 [b]"},
		},
		{
			name: "a key declared on a column after a table-level key",
			sql:  `CREATE TABLE kc (x int, KEY (a, x), a int UNIQUE);`,
			want: []string{"INDEX a [a x]", "UNIQUE a_2 [a]"},
		},
		{
			name: "a foreign key's index before a key declared on a later column",
			sql:  `CREATE TABLE fc (x int, FOREIGN KEY (b, c) REFERENCES p (b, c), b int UNIQUE, c int);`,
			want: []string{"FOREIGN KEY fc_ibfk_1 [b c]", "INDEX b [b c]", "UNIQUE b_2 [b]"},
		},
		{
			name: "keys, columns and a foreign key interleaved",
			sql:  `CREATE TABLE mx (b int, c int, d int, FOREIGN KEY (b, c) REFERENCES p (b, c), e int UNIQUE, KEY (b), UNIQUE (b, d));`,
			want: []string{"FOREIGN KEY mx_ibfk_1 [b c]", "INDEX b [b c]", "UNIQUE e [e]", "INDEX b_2 [b]", "UNIQUE b_3 [b d]"},
		},
		{
			name: "a foreign key's index replaced by a later key",
			sql:  `CREATE TABLE rk (a int, b int, FOREIGN KEY (a, b) REFERENCES p (b, c), KEY (a), KEY (a, b));`,
			want: []string{"FOREIGN KEY rk_ibfk_1 [a b]", "INDEX a [a]", "INDEX a_2 [a b]"},
		},
		{
			name: "a foreign key's index replaced by a later foreign key's",
			sql:  `CREATE TABLE rf (a int, b int, FOREIGN KEY (a) REFERENCES p (id), FOREIGN KEY (a, b) REFERENCES p (b, c), KEY (a));`,
			want: []string{"FOREIGN KEY rf_ibfk_1 [a]", "FOREIGN KEY rf_ibfk_2 [a b]", "INDEX a [a b]", "INDEX a_2 [a]"},
		},
		{
			name: "a foreign key's index replaced by a later foreign key's, past a key",
			sql:  `CREATE TABLE lf (a int, b int, c int, FOREIGN KEY (a, b) REFERENCES p (b, c), KEY (a), FOREIGN KEY (a, b, c) REFERENCES p (b, c, d));`,
			want: []string{"FOREIGN KEY lf_ibfk_1 [a b]", "FOREIGN KEY lf_ibfk_2 [a b c]", "INDEX a [a]", "INDEX a_2 [a b c]"},
		},
		{
			name: "a foreign key's index kept past a later, shorter foreign key's",
			sql:  `CREATE TABLE sk (a int, b int, FOREIGN KEY (a, b) REFERENCES p (b, c), KEY (b), FOREIGN KEY (a) REFERENCES p (id));`,
			want: []string{"FOREIGN KEY sk_ibfk_1 [a b]", "FOREIGN KEY sk_ibfk_2 [a]", "INDEX a [a b]", "INDEX b [b]"},
		},
		{
			name: "an inline REFERENCES after a table-level foreign key",
			sql:  `CREATE TABLE il (a int, FOREIGN KEY (a) REFERENCES p (id), b int REFERENCES p (id));`,
			want: []string{"FOREIGN KEY il_ibfk_1 [a]", "INDEX a [a]", "FOREIGN KEY il_ibfk_2 [b]", "INDEX b [b]"},
		},
		{
			name: "a CHECK on a column between table-level CHECKs",
			sql:  `CREATE TABLE ck (x int, CHECK (a > 0), a int CONSTRAINT nn CHECK (a < 10), CHECK (a <> 5), y int);`,
			want: []string{"CHECK ck_chk_1 [a]", "CHECK nn [a]", "CHECK ck_chk_2 [a]"},
		},
		{
			name: "keywords in comments, strings and nested parentheses",
			sql: "CREATE TABLE kw (/* KEY (x), */ x int COMMENT 'UNIQUE, CHECK (', s enum('KEY', 'a,b') DEFAULT 'KEY', " +
				"FOREIGN KEY (x) REFERENCES p (id), `key` int UNIQUE, CONSTRAINT UNIQUE (s, x), KEY (s));",
			want: []string{"FOREIGN KEY kw_ibfk_1 [x]", "INDEX x [x]", "UNIQUE key [key]", "UNIQUE s [s x]", "INDEX s_2 [s]"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newParser(t)
			if err := p.Parse("test.sql", []byte(parent+tt.sql)); err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			tables := p.Schema().Tables
			constraints := tables[len(tables)-1].Constraints
			got := make([]string, 0, len(constraints))
			for _, c := range constraints {
				got = append(got, fmt.Sprintf("%s %s %v", c.Type, c.Name, c.Columns))
			}
			slices.Sort(got)
			want := slices.Sorted(slices.Values(tt.want))
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("constraint names mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestDerivedConstraintNames pins the names the parser gives unnamed keys and
// constraints, and which indexes MySQL builds for foreign keys. Each was
// measured on MySQL 8.0.
func TestDerivedConstraintNames(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want []string
	}{
		{
			name: "checks are numbered among the unnamed ones",
			sql:  `CREATE TABLE k (a int, CHECK (a > 0), CONSTRAINT nn CHECK (a < 10), CHECK (a <> 5), b int CHECK (b > 0));`,
			want: []string{"k CHECK k_chk_1", "k CHECK nn", "k CHECK k_chk_2", "k CHECK k_chk_3"},
		},
		{
			name: "foreign keys and the indexes built for them",
			sql: `CREATE TABLE p (id int PRIMARY KEY);
				CREATE TABLE fk (a int, b int, FOREIGN KEY (a) REFERENCES p (id), CONSTRAINT named_fk FOREIGN KEY (b) REFERENCES p (id), FOREIGN KEY (b) REFERENCES p (id));
				ALTER TABLE fk ADD FOREIGN KEY (a) REFERENCES p (id);`,
			want: []string{
				"p PRIMARY KEY PRIMARY",
				"fk FOREIGN KEY fk_ibfk_1", "fk FOREIGN KEY named_fk", "fk FOREIGN KEY fk_ibfk_2", "fk FOREIGN KEY fk_ibfk_3",
				"fk INDEX a", "fk INDEX b",
			},
		},
		{
			name: "a built index takes the constraint name before the index name",
			sql: `CREATE TABLE p (id int PRIMARY KEY);
				CREATE TABLE fi (a int, b int, FOREIGN KEY my_idx (a) REFERENCES p (id), CONSTRAINT cn FOREIGN KEY ix2 (b) REFERENCES p (id));`,
			want: []string{
				"p PRIMARY KEY PRIMARY",
				"fi FOREIGN KEY fi_ibfk_1", "fi FOREIGN KEY cn", "fi INDEX my_idx", "fi INDEX cn",
			},
		},
		{
			name: "unnamed keys share one counter",
			sql: `CREATE TABLE u (a int UNIQUE, UNIQUE KEY (a), UNIQUE KEY a_3 (a), KEY (a));
				CREATE TABLE up (A int, UNIQUE (a), KEY (A));`,
			want: []string{
				"u UNIQUE a", "u UNIQUE a_2", "u UNIQUE a_3", "u INDEX a_4",
				"up UNIQUE A", "up INDEX A_2",
			},
		},
		{
			name: "a declared key covering a foreign key replaces its index",
			sql: `CREATE TABLE p (id int PRIMARY KEY);
				CREATE TABLE o4 (b int UNIQUE, c int, FOREIGN KEY (c) REFERENCES p (id), UNIQUE (c, b), KEY (c));`,
			want: []string{
				"p PRIMARY KEY PRIMARY",
				"o4 UNIQUE b", "o4 UNIQUE c", "o4 INDEX c_2", "o4 FOREIGN KEY o4_ibfk_1",
			},
		},
		{
			name: "a longer built index replaces a built one",
			sql: `CREATE TABLE p (id int PRIMARY KEY, b int, c int, KEY (b, c));
				CREATE TABLE gen (a int, b int, FOREIGN KEY (a) REFERENCES p (id));
				ALTER TABLE gen ADD CONSTRAINT gen_ab FOREIGN KEY (a, b) REFERENCES p (b, c);`,
			want: []string{
				"p PRIMARY KEY PRIMARY", "p INDEX b",
				"gen FOREIGN KEY gen_ibfk_1", "gen FOREIGN KEY gen_ab", "gen INDEX gen_ab",
			},
		},
		{
			name: "rename carries generated names and a matching built index is replaced",
			sql: `CREATE TABLE p (id int PRIMARY KEY);
				CREATE TABLE ren (a int, CHECK (a > 0), FOREIGN KEY (a) REFERENCES p (id), CONSTRAINT ren_ibfk_9 FOREIGN KEY (a) REFERENCES p (id));
				ALTER TABLE ren ADD FOREIGN KEY (a) REFERENCES p (id), ADD CHECK (a < 100);
				RENAME TABLE ren TO ren2;`,
			want: []string{
				"p PRIMARY KEY PRIMARY",
				"ren2 CHECK ren2_chk_1", "ren2 CHECK ren2_chk_2",
				"ren2 FOREIGN KEY ren2_ibfk_1", "ren2 FOREIGN KEY ren2_ibfk_9", "ren2 FOREIGN KEY ren2_ibfk_10",
				"ren2 INDEX a",
			},
		},
		{
			name: "a unique key without an index name takes its constraint name",
			sql: `CREATE TABLE cu (s int, t int, u int, v int, CONSTRAINT cu_s_key UNIQUE (s), CONSTRAINT cu_v_key UNIQUE KEY cu_v_idx (v));
				ALTER TABLE cu ADD CONSTRAINT cu_t_key UNIQUE (t);
				ALTER TABLE cu ADD CONSTRAINT cu_u_key UNIQUE KEY (u);`,
			want: []string{"cu UNIQUE cu_s_key", "cu UNIQUE cu_t_key", "cu UNIQUE cu_u_key", "cu UNIQUE cu_v_idx"},
		},
		{
			name: "a unique key named by its constraint drops by that name",
			sql: `CREATE TABLE cd (a int, CONSTRAINT cd_a_key UNIQUE (a));
				ALTER TABLE cd DROP INDEX cd_a_key;`,
		},
		{
			name: "a primary key is PRIMARY whatever its declared name",
			sql:  `CREATE TABLE pk3 (a int, CONSTRAINT mypk PRIMARY KEY (a));`,
			want: []string{"pk3 PRIMARY KEY PRIMARY"},
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
				for _, c := range tbl.Constraints {
					got = append(got, fmt.Sprintf("%s %s %s", tbl.Name, c.Type, c.Name))
				}
			}
			slices.Sort(got)
			want := slices.Sorted(slices.Values(tt.want))
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("constraint names mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestExpressionKeys pins that a key with an expression part is not listed,
// unique or not, while it keeps its name, an unnamed one named
// functional_index, and serves as a foreign key's index when its leading
// columns are the foreign key's, as on MySQL 8.0 (PRD §30.7).
func TestExpressionKeys(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want []string // every UNIQUE and INDEX, as "table TYPE name [columns]"
	}{
		{name: "unique expression only", sql: `CREATE TABLE e (x varchar(9)); CREATE UNIQUE INDEX ux ON e ((lower(x)));`},
		{name: "unique column and expression", sql: `CREATE TABLE e (a int, x varchar(9), UNIQUE INDEX ux (a, (lower(x))));`},
		{name: "column and expression", sql: `CREATE TABLE e (a int, b int); ALTER TABLE e ADD INDEX ix (a, (b + 1));`},
		{
			name: "a later unnamed key takes the name it would have",
			sql:  `CREATE TABLE e (a int, b int, KEY (a, (b + 1)), KEY (a));`,
			want: []string{"e INDEX a [a]"},
		},
		{
			name: "a named expression key keeps its name",
			sql:  `CREATE TABLE e (a int, b int, KEY a ((b + 1)), KEY (a));`,
			want: []string{"e INDEX a_2 [a]"},
		},
		{
			name: "a unique expression key takes its constraint name",
			sql:  `CREATE TABLE e (a int, b int, CONSTRAINT a UNIQUE ((b + 1)), KEY (a));`,
			want: []string{"e INDEX a_2 [a]"},
		},
		{
			name: "an unnamed expression key is functional_index",
			sql:  `CREATE TABLE e (functional_index int, b int, KEY ((b + 1)), KEY (functional_index));`,
			want: []string{"e INDEX functional_index_2 [functional_index]"},
		},
		{
			name: "dropping an expression key frees its name",
			sql: `CREATE TABLE e (a int, b int, KEY A ((b + 1)));
				ALTER TABLE e DROP INDEX a;
				ALTER TABLE e ADD KEY (a);`,
			want: []string{"e INDEX a [a]"},
		},
		{
			name: "dropping the table frees its name",
			sql: `CREATE TABLE e (a int, b int, KEY a ((b + 1)));
				DROP TABLE e;
				CREATE TABLE e (a int, KEY (a));`,
			want: []string{"e INDEX a [a]"},
		},
		{
			name: "renaming the table carries its name",
			sql: `CREATE TABLE e (a int, b int, KEY a ((b + 1)));
				ALTER TABLE e RENAME TO e2;
				ALTER TABLE e2 ADD KEY (a);`,
			want: []string{"e2 INDEX a_2 [a]"},
		},
		{
			name: "renaming the table to its own name keeps its name",
			sql: `CREATE TABLE e (a int, b int, KEY a ((b + 1)));
				ALTER TABLE e RENAME TO e;
				ALTER TABLE e ADD KEY (a);`,
			want: []string{"e INDEX a_2 [a]"},
		},
		{
			name: "an expression key serves a foreign key",
			sql: `CREATE TABLE p (id int PRIMARY KEY);
				CREATE TABLE e (a int, b int, KEY (a, (b + 1)), FOREIGN KEY (a) REFERENCES p (id));
				CREATE TABLE e2 (a int, b int, FOREIGN KEY (a) REFERENCES p (id), UNIQUE (a, (b + 1)));`,
		},
		{
			name: "an expression key replaces a foreign key's index",
			sql: `CREATE TABLE p (id int PRIMARY KEY);
				CREATE TABLE e (a int, b int, FOREIGN KEY (a) REFERENCES p (id));
				ALTER TABLE e ADD INDEX (a, (b + 1));`,
		},
		{
			name: "an expression part does not match a foreign key column",
			sql: `CREATE TABLE p (x int, y int, KEY (x, y));
				CREATE TABLE e (a int, b int, FOREIGN KEY (a, b) REFERENCES p (x, y), KEY (a, (b + 1)));`,
			want: []string{"p INDEX x [x y]", "e INDEX a [a b]"},
		},
		{
			name: "a renamed column stays in the expression key",
			sql: `CREATE TABLE p (id int PRIMARY KEY);
				CREATE TABLE e (a int, b int, KEY (a, (b + 1)));
				ALTER TABLE e RENAME COLUMN a TO c;
				ALTER TABLE e ADD FOREIGN KEY (c) REFERENCES p (id);
				CREATE TABLE e2 (a int, b int, KEY (a, (b + 1)));
				ALTER TABLE e2 CHANGE COLUMN a c int;
				ALTER TABLE e2 ADD FOREIGN KEY (c) REFERENCES p (id);`,
		},
		{
			name: "a dropped column leaves the expression key",
			sql: `CREATE TABLE p (id int PRIMARY KEY);
				CREATE TABLE e (a int, b int, KEY (a, (b + 1)));
				ALTER TABLE e DROP COLUMN a;
				ALTER TABLE e ADD COLUMN a int;
				ALTER TABLE e ADD FOREIGN KEY (a) REFERENCES p (id);`,
			want: []string{"e INDEX a [a]"},
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
				for _, c := range tbl.Constraints {
					if c.Type == parser.Unique || c.Type == parser.Index {
						got = append(got, fmt.Sprintf("%s %s %s %v", tbl.Name, c.Type, c.Name, c.Columns))
					}
				}
				for _, col := range tbl.Columns {
					if col.Unique {
						t.Errorf("%s.%s: Unique = true", tbl.Name, col.Name)
					}
				}
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("keys mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestSeedKeepsExpressionKeys covers `input.source: both`: an introspected key
// with an expression part keeps its name against a key the DDL files add, and
// serves a foreign key whose columns lead it, as on MySQL 8.0.
func TestSeedKeepsExpressionKeys(t *testing.T) {
	tests := []struct {
		name string
		key  parser.ExpressionIndex
		sql  string
		want []string // every UNIQUE and INDEX of e, as "name [columns]"
	}{
		{
			name: "a named expression key keeps its name",
			key:  parser.ExpressionIndex{Name: "a", Columns: []string{""}},
			sql:  `ALTER TABLE e ADD KEY (a);`,
			want: []string{"a_2 [a]"},
		},
		{
			name: "an unnamed expression key keeps functional_index",
			key:  parser.ExpressionIndex{Name: "functional_index", Columns: []string{""}},
			sql:  `ALTER TABLE e ADD KEY (functional_index);`,
			want: []string{"functional_index_2 [functional_index]"},
		},
		{
			name: "an expression key serves a foreign key",
			key:  parser.ExpressionIndex{Name: "k", Columns: []string{"a", ""}},
			sql:  `ALTER TABLE e ADD FOREIGN KEY (a) REFERENCES p (id);`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newParser(t)
			p.Seed(&parser.Schema{Tables: []parser.Table{
				{
					Name:        "p",
					Columns:     []parser.Column{{Name: "id", Type: "int", PrimaryKey: true}},
					Constraints: []parser.Constraint{{Name: "PRIMARY", Type: parser.PrimaryKey, Columns: []string{"id"}, Method: "btree"}},
				},
				{
					Name:              "e",
					Columns:           []parser.Column{{Name: "a", Type: "int", Nullable: true}, {Name: "functional_index", Type: "int", Nullable: true}, {Name: "b", Type: "int", Nullable: true}},
					ExpressionIndexes: []parser.ExpressionIndex{tt.key},
				},
			}})
			if err := p.Parse("test.sql", []byte(tt.sql)); err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			var got []string
			for _, c := range p.Schema().Tables[1].Constraints {
				if c.Type == parser.Unique || c.Type == parser.Index {
					got = append(got, fmt.Sprintf("%s %v", c.Name, c.Columns))
				}
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("keys mismatch (-want +got):\n%s", diff)
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
		{name: "right join", sql: `CREATE VIEW v AS SELECT a.id FROM a RIGHT JOIN b ON b.a_id = a.id;`, want: map[string]bool{"a": true}},
		{name: "join nested under the outer side", sql: `CREATE VIEW v AS SELECT a.id FROM a LEFT JOIN (b JOIN c ON c.b_id = b.id) ON b.a_id = a.id;`, want: map[string]bool{"b": true, "c": true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mysql.ParseViewSQL([]byte(tt.sql))
			if err != nil {
				t.Fatalf("ParseViewSQL() error: %v", err)
			}
			if diff := cmp.Diff(tt.want, got.OuterAliases); diff != "" {
				t.Errorf("OuterAliases mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestParseViewSQL_ColumnNames checks that every view column has a name (PRD
// §16.1): an unaliased expression with no column name, * and t.* are
// ViewQueryErrors, and a column list names the columns in order. MySQL
// requires the list to name every column.
func TestParseViewSQL_ColumnNames(t *testing.T) {
	tests := []struct {
		name, sql string
		want      []string
		wantErr   string
	}{
		{name: "unaliased expression", sql: "CREATE VIEW v AS SELECT id, a + b FROM t", wantErr: "view v selects a + b with no alias and no column name"},
		{name: "unaliased aggregate", sql: "CREATE VIEW v AS SELECT id, COUNT(*) FROM t GROUP BY id", wantErr: "view v selects count(*) with no alias"},
		{name: "star", sql: "CREATE VIEW v AS SELECT * FROM t", wantErr: "view v selects *, whose columns the view parser cannot list"},
		{name: "table star", sql: "CREATE VIEW v AS SELECT id, t.* FROM t", wantErr: "view v selects t.*, whose columns"},
		{name: "column list", sql: "CREATE VIEW v (x, total) AS SELECT id, a + b FROM t", want: []string{"x", "total"}},
		{name: "short column list", sql: "CREATE VIEW v (x) AS SELECT id, name FROM t", wantErr: "view v names 1 columns in its column list but selects 2"},
		{name: "long column list", sql: "CREATE VIEW v (x, y) AS SELECT id FROM t", wantErr: "view v names 2 columns in its column list but selects 1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mysql.ParseViewSQL([]byte(tt.sql))
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
