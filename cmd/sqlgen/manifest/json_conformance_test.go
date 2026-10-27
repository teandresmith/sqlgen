package manifest_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/manifest"
	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/parser/mysql"
	"github.com/teandresmith/sqlgen/parser/postgres"
	"github.com/teandresmith/sqlgen/parser/sqlite"
)

// emittedManifestJSON builds the manifest for schema under cfg, writes it through
// EmitJSON (single layout) and returns the decoded on-disk JSON.
func emittedManifestJSON(t *testing.T, schema *parser.Schema, cfg *config.RootConfig) map[string]any {
	t.Helper()
	in := testBuildInput(t, schema, cfg)
	doc, err := manifest.Build(in)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	dir := t.TempDir()
	if err := manifest.EmitJSON(doc, &config.ManifestConfig{JSONLayout: config.JSONLayoutSingle}, dir); err != nil {
		t.Fatalf("EmitJSON: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "manifest", "manifest_gen.json")) //nolint:gosec // test reads the file it just emitted under t.TempDir
	if err != nil {
		t.Fatalf("reading emitted manifest: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decoding emitted manifest: %v", err)
	}
	return out
}

// emittedEntity returns the emitted entity whose name is name.
func emittedEntity(t *testing.T, doc map[string]any, name string) map[string]any {
	t.Helper()
	entities, _ := doc["entities"].([]any)
	for _, e := range entities {
		if m, ok := e.(map[string]any); ok && m["name"] == name {
			return m
		}
	}
	t.Fatalf("entity %q not in emitted entities %v", name, entities)
	return nil
}

// emittedColumn returns the emitted column of entity whose name is name.
func emittedColumn(t *testing.T, entity map[string]any, name string) map[string]any {
	t.Helper()
	columns, _ := entity["columns"].([]any)
	for _, c := range columns {
		if m, ok := c.(map[string]any); ok && m["name"] == name {
			return m
		}
	}
	t.Fatalf("column %q not in entity %v", name, entity["name"])
	return nil
}

// parseDDL parses ddl with dialect's file parser and runs relationship
// detection, the same two steps the generate pipeline runs on a files source.
func parseDDL(t *testing.T, dialect config.Dialect, ddl string) *parser.Schema {
	t.Helper()
	var p parser.Parser
	switch dialect {
	case config.DialectPostgres:
		p = postgres.New("public")
	case config.DialectMySQL:
		mp, err := mysql.New()
		if err != nil {
			t.Fatalf("mysql.New: %v", err)
		}
		p = mp
	case config.DialectSQLite:
		p = sqlite.New()
	}
	if err := p.Parse("schema.sql", []byte(ddl)); err != nil {
		t.Fatalf("parsing %s DDL: %v", dialect, err)
	}
	s := p.Schema()
	parser.DetectRelationships(s)
	return s
}

// dialectConfig is baseConfig switched to dialect, with that dialect's
// driver.
func dialectConfig(dialect config.Dialect) *config.RootConfig {
	cfg := baseConfig()
	cfg.Input.Dialect = dialect
	if dialect != config.DialectPostgres {
		cfg.Output.Driver = config.DriverStdlib
	}
	return cfg
}

// TestEmitJSON_SchemaURLNamesTheTaggedRelease pins PRD §30.4: the emitted `$schema`
// is the GitHub raw URL of the schema at the tagged release the generator
// was built from (`v<version>`), naming the schema/v1.json path.
func TestEmitJSON_SchemaURLNamesTheTaggedRelease(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    string
	}{
		{name: "release 0.42.0", version: "0.42.0", want: "https://raw.githubusercontent.com/teandresmith/sqlgen/v0.42.0/cmd/sqlgen/manifest/schema/v1.json"},
		{name: "release 1.3.7", version: "1.3.7", want: "https://raw.githubusercontent.com/teandresmith/sqlgen/v1.3.7/cmd/sqlgen/manifest/schema/v1.json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := &parser.Schema{Tables: []parser.Table{{Name: "users", Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}}}}}
			in := testBuildInput(t, schema, baseConfig())
			in.Version = tt.version
			doc, err := manifest.Build(in)
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			dir := t.TempDir()
			if err := manifest.EmitJSON(doc, &config.ManifestConfig{}, dir); err != nil {
				t.Fatalf("EmitJSON: %v", err)
			}
			data, err := os.ReadFile(filepath.Join(dir, "manifest", "manifest_gen.json")) //nolint:gosec // test reads the file it just emitted under t.TempDir
			if err != nil {
				t.Fatal(err)
			}
			var got struct {
				Schema string `json:"$schema"`
			}
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			if got.Schema != tt.want {
				t.Errorf("$schema for generator version %s = %q, want %q", tt.version, got.Schema, tt.want)
			}
		})
	}
}

