package introspect_test

import (
	"context"
	"database/sql"
	"errors"
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

// parityException excludes one field from assertSchemaParity. Field names a
// field of a parser type as "<Type>.<Field>", such as "Constraint.CheckExpression".
type parityException struct {
	Field  string
	Reason string
}

// parityExceptions lists, per dialect, the fields no catalog stores, so an
// introspected schema cannot round-trip them. They are the only differences
// assertSchemaParity tolerates. A test that needs its own exception declares
// it beside this table and passes it as ignore, so every exception can be
// reviewed in one place. Every dialect round-trips every field today.
var parityExceptions = map[string][]parityException{
	"postgres": nil,
	"mysql":    nil,
	"sqlite":   nil,
}

// pgSkippedStatementExceptions are for a PostgreSQL fixture holding statements
// the file parser skips with a warning: CREATE VIEW and CREATE TABLE …
// PARTITION OF. A test that passes them checks the warnings itself.
var pgSkippedStatementExceptions = []parityException{
	{Field: "Schema.Views", Reason: "the file parser skips CREATE VIEW; views come from introspection or input.views (PRD §16)"},
	{Field: "Schema.Warnings", Reason: "the file parser warns on each statement it skips; the introspector skips partitions without a word (PRD §5.2, §6.3)"},
}

// parityFieldTypes maps the type half of a parityException.Field to the type
// whose field it ignores.
var parityFieldTypes = map[string]any{
	"Schema":        parser.Schema{},
	"Table":         parser.Table{},
	"Column":        parser.Column{},
	"FKReference":   parser.FKReference{},
	"Constraint":    parser.Constraint{},
	"Enum":          parser.Enum{},
	"Set":           parser.Set{},
	"CompositeType": parser.CompositeType{},
	"Attribute":     parser.Attribute{},
	"DomainType":    parser.DomainType{},
	"View":          parser.View{},
}

// assertSchemaParity compares an introspected schema against the file
// parser's schema for the same DDL: tables, columns, constraints, enums, sets,
// composite types, domains and views, in full. The dialect's
// parityExceptions are the only fields left out, plus any ignore entries. It
// sorts both schemas in place first.
func assertSchemaParity(t *testing.T, dialect string, parsed, introspected *parser.Schema, ignore ...parityException) {
	t.Helper()
	exceptions, ok := parityExceptions[dialect]
	if !ok {
		t.Fatalf("assertSchemaParity: no parity exceptions for dialect %q", dialect)
	}

	all := slices.Concat(exceptions, ignore)
	opts := make([]cmp.Option, 0, len(all))
	for _, e := range all {
		typeName, field, ok := strings.Cut(e.Field, ".")
		typ, known := parityFieldTypes[typeName]
		if !ok || !known || e.Reason == "" {
			t.Fatalf("assertSchemaParity: malformed parity exception %+v", e)
		}
		opts = append(opts, cmpopts.IgnoreFields(typ, field))
	}

	sortForParity(parsed)
	sortForParity(introspected)
	if diff := cmp.Diff(parsed, introspected, opts...); diff != "" {
		t.Errorf("%s: introspected schema differs from the file parser's (-parsed +introspected):\n%s", dialect, diff)
	}
}

// sortForParity orders everything whose order carries no meaning. Columns,
// constraint columns and enum values keep their order.
func sortForParity(s *parser.Schema) {
	s.Sort()
	slices.Sort(s.Warnings)
	for i := range s.Tables {
		sortConstraints(s.Tables[i].Constraints)
	}
	for i := range s.DomainTypes {
		sortConstraints(s.DomainTypes[i].Constraints)
	}
}

func sortConstraints(cs []parser.Constraint) {
	slices.SortFunc(cs, func(a, b parser.Constraint) int {
		if a.Type != b.Type {
			return int(a.Type) - int(b.Type)
		}
		if c := strings.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		return slices.Compare(a.Columns, b.Columns)
	})
}

var (
	pgConnStr    string
	mysqlConnStr string
	sqliteDBPath string
)

func TestMain(m *testing.M) {
	flag.Parse()
	// The white-box unit tests share this binary, so -short still runs them;
	// each integration test skips itself through requireDatabases.
	if testing.Short() {
		os.Exit(m.Run())
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
	requireDatabases(t)

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

	// Parse the same DDL.
	p := postgres.New("introspect_test")
	if err := p.Parse("test.sql", []byte(ddl)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	assertSchemaParity(t, "postgres", p.Schema(), introspected)
}

// --- MySQL integration tests ---

func TestMySQLIntrospect(t *testing.T) {
	requireDatabases(t)

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
	err = intro.Introspect(ctx, mysqlConnStr, introspected, introspect.IntrospectionOptions{
		Tables: []string{"users", "posts"},
	})
	if err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}

	// Parse the same DDL.
	p, err := mysql.New()
	if err != nil {
		t.Fatalf("creating mysql parser: %v", err)
	}
	if err := p.Parse("test.sql", []byte(ddl)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	assertSchemaParity(t, "mysql", p.Schema(), introspected)
}

// --- SQLite integration tests ---

func TestSQLiteIntrospect(t *testing.T) {
	requireDatabases(t)

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

	// Parse the same DDL.
	p := sqlite.New()
	if err := p.Parse("test.sql", []byte(ddl)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	assertSchemaParity(t, "sqlite", p.Schema(), introspected)
}

// --- Generated column tests ---

// generatedColumn is the generated-column shape one column must have.
type generatedColumn struct {
	expr, storage string
}

// assertGeneratedColumns checks the generated-column fields of every column
// in table, so parity cannot pass with both sides missing them. A column
// absent from want must not be generated.
func assertGeneratedColumns(t *testing.T, table *parser.Table, want map[string]generatedColumn) {
	t.Helper()
	got := make(map[string]generatedColumn)
	for _, c := range table.Columns {
		if c.GeneratedExpr != "" || c.GeneratedStorage != "" {
			got[c.Name] = generatedColumn{expr: c.GeneratedExpr, storage: c.GeneratedStorage}
		}
		if c.GeneratedExpr != "" && c.Default != "" {
			t.Errorf("%s.%s: Default = %q on a generated column, want empty", table.Name, c.Name, c.Default)
		}
	}
	if diff := cmp.Diff(want, got, cmp.AllowUnexported(generatedColumn{})); diff != "" {
		t.Errorf("%s generated columns mismatch (-want +got):\n%s", table.Name, diff)
	}
}

func TestPostgresIntrospect_GeneratedColumn(t *testing.T) {
	requireDatabases(t)

	ctx := context.Background()

	ddl := `
		CREATE TABLE gen_items (
			id integer PRIMARY KEY,
			qty integer NOT NULL,
			name text,
			price numeric,
			code varchar(10),
			doubled integer GENERATED ALWAYS AS (qty * 2) STORED,
			shout text NOT NULL GENERATED ALWAYS AS (upper(name)) STORED,
			total numeric GENERATED ALWAYS AS (price * 2) STORED,
			loud text GENERATED ALWAYS AS (upper(code) || '!') STORED,
			seq integer GENERATED ALWAYS AS IDENTITY
		);
	`

	db, err := sql.Open("pgx", pgConnStr)
	if err != nil {
		t.Fatalf("opening postgres: %v", err)
	}
	defer db.Close() //nolint:errcheck // test cleanup

	if _, err := db.ExecContext(ctx, "DROP SCHEMA IF EXISTS introspect_gen CASCADE; CREATE SCHEMA introspect_gen"); err != nil {
		t.Fatalf("creating schema: %v", err)
	}
	defer db.ExecContext(ctx, "DROP SCHEMA IF EXISTS introspect_gen CASCADE") //nolint:errcheck // test cleanup
	if _, err := db.ExecContext(ctx, "SET search_path TO introspect_gen; "+ddl); err != nil {
		t.Fatalf("applying DDL: %v", err)
	}

	intro := introspect.NewPostgresIntrospector()
	defer intro.Close() //nolint:errcheck // test cleanup

	introspected := &parser.Schema{}
	if err := intro.Introspect(ctx, pgConnStr, introspected, introspect.IntrospectionOptions{
		Schemas: []string{"introspect_gen"},
	}); err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}

	p := postgres.New("introspect_gen")
	if err := p.Parse("test.sql", []byte(ddl)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	want := map[string]generatedColumn{
		"doubled": {expr: "qty * 2", storage: "STORED"},
		"shout":   {expr: "upper(name)", storage: "STORED"},
		"total":   {expr: "price * 2", storage: "STORED"},
		"loud":    {expr: "upper(code) || '!'", storage: "STORED"},
	}
	assertGeneratedColumns(t, findTable(t, introspected, "gen_items"), want)
	assertGeneratedColumns(t, findTable(t, p.Schema(), "gen_items"), want)
	assertSchemaParity(t, "postgres", p.Schema(), introspected)
}

// TestPostgresIntrospect_VirtualGeneratedColumn needs PostgreSQL 18, the first
// release with VIRTUAL generated columns, so it starts its own container. The
// file parser's PostgreSQL 17 grammar rejects the syntax, so there is no
// parsed schema to compare against.
func TestPostgresIntrospect_VirtualGeneratedColumn(t *testing.T) {
	requireDatabases(t)

	ctx := context.Background()

	container, err := tcpostgres.Run(
		ctx,
		"postgres:18-alpine",
		tcpostgres.WithDatabase("sqlgen_test"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2),
		),
	)
	if err != nil {
		t.Fatalf("starting postgres 18 container: %v", err)
	}
	defer container.Terminate(ctx) //nolint:errcheck // test cleanup

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("postgres 18 connection string: %v", err)
	}

	db, err := sql.Open("pgx", connStr)
	if err != nil {
		t.Fatalf("opening postgres: %v", err)
	}
	defer db.Close() //nolint:errcheck // test cleanup

	// With neither STORED nor VIRTUAL, PostgreSQL 18 makes the column VIRTUAL.
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE gen_items (
			id integer PRIMARY KEY,
			qty integer NOT NULL,
			implicit_total integer GENERATED ALWAYS AS (qty * 2),
			virtual_total integer GENERATED ALWAYS AS (qty * 3) VIRTUAL,
			stored_total integer GENERATED ALWAYS AS (qty * 4) STORED
		)`); err != nil {
		t.Fatalf("applying DDL: %v", err)
	}

	intro := introspect.NewPostgresIntrospector()
	defer intro.Close() //nolint:errcheck // test cleanup

	introspected := &parser.Schema{}
	if err := intro.Introspect(ctx, connStr, introspected, introspect.IntrospectionOptions{
		Schemas: []string{"public"},
	}); err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}

	assertGeneratedColumns(t, findTable(t, introspected, "gen_items"), map[string]generatedColumn{
		"implicit_total": {expr: "qty * 2", storage: "VIRTUAL"},
		"virtual_total":  {expr: "qty * 3", storage: "VIRTUAL"},
		"stored_total":   {expr: "qty * 4", storage: "STORED"},
	})
}

func TestMySQLIntrospect_GeneratedColumn(t *testing.T) {
	requireDatabases(t)

	ctx := context.Background()

	ddl := `
		CREATE TABLE gen_items (
			id int NOT NULL PRIMARY KEY,
			qty int NOT NULL,
			stored_total int GENERATED ALWAYS AS (qty * 2) STORED,
			virtual_total int GENERATED ALWAYS AS (qty * 2) VIRTUAL,
			default_total int GENERATED ALWAYS AS (qty + 1),
			short_total int AS (qty + 2),
			abs_qty int AS (abs(qty)) STORED NOT NULL,
			label varchar(20) AS (concat('n-', qty))
		);
	`

	db, err := sql.Open("mysql", mysqlConnStr)
	if err != nil {
		t.Fatalf("opening mysql: %v", err)
	}
	defer db.Close() //nolint:errcheck // test cleanup

	if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS gen_items"); err != nil {
		t.Fatalf("dropping table: %v", err)
	}
	defer db.ExecContext(ctx, "DROP TABLE IF EXISTS gen_items") //nolint:errcheck // test cleanup
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		t.Fatalf("applying DDL: %v", err)
	}

	intro := introspect.NewMySQLIntrospector()
	defer intro.Close() //nolint:errcheck // test cleanup

	introspected := &parser.Schema{}
	if err := intro.Introspect(ctx, mysqlConnStr, introspected, introspect.IntrospectionOptions{
		Tables: []string{"gen_items"},
	}); err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}

	p, err := mysql.New()
	if err != nil {
		t.Fatalf("creating mysql parser: %v", err)
	}
	if err := p.Parse("test.sql", []byte(ddl)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	want := map[string]generatedColumn{
		"stored_total":  {expr: "qty * 2", storage: "STORED"},
		"virtual_total": {expr: "qty * 2", storage: "VIRTUAL"},
		"default_total": {expr: "qty + 1", storage: "VIRTUAL"},
		"short_total":   {expr: "qty + 2", storage: "VIRTUAL"},
		"abs_qty":       {expr: "abs(qty)", storage: "STORED"},
		"label":         {expr: "concat('n-', qty)", storage: "VIRTUAL"},
	}
	assertGeneratedColumns(t, findTable(t, introspected, "gen_items"), want)
	assertGeneratedColumns(t, findTable(t, p.Schema(), "gen_items"), want)
	assertSchemaParity(t, "mysql", p.Schema(), introspected)
}

func TestSQLiteIntrospect_GeneratedColumn(t *testing.T) {
	requireDatabases(t)

	ctx := context.Background()

	ddl := `
		CREATE TABLE gen_items (
			id integer PRIMARY KEY,
			qty integer NOT NULL,
			stored_total integer GENERATED ALWAYS AS (qty * 2) STORED,
			virtual_total integer GENERATED ALWAYS AS (qty * 2) VIRTUAL,
			short_total integer AS (qty + 1),
			abs_qty integer AS (abs(qty)) STORED NOT NULL
		);
	`

	dbPath := filepath.Join(t.TempDir(), "generated.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("opening sqlite: %v", err)
	}
	defer db.Close() //nolint:errcheck // test cleanup

	if _, err := db.ExecContext(ctx, ddl); err != nil {
		t.Fatalf("applying DDL: %v", err)
	}

	intro := introspect.NewSQLiteIntrospector()
	defer intro.Close() //nolint:errcheck // test cleanup

	introspected := &parser.Schema{}
	if err := intro.Introspect(ctx, dbPath, introspected, introspect.IntrospectionOptions{}); err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}

	p := sqlite.New()
	if err := p.Parse("test.sql", []byte(ddl)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	want := map[string]generatedColumn{
		"stored_total":  {expr: `"qty" * 2`, storage: "STORED"},
		"virtual_total": {expr: `"qty" * 2`, storage: "VIRTUAL"},
		"short_total":   {expr: `"qty" + 1`, storage: "VIRTUAL"},
		"abs_qty":       {expr: `abs("qty")`, storage: "STORED"},
	}
	assertGeneratedColumns(t, findTable(t, introspected, "gen_items"), want)
	assertGeneratedColumns(t, findTable(t, p.Schema(), "gen_items"), want)
	assertSchemaParity(t, "sqlite", p.Schema(), introspected)
}

// --- SQLite parity tests ---

// applySQLite applies ddl to a fresh database file, one statement at a time,
// and returns the file's path.
func applySQLite(t *testing.T, ddl string) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "schema.db")
	applyParityFixture(t, "sqlite", dbPath, "", ddl)
	return dbPath
}

func introspectSQLite(t *testing.T, dbPath string, opts introspect.IntrospectionOptions) *parser.Schema {
	t.Helper()
	intro := introspect.NewSQLiteIntrospector()
	t.Cleanup(func() { _ = intro.Close() })
	schema := &parser.Schema{}
	if err := intro.Introspect(context.Background(), dbPath, schema, opts); err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}
	return schema
}

func tableNames(s *parser.Schema) []string {
	names := make([]string, 0, len(s.Tables))
	for _, table := range s.Tables {
		names = append(names, table.Name)
	}
	return names
}

func columnNames(cols []parser.Column) []string {
	names := make([]string, 0, len(cols))
	for _, c := range cols {
		names = append(names, c.Name)
	}
	return names
}

// sqliteSchemas returns the file parser's and the introspector's schema for
// ddl.
func sqliteSchemas(t *testing.T, ddl string) (parsed, introspected *parser.Schema) {
	t.Helper()
	dbPath := applySQLite(t, ddl)
	introspected = introspectSQLite(t, dbPath, introspect.IntrospectionOptions{})
	return parseStatements(t, sqlite.New(), ddl), introspected
}

func TestSQLiteIntrospect_ParityFixture(t *testing.T) {
	requireDatabases(t)
	parsed, introspected := sqliteSchemas(t, sqliteParityTables)
	assertSchemaParity(t, "sqlite", parsed, introspected)
}

func TestSQLiteIntrospect_CompositePKOrder(t *testing.T) {
	requireDatabases(t)
	parsed, introspected := sqliteSchemas(t, `
		CREATE TABLE pairs (a INTEGER NOT NULL, b INTEGER NOT NULL, c TEXT, PRIMARY KEY (b, a));
		CREATE TABLE kv (k TEXT, ns TEXT, v TEXT, PRIMARY KEY (ns, k)) WITHOUT ROWID
	`)

	for table, want := range map[string][]string{"pairs": {"b", "a"}, "kv": {"ns", "k"}} {
		pk := filterConstraints(findTable(t, introspected, table).Constraints, parser.PrimaryKey)
		if len(pk) != 1 {
			t.Errorf("%s: introspected %d PRIMARY KEY constraints, want 1: %+v", table, len(pk), pk)
			continue
		}
		if diff := cmp.Diff(want, pk[0].Columns); diff != "" {
			t.Errorf("%s: introspected PRIMARY KEY columns mismatch (-want +got):\n%s", table, diff)
		}
	}
	assertSchemaParity(t, "sqlite", parsed, introspected)
}

// REFERENCES t with no column list is legal SQLite, but the file parser
// cannot read it, and the introspector reads every stored CREATE TABLE with
// the file parser. So both paths fail on it, loudly (PRD §6.3).
func TestSQLiteIntrospect_ImplicitFKTarget(t *testing.T) {
	requireDatabases(t)
	tests := []struct {
		name string
		ddl  string
	}{
		{
			name: "column REFERENCES",
			ddl:  `CREATE TABLE u (id INTEGER PRIMARY KEY); CREATE TABLE p (id INTEGER PRIMARY KEY, uid INTEGER REFERENCES u)`,
		},
		{
			name: "table FOREIGN KEY",
			ddl:  `CREATE TABLE u (id INTEGER PRIMARY KEY); CREATE TABLE p (id INTEGER PRIMARY KEY, uid INTEGER, FOREIGN KEY (uid) REFERENCES u)`,
		},
	}

	const parseError = "expected left paren"
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dbPath := applySQLite(t, tt.ddl)
			intro := introspect.NewSQLiteIntrospector()
			defer intro.Close() //nolint:errcheck // test cleanup
			err := intro.Introspect(context.Background(), dbPath, &parser.Schema{}, introspect.IntrospectionOptions{})
			if err == nil || !strings.Contains(err.Error(), "introspect sqlite table p:") || !strings.Contains(err.Error(), parseError) {
				t.Errorf("Introspect() error = %v, want the parse error for table p", err)
			}
			if _, ok := errors.AsType[*introspect.SchemaError](err); !ok {
				t.Errorf("Introspect() error = %v, want a *introspect.SchemaError", err)
			}

			p := sqlite.New()
			err = p.Parse("schema.sql", []byte(tt.ddl))
			if err == nil || !strings.Contains(err.Error(), parseError) {
				t.Errorf("Parse() error = %v, want %q", err, parseError)
			}
		})
	}
}

func TestSQLiteIntrospect_UniqueAndPKAutoIndexes(t *testing.T) {
	requireDatabases(t)
	parsed, introspected := sqliteSchemas(t, `
		CREATE TABLE tpk (id TEXT PRIMARY KEY, v INTEGER);
		CREATE TABLE ipk (id INT PRIMARY KEY, v INTEGER);
		CREATE TABLE dpk (id INTEGER PRIMARY KEY DESC, v INTEGER);
		CREATE TABLE cpk (a INTEGER, b INTEGER, PRIMARY KEY (a, b));
		CREATE TABLE u (
			id INTEGER PRIMARY KEY,
			a TEXT UNIQUE,
			b TEXT CONSTRAINT u_b_named UNIQUE,
			c TEXT,
			d TEXT,
			UNIQUE (c, d),
			CONSTRAINT u_dc_named UNIQUE (d, c)
		);
		CREATE UNIQUE INDEX u_c_idx ON u (c);
		CREATE TABLE collated (email TEXT, name TEXT, UNIQUE (email COLLATE NOCASE));
		CREATE UNIQUE INDEX collated_name_idx ON collated (name COLLATE NOCASE);
		CREATE TABLE coll_two (a TEXT, UNIQUE (a), UNIQUE (a COLLATE NOCASE));
		CREATE TABLE coll_key (id TEXT PRIMARY KEY, UNIQUE (id COLLATE NOCASE));
		CREATE TABLE coll_order (a TEXT, UNIQUE (a), UNIQUE (a DESC));
		CREATE TABLE coll_override (a TEXT COLLATE NOCASE UNIQUE, UNIQUE (a COLLATE BINARY));
		CREATE TABLE coll_same_key (id TEXT COLLATE NOCASE PRIMARY KEY, UNIQUE (id))
	`)

	for _, table := range introspected.Tables {
		for _, c := range table.Constraints {
			if strings.HasPrefix(c.Name, "sqlite_autoindex_") {
				t.Errorf("%s: constraint %+v carries an auto-index name", table.Name, c)
			}
		}
	}
	for _, table := range []string{"tpk", "ipk", "dpk", "cpk"} {
		if uniques := filterConstraints(findTable(t, introspected, table).Constraints, parser.Unique); len(uniques) != 0 {
			t.Errorf("%s: introspected UNIQUE constraints %+v, want none beside the primary key", table, uniques)
		}
	}
	wantU := []parser.Constraint{
		{Type: parser.Unique, Columns: []string{"a"}},
		{Type: parser.Unique, Columns: []string{"c", "d"}},
		{Name: "u_b_named", Type: parser.Unique, Columns: []string{"b"}},
		{Name: "u_c_idx", Type: parser.Unique, Columns: []string{"c"}, Method: "btree"},
		{Name: "u_dc_named", Type: parser.Unique, Columns: []string{"d", "c"}},
	}
	gotU := filterConstraints(findTable(t, introspected, "u").Constraints, parser.Unique)
	sortConstraints(gotU)
	if diff := cmp.Diff(wantU, gotU, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("u: introspected UNIQUE constraints mismatch (-want +got):\n%s", diff)
	}
	assertSchemaParity(t, "sqlite", parsed, introspected)
}

// A partial index's predicate comes from its stored CREATE INDEX, read with
// the file parser, so it is the parser's text and not the DDL as written
// (PRD §6.1, §6.3).
func TestSQLiteIntrospect_PartialIndexes(t *testing.T) {
	requireDatabases(t)
	parsed, introspected := sqliteSchemas(t, `
		CREATE TABLE t (a TEXT, b INTEGER, note TEXT);
		CREATE UNIQUE INDEX t_a_live ON t (a) WHERE a IS NOT NULL;
		CREATE INDEX t_b_pos ON t (b) where b>0 AND note<>'x WHERE y';
		CREATE INDEX t_note_idx ON t (note) /* WHERE b = 1 */ WHERE note IN ('a', 'b')
	`)

	want := map[string]string{
		"t_a_live":   `"a" NOT NULL`,
		"t_b_pos":    `"b" > 0 AND "note" != 'x WHERE y'`,
		"t_note_idx": `"note" IN ('a', 'b')`,
	}
	got := make(map[string]string)
	for _, c := range findTable(t, introspected, "t").Constraints {
		got[c.Name] = c.Where
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("t: introspected partial-index predicates mismatch (-want +got):\n%s", diff)
	}
	assertSchemaParity(t, "sqlite", parsed, introspected)
}

// An index with an expression part is skipped under both sources, unique or
// not, and a part in parentheses names its column and keeps its collation
// (PRD §6.1, §30.7).
func TestSQLiteIntrospect_ExpressionIndexes(t *testing.T) {
	requireDatabases(t)
	parsed, introspected := sqliteSchemas(t, `
		CREATE TABLE t (a TEXT, b TEXT, email TEXT, UNIQUE ((email)));
		CREATE UNIQUE INDEX t_lower_email ON t (lower(email));
		CREATE UNIQUE INDEX t_a_b1 ON t (a, b + 1);
		CREATE INDEX t_a_lower_b ON t (a, lower(b));
		CREATE INDEX t_lower_a ON t (lower(a)) WHERE b IS NOT NULL;
		CREATE INDEX t_b_a ON t (b COLLATE NOCASE, (a));
		CREATE TABLE u (a TEXT, UNIQUE (a), UNIQUE ((a COLLATE NOCASE)))
	`)

	want := []parser.Constraint{
		{Type: parser.Unique, Columns: []string{"email"}},
		{Name: "t_b_a", Type: parser.Index, Columns: []string{"b", "a"}, Method: "btree"},
	}
	got := findTable(t, introspected, "t").Constraints
	sortConstraints(got)
	if diff := cmp.Diff(want, got, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("t: introspected constraints mismatch (-want +got):\n%s", diff)
	}
	assertSchemaParity(t, "sqlite", parsed, introspected)
}

// The file parser cannot read IS [NOT] DISTINCT FROM and cannot render
// LIKE … ESCAPE, so a partial index using either fails both schema sources
// with an error, not a panic (PRD §6.3).
func TestSQLiteIntrospect_UnreadablePartialIndex(t *testing.T) {
	requireDatabases(t)
	tests := []struct {
		name       string
		ddl        string
		parseError string
	}{
		{
			name:       "IS NOT DISTINCT FROM",
			ddl:        `CREATE TABLE t (a TEXT, b TEXT); CREATE INDEX t_ab ON t (a) WHERE a IS NOT DISTINCT FROM b`,
			parseError: "expected expression, found 'DISTINCT'",
		},
		{
			name:       "LIKE ESCAPE",
			ddl:        `CREATE TABLE t (a TEXT, b TEXT); CREATE INDEX t_ab ON t (a) WHERE a LIKE 'x%' ESCAPE '\'`,
			parseError: "rendering CREATE INDEX predicate: sql.BinaryExpr.String(): invalid op ESCAPE",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dbPath := applySQLite(t, tt.ddl)
			intro := introspect.NewSQLiteIntrospector()
			defer intro.Close() //nolint:errcheck // test cleanup
			err := intro.Introspect(context.Background(), dbPath, &parser.Schema{}, introspect.IntrospectionOptions{})
			if err == nil || !strings.Contains(err.Error(), "introspect sqlite index t_ab:") || !strings.Contains(err.Error(), tt.parseError) {
				t.Errorf("Introspect() error = %v, want the parse error for index t_ab", err)
			}
			if _, ok := errors.AsType[*introspect.SchemaError](err); !ok {
				t.Errorf("Introspect() error = %v, want a *introspect.SchemaError", err)
			}

			p := sqlite.New()
			err = p.Parse("schema.sql", []byte(tt.ddl))
			if err == nil || !strings.Contains(err.Error(), tt.parseError) {
				t.Errorf("Parse() error = %v, want %q", err, tt.parseError)
			}
		})
	}
}

