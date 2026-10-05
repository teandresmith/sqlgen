package gen_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
	parserpkg "github.com/teandresmith/sqlgen/parser"
)

// This file is the scalar-binding gate. It closes the class that integer
// widths and `float32` are each one member of: a column whose resolved Go
// type has no GraphQL binding, or has one that binds gqlgen to a different Go
// type than the model wants.
//
// Three layers, in the order they run:
//
//  1. TestScalarBinding_everyBuiltInGoTypeResolves enumerates every Go type the
//     dialect tables can produce, at BOTH settings of `use_pointers`, and
//     asserts each one has a binding whose gqlgen Go type the translator can
//     reach. This is the regression pin.
//  2. TestInputTranslate_* pin the emitted code per shape.
//  3. TestScalarBinding_unboundTypeIsRejected pins the actual closure — the
//     codegen error. Enumeration alone cannot close the class, because
//     `overrides.types` and `type_map` literals produce Go types no test can
//     enumerate.

// scalarBindingSchema is one table carrying a column for every SQL type in the
// dialect's mapping table, at the requested nullability. The PK is a plain
// `text` column so the fixture works on all three dialects.
func scalarBindingSchema(t *testing.T, sqlTypes []string, nullable bool) *parserpkg.Schema {
	t.Helper()
	cols := make([]parserpkg.Column, 0, 1+len(sqlTypes))
	cols = append(cols, parserpkg.Column{Name: "id", Type: "text", PrimaryKey: true})
	for i, st := range sqlTypes {
		cols = append(cols, parserpkg.Column{
			// Positional names keep two SQL types that map to one Go type
			// (e.g. `blob` and `varbinary`) from colliding.
			Name:     "c" + string(rune('a'+i%26)) + strings.Repeat("x", i/26),
			Type:     st,
			Nullable: nullable,
		})
	}
	return &parserpkg.Schema{Tables: []parserpkg.Table{{Name: "widgets", Columns: cols}}}
}

// dialectSQLTypes lists the SQL types each dialect's gotype table maps, minus
// the ones that do not reach the scalar path.
//
// PostgreSQL `json` / `jsonb` are included; the enum and array paths are
// covered by the projection tests, which already exercise the named-slice
// bridge this table's assertion would otherwise duplicate.
var dialectSQLTypes = map[config.Dialect][]string{
	config.DialectPostgres: {
		"text", "varchar", "char", "name",
		"int2", "smallint", "int4", "integer", "serial", "int8", "bigint", "bigserial",
		"float4", "real", "float8", "double precision", "numeric", "decimal",
		"bool", "boolean",
		"bytea",
		"timestamp", "timestamptz", "date", "time", "interval",
		"uuid",
		"json", "jsonb",
		"inet", "cidr", "macaddr",
	},
	config.DialectMySQL: {
		"tinyint", "smallint", "int", "integer", "mediumint", "bigint",
		"int unsigned", "bigint unsigned",
		"float", "double", "decimal",
		"varchar", "text", "char", "enum", "tinytext", "mediumtext", "longtext",
		"set", "time",
		"datetime", "timestamp", "date",
		"json",
		"blob", "binary", "varbinary", "tinyblob", "mediumblob", "longblob",
		"bool", "boolean", "bit(1)",
		"year",
	},
	config.DialectSQLite: {
		"integer", "int", "tinyint", "smallint", "bigint",
		"text", "varchar", "char", "clob",
		"datetime", "timestamp", "timestamptz", "date", "time",
		"real", "float", "double", "numeric", "decimal",
		"blob",
		"boolean", "bool",
	},
}

// wantGqlgenGoType is a deliberate independent restatement of the rule
// gqlgenGoTypeFor implements: the four GraphQL spec built-ins have a fixed
// gqlgen binding regardless of the column's own Go type; every other scalar is
// bound to the model's Go type through the wrapper-merged `models:` entry.
//
// Restated here rather than exported from gen so the gate has a second opinion.
// A test that called the production helper would agree with it by construction
// and could not catch the helper itself drifting.
func wantGqlgenGoType(scalar, modelGoType string) string {
	switch scalar {
	case "Int":
		return "int"
	case "ID", "String":
		return "string"
	case "Boolean":
		return "bool"
	case "Float":
		return "float64"
	}
	return modelGoType
}

// declaredWidthCast mirrors gqlgenNumericWidthCasts — the only licensed
// disagreement between gqlgen's Go type and the model's.
var declaredWidthCast = map[string][]string{
	"int":     {"int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64"},
	"float64": {"float32"},
}

// TestScalarBinding_everyBuiltInGoTypeResolves is the scalar-binding
// enumeration, including the `use_pointers` axis.
//
// `use_pointers` is a table dimension, not a fixed setting. It is the whole
// reason the class was understated: at `true` a nullable column keeps its bare
// Go type, but at `false` — a documented, supported setting (PRD §4.7) — every
// nullable column of a `gotype.nullSQL` type becomes a `database/sql.NullX`
// wrapper. None of those was registered, so an ordinary nullable `text` column
// was advertised to clients as `String` in front of a `sql.NullString` field.
//
// Two assertions per column, and the second is what makes this fail-first:
//
//   - BuildAPIContext succeeds. An unbound Go type is a hard error, so
//     "builds" IS "every column resolved". Before that guard this passed
//     vacuously — there was no error to raise — which is why it is not the
//     only assertion.
//   - The Go type gqlgen will emit for the resolved scalar either equals the
//     model's Go type or differs by a declared numeric width. This is the
//     invariant itself, and it fails against the pre-fix generator for all
//     twelve newly-bound types: `bytea` resolved to `String`, whose gqlgen
//     binding is `string`, against a model `[]byte`.
func TestScalarBinding_everyBuiltInGoTypeResolves(t *testing.T) {
	for _, dialect := range []config.Dialect{config.DialectPostgres, config.DialectMySQL, config.DialectSQLite} {
		for _, nullable := range []bool{false, true} {
			for _, usePointers := range []bool{true, false} {
				name := string(dialect)
				if nullable {
					name += "/nullable"
				} else {
					name += "/not_null"
				}
				if usePointers {
					name += "/use_pointers"
				} else {
					name += "/sql_null"
				}
				t.Run(name, func(t *testing.T) {
					in := apiTestInput(t, scalarBindingSchema(t, dialectSQLTypes[dialect], nullable))
					in.Config.Input.Dialect = dialect
					in.Config.Overrides.UsePointers = new(usePointers)
					in.Resolver = gotype.NewResolver(dialect, usePointers, in.Config.Overrides.Types)

					tables, err := gen.BuildTableContexts(in, nil)
					if err != nil {
						t.Fatalf("BuildTableContexts() unexpected error: %v", err)
					}
					apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
					if err != nil {
						t.Fatalf("BuildAPIContext() unexpected error — a built-in SQL type has no sound GraphQL binding: %v", err)
					}

					modelGoType := make(map[string]string, len(tables[0].Columns))
					sqlType := make(map[string]string, len(tables[0].Columns))
					for _, c := range tables[0].Columns {
						modelGoType[c.Name] = strings.TrimPrefix(c.GoType, "*")
						sqlType[c.Name] = c.SQLType
					}

					for _, f := range apiCtx.Tables[0].Fields {
						model := modelGoType[f.SQLName]
						scalar := strings.TrimSuffix(f.GraphQLBare, "!")
						scalar = strings.TrimPrefix(strings.TrimSuffix(scalar, "!]"), "[")
						want := wantGqlgenGoType(scalar, model)
						if want == model || slices.Contains(declaredWidthCast[want], model) {
							continue
						}
						t.Errorf("%s column %q (%s): GraphQL %q binds gqlgen to Go %q, but the model is %q — "+
							"no conversion between them is defined",
							dialect, f.SQLName, sqlType[f.SQLName], scalar, want, model)
					}
				})
			}
		}
	}
}

