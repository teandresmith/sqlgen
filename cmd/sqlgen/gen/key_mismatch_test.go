package gen_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// keyMismatchTables builds ddl's contexts by table name, with nested mutations
// on so a mismatched key reaches the write surface too.
func keyMismatchTables(t *testing.T, dialect config.Dialect, driver config.Driver, usePointers bool, ddl string) map[string]gen.TableContext {
	t.Helper()
	schema := parseDDLForTest(t, dialect, ddl)
	parser.DetectRelationships(schema)
	input := testInput(schema)
	input.Config.Input.Dialect = dialect
	input.Config.Output.Driver = driver
	input.Config.Overrides.UsePointers = new(usePointers)
	input.Config.Generation.NestedMutations = &config.NestedMutationsConfig{Enabled: new(true)}
	contexts, err := gen.BuildTableContextsFromSchema(schema, input.Config)
	if err != nil {
		t.Fatalf("BuildTableContextsFromSchema() error: %v", err)
	}
	byName := make(map[string]gen.TableContext, len(contexts))
	for _, tc := range contexts {
		byName[tc.TableName] = tc
	}
	return byName
}

// An FK column that filters through another comparator than the column it
// references loads by key tuples, whose values bind as they are. A list of the
// parent's keys would not assign into the child's filter field, so the package
// would not compile. MySQL refuses these foreign keys at DDL time, but a schema
// file reaches the generator without a database to refuse it.
func TestGetTemplate_fkComparatorDiffersFromParentKey(t *testing.T) {
	const ints = `
CREATE TABLE smalls (id INT PRIMARY KEY);
CREATE TABLE wides (id BIGINT PRIMARY KEY, small_id BIGINT, FOREIGN KEY (small_id) REFERENCES smalls (id));
CREATE TABLE bigs (id BIGINT PRIMARY KEY);
CREATE TABLE narrows (id BIGINT PRIMARY KEY, big_id INT NOT NULL, FOREIGN KEY (big_id) REFERENCES bigs (id));
`
	const sqliteFamilies = `
CREATE TABLE smalls (id INTEGER PRIMARY KEY);
CREATE TABLE wides (id INTEGER PRIMARY KEY, small_id TEXT REFERENCES smalls (id));
CREATE TABLE bigs (id TEXT PRIMARY KEY);
CREATE TABLE narrows (id INTEGER PRIMARY KEY, big_id INTEGER NOT NULL REFERENCES bigs (id));
`
	for _, tt := range []struct {
		name    string
		dialect config.Dialect
		driver  config.Driver
		ddl     string
	}{
		{"postgres pgx", config.DialectPostgres, config.DriverPgx, ints},
		{"postgres stdlib", config.DialectPostgres, config.DriverStdlib, ints},
		{"mysql", config.DialectMySQL, config.DriverStdlib, ints},
		{"sqlite", config.DialectSQLite, config.DriverStdlib, sqliteFamilies},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tables := keyMismatchTables(t, tt.dialect, tt.driver, true, tt.ddl)
			for _, side := range []struct{ parent, column, field string }{
				{"smalls", "small_id", "SmallID"},
				{"bigs", "big_id", "BigID"},
			} {
				output := executeGetTemplate(t, tables[side.parent])
				assertContainsAll(t, output, `sql.BuildCompositePKBatchCondition(c.dialect, []string{"`+side.column+`"}, chunk),`)
				if strings.Contains(output, side.field+": &comparator") {
					t.Errorf("%s: the key went through the child's filter\n\nfull output:\n%s", side.parent, output)
				}
			}
		})
	}

	t.Run("same comparator keeps the plain list", func(t *testing.T) {
		tables := keyMismatchTables(t, config.DialectPostgres, config.DriverPgx, true, `
CREATE TABLE parents (id INT PRIMARY KEY);
CREATE TABLE children (id BIGINT PRIMARY KEY, parent_id INT REFERENCES parents (id));
`)
		assertContainsAll(t, executeGetTemplate(t, tables["parents"]), "ParentID: &comparator.NullableNumber[int32]{Number: comparator.Number[int32]{In: parentIDs}},")
	})
}