// TestEmitJSON_ExtrasListConfigExtras pins PRD §30.4: `extras` lists the generated
// config `extras` types. Each config key is the generated Go struct name
// (PRD §4: "Key is Go struct name"), so each entry names that struct.
func TestEmitJSON_ExtrasListConfigExtras(t *testing.T) {
	cfg := baseConfig()
	cfg.Extras["Address"] = config.ExtraType{Fields: map[string]config.ExtraTypeField{"Street": {Type: "string"}}}
	cfg.Extras["GeoPoint"] = config.ExtraType{Fields: map[string]config.ExtraTypeField{"Lat": {Type: "float64"}}}
	schema := &parser.Schema{Tables: []parser.Table{{Name: "users", Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}}}}}

	doc := emittedManifestJSON(t, schema, cfg)
	extras, _ := doc["extras"].([]any)
	var got []map[string]any
	for _, x := range extras {
		if m, ok := x.(map[string]any); ok && m["kind"] == "extra" {
			got = append(got, m)
		}
	}
	want := []map[string]any{
		{"kind": "extra", "name": "Address", "go_type": "Address", "source": "config", "json_only": true},
		{"kind": "extra", "name": "GeoPoint", "go_type": "GeoPoint", "source": "config", "json_only": true},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("emitted extras of kind extra mismatch (-want +got):\n%s", diff)
	}
}

// TestEmitJSON_CommentAlwaysWritten pins PRD §30.4.2 / §30.7: `comment` is always
// written, `""` when there is none, on the entity and on every column, while
// a declared comment is written as its text.
func TestEmitJSON_CommentAlwaysWritten(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{Name: "users", Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}, {Name: "email", Type: "text"}}},
			{Name: "posts", Comment: "Blog posts", Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}, {Name: "title", Type: "text", Comment: "Headline"}}},
		},
		Views: []parser.View{{
			Name:    "active_users",
			SQL:     "SELECT id, email FROM users",
			Columns: []parser.Column{{Name: "id", Type: "uuid"}, {Name: "email", Type: "text"}},
		}},
	}
	doc := emittedManifestJSON(t, schema, baseConfig())
	tests := []struct {
		name         string
		entity       string
		wantEntity   any
		wantByColumn map[string]any
	}{
		{name: "table without comments", entity: "User", wantEntity: "", wantByColumn: map[string]any{"id": "", "email": ""}},
		{name: "view without comments", entity: "ActiveUser", wantEntity: "", wantByColumn: map[string]any{"id": "", "email": ""}},
		{name: "table with comments", entity: "Post", wantEntity: "Blog posts", wantByColumn: map[string]any{"id": "", "title": "Headline"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := emittedEntity(t, doc, tt.entity)
			got, ok := e["comment"]
			if !ok {
				t.Errorf("%s: emitted entity has no comment key, want %q", tt.entity, tt.wantEntity)
			} else if got != tt.wantEntity {
				t.Errorf("%s: comment = %#v, want %#v", tt.entity, got, tt.wantEntity)
			}
			for col, want := range tt.wantByColumn {
				c := emittedColumn(t, e, col)
				got, ok := c["comment"]
				if !ok {
					t.Errorf("%s.%s: emitted column has no comment key, want %q", tt.entity, col, want)
					continue
				}
				if got != want {
					t.Errorf("%s.%s: comment = %#v, want %#v", tt.entity, col, got, want)
				}
			}
		})
	}
}