// gqlgenBoundOnRead is a deliberate independent restatement of which Go types
// gqlgen can BIND a row-struct field to for each spec numeric scalar —
// `codegen/config/config.go::injectBuiltins` plus the exact basic-kind match
// `internal/code/compare.go::CompatibleTypes` demands.
//
// It is a different question from wantGqlgenGoType above, which answers what
// gqlgen EMITS for a generated position. Conflating the two is the
// defect this table exists to prevent: the emit side was fixed for every
// integer width and float32, while on the read side those same widths matched no entry, so
// `codegen/field.go::buildField` swallowed the binder error into
// `f.IsResolver = true` and gqlgen answered the field with
// panic("not implemented"). Silently — no error, no log.
var gqlgenBoundOnRead = map[string][]string{
	"Int":   {"int", "int32", "int64"},
	"Float": {"float64"},
}

// TestScalarBinding_everyReadableGoTypeBindsOrAnchors is the read-side twin of
// TestScalarBinding_everyBuiltInGoTypeResolves, and the assertion whose absence
// let the read-side half of the width defect go unnoticed.
//
// For every API-readable column the model Go type must either be one gqlgen
// already binds, or carry a marshaler anchor that makes it bindable. Anything
// else is a panic resolver in the consumer's server that no test, lint or
// golden would have caught.
func TestScalarBinding_everyReadableGoTypeBindsOrAnchors(t *testing.T) {
	for _, dialect := range []config.Dialect{config.DialectPostgres, config.DialectMySQL, config.DialectSQLite} {
		for _, nullable := range []bool{false, true} {
			for _, usePointers := range []bool{true, false} {
				name := string(dialect)
				if nullable {
					name += "/nullable"
				} else {
					name += "/not_null"
				}
				if usePointers {
					name += "/use_pointers"
				} else {
					name += "/sql_null"
				}
				t.Run(name, func(t *testing.T) {
					in := apiTestInput(t, scalarBindingSchema(t, dialectSQLTypes[dialect], nullable))
					in.Config.Input.Dialect = dialect
					in.Config.Overrides.UsePointers = new(usePointers)
					in.Resolver = gotype.NewResolver(dialect, usePointers, in.Config.Overrides.Types)

					tables, err := gen.BuildTableContexts(in, nil)
					if err != nil {
						t.Fatalf("BuildTableContexts() unexpected error: %v", err)
					}
					apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
					if err != nil {
						t.Fatalf("BuildAPIContext() unexpected error: %v", err)
					}

					modelGoType := make(map[string]string, len(tables[0].Columns))
					sqlType := make(map[string]string, len(tables[0].Columns))
					for _, c := range tables[0].Columns {
						modelGoType[c.Name] = strings.TrimPrefix(c.GoType, "*")
						sqlType[c.Name] = c.SQLType
					}

					for _, f := range apiCtx.Tables[0].Fields {
						if !f.Readable {
							continue
						}
						// The registry / consumer / enum scalars bind gqlgen to
						// the model's own Go type, so only the two spec numerics
						// can disagree at all.
						scalar := strings.TrimSuffix(f.GraphQLBare, "!")
						scalar = strings.TrimPrefix(strings.TrimSuffix(scalar, "!]"), "[")
						bound, isSpecNumeric := gqlgenBoundOnRead[scalar]
						if !isSpecNumeric {
							continue
						}
						// An array binds through its ELEMENT; the model type is
						// the slice, so compare on the element either way.
						model := strings.TrimPrefix(modelGoType[f.SQLName], "[]")
						if slices.Contains(bound, model) {
							continue
						}
						if f.NumericWidthAnchor.Name != "" && f.NumericWidthAnchor.GoType == model {
							continue
						}
						t.Errorf("%s column %q (%s): GraphQL %q against model Go type %q — gqlgen binds "+
							"only %v for that scalar and no marshaler anchor was registered, so gqlgen "+
							"answers this field with panic(\"not implemented\")",
							dialect, f.SQLName, sqlType[f.SQLName], scalar, model, bound)
					}
				})
			}
		}
	}
}

// TestScalarBinding_unboundTypeIsRejected pins the closure itself.
//
// Enumeration cannot close this class: `overrides.types` binds a column to any
// Go type the consumer names, and no test can enumerate those. The error is
// what covers them, and it has to name the column, the Go type and the way out
// — otherwise it is no improvement on the Go compile error at the far end of
// the pipeline that it replaces.
func TestScalarBinding_unboundTypeIsRejected(t *testing.T) {
	schema := &parserpkg.Schema{
		Tables: []parserpkg.Table{{
			Name: "widgets",
			Columns: []parserpkg.Column{
				{Name: "id", Type: "text", PrimaryKey: true},
				{Name: "weird", Type: "text"},
			},
		}},
	}
	in := apiTestInput(t, schema)
	in.Config.Overrides.Types["text"] = config.TypeOverride{
		Type:   "widget.Exotic",
		Import: "example.com/widget",
	}
	in.Resolver = gotype.NewResolver(config.DialectPostgres, true, in.Config.Overrides.Types)

	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() unexpected error: %v", err)
	}
	_, err = gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
	if err == nil {
		t.Fatal("BuildAPIContext() = nil error, want a rejection for the unbound Go type")
	}
	for _, want := range []string{"widget.Exotic", "api.graphql.scalars"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("BuildAPIContext() error = %v, want it to mention %q", err, want)
		}
	}
}