// The comparator family decides, not the Go type: two enums of different names
// and two time types of different spellings filter alike, so they keep the
// one-column loader, while an enum next to a text key does not. A time key next
// to a key of another family stays off the tuple loader, which would match no
// row.
func TestGetTemplate_keyComparatorFamilies(t *testing.T) {
	for _, tt := range []struct {
		name    string
		dialect config.Dialect
		ddl     string
		parent  string
		want    string
		keyed   bool
	}{
		{
			name:    "enum next to text",
			dialect: config.DialectPostgres,
			ddl: `CREATE TYPE kind_t AS ENUM ('a', 'b');
CREATE TABLE kinds (k kind_t PRIMARY KEY);
CREATE TABLE kind_kids (id BIGINT PRIMARY KEY, k TEXT REFERENCES kinds (k));`,
			parent: "kinds",
			keyed:  true,
		},
		{
			name:    "mysql per-column enums",
			dialect: config.DialectMySQL,
			ddl: `CREATE TABLE kinds (k ENUM('a','b') PRIMARY KEY);
CREATE TABLE kind_kids (id BIGINT PRIMARY KEY, k ENUM('a','b','c'), FOREIGN KEY (k) REFERENCES kinds (k));`,
			parent: "kinds",
			want:   "K: &comparator.NullableEnum[KindKidsKEnum]{Enum: comparator.Enum[KindKidsKEnum]{In: chunk}},",
		},
		{
			name:    "timestamptz next to timestamp",
			dialect: config.DialectPostgres,
			ddl: `CREATE TABLE stamps (k timestamptz PRIMARY KEY);
CREATE TABLE stamp_kids (id BIGINT PRIMARY KEY, k timestamp REFERENCES stamps (k));`,
			parent: "stamps",
			want:   "K: &comparator.NullableTime{Time: comparator.Time{In: stampIDs}},",
		},
		{
			name:    "nullable sqlite datetime",
			dialect: config.DialectSQLite,
			ddl: `CREATE TABLE stamps (k DATETIME PRIMARY KEY);
CREATE TABLE stamp_kids (id INTEGER PRIMARY KEY, k DATETIME REFERENCES stamps (k));`,
			parent: "stamps",
			want:   "K: &comparator.NullableTime{Time: comparator.Time{In: dateTimeKeys(chunk)}},",
		},
		{
			name:    "sqlite datetime next to text",
			dialect: config.DialectSQLite,
			ddl: `CREATE TABLE stamps (k DATETIME PRIMARY KEY);
CREATE TABLE stamp_kids (id INTEGER PRIMARY KEY, k TEXT REFERENCES stamps (k));`,
			parent: "stamps",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tables := keyMismatchTables(t, tt.dialect, config.DriverStdlib, true, tt.ddl)
			if tt.dialect == config.DialectPostgres {
				tables = keyMismatchTables(t, tt.dialect, config.DriverPgx, true, tt.ddl)
			}
			output := executeGetTemplate(t, tables[tt.parent])
			if got := strings.Contains(output, "sql.BuildCompositePKBatchCondition("); got != tt.keyed {
				t.Errorf("tuple loader = %v, want %v\n\nfull output:\n%s", got, tt.keyed, output)
			}
			if tt.want != "" {
				assertContainsAll(t, output, tt.want)
			}
		})
	}
}

