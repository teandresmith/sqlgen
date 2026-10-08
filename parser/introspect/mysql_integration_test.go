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
		{Name: "status", Type: "m_users_status_enum"},
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
		"status":     "m_types_status_enum",
		"perms":      "m_types_perms_set",
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
