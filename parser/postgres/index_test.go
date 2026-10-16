package postgres_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/parser/postgres"
)

// TestCreateIndexOnUnknownRelation pins that CREATE INDEX on a relation no
// earlier statement created is an error, as it is in PostgreSQL 16 and 17
// (`relation "users" does not exist`, `cannot create index on relation "v"`),
// while an index on a materialized view or a partition, which the parser skips
// on purpose, is skipped with it.
func TestCreateIndexOnUnknownRelation(t *testing.T) {
	const base = `CREATE TABLE t (id int PRIMARY KEY);
CREATE TABLE p (id int, k int) PARTITION BY RANGE (k);
CREATE TABLE p1 PARTITION OF p FOR VALUES FROM (0) TO (10);
CREATE SCHEMA s;
CREATE TABLE s.p2 PARTITION OF p FOR VALUES FROM (10) TO (20);
CREATE MATERIALIZED VIEW mv AS SELECT id FROM t;
CREATE VIEW v AS SELECT id FROM t;
`
	tests := []struct {
		name    string
		ddl     string
		wantErr string
	}{
		{name: "table created later", ddl: `CREATE INDEX users_email ON users (id); CREATE TABLE users (id int);`, wantErr: "create index users_email: table public.users not found"},
		{name: "unnamed on missing table", ddl: `CREATE INDEX ON nope (id);`, wantErr: "create index: table public.nope not found"},
		{name: "if not exists on missing table", ddl: `CREATE INDEX IF NOT EXISTS ix ON nope (id);`, wantErr: "create index ix: table public.nope not found"},
		{name: "table in another schema", ddl: `CREATE INDEX ix ON s.t (id);`, wantErr: "create index ix: table s.t not found"},
		{name: "plain view", ddl: `CREATE INDEX v_id ON v (id);`, wantErr: "create index v_id: table public.v not found"},
		{name: "materialized view", ddl: `CREATE UNIQUE INDEX mv_id ON mv (id);`},
		{name: "unnamed on materialized view", ddl: `CREATE INDEX ON mv (id);`},
		{name: "partition", ddl: `CREATE INDEX p1_k ON p1 (k);`},
		{name: "partition in another schema", ddl: `CREATE INDEX p2_k ON s.p2 (k);`},
		{name: "dropped materialized view", ddl: `DROP MATERIALIZED VIEW mv; CREATE INDEX mv_id ON mv (id);`, wantErr: "create index mv_id: table public.mv not found"},
		{name: "renamed materialized view", ddl: `ALTER MATERIALIZED VIEW mv RENAME TO mv2; CREATE INDEX mv2_id ON mv2 (id);`},
		{name: "old name of a renamed materialized view", ddl: `ALTER MATERIALIZED VIEW mv RENAME TO mv2; CREATE INDEX mv_id ON mv (id);`, wantErr: "create index mv_id: table public.mv not found"},
		{name: "materialized view moved to another schema", ddl: `ALTER MATERIALIZED VIEW mv SET SCHEMA s; CREATE INDEX mv_id ON s.mv (id);`},
		{name: "renamed view", ddl: `ALTER VIEW v RENAME TO v2; CREATE INDEX v2_id ON v2 (id);`, wantErr: "create index v2_id: table public.v2 not found"},
		{name: "dropped partition", ddl: `DROP TABLE p1; CREATE INDEX p1_k ON p1 (k);`, wantErr: "create index p1_k: table public.p1 not found"},
		{name: "partition of a dropped table", ddl: `DROP TABLE p; CREATE INDEX p2_k ON s.p2 (k);`, wantErr: "create index p2_k: table s.p2 not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := postgres.New("")
			err := p.Parse("test.sql", []byte(base+tt.ddl))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Parse(%q) error: %v", tt.ddl, err)
				}
				if got := p.Schema().Tables[0].Constraints; len(got) != 1 {
					t.Errorf("Parse(%q): t constraints = %+v, want the primary key only", tt.ddl, got)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Parse(%q) error = %v, want containing %q", tt.ddl, err, tt.wantErr)
			}
		})
	}
}