// The columns come from PRAGMA table_xinfo, but the stored CREATE TABLE is
// still read with the file parser, so DDL it cannot read fails both sources.
func TestSQLiteIntrospect_UnreadableTable(t *testing.T) {
	requireDatabases(t)
	tests := []struct {
		name       string
		ddl        string
		parseError string
	}{
		{
			name:       "quoted type name",
			ddl:        `CREATE TABLE t (id INTEGER PRIMARY KEY, f "my type")`,
			parseError: `"my type" after column f is not a column`,
		},
		{
			name:       "MATCH on a foreign key",
			ddl:        `CREATE TABLE p (id INTEGER PRIMARY KEY); CREATE TABLE t (id INTEGER PRIMARY KEY, pid INTEGER REFERENCES p(id) MATCH SIMPLE)`,
			parseError: `"MATCH" after column pid is not a column`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dbPath := applySQLite(t, tt.ddl)
			intro := introspect.NewSQLiteIntrospector()
			defer intro.Close() //nolint:errcheck // test cleanup
			err := intro.Introspect(context.Background(), dbPath, &parser.Schema{}, introspect.IntrospectionOptions{})
			if err == nil || !strings.Contains(err.Error(), "introspect sqlite table t:") || !strings.Contains(err.Error(), tt.parseError) {
				t.Errorf("Introspect() error = %v, want the parse error for table t", err)
			}
			if _, ok := errors.AsType[*introspect.SchemaError](err); !ok {
				t.Errorf("Introspect() error = %v, want a *introspect.SchemaError", err)
			}

			p := sqlite.New()
			err = p.Parse("schema.sql", []byte(tt.ddl))
			if err == nil || !strings.Contains(err.Error(), tt.parseError) {
				t.Errorf("Parse() error = %v, want %q", err, tt.parseError)
			}
		})
	}
}

