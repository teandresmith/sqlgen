package gen_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// TestGenerate_NestedSelfReferenceTimeKey generates a self-referential edge on a
// time-keyed table and names the parent's own key, in another zone, under
// connect and disconnect. The self-reference guard (PRD §9.9.4) compares
// instants, so both are refused before any statement runs. The guard and the
// visibility read bucket ids by the same spelling, so this also compiles the
// executor's time-key buckets on every dialect.
func TestGenerate_NestedSelfReferenceTimeKey(t *testing.T) {
	const probeTmpl = `package db

import (
	"context"
	"errors"
	"testing"
	"time"
	%s
)

func TestSelfReferenceAcrossZones(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC)
	ist := time.FixedZone("IST", 5*3600+30*60)
	c := New(&probeQuerier{}).stamp
	parent := &Stamp{K: %s}
	for verb, nested := range map[string]*StampStampsUpdateNested{
		"connect":    {Connect: []%s{%s}},
		"disconnect": {Disconnect: []%s{%s}},
	} {
		err := c.applyStampStampsNested(context.Background(), parent, nested, CallOptions[StampFieldOptions]{})
		if !errors.Is(err, ErrNestedVerbConflict) {
			t.Errorf("%%s naming the parent in another zone: error = %%v, want ErrNestedVerbConflict", verb, err)
		}
	}
}
`
	tests := []struct {
		name    string
		dialect config.Dialect
		ddl     string
		imports string
		keyType string
		key     string
		other   string
	}{
		{
			name:    "postgres",
			dialect: config.DialectPostgres,
			ddl:     `CREATE TABLE stamps (k timestamptz PRIMARY KEY, parent_k timestamptz REFERENCES stamps (k), label text NOT NULL);`,
			keyType: "time.Time", key: "at", other: "at.In(ist)",
		},
		{
			name:    "mysql",
			dialect: config.DialectMySQL,
			ddl:     `CREATE TABLE stamps (k datetime(6) PRIMARY KEY, parent_k datetime(6), label varchar(50) NOT NULL, FOREIGN KEY (parent_k) REFERENCES stamps (k));`,
			keyType: "time.Time", key: "at", other: "at.In(ist)",
		},
		{
			name:    "sqlite",
			dialect: config.DialectSQLite,
			ddl:     `CREATE TABLE stamps (k datetime PRIMARY KEY, parent_k datetime REFERENCES stamps (k), label text NOT NULL);`,
			imports: `"github.com/teandresmith/sqlgen/types"`,
			keyType: "types.DateTime", key: "types.DateTime{Time: at}", other: "{Time: at.In(ist)}",
		},
	}
	const nested = "generation:\n  nested_mutations:\n    enabled: true\n"
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := strings.ReplaceAll(probeTmpl, "%%", "%")
			for _, v := range []string{tt.imports, tt.key, tt.keyType, tt.other, tt.keyType, tt.other} {
				src = strings.Replace(src, "%s", v, 1)
			}
			gnwRunGeneratedProbe(t, []gnwProbePackage{{dir: "db", dialect: tt.dialect, ddl: tt.ddl, extra: nested}}, src)
		})
	}
}
