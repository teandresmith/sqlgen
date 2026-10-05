package gen_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// The `discriminator:` read path (PRD §13.4.1). The structured form compiles to
// the same predicate the equivalent `filter:` produced, but the three read
// paths do not share a parameter channel, so it lands two ways: bound on the
// O2M / M2M loader and the relationship-filter subquery, interpolated in the
// O2O JOIN ON clause, whose builder contract documents it as opaque SQL
// carrying no placeholders (`sql/builder.go:80-87`).

// discriminatorSchema mirrors the shape PRD §13.7.1 describes: one parent with
// several edges into one polymorphic child table, distinguished by a value in
// a discriminator column. `assets` is the same fixture the examples migrate.
func discriminatorSchema() *parser.Schema {
	return &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "assets",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "name", Type: "text"},
				},
			},
			{
				Name: "documents",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "entity_id", Type: "uuid"},
					{Name: "entity_type", Type: "text"},
					{Name: "name", Type: "text"},
				},
			},
		},
	}
}

// discriminatorTables builds the `assets` context with one declared edge of the
// given relationship type, carrying either a filter or a discriminator.
func discriminatorTables(t *testing.T, dialect config.Dialect, rel config.TableRelationship) gen.TableContext {
	t.Helper()

	in := testInput(discriminatorSchema())
	in.Config.Input.Dialect = dialect
	if dialect != config.DialectPostgres {
		in.Config.Output.Driver = "stdlib"
	}
	parser.DetectRelationships(in.Schema)
	in.Config.Tables["assets"] = config.TableConfig{
		Relationships: []config.TableRelationship{rel},
	}

	tables, err := gen.BuildTableContextsFromSchema(in.Schema, in.Config)
	if err != nil {
		t.Fatalf("BuildTableContextsFromSchema(%s) = %v, want nil", dialect, err)
	}
	for _, tc := range tables {
		if tc.TableName == "assets" {
			return tc
		}
	}
	t.Fatalf("no assets table context for dialect %s", dialect)
	return gen.TableContext{}
}

// filterRel and discriminatorRel are the two spellings of one edge. Keeping
// them beside each other is the point of every equivalence test below.
func filterRel(name, relType string) config.TableRelationship {
	return config.TableRelationship{
		Name: name, Type: relType, Table: "documents", FK: "entity_id",
		Filter: "entity_type = 'asset.primary'",
	}
}

func discRel(name, relType string) config.TableRelationship {
	return config.TableRelationship{
		Name: name, Type: relType, Table: "documents", FK: "entity_id",
		Discriminator: &config.RelationshipDiscriminator{
			Column: "entity_type",
			Value:  "asset.primary",
		},
	}
}

// TestDiscriminator_O2OJoinByteIdenticalToFilter is the surviving half of PRD
// §13.4.1's byte-identity promise, and the one that needs a test per dialect.
//
// The O2O predicate is qualified by the dialect's own SQL parser, and the three
// disagree about how a rewritten identifier is spelled: pg_query and vitess
// emit `alias.column`, rqlite/sql emits `"alias"."column"`. A hand-assembled
// predicate matched two dialects and silently changed the third, which is
// exactly what this pins — the discriminator is routed through the same
// qualifier as the filter it replaces, so equivalence holds by construction
// rather than by matching each dialect's output by hand.
func TestDiscriminator_O2OJoinByteIdenticalToFilter(t *testing.T) {
	for _, dialect := range []config.Dialect{config.DialectPostgres, config.DialectMySQL, config.DialectSQLite} {
		t.Run(string(dialect), func(t *testing.T) {
			withFilter := discriminatorTables(t, dialect, filterRel("PrimaryDocument", "one_to_one"))
			withDisc := discriminatorTables(t, dialect, discRel("PrimaryDocument", "one_to_one"))

			want := o2oJoinFilter(t, withFilter, "PrimaryDocument")
			got := o2oJoinFilter(t, withDisc, "PrimaryDocument")

			if want == "" {
				t.Fatal("the filter form produced an empty JOIN predicate; the comparison below would be vacuous")
			}
			if got != want {
				t.Errorf("O2O JOIN predicate on %s:\n  discriminator = %q\n  filter        = %q\nwant byte-identical (PRD §13.4.1)", dialect, got, want)
			}
		})
	}
}

