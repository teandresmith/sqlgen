package manifest

import (
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// BuildInput bundles every read-only handle the builder consumes. The fields
// match the in-memory state already produced by the codegen pipeline so the
// builder runs as a leaf stage after all _gen.go files have been emitted.
//
// APIContext may be nil when API generation is disabled. Version threads in
// the running sqlgen binary's version for the Generator stanza and the
// $schema URL.
type BuildInput struct {
	Schema    *parser.Schema
	Config    *config.RootConfig
	Tables    []gen.TableContext
	Views     []gen.ViewContext
	Enums     []gen.EnumContext
	API       *gen.APIContext
	Version   string
	Timestamp string
}

// Build constructs the manifest Document from the codegen pipeline's
// in-memory state. Read-only — no emission, no I/O.
//
// Per PRD §30.4 / §30.4.3 the returned Document carries the top-level
// envelope (schema_version, generator, dialect, package, layout,
// generation_config), the package-wide conventions block, and one Entity per
// included table + view. Per-entity `manifest.enabled: false` opt-outs
// (PRD §30.2) suppress the entity from the result; the global
// `manifest.enabled` toggle gates Build's invocation in the orchestrator.
func Build(in BuildInput) (*Document, error) {
	if in.Config == nil {
		return nil, fmt.Errorf("manifest build: config is nil")
	}
	if in.Schema == nil {
		return nil, fmt.Errorf("manifest build: schema is nil")
	}

	// Two unrelated "layout" knobs meet here. jsonLayout is the manifest's own
	// single / per_entity JSON shape; goLayout is output.layout, which decides
	// whether an entity's generated body lands in models_gen.go / views_gen.go
	// or in its own <snake>_gen.go. Reading the first where the second was
	// meant once sent files[] to the wrong file, so neither is spelled `layout`
	// any more.
	jsonLayout := manifestJSONLayout(in.Config)
	goLayout := in.Config.Output.Layout
	includeExamples := manifestIncludesExamples(in.Config)

	entities := make([]Entity, 0, len(in.Tables)+len(in.Views))
	for _, t := range in.Tables {
		if !manifestEntityEnabled(in.Config, t.TableName, t.Schema) {
			continue
		}
		entities = append(entities, buildTableEntity(in, t, goLayout, includeExamples))
	}
	for _, v := range in.Views {
		if !manifestEntityEnabled(in.Config, v.ViewName, v.Schema) {
			continue
		}
		entities = append(entities, buildViewEntity(in, v, goLayout))
	}
	sort.SliceStable(entities, func(i, j int) bool {
		return entities[i].Name < entities[j].Name
	})

	doc := &Document{
		Schema:           schemaURL(in.Version),
		SchemaVersion:    SchemaVersion,
		GeneratedAt:      in.Timestamp,
		Generator:        Generator{Name: generatorName, Version: in.Version},
		Dialect:          string(in.Config.Input.Dialect),
		Package:          in.Config.Output.Package,
		Layout:           jsonLayout,
		Conventions:      buildConventions(in.Config, in.Tables, in.Views),
		GenerationConfig: buildGenerationConfig(in.Config, in.Tables),
		Entities:         entities,
		Enums:            buildEnums(in.Enums),
		Extras:           buildExtras(in.Schema, in.Config),
	}
	return doc, nil
}

// --- JSON layout + opt-out resolution ---

// manifestJSONLayout reports the manifest's own JSON shape (single /
// per_entity). Deliberately NOT named for "layout" alone: it has nothing to do
// with output.layout, which governs the generated Go files (see TableFileName).
func manifestJSONLayout(cfg *config.RootConfig) string {
	if cfg.Generation.Manifest == nil || cfg.Generation.Manifest.JSONLayout == "" {
		return string(config.JSONLayoutSingle)
	}
	return string(cfg.Generation.Manifest.JSONLayout)
}

func manifestIncludesExamples(cfg *config.RootConfig) bool {
	m := cfg.Generation.Manifest
	if m == nil || m.IncludeExamples == nil {
		return true
	}
	return *m.IncludeExamples
}

func manifestEntityEnabled(cfg *config.RootConfig, name, schema string) bool {
	return config.ResolveTableManifestEnabled(findTableConfig(cfg, name, schema), cfg.Generation)
}

// findTableConfig mirrors gen.findTableConfig but is duplicated here to avoid
// exposing the unexported helper. Tries schema-qualified key first, then bare;
// returns the zero TableConfig when neither key is present.
func findTableConfig(cfg *config.RootConfig, name, schema string) config.TableConfig {
	if schema != "" {
		if tc, ok := cfg.Tables[schema+"."+name]; ok {
			return tc
		}
	}
	if tc, ok := cfg.Tables[name]; ok {
		return tc
	}
	return config.TableConfig{}
}

// findViewConfig is findTableConfig's view twin: same schema-qualified-then-bare
// key resolution against cfg.Views, returning the zero ViewConfig when neither
// key is present.
func findViewConfig(cfg *config.RootConfig, name, schema string) config.ViewConfig {
	if schema != "" {
		if vc, ok := cfg.Views[schema+"."+name]; ok {
			return vc
		}
	}
	if vc, ok := cfg.Views[name]; ok {
		return vc
	}
	return config.ViewConfig{}
}

func schemaURL(version string) string {
	if version == "" {
		return ""
	}
	return "https://raw.githubusercontent.com/teandresmith/sqlgen/v" + version + "/cmd/sqlgen/manifest/schema/v1.json"
}

// --- generation_config snapshot (PRD §30.4.3) ---

// buildGenerationConfig snapshots the package-level feature toggles. Per PRD
// §30.4.3, soft_delete and audit_columns reflect actual per-table detection
// over the whole generated package — not the mere presence of a config list,
// which applyDefaults fills with detection *candidates* whenever unset. It
// therefore aggregates over every built table context (not the
// manifest-filtered entities, since a per-table `manifest.enabled: false`
// opt-out must not flip a package-level toggle).
func buildGenerationConfig(cfg *config.RootConfig, tables []gen.TableContext) GenerationConfig {
	return GenerationConfig{
		AuditColumns:  anyTableHasAuditColumns(tables, cfg),
		Cache:         cfg.Cache != nil && cfg.Cache.Enabled,
		Events:        cfg.Events != nil && cfg.Events.Enabled,
		GraphQL:       graphQLEnabled(cfg),
		GraphTopLevel: graphTopLevel(cfg),
		SoftDelete:    anyTableHasSoftDelete(tables),
		Tenancy:       tenancyEnabled(cfg),
		Views:         len(cfg.Views) > 0,
	}
}

// anyTableHasSoftDelete reports whether any generated table has a detected
// soft-delete column (PRD §30.4.3 → §15). SoftDelete is nil when
// detectTableSoftDelete found no matching column on the table.
func anyTableHasSoftDelete(tables []gen.TableContext) bool {
	for i := range tables {
		if tables[i].SoftDelete != nil {
			return true
		}
	}
	return false
}

// anyTableHasAuditColumns reports whether any generated table declares audit
// columns (PRD §30.4.3 → §4.6). auditColumnsForTable intersects the configured
// update-column candidates against the table's real columns, so a non-empty
// result means the table actually carries an audit column.
func anyTableHasAuditColumns(tables []gen.TableContext, cfg *config.RootConfig) bool {
	for i := range tables {
		if len(auditColumnsForTable(tables[i], cfg)) > 0 {
			return true
		}
	}
	return false
}

func graphQLEnabled(cfg *config.RootConfig) bool {
	return cfg.API != nil && cfg.API.Enabled && cfg.API.GraphQL != nil && cfg.API.GraphQL.Enabled
}

// graphTopLevel reports whether the resolved GraphQL graph package lives
// outside output.dir — a top-level sibling of the models tree — rather than
// nested beneath it (the default <output.dir>/graph). Per PRD §30.4.3 /
// §26.5.8 the topology is a pure function of the root-relative resolver_dir
// path; there is no mode flag. Both output.dir and resolver_dir are
// root-relative (anchored at the module root), so a relative path from
// output.dir to resolver_dir that escapes output.dir (starts with "..") means
// the graph tree is a sibling. Nested layouts (<output.dir>/graph,
// models/graph) stay under output.dir and report false.
func graphTopLevel(cfg *config.RootConfig) bool {
	if !graphQLEnabled(cfg) {
		return false
	}
	// After applyDefaults ResolverDir carries the full root-relative path
	// (defaulting to <output.dir>/graph). An empty value only occurs for a
	// hand-built config that skipped defaults — treat that as the nested
	// default.
	resolverDir := cfg.API.GraphQL.ResolverDir
	if resolverDir == "" {
		return false
	}
	rel, err := filepath.Rel(cfg.Output.Dir, resolverDir)
	if err != nil {
		return false
	}
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// tenancyEnabled reports whether tenancy is configured for the package. Shared
// by the generation_config snapshot and the conventions block so the two can
// never disagree about whether the tenancy sentinels are reachable.
func tenancyEnabled(cfg *config.RootConfig) bool {
	return cfg.Tenancy != nil && cfg.Tenancy.Enabled
}

// callOptionsFieldNames lists the fields of the generated CallOptions[FO] in
// declaration order, under the same conditions sharedTypeDefinitions emits
// them. An agent reads this list to know which knobs a call carries, so a name
// that resolves to nothing is worse than a missing one (the reasoning §30.4.1
// records for error_sentinels): the list previously advertised a `Tx` field
// that has never existed on the struct, while omitting FieldOptions and both
// tenancy fields.
//
//   - SkipTenancy is emitted whenever tenancy is configured (PRD §29.4.4).
//   - Tenant is emitted only when some entity is actually tenanted, because it
//     is concretely typed from the package's uniform tenant type (§29.2.4).
//     The gate is gen.FirstTenantedEntityType — the same call the generator's
//     own emission decision reads — rather than a second predicate reproducing
//     it here, which would be the drift this function exists to remove.
//   - AllowInTransaction is unconditional, like LockMode (PRD §9.4a).
//
// Tenant therefore inherits the contexts' attached tenancy: like
// buildTenancyFeature, it reads false for a Build that ran without the
// orchestrator's attach pass (tests, dry runs, non-pipeline callers). The
// pipeline always attaches, which is what the drift guard over the emitted
// example manifests pins.
func callOptionsFieldNames(cfg *config.RootConfig, tables []gen.TableContext, views []gen.ViewContext) []string {
	fields := []string{"SkipCache", "SkipEvents", "SkipHooks"}
	if tenancyEnabled(cfg) {
		fields = append(fields, "SkipTenancy")
		if tenantGoType, _ := gen.FirstTenantedEntityType(tables, views); tenantGoType != "" {
			fields = append(fields, "Tenant")
		}
	}
	return append(fields, "FieldOptions", "LockMode", "AllowInTransaction")
}

// --- conventions block (PRD §30.4.1) ---

// buildConventions assembles the package-wide reference block.
//
// error_sentinels names the sentinels that actually exist: the ten PRD §22.1
// values re-exported into the generated package by error.go.tmpl, plus the two
// runtime tenancy sentinels when tenancy is configured. Earlier revisions
// listed ErrConflict / ErrBadReference / ErrInvalidInput / ErrForbidden, which
// are not Go identifiers anywhere in the project — they are the GraphQL
// extensions.code values of PRD §26.5.5, PascalCased into a name column.
// The constraint half of that mapping is preserved structurally in
// ConstraintErrorCodes rather than as fictional sentinels, because the four
// codes share one *database.ConstraintError discriminated by Type (§22.2).
func buildConventions(cfg *config.RootConfig, tables []gen.TableContext, views []gen.ViewContext) Conventions {
	pkg := cfg.Output.Package
	// Declaration order of PRD §22.1 / error.go.tmpl. Three have a 1:1 GraphQL
	// code — ErrNotFound and the two nested-mutation sentinels, which
	// mapErrorToGQL matches with their own errors.Is arms (§26.5.5); the rest
	// fall through §26.5.5's "Other" row or, for ErrConstraintViolation, map
	// 1:N via ConstraintErrorCodes. ErrRefreshConcurrentlyInTx is deliberately
	// absent: it is reached through the runtime database package and is not
	// re-exported into the generated one, which is the boundary §22.1 draws.
	//
	// The nested sentinels are advertised whether or not nested mutations are
	// enabled, matching the re-export they mirror — error.go.tmpl declares them
	// unconditionally, and TestErrorSentinelsMatchGeneratedPackage compares
	// this list against that file.
	sentinels := []ErrorSentinel{
		{Name: "ErrNotFound", GraphQLCode: "NOT_FOUND", Package: pkg},
		{Name: "ErrEmptyFilter", Package: pkg},
		{Name: "ErrNilInput", Package: pkg},
		{Name: "ErrAmbiguousFilter", Package: pkg},
		{Name: "ErrInvalidCursor", Package: pkg},
		{Name: "ErrDeadlock", Package: pkg},
		{Name: "ErrConnectionFailed", Package: pkg},
		{Name: "ErrConstraintViolation", Package: pkg},
		{Name: "ErrAlreadyRelated", GraphQLCode: "CONFLICT", Package: pkg},
		{Name: "ErrNestedVerbConflict", GraphQLCode: "INVALID_INPUT", Package: pkg},
	}
	// The tenancy sentinels live in the runtime tenancy package, not the
	// generated one, and are advertised only when tenancy is configured — a
	// package with tenancy off can never return them.
	if tenancyEnabled(cfg) {
		sentinels = append(
			sentinels,
			ErrorSentinel{Name: "ErrMissing", GraphQLCode: "UNAUTHENTICATED", Package: "tenancy"},
			ErrorSentinel{Name: "ErrMismatch", GraphQLCode: "FORBIDDEN", Package: "tenancy"},
		)
	}
	return Conventions{
		// One accessor per entity carries both halves — there is no c.Q / c.M
		// split (PRD §20). Tables are reached by the plural struct name, views
		// by the singular one. Both keys hold the same pattern because the
		// schema types them separately, not because the surface does.
		ClientEntryPoints: ClientEntryPoints{
			Query:    clientAccessorPattern,
			Mutation: clientAccessorPattern,
		},
		ErrorSentinels: sentinels,
		// Keyed by database.ConstraintType (§22.2); values are the §26.5.5
		// extensions.code each Type maps to. Literals rather than the
		// database package's constants — the builder does not import the
		// runtime (guidelines/ARCHITECTURE.md).
		ConstraintErrorCodes: map[string]string{
			"unique":      "CONFLICT",
			"foreign_key": "BAD_REFERENCE",
			"check":       "INVALID_INPUT",
			"not_null":    "INVALID_INPUT",
		},
		// Get returns ErrNotFound, not (nil, nil), on a missing row — it is a
		// thin wrapper over GetMany that adds exactly that (PRD §10). The
		// field read true while methods[].errors omitted the sentinel; both
		// halves of that contradiction are corrected together.
		FindReturnsNilOnMissing: false,
		Pagination: PaginationConvention{
			PageType:           "Page",
			ListEnvelopeSuffix: "List",
			CursorEncoding:     "opaque-base64",
		},
		CallOptions: CallOptionsConvention{
			Type:   "CallOptions",
			Fields: callOptionsFieldNames(cfg, tables, views),
		},
		SoftDelete: SoftDeleteConvention{
			DefaultExcludedFromFinds: true,
			IncludeVia:               "set <SoftDeleteColumn> comparator on filter",
			HardDeleteMethodSuffix:   "HardDelete",
			RestoreMethodSuffix:      "Restore",
		},
		Comparator: ComparatorConvention{
			Package:  "comparator",
			Families: []string{"Bool", "Enum", "ID", "JSON", "JSONB", "Number", "Slice", "String", "Time"},
			ShapeNotes: []string{
				"All families share: Eq, Neq, In, NotIn, IsNull, IsNotNull",
				"Numeric families (Number, Time) add: Gt, Gte, Lt, Lte",
				"String adds: Like, ILike, NotLike, NotILike",
				"Slice (JSON columns, PostgreSQL arrays) adds: Contains, ContainedBy, Overlap",
			},
			Composition: map[string]string{
				"and":     "Filter.And: []*Filter — predicates AND'd together",
				"or":      "Filter.Or:  []*Filter — entries OR'd together; each entry's own fields are AND'd, so a union is one entry per branch",
				"nesting": "And and Or nest arbitrarily; default is AND across top-level fields",
			},
			// Canonical snippets mirror the runtime comparator surface so
			// consumers copy-paste valid Go. Coverage spans the patterns a
			// caller actually reaches for: the bind pattern (a comparator is a
			// field on the generated <Entity>Filter), a string convenience op,
			// generic-family instantiation with the Range helper, a time
			// comparison, an IN list, a NULL check on a Nullable* variant, and
			// filter composition. Conventions:
			//   - scalar operators are pointer fields; Go 1.26 new(v) builds
			//     the pointer.
			//   - In/Nin are plain slices; generic families need the [T] type
			//     argument (Number[int], Slice[string]).
			//   - null checks use the Null *bool field carried by the Nullable*
			//     variants only — the base families (String, ID, ...) have none.
			//   - <pkg> is a placeholder for the generated models package, whose
			//     name is config-dependent (like <Entity> elsewhere in this block).
			Examples: map[string]string{
				"field_filter":  `<pkg>.UserFilter{Email: &comparator.String{Eq: new("a@x.com")}}`,
				"text_contains": `&comparator.String{Contains: new("acme")}`,
				"numeric_range": `&comparator.Number[int]{Between: &comparator.Range[int]{Start: 18, End: 65}}`,
				"time_after":    `&comparator.Time{Gt: new(cutoff)}`,
				"id_in":         `&comparator.ID{In: []string{"u1", "u2", "u3"}}`,
				"is_null":       `&comparator.NullableString{Null: new(true)}`,
				"compound_or":   `<pkg>.UserFilter{Or: []*<pkg>.UserFilter{{Email: &comparator.String{Eq: new("a@x.com")}}, {Email: &comparator.String{Eq: new("b@x.com")}}}}`,
			},
		},
		Omittable: OmittableConvention{
			Package:      "omittable",
			Type:         "omittable.Value[T]",
			Purpose:      "Distinguishes 'not set' from 'set to zero value'. Used in <Entity>Update structs and partial-update contexts.",
			Construction: []string{"omittable.Set(value) — set to a value", "omittable.Omit[T]() — explicitly unset (zero state)"},
			Methods:      []string{"IsSet() bool", "IsZero() bool", "Get() (T, bool)", "MustGet() T"},
			JSONBehavior: "Marshals to the wrapped value when set; omitted when unset (omitzero semantics on the wrapper, via IsZero)",
		},
	}
}

// --- entity transform ---

func buildTableEntity(in BuildInput, tc gen.TableContext, layout config.Layout, includeExamples bool) Entity {
	parserTable := findParserTable(in.Schema, tc.TableName, tc.Schema)
	tCfg := findTableConfig(in.Config, tc.TableName, tc.Schema)

	cols := buildColumns(tc.Columns, parserTable, tc.FilterFields)
	rels := buildRelationships(tc, in.Tables)
	indexes := buildIndexes(parserTable, tc)
	features := buildFeatures(tc, tCfg, in.Config)

	entity := Entity{
		Kind:          "table",
		Name:          tc.StructName,
		Table:         tc.TableName,
		Schema:        tc.Schema,
		FilePrefix:    tc.SnakeName,
		Files:         tableFiles(tc, layout),
		Comment:       parserTableComment(parserTable),
		Indexes:       indexes,
		PK:            buildPK(tc),
		Features:      features,
		Columns:       cols,
		Relationships: rels,
		Methods:       buildMethods(in, tc, tCfg),
		Filter:        buildFilter(tc),
		Sort:          buildSort(tc),
	}
	if includeExamples {
		entity.Examples = &Examples{}
	}
	return entity
}

func buildViewEntity(in BuildInput, vc gen.ViewContext, layout config.Layout) Entity {
	parserView := findParserView(in.Schema, vc.ViewName, vc.Schema)

	cols := buildColumns(vc.Columns, nil, vc.FilterFields)
	for i := range cols {
		// Views surface comments via the parser as adjacent line-comments.
		// Lift them onto the column entries when present.
		if parserView != nil && i < len(parserView.Columns) {
			cols[i].Comment = parserView.Columns[i].Comment
		}
	}

	pk := PK{Kind: "none"}
	if vc.HasPK {
		kind := "single"
		if vc.CompositePK {
			kind = "composite"
		}
		pkCols := make([]PKColumn, 0, len(vc.PKColumns))
		for _, c := range vc.PKColumns {
			pkCols = append(pkCols, PKColumn{Name: c.Name, GoField: c.FieldName, GoType: c.GoType})
		}
		pk = PK{Kind: kind, Struct: vc.CompositePKStructName, Columns: pkCols}
	}

	source := ""
	if parserView != nil {
		source = parserView.SQL
	}

	return Entity{
		Kind:         "view",
		Name:         vc.StructName,
		Table:        vc.ViewName,
		Schema:       vc.Schema,
		Source:       source,
		Materialized: vc.Materialized,
		FilePrefix:   vc.SnakeName,
		Files:        viewFiles(vc, layout),
		Comment:      "",
		// A view has no indexes, but the field must serialize as [] not null:
		// schema/v1.json types indexes as an array, and a nil slice would emit
		// `"indexes": null`, failing validation (surfaced by the real-output
		// schema check). Mirrors the sibling Relationships: []Relationship{}.
		Indexes:       []Index{},
		PK:            pk,
		Features:      buildViewFeatures(in, vc),
		Columns:       cols,
		Relationships: []Relationship{},
		Methods:       buildViewMethods(in, vc),
		Filter:        buildFilter(viewAsTable(vc)),
		Sort:          buildSort(viewAsTable(vc)),
	}
}

// viewAsTable maps a ViewContext into the subset of TableContext fields the
// filter/sort builders read. Views lack relationships, soft delete, and
// operations metadata — the helpers handle the empty slices cleanly.
func viewAsTable(vc gen.ViewContext) gen.TableContext {
	return gen.TableContext{
		StructName:   vc.StructName,
		TableName:    vc.ViewName,
		Schema:       vc.Schema,
		Columns:      vc.Columns,
		FilterFields: vc.FilterFields,
	}
}

func findParserTable(s *parser.Schema, name, schema string) *parser.Table {
	for i := range s.Tables {
		if s.Tables[i].Name == name && s.Tables[i].Schema == schema {
			return &s.Tables[i]
		}
	}
	return nil
}

func findParserView(s *parser.Schema, name, schema string) *parser.View {
	for i := range s.Views {
		if s.Views[i].Name == name && s.Views[i].Schema == schema {
			return &s.Views[i]
		}
	}
	return nil
}

func parserTableComment(t *parser.Table) string {
	if t == nil {
		return ""
	}
	return t.Comment
}

// tableFiles returns the generated Go files carrying a table's body. The name
// is `gen`'s own — gen.TableFileName is the function the emission path calls,
// so the manifest reports the file that was actually written rather than
// recomputing it (PRD §30 "File naming").
//
// The layout must be read too. The stem alone is only correct under
// `output.layout: file_per_table`; under the default `single_file` every
// table's body is concatenated into models_gen.go, and naming `<snake>_gen.go`
// there sent every manifest reader — `sqlgen_get_entity` over MCP included — to
// a path that does not exist.
//
// The builder used to carry its own PascalCase splitter, which broke on every
// acronym ("UserID" → "user_i_d", "OSILayer" → "o_s_i_layer") and applied the
// schema prefix on `schema != ""` where `gen` applies it only on a cross-schema
// name collision — so a single-schema `public.assets` was reported as
// `public_asset_gen.go` against an emitted `asset_gen.go`.
func tableFiles(tc gen.TableContext, layout config.Layout) []string {
	return []string{gen.TableFileName(tc, layout)}
}

// viewFiles is tableFiles' view half — views_gen.go under the default layout,
// `<snake>_gen.go` under file_per_table.
func viewFiles(vc gen.ViewContext, layout config.Layout) []string {
	return []string{gen.ViewFileName(vc, layout)}
}

// --- columns ---

// buildColumns projects the already-resolved columns. The published
// `comparator` is read from the entity's own FilterFields rather than
// re-derived from the Go type, so it names the comparator the generated
// `<T>Filter` actually declares — nullable variant, type parameter and all —
// and is absent for exactly the columns that have no filter field.
//
// The builder used to carry its own classifier, and it had drifted a long way
// from resolveComparatorType: it reported `comparator.Time` for an `interval`
// column (really `Number[time.Duration]`), the non-nullable spelling for every
// nullable column, a bare `comparator.Slice` with no element type, and nothing
// at all for enum, `[]byte`, `net.*` and decimal columns. It also had no
// dialect, so it could not see that a postgres `jsonb` column takes `JSONB`,
// nor that a `types.JSON` column on SQLite has no comparator at all —
// publishing a comparator for a column whose `filter.fields` correctly
// omitted it. Same class as the old file-name recomputation and the enum
// `go_type` fix: a second derivation of a fact the generator already owns.
func buildColumns(cols []gen.ColumnContext, parserTable *parser.Table, filterFields []gen.FilterFieldContext) []Column {
	comparators := make(map[string]string, len(filterFields))
	for _, ff := range filterFields {
		if !ff.Filterable {
			continue
		}
		// ComparatorType is a Go field type and so carries a leading `*`
		// (see buildFilter, which publishes it verbatim). This field names a
		// family rather than declaring a field, so the pointer comes off.
		comparators[ff.ColumnName] = strings.TrimPrefix(ff.ComparatorType, "*")
	}

	out := make([]Column, 0, len(cols))
	for _, c := range cols {
		pc := parserColumn(parserTable, c.Name)
		col := Column{
			Name:       c.Name,
			GoField:    c.FieldName,
			GoType:     c.GoType,
			DBType:     c.SQLType,
			Nullable:   c.Nullable,
			PK:         c.PrimaryKey,
			Unique:     c.Unique,
			Comparator: comparators[c.Name],
			Comment:    parserColumnComment(pc),
		}
		if c.HasDefault {
			col.Default = defaultLiteral(pc, c.DefaultExpr)
			col.DefaultKind = classifyDefaultKind(col.Default)
		}
		if c.AutoIncrement {
			col.Auto = "insert"
		}
		if check := checkExpressionForColumn(parserTable, c.Name); check != "" {
			col.Check = check
		}
		// Access markers (PRD §32.3): omitted for public (the additive
		// default). Redacted derives from the role, not EventRedacted — the
		// two coincide today (§32.2) but mark independent surfaces.
		if c.ManifestVisibility != "" && c.ManifestVisibility != config.AccessPublic {
			col.Access = c.ManifestVisibility
			col.Redacted = c.ManifestVisibility == config.AccessWriteOnly || c.ManifestVisibility == config.AccessInternal
		}
		out = append(out, col)
	}
	return out
}

func parserColumn(t *parser.Table, name string) *parser.Column {
	if t == nil {
		return nil
	}
	for i := range t.Columns {
		if t.Columns[i].Name == name {
			return &t.Columns[i]
		}
	}
	return nil
}

func parserColumnComment(c *parser.Column) string {
	if c == nil {
		return ""
	}
	return c.Comment
}

// defaultLiteral prefers the parser's raw default expression so manifest
// readers see exactly what the DDL declared. Falls back to the codegen
// pipeline's resolved expression when the parser has no value (e.g. when
// the column was synthesized from config).
func defaultLiteral(pc *parser.Column, fallback string) string {
	if pc != nil && pc.Default != "" {
		return pc.Default
	}
	if fallback == "NULL" {
		return ""
	}
	return fallback
}

// classifyDefaultKind triages a raw default expression into one of `literal`,
// `function`, or `expression`. PRD §30.7: lets consumers distinguish "you must
// provide this" from "the database provides this."
func classifyDefaultKind(def string) string {
	if def == "" {
		return ""
	}
	trimmed := strings.TrimSpace(def)
	// Function calls end with `)` and contain `(` — e.g. gen_random_uuid().
	// CURRENT_TIMESTAMP is also classified as a function (callable in DDL).
	upper := strings.ToUpper(trimmed)
	if upper == "CURRENT_TIMESTAMP" || upper == "CURRENT_DATE" || upper == "CURRENT_TIME" || upper == "NOW" {
		return "function"
	}
	if strings.Contains(trimmed, "(") && strings.HasSuffix(trimmed, ")") {
		// A leading `(` may wrap either a function default — SQLite requires
		// `DEFAULT (datetime('now'))`, and `(now())` is common — or a genuine
		// parenthesized expression like `(price * quantity)`. Strip a single
		// balanced outer wrapper and re-test the unwrapped form.
		if inner, ok := stripOuterParens(trimmed); ok {
			if containsOperator(inner) {
				return "expression"
			}
			if isFunctionShape(inner) {
				return "function"
			}
			return "expression"
		}
		// Non-wrapped call — `gen_random_uuid()`, `nextval('seq')`. An
		// operator inside marks a parenthesized expression instead.
		if containsOperator(trimmed[1 : len(trimmed)-1]) {
			return "expression"
		}
		return "function"
	}
	// Quoted literals or bare numbers / booleans.
	return "literal"
}

// stripOuterParens removes a single balanced outer paren wrapper from s,
// returning the inner content. ok is false when s is not wrapped in exactly one
// balanced pair — e.g. `(a) + (b)`, where the leading `(` closes before the
// final `)`. It unwraps a single layer only and is not string-literal-aware, so
// nested wrappers (`((now()))`) or parens inside quoted text fall back to the
// non-wrapped classification path — acceptable for the default-expression forms
// real DDL declares.
func stripOuterParens(s string) (string, bool) {
	if len(s) < 2 || s[0] != '(' || s[len(s)-1] != ')' {
		return s, false
	}
	depth := 0
	for i, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 && i != len(s)-1 {
				return s, false
			}
		}
	}
	return s[1 : len(s)-1], true
}

