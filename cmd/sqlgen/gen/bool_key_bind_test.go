package gen_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// TestGenerate_BoolKeyBindsAsBool generates a boolean-keyed table with an FK
// and a composite key holding a boolean member on each dialect, with nested
// writes on, and checks what the client binds for the key. The text "true" is
// the defect: MySQL and SQLite compare it with the stored 1 and match nothing.
// The compile is half the test: the key lists, the nullable FK loader and the
// nested executors all assign into comparator.Bool.
func TestGenerate_BoolKeyBindsAsBool(t *testing.T) {
	const probe = `package db

import (
	"context"
	"testing"
)

func TestBoolKeyBind(t *testing.T) {
	for _, get := range []func(*probeQuerier){
		func(q *probeQuerier) { _, _ = New(q).Flags().Get(context.Background(), true) },
		func(q *probeQuerier) { _, _ = New(q).FlagPairs().Get(context.Background(), FlagPairPK{OwnerID: 1, Active: true}) },
	} {
		q := &probeQuerier{}
		get(q)
		stmts := q.statements()
		if len(stmts) == 0 || len(stmts[0].args) == 0 {
			t.Fatalf("no bound argument recorded: %+v", stmts)
		}
		var found bool
		for _, a := range stmts[0].args {
			if a == true {
				found = true
			}
			if s, ok := a.(string); ok && s == "true" {
				t.Fatalf("bound the text %q for a boolean key", s)
			}
		}
		if !found {
			t.Fatalf("bound %#v, want the bool true among the args", stmts[0].args)
		}
	}
}
`
	const nested = "generation:\n  nested_mutations:\n    enabled: true\n"
	tests := []struct {
		name    string
		dialect config.Dialect
		ddl     string
	}{
		{
			name:    "postgres",
			dialect: config.DialectPostgres,
			ddl: `CREATE TABLE flags (k boolean PRIMARY KEY, label text NOT NULL);
CREATE TABLE kids (id bigserial PRIMARY KEY, flag_k boolean REFERENCES flags (k), note text NOT NULL);
CREATE TABLE flag_pairs (owner_id bigint NOT NULL, active boolean NOT NULL, label text NOT NULL, PRIMARY KEY (owner_id, active));`,
		},
		{
			name:    "mysql",
			dialect: config.DialectMySQL,
			ddl: `CREATE TABLE flags (k boolean PRIMARY KEY, label varchar(50) NOT NULL);
CREATE TABLE kids (id bigint AUTO_INCREMENT PRIMARY KEY, flag_k boolean, note varchar(50) NOT NULL, FOREIGN KEY (flag_k) REFERENCES flags (k));
CREATE TABLE flag_pairs (owner_id bigint NOT NULL, active boolean NOT NULL, label varchar(50) NOT NULL, PRIMARY KEY (owner_id, active));`,
		},
		{
			name:    "sqlite",
			dialect: config.DialectSQLite,
			ddl: `CREATE TABLE flags (k boolean PRIMARY KEY, label text NOT NULL);
CREATE TABLE kids (id integer PRIMARY KEY, flag_k boolean REFERENCES flags (k), note text NOT NULL);
CREATE TABLE flag_pairs (owner_id integer NOT NULL, active boolean NOT NULL, label text NOT NULL, PRIMARY KEY (owner_id, active));`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gnwRunGeneratedProbe(t, []gnwProbePackage{{dir: "db", dialect: tt.dialect, ddl: tt.ddl, extra: nested}}, strings.TrimSpace(probe)+"\n")
		})
	}
}
