package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A detected foreign key over columns that cannot key a Go map — a
// PostgreSQL array or a MySQL SET into a UNIQUE column of the same type,
// whatever Go type column_map, type_map or overrides.types gives it — gives
// no to-many relationship and a warning naming the constraint and the
// column, as a foreign key into non-key columns does, rather than an edge
// whose loader and filter do not compile (PRD §4.13). A one-to-one edge over
// the same columns is loaded by a join, so it keeps its edge with no warning,
// declared or detected. Every package builds.
func TestGenerate_UnkeyableForeignKeyGivesNoRelationship(t *testing.T) {
	const body = `version: 1
input:
  dialect: %s
  paths: [schema.sql]
output:
  driver: %s
  dir: out
  package: models
generation:
  nested_mutations:
    enabled: true
%s`
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, dialect, driver, schema, tables string
		// want is the warning stderr carries once; "" wants no warning.
		want string
		// kept is a declaration the models file carries when the edge is kept.
		kept string
	}{
		{
			"postgres text array", "postgres", "pgx",
			`CREATE TABLE things (id integer PRIMARY KEY, tags text[] NOT NULL UNIQUE);
CREATE TABLE refs (id integer PRIMARY KEY, tags text[] REFERENCES things(tags));`, "",
			"Warning: tables.public.things: foreign key refs_tags_fkey gives no relationship \"refs\": its column public.refs.tags has Go type []string, which cannot key a Go map, so the generated package would not compile; list \"refs\" in `exclude_relationships` to silence this (PRD §4.13, §5.3)\n",
			"",
		},
		{
			"postgres text array stdlib", "postgres", "stdlib",
			`CREATE TABLE things (id integer PRIMARY KEY, tags text[] NOT NULL UNIQUE);
CREATE TABLE refs (id integer PRIMARY KEY, tags text[] REFERENCES things(tags));`, "",
			`Warning: tables.public.things: foreign key refs_tags_fkey gives no relationship "refs": its column public.refs.tags has Go type []string`,
			"",
		},
		{
			"mysql set", "mysql", "stdlib",
			`CREATE TABLE things (id int PRIMARY KEY, sk set('a', 'b') NOT NULL, UNIQUE KEY uq_sk (sk));
CREATE TABLE refs (id int PRIMARY KEY, sk set('a', 'b'), CONSTRAINT fk_sk FOREIGN KEY (sk) REFERENCES things(sk));`, "",
			"Warning: tables.things: foreign key fk_sk gives no relationship \"refs\": its column refs.sk is a MySQL SET column; a SET, array or JSON column never keys a Go map, whatever Go type (here RefsSkSet) column_map, type_map or overrides.types gives it; list \"refs\" in `exclude_relationships` to silence this (PRD §4.13, §5.3)\n",
			"",
		},
		{
			"mysql set retyped to its own set type", "mysql", "stdlib",
			`CREATE TABLE things (id int PRIMARY KEY, sk set('a', 'b') NOT NULL, UNIQUE KEY uq_sk (sk));
CREATE TABLE refs (id int PRIMARY KEY, sk set('a', 'b'), CONSTRAINT fk_sk FOREIGN KEY (sk) REFERENCES things(sk));`,
			"tables:\n  refs:\n    type_map:\n      sk: RefsSkSet\n",
			`Warning: tables.things: foreign key fk_sk gives no relationship "refs": its column refs.sk is a MySQL SET column; a SET, array or JSON column never keys a Go map, whatever Go type (here RefsSkSet)`,
			"",
		},
		{
			"mysql set retyped to string", "mysql", "stdlib",
			`CREATE TABLE things (id int PRIMARY KEY, sk set('a', 'b') NOT NULL, UNIQUE KEY uq_sk (sk));
CREATE TABLE refs (id int PRIMARY KEY, sk set('a', 'b'), CONSTRAINT fk_sk FOREIGN KEY (sk) REFERENCES things(sk));`,
			"tables:\n  things:\n    type_map:\n      sk: string\n  refs:\n    type_map:\n      sk: string\n",
			`Warning: tables.things: foreign key fk_sk gives no relationship "refs": its column refs.sk is a MySQL SET column; a SET, array or JSON column never keys a Go map, whatever Go type (here string)`,
			"",
		},
		{
			"postgres text array retyped to string", "postgres", "pgx",
			`CREATE TABLE things (id integer PRIMARY KEY, tags text[] NOT NULL UNIQUE);
CREATE TABLE refs (id integer PRIMARY KEY, tags text[] REFERENCES things(tags));`,
			"tables:\n  things:\n    type_map:\n      tags: string\n  refs:\n    type_map:\n      tags: string\n",
			`Warning: tables.public.things: foreign key refs_tags_fkey gives no relationship "refs": its column public.refs.tags is an array column; a SET, array or JSON column never keys a Go map, whatever Go type (here string)`,
			"",
		},
		{
			// A junction whose two foreign keys sit inside a UNIQUE key is
			// detected as many-to-many through it.
			"postgres m2m", "postgres", "pgx",
			`CREATE TABLE things (id integer PRIMARY KEY, tags text[] NOT NULL UNIQUE);
CREATE TABLE others (id integer PRIMARY KEY);
CREATE TABLE links (id integer PRIMARY KEY, tags text[] NOT NULL REFERENCES things(tags), other_id integer NOT NULL REFERENCES others(id), UNIQUE (tags, other_id));`, "",
			"Warning: tables.public.things: foreign key links_tags_fkey gives no relationship \"others\": its column public.links.tags has Go type []string, which cannot key a Go map",
			"",
		},
		{
			"postgres one-to-one", "postgres", "pgx",
			`CREATE TABLE things (id integer PRIMARY KEY, tags text[] NOT NULL UNIQUE);
CREATE TABLE refs (id integer PRIMARY KEY, tags text[] UNIQUE REFERENCES things(tags));`, "",
			"", "Things *Thing",
		},
		{
			// refs is reached from heads through a keyable one-to-one, so its
			// edge into things is joined in heads' O2O chain as well.
			"postgres chained one-to-one", "postgres", "pgx",
			`CREATE TABLE things (id integer PRIMARY KEY, tags text[] NOT NULL UNIQUE);
CREATE TABLE refs (id integer PRIMARY KEY, tags text[] UNIQUE REFERENCES things(tags));
CREATE TABLE heads (id integer PRIMARY KEY, ref_id integer NOT NULL UNIQUE REFERENCES refs(id));`, "",
			"", "Things *Thing",
		},
		{
			"mysql set one-to-one", "mysql", "stdlib",
			`CREATE TABLE things (id int PRIMARY KEY, sk set('a', 'b') NOT NULL, UNIQUE KEY uq_sk (sk));
CREATE TABLE refs (id int PRIMARY KEY, sk set('a', 'b'), UNIQUE KEY uq_rsk (sk), CONSTRAINT fk_sk FOREIGN KEY (sk) REFERENCES things(sk));`, "",
			"", "Things *Thing",
		},
		{
			"declared one-to-one", "mysql", "stdlib",
			`CREATE TABLE things (id int PRIMARY KEY, name varchar(40) NOT NULL);
CREATE TABLE refs (id int PRIMARY KEY, sk set('a', 'b'));`,
			"tables:\n  refs:\n    relationships:\n      - name: Thing\n        type: o2o\n        table: things\n        fk: sk\n",
			"", "Thing *Thing",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := fmt.Sprintf(body, tt.dialect, tt.driver, tt.tables)
			for _, command := range []string{"validate", "generate"} {
				dir, stderr, err := runCommand(t, command, tt.schema, cfg)
				if err != nil {
					t.Fatalf("%s: %v\nstderr: %s", command, err, stderr)
				}
				switch {
				case tt.want != "" && strings.Count(stderr, tt.want) != 1:
					t.Errorf("%s stderr = %q, want %q once", command, stderr, tt.want)
				case tt.want == "" && strings.Contains(stderr, "cannot key a Go map"):
					t.Errorf("%s stderr = %q, want no unkeyable-key warning", command, stderr)
				}
				if command != "generate" {
					continue
				}
				if tt.kept != "" && !strings.Contains(generatedGo(t, dir)["models_gen.go"], tt.kept) {
					t.Errorf("models_gen.go has no %q, want the edge kept", tt.kept)
				}
				if !testing.Short() || os.Getenv("SQLGEN_BUILD_GENERATED") != "" {
					buildGenerated(t, root, dir)
				}
			}
		})
	}
}