// isFunctionShape reports whether s looks like a function call — an identifier
// followed by a parenthesized argument list, e.g. `now()` or `datetime('now')`.
func isFunctionShape(s string) bool {
	s = strings.TrimSpace(s)
	open := strings.IndexByte(s, '(')
	if open <= 0 || !strings.HasSuffix(s, ")") {
		return false
	}
	for _, r := range s[:open] {
		if r != '_' && r != '.' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func containsOperator(s string) bool {
	for _, r := range s {
		switch r {
		case '+', '-', '*', '/', '%', '<', '>', '=':
			return true
		}
	}
	return false
}

// checkExpressionForColumn flattens any multi-column CHECK constraint onto each
// participating column per PRD §30.7. Returns the raw expression text.
func checkExpressionForColumn(t *parser.Table, colName string) string {
	if t == nil {
		return ""
	}
	for _, c := range t.Constraints {
		if c.Type != parser.Check {
			continue
		}
		if slices.Contains(checkConstraintColumns(t, c), colName) {
			return c.CheckExpression
		}
	}
	return ""
}

// checkConstraintColumns resolves the columns a CHECK constraint participates
// in. The dialect parsers populate CheckExpression but never Constraint.Columns
// for CHECK-typed constraints (they cannot cheaply map the boolean expression
// back onto columns), so when Columns is empty we derive the set by matching
// identifier tokens in the expression against the table's real column names.
// An explicit Columns list — set by tests or a future parser — takes priority.
func checkConstraintColumns(t *parser.Table, c parser.Constraint) []string {
	if len(c.Columns) > 0 {
		return c.Columns
	}
	return columnsInExpression(c.CheckExpression, t.Columns)
}

// columnsInExpression returns the subset of cols whose names appear as
// identifier tokens in expr. It skips single-quoted string literals so a column
// name embedded in a literal (e.g. status IN ('price')) does not falsely match,
// and recognises double-quoted / backtick-quoted identifiers. Dot qualifiers
// (order_items.unit_price) are handled naturally: each dotted segment is a
// separate token, so unit_price matches while the table qualifier does not
// unless a table happens to share a column's name — an acceptable edge case for
// the DDL shapes real schemas declare.
func columnsInExpression(expr string, cols []parser.Column) []string {
	if expr == "" {
		return nil
	}
	names := make(map[string]struct{}, len(cols))
	for _, col := range cols {
		names[col.Name] = struct{}{}
	}
	var out []string
	seen := make(map[string]struct{})
	for _, tok := range expressionIdentifiers(expr) {
		if _, ok := names[tok]; !ok {
			continue
		}
		if _, dup := seen[tok]; dup {
			continue
		}
		seen[tok] = struct{}{}
		out = append(out, tok)
	}
	return out
}

// expressionIdentifiers tokenises a CHECK expression into the bare and quoted
// identifiers it references, skipping the contents of single-quoted string
// literals (with doubled ” escapes). It is deliberately lightweight — enough to
// map CHECK expressions onto columns, not a full SQL grammar.
func expressionIdentifiers(expr string) []string {
	var out []string
	runes := []rune(expr)
	for i := 0; i < len(runes); {
		switch r := runes[i]; {
		case r == '\'':
			i = skipStringLiteral(runes, i)
		case r == '"' || r == '`':
			var ident string
			ident, i = readQuotedIdent(runes, i)
			if ident != "" {
				out = append(out, ident)
			}
		case isIdentStart(r):
			start := i
			for i < len(runes) && isIdentPart(runes[i]) {
				i++
			}
			out = append(out, string(runes[start:i]))
		default:
			i++
		}
	}
	return out
}

// skipStringLiteral advances past a single-quoted string literal starting at the
// opening quote runes[i], honouring doubled ” escapes, and returns the index
// just past the closing quote.
func skipStringLiteral(runes []rune, i int) int {
	i++ // opening quote
	for i < len(runes) {
		if runes[i] == '\'' {
			if i+1 < len(runes) && runes[i+1] == '\'' {
				i += 2
				continue
			}
			return i + 1
		}
		i++
	}
	return i
}

// readQuotedIdent reads a delimited identifier ("col" or `col`) starting at the
// opening delimiter runes[i], returning the inner text and the index just past
// the closing delimiter.
func readQuotedIdent(runes []rune, i int) (string, int) {
	quote := runes[i]
	i++
	start := i
	for i < len(runes) && runes[i] != quote {
		i++
	}
	ident := string(runes[start:i])
	if i < len(runes) {
		i++ // closing delimiter
	}
	return ident, i
}

func isIdentStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r)
}