func TestSQLiteIntrospect_VirtualTables(t *testing.T) {
	requireDatabases(t)
	ddl := `
		CREATE TABLE docs (id INTEGER PRIMARY KEY, body TEXT);
		CREATE VIRTUAL TABLE docs_fts USING fts5(body);
		CREATE VIRTUAL TABLE boxes USING rtree(id, minx, maxx)
	`
	dbPath := applySQLite(t, ddl)
	introspected := introspectSQLite(t, dbPath, introspect.IntrospectionOptions{})
	parsed := parseStatements(t, sqlite.New(), ddl)

	if diff := cmp.Diff([]string{"docs"}, tableNames(introspected)); diff != "" {
		t.Errorf("introspected tables mismatch (-want +got):\n%s", diff)
	}
	wantWarnings := []string{
		"CREATE VIRTUAL TABLE boxes skipped — virtual tables are not generated",
		"CREATE VIRTUAL TABLE docs_fts skipped — virtual tables are not generated",
	}
	if diff := cmp.Diff(wantWarnings, introspected.Warnings); diff != "" {
		t.Errorf("introspected warnings mismatch (-want +got):\n%s", diff)
	}
	assertSchemaParity(t, "sqlite", parsed, introspected)

	// A virtual table the table filters leave out is not warned about.
	filtered := introspectSQLite(t, dbPath, introspect.IntrospectionOptions{ExcludeTables: []string{"boxes"}})
	if diff := cmp.Diff(wantWarnings[1:], filtered.Warnings); diff != "" {
		t.Errorf("filtered warnings mismatch (-want +got):\n%s", diff)
	}
}