// A parent key the FK's type cannot hold is referenced by nothing, and a driver
// refuses to bind it, so it stays out of the load. That holds on every edge
// whose parent key is bound against a narrower column: the one-column O2M, an
// M2M junction and a composite member. The opposite direction always fits.
func TestGetTemplate_keyWiderThanFKStaysOutOfTheLoad(t *testing.T) {
	for _, tt := range []struct {
		name    string
		ddl     string
		parent  string
		guard   string
		noGuard string
		keys    []string
	}{
		{
			name: "o2m",
			ddl: `CREATE TABLE bigs (id BIGINT PRIMARY KEY);
CREATE TABLE narrows (id BIGINT PRIMARY KEY, big_id INT REFERENCES bigs (id));`,
			parent: "bigs",
			guard:  "if !(int64(int32(b.ID)) == b.ID) {",
		},
		{
			name: "o2m on a narrower parent",
			ddl: `CREATE TABLE smalls (id INT PRIMARY KEY);
CREATE TABLE wides (id BIGINT PRIMARY KEY, small_id BIGINT REFERENCES smalls (id));`,
			parent:  "smalls",
			noGuard: "int32(int64(",
		},
		{
			name: "m2m",
			ddl: `CREATE TABLE bigs (id BIGINT PRIMARY KEY);
CREATE TABLE smalls (id INT PRIMARY KEY);
CREATE TABLE links (big_id INT NOT NULL REFERENCES bigs (id), small_id BIGINT NOT NULL REFERENCES smalls (id), PRIMARY KEY (big_id, small_id));`,
			parent: "bigs",
			guard:  "if !(int64(int32(b.ID)) == b.ID) {",
		},
		{
			name: "composite member",
			ddl: `CREATE TABLE bigs (id BIGINT PRIMARY KEY, a INT NOT NULL, b BIGINT NOT NULL, UNIQUE (a, b));
CREATE TABLE scoped (id BIGINT PRIMARY KEY, a BIGINT NOT NULL, b INT NOT NULL, FOREIGN KEY (a, b) REFERENCES bigs (a, b));`,
			parent:  "bigs",
			guard:   "if !(int64(int32(b.B)) == b.B) {",
			noGuard: "int32(int64(b.A))",
		},
		{
			name: "float parent",
			ddl: `CREATE TABLE floats (id DOUBLE PRECISION PRIMARY KEY);
CREATE TABLE ints (id BIGINT PRIMARY KEY, float_id INT REFERENCES floats (id));`,
			parent: "floats",
			guard:  "if !(float64(int32(f.ID)) == f.ID) {",
			keys:   []string{"fmt.Sprint(float64((*r.FloatID)))"},
		},
		{
			name: "m2m into a float key",
			ddl: `CREATE TABLE floats (id DOUBLE PRECISION PRIMARY KEY);
CREATE TABLE others (id BIGINT PRIMARY KEY);
CREATE TABLE links (other_id BIGINT NOT NULL REFERENCES others (id), float_id INT NOT NULL REFERENCES floats (id), PRIMARY KEY (other_id, float_id));`,
			parent: "others",
			keys:   []string{"fmt.Sprint(float64(key1))"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			output := executeGetTemplate(t, keyMismatchTables(t, config.DialectPostgres, config.DriverPgx, true, tt.ddl)[tt.parent])
			if tt.guard != "" {
				assertContainsAll(t, output, tt.guard)
			}
			assertContainsAll(t, output, tt.keys...)
			if tt.noGuard != "" && strings.Contains(output, tt.noGuard) {
				t.Errorf("output holds %q\n\nfull output:\n%s", tt.noGuard, output)
			}
		})
	}
}

// A Null wrapper over another type than the parent's key cannot carry that key,
// so the edge is not write-eligible (PRD §9.9.4), as with the pointer and bare
// bindings. A wrapper literal compiles only when its value field matches.
func TestNestedEligibility_nullWrapperOfAnotherWidth(t *testing.T) {
	const ddl = `
CREATE TABLE smalls (id serial PRIMARY KEY);
CREATE TABLE wides (id bigserial PRIMARY KEY, small_id BIGINT REFERENCES smalls (id));
CREATE TABLE bigs (id bigserial PRIMARY KEY);
CREATE TABLE narrows (id bigserial PRIMARY KEY, big_id INT REFERENCES bigs (id));
CREATE TABLE same (id bigserial PRIMARY KEY, big_id BIGINT REFERENCES bigs (id));
`
	for _, usePointers := range []bool{true, false} {
		tables := keyMismatchTables(t, config.DialectPostgres, config.DriverPgx, usePointers, ddl)
		if nested := tables["smalls"].Nested; nested != nil {
			t.Errorf("use_pointers %v: smalls has nested edges %v, want none", usePointers, nestedEdgeNames(tables["smalls"]))
		}
		names := nestedEdgeNames(tables["bigs"])
		if slices.Contains(names, "Narrows") {
			t.Errorf("use_pointers %v: bigs.Narrows survived: %v", usePointers, names)
		}
		if !slices.Contains(names, "Sames") {
			t.Errorf("use_pointers %v: bigs.Sames was dropped: %v", usePointers, names)
		}
	}
}