func isIdentPart(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// --- indexes ---

func buildIndexes(t *parser.Table, tc gen.TableContext) []Index {
	if t == nil {
		return nil
	}
	out := make([]Index, 0, len(t.Constraints)+1)
	// Only a PRIMARY KEY the schema declared has a primary-key index. A key
	// asserted through primary_key.columns has none: either nothing enforces
	// it, or a UNIQUE does and is listed below under its own name (PRD §30.7
	// lists declared indexes, and §8.6 lets the override assume uniqueness).
	if len(tc.PKColumns) > 0 && tc.PKDeclaredInSchema {
		pkCols := make([]string, 0, len(tc.PKColumns))
		for _, c := range tc.PKColumns {
			pkCols = append(pkCols, c.Name)
		}
		out = append(out, Index{
			Name:    pkIndexName(t.Name),
			Columns: pkCols,
			Unique:  true,
			Method:  "btree",
		})
	}
	for _, c := range t.Constraints {
		switch c.Type {
		case parser.Unique:
			out = append(out, Index{
				Name:    indexNameOrDerived(c, t.Name),
				Columns: append([]string(nil), c.Columns...),
				Unique:  true,
				Method:  indexMethodOrDefault(c.Method),
				Where:   c.Where,
			})
		case parser.Index:
			out = append(out, Index{
				Name:    indexNameOrDerived(c, t.Name),
				Columns: append([]string(nil), c.Columns...),
				Unique:  false,
				Method:  indexMethodOrDefault(c.Method),
				Where:   c.Where,
			})
		case parser.PrimaryKey, parser.ForeignKey, parser.Check:
			// already covered (PK) or not an index
		}
	}
	// An inline column UNIQUE (`email text UNIQUE`) is recorded on the column,
	// not in t.Constraints, but the database builds an index for it all the
	// same (PRD §30.7 "All declared indexes"). It has no declared name, so it
	// takes PostgreSQL's default, as the PK index takes `<table>_pkey`.
	for _, col := range t.Columns {
		if !col.InlineUnique {
			continue
		}
		out = append(out, Index{
			Name:    inlineUniqueIndexName(t.Name, col.Name),
			Columns: []string{col.Name},
			Unique:  true,
			Method:  "btree",
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func pkIndexName(table string) string { return table + "_pkey" }

func inlineUniqueIndexName(table, column string) string { return table + "_" + column + "_key" }

func indexNameOrDerived(c parser.Constraint, table string) string {
	if c.Name != "" {
		return c.Name
	}
	return table + "_" + strings.Join(c.Columns, "_") + "_idx"
}

// indexMethodOrDefault returns the parser-declared access method when set,
// falling back to "btree" — the implicit default for all three dialects when
// no USING clause is present (PRD §30.7 examples lead with btree).
func indexMethodOrDefault(method string) string {
	if method == "" {
		return "btree"
	}
	return method
}

// --- PK ---

func buildPK(tc gen.TableContext) PK {
	if len(tc.PKColumns) == 0 {
		return PK{Kind: "none"}
	}
	pkCols := make([]PKColumn, 0, len(tc.PKColumns))
	for _, c := range tc.PKColumns {
		pkCols = append(pkCols, PKColumn{Name: c.Name, GoField: c.FieldName, GoType: c.GoType})
	}
	kind := "single"
	if tc.CompositePK {
		kind = "composite"
	}
	return PK{
		Kind:    kind,
		Struct:  tc.CompositePKStructName,
		Columns: pkCols,
	}
}

// --- features ---

func buildFeatures(tc gen.TableContext, tCfg config.TableConfig, cfg *config.RootConfig) Features {
	f := Features{}
	if tc.SoftDelete != nil {
		f.SoftDelete = &SoftDeleteFeature{
			Column: tc.SoftDelete.Column,
			Type:   string(tc.SoftDelete.Strategy),
		}
	}
	if cache := buildCacheFeature(tc, tCfg, cfg); cache != nil {
		f.Cache = cache
	}
	if events := buildEventsFeature(tc, tCfg, cfg); events != nil {
		f.Events = events
	}
	if ten := buildTenancyFeature(tc, tCfg, cfg); ten != nil {
		f.Tenancy = ten
	}
	if cols := auditColumnsForTable(tc, cfg); len(cols) > 0 {
		f.AuditColumns = cols
	}
	return f
}

func buildCacheFeature(tc gen.TableContext, tCfg config.TableConfig, cfg *config.RootConfig) *CacheFeature {
	if cfg.Cache == nil || !cfg.Cache.Enabled {
		return nil
	}
	if !config.ResolveTableCacheEnabled(tCfg, cfg.Cache) {
		return nil
	}
	ttlSeconds := 0
	if cfg.Cache.TTL != "" {
		if d := parseDurationSeconds(cfg.Cache.TTL); d > 0 {
			ttlSeconds = d
		}
	}
	hydration := "lazy"
	if cfg.Cache.Hydration != nil && cfg.Cache.Hydration.Enabled {
		hydration = "eager"
	}
	keyPattern := cacheKeyPattern(tc, cfg)
	return &CacheFeature{
		TTLSeconds:    ttlSeconds,
		Hydration:     hydration,
		KeyPattern:    keyPattern,
		InvalidatesOn: []string{"Create", "Update", "Delete", "SoftDelete", "Restore", "Increment"},
	}
}

func cacheKeyPattern(tc gen.TableContext, cfg *config.RootConfig) string {
	prefix := "sqlgen"
	if cfg.Cache != nil && cfg.Cache.KeyPrefix != "" {
		prefix = cfg.Cache.KeyPrefix
	}
	parts := []string{prefix, strings.ToLower(tc.StructName)}
	pkParts := make([]string, 0, len(tc.PKColumns))
	for _, c := range tc.PKColumns {
		pkParts = append(pkParts, "{"+c.Name+"}")
	}
	if len(pkParts) > 0 {
		parts = append(parts, strings.Join(pkParts, ":"))
	}
	return strings.Join(parts, ":")
}

func buildEventsFeature(tc gen.TableContext, tCfg config.TableConfig, cfg *config.RootConfig) *EventsFeature {
	if cfg.Events == nil || !cfg.Events.Enabled {
		return nil
	}
	if !config.ResolveTableEventsEnabled(tCfg, cfg.Events) {
		return nil
	}
	types := []string{
		tc.StructName + "Created",
		tc.StructName + "Updated",
		tc.StructName + "Deleted",
	}
	if tc.SoftDelete != nil {
		types = append(types, tc.StructName+"SoftDeleted", tc.StructName+"Restored")
	}
	sort.Strings(types)
	return &EventsFeature{
		Enabled:      true,
		Types:        types,
		PayloadShape: cfg.Output.Package + "." + tc.StructName + "Event",
	}
}

// buildTenancyFeature reads tenancy state from the resolved config + schema.
// The manifest stage does not depend on the orchestrator's
// attachTenancyToTables side-effect: we re-derive the per-entity tenancy
// signal here so Build is invocable in isolation (tests, dry runs, future
// non-pipeline callers).
func buildTenancyFeature(tc gen.TableContext, tCfg config.TableConfig, cfg *config.RootConfig) *TenancyFeature {
	if !tenancyEnabled(cfg) {
		return nil
	}
	if !config.ResolveTableTenancyEnabled(tCfg, cfg.Tenancy) {
		return nil
	}
	column := config.ResolveTableTenancyColumn(tCfg, cfg.Tenancy)
	if column == "" {
		return nil
	}
	hasColumn := false
	inPK := false
	for _, c := range tc.Columns {
		if c.Name == column {
			hasColumn = true
			break
		}
	}
	if !hasColumn {
		return nil
	}
	for _, c := range tc.PKColumns {
		if c.Name == column {
			inPK = true
			break
		}
	}
	mode := "auto-filter"
	if inPK {
		mode = "verify-match"
	}
	// Qualified Go sentinels, not GraphQL codes. These fields carry no
	// `package` sibling and the symbols live in the runtime tenancy package
	// rather than the generated one, so the qualified spelling is the only
	// unambiguous one. UNAUTHENTICATED / FORBIDDEN are the §26.5.5 codes for
	// these sentinels, not their names.
	return &TenancyFeature{
		Column:               column,
		Mode:                 mode,
		MissingResolverError: "tenancy.ErrMissing",
		MismatchError:        "tenancy.ErrMismatch",
	}
}

// buildViewFeatures assembles the per-entity feature block for a view. Every
// member but tenancy is structurally nil on a read-only entity (PRD §30.4.2):
// a view emits no events, carries no soft-delete column, has no audit columns,
// and features.cache stays null pending its own spec pass — a view's
// invalidate_on names source tables where a table's invalidates_on names
// methods, and key_pattern / hydration describe a read-through path views do
// not have (§29.2.5).
func buildViewFeatures(in BuildInput, vc gen.ViewContext) Features {
	return Features{Tenancy: buildViewTenancyFeature(vc, findViewConfig(in.Config, vc.ViewName, vc.Schema), in.Config)}
}

// buildViewTenancyFeature mirrors buildTenancyFeature for a read-only entity
// (PRD §30.4.2). Two fields differ from the table shape, both
// because a view has no write path:
//
//   - Mode is always "auto-filter". "verify-match" denotes the mutation-input
//     and PK-argument check of §29.4.2 / §29.7, which has no view analogue. A
//     view whose tenant column carries an @pk annotation is still plain-
//     filtered — @pk selects a Get signature, it does not make the column a
//     DDL primary key — so this deliberately does not consult vc.PKColumns.
//   - MismatchError is left empty and omitted from the JSON. There is no
//     mutation input to mismatch against, and advertising an error the entity
//     can never return is worse than omitting the key.
//
// Like its table twin this re-derives from cfg rather than reading
// vc.Tenancy: Build runs without the orchestrator's attach pass (tests, dry
// runs, non-pipeline callers), so vc.Tenancy is nil there.
func buildViewTenancyFeature(vc gen.ViewContext, vCfg config.ViewConfig, cfg *config.RootConfig) *TenancyFeature {
	if !tenancyEnabled(cfg) {
		return nil
	}
	if !config.ResolveViewTenancyEnabled(vCfg, cfg.Tenancy) {
		return nil
	}
	column := config.ResolveViewTenancyColumn(vCfg, cfg.Tenancy)
	if column == "" {
		return nil
	}
	hasColumn := slices.ContainsFunc(vc.Columns, func(c gen.ColumnContext) bool { return c.Name == column })
	if !hasColumn {
		return nil
	}
	return &TenancyFeature{
		Column:               column,
		Mode:                 "auto-filter",
		MissingResolverError: "tenancy.ErrMissing",
	}
}

func auditColumnsForTable(tc gen.TableContext, cfg *config.RootConfig) []string {
	defaults := cfg.Generation.UpdateColumns
	if len(defaults) == 0 {
		return nil
	}
	colSet := make(map[string]struct{}, len(tc.Columns))
	for _, c := range tc.Columns {
		colSet[c.Name] = struct{}{}
	}
	out := make([]string, 0, len(defaults))
	for _, name := range defaults {
		if _, ok := colSet[name]; ok {
			out = append(out, name)
		}
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

// parseDurationSeconds parses an integer-seconds value out of a duration
// string (e.g. "30s", "1m"). Self-contained so the builder doesn't pull in
// time.ParseDuration directly — keeps stdlib import surface minimal here.
func parseDurationSeconds(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	if v, ok := strings.CutSuffix(s, "s"); ok {
		return atoi(v)
	}
	if v, ok := strings.CutSuffix(s, "m"); ok {
		return atoi(v) * 60
	}
	if v, ok := strings.CutSuffix(s, "h"); ok {
		return atoi(v) * 3600
	}
	return atoi(s)
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// --- relationships ---

// relationshipPredicate renders the edge's sub-categorization predicate for the
// manifest's `filter` field.
//
// A `discriminator:` compiles to exactly the equality predicate the equivalent
// `filter:` produced (PRD §13.4.1), so it is reported in the same field and the
// same spelling rather than as a second, structurally different key. Two
// reasons: the manifest's JSON Schema is frozen at schema_version 0.1.0 (PRD
// §30.4) and gains no field for this, and a reader of the manifest wants the
// predicate the edge applies — not which of two config spellings expressed it.
// Reporting it here is also what keeps migrating an edge between the two forms
// invisible to the manifest.
//
// Unqualified, matching how a `filter:` is reported: the manifest carries the
// predicate as the config declared it, not as any one read path qualified it.
func relationshipPredicate(r gen.RelationshipContext) string {
	if r.Discriminator != nil {
		return r.Discriminator.EquivalentFilter()
	}
	return r.Filter
}

func buildRelationships(tc gen.TableContext, allTables []gen.TableContext) []Relationship {
	out := make([]Relationship, 0, len(tc.Relationships))
	for _, r := range tc.Relationships {
		rel := Relationship{
			Name:         r.Name,
			Kind:         relationshipKind(r.Type, r.Side),
			TargetEntity: r.TargetStructName,
			Filter:       relationshipPredicate(r),
		}
		switch r.Type {
		case parser.OneToOne, parser.OneToMany:
			// FK names the table that holds the column: the target for a
			// has-one / has-many edge, this table for a belongs-to edge.
			// FKOnTarget is the one derived answer (PRD §13.1), read only for
			// O2O: an O2M edge fixes the direction by its type, so false there
			// (a config `fk:` the target does not declare) is not belongs-to
			// — see gen/nested_names.go. An unresolved target also reads
			// false, but relationship building rejects one before the
			// manifest runs (checkDeclaredTarget).
			fkTable, fkSchema := r.TargetTable, r.TargetSchema
			if r.Type == parser.OneToOne && !r.FKOnTarget {
				fkTable, fkSchema = tc.TableName, tc.Schema
			}
			rel.FK = &FK{
				Table:  fkTable,
				Schema: fkSchema,
				Column: r.FKColumn,
			}
		case parser.ManyToMany:
			rel.Junction = &Junction{
				Table:    r.JunctionTable,
				Schema:   r.JunctionSchema,
				LocalFK:  r.JunctionLocalFK,
				TargetFK: r.JunctionReferenceFK,
			}
		}
		out = append(out, rel)
	}
	_ = allTables
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func relationshipKind(t parser.RelationshipType, side parser.RelationshipSide) string {
	switch t {
	case parser.OneToOne:
		// O2O on the parent side carries the FK; m2o on the child side. The
		// codegen marks both as OneToOne — Side is the authoritative
		// disambiguator. Every production emit path (addO2O,
		// configRelationshipToContext, relationshipToContext) populates Side,
		// so SideUnspecified here means a caller fed a hand-built fixture
		// through the manifest pipeline without going through the gen
		// builders — surface that as empty rather than guessing.
		switch side {
		case parser.SideParent:
			return "o2o"
		case parser.SideChild:
			return "m2o"
		case parser.SideUnspecified:
			return ""
		}
		return ""
	case parser.OneToMany:
		return "o2m"
	case parser.ManyToMany:
		return "m2m"
	}
	return ""
}

// --- methods ---

// methodCtx bundles the per-table inputs every method-builder reads. Defined
// to keep cyclop happy on the orchestrating buildMethods and to centralize the
// PK-shape branch the by-PK methods share.
type methodCtx struct {
	in          BuildInput
	tc          gen.TableContext
	dialect     string
	entity      string // <Entity> — the generated struct name
	plural      string // the plural form the generated input types are named for
	pkParamName string
	pkParamType string
	tenancy     tenancyReach
}

// tenancyReach records which tenancy sentinels a tenanted entity's methods can
// actually return, so methods[].errors advertises neither more nor less than
// the templates emit.
type tenancyReach struct {
	// tenanted is true when the entity carries the effective tenant column.
	tenanted bool
	// missing is true when tenancy.required resolves true: resolveTenant then
	// returns tenancy.ErrMissing on an absent resolver (§29.2.3). Every
	// tenanted method calls resolveTenant, reads included, so all of them can
	// surface it.
	missing bool
	// inPK is true when the tenant column is one of the entity's PK columns.
	// UpdateWhere is the one method whose ErrMismatch check is gated on its
	// negation — table/update.go.tmpl:719 skips the input-side comparison when
	// the tenant lives in the key, because it is not on UpdateInput there.
	inPK bool
	// pkVerify is true for the §29.7 tenant-in-composite-PK shape, the only
	// one where a by-PK method can mismatch — table/delete.go.tmpl,
	// table/get.go.tmpl, table/exists.go.tmpl and table/increment.go.tmpl all
	// guard their ErrMismatch on `and .Tenancy.InPrimaryKey .CompositePK`.
	pkVerify bool
}

// tenancyReachForTable mirrors buildTenancyFeature's detection — same guards,
// same column resolution — and adds the three facts the feature block does not
// need: whether required resolves true, whether the tenant column sits in the
// PK, and whether that PK is composite.
func tenancyReachForTable(tc gen.TableContext, tCfg config.TableConfig, cfg *config.RootConfig) tenancyReach {
	if !tenancyEnabled(cfg) || !config.ResolveTableTenancyEnabled(tCfg, cfg.Tenancy) {
		return tenancyReach{}
	}
	column := config.ResolveTableTenancyColumn(tCfg, cfg.Tenancy)
	if column == "" || !slices.ContainsFunc(tc.Columns, func(c gen.ColumnContext) bool { return c.Name == column }) {
		return tenancyReach{}
	}
	inPK := slices.ContainsFunc(tc.PKColumns, func(c gen.ColumnContext) bool { return c.Name == column })
	return tenancyReach{
		tenanted: true,
		missing:  config.ResolveTableTenancyRequired(tCfg, cfg.Tenancy),
		inPK:     inPK,
		pkVerify: inPK && tc.CompositePK,
	}
}

// tenancyReachForView is tenancyReachForTable's read-only twin. It mirrors
// buildViewTenancyFeature's guards exactly — same resolvers, same
// projected-column check — and adds the one extra fact the feature block does
// not need: whether `required` resolves true, which is what makes
// tenancy.ErrMissing reachable from a view read (PRD §29.3.1).
//
// inPK and pkVerify stay false by construction. Both drive the §29.4.2 / §29.7
// verify-match, which is a mutation-input rule with no read-only analogue — a
// view's @pk annotation selects a Get signature, it does not make the column a
// DDL primary key (§30.4.2). So every view method takes the mismatch=false
// path, and a view can never advertise tenancy.ErrMismatch.
func tenancyReachForView(vc gen.ViewContext, vCfg config.ViewConfig, cfg *config.RootConfig) tenancyReach {
	if !tenancyEnabled(cfg) || !config.ResolveViewTenancyEnabled(vCfg, cfg.Tenancy) {
		return tenancyReach{}
	}
	column := config.ResolveViewTenancyColumn(vCfg, cfg.Tenancy)
	if column == "" || !slices.ContainsFunc(vc.Columns, func(c gen.ColumnContext) bool { return c.Name == column }) {
		return tenancyReach{}
	}
	return tenancyReach{
		tenanted: true,
		missing:  config.ResolveViewTenancyRequired(vCfg, cfg.Tenancy),
	}
}

// tenancyErrors returns the tenancy sentinels a tenanted method can return,
// qualified because they live in the runtime tenancy package rather than the
// generated one. Every tenanted method resolves the tenant, so ErrMissing
// rides on `required` alone; mismatch differs per method shape and is the
// caller's to decide — see the three helpers on methodCtx below.
func tenancyErrors(t tenancyReach, mismatch bool) []string {
	if !t.tenanted {
		return nil
	}
	var out []string
	if t.missing {
		out = append(out, "tenancy.ErrMissing")
	}
	if mismatch {
		out = append(out, "tenancy.ErrMismatch")
	}
	return out
}

// errs normalizes a method's error list. The manifest schema types errors as
// an array, so a method that can return no sentinel carries [] rather than
// null — and the variadic form keeps the per-method call sites free of
// append-onto-a-literal noise.
func errs(lists ...[]string) []string {
	out := []string{}
	for _, l := range lists {
		out = append(out, l...)
	}
	return out
}

// inputTenancy covers the methods that carry a mutation input naming the
// tenant: Create, CreateMany, Update, UpdateMany and Upsert compare the
// caller-supplied tenant against the resolved one in both the tenant-in-PK and
// auto-filter shapes, so ErrMismatch is always reachable.
func (ctx methodCtx) inputTenancy() []string { return tenancyErrors(ctx.tenancy, true) }

// pkTenancy covers the by-PK and batch-of-PK methods, whose only ErrMismatch
// is the §29.7 verify-match against a tenant component of a composite PK.
func (ctx methodCtx) pkTenancy() []string { return tenancyErrors(ctx.tenancy, ctx.tenancy.pkVerify) }

// filterTenancy covers the methods that scope by filter alone and compare
// nothing: they resolve the tenant, append it as a condition, and return.
func (ctx methodCtx) filterTenancy() []string { return tenancyErrors(ctx.tenancy, false) }

// updateWhereTenancy is UpdateWhere's shape: it carries an UpdateInput and so
// compares, but only when the tenant column is not part of the key — where it
// is, the column is excluded from UpdateInput and there is nothing to compare.
func (ctx methodCtx) updateWhereTenancy() []string {
	return tenancyErrors(ctx.tenancy, !ctx.tenancy.inPK)
}

// notFoundOnMissingPK reports whether a by-PK write advertises ErrNotFound.
// Update and Increment return it only under strict_updates; with the flag off
// both are idempotent (PRD §9.5).
func (ctx methodCtx) notFoundOnMissingPK() []string {
	if ctx.tc.StrictUpdates {
		return []string{"ErrNotFound"}
	}
	return nil
}

func buildMethods(in BuildInput, tc gen.TableContext, tCfg config.TableConfig) Methods {
	ctx := methodCtx{
		in:          in,
		tc:          tc,
		dialect:     string(in.Config.Input.Dialect),
		entity:      tc.StructName,
		plural:      gen.StructNamePlural(tc.StructName),
		pkParamName: "id",
		pkParamType: pkParamTypeForTable(tc),
		tenancy:     tenancyReachForTable(tc, tCfg, in.Config),
	}
	if tc.CompositePK {
		ctx.pkParamName = "pk"
		ctx.pkParamType = tc.CompositePKStructName
	}
	return Methods{
		Query:    buildQueryMethods(ctx),
		Mutation: buildMutationMethods(ctx),
	}
}

// body wraps one dialect-keyed SQL body. Methods that issue no statement of
// their own pass nil and carry no sql_bodies key at all.
func (ctx methodCtx) body(sql string) map[string]string {
	return map[string]string{ctx.dialect: sql}
}

// pkParam is the leading argument of every by-PK method — `id <GoType>` for a
// single-column key, `pk <Entity>PK` for a composite one.
func (ctx methodCtx) pkParam() MethodParam {
	return MethodParam{Name: ctx.pkParamName, Type: ctx.pkParamType}
}

// pkListParam is its batch counterpart on the *Many methods.
func (ctx methodCtx) pkListParam() MethodParam {
	if ctx.tc.CompositePK {
		return MethodParam{Name: "pks", Type: "[]" + ctx.pkParamType}
	}
	return MethodParam{Name: "ids", Type: "[]" + ctx.pkParamType}
}

func (ctx methodCtx) filterParam() MethodParam {
	return MethodParam{Name: "filter", Type: "*" + ctx.entity + "Filter"}
}

// buildQueryMethods lists the read half of the generated <Entity>Client
// interface (PRD §9.1, §9.4, §9.4a). Get, GetMany and Count are unconditional —
// toResolvedOperations pins all three true because the rest of the surface is
// built on them; everything else follows its resolved flag, which is a schema
// fact (PRD §4.6).
//
// Earlier revisions advertised FindByID / FindBy<Col> / List / Walk, names
// taken from a documentation example rather than from the templates. None of
// them is generated anywhere.
func buildQueryMethods(ctx methodCtx) []Method {
	out := []Method{
		{
			Name:      "Get",
			Params:    []MethodParam{ctx.pkParam()},
			Returns:   "*" + ctx.entity,
			Errors:    errs([]string{"ErrNotFound"}, ctx.pkTenancy()),
			Notes:     "Delegates to GetMany with a primary-key filter; returns ErrNotFound when no row matches.",
			SQLBodies: ctx.body(sqlGetByPK(ctx.in.Config, ctx.tc)),
		},
		{
			Name:      "GetMany",
			Params:    []MethodParam{{Name: "input", Type: "*Get" + ctx.plural + "Input"}},
			Returns:   "[]*" + ctx.entity,
			Errors:    errs(ctx.filterTenancy()),
			SQLBodies: ctx.body(sqlGetMany(ctx.in.Config, ctx.tc)),
		},
		{
			Name:      "Count",
			Params:    []MethodParam{ctx.filterParam()},
			Returns:   "int64",
			Errors:    errs(ctx.filterTenancy()),
			SQLBodies: ctx.body(sqlCount(ctx.in.Config, ctx.tc)),
		},
	}
	if ctx.tc.Operations.Exists {
		out = append(
			out,
			Method{
				Name:      "Exists",
				Params:    []MethodParam{ctx.pkParam()},
				Returns:   "bool",
				Errors:    errs(ctx.pkTenancy()),
				SQLBodies: ctx.body(sqlExistsByPK(ctx.in.Config, ctx.tc)),
			},
			Method{
				Name:      "ExistsWhere",
				Params:    []MethodParam{ctx.filterParam()},
				Returns:   "bool",
				Errors:    errs(ctx.filterTenancy()),
				SQLBodies: ctx.body(sqlExistsWhere(ctx.in.Config, ctx.tc)),
			},
		)
	}
	if ctx.tc.Operations.Paginate {
		out = append(out, Method{
			Name:    "Paginate",
			Params:  []MethodParam{{Name: "input", Type: "PaginateInput[" + ctx.entity + "Filter]"}},
			Returns: "*PaginateResult[" + ctx.entity + "]",
			Errors:  errs(ctx.filterTenancy()),
			Notes:   "Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.",
		})
	}
	if ctx.tc.Operations.Connection {
		out = append(out, Method{
			Name:    "Connection",
			Params:  []MethodParam{{Name: "input", Type: "ConnectionInput[" + ctx.entity + "Filter]"}},
			Returns: "*Connection[" + ctx.entity + "]",
			Errors:  errs([]string{"ErrInvalidCursor"}, ctx.filterTenancy()),
			Notes:   "Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.",
		})
	}
	if ctx.tc.Operations.Stream {
		out = append(out, Method{
			Name:      "Stream",
			Params:    []MethodParam{{Name: "input", Type: "*Stream" + ctx.plural + "Input"}},
			Returns:   "iter.Seq2[*" + ctx.entity + ", error]",
			Errors:    errs(ctx.filterTenancy()),
			Notes:     "Scalars only — no relationship loading, cache always bypassed. Errors surface through the iterator's second value.",
			SQLBodies: ctx.body(sqlStream(ctx.in.Config, ctx.tc)),
		})
	}
	return out
}

// buildMutationMethods lists the write half of the generated <Entity>Client
// interface (PRD §9.2, §9.3), each gated on the same resolved flag as its
// template — a schema fact (PRD §4.6).
//
// Constraint failures are one structured *database.ConstraintError unwrapping
// to ErrConstraintViolation (§22.2) — earlier revisions split it into
// ErrConflict / ErrBadReference / ErrInvalidInput, which are §26.5.5 GraphQL
// codes rather than Go identifiers and resolve nowhere. The per-Type
// code mapping lives in conventions.constraint_error_codes.
//
// ErrNotFound appears only where the generated body returns it: on Update and
// Increment under strict_updates. The by-PK deletes and Restore are idempotent
// by §9.5 and return nil for a missing key, so they advertise nothing.
func buildMutationMethods(ctx methodCtx) []Method {
	out := buildCreateMethods(ctx)
	out = append(out, buildUpdateMethods(ctx)...)
	out = append(out, buildDeleteMethods(ctx)...)
	out = append(out, buildNestedMethods(ctx)...)
	return out
}

// buildNestedMethods lists the three `…WithRelated` methods (PRD §9.9), each
// gated on the same NestedContext flag its template reads — so a family the
// emitter skipped cannot appear here, and one it emitted cannot go missing.
//
// **None of them carries a `sql_bodies` key**, and that is the existing shape
// rather than a new one. A nested method issues no statement of its own: it
// composes its base operation and, per edge, the target's and junction's own
// client methods (§9.9.6). PRD §30.4.2 already fixes what the manifest does
// with such a method — "a method that issues no statement of its own carries no
// sql_bodies key at all rather than a plausible-looking invention" — which is
// why Paginate and Connection carry none either, and why MCP §31 answers
// sqlgen_show_sql for them with -32005 METHOD_SQL_UNAVAILABLE. Listing the
// inner statements instead would publish SQL this method's own body does not
// contain, keyed under its name; inventing a field to name the composed methods
// would change the JSON schema (§30.5) to say what `notes` already says.
//
// What an agent cannot get anywhere else is *which edges* the call reaches, so
// the note names them. They are read off the resolved nested surface rather
// than from the relationship list: the two differ, and the difference is the
// whole of §9.9.4.
func buildNestedMethods(ctx methodCtx) []Method {
	nested := ctx.tc.Nested
	if nested == nil {
		return nil
	}
	edges := make([]string, 0, len(nested.Edges))
	for _, e := range nested.Edges {
		edges = append(edges, e.FieldName)
	}
	note := func(base string) string {
		return "Writes the parent and its nested rows in one transaction — a SAVEPOINT when ctx already holds one. " +
			"Issues no statement of its own: it composes " + base +
			" and, per eligible relationship (" + strings.Join(edges, ", ") + "), the target's and junction's own client methods, " +
			"so no sql_bodies entry. Each inner call fires its own hooks, events and cache invalidation under its own op; the nested method has none."
	}
	errsNested := func(parentNotFound bool) []string {
		return errs(nestedSentinels(nested, parentNotFound), ctx.inputTenancy())
	}

	var out []Method
	if nested.EmitCreate {
		out = append(out, Method{
			Name:    "CreateWithRelated",
			Params:  []MethodParam{{Name: "input", Type: "*" + nested.CreateInputName}},
			Returns: "*" + ctx.entity,
			Errors:  errsNested(false),
			Notes:   note("Create"),
		})
	}
	if nested.EmitUpdate {
		out = append(out, Method{
			Name:    "UpdateWithRelated",
			Params:  []MethodParam{ctx.pkParam(), {Name: "input", Type: "*" + nested.UpdateInputName}},
			Returns: "*" + ctx.entity,
			// The parent write's own ErrNotFound on a missing key, under
			// strict_updates.
			Errors: errsNested(len(ctx.notFoundOnMissingPK()) > 0),
			Notes:  note("Update"),
		})
	}
	if nested.EmitUpsert {
		out = append(out, Method{
			Name: "UpsertWithRelated",
			Params: []MethodParam{
				{Name: "input", Type: "*" + nested.UpsertInputName},
				{Name: "target", Type: ctx.entity + "ConflictTarget"},
			},
			Returns: "*" + ctx.entity,
			Errors:  errsNested(false),
			Notes:   note("Upsert"),
		})
	}
	return out
}

// nestedSentinels lists the generated-package sentinels a `…WithRelated` body
// can actually reach on this entity.
//
// errors[] publishes what the body reaches, not what the feature can produce
// somewhere (§30.4.2), so the two §9.9.8 sentinels are read off this
// parent's own verb sets: a parent whose every edge is create-only can return
// neither. parentNotFound carries the base operation's own ErrNotFound —
// Update's missing key under strict_updates — so the sentinel is listed once
// however many ways it is reachable.
func nestedSentinels(nested *gen.NestedContext, parentNotFound bool) []string {
	notFound, alreadyRelated, verbConflict := parentNotFound, false, false
	for _, e := range nested.Edges {
		// A visibility miss is ErrNotFound on every shape (§9.9.8), and only
		// `connect` performs that read.
		notFound = notFound || e.HasConnect
		// ErrAlreadyRelated is the O2M / has-one middle outcome. An M2M
		// `connect` adds a link rather than moving one, and an `allow_reparent`
		// edge adopts instead of reporting (§9.9.6).
		alreadyRelated = alreadyRelated || (e.HasConnect && e.Shape != "m2m" && !e.AllowReparent)
		// A two-verb conflict needs `disconnect` to name one target twice;
		// the self-reference rule fires on a self-referential edge under
		// either linking verb.
		verbConflict = verbConflict || e.HasDisconnect || (e.SelfReferential && e.HasConnect)
	}
	out := []string{"ErrNilInput"}
	if notFound {
		out = append(out, "ErrNotFound")
	}
	if alreadyRelated {
		out = append(out, "ErrAlreadyRelated")
	}
	if verbConflict {
		out = append(out, "ErrNestedVerbConflict")
	}
	return append(out, "ErrConstraintViolation")
}

func buildCreateMethods(ctx methodCtx) []Method {
	var out []Method
	createInput := "*Create" + ctx.entity + "Input"
	if ctx.tc.Operations.Create {
		out = append(out, Method{
			Name:      "Create",
			Params:    []MethodParam{{Name: "input", Type: createInput}},
			Returns:   "*" + ctx.entity,
			Errors:    errs([]string{"ErrNilInput", "ErrConstraintViolation"}, ctx.inputTenancy()),
			SQLBodies: ctx.body(sqlCreate(ctx.in.Config, ctx.tc)),
		})
	}
	if ctx.tc.Operations.CreateMany {
		out = append(out, Method{
			Name:      "CreateMany",
			Params:    []MethodParam{{Name: "inputs", Type: "[]" + createInput}},
			Returns:   "[]*" + ctx.entity,
			Errors:    errs([]string{"ErrNilInput", "ErrConstraintViolation"}, ctx.inputTenancy()),
			Notes:     "Batched in generation.batch_size chunks. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.",
			SQLBodies: ctx.body(sqlCreateMany(ctx.in.Config, ctx.tc)),
		})
	}
	if ctx.tc.Operations.Upsert {
		out = append(out, Method{
			Name: "Upsert",
			Params: []MethodParam{
				{Name: "input", Type: createInput},
				{Name: "target", Type: ctx.entity + "ConflictTarget"},
			},
			Returns:   "*" + ctx.entity,
			Errors:    errs([]string{"ErrNilInput", "ErrConstraintViolation"}, ctx.inputTenancy()),
			Notes:     "One method over the generated " + ctx.entity + "ConflictTarget enum — the target argument selects the conflict columns at call time.",
			SQLBodies: ctx.body(sqlUpsert(ctx.in.Config, ctx.tc)),
		})
	}
	if ctx.tc.Operations.UpsertMany {
		out = append(out, Method{
			Name: "UpsertMany",
			Params: []MethodParam{
				{Name: "inputs", Type: "[]" + createInput},
				{Name: "target", Type: ctx.entity + "ConflictTarget"},
			},
			Returns:   "[]*" + ctx.entity,
			Errors:    errs([]string{"ErrNilInput", "ErrConstraintViolation"}, ctx.inputTenancy()),
			Notes:     "Batched in generation.batch_size chunks over the same " + ctx.entity + "ConflictTarget enum Upsert takes. Inputs resolving to one target are deduped before the statement is built, last occurrence wins. Earlier chunks are not rolled back on a later failure — wrap in a transaction for atomicity.",
			SQLBodies: ctx.body(sqlUpsertMany(ctx.in.Config, ctx.tc)),
		})
	}
	return out
}

func buildUpdateMethods(ctx methodCtx) []Method {
	var out []Method
	updateInput := "*Update" + ctx.entity + "Input"
	if ctx.tc.Operations.Update {
		out = append(out, Method{
			Name:      "Update",
			Params:    []MethodParam{ctx.pkParam(), {Name: "input", Type: updateInput}},
			Returns:   "*" + ctx.entity,
			Errors:    errs(ctx.notFoundOnMissingPK(), []string{"ErrNilInput", "ErrConstraintViolation"}, ctx.inputTenancy()),
			Notes:     "Only fields where IsSet() reports true reach the SET clause; an input with none set issues no UPDATE and returns the row unchanged.",
			SQLBodies: ctx.body(sqlUpdateByPK(ctx.in.Config, ctx.tc)),
		})
	}
	if ctx.tc.Operations.UpdateMany {
		out = append(out, Method{
			Name:    "UpdateMany",
			Params:  []MethodParam{{Name: "items", Type: "[]Update" + ctx.entity + "Item"}},
			Returns: "[]*" + ctx.entity,
			// No ErrNotFound: table/update.go.tmpl guards it inside Update
			// alone (lines 177/192). UpdateMany issues the same statement per
			// item but never inspects RowsAffected, so a missing key is silent
			// there even under strict_updates.
			Errors:    errs([]string{"ErrNilInput", "ErrConstraintViolation"}, ctx.inputTenancy()),
			Notes:     "Issues one statement per item. Batched like CreateMany — earlier items are not rolled back on a later failure. A primary key that does not exist is silently skipped, even under strict_updates.",
			SQLBodies: ctx.body(sqlUpdateByPK(ctx.in.Config, ctx.tc)),
		})
	}
	if ctx.tc.Operations.UpdateWhere {
		out = append(out, Method{
			Name:      "UpdateWhere",
			Params:    []MethodParam{ctx.filterParam(), {Name: "input", Type: updateInput}},
			Returns:   "[]*" + ctx.entity,
			Errors:    errs([]string{"ErrNilInput", "ErrEmptyFilter", "ErrConstraintViolation"}, ctx.updateWhereTenancy()),
			Notes:     "Idempotent — returns an empty slice when no row matches. ErrEmptyFilter when the filter produces no conditions.",
			SQLBodies: ctx.body(sqlUpdateWhere(ctx.in.Config, ctx.tc)),
		})
	}
	if ctx.tc.Operations.Increment && len(ctx.tc.IncrementColumns) > 0 {
		out = append(out, Method{
			Name:      "Increment",
			Params:    []MethodParam{ctx.pkParam(), {Name: "input", Type: "IncrementInput[" + ctx.entity + "IncrementColumn]"}},
			Returns:   "",
			Errors:    errs(ctx.notFoundOnMissingPK(), ctx.pkTenancy()),
			Notes:     "Atomic single-column arithmetic. The column is restricted to the generated " + ctx.entity + "IncrementColumn enum — numeric, and identifying nothing: neither PK, FK, nor the tenant column. A negative amount decrements.",
			SQLBodies: ctx.body(sqlIncrement(ctx.in.Config, ctx.tc)),
		})
	}
	return out
}

func buildDeleteMethods(ctx methodCtx) []Method {
	var out []Method
	if ctx.tc.Operations.SoftDelete && ctx.tc.SoftDelete != nil {
		out = append(
			out,
			ctx.idempotentByPK("SoftDelete", "*"+ctx.entity, sqlSoftDeleteByPK(ctx.in.Config, ctx.tc)),
			ctx.idempotentBatch("SoftDeleteMany", "[]*"+ctx.entity, sqlSoftDeleteMany(ctx.in.Config, ctx.tc)),
			ctx.idempotentWhere("SoftDeleteWhere", "[]*"+ctx.entity, sqlSoftDeleteWhere(ctx.in.Config, ctx.tc)),
		)
	}
	if ctx.tc.Operations.Restore && ctx.tc.SoftDelete != nil {
		out = append(
			out,
			ctx.idempotentByPK("Restore", "*"+ctx.entity, sqlRestoreByPK(ctx.in.Config, ctx.tc)),
			ctx.idempotentBatch("RestoreMany", "[]*"+ctx.entity, sqlRestoreMany(ctx.in.Config, ctx.tc)),
			ctx.idempotentWhere("RestoreWhere", "[]*"+ctx.entity, sqlRestoreWhere(ctx.in.Config, ctx.tc)),
		)
	}
	if ctx.tc.Operations.HardDelete {
		out = append(
			out,
			ctx.idempotentByPK("HardDelete", "", sqlHardDeleteByPK(ctx.in.Config, ctx.tc)),
			ctx.idempotentBatch("HardDeleteMany", "", sqlHardDeleteMany(ctx.in.Config, ctx.tc)),
			ctx.idempotentWhere("HardDeleteWhere", "", sqlHardDeleteWhere(ctx.in.Config, ctx.tc)),
		)
	}
	return out
}

// idempotentByPK builds one of the six by-PK delete/restore entries. All are
// idempotent on a missing key (§9.5), so none advertises ErrNotFound.
func (ctx methodCtx) idempotentByPK(name, returns, sql string) Method {
	return Method{
		Name:      name,
		Params:    []MethodParam{ctx.pkParam()},
		Returns:   returns,
		Errors:    errs(ctx.pkTenancy()),
		Notes:     "Idempotent — a primary key that does not exist is not an error.",
		SQLBodies: ctx.body(sql),
	}
}

func (ctx methodCtx) idempotentBatch(name, returns, sql string) Method {
	return Method{
		Name:      name,
		Params:    []MethodParam{ctx.pkListParam()},
		Returns:   returns,
		Errors:    errs(ctx.pkTenancy()),
		Notes:     "Idempotent — primary keys that do not exist are not an error.",
		SQLBodies: ctx.body(sql),
	}
}

func (ctx methodCtx) idempotentWhere(name, returns, sql string) Method {
	return Method{
		Name:      name,
		Params:    []MethodParam{ctx.filterParam()},
		Returns:   returns,
		Errors:    errs([]string{"ErrEmptyFilter"}, ctx.filterTenancy()),
		Notes:     "Idempotent — no match is not an error. ErrEmptyFilter when the filter produces no conditions.",
		SQLBodies: ctx.body(sql),
	}
}

func pascal(s string) string {
	if s == "" {
		return ""
	}
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "")
}

func pkParamTypeForTable(tc gen.TableContext) string {
	if tc.CompositePK {
		return tc.CompositePKStructName
	}
	if len(tc.PKColumns) == 1 {
		return tc.PKColumns[0].GoType
	}
	return ""
}

// buildViewMethods produces the subset of methods generated on a view (read
// surface only — no mutations). Gating mirrors the view templates: Get needs a
// @pk annotation, Connection needs resolvable cursor keys, and GetMany, Count
// and Paginate are unconditional.
func buildViewMethods(in BuildInput, vc gen.ViewContext) Methods {
	dialect := string(in.Config.Input.Dialect)
	cols := allColumnNamesFromContexts(vc.Columns)
	body := func(sql string) map[string]string { return map[string]string{dialect: sql} }

	// Every view read resolves the tenant, so under `required: true`
	// all five carry tenancy.ErrMissing (PRD §30.4.2 — errors[] lists the
	// sentinels the generated body can actually reach). Refresh /
	// RefreshConcurrently below deliberately take none: they are never scoped
	// and never resolve (§29.2.5).
	tenancy := tenancyErrors(tenancyReachForView(vc, findViewConfig(in.Config, vc.ViewName, vc.Schema), in.Config), false)

	var queries []Method
	if vc.HasPK {
		pkName, pkType := "id", ""
		if vc.CompositePK {
			pkName, pkType = "pk", vc.CompositePKStructName
		} else if len(vc.PKColumns) == 1 {
			pkType = vc.PKColumns[0].GoType
		}
		queries = append(queries, Method{
			Name:      "Get",
			Params:    []MethodParam{{Name: pkName, Type: pkType}},
			Returns:   "*" + vc.StructName,
			Errors:    errs([]string{"ErrNotFound"}, tenancy),
			Notes:     "Generated from the view's @pk annotation.",
			SQLBodies: body(sqlViewGetByPK(in.Config, vc, cols)),
		})
	}
	queries = append(
		queries,
		Method{
			Name:      "GetMany",
			Params:    []MethodParam{{Name: "input", Type: "*Get" + gen.StructNamePlural(vc.StructName) + "Input"}},
			Returns:   "[]*" + vc.StructName,
			Errors:    errs(tenancy),
			SQLBodies: body(sqlSelectView(in.Config, vc.ViewName, vc.Schema, cols, true, true)),
		},
		Method{
			Name:      "Count",
			Params:    []MethodParam{{Name: "filter", Type: "*" + vc.StructName + "Filter"}},
			Returns:   "int64",
			Errors:    errs(tenancy),
			SQLBodies: body(sqlCountView(in.Config, vc.ViewName, vc.Schema)),
		},
		Method{
			Name:    "Paginate",
			Params:  []MethodParam{{Name: "input", Type: "PaginateInput[" + vc.StructName + "Filter]"}},
			Returns: "*PaginateResult[" + vc.StructName + "]",
			Errors:  errs(tenancy),
			Notes:   "Offset pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.",
		},
	)
	if vc.HasConnection {
		queries = append(queries, Method{
			Name:    "Connection",
			Params:  []MethodParam{{Name: "input", Type: "ConnectionInput[" + vc.StructName + "Filter]"}},
			Returns: "*Connection[" + vc.StructName + "]",
			Errors:  errs([]string{"ErrInvalidCursor"}, tenancy),
			Notes:   "Relay cursor pagination. Issues no statement of its own — composes Count and GetMany, so no sql_bodies entry.",
		})
	}

	// A materialized view advertises its refresh capability (PRD §16.5,
	// §30.4.2). Refresh mutates the stored rows, so the
	// methods live on the mutation list even though the runtime routes them
	// through the query-hook chain.
	mutations := []Method{}
	if vc.Materialized {
		mutations = append(mutations, Method{
			Name:      "Refresh",
			Params:    []MethodParam{},
			Returns:   "",
			Errors:    []string{},
			Notes:     "Recomputes the materialized view (ACCESS EXCLUSIVE lock; may run inside a transaction). Invalidates the view's cache entries on success (no-op when the view is not cached).",
			SQLBodies: body(sqlRefreshView(in.Config, vc.ViewName, vc.Schema, false)),
		})
		if vc.ConcurrentlyRefreshable {
			mutations = append(mutations, Method{
				Name:    "RefreshConcurrently",
				Params:  []MethodParam{},
				Returns: "",
				// Qualified: error.go.tmpl re-exports only the eight PRD §22.1
				// sentinels, so this one resolves as database.… and not in the
				// generated package.
				Errors:    []string{"database.ErrRefreshConcurrentlyInTx"},
				Notes:     "Non-blocking refresh; requires a UNIQUE index on the view; cannot run inside a transaction. Invalidates the view's cache entries on success (no-op when the view is not cached).",
				SQLBodies: body(sqlRefreshView(in.Config, vc.ViewName, vc.Schema, true)),
			})
		}
	}
	return Methods{Query: queries, Mutation: mutations}
}

func allColumnNamesFromContexts(cols []gen.ColumnContext) []string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = c.Name
	}
	return out
}

// --- filter / sort ---

func buildFilter(tc gen.TableContext) Filter {
	fields := make([]FilterField, 0, len(tc.FilterFields)+2)
	for _, ff := range tc.FilterFields {
		if !ff.Filterable {
			continue
		}
		fields = append(fields, FilterField{
			Name: ff.FieldName,
			// ComparatorType already carries a leading `*` (see
			// FilterFieldContext / resolveComparatorType); do not double-prefix.
			Type: ff.ComparatorType,
		})
	}
	sort.SliceStable(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
	fields = append(
		fields,
		FilterField{Name: "And", Type: "[]*" + tc.StructName + "Filter"},
		FilterField{Name: "Or", Type: "[]*" + tc.StructName + "Filter"},
	)
	return Filter{Type: tc.StructName + "Filter", Fields: fields}
}

func buildSort(tc gen.TableContext) Sort {
	fields := make([]string, 0, len(tc.Columns))
	for _, c := range tc.Columns {
		fields = append(fields, c.FieldName)
	}
	sort.Strings(fields)
	return Sort{Type: tc.StructName + "Sort", Fields: fields}
}

// --- enums + extras ---

// buildEnums projects the already-resolved EnumContexts rather than
// re-deriving names from the parser schema. The published `go_type` has to be
// the identifier the models package actually emits, and three things move it
// away from a bare pascal(): singularization, the cross-schema collision
// prefix, and an `enums.<name>.struct_name` override (PRD §4.11). Deriving it
// a second time here is how the manifest came to advertise a type name no
// generated file declared.
//
// GraphQLName tracks GoType because the GraphQL enum name IS the Go type name
// (PRD §26.4) — there is no separate alias to carry.
func buildEnums(enums []gen.EnumContext) []Enum {
	out := make([]Enum, 0, len(enums))
	for _, e := range enums {
		out = append(out, Enum{
			Name:        e.GoTypeName,
			GoType:      e.GoTypeName,
			DBType:      e.Name,
			Values:      append([]string(nil), e.Values...),
			GraphQLName: e.GoTypeName,
			SliceType:   e.SliceGoTypeName,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func buildExtras(s *parser.Schema, cfg *config.RootConfig) []Extra {
	out := make([]Extra, 0, len(s.CompositeTypes)+len(s.DomainTypes)+len(cfg.Extras))
	for _, c := range s.CompositeTypes {
		fields := make([]ExtraField, 0, len(c.Attributes))
		for _, a := range c.Attributes {
			fields = append(fields, ExtraField{Name: pascal(a.Name), GoType: a.Type, JSON: a.Name})
		}
		out = append(out, Extra{Kind: "composite", Name: pascal(c.Name), GoType: pascal(c.Name), Fields: fields})
	}
	for _, d := range s.DomainTypes {
		out = append(out, Extra{Kind: "domain", Name: pascal(d.Name), GoType: pascal(d.Name), BaseType: d.BaseType})
	}
	for name := range cfg.Extras {
		out = append(out, Extra{Kind: "extra", Name: pascal(name), GoType: pascal(name), Source: "config", JSONOnly: true})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