// TestInputTranslate_BinaryAndNetworkColumns is the binary/network shape pin.
//
// Five Go types the dialect tables reach — `[]byte`, `time.Duration`,
// `net.IP`, `net.IPNet`, `net.HardwareAddr` — had no binding, so every one
// emitted a `String` field in front of a non-string Go field and a translator
// that did not compile. `[]byte` is the severe one: `bytea` on PostgreSQL and
// `blob` / `binary` / `varbinary` on MySQL are routine columns.
//
// The two `[]byte` arms are asserted in OPPOSITE directions on purpose, because
// the old code got them wrong in opposite directions: it dereferenced where the
// value is already bare (`*in.Signature`, gqlgen emits a bare nilable slice)
// and failed to on the required arm. Which of the two applies is decided by
// the registry's `Nilable` flag, so both directions pin the same one bit.
func TestInputTranslate_BinaryAndNetworkColumns(t *testing.T) {
	schema := &parserpkg.Schema{
		Tables: []parserpkg.Table{{
			Name: "blobs",
			Columns: []parserpkg.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
				// Go-nilable model types: gqlgen emits bare T, never *T.
				{Name: "payload", Type: "bytea"},
				{Name: "signature", Type: "bytea", Nullable: true},
				{Name: "addr", Type: "inet"},
				{Name: "addr_n", Type: "inet", Nullable: true},
				{Name: "mac", Type: "macaddr"},
				{Name: "mac_n", Type: "macaddr", Nullable: true},
				// Non-nilable model types: gqlgen pointer-wraps when nullable.
				{Name: "dur", Type: "interval"},
				{Name: "dur_n", Type: "interval", Nullable: true},
				{Name: "net_block", Type: "cidr"},
				{Name: "net_block_n", Type: "cidr", Nullable: true},
			},
		}},
	}
	apiCtx := projectionAPIContext(t, schema)
	apiCtx.ModelsPackage = "models"

	tbl := apiCtx.Tables[0]
	for _, tc := range []struct{ column, want string }{
		{"payload", "Bytes!"},
		{"signature", "Bytes"},
		{"addr", "IP!"},
		{"dur", "Duration!"},
		{"net_block", "CIDR!"},
		{"mac", "MacAddr!"},
	} {
		if got := fieldBySQLName(t, tbl, tc.column).GraphQLType; got != tc.want {
			t.Errorf("column %q GraphQLType = %q, want %q", tc.column, got, tc.want)
		}
	}

	out := renderGqlmodelTemplate(t, "api/input-translate", apiCtx)
	createIdx := strings.Index(out, "func translateCreateBlobInput")
	updateIdx := strings.Index(out, "func translateUpdateBlobInput")
	if createIdx < 0 || updateIdx < 0 {
		t.Fatalf("expected both create and update translators:\n%s", out)
	}
	create, update := out[createIdx:updateIdx], out[updateIdx:]

	for _, want := range []string{
		// Nilable: threaded through with no deref, on both nullabilities.
		"out.Payload = in.Payload",
		"out.Signature = omittable.Set(in.Signature)",
		"out.Addr = in.Addr",
		"out.AddrN = omittable.Set(in.AddrN)",
		"out.MAC = in.MAC",
		"out.MACN = omittable.Set(in.MACN)",
		// Non-nilable: value on the required arm, gqlgen's *T on the nullable
		// one, whose pointee already matches the model's.
		"out.Dur = in.Dur",
		"out.DurN = omittable.Set(in.DurN)",
		"out.NetBlock = in.NetBlock",
		"out.NetBlockN = omittable.Set(in.NetBlockN)",
	} {
		if !strings.Contains(create, want) {
			t.Errorf("create translator missing %q:\n%s", want, create)
		}
	}

	for _, want := range []string{
		// Every update field is optional, so gqlgen emits bare T for the
		// nilable bindings and *T for the rest. The required-but-nilable arm
		// (`in.Payload`) is the one the old code dereferenced.
		"out.Payload = omittable.Set(in.Payload)",
		"out.Signature = omittable.Set(in.Signature)",
		"out.Addr = omittable.Set(in.Addr)",
		"out.MAC = omittable.Set(in.MAC)",
		"out.Dur = omittable.Set(*in.Dur)",
		"out.DurN = omittable.Set(in.DurN)",
		"out.NetBlock = omittable.Set(*in.NetBlock)",
		"out.NetBlockN = omittable.Set(in.NetBlockN)",
	} {
		if !strings.Contains(update, want) {
			t.Errorf("update translator missing %q:\n%s", want, update)
		}
	}

	// The uncastable spellings the pre-fix generator emitted must appear
	// nowhere: a deref of a nilable slice, and a cast that would reinterpret
	// a string's raw bytes as an address.
	for _, bad := range []string{
		"omittable.Set(*in.Signature)",
		"omittable.Set(*in.AddrN)",
		"omittable.Set(*in.MACN)",
		"net.IP(",
		"[]byte(in.",
	} {
		if strings.Contains(out, bad) {
			t.Errorf("input translator still emits the pre-fix form %q:\n%s", bad, out)
		}
	}
}