// TestEmitJSON_ManyToOneEdgeCarriesFK pins PRD §30.4.2: "An `o2o` / `o2m` / `m2o`
// edge carries `fk`; an `m2m` edge carries `junction` and no `fk`" — here the
// m2o arm, read from the emitted JSON. An m2o edge is the `side: child` view
// of a one-to-one from the referenced table (PRD §4.8), so its key columns
// sit on the target and fk.table names the target's table.
func TestEmitJSON_ManyToOneEdgeCarriesFK(t *testing.T) {
	schema := parseDDL(t, config.DialectPostgres, `
CREATE TABLE profiles (id uuid PRIMARY KEY);
CREATE TABLE users (id uuid PRIMARY KEY, profile_id uuid UNIQUE REFERENCES profiles(id));
`)
	cfg := baseConfig()
	cfg.Tables["profiles"] = config.TableConfig{Relationships: []config.TableRelationship{
		{Name: "User", Type: "o2o", Table: "users", FK: "profile_id", Side: "child"},
	}}
	profile := emittedEntity(t, emittedManifestJSON(t, schema, cfg), "Profile")
	rels, _ := profile["relationships"].([]any)
	var m2o []map[string]any
	for _, r := range rels {
		if m, ok := r.(map[string]any); ok && m["kind"] == "m2o" {
			m2o = append(m2o, m)
		}
	}
	if len(m2o) != 1 {
		t.Fatalf("Profile m2o edges = %v (all edges %v), want exactly the one edge to User", m2o, rels)
	}
	edge := m2o[0]
	if edge["target_entity"] != "User" {
		t.Errorf("m2o target_entity = %v, want User", edge["target_entity"])
	}
	fk, ok := edge["fk"].(map[string]any)
	if !ok {
		t.Fatalf("m2o edge %v carries no fk object", edge)
	}
	if fk["table"] != "users" {
		t.Errorf("m2o fk.table = %v, want users (the table holding the key columns)", fk["table"])
	}
	if _, ok := edge["junction"]; ok {
		t.Errorf("m2o edge carries junction %v, want none", edge["junction"])
	}
}

// TestEmitJSON_EventsNullForOptedOutTable pins PRD §30.7: features.events is
// populated only when events are enabled for the entity, so a table that opts
// out under an enabled global block emits `"events": null` while its sibling
// carries the block.
func TestEmitJSON_EventsNullForOptedOutTable(t *testing.T) {
	cfg := baseConfig()
	cfg.Events = &config.EventConfig{Enabled: true}
	cfg.Tables["users"] = config.TableConfig{Events: &config.TableEventConfig{Enabled: new(false)}}
	schema := &parser.Schema{Tables: []parser.Table{
		{Name: "users", Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}, {Name: "email", Type: "text"}}},
		{Name: "posts", Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}, {Name: "title", Type: "text"}}},
	}}
	doc := emittedManifestJSON(t, schema, cfg)

	userFeatures, _ := emittedEntity(t, doc, "User")["features"].(map[string]any)
	events, ok := userFeatures["events"]
	if !ok {
		t.Errorf("User features has no events key, want \"events\": null")
	} else if events != nil {
		t.Errorf("User (opted out) features.events = %v, want null", events)
	}
	postFeatures, _ := emittedEntity(t, doc, "Post")["features"].(map[string]any)
	if pe, _ := postFeatures["events"].(map[string]any); pe == nil || pe["enabled"] != true {
		t.Errorf("Post (not opted out) features.events = %v, want a populated, enabled block", postFeatures["events"])
	}
}

// TestEmitJSON_ComparatorOmittedForNonFilterableColumn pins PRD §30.7: `comparator`
// is omitted for a non-filterable column — a `json[]` column, and a
// `types.JSON` column on SQLite — while a filterable sibling keeps its
// comparator.
func TestEmitJSON_ComparatorOmittedForNonFilterableColumn(t *testing.T) {
	tests := []struct {
		name       string
		dialect    config.Dialect
		overrides  map[string]config.TypeOverride
		column     parser.Column
		wantGoType string
	}{
		{
			name:       "postgres json array",
			dialect:    config.DialectPostgres,
			column:     parser.Column{Name: "payloads", Type: "json[]"},
			wantGoType: "[]types.JSON",
		},
		{
			name:       "sqlite types.JSON column",
			dialect:    config.DialectSQLite,
			overrides:  map[string]config.TypeOverride{"json": {Type: "types.JSON", Import: "github.com/teandresmith/sqlgen/types"}},
			column:     parser.Column{Name: "payload", Type: "json"},
			wantGoType: "types.JSON",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := dialectConfig(tt.dialect)
			maps := cfg.Overrides.Types
			for k, v := range tt.overrides {
				maps[k] = v
			}
			idType := "uuid"
			if tt.dialect == config.DialectSQLite {
				idType = "integer"
			}
			schema := &parser.Schema{Tables: []parser.Table{{Name: "probes", Columns: []parser.Column{
				{Name: "id", Type: idType, PrimaryKey: true},
				{Name: "label", Type: "text"},
				tt.column,
			}}}}
			e := emittedEntity(t, emittedManifestJSON(t, schema, cfg), "Probe")
			col := emittedColumn(t, e, tt.column.Name)
			if col["go_type"] != tt.wantGoType {
				t.Fatalf("fixture: %s go_type = %v, want %s", tt.column.Name, col["go_type"], tt.wantGoType)
			}
			if c, ok := col["comparator"]; ok {
				t.Errorf("%s emitted comparator %v, want the key omitted (non-filterable)", tt.column.Name, c)
			}
			if c := emittedColumn(t, e, "label")["comparator"]; c != "comparator.String" {
				t.Errorf("filterable label comparator = %v, want comparator.String", c)
			}
		})
	}
}

