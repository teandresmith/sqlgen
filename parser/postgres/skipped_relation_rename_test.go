package postgres_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/parser/postgres"
)

// TestRenameOnSkippedRelation pins that a rename on a relation the parser
// skips, a plain view, a materialized view or a partition, is skipped like
// every other ALTER action on it: no error, and the model is unchanged.
func TestRenameOnSkippedRelation(t *testing.T) {
	const base = `CREATE TABLE t (id int PRIMARY KEY, a int);
CREATE VIEW v AS SELECT id, a FROM t;
CREATE MATERIALIZED VIEW mv AS SELECT id FROM t;
CREATE TABLE p (id int, k int) PARTITION BY RANGE (k);
CREATE TABLE p1 PARTITION OF p FOR VALUES FROM (0) TO (10);
`
	tests := []struct {
		name string
		sql  string
	}{
		{name: "alter view rename column", sql: `ALTER VIEW v RENAME COLUMN a TO b;`},
		{name: "alter view rename to", sql: `ALTER VIEW v RENAME TO v2;`},
		{name: "alter table rename view", sql: `ALTER TABLE v RENAME TO v2;`},
		{name: "alter table rename view column", sql: `ALTER TABLE v RENAME COLUMN a TO b;`},
		{name: "alter materialized view rename column", sql: `ALTER MATERIALIZED VIEW mv RENAME COLUMN id TO id2;`},
		{name: "alter materialized view rename to", sql: `ALTER MATERIALIZED VIEW mv RENAME TO mv2;`},
		{name: "alter table rename partition", sql: `ALTER TABLE p1 RENAME TO p1b;`},
		{name: "alter table rename partition column", sql: `ALTER TABLE p1 RENAME COLUMN k TO k2;`},
		{name: "alter table rename partition constraint", sql: `ALTER TABLE p1 RENAME CONSTRAINT p1_check TO p1_c;`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if diff := cmp.Diff(columnKeys(t, base), columnKeys(t, base+tt.sql)); diff != "" {
				t.Errorf("columns (-want +got):\n%s", diff)
			}
		})
	}
}

// columnKeys parses sql and returns schema.table.column for every column of
// every table in the model.
func columnKeys(t *testing.T, sql string) []string {
	t.Helper()
	var out []string
	for _, tbl := range parsePGSchema(t, sql).Tables {
		for _, c := range tbl.Columns {
			out = append(out, tbl.Schema+"."+tbl.Name+"."+c.Name)
		}
	}
	return out
}

// TestRenamedSkippedRelationAnswersToNewName pins that a skipped relation
// renamed with RENAME TO is found under its new name, so an index on it is
// skipped with it, and no longer answers to its old one.
func TestRenamedSkippedRelationAnswersToNewName(t *testing.T) {
	const base = `CREATE TABLE p (id int, k int) PARTITION BY RANGE (k);
CREATE TABLE p1 PARTITION OF p FOR VALUES FROM (0) TO (10);
CREATE TABLE t (id int PRIMARY KEY);
CREATE MATERIALIZED VIEW mv AS SELECT id FROM t;
`
	tests := []struct {
		name    string
		sql     string
		wantErr bool
	}{
		{name: "index on the renamed partition", sql: "ALTER TABLE p1 RENAME TO p1b;\nCREATE INDEX ON p1b (k);"},
		{name: "index on the partition's old name", sql: "ALTER TABLE p1 RENAME TO p1b;\nCREATE INDEX ON p1 (k);", wantErr: true},
		{name: "index on the renamed materialized view", sql: "ALTER TABLE mv RENAME TO mv2;\nCREATE INDEX ON mv2 (id);"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := postgres.New("").Parse("schema.sql", []byte(base+tt.sql))
			if (err != nil) != tt.wantErr {
				t.Errorf("Parse error = %v, want error %v", err, tt.wantErr)
			}
		})
	}
}

