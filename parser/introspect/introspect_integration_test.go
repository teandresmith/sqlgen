package introspect_test

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/testcontainers/testcontainers-go"
	tcmysql "github.com/testcontainers/testcontainers-go/modules/mysql"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"

	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/parser/introspect"
	"github.com/teandresmith/sqlgen/parser/mysql"
	"github.com/teandresmith/sqlgen/parser/postgres"
	"github.com/teandresmith/sqlgen/parser/sqlite"
)

var (
	pgConnStr    string
	mysqlConnStr string
	sqliteDBPath string
)

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(0)
	}

	ctx := context.Background()

	// --- PostgreSQL container ---
	pgContainer, err := tcpostgres.Run(
		ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("sqlgen_test"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2),
		),
	)
	if err != nil {
		panic(fmt.Sprintf("starting postgres container: %v", err))
	}

	pgConnStr, err = pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		panic(fmt.Sprintf("postgres connection string: %v", err))
	}

	// --- MySQL container ---
	mysqlContainer, err := tcmysql.Run(
		ctx,
		"mysql:8.0",
		tcmysql.WithDatabase("sqlgen_test"),
		tcmysql.WithUsername("test"),
		tcmysql.WithPassword("test"),
	)
	if err != nil {
		panic(fmt.Sprintf("starting mysql container: %v", err))
	}

	mysqlConnStr, err = mysqlContainer.ConnectionString(ctx)
	if err != nil {
		panic(fmt.Sprintf("mysql connection string: %v", err))
	}

	// --- SQLite temp file ---
	tmpDir, err := os.MkdirTemp("", "sqlgen_sqlite_test")
	if err != nil {
		panic(fmt.Sprintf("creating temp dir: %v", err))
	}
	sqliteDBPath = filepath.Join(tmpDir, "test.db")

	code := m.Run()

	_ = pgContainer.Terminate(ctx)
	_ = mysqlContainer.Terminate(ctx)
	_ = os.RemoveAll(tmpDir)
	os.Exit(code)
}

// --- PostgreSQL integration tests ---

func TestPostgresIntrospect(t *testing.T) {
	ctx := context.Background()

	ddl := `
		CREATE TABLE users (
			id bigserial PRIMARY KEY,
			name text NOT NULL,
			email varchar(255) UNIQUE,
			age integer,
			active boolean NOT NULL DEFAULT true,
			data jsonb,
			tags text[],
			created_at timestamptz NOT NULL DEFAULT now(),
			CONSTRAINT users_age_check CHECK (age > 0)
		);

		COMMENT ON TABLE users IS 'Application users';
		COMMENT ON COLUMN users.email IS 'Unique email address';

		CREATE TABLE posts (
			id bigserial PRIMARY KEY,
			user_id bigint NOT NULL REFERENCES users(id),
			title text NOT NULL,
			body text,
			published boolean NOT NULL DEFAULT false
		);

		CREATE TYPE status_enum AS ENUM ('active', 'inactive', 'pending');

		CREATE TYPE address AS (
			street text,
			city text,
			zip varchar(10)
		);

		CREATE DOMAIN positive_int AS integer CHECK (VALUE > 0);
	`

	// Apply DDL to live database.
	db, err := sql.Open("pgx", pgConnStr)
	if err != nil {
		t.Fatalf("opening postgres: %v", err)
	}
	defer db.Close() //nolint:errcheck // test cleanup

	// Use a separate schema to avoid conflicts.
	_, err = db.ExecContext(ctx, "CREATE SCHEMA IF NOT EXISTS introspect_test")
	if err != nil {
		t.Fatalf("creating schema: %v", err)
	}
	_, err = db.ExecContext(ctx, "SET search_path TO introspect_test")
	if err != nil {
		t.Fatalf("setting search_path: %v", err)
	}
	// Clean up any previous run.
	_, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS posts CASCADE")
	_, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS users CASCADE")
	_, _ = db.ExecContext(ctx, "DROP TYPE IF EXISTS status_enum CASCADE")
	_, _ = db.ExecContext(ctx, "DROP TYPE IF EXISTS address CASCADE")
	_, _ = db.ExecContext(ctx, "DROP DOMAIN IF EXISTS positive_int CASCADE")

	_, err = db.ExecContext(ctx, ddl)
	if err != nil {
		t.Fatalf("applying DDL: %v", err)
	}

	// Introspect.
	intro := introspect.NewPostgresIntrospector()
	defer intro.Close() //nolint:errcheck // test cleanup

	introspected := &parser.Schema{}
	err = intro.Introspect(ctx, pgConnStr+"&search_path=introspect_test", introspected, introspect.IntrospectionOptions{
		Schemas: []string{"introspect_test"},
	})
	if err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}
	introspected.Sort()

	// Parse the same DDL.
	p := postgres.New("introspect_test")
	if err := p.Parse("test.sql", []byte(ddl)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	parsed := p.Schema()
	parsed.Sort()

	// Compare tables.
	if len(introspected.Tables) != len(parsed.Tables) {
		t.Fatalf("table count: introspected=%d, parsed=%d", len(introspected.Tables), len(parsed.Tables))
	}

	for i, want := range parsed.Tables {
		got := introspected.Tables[i]

		if got.Schema != want.Schema || got.Name != want.Name {
			t.Errorf("table[%d]: got %s.%s, want %s.%s", i, got.Schema, got.Name, want.Schema, want.Name)
			continue
		}

		// Compare columns.
		if len(got.Columns) != len(want.Columns) {
			t.Errorf("table %s columns: got %d, want %d", want.Name, len(got.Columns), len(want.Columns))
			continue
		}

		for j, wantCol := range want.Columns {
			gotCol := got.Columns[j]
			// Default: introspection normalizes default expressions, so the text
			// differs from the DDL source. InlineUnique: a parse-provenance flag
			// (inline `col UNIQUE` vs table-level `UNIQUE(col)`) that the DDL
			// parsers set but introspection cannot reconstruct — Postgres stores
			// both forms identically in pg_constraint. Neither field is
			// recoverable from the live catalog, so both are excluded.
			if diff := cmp.Diff(wantCol, gotCol, cmpopts.IgnoreFields(parser.Column{}, "Default", "InlineUnique")); diff != "" {
				t.Errorf("table %s column %s mismatch (-want +got):\n%s", want.Name, wantCol.Name, diff)
			}
		}

		// Compare comments.
		if got.Comment != want.Comment {
			t.Errorf("table %s comment: got %q, want %q", want.Name, got.Comment, want.Comment)
		}

		// Compare CHECK constraints.
		wantChecks := filterConstraints(want.Constraints, parser.Check)
		gotChecks := filterConstraints(got.Constraints, parser.Check)
		if len(gotChecks) != len(wantChecks) {
			t.Errorf("table %s CHECK constraints: got %d, want %d", want.Name, len(gotChecks), len(wantChecks))
		} else {
			for j, wc := range wantChecks {
				gc := gotChecks[j]
				if gc.Name != wc.Name {
					t.Errorf("table %s CHECK[%d] name: got %q, want %q", want.Name, j, gc.Name, wc.Name)
				}
				if gc.CheckExpression == "" {
					t.Errorf("table %s CHECK %q: introspected expression is empty", want.Name, gc.Name)
				}
			}
		}
	}

	// Compare enums.
	if diff := cmp.Diff(parsed.Enums, introspected.Enums); diff != "" {
		t.Errorf("enums mismatch (-want +got):\n%s", diff)
	}

	// Compare composite types.
	if len(introspected.CompositeTypes) != len(parsed.CompositeTypes) {
		t.Errorf("composite type count: introspected=%d, parsed=%d",
			len(introspected.CompositeTypes), len(parsed.CompositeTypes))
	} else {
		for i, want := range parsed.CompositeTypes {
			got := introspected.CompositeTypes[i]
			if got.Name != want.Name || got.Schema != want.Schema {
				t.Errorf("composite type[%d]: got %s.%s, want %s.%s", i, got.Schema, got.Name, want.Schema, want.Name)
			}
			if diff := cmp.Diff(want.Attributes, got.Attributes); diff != "" {
				t.Errorf("composite type %s attributes mismatch (-want +got):\n%s", want.Name, diff)
			}
		}
	}

	// Compare domain types.
	if len(introspected.DomainTypes) != len(parsed.DomainTypes) {
		t.Errorf("domain type count: introspected=%d, parsed=%d",
			len(introspected.DomainTypes), len(parsed.DomainTypes))
	} else {
		for i, want := range parsed.DomainTypes {
			got := introspected.DomainTypes[i]
			if got.Name != want.Name || got.Schema != want.Schema {
				t.Errorf("domain type[%d]: got %s.%s, want %s.%s", i, got.Schema, got.Name, want.Schema, want.Name)
			}
			if got.BaseType != want.BaseType {
				t.Errorf("domain type %s base type: got %q, want %q", want.Name, got.BaseType, want.BaseType)
			}
		}
	}
}