// TestInputTranslate_SQLNullWrappers is the sql.NullX wrapper shape pin.
//
// Under `overrides.use_pointers: false` every nullable column of a
// `gotype.nullSQL` type resolves to a `database/sql.NullX` wrapper. None was
// registered, so this shape did not need an exotic column type — an ordinary
// nullable `text` column produced a `String` field in front of a
// `sql.NullString` and `omittable.Set(*in.Description)`, which does not
// compile. It is the half of the class that also produced a WRONG SCHEMA: a
// nullable `integer` was advertised as `String`.
//
// PRD §26.4.1's Null-wrapper pairing already named the `database/sql.NullX`
// family; these are the registry entries that sentence always implied.
func TestInputTranslate_SQLNullWrappers(t *testing.T) {
	schema := &parserpkg.Schema{
		Tables: []parserpkg.Table{{
			Name: "products",
			Columns: []parserpkg.Column{
				{Name: "id", Type: "text", PrimaryKey: true},
				{Name: "description", Type: "text", Nullable: true},
				{Name: "stock", Type: "integer", Nullable: true},
				{Name: "big", Type: "bigint", Nullable: true},
				{Name: "small", Type: "smallint", Nullable: true},
				{Name: "active", Type: "boolean", Nullable: true},
				{Name: "ratio", Type: "float8", Nullable: true},
				{Name: "created_at", Type: "timestamp", Nullable: true},
			},
		}},
	}
	in := apiTestInput(t, schema)
	in.Config.Overrides.UsePointers = new(false)
	in.Resolver = gotype.NewResolver(config.DialectPostgres, false, in.Config.Overrides.Types)
	apiCtx := buildProjectionAPIContext(t, in)
	apiCtx.ModelsPackage = "models"

	// A Null-wrapper scalar carries nullability inside the struct, so the
	// field is emitted without `!` — and, critically, it is no longer `String`
	// for a column that is not a string.
	tbl := apiCtx.Tables[0]
	for _, tc := range []struct{ column, want string }{
		{"description", "NullString"},
		{"stock", "NullInt32"},
		{"big", "NullInt64"},
		{"small", "NullInt16"},
		{"active", "NullBool"},
		{"ratio", "NullFloat64"},
		{"created_at", "NullTime"},
	} {
		if got := fieldBySQLName(t, tbl, tc.column).GraphQLType; got != tc.want {
			t.Errorf("column %q GraphQLType = %q, want %q", tc.column, got, tc.want)
		}
	}

	out := renderGqlmodelTemplate(t, "api/input-translate", apiCtx)
	// The wrapper is a struct, so gqlgen pointer-wraps it and the translator
	// derefs onto the model's bare wrapper value. That arm always existed;
	// what changed is that the pointee is now the wrapper rather than a
	// `string` the model could not accept.
	for _, want := range []string{
		"out.Description = omittable.Set(*in.Description)",
		"out.Stock = omittable.Set(*in.Stock)",
		"out.Active = omittable.Set(*in.Active)",
		"out.CreatedAt = omittable.Set(*in.CreatedAt)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("input translator missing %q:\n%s", want, out)
		}
	}
	// No width cast: the wrapper binds gqlgen to the model's own type, so a
	// conversion here would be a self-conversion `unconvert` flags.
	for _, bad := range []string{"sql.NullInt32(", "sql.NullString("} {
		if strings.Contains(out, bad) {
			t.Errorf("input translator emits a needless conversion %q:\n%s", bad, out)
		}
	}
}

// TestScalarBinding_int64ScalarSuppressesSelfConversion pins a latent defect
// the scalar-binding rewrite fixes as a consequence rather than by intent.
//
// PRD §26.4.1 supports routing a column onto gqlgen's bundled `Int64` scalar
// via `api.graphql.scalars`. That binds gqlgen to `int64` directly, so no
// conversion is needed — but the predicate this change replaced keyed on the
// MODEL type alone, and `int64` was in its list, so it emitted `int64(in.X)`:
// a no-op self-conversion `unconvert` flags. Comparing the two sides instead
// of matching one of them is what removes it.
//
// "Binds gqlgen to `int64` directly" is only true because the merge pins the
// bundled marshaler. Without that pin, gqlgen's own two-entry model
// list decided the input field's type, and the identity arm this asserts —
// correct here — faced an `int` on the other side. The merge-side half is
// pinned by TestBuildMergeInput_BuiltinScalarPinsTheBundledMarshaler; the two
// together are what make the route compile.
func TestScalarBinding_int64ScalarSuppressesSelfConversion(t *testing.T) {
	schema := &parserpkg.Schema{
		Tables: []parserpkg.Table{{
			Name: "counters",
			Columns: []parserpkg.Column{
				{Name: "id", Type: "text", PrimaryKey: true},
				{Name: "hits", Type: "bigint"},
			},
		}},
	}
	in := apiTestInput(t, schema)
	in.Config.API.GraphQL.Scalars = map[string]config.ScalarBinding{
		"Int64": {GoType: "int64", Marshaling: config.ScalarMarshalingBuiltin},
	}
	apiCtx := buildProjectionAPIContext(t, in)
	apiCtx.ModelsPackage = "models"

	if got := fieldBySQLName(t, apiCtx.Tables[0], "hits").GraphQLType; got != "Int64!" {
		t.Fatalf("bigint column with an Int64 scalar declared GraphQLType = %q, want %q", got, "Int64!")
	}
	out := renderGqlmodelTemplate(t, "api/input-translate", apiCtx)
	if strings.Contains(out, "int64(in.Hits)") {
		t.Errorf("input translator emits a self-conversion for an Int64-bound column:\n%s", out)
	}
	if !strings.Contains(out, "out.Hits = in.Hits") {
		t.Errorf("input translator should thread an Int64-bound column through unchanged:\n%s", out)
	}
}

// TestScalarBinding_registryDrivesEveryDerivedTable pins the four tables a
// registry addition has to reach. Only the first pair was once pinned,
// which is why `scalarImports` and the scalars template could — and
// did — go stale relative to the registry.
func TestScalarBinding_registryDrivesEveryDerivedTable(t *testing.T) {
	registry := gen.BuiltInScalarRegistryNamesForTest()

	// config.BuiltInScalarGoTypes: the Go-type axis of "additions only".
	for goType, scalar := range registry {
		got, ok := config.BuiltInScalarGoTypes[goType]
		if !ok {
			t.Errorf("config.BuiltInScalarGoTypes is missing %q — a consumer could rebind it via api.graphql.scalars", goType)
			continue
		}
		if got != scalar {
			t.Errorf("config.BuiltInScalarGoTypes[%q] = %q, want %q", goType, got, scalar)
		}
	}
	for goType := range config.BuiltInScalarGoTypes {
		if _, ok := registry[goType]; !ok {
			t.Errorf("config.BuiltInScalarGoTypes has %q, which the registry does not bind", goType)
		}
	}

	// config.ReservedScalarNames: the name axis. A consumer entry named
	// `Decimal` does not add a scalar, it takes over the registry's slot.
	for goType, scalar := range registry {
		if _, ok := config.ReservedScalarNames[scalar]; !ok {
			t.Errorf("config.ReservedScalarNames is missing %q (bound to %q) — a consumer could shadow it", scalar, goType)
		}
	}

	// The scalars template: every external-marshaling entry needs a body, or
	// gqlgen anchors the models: entry at a package with no Marshal<Scalar>.
	tmplSrc := gen.APIScalarTemplateSourceForTest(t)
	for goType, scalar := range registry {
		if !gen.ScalarNeedsEmittedBodyForTest(goType) {
			continue
		}
		if !strings.Contains(tmplSrc, `eq $s.Name "`+scalar+`"`) {
			t.Errorf("scalars.go.tmpl has no Marshal/Unmarshal body for %q (bound to %q)", scalar, goType)
		}
	}
}