func TestSQLiteIntrospect_QuotedNames(t *testing.T) {
	requireDatabases(t)
	ddl := `
		CREATE TABLE "Quoted""Name" (id INTEGER PRIMARY KEY, "weird col" TEXT, "MixedCase" TEXT UNIQUE, "say ""hi""" TEXT CHECK ("say ""hi""" <> ''));
		CREATE INDEX "idx ""q""" ON "Quoted""Name" ("weird col");
		CREATE TABLE "Child Table" (id INTEGER PRIMARY KEY, qid INTEGER REFERENCES "Quoted""Name"(id));
		CREATE TABLE "back\slash" (id TEXT PRIMARY KEY)
	`
	const view = `v "q"`
	dbPath := applySQLite(t, ddl+`; CREATE VIEW "v ""q""" AS SELECT id, "weird col" FROM "Quoted""Name"`)
	introspected := introspectSQLite(t, dbPath, introspect.IntrospectionOptions{ExcludeTables: []string{view}})
	parsed := parseStatements(t, sqlite.New(), ddl)

	if diff := cmp.Diff([]string{"Child Table", `Quoted"Name`, `back\slash`}, tableNames(introspected)); diff != "" {
		t.Errorf("introspected tables mismatch (-want +got):\n%s", diff)
	}
	assertSchemaParity(t, "sqlite", parsed, introspected)

	views := introspectSQLite(t, dbPath, introspect.IntrospectionOptions{Tables: []string{view}}).Views
	if len(views) != 1 {
		t.Fatalf("introspected %d views, want 1: %+v", len(views), views)
	}
	if diff := cmp.Diff([]string{"id", "weird col"}, columnNames(views[0].Columns)); diff != "" {
		t.Errorf("view %s columns mismatch (-want +got):\n%s", view, diff)
	}
}