// --- MySQL integration tests ---

func TestMySQLIntrospect(t *testing.T) {
	ctx := context.Background()

	ddl := `
		CREATE TABLE users (
			id bigint NOT NULL AUTO_INCREMENT,
			name varchar(255) NOT NULL,
			email varchar(255),
			age int,
			active tinyint(1) NOT NULL DEFAULT 1,
			status enum('active','inactive','pending') NOT NULL DEFAULT 'active',
			PRIMARY KEY (id),
			UNIQUE KEY idx_email (email),
			CONSTRAINT users_age_check CHECK (age > 0)
		) COMMENT='Application users';

		CREATE TABLE posts (
			id bigint NOT NULL AUTO_INCREMENT,
			user_id bigint NOT NULL,
			title varchar(255) NOT NULL,
			body text,
			published tinyint(1) NOT NULL DEFAULT 0,
			PRIMARY KEY (id),
			CONSTRAINT fk_posts_user FOREIGN KEY (user_id) REFERENCES users(id)
		);
	`

	// Apply DDL to live database.
	db, err := sql.Open("mysql", mysqlConnStr)
	if err != nil {
		t.Fatalf("opening mysql: %v", err)
	}
	defer db.Close() //nolint:errcheck // test cleanup

	// Clean up previous run.
	_, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS posts")
	_, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS users")

	for _, stmt := range splitStatements(ddl) {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("applying DDL %q: %v", stmt[:min(len(stmt), 60)], err)
		}
	}

	// Introspect.
	intro := introspect.NewMySQLIntrospector()
	defer intro.Close() //nolint:errcheck // test cleanup

	introspected := &parser.Schema{}
	err = intro.Introspect(ctx, mysqlConnStr, introspected, introspect.IntrospectionOptions{})
	if err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}
	introspected.Sort()

	// Parse the same DDL.
	p, err := mysql.New()
	if err != nil {
		t.Fatalf("creating mysql parser: %v", err)
	}
	if err := p.Parse("test.sql", []byte(ddl)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	parsed := p.Schema()
	parsed.Sort()

	// Compare tables.
	if len(introspected.Tables) != len(parsed.Tables) {
		t.Fatalf("table count: introspected=%d, parsed=%d", len(introspected.Tables), len(parsed.Tables))
	}

	for i, want := range parsed.Tables {
		got := introspected.Tables[i]

		if got.Name != want.Name {
			t.Errorf("table[%d]: got %q, want %q", i, got.Name, want.Name)
			continue
		}

		// Compare column count and key properties.
		if len(got.Columns) != len(want.Columns) {
			t.Errorf("table %s columns: got %d, want %d", want.Name, len(got.Columns), len(want.Columns))
			continue
		}

		for j, wantCol := range want.Columns {
			gotCol := got.Columns[j]
			if gotCol.Name != wantCol.Name {
				t.Errorf("table %s col[%d] name: got %q, want %q", want.Name, j, gotCol.Name, wantCol.Name)
			}
			if gotCol.Type != wantCol.Type {
				t.Errorf("table %s col %s type: got %q, want %q", want.Name, wantCol.Name, gotCol.Type, wantCol.Type)
			}
			if gotCol.Nullable != wantCol.Nullable {
				t.Errorf("table %s col %s nullable: got %v, want %v", want.Name, wantCol.Name, gotCol.Nullable, wantCol.Nullable)
			}
			if gotCol.PrimaryKey != wantCol.PrimaryKey {
				t.Errorf("table %s col %s pk: got %v, want %v", want.Name, wantCol.Name, gotCol.PrimaryKey, wantCol.PrimaryKey)
			}
			if gotCol.AutoIncrement != wantCol.AutoIncrement {
				t.Errorf("table %s col %s auto_increment: got %v, want %v", want.Name, wantCol.Name, gotCol.AutoIncrement, wantCol.AutoIncrement)
			}
		}
	}

	// Compare enums (MySQL inline enums).
	if len(introspected.Enums) != len(parsed.Enums) {
		t.Errorf("enum count: introspected=%d, parsed=%d", len(introspected.Enums), len(parsed.Enums))
	}

	// Verify CHECK constraint on users table.
	usersTable := introspected.Tables[1] // sorted: posts, users
	if usersTable.Name != "users" {
		// Find it explicitly.
		for _, tbl := range introspected.Tables {
			if tbl.Name == "users" {
				usersTable = tbl
				break
			}
		}
	}
	checks := filterConstraints(usersTable.Constraints, parser.Check)
	if len(checks) == 0 {
		t.Error("MySQL users table: expected at least 1 CHECK constraint, got 0")
	} else {
		found := false
		for _, c := range checks {
			if c.Name == "users_age_check" {
				found = true
				if c.CheckExpression == "" {
					t.Error("MySQL CHECK users_age_check: expression is empty")
				}
			}
		}
		if !found {
			t.Errorf("MySQL users table: CHECK constraint 'users_age_check' not found in %v", checks)
		}
	}
}