// TestScalarBinding_bundledRegistryEntriesPinAGqlgenMarshaler is the
// parity pin between the two tables that have to agree about what gqlgen
// ships: the registry's `marshaling: builtin` entries, and
// config.gqlgenBundledScalars, which turns each into a `models:` path.
//
// A registry entry claiming `builtin` for a Go type gqlgen bundles no
// marshaler for gets an empty path, so the merge silently drops the binding
// and the column falls back to whatever gqlgen's defaults make of it — a
// `panic("not implemented")` field resolver on read, and a create/update
// translator that does not compile. `time.Time` → `Time` is the only such
// entry today; the assertion is over the registry so a second one cannot be
// added without the table growing to match.
func TestScalarBinding_bundledRegistryEntriesPinAGqlgenMarshaler(t *testing.T) {
	for goType, scalar := range gen.BuiltInScalarRegistryNamesForTest() {
		if !gen.ScalarIsGqlgenBundledForTest(goType) {
			continue
		}
		if got := config.GqlgenBundledMarshalerPath(scalar, goType); got == "" {
			t.Errorf("registry binds %q to the gqlgen-bundled scalar %q, but config.GqlgenBundledMarshalerPath(%q, %q) = \"\" — the merge would emit no models: entry and the column would degrade to a panic resolver", goType, scalar, scalar, goType)
		}
	}
}

// qualifierToImport maps the package qualifier a marshaler body writes to the
// import path that provides it. Every package any emitted scalar body can
// reference must appear here; an unmapped qualifier fails the test below
// rather than being skipped, so a new body using a new package cannot slip
// past unnoticed.
var qualifierToImport = map[string]string{
	"graphql": "github.com/99designs/gqlgen/graphql",
	"io":      "io",
	"fmt":     "fmt",
	"strconv": "strconv",
	"base64":  "encoding/base64",
	"json":    "encoding/json",
	"sql":     "database/sql",
	"net":     "net",
	"time":    "time",
	"math":    "math",
	"uuid":    "github.com/google/uuid",
	"decimal": "github.com/shopspring/decimal",
}

// packageQualifiers returns the package qualifiers a rendered marshaler body
// references — the `X` of every `X.Sel` selector that is not a local.
//
// Parsed rather than pattern-matched. A regex over the source cannot tell
// `base64.StdEncoding` from `v.String()` or from `uuid.Nil` written inside a
// doc comment, and both false positives appeared when this was first written
// that way. The AST drops comments and the locals set removes receivers,
// parameters, named results and `:=` bindings, which is exactly the difference
// between a package qualifier and a variable.
func packageQualifiers(t *testing.T, body string) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "scalars.go", "package p\n"+body, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing rendered scalar body: %v\n%s", err, body)
	}

	locals := map[string]bool{}
	addField := func(fl *ast.FieldList) {
		if fl == nil {
			return
		}
		for _, f := range fl.List {
			for _, n := range f.Names {
				locals[n.Name] = true
			}
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.FuncDecl:
			addField(v.Recv)
			if v.Type != nil {
				addField(v.Type.Params)
				addField(v.Type.Results)
			}
		case *ast.FuncLit:
			if v.Type != nil {
				addField(v.Type.Params)
				addField(v.Type.Results)
			}
		case *ast.AssignStmt:
			for _, lhs := range v.Lhs {
				if id, ok := lhs.(*ast.Ident); ok {
					locals[id.Name] = true
				}
			}
		case *ast.TypeSwitchStmt:
			if a, ok := v.Assign.(*ast.AssignStmt); ok {
				for _, lhs := range a.Lhs {
					if id, ok := lhs.(*ast.Ident); ok {
						locals[id.Name] = true
					}
				}
			}
		}
		return true
	})

	seen := map[string]bool{}
	var out []string
	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok || locals[id.Name] || seen[id.Name] {
			return true
		}
		seen[id.Name] = true
		out = append(out, id.Name)
		return true
	})
	return out
}

// TestScalarBinding_everyMarshalerBodyHasItsImports pins the fourth derived
// table against the thing it derives: for each scalar sqlgen emits a body for,
// every package that body references must be in scalarImports' output.
//
// This pin is not theoretical. `Import` and `MarshalerImports` are hand-written on each
// registry entry, and guidelines/TEMPLATES.md §9 requires templates to supply
// their imports explicitly precisely because goimports may otherwise "repair"
// an omission by guessing — silently, and only for packages it can resolve.
// Rendering the body and reading the qualifiers back out is the only check
// that cannot be fooled by that rescue.
func TestScalarBinding_everyMarshalerBodyHasItsImports(t *testing.T) {
	// A third-party binding's import arrives on APIScalarUse.GoImport, taken
	// from the resolved column, because the registry cannot know it — a
	// consumer may bind `uuid.UUID` to google's package or gofrs'. The
	// registry's own `Import` covers only the stdlib bindings it can name.
	// Supplying these here reproduces what BuildAPIContext populates; the
	// twelve stdlib entries this test exists for get nothing extra.
	columnImport := map[string]string{
		"uuid.UUID":           "github.com/google/uuid",
		"uuid.NullUUID":       "github.com/google/uuid",
		"decimal.Decimal":     "github.com/shopspring/decimal",
		"decimal.NullDecimal": "github.com/shopspring/decimal",
	}

	for _, use := range gen.ExternalScalarUsesForTest() {
		t.Run(use.Name, func(t *testing.T) {
			if imp, ok := columnImport[use.GoType]; ok {
				use.GoImport = imp
			}
			ctx := &gen.APIContext{ExternalScalars: []gen.APIScalarUse{use}}
			body := renderGqlmodelTemplate(t, "api/scalars", ctx)
			if strings.TrimSpace(body) == "" {
				t.Fatalf("scalar %q (%s) renders an empty body, but its registry entry says sqlgen emits one",
					use.Name, use.GoType)
			}

			declared := gen.ScalarImportsForTest([]gen.APIScalarUse{use})
			for _, qualifier := range packageQualifiers(t, body) {
				want, known := qualifierToImport[qualifier]
				if !known {
					t.Errorf("body references unknown package qualifier %q; add it to qualifierToImport "+
						"(and make sure the registry entry declares its import)", qualifier)
					continue
				}
				if !slices.Contains(declared, want) {
					t.Errorf("body references %s.* but scalarImports = %v does not include %q — "+
						"add it to the registry entry's Import or MarshalerImports",
						qualifier, declared, want)
				}
			}
		})
	}
}