// TestMoveSkippedRelation pins that RENAME TO or SET SCHEMA on a partition or
// materialized view refuses a name a relation already has, as PostgreSQL
// does, and that the partition list follows the move.
func TestMoveSkippedRelation(t *testing.T) {
	const base = `CREATE SCHEMA other;
CREATE TABLE p (id int, k int) PARTITION BY RANGE (k);
CREATE TABLE p1 PARTITION OF p FOR VALUES FROM (0) TO (10);
CREATE TABLE t (id int PRIMARY KEY);
CREATE VIEW v AS SELECT id FROM t;
CREATE MATERIALIZED VIEW mv AS SELECT id FROM t;
`
	tests := []struct {
		name           string
		sql            string
		wantErr        string
		wantPartitions []parser.Partition
	}{
		{name: "partition renamed onto a table", sql: "ALTER TABLE p1 RENAME TO t;", wantErr: "parsing schema.sql: relation public.t already exists"},
		{name: "partition renamed onto a view", sql: "ALTER TABLE p1 RENAME TO v;", wantErr: "parsing schema.sql: relation public.v already exists"},
		{name: "partition renamed onto its parent", sql: "ALTER TABLE p1 RENAME TO p;", wantErr: "parsing schema.sql: relation public.p already exists"},
		{name: "materialized view renamed onto a table", sql: "ALTER TABLE mv RENAME TO t;", wantErr: "parsing schema.sql: relation public.t already exists"},
		{name: "alter view renames a view onto a table", sql: "ALTER VIEW v RENAME TO t;", wantErr: "parsing schema.sql: relation public.t already exists"},
		{name: "alter materialized view renames onto a table", sql: "ALTER MATERIALIZED VIEW mv RENAME TO t;", wantErr: "parsing schema.sql: relation public.t already exists"},
		{name: "alter materialized view renames onto a view", sql: "ALTER MATERIALIZED VIEW mv RENAME TO v;", wantErr: "parsing schema.sql: relation public.v already exists"},
		{
			name:    "materialized view moved onto a relation in another schema",
			sql:     "CREATE TABLE other.mv (id int);\nALTER MATERIALIZED VIEW mv SET SCHEMA other;",
			wantErr: "parsing schema.sql: relation other.mv already exists",
		},
		{
			name: "renamed sub-partitioned partition is its partitions' parent",
			sql: "CREATE TABLE p2 PARTITION OF p FOR VALUES FROM (10) TO (20) PARTITION BY RANGE (id);\n" +
				"CREATE TABLE p2a PARTITION OF p2 FOR VALUES FROM (0) TO (5);\n" +
				"ALTER TABLE p2 RENAME TO p2b;",
			wantPartitions: []parser.Partition{
				{Schema: "public", Name: "p1", ParentSchema: "public", ParentName: "p"},
				{Schema: "public", Name: "p2b", ParentSchema: "public", ParentName: "p"},
				{Schema: "public", Name: "p2a", ParentSchema: "public", ParentName: "p2b"},
			},
		},
		{
			name:           "renamed partition is listed under its new name",
			sql:            "ALTER TABLE p1 RENAME TO p1b;",
			wantPartitions: []parser.Partition{{Schema: "public", Name: "p1b", ParentSchema: "public", ParentName: "p"}},
		},
		{
			name:           "partition moved to another schema",
			sql:            "ALTER TABLE p1 SET SCHEMA other;\nCREATE INDEX ON other.p1 (k);",
			wantPartitions: []parser.Partition{{Schema: "other", Name: "p1", ParentSchema: "public", ParentName: "p"}},
		},
		{name: "moved partition no longer answers to its old schema", sql: "ALTER TABLE p1 SET SCHEMA other;\nCREATE INDEX ON public.p1 (k);", wantErr: "parsing schema.sql: create index: table public.p1 not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := postgres.New("")
			err := p.Parse("schema.sql", []byte(base+tt.sql))
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("Parse error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if diff := cmp.Diff(tt.wantPartitions, p.Schema().Partitions); diff != "" {
				t.Errorf("Partitions (-want +got):\n%s", diff)
			}
		})
	}
}