// --- SQLite integration tests ---

func TestSQLiteIntrospect(t *testing.T) {
	ctx := context.Background()

	ddl := `
		CREATE TABLE users (
			id integer PRIMARY KEY,
			name text NOT NULL,
			email text UNIQUE,
			age integer,
			active boolean NOT NULL DEFAULT 1
		);

		CREATE TABLE posts (
			id integer PRIMARY KEY,
			user_id integer NOT NULL REFERENCES users(id),
			title text NOT NULL,
			body text,
			published boolean NOT NULL DEFAULT 0
		);
	`

	// Create a fresh SQLite database.
	dbPath := sqliteDBPath + "_basic"
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("opening sqlite: %v", err)
	}
	defer db.Close()        //nolint:errcheck // test cleanup
	defer os.Remove(dbPath) //nolint:errcheck // test cleanup

	_, err = db.ExecContext(ctx, ddl)
	if err != nil {
		t.Fatalf("applying DDL: %v", err)
	}

	// Introspect.
	intro := introspect.NewSQLiteIntrospector()
	defer intro.Close() //nolint:errcheck // test cleanup

	introspected := &parser.Schema{}
	err = intro.Introspect(ctx, dbPath, introspected, introspect.IntrospectionOptions{})
	if err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}
	introspected.Sort()

	// Parse the same DDL.
	p := sqlite.New()
	if err := p.Parse("test.sql", []byte(ddl)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	parsed := p.Schema()
	parsed.Sort()

	// Compare tables.
	if len(introspected.Tables) != len(parsed.Tables) {
		t.Fatalf("table count: introspected=%d, parsed=%d", len(introspected.Tables), len(parsed.Tables))
	}

	for i, want := range parsed.Tables {
		got := introspected.Tables[i]

		if got.Name != want.Name {
			t.Errorf("table[%d]: got %q, want %q", i, got.Name, want.Name)
			continue
		}

		if len(got.Columns) != len(want.Columns) {
			t.Errorf("table %s columns: got %d, want %d", want.Name, len(got.Columns), len(want.Columns))
			continue
		}

		for j, wantCol := range want.Columns {
			gotCol := got.Columns[j]
			if gotCol.Name != wantCol.Name {
				t.Errorf("table %s col[%d] name: got %q, want %q", want.Name, j, gotCol.Name, wantCol.Name)
			}
			if gotCol.Nullable != wantCol.Nullable {
				t.Errorf("table %s col %s nullable: got %v, want %v", want.Name, wantCol.Name, gotCol.Nullable, wantCol.Nullable)
			}
			if gotCol.PrimaryKey != wantCol.PrimaryKey {
				t.Errorf("table %s col %s pk: got %v, want %v", want.Name, wantCol.Name, gotCol.PrimaryKey, wantCol.PrimaryKey)
			}
			if gotCol.AutoIncrement != wantCol.AutoIncrement {
				t.Errorf("table %s col %s auto_increment: got %v, want %v", want.Name, wantCol.Name, gotCol.AutoIncrement, wantCol.AutoIncrement)
			}
		}
	}
}

// --- Filtering tests ---

func TestPostgresSchemaFiltering(t *testing.T) {
	ctx := context.Background()

	db, err := sql.Open("pgx", pgConnStr)
	if err != nil {
		t.Fatalf("opening postgres: %v", err)
	}
	defer db.Close() //nolint:errcheck // test cleanup

	// Create two schemas with tables.
	for _, stmt := range []string{
		"CREATE SCHEMA IF NOT EXISTS filter_a",
		"CREATE SCHEMA IF NOT EXISTS filter_b",
		"DROP TABLE IF EXISTS filter_a.t1 CASCADE",
		"DROP TABLE IF EXISTS filter_b.t2 CASCADE",
		"CREATE TABLE filter_a.t1 (id integer PRIMARY KEY)",
		"CREATE TABLE filter_b.t2 (id integer PRIMARY KEY)",
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}

	intro := introspect.NewPostgresIntrospector()
	defer intro.Close() //nolint:errcheck // test cleanup

	// Include only filter_a.
	schema := &parser.Schema{}
	err = intro.Introspect(ctx, pgConnStr, schema, introspect.IntrospectionOptions{
		Schemas: []string{"filter_a"},
	})
	if err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}

	if len(schema.Tables) != 1 {
		t.Fatalf("expected 1 table, got %d", len(schema.Tables))
	}
	if schema.Tables[0].Name != "t1" {
		t.Errorf("expected table t1, got %s", schema.Tables[0].Name)
	}

	// Exclude filter_a.
	intro2 := introspect.NewPostgresIntrospector()
	defer intro2.Close() //nolint:errcheck // test cleanup

	schema2 := &parser.Schema{}
	err = intro2.Introspect(ctx, pgConnStr, schema2, introspect.IntrospectionOptions{
		Schemas:        []string{"filter_a", "filter_b"},
		ExcludeSchemas: []string{"filter_a"},
	})
	if err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}

	if len(schema2.Tables) != 1 {
		t.Fatalf("expected 1 table, got %d", len(schema2.Tables))
	}
	if schema2.Tables[0].Name != "t2" {
		t.Errorf("expected table t2, got %s", schema2.Tables[0].Name)
	}
}

func TestTableFilteringWithWildcards(t *testing.T) {
	ctx := context.Background()

	// Use SQLite for wildcard table filtering — simplest setup.
	dbPath := sqliteDBPath + "_filter"
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("opening sqlite: %v", err)
	}
	defer db.Close()        //nolint:errcheck // test cleanup
	defer os.Remove(dbPath) //nolint:errcheck // test cleanup

	_, err = db.ExecContext(ctx, `
		CREATE TABLE users (id integer PRIMARY KEY);
		CREATE TABLE user_settings (id integer PRIMARY KEY);
		CREATE TABLE posts (id integer PRIMARY KEY);
		CREATE TABLE post_tags (id integer PRIMARY KEY);
	`)
	if err != nil {
		t.Fatalf("applying DDL: %v", err)
	}

	intro := introspect.NewSQLiteIntrospector()
	defer intro.Close() //nolint:errcheck // test cleanup

	// Include user* tables.
	schema := &parser.Schema{}
	err = intro.Introspect(ctx, dbPath, schema, introspect.IntrospectionOptions{
		Tables: []string{"user*"},
	})
	if err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}

	if len(schema.Tables) != 2 {
		t.Fatalf("expected 2 tables, got %d", len(schema.Tables))
	}

	// Exclude *_tags tables.
	intro2 := introspect.NewSQLiteIntrospector()
	defer intro2.Close() //nolint:errcheck // test cleanup

	schema2 := &parser.Schema{}
	err = intro2.Introspect(ctx, dbPath, schema2, introspect.IntrospectionOptions{
		ExcludeTables: []string{"*_tags"},
	})
	if err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}

	if len(schema2.Tables) != 3 {
		t.Fatalf("expected 3 tables, got %d", len(schema2.Tables))
	}
	for _, tbl := range schema2.Tables {
		if tbl.Name == "post_tags" {
			t.Errorf("post_tags should have been excluded")
		}
	}
}