// nullWrapperPartners names each `sql.NullX` scalar's spec-built-in partner —
// the gqlgen unmarshaler its value half must delegate to.
var nullWrapperPartners = map[string]string{
	"NullString":  "graphql.UnmarshalString",
	"NullBool":    "graphql.UnmarshalBoolean",
	"NullInt16":   "graphql.UnmarshalInt16",
	"NullInt32":   "graphql.UnmarshalInt32",
	"NullInt64":   "graphql.UnmarshalInt64",
	"NullFloat64": "graphql.UnmarshalFloat",
	"NullTime":    "graphql.UnmarshalTime",
}

// TestScalarBinding_nullWrappersDelegateCoercion pins that each `sql.NullX`
// unmarshaler hands the value half to gqlgen rather than asserting a Go type.
//
// This is the pin for the defect the first implementation of these bodies
// shipped: they asserted `v.(int)`, reasoning from the Go type gqlgen BINDS
// the `Int` scalar to. That is not what an unmarshaler receives. The argument
// is whatever the request decoder produced — gqlparser parses an inline
// IntValue with `strconv.ParseInt(..., 10, 64)`, so it arrives as `int64`, and
// the HTTP handler decodes variables with `UseNumber`, so those arrive as
// `json.Number`. A `v.(int)` assertion therefore rejected essentially every
// real request while every codegen-level test passed, because nothing here
// executed the body.
//
// Asserting on delegation rather than on behaviour is deliberate: these bodies
// are generated text, so executing them needs a compiled consumer package.
// Delegation is the property that makes behaviour correct BY CONSTRUCTION --
// gqlgen's own unmarshaler is what the partner scalar uses, so the pair cannot
// disagree about what it accepts, which is PRD 26.4.1's wire-format invariant
// stated as code instead of prose.
func TestScalarBinding_nullWrappersDelegateCoercion(t *testing.T) {
	for _, use := range gen.ExternalScalarUsesForTest() {
		partner, ok := nullWrapperPartners[use.Name]
		if !ok {
			continue
		}
		t.Run(use.Name, func(t *testing.T) {
			ctx := &gen.APIContext{ExternalScalars: []gen.APIScalarUse{use}}
			body := renderGqlmodelTemplate(t, "api/scalars", ctx)

			if !strings.Contains(body, partner+"(v)") {
				t.Errorf("Unmarshal%s does not delegate to %s; it must not hand-roll the coercion:\n%s",
					use.Name, partner, body)
			}
			// The specific anti-pattern: a bare type assertion on the incoming
			// value. `v == nil` stays -- that is the wrapper's own null case.
			// Checked against the AST, not the text: the doc comments in these
			// bodies deliberately quote `v.(int)` while explaining why it is
			// wrong, and a substring match cannot tell prose from code.
			if assertsOnInput(t, body) {
				t.Errorf("Unmarshal%s type-asserts the raw input; gqlgen delivers int64 / json.Number "+
					"for numeric scalars, so an assertion rejects valid requests:\n%s", use.Name, body)
			}
		})
	}

	// Every partner named above must actually be a registry entry, or this
	// table has gone stale relative to the registry it describes.
	registered := map[string]bool{}
	for _, scalar := range gen.BuiltInScalarRegistryNamesForTest() {
		registered[scalar] = true
	}
	for name := range nullWrapperPartners {
		if !registered[name] {
			t.Errorf("nullWrapperPartners names %q, which the registry does not bind", name)
		}
	}
}

// assertsOnInput reports whether a rendered marshaler body type-asserts or
// type-switches on `v`, the raw value gqlgen hands an unmarshaler.
func assertsOnInput(t *testing.T, body string) bool {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "scalars.go", "package p\n"+body, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing rendered scalar body: %v\n%s", err, body)
	}
	isInput := func(e ast.Expr) bool {
		id, ok := e.(*ast.Ident)
		return ok && id.Name == "v"
	}
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.TypeAssertExpr:
			if isInput(x.X) {
				found = true
			}
		case *ast.TypeSwitchStmt:
			ast.Inspect(x.Assign, func(m ast.Node) bool {
				if ta, ok := m.(*ast.TypeAssertExpr); ok && isInput(ta.X) {
					found = true
				}
				return true
			})
		}
		return !found
	})
	return found
}

// TestScalarBinding_numericArrayDictatesItsInputGoType pins the boundary:
// a PostgreSQL array of a narrowed numeric type is generated,
// not rejected.
//
// gqlgen types a GENERATED list field as `[]Model[0]` — `[]int` for `[Int!]!` —
// against a model that carries the column's native width (`[]int32` for
// `integer[]`), and Go has no conversion between slices of differing element
// types. The fix removes the disagreement instead of bridging it: sqlgen
// dictates the field's Go type through `models.<Input>.fields.<f>.type`, so
// the model's own `[]int32` IS what gqlgen emits and inputCoercionFor takes
// its identity arm.
//
// The assertions are the two halves of that claim. CastTo must be empty —
// a cast here would mean the two sides still disagreed — and GqlgenGoType must
// carry the model's slice type, because that string is what reaches the merged
// gqlgen.yml. `text[]` is the control: it already agreed, so it must acquire
// no override.
func TestScalarBinding_numericArrayDictatesItsInputGoType(t *testing.T) {
	schema := &parserpkg.Schema{Tables: []parserpkg.Table{{
		Name: "arr",
		Columns: []parserpkg.Column{
			{Name: "id", Type: "uuid", PrimaryKey: true},
			{Name: "tags", Type: "text[]"},
			{Name: "smalls", Type: "smallint[]"},
			{Name: "scores", Type: "integer[]"},
			{Name: "bigs", Type: "bigint[]"},
			{Name: "ratios", Type: "real[]"},
		},
	}}}
	in := apiTestInput(t, schema)
	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() unexpected error: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext() unexpected error — a numeric array must now generate: %v", err)
	}

	want := map[string]string{
		"tags":   "",
		"smalls": "[]int16",
		"scores": "[]int32",
		"bigs":   "[]int64",
		"ratios": "[]float32",
	}
	for _, f := range apiCtx.Tables[0].CreateInputFields {
		wantType, ok := want[f.GraphQLName]
		if !ok {
			continue
		}
		if f.GqlgenGoType != wantType {
			t.Errorf("column %q: GqlgenGoType = %q, want %q", f.GraphQLName, f.GqlgenGoType, wantType)
		}
		if f.CastTo != "" {
			t.Errorf("column %q: CastTo = %q, want empty — a dictated input type leaves nothing to convert",
				f.GraphQLName, f.CastTo)
		}
	}
}

