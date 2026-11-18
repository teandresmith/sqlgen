package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An enum in a primary key, a one-column FK and a junction's primary key
// filters through comparator.Enum, and the writers and loaders over it compile
// with the key's own type. MySQL declares an enum per column, so its FK and its
// parent key are different Go types.
func TestGenerate_EnumKeysCompile(t *testing.T) {
	const pgSchema = `CREATE TYPE kind AS ENUM ('a', 'b');
CREATE TABLE things (kind kind PRIMARY KEY, name text NOT NULL);
CREATE TABLE children (id integer PRIMARY KEY, name text NOT NULL, thing_kind kind REFERENCES things (kind));
CREATE TABLE owned (id integer PRIMARY KEY, thing_kind kind NOT NULL REFERENCES things (kind));
CREATE TABLE tags (id integer PRIMARY KEY, name text NOT NULL);
CREATE TABLE thing_tags (thing_kind kind NOT NULL REFERENCES things (kind), tag_id integer NOT NULL REFERENCES tags (id),
  PRIMARY KEY (thing_kind, tag_id));`
	const mysqlSchema = `CREATE TABLE things (kind enum('a', 'b') PRIMARY KEY, name varchar(40) NOT NULL);
CREATE TABLE children (id integer PRIMARY KEY, name varchar(40) NOT NULL, thing_kind enum('a', 'b'),
  FOREIGN KEY (thing_kind) REFERENCES things (kind));
CREATE TABLE owned (id integer PRIMARY KEY, thing_kind enum('a', 'b') NOT NULL,
  FOREIGN KEY (thing_kind) REFERENCES things (kind));
CREATE TABLE tags (id integer PRIMARY KEY, name varchar(40) NOT NULL);
CREATE TABLE thing_tags (thing_kind enum('a', 'b') NOT NULL, tag_id integer NOT NULL, PRIMARY KEY (thing_kind, tag_id),
  FOREIGN KEY (thing_kind) REFERENCES things (kind), FOREIGN KEY (tag_id) REFERENCES tags (id));`
	const body = `version: 1
input:
  dialect: %s
  paths: [schema.sql]
output:
  driver: %s
  dir: out
  package: models
  layout: %s
generation:
  nested_mutations:
    enabled: true
`
	tests := []struct {
		dialect, driver, layout, schema string
		want                            []string
	}{
		{
			"postgres", "pgx", "single_file", pgSchema,
			[]string{
				"Kind: &comparator.Enum[Kind]{Eq: new(id)}",
				"ThingKind: &comparator.NullableEnum[Kind]{Enum: comparator.Enum[Kind]{In: thingIDs}}",
				// The scope of an M2M `clear`, on the junction's enum member.
				"ThingKind: &comparator.Enum[Kind]{Eq: new(pid)}",
			},
		},
		{"postgres", "stdlib", "file_per_table", pgSchema, nil},
		{
			"mysql", "stdlib", "single_file", mysqlSchema,
			[]string{
				"thingIDs[idx] = ChildrenThingKindEnum(t.Kind)",
				"ThingKind: &comparator.NullableEnum[ChildrenThingKindEnum]{Enum: comparator.Enum[ChildrenThingKindEnum]{In: chunk}}",
				"ThingKind: &comparator.Enum[OwnedThingKindEnum]{In: chunk}",
			},
		},
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range tests {
		t.Run(tt.dialect+"/"+tt.driver, func(t *testing.T) {
			dir, stderr, err := runCommand(t, "generate", tt.schema, fmt.Sprintf(body, tt.dialect, tt.driver, tt.layout))
			if err != nil {
				t.Fatalf("generate: %v\nstderr: %s", err, stderr)
			}
			if len(tt.want) > 0 {
				src, err := os.ReadFile(filepath.Join(dir, "out", "models_gen.go")) //nolint:gosec // test code, path is under the test's temp dir
				if err != nil {
					t.Fatal(err)
				}
				for _, want := range tt.want {
					if !strings.Contains(string(src), want) {
						t.Errorf("generated code does not contain %q", want)
					}
				}
			}
			if testing.Short() && os.Getenv("SQLGEN_BUILD_GENERATED") == "" {
				return
			}
			buildGenerated(t, root, dir)
		})
	}
}