// --- Both mode tests ---

func TestBothModeAlterOverlay(t *testing.T) {
	ctx := context.Background()

	// Create a SQLite database with a base table.
	dbPath := sqliteDBPath + "_both_alter"
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("opening sqlite: %v", err)
	}
	defer db.Close()        //nolint:errcheck // test cleanup
	defer os.Remove(dbPath) //nolint:errcheck // test cleanup

	_, err = db.ExecContext(ctx, `
		CREATE TABLE users (
			id integer PRIMARY KEY,
			name text NOT NULL
		);
	`)
	if err != nil {
		t.Fatalf("applying base DDL: %v", err)
	}

	// Introspect base schema.
	intro := introspect.NewSQLiteIntrospector()
	defer intro.Close() //nolint:errcheck // test cleanup

	base := &parser.Schema{}
	if err := intro.Introspect(ctx, dbPath, base, introspect.IntrospectionOptions{}); err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}

	// Create a migration file that adds a column.
	tmpDir, err := os.MkdirTemp("", "sqlgen_both_test")
	if err != nil {
		t.Fatalf("creating temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir) //nolint:errcheck // test cleanup

	migrationFile := filepath.Join(tmpDir, "001_add_email.sql")
	if err := os.WriteFile(migrationFile, []byte("ALTER TABLE users ADD COLUMN email text;"), 0o600); err != nil {
		t.Fatalf("writing migration: %v", err)
	}

	// Merge.
	p := sqlite.New()
	if err := introspect.Merge(base, p, []string{migrationFile}); err != nil {
		t.Fatalf("Merge() error: %v", err)
	}

	merged := p.Schema()
	if len(merged.Tables) != 1 {
		t.Fatalf("expected 1 table, got %d", len(merged.Tables))
	}

	users := merged.Tables[0]
	if len(users.Columns) != 3 {
		t.Fatalf("expected 3 columns, got %d", len(users.Columns))
	}
	if users.Columns[2].Name != "email" {
		t.Errorf("expected column 'email', got %q", users.Columns[2].Name)
	}
}

func TestBothModeCreateExisting(t *testing.T) {
	ctx := context.Background()

	// Create a SQLite database with a table.
	dbPath := sqliteDBPath + "_both_create"
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("opening sqlite: %v", err)
	}
	defer db.Close()        //nolint:errcheck // test cleanup
	defer os.Remove(dbPath) //nolint:errcheck // test cleanup

	_, err = db.ExecContext(ctx, "CREATE TABLE users (id integer PRIMARY KEY);")
	if err != nil {
		t.Fatalf("applying base DDL: %v", err)
	}

	// Introspect base.
	intro := introspect.NewSQLiteIntrospector()
	defer intro.Close() //nolint:errcheck // test cleanup

	base := &parser.Schema{}
	if err := intro.Introspect(ctx, dbPath, base, introspect.IntrospectionOptions{}); err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}

	// File tries to CREATE TABLE users — should fail.
	tmpDir, err := os.MkdirTemp("", "sqlgen_both_create")
	if err != nil {
		t.Fatalf("creating temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir) //nolint:errcheck // test cleanup

	migrationFile := filepath.Join(tmpDir, "001_create_users.sql")
	if err := os.WriteFile(migrationFile, []byte("CREATE TABLE users (id integer PRIMARY KEY, name text);"), 0o600); err != nil {
		t.Fatalf("writing migration: %v", err)
	}

	p := sqlite.New()
	err = introspect.Merge(base, p, []string{migrationFile})
	if err == nil {
		t.Fatal("Merge() expected error for CREATE TABLE on existing, got nil")
	}
	if got := err.Error(); !strings.Contains(got, "already exists") {
		t.Errorf("Merge() error = %q, want error containing 'already exists'", got)
	}
}

func TestBothModeColumnConflict(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sqlgen_both_conflict")
	if err != nil {
		t.Fatalf("creating temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir) //nolint:errcheck // test cleanup

	t.Run("DROP+ADD is explicit and allowed", func(t *testing.T) {
		base := &parser.Schema{
			Tables: []parser.Table{
				{
					Name: "items",
					Columns: []parser.Column{
						{Name: "id", Type: "integer", PrimaryKey: true, AutoIncrement: true},
						{Name: "count", Type: "integer"},
					},
				},
			},
		}

		migrationFile := filepath.Join(tmpDir, "001_change_type.sql")
		migration := "ALTER TABLE items DROP COLUMN count;\nALTER TABLE items ADD COLUMN count text;"
		if err := os.WriteFile(migrationFile, []byte(migration), 0o600); err != nil {
			t.Fatalf("writing migration: %v", err)
		}

		p := sqlite.New()
		if err := introspect.Merge(base, p, []string{migrationFile}); err != nil {
			t.Fatalf("Merge() unexpected error for DROP+ADD: %v", err)
		}

		merged := p.Schema()
		items := merged.Tables[0]
		countCol := parser.ColumnByName(&items, "count")
		if countCol == nil {
			t.Fatal("expected column 'count' in merged schema")
		}
		if countCol.Type != "text" {
			t.Errorf("expected count type 'text', got %q", countCol.Type)
		}
	})

	t.Run("ADD COLUMN duplicating existing with different type errors", func(t *testing.T) {
		base := &parser.Schema{
			Tables: []parser.Table{
				{
					Name: "items",
					Columns: []parser.Column{
						{Name: "id", Type: "integer", PrimaryKey: true, AutoIncrement: true},
						{Name: "count", Type: "integer"},
					},
				},
			},
		}

		migrationFile := filepath.Join(tmpDir, "002_dup_column.sql")
		migration := "ALTER TABLE items ADD COLUMN count text;"
		if err := os.WriteFile(migrationFile, []byte(migration), 0o600); err != nil {
			t.Fatalf("writing migration: %v", err)
		}

		p := sqlite.New()
		err := introspect.Merge(base, p, []string{migrationFile})
		if err == nil {
			t.Fatal("Merge() expected error for duplicate column with different type, got nil")
		}
		if !strings.Contains(err.Error(), "column conflict") {
			t.Errorf("Merge() error = %q, want error containing 'column conflict'", err.Error())
		}
	})
}

// --- View introspection tests ---

func TestPostgresIntrospectViews(t *testing.T) {
	ctx := context.Background()

	db, err := sql.Open("pgx", pgConnStr)
	if err != nil {
		t.Fatalf("opening postgres: %v", err)
	}
	defer db.Close() //nolint:errcheck // test cleanup

	// Set up schema with table and view.
	for _, stmt := range []string{
		"CREATE SCHEMA IF NOT EXISTS view_test",
		"SET search_path TO view_test",
		"DROP VIEW IF EXISTS view_test.user_summary CASCADE",
		"DROP TABLE IF EXISTS view_test.users CASCADE",
		`CREATE TABLE view_test.users (
			id bigserial PRIMARY KEY,
			name text NOT NULL,
			email varchar(255),
			score integer NOT NULL DEFAULT 0
		)`,
		`CREATE VIEW view_test.user_summary AS
			SELECT id, name, score FROM view_test.users WHERE score > 0`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}

	intro := introspect.NewPostgresIntrospector()
	defer intro.Close() //nolint:errcheck // test cleanup

	schema := &parser.Schema{}
	err = intro.Introspect(ctx, pgConnStr+"&search_path=view_test", schema, introspect.IntrospectionOptions{
		Schemas: []string{"view_test"},
	})
	if err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}

	if len(schema.Views) != 1 {
		t.Fatalf("views = %d, want 1", len(schema.Views))
	}

	view := schema.Views[0]
	if view.Name != "user_summary" {
		t.Errorf("view.Name = %q, want %q", view.Name, "user_summary")
	}
	if view.Schema != "view_test" {
		t.Errorf("view.Schema = %q, want %q", view.Schema, "view_test")
	}
	if len(view.Columns) != 3 {
		t.Fatalf("view columns = %d, want 3", len(view.Columns))
	}

	wantCols := map[string]string{
		"id":    "bigint",
		"name":  "text",
		"score": "integer",
	}
	for _, col := range view.Columns {
		want, ok := wantCols[col.Name]
		if !ok {
			t.Errorf("unexpected view column %q", col.Name)
			continue
		}
		if col.Type != want {
			t.Errorf("view column %s type = %q, want %q", col.Name, col.Type, want)
		}
	}
}

func TestPostgresIntrospectMaterializedViews(t *testing.T) {
	ctx := context.Background()

	db, err := sql.Open("pgx", pgConnStr)
	if err != nil {
		t.Fatalf("opening postgres: %v", err)
	}
	defer db.Close() //nolint:errcheck // test cleanup

	// mv_totals carries three qualifying unique indexes to pin deterministic
	// PK selection (fewest columns, then name ascending): the two single-column
	// indexes tie on column count and mv_totals_a_idx wins by name.
	// mv_plain has only non-qualifying unique indexes (partial + expression),
	// so it must surface with no PK and ConcurrentlyRefreshable == false.
	for _, stmt := range []string{
		"CREATE SCHEMA IF NOT EXISTS matview_test",
		"DROP MATERIALIZED VIEW IF EXISTS matview_test.mv_totals CASCADE",
		"DROP MATERIALIZED VIEW IF EXISTS matview_test.mv_plain CASCADE",
		"DROP VIEW IF EXISTS matview_test.order_view CASCADE",
		"DROP TABLE IF EXISTS matview_test.orders CASCADE",
		`CREATE TABLE matview_test.orders (
			id bigserial PRIMARY KEY,
			customer_id bigint NOT NULL,
			email text NOT NULL,
			amount numeric(10,2) NOT NULL
		)`,
		`CREATE VIEW matview_test.order_view AS
			SELECT id, customer_id FROM matview_test.orders`,
		`CREATE MATERIALIZED VIEW matview_test.mv_totals AS
			SELECT customer_id, email, count(*) AS order_count, sum(amount) AS total
			FROM matview_test.orders
			GROUP BY customer_id, email`,
		"CREATE UNIQUE INDEX mv_totals_a_idx ON matview_test.mv_totals (customer_id)",
		"CREATE UNIQUE INDEX mv_totals_b_idx ON matview_test.mv_totals (email)",
		"CREATE UNIQUE INDEX mv_totals_pair_idx ON matview_test.mv_totals (customer_id, email)",
		`CREATE MATERIALIZED VIEW matview_test.mv_plain AS
			SELECT customer_id, amount FROM matview_test.orders`,
		"CREATE UNIQUE INDEX mv_plain_partial_idx ON matview_test.mv_plain (customer_id) WHERE amount > 0",
		"CREATE UNIQUE INDEX mv_plain_expr_idx ON matview_test.mv_plain ((customer_id + 1))",
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}

	intro := introspect.NewPostgresIntrospector()
	defer intro.Close() //nolint:errcheck // test cleanup

	schema := &parser.Schema{}
	err = intro.Introspect(ctx, pgConnStr, schema, introspect.IntrospectionOptions{
		Schemas: []string{"matview_test"},
	})
	if err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}

	views := make(map[string]parser.View, len(schema.Views))
	for _, v := range schema.Views {
		views[v.Name] = v
	}
	if len(views) != 3 {
		t.Fatalf("views = %d (%v), want 3", len(views), slices.Collect(maps.Keys(views)))
	}

	regular, ok := views["order_view"]
	if !ok {
		t.Fatal("regular view order_view not discovered")
	}
	if regular.Materialized {
		t.Error("order_view.Materialized = true, want false")
	}
	if regular.ConcurrentlyRefreshable {
		t.Error("order_view.ConcurrentlyRefreshable = true, want false")
	}

	totals, ok := views["mv_totals"]
	if !ok {
		t.Fatal("materialized view mv_totals not discovered")
	}
	if !totals.Materialized {
		t.Error("mv_totals.Materialized = false, want true")
	}
	if !totals.ConcurrentlyRefreshable {
		t.Error("mv_totals.ConcurrentlyRefreshable = false, want true")
	}
	if totals.Schema != "matview_test" {
		t.Errorf("mv_totals.Schema = %q, want %q", totals.Schema, "matview_test")
	}
	if !strings.Contains(totals.SQL, "GROUP BY") {
		t.Errorf("mv_totals.SQL = %q, want the pg_matviews definition", totals.SQL)
	}

	wantCols := map[string]string{
		"customer_id": "bigint",
		"email":       "text",
		"order_count": "bigint",
		// sum(numeric(10,2)) widens to unconstrained numeric in Postgres.
		"total": "numeric",
	}
	if len(totals.Columns) != len(wantCols) {
		t.Fatalf("mv_totals columns = %d, want %d", len(totals.Columns), len(wantCols))
	}
	for _, col := range totals.Columns {
		want, ok := wantCols[col.Name]
		if !ok {
			t.Errorf("unexpected mv_totals column %q", col.Name)
			continue
		}
		if col.Type != want {
			t.Errorf("mv_totals column %s type = %q, want %q", col.Name, col.Type, want)
		}
	}

	// Deterministic PK: mv_totals_a_idx (customer_id) beats mv_totals_b_idx by
	// name and mv_totals_pair_idx by column count.
	var pkCols []string
	for _, col := range totals.Columns {
		if col.PrimaryKey {
			pkCols = append(pkCols, col.Name)
			if col.Nullable {
				t.Errorf("PK column %s is nullable, want NOT NULL", col.Name)
			}
		}
	}
	if diff := cmp.Diff([]string{"customer_id"}, pkCols); diff != "" {
		t.Errorf("mv_totals PK columns mismatch (-want +got):\n%s", diff)
	}

	plain, ok := views["mv_plain"]
	if !ok {
		t.Fatal("materialized view mv_plain not discovered")
	}
	if !plain.Materialized {
		t.Error("mv_plain.Materialized = false, want true")
	}
	if plain.ConcurrentlyRefreshable {
		t.Error("mv_plain.ConcurrentlyRefreshable = true, want false (partial/expression indexes must not qualify)")
	}
	for _, col := range plain.Columns {
		if col.PrimaryKey {
			t.Errorf("mv_plain column %s marked PrimaryKey, want none (no qualifying unique index)", col.Name)
		}
	}
}

func TestMySQLIntrospectViews(t *testing.T) {
	ctx := context.Background()

	db, err := sql.Open("mysql", mysqlConnStr)
	if err != nil {
		t.Fatalf("opening mysql: %v", err)
	}
	defer db.Close() //nolint:errcheck // test cleanup

	// Clean up and set up.
	for _, stmt := range []string{
		"DROP VIEW IF EXISTS user_summary",
		"DROP TABLE IF EXISTS view_users",
		`CREATE TABLE view_users (
			id bigint NOT NULL AUTO_INCREMENT,
			name varchar(255) NOT NULL,
			score int NOT NULL DEFAULT 0,
			PRIMARY KEY (id)
		)`,
		"CREATE VIEW user_summary AS SELECT id, name, score FROM view_users WHERE score > 0",
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("setup %q: %v", stmt[:min(len(stmt), 40)], err)
		}
	}

	intro := introspect.NewMySQLIntrospector()
	defer intro.Close() //nolint:errcheck // test cleanup

	schema := &parser.Schema{}
	err = intro.Introspect(ctx, mysqlConnStr, schema, introspect.IntrospectionOptions{})
	if err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}

	// Find user_summary view.
	var found *parser.View
	for i := range schema.Views {
		if schema.Views[i].Name == "user_summary" {
			found = &schema.Views[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("view user_summary not found in %d views", len(schema.Views))
	}

	if len(found.Columns) != 3 {
		t.Fatalf("view columns = %d, want 3", len(found.Columns))
	}

	wantCols := map[string]string{
		"id":    "bigint",
		"name":  "varchar(255)",
		"score": "int",
	}
	for _, col := range found.Columns {
		want, ok := wantCols[col.Name]
		if !ok {
			t.Errorf("unexpected view column %q", col.Name)
			continue
		}
		if col.Type != want {
			t.Errorf("view column %s type = %q, want %q", col.Name, col.Type, want)
		}
	}
}

func TestSQLiteIntrospectViews(t *testing.T) {
	ctx := context.Background()

	dbPath := sqliteDBPath + "_view"
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("opening sqlite: %v", err)
	}
	defer db.Close()        //nolint:errcheck // test cleanup
	defer os.Remove(dbPath) //nolint:errcheck // test cleanup

	_, err = db.ExecContext(ctx, `
		CREATE TABLE users (
			id integer PRIMARY KEY,
			name text NOT NULL,
			score integer NOT NULL DEFAULT 0
		);
		CREATE VIEW user_summary AS SELECT id, name, score FROM users WHERE score > 0;
	`)
	if err != nil {
		t.Fatalf("applying DDL: %v", err)
	}

	intro := introspect.NewSQLiteIntrospector()
	defer intro.Close() //nolint:errcheck // test cleanup

	schema := &parser.Schema{}
	err = intro.Introspect(ctx, dbPath, schema, introspect.IntrospectionOptions{})
	if err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}

	if len(schema.Views) != 1 {
		t.Fatalf("views = %d, want 1", len(schema.Views))
	}

	view := schema.Views[0]
	if view.Name != "user_summary" {
		t.Errorf("view.Name = %q, want %q", view.Name, "user_summary")
	}
	if len(view.Columns) != 3 {
		t.Fatalf("view columns = %d, want 3", len(view.Columns))
	}

	// SQLite PRAGMA table_info may not report precise types for view columns.
	// Verify at least the column names are correct.
	wantNames := []string{"id", "name", "score"}
	for i, want := range wantNames {
		if view.Columns[i].Name != want {
			t.Errorf("view column[%d] name = %q, want %q", i, view.Columns[i].Name, want)
		}
	}
}