// o2oJoinFilter returns the qualified sub-categorization predicate on the named
// O2O join, failing the test when the edge is missing.
func o2oJoinFilter(t *testing.T, tc gen.TableContext, field string) string {
	t.Helper()
	for _, d := range tc.O2OJoinDetails {
		if d.FieldName == field {
			return d.Filter
		}
	}
	t.Fatalf("table %s has no O2O join for %q", tc.TableName, field)
	return ""
}

// TestDiscriminator_LoaderBindsTheValue pins the O2M / M2M loader half: the
// structured form carries a column and a value rather than opaque SQL, so the
// value binds as a parameter and the column is quoted through the dialect. The
// `filter:` form cannot do either — it is raw SQL under PRD §4.8's contract.
func TestDiscriminator_LoaderBindsTheValue(t *testing.T) {
	tc := discriminatorTables(t, config.DialectPostgres, discRel("Attachments", "one_to_many"))
	got := executeGetTemplate(t, tc)

	want := `conditions: []sql.Condition{sql.Raw(c.dialect.QuoteIdentifier("entity_type")+" = $", "asset.primary")}`
	if !strings.Contains(got, want) {
		t.Errorf("O2M loader does not bind the discriminator.\nwant substring: %s\ngot:\n%s", want, got)
	}
	if strings.Contains(got, `sql.Raw("entity_type = 'asset.primary'")`) {
		t.Error("O2M loader still interpolates the discriminator value; it must bind (PRD §13.4.1)")
	}
}

// TestDiscriminator_M2MLoaderBindsTheValue covers the second, duplicated
// emission site in get.go.tmpl. The O2M and M2M loader branches are separate
// template blocks that happen to agree today, and no fixture declares an M2M
// `discriminator:` edge — `LinkedAttachments` deliberately stays on `filter:`
// as the uninvertible case — so without this the M2M branch ships untested and
// a change to one block could silently diverge from the other.
func TestDiscriminator_M2MLoaderBindsTheValue(t *testing.T) {
	rel := discRel("LinkedDocs", "many_to_many")
	rel.FK = ""
	rel.Junction = "asset_document_links"
	rel.JunctionLocalFK = "asset_id"
	rel.JunctionReferenceFK = "document_id"

	tc := discriminatorTables(t, config.DialectPostgres, rel)
	got := executeGetTemplate(t, tc)

	want := `conditions: []sql.Condition{sql.Raw(c.dialect.QuoteIdentifier("entity_type")+" = $", "asset.primary")}`
	if !strings.Contains(got, want) {
		t.Errorf("M2M loader does not bind the discriminator.\nwant substring: %s\ngot:\n%s", want, got)
	}
}

// TestDiscriminator_RelationshipFilterBindsTheValue pins the third read path.
// Only O2M and M2M edges reach it — buildRelationshipFilters draws from those
// two lists — so every discriminator here binds, with no O2O exception.
func TestDiscriminator_RelationshipFilterBindsTheValue(t *testing.T) {
	tc := discriminatorTables(t, config.DialectPostgres, discRel("Attachments", "one_to_many"))
	got := executeFilterTemplate(t, tc)

	for _, want := range []string{
		`subSQL += " AND (" + tgt + "." + dialect.QuoteIdentifier("entity_type") + " = $)"`,
		`subArgs = append(subArgs, "asset.primary")`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("relationship-filter subquery missing %s\ngot:\n%s", want, got)
		}
	}
}

// TestDiscriminator_RelationshipFilterAliasIsRuntimeToken guards the failure
// mode already fixed on the `filter:` form: on SQLite the qualifier emits
// `"sqlgenrel"."col"`, the splice matched only the unquoted token, and the
// alias placeholder survived into the emitted SQL as a table name the subquery
// never defines.
//
// The discriminator path never had it — it builds the predicate from the
// runtime `tgt` variable rather than substituting a token — and this pins that
// on the dialect where the token form broke.
// TestRelationshipStaticFilter_NoAliasTokenSurvives is the `filter:` half.
func TestDiscriminator_RelationshipFilterAliasIsRuntimeToken(t *testing.T) {
	tc := discriminatorTables(t, config.DialectSQLite, discRel("Attachments", "one_to_many"))
	got := executeFilterTemplate(t, tc)

	if strings.Contains(got, gen.RelationshipFilterAliasToken) {
		t.Errorf("emitted subquery carries the codegen alias token %q, which names no relation at runtime:\n%s",
			gen.RelationshipFilterAliasToken, got)
	}
}

