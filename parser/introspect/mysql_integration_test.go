package introspect_test

import (
	"bytes"
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/parser/introspect"
	"github.com/teandresmith/sqlgen/parser/mysql"
)

// introspectMySQL introspects tables from the test database.
func introspectMySQL(t *testing.T, tables ...string) *parser.Schema {
	t.Helper()
	intro := introspect.NewMySQLIntrospector()
	defer intro.Close() //nolint:errcheck // test cleanup
	schema := &parser.Schema{}
	if err := intro.Introspect(context.Background(), mysqlConnStr, schema, introspect.IntrospectionOptions{
		Tables: tables,
	}); err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}
	return schema
}

// parseMySQL parses ddl one statement at a time with the MySQL file parser.
func parseMySQL(t *testing.T, ddl string) *parser.Schema {
	t.Helper()
	p, err := mysql.New()
	if err != nil {
		t.Fatalf("mysql.New() error: %v", err)
	}
	return parseStatements(t, p, ddl)
}

// columnField returns field(c) for every column of table, keyed by column name.
func columnField(t *testing.T, s *parser.Schema, table string, field func(parser.Column) string) map[string]string {
	t.Helper()
	got := make(map[string]string)
	for _, c := range findTable(t, s, table).Columns {
		got[c.Name] = field(c)
	}
	return got
}

// withoutViews returns a copy of s without its views and warnings. The file
// parser skips CREATE VIEW with a warning, so views are compared on their own.
func withoutViews(s *parser.Schema) *parser.Schema {
	c := *s
	c.Views = nil
	c.Warnings = nil
	return &c
}

func TestMySQLIntrospect_ProbeFixture(t *testing.T) {
	requireDatabases(t)
	applyParityFixture(t, "mysql", mysqlConnStr, mysqlParityFixtureReset, mysqlParityFixture)

	introspected := introspectMySQL(t, "m_users", "m_pairs", "MixedCase", "m_user_status")
	parsed := parseMySQL(t, mysqlParityFixture)

	view := findView(t, introspected, "m_user_status")
	wantView := []parser.Column{
		{Name: "id", Type: "bigint"},
		{Name: "status", Type: "m_users__status_enum"},
	}
	if diff := cmp.Diff(wantView, view.Columns); diff != "" {
		t.Errorf("m_user_status columns mismatch (-want +got):\n%s", diff)
	}
	assertSchemaParity(t, "mysql", withoutViews(parsed), withoutViews(introspected))
}