// --- Index method + partial-where round-trip tests ---

func TestPostgresIntrospect_IndexMethodAndPartialWhere(t *testing.T) {
	ctx := context.Background()

	db, err := sql.Open("pgx", pgConnStr)
	if err != nil {
		t.Fatalf("opening postgres: %v", err)
	}
	defer db.Close() //nolint:errcheck // test cleanup

	for _, stmt := range []string{
		"CREATE SCHEMA IF NOT EXISTS idx_methods",
		"DROP TABLE IF EXISTS idx_methods.documents CASCADE",
		"DROP TABLE IF EXISTS idx_methods.users CASCADE",
		`CREATE TABLE idx_methods.users (
			id bigserial PRIMARY KEY,
			email text NOT NULL,
			deleted_at timestamptz
		)`,
		`CREATE UNIQUE INDEX users_email_active_uniq
		 ON idx_methods.users (email) WHERE deleted_at IS NULL`,
		`CREATE TABLE idx_methods.documents (
			id bigserial PRIMARY KEY,
			tags text[] NOT NULL DEFAULT '{}'
		)`,
		`CREATE INDEX documents_tags_gin
		 ON idx_methods.documents USING gin (tags)`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("setup %q: %v", truncate(stmt), err)
		}
	}

	intro := introspect.NewPostgresIntrospector()
	defer intro.Close() //nolint:errcheck // test cleanup

	schema := &parser.Schema{}
	err = intro.Introspect(ctx, pgConnStr+"&search_path=idx_methods", schema, introspect.IntrospectionOptions{
		Schemas: []string{"idx_methods"},
	})
	if err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}

	users := findTable(t, schema, "users")
	gotPartial := findConstraintByName(users.Constraints, "users_email_active_uniq")
	if gotPartial == nil {
		t.Fatalf("users: partial unique index users_email_active_uniq not surfaced; constraints=%+v", users.Constraints)
	}
	if gotPartial.Type != parser.Unique {
		t.Errorf("users partial unique: Type = %v, want Unique", gotPartial.Type)
	}
	if gotPartial.Method != "btree" {
		t.Errorf("users partial unique: Method = %q, want %q", gotPartial.Method, "btree")
	}
	if gotPartial.Where == "" {
		t.Errorf("users partial unique: Where is empty, want non-empty predicate")
	}
	if emailCol := parser.ColumnByName(users, "email"); emailCol != nil && emailCol.Unique {
		t.Errorf("users.email: col.Unique = true for partial UNIQUE, want false")
	}

	docs := findTable(t, schema, "documents")
	gotGIN := findConstraintByName(docs.Constraints, "documents_tags_gin")
	if gotGIN == nil {
		t.Fatalf("documents: gin index documents_tags_gin not surfaced; constraints=%+v", docs.Constraints)
	}
	if gotGIN.Type != parser.Index {
		t.Errorf("documents gin index: Type = %v, want Index", gotGIN.Type)
	}
	if gotGIN.Method != "gin" {
		t.Errorf("documents gin index: Method = %q, want %q", gotGIN.Method, "gin")
	}
	if gotGIN.Where != "" {
		t.Errorf("documents gin index: Where = %q, want empty", gotGIN.Where)
	}
}

