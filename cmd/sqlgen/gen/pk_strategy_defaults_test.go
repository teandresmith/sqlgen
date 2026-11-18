package gen_test

import (
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	sqlparser "github.com/teandresmith/sqlgen/parser"
)

// gnwPKColumn returns the parsed key column of the single table in schema.
func gnwPKColumn(t *testing.T, schema *sqlparser.Schema) sqlparser.Column {
	t.Helper()
	if len(schema.Tables) != 1 {
		t.Fatalf("schema has %d tables, want 1", len(schema.Tables))
	}
	i := slices.IndexFunc(schema.Tables[0].Columns, func(c sqlparser.Column) bool { return c.PrimaryKey })
	if i < 0 {
		t.Fatalf("table %s has no key column", schema.Tables[0].Name)
	}
	return schema.Tables[0].Columns[i]
}

// TestPKStrategy_DefaultedKeyIsDBAndOmittable pins two auto-detection rows:
// a PostgreSQL key of a type other than uuid, serial or identity that has a
// DB default, and a SQLite integer key with a DB default that is not
// AUTOINCREMENT, both resolve to the db strategy, and Create<T>Input carries
// the key as omittable.Value[T] so the caller may leave it to the database.
// The undefaulted twin of each fixture is caller with a bare required field,
// which shows the default is what moves the strategy.
func TestPKStrategy_DefaultedKeyIsDBAndOmittable(t *testing.T) {
	tests := []struct {
		name       string
		dialect    config.Dialect
		ddl        string
		input      string
		want       config.PKStrategy
		wantFields []string
	}{
		{
			name:       "postgres text key with a default is db",
			dialect:    config.DialectPostgres,
			ddl:        `CREATE TABLE coupons (code text PRIMARY KEY DEFAULT md5(random()::text), label text NOT NULL);`,
			input:      "CreateCouponInput",
			want:       config.PKStrategyDB,
			wantFields: []string{"Code omittable.Value[string]", "Label string"},
		},
		{
			name:       "postgres bigint key with an expression default is db",
			dialect:    config.DialectPostgres,
			ddl:        `CREATE TABLE coupons (code bigint PRIMARY KEY DEFAULT (floor(random() * 1000000000))::bigint, label text NOT NULL);`,
			input:      "CreateCouponInput",
			want:       config.PKStrategyDB,
			wantFields: []string{"Code omittable.Value[int64]", "Label string"},
		},
		{
			name:       "postgres text key without a default is caller",
			dialect:    config.DialectPostgres,
			ddl:        `CREATE TABLE coupons (code text PRIMARY KEY, label text NOT NULL);`,
			input:      "CreateCouponInput",
			want:       config.PKStrategyCaller,
			wantFields: []string{"Code string", "Label string"},
		},
		{
			name:       "sqlite INT key with a default is db",
			dialect:    config.DialectSQLite,
			ddl:        `CREATE TABLE tickets (num INT PRIMARY KEY DEFAULT 7, label TEXT NOT NULL);`,
			input:      "CreateTicketInput",
			want:       config.PKStrategyDB,
			wantFields: []string{"Num omittable.Value[int64]", "Label string"},
		},
		{
			name:       "sqlite BIGINT key with a default is db",
			dialect:    config.DialectSQLite,
			ddl:        `CREATE TABLE tickets (num BIGINT PRIMARY KEY DEFAULT 7, label TEXT NOT NULL);`,
			input:      "CreateTicketInput",
			want:       config.PKStrategyDB,
			wantFields: []string{"Num omittable.Value[int64]", "Label string"},
		},
		{
			name:       "sqlite BIGINT key without a default is caller",
			dialect:    config.DialectSQLite,
			ddl:        `CREATE TABLE tickets (num BIGINT PRIMARY KEY, label TEXT NOT NULL);`,
			input:      "CreateTicketInput",
			want:       config.PKStrategyCaller,
			wantFields: []string{"Num int64", "Label string"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := parseDDLForTest(t, tt.dialect, tt.ddl)
			// The rows are about a key the database fills from a default, so
			// a fixture the parser reads as auto-incrementing would pass for
			// the wrong reason.
			if pk := gnwPKColumn(t, schema); pk.AutoIncrement {
				t.Fatalf("fixture key %s parses as auto-increment; it must take the default path", pk.Name)
			}

			in := testInput(schema)
			in.Config.Input.Dialect = tt.dialect
			tables, err := gen.BuildTableContexts(in, nil)
			if err != nil {
				t.Fatalf("BuildTableContexts() error: %v", err)
			}
			if len(tables) != 1 {
				t.Fatalf("BuildTableContexts() returned %d tables, want 1", len(tables))
			}
			if got := tables[0].PKStrategy; got != tt.want {
				t.Errorf("PKStrategy = %q, want %q", got, tt.want)
			}

			models := readGenFile(t, gnwGenerate(t, tt.dialect, tt.ddl, ""), "models_gen.go")
			if diff := cmp.Diff(tt.wantFields, gnwStructFields(t, models, tt.input)); diff != "" {
				t.Errorf("%s fields (-want +got):\n%s", tt.input, diff)
			}
		})
	}
}
