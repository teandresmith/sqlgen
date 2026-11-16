package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A primary key whose Go type cannot key a Go map — a MySQL SET, a
// PostgreSQL array or a jsonb column — is refused by validate and generate,
// naming the column, rather than generating a package that does not compile
// (PRD §4.13). Keying the same table on another column generates a package
// that builds.
func TestGenerate_UnkeyablePrimaryKeyIsAnError(t *testing.T) {
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
	tests := []struct {
		name, dialect, driver, schema, tables, want string
	}{
		{
			"mysql set", "mysql", "stdlib",
			`CREATE TABLE things (sk set('a', 'b') PRIMARY KEY, name varchar(40) NOT NULL);`, "",
			`primary-key column "sk" of table things has Go type ThingsSkSet, which cannot key a Go map, so the generated package would not compile; key the table on other columns with tables.things.primary_key.columns, or drop it with exclude_tables`,
		},
		{
			"mysql set retyped to its own set type", "mysql", "stdlib",
			`CREATE TABLE things (sk set('a', 'b') PRIMARY KEY, name varchar(40) NOT NULL);`,
			"tables:\n  things:\n    type_map:\n      sk: ThingsSkSet\n",
			`primary-key column "sk" of table things has Go type ThingsSkSet, which cannot key a Go map`,
		},
		{
			"postgres enum array", "postgres", "pgx",
			`CREATE TYPE kind AS ENUM ('a', 'b');
CREATE TABLE things (kinds kind[] PRIMARY KEY, name text NOT NULL);`, "",
			`primary-key column "kinds" of table public.things has Go type KindSlice, which cannot key a Go map`,
		},
		{
			"postgres enum array retyped to its own slice type", "postgres", "pgx",
			`CREATE TYPE kind AS ENUM ('a', 'b');
CREATE TABLE things (kinds kind[] PRIMARY KEY, name text NOT NULL);`,
			"tables:\n  things:\n    type_map:\n      kinds: KindSlice\n",
			`primary-key column "kinds" of table public.things has Go type KindSlice, which cannot key a Go map`,
		},
		{
			"postgres text array", "postgres", "stdlib",
			`CREATE TABLE things (tags text[] PRIMARY KEY, name text NOT NULL);`, "",
			`primary-key column "tags" of table public.things has Go type []string, which cannot key a Go map, so the generated package would not compile; key the table on other columns with tables.public.things.primary_key.columns, or drop it with exclude_tables`,
		},
		{
			"postgres jsonb", "postgres", "pgx",
			`CREATE TABLE things (j jsonb PRIMARY KEY, name text NOT NULL);`, "",
			`primary-key column "j" of table public.things has Go type types.JSON, which cannot key a Go map`,
		},
		{
			"composite member", "postgres", "pgx",
			`CREATE TABLE things (a integer, tags text[], PRIMARY KEY (a, tags));`, "",
			`primary-key column "tags" of table public.things has Go type []string, which cannot key a Go map`,
		},
		{
			"declared key", "postgres", "pgx",
			`CREATE TABLE things (id integer PRIMARY KEY, tags text[] NOT NULL UNIQUE);`,
			"tables:\n  things:\n    primary_key:\n      columns: [tags]\n",
			`primary-key column "tags" of table public.things has Go type []string, which cannot key a Go map`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, command := range []string{"validate", "generate"} {
				_, stderr, err := runCommand(t, command, tt.schema, fmt.Sprintf(body, tt.dialect, tt.driver, tt.tables))
				if err == nil {
					t.Errorf("%s succeeded, want an error naming the key column", command)
					continue
				}
				if got := err.Error() + stderr; !strings.Contains(got, tt.want) {
					t.Errorf("%s error = %q, want it to contain %q", command, got, tt.want)
				}
			}
		})
	}

	// The way out the error names: the same columns, keyed on a comparable
	// one, generate a package that compiles. A bytea key is a slice too, but
	// comparator.Opaque keys it, so it is not refused. A SET retyped to
	// string by type_map is a string key, so it is not refused either.
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	accepted := []struct {
		name, dialect, driver, schema, tables string
	}{
		{
			"keyed elsewhere and bytea", "postgres", "pgx",
			`CREATE TABLE things (id integer PRIMARY KEY, tags text[] NOT NULL UNIQUE, name text NOT NULL);
CREATE TABLE blobs (b bytea PRIMARY KEY, name text NOT NULL);`, "",
		},
		{
			"postgres text array retyped to string", "postgres", "pgx",
			`CREATE TABLE things (tags text[] PRIMARY KEY, name text NOT NULL);`,
			"tables:\n  things:\n    type_map:\n      tags: string\n",
		},
		{
			"mysql set retyped to string", "mysql", "stdlib",
			`CREATE TABLE things (sk set('a', 'b') PRIMARY KEY, name varchar(40) NOT NULL);`,
			"tables:\n  things:\n    type_map:\n      sk: string\n",
		},
	}
	for _, tt := range accepted {
		t.Run(tt.name, func(t *testing.T) {
			dir, stderr, err := runCommand(t, "generate", tt.schema, fmt.Sprintf(body, tt.dialect, tt.driver, tt.tables))
			if err != nil {
				t.Fatalf("generate: %v\nstderr: %s", err, stderr)
			}
			if testing.Short() && os.Getenv("SQLGEN_BUILD_GENERATED") == "" {
				return
			}
			buildGenerated(t, root, dir)
		})
	}
}