func TestMySQLIntrospect_IndexMethod(t *testing.T) {
	ctx := context.Background()

	db, err := sql.Open("mysql", mysqlConnStr)
	if err != nil {
		t.Fatalf("opening mysql: %v", err)
	}
	defer db.Close() //nolint:errcheck // test cleanup

	for _, stmt := range []string{
		"DROP TABLE IF EXISTS idx_method_users",
		`CREATE TABLE idx_method_users (
			id bigint NOT NULL AUTO_INCREMENT,
			email varchar(255) NOT NULL,
			PRIMARY KEY (id),
			UNIQUE KEY uniq_email (email) USING HASH
		)`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("setup %q: %v", truncate(stmt), err)
		}
	}
	defer db.ExecContext(ctx, "DROP TABLE IF EXISTS idx_method_users") //nolint:errcheck // test cleanup

	intro := introspect.NewMySQLIntrospector()
	defer intro.Close() //nolint:errcheck // test cleanup

	schema := &parser.Schema{}
	if err := intro.Introspect(ctx, mysqlConnStr, schema, introspect.IntrospectionOptions{
		Tables: []string{"idx_method_users"},
	}); err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}

	users := findTable(t, schema, "idx_method_users")
	uniq := findConstraintByName(users.Constraints, "uniq_email")
	if uniq == nil {
		t.Fatalf("idx_method_users: uniq_email not surfaced; constraints=%+v", users.Constraints)
	}
	if uniq.Method == "" {
		t.Errorf("idx_method_users uniq_email: Method is empty, want non-empty (e.g. hash/btree)")
	}

	// PRIMARY KEY intentionally leaves Method empty to match DDL-parser output.
	for _, c := range users.Constraints {
		if c.Type == parser.PrimaryKey && c.Method != "" {
			t.Errorf("idx_method_users PRIMARY: Method = %q, want empty to match DDL-parser path", c.Method)
		}
	}
}