// An enum array retyped to its own slice type on both sides of a detected
// foreign key is still an array: the edge is omitted with the warning, and
// the package builds.
func TestGenerate_UnkeyableForeignKeyRetypedToItsOwnSliceTypeGivesNoRelationship(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	const cfg = `version: 1
input:
  dialect: postgres
  paths: [schema.sql]
output:
  driver: pgx
  dir: out
  package: models
tables:
  things:
    type_map:
      kinds: KindSlice
  refs:
    type_map:
      kinds: KindSlice
`
	const schema = `CREATE TYPE kind AS ENUM ('a', 'b');
CREATE TABLE things (id integer PRIMARY KEY, kinds kind[] NOT NULL UNIQUE);
CREATE TABLE refs (id integer PRIMARY KEY, kinds kind[] REFERENCES things(kinds));`
	const want = `Warning: tables.public.things: foreign key refs_kinds_fkey gives no relationship "refs": its column public.refs.kinds is an array column; a SET, array or JSON column never keys a Go map, whatever Go type (here KindSlice)`
	for _, command := range []string{"validate", "generate"} {
		dir, stderr, err := runCommand(t, command, schema, cfg)
		if err != nil {
			t.Fatalf("%s: %v\nstderr: %s", command, err, stderr)
		}
		if strings.Count(stderr, want) != 1 {
			t.Errorf("%s stderr = %q, want %q once", command, stderr, want)
		}
		if command != "generate" {
			continue
		}
		if strings.Contains(generatedGo(t, dir)["models_gen.go"], "Refs []*Ref") {
			t.Errorf("models_gen.go has the Refs edge, want it omitted")
		}
		if !testing.Short() || os.Getenv("SQLGEN_BUILD_GENERATED") != "" {
			buildGenerated(t, root, dir)
		}
	}
}

