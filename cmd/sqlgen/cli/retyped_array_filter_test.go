package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// A PostgreSQL array column keeps comparator.Slice of its element whatever Go
// type column_map, type_map or overrides.types gives it, since its filter
// compares what the database holds (PRD §11.2); an array of JSON stays
// unfilterable, as it is unretyped.
func TestGenerate_RetypedArrayColumnKeepsSliceFilter(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	const enums = `CREATE TYPE kind AS ENUM ('a', 'b');
CREATE TABLE refs (id integer PRIMARY KEY, kinds kind[], nn kind[] NOT NULL);`
	const texts = `CREATE TABLE refs (id integer PRIMARY KEY, kinds text[], nn text[] NOT NULL);`
	const uuids = `CREATE TABLE refs (id integer PRIMARY KEY, kinds uuid[], nn uuid[] NOT NULL);`
	const jsons = `CREATE TABLE refs (id integer PRIMARY KEY, kinds jsonb[], nn jsonb[] NOT NULL);`
	const localSlice = "package models\n\ntype Local []string\n"
	tests := []struct {
		name, schema, config string
		// view, when set, is a view file selecting the table's columns.
		view string
		// local is a hand-written file the retype names a type from.
		local string
		// file is the generated file holding the filter; elem its element,
		// "" when the columns take no filter field.
		file, elem string
	}{
		{
			name:   "type_map to the enum's own slice type",
			schema: enums,
			config: "tables:\n  refs:\n    type_map:\n      kinds: KindSlice\n      nn: KindSlice\n",
			file:   "models_gen.go", elem: "Kind",
		},
		{
			name:   "column_map type to the enum's own slice type",
			schema: enums,
			config: "tables:\n  refs:\n    column_map:\n      kinds:\n        type: KindSlice\n      nn:\n        type: KindSlice\n",
			file:   "models_gen.go", elem: "Kind",
		},
		{
			name:   "table overrides.types to the enum's own slice type",
			schema: enums,
			config: "tables:\n  refs:\n    overrides:\n      types:\n        kind[]:\n          type: KindSlice\n",
			file:   "models_gen.go", elem: "Kind",
		},
		{
			name:   "global overrides.types on a view column",
			schema: enums,
			config: "overrides:\n  types:\n    kind[]:\n      type: KindSlice\n",
			view:   "-- @pk: id\nCREATE VIEW vrefs AS SELECT id, kinds, nn FROM refs;",
			file:   "views_gen.go", elem: "Kind",
		},
		{
			name:   "view @type to the enum's own slice type",
			schema: enums,
			view:   "-- @pk: id\n-- @type kinds: KindSlice\n-- @type nn: KindSlice\n-- @nullable: kinds\nCREATE VIEW vrefs AS SELECT id, kinds, nn FROM refs;",
			file:   "views_gen.go", elem: "Kind",
		},
		{
			name:   "type_map over an enum element that overrides.types also maps",
			schema: enums,
			config: "overrides:\n  types:\n    kind:\n      type: string\ntables:\n  refs:\n    type_map:\n      kinds: KindSlice\n      nn: KindSlice\n",
			file:   "models_gen.go", elem: "Kind",
		},
		{
			name:   "type_map to a slice literal",
			schema: texts,
			config: "tables:\n  refs:\n    type_map:\n      kinds: '[]string'\n      nn: '[]string'\n",
			file:   "models_gen.go", elem: "string",
		},
		{
			name:   "column_map type to an enum slice literal",
			schema: enums,
			config: "tables:\n  refs:\n    column_map:\n      kinds:\n        type: '[]Kind'\n      nn:\n        type: '[]Kind'\n",
			file:   "models_gen.go", elem: "Kind",
		},
		{
			name:   "type_map to a local named slice",
			schema: texts,
			config: "tables:\n  refs:\n    type_map:\n      kinds: Local\n      nn: Local\n",
			local:  localSlice,
			file:   "models_gen.go", elem: "string",
		},
		{
			name:   "type_map over an element that imports its type",
			schema: uuids,
			config: "overrides:\n  types:\n    uuid:\n      type: uuid.UUID\n      import: uuid\ntables:\n  refs:\n    type_map:\n      kinds: Local\n      nn: Local\n",
			local:  localSlice,
			file:   "models_gen.go", elem: "uuid.UUID",
		},
		{
			name:   "type_map over a JSON element",
			schema: jsons,
			config: "tables:\n  refs:\n    type_map:\n      kinds: Local\n      nn: Local\n",
			local:  localSlice,
			file:   "models_gen.go",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			views := ""
			if tt.view != "" {
				writeFile(t, filepath.Join(dir, "views"), "vrefs.sql", tt.view)
				views = "  views: [views]\n"
			}
			writeFile(t, dir, "schema.sql", tt.schema)
			cfg := writeFile(t, dir, "sqlgen.yml", "version: 1\ninput:\n  dialect: postgres\n  paths: [schema.sql]\n"+views+
				"output:\n  driver: pgx\n  dir: out\n  package: models\n"+tt.config)
			t.Chdir(dir)
			if _, stderr, err := executeCommand("generate", "--config", cfg); err != nil {
				t.Fatalf("generate: %v\nstderr: %s", err, stderr)
			}
			src := generatedGo(t, dir)[tt.file]
			if tt.elem == "" {
				if m := regexp.MustCompile(`(Kinds|Nn)\s+\*comparator\.\S+`).FindString(src); m != "" {
					t.Errorf("%s has filter field %q, want none", tt.file, m)
				}
			}
			elem := regexp.QuoteMeta(tt.elem)
			for _, want := range []string{
				`Kinds\s+\*comparator\.NullableSlice\[` + elem + `\]`,
				`Nn\s+\*comparator\.Slice\[` + elem + `\]`,
			} {
				if tt.elem != "" && !regexp.MustCompile(want).MatchString(src) {
					t.Errorf("%s has no filter field matching %s", tt.file, want)
				}
			}
			if tt.local != "" {
				writeFile(t, filepath.Join(dir, "out"), "local.go", tt.local)
			}
			if !testing.Short() || os.Getenv("SQLGEN_BUILD_GENERATED") != "" {
				buildGenerated(t, root, dir)
			}
		})
	}
}