func TestSQLiteIntrospect_PartialUnique(t *testing.T) {
	ctx := context.Background()

	dbPath := sqliteDBPath + "_partial_unique"
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("opening sqlite: %v", err)
	}
	defer db.Close()        //nolint:errcheck // test cleanup
	defer os.Remove(dbPath) //nolint:errcheck // test cleanup

	_, err = db.ExecContext(ctx, `
		CREATE TABLE users (
			id integer PRIMARY KEY,
			email text NOT NULL,
			deleted_at text
		);
		CREATE UNIQUE INDEX users_email_active_uniq ON users(email) WHERE deleted_at IS NULL;
	`)
	if err != nil {
		t.Fatalf("applying DDL: %v", err)
	}

	intro := introspect.NewSQLiteIntrospector()
	defer intro.Close() //nolint:errcheck // test cleanup

	schema := &parser.Schema{}
	if err := intro.Introspect(ctx, dbPath, schema, introspect.IntrospectionOptions{}); err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}

	users := findTable(t, schema, "users")
	uniq := findConstraintByName(users.Constraints, "users_email_active_uniq")
	if uniq == nil {
		t.Fatalf("users: partial unique index not surfaced; constraints=%+v", users.Constraints)
	}
	if uniq.Type != parser.Unique {
		t.Errorf("users partial unique: Type = %v, want Unique", uniq.Type)
	}
	if uniq.Method != "btree" {
		t.Errorf("users partial unique: Method = %q, want %q", uniq.Method, "btree")
	}
	if uniq.Where == "" {
		t.Errorf("users partial unique: Where is empty, want predicate text")
	}
	if emailCol := parser.ColumnByName(users, "email"); emailCol != nil && emailCol.Unique {
		t.Errorf("users.email: col.Unique = true for partial UNIQUE, want false (partial-UNIQUE gate)")
	}
}

func TestMySQLIntrospect_NonUniqueIndex(t *testing.T) {
	ctx := context.Background()

	db, err := sql.Open("mysql", mysqlConnStr)
	if err != nil {
		t.Fatalf("opening mysql: %v", err)
	}
	defer db.Close() //nolint:errcheck // test cleanup

	for _, stmt := range []string{
		"DROP TABLE IF EXISTS nonuniq_idx",
		`CREATE TABLE nonuniq_idx (
			id bigint NOT NULL AUTO_INCREMENT,
			name varchar(255) NOT NULL,
			PRIMARY KEY (id),
			KEY idx_name (name) USING BTREE
		)`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("setup %q: %v", truncate(stmt), err)
		}
	}
	defer db.ExecContext(ctx, "DROP TABLE IF EXISTS nonuniq_idx") //nolint:errcheck // test cleanup

	intro := introspect.NewMySQLIntrospector()
	defer intro.Close() //nolint:errcheck // test cleanup

	schema := &parser.Schema{}
	if err := intro.Introspect(ctx, mysqlConnStr, schema, introspect.IntrospectionOptions{
		Tables: []string{"nonuniq_idx"},
	}); err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}

	table := findTable(t, schema, "nonuniq_idx")
	idx := findConstraintByName(table.Constraints, "idx_name")
	if idx == nil {
		t.Fatalf("nonuniq_idx: non-unique index idx_name not surfaced; constraints=%+v", table.Constraints)
	}
	if idx.Type != parser.Index {
		t.Errorf("nonuniq_idx idx_name: Type = %v, want Index", idx.Type)
	}
	if diff := cmp.Diff([]string{"name"}, idx.Columns); diff != "" {
		t.Errorf("nonuniq_idx idx_name: Columns mismatch (-want +got):\n%s", diff)
	}
	if idx.Method == "" {
		t.Errorf("nonuniq_idx idx_name: Method is empty, want non-empty (e.g. btree)")
	}
}

