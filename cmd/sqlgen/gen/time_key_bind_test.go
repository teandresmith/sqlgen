package gen_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// TestGenerate_TimeKeyBindsAsTime generates a time-keyed table with an FK and a
// junction on each dialect, with nested writes on, and checks what the client
// binds for the key. Text a driver cannot read back is the defect: PostgreSQL
// rejects time.Time.String() outright, and a string on MySQL or SQLite matches
// nothing or the wrong row. The compile is half the test: the key list, the
// nullable FK loader and the nested executors all assign into comparator.Time.
func TestGenerate_TimeKeyBindsAsTime(t *testing.T) {
	const probeTmpl = `package db

import (
	"context"
	"testing"
	"time"
	%s
)

func TestTimeKeyBind(t *testing.T) {
	q := &probeQuerier{}
	key := time.Date(2026, 1, 1, 20, 4, 5, 123456000, time.FixedZone("MST", -7*3600))
	_, _ = New(q).Stamps().Get(context.Background(), %s)
	stmts := q.statements()
	if len(stmts) == 0 || len(stmts[0].args) == 0 {
		t.Fatalf("no bound argument recorded: %%+v", stmts)
	}
	%s
}
`
	const wantTime = `got, ok := stmts[0].args[0].(time.Time)
	if !ok || !got.Equal(key) {
		t.Fatalf("bound %#v, want the time.Time %v", stmts[0].args[0], key)
	}`
	const wantSQLiteText = `if got := stmts[0].args[0]; got != "2026-01-02 03:04:05.123456" {
		t.Fatalf("bound %#v, want the text sql.bindArgs writes", got)
	}`

	const nested = "generation:\n  nested_mutations:\n    enabled: true\n"
	tests := []struct {
		name    string
		dialect config.Dialect
		ddl     string
		imports string
		key     string
		want    string
	}{
		{
			name:    "postgres",
			dialect: config.DialectPostgres,
			ddl: `CREATE TABLE stamps (k timestamptz PRIMARY KEY, label text NOT NULL);
CREATE TABLE kids (id bigserial PRIMARY KEY, stamp_k timestamptz REFERENCES stamps (k), note text NOT NULL);
CREATE TABLE tags (id bigserial PRIMARY KEY, name text NOT NULL);
CREATE TABLE stamp_tags (stamp_k timestamptz NOT NULL REFERENCES stamps (k), tag_id bigint NOT NULL REFERENCES tags (id), PRIMARY KEY (stamp_k, tag_id));`,
			key:  "key",
			want: wantTime,
		},
		{
			name:    "mysql",
			dialect: config.DialectMySQL,
			ddl: `CREATE TABLE stamps (k datetime(6) PRIMARY KEY, label varchar(50) NOT NULL);
CREATE TABLE kids (id bigint AUTO_INCREMENT PRIMARY KEY, stamp_k datetime(6), note varchar(50) NOT NULL, FOREIGN KEY (stamp_k) REFERENCES stamps (k));
CREATE TABLE tags (id bigint AUTO_INCREMENT PRIMARY KEY, name varchar(50) NOT NULL);
CREATE TABLE stamp_tags (stamp_k datetime(6) NOT NULL, tag_id bigint NOT NULL, PRIMARY KEY (stamp_k, tag_id), FOREIGN KEY (stamp_k) REFERENCES stamps (k), FOREIGN KEY (tag_id) REFERENCES tags (id));`,
			key:  "key",
			want: wantTime,
		},
		{
			name:    "sqlite",
			dialect: config.DialectSQLite,
			ddl: `CREATE TABLE stamps (k datetime PRIMARY KEY, label text NOT NULL);
CREATE TABLE kids (id integer PRIMARY KEY, stamp_k datetime REFERENCES stamps (k), note text NOT NULL);
CREATE TABLE tags (id integer PRIMARY KEY, name text NOT NULL);
CREATE TABLE stamp_tags (stamp_k datetime NOT NULL REFERENCES stamps (k), tag_id integer NOT NULL REFERENCES tags (id), PRIMARY KEY (stamp_k, tag_id));`,
			imports: `"github.com/teandresmith/sqlgen/types"`,
			key:     "types.DateTime{Time: key}",
			want:    wantSQLiteText,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := strings.ReplaceAll(probeTmpl, "%%", "%")
			src = strings.Replace(src, "%s", tt.imports, 1)
			src = strings.Replace(src, "%s", tt.key, 1)
			src = strings.Replace(src, "%s", tt.want, 1)
			gnwRunGeneratedProbe(t, []gnwProbePackage{{dir: "db", dialect: tt.dialect, ddl: tt.ddl, extra: nested}}, src)
		})
	}
}