// TestDiscriminatorLiterals pins both renderings of a scalar value. GoLiteral
// feeds the two bound paths; SQLLiteral feeds the O2O clause that has no
// parameter channel, so its string form doubles embedded quotes — the one
// escape all three dialects share.
func TestDiscriminatorLiterals(t *testing.T) {
	tests := []struct {
		name    string
		value   any
		wantGo  string
		wantSQL string
	}{
		{"string", "asset.primary", `"asset.primary"`, `'asset.primary'`},
		{"string with an apostrophe", "o'brien", `"o'brien"`, `'o''brien'`},
		{"string with a double quote", `say "hi"`, `"say \"hi\""`, `'say "hi"'`},
		// Rendered here for completeness; config.validateDiscriminatorValue
		// refuses a backslash on the O2O edges that reach SQLLiteral, because
		// MySQL reads it as an escape and the other two do not.
		{"string with a backslash", `a\b`, `"a\\b"`, `'a\b'`},
		{"empty string", "", `""`, `''`},
		{"int", 3, `3`, `3`},
		{"bool true", true, `true`, `TRUE`},
		{"bool false", false, `false`, `FALSE`},
		{"float", 3.5, `3.5`, `3.5`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := gen.DiscriminatorContextFor(&config.RelationshipDiscriminator{Column: "entity_type", Value: tt.value})
			if d.GoLiteral != tt.wantGo {
				t.Errorf("GoLiteral(%#v) = %s, want %s", tt.value, d.GoLiteral, tt.wantGo)
			}
			if d.SQLLiteral != tt.wantSQL {
				t.Errorf("SQLLiteral(%#v) = %s, want %s", tt.value, d.SQLLiteral, tt.wantSQL)
			}
			if want := "entity_type = " + tt.wantSQL; d.EquivalentFilter() != want {
				t.Errorf("EquivalentFilter() = %q, want %q", d.EquivalentFilter(), want)
			}
		})
	}
}

// TestDiscriminator_SubCategorizedEdgesStayDistinct is the end-to-end shape
// §13.7.1 exists for: three edges into one child table on one FK, separated
// only by their discriminator value, each producing its own field and its own
// predicate.
func TestDiscriminator_SubCategorizedEdgesStayDistinct(t *testing.T) {
	in := testInput(discriminatorSchema())
	parser.DetectRelationships(in.Schema)

	rels := make([]config.TableRelationship, 0, 3)
	for _, v := range []string{"asset.primary", "asset.attachment", "asset.invoice"} {
		name := "Edge" + strings.ToUpper(v[len(v)-1:])
		rels = append(rels, config.TableRelationship{
			Name: name, Type: "one_to_many", Table: "documents", FK: "entity_id",
			Discriminator: &config.RelationshipDiscriminator{Column: "entity_type", Value: v},
		})
	}
	in.Config.Tables["assets"] = config.TableConfig{Relationships: rels}

	tables, err := gen.BuildTableContextsFromSchema(in.Schema, in.Config)
	if err != nil {
		t.Fatalf("BuildTableContextsFromSchema = %v, want nil", err)
	}

	var assets gen.TableContext
	for _, tc := range tables {
		if tc.TableName == "assets" {
			assets = tc
		}
	}

	seen := make(map[string]string)
	for _, rel := range assets.O2MRelationships {
		if rel.Discriminator == nil {
			continue
		}
		seen[rel.FieldName] = rel.Discriminator.GoLiteral
	}
	if len(seen) != 3 {
		t.Fatalf("declared 3 discriminator edges, context carries %d: %v", len(seen), seen)
	}
	values := make(map[string]bool, 3)
	for field, lit := range seen {
		if values[lit] {
			t.Errorf("edge %s reuses discriminator literal %s; sub-categorized edges must stay distinct", field, lit)
		}
		values[lit] = true
	}
}