// A declared to-many relationship whose link columns cannot key a Go map —
// whatever Go type column_map, type_map or overrides.types gives a SET or
// array column — is an error naming the relationship and the column, since the user asked for
// the edge by name (PRD §4.13). A declared one-to-one keeps its edge
// (TestGenerate_UnkeyableForeignKeyGivesNoRelationship/declared one-to-one).
func TestGenerate_UnkeyableDeclaredRelationshipIsAnError(t *testing.T) {
	const body = `version: 1
input:
  dialect: %s
  paths: [schema.sql]
output:
  driver: %s
  dir: out
  package: models
tables:
%s`
	tests := []struct {
		name, dialect, driver, schema, tables, want string
	}{
		{
			"postgres o2m", "postgres", "pgx",
			`CREATE TABLE things (id integer PRIMARY KEY, name text NOT NULL);
CREATE TABLE refs (id integer PRIMARY KEY, tags text[]);`,
			"  things:\n    relationships:\n      - name: Refs\n        type: o2m\n        table: refs\n        fk: tags\n",
			`tables.public.things.relationships: "Refs" links through public.refs.tags, and that column has Go type []string, which cannot key a Go map`,
		},
		{
			"postgres m2m", "postgres", "pgx",
			`CREATE TABLE things (id integer PRIMARY KEY, name text NOT NULL);
CREATE TABLE others (id integer PRIMARY KEY);
CREATE TABLE links (tags text[], other_id integer);`,
			"  things:\n    relationships:\n      - name: Others\n        type: m2m\n        table: others\n        junction: links\n        junction_local_fk: tags\n        junction_reference_fk: other_id\n",
			`tables.public.things.relationships: "Others" links through public.links.tags, and that column has Go type []string, which cannot key a Go map`,
		},
		{
			"postgres enum array retyped to its own slice type", "postgres", "pgx",
			`CREATE TYPE kind AS ENUM ('a', 'b');
CREATE TABLE things (id integer PRIMARY KEY, name text NOT NULL);
CREATE TABLE refs (id integer PRIMARY KEY, kinds kind[]);`,
			"  things:\n    relationships:\n      - name: Refs\n        type: o2m\n        table: refs\n        fk: kinds\n  refs:\n    type_map:\n      kinds: KindSlice\n",
			`tables.public.things.relationships: "Refs" links through public.refs.kinds, and that column is an array column; a SET, array or JSON column never keys a Go map, whatever Go type (here KindSlice)`,
		},
		{
			"mysql o2m", "mysql", "stdlib",
			`CREATE TABLE things (id int PRIMARY KEY, name varchar(40) NOT NULL);
CREATE TABLE refs (id int PRIMARY KEY, sk set('a', 'b'));`,
			"  things:\n    relationships:\n      - name: Refs\n        type: o2m\n        table: refs\n        fk: sk\n",
			`tables.things.relationships: "Refs" links through refs.sk, and that column is a MySQL SET column; a SET, array or JSON column never keys a Go map, whatever Go type (here RefsSkSet)`,
		},
		{
			"mysql set retyped to string", "mysql", "stdlib",
			`CREATE TABLE things (id int PRIMARY KEY, name varchar(40) NOT NULL);
CREATE TABLE refs (id int PRIMARY KEY, sk set('a', 'b'));`,
			"  things:\n    relationships:\n      - name: Refs\n        type: o2m\n        table: refs\n        fk: sk\n  refs:\n    type_map:\n      sk: string\n",
			`tables.things.relationships: "Refs" links through refs.sk, and that column is a MySQL SET column; a SET, array or JSON column never keys a Go map, whatever Go type (here string) column_map, type_map or overrides.types gives it; link the relationship through other columns or remove it (PRD §4.13, §13.4)`,
		},
		{
			"postgres text array retyped to string", "postgres", "pgx",
			`CREATE TABLE things (id integer PRIMARY KEY, name text NOT NULL);
CREATE TABLE refs (id integer PRIMARY KEY, tags text[]);`,
			"  things:\n    relationships:\n      - name: Refs\n        type: o2m\n        table: refs\n        fk: tags\n  refs:\n    type_map:\n      tags: string\n",
			`tables.public.things.relationships: "Refs" links through public.refs.tags, and that column is an array column; a SET, array or JSON column never keys a Go map, whatever Go type (here string)`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, command := range []string{"validate", "generate"} {
				_, stderr, err := runCommand(t, command, tt.schema, fmt.Sprintf(body, tt.dialect, tt.driver, tt.tables))
				if err == nil {
					t.Errorf("%s succeeded, want an error naming the link column", command)
					continue
				}
				if got := err.Error() + stderr; !strings.Contains(got, tt.want) {
					t.Errorf("%s error = %q, want it to contain %q", command, got, tt.want)
				}
			}
		})
	}
}