func TestMySQLIntrospect_Defaults(t *testing.T) {
	requireDatabases(t)
	const ddl = `CREATE TABLE m_defaults (
		id int NOT NULL PRIMARY KEY,
		empty_str varchar(50) NOT NULL DEFAULT '',
		str varchar(50) DEFAULT 'active',
		quoted varchar(50) DEFAULT 'it''s',
		backslash varchar(50) DEFAULT 'a\\b',
		keyword_str varchar(50) DEFAULT 'CURRENT_TIMESTAMP',
		null_str varchar(50) DEFAULT 'NULL',
		status ENUM('active','in active') NOT NULL DEFAULT 'in active',
		perms SET('r','w') DEFAULT 'r,w',
		created_at datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
		updated_at timestamp NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
		now_at datetime DEFAULT NOW(),
		now_ms datetime(3) DEFAULT NOW(3),
		local_at datetime DEFAULT LOCALTIMESTAMP,
		local_us datetime(6) DEFAULT LOCALTIME(6),
		now_expr datetime DEFAULT (LOCALTIMESTAMP() + INTERVAL 1 DAY),
		today date DEFAULT (CURRENT_DATE),
		clock time DEFAULT (CURTIME()),
		day date DEFAULT '2020-01-01',
		at time DEFAULT '12:00:00',
		uid varchar(36) DEFAULT (uuid()),
		doc json DEFAULT ('{}'),
		label varchar(20) DEFAULT (concat('a', 'b')),
		sum_default int DEFAULT (1 + 2),
		nothing int DEFAULT NULL,
		unset int,
		qty int NOT NULL DEFAULT 0,
		neg int DEFAULT -1,
		price decimal(10,2) DEFAULT 1.50,
		flags bit(8) DEFAULT b'101',
		yr year DEFAULT 2020
	)`
	applyParityFixture(t, "mysql", mysqlConnStr, "DROP TABLE IF EXISTS m_defaults", ddl)

	introspected := introspectMySQL(t, "m_defaults")
	parsed := parseMySQL(t, ddl)

	want := map[string]string{
		"id":          "",
		"empty_str":   "''",
		"str":         "'active'",
		"quoted":      `'it\'s'`,
		"backslash":   `'a\\b'`,
		"keyword_str": "'CURRENT_TIMESTAMP'",
		"null_str":    "'NULL'",
		"status":      "'in active'",
		"perms":       "'r,w'",
		"created_at":  "current_timestamp(3)",
		"updated_at":  "current_timestamp()",
		"now_at":      "current_timestamp()",
		"now_ms":      "current_timestamp(3)",
		"local_at":    "current_timestamp()",
		"local_us":    "current_timestamp(6)",
		"now_expr":    "current_timestamp() + interval 1 day",
		"today":       "current_date()",
		"clock":       "current_time()",
		"day":         "'2020-01-01'",
		"at":          "'12:00:00'",
		"uid":         "uuid()",
		"doc":         "'{}'",
		"label":       "concat('a', 'b')",
		"sum_default": "1 + 2",
		"nothing":     "",
		"unset":       "",
		"qty":         "0",
		"neg":         "-1",
		"price":       "1.50",
		"flags":       "0b101",
		"yr":          "2020",
	}
	def := func(c parser.Column) string { return c.Default }
	if diff := cmp.Diff(want, columnField(t, introspected, "m_defaults", def)); diff != "" {
		t.Errorf("introspected defaults mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(want, columnField(t, parsed, "m_defaults", def)); diff != "" {
		t.Errorf("parsed defaults mismatch (-want +got):\n%s", diff)
	}
	assertSchemaParity(t, "mysql", parsed, introspected)
}

func TestMySQLIntrospect_TypeCoverage(t *testing.T) {
	requireDatabases(t)
	const ddl = `CREATE TABLE m_types (
		id int NOT NULL PRIMARY KEY,
		legacy INT(11),
		big BIGINT(20) UNSIGNED,
		u INT UNSIGNED,
		bu BIGINT UNSIGNED,
		small SMALLINT(6),
		flag tinyint(1),
		tiny tinyint(4),
		utiny tinyint(1) unsigned,
		zf int(5) zerofill,
		zf_default int zerofill,
		price decimal(10,2),
		doc json,
		at datetime(3),
		uid binary(16),
		vb varbinary(255),
		b blob,
		status ENUM('a','b'),
		perms SET('r','w')
	)`
	applyParityFixture(t, "mysql", mysqlConnStr, "DROP TABLE IF EXISTS m_types", ddl)

	introspected := introspectMySQL(t, "m_types")
	parsed := parseMySQL(t, ddl)

	want := map[string]string{
		"id":         "int",
		"legacy":     "int",
		"big":        "bigint unsigned",
		"u":          "int unsigned",
		"bu":         "bigint unsigned",
		"small":      "smallint",
		"flag":       "tinyint(1)",
		"tiny":       "tinyint",
		"utiny":      "tinyint unsigned",
		"zf":         "int(5) unsigned zerofill",
		"zf_default": "int(10) unsigned zerofill",
		"price":      "decimal(10,2)",
		"doc":        "json",
		"at":         "datetime(3)",
		"uid":        "binary(16)",
		"vb":         "varbinary(255)",
		"b":          "blob",
		"status":     "m_types__status_enum",
		"perms":      "m_types__perms_set",
	}
	typ := func(c parser.Column) string { return c.Type }
	if diff := cmp.Diff(want, columnField(t, introspected, "m_types", typ)); diff != "" {
		t.Errorf("introspected types mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(want, columnField(t, parsed, "m_types", typ)); diff != "" {
		t.Errorf("parsed types mismatch (-want +got):\n%s", diff)
	}
	assertSchemaParity(t, "mysql", parsed, introspected)
}

// The catalog stores a type synonym, and ZEROFILL on a non-integer type, in
// canonical form, and the parser must write the same (PRD §6.1, §7.2).
func TestMySQLIntrospect_TypeSynonyms(t *testing.T) {
	requireDatabases(t)
	const ddl = `CREATE TABLE m_synonyms (
		id INTEGER NOT NULL PRIMARY KEY,
		b1 BOOL,
		b2 BOOLEAN NOT NULL,
		iu INTEGER(11) UNSIGNED,
		iz INTEGER ZEROFILL,
		n NUMERIC(8,3),
		n0 NUMERIC,
		d0 DECIMAL,
		d5 DECIMAL(5),
		r REAL,
		r2 REAL(8,2),
		f24 FLOAT(24),
		f30 FLOAT(30),
		f4 FLOAT4,
		f8 FLOAT8,
		c CHAR,
		bn BINARY,
		bt BIT,
		y YEAR(4),
		dz DOUBLE ZEROFILL,
		duz DOUBLE UNSIGNED ZEROFILL,
		decz DECIMAL(5,2) UNSIGNED ZEROFILL,
		fz FLOAT(7,3) UNSIGNED ZEROFILL
	)`
	applyParityFixture(t, "mysql", mysqlConnStr, "DROP TABLE IF EXISTS m_synonyms", ddl)

	introspected := introspectMySQL(t, "m_synonyms")
	parsed := parseMySQL(t, ddl)

	want := map[string]string{
		"id":   "int",
		"b1":   "tinyint(1)",
		"b2":   "tinyint(1)",
		"iu":   "int unsigned",
		"iz":   "int(10) unsigned zerofill",
		"n":    "decimal(8,3)",
		"n0":   "decimal(10,0)",
		"d0":   "decimal(10,0)",
		"d5":   "decimal(5,0)",
		"r":    "double",
		"r2":   "double(8,2)",
		"f24":  "float",
		"f30":  "double",
		"f4":   "float",
		"f8":   "double",
		"c":    "char(1)",
		"bn":   "binary(1)",
		"bt":   "bit(1)",
		"y":    "year",
		"dz":   "double unsigned zerofill",
		"duz":  "double unsigned zerofill",
		"decz": "decimal(5,2) unsigned zerofill",
		"fz":   "float(7,3) unsigned zerofill",
	}
	typ := func(c parser.Column) string { return c.Type }
	if diff := cmp.Diff(want, columnField(t, introspected, "m_synonyms", typ)); diff != "" {
		t.Errorf("introspected types mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(want, columnField(t, parsed, "m_synonyms", typ)); diff != "" {
		t.Errorf("parsed types mismatch (-want +got):\n%s", diff)
	}
	assertSchemaParity(t, "mysql", parsed, introspected)
}

func TestMySQLIntrospect_QuotedIdentifiers(t *testing.T) {
	requireDatabases(t)
	const ddl = "CREATE TABLE `UserAccounts` (" + `
		` + "`Id`" + ` int NOT NULL PRIMARY KEY,
		` + "`createdAt`" + ` datetime NOT NULL,
		` + "`order`" + ` int NOT NULL DEFAULT 0,
		UNIQUE KEY ` + "`UserAccounts_order_createdAt` (`order`, `createdAt`)" + `,
		CONSTRAINT ` + "`UserAccounts_order_check` CHECK (`order` >= 0)" + `
	);
	CREATE TABLE ` + "`AccountOrders` (`Id` int NOT NULL PRIMARY KEY, `accountId` int, " +
		"CONSTRAINT `AccountOrders_account_fk` FOREIGN KEY (`accountId`) REFERENCES `UserAccounts` (`Id`))"
	applyParityFixture(t, "mysql", mysqlConnStr, "DROP TABLE IF EXISTS `AccountOrders`; DROP TABLE IF EXISTS `UserAccounts`", ddl)

	introspected := introspectMySQL(t, "UserAccounts", "AccountOrders")
	parsed := parseMySQL(t, ddl)

	check := findConstraintByName(findTable(t, introspected, "UserAccounts").Constraints, "UserAccounts_order_check")
	if check == nil || check.CheckExpression != "`order` >= 0" {
		t.Errorf("UserAccounts_order_check = %+v, want CheckExpression \"`order` >= 0\"", check)
	}
	assertSchemaParity(t, "mysql", parsed, introspected)
}

// TestMySQLIntrospect_InlineTypeNames checks that both schema sources keep
// apart the inline ENUM and SET names of two columns a single underscore would
// merge (m_nc.k_v and m_nc_k.v), and that a view column named after its view
// (m_nc_v.c) stays apart from the table column m_nc.v_c.
func TestMySQLIntrospect_InlineTypeNames(t *testing.T) {
	requireDatabases(t)
	const ddl = `CREATE TABLE m_nc (id int NOT NULL PRIMARY KEY, k_v enum('1','2') NOT NULL, s_t set('r','w'), v_c enum('x') NOT NULL);
	CREATE TABLE m_nc_k (id int NOT NULL PRIMARY KEY, v enum('3') NOT NULL);
	CREATE TABLE m_nc_s (id int NOT NULL PRIMARY KEY, t set('r'))`
	applyParityFixture(t, "mysql", mysqlConnStr,
		"DROP VIEW IF EXISTS m_nc_v; DROP TABLE IF EXISTS m_nc; DROP TABLE IF EXISTS m_nc_k; DROP TABLE IF EXISTS m_nc_s",
		ddl+";\nCREATE VIEW m_nc_v AS SELECT d.c FROM (SELECT v_c AS c FROM m_nc) d")

	introspected := introspectMySQL(t, "m_nc", "m_nc_k", "m_nc_s")
	parsed := parseMySQL(t, ddl)
	assertSchemaParity(t, "mysql", parsed, introspected)
	wantEnums := []parser.Enum{
		{Name: "m_nc__k_v_enum", Values: []string{"1", "2"}},
		{Name: "m_nc__v_c_enum", Values: []string{"x"}},
		{Name: "m_nc_k__v_enum", Values: []string{"3"}},
	}
	if diff := cmp.Diff(wantEnums, introspected.Enums); diff != "" {
		t.Errorf("enums mismatch (-want +got):\n%s", diff)
	}
	wantSets := []parser.Set{
		{Name: "m_nc__s_t_set", Values: []string{"r", "w"}},
		{Name: "m_nc_s__t_set", Values: []string{"r"}},
	}
	if diff := cmp.Diff(wantSets, introspected.Sets); diff != "" {
		t.Errorf("sets mismatch (-want +got):\n%s", diff)
	}

	withView := introspectMySQL(t, "m_nc", "m_nc_v")
	if got := findView(t, withView, "m_nc_v").Columns[0].Type; got != "m_nc_v__c_enum" {
		t.Errorf("m_nc_v.c type = %q, want m_nc_v__c_enum", got)
	}
	wantViewEnums := []parser.Enum{
		{Name: "m_nc__k_v_enum", Values: []string{"1", "2"}},
		{Name: "m_nc__v_c_enum", Values: []string{"x"}},
		{Name: "m_nc_v__c_enum", Values: []string{"x"}},
	}
	if diff := cmp.Diff(wantViewEnums, withView.Enums); diff != "" {
		t.Errorf("enums with view mismatch (-want +got):\n%s", diff)
	}
}

// TestMySQLIntrospect_InlineEnumAfterRenameAndDrop checks that the file parser
// names inline ENUM and SET types from the current table and column names after
// RENAME, and drops them with their column or table, as the catalog does.
func TestMySQLIntrospect_InlineEnumAfterRenameAndDrop(t *testing.T) {
	requireDatabases(t)
	const ddl = `CREATE TABLE m_enum_alter (
		id int NOT NULL PRIMARY KEY,
		role enum('a','b') NOT NULL,
		tier enum('p','q'),
		tags set('x','y')
	);
	ALTER TABLE m_enum_alter RENAME COLUMN role TO kind;
	ALTER TABLE m_enum_alter DROP COLUMN tier;
	ALTER TABLE m_enum_alter ADD COLUMN role enum('z') NOT NULL;
	CREATE TABLE m_enum_old (id int NOT NULL PRIMARY KEY, mode set('r','w'));
	RENAME TABLE m_enum_old TO m_enum_new;
	CREATE TABLE m_enum_gone (id int NOT NULL PRIMARY KEY, state enum('on','off'));
	DROP TABLE m_enum_gone`
	applyParityFixture(t, "mysql", mysqlConnStr, "DROP TABLE IF EXISTS m_enum_alter; DROP TABLE IF EXISTS m_enum_old; DROP TABLE IF EXISTS m_enum_new; DROP TABLE IF EXISTS m_enum_gone", ddl)

	introspected := introspectMySQL(t, "m_enum_alter", "m_enum_new")
	parsed := parseMySQL(t, ddl)

	assertSchemaParity(t, "mysql", parsed, introspected)
}

// TestMySQLIntrospect_InlineEnumCollisionResolvedByAlter checks that a
// migration renaming or dropping one of two colliding inline ENUM columns
// leaves the file parser with the entries the catalog gives: m_cl.b_c and
// m_cl_b.c share the Go type name MClBCEnum, and m_q.b__c and m_q__b.c share
// the name m_q__b__c_enum.
func TestMySQLIntrospect_InlineEnumCollisionResolvedByAlter(t *testing.T) {
	requireDatabases(t)
	const (
		goName  = "CREATE TABLE m_cl (id int NOT NULL PRIMARY KEY, b_c enum('1','2') NOT NULL);\nCREATE TABLE m_cl_b (id int NOT NULL PRIMARY KEY, c enum('3','4') NOT NULL);\n"
		sqlName = "CREATE TABLE m_q (id int NOT NULL PRIMARY KEY, b__c enum('1') NOT NULL);\nCREATE TABLE m_q__b (id int NOT NULL PRIMARY KEY, c enum('2') NOT NULL);\n"
		reset   = "DROP TABLE IF EXISTS m_cl; DROP TABLE IF EXISTS m_cl_b; DROP TABLE IF EXISTS m_q; DROP TABLE IF EXISTS m_q__b"
	)
	tests := []struct {
		name   string
		ddl    string
		tables []string
	}{
		{name: "RENAME COLUMN", ddl: goName + "ALTER TABLE m_cl_b RENAME COLUMN c TO d", tables: []string{"m_cl", "m_cl_b"}},
		{name: "DROP COLUMN", ddl: goName + "ALTER TABLE m_cl_b DROP COLUMN c", tables: []string{"m_cl", "m_cl_b"}},
		{name: "RENAME COLUMN on a shared name", ddl: sqlName + "ALTER TABLE m_q__b RENAME COLUMN c TO d", tables: []string{"m_q", "m_q__b"}},
		{name: "DROP COLUMN on a shared name", ddl: sqlName + "ALTER TABLE m_q DROP COLUMN b__c", tables: []string{"m_q", "m_q__b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			applyParityFixture(t, "mysql", mysqlConnStr, reset, tt.ddl)

			introspected := introspectMySQL(t, tt.tables...)
			parsed := parseMySQL(t, tt.ddl)

			assertSchemaParity(t, "mysql", parsed, introspected)
		})
	}
}

// TestMySQLIntrospect_AlterColumnNamedInAnotherCase checks that each ALTER
// TABLE column form naming a column in another case leaves the file parser
// with the columns, keys and inline ENUMs the catalog gives: MySQL column
// names are case-insensitive, and MODIFY COLUMN takes the spelling it names.
func TestMySQLIntrospect_AlterColumnNamedInAnotherCase(t *testing.T) {
	requireDatabases(t)
	const ddl = `CREATE TABLE m_cc_drop (id int NOT NULL PRIMARY KEY, a int, b int, UNIQUE KEY uk (a, b), KEY kx ((a + 1), b));
	ALTER TABLE m_cc_drop DROP COLUMN B;
	CREATE TABLE m_cc_mod (id int NOT NULL PRIMARY KEY, b int, e enum('x','y'), UNIQUE KEY uk (b));
	CREATE TABLE m_cc_ref (id int NOT NULL PRIMARY KEY, p_b int, CONSTRAINT fk_cc FOREIGN KEY (p_b) REFERENCES m_cc_mod (b));
	ALTER TABLE m_cc_mod MODIFY COLUMN B int NOT NULL;
	ALTER TABLE m_cc_mod MODIFY COLUMN E enum('x','y','z');
	CREATE TABLE m_cc_change (id int NOT NULL PRIMARY KEY, b int, UNIQUE KEY uk (b));
	ALTER TABLE m_cc_change CHANGE COLUMN B c bigint;
	CREATE TABLE m_cc_rename (id int NOT NULL PRIMARY KEY, b enum('x','y'), UNIQUE KEY uk (b));
	ALTER TABLE m_cc_rename RENAME COLUMN B TO c`
	applyParityFixture(t, "mysql", mysqlConnStr,
		"DROP TABLE IF EXISTS m_cc_drop; DROP TABLE IF EXISTS m_cc_ref; DROP TABLE IF EXISTS m_cc_mod; DROP TABLE IF EXISTS m_cc_change; DROP TABLE IF EXISTS m_cc_rename",
		ddl)

	introspected := introspectMySQL(t, "m_cc_drop", "m_cc_mod", "m_cc_ref", "m_cc_change", "m_cc_rename")
	parsed := parseMySQL(t, ddl)

	assertSchemaParity(t, "mysql", parsed, introspected)
}

// TestMySQLIntrospect_BothModeInlineEnumAfterRenameAndDrop checks `input.source:
// both`: a migration renaming or dropping introspected inline ENUM and SET
// columns leaves the merged schema with the entries the catalog gives once the
// migration is applied.
func TestMySQLIntrospect_BothModeInlineEnumAfterRenameAndDrop(t *testing.T) {
	requireDatabases(t)
	const (
		base = `CREATE TABLE m_seed (
			id int NOT NULL PRIMARY KEY,
			role enum('a','b') NOT NULL,
			tier enum('p','q'),
			tags set('x','y')
		);
		CREATE TABLE m_seed_gone (id int NOT NULL PRIMARY KEY, state enum('on','off'))`
		migration = `ALTER TABLE m_seed RENAME COLUMN role TO kind;
		ALTER TABLE m_seed DROP COLUMN tier;
		RENAME TABLE m_seed TO m_seed_new;
		DROP TABLE m_seed_gone`
	)
	applyParityFixture(t, "mysql", mysqlConnStr, "DROP TABLE IF EXISTS m_seed; DROP TABLE IF EXISTS m_seed_new; DROP TABLE IF EXISTS m_seed_gone", base)
	seeded := introspectMySQL(t, "m_seed", "m_seed_gone")

	p, err := mysql.New()
	if err != nil {
		t.Fatalf("mysql.New() error: %v", err)
	}
	if err := introspect.Merge(seeded, p, []string{writeMigration(t, migration)}); err != nil {
		t.Fatalf("Merge() error: %v", err)
	}

	applyParityFixture(t, "mysql", mysqlConnStr, "", migration)
	introspected := introspectMySQL(t, "m_seed_new")

	assertSchemaParity(t, "mysql", p.Schema(), introspected)
}

// TestMySQLIntrospect_KeyDeclarationOrder pins MySQL's numbering of unnamed
// keys in declaration order, with the index it builds for a foreign key taking
// its place where the FOREIGN KEY is declared unless a later key replaces it,
// and a key declared on a column taking the column's place (PRD §5.2).
func TestMySQLIntrospect_KeyDeclarationOrder(t *testing.T) {
	requireDatabases(t)
	const ddl = `CREATE TABLE m_ko_p (id int PRIMARY KEY, b int, c int, d int, KEY (b, c), KEY (b, c, d));
	CREATE TABLE m_ko_fk (b int, c int, FOREIGN KEY (b, c) REFERENCES m_ko_p (b, c), KEY (b));
	CREATE TABLE m_ko_col (x int, KEY (a, x), a int UNIQUE);
	CREATE TABLE m_ko_fkcol (x int, FOREIGN KEY (b, c) REFERENCES m_ko_p (b, c), b int UNIQUE, c int);
	CREATE TABLE m_ko_mix (b int, c int, d int, FOREIGN KEY (b, c) REFERENCES m_ko_p (b, c), e int UNIQUE, KEY (b), UNIQUE (b, d));
	CREATE TABLE m_ko_rk (b int, c int, FOREIGN KEY (b, c) REFERENCES m_ko_p (b, c), KEY (b), KEY (b, c));
	CREATE TABLE m_ko_rf (b int, c int, FOREIGN KEY (b) REFERENCES m_ko_p (id), FOREIGN KEY (b, c) REFERENCES m_ko_p (b, c), KEY (b));
	CREATE TABLE m_ko_lf (b int, c int, d int, FOREIGN KEY (b, c) REFERENCES m_ko_p (b, c), KEY (b), FOREIGN KEY (b, c, d) REFERENCES m_ko_p (b, c, d));
	CREATE TABLE m_ko_sk (b int, c int, FOREIGN KEY (b, c) REFERENCES m_ko_p (b, c), KEY (c), FOREIGN KEY (b) REFERENCES m_ko_p (id))`
	applyParityFixture(t, "mysql", mysqlConnStr, "DROP TABLE IF EXISTS m_ko_fk; DROP TABLE IF EXISTS m_ko_col; DROP TABLE IF EXISTS m_ko_fkcol; DROP TABLE IF EXISTS m_ko_mix; DROP TABLE IF EXISTS m_ko_rk; DROP TABLE IF EXISTS m_ko_rf; DROP TABLE IF EXISTS m_ko_lf; DROP TABLE IF EXISTS m_ko_sk; DROP TABLE IF EXISTS m_ko_p", ddl)

	tables := []string{"m_ko_p", "m_ko_fk", "m_ko_col", "m_ko_fkcol", "m_ko_mix", "m_ko_rk", "m_ko_rf", "m_ko_lf", "m_ko_sk"}
	introspected := introspectMySQL(t, tables...)
	parsed := parseMySQL(t, ddl)

	want := map[string]map[string]string{
		"m_ko_fk":    {"b": "b,c", "b_2": "b"},
		"m_ko_col":   {"a": "a,x", "a_2": "a"},
		"m_ko_fkcol": {"b": "b,c", "b_2": "b"},
		"m_ko_mix":   {"b": "b,c", "e": "e", "b_2": "b", "b_3": "b,d"},
		"m_ko_rk":    {"b": "b", "b_2": "b,c"},
		"m_ko_rf":    {"b": "b,c", "b_2": "b"},
		"m_ko_lf":    {"b": "b", "b_2": "b,c,d"},
		"m_ko_sk":    {"b": "b,c", "c": "c"},
	}
	keyNames := func(s *parser.Schema) map[string]map[string]string {
		got := make(map[string]map[string]string)
		for table := range want {
			got[table] = make(map[string]string)
			for _, c := range findTable(t, s, table).Constraints {
				if c.Type == parser.Unique || c.Type == parser.Index {
					got[table][c.Name] = strings.Join(c.Columns, ",")
				}
			}
		}
		return got
	}
	if diff := cmp.Diff(want, keyNames(introspected)); diff != "" {
		t.Errorf("introspected key names mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(want, keyNames(parsed)); diff != "" {
		t.Errorf("parsed key names mismatch (-want +got):\n%s", diff)
	}
	assertSchemaParity(t, "mysql", parsed, introspected)
}

func TestMySQLIntrospect_CheckText(t *testing.T) {
	requireDatabases(t)
	const ddl = `CREATE TABLE m_checks (
		id int NOT NULL PRIMARY KEY,
		price decimal(10,2),
		name varchar(20),
		doc json,
		CONSTRAINT m_checks_price CHECK (price >= 0),
		CONSTRAINT m_checks_quote CHECK (name <> 'it''s'),
		CONSTRAINT m_checks_in CHECK (name IN ('a', 'b') OR name IS NULL),
		CONSTRAINT m_checks_like CHECK (name LIKE 'a\_%'),
		CONSTRAINT m_checks_backslash CHECK (name <> 'back\\slash'),
		CONSTRAINT m_checks_negative CHECK ((price > 1) OR (price < -1)),
		CONSTRAINT m_checks_introducer CHECK (name <> _latin1'q'),
		CONSTRAINT m_checks_json CHECK (doc->>'$.a' <> 'z'),
		CONSTRAINT m_checks_json_extract CHECK (doc->'$.b' IS NOT NULL),
		CHECK (price < 1000)
	)`
	applyParityFixture(t, "mysql", mysqlConnStr, "DROP TABLE IF EXISTS m_checks", ddl)

	introspected := introspectMySQL(t, "m_checks")
	parsed := parseMySQL(t, ddl)

	want := map[string]string{
		"m_checks_price":        "price >= 0",
		"m_checks_quote":        "`name` != 'it\\'s'",
		"m_checks_in":           "`name` in ('a', 'b') or `name` is null",
		"m_checks_like":         "`name` like 'a\\_%'",
		"m_checks_backslash":    "`name` != 'back\\\\slash'",
		"m_checks_negative":     "price > 1 or price < -1",
		"m_checks_introducer":   "`name` != 'q'",
		"m_checks_json":         "json_unquote(json_extract(doc, '$.a')) != 'z'",
		"m_checks_json_extract": "json_extract(doc, '$.b') is not null",
		"m_checks_chk_1":        "price < 1000",
	}
	checkText := func(s *parser.Schema) map[string]string {
		got := make(map[string]string)
		for _, c := range filterConstraints(findTable(t, s, "m_checks").Constraints, parser.Check) {
			got[c.Name] = c.CheckExpression
		}
		return got
	}
	if diff := cmp.Diff(want, checkText(introspected)); diff != "" {
		t.Errorf("introspected CHECK text mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(want, checkText(parsed)); diff != "" {
		t.Errorf("parsed CHECK text mismatch (-want +got):\n%s", diff)
	}
	assertSchemaParity(t, "mysql", parsed, introspected)
}

// TestMySQLIntrospect_CheckErrorsSurface cancels the context while the CHECK
// query is on the wire, after every earlier catalog query has succeeded.
// A context cancelled up front would fail at ping instead.
func TestMySQLIntrospect_CheckErrorsSurface(t *testing.T) {
	requireDatabases(t)
	const ddl = `CREATE TABLE m_check_err (id int NOT NULL PRIMARY KEY, v int, CHECK (v > 0))`
	applyParityFixture(t, "mysql", mysqlConnStr, "DROP TABLE IF EXISTS m_check_err", ddl)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const network = "cancel_on_check_query"
	mysqldriver.RegisterDialContext(network, func(dialCtx context.Context, addr string) (net.Conn, error) {
		var d net.Dialer
		conn, err := d.DialContext(dialCtx, "tcp", addr)
		if err != nil {
			return nil, err
		}
		return &cancelOnCheckQuery{Conn: conn, cancel: cancel, closed: make(chan struct{})}, nil
	})
	defer mysqldriver.DeregisterDialContext(network)

	cfg, err := mysqldriver.ParseDSN(mysqlConnStr)
	if err != nil {
		t.Fatalf("ParseDSN(%q) error: %v", mysqlConnStr, err)
	}
	cfg.Net = network

	intro := introspect.NewMySQLIntrospector()
	defer intro.Close() //nolint:errcheck // test cleanup
	schema := &parser.Schema{}
	err = intro.Introspect(ctx, cfg.FormatDSN(), schema, introspect.IntrospectionOptions{Tables: []string{"m_check_err"}})
	// A later catalog query would fail on the cancelled context too, so the
	// error must come from the CHECK query itself.
	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "check constraints") {
		t.Errorf("Introspect() error = %v, want the CHECK query's context.Canceled", err)
	}
}

// cancelOnCheckQuery cancels the introspection context when the CHECK
// constraint query is sent, then holds the write until the driver closes the
// connection in response, so the query cannot finish first.
type cancelOnCheckQuery struct {
	net.Conn
	cancel    context.CancelFunc
	closed    chan struct{}
	closeOnce sync.Once
}

func (c *cancelOnCheckQuery) Write(b []byte) (int, error) {
	if bytes.Contains(b, []byte("information_schema.check_constraints")) {
		c.cancel()
		<-c.closed
		return 0, net.ErrClosed
	}
	return c.Conn.Write(b)
}

func (c *cancelOnCheckQuery) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return c.Conn.Close()
}

// TestMySQLIntrospect_CatalogRewrites pins the text MySQL's catalog stores in
// a form the file parser cannot rebuild from the DDL, so the two sources
// differ (PRD §6.1). A CAST to a character type gains the connection
// charset, a hex string literal reads back as 0x…, a literal default is
// stored converted to its column's type, REGEXP becomes regexp_like(), and a
// NOT is folded into the expression it negates.
func TestMySQLIntrospect_CatalogRewrites(t *testing.T) {
	requireDatabases(t)
	const ddl = `CREATE TABLE m_rewrites (
		id int NOT NULL PRIMARY KEY,
		price decimal(10,2) DEFAULT 1.5,
		ratio double DEFAULT 1e3,
		flag tinyint(1) DEFAULT TRUE,
		qty int DEFAULT '5',
		code varbinary(4) DEFAULT 'ab',
		raw binary(2) DEFAULT x'4142',
		name varchar(20),
		label varchar(20) AS (cast(price as char)),
		small tinyint AS (not (qty > 3)),
		CONSTRAINT m_rewrites_cast CHECK (cast(price as char) <> ''),
		CONSTRAINT m_rewrites_hex CHECK (name <> x'41'),
		CONSTRAINT m_rewrites_regexp CHECK (name regexp '^[a-z]+$'),
		CONSTRAINT m_rewrites_rlike CHECK (name rlike 'x'),
		CONSTRAINT m_rewrites_not_regexp CHECK (name not regexp 'y'),
		CONSTRAINT m_rewrites_not_cmp CHECK (not (qty > 200)),
		CONSTRAINT m_rewrites_bang CHECK (!(qty < 0)),
		CONSTRAINT m_rewrites_not_in CHECK (not (qty in (1, 2))),
		CONSTRAINT m_rewrites_not_between CHECK (not (qty between 1 and 5)),
		CONSTRAINT m_rewrites_not_null CHECK (not (qty is null)),
		CONSTRAINT m_rewrites_not_not CHECK (not (not (qty > 7))),
		CONSTRAINT m_rewrites_not_and CHECK (not (qty > 1 and price > 1)),
		CONSTRAINT m_rewrites_not_col CHECK (not (flag)),
		CONSTRAINT m_rewrites_not_like CHECK (not (name like 'z%'))
	)`
	applyParityFixture(t, "mysql", mysqlConnStr, "DROP TABLE IF EXISTS m_rewrites", ddl)

	introspected := introspectMySQL(t, "m_rewrites")
	parsed := parseMySQL(t, ddl)

	text := func(s *parser.Schema) map[string]string {
		table := findTable(t, s, "m_rewrites")
		got := make(map[string]string)
		for _, c := range table.Columns {
			if c.Default != "" {
				got[c.Name] = c.Default
			}
			if c.GeneratedExpr != "" {
				got[c.Name] = c.GeneratedExpr
			}
		}
		for _, c := range filterConstraints(table.Constraints, parser.Check) {
			got[c.Name] = c.CheckExpression
		}
		return got
	}
	wantParsed := map[string]string{
		"price":                  "1.5",
		"ratio":                  "1e3",
		"flag":                   "true",
		"qty":                    "'5'",
		"code":                   "'ab'",
		"raw":                    "X'4142'",
		"label":                  "cast(price as char)",
		"small":                  "not qty > 3",
		"m_rewrites_cast":        "cast(price as char) != ''",
		"m_rewrites_hex":         "`name` != X'41'",
		"m_rewrites_regexp":      "`name` regexp '^[a-z]+$'",
		"m_rewrites_rlike":       "`name` regexp 'x'",
		"m_rewrites_not_regexp":  "`name` not regexp 'y'",
		"m_rewrites_not_cmp":     "not qty > 200",
		"m_rewrites_bang":        "!(qty < 0)",
		"m_rewrites_not_in":      "not qty in (1, 2)",
		"m_rewrites_not_between": "not qty between 1 and 5",
		"m_rewrites_not_null":    "not qty is null",
		"m_rewrites_not_not":     "not not qty > 7",
		"m_rewrites_not_and":     "not (qty > 1 and price > 1)",
		"m_rewrites_not_col":     "not flag",
		"m_rewrites_not_like":    "not `name` like 'z%'",
	}
	wantIntrospected := map[string]string{
		"price":                  "1.50",
		"ratio":                  "1000",
		"flag":                   "1",
		"qty":                    "5",
		"code":                   "0x6162",
		"raw":                    "0x4142",
		"label":                  "cast(price as char character set utf8mb4)",
		"small":                  "qty <= 3",
		"m_rewrites_cast":        "cast(price as char character set utf8mb4) != ''",
		"m_rewrites_hex":         "`name` != 0x41",
		"m_rewrites_regexp":      "regexp_like(`name`, '^[a-z]+$')",
		"m_rewrites_rlike":       "regexp_like(`name`, 'x')",
		"m_rewrites_not_regexp":  "not regexp_like(`name`, 'y')",
		"m_rewrites_not_cmp":     "qty <= 200",
		"m_rewrites_bang":        "qty >= 0",
		"m_rewrites_not_in":      "qty not in (1, 2)",
		"m_rewrites_not_between": "qty not between 1 and 5",
		"m_rewrites_not_null":    "qty is not null",
		"m_rewrites_not_not":     "qty > 7",
		"m_rewrites_not_and":     "qty <= 1 or price <= 1",
		"m_rewrites_not_col":     "0 = flag",
		"m_rewrites_not_like":    "not `name` like 'z%'",
	}
	if diff := cmp.Diff(wantParsed, text(parsed)); diff != "" {
		t.Errorf("parsed text mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(wantIntrospected, text(introspected)); diff != "" {
		t.Errorf("introspected text mismatch (-want +got):\n%s", diff)
	}
}

// TestMySQLIntrospect_ExpressionIndexes pins that both sources skip an index
// with an expression part, unique or not, while it keeps its name, and that it
// serves as a foreign key's index when its leading columns are the foreign
// key's (PRD §30.7).
func TestMySQLIntrospect_ExpressionIndexes(t *testing.T) {
	requireDatabases(t)
	const ddl = `CREATE TABLE m_ex_p (id int PRIMARY KEY, x int, y int, KEY (x, y));
	CREATE TABLE m_ex_u (email varchar(64), UNIQUE INDEX ux ((lower(email))));
	CREATE TABLE m_ex_m (a int, b varchar(64), c int, UNIQUE INDEX ux (a, (lower(b))), KEY ix (a, (c + 1)));
	CREATE TABLE m_ex_n (a int, b int, KEY (a, (b + 1)), KEY (a));
	CREATE TABLE m_ex_r (a int, b int, KEY a ((b + 1)), KEY (a));
	CREATE TABLE m_ex_c (functional_index int, b int, KEY ((b + 1)), KEY (functional_index));
	CREATE TABLE m_ex_fk (a int, b int, KEY (a, (b + 1)), FOREIGN KEY (a) REFERENCES m_ex_p (id));
	CREATE TABLE m_ex_fk2 (a int, b int, FOREIGN KEY (a) REFERENCES m_ex_p (id), UNIQUE (a, (b + 1)));
	CREATE TABLE m_ex_alt (a int, b int, FOREIGN KEY (a) REFERENCES m_ex_p (id));
	ALTER TABLE m_ex_alt ADD INDEX (a, (b + 1));
	CREATE TABLE m_ex_two (a int, b int, FOREIGN KEY (a, b) REFERENCES m_ex_p (x, y), KEY (a, (b + 1)));
	CREATE TABLE m_ex_d (a int, b int, KEY A ((b + 1)));
	ALTER TABLE m_ex_d DROP INDEX a;
	ALTER TABLE m_ex_d ADD KEY (a);
	CREATE TABLE m_ex_dt (a int, b int, KEY a ((b + 1)));
	DROP TABLE m_ex_dt;
	CREATE TABLE m_ex_dt (a int, KEY (a));
	CREATE TABLE m_ex_rt0 (a int, b int, KEY a ((b + 1)));
	ALTER TABLE m_ex_rt0 RENAME TO m_ex_rt;
	ALTER TABLE m_ex_rt ADD KEY (a);
	CREATE TABLE m_ex_rs (a int, b int, KEY a ((b + 1)));
	ALTER TABLE m_ex_rs RENAME TO m_ex_rs;
	ALTER TABLE m_ex_rs ADD KEY (a);
	CREATE TABLE m_ex_rc (a int, b int, KEY (a, (b + 1)));
	ALTER TABLE m_ex_rc RENAME COLUMN a TO c;
	ALTER TABLE m_ex_rc ADD FOREIGN KEY (c) REFERENCES m_ex_p (id);
	CREATE TABLE m_ex_cc (a int, b int, KEY (a, (b + 1)));
	ALTER TABLE m_ex_cc CHANGE COLUMN a c int;
	ALTER TABLE m_ex_cc ADD FOREIGN KEY (c) REFERENCES m_ex_p (id);
	CREATE TABLE m_ex_dc (a int, b int, KEY (a, (b + 1)));
	ALTER TABLE m_ex_dc DROP COLUMN a;
	ALTER TABLE m_ex_dc ADD COLUMN a int;
	ALTER TABLE m_ex_dc ADD FOREIGN KEY (a) REFERENCES m_ex_p (id)`
	want := map[string]map[string]string{
		"m_ex_u":   {},
		"m_ex_m":   {},
		"m_ex_n":   {"a": "a"},
		"m_ex_r":   {"a_2": "a"},
		"m_ex_c":   {"functional_index_2": "functional_index"},
		"m_ex_fk":  {},
		"m_ex_fk2": {},
		"m_ex_alt": {},
		"m_ex_two": {"a": "a,b"},
		"m_ex_d":   {"a": "a"},
		"m_ex_dt":  {"a": "a"},
		"m_ex_rt":  {"a_2": "a"},
		"m_ex_rs":  {"a_2": "a"},
		"m_ex_rc":  {},
		"m_ex_cc":  {},
		"m_ex_dc":  {"a": "a"},
	}
	tables := make([]string, 0, len(want)+1)
	reset := make([]string, 0, len(want)+2)
	for table := range want {
		tables = append(tables, table)
		reset = append(reset, "DROP TABLE IF EXISTS "+table)
	}
	tables = append(tables, "m_ex_p")
	reset = append(reset, "DROP TABLE IF EXISTS m_ex_rt0", "DROP TABLE IF EXISTS m_ex_p")
	applyParityFixture(t, "mysql", mysqlConnStr, strings.Join(reset, ";"), ddl)
	introspected := introspectMySQL(t, tables...)
	parsed := parseMySQL(t, ddl)

	keyNames := func(s *parser.Schema) map[string]map[string]string {
		got := make(map[string]map[string]string)
		for table := range want {
			got[table] = make(map[string]string)
			for _, c := range findTable(t, s, table).Constraints {
				if c.Type == parser.Unique || c.Type == parser.Index {
					got[table][c.Name] = strings.Join(c.Columns, ",")
				}
			}
			for _, col := range findTable(t, s, table).Columns {
				if col.Unique {
					t.Errorf("%s.%s: Unique = true", table, col.Name)
				}
			}
		}
		return got
	}
	if diff := cmp.Diff(want, keyNames(introspected)); diff != "" {
		t.Errorf("introspected keys mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(want, keyNames(parsed)); diff != "" {
		t.Errorf("parsed keys mismatch (-want +got):\n%s", diff)
	}
	assertSchemaParity(t, "mysql", parsed, introspected)
}

// TestMySQLIntrospect_ReferenceColumnCase checks that a foreign key naming its
// referenced column in another case gives both sources one schema: MySQL
// stores the column as the parent declares it, and carries the key along when
// the column is renamed. The CLI runs ResolveReferenceNames on either source.
func TestMySQLIntrospect_ReferenceColumnCase(t *testing.T) {
	requireDatabases(t)
	const ddl = `CREATE TABLE m_rc_p (b int PRIMARY KEY, k int UNIQUE);
	CREATE TABLE m_rc_c (x int, y int, CONSTRAINT m_rc_fk1 FOREIGN KEY (x) REFERENCES m_rc_p (B), CONSTRAINT m_rc_fk2 FOREIGN KEY (y) REFERENCES m_rc_p (K));
	ALTER TABLE m_rc_p RENAME COLUMN k TO z`
	applyParityFixture(t, "mysql", mysqlConnStr, "DROP TABLE IF EXISTS m_rc_c; DROP TABLE IF EXISTS m_rc_p", ddl)

	introspected := introspectMySQL(t, "m_rc_p", "m_rc_c")
	parsed := parseMySQL(t, ddl)
	parser.ResolveReferenceNames(parsed, "mysql")
	parser.ResolveReferenceNames(introspected, "mysql")

	assertSchemaParity(t, "mysql", parsed, introspected)
}

// TestMySQLIntrospect_DropIgnoresNameCase checks that the file parser drops a
// key or constraint named in another case, as MySQL does.
func TestMySQLIntrospect_DropIgnoresNameCase(t *testing.T) {
	requireDatabases(t)
	const ddl = `CREATE TABLE m_dc_p (id int PRIMARY KEY);
	CREATE TABLE m_dc_i (id int PRIMARY KEY, a int, KEY A (a));
	ALTER TABLE m_dc_i DROP INDEX a;
	CREATE TABLE m_dc_u (id int PRIMARY KEY, a int, UNIQUE KEY A (a));
	ALTER TABLE m_dc_u DROP KEY a;
	CREATE TABLE m_dc_s (id int PRIMARY KEY, a int, KEY a (a));
	DROP INDEX A ON m_dc_s;
	CREATE TABLE m_dc_f (id int PRIMARY KEY, a int, CONSTRAINT Fk FOREIGN KEY (a) REFERENCES m_dc_p (id));
	ALTER TABLE m_dc_f DROP FOREIGN KEY fk;
	CREATE TABLE m_dc_c (id int PRIMARY KEY, a int, CONSTRAINT Ck CHECK (a > 0), CONSTRAINT Ck2 CHECK (a < 9));
	ALTER TABLE m_dc_c DROP CHECK ck;
	ALTER TABLE m_dc_c DROP CONSTRAINT ck2;
	CREATE TABLE m_dc_g (id int PRIMARY KEY, a int, b int, CONSTRAINT m_dc_fk1 FOREIGN KEY (a) REFERENCES m_dc_p (id));
	ALTER TABLE m_dc_g DROP FOREIGN KEY M_DC_FK1;
	ALTER TABLE m_dc_g DROP INDEX M_DC_FK1;
	ALTER TABLE m_dc_g ADD KEY m_dc_fk1 (a);
	ALTER TABLE m_dc_g ADD KEY (a, b)`
	tables := []string{"m_dc_i", "m_dc_u", "m_dc_s", "m_dc_f", "m_dc_c", "m_dc_g", "m_dc_p"}
	reset := make([]string, len(tables))
	for i, table := range tables {
		reset[i] = "DROP TABLE IF EXISTS " + table
	}
	applyParityFixture(t, "mysql", mysqlConnStr, strings.Join(reset, ";"), ddl)

	introspected := introspectMySQL(t, tables...)
	parsed := parseMySQL(t, ddl)

	assertSchemaParity(t, "mysql", parsed, introspected)
}

func findView(t *testing.T, s *parser.Schema, name string) *parser.View {
	t.Helper()
	for i := range s.Views {
		if s.Views[i].Name == name {
			return &s.Views[i]
		}
	}
	t.Fatalf("view %q not found in schema", name)
	return nil
}
