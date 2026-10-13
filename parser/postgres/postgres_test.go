package postgres_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/parser/postgres"
)

func TestCreateTableColumnTypes(t *testing.T) {
	sql := `CREATE TABLE test_types (
		t_text text,
		t_int integer,
		t_smallint smallint,
		t_bigint bigint,
		t_bool boolean,
		t_real real,
		t_double double precision,
		t_numeric numeric(10, 2),
		t_numeric_arr numeric(12, 2)[],
		t_varchar varchar(255),
		t_char char(1),
		t_timestamp timestamp,
		t_timestamptz timestamptz,
		t_date date,
		t_time time,
		t_uuid uuid,
		t_json json,
		t_jsonb jsonb,
		t_bytea bytea,
		t_array text[]
	);`

	p := postgres.New("")
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	schema := p.Schema()
	if len(schema.Tables) != 1 {
		t.Fatalf("Parse() tables = %d, want 1", len(schema.Tables))
	}
	table := schema.Tables[0]
	if table.Name != "test_types" || table.Schema != "public" {
		t.Errorf("table = %s.%s, want public.test_types", table.Schema, table.Name)
	}

	wantTypes := map[string]string{
		"t_text":        "text",
		"t_int":         "integer",
		"t_smallint":    "smallint",
		"t_bigint":      "bigint",
		"t_bool":        "boolean",
		"t_real":        "real",
		"t_double":      "double precision",
		"t_numeric":     "numeric(10,2)",
		"t_numeric_arr": "numeric(12,2)[]",
		"t_varchar":     "varchar(255)",
		"t_char":        "char(1)",
		"t_timestamp":   "timestamp",
		"t_timestamptz": "timestamptz",
		"t_date":        "date",
		"t_time":        "time",
		"t_uuid":        "uuid",
		"t_json":        "json",
		"t_jsonb":       "jsonb",
		"t_bytea":       "bytea",
		"t_array":       "text[]",
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
		id integer PRIMARY KEY,
		email text UNIQUE NOT NULL,
		amount numeric(10, 2) DEFAULT 0,
		user_id integer REFERENCES users(id)
	);`

	// Pre-create the referenced table so FK parsing doesn't fail.
	// Actually, inline FK parsing doesn't require the table to exist in the parser.
	p := postgres.New("")
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

func TestCreateTableTableLevelConstraints(t *testing.T) {
	sql := `CREATE TABLE order_items (
		order_id integer,
		product_id integer,
		quantity integer,
		CONSTRAINT order_items_pk PRIMARY KEY (order_id, product_id),
		CONSTRAINT order_items_qty_unique UNIQUE (order_id, quantity),
		CONSTRAINT order_items_order_fk FOREIGN KEY (order_id) REFERENCES orders(id),
		CONSTRAINT order_items_qty_check CHECK (quantity > 0)
	);`

	p := postgres.New("")
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	table := p.Schema().Tables[0]

	// Check constraint count
	if len(table.Constraints) != 4 {
		t.Fatalf("constraints = %d, want 4", len(table.Constraints))
	}

	constraints := make(map[string]parser.Constraint)
	for _, c := range table.Constraints {
		constraints[c.Name] = c
	}

	// Composite PK
	pk := constraints["order_items_pk"]
	if pk.Type != parser.PrimaryKey {
		t.Errorf("order_items_pk type = %v, want PrimaryKey", pk.Type)
	}
	if diff := cmp.Diff([]string{"order_id", "product_id"}, pk.Columns); diff != "" {
		t.Errorf("order_items_pk columns mismatch (-want +got):\n%s", diff)
	}

	// Composite PK columns should be marked as PK
	cols := make(map[string]parser.Column)
	for _, c := range table.Columns {
		cols[c.Name] = c
	}
	if !cols["order_id"].PrimaryKey || !cols["product_id"].PrimaryKey {
		t.Error("composite PK columns should have PrimaryKey=true")
	}

	// Composite UNIQUE (should NOT set Column.Unique since it's multi-column)
	uq := constraints["order_items_qty_unique"]
	if uq.Type != parser.Unique {
		t.Errorf("order_items_qty_unique type = %v, want Unique", uq.Type)
	}

	// Named FK
	fk := constraints["order_items_order_fk"]
	if fk.Type != parser.ForeignKey {
		t.Errorf("order_items_order_fk type = %v, want ForeignKey", fk.Type)
	}
	if fk.ReferenceTable != "orders" {
		t.Errorf("FK reference table = %q, want %q", fk.ReferenceTable, "orders")
	}
	if diff := cmp.Diff([]string{"id"}, fk.ReferenceColumns); diff != "" {
		t.Errorf("FK reference columns mismatch (-want +got):\n%s", diff)
	}

	// CHECK
	chk := constraints["order_items_qty_check"]
	if chk.Type != parser.Check {
		t.Errorf("order_items_qty_check type = %v, want Check", chk.Type)
	}
	if chk.CheckExpression == "" {
		t.Error("CHECK expression is empty, want non-empty")
	}
}

func TestCreateEnumType(t *testing.T) {
	sql := `CREATE TYPE status AS ENUM ('active', 'inactive', 'pending');`

	p := postgres.New("")
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	schema := p.Schema()
	if len(schema.Enums) != 1 {
		t.Fatalf("enums = %d, want 1", len(schema.Enums))
	}

	want := parser.Enum{
		Name:   "status",
		Schema: "public",
		Values: []string{"active", "inactive", "pending"},
	}
	if diff := cmp.Diff(want, schema.Enums[0]); diff != "" {
		t.Errorf("enum mismatch (-want +got):\n%s", diff)
	}
}

func TestCreateCompositeType(t *testing.T) {
	sql := `CREATE TYPE address AS (
		street text,
		city text,
		zip varchar(10)
	);`

	p := postgres.New("")
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	schema := p.Schema()
	if len(schema.CompositeTypes) != 1 {
		t.Fatalf("composite types = %d, want 1", len(schema.CompositeTypes))
	}

	ct := schema.CompositeTypes[0]
	if ct.Name != "address" || ct.Schema != "public" {
		t.Errorf("composite type = %s.%s, want public.address", ct.Schema, ct.Name)
	}

	wantAttrs := []parser.Attribute{
		{Name: "street", Type: "text"},
		{Name: "city", Type: "text"},
		{Name: "zip", Type: "varchar(10)"},
	}
	if diff := cmp.Diff(wantAttrs, ct.Attributes); diff != "" {
		t.Errorf("attributes mismatch (-want +got):\n%s", diff)
	}
}

func TestCreateDomain(t *testing.T) {
	sql := `CREATE DOMAIN positive_int AS integer CHECK (VALUE > 0);`

	p := postgres.New("")
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	schema := p.Schema()
	if len(schema.DomainTypes) != 1 {
		t.Fatalf("domain types = %d, want 1", len(schema.DomainTypes))
	}

	dt := schema.DomainTypes[0]
	if dt.Name != "positive_int" || dt.Schema != "public" {
		t.Errorf("domain = %s.%s, want public.positive_int", dt.Schema, dt.Name)
	}
	if dt.BaseType != "integer" {
		t.Errorf("base type = %q, want %q", dt.BaseType, "integer")
	}
	if len(dt.Constraints) != 1 || dt.Constraints[0].Type != parser.Check {
		t.Errorf("constraints = %v, want 1 CHECK constraint", dt.Constraints)
	}
	if dt.Constraints[0].CheckExpression == "" {
		t.Error("CHECK expression is empty, want non-empty")
	}
}

func TestCommentOnTableAndColumn(t *testing.T) {
	sql := `
		CREATE TABLE users (
			id integer PRIMARY KEY,
			email text
		);
		COMMENT ON TABLE users IS 'User accounts';
		COMMENT ON COLUMN users.email IS 'Email address';
	`

	p := postgres.New("")
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	table := p.Schema().Tables[0]
	if table.Comment != "User accounts" {
		t.Errorf("table comment = %q, want %q", table.Comment, "User accounts")
	}

	for _, col := range table.Columns {
		if col.Name == "email" {
			if col.Comment != "Email address" {
				t.Errorf("column email comment = %q, want %q", col.Comment, "Email address")
			}
			return
		}
	}
	t.Error("column email not found")
}

func TestCommentOnTypeAndDomain(t *testing.T) {
	sql := `
		CREATE TYPE user_role AS ENUM ('admin', 'member', 'guest');
		CREATE TYPE address AS (
			street text,
			city text
		);
		CREATE DOMAIN email AS text CHECK (VALUE ~ '@');
		COMMENT ON TYPE user_role IS 'Access level assigned to a user';
		COMMENT ON TYPE address IS 'Mailing address components';
		COMMENT ON DOMAIN email IS 'RFC 5321 email address';
	`

	p := postgres.New("")
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	schema := p.Schema()

	// Verify enum comment.
	if len(schema.Enums) != 1 {
		t.Fatalf("enums = %d, want 1", len(schema.Enums))
	}
	if schema.Enums[0].Comment != "Access level assigned to a user" {
		t.Errorf("enum comment = %q, want %q", schema.Enums[0].Comment, "Access level assigned to a user")
	}

	// Verify composite type comment.
	if len(schema.CompositeTypes) != 1 {
		t.Fatalf("composite types = %d, want 1", len(schema.CompositeTypes))
	}
	if schema.CompositeTypes[0].Comment != "Mailing address components" {
		t.Errorf("composite type comment = %q, want %q", schema.CompositeTypes[0].Comment, "Mailing address components")
	}

	// Verify domain comment.
	if len(schema.DomainTypes) != 1 {
		t.Fatalf("domain types = %d, want 1", len(schema.DomainTypes))
	}
	if schema.DomainTypes[0].Comment != "RFC 5321 email address" {
		t.Errorf("domain comment = %q, want %q", schema.DomainTypes[0].Comment, "RFC 5321 email address")
	}
}

func TestAlterTableAddDropColumn(t *testing.T) {
	sql := `
		CREATE TABLE users (
			id integer PRIMARY KEY,
			name text
		);
		ALTER TABLE users ADD COLUMN email text NOT NULL;
		ALTER TABLE users DROP COLUMN name;
	`

	p := postgres.New("")
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

	// Verify the added column's NOT NULL
	for _, c := range table.Columns {
		if c.Name == "email" && c.Nullable {
			t.Error("email column should be NOT NULL after ALTER TABLE ADD COLUMN")
		}
	}
}

func TestAlterTableAddDropConstraint(t *testing.T) {
	sql := `
		CREATE TABLE users (
			id integer,
			email text
		);
		ALTER TABLE users ADD CONSTRAINT users_pk PRIMARY KEY (id);
		ALTER TABLE users ADD CONSTRAINT users_email_unique UNIQUE (email);
	`

	p := postgres.New("")
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	table := p.Schema().Tables[0]

	// Check PK was applied
	cols := make(map[string]parser.Column)
	for _, c := range table.Columns {
		cols[c.Name] = c
	}
	if !cols["id"].PrimaryKey {
		t.Error("id should be PrimaryKey after ADD CONSTRAINT")
	}
	if !cols["email"].Unique {
		t.Error("email should be Unique after ADD CONSTRAINT")
	}

	// Now drop constraints
	sql2 := `
		ALTER TABLE users DROP CONSTRAINT users_email_unique;
	`
	if err := p.Parse("test2.sql", []byte(sql2)); err != nil {
		t.Fatalf("Parse() error on drop: %v", err)
	}

	// Refresh the table after second parse
	table = p.Schema().Tables[0]
	cols = make(map[string]parser.Column)
	for _, c := range table.Columns {
		cols[c.Name] = c
	}
	if cols["email"].Unique {
		t.Error("email should not be Unique after DROP CONSTRAINT")
	}
	// PK should still be there
	if !cols["id"].PrimaryKey {
		t.Error("id should still be PrimaryKey after dropping a different constraint")
	}
}

func TestSchemaQualification(t *testing.T) {
	tests := []struct {
		name       string
		sql        string
		defSchema  string
		wantSchema string
		wantName   string
	}{
		{
			name:       "bare name gets default schema",
			sql:        "CREATE TABLE products (id integer);",
			defSchema:  "",
			wantSchema: "public",
			wantName:   "products",
		},
		{
			name:       "bare name gets custom default schema",
			sql:        "CREATE TABLE products (id integer);",
			defSchema:  "store",
			wantSchema: "store",
			wantName:   "products",
		},
		{
			name:       "explicit schema preserved",
			sql:        "CREATE TABLE myschema.products (id integer);",
			defSchema:  "",
			wantSchema: "myschema",
			wantName:   "products",
		},
		{
			name:       "explicit public schema preserved",
			sql:        "CREATE TABLE public.products (id integer);",
			defSchema:  "other",
			wantSchema: "public",
			wantName:   "products",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := postgres.New(tt.defSchema)
			if err := p.Parse("test.sql", []byte(tt.sql)); err != nil {
				t.Fatalf("Parse() error: %v", err)
			}

			table := p.Schema().Tables[0]
			if table.Schema != tt.wantSchema {
				t.Errorf("schema = %q, want %q", table.Schema, tt.wantSchema)
			}
			if table.Name != tt.wantName {
				t.Errorf("name = %q, want %q", table.Name, tt.wantName)
			}
		})
	}
}

func TestQuotedIdentifiers(t *testing.T) {
	sql := `CREATE TABLE "order" (
		"select" integer PRIMARY KEY,
		"from" text NOT NULL
	);`

	p := postgres.New("")
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	table := p.Schema().Tables[0]
	if table.Name != "order" {
		t.Errorf("table name = %q, want %q", table.Name, "order")
	}

	colNames := make([]string, 0, len(table.Columns))
	for _, c := range table.Columns {
		colNames = append(colNames, c.Name)
	}
	wantCols := []string{"select", "from"}
	if diff := cmp.Diff(wantCols, colNames); diff != "" {
		t.Errorf("column names mismatch (-want +got):\n%s", diff)
	}
}

func TestSerialTypes(t *testing.T) {
	tests := []struct {
		name         string
		sql          string
		wantType     string
		wantAutoIncr bool
		wantNullable bool
	}{
		{
			name:         "serial",
			sql:          "CREATE TABLE t (id serial);",
			wantType:     "integer",
			wantAutoIncr: true,
			wantNullable: false,
		},
		{
			name:         "bigserial",
			sql:          "CREATE TABLE t (id bigserial);",
			wantType:     "bigint",
			wantAutoIncr: true,
			wantNullable: false,
		},
		{
			name:         "smallserial",
			sql:          "CREATE TABLE t (id smallserial);",
			wantType:     "smallint",
			wantAutoIncr: true,
			wantNullable: false,
		},
		{
			name:         "generated always as identity",
			sql:          "CREATE TABLE t (id integer GENERATED ALWAYS AS IDENTITY);",
			wantType:     "integer",
			wantAutoIncr: true,
			wantNullable: false,
		},
		{
			name:         "generated by default as identity bigint",
			sql:          "CREATE TABLE t (id bigint GENERATED BY DEFAULT AS IDENTITY);",
			wantType:     "bigint",
			wantAutoIncr: true,
			wantNullable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := postgres.New("")
			if err := p.Parse("test.sql", []byte(tt.sql)); err != nil {
				t.Fatalf("Parse() error: %v", err)
			}

			col := p.Schema().Tables[0].Columns[0]
			if col.Type != tt.wantType {
				t.Errorf("type = %q, want %q", col.Type, tt.wantType)
			}
			if col.AutoIncrement != tt.wantAutoIncr {
				t.Errorf("auto_increment = %v, want %v", col.AutoIncrement, tt.wantAutoIncr)
			}
			if col.Nullable != tt.wantNullable {
				t.Errorf("nullable = %v, want %v", col.Nullable, tt.wantNullable)
			}
		})
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
			sql:  "CREATE TABLE t (qty integer, total integer GENERATED ALWAYS AS (qty * 2) STORED);",
			want: parser.Column{Name: "total", Type: "integer", Nullable: true, GeneratedExpr: "qty * 2", GeneratedStorage: "STORED"},
		},
		{
			name: "not null generated column",
			sql:  "CREATE TABLE t (qty integer, total integer NOT NULL GENERATED ALWAYS AS (qty + 1) STORED);",
			want: parser.Column{Name: "total", Type: "integer", GeneratedExpr: "qty + 1", GeneratedStorage: "STORED"},
		},
		{
			name: "generated column added by alter table",
			sql: `CREATE TABLE t (qty integer);
				ALTER TABLE t ADD COLUMN total integer GENERATED ALWAYS AS (abs(qty)) STORED;`,
			want: parser.Column{Name: "total", Type: "integer", Nullable: true, GeneratedExpr: "abs(qty)", GeneratedStorage: "STORED"},
		},
		{
			name: "identity column is auto-increment, not generated",
			sql:  "CREATE TABLE t (qty integer, total integer GENERATED ALWAYS AS IDENTITY);",
			want: parser.Column{Name: "total", Type: "integer", AutoIncrement: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := postgres.New("")
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
		{name: "outer parentheses dropped", expr: "(qty * 2)", want: "qty * 2"},
		{name: "function call", expr: "abs(qty)", want: "abs(qty)"},
		{name: "string concatenation", expr: "(upper(name) || '-x')", want: "upper(name) || '-x'"},
		{name: "already normalized", expr: "qty + 1", want: "qty + 1"},
		{name: "incomplete expression", expr: "qty *", wantErr: true},
		{name: "more than an expression", expr: "qty FROM t", wantErr: true},
		{name: "two expressions", expr: "qty, price", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := postgres.NormalizeExpr(tt.expr)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NormalizeExpr(%q) error = %v, wantErr %t", tt.expr, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("NormalizeExpr(%q) = %q, want %q", tt.expr, got, tt.want)
			}
		})
	}
}

func TestEmptyTable(t *testing.T) {
	sql := `CREATE TABLE empty (id integer PRIMARY KEY);`

	p := postgres.New("")
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	table := p.Schema().Tables[0]
	if len(table.Columns) != 1 {
		t.Errorf("columns = %d, want 1", len(table.Columns))
	}
	if !table.Columns[0].PrimaryKey {
		t.Error("id should be PrimaryKey")
	}
}

func TestTableWithNoPK(t *testing.T) {
	sql := `CREATE TABLE logs (
		message text,
		created_at timestamp
	);`

	p := postgres.New("")
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	table := p.Schema().Tables[0]
	for _, col := range table.Columns {
		if col.PrimaryKey {
			t.Errorf("column %q should not be PrimaryKey", col.Name)
		}
	}
	if len(table.Constraints) != 0 {
		t.Errorf("constraints = %d, want 0", len(table.Constraints))
	}
}

func TestParserReentrant(t *testing.T) {
	sql1 := `CREATE TABLE users (id integer PRIMARY KEY, name text);`
	sql2 := `CREATE TABLE posts (id integer PRIMARY KEY, user_id integer REFERENCES users(id), title text);`

	p := postgres.New("")
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

func TestSchemaQualifiedFK(t *testing.T) {
	sql := `
		CREATE TABLE public.users (id integer PRIMARY KEY);
		CREATE TABLE billing.invoices (
			id integer PRIMARY KEY,
			user_id integer REFERENCES public.users(id)
		);
	`

	p := postgres.New("")
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	invoices := p.Schema().Tables[1]
	for _, col := range invoices.Columns {
		if col.Name == "user_id" {
			if col.FKReference == nil {
				t.Fatal("user_id FKReference is nil")
			}
			if col.FKReference.Schema != "public" {
				t.Errorf("FK schema = %q, want %q", col.FKReference.Schema, "public")
			}
			if col.FKReference.Table != "users" {
				t.Errorf("FK table = %q, want %q", col.FKReference.Table, "users")
			}
			return
		}
	}
	t.Error("user_id column not found")
}

// TestUnqualifiedFKTargetSchema pins how an unqualified REFERENCES target is
// resolved: to input.schema (default "public"), the way PostgreSQL resolves it
// through the search path, not to the schema of the table declaring the key.
// Every spelling of a foreign key goes through the rule: inline on
// a column, as a table constraint, and through ALTER TABLE ADD CONSTRAINT.
func TestUnqualifiedFKTargetSchema(t *testing.T) {
	type ref struct{ Table, Column, Schema, Target string }
	tests := []struct {
		name          string
		defaultSchema string
		sql           string
		want          []ref
	}{
		{
			name: "qualified target keeps its schema",
			sql: `CREATE TABLE audit.documents (id uuid PRIMARY KEY);
				CREATE TABLE audit.labels (document_id uuid REFERENCES audit.documents(id));`,
			want: []ref{{"audit.labels", "document_id", "audit", "documents"}},
		},
		{
			name: "unqualified target from a public table resolves to public",
			sql: `CREATE TABLE documents (id uuid PRIMARY KEY);
				CREATE TABLE labels (document_id uuid REFERENCES documents(id));`,
			want: []ref{{"public.labels", "document_id", "public", "documents"}},
		},
		{
			name: "inline column FK from another schema resolves to public",
			sql: `CREATE TABLE documents (id uuid PRIMARY KEY);
				CREATE TABLE audit.labels (document_id uuid REFERENCES documents(id));`,
			want: []ref{{"audit.labels", "document_id", "public", "documents"}},
		},
		{
			name: "table constraint FK from another schema resolves to public",
			sql: `CREATE TABLE documents (id uuid PRIMARY KEY);
				CREATE TABLE audit.labels (document_id uuid, FOREIGN KEY (document_id) REFERENCES documents(id));`,
			want: []ref{{"audit.labels", "document_id", "public", "documents"}},
		},
		{
			name: "ALTER TABLE ADD CONSTRAINT FK from another schema resolves to public",
			sql: `CREATE TABLE documents (id uuid PRIMARY KEY);
				CREATE TABLE audit.labels (document_id uuid);
				ALTER TABLE audit.labels ADD CONSTRAINT labels_doc_fk FOREIGN KEY (document_id) REFERENCES documents(id);`,
			want: []ref{{"audit.labels", "document_id", "public", "documents"}},
		},
		{
			name: "same name in both schemas resolves to public, not the declaring schema",
			sql: `CREATE TABLE users (id uuid PRIMARY KEY);
				CREATE TABLE audit.users (id uuid PRIMARY KEY);
				CREATE TABLE audit.logins (id uuid PRIMARY KEY, user_id uuid REFERENCES users(id));`,
			want: []ref{{"audit.logins", "user_id", "public", "users"}},
		},
		{
			name:          "input.schema sets the schema an unqualified target resolves to",
			defaultSchema: "audit",
			sql: `CREATE TABLE documents (id uuid PRIMARY KEY);
				CREATE TABLE public.labels (document_id uuid REFERENCES documents(id));`,
			want: []ref{{"public.labels", "document_id", "audit", "documents"}},
		},
		{
			name: "renaming the referenced table carries the reference along",
			sql: `CREATE TABLE users (id uuid PRIMARY KEY);
				CREATE TABLE posts (id uuid PRIMARY KEY, user_id uuid REFERENCES users(id));
				ALTER TABLE users RENAME TO accounts;`,
			want: []ref{{"public.posts", "user_id", "public", "accounts"}},
		},
		{
			name: "moving the referenced table to another schema carries the reference along",
			sql: `CREATE TABLE users (id uuid PRIMARY KEY, boss_id uuid REFERENCES users(id));
				CREATE TABLE posts (id uuid PRIMARY KEY, user_id uuid, FOREIGN KEY (user_id) REFERENCES users(id));
				ALTER TABLE users SET SCHEMA auth;`,
			want: []ref{
				{"auth.users", "boss_id", "auth", "users"},
				{"public.posts", "user_id", "auth", "users"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := postgres.New(tt.defaultSchema)
			if err := p.Parse("test.sql", []byte(tt.sql)); err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			// A table-level FK constraint is recorded both as a Constraint
			// and on its column's FKReference; both must name the target.
			var got []ref
			for _, tbl := range p.Schema().Tables {
				name := tbl.Schema + "." + tbl.Name
				for _, col := range tbl.Columns {
					if col.FKReference != nil {
						got = append(got, ref{name, col.Name, col.FKReference.Schema, col.FKReference.Table})
					}
				}
				for _, c := range tbl.Constraints {
					if c.Type != parser.ForeignKey {
						continue
					}
					cr := ref{name, strings.Join(c.Columns, ","), c.ReferenceSchema, c.ReferenceTable}
					if !slices.Contains(got, cr) {
						t.Errorf("FK constraint %+v disagrees with its column's FKReference in %+v", cr, got)
					}
				}
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("FK references mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestImplicitReferenceColumns pins `REFERENCES t` with no column list: the
// foreign key references t's primary key as t has it once the statement is
// applied, as PostgreSQL resolves it, and it is an error when t does not exist
// yet or has no primary key.
func TestImplicitReferenceColumns(t *testing.T) {
	type fk struct {
		Table, Name string
		RefColumns  []string
		Column      *parser.FKReference // FKReference of the key's one column
	}
	tests := []struct {
		name    string
		sql     string
		want    []fk
		wantErr string
	}{
		{
			name: "inline",
			sql:  `CREATE TABLE u (id int PRIMARY KEY); CREATE TABLE t (uid int REFERENCES u);`,
			want: []fk{{"public.t", "t_uid_fkey", []string{"id"}, &parser.FKReference{Table: "u", Schema: "public", Column: "id"}}},
		},
		{
			name: "table level",
			sql:  `CREATE TABLE u (id int PRIMARY KEY); CREATE TABLE t (vid int, FOREIGN KEY (vid) REFERENCES u);`,
			want: []fk{{"public.t", "t_vid_fkey", []string{"id"}, &parser.FKReference{Table: "u", Schema: "public", Column: "id"}}},
		},
		{
			name: "composite takes the primary key's column order",
			sql:  `CREATE TABLE u (a int, b int, PRIMARY KEY (b, a)); CREATE TABLE t (x int, y int, FOREIGN KEY (x, y) REFERENCES u);`,
			want: []fk{{"public.t", "t_x_y_fkey", []string{"b", "a"}, nil}},
		},
		{
			name: "qualified target",
			sql:  `CREATE TABLE o.p (pk int PRIMARY KEY); CREATE TABLE t (r int REFERENCES o.p);`,
			want: []fk{{"public.t", "t_r_fkey", []string{"pk"}, &parser.FKReference{Table: "p", Schema: "o", Column: "pk"}}},
		},
		{
			name: "self reference with the primary key declared later",
			sql:  `CREATE TABLE s (parent int REFERENCES s, id int PRIMARY KEY);`,
			want: []fk{{"public.s", "s_parent_fkey", []string{"id"}, &parser.FKReference{Table: "s", Schema: "public", Column: "id"}}},
		},
		{
			name: "self reference with a table-level primary key declared later",
			sql:  `CREATE TABLE s (parent int, id int, FOREIGN KEY (parent) REFERENCES s, PRIMARY KEY (id));`,
			want: []fk{{"public.s", "s_parent_fkey", []string{"id"}, &parser.FKReference{Table: "s", Schema: "public", Column: "id"}}},
		},
		{
			name: "ALTER TABLE ADD CONSTRAINT and ADD COLUMN",
			sql: `CREATE TABLE u (id int PRIMARY KEY); CREATE TABLE t (z int);
				ALTER TABLE t ADD CONSTRAINT tz FOREIGN KEY (z) REFERENCES u;
				ALTER TABLE t ADD COLUMN q int REFERENCES u;`,
			want: []fk{
				{"public.t", "t_q_fkey", []string{"id"}, &parser.FKReference{Table: "u", Schema: "public", Column: "id"}},
				{"public.t", "tz", []string{"id"}, &parser.FKReference{Table: "u", Schema: "public", Column: "id"}},
			},
		},
		{
			name: "one ALTER TABLE replaces the primary key after adding the foreign key",
			sql: `CREATE TABLE s (id int, parent int, CONSTRAINT pk PRIMARY KEY (id));
				ALTER TABLE s ADD FOREIGN KEY (id) REFERENCES s, DROP CONSTRAINT pk, ADD PRIMARY KEY (parent);`,
			want: []fk{{"public.s", "s_id_fkey", []string{"parent"}, &parser.FKReference{Table: "s", Schema: "public", Column: "parent"}}},
		},
		{
			name: "partition takes its partitioned table's primary key",
			sql: `CREATE TABLE ev (a int, b int, PRIMARY KEY (b, a)) PARTITION BY RANGE (a);
				CREATE TABLE ev1 PARTITION OF ev FOR VALUES FROM (0) TO (10) PARTITION BY RANGE (b);
				CREATE TABLE ev11 PARTITION OF ev1 FOR VALUES FROM (0) TO (5);
				CREATE TABLE t (x int, y int, FOREIGN KEY (x, y) REFERENCES ev11);`,
			want: []fk{{"public.t", "t_x_y_fkey", []string{"b", "a"}, nil}},
		},
		{
			name: "attached partition",
			sql: `CREATE TABLE ev (id int PRIMARY KEY) PARTITION BY RANGE (id); CREATE TABLE p1 (id int PRIMARY KEY);
				ALTER TABLE ev ATTACH PARTITION p1 FOR VALUES FROM (0) TO (10); CREATE TABLE t (x int REFERENCES p1);`,
			want: []fk{{"public.t", "t_x_fkey", []string{"id"}, &parser.FKReference{Table: "p1", Schema: "public", Column: "id"}}},
		},
		{
			name: "other skipped relation stays as written",
			sql:  `CREATE TABLE u (id int PRIMARY KEY); CREATE TABLE c AS SELECT * FROM u; CREATE TABLE t (x int REFERENCES c);`,
			want: []fk{{"public.t", "t_x_fkey", []string{}, &parser.FKReference{Table: "c", Schema: "public"}}},
		},
		{
			name: "view stays as written",
			sql:  `CREATE TABLE u (id int PRIMARY KEY); CREATE VIEW v AS SELECT * FROM u; CREATE TABLE t (x int REFERENCES v);`,
			want: []fk{{"public.t", "t_x_fkey", []string{}, &parser.FKReference{Table: "v", Schema: "public"}}},
		},
		{
			name:    "target with no primary key",
			sql:     `CREATE TABLE u (id int UNIQUE); CREATE TABLE t (uid int REFERENCES u);`,
			wantErr: "foreign key t_uid_fkey on public.t: there is no primary key for referenced table public.u",
		},
		{
			name:    "self reference with no primary key",
			sql:     `CREATE TABLE s (id int UNIQUE, parent int REFERENCES s);`,
			wantErr: "foreign key s_parent_fkey on public.s: there is no primary key for referenced table public.s",
		},
		{
			name:    "target not created yet",
			sql:     `CREATE TABLE t (wid int REFERENCES w); CREATE TABLE w (id int PRIMARY KEY);`,
			wantErr: "foreign key t_wid_fkey on public.t: referenced table public.w does not exist",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := postgres.New("")
			err := p.Parse("test.sql", []byte(tt.sql))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Parse() error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			var got []fk
			for _, tbl := range p.Schema().Tables {
				for _, c := range tbl.Constraints {
					if c.Type != parser.ForeignKey {
						continue
					}
					var ref *parser.FKReference
					if len(c.Columns) == 1 {
						ref = parser.ColumnByName(&tbl, c.Columns[0]).FKReference
					}
					got = append(got, fk{tbl.Schema + "." + tbl.Name, c.Name, c.ReferenceColumns, ref})
				}
			}
			slices.SortFunc(got, func(a, b fk) int { return strings.Compare(a.Name, b.Name) })
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("foreign keys mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDuplicateCreateTable(t *testing.T) {
	sql := `
		CREATE TABLE users (id integer PRIMARY KEY);
		CREATE TABLE users (id integer PRIMARY KEY, name text);
	`
	p := postgres.New("")
	err := p.Parse("test.sql", []byte(sql))
	if err == nil {
		t.Fatal("expected error for duplicate CREATE TABLE, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should mention 'already exists', got: %v", err)
	}
}

func TestDuplicateCreateEnum(t *testing.T) {
	sql := `
		CREATE TYPE status AS ENUM ('active', 'inactive');
		CREATE TYPE status AS ENUM ('active', 'inactive', 'suspended');
	`
	p := postgres.New("")
	err := p.Parse("test.sql", []byte(sql))
	if err == nil {
		t.Fatal("expected error for duplicate CREATE TYPE ENUM, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should mention 'already exists', got: %v", err)
	}
}

func TestDuplicateCreateCompositeType(t *testing.T) {
	sql := `
		CREATE TYPE address AS (street text, city text);
		CREATE TYPE address AS (street text, city text, zip text);
	`
	p := postgres.New("")
	err := p.Parse("test.sql", []byte(sql))
	if err == nil {
		t.Fatal("expected error for duplicate CREATE TYPE composite, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should mention 'already exists', got: %v", err)
	}
}

func TestDuplicateCreateDomain(t *testing.T) {
	sql := `
		CREATE DOMAIN email AS text CHECK (VALUE ~ '^.+@.+$');
		CREATE DOMAIN email AS text;
	`
	p := postgres.New("")
	err := p.Parse("test.sql", []byte(sql))
	if err == nil {
		t.Fatal("expected error for duplicate CREATE DOMAIN, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should mention 'already exists', got: %v", err)
	}
}

func TestDropTable(t *testing.T) {
	sql := `
		CREATE TABLE users (id integer PRIMARY KEY);
		CREATE TABLE products (id integer PRIMARY KEY);
		DROP TABLE products;
	`
	p := postgres.New("")
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
	sql := `
		CREATE TABLE users (id integer PRIMARY KEY);
		CREATE TABLE orders (
			id integer PRIMARY KEY,
			user_id integer REFERENCES users(id)
		);
		DROP TABLE users;
	`
	p := postgres.New("")
	err := p.Parse("test.sql", []byte(sql))
	if err == nil {
		t.Fatal("expected error when dropping table referenced by FK, got nil")
	}
}

func TestDropTableCrossFile(t *testing.T) {
	p := postgres.New("")
	if err := p.Parse("001.sql", []byte(`
		CREATE TABLE users (id integer PRIMARY KEY);
		CREATE TABLE orders (
			id integer PRIMARY KEY,
			user_id integer REFERENCES users(id)
		);
	`)); err != nil {
		t.Fatalf("Parse(001.sql) error: %v", err)
	}
	if err := p.Parse("002.sql", []byte(`DROP TABLE users;`)); err == nil {
		t.Fatal("expected error when dropping cross-file table referenced by FK, got nil")
	}
}

func TestDropEnum(t *testing.T) {
	sql := `
		CREATE TYPE status AS ENUM ('active', 'inactive');
		DROP TYPE status;
	`
	p := postgres.New("")
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	if len(p.Schema().Enums) != 0 {
		t.Errorf("expected 0 enums after DROP TYPE, got %d", len(p.Schema().Enums))
	}
}

func TestDropEnumWithColumnReference(t *testing.T) {
	sql := `
		CREATE TYPE status AS ENUM ('active', 'inactive');
		CREATE TABLE users (id integer PRIMARY KEY, status status);
		DROP TYPE status;
	`
	p := postgres.New("")
	err := p.Parse("test.sql", []byte(sql))
	if err == nil {
		t.Fatal("expected error when dropping type used by column, got nil")
	}
}

func TestDropDomain(t *testing.T) {
	sql := `
		CREATE DOMAIN email AS text CHECK (VALUE ~ '^.+@.+$');
		DROP DOMAIN email;
	`
	p := postgres.New("")
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	if len(p.Schema().DomainTypes) != 0 {
		t.Errorf("expected 0 domains after DROP DOMAIN, got %d", len(p.Schema().DomainTypes))
	}
}

func TestDropDomainWithColumnReference(t *testing.T) {
	sql := `
		CREATE DOMAIN email AS text CHECK (VALUE ~ '^.+@.+$');
		CREATE TABLE users (id integer PRIMARY KEY, email email);
		DROP DOMAIN email;
	`
	p := postgres.New("")
	err := p.Parse("test.sql", []byte(sql))
	if err == nil {
		t.Fatal("expected error when dropping domain used by column, got nil")
	}
}

// TestDropTypeReferences checks DROP TYPE and DROP DOMAIN against what
// PostgreSQL 17 does with the same DDL: a drop it refuses with 2BP01 is an
// error, and one it accepts removes the type.
func TestDropTypeReferences(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantErr string // empty: PostgreSQL accepts the drop
	}{
		{
			name:    "array column",
			sql:     `CREATE TYPE status_enum AS ENUM ('a'); CREATE TABLE t1 (s status_enum[]); DROP TYPE status_enum;`,
			wantErr: "parsing test.sql: drop type: cannot drop type public.status_enum: column public.t1.s uses it",
		},
		{
			name: "a same-named type in another schema",
			sql: `CREATE TYPE s1.st AS ENUM ('a'); CREATE TYPE s2.st AS ENUM ('a');
				CREATE TABLE s1.t (s s1.st); DROP TYPE s2.st;`,
		},
		{
			name:    "DROP DOMAIN naming a composite type",
			sql:     `CREATE DOMAIN d AS int; CREATE TYPE comp AS (x d); DROP DOMAIN d, comp;`,
			wantErr: "parsing test.sql: drop domain: public.comp is not a domain",
		},
		{
			name: "DROP TYPE drops a domain with its base type",
			sql:  `CREATE TYPE e AS ENUM ('a'); CREATE DOMAIN de AS e; DROP TYPE e, de; CREATE DOMAIN de AS int;`,
		},
		{
			name:    "domain base type",
			sql:     `CREATE TYPE e AS ENUM ('a'); CREATE DOMAIN de AS e; DROP TYPE e;`,
			wantErr: "parsing test.sql: drop type: cannot drop type public.e: domain public.de uses it",
		},
		{
			name:    "domain over a domain",
			sql:     `CREATE DOMAIN d1 AS int; CREATE DOMAIN d2 AS d1; DROP DOMAIN d1;`,
			wantErr: "parsing test.sql: drop domain: cannot drop type public.d1: domain public.d2 uses it",
		},
		{
			name:    "composite attribute",
			sql:     `CREATE TYPE e2 AS ENUM ('a'); CREATE TYPE comp AS (x e2); DROP TYPE e2;`,
			wantErr: "parsing test.sql: drop type: cannot drop type public.e2: attribute x of composite type public.comp uses it",
		},
		{
			name:    "composite attribute of an array type",
			sql:     `CREATE TYPE e3 AS ENUM ('a'); CREATE TYPE comp3 AS (x e3[]); DROP TYPE e3;`,
			wantErr: "parsing test.sql: drop type: cannot drop type public.e3: attribute x of composite type public.comp3 uses it",
		},
		{
			name:    "domain array column",
			sql:     `CREATE DOMAIN dd AS int; CREATE TABLE t5 (s dd[]); DROP DOMAIN dd;`,
			wantErr: "parsing test.sql: drop domain: cannot drop type public.dd: column public.t5.s uses it",
		},
		{
			name: "a composite type dropped with its attribute's type",
			sql:  `CREATE TYPE e4 AS ENUM ('a'); CREATE TYPE comp4 AS (x e4); DROP TYPE e4, comp4;`,
		},
		{
			name: "a domain dropped with its base domain",
			sql:  `CREATE DOMAIN d1 AS int; CREATE DOMAIN d2 AS d1; DROP DOMAIN d1, d2;`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := postgres.New("public")
			err := p.Parse("test.sql", []byte(tt.sql))
			got := ""
			if err != nil {
				got = err.Error()
			}
			if got != tt.wantErr {
				t.Fatalf("Parse() error = %q, want %q", got, tt.wantErr)
			}
		})
	}
}

func TestAlterTableIfExists(t *testing.T) {
	p := postgres.New("")
	if err := p.Parse("test.sql", []byte(`CREATE TABLE users (id integer PRIMARY KEY);`)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	// ALTER TABLE IF EXISTS on nonexistent table should silently succeed.
	if err := p.Parse("test2.sql", []byte(`ALTER TABLE IF EXISTS nonexistent ADD COLUMN name text;`)); err != nil {
		t.Fatalf("ALTER TABLE IF EXISTS should not error: %v", err)
	}

	// ALTER TABLE on nonexistent table without IF EXISTS should error.
	err := p.Parse("test3.sql", []byte(`ALTER TABLE nonexistent ADD COLUMN name text;`))
	if err == nil {
		t.Fatal("ALTER TABLE on nonexistent table should error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

func TestAddColumnIfNotExists(t *testing.T) {
	sql := `
		CREATE TABLE users (id integer PRIMARY KEY, name text);
		ALTER TABLE users ADD COLUMN IF NOT EXISTS name varchar(100);
	`
	p := postgres.New("")
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	table := p.Schema().Tables[0]
	if len(table.Columns) != 2 {
		t.Fatalf("expected 2 columns (IF NOT EXISTS should skip), got %d", len(table.Columns))
	}
	// Original type should be preserved.
	if table.Columns[1].Type != "text" {
		t.Errorf("name column type = %q, want %q (should not be overwritten)", table.Columns[1].Type, "text")
	}
}

func TestDropColumnIfExists(t *testing.T) {
	sql := `
		CREATE TABLE users (id integer PRIMARY KEY, name text);
		ALTER TABLE users DROP COLUMN IF EXISTS nonexistent;
	`
	p := postgres.New("")
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	table := p.Schema().Tables[0]
	if len(table.Columns) != 2 {
		t.Fatalf("expected 2 columns (IF EXISTS should skip missing), got %d", len(table.Columns))
	}
}

// TestAddColumnOntoExistingName pins ADD COLUMN of a name the table already
// has against PostgreSQL 17: `column "count" of relation "items" already
// exists`, while IF NOT EXISTS skips it and a DROP COLUMN anywhere in the same
// statement runs first.
func TestAddColumnOntoExistingName(t *testing.T) {
	const items = "CREATE TABLE items (id bigint PRIMARY KEY, count int);"
	tests := []struct {
		name    string
		sql     string
		want    []string
		wantErr string
	}{
		{
			name:    "a column the table has",
			sql:     items + "ALTER TABLE items ADD COLUMN count int;",
			wantErr: "add column count to public.items: column already exists",
		},
		{
			name:    "a column the same statement adds",
			sql:     items + "ALTER TABLE items ADD COLUMN x int, ADD COLUMN x int;",
			wantErr: "add column x to public.items: column already exists",
		},
		{
			name: "a quoted name in another case",
			sql:  items + `ALTER TABLE items ADD COLUMN "COUNT" text;`,
			want: []string{"id bigint", "count integer", "COUNT text"},
		},
		{
			name: "IF NOT EXISTS",
			sql:  items + "ALTER TABLE items ADD COLUMN IF NOT EXISTS count text;",
			want: []string{"id bigint", "count integer"},
		},
		{
			name: "a column the same statement drops later",
			sql:  items + "ALTER TABLE items ADD COLUMN count text, DROP COLUMN count;",
			want: []string{"id bigint", "count text"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := postgres.New("")
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
			for _, c := range p.Schema().Tables[0].Columns {
				got = append(got, c.Name+" "+c.Type)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("columns after ADD COLUMN mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDropConstraintIfExists(t *testing.T) {
	sql := `
		CREATE TABLE users (
			id integer,
			name text,
			CONSTRAINT pk_id PRIMARY KEY (id),
			CONSTRAINT uq_name UNIQUE (name)
		);
		ALTER TABLE users DROP CONSTRAINT IF EXISTS nonexistent;
	`
	p := postgres.New("")
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	table := p.Schema().Tables[0]
	if len(table.Constraints) != 2 { // PK + UNIQUE
		t.Errorf("expected 2 constraints (IF EXISTS should skip missing), got %d", len(table.Constraints))
	}
}

func TestRenameTable(t *testing.T) {
	sql := `
		CREATE TABLE users (id integer PRIMARY KEY, name text);
		ALTER TABLE users RENAME TO people;
	`
	p := postgres.New("")
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

// TestRenameOntoExistingName pins that a rename onto a name already taken is
// an error, as it is on PostgreSQL 16 (relation "b" already exists; column "b"
// of relation "t" already exists), the table's or column's own name included.
// A table in another schema does not take the name.
func TestRenameOntoExistingName(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		want    []string
		wantErr string
	}{
		{
			name:    "a table name another table has",
			sql:     `CREATE TABLE a (id int); CREATE TABLE b (id int); ALTER TABLE a RENAME TO b;`,
			wantErr: "table public.b already exists",
		},
		{
			name:    "the table's own name",
			sql:     `CREATE TABLE a (id int); ALTER TABLE a RENAME TO a;`,
			wantErr: "table public.a already exists",
		},
		{
			name: "a name a table in another schema has",
			sql:  `CREATE SCHEMA s2; CREATE TABLE b (id int); CREATE TABLE s2.a (id int); ALTER TABLE s2.a RENAME TO b;`,
			want: []string{"public.b(id)", "s2.b(id)"},
		},
		{
			name:    "a column name another column has",
			sql:     `CREATE TABLE t (a int, b int); ALTER TABLE t RENAME COLUMN a TO b;`,
			wantErr: "column b already exists in table t",
		},
		{
			name:    "the column's own name",
			sql:     `CREATE TABLE t (a int, b int); ALTER TABLE t RENAME COLUMN a TO a;`,
			wantErr: "column a already exists in table t",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := postgres.New("")
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
				got = append(got, tbl.Schema+"."+tbl.Name+"("+strings.Join(cols, ", ")+")")
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("tables after the rename mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRenameColumn(t *testing.T) {
	sql := `
		CREATE TABLE users (id integer PRIMARY KEY, name text);
		ALTER TABLE users RENAME COLUMN name TO full_name;
	`
	p := postgres.New("")
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
	sql := `
		CREATE TABLE users (id integer PRIMARY KEY);
		ALTER TABLE users RENAME COLUMN nonexistent TO something;
	`
	p := postgres.New("")
	err := p.Parse("test.sql", []byte(sql))
	if err == nil {
		t.Fatal("expected error renaming nonexistent column")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

// TestRenameColumnCarriesReferences pins that a column rename carries the
// constraints and foreign keys naming the column along, as PostgreSQL does
// (measured on 16: pg_get_constraintdef prints PRIMARY KEY (user_id) and
// REFERENCES users(user_id) after the rename). Left on the old name, a
// composite key no longer matches its columns.
func TestRenameColumnCarriesReferences(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want []string
	}{
		{
			name: "referenced primary key column",
			sql: `CREATE TABLE users (id uuid, boss_id uuid REFERENCES users(id), CONSTRAINT users_pk PRIMARY KEY (id));
				CREATE TABLE posts (id uuid PRIMARY KEY, user_id uuid REFERENCES users(id));
				CREATE TABLE comments (id uuid PRIMARY KEY, user_id uuid, FOREIGN KEY (user_id) REFERENCES users(id));
				ALTER TABLE users RENAME COLUMN id TO user_id;`,
			want: []string{
				"users.boss_id -> public.users(user_id)",
				"users FOREIGN KEY [boss_id] -> public.users[user_id]",
				"users PRIMARY KEY [user_id]",
				"posts.user_id -> public.users(user_id)",
				"posts PRIMARY KEY [id]",
				"posts FOREIGN KEY [user_id] -> public.users[user_id]",
				"comments.user_id -> public.users(user_id)",
				"comments PRIMARY KEY [id]",
				"comments FOREIGN KEY [user_id] -> public.users[user_id]",
			},
		},
		{
			name: "junction key column",
			sql: `CREATE TABLE posts (id uuid PRIMARY KEY);
				CREATE TABLE tags (id uuid PRIMARY KEY);
				CREATE TABLE post_tags (post_id uuid REFERENCES posts(id), tag_id uuid REFERENCES tags(id), PRIMARY KEY (post_id, tag_id));
				ALTER TABLE post_tags RENAME COLUMN post_id TO article_id;`,
			want: []string{
				"posts PRIMARY KEY [id]",
				"tags PRIMARY KEY [id]",
				"post_tags.article_id -> public.posts(id)",
				"post_tags.tag_id -> public.tags(id)",
				"post_tags FOREIGN KEY [article_id] -> public.posts[id]",
				"post_tags FOREIGN KEY [tag_id] -> public.tags[id]",
				"post_tags PRIMARY KEY [article_id tag_id]",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := postgres.New("")
			if err := p.Parse("test.sql", []byte(tt.sql)); err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			if diff := cmp.Diff(tt.want, columnReferences(p.Schema())); diff != "" {
				t.Errorf("references after RENAME COLUMN mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// columnReferences lists every column-naming reference in schema: each
// column's foreign key, then each table constraint's column list and target.
func columnReferences(s *parser.Schema) []string {
	var got []string
	for _, tbl := range s.Tables {
		for _, col := range tbl.Columns {
			if ref := col.FKReference; ref != nil {
				got = append(got, fmt.Sprintf("%s.%s -> %s.%s(%s)", tbl.Name, col.Name, ref.Schema, ref.Table, ref.Column))
			}
		}
		for _, c := range tbl.Constraints {
			s := fmt.Sprintf("%s %s %v", tbl.Name, c.Type, c.Columns)
			if c.Type == parser.ForeignKey {
				s += fmt.Sprintf(" -> %s.%s%v", c.ReferenceSchema, c.ReferenceTable, c.ReferenceColumns)
			}
			got = append(got, s)
		}
	}
	return got
}

// TestSetSchema pins ALTER TABLE … SET SCHEMA, which pg_query parses as an
// AlterObjectSchemaStmt: the table moves to the new schema, and the edge
// cases behave as PostgreSQL 16 does.
func TestSetSchema(t *testing.T) {
	tests := []struct {
		name       string
		sql        string
		wantTables []string
		wantErr    string
	}{
		{
			name: "moves the table",
			sql: `CREATE TABLE users (id uuid PRIMARY KEY);
				ALTER TABLE users SET SCHEMA auth;`,
			wantTables: []string{"auth.users"},
		},
		{
			name: "a schema-qualified table moves",
			sql: `CREATE TABLE billing.users (id uuid PRIMARY KEY);
				ALTER TABLE billing.users SET SCHEMA auth;`,
			wantTables: []string{"auth.users"},
		},
		{
			name: "moving into the current schema is a no-op",
			sql: `CREATE TABLE users (id uuid PRIMARY KEY);
				ALTER TABLE users SET SCHEMA public;`,
			wantTables: []string{"public.users"},
		},
		{
			name:       "IF EXISTS skips a missing table",
			sql:        `ALTER TABLE IF EXISTS users SET SCHEMA auth;`,
			wantTables: nil,
		},
		{
			name:    "a missing table is an error",
			sql:     `ALTER TABLE users SET SCHEMA auth;`,
			wantErr: "table public.users not found",
		},
		{
			name: "a table of the same name in the new schema is an error",
			sql: `CREATE TABLE users (id uuid PRIMARY KEY);
				CREATE TABLE auth.users (id uuid PRIMARY KEY);
				ALTER TABLE users SET SCHEMA auth;`,
			wantErr: "table auth.users already exists",
		},
		{
			name: "the moved table answers to its new name",
			sql: `CREATE TABLE users (id uuid PRIMARY KEY);
				ALTER TABLE users SET SCHEMA auth;
				ALTER TABLE auth.users ADD COLUMN email text;`,
			wantTables: []string{"auth.users"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := postgres.New("")
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
				got = append(got, tbl.Schema+"."+tbl.Name)
			}
			if diff := cmp.Diff(tt.wantTables, got); diff != "" {
				t.Errorf("tables after SET SCHEMA mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestStatementsApplyInFileOrder pins that the statements of one file
// apply one at a time, in order, as PostgreSQL 16 applies them. A table moved
// or renamed mid-file no longer answers to its old name, so a later reference
// to that name stays on it rather than following the table, and the vacated
// name can be created again. A statement that names a table before the file
// creates it fails, as it does in PostgreSQL.
func TestStatementsApplyInFileOrder(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		want    []string // "schema.table: col, col->schema.table" per table
		wantErr string
	}{
		{
			name: "a reference to a moved table's old name stays on the old name",
			sql: `CREATE TABLE users (id int PRIMARY KEY);
				ALTER TABLE users SET SCHEMA auth;
				CREATE TABLE posts (id int PRIMARY KEY, user_id int REFERENCES users(id));`,
			want: []string{"auth.users: id", "public.posts: id, user_id->public.users"},
		},
		{
			name: "a moved table's old name can be created again",
			sql: `CREATE TABLE users (id int PRIMARY KEY);
				ALTER TABLE users SET SCHEMA auth;
				CREATE TABLE users (id int PRIMARY KEY);`,
			want: []string{"auth.users: id", "public.users: id"},
		},
		{
			name: "a reference to a renamed table's old name stays on the old name",
			sql: `CREATE TABLE users (id int PRIMARY KEY);
				ALTER TABLE users RENAME TO accounts;
				CREATE TABLE posts (id int PRIMARY KEY, user_id int REFERENCES users(id));`,
			want: []string{"public.accounts: id", "public.posts: id, user_id->public.users"},
		},
		{
			name: "a renamed table's old name can be created again",
			sql: `CREATE TABLE users (id int PRIMARY KEY);
				ALTER TABLE users RENAME TO accounts;
				CREATE TABLE users (id int PRIMARY KEY);`,
			want: []string{"public.accounts: id", "public.users: id"},
		},
		{
			name: "a dropped table can be created again",
			sql: `CREATE TABLE users (id int PRIMARY KEY);
				DROP TABLE users;
				CREATE TABLE users (id int PRIMARY KEY, email text);`,
			want: []string{"public.users: id, email"},
		},
		{
			name: "a dropped type can be created again",
			sql: `CREATE TYPE mood AS ENUM ('sad');
				DROP TYPE mood;
				CREATE TYPE mood AS ENUM ('happy');
				CREATE TABLE users (id int PRIMARY KEY);`,
			want: []string{"public.users: id"},
		},
		{
			name: "a reference made before the move follows the table",
			sql: `CREATE TABLE users (id int PRIMARY KEY);
				CREATE TABLE posts (id int PRIMARY KEY, user_id int REFERENCES users(id));
				ALTER TABLE users SET SCHEMA auth;`,
			want: []string{"auth.users: id", "public.posts: id, user_id->auth.users"},
		},
		{
			name: "ALTER TABLE before CREATE TABLE is an error",
			sql: `ALTER TABLE users ADD COLUMN email text;
				CREATE TABLE users (id int PRIMARY KEY);`,
			wantErr: "table public.users not found",
		},
		{
			name: "COMMENT ON before CREATE TABLE is an error",
			sql: `COMMENT ON TABLE users IS 'people';
				CREATE TABLE users (id int PRIMARY KEY);`,
			wantErr: "comment on unknown table users",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := postgres.New("")
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
				cols := make([]string, 0, len(tbl.Columns))
				for _, c := range tbl.Columns {
					col := c.Name
					if c.FKReference != nil {
						col += "->" + c.FKReference.Schema + "." + c.FKReference.Table
					}
					cols = append(cols, col)
				}
				got = append(got, tbl.Schema+"."+tbl.Name+": "+strings.Join(cols, ", "))
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("tables mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAlterColumnSetNotNull(t *testing.T) {
	sql := `
		CREATE TABLE users (id integer PRIMARY KEY, name text);
		ALTER TABLE users ALTER COLUMN name SET NOT NULL;
	`
	p := postgres.New("")
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	table := p.Schema().Tables[0]
	for _, col := range table.Columns {
		if col.Name == "name" {
			if col.Nullable {
				t.Error("name column should not be nullable after SET NOT NULL")
			}
			return
		}
	}
	t.Error("name column not found")
}

func TestAlterColumnDropNotNull(t *testing.T) {
	sql := `
		CREATE TABLE users (id integer PRIMARY KEY, name text NOT NULL);
		ALTER TABLE users ALTER COLUMN name DROP NOT NULL;
	`
	p := postgres.New("")
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	table := p.Schema().Tables[0]
	for _, col := range table.Columns {
		if col.Name == "name" {
			if !col.Nullable {
				t.Error("name column should be nullable after DROP NOT NULL")
			}
			return
		}
	}
	t.Error("name column not found")
}

func TestAlterColumnSetDefault(t *testing.T) {
	sql := `
		CREATE TABLE users (id integer PRIMARY KEY, status text);
		ALTER TABLE users ALTER COLUMN status SET DEFAULT 'active';
	`
	p := postgres.New("")
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	table := p.Schema().Tables[0]
	for _, col := range table.Columns {
		if col.Name == "status" {
			if col.Default != "'active'" {
				t.Errorf("status default = %q, want %q", col.Default, "'active'")
			}
			return
		}
	}
	t.Error("status column not found")
}

func TestAlterColumnDropDefault(t *testing.T) {
	sql := `
		CREATE TABLE users (id integer PRIMARY KEY, status text DEFAULT 'active');
		ALTER TABLE users ALTER COLUMN status DROP DEFAULT;
	`
	p := postgres.New("")
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	table := p.Schema().Tables[0]
	for _, col := range table.Columns {
		if col.Name == "status" {
			if col.Default != "" {
				t.Errorf("status default = %q, want empty after DROP DEFAULT", col.Default)
			}
			return
		}
	}
	t.Error("status column not found")
}

func TestAlterColumnType(t *testing.T) {
	sql := `
		CREATE TABLE users (id integer PRIMARY KEY, name varchar(50));
		ALTER TABLE users ALTER COLUMN name TYPE text;
	`
	p := postgres.New("")
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	table := p.Schema().Tables[0]
	for _, col := range table.Columns {
		if col.Name == "name" {
			if col.Type != "text" {
				t.Errorf("name type = %q, want %q", col.Type, "text")
			}
			return
		}
	}
	t.Error("name column not found")
}

func TestMigrationSequence(t *testing.T) {
	// Simulates a realistic multi-step migration.
	p := postgres.New("")

	if err := p.Parse("001_init.sql", []byte(`
		CREATE TABLE users (
			id serial PRIMARY KEY,
			name text NOT NULL,
			email text
		);
	`)); err != nil {
		t.Fatalf("001: %v", err)
	}

	if err := p.Parse("002_add_status.sql", []byte(`
		ALTER TABLE users ADD COLUMN status text DEFAULT 'active';
		ALTER TABLE users ALTER COLUMN email SET NOT NULL;
	`)); err != nil {
		t.Fatalf("002: %v", err)
	}

	if err := p.Parse("003_rename.sql", []byte(`
		ALTER TABLE users RENAME COLUMN name TO full_name;
		ALTER TABLE users ALTER COLUMN status TYPE varchar(20);
	`)); err != nil {
		t.Fatalf("003: %v", err)
	}

	// Idempotent migration: IF NOT EXISTS / IF EXISTS
	if err := p.Parse("004_idempotent.sql", []byte(`
		ALTER TABLE users ADD COLUMN IF NOT EXISTS status text;
		ALTER TABLE users DROP COLUMN IF EXISTS nonexistent;
		ALTER TABLE IF EXISTS nonexistent ADD COLUMN x text;
	`)); err != nil {
		t.Fatalf("004: %v", err)
	}

	table := p.Schema().Tables[0]
	if table.Name != "users" {
		t.Errorf("table name = %q, want %q", table.Name, "users")
	}

	cols := make(map[string]parser.Column)
	for _, c := range table.Columns {
		cols[c.Name] = c
	}

	if len(cols) != 4 { // id, full_name, email, status
		t.Fatalf("expected 4 columns, got %d: %v", len(cols), table.Columns)
	}

	if cols["full_name"].Name == "" {
		t.Error("expected 'full_name' column (renamed from 'name')")
	}
	if cols["email"].Nullable {
		t.Error("email should be NOT NULL after SET NOT NULL")
	}
	if cols["status"].Type != "varchar(20)" {
		t.Errorf("status type = %q, want %q", cols["status"].Type, "varchar(20)")
	}
}

func TestParseViewSQL(t *testing.T) {
	sql := `-- @pk: id
-- @type avg_rating: float64

CREATE VIEW product_summary AS
SELECT
    p.id,
    p.name,
    p.discount,
    AVG(r.rating) AS avg_rating,
    COUNT(r.id) AS review_count
FROM products p
LEFT JOIN reviews r ON r.product_id = p.id
GROUP BY p.id, p.name, p.discount;`

	got, err := postgres.ParseViewSQL([]byte(sql), "")
	if err != nil {
		t.Fatalf("ParseViewSQL() error: %v", err)
	}

	if got.Name != "product_summary" {
		t.Errorf("Name = %q, want %q", got.Name, "product_summary")
	}

	if len(got.Columns) != 5 {
		t.Fatalf("columns = %d, want 5", len(got.Columns))
	}

	// Verify column extraction.
	wantCols := []struct {
		alias, sourceTable, sourceColumn, aggregate string
	}{
		{"id", "p", "id", ""},
		{"name", "p", "name", ""},
		{"discount", "p", "discount", ""},
		{"avg_rating", "r", "rating", "AVG"},
		{"review_count", "r", "id", "COUNT"},
	}
	for i, w := range wantCols {
		c := got.Columns[i]
		if c.Alias != w.alias {
			t.Errorf("col[%d].Alias = %q, want %q", i, c.Alias, w.alias)
		}
		if c.SourceTable != w.sourceTable {
			t.Errorf("col[%d].SourceTable = %q, want %q", i, c.SourceTable, w.sourceTable)
		}
		if c.SourceColumn != w.sourceColumn {
			t.Errorf("col[%d].SourceColumn = %q, want %q", i, c.SourceColumn, w.sourceColumn)
		}
		if c.Aggregate != w.aggregate {
			t.Errorf("col[%d].Aggregate = %q, want %q", i, c.Aggregate, w.aggregate)
		}
	}

	// Verify table aliases.
	if got.TableAliases["p"] != "products" {
		t.Errorf("alias p = %q, want %q", got.TableAliases["p"], "products")
	}
	if got.TableAliases["r"] != "reviews" {
		t.Errorf("alias r = %q, want %q", got.TableAliases["r"], "reviews")
	}
}

func TestParseViewSQL_CreateOrReplace(t *testing.T) {
	sql := `CREATE OR REPLACE VIEW reporting.monthly_stats AS
SELECT id, total FROM orders;`

	got, err := postgres.ParseViewSQL([]byte(sql), "")
	if err != nil {
		t.Fatalf("ParseViewSQL() error: %v", err)
	}

	if got.Name != "monthly_stats" {
		t.Errorf("Name = %q, want %q", got.Name, "monthly_stats")
	}
	if got.Schema != "reporting" {
		t.Errorf("Schema = %q, want %q", got.Schema, "reporting")
	}
}

func TestCreateViewSkippedGracefully(t *testing.T) {
	sql := `CREATE TABLE users (id serial PRIMARY KEY, name text);
CREATE VIEW active_users AS SELECT id, name FROM users WHERE id > 0;`

	p := postgres.New("")
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	schema := p.Schema()

	// Table should exist.
	if len(schema.Tables) != 1 {
		t.Fatalf("tables = %d, want 1", len(schema.Tables))
	}

	// No views should be created from DDL parsing.
	if len(schema.Views) != 0 {
		t.Errorf("views = %d, want 0", len(schema.Views))
	}

	// A warning should be emitted.
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
	sql := `CREATE TABLE users (id serial PRIMARY KEY);
DROP VIEW IF EXISTS old_view;`

	p := postgres.New("")
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	schema := p.Schema()
	if len(schema.Tables) != 1 {
		t.Fatalf("tables = %d, want 1", len(schema.Tables))
	}
}

func TestCreateIndex_MethodAndPartialWhere(t *testing.T) {
	sql := `CREATE TABLE users (
    id uuid PRIMARY KEY,
    email text NOT NULL,
    tags jsonb,
    deleted_at timestamptz
);
CREATE INDEX users_tags_gin ON users USING gin (tags);
CREATE UNIQUE INDEX users_active_email_idx ON users (email) WHERE deleted_at IS NULL;
CREATE INDEX users_email_idx ON users (email);`

	p := postgres.New("")
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

	gin, ok := byName["users_tags_gin"]
	if !ok {
		t.Fatalf("missing users_tags_gin; constraints=%+v", table.Constraints)
	}
	if gin.Type != parser.Index {
		t.Errorf("gin: Type=%v, want Index", gin.Type)
	}
	if gin.Method != "gin" {
		t.Errorf("gin: Method=%q, want %q", gin.Method, "gin")
	}
	if gin.Where != "" {
		t.Errorf("gin: Where=%q, want empty", gin.Where)
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

	// Plain non-unique CREATE INDEX with no USING defaults to btree.
	plain, ok := byName["users_email_idx"]
	if !ok {
		t.Fatalf("missing users_email_idx; constraints=%+v", table.Constraints)
	}
	if plain.Type != parser.Index || plain.Method != "btree" || plain.Where != "" {
		t.Errorf("plain: %+v, want Type=Index Method=btree Where=\"\"", plain)
	}

	// Partial UNIQUE must NOT promote the column to col.Unique = true: that
	// flag drives FindByX / upsert paths which can't safely use a partial
	// index without the predicate.
	for _, col := range table.Columns {
		if col.Name == "email" && col.Unique {
			t.Errorf("email column unexpectedly marked Unique=true from partial UNIQUE index")
		}
	}
}

// TestCreateIndex_ExpressionParts pins that an index with an expression part
// is skipped, unique or not, and that a column written as an expression is a
// plain column, as PostgreSQL 16 stores it.
func TestCreateIndex_ExpressionParts(t *testing.T) {
	tests := []struct {
		name   string
		index  string
		want   []parser.Constraint
		unique string // the one column marked Unique
	}{
		{name: "expression only", index: `CREATE INDEX ON e (lower(x))`},
		{name: "unique expression only", index: `CREATE UNIQUE INDEX ux ON e (lower(x))`},
		{name: "unique column and expression", index: `CREATE UNIQUE INDEX ux ON e (a, lower(x))`},
		{name: "column and expression", index: `CREATE INDEX ix ON e (a, (a + b))`},
		{name: "partial expression", index: `CREATE INDEX ix ON e (lower(x)) WHERE a > 0`},
		{
			name:  "parenthesized column",
			index: `CREATE INDEX px ON e (((a)), b)`,
			want:  []parser.Constraint{{Name: "px", Type: parser.Index, Columns: []string{"a", "b"}, Method: "btree"}},
		},
		{
			name:   "parenthesized collated column",
			index:  `CREATE UNIQUE INDEX ON e ((x COLLATE "C"))`,
			want:   []parser.Constraint{{Name: "e_x_idx", Type: parser.Unique, Columns: []string{"x"}, Method: "btree"}},
			unique: "x",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := postgres.New("")
			if err := p.Parse("test.sql", []byte(`CREATE TABLE e (x text, a int, b int); `+tt.index)); err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			table := p.Schema().Tables[0]
			if diff := cmp.Diff(tt.want, table.Constraints); diff != "" {
				t.Errorf("constraints mismatch (-want +got):\n%s", diff)
			}
			for _, col := range table.Columns {
				if col.Unique != (col.Name == tt.unique) {
					t.Errorf("column %s: Unique = %v, want %v", col.Name, col.Unique, !col.Unique)
				}
			}
		})
	}
}

// TestCreateIndex_SameTypeCast pins that an index part casting a column to its
// own type and typmod is a plain column, as PostgreSQL drops the cast, through
// nested casts and COLLATE, and that any other cast is an expression: a
// different typmod, a domain's base type, or a domain over the column's type.
func TestCreateIndex_SameTypeCast(t *testing.T) {
	const table = `CREATE DOMAIN dint AS int;
CREATE TYPE mood AS ENUM ('a', 'b');
CREATE TABLE r (x text, y int, w varchar(10), q varchar, d dint, e mood,
	n numeric(10,2), a int[], c char, b bigint, t timestamp(3));
`
	tests := []struct {
		name   string
		index  string
		want   []parser.Constraint
		unique string // the one column marked Unique
	}{
		{
			name:  "text to text",
			index: `CREATE INDEX ON r ((x::text))`,
			want:  []parser.Constraint{{Name: "r_x_idx", Type: parser.Index, Columns: []string{"x"}, Method: "btree"}},
		},
		{
			name:   "unique int to integer",
			index:  `CREATE UNIQUE INDEX ON r ((y::integer))`,
			want:   []parser.Constraint{{Name: "r_y_idx", Type: parser.Unique, Columns: []string{"y"}, Method: "btree"}},
			unique: "y",
		},
		{
			name:  "int to int4",
			index: `CREATE INDEX iy ON r ((y::int4))`,
			want:  []parser.Constraint{{Name: "iy", Type: parser.Index, Columns: []string{"y"}, Method: "btree"}},
		},
		{
			name:  "nested same-type casts",
			index: `CREATE INDEX iy ON r (((y::int)::int))`,
			want:  []parser.Constraint{{Name: "iy", Type: parser.Index, Columns: []string{"y"}, Method: "btree"}},
		},
		{
			name:  "same typmod",
			index: `CREATE INDEX iw ON r ((w::varchar(10)), (n::numeric(10,2)), (t::timestamp(3)), (c::char))`,
			want:  []parser.Constraint{{Name: "iw", Type: parser.Index, Columns: []string{"w", "n", "t", "c"}, Method: "btree"}},
		},
		{
			name:  "schema-qualified type",
			index: `CREATE INDEX ix ON r ((x::pg_catalog.text))`,
			want:  []parser.Constraint{{Name: "ix", Type: parser.Index, Columns: []string{"x"}, Method: "btree"}},
		},
		{
			name:  "array, enum and domain to their own type",
			index: `CREATE INDEX ia ON r ((a::int[]), (e::mood), (d::dint))`,
			want:  []parser.Constraint{{Name: "ia", Type: parser.Index, Columns: []string{"a", "e", "d"}, Method: "btree"}},
		},
		{
			name:  "collate inside, outside and between casts",
			index: `CREATE INDEX ix ON r (((x COLLATE "C")::text), (x::text COLLATE "C"), (((x::text) COLLATE "C")::text))`,
			want:  []parser.Constraint{{Name: "ix", Type: parser.Index, Columns: []string{"x", "x", "x"}, Method: "btree"}},
		},
		{
			name:  "cast and plain column",
			index: `CREATE INDEX ON r ((x::text), y)`,
			want:  []parser.Constraint{{Name: "r_x_y_idx", Type: parser.Index, Columns: []string{"x", "y"}, Method: "btree"}},
		},
		{name: "varchar(10) to varchar", index: `CREATE INDEX ON r ((w::varchar))`},
		{name: "varchar to text", index: `CREATE INDEX ON r ((q::text))`},
		{name: "numeric(10,2) to numeric", index: `CREATE INDEX ON r ((n::numeric))`},
		{name: "timestamp(3) to timestamp", index: `CREATE INDEX ON r ((t::timestamp))`},
		{name: "char to bpchar", index: `CREATE INDEX ON r ((c::bpchar))`},
		{name: "domain to its base", index: `CREATE INDEX ON r ((d::int))`},
		{name: "int to a domain over int", index: `CREATE INDEX ON r ((y::dint))`},
		{name: "cast away and back", index: `CREATE INDEX ON r (((b::int)::bigint))`},
		{name: "one cast part of two", index: `CREATE UNIQUE INDEX ON r ((x::text), (y::bigint))`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := postgres.New("")
			if err := p.Parse("test.sql", []byte(table+tt.index)); err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			got := p.Schema().Tables[0]
			if diff := cmp.Diff(tt.want, got.Constraints); diff != "" {
				t.Errorf("constraints mismatch (-want +got):\n%s", diff)
			}
			for _, col := range got.Columns {
				if col.Unique != (col.Name == tt.unique) {
					t.Errorf("column %s: Unique = %v, want %v", col.Name, col.Unique, !col.Unique)
				}
			}
		})
	}
}

// TestDropConstraintRecomputesColumnUnique pins that dropping a UNIQUE
// constraint re-derives col.Unique from the unconditional UNIQUEs that
// remain, an inline one included, rather than unconditionally clearing it.
func TestDropConstraintRecomputesColumnUnique(t *testing.T) {
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
					id integer PRIMARY KEY,
					email text UNIQUE
				);
				ALTER TABLE users ADD CONSTRAINT users_email_unique UNIQUE (email);
			`,
			dropSQL:     `ALTER TABLE users DROP CONSTRAINT users_email_unique;`,
			column:      "email",
			wantUnique:  true,
			wantComment: "inline UNIQUE remains in force",
		},
		{
			name: "inline UNIQUE dropped by its derived name clears Unique",
			setupSQL: `
				CREATE TABLE users (
					id integer PRIMARY KEY,
					email text UNIQUE
				);
			`,
			dropSQL:     `ALTER TABLE users DROP CONSTRAINT users_email_key;`,
			column:      "email",
			wantUnique:  false,
			wantComment: "the inline UNIQUE is the constraint users_email_key",
		},
		{
			name: "full UNIQUE plus partial UNIQUE — drop partial keeps Unique",
			setupSQL: `
				CREATE TABLE users (
					id integer PRIMARY KEY,
					email text NOT NULL,
					deleted_at timestamp
				);
				CREATE UNIQUE INDEX users_email_uq ON users (email);
				CREATE UNIQUE INDEX users_email_active_uq ON users (email) WHERE deleted_at IS NULL;
			`,
			dropSQL:     `DROP INDEX users_email_active_uq;`,
			column:      "email",
			wantUnique:  true,
			wantComment: "full UNIQUE still covers the column",
		},
		{
			name: "single table-level UNIQUE — drop it clears Unique",
			setupSQL: `
				CREATE TABLE users (
					id integer PRIMARY KEY,
					email text NOT NULL
				);
				ALTER TABLE users ADD CONSTRAINT users_email_unique UNIQUE (email);
			`,
			dropSQL:     `ALTER TABLE users DROP CONSTRAINT users_email_unique;`,
			column:      "email",
			wantUnique:  false,
			wantComment: "no remaining UNIQUE and no inline UNIQUE",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := postgres.New("")
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

func TestParseViewSQL_Materialized(t *testing.T) {
	tests := []struct {
		name       string
		sql        string
		wantName   string
		wantSchema string
	}{
		{
			name: "bare name",
			sql: `-- @pk: customer_id

CREATE MATERIALIZED VIEW order_totals AS
SELECT o.customer_id, SUM(o.amount) AS total
FROM orders o
GROUP BY o.customer_id;`,
			wantName:   "order_totals",
			wantSchema: "public",
		},
		{
			name: "schema-qualified",
			sql: `CREATE MATERIALIZED VIEW reporting.order_totals AS
SELECT o.customer_id, SUM(o.amount) AS total
FROM orders o
GROUP BY o.customer_id;`,
			wantName:   "order_totals",
			wantSchema: "reporting",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := postgres.ParseViewSQL([]byte(tt.sql), "")
			if err != nil {
				t.Fatalf("ParseViewSQL() error: %v", err)
			}

			if !got.Materialized {
				t.Error("Materialized = false, want true")
			}
			if got.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", got.Name, tt.wantName)
			}
			if got.Schema != tt.wantSchema {
				t.Errorf("Schema = %q, want %q", got.Schema, tt.wantSchema)
			}

			if len(got.Columns) != 2 {
				t.Fatalf("columns = %d, want 2", len(got.Columns))
			}
			if got.Columns[0].Alias != "customer_id" || got.Columns[0].SourceColumn != "customer_id" {
				t.Errorf("col[0] = %+v, want customer_id from o", got.Columns[0])
			}
			if got.Columns[1].Alias != "total" || got.Columns[1].Aggregate != "SUM" {
				t.Errorf("col[1] = %+v, want total SUM aggregate", got.Columns[1])
			}
			if got.TableAliases["o"] != "orders" {
				t.Errorf("alias o = %q, want %q", got.TableAliases["o"], "orders")
			}
		})
	}
}

// TestParseViewSQL_DefaultSchema pins the default-schema rule for views: an
// unqualified view name resolves to input.schema ("public" when unset), the
// rule an unqualified CREATE TABLE follows, so a view's hook.TableName value
// and SQL are schema-qualified like a table's (PRD §5.5).
func TestParseViewSQL_DefaultSchema(t *testing.T) {
	tests := []struct {
		name          string
		sql           string
		defaultSchema string
		wantSchema    string
	}{
		{
			name:       "unqualified view, no default",
			sql:        `CREATE VIEW totals AS SELECT id FROM orders;`,
			wantSchema: "public",
		},
		{
			name:          "unqualified view, input.schema set",
			sql:           `CREATE VIEW totals AS SELECT id FROM orders;`,
			defaultSchema: "billing",
			wantSchema:    "billing",
		},
		{
			name:          "qualified view keeps its schema",
			sql:           `CREATE VIEW reporting.totals AS SELECT id FROM orders;`,
			defaultSchema: "billing",
			wantSchema:    "reporting",
		},
		{
			name:       "unqualified materialized view, no default",
			sql:        `CREATE MATERIALIZED VIEW totals AS SELECT id FROM orders;`,
			wantSchema: "public",
		},
		{
			name:          "unqualified materialized view, input.schema set",
			sql:           `CREATE MATERIALIZED VIEW totals AS SELECT id FROM orders;`,
			defaultSchema: "billing",
			wantSchema:    "billing",
		},
		{
			name:          "qualified materialized view keeps its schema",
			sql:           `CREATE MATERIALIZED VIEW reporting.totals AS SELECT id FROM orders;`,
			defaultSchema: "billing",
			wantSchema:    "reporting",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := postgres.ParseViewSQL([]byte(tt.sql), tt.defaultSchema)
			if err != nil {
				t.Fatalf("ParseViewSQL() error: %v", err)
			}
			if got.Schema != tt.wantSchema {
				t.Errorf("Schema = %q, want %q", got.Schema, tt.wantSchema)
			}
		})
	}
}

// A FROM-clause table keeps its schema, keyed like TableAliases. An
// unqualified one is in the default schema, as an unqualified CREATE TABLE is.
func TestParseViewSQL_TableSchemas(t *testing.T) {
	tests := []struct {
		name          string
		sql           string
		defaultSchema string
		want          map[string]string
	}{
		{
			name: "qualified, aliased and joined",
			sql: `CREATE VIEW billing.v AS SELECT a.id, i.total
FROM app.accounts a JOIN billing.invoices i ON i.account_id = a.id;`,
			want: map[string]string{"a": "app", "i": "billing"},
		},
		{
			name:          "unqualified table takes the default schema",
			sql:           `CREATE MATERIALIZED VIEW reporting.v AS SELECT orders.id FROM orders;`,
			defaultSchema: "billing",
			want:          map[string]string{"orders": "billing"},
		},
		{
			name: "unqualified table with no default is in public",
			sql:  `CREATE VIEW v AS SELECT o.id FROM orders o;`,
			want: map[string]string{"o": "public"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := postgres.ParseViewSQL([]byte(tt.sql), tt.defaultSchema)
			if err != nil {
				t.Fatalf("ParseViewSQL() error: %v", err)
			}
			if diff := cmp.Diff(tt.want, got.TableSchemas); diff != "" {
				t.Errorf("TableSchemas mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestParseViewSQL_RegularViewNotMaterialized(t *testing.T) {
	sql := `CREATE VIEW plain AS SELECT id FROM users;`

	got, err := postgres.ParseViewSQL([]byte(sql), "")
	if err != nil {
		t.Fatalf("ParseViewSQL() error: %v", err)
	}
	if got.Materialized {
		t.Error("Materialized = true for a regular CREATE VIEW, want false")
	}
}

func TestParseViewSQL_OrReplaceMaterializedRejected(t *testing.T) {
	sql := `CREATE OR REPLACE MATERIALIZED VIEW order_totals AS
SELECT customer_id FROM orders;`

	_, err := postgres.ParseViewSQL([]byte(sql), "")
	if err == nil {
		t.Fatal("ParseViewSQL() error = nil, want parse error (OR REPLACE is not valid for materialized views)")
	}
}

func TestParseViewSQL_CreateTableAsNotAView(t *testing.T) {
	sql := `CREATE TABLE order_copy AS SELECT * FROM orders;`

	_, err := postgres.ParseViewSQL([]byte(sql), "")
	if err == nil {
		t.Fatal("ParseViewSQL() error = nil, want 'no CREATE VIEW statement found' for CREATE TABLE AS")
	}
}

func TestCreateMaterializedViewSkippedGracefully(t *testing.T) {
	sql := `CREATE TABLE users (id serial PRIMARY KEY, name text);
CREATE MATERIALIZED VIEW active_users AS SELECT id, name FROM users;`

	p := postgres.New("")
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	schema := p.Schema()
	if len(schema.Tables) != 1 {
		t.Fatalf("tables = %d, want 1", len(schema.Tables))
	}
	if len(schema.Views) != 0 {
		t.Errorf("views = %d, want 0 (matview in DDL paths must be skipped)", len(schema.Views))
	}

	found := false
	for _, w := range schema.Warnings {
		if strings.Contains(w, "CREATE MATERIALIZED VIEW active_users") && strings.Contains(w, "skipped") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected warning mentioning CREATE MATERIALIZED VIEW active_users, got %v", schema.Warnings)
	}
}

// TestDerivedConstraintNames pins the names the parser gives unnamed
// constraints and indexes. Each was measured on PostgreSQL 16.
func TestDerivedConstraintNames(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want []string
	}{
		{
			name: "checks name after their one column",
			sql:  `CREATE TABLE t (a int CHECK (a > 0), b int CHECK (a > b), c int CHECK (length(c::text) > 0), CHECK (1 = 1));`,
			want: []string{"t CHECK t_a_check", "t CHECK t_check", "t CHECK t_c_check", "t CHECK t_check1"},
		},
		{
			name: "foreign keys on one column take a suffix",
			sql: `CREATE TABLE p (id int PRIMARY KEY);
				CREATE TABLE f (a int REFERENCES p (id), CONSTRAINT x FOREIGN KEY (a) REFERENCES p (id), FOREIGN KEY (a) REFERENCES p (id));`,
			want: []string{"p PRIMARY KEY p_pkey", "f FOREIGN KEY f_a_fkey", "f FOREIGN KEY x", "f FOREIGN KEY f_a_fkey1"},
		},
		{
			name: "an index name clashes with a table",
			sql: `CREATE TABLE z_a_key (id int);
				CREATE TABLE z (a int UNIQUE);`,
			want: []string{"z UNIQUE z_a_key1"},
		},
		{
			name: "index columns from expressions and repeats",
			sql: `CREATE TABLE e (x text, a int, b int, lower int, expr int);
				CREATE INDEX ON e (lower(x));
				CREATE INDEX ON e ((a + b));
				CREATE INDEX ON e (a, a);
				CREATE INDEX ON e ((x::varchar));
				CREATE INDEX ON e (a) INCLUDE (b);
				CREATE UNIQUE INDEX ON e (b);
				CREATE INDEX ON e (lower);
				CREATE INDEX ON e (expr);
				CREATE INDEX ON e (x);`,
			// The skipped expression indexes still take e_lower_idx,
			// e_expr_idx and e_x_idx.
			want: []string{
				"e INDEX e_a_a1_idx", "e INDEX e_a_b_idx", "e UNIQUE e_b_idx",
				"e INDEX e_lower_idx1", "e INDEX e_expr_idx1", "e INDEX e_x_idx1",
			},
		},
		{
			name: "a skipped expression index's name goes with its table",
			sql: `CREATE TABLE d (x text, lower int);
				CREATE INDEX ON d (lower(x));
				DROP TABLE d;
				CREATE TABLE d (x text, lower int);
				CREATE INDEX ON d (lower);
				CREATE TABLE m (x text, lower int);
				CREATE INDEX ON m (lower(x));
				ALTER TABLE m SET SCHEMA s;
				CREATE TABLE m (x text, lower int);
				CREATE INDEX ON m (lower);
				CREATE TABLE r (x text, lower int);
				CREATE INDEX ON r (lower(x));
				ALTER TABLE r RENAME TO r2;
				CREATE TABLE r (x text, lower int);
				CREATE INDEX ON r (lower);
				CREATE TABLE q (x text);
				CREATE INDEX ON q (lower(x));
				ALTER TABLE q RENAME TO q2;
				DROP TABLE q2;
				CREATE TABLE q (x text, lower int);
				CREATE INDEX ON q (lower);`,
			want: []string{"d INDEX d_lower_idx", "m INDEX m_lower_idx", "r INDEX r_lower_idx1", "q INDEX q_lower_idx"},
		},
		{
			name: "dropping a column drops the expression and partial indexes that read it",
			sql: `CREATE TABLE e (a int, x text, b int);
				CREATE INDEX ON e (a, lower(x));
				CREATE INDEX ON e (b) WHERE a > 0;
				ALTER TABLE e DROP COLUMN a;
				ALTER TABLE e ADD COLUMN a int, ADD COLUMN lower int;
				CREATE INDEX ON e (a, lower);
				CREATE TABLE i (a int, x text, c int);
				CREATE INDEX ON i (lower(x)) INCLUDE (c);
				ALTER TABLE i DROP COLUMN c;
				ALTER TABLE i ADD COLUMN c int, ADD COLUMN lower int;
				CREATE INDEX ON i (lower) INCLUDE (c);`,
			want: []string{"e INDEX e_a_lower_idx", "i INDEX i_lower_c_idx"},
		},
		{
			name: "an expression index follows a column rename",
			sql: `CREATE TABLE r (a int, x text);
				CREATE INDEX ON r (a, lower(x));
				ALTER TABLE r RENAME COLUMN x TO y;
				ALTER TABLE r DROP COLUMN y;
				ALTER TABLE r ADD COLUMN lower int;
				CREATE INDEX ON r (a, lower);
				CREATE TABLE k (a int, x text);
				CREATE INDEX ON k (a, lower(x));
				ALTER TABLE k RENAME COLUMN x TO y;
				ALTER TABLE k ADD COLUMN lower int;
				CREATE INDEX ON k (a, lower);`,
			want: []string{"r INDEX r_a_lower_idx", "k INDEX k_a_lower_idx1"},
		},
		{
			name: "a view takes an index name until it is dropped",
			sql: `CREATE TABLE v (a int, b int, c int);
				CREATE VIEW v_a_idx AS SELECT 1 AS x;
				CREATE MATERIALIZED VIEW v_b_idx AS SELECT 1 AS x;
				CREATE VIEW v_c_idx AS SELECT 1 AS x;
				DROP VIEW v_c_idx;
				CREATE INDEX ON v (a);
				CREATE INDEX ON v (b);
				CREATE INDEX ON v (c);`,
			want: []string{"v INDEX v_a_idx1", "v INDEX v_b_idx1", "v INDEX v_c_idx"},
		},
		{
			name: "a partition takes an index name until it is dropped",
			sql: `CREATE TABLE p (a int) PARTITION BY RANGE (a);
				CREATE TABLE n_a_idx PARTITION OF p FOR VALUES FROM (0) TO (10);
				CREATE TABLE n_b_idx PARTITION OF p FOR VALUES FROM (10) TO (20);
				DROP TABLE n_b_idx;
				CREATE TABLE n (a int, b int);
				CREATE INDEX ON n (a);
				CREATE INDEX ON n (b);`,
			want: []string{"n INDEX n_a_idx1", "n INDEX n_b_idx"},
		},
		{
			name: "a renamed or moved view and a dropped table's partitions free their names",
			sql: `CREATE TABLE w (a int, b int, c int, d int);
				CREATE VIEW w_a_idx AS SELECT 1 AS x;
				ALTER VIEW w_a_idx RENAME TO w_d_idx;
				CREATE MATERIALIZED VIEW w_b_idx AS SELECT 1 AS x;
				ALTER MATERIALIZED VIEW w_b_idx SET SCHEMA s;
				CREATE TABLE pp (a int) PARTITION BY RANGE (a);
				CREATE TABLE w_c_idx PARTITION OF pp FOR VALUES FROM (0) TO (10);
				DROP TABLE pp;
				CREATE INDEX ON w (a);
				CREATE INDEX ON w (b);
				CREATE INDEX ON w (c);
				CREATE INDEX ON w (d);`,
			want: []string{"w INDEX w_a_idx", "w INDEX w_b_idx", "w INDEX w_c_idx", "w INDEX w_d_idx1"},
		},
		{
			name: "a CREATE TABLE AS or SELECT INTO table takes an index name until it is dropped",
			sql: `CREATE TABLE n_a_idx AS SELECT 1 AS x;
				SELECT 1 AS x INTO n_b_idx;
				CREATE TABLE n_c_idx AS SELECT 1 AS x;
				DROP TABLE n_c_idx;
				CREATE TABLE n (a int, b int, c int);
				CREATE INDEX ON n (a);
				CREATE INDEX ON n (b);
				CREATE INDEX ON n (c);`,
			want: []string{"n INDEX n_a_idx1", "n INDEX n_b_idx1", "n INDEX n_c_idx"},
		},
		{
			name: "a merged primary key takes a unique's name",
			sql: `CREATE TABLE m (id int UNIQUE, CONSTRAINT pk PRIMARY KEY (id), CONSTRAINT u2 UNIQUE (id));
				CREATE TABLE m2 (CONSTRAINT u1 UNIQUE (id), id int PRIMARY KEY);`,
			want: []string{"m PRIMARY KEY pk", "m2 PRIMARY KEY u1"},
		},
		{
			name: "constraints added by ALTER TABLE",
			sql: `CREATE TABLE p (id int PRIMARY KEY);
				CREATE TABLE pk2_pkey (id int);
				CREATE TABLE pk2 (id int);
				ALTER TABLE pk2 ADD PRIMARY KEY (id);
				CREATE TABLE ck (a int);
				ALTER TABLE ck ADD CHECK (a > 0), ADD CHECK (a < 10);
				ALTER TABLE ck ADD COLUMN b int UNIQUE CHECK (b > 0) REFERENCES p (id);
				CREATE TABLE c (email text UNIQUE);
				ALTER TABLE c ADD UNIQUE (email);`,
			want: []string{
				"p PRIMARY KEY p_pkey", "pk2 PRIMARY KEY pk2_pkey1",
				"ck CHECK ck_a_check", "ck CHECK ck_a_check1", "ck UNIQUE ck_b_key", "ck FOREIGN KEY ck_b_fkey", "ck CHECK ck_b_check",
				"c UNIQUE c_email_key", "c UNIQUE c_email_key1",
			},
		},
		{
			name: "a name stays across renames",
			sql: `CREATE TABLE r (v int UNIQUE);
				ALTER TABLE r RENAME TO r2;
				ALTER TABLE r2 RENAME COLUMN v TO w;`,
			want: []string{"r2 UNIQUE r_v_key"},
		},
		{
			name: "dropping a column drops its constraints",
			sql: `CREATE TABLE dc (a int UNIQUE, b int UNIQUE, CHECK (b > a));
				ALTER TABLE dc DROP COLUMN a;`,
			want: []string{"dc UNIQUE dc_b_key"},
		},
		{
			name: "dropping a key column drops the composite primary key",
			sql: `CREATE TABLE cp (a int, b int, PRIMARY KEY (a, b));
				ALTER TABLE cp DROP COLUMN a;
				ALTER TABLE cp ADD COLUMN c int;`,
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := postgres.New("")
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
			for _, tbl := range p.Schema().Tables {
				for _, col := range tbl.Columns {
					if col.PrimaryKey && !slices.ContainsFunc(tbl.Constraints, func(c parser.Constraint) bool { return c.Type == parser.PrimaryKey }) {
						t.Errorf("%s.%s is PrimaryKey with no PRIMARY KEY constraint", tbl.Name, col.Name)
					}
				}
			}
		})
	}
}

// TestSeedKeepsReservedIndexNames covers `input.source: both`: an introspected
// expression index and view hold their names against an index the DDL files
// create, and dropping a column the expression index reads frees its name.
func TestSeedKeepsReservedIndexNames(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want string
	}{
		{name: "expression index and view hold names", sql: `CREATE INDEX ON n (a);`, want: "n_a_idx2"},
		{name: "dropped column", sql: `ALTER TABLE n DROP COLUMN x; CREATE INDEX ON n (a);`, want: "n_a_idx"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := postgres.New("")
			p.Seed(&parser.Schema{
				Tables: []parser.Table{{
					Name:              "n",
					Schema:            "public",
					Columns:           []parser.Column{{Name: "a", Type: "integer", Nullable: true}, {Name: "x", Type: "text", Nullable: true}},
					ExpressionIndexes: []parser.ExpressionIndex{{Name: "n_a_idx", Columns: []string{"x"}}},
				}},
				Views: []parser.View{{Name: "n_a_idx1", Schema: "public"}},
			})
			if err := p.Parse("test.sql", []byte(tt.sql)); err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			if got := p.Schema().Tables[0].Constraints; len(got) != 1 || got[0].Name != tt.want {
				t.Errorf("constraints = %+v, want one index named %s", got, tt.want)
			}
		})
	}
}

// TestDropConstraintByDerivedName pins that DROP CONSTRAINT finds a key
// declared without a name, or declared inline, under the name the parser
// gave it.
func TestDropConstraintByDerivedName(t *testing.T) {
	tests := []struct {
		name string
		sql  string
	}{
		{name: "inline primary key", sql: `CREATE TABLE users (id int PRIMARY KEY); ALTER TABLE users DROP CONSTRAINT users_pkey;`},
		{name: "unnamed table-level primary key", sql: `CREATE TABLE users (id int, PRIMARY KEY (id)); ALTER TABLE users DROP CONSTRAINT users_pkey;`},
		{name: "named inline primary key", sql: `CREATE TABLE users (id int CONSTRAINT pk_u PRIMARY KEY); ALTER TABLE users DROP CONSTRAINT pk_u;`},
		{name: "named inline unique", sql: `CREATE TABLE users (email text CONSTRAINT c_mail_named UNIQUE); ALTER TABLE users DROP CONSTRAINT c_mail_named;`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := postgres.New("")
			if err := p.Parse("test.sql", []byte(tt.sql)); err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			table := p.Schema().Tables[0]
			if len(table.Constraints) != 0 {
				t.Errorf("Constraints = %+v, want none", table.Constraints)
			}
			if col := table.Columns[0]; col.PrimaryKey || col.Unique {
				t.Errorf("column %s: PrimaryKey = %v, Unique = %v, want both false", col.Name, col.PrimaryKey, col.Unique)
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
		{name: "full join", sql: `CREATE VIEW v AS SELECT a.id FROM a FULL JOIN b ON b.a_id = a.id;`, want: map[string]bool{"a": true, "b": true}},
		{name: "join nested under the outer side", sql: `CREATE VIEW v AS SELECT a.id FROM a LEFT JOIN (b JOIN c ON c.b_id = b.id) ON b.a_id = a.id;`, want: map[string]bool{"b": true, "c": true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := postgres.ParseViewSQL([]byte(tt.sql), "")
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
	tests := []struct{ sql, schema string }{
		{"CREATE VIEW v AS SELECT id FROM a UNION SELECT id FROM b;", "public"},
		{"CREATE VIEW v AS SELECT id FROM a UNION ALL SELECT id FROM b;", "public"},
		{"CREATE VIEW v AS SELECT id FROM a EXCEPT SELECT id FROM b;", "public"},
		{"CREATE VIEW app.v AS SELECT id FROM a INTERSECT SELECT id FROM b;", "app"},
		{"CREATE MATERIALIZED VIEW v AS SELECT id FROM a UNION SELECT id FROM b;", "public"},
	}
	for _, tt := range tests {
		_, err := postgres.ParseViewSQL([]byte(tt.sql), "")
		if err == nil || !strings.Contains(err.Error(), "not a simple SELECT") {
			t.Errorf("ParseViewSQL(%q) error = %v, want not a simple SELECT", tt.sql, err)
		}
		// The error names the view, so an annotation file for a view the
		// database defines can still overlay it.
		qe, ok := errors.AsType[*parser.ViewQueryError](err)
		if !ok || qe.Schema != tt.schema || qe.Name != "v" {
			t.Errorf("ParseViewSQL(%q) error = %#v, want a ViewQueryError naming %s.v", tt.sql, err, tt.schema)
		}
	}
}

// TestParseViewSQL_ColumnNames checks that every view column has a name (PRD
// §16.1): an unaliased expression with no column name, * and t.* are
// ViewQueryErrors, and a column list names the columns in order. PostgreSQL
// lets the list name fewer columns than the SELECT has.
func TestParseViewSQL_ColumnNames(t *testing.T) {
	tests := []struct {
		name, sql string
		want      []string
		wantErr   string
	}{
		{name: "unaliased expression", sql: "CREATE VIEW v AS SELECT id, a + b FROM t", wantErr: "view v selects a + b with no alias and no column name"},
		{name: "unaliased aggregate", sql: "CREATE VIEW v AS SELECT id, COUNT(*) FROM t GROUP BY id", wantErr: "view v selects count(*) with no alias"},
		{name: "unaliased matview expression", sql: "CREATE MATERIALIZED VIEW v AS SELECT id, lower(name) FROM t", wantErr: "view v selects lower(name) with no alias"},
		{name: "star", sql: "CREATE VIEW v AS SELECT * FROM t", wantErr: "view v selects *, whose columns the view parser cannot list"},
		{name: "table star", sql: "CREATE VIEW v AS SELECT id, t.* FROM t", wantErr: "view v selects t.*, whose columns"},
		{name: "column list", sql: "CREATE VIEW v (x, total) AS SELECT id, a + b FROM t", want: []string{"x", "total"}},
		{name: "short column list", sql: "CREATE VIEW v (x) AS SELECT id, name FROM t", want: []string{"x", "name"}},
		{name: "matview column list", sql: "CREATE MATERIALIZED VIEW v (x) AS SELECT id FROM t", want: []string{"x"}},
		{name: "long column list", sql: "CREATE VIEW v (x, y) AS SELECT id FROM t", wantErr: "view v names 2 columns in its column list but selects 1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := postgres.ParseViewSQL([]byte(tt.sql), "")
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

func TestAlterColumnExpression(t *testing.T) {
	sql := `
		CREATE TABLE t (a int, b int GENERATED ALWAYS AS (a * 2) STORED, c int GENERATED ALWAYS AS (a * 3) STORED);
		ALTER TABLE t ALTER COLUMN b DROP EXPRESSION;
		ALTER TABLE t ALTER COLUMN c SET EXPRESSION AS (a * 4);
		ALTER TABLE t ALTER COLUMN a DROP EXPRESSION IF EXISTS;
	`
	p := postgres.New("")
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	cols := p.Schema().Tables[0].Columns
	got := make([]string, 0, len(cols))
	for _, c := range cols {
		got = append(got, fmt.Sprintf("%s %q %q", c.Name, c.GeneratedExpr, c.GeneratedStorage))
	}
	// PostgreSQL 17: b is a plain column and c is generated as a * 4.
	want := []string{`a "" ""`, `b "" ""`, `c "a * 4" "STORED"`}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("generated columns mismatch (-want +got):\n%s", diff)
	}
}

func TestAlterTablePartitions(t *testing.T) {
	sql := `
		CREATE TABLE p (id int NOT NULL, k int NOT NULL, PRIMARY KEY (id, k)) PARTITION BY LIST (k);
		CREATE TABLE c1 (id int NOT NULL, k int NOT NULL, PRIMARY KEY (id, k));
		CREATE TABLE r (id int, k int, FOREIGN KEY (id, k) REFERENCES c1 (id, k));
		CREATE TABLE c2 PARTITION OF p FOR VALUES IN (2);
		ALTER TABLE c2 ADD CONSTRAINT c2_k CHECK (k = 2);
		ALTER TABLE ONLY p ATTACH PARTITION c1 FOR VALUES IN (1);
		ALTER TABLE ONLY c1 ADD CONSTRAINT c1_k CHECK (k = 1);
		COMMENT ON TABLE c1 IS 'partition';
		COMMENT ON COLUMN c1.k IS 'key';
		ALTER TABLE p DETACH PARTITION c2;
		ALTER TABLE p ADD COLUMN extra int;
		CREATE INDEX ON c1 (k);
		CREATE INDEX ON c2 (k);
	`
	p := postgres.New("")
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	tables := make([]string, 0, len(p.Schema().Tables))
	for _, tbl := range p.Schema().Tables {
		cols := make([]string, 0, len(tbl.Columns))
		for _, c := range tbl.Columns {
			cols = append(cols, c.Name)
		}
		tables = append(tables, tbl.Name+" "+strings.Join(cols, ","))
	}
	// The attached c1 is a partition, which the introspector skips; a foreign
	// key that references it stays. The detached c2 is a table again, which
	// the parser warns it does not model.
	if diff := cmp.Diff([]string{"p id,k,extra", "r id,k"}, tables); diff != "" {
		t.Errorf("tables mismatch (-want +got):\n%s", diff)
	}
	// Both still take an index, as PostgreSQL allows; only c1 is a partition.
	c1 := parser.Partition{Schema: "public", Name: "c1", ParentSchema: "public", ParentName: "p"}
	if diff := cmp.Diff([]parser.Partition{c1}, p.Schema().Partitions); diff != "" {
		t.Errorf("partitions mismatch (-want +got):\n%s", diff)
	}
	// A detached table attached again is a partition again.
	if err := p.Parse("reattach.sql", []byte("ALTER TABLE p ATTACH PARTITION c2 FOR VALUES IN (2);")); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	c2 := parser.Partition{Schema: "public", Name: "c2", ParentSchema: "public", ParentName: "p"}
	if diff := cmp.Diff([]parser.Partition{c1, c2}, p.Schema().Partitions); diff != "" {
		t.Errorf("partitions after reattach mismatch (-want +got):\n%s", diff)
	}
	wantWarnings := []string{
		"CREATE TABLE c2 PARTITION OF p skipped in DDL file — the partitioned table is the entity",
		"ALTER TABLE p ATTACH PARTITION c1: the partition is dropped from the model — the partitioned table is the entity",
		"ALTER TABLE p DETACH PARTITION c2 skipped in DDL file — the detached table is not modelled under files; use introspection",
	}
	if diff := cmp.Diff(wantWarnings, p.Schema().Warnings); diff != "" {
		t.Errorf("warnings mismatch (-want +got):\n%s", diff)
	}
}

func TestAttachSubPartition(t *testing.T) {
	sql := `
		CREATE TABLE p (id int, s int) PARTITION BY LIST (s);
		CREATE TABLE c1 PARTITION OF p FOR VALUES IN (1) PARTITION BY LIST (id);
		CREATE TABLE gc (id int, s int);
		ALTER TABLE ONLY c1 ATTACH PARTITION gc FOR VALUES IN (1);
	`
	p := postgres.New("")
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	if n := len(p.Schema().Tables); n != 1 || p.Schema().Tables[0].Name != "p" {
		t.Errorf("tables = %d, want only p", n)
	}
	want := []parser.Partition{
		{Schema: "public", Name: "c1", ParentSchema: "public", ParentName: "p"},
		{Schema: "public", Name: "gc", ParentSchema: "public", ParentName: "c1"},
	}
	if diff := cmp.Diff(want, p.Schema().Partitions); diff != "" {
		t.Errorf("partitions mismatch (-want +got):\n%s", diff)
	}
}

func TestAlterDomainConstraints(t *testing.T) {
	sql := `
		CREATE DOMAIN posint AS integer CHECK (VALUE < 1000);
		CREATE TABLE t (x int, CONSTRAINT posint_check1 CHECK (x > 0));
		ALTER DOMAIN posint ADD CHECK (VALUE <> 5);
		ALTER DOMAIN posint ADD CONSTRAINT pos CHECK (VALUE > 0);
		ALTER DOMAIN posint DROP CONSTRAINT posint_check;
		ALTER DOMAIN posint DROP CONSTRAINT IF EXISTS nope;
		ALTER DOMAIN posint RENAME CONSTRAINT pos TO positive;
		ALTER DOMAIN posint ADD CHECK (VALUE <> 7);
		ALTER DOMAIN posint ADD NOT NULL;
		ALTER DOMAIN posint SET DEFAULT 1;
	`
	p := postgres.New("")
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	checks := p.Schema().DomainTypes[0].Constraints
	got := make([]string, 0, len(checks))
	for _, c := range checks {
		got = append(got, c.Name+" "+c.CheckExpression)
	}
	// PostgreSQL 17 names the checks as CREATE DOMAIN does, against every
	// constraint name in the schema.
	want := []string{"posint_check2 value <> 5", "positive value > 0", "posint_check value <> 7"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("domain constraints mismatch (-want +got):\n%s", diff)
	}

	for _, stmt := range []string{
		`ALTER DOMAIN nope ADD CHECK (VALUE > 0);`,
		`ALTER DOMAIN nope RENAME CONSTRAINT a TO b;`,
	} {
		err := postgres.New("").Parse("test.sql", []byte(stmt))
		if err == nil || !strings.Contains(err.Error(), "unknown domain public.nope") {
			t.Errorf("Parse(%q) error = %v, want unknown domain public.nope", stmt, err)
		}
	}
}

func TestRenameConstraintAndIndex(t *testing.T) {
	sql := `
		CREATE TABLE ri (a int, b int, c int, CONSTRAINT uq UNIQUE (a), CONSTRAINT pk PRIMARY KEY (b), CONSTRAINT ck CHECK (c > 0));
		CREATE INDEX ix ON ri (c);
		CREATE INDEX ON ri ((a + 1));
		ALTER TABLE ri RENAME CONSTRAINT uq TO uq2;
		ALTER TABLE ri RENAME CONSTRAINT pk TO pk2;
		ALTER TABLE ri RENAME CONSTRAINT ck TO ck2;
		ALTER TABLE IF EXISTS nope RENAME CONSTRAINT a TO b;
		ALTER INDEX ix RENAME TO ix2;
		ALTER INDEX uq2 RENAME TO uq3;
		ALTER INDEX IF EXISTS nope RENAME TO nope2;
		ALTER INDEX ri_expr_idx RENAME TO ri_c_idx;
		CREATE INDEX ON ri (c);
	`
	p := postgres.New("")
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	constraints := p.Schema().Tables[0].Constraints
	got := make([]string, 0, len(constraints))
	for _, c := range constraints {
		got = append(got, c.Type.String()+" "+c.Name)
	}
	// PostgreSQL 17 renames the index behind a UNIQUE or PRIMARY KEY with its
	// constraint, and the other way round. The expression index renamed to
	// ri_c_idx keeps the last index from that name.
	want := []string{"UNIQUE uq3", "PRIMARY KEY pk2", "CHECK ck2", "INDEX ix2", "INDEX ri_c_idx1"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("constraints mismatch (-want +got):\n%s", diff)
	}
}