// TestScalarBinding_numericArrayAnchorsItsElementWidth pins the read-side half
// of the same columns. Dictating the input type is not enough on its own: with
// no marshaler for `int16` in the `Int` model list, gqlgen accepts the override
// and then falls back to an input-object field RESOLVER — a second panic stub
// in place of the first.
func TestScalarBinding_numericArrayAnchorsItsElementWidth(t *testing.T) {
	schema := &parserpkg.Schema{Tables: []parserpkg.Table{{
		Name: "arr",
		Columns: []parserpkg.Column{
			{Name: "id", Type: "uuid", PrimaryKey: true},
			{Name: "smalls", Type: "smallint[]"},
			{Name: "scores", Type: "integer[]"},
			{Name: "ratios", Type: "real[]"},
		},
	}}}
	in := apiTestInput(t, schema)
	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() unexpected error: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext() unexpected error: %v", err)
	}

	got := make(map[string]gen.APINumericWidth, len(apiCtx.NumericWidthScalars))
	for _, w := range apiCtx.NumericWidthScalars {
		got[w.Name] = w
	}
	// int32 is gqlgen-bound already, so `integer[]` must contribute nothing.
	if _, ok := got["Int32"]; ok {
		t.Errorf("NumericWidthScalars carries Int32, which gqlgen already binds: %+v", apiCtx.NumericWidthScalars)
	}
	for _, want := range []struct {
		name, goType, scalar string
		sqlgenEmitted        bool
	}{
		{"Int16", "int16", "Int", false},
		{"Float32", "float32", "Float", true},
	} {
		w, ok := got[want.name]
		if !ok {
			t.Errorf("NumericWidthScalars missing %q, got %+v", want.name, apiCtx.NumericWidthScalars)
			continue
		}
		if w.GoType != want.goType || w.Scalar != want.scalar {
			t.Errorf("%s = {GoType:%q Scalar:%q}, want {GoType:%q Scalar:%q}",
				want.name, w.GoType, w.Scalar, want.goType, want.scalar)
		}
		if w.SqlgenEmitted() != want.sqlgenEmitted {
			t.Errorf("%s.SqlgenEmitted() = %v, want %v (MarshalerPath %q)",
				want.name, w.SqlgenEmitted(), want.sqlgenEmitted, w.MarshalerPath)
		}
	}
}

// setBindingInput builds the MySQL fixture the SET assertions share: one table
// carrying a NOT NULL and a nullable SET column, with both SET types
// registered on the resolver exactly as the `Generate` pipeline's
// registerSchemaTypes does.
//
// The SET path cannot ride dialectSQLTypes above. That table is keyed on SQL
// type NAMES, and the bare `set` entry there resolves through the built-in
// MySQL mapping to a plain `string` — it never reaches the named-slice type a
// real SET column produces, which is exactly why the enumeration once passed
// while SET columns were broken.
func setBindingInput(t *testing.T) (*gen.GenerateInput, []gen.SetContext) {
	t.Helper()
	schema := &parserpkg.Schema{
		Tables: []parserpkg.Table{{
			Name: "users",
			Columns: []parserpkg.Column{
				{Name: "id", Type: "bigint", PrimaryKey: true},
				{Name: "permissions", Type: "users_permissions_set"},
				{Name: "opt_perms", Type: "users_opt_perms_set", Nullable: true},
			},
		}},
		Sets: []parserpkg.Set{
			{Name: "users_permissions_set", Values: []string{"read", "write", "delete", "admin"}},
			// A multi-word value pins that SET identifiers go through the same
			// gqlEnumIdent helper enums use, rather than a second spelling.
			{Name: "users_opt_perms_set", Values: []string{"alpha", "multi_word_value"}},
		},
	}
	in := apiTestInput(t, schema)
	in.Config.Input.Dialect = config.DialectMySQL
	collisions := gen.ComputeNameCollisions(schema)
	r := gotype.NewResolver(config.DialectMySQL, true, in.Config.Overrides.Types)
	for _, s := range schema.Sets {
		r.RegisterSet(s.Name, gen.StructName(s.Name, s.Schema, collisions))
	}
	in.Resolver = r
	return in, gen.BuildSetContexts(schema, collisions)
}

// TestScalarBinding_setColumnProjectsEnumList is the SET pin — the SET
// member of the class TestScalarBinding_everyBuiltInGoTypeResolves enumerates.
//
// A MySQL SET column resolves to a named slice (`UsersPermissionsSet` =
// `[]UsersPermissionsSetValue`). Without SET types threaded into typeBindings,
// graphQLTypeForGoType finds no binding and the unbound-type guard fails
// generation outright; before that guard existed the same column fell
// through to `String` and gqlgen answered it with panic("not implemented").
//
// The projection is a GraphQL enum list (PRD §26.4), which is the same shape
// a PostgreSQL enum array takes — value type on the elements, named slice on
// the column — so both assertions here mirror the enum-array path exactly.
func TestScalarBinding_setColumnProjectsEnumList(t *testing.T) {
	in, sets := setBindingInput(t)
	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() unexpected error: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, nil, nil, sets, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext() unexpected error — a MySQL SET column has no GraphQL binding: %v", err)
	}

	wantField := map[string]string{
		// NOT NULL: the list itself is non-null, elements are non-null.
		"permissions": "[UsersPermissionsSetValue!]!",
		// Nullable: SETs are reference types, so the Go type is unchanged and
		// only the outer list loses its `!`.
		"opt_perms": "[UsersOptPermsSetValue!]",
		"id":        "Int!",
	}
	for _, f := range apiCtx.Tables[0].Fields {
		if want := wantField[f.SQLName]; f.GraphQLType != want {
			t.Errorf("column %q GraphQL type = %q, want %q", f.SQLName, f.GraphQLType, want)
		}
	}

	// The GraphQL enum is named after the VALUE type and carries the named
	// slice as its sibling, which is what makes the wrapper emit the two-entry
	// `models:` list gqlgen needs to bind both the list elements and the
	// column's own Go type.
	wantEnum := map[string]struct {
		slice  string
		idents []string
	}{
		"UsersPermissionsSetValue": {slice: "UsersPermissionsSet", idents: []string{"READ", "WRITE", "DELETE", "ADMIN"}},
		"UsersOptPermsSetValue":    {slice: "UsersOptPermsSet", idents: []string{"ALPHA", "MULTI_WORD_VALUE"}},
	}
	if len(apiCtx.UsedEnums) != len(wantEnum) {
		t.Fatalf("UsedEnums = %d entries, want %d — every referenced SET must declare its enum", len(apiCtx.UsedEnums), len(wantEnum))
	}
	for _, e := range apiCtx.UsedEnums {
		want, ok := wantEnum[e.GraphQLName]
		if !ok {
			t.Errorf("UsedEnums contains unexpected entry %q", e.GraphQLName)
			continue
		}
		if e.GoTypeName != e.GraphQLName {
			t.Errorf("enum %q Go type = %q, want the GraphQL name — the models: merge binds list elements to it", e.GraphQLName, e.GoTypeName)
		}
		if e.SliceGoTypeName != want.slice {
			t.Errorf("enum %q slice sibling = %q, want %q — the column's own Go type", e.GraphQLName, e.SliceGoTypeName, want.slice)
		}
		got := make([]string, 0, len(e.Values))
		for _, v := range e.Values {
			got = append(got, v.GraphQLName)
		}
		if diff := cmp.Diff(want.idents, got); diff != "" {
			t.Errorf("enum %q identifiers mismatch (-want +got):\n%s", e.GraphQLName, diff)
		}
	}
}