// Only SQLite's own sqlite_ objects are left out; a user table or view whose
// name merely starts with "sqlite" is introspected.
func TestSQLiteIntrospect_ReservedPrefix(t *testing.T) {
	requireDatabases(t)
	ddl := `
		CREATE TABLE sqlitex (id INTEGER PRIMARY KEY, v TEXT);
		CREATE TABLE sqlite1 (id INTEGER PRIMARY KEY);
		CREATE TABLE SQLiteZ (id INTEGER PRIMARY KEY);
		CREATE TABLE seq (id INTEGER PRIMARY KEY AUTOINCREMENT)
	`
	const view = "sqlite1v"
	dbPath := applySQLite(t, ddl+`; CREATE VIEW sqlite1v AS SELECT id, v FROM sqlitex; ANALYZE`)
	introspected := introspectSQLite(t, dbPath, introspect.IntrospectionOptions{ExcludeTables: []string{view}})
	parsed := parseStatements(t, sqlite.New(), ddl)

	if diff := cmp.Diff([]string{"SQLiteZ", "seq", "sqlite1", "sqlitex"}, tableNames(introspected)); diff != "" {
		t.Errorf("introspected tables mismatch (-want +got):\n%s", diff)
	}
	assertSchemaParity(t, "sqlite", parsed, introspected)

	views := introspectSQLite(t, dbPath, introspect.IntrospectionOptions{}).Views
	if len(views) != 1 || views[0].Name != view {
		t.Errorf("introspected views %+v, want only %s", views, view)
	}
}

func TestSQLiteIntrospect_Checks(t *testing.T) {
	requireDatabases(t)
	parsed, introspected := sqliteSchemas(t, `
		CREATE TABLE c (
			id INTEGER PRIMARY KEY,
			qty INTEGER CHECK (qty >= 0),
			price REAL CONSTRAINT c_price_positive CHECK (price > 0),
			lo INTEGER,
			hi INTEGER,
			CHECK (lo <= hi),
			CONSTRAINT c_always CHECK (1 = 1)
		)
	`)

	want := []parser.Constraint{
		{Type: parser.Check, Columns: []string{"lo", "hi"}, CheckExpression: `"lo" <= "hi"`},
		{Type: parser.Check, Columns: []string{"qty"}, CheckExpression: `"qty" >= 0`},
		{Name: "c_always", Type: parser.Check, CheckExpression: `1 = 1`},
		{Name: "c_price_positive", Type: parser.Check, Columns: []string{"price"}, CheckExpression: `"price" > 0`},
	}
	got := filterConstraints(findTable(t, introspected, "c").Constraints, parser.Check)
	sortConstraints(got)
	if diff := cmp.Diff(want, got, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("introspected CHECK constraints mismatch (-want +got):\n%s", diff)
	}
	assertSchemaParity(t, "sqlite", parsed, introspected)
}

// insertOutcome is what SQLite does with an INSERT that leaves the key out.
type insertOutcome string

const (
	keyAssigned insertOutcome = "assigned"
	keyNull     insertOutcome = "null"
	keyRejected insertOutcome = "rejected"
)

func TestSQLiteIntrospect_RowidAlias(t *testing.T) {
	requireDatabases(t)
	tests := []struct {
		name        string
		ddl         string
		wantAutoInc bool
		wantInsert  insertOutcome
	}{
		{name: "INTEGER PRIMARY KEY", ddl: `CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)`, wantAutoInc: true, wantInsert: keyAssigned},
		{name: "ascending key", ddl: `CREATE TABLE t (id INTEGER PRIMARY KEY ASC, v TEXT)`, wantAutoInc: true, wantInsert: keyAssigned},
		{name: "AUTOINCREMENT", ddl: `CREATE TABLE t (id INTEGER PRIMARY KEY AUTOINCREMENT, v TEXT)`, wantAutoInc: true, wantInsert: keyAssigned},
		{name: "table-level key", ddl: `CREATE TABLE t (id INTEGER, v TEXT, PRIMARY KEY (id))`, wantAutoInc: true, wantInsert: keyAssigned},
		{name: "WITHOUT ROWID", ddl: `CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT) WITHOUT ROWID`, wantInsert: keyRejected},
		{name: "descending key", ddl: `CREATE TABLE t (id INTEGER PRIMARY KEY DESC, v TEXT)`, wantInsert: keyNull},
		{name: "INT PRIMARY KEY", ddl: `CREATE TABLE t (id INT PRIMARY KEY, v TEXT)`, wantInsert: keyNull},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dbPath := applySQLite(t, tt.ddl)
			introspected := introspectSQLite(t, dbPath, introspect.IntrospectionOptions{})
			parsed := parseStatements(t, sqlite.New(), tt.ddl)

			for source, schema := range map[string]*parser.Schema{"parsed": parsed, "introspected": introspected} {
				if got := parser.ColumnByName(findTable(t, schema, "t"), "id").AutoIncrement; got != tt.wantAutoInc {
					t.Errorf("%s: id AutoIncrement = %t, want %t", source, got, tt.wantAutoInc)
				}
			}
			if got := sqliteInsertWithoutKey(t, dbPath); got != tt.wantInsert {
				t.Errorf("INSERT without id: %s, want %s", got, tt.wantInsert)
			}
			assertSchemaParity(t, "sqlite", parsed, introspected)
		})
	}
}