func TestSQLiteIntrospect_NonUniqueIndex(t *testing.T) {
	ctx := context.Background()

	dbPath := sqliteDBPath + "_nonunique_index"
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("opening sqlite: %v", err)
	}
	defer db.Close()        //nolint:errcheck // test cleanup
	defer os.Remove(dbPath) //nolint:errcheck // test cleanup

	_, err = db.ExecContext(ctx, `
		CREATE TABLE t (
			id integer PRIMARY KEY,
			a integer NOT NULL
		);
		CREATE INDEX idx_a ON t(a);
		CREATE INDEX idx_a_partial ON t(a) WHERE a > 0;
		CREATE INDEX idx_a_expr ON t(a * 2);
	`)
	if err != nil {
		t.Fatalf("applying DDL: %v", err)
	}

	intro := introspect.NewSQLiteIntrospector()
	defer intro.Close() //nolint:errcheck // test cleanup

	schema := &parser.Schema{}
	if err := intro.Introspect(ctx, dbPath, schema, introspect.IntrospectionOptions{}); err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}

	table := findTable(t, schema, "t")

	plain := findConstraintByName(table.Constraints, "idx_a")
	if plain == nil {
		t.Fatalf("t: non-unique index idx_a not surfaced; constraints=%+v", table.Constraints)
	}
	if plain.Type != parser.Index {
		t.Errorf("t idx_a: Type = %v, want Index", plain.Type)
	}
	if plain.Where != "" {
		t.Errorf("t idx_a: Where = %q, want empty", plain.Where)
	}

	partial := findConstraintByName(table.Constraints, "idx_a_partial")
	if partial == nil {
		t.Fatalf("t: partial non-unique index idx_a_partial not surfaced; constraints=%+v", table.Constraints)
	}
	if partial.Type != parser.Index {
		t.Errorf("t idx_a_partial: Type = %v, want Index", partial.Type)
	}
	if partial.Where == "" {
		t.Errorf("t idx_a_partial: Where is empty, want predicate text")
	}

	// Expression-only indexes project no columns (PRAGMA index_info reports a
	// NULL name): introspection must not error and must skip them, matching the
	// PostgreSQL sweep.
	if expr := findConstraintByName(table.Constraints, "idx_a_expr"); expr != nil {
		t.Errorf("t idx_a_expr: expression-only index surfaced with columns=%v, want skipped", expr.Columns)
	}
}

func TestMySQLIntrospect_ForeignKeyBackingIndexParity(t *testing.T) {
	ctx := context.Background()

	// A bare FOREIGN KEY with no explicit KEY: MySQL materializes a backing
	// index, which introspection reports. The DDL parser synthesizes the same
	// index, so both paths must agree on the index population.
	ddl := `
		CREATE TABLE fk_parity_users (
			id INT NOT NULL AUTO_INCREMENT,
			PRIMARY KEY (id)
		);
		CREATE TABLE fk_parity_orders (
			id INT NOT NULL AUTO_INCREMENT,
			user_id INT NOT NULL,
			PRIMARY KEY (id),
			FOREIGN KEY (user_id) REFERENCES fk_parity_users(id)
		);
	`

	db, err := sql.Open("mysql", mysqlConnStr)
	if err != nil {
		t.Fatalf("opening mysql: %v", err)
	}
	defer db.Close() //nolint:errcheck // test cleanup

	_, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS fk_parity_orders")
	_, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS fk_parity_users")
	for _, stmt := range splitStatements(ddl) {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("applying DDL %q: %v", truncate(stmt), err)
		}
	}
	defer db.ExecContext(ctx, "DROP TABLE IF EXISTS fk_parity_orders") //nolint:errcheck // test cleanup
	defer db.ExecContext(ctx, "DROP TABLE IF EXISTS fk_parity_users")  //nolint:errcheck // test cleanup

	// Introspection path.
	intro := introspect.NewMySQLIntrospector()
	defer intro.Close() //nolint:errcheck // test cleanup
	introSchema := &parser.Schema{}
	if err := intro.Introspect(ctx, mysqlConnStr, introSchema, introspect.IntrospectionOptions{
		Tables: []string{"fk_parity_users", "fk_parity_orders"},
	}); err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}

	// DDL path.
	p, err := mysql.New()
	if err != nil {
		t.Fatalf("mysql.New() error: %v", err)
	}
	if err := p.Parse("fk_parity.sql", []byte(ddl)); err != nil {
		t.Fatalf("DDL Parse() error: %v", err)
	}

	// Both paths must surface the same non-unique index column-lists on orders.
	// Compare by columns only — Method (introspect "btree" vs DDL "") is a
	// cosmetic difference the manifest normalizes, and is out of scope here.
	introCols := indexColumnLists(findTable(t, introSchema, "fk_parity_orders"))
	ddlCols := indexColumnLists(findTable(t, p.Schema(), "fk_parity_orders"))

	if diff := cmp.Diff(introCols, ddlCols); diff != "" {
		t.Errorf("index population differs between introspect and DDL (-introspect +ddl):\n%s", diff)
	}
	if len(ddlCols) != 1 || ddlCols[0] != "user_id" {
		t.Errorf("expected a single FK backing index on [user_id], got %v", ddlCols)
	}
}

// indexColumnLists returns the sorted, comma-joined column lists of every
// parser.Index constraint on the table.
func indexColumnLists(table *parser.Table) []string {
	var out []string
	for _, c := range table.Constraints {
		if c.Type == parser.Index {
			out = append(out, strings.Join(c.Columns, ","))
		}
	}
	slices.Sort(out)
	return out
}

// --- Helpers ---

func findTable(t *testing.T, schema *parser.Schema, name string) *parser.Table {
	t.Helper()
	for i := range schema.Tables {
		if schema.Tables[i].Name == name {
			return &schema.Tables[i]
		}
	}
	t.Fatalf("table %q not found in schema", name)
	return nil
}

func findConstraintByName(constraints []parser.Constraint, name string) *parser.Constraint {
	for i := range constraints {
		if constraints[i].Name == name {
			return &constraints[i]
		}
	}
	return nil
}

func truncate(s string) string {
	const n = 60
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func splitStatements(sqlText string) []string {
	var stmts []string
	for part := range strings.SplitSeq(sqlText, ";") {
		s := strings.TrimSpace(part)
		if s != "" {
			stmts = append(stmts, s+";")
		}
	}
	return stmts
}

func filterConstraints(constraints []parser.Constraint, ctype parser.ConstraintType) []parser.Constraint {
	var result []parser.Constraint
	for _, c := range constraints {
		if c.Type == ctype {
			result = append(result, c)
		}
	}
	return result
}
