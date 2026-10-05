package postgres_test

import (
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
		"t_numeric":     "numeric(10, 2)",
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
				"users PRIMARY KEY [user_id]",
				"posts.user_id -> public.users(user_id)",
				"comments.user_id -> public.users(user_id)",
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
				"post_tags.article_id -> public.posts(id)",
				"post_tags.tag_id -> public.tags(id)",
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

// TestDropConstraintRecomputesColumnUnique pins that dropping a
// table-level UNIQUE constraint must re-derive col.Unique from the
// remaining state (other unconditional UNIQUEs + the column's
// InlineUnique flag) rather than unconditionally clearing it.
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