// TestCreateIndexOnSeededRelation pins that under source: both a CREATE INDEX
// on the database's materialized view or partition is skipped, as it is under
// files, and one on its plain view is an error.
func TestCreateIndexOnSeededRelation(t *testing.T) {
	base := func() *parser.Schema {
		return &parser.Schema{
			Tables:     []parser.Table{{Schema: "public", Name: "p", Columns: []parser.Column{{Name: "k", Type: "integer"}}}},
			Views:      []parser.View{{Schema: "public", Name: "mv", Materialized: true}, {Schema: "public", Name: "v"}},
			Partitions: []parser.Partition{{Schema: "public", Name: "p1", ParentSchema: "public", ParentName: "p"}},
		}
	}
	tests := []struct {
		name    string
		ddl     string
		wantErr string
	}{
		{name: "materialized view", ddl: `CREATE UNIQUE INDEX mv_k ON mv (k);`},
		{name: "partition", ddl: `CREATE INDEX p1_k ON p1 (k);`},
		{name: "plain view", ddl: `CREATE INDEX v_k ON v (k);`, wantErr: "create index v_k: table public.v not found"},
		{name: "unknown table", ddl: `CREATE INDEX x_k ON x (k);`, wantErr: "create index x_k: table public.x not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := postgres.New("")
			p.Seed(base())
			err := p.Parse("test.sql", []byte(tt.ddl))
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("Parse(%q) after Seed error: %v", tt.ddl, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Parse(%q) after Seed error = %v, want containing %q", tt.ddl, err, tt.wantErr)
			}
		})
	}
}

// TestPartitionsListed pins the partitions the parser lists, each with its
// parent, and that DROP TABLE forgets a partition and every partition of a
// dropped table, as PostgreSQL drops them with it.
func TestPartitionsListed(t *testing.T) {
	const base = `CREATE TABLE p (id int, k int) PARTITION BY RANGE (k);
CREATE TABLE p1 PARTITION OF p FOR VALUES FROM (0) TO (10) PARTITION BY RANGE (id);
CREATE TABLE p1a PARTITION OF p1 FOR VALUES FROM (0) TO (10);
CREATE SCHEMA s;
CREATE TABLE s.p2 PARTITION OF p FOR VALUES FROM (10) TO (20);
CREATE TABLE q (id int, k int) PARTITION BY RANGE (k);
CREATE TABLE q1 PARTITION OF q FOR VALUES FROM (0) TO (10);
`
	p1 := parser.Partition{Schema: "public", Name: "p1", ParentSchema: "public", ParentName: "p"}
	p1a := parser.Partition{Schema: "public", Name: "p1a", ParentSchema: "public", ParentName: "p1"}
	p2 := parser.Partition{Schema: "s", Name: "p2", ParentSchema: "public", ParentName: "p"}
	q1 := parser.Partition{Schema: "public", Name: "q1", ParentSchema: "public", ParentName: "q"}
	tests := []struct {
		name string
		ddl  string
		want []parser.Partition
	}{
		{name: "created", want: []parser.Partition{p1, p1a, p2, q1}},
		{name: "partition dropped", ddl: `DROP TABLE s.p2;`, want: []parser.Partition{p1, p1a, q1}},
		{name: "partition with its own partitions dropped", ddl: `DROP TABLE p1;`, want: []parser.Partition{p2, q1}},
		{name: "partitioned table dropped", ddl: `DROP TABLE p;`, want: []parser.Partition{q1}},
		{name: "every partition dropped", ddl: `DROP TABLE p; DROP TABLE q;`, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := postgres.New("")
			if err := p.Parse("test.sql", []byte(base+tt.ddl)); err != nil {
				t.Fatalf("Parse(%q) error: %v", tt.ddl, err)
			}
			if diff := cmp.Diff(tt.want, p.Schema().Partitions); diff != "" {
				t.Errorf("Parse(%q): partitions mismatch (-want +got):\n%s", tt.ddl, diff)
			}
		})
	}
}