// TestDiscriminator_ValueTheReadCannotCompareIsRejected pins PRD §13.4.1
// "Validation" and its §4.13 row. Every read path compares the discriminator
// column with the declared value, and three shapes of column make that
// comparison fail at runtime, measured against real databases:
//
//   - a value of the wrong kind for a basic or enum column: PostgreSQL rejects
//     it (22P02, or a pgx encode error), MySQL's `int = 'pinned'` matches the
//     rows holding 0, SQLite's matches nothing;
//   - PostgreSQL json / jsonb: 42883 / 22P02 on the loader, the O2O JOIN and
//     the relationship-filter subquery alike;
//   - a SQLite column bound to []byte or types.JSON: the client writes a BLOB,
//     which never equals the value the read binds.
//
// The kept cases were measured to read correctly, so refusing them would
// remove a working read: MySQL json, and PostgreSQL uuid / timestamptz / inet.
//
// Each case runs through `sqlgen validate` (ValidateGeneration), the graphql
// wrapper's entry point (BuildTableContextsFromSchema) and, for the refused
// ones, `sqlgen generate` (Generate), so none of the three reports clean where
// another refuses.
func TestDiscriminator_ValueTheReadCannotCompareIsRejected(t *testing.T) {
	tests := []struct {
		name        string
		dialect     config.Dialect
		relType     string
		colType     string
		nullable    bool
		noPointers  bool
		column      *config.ColumnOverride
		value       any
		wantRefusal string // empty: the config is valid
	}{
		{name: "pg json", dialect: config.DialectPostgres, colType: "json", value: "pinned", wantRefusal: "PostgreSQL json"},
		{name: "pg jsonb", dialect: config.DialectPostgres, colType: "jsonb", nullable: true, value: "pinned", wantRefusal: "PostgreSQL jsonb"},
		{name: "pg json, o2o", dialect: config.DialectPostgres, relType: "one_to_one", colType: "json", value: "primary", wantRefusal: "PostgreSQL json"},
		{name: "pg string on integer", dialect: config.DialectPostgres, colType: "integer", value: "pinned", wantRefusal: "binds to int32 and the value is a string"},
		{name: "pg string on nullable integer, no pointers", dialect: config.DialectPostgres, colType: "integer", nullable: true, noPointers: true, value: "pinned", wantRefusal: "binds to sql.NullInt32 and the value is a string"},
		{name: "pg integer on text", dialect: config.DialectPostgres, colType: "text", value: 3, wantRefusal: "binds to string and the value is an integer"},
		{name: "pg string on boolean", dialect: config.DialectPostgres, colType: "boolean", value: "true", wantRefusal: "binds to bool and the value is a string"},
		{name: "pg integer on enum", dialect: config.DialectPostgres, colType: "pin_kind", value: 3, wantRefusal: "binds to PinKind and the value is an integer"},
		{name: "pg uuid", dialect: config.DialectPostgres, colType: "uuid", value: "7f1c3a52-3b7e-4c5e-9f61-0c1b2d3e4f50"},
		{name: "pg timestamptz", dialect: config.DialectPostgres, colType: "timestamptz", value: "2026-01-01T00:00:00Z"},
		{name: "pg inet", dialect: config.DialectPostgres, colType: "inet", value: "10.0.0.1"},
		{name: "pg text", dialect: config.DialectPostgres, colType: "text", value: "pinned"},
		{name: "pg nullable integer", dialect: config.DialectPostgres, colType: "integer", nullable: true, value: 3},
		{name: "pg enum", dialect: config.DialectPostgres, colType: "pin_kind", value: "pinned"},
		{name: "mysql json", dialect: config.DialectMySQL, colType: "json", value: "pinned"},
		{name: "mysql json, o2o", dialect: config.DialectMySQL, relType: "one_to_one", colType: "json", value: "primary"},
		{name: "mysql string on int", dialect: config.DialectMySQL, colType: "int", value: "pinned", wantRefusal: "binds to int32 and the value is a string"},
		{name: "mysql integer on varchar", dialect: config.DialectMySQL, colType: "varchar(64)", value: 3, wantRefusal: "binds to string and the value is an integer"},
		// The SQLite parser types a JSON column, a declared type it does not
		// recognize, as "blob" (parser/sqlite: buildTypeString), so the schema
		// carries "blob". SQLite itself gives the column NUMERIC affinity.
		{name: "sqlite JSON, bound to []byte", dialect: config.DialectSQLite, colType: "blob", value: "pinned", wantRefusal: "binds to []byte, which the generated client writes as a BLOB"},
		{name: "sqlite JSON overridden to types.JSON", dialect: config.DialectSQLite, colType: "blob", column: &config.ColumnOverride{Type: "types.JSON", Import: "github.com/teandresmith/sqlgen/types"}, value: "pinned", wantRefusal: "binds to types.JSON, which the generated client writes as a BLOB"},
		{name: "sqlite JSON overridden to string", dialect: config.DialectSQLite, colType: "blob", column: &config.ColumnOverride{Type: "string"}, value: "pinned"},
		{name: "sqlite string on INTEGER", dialect: config.DialectSQLite, colType: "integer", value: "pinned", wantRefusal: "binds to int64 and the value is a string"},
		{name: "sqlite TEXT", dialect: config.DialectSQLite, colType: "text", value: "pinned"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := discriminatorSchema()
			if tt.dialect == config.DialectPostgres {
				schema.Enums = append(schema.Enums, parser.Enum{Name: "pin_kind", Values: []string{"pinned", "other"}})
			}
			schema.Tables = append(schema.Tables, parser.Table{
				Name: "pins",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "entity_id", Type: "uuid"},
					{Name: "tag", Type: tt.colType, Nullable: tt.nullable},
				},
			})
			in := testInput(schema)
			cfg := in.Config
			cfg.Input.Dialect = tt.dialect
			if tt.dialect != config.DialectPostgres {
				cfg.Output.Driver = "stdlib"
			}
			if tt.noPointers {
				cfg.Overrides.UsePointers = new(false)
			}
			if tt.column != nil {
				cfg.Tables["pins"] = config.TableConfig{ColumnMap: map[string]config.ColumnOverride{"tag": *tt.column}}
			}
			relType := tt.relType
			if relType == "" {
				relType = "one_to_many"
			}
			cfg.Tables["assets"] = config.TableConfig{Relationships: []config.TableRelationship{{
				Name: "Pinned", Type: relType, Table: "pins", FK: "entity_id",
				Discriminator: &config.RelationshipDiscriminator{Column: "tag", Value: tt.value},
			}}}

			_, validateErr := gen.ValidateGeneration(schema, cfg)
			_, buildErr := gen.BuildTableContextsFromSchema(schema, cfg)
			if tt.wantRefusal == "" {
				if validateErr != nil {
					t.Errorf("ValidateGeneration() = %v, want nil", validateErr)
				}
				if buildErr != nil {
					t.Errorf("BuildTableContextsFromSchema() = %v, want nil", buildErr)
				}
				return
			}

			// The fixture config is not complete enough to emit files, so a
			// Generate that does not refuse panics in emission. Either way it
			// got past validation, which is the failure being checked.
			cfg.Output.Dir = t.TempDir()
			generateErr := func() (err error) {
				defer func() {
					if r := recover(); r != nil {
						err = nil
						t.Logf("Generate() reached file emission and panicked there: %v", r)
					}
				}()
				_, err = gen.Generate(schema, cfg, "test")
				return err
			}()
			for _, entry := range []struct {
				path string
				err  error
			}{
				{"ValidateGeneration", validateErr},
				{"BuildTableContextsFromSchema", buildErr},
				{"Generate", generateErr},
			} {
				path, err := entry.path, entry.err
				if err == nil {
					t.Errorf("%s() = nil, want the discriminator refused", path)
					continue
				}
				for _, want := range []string{`"Pinned" declares discriminator tag = `, tt.wantRefusal, "§13.4.1"} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("%s() error %q does not mention %q", path, err, want)
					}
				}
			}
		})
	}
}
