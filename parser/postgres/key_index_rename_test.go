package postgres_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/parser/postgres"
)

// pgwConstraintNames parses sql with the default schema and returns the
// type and name of every constraint of its first table.
func pgwConstraintNames(t *testing.T, sql string) []string {
	t.Helper()
	p := postgres.New("")
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	tables := p.Schema().Tables
	if len(tables) == 0 {
		t.Fatalf("Parse() produced no table")
	}
	got := make([]string, 0, len(tables[0].Constraints))
	for _, c := range tables[0].Constraints {
		got = append(got, c.Type.String()+" "+c.Name)
	}
	return got
}

// Renaming a PRIMARY KEY renames the index behind it: the new name is the
// index's, so an unnamed index steps past it and ALTER INDEX answers to it,
// and the old name is free for an unnamed index to take.
func TestRenamePrimaryKeyRenamesItsIndex(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want []string
	}{
		{
			name: "the new name is taken by the index",
			sql: `CREATE TABLE t (c int, CONSTRAINT k PRIMARY KEY (c));
				ALTER TABLE t RENAME CONSTRAINT k TO t_c_idx;
				CREATE INDEX ON t (c);`,
			want: []string{"PRIMARY KEY t_c_idx", "INDEX t_c_idx1"},
		},
		{
			name: "the old name is free",
			sql: `CREATE TABLE t (c int, CONSTRAINT t_c_idx PRIMARY KEY (c));
				ALTER TABLE t RENAME CONSTRAINT t_c_idx TO k;
				CREATE INDEX ON t (c);`,
			want: []string{"PRIMARY KEY k", "INDEX t_c_idx"},
		},
		{
			name: "ALTER INDEX answers to the new name",
			sql: `CREATE TABLE t (c int PRIMARY KEY);
				ALTER TABLE t RENAME CONSTRAINT t_pkey TO k;
				ALTER INDEX t_pkey RENAME TO stale;
				ALTER INDEX k RENAME TO k2;`,
			want: []string{"PRIMARY KEY k2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pgwConstraintNames(t, tt.sql)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("constraints mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// ALTER INDEX … RENAME TO on the index behind a PRIMARY KEY renames the
// constraint: it takes the new name, and ALTER TABLE answers to that name
// and no longer to the old one.
func TestRenamePrimaryKeyIndexRenamesConstraint(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want []string
	}{
		{
			name: "the constraint takes the index's new name",
			sql: `CREATE TABLE t (c int PRIMARY KEY, d int);
				ALTER INDEX t_pkey RENAME TO t_key_ix;`,
			want: []string{"PRIMARY KEY t_key_ix"},
		},
		{
			name: "RENAME CONSTRAINT answers to the new name",
			sql: `CREATE TABLE t (c int PRIMARY KEY, d int);
				ALTER INDEX t_pkey RENAME TO t_key_ix;
				ALTER TABLE t RENAME CONSTRAINT t_pkey TO stale;
				ALTER TABLE t RENAME CONSTRAINT t_key_ix TO t_key2;`,
			want: []string{"PRIMARY KEY t_key2"},
		},
		{
			name: "DROP CONSTRAINT by the new name drops the key",
			sql: `CREATE TABLE t (c int PRIMARY KEY, d int);
				ALTER INDEX t_pkey RENAME TO t_key_ix;
				ALTER TABLE t DROP CONSTRAINT t_key_ix;
				CREATE INDEX t_pkey ON t (d);`,
			want: []string{"INDEX t_pkey"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pgwConstraintNames(t, tt.sql)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("constraints mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