// TestDropIndex pins DROP INDEX: the index leaves its table and the column
// flags it set are re-derived, as measured on PostgreSQL 16 and 17. A skipped
// expression index releases its name for a later derived one, and an index
// the parser never recorded leaves the schema as it is.
func TestDropIndex(t *testing.T) {
	const base = `CREATE TABLE g (id int PRIMARY KEY, tags text[], b text, c text UNIQUE);
CREATE SCHEMA s;
CREATE TABLE s.h (a int);
CREATE INDEX g_tags ON g USING gin (tags);
CREATE UNIQUE INDEX g_b ON g (b);
CREATE UNIQUE INDEX g_b_p ON g (b) WHERE id > 0;
CREATE UNIQUE INDEX g_c ON g (c);
CREATE INDEX h_a ON s.h (a);
`
	pkey := parser.Constraint{Name: "g_pkey", Type: parser.PrimaryKey, Columns: []string{"id"}}
	cKey := parser.Constraint{Name: "g_c_key", Type: parser.Unique, Columns: []string{"c"}, Method: "btree"}
	tags := parser.Constraint{Name: "g_tags", Type: parser.Index, Columns: []string{"tags"}, Method: "gin"}
	b := parser.Constraint{Name: "g_b", Type: parser.Unique, Columns: []string{"b"}, Method: "btree"}
	bPartial := parser.Constraint{Name: "g_b_p", Type: parser.Unique, Columns: []string{"b"}, Method: "btree", Where: "id > 0"}
	c := parser.Constraint{Name: "g_c", Type: parser.Unique, Columns: []string{"c"}, Method: "btree"}
	tests := []struct {
		name       string
		ddl        string
		want       []parser.Constraint
		wantUnique []string
	}{
		{name: "unique index", ddl: `DROP INDEX g_b;`, want: []parser.Constraint{pkey, cKey, tags, bPartial, c}, wantUnique: []string{"c"}},
		{name: "two indexes", ddl: `DROP INDEX g_tags, public.g_b;`, want: []parser.Constraint{pkey, cKey, bPartial, c}, wantUnique: []string{"c"}},
		{name: "index beside a unique constraint", ddl: `DROP INDEX g_c;`, want: []parser.Constraint{pkey, cKey, tags, b, bPartial}, wantUnique: []string{"b", "c"}},
		{name: "concurrently", ddl: `DROP INDEX CONCURRENTLY IF EXISTS g_tags;`, want: []parser.Constraint{pkey, cKey, b, bPartial, c}, wantUnique: []string{"b", "c"}},
		{name: "unknown", ddl: `DROP INDEX nope;`, want: []parser.Constraint{pkey, cKey, tags, b, bPartial, c}, wantUnique: []string{"b", "c"}},
		{name: "index in another schema unqualified", ddl: `DROP INDEX h_a;`, want: []parser.Constraint{pkey, cKey, tags, b, bPartial, c}, wantUnique: []string{"b", "c"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := postgres.New("")
			if err := p.Parse("test.sql", []byte(base+tt.ddl)); err != nil {
				t.Fatalf("Parse(%q) error: %v", tt.ddl, err)
			}
			g := p.Schema().Tables[0]
			if diff := cmp.Diff(tt.want, g.Constraints); diff != "" {
				t.Errorf("Parse(%q): g constraints mismatch (-want +got):\n%s", tt.ddl, diff)
			}
			var unique []string
			for _, col := range g.Columns {
				if col.Unique {
					unique = append(unique, col.Name)
				}
			}
			if diff := cmp.Diff(tt.wantUnique, unique); diff != "" {
				t.Errorf("Parse(%q): unique columns mismatch (-want +got):\n%s", tt.ddl, diff)
			}
		})
	}

	t.Run("qualified index in another schema", func(t *testing.T) {
		p := postgres.New("")
		if err := p.Parse("test.sql", []byte(base+`DROP INDEX s.h_a;`)); err != nil {
			t.Fatalf("Parse() error: %v", err)
		}
		if got := p.Schema().Tables[1].Constraints; len(got) != 0 {
			t.Errorf("s.h constraints = %+v, want none", got)
		}
	})

	t.Run("expression index releases its name", func(t *testing.T) {
		p := postgres.New("")
		ddl := `CREATE TABLE n (a int); CREATE INDEX ON n ((a::text)); DROP INDEX n_a_idx; CREATE INDEX ON n (a);`
		if err := p.Parse("test.sql", []byte(ddl)); err != nil {
			t.Fatalf("Parse(%q) error: %v", ddl, err)
		}
		want := []parser.Constraint{{Name: "n_a_idx", Type: parser.Index, Columns: []string{"a"}, Method: "btree"}}
		if diff := cmp.Diff(want, p.Schema().Tables[0].Constraints); diff != "" {
			t.Errorf("Parse(%q): n constraints mismatch (-want +got):\n%s", ddl, diff)
		}
	})
}