// TestInputTranslate_SetColumn pins the write half: gqlgen types a generated
// input position for `[<Enum>!]!` as `[]<Enum>`, while the model field carries
// the named slice. The bridge is the same zero-cost reinterpret the PostgreSQL
// enum array uses, because the named slice's underlying type IS `[]<Enum>`.
//
// GqlgenIsBareNullable is the assertion the SET projection's coupled hazard
// calls for, and it lands here rather than on isNilableGqlgenGoType because the
// predicate keys on the GQLGEN type, not the model's: gqlgen emits a bare
// `[]T` for a nullable list (verified against v0.17.90), so the `[]` arm
// already answers correctly and the translator must not emit a deref.
func TestInputTranslate_SetColumn(t *testing.T) {
	in, sets := setBindingInput(t)
	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() unexpected error: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, nil, nil, sets, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext() unexpected error: %v", err)
	}

	wantCast := map[string]string{
		"permissions": "db.UsersPermissionsSet",
		"optPerms":    "db.UsersOptPermsSet",
	}
	seen := 0
	for _, f := range apiCtx.Tables[0].CreateInputFields {
		want, ok := wantCast[f.GraphQLName]
		if !ok {
			continue
		}
		seen++
		if f.CastTo != want {
			t.Errorf("input field %q CastTo = %q, want %q — the named-slice reinterpret", f.GraphQLName, f.CastTo, want)
		}
		if !f.GqlgenIsBareNullable {
			t.Errorf("input field %q GqlgenIsBareNullable = false, want true — gqlgen emits a bare []T for a list, so a deref would not compile", f.GraphQLName)
		}
		if f.GqlgenGoType != "" {
			t.Errorf("input field %q dictates gqlgen type %q, want gqlgen's own default to stand", f.GraphQLName, f.GqlgenGoType)
		}
	}
	if seen != len(wantCast) {
		t.Fatalf("matched %d SET input fields, want %d", seen, len(wantCast))
	}
}

// TestScalarBinding_overriddenSetIsNotListShaped pins the trap the SET
// projection walked into: `ColumnContext.IsSet` is derived from the SQL type
// NAME, but `type_map.<col>` and `overrides.types` win over the SET branch in
// the resolution chain, so an overridden SET column carries IsSet=true while
// its Go type is not a named slice at all.
//
// Keying the list projection on IsSet alone advertised such a column as
// `[String!]!` in front of a plain `string` field and emitted
// `models.string(in.Permissions)` in the input translator — a NEW defect
// introduced by the SET projection, on a path that generated correctly before it.
// Everything API-side now keys on setElemGoType, which reads the RESOLVED
// element type and is therefore override-aware.
func TestScalarBinding_overriddenSetIsNotListShaped(t *testing.T) {
	schema := &parserpkg.Schema{
		Tables: []parserpkg.Table{{
			Name: "users",
			Columns: []parserpkg.Column{
				{Name: "id", Type: "bigint", PrimaryKey: true},
				{Name: "permissions", Type: "users_permissions_set"},
			},
		}},
		Sets: []parserpkg.Set{{Name: "users_permissions_set", Values: []string{"read", "write"}}},
	}
	in := apiTestInput(t, schema)
	in.Config.Input.Dialect = config.DialectMySQL
	// The documented escape hatch (PRD §7.1): retype the column, and the
	// column stops being a SET as far as Go is concerned.
	in.Config.Tables["users"] = config.TableConfig{TypeMap: map[string]string{"permissions": "string"}}
	collisions := gen.ComputeNameCollisions(schema)
	r := gotype.NewResolver(config.DialectMySQL, true, in.Config.Overrides.Types)
	r.RegisterSet("users_permissions_set", "UsersPermissionsSet")
	in.Resolver = r

	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() unexpected error: %v", err)
	}
	// The premise: the flag says SET, the resolved type says otherwise. If this
	// ever stops holding, the guard under test is no longer load-bearing and
	// this test should be revisited rather than deleted.
	for _, c := range tables[0].Columns {
		if c.Name != "permissions" {
			continue
		}
		if !c.IsSet || c.GoType != "string" || c.SliceElemType != "" {
			t.Fatalf("fixture premise broken: IsSet=%v GoType=%q SliceElemType=%q, want true/\"string\"/\"\"",
				c.IsSet, c.GoType, c.SliceElemType)
		}
	}

	apiCtx, err := gen.BuildAPIContext(tables, nil, nil, gen.BuildSetContexts(schema, collisions), in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext() unexpected error: %v", err)
	}
	for _, f := range apiCtx.Tables[0].Fields {
		if f.SQLName != "permissions" {
			continue
		}
		if f.GraphQLType != "String!" {
			t.Errorf("overridden SET column GraphQL type = %q, want %q — the override made it a scalar", f.GraphQLType, "String!")
		}
	}
	for _, f := range apiCtx.Tables[0].CreateInputFields {
		if f.GraphQLName != "permissions" {
			continue
		}
		if f.CastTo != "" {
			t.Errorf("overridden SET input field CastTo = %q, want empty — gqlgen binds `String` to `string`, so the identity arm applies", f.CastTo)
		}
	}

	// A retyped column references no SET, so the schema must not declare an
	// enum nothing uses.
	if len(apiCtx.UsedEnums) != 0 {
		t.Errorf("UsedEnums = %v, want none — the overridden column no longer references the SET", apiCtx.UsedEnums)
	}
}