// sqliteInsertWithoutKey inserts a row into t that leaves id out and reports
// what SQLite did with the key.
func sqliteInsertWithoutKey(t *testing.T, dbPath string) insertOutcome {
	t.Helper()
	ctx := context.Background()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("opening sqlite: %v", err)
	}
	defer db.Close() //nolint:errcheck // test cleanup
	if _, err := db.ExecContext(ctx, `INSERT INTO t (v) VALUES ('x')`); err != nil {
		return keyRejected
	}
	var id sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT id FROM t`).Scan(&id); err != nil {
		t.Fatalf("reading id: %v", err)
	}
	if !id.Valid {
		return keyNull
	}
	return keyAssigned
}

func TestSQLiteIntrospect_TableFlags(t *testing.T) {
	requireDatabases(t)
	parsed, introspected := sqliteSchemas(t, `
		CREATE TABLE plain (id INTEGER PRIMARY KEY, v TEXT);
		CREATE TABLE wr (id INTEGER PRIMARY KEY, v TEXT) WITHOUT ROWID;
		CREATE TABLE st (id INTEGER PRIMARY KEY, v TEXT) STRICT;
		CREATE TABLE both_flags (id INTEGER PRIMARY KEY, v TEXT) STRICT, WITHOUT ROWID
	`)

	type flags struct{ WithoutRowID, Strict bool }
	want := map[string]flags{
		"plain":      {},
		"wr":         {WithoutRowID: true},
		"st":         {Strict: true},
		"both_flags": {WithoutRowID: true, Strict: true},
	}
	for name, w := range want {
		table := findTable(t, introspected, name)
		if got := (flags{WithoutRowID: table.WithoutRowID, Strict: table.Strict}); got != w {
			t.Errorf("%s: introspected flags %+v, want %+v", name, got, w)
		}
	}
	assertSchemaParity(t, "sqlite", parsed, introspected)
}

func TestSQLiteIntrospect_TypeCoverage(t *testing.T) {
	requireDatabases(t)
	parsed, introspected := sqliteSchemas(t, `
		CREATE TABLE types (
			c_integer INTEGER,
			c_text TEXT,
			c_real REAL,
			c_blob BLOB,
			c_numeric NUMERIC,
			c_varchar VARCHAR(255),
			c_boolean BOOLEAN,
			c_datetime DATETIME,
			c_untyped,
			c_decimal DECIMAL(10, 2),
			c_uuid UUID
		)
	`)

	want := map[string]string{
		"c_integer":  "integer",
		"c_text":     "text",
		"c_real":     "real",
		"c_blob":     "blob",
		"c_numeric":  "numeric",
		"c_varchar":  "varchar(255)",
		"c_boolean":  "boolean",
		"c_datetime": "datetime",
		"c_untyped":  "blob",
		"c_decimal":  "decimal(10,2)",
		"c_uuid":     "blob",
	}
	for source, schema := range map[string]*parser.Schema{"parsed": parsed, "introspected": introspected} {
		got := make(map[string]string)
		for _, c := range findTable(t, schema, "types").Columns {
			got[c.Name] = c.Type
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("%s column types mismatch (-want +got):\n%s", source, diff)
		}
	}
	assertSchemaParity(t, "sqlite", parsed, introspected)
}

func TestSQLiteIntrospect_Defaults(t *testing.T) {
	requireDatabases(t)
	ddl := `
		CREATE TABLE d (
			id INTEGER PRIMARY KEY,
			s TEXT DEFAULT 'it''s',
			n REAL DEFAULT 1.5,
			neg INTEGER DEFAULT -1,
			ts DATETIME DEFAULT CURRENT_TIMESTAMP,
			expr INTEGER DEFAULT (1 + 2),
			fn TEXT DEFAULT (lower('X')),
			nul TEXT DEFAULT NULL
		)
	`
	dbPath := applySQLite(t, ddl)
	introspected := introspectSQLite(t, dbPath, introspect.IntrospectionOptions{})
	parsed := parseStatements(t, sqlite.New(), ddl)

	want := map[string]string{
		"id":   "",
		"s":    `'it''s'`,
		"n":    "1.5",
		"neg":  "-1",
		"ts":   "CURRENT_TIMESTAMP",
		"expr": "1 + 2",
		"fn":   "lower('X')",
		"nul":  "NULL",
	}
	got := make(map[string]string)
	for _, c := range findTable(t, introspected, "d").Columns {
		got[c.Name] = c.Default
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("introspected defaults mismatch (-want +got):\n%s", diff)
	}

	// The generator inlines a SQLite default into SQL, so each must be a valid
	// expression on its own.
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("opening sqlite: %v", err)
	}
	defer db.Close() //nolint:errcheck // test cleanup
	for name, dflt := range got {
		if dflt == "" {
			continue
		}
		var v any
		if err := db.QueryRowContext(context.Background(), "SELECT "+dflt).Scan(&v); err != nil {
			t.Errorf("%s: default %q is not a valid expression: %v", name, dflt, err)
		}
	}
	assertSchemaParity(t, "sqlite", parsed, introspected)
}

func TestSQLiteIntrospect_CompositeConstraints(t *testing.T) {
	requireDatabases(t)
	parsed, introspected := sqliteSchemas(t, `
		CREATE TABLE parent (a INTEGER NOT NULL, b INTEGER NOT NULL, name TEXT, PRIMARY KEY (a, b), UNIQUE (name, a));
		CREATE TABLE child (
			id INTEGER PRIMARY KEY,
			pa INTEGER,
			pb INTEGER,
			parent_id INTEGER REFERENCES child(id),
			CONSTRAINT child_parent_fk FOREIGN KEY (pb, pa) REFERENCES parent (b, a)
		)
	`)

	want := map[string][]parser.Constraint{
		"parent": {
			{Type: parser.PrimaryKey, Columns: []string{"a", "b"}},
			{Type: parser.Unique, Columns: []string{"name", "a"}},
		},
		"child": {
			{Type: parser.PrimaryKey, Columns: []string{"id"}},
			{Type: parser.ForeignKey, Columns: []string{"parent_id"}, ReferenceTable: "child", ReferenceColumns: []string{"id"}},
			{Name: "child_parent_fk", Type: parser.ForeignKey, Columns: []string{"pb", "pa"}, ReferenceTable: "parent", ReferenceColumns: []string{"b", "a"}},
		},
	}
	for name, w := range want {
		got := slices.Clone(findTable(t, introspected, name).Constraints)
		sortConstraints(got)
		if diff := cmp.Diff(w, got, cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("%s: introspected constraints mismatch (-want +got):\n%s", name, diff)
		}
	}
	assertSchemaParity(t, "sqlite", parsed, introspected)
}

// Several foreign keys over the same columns to the same parent keep their
// declared names and declaration order, and the column's FKReference is the
// first declared, as the file parser reads them. PRAGMA foreign_key_list
// numbers them last-declared first.
func TestSQLiteIntrospect_SameColumnForeignKeys(t *testing.T) {
	requireDatabases(t)
	const parent = "CREATE TABLE p (x INTEGER UNIQUE, y INTEGER UNIQUE, UNIQUE (x, y), UNIQUE (y, x));\n"
	tests := []struct {
		name    string
		ddl     string
		wantFKs []parser.Constraint
		wantRef *parser.FKReference
	}{
		{
			name: "both named",
			ddl:  "CREATE TABLE t (id INTEGER PRIMARY KEY, a INTEGER, CONSTRAINT fa FOREIGN KEY (a) REFERENCES p(x), CONSTRAINT fb FOREIGN KEY (a) REFERENCES p(y))",
			wantFKs: []parser.Constraint{
				{Name: "fa", Type: parser.ForeignKey, Columns: []string{"a"}, ReferenceTable: "p", ReferenceColumns: []string{"x"}},
				{Name: "fb", Type: parser.ForeignKey, Columns: []string{"a"}, ReferenceTable: "p", ReferenceColumns: []string{"y"}},
			},
			wantRef: &parser.FKReference{Table: "p", Column: "x"},
		},
		{
			name: "only the first named",
			ddl:  "CREATE TABLE t (id INTEGER PRIMARY KEY, a INTEGER, CONSTRAINT fa FOREIGN KEY (a) REFERENCES p(x), FOREIGN KEY (a) REFERENCES p(y))",
			wantFKs: []parser.Constraint{
				{Name: "fa", Type: parser.ForeignKey, Columns: []string{"a"}, ReferenceTable: "p", ReferenceColumns: []string{"x"}},
				{Type: parser.ForeignKey, Columns: []string{"a"}, ReferenceTable: "p", ReferenceColumns: []string{"y"}},
			},
			wantRef: &parser.FKReference{Table: "p", Column: "x"},
		},
		{
			name: "neither named",
			ddl:  "CREATE TABLE t (id INTEGER PRIMARY KEY, a INTEGER, FOREIGN KEY (a) REFERENCES p(x), FOREIGN KEY (a) REFERENCES p(y))",
			wantFKs: []parser.Constraint{
				{Type: parser.ForeignKey, Columns: []string{"a"}, ReferenceTable: "p", ReferenceColumns: []string{"x"}},
				{Type: parser.ForeignKey, Columns: []string{"a"}, ReferenceTable: "p", ReferenceColumns: []string{"y"}},
			},
			wantRef: &parser.FKReference{Table: "p", Column: "x"},
		},
		{
			name: "inline then table level",
			ddl:  "CREATE TABLE t (id INTEGER PRIMARY KEY, a INTEGER CONSTRAINT fa REFERENCES p(x), CONSTRAINT fb FOREIGN KEY (a) REFERENCES p(y))",
			wantFKs: []parser.Constraint{
				{Name: "fa", Type: parser.ForeignKey, Columns: []string{"a"}, ReferenceTable: "p", ReferenceColumns: []string{"x"}},
				{Name: "fb", Type: parser.ForeignKey, Columns: []string{"a"}, ReferenceTable: "p", ReferenceColumns: []string{"y"}},
			},
			wantRef: &parser.FKReference{Table: "p", Column: "x"},
		},
		{
			name: "composite",
			ddl:  "CREATE TABLE t (id INTEGER PRIMARY KEY, a INTEGER, b INTEGER, CONSTRAINT fa FOREIGN KEY (a, b) REFERENCES p(x, y), CONSTRAINT fb FOREIGN KEY (a, b) REFERENCES p(y, x))",
			wantFKs: []parser.Constraint{
				{Name: "fa", Type: parser.ForeignKey, Columns: []string{"a", "b"}, ReferenceTable: "p", ReferenceColumns: []string{"x", "y"}},
				{Name: "fb", Type: parser.ForeignKey, Columns: []string{"a", "b"}, ReferenceTable: "p", ReferenceColumns: []string{"y", "x"}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, introspected := sqliteSchemas(t, parent+tt.ddl)
			table := findTable(t, introspected, "t")
			if diff := cmp.Diff(tt.wantFKs, filterConstraints(table.Constraints, parser.ForeignKey)); diff != "" {
				t.Errorf("t: introspected foreign keys mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantRef, parser.ColumnByName(table, "a").FKReference); diff != "" {
				t.Errorf("t.a: introspected FKReference mismatch (-want +got):\n%s", diff)
			}
			assertSchemaParity(t, "sqlite", parsed, introspected)
		})
	}
}

// --- Filtering tests ---

func TestPostgresSchemaFiltering(t *testing.T) {
	requireDatabases(t)

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
	requireDatabases(t)

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

// bothModeTable is the table the both-mode tests create in each database.
const bothModeTable = "both_users"

// bothModeDialect is one dialect's setup for the both-mode tests.
type bothModeDialect struct {
	name string
	// schema is the schema a hand-built base table belongs to, the one the
	// parser resolves an unqualified name to.
	schema string
	// base applies ddl, which creates only bothModeTable, and returns the
	// introspected schema.
	base      func(t *testing.T, ddl string) *parser.Schema
	newParser func(t *testing.T) introspect.Seedable
}

var bothModeDialects = []bothModeDialect{
	{
		name:      "postgres",
		schema:    "both_mode",
		base:      postgresBothModeBase,
		newParser: func(*testing.T) introspect.Seedable { return postgres.New("both_mode") },
	},
	{
		name: "mysql",
		base: mysqlBothModeBase,
		newParser: func(t *testing.T) introspect.Seedable {
			p, err := mysql.New()
			if err != nil {
				t.Fatalf("mysql.New() error: %v", err)
			}
			return p
		},
	},
	{
		name:      "sqlite",
		base:      sqliteBothModeBase,
		newParser: func(*testing.T) introspect.Seedable { return sqlite.New() },
	},
}

func postgresBothModeBase(t *testing.T, ddl string) *parser.Schema {
	t.Helper()
	ctx := context.Background()
	conn := pgConnStr + "&search_path=both_mode"
	db, err := sql.Open("pgx", conn)
	if err != nil {
		t.Fatalf("opening postgres: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DROP SCHEMA IF EXISTS both_mode CASCADE")
		_ = db.Close()
	})
	for _, stmt := range []string{"DROP SCHEMA IF EXISTS both_mode CASCADE", "CREATE SCHEMA both_mode", ddl} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("applying %q: %v", truncate(stmt), err)
		}
	}

	intro := introspect.NewPostgresIntrospector()
	t.Cleanup(func() { _ = intro.Close() })
	schema := &parser.Schema{}
	if err := intro.Introspect(ctx, conn, schema, introspect.IntrospectionOptions{Schemas: []string{"both_mode"}}); err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}
	return schema
}

func mysqlBothModeBase(t *testing.T, ddl string) *parser.Schema {
	t.Helper()
	ctx := context.Background()
	db, err := sql.Open("mysql", mysqlConnStr)
	if err != nil {
		t.Fatalf("opening mysql: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS "+bothModeTable)
		_ = db.Close()
	})
	for _, stmt := range []string{"DROP TABLE IF EXISTS " + bothModeTable, ddl} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("applying %q: %v", truncate(stmt), err)
		}
	}

	intro := introspect.NewMySQLIntrospector()
	t.Cleanup(func() { _ = intro.Close() })
	schema := &parser.Schema{}
	if err := intro.Introspect(ctx, mysqlConnStr, schema, introspect.IntrospectionOptions{Tables: []string{bothModeTable}}); err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}
	return schema
}

func sqliteBothModeBase(t *testing.T, ddl string) *parser.Schema {
	t.Helper()
	return introspectSQLite(t, applySQLite(t, ddl), introspect.IntrospectionOptions{})
}

// writeMigration writes sql to a migration file and returns its path.
func writeMigration(t *testing.T, sql string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "001_migration.sql")
	if err := os.WriteFile(path, []byte(sql), 0o600); err != nil {
		t.Fatalf("writing migration: %v", err)
	}
	return path
}

func TestBothModeAlterOverlay(t *testing.T) {
	requireDatabases(t)

	for _, d := range bothModeDialects {
		t.Run(d.name, func(t *testing.T) {
			base := d.base(t, "CREATE TABLE "+bothModeTable+" (id integer PRIMARY KEY, name text NOT NULL)")
			migration := writeMigration(t, "ALTER TABLE "+bothModeTable+" ADD COLUMN email text;")

			p := d.newParser(t)
			if err := introspect.Merge(base, p, []string{migration}); err != nil {
				t.Fatalf("Merge() error: %v", err)
			}

			merged := p.Schema()
			if len(merged.Tables) != 1 {
				t.Fatalf("merged tables = %d, want 1", len(merged.Tables))
			}
			if diff := cmp.Diff([]string{"id", "name", "email"}, columnNames(merged.Tables[0].Columns)); diff != "" {
				t.Errorf("merged columns mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestBothModeCreateExisting(t *testing.T) {
	requireDatabases(t)

	for _, d := range bothModeDialects {
		t.Run(d.name, func(t *testing.T) {
			base := d.base(t, "CREATE TABLE "+bothModeTable+" (id integer PRIMARY KEY)")
			migration := writeMigration(t, "CREATE TABLE "+bothModeTable+" (id integer PRIMARY KEY, name text);")

			err := introspect.Merge(base, d.newParser(t), []string{migration})
			if err == nil {
				t.Fatal("Merge() expected error for CREATE TABLE on existing, got nil")
			}
			if got := err.Error(); !strings.Contains(got, "already exists") {
				t.Errorf("Merge() error = %q, want error containing 'already exists'", got)
			}
		})
	}
}

func TestBothModeColumnConflict(t *testing.T) {
	requireDatabases(t)

	// base is an introspected table, built by hand: the conflict rule needs no
	// database.
	base := func(schema string) *parser.Schema {
		return &parser.Schema{
			Tables: []parser.Table{
				{
					Name:   "items",
					Schema: schema,
					Columns: []parser.Column{
						{Name: "id", Type: "integer", PrimaryKey: true, AutoIncrement: true},
						{Name: "count", Type: "integer"},
					},
				},
			},
		}
	}

	for _, d := range bothModeDialects {
		t.Run(d.name, func(t *testing.T) {
			t.Run("DROP+ADD is explicit and allowed", func(t *testing.T) {
				migration := writeMigration(t, "ALTER TABLE items DROP COLUMN count;\nALTER TABLE items ADD COLUMN count text;")

				p := d.newParser(t)
				if err := introspect.Merge(base(d.schema), p, []string{migration}); err != nil {
					t.Fatalf("Merge() unexpected error for DROP+ADD: %v", err)
				}

				items := p.Schema().Tables[0]
				countCol := parser.ColumnByName(&items, "count")
				if countCol == nil {
					t.Fatal("expected column 'count' in merged schema")
				}
				if countCol.Type != "text" {
					t.Errorf("expected count type 'text', got %q", countCol.Type)
				}
			})

			t.Run("ADD COLUMN duplicating existing with different type errors", func(t *testing.T) {
				migration := writeMigration(t, "ALTER TABLE items ADD COLUMN count text;")

				err := introspect.Merge(base(d.schema), d.newParser(t), []string{migration})
				if err == nil {
					t.Fatal("Merge() expected error for duplicate column with different type, got nil")
				}
				if !strings.Contains(err.Error(), "column conflict") {
					t.Errorf("Merge() error = %q, want error containing 'column conflict'", err.Error())
				}
			})
		})
	}
}

// --- View introspection tests ---

func TestPostgresIntrospectViews(t *testing.T) {
	requireDatabases(t)

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
	requireDatabases(t)

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

	// Types as the view parser reads the definition (PRD §16.3): COUNT is an
	// int64 literal, and SUM keeps its source column's type for the
	// generator's widening.
	wantCols := map[string]string{
		"customer_id": "bigint",
		"email":       "text",
		"order_count": "int64",
		"total":       "numeric(10,2)",
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
	requireDatabases(t)

	const ddl = `CREATE TABLE view_users (
			id bigint NOT NULL AUTO_INCREMENT,
			name varchar(255) NOT NULL,
			score int NOT NULL DEFAULT 0,
			status enum('active','inactive') NOT NULL DEFAULT 'active',
			perms set('r','w'),
			PRIMARY KEY (id)
		);
		CREATE VIEW user_summary AS
			SELECT u.id, u.name, u.score, u.status, u.status AS state, u.perms, CAST(u.status AS CHAR) AS status_text
			FROM view_users u WHERE u.score > 0;
		CREATE VIEW user_states AS SELECT d.status FROM (SELECT status FROM view_users) d;
		CREATE VIEW user_states_copy AS SELECT status FROM user_states`
	applyParityFixture(t, "mysql", mysqlConnStr,
		"DROP VIEW IF EXISTS user_states_copy; DROP VIEW IF EXISTS user_states; DROP VIEW IF EXISTS user_summary; DROP TABLE IF EXISTS view_users", ddl)

	intro := introspect.NewMySQLIntrospector()
	defer intro.Close() //nolint:errcheck // test cleanup

	schema := &parser.Schema{}
	err := intro.Introspect(context.Background(), mysqlConnStr, schema, introspect.IntrospectionOptions{
		Tables: []string{"view_users", "user_summary", "user_states", "user_states_copy"},
	})
	if err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}

	// A projected ENUM or SET column takes its source column's type. One whose
	// source is not a table column read from the view's SELECT gets a type of
	// its own.
	wantViews := []parser.View{
		{Name: "user_states", Columns: []parser.Column{{Name: "status", Type: "user_states__status_enum"}}},
		{Name: "user_states_copy", Columns: []parser.Column{{Name: "status", Type: "user_states_copy__status_enum"}}},
		{Name: "user_summary", Columns: []parser.Column{
			{Name: "id", Type: "bigint"},
			{Name: "name", Type: "varchar(255)"},
			{Name: "score", Type: "int"},
			{Name: "status", Type: "view_users__status_enum"},
			{Name: "state", Type: "view_users__status_enum"},
			{Name: "perms", Type: "view_users__perms_set", Nullable: true},
			{Name: "status_text", Type: "varchar(8)", Nullable: true},
		}},
	}
	if diff := cmp.Diff(wantViews, schema.Views, cmpopts.IgnoreFields(parser.View{}, "SQL", "Definition")); diff != "" {
		t.Errorf("views mismatch (-want +got):\n%s", diff)
	}
	wantEnums := []parser.Enum{
		{Name: "view_users__status_enum", Values: []string{"active", "inactive"}},
		{Name: "user_states__status_enum", Values: []string{"active", "inactive"}},
		{Name: "user_states_copy__status_enum", Values: []string{"active", "inactive"}},
	}
	if diff := cmp.Diff(wantEnums, schema.Enums); diff != "" {
		t.Errorf("enums mismatch (-want +got):\n%s", diff)
	}
	wantSets := []parser.Set{{Name: "view_users__perms_set", Values: []string{"r", "w"}}}
	if diff := cmp.Diff(wantSets, schema.Sets); diff != "" {
		t.Errorf("sets mismatch (-want +got):\n%s", diff)
	}

	p, err := mysql.New()
	if err != nil {
		t.Fatalf("creating mysql parser: %v", err)
	}
	parsed := parseStatements(t, p, ddl)
	tables := &parser.Schema{
		Tables: schema.Tables,
		Enums:  slices.DeleteFunc(slices.Clone(schema.Enums), func(e parser.Enum) bool { return strings.HasPrefix(e.Name, "user_states") }),
		Sets:   schema.Sets,
	}
	assertSchemaParity(t, "mysql", withoutViews(parsed), tables)
}

func TestSQLiteIntrospectViews(t *testing.T) {
	requireDatabases(t)

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
	requireDatabases(t)

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

func TestMySQLIntrospect_EnumEdgeValues(t *testing.T) {
	requireDatabases(t)

	ctx := context.Background()
	ddl := `CREATE TABLE enum_edge (
		id int PRIMARY KEY,
		status ENUM('active', 'in,active', 'it''s', '', 'b\\c') NOT NULL
	)`

	db, err := sql.Open("mysql", mysqlConnStr)
	if err != nil {
		t.Fatalf("opening mysql: %v", err)
	}
	defer db.Close() //nolint:errcheck // test cleanup

	_, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS enum_edge")
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		t.Fatalf("applying DDL: %v", err)
	}
	defer db.ExecContext(ctx, "DROP TABLE IF EXISTS enum_edge") //nolint:errcheck // test cleanup

	intro := introspect.NewMySQLIntrospector()
	defer intro.Close() //nolint:errcheck // test cleanup

	introspected := &parser.Schema{}
	if err := intro.Introspect(ctx, mysqlConnStr, introspected, introspect.IntrospectionOptions{
		Tables: []string{"enum_edge"},
	}); err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}

	p, err := mysql.New()
	if err != nil {
		t.Fatalf("creating mysql parser: %v", err)
	}
	if err := p.Parse("test.sql", []byte(ddl)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	want := []parser.Enum{{
		Name:   "enum_edge__status_enum",
		Values: []string{"active", "in,active", "it's", "", `b\c`},
	}}
	if diff := cmp.Diff(want, p.Schema().Enums); diff != "" {
		t.Errorf("parsed enums mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(want, introspected.Enums); diff != "" {
		t.Errorf("introspected enums mismatch (-want +got):\n%s", diff)
	}
}

func TestMySQLIntrospect_IndexMethod(t *testing.T) {
	requireDatabases(t)

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
	requireDatabases(t)

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
	requireDatabases(t)

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
	requireDatabases(t)

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
	requireDatabases(t)

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

// requireDatabases skips an integration test under -short, when TestMain
// starts no containers.
func requireDatabases(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
}

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