// emittedIndex is the emitted shape of one indexes[] entry.
type emittedIndex struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
	Unique  bool     `json:"unique"`
	Method  string   `json:"method"`
	Where   string   `json:"where,omitempty"`
}

// emittedIndexes decodes the emitted indexes[] of entity, sorted by name.
func emittedIndexes(t *testing.T, entity map[string]any) []emittedIndex {
	t.Helper()
	raw, err := json.Marshal(entity["indexes"])
	if err != nil {
		t.Fatal(err)
	}
	var out []emittedIndex
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decoding indexes %s: %v", raw, err)
	}
	slices.SortFunc(out, func(a, b emittedIndex) int {
		switch {
		case a.Name < b.Name:
			return -1
		case a.Name > b.Name:
			return 1
		}
		return 0
	})
	return out
}

// TestEmitJSON_GistIndexMethod pins PRD §30.7: indexes[] lists a GiST index with
// `method: "gist"`, read from parsed DDL.
func TestEmitJSON_GistIndexMethod(t *testing.T) {
	schema := parseDDL(t, config.DialectPostgres, `
CREATE TABLE hosts (id uuid PRIMARY KEY, addr inet NOT NULL);
CREATE INDEX hosts_addr_gist ON hosts USING gist (addr inet_ops);
`)
	got := emittedIndexes(t, emittedEntity(t, emittedManifestJSON(t, schema, baseConfig()), "Host"))
	want := []emittedIndex{
		{Name: "hosts_addr_gist", Columns: []string{"addr"}, Method: "gist"},
		{Name: "hosts_pkey", Columns: []string{"id"}, Unique: true, Method: "btree"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("indexes mismatch (-want +got):\n%s", diff)
	}
}

// TestEmitJSON_ExpressionIndexNotListed pins PRD §30.7: "An index with an expression
// part (`lower(email)`, `a + 1`) is not listed", on every dialect, while a
// plain index declared beside it is.
func TestEmitJSON_ExpressionIndexNotListed(t *testing.T) {
	tests := []struct {
		name    string
		dialect config.Dialect
		ddl     string
		want    []emittedIndex
	}{
		{
			name:    "postgres",
			dialect: config.DialectPostgres,
			ddl: `CREATE TABLE users (id uuid PRIMARY KEY, email text NOT NULL, a integer NOT NULL, placed_at timestamptz NOT NULL);
CREATE INDEX users_lower_email_idx ON users (lower(email));
CREATE INDEX users_a_plus_idx ON users ((a + 1));
CREATE INDEX users_mixed_idx ON users (placed_at, lower(email));
CREATE INDEX users_placed_at_idx ON users (placed_at);`,
			want: []emittedIndex{
				{Name: "users_pkey", Columns: []string{"id"}, Unique: true, Method: "btree"},
				{Name: "users_placed_at_idx", Columns: []string{"placed_at"}, Method: "btree"},
			},
		},
		{
			name:    "mysql",
			dialect: config.DialectMySQL,
			ddl: "CREATE TABLE users (id bigint NOT NULL PRIMARY KEY, email varchar(255) NOT NULL, a int NOT NULL, placed_at datetime NOT NULL);\n" +
				"CREATE INDEX users_lower_email_idx ON users ((lower(email)));\n" +
				"CREATE INDEX users_a_plus_idx ON users ((a + 1));\n" +
				"CREATE INDEX users_placed_at_idx ON users (placed_at);",
			want: []emittedIndex{
				{Name: "PRIMARY", Columns: []string{"id"}, Unique: true, Method: "btree"},
				{Name: "users_placed_at_idx", Columns: []string{"placed_at"}, Method: "btree"},
			},
		},
		{
			name:    "sqlite",
			dialect: config.DialectSQLite,
			ddl: `CREATE TABLE users (id integer PRIMARY KEY, email text NOT NULL, a integer NOT NULL, placed_at integer NOT NULL);
CREATE INDEX users_lower_email_idx ON users (lower(email));
CREATE INDEX users_a_plus_idx ON users (a + 1);
CREATE INDEX users_placed_at_idx ON users (placed_at);`,
			want: []emittedIndex{
				{Name: "users_pkey", Columns: []string{"id"}, Unique: true, Method: "btree"},
				{Name: "users_placed_at_idx", Columns: []string{"placed_at"}, Method: "btree"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := parseDDL(t, tt.dialect, tt.ddl)
			got := emittedIndexes(t, emittedEntity(t, emittedManifestJSON(t, schema, dialectConfig(tt.dialect)), "User"))
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("indexes mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestEmitJSON_DeclaredIndexNameInlineOrTableLevel pins PRD §30.7: "Each index is
// listed under the name the database assigns, inline or table-level alike":
// a declared constraint name is listed as declared whether the constraint is
// written on the column or at table level.
func TestEmitJSON_DeclaredIndexNameInlineOrTableLevel(t *testing.T) {
	tests := []struct {
		name    string
		dialect config.Dialect
		ddl     string
		want    []emittedIndex
	}{
		{
			name:    "postgres",
			dialect: config.DialectPostgres,
			ddl: `CREATE TABLE users (
	id uuid CONSTRAINT users_id_pk PRIMARY KEY,
	email text NOT NULL CONSTRAINT users_email_uq UNIQUE,
	handle text NOT NULL,
	CONSTRAINT users_handle_uq UNIQUE (handle)
);`,
			want: []emittedIndex{
				{Name: "users_email_uq", Columns: []string{"email"}, Unique: true, Method: "btree"},
				{Name: "users_handle_uq", Columns: []string{"handle"}, Unique: true, Method: "btree"},
				{Name: "users_id_pk", Columns: []string{"id"}, Unique: true, Method: "btree"},
			},
		},
		{
			name:    "sqlite",
			dialect: config.DialectSQLite,
			ddl: `CREATE TABLE users (
	id integer CONSTRAINT users_id_pk PRIMARY KEY,
	email text NOT NULL CONSTRAINT users_email_uq UNIQUE,
	handle text NOT NULL,
	CONSTRAINT users_handle_uq UNIQUE (handle)
);`,
			want: []emittedIndex{
				{Name: "users_email_uq", Columns: []string{"email"}, Unique: true, Method: "btree"},
				{Name: "users_handle_uq", Columns: []string{"handle"}, Unique: true, Method: "btree"},
				{Name: "users_id_pk", Columns: []string{"id"}, Unique: true, Method: "btree"},
			},
		},
		{
			// MySQL has no named column-level constraint; its two
			// table-level spellings are the CONSTRAINT clause and the
			// UNIQUE KEY index name.
			name:    "mysql",
			dialect: config.DialectMySQL,
			ddl: "CREATE TABLE users (\n" +
				"\tid bigint NOT NULL,\n" +
				"\temail varchar(255) NOT NULL,\n" +
				"\thandle varchar(255) NOT NULL,\n" +
				"\tCONSTRAINT users_id_pk PRIMARY KEY (id),\n" +
				"\tCONSTRAINT users_email_uq UNIQUE (email),\n" +
				"\tUNIQUE KEY users_handle_uq (handle)\n" +
				");",
			want: []emittedIndex{
				{Name: "PRIMARY", Columns: []string{"id"}, Unique: true, Method: "btree"},
				{Name: "users_email_uq", Columns: []string{"email"}, Unique: true, Method: "btree"},
				{Name: "users_handle_uq", Columns: []string{"handle"}, Unique: true, Method: "btree"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := parseDDL(t, tt.dialect, tt.ddl)
			got := emittedIndexes(t, emittedEntity(t, emittedManifestJSON(t, schema, dialectConfig(tt.dialect)), "User"))
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("indexes mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
