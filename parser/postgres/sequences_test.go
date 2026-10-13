package postgres_test

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/parser/postgres"
)

// columnDefaults lists, per column of the named table, its default and
// whether it auto-increments, as "col default=… ai=…".
func columnDefaults(t *testing.T, s *parser.Schema, table string) []string {
	t.Helper()
	for _, tbl := range s.Tables {
		if tbl.Name != table {
			continue
		}
		out := make([]string, 0, len(tbl.Columns))
		for _, c := range tbl.Columns {
			out = append(out, fmt.Sprintf("%s default=%q ai=%v nullable=%v", c.Name, c.Default, c.AutoIncrement, c.Nullable))
		}
		return out
	}
	t.Fatalf("table %s not found", table)
	return nil
}

// TestSequenceOwnership checks that a nextval default reads as the
// introspector reads it once the column owns the sequence, whatever order the
// statements come in. Each want is what PostgreSQL 17 introspects.
func TestSequenceOwnership(t *testing.T) {
	tests := []struct {
		name  string
		sql   string
		table string
		want  []string
	}{
		{
			name: "pg_dump serial: OWNED BY, then SET DEFAULT",
			sql: `CREATE TABLE t (id integer NOT NULL, x integer);
				CREATE SEQUENCE t_id_seq AS integer START WITH 1 INCREMENT BY 1 NO MINVALUE NO MAXVALUE CACHE 1;
				ALTER SEQUENCE t_id_seq OWNED BY t.id;
				ALTER TABLE ONLY t ALTER COLUMN id SET DEFAULT nextval('t_id_seq'::regclass);`,
			table: "t",
			want:  []string{`id default="" ai=true nullable=false`, `x default="" ai=false nullable=true`},
		},
		{
			name: "SET DEFAULT, then OWNED BY",
			sql: `CREATE SEQUENCE s1;
				CREATE TABLE a (id int NOT NULL);
				ALTER TABLE a ALTER COLUMN id SET DEFAULT nextval('s1');
				ALTER SEQUENCE s1 OWNED BY a.id;`,
			table: "a",
			want:  []string{`id default="" ai=true nullable=false`},
		},
		{
			name: "CREATE SEQUENCE … OWNED BY",
			sql: `CREATE TABLE g (id int);
				CREATE SEQUENCE gs OWNED BY g.id;
				ALTER TABLE g ALTER COLUMN id SET DEFAULT nextval('gs');`,
			table: "g",
			want:  []string{`id default="" ai=true nullable=true`},
		},
		{
			name: "a sequence another column owns",
			sql: `CREATE TABLE f (id int, other int);
				CREATE SEQUENCE fs OWNED BY f.other;
				ALTER TABLE f ALTER COLUMN id SET DEFAULT nextval('fs');`,
			table: "f",
			want:  []string{`id default="nextval('fs')" ai=true nullable=true`, `other default="" ai=false nullable=true`},
		},
		{
			name: "ownership follows a table rename",
			sql: `CREATE TABLE h (id int);
				CREATE SEQUENCE hs OWNED BY h.id;
				ALTER TABLE h RENAME TO h2;
				ALTER TABLE h2 ALTER COLUMN id SET DEFAULT nextval('hs');`,
			table: "h2",
			want:  []string{`id default="" ai=true nullable=true`},
		},
		{
			name: "ownership follows a column rename",
			sql: `CREATE TABLE i (id int);
				CREATE SEQUENCE is1 OWNED BY i.id;
				ALTER TABLE i RENAME COLUMN id TO id2;
				ALTER TABLE i ALTER COLUMN id2 SET DEFAULT nextval('is1');`,
			table: "i",
			want:  []string{`id2 default="" ai=true nullable=true`},
		},
		{
			name: "ownership and the sequence follow SET SCHEMA",
			sql: `CREATE SCHEMA other;
				CREATE TABLE j (id int);
				CREATE SEQUENCE js OWNED BY j.id;
				ALTER TABLE j SET SCHEMA other;
				ALTER TABLE other.j ALTER COLUMN id SET DEFAULT nextval('other.js');`,
			table: "j",
			want:  []string{`id default="" ai=true nullable=true`},
		},
		{
			name: "DROP DEFAULT on a serial column",
			sql: `CREATE TABLE c (id serial);
				ALTER TABLE c ALTER COLUMN id DROP DEFAULT;`,
			table: "c",
			want:  []string{`id default="" ai=false nullable=false`},
		},
		{
			name: "SET DEFAULT of a constant on a serial column",
			sql: `CREATE TABLE d (id serial);
				ALTER TABLE d ALTER COLUMN id SET DEFAULT 5;`,
			table: "d",
			want:  []string{`id default="5" ai=false nullable=false`},
		},
		{
			name: "OWNED BY NONE gives a serial default back",
			sql: `CREATE SCHEMA s;
				CREATE TABLE s.b (id serial);
				ALTER SEQUENCE s.b_id_seq OWNED BY NONE;`,
			table: "b",
			want:  []string{`id default="nextval('s.b_id_seq')" ai=true nullable=false`},
		},
		{
			name: "OWNED BY another column gives the serial default back",
			sql: `CREATE TABLE k (id serial, x int);
				ALTER SEQUENCE k_id_seq OWNED BY k.x;`,
			table: "k",
			want:  []string{`id default="nextval('k_id_seq')" ai=true nullable=false`, `x default="" ai=false nullable=true`},
		},
		{
			name: "a serial sequence name clashes with a table",
			sql: `CREATE TABLE dd_id_seq (z int);
				CREATE TABLE dd (id serial);
				ALTER SEQUENCE dd_id_seq1 OWNED BY NONE;`,
			table: "dd",
			want:  []string{`id default="nextval('dd_id_seq1')" ai=true nullable=false`},
		},
		{
			name: "DROP SEQUENCE … CASCADE drops the serial default",
			sql: `CREATE TABLE m (id serial, q int);
				DROP SEQUENCE m_id_seq CASCADE;`,
			table: "m",
			want:  []string{`id default="" ai=false nullable=false`, `q default="" ai=false nullable=true`},
		},
		{
			name: "a serial column added with ADD COLUMN owns its sequence",
			sql: `CREATE TABLE ser (v int);
				ALTER TABLE ser ADD COLUMN s2 bigserial;
				ALTER SEQUENCE ser_s2_seq OWNED BY NONE;`,
			table: "ser",
			want:  []string{`v default="" ai=false nullable=true`, `s2 default="nextval('ser_s2_seq')" ai=true nullable=false`},
		},
		{
			name: "pg_dump identity: ADD GENERATED … AS IDENTITY",
			sql: `CREATE TABLE u (id integer NOT NULL, y integer);
				ALTER TABLE u ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
					SEQUENCE NAME public.u_id_seq START WITH 1 INCREMENT BY 1 NO MINVALUE NO MAXVALUE CACHE 1);`,
			table: "u",
			want:  []string{`id default="" ai=true nullable=false`, `y default="" ai=false nullable=true`},
		},
		{
			name: "DROP IDENTITY",
			sql: `CREATE TABLE w (id bigint GENERATED ALWAYS AS IDENTITY, z integer);
				ALTER TABLE w ALTER COLUMN id DROP IDENTITY;`,
			table: "w",
			want:  []string{`id default="" ai=false nullable=false`, `z default="" ai=false nullable=true`},
		},
		{
			name: "DROP IDENTITY IF EXISTS leaves a serial column alone",
			sql: `CREATE TABLE ser (id serial, v int GENERATED BY DEFAULT AS IDENTITY);
				ALTER TABLE ser ALTER COLUMN id DROP IDENTITY IF EXISTS;
				ALTER TABLE ser ALTER COLUMN v DROP IDENTITY IF EXISTS;`,
			table: "ser",
			want:  []string{`id default="" ai=true nullable=false`, `v default="" ai=false nullable=false`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := postgres.New("")
			if err := p.Parse("test.sql", []byte(tt.sql)); err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			if diff := cmp.Diff(tt.want, columnDefaults(t, p.Schema(), tt.table)); diff != "" {
				t.Errorf("columns mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestSequenceNamesTakeRelationNames checks that a sequence's name is taken
// for an index, as PostgreSQL 17 names the index.
func TestSequenceNamesTakeRelationNames(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want []string
	}{
		{
			name: "CREATE SEQUENCE",
			sql: `CREATE TABLE n (a int);
				CREATE SEQUENCE n_a_idx;
				CREATE INDEX ON n (a);`,
			want: []string{"n INDEX n_a_idx1"},
		},
		{
			name: "CREATE SEQUENCE before a UNIQUE",
			sql: `CREATE SEQUENCE g_id_key;
				CREATE TABLE g (id int NOT NULL);
				ALTER TABLE g ADD UNIQUE (id);`,
			want: []string{"g UNIQUE g_id_key1"},
		},
		{
			name: "a renamed sequence",
			sql: `CREATE TABLE r (c int);
				CREATE SEQUENCE rs;
				ALTER SEQUENCE rs RENAME TO r_c_idx;
				CREATE INDEX ON r (c);`,
			want: []string{"r INDEX r_c_idx1"},
		},
		{
			name: "a dropped sequence frees its name",
			sql: `CREATE TABLE n (a int);
				CREATE SEQUENCE n_a_idx;
				DROP SEQUENCE n_a_idx;
				CREATE INDEX ON n (a);`,
			want: []string{"n INDEX n_a_idx"},
		},
		{
			name: "an owned sequence goes with DROP COLUMN",
			sql: `CREATE TABLE dc (id int, v int);
				CREATE SEQUENCE dc_v_idx OWNED BY dc.id;
				ALTER TABLE dc DROP COLUMN id;
				CREATE INDEX ON dc (v);`,
			want: []string{"dc INDEX dc_v_idx"},
		},
		{
			name: "an owned sequence goes with DROP TABLE",
			sql: `CREATE TABLE dt (id int);
				CREATE SEQUENCE x_a_idx OWNED BY dt.id;
				DROP TABLE dt;
				CREATE TABLE x (a int);
				CREATE INDEX ON x (a);`,
			want: []string{"x INDEX x_a_idx"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := postgres.New("")
			if err := p.Parse("test.sql", []byte(tt.sql)); err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			var got []string
			for _, tbl := range p.Schema().Tables {
				for _, c := range tbl.Constraints {
					got = append(got, tbl.Name+" "+c.Type.String()+" "+c.Name)
				}
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("constraint names mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDuplicateCreateSequence(t *testing.T) {
	p := postgres.New("")
	if err := p.Parse("test.sql", []byte(`CREATE SEQUENCE s; CREATE SEQUENCE IF NOT EXISTS s;`)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	err := p.Parse("test.sql", []byte(`CREATE SEQUENCE s;`))
	if err == nil || err.Error() != "parsing test.sql: sequence public.s already exists" {
		t.Errorf("Parse() error = %v, want sequence public.s already exists", err)
	}
}
