package gen

import (
	"bufio"
	"embed"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/template"
	"unicode"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/sql"
)

//go:embed all:templates
var templateFS embed.FS

// GenerateResult holds the result of a generation run.
type GenerateResult struct {
	// Files is the list of file paths written.
	Files []string
	// Warnings holds non-fatal notices raised while building contexts (e.g. a
	// type override that took a column out of the Increment surface). The CLI
	// prints them next to the config-validation warnings.
	Warnings []string
	// TableCount is the number of tables processed.
	TableCount int
	// ViewCount is the number of views processed.
	ViewCount int

	// Tables, Views, and API expose the in-memory contexts that produced the
	// _gen.go files so a downstream stage (e.g. the manifest builder in
	// cmd/sqlgen/manifest, which imports this package and therefore cannot be
	// called from inside Generate) can reuse them without re-parsing the schema
	// or rebuilding contexts. API is nil when API generation is disabled.
	Tables []TableContext
	Views  []ViewContext
	Enums  []EnumContext
	API    *APIContext

	// WriteDirs maps each directory this run wrote into to the directory that
	// same file would occupy in a normal run. The two are equal unless the run
	// was redirected (Options.WriteRoot), which only GenerateInto does.
	//
	// It is a lookup table, not an ordered listing: `sqlgen diff` resolves a
	// written file's working-tree counterpart with WriteDirs[filepath.Dir(f)]
	// rather than re-deriving the generator's path resolution for itself. A
	// key that is missing is the signal that a file was written somewhere the
	// generator did not declare — the exact failure this map exists to make
	// loud instead of silent.
	WriteDirs map[string]string
}

// renderAndWrite executes a named template, wraps with package/import preamble,
// formats with goimports, and writes to disk. This is the correct pipeline for
// templates that produce only a code body (no package declaration).
func renderAndWrite(tmpl *template.Template, name string, data any, pkg string, imps []string, filePath, version string) error {
	body, err := executeTemplateSafe(tmpl, name, data)
	if err != nil {
		return fmt.Errorf("executing template %s: %w", name, err)
	}

	raw := WrapWithPreamble(pkg, imps, []byte(body))
	formatted, err := Format(raw, version, filePath)
	if err != nil {
		return err
	}

	return writeFile(filePath, formatted)
}

// generationContexts holds every context the emission steps consume, built as
// one batch ahead of the first file write.
type generationContexts struct {
	enums  []EnumContext
	sets   []SetContext
	types  TypeContext
	tables []TableContext
	views  []ViewContext
	// tenancy outlives the table contexts it was attached to — the client and
	// cache hooks read it again at emission time.
	tenancy map[string]TenancyContext
	// uuid is the UUID integration this package generates values with,
	// resolved from what its columns actually bound (PRD §7.4). The event
	// hooks read it at emission time; the tables have already had it folded
	// into PKAutoGenExpr.
	uuid gotype.UUIDIntegration
}

// buildAllContexts builds every context and rejects the schema if two entities
// resolve to one package-scope name.
//
// Building them all up front is what makes the name check possible: it needs
// the whole schema in view at once — a table's file stem can collide with a
// view's, and view contexts used to be built two steps after the tables were
// already on disk. Doing it before any write also means a rejected config
// leaves nothing behind, where before a late failure stranded the enums, types,
// scaffold and table files of a run that did not finish.
func buildAllContexts(input *GenerateInput, collisions map[string]bool) (generationContexts, error) {
	schema, cfg := input.Schema, input.Config

	ctxs := generationContexts{
		enums: BuildEnumContexts(schema, collisions, cfg),
		sets:  BuildSetContexts(schema, collisions),
		types: BuildTypeContext(input, collisions),
	}

	// Tenancy metadata is computed ahead of the table contexts so per-table
	// data (column, resolved Go type, in-PK flag) can be attached directly.
	tenancyMap, tenancyWarnings, err := BuildTenancyContext(cfg, schema, input.Resolver)
	input.Warnings = append(input.Warnings, tenancyWarnings...)
	if err != nil {
		return generationContexts{}, fmt.Errorf("building tenancy context: %w", err)
	}
	ctxs.tenancy = tenancyMap

	ctxs.tables, err = BuildTableContexts(input, collisions)
	if err != nil {
		return generationContexts{}, fmt.Errorf("building table contexts: %w", err)
	}

	ctxs.views, err = BuildViewContexts(input, collisions)
	if err != nil {
		return generationContexts{}, fmt.Errorf("building view contexts: %w", err)
	}

	if err := validateResolvedPackage(cfg, ctxs); err != nil {
		return generationContexts{}, err
	}

	// Resolved after the one-library rule has passed, so there is a single
	// UUID binding to find, and before any emission, so every generating call
	// in the package spells the same library (PRD §7.4).
	ctxs.uuid = selectUUIDIntegration(cfg, ctxs)
	attachUUIDGeneration(ctxs.tables, ctxs.uuid)

	attachTenancyToTables(ctxs.tables, tenancyMap)
	attachTenancyToViews(ctxs.views, tenancyMap)

	// Deduplicate RelationshipOptionsDefs across tables so that structs like
	// OrderItemRelationshipOptions are emitted only once when multiple parent
	// tables reference the same target.
	ctxs.tables = deduplicateRelationshipOptionsDefs(ctxs.tables)

	// Relationship filter members (PRD §11.1) are wired last: each entry reads
	// the *target's* PK, soft-delete, and tenant facts, so every table context
	// must exist and tenancy must already be attached.
	if err := wireRelationshipFilters(ctxs.tables, ctxs.views); err != nil {
		return generationContexts{}, fmt.Errorf("building relationship filters: %w", err)
	}
	if err := validateDiscriminatorBindings(ctxs.tables, ctxs.views, cfg.Input.Dialect, input.Resolver); err != nil {
		return generationContexts{}, fmt.Errorf("validating relationship discriminators: %w", err)
	}

	// Nested-mutation eligibility (PRD §9.9.4) reads the *target's* primary
	// key, create input and conflict targets, so it runs after
	// every other table pass. ValidateNestedWriteEligibility answers the same
	// question for `sqlgen validate`; both run here so a generate that emits a
	// surface and a validate that reports it clean cannot disagree.
	if err := ValidateNestedWriteEligibility(ctxs.tables, cfg, input.Resolver); err != nil {
		return generationContexts{}, fmt.Errorf("validating nested mutations: %w", err)
	}
	wireNestedMutations(ctxs.tables, cfg, input.Resolver)
	finalizeTenancyWiring(ctxs.tables, ctxs.views)

	return ctxs, nil
}

// Generate runs the full code generation pipeline:
// build contexts from schema + config, load templates, execute, format, write.
func Generate(schema *parser.Schema, cfg *config.RootConfig, version string) (*GenerateResult, error) {
	return GenerateInto(schema, cfg, version, "")
}

// GenerateInto is Generate with every write redirected underneath writeRoot.
// An empty writeRoot is an ordinary run, so Generate is this function with the
// redirect switched off.
//
// Only the *destinations* move. Import paths and rendered bodies keep naming
// the configured directories, so a redirected run produces files
// byte-identical to the ones a real run would leave in the working tree. That
// property is the whole point: `sqlgen diff` generates into a temp dir and
// compares the result against the working tree, which answers "what would
// change?" only if redirecting changed nothing about what gets written.
//
// The result's WriteDirs records where each directory landed, so the caller
// pairs preview files with their real counterparts from the generator's own
// bookkeeping instead of recomputing it.
func GenerateInto(schema *parser.Schema, cfg *config.RootConfig, version, writeRoot string) (*GenerateResult, error) {
	dialect := resolveDialect(cfg.Input.Dialect)
	usePointers := cfg.Overrides.UsePointers == nil || *cfg.Overrides.UsePointers
	resolver := gotype.NewResolver(cfg.Input.Dialect, usePointers, cfg.Overrides.Types)

	collisions := computeNameCollisions(schema)
	registerSchemaTypes(resolver, schema, cfg, collisions)

	input := &GenerateInput{
		Schema:   schema,
		Config:   cfg,
		Resolver: resolver,
	}

	tmpl, err := loadTemplatesWithResolver(dialect, resolver)
	if err != nil {
		return nil, fmt.Errorf("loading templates: %w", err)
	}

	opts := buildOptions(cfg, version, writeRoot)
	writeDirs := map[string]string{opts.OutputDir: cfg.Output.Dir}

	ctxs, err := buildAllContexts(input, collisions)
	if err != nil {
		return nil, err
	}
	tableContexts, viewContexts, tenancyMap := ctxs.tables, ctxs.views, ctxs.tenancy

	// The API plan is made before step 1 writes anything, so a run it refuses
	// (checkAPISchemaOrphans) leaves the working tree as it found it.
	api, err := planAPI(input, tableContexts, viewContexts, schema, collisions, ctxs.uuid, opts)
	if err != nil {
		return nil, err
	}

	var files []string

	// 1. Enums
	enumFiles, err := generateEnums(tmpl, ctxs.enums, cfg, opts, version)
	if err != nil {
		return nil, err
	}
	files = append(files, enumFiles...)

	// 1b. Sets (MySQL SET types)
	setFiles, err := generateSets(tmpl, ctxs.sets, opts, version)
	if err != nil {
		return nil, err
	}
	files = append(files, setFiles...)

	// 2. Types (composite, domain, extra)
	typeFiles, err := generateTypes(tmpl, ctxs.types, cfg, opts, version)
	if err != nil {
		return nil, err
	}
	files = append(files, typeFiles...)

	// 3. Errors + table name constants
	scaffoldFiles, err := generateScaffold(tmpl, tableContexts, viewContexts, opts, version)
	if err != nil {
		return nil, err
	}
	files = append(files, scaffoldFiles...)

	// 4. Tables
	tableFiles, err := generateTables(tmpl, tableContexts, cfg, opts, version)
	if err != nil {
		return nil, err
	}
	files = append(files, tableFiles...)

	// 5. Sorters, pagination, connections, shared types
	supportFiles, err := generateSupport(tmpl, cfg, opts, tableContexts, viewContexts, version)
	if err != nil {
		return nil, err
	}
	files = append(files, supportFiles...)

	// 6. Views
	viewFiles, err := generateViews(tmpl, viewContexts, cfg.Output.Layout, opts, version)
	if err != nil {
		return nil, err
	}
	files = append(files, viewFiles...)

	// 7. Client + hook files (client, events, cache)
	hookFiles, err := generateClientAndHooks(tmpl, tableContexts, viewContexts, cfg, tenancyMap, ctxs.uuid, opts, version)
	if err != nil {
		return nil, err
	}
	files = append(files, hookFiles...)

	// 8. API generation (GraphQL schema + scalar marshalers).
	apiFiles, err := generateAPI(tmpl, api, cfg, opts, version)
	if err != nil {
		return nil, err
	}
	files = append(files, apiFiles...)
	maps.Copy(writeDirs, api.writeDirs)

	return &GenerateResult{
		Files:      files,
		TableCount: len(tableContexts),
		ViewCount:  len(viewContexts),
		Tables:     tableContexts,
		Views:      viewContexts,
		Enums:      ctxs.enums,
		API:        api.ctx,
		Warnings:   input.Warnings,
		WriteDirs:  writeDirs,
	}, nil
}

// generateClientAndHooks renders the unified client plus every feature-gated
// hook file. Each step knows how to no-op when its feature is disabled.
func generateClientAndHooks(tmpl *template.Template, tables []TableContext, views []ViewContext, cfg *config.RootConfig, tenancyMap map[string]TenancyContext, uuid gotype.UUIDIntegration, opts *Options, version string) ([]string, error) {
	// Cache context is precomputed first so the client template can emit WithCache.
	cacheCtx := BuildCacheContext(tables, views, cfg, opts.Package, cfg.Output.Client.Name)

	var files []string

	clientFiles, err := generateClient(tmpl, tables, views, cfg, cacheCtx, tenancyMap, opts, version)
	if err != nil {
		return nil, err
	}
	files = append(files, clientFiles...)

	eventFiles, err := generateEventHooks(tmpl, tables, cfg, uuid, opts, version)
	if err != nil {
		return nil, err
	}
	files = append(files, eventFiles...)

	cacheFiles, err := generateCache(tmpl, cacheCtx, opts, version)
	if err != nil {
		return nil, err
	}
	files = append(files, cacheFiles...)

	return files, nil
}

// generateTypes generates the types file for composite, domain, and extra types.
func generateTypes(tmpl *template.Template, typeCtx TypeContext, cfg *config.RootConfig, opts *Options, version string) ([]string, error) {
	hasTypes := len(typeCtx.Composites) > 0 || len(typeCtx.Domains) > 0 || len(typeCtx.Extras) > 0
	if !hasTypes {
		return nil, nil
	}
	typesPath := filepath.Join(opts.OutputDir, cfg.Output.Types.File)
	typeFileCtx := TypeFileContext{
		Package: cfg.Output.Types.Package,
		// The Value/Scan pair the composite and extra templates emit is the
		// only code in this file that imports anything; a file of nothing but
		// domain aliases declares the three and has them pruned. Declaring
		// them keeps goimports off its module-cache scan (// modelTemplateImports).
		Imports:    []string{"database/sql/driver", "encoding/json", "fmt"},
		Composites: typeCtx.Composites,
		Domains:    typeCtx.Domains,
		Extras:     typeCtx.Extras,
	}
	if err := renderAndWrite(tmpl, "types", typeFileCtx, typeFileCtx.Package, typeFileCtx.Imports, typesPath, version); err != nil {
		return nil, fmt.Errorf("generating types: %w", err)
	}
	return []string{typesPath}, nil
}

// generateClient generates the unified client file.
func generateClient(tmpl *template.Template, tables []TableContext, views []ViewContext, cfg *config.RootConfig, cacheCtx *CacheContext, tenancyMap map[string]TenancyContext, opts *Options, version string) ([]string, error) {
	eventsEnabled := cfg.Events != nil && cfg.Events.Enabled
	clientCtx := BuildClientContext(tables, views, opts.Package, cfg.Output.Client.Name, eventsEnabled, cacheCtx, cfg, tenancyMap)
	clientPath := filepath.Join(opts.OutputDir, cfg.Output.Client.File)
	if err := renderAndWrite(tmpl, "client", clientCtx, clientCtx.Package, clientCtx.Imports, clientPath, version); err != nil {
		return nil, fmt.Errorf("generating unified client: %w", err)
	}
	return []string{clientPath}, nil
}

// generateCache generates the cache_gen.go file when caching is enabled.
func generateCache(tmpl *template.Template, cacheCtx *CacheContext, opts *Options, version string) ([]string, error) {
	if cacheCtx == nil {
		return nil, nil
	}
	cachePath := filepath.Join(opts.OutputDir, "cache_gen.go")
	if err := renderAndWrite(tmpl, "cache", cacheCtx, cacheCtx.Package, cacheCtx.Imports, cachePath, version); err != nil {
		return nil, fmt.Errorf("generating cache: %w", err)
	}
	return []string{cachePath}, nil
}

// generateEventHooks generates the event_hooks_gen.go file when events are enabled.
func generateEventHooks(tmpl *template.Template, tables []TableContext, cfg *config.RootConfig, uuid gotype.UUIDIntegration, opts *Options, version string) ([]string, error) {
	eventCtx := BuildEventHooksContext(tables, cfg, opts.Package, cfg.Output.Client.Name, uuid)
	if eventCtx == nil {
		return nil, nil
	}
	eventPath := filepath.Join(opts.OutputDir, "event_hooks_gen.go")
	if err := renderAndWrite(tmpl, "event-hooks", eventCtx, eventCtx.Package, eventCtx.Imports, eventPath, version); err != nil {
		return nil, fmt.Errorf("generating event hooks: %w", err)
	}
	return []string{eventPath}, nil
}

// generateScaffold generates the error types and table name constant files.
func generateScaffold(tmpl *template.Template, tables []TableContext, views []ViewContext, opts *Options, version string) ([]string, error) {
	var files []string

	errPath := filepath.Join(opts.OutputDir, "errors_gen.go")
	errCtx := ErrorFileContext{
		Package: opts.Package,
		Imports: []string{"github.com/teandresmith/sqlgen/database"},
	}
	if err := renderAndWrite(tmpl, "error", errCtx, errCtx.Package, errCtx.Imports, errPath, version); err != nil {
		return nil, fmt.Errorf("generating errors: %w", err)
	}
	files = append(files, errPath)

	tableNameCtx := buildTableNameFileContext(tables, views, opts.Package)
	tableNamePath := filepath.Join(opts.OutputDir, "tablenames_gen.go")
	if err := renderAndWrite(tmpl, "tablename", tableNameCtx, tableNameCtx.Package, tableNameCtx.Imports, tableNamePath, version); err != nil {
		return nil, fmt.Errorf("generating table names: %w", err)
	}
	files = append(files, tableNamePath)

	return files, nil
}

// generateEnums generates the enums file if there are any enum types.
func generateEnums(tmpl *template.Template, enumContexts []EnumContext, cfg *config.RootConfig, opts *Options, version string) ([]string, error) {
	if len(enumContexts) == 0 {
		return nil, nil
	}
	enumPath := filepath.Join(opts.OutputDir, cfg.Output.Enums.File)
	// `io` is required by MarshalGQL on every emitted enum so the type
	// satisfies gqlgen's graphql.Marshaler interface via duck typing. The
	// import is unconditional even when API generation is off — the methods
	// only depend on stdlib and stay dormant unless a GraphQL handler calls
	// them. Tiny cost for a uniformly generated package.
	imports := []string{"database/sql/driver", "fmt", "io"}
	if cfg.Input.Dialect == config.DialectPostgres {
		imports = append(imports, "strings")
	}
	enumFileCtx := EnumFileContext{
		Package: cfg.Output.Enums.Package,
		Imports: imports,
		Enums:   enumContexts,
		Dialect: cfg.Input.Dialect,
	}
	if err := renderAndWrite(tmpl, "enum", enumFileCtx, enumFileCtx.Package, enumFileCtx.Imports, enumPath, version); err != nil {
		return nil, fmt.Errorf("generating enums: %w", err)
	}
	return []string{enumPath}, nil
}

// generateSets generates the sets file if there are any MySQL SET types.
func generateSets(tmpl *template.Template, setContexts []SetContext, opts *Options, version string) ([]string, error) {
	if len(setContexts) == 0 {
		return nil, nil
	}
	setPath := filepath.Join(opts.OutputDir, "sets_gen.go")
	// `io` is required by MarshalGQL on the value type and on the named slice,
	// which is what lets gqlgen bind a SET column as a GraphQL enum list
	// (PRD §26.4). Unconditional even when API generation is off, matching
	// generateEnums: the methods depend only on stdlib and stay dormant unless
	// a GraphQL handler calls them.
	setFileCtx := SetFileContext{
		Package: opts.Package,
		Imports: []string{"database/sql/driver", "fmt", "io", "strings"},
		Sets:    setContexts,
	}
	if err := renderAndWrite(tmpl, "set", setFileCtx, setFileCtx.Package, setFileCtx.Imports, setPath, version); err != nil {
		return nil, fmt.Errorf("generating sets: %w", err)
	}
	return []string{setPath}, nil
}

// generateSupport generates sorter, pagination, connection, and shared type files.
func generateSupport(tmpl *template.Template, cfg *config.RootConfig, opts *Options, tableContexts []TableContext, viewContexts []ViewContext, version string) ([]string, error) {
	var files []string

	sorterCtx := BuildSorterContext(opts.Package, tableContexts)
	sorterPath := filepath.Join(opts.OutputDir, "sorter_gen.go")
	if err := renderAndWrite(tmpl, "sorter", sorterCtx, sorterCtx.Package, sorterCtx.Imports, sorterPath, version); err != nil {
		return nil, fmt.Errorf("generating sorters: %w", err)
	}
	files = append(files, sorterPath)

	paginationCtx := PaginationContext{
		Package: opts.Package,
		Imports: []string{"github.com/teandresmith/sqlgen/sql"},
	}
	paginationPath := filepath.Join(opts.OutputDir, "pagination_gen.go")
	if err := renderAndWrite(tmpl, "pagination", paginationCtx, paginationCtx.Package, paginationCtx.Imports, paginationPath, version); err != nil {
		return nil, fmt.Errorf("generating pagination: %w", err)
	}
	files = append(files, paginationPath)

	connCtx := BuildConnectionContext(opts.Package)
	connPath := filepath.Join(opts.OutputDir, "connection_gen.go")
	if err := renderAndWrite(tmpl, "connection", connCtx, connCtx.Package, connCtx.Imports, connPath, version); err != nil {
		return nil, fmt.Errorf("generating connections: %w", err)
	}
	files = append(files, connPath)

	tenancyEnabled := cfg.Tenancy != nil && cfg.Tenancy.Enabled
	tenantGoType, tenantImport := FirstTenantedEntityType(tableContexts, viewContexts)
	hasFilterOptions := slices.ContainsFunc(tableContexts, func(tc TableContext) bool {
		return tc.HasFilterOptions
	})
	hasUpsertMany := slices.ContainsFunc(tableContexts, func(tc TableContext) bool {
		return tc.Operations.UpsertMany
	})
	hasNested := slices.ContainsFunc(tableContexts, func(tc TableContext) bool {
		return tc.Nested != nil
	})
	sharedCtx := BuildSharedTypesContext(opts.Package, tenancyEnabled, tenantGoType, tenantImport, hasFilterOptions, hasUpsertMany, hasNested)
	sharedPath := filepath.Join(opts.OutputDir, "shared_types_gen.go")
	if err := renderAndWrite(tmpl, "shared-types", sharedCtx, sharedCtx.Package, sharedCtx.Imports, sharedPath, version); err != nil {
		return nil, fmt.Errorf("generating shared types: %w", err)
	}
	files = append(files, sharedPath)

	return files, nil
}

// tableTemplateNames is the ordered list of templates to render for each table.
var tableTemplateNames = []string{
	"table/model",
	"table/client",
	"table/get",
	"table/create",
	"table/update",
	"table/delete",
	"table/upsert",
	"table/upsert-many",
	"table/nested",
	"table/increment",
	"table/exists",
	"table/count",
	"table/pagination",
	"table/stream",
	"shared/filter",
	"shared/field-options",
	"shared/create-input",
	"shared/update-input",
	"shared/get-input",
}

// viewTemplateNames is the ordered list of templates to render for each view.
var viewTemplateNames = []string{
	"view/model",
	"view/client",
	"view/get",
	"view/count",
	"view/pagination",
	"view/refresh",
	"shared/filter",
	"shared/field-options",
	"shared/get-input",
}

// renderTableBody renders all templates for a single table and returns the
// concatenated body text.
func renderTableBody(tmpl *template.Template, tc TableContext) (string, error) {
	var parts []string
	for _, name := range tableTemplateNames {
		out, err := executeTemplateSafe(tmpl, name, tc)
		if err != nil {
			return "", fmt.Errorf("generating %s for table %s: %w", name, tc.TableName, err)
		}
		if out != "" {
			parts = append(parts, out)
		}
	}

	if tc.HasO2ORelationships || tc.HasO2MRelationships {
		out, err := executeTemplateSafe(tmpl, "table/relationships", tc)
		if err != nil {
			return "", fmt.Errorf("generating relationships for table %s: %w", tc.TableName, err)
		}
		if out != "" {
			parts = append(parts, out)
		}
	}

	return strings.Join(parts, "\n"), nil
}

// renderViewBody renders all templates for a single view and returns the
// concatenated body text.
func renderViewBody(tmpl *template.Template, vc ViewContext) (string, error) {
	var parts []string
	for _, name := range viewTemplateNames {
		out, err := executeTemplateSafe(tmpl, name, vc)
		if err != nil {
			return "", fmt.Errorf("generating %s for view %s: %w", name, vc.ViewName, err)
		}
		if out != "" {
			parts = append(parts, out)
		}
	}
	return strings.Join(parts, "\n"), nil
}

// generateTables generates files for each table based on layout mode.
func generateTables(tmpl *template.Template, tables []TableContext, cfg *config.RootConfig, opts *Options, version string) ([]string, error) {
	if cfg.Output.Layout == config.LayoutFilePerTable {
		return generateTablesPerFile(tmpl, tables, opts, version)
	}
	return generateTablesSingleFile(tmpl, tables, opts, version)
}

// generateTablesPerFile generates one file per table.
//
// Import handling: a table context's declared Imports are NOT complete —
// context / comparator / sql / database and other package imports are back-
// filled by goimports resolution (the single-file path relies on the same
// Format() call). To keep per-file output both correct AND fast, resolution is
// done once over the concatenated bodies (resolvePackageImports → one module
// scan); each per-file emission then reuses that complete set and formats with
// full goimports, which prunes the imports the file does not use without
// re-scanning. The file_per_table sqlite example guards against import drift —
// a template that introduces a new import missed here fails its compile.
func generateTablesPerFile(tmpl *template.Template, tables []TableContext, opts *Options, version string) ([]string, error) {
	bodies := make([]string, len(tables))
	var allImports []string
	for i, tc := range tables {
		body, err := renderTableBody(tmpl, tc)
		if err != nil {
			return nil, err
		}
		bodies[i] = body
		allImports = append(allImports, tc.Imports...)
	}

	resolvedImports, err := resolvePackageImports(
		opts.Package, deduplicateImports(allImports),
		[]byte(strings.Join(bodies, "\n\n")), version,
		filepath.Join(opts.OutputDir, modelsFileName),
	)
	if err != nil {
		return nil, err
	}

	var files []string
	for i, tc := range tables {
		filename := TableFileName(tc, config.LayoutFilePerTable)
		filePath := filepath.Join(opts.OutputDir, filename)

		raw := WrapWithPreamble(tc.Package, resolvedImports, []byte(bodies[i]))
		formatted, err := Format(raw, version, filePath)
		if err != nil {
			return nil, fmt.Errorf("formatting %s: %w", filePath, err)
		}

		if err := writeFile(filePath, formatted); err != nil {
			return nil, err
		}
		files = append(files, filePath)
	}
	return files, nil
}

// generateTablesSingleFile renders all tables into one combined file.
func generateTablesSingleFile(tmpl *template.Template, tables []TableContext, opts *Options, version string) ([]string, error) {
	if len(tables) == 0 {
		return nil, nil
	}

	var allBodies []string
	var allImports []string
	pkg := opts.Package

	for _, tc := range tables {
		body, err := renderTableBody(tmpl, tc)
		if err != nil {
			return nil, err
		}
		if body != "" {
			allBodies = append(allBodies, body)
		}
		allImports = append(allImports, tc.Imports...)
	}

	combined := strings.Join(allBodies, "\n\n")
	filePath := filepath.Join(opts.OutputDir, modelsFileName)
	raw := WrapWithPreamble(pkg, deduplicateImports(allImports), []byte(combined))
	formatted, err := Format(raw, version, filePath)
	if err != nil {
		return nil, fmt.Errorf("formatting %s: %w", filePath, err)
	}

	if err := writeFile(filePath, formatted); err != nil {
		return nil, err
	}
	return []string{filePath}, nil
}

// generateViews generates files for each view based on layout mode.
func generateViews(tmpl *template.Template, views []ViewContext, layout config.Layout, opts *Options, version string) ([]string, error) {
	if layout == config.LayoutFilePerTable {
		return generateViewsPerFile(tmpl, views, opts, version)
	}
	return generateViewsSingleFile(tmpl, views, opts, version)
}

// generateViewsPerFile generates one file per view. Import resolution mirrors
// generateTablesPerFile: resolve the complete set once over the concatenated
// view bodies, then emit each file with prune-only formatting.
func generateViewsPerFile(tmpl *template.Template, views []ViewContext, opts *Options, version string) ([]string, error) {
	bodies := make([]string, len(views))
	var allImports []string
	for i, vc := range views {
		body, err := renderViewBody(tmpl, vc)
		if err != nil {
			return nil, err
		}
		bodies[i] = body
		allImports = append(allImports, vc.Imports...)
	}

	resolvedImports, err := resolvePackageImports(
		opts.Package, deduplicateImports(allImports),
		[]byte(strings.Join(bodies, "\n\n")), version,
		filepath.Join(opts.OutputDir, viewsFileName),
	)
	if err != nil {
		return nil, err
	}

	var files []string
	for i, vc := range views {
		filename := ViewFileName(vc, config.LayoutFilePerTable)
		filePath := filepath.Join(opts.OutputDir, filename)

		raw := WrapWithPreamble(vc.Package, resolvedImports, []byte(bodies[i]))
		formatted, err := Format(raw, version, filePath)
		if err != nil {
			return nil, fmt.Errorf("formatting %s: %w", filePath, err)
		}

		if err := writeFile(filePath, formatted); err != nil {
			return nil, err
		}
		files = append(files, filePath)
	}
	return files, nil
}

// generateViewsSingleFile renders all views into one combined file.
func generateViewsSingleFile(tmpl *template.Template, views []ViewContext, opts *Options, version string) ([]string, error) {
	if len(views) == 0 {
		return nil, nil
	}

	var allBodies []string
	var allImports []string
	pkg := opts.Package

	for _, vc := range views {
		body, err := renderViewBody(tmpl, vc)
		if err != nil {
			return nil, err
		}
		if body != "" {
			allBodies = append(allBodies, body)
		}
		allImports = append(allImports, vc.Imports...)
	}

	combined := strings.Join(allBodies, "\n\n")
	filePath := filepath.Join(opts.OutputDir, viewsFileName)
	raw := WrapWithPreamble(pkg, deduplicateImports(allImports), []byte(combined))
	formatted, err := Format(raw, version, filePath)
	if err != nil {
		return nil, fmt.Errorf("formatting %s: %w", filePath, err)
	}

	if err := writeFile(filePath, formatted); err != nil {
		return nil, err
	}
	return []string{filePath}, nil
}

// StaleFiles returns the orphaned *_gen.go files in outputDir — those a
// generation run producing writtenFiles would leave behind. Only *_gen.go is
// considered, and matching is by base name, so a redirected run's writtenFiles
// (which carry temp-dir prefixes) classify identically to a real one's.
//
// Split out from CleanStaleFiles so `sqlgen diff` can report the deletions a
// run would perform without performing them, and cannot drift from what
// `sqlgen generate` actually deletes.
func StaleFiles(outputDir string, writtenFiles []string) ([]string, error) {
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading output dir for cleanup: %w", err)
	}

	written := make(map[string]bool, len(writtenFiles))
	for _, f := range writtenFiles {
		written[filepath.Base(f)] = true
	}

	var stale []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, "_gen.go") || written[name] {
			continue
		}
		// The suffix says a file is eligible; the header says it is ours.
		// PRD §23.8 scopes this sweep to "orphaned generated files" and makes
		// *_gen.go the filter on what is *considered* — it is not a licence to
		// delete another tool's output. output.dir defaults to "." (PRD §4.2),
		// so the sweep routinely runs over a consumer's module root, where
		// wire_gen.go and mock_gen.go are ordinary residents.
		path := filepath.Join(outputDir, name)
		if !isSqlgenGenerated(path) {
			continue
		}
		stale = append(stale, path)
	}

	return stale, nil
}

// isSqlgenGenerated reports whether the file at path carries sqlgen's generated
// header. Only the lines above the package clause are consulted, mirroring Go's
// own convention for where a generated marker lives — and mirroring the
// manifest breadcrumb rule, where deleting the marker disowns the file.
//
// It fails closed: a file that cannot be opened or read reports false and is
// therefore left alone. Declining to delete is the only safe answer to "I could
// not tell whether this is mine".
func isSqlgenGenerated(path string) bool {
	f, err := os.Open(path) //nolint:gosec // path is an entry read from the configured output dir
	if err != nil {
		return false
	}
	defer f.Close() //nolint:errcheck // read-only handle

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, GeneratedHeaderPrefix) {
			return true
		}
		if strings.HasPrefix(line, "package ") {
			return false
		}
	}
	return false
}

// CleanStaleFiles removes orphaned *_gen.go files from the output directory
// that were not part of the current generation run. Only files matching
// *_gen.go are considered for deletion. Returns the paths of deleted files.
func CleanStaleFiles(outputDir string, writtenFiles []string) ([]string, error) {
	stale, err := StaleFiles(outputDir, writtenFiles)
	if err != nil {
		return nil, err
	}

	var deleted []string
	for _, p := range stale {
		if err := os.Remove(p); err != nil {
			return deleted, fmt.Errorf("removing stale file %s: %w", filepath.Base(p), err)
		}
		deleted = append(deleted, p)
	}

	return deleted, nil
}

// deduplicateRelationshipOptionsDefs removes duplicate RelationshipOptionsDef
// entries across tables. All generated files share one Go package, so a struct
// like OrderItemRelationshipOptions must be emitted exactly once even when
// multiple parent tables have O2M/M2M relationships to the same target.
// Each def is kept on the first table (alphabetically) that references it.
func deduplicateRelationshipOptionsDefs(tables []TableContext) []TableContext {
	seen := make(map[string]bool)
	for i := range tables {
		var kept []RelationshipOptionsDef
		for _, def := range tables[i].RelationshipOptionsDefs {
			if !seen[def.StructName] {
				seen[def.StructName] = true
				kept = append(kept, def)
			}
		}
		tables[i].RelationshipOptionsDefs = kept
	}
	return tables
}

// deduplicateImports returns a sorted, deduplicated copy of the import paths.
func deduplicateImports(imports []string) []string {
	seen := make(map[string]bool, len(imports))
	var result []string
	for _, imp := range imports {
		if !seen[imp] {
			seen[imp] = true
			result = append(result, imp)
		}
	}
	slices.Sort(result)
	return result
}

// executeTemplateSafe executes a template and returns the output as a string.
// Returns empty string if the template produces only whitespace.
func executeTemplateSafe(tmpl *template.Template, name string, data any) (string, error) {
	t := tmpl.Lookup(name)
	if t == nil {
		return "", nil
	}
	var buf strings.Builder
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("executing template: %w", err)
	}
	result := strings.TrimSpace(buf.String())
	return result, nil
}

// loadTemplates loads all generation templates from the embedded filesystem.
// Equivalent to loadTemplatesWithResolver(dialect, nil); used by callers that
// do not need user-declared Null-wrapper extraction, e.g. the API
// seed renderer which never emits relationship-loader code.
func loadTemplates(dialect sql.Dialect) (*template.Template, error) {
	return loadTemplatesWithResolver(dialect, nil)
}

// loadTemplatesWithResolver loads generation templates with a Resolver-aware
// funcmap so user-declared Null wrappers (valid_field/valid_method)
// participate in FK extraction.
func loadTemplatesWithResolver(dialect sql.Dialect, resolver *gotype.Resolver) (*template.Template, error) {
	tmpl := template.New("").Funcs(FuncMapWithResolver(dialect, resolver))

	// Parse shared fragments first.
	tmpl, err := tmpl.ParseFS(templateFS, "templates/shared/_*.tmpl")
	if err != nil {
		return nil, fmt.Errorf("parsing shared templates: %w", err)
	}

	// Parse top-level templates.
	tmpl, err = tmpl.ParseFS(templateFS, "templates/*.go.tmpl")
	if err != nil {
		return nil, fmt.Errorf("parsing top-level templates: %w", err)
	}

	// Parse table templates.
	tmpl, err = tmpl.ParseFS(templateFS, "templates/table/*.go.tmpl")
	if err != nil {
		return nil, fmt.Errorf("parsing table templates: %w", err)
	}

	// Parse view templates.
	tmpl, err = tmpl.ParseFS(templateFS, "templates/view/*.go.tmpl")
	if err != nil {
		return nil, fmt.Errorf("parsing view templates: %w", err)
	}

	// Parse api templates (GraphQL schema fragments + scalars Go file).
	tmpl, err = tmpl.ParseFS(templateFS, "templates/api/*.tmpl")
	if err != nil {
		return nil, fmt.Errorf("parsing api templates: %w", err)
	}

	return tmpl, nil
}

// apiPlan is the GraphQL half of a run, settled before anything is written.
// ctx is nil when API generation is disabled; it also lands on
// GenerateResult.API, so the manifest builder reads it without rebuilding it.
type apiPlan struct {
	ctx               *APIContext
	schemaDir         string
	resolverDir       string
	sqlgenResolverDir string
	writeDirs         map[string]string
}

// planAPI builds the APIContext and resolves the graph dirs without writing,
// then refuses the run when the working tree holds a schema file this run
// would not write (checkAPISchemaOrphans). GenerateInto calls it before its
// first write, so a refused run leaves the tree untouched.
//
// Views are passed in alongside tables and land in the same APIContext.Tables
// slice with IsView set (PRD §26.4 "Views on the GraphQL surface"),
// which is why every emission step needs no view-specific arm.
//
// Takes the whole GenerateInput rather than just its Config because it also
// contributes non-fatal notices to input.Warnings.
func planAPI(input *GenerateInput, tables []TableContext, views []ViewContext, schema *parser.Schema, collisions map[string]bool, uuidInteg gotype.UUIDIntegration, opts *Options) (*apiPlan, error) {
	cfg := input.Config
	enumCtxs := BuildEnumContexts(schema, collisions, cfg)
	apiCtx, err := BuildAPIContext(tables, views, enumCtxs, BuildSetContexts(schema, collisions), cfg)
	if err != nil {
		return nil, fmt.Errorf("building api context: %w", err)
	}
	// A declared scalar nothing resolved to is the one failure the declaration
	// mechanism cannot catch structurally — see unusedScalarWarnings. Raised
	// even when apiCtx is nil-gated below, so a config that declares scalars
	// with the API off still says nothing rather than misreporting.
	input.Warnings = append(input.Warnings, unusedScalarWarnings(cfg, apiCtx)...)
	input.Warnings = append(input.Warnings, apiOverReachWarnings(cfg, tables, apiCtx)...)
	input.Warnings = append(input.Warnings, flatUpsertOmittedWarnings(cfg, tables, apiCtx)...)
	if apiCtx == nil {
		return &apiPlan{}, nil
	}
	// The emitted UUID / NullUUID unmarshalers call the package's own library,
	// which BuildAPIContext cannot see — the binding is a whole-package fact
	// resolved from every built context, the same one attachUUIDGeneration
	// consumes (PRD §7.4 "Parsing a UUID from a string"). Threaded in rather
	// than re-derived so the scalar file and the generating calls can never
	// name two different libraries.
	apiCtx.UUIDParseFunc = uuidInteg.ParseFunc

	g := cfg.API.GraphQL
	// §26.5.8: schema_dir / resolver_dir are root-relative — resolved against
	// the project root, NOT output.dir — so graph can live nested under models
	// (the default) or as a top-level sibling depending only on the path.
	//
	// SandboxPath is what keeps a redirected run (Options.WriteRoot) from
	// reaching these dirs. They do not derive from output.dir, so redirecting
	// output.dir alone leaves them pointing straight at the consumer's working
	// tree — which is how `sqlgen diff`, a command specified to write nothing,
	// used to overwrite the whole graph package on every invocation.
	configuredSchemaDir := resolveGraphDir(opts.ProjectRoot, g.SchemaDir)
	configuredResolverDir := resolveGraphDir(opts.ProjectRoot, g.ResolverDir)
	configuredSqlgenResolverDir := filepath.Join(configuredResolverDir, sqlgenResolverPackageName)

	if err := checkAPISchemaOrphans(configuredSchemaDir, configuredResolverDir, apiSchemaFileNames(apiCtx)); err != nil {
		return nil, err
	}

	plan := &apiPlan{
		ctx:               apiCtx,
		schemaDir:         SandboxPath(opts.WriteRoot, configuredSchemaDir),
		resolverDir:       SandboxPath(opts.WriteRoot, configuredResolverDir),
		sqlgenResolverDir: SandboxPath(opts.WriteRoot, configuredSqlgenResolverDir),
	}
	plan.writeDirs = map[string]string{
		plan.schemaDir:         configuredSchemaDir,
		plan.resolverDir:       configuredResolverDir,
		plan.sqlgenResolverDir: configuredSqlgenResolverDir,
	}

	populateAPIContextResolverFields(apiCtx, cfg, opts, g)
	return plan, nil
}

// generateAPI emits the GraphQL schema files (per-entity + shared) plus the
// scalars_gen.go file when category-4 scalars are in use, from a plan
// planAPI has already accepted. Emits nothing when API generation is disabled.
func generateAPI(tmpl *template.Template, plan *apiPlan, cfg *config.RootConfig, opts *Options, version string) ([]string, error) {
	apiCtx := plan.ctx
	if apiCtx == nil {
		return nil, nil
	}
	g := cfg.API.GraphQL
	schemaDir, resolverDir, sqlgenResolverDir := plan.schemaDir, plan.resolverDir, plan.sqlgenResolverDir

	schemaFiles, err := generateAPISchemaFiles(tmpl, apiCtx, schemaDir)
	if err != nil {
		return nil, err
	}
	files := schemaFiles

	scalarFiles, err := generateAPIScalarFile(tmpl, apiCtx, g, resolverDir, version)
	if err != nil {
		return nil, err
	}
	files = append(files, scalarFiles...)

	aliasFiles, err := generateAPIEnvelopeAliasesFile(tmpl, apiCtx, cfg, opts, version)
	if err != nil {
		return nil, err
	}
	files = append(files, aliasFiles...)

	if apiCtx.ModelsImportPath != "" && len(apiCtx.Tables) > 0 {
		resolverFiles, err := generateAPIResolverFiles(tmpl, apiCtx, g, sqlgenResolverDir, version)
		if err != nil {
			return nil, err
		}
		files = append(files, resolverFiles...)
	}

	translatorFiles, err := generateAPITranslatorFiles(tmpl, apiCtx, g, resolverDir, version)
	if err != nil {
		return nil, err
	}
	files = append(files, translatorFiles...)

	supportFiles, err := generateAPISupportFiles(tmpl, apiCtx, g, sqlgenResolverDir, version)
	if err != nil {
		return nil, err
	}
	files = append(files, supportFiles...)

	if apiCtx.ModelsImportPath != "" && len(apiCtx.Tables) > 0 && hasAnyInputTranslator(apiCtx) {
		inputFile, err := generateAPIInputTranslatorFile(tmpl, apiCtx, g, resolverDir, version)
		if err != nil {
			return nil, err
		}
		files = append(files, inputFile)
	}

	return files, nil
}

// populateAPIContextResolverFields resolves the consumer module's path from
// go.mod and writes the derived resolver-side fields (models package alias,
// import paths, sqlgenresolver helper sub-package coordinates) into the
// APIContext so per-template render passes pick them up. Module path is
// best-effort: empty when the working directory has no go.mod, in which
// case modules-package-dependent renders fail-soft.
func populateAPIContextResolverFields(apiCtx *APIContext, cfg *config.RootConfig, opts *Options, _ *config.GraphQLAPIConfig) {
	PopulateAPIContextResolverFields(apiCtx, cfg, apiImportDir(opts, cfg))
}

// PopulateAPIContextResolverFields is the exported wrapper around the
// resolver-field plumbing. CLI subcommands that build an APIContext directly
// (e.g. `sqlgen graphql gen`) call this so the per-template render passes see
// the same resolver-side fields the `sqlgen generate` orchestrator wires up.
// importDir overrides the directory used for the consumer's models import
// path; pass cfg.Output.Dir when no remap is in play.
func PopulateAPIContextResolverFields(apiCtx *APIContext, cfg *config.RootConfig, importDir string) {
	if cfg == nil || cfg.API == nil || cfg.API.GraphQL == nil {
		return
	}
	g := cfg.API.GraphQL
	apiCtx.ModelsPackage = cfg.Output.Package
	apiCtx.ClientName = cfg.Output.Client.Name
	apiCtx.ModelsImportPath = JoinModulePath(ReadModulePath("."), importDir)
	apiCtx.SqlgenResolverPkgName = sqlgenResolverPackageName
	if module := ReadModulePath("."); module != "" {
		// §26.5.8: resolver_dir is root-relative, so the graph package's import
		// path joins it onto the module directly — no output.dir prefix. Nothing
		// prefixes output.dir onto the resolver import path: the default `<output.dir>/graph` already
		// carries the full path, and a top-level `graph` resolves to
		// `<module>/graph` exactly, independent of output.dir.
		apiCtx.SqlgenResolverImportPath = JoinModulePath(module, filepath.Join(g.ResolverDir, sqlgenResolverPackageName))
		// The resolver-dir import path drives the gqlgen.yml `models:`
		// binding for category-4 scalars (UUID/NullUUID/Decimal/NullDecimal/
		// JSON) — the marshaler functions live in <resolver_dir>/scalars_gen.go,
		// so the discovery anchor must point at this package.
		apiCtx.ResolverImportPath = JoinModulePath(module, g.ResolverDir)

		// The gqlgen-emitted input types (`Create<T>Input`,
		// `<T>Filter`, `StringComparator`, …) live in the package
		// gqlgen writes to per its `model:` block. Seed bodies and the
		// per-table input/filter/sort/comparator translators reference
		// these types under the sqlgen-controlled `gqlmodel` alias; the
		// rendered file preamble emits the matching aliased import line
		// only when both the gqlgen.yml path and the consumer module path
		// are resolvable (fail-soft otherwise — same shape as
		// ModelsImportPath / SqlgenResolverImportPath).
		gqlgenCfgPath := g.GqlgenConfig
		if gqlgenCfgPath == "" {
			gqlgenCfgPath = "./gqlgen.yml"
		}
		apiCtx.GqlgenModelImportPath, apiCtx.GqlgenModelAlias = readGqlgenModelInfo(gqlgenCfgPath, module)
	}
}

// generateAPISchemaFiles emits per-table `<table>_gen.graphqls` plus the
// shared `shared_gen.graphqls` schema, under the names apiSchemaFileNames
// reports. Splitting this out of generateAPI keeps the orchestrator's
// cyclomatic complexity below cyclop's threshold.
func generateAPISchemaFiles(tmpl *template.Template, apiCtx *APIContext, schemaDir string) ([]string, error) {
	var files []string
	for _, t := range apiCtx.Tables {
		path := filepath.Join(schemaDir, apiTableSchemaFileName(t))
		body, err := executeTemplateSafe(tmpl, "api/table-schema", t)
		if err != nil {
			return nil, fmt.Errorf("rendering api table schema for %s: %w", t.StructName, err)
		}
		if err := writeFile(path, []byte(body+"\n")); err != nil {
			return nil, err
		}
		files = append(files, path)
	}

	sharedPath := filepath.Join(schemaDir, apiSharedSchemaFileName)
	sharedBody, err := executeTemplateSafe(tmpl, "api/shared-schema", apiCtx)
	if err != nil {
		return nil, fmt.Errorf("rendering api shared schema: %w", err)
	}
	if err := writeFile(sharedPath, []byte(sharedBody+"\n")); err != nil {
		return nil, err
	}
	return append(files, sharedPath), nil
}

const apiSharedSchemaFileName = "shared_gen.graphqls"

func apiTableSchemaFileName(t APITableContext) string { return t.SnakeName + "_gen.graphqls" }

// apiSchemaFileNames is every schema file name generateAPISchemaFiles writes
// for apiCtx — what planAPI checks the working tree against before writing.
func apiSchemaFileNames(apiCtx *APIContext) []string {
	names := make([]string, 0, len(apiCtx.Tables)+1)
	for _, t := range apiCtx.Tables {
		names = append(names, apiTableSchemaFileName(t))
	}
	return append(names, apiSharedSchemaFileName)
}

// checkAPISchemaOrphans fails generation when schemaDir holds a
// `*_gen.graphqls` that is not among names, the schema files this run will
// write (PRD §23.8). An entity that left the API — hidden, dropped
// from the schema, or renamed — leaves its old schema file behind, and gqlgen
// loads every `.graphqls` in the directory, so without this the run fails
// inside gqlgen on a type the orphan still names, far from the cause.
//
// It reports and never deletes. Removing the schema file is not enough on its
// own: under gqlgen's follow-schema layout the orphan's `<stem>.resolvers.go`
// stays behind and no longer compiles, and that file is not sqlgen's to sweep —
// §26.5.6 has sqlgen seed it and gqlgen own it, with consumer-written resolvers
// preserved inside. The companion is named only when it exists at gqlgen's
// default path. Under single-file layout there is none: once the schema file is
// gone, gqlgen moves the stale methods into its own WARNING block.
//
// The dirs are the configured ones rather than this run's write dirs, so a
// redirected run (`sqlgen diff`) fails exactly as `sqlgen generate` does on the
// same tree, instead of previewing changes a real run would never reach. It
// runs from planAPI, before anything is written, which is why it takes the
// names to be written rather than the paths that were.
func checkAPISchemaOrphans(schemaDir, resolverDir string, names []string) error {
	entries, err := os.ReadDir(schemaDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading api schema dir for orphans: %w", err)
	}

	var orphans, resolvers []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, "_gen.graphqls") || slices.Contains(names, name) {
			continue
		}
		orphans = append(orphans, name)
		companion := filepath.Join(resolverDir, strings.TrimSuffix(name, ".graphqls")+".resolvers.go")
		if _, err := os.Stat(companion); err == nil {
			resolvers = append(resolvers, companion)
		}
	}
	if len(orphans) == 0 {
		return nil
	}

	action := "delete them"
	if len(resolvers) > 0 {
		action += fmt.Sprintf(", and the gqlgen resolver files that go with them (%s) after moving out any resolvers written by hand", strings.Join(resolvers, ", "))
	}
	return fmt.Errorf("api: %s holds schema files for entities no longer on the API (%s), which gqlgen would load and fail on; %s (PRD §23.8)", schemaDir, strings.Join(orphans, ", "), action)
}

// generateAPIScalarFile emits scalars_gen.go when category-4 scalars are in
// use, or when a numeric width needs a marshaler pair sqlgen must supply
// (`float32` alone; every integer width anchors on a marshaler
// gqlgen already ships). Returns an empty slice when neither applies, so
// generateAPI doesn't have to track the gating itself.
func generateAPIScalarFile(tmpl *template.Template, apiCtx *APIContext, g *config.GraphQLAPIConfig, resolverDir, version string) ([]string, error) {
	if len(apiCtx.ExternalScalars) == 0 && !apiCtx.NeedsWidthMarshalers() {
		return nil, nil
	}
	scalarsPath := filepath.Join(resolverDir, "scalars_gen.go")
	imports := scalarImports(apiCtx.ExternalScalars)
	if err := renderAndWrite(tmpl, "api/scalars", apiCtx, g.Package, imports, scalarsPath, version); err != nil {
		return nil, fmt.Errorf("generating api scalars: %w", err)
	}
	return []string{scalarsPath}, nil
}

// generateAPIEnvelopeAliasesFile emits api_envelopes_gen.go alongside
// connection_gen.go / pagination_gen.go in the consumer's models package.
// Each managed table gets two generic type aliases —
// `type <T>Connection = Connection[<T>]` and
// `type <T>ListResult = PaginateResult[<T>]` — that the wrapper's gqlgen.yml
// `models:` merge binds the GraphQL `<T>Connection` / `<T>ListResult` envelope
// types to. The aliases live in the same package as Connection / PaginateResult /
// <T> so they resolve to concrete instantiations at zero cost; gqlgen
// dereferences them via `code.Unalias()` and reuses the runtime envelopes
// directly. Returns nil when API generation is disabled or no tables surface
// API operations (so generateAPI doesn't have to track the gating itself).
func generateAPIEnvelopeAliasesFile(tmpl *template.Template, apiCtx *APIContext, cfg *config.RootConfig, opts *Options, version string) ([]string, error) {
	if len(apiCtx.Tables) == 0 {
		return nil, nil
	}
	aliasPath := filepath.Join(opts.OutputDir, "api_envelopes_gen.go")
	if err := renderAndWrite(tmpl, "api/envelope-aliases", apiCtx, cfg.Output.Package, nil, aliasPath, version); err != nil {
		return nil, fmt.Errorf("generating api envelope aliases: %w", err)
	}
	return []string{aliasPath}, nil
}

// hasAnyInputTranslator reports whether any included table has at least one
// create/update mutation that would require a translate*Input helper.
//
// It also checks HasCreateInput / HasUpdateInput so an all-PK table
// (e.g. a pure M2M junction with no metadata columns) doesn't trip the
// file-level gate when its mutations are gated away at the per-method level.
// Without this, every per-table block in input_translate.go.tmpl would skip
// emission and we'd write an empty stub file.
func hasAnyInputTranslator(apiCtx *APIContext) bool {
	for _, t := range apiCtx.Tables {
		o := t.Operations
		if (o.Create || o.CreateMany || o.Upsert) && t.HasCreateInput {
			return true
		}
		if (o.Update || o.UpdateWhere) && t.HasUpdateInput {
			return true
		}
	}
	return false
}

// generateAPIInputTranslatorFile emits graph/input_translate_gen.go containing
// the translate<Op><Table>Input helpers and hasUpdate<Table>SetFields
// predicates. These bridge gqlgen-emitted plain-pointer input types onto the
// consumer model's omittable-wrapped Create/Update<Table>Input shape; seed
// files in the same package call these before delegating into r.Q / r.M.
func generateAPIInputTranslatorFile(tmpl *template.Template, apiCtx *APIContext, g *config.GraphQLAPIConfig, resolverDir, version string) (string, error) {
	path := filepath.Join(resolverDir, "input_translate_gen.go")
	body, err := executeTemplateSafe(tmpl, "api/input-translate", apiCtx)
	if err != nil {
		return "", fmt.Errorf("rendering api input translators: %w", err)
	}
	formatted, err := FormatOnly(buildAPIInputTranslateFile(g.Package, apiCtx, body), version, path)
	if err != nil {
		return "", fmt.Errorf("formatting api input translators: %w", err)
	}
	if err := writeFile(path, formatted); err != nil {
		return "", err
	}
	return path, nil
}

// buildAPIInputTranslateFile assembles the input_translate_gen.go source with
// the explicit aliased import for the consumer's models package plus omittable.
// Mirrors buildAPIFilterFile / buildAPIWalkerFile.
//
// When the gqlgen-emitted model package is resolvable, the rendered
// translator body references gqlgen-generated input types (Create<T>Input /
// Update<T>Input) under apiCtx.GqlgenModelAlias — emit the matching aliased
// import line so the file resolves the qualified references.
func buildAPIInputTranslateFile(pkg string, apiCtx *APIContext, body string) []byte {
	var b strings.Builder
	b.WriteString("package ")
	b.WriteString(pkg)
	b.WriteString("\n\nimport (\n")
	b.WriteString("\t\"github.com/teandresmith/sqlgen/omittable\"\n")
	b.WriteString("\n\t")
	b.WriteString(apiCtx.ModelsPackage)
	b.WriteString(" \"")
	b.WriteString(apiCtx.ModelsImportPath)
	b.WriteString("\"\n")
	if apiCtx.GqlgenModelImportPath != "" && apiCtx.GqlgenModelAlias != "" {
		b.WriteString("\t")
		b.WriteString(apiCtx.GqlgenModelAlias)
		b.WriteString(" \"")
		b.WriteString(apiCtx.GqlgenModelImportPath)
		b.WriteString("\"\n")
	}
	if nestedTranslatorsNeedSqlgenResolver(apiCtx) {
		b.WriteString("\n\t\"")
		b.WriteString(apiCtx.SqlgenResolverImportPath)
		b.WriteString("\"\n")
	}
	b.WriteString(")\n\n")
	b.WriteString(body)
	b.WriteString("\n")
	return []byte(b.String())
}

// nestedTranslatorsNeedSqlgenResolver reports whether the rendered nested
// translators reference the sqlgenresolver package: the conflict-target
// translator and the increment collector both return its INVALID_INPUT error,
// and the collector builds its IncrementOp values (PRD §26.5.1, §26.5.4).
// Scoped so a package with no such translator keeps its import block as it
// was — FormatOnly does not prune an unused import.
func nestedTranslatorsNeedSqlgenResolver(apiCtx *APIContext) bool {
	for _, t := range apiCtx.Tables {
		if n := t.Nested; n != nil && (n.EmitUpsert || n.IncrementOpsFunc != "") {
			return true
		}
	}
	return false
}

// generateAPISupportFiles emits the §16.6 helper files: errors_gen.go
// (mapErrorToGQL — PRD §26.5.5) and middleware_gen.go (WithCallOptionsMiddleware
// + callOptionsFromHTTP — PRD §26.11). Both emit to the sqlgenresolver/
// helper sub-package since Q/M methods reference them and seeds
// in the parent graph package import sqlgenresolver to call them too.
func generateAPISupportFiles(tmpl *template.Template, apiCtx *APIContext, g *config.GraphQLAPIConfig, sqlgenResolverDir, version string) ([]string, error) {
	_ = g
	var files []string

	errorFile, err := generateAPIErrorsFile(tmpl, sqlgenResolverDir, version)
	if err != nil {
		return nil, err
	}
	files = append(files, errorFile)

	if apiCtx.ModelsImportPath != "" {
		middlewareFile, err := generateAPIMiddlewareFile(tmpl, apiCtx, sqlgenResolverDir, version)
		if err != nil {
			return nil, err
		}
		files = append(files, middlewareFile)
	}

	return files, nil
}

// generateAPIErrorsFile emits sqlgenresolver/errors_gen.go containing
// mapErrorToGQL (PRD §26.5.5). The file has no dependency on the consumer's
// models package — it imports only the runtime sentinel packages plus
// gqlerror — so the standard renderAndWrite pipeline is sufficient.
func generateAPIErrorsFile(tmpl *template.Template, sqlgenResolverDir, version string) (string, error) {
	path := filepath.Join(sqlgenResolverDir, "errors_gen.go")
	imports := []string{
		"errors",
		"github.com/teandresmith/sqlgen/database",
		"github.com/teandresmith/sqlgen/tenancy",
		"github.com/vektah/gqlparser/v2/gqlerror",
	}
	if err := renderAndWrite(tmpl, "api/errors", nil, sqlgenResolverPackageName, imports, path, version); err != nil {
		return "", fmt.Errorf("generating api errors: %w", err)
	}
	return path, nil
}

// generateAPIMiddlewareFile emits sqlgenresolver/middleware_gen.go containing
// WithCallOptionsMiddleware + callOptionsFromHTTP (PRD §26.11). The file
// references the consumer's models package via the per-call CallOptions[FO]
// type, so it uses the same explicit aliased-import pattern as
// resolvers_gen.go and field_options_gen.go.
func generateAPIMiddlewareFile(tmpl *template.Template, apiCtx *APIContext, sqlgenResolverDir, version string) (string, error) {
	path := filepath.Join(sqlgenResolverDir, "middleware_gen.go")
	body, err := executeTemplateSafe(tmpl, "api/middleware", apiCtx)
	if err != nil {
		return "", fmt.Errorf("rendering api middleware: %w", err)
	}
	formatted, err := FormatOnly(buildAPIMiddlewareFile(sqlgenResolverPackageName, apiCtx, body), version, path)
	if err != nil {
		return "", fmt.Errorf("formatting api middleware: %w", err)
	}
	if err := writeFile(path, formatted); err != nil {
		return "", err
	}
	return path, nil
}

// buildAPIMiddlewareFile assembles middleware_gen.go with the explicit
// aliased-import block for the consumer's models package, mirroring
// buildAPIWalkerFile / buildAPIFilterFile. FormatOnly is used downstream so
// goimports' module-cache scan does not fire while the consumer's models
// package may not yet exist on disk during a fresh sqlgen run.
func buildAPIMiddlewareFile(pkg string, apiCtx *APIContext, body string) []byte {
	var b strings.Builder
	b.WriteString("package ")
	b.WriteString(pkg)
	b.WriteString("\n\nimport (\n")
	b.WriteString("\t\"context\"\n")
	b.WriteString("\t\"net/http\"\n")
	b.WriteString("\n\t")
	b.WriteString(apiCtx.ModelsPackage)
	b.WriteString(" \"")
	b.WriteString(apiCtx.ModelsImportPath)
	b.WriteString("\"\n")
	b.WriteString(")\n\n")
	b.WriteString(body)
	b.WriteString("\n")
	return []byte(b.String())
}

// generateAPITranslatorFiles emits the three translator files
// — comparator_translate_gen.go (per-family + per-T comparator translators,
// shared across the whole project), filter_translate_gen.go (per-table
// filter translators), and sort_translate_gen.go (per-table sort translators
// + shared direction helper). Each file is gated on having something to
// emit so a schema with no filterable / sortable columns produces no empty
// stub files.
//
// comparator_translate_gen.go has no dependency on the consumer's models
// package and is emitted whenever at least one translator was registered.
// filter_translate_gen.go depends on the consumer's models package via the
// per-table <Table>Filter type — it is skipped when ModelsImportPath cannot
// be resolved (same fail-soft path as resolvers_gen.go). sort_translate_gen.go
// depends only on `sql.Sort` and the gqlgen-generated graph-package types,
// so it has the same gating as comparator_translate_gen.go.
func generateAPITranslatorFiles(tmpl *template.Template, apiCtx *APIContext, g *config.GraphQLAPIConfig, resolverDir, version string) ([]string, error) {
	var files []string

	if len(apiCtx.ComparatorTranslators) > 0 {
		comparatorPath := filepath.Join(resolverDir, "comparator_translate_gen.go")
		body, err := executeTemplateSafe(tmpl, "api/comparator-translate", apiCtx)
		if err != nil {
			return nil, fmt.Errorf("rendering api comparator translators: %w", err)
		}
		formatted, err := FormatOnly(buildAPIComparatorTranslateFile(g.Package, apiCtx, body), version, comparatorPath)
		if err != nil {
			return nil, fmt.Errorf("formatting api comparator translators: %w", err)
		}
		if err := writeFile(comparatorPath, formatted); err != nil {
			return nil, err
		}
		files = append(files, comparatorPath)
	}

	if hasAnySortFields(apiCtx) {
		sortPath := filepath.Join(resolverDir, "sort_translate_gen.go")
		body, err := executeTemplateSafe(tmpl, "api/sort-translate", apiCtx)
		if err != nil {
			return nil, fmt.Errorf("rendering api sort translators: %w", err)
		}
		formatted, err := FormatOnly(buildAPISortTranslateFile(g.Package, apiCtx, body), version, sortPath)
		if err != nil {
			return nil, fmt.Errorf("formatting api sort translators: %w", err)
		}
		if err := writeFile(sortPath, formatted); err != nil {
			return nil, err
		}
		files = append(files, sortPath)
	}

	if apiCtx.ModelsImportPath != "" && hasAnyFilterField(apiCtx) {
		filterPath := filepath.Join(resolverDir, "filter_translate_gen.go")
		body, err := executeTemplateSafe(tmpl, "api/filter-translate", apiCtx)
		if err != nil {
			return nil, fmt.Errorf("rendering api filter translators: %w", err)
		}
		formatted, err := FormatOnly(buildAPIFilterFile(g.Package, apiCtx, body), version, filterPath)
		if err != nil {
			return nil, fmt.Errorf("formatting api filter translators: %w", err)
		}
		if err := writeFile(filterPath, formatted); err != nil {
			return nil, err
		}
		files = append(files, filterPath)
	}

	return files, nil
}

// buildAPIComparatorTranslateFile assembles comparator_translate_gen.go with
// the runtime comparator import (always required since every translator
// returns a *comparator.<X>) and, when in scope, the time stdlib — the Time
// family references `time.Time` from four operands (`in`, `nin`, `between`,
// `nbetween`), in both the base and the nullable variant.
//
// The Enum family is the one whose type parameter lives in the
// consumer's models package (`*comparator.Enum[models.OrderStatus]`), so an
// enum translator pulls that import in too. It is emitted only when the
// module path resolved, so an unresolvable path yields a file that still
// parses rather than one carrying `models ""`. That is weaker than the
// filter translator's fail-soft, which skips the file outright: this file is
// gated only on having translators, so with no resolvable go.mod it is
// written with `models.X` bodies and no import. Pre-existing degeneracy
// rather than a new one — `GqlgenModelAlias` is empty on that same path, so
// every `in *StringComparator` parameter is already unresolvable and the
// file could not compile either way.
//
// When the gqlgen-emitted model package is resolvable, the rendered
// body references gqlgen-emitted scalar comparator inputs (StringComparator,
// NumericComparator, etc.) under apiCtx.GqlgenModelAlias — emit the matching
// aliased import line so the file resolves the qualified references. The
// previous renderAndWrite path used unaliased imports, which couldn't carry
// the sqlgen-controlled alias.
func buildAPIComparatorTranslateFile(pkg string, apiCtx *APIContext, body string) []byte {
	var b strings.Builder
	b.WriteString("package ")
	b.WriteString(pkg)
	b.WriteString("\n\nimport (\n")
	if std := comparatorTranslatorStdImports(apiCtx); len(std) > 0 {
		for _, imp := range std {
			b.WriteString("\t\"")
			b.WriteString(imp)
			b.WriteString("\"\n")
		}
		b.WriteString("\n")
	}
	b.WriteString("\t\"github.com/teandresmith/sqlgen/comparator\"\n")
	// The consumer's own packages share one group, the way
	// filter_translate_gen.go already writes them.
	wantModels := comparatorTranslatorsUseModels(apiCtx) && apiCtx.ModelsImportPath != ""
	wantGqlgen := apiCtx.GqlgenModelImportPath != "" && apiCtx.GqlgenModelAlias != ""
	if wantModels || wantGqlgen {
		b.WriteString("\n")
	}
	if wantModels {
		b.WriteString("\t")
		b.WriteString(apiCtx.ModelsPackage)
		b.WriteString(" \"")
		b.WriteString(apiCtx.ModelsImportPath)
		b.WriteString("\"\n")
	}
	if wantGqlgen {
		b.WriteString("\t")
		b.WriteString(apiCtx.GqlgenModelAlias)
		b.WriteString(" \"")
		b.WriteString(apiCtx.GqlgenModelImportPath)
		b.WriteString("\"\n")
	}
	b.WriteString(")\n\n")
	b.WriteString(body)
	b.WriteString("\n")
	return []byte(b.String())
}

// comparatorTranslatorsUseModels reports whether any emitted translator names
// a type from the consumer's models package. Two families can: Enum, whose type
// parameter IS the consumer's enum type, and a Slice over that same enum. Every
// other family's parameter is a builtin or a stdlib type (see
// comparatorTranslatorStdImports).
func comparatorTranslatorsUseModels(apiCtx *APIContext) bool {
	for _, tr := range apiCtx.ComparatorTranslators {
		if tr.EnumT != "" || tr.SliceElemIsEnum {
			return true
		}
	}
	return false
}

// buildAPISortTranslateFile assembles sort_translate_gen.go with the runtime
// sql import (sql.Sort / sql.SortDirection) and, when in scope, the
// gqlgen-emitted model package alias for SortDirection / <T>Sort /
// <T>SortField references. The previous renderAndWrite path used
// unaliased imports, which couldn't carry the sqlgen-controlled alias.
func buildAPISortTranslateFile(pkg string, apiCtx *APIContext, body string) []byte {
	var b strings.Builder
	b.WriteString("package ")
	b.WriteString(pkg)
	b.WriteString("\n\nimport (\n")
	b.WriteString("\t\"github.com/teandresmith/sqlgen/sql\"\n")
	if apiCtx.GqlgenModelImportPath != "" && apiCtx.GqlgenModelAlias != "" {
		b.WriteString("\n\t")
		b.WriteString(apiCtx.GqlgenModelAlias)
		b.WriteString(" \"")
		b.WriteString(apiCtx.GqlgenModelImportPath)
		b.WriteString("\"\n")
	}
	b.WriteString(")\n\n")
	b.WriteString(body)
	b.WriteString("\n")
	return []byte(b.String())
}

// buildAPIFilterFile assembles filter_translate_gen.go with the same explicit
// aliased-import pattern used by resolvers_gen.go and field_options_gen.go —
// the consumer's models package is pinned to cfg.Output.Package even when
// the import path's last segment differs. FormatOnly skips goimports' module-
// cache scan since the consumer's models package may not exist during a
// fresh sqlgen run.
//
// The filter translator does not import the comparator package directly —
// every call into a per-family comparator translator (which lives in the
// same graph package) returns a concrete `*comparator.<X>` value that the
// model filter struct field accepts via Go's structural assignment.
//
// When the gqlgen-emitted model package is resolvable, the
// rendered body references gqlgen-emitted `<T>Filter` types under
// apiCtx.GqlgenModelAlias — emit the matching aliased import line so the
// file resolves the qualified references.
func buildAPIFilterFile(pkg string, apiCtx *APIContext, body string) []byte {
	var b strings.Builder
	b.WriteString("package ")
	b.WriteString(pkg)
	b.WriteString("\n\nimport (\n")
	b.WriteString("\t")
	b.WriteString(apiCtx.ModelsPackage)
	b.WriteString(" \"")
	b.WriteString(apiCtx.ModelsImportPath)
	b.WriteString("\"\n")
	if apiCtx.GqlgenModelImportPath != "" && apiCtx.GqlgenModelAlias != "" {
		b.WriteString("\t")
		b.WriteString(apiCtx.GqlgenModelAlias)
		b.WriteString(" \"")
		b.WriteString(apiCtx.GqlgenModelImportPath)
		b.WriteString("\"\n")
	}
	b.WriteString(")\n\n")
	b.WriteString(body)
	b.WriteString("\n")
	return []byte(b.String())
}

// comparatorTranslatorStdImports returns the stdlib packages the comparator
// translator file's bodies reference, sorted. Three families can name one:
// Time (`[]time.Time` + `Range[time.Time]`), the duration monomorphization of
// Number, and the three `net`-typed members of Opaque. Every other family's
// operands are builtins needing no import.
//
// Slice is deliberately absent rather than overlooked: sliceOperandElemGoType
// admits only the GraphQL spec built-ins, so a slice element is either one of
// those (`string` / `bool` / a numeric width — no import) or a schema enum,
// which is the consumer's type and rides the models group below, not this one.
func comparatorTranslatorStdImports(apiCtx *APIContext) []string {
	seen := make(map[string]bool, 2)
	for _, tr := range apiCtx.ComparatorTranslators {
		// Both variants reference their operand type: the nullable
		// form stops delegating to its base (PRD §26.4 Rule 2 makes them
		// distinct input types) and inlines the same operand block. Gating on
		// `!tr.Nullable` here would omit the import for a project whose only
		// such columns are nullable, and FormatOnly rendering means goimports
		// never repairs it — a missing import is a compile error in the
		// consumer's project, and so is an unused one, so this must name
		// exactly what the emitted bodies reference.
		switch tr.Family {
		case "Time":
			// `[]time.Time` + `comparator.Range[time.Time]`.
			seen["time"] = true
		case "Number":
			// Every other numeric width is a builtin needing no import; only
			// the duration monomorphization names a package.
			if pkg, ok := stdImportForQualifiedType(tr.NumericT); ok {
				seen[pkg] = true
			}
		case "Opaque":
			// `[]byte` names no package; the three `net` types do.
			if pkg, ok := stdImportForQualifiedType(tr.OpaqueT); ok {
				seen[pkg] = true
			}
		}
	}
	return slices.Sorted(maps.Keys(seen))
}

// stdImportForQualifiedType returns the stdlib import path for a comparator
// type parameter spelled `pkg.Name`, and whether it needs one at all. Only
// the two packages the built-in dialect tables can produce here are accepted
// — `time` (time.Time / time.Duration) and `net` (net.IP / net.IPNet /
// net.HardwareAddr). Anything else is reported as needing no import rather
// than guessed at: a wrong import path is a compile error in the consumer's
// project, and every type reaching this point comes from a closed set the
// generator controls.
func stdImportForQualifiedType(typeParam string) (string, bool) {
	pkg, _, ok := strings.Cut(strings.TrimPrefix(typeParam, "[]"), ".")
	if !ok {
		return "", false
	}
	switch pkg {
	case "time", "net":
		return pkg, true
	}
	return "", false
}

// hasAnySortFields reports whether any included table emits a sort enum +
// translator. Always true in practice (every table has at least one column),
// but defended here so the orchestrator does not emit an empty file.
func hasAnySortFields(apiCtx *APIContext) bool {
	for _, t := range apiCtx.Tables {
		if len(t.SortFields) > 0 {
			return true
		}
	}
	return false
}

// hasAnyFilterField reports whether any included table emits at least one
// filterable column whose comparator type maps to a supported family. False
// when every column on every table is excluded (PK-only or comparator family
// outside the supported set).
func hasAnyFilterField(apiCtx *APIContext) bool {
	for _, t := range apiCtx.Tables {
		if len(t.FilterFields) > 0 {
			return true
		}
	}
	return false
}

// generateAPIResolverFiles emits the modules-package-dependent files for the
// sqlgen-owned helper sub-package: resolvers_gen.go (Q/M
// structs + per-table methods), field_options_gen.go (per-table selection-set
// walker), and connection_walker_gen.go (shared Relay Connection unwrapper).
// All emit under <resolver_dir>/sqlgenresolver/ with package "sqlgenresolver"
// so gqlgen's resolvergen plugin never scans them. Skipped when no module
// path is resolvable.
//
// The resolvers + walker templates embed their own import blocks (instead of
// relying on WrapWithPreamble) so they can alias the consumer's package to a
// name that matches cfg.Output.Package even when the import path's last
// segment differs. FormatOnly is used afterwards to keep goimports out of
// the picture — the consumer's package may not exist in the module cache yet
// during a fresh sqlgen run, which would trip goimports' import-resolution
// scan.
func generateAPIResolverFiles(tmpl *template.Template, apiCtx *APIContext, g *config.GraphQLAPIConfig, sqlgenResolverDir, version string) ([]string, error) {
	_ = g
	var files []string

	// sqlgenresolver/resolvers_gen.go — Q + M structs and per-table methods.
	resolversPath := filepath.Join(sqlgenResolverDir, "resolvers_gen.go")
	body, err := executeTemplateSafe(tmpl, "api/resolvers", apiCtx)
	if err != nil {
		return nil, fmt.Errorf("rendering api resolvers: %w", err)
	}
	formatted, err := FormatOnly(buildResolverFile(sqlgenResolverPackageName, apiCtx, body), version, resolversPath)
	if err != nil {
		return nil, fmt.Errorf("formatting api resolvers: %w", err)
	}
	if err := writeFile(resolversPath, formatted); err != nil {
		return nil, err
	}
	files = append(files, resolversPath)

	// sqlgenresolver/field_options_gen.go — per-table selection-set walker.
	walkerPath := filepath.Join(sqlgenResolverDir, "field_options_gen.go")
	walkerBody, err := executeTemplateSafe(tmpl, "api/field-options", apiCtx)
	if err != nil {
		return nil, fmt.Errorf("rendering api field options: %w", err)
	}
	walkerFormatted, err := FormatOnly(buildAPIWalkerFile(sqlgenResolverPackageName, apiCtx, walkerBody), version, walkerPath)
	if err != nil {
		return nil, fmt.Errorf("formatting api field options: %w", err)
	}
	if err := writeFile(walkerPath, walkerFormatted); err != nil {
		return nil, err
	}
	files = append(files, walkerPath)

	// sqlgenresolver/connection_walker_gen.go — shared Connection unwrapper.
	// The helper derives the parent GraphQL type from ctx, so the
	// file needs "context" in its preamble.
	connWalkerPath := filepath.Join(sqlgenResolverDir, "connection_walker_gen.go")
	connWalkerImports := []string{"context", "github.com/99designs/gqlgen/graphql"}
	if err := renderAndWrite(tmpl, "api/connection-walker", apiCtx, sqlgenResolverPackageName, connWalkerImports, connWalkerPath, version); err != nil {
		return nil, fmt.Errorf("generating api connection walker: %w", err)
	}
	files = append(files, connWalkerPath)

	return files, nil
}

// sqlgenResolverPackageName is the Go package name of the sqlgen-owned helper
// sub-package. Always emitted as a sibling directory of the consumer's resolver
// dir to keep gqlgen's resolvergen plugin from scanning it.
const sqlgenResolverPackageName = "sqlgenresolver"

// SeedBody is one rendered per-table delegation block.
// SnakeName is the snake_case form of the table's struct name (matching the
// schema-side .graphqls filename stem) and Body is the rendered delegation
// block with no preamble — the wrapper layer wraps with the package /
// import header.
type SeedBody struct {
	SnakeName string
	Body      string
}

// RenderAPISeeds renders the per-table seed delegation bodies an `sqlgen
// graphql gen` invocation needs. The wrapper writes one file per
// table under follow-schema layout, or concatenates them under single-file
// layout — the data flow is the same in both cases.
func RenderAPISeeds(dialect sql.Dialect, apiCtx *APIContext) ([]SeedBody, error) {
	tmpl, err := loadTemplates(dialect)
	if err != nil {
		return nil, err
	}
	out := make([]SeedBody, 0, len(apiCtx.Tables))
	for _, t := range apiCtx.Tables {
		single := *apiCtx
		single.Tables = []APITableContext{t}
		body, err := executeTemplateSafe(tmpl, "api/seeds", &single)
		if err != nil {
			return nil, fmt.Errorf("rendering seed for %s: %w", t.StructName, err)
		}
		// An entity the mask leaves with no root resolver (every read off and
		// no mutation that emits, as on a view with all four reads masked)
		// gets no seed. The file would be nothing but the import block, and
		// gqlgen renders no resolver file for a schema source with no
		// resolver field, so nothing would prune the unused imports before
		// the build. The rendered body is the test rather than the
		// root-field list, because that list does not gate create / update on
		// the input existing and the template does.
		if strings.TrimSpace(body) == "" {
			continue
		}
		out = append(out, SeedBody{
			SnakeName: t.SnakeName,
			Body:      body,
		})
	}
	return out, nil
}

// CollectSqlgenManagedFields enumerates the curated-surface field names
// sqlgen emits per table. The wrapper's panic-stub rewriter uses this list
// to know which gqlgen-owned panic stubs are safe to rewrite into
// delegations (names not in this set are consumer-authored and must not be
// touched).
//
// Field names follow gqlgen's PascalCase resolver-method naming convention
// (e.g. "Product", "Products", "ProductList", "CreateProduct", "DeleteProduct").
// Naming logic mirrors api/seeds.go.tmpl exactly so the rewriter and seed
// emitter stay in lockstep.
func CollectSqlgenManagedFields(apiCtx *APIContext) (queryFields, mutationFields []string) {
	q, m := CollectSqlgenRootFields(apiCtx)
	return goNames(q), goNames(m)
}

// APIRootField pairs a Query/Mutation field as it appears in the emitted
// schema with the Go method name sqlgen's resolver seed declares for it.
//
// GoName must equal the method gqlgen derives from GraphQLName, or the seed
// does not satisfy gqlgen's generated resolver interface. sqlgen cannot
// dictate it: gqlgen ignores `fieldName` on root fields. Where the two
// spellings differ (`onlyPK` is `OnlyPk` to gqlgen), the wrapper fails
// generation on the stub gqlgen leaves and names the entity to rename
// (PRD §26.5.6).
type APIRootField struct {
	// GraphQLName is the field on the Query or Mutation type (e.g. "csvRecord",
	// "createCSVRecord").
	GraphQLName string
	// GoName is the resolver method sqlgen's seed emits (e.g. "CSVRecord",
	// "CreateCSVRecord").
	GoName string
	// ConfigKey is the owning entity's config path, "tables.<sql name>" or
	// "views.<sql name>". The wrapper names it, with its struct_name, when
	// gqlgen spells GoName differently and leaves the field a stub.
	ConfigKey string
}

// CollectSqlgenRootFields returns every Query and Mutation field sqlgen
// manages, paired with its resolver method name. This is the single gated
// source for the root surface: the schema template, the seed template, the
// panic-stub rewriter's managed-field set, and the wrapper's post-gqlgen
// stub check all resolve to it, so an operation cannot be emitted into the
// schema without the matching method name being known.
func CollectSqlgenRootFields(apiCtx *APIContext) (queryFields, mutationFields []APIRootField) {
	for _, t := range apiCtx.Tables {
		key := "tables." + t.SQLTable
		if t.IsView {
			key = "views." + t.SQLTable
		}
		for _, f := range tableQueryFieldNames(t) {
			f.ConfigKey = key
			queryFields = append(queryFields, f)
		}
		for _, f := range tableMutationFieldNames(t) {
			f.ConfigKey = key
			mutationFields = append(mutationFields, f)
		}
	}
	return queryFields, mutationFields
}

// goNames projects the Go half of a root-field list.
func goNames(fields []APIRootField) []string {
	if len(fields) == 0 {
		return nil
	}
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		out = append(out, f.GoName)
	}
	return out
}

// lcFirstName returns the GraphQL field name for a mutation whose schema form
// is a lowercase verb followed by the PascalCase entity name — `create` +
// "CSVRecord" is emitted as "createCSVRecord", so lowering the first rune of
// the Go method name reproduces it exactly.
func lcFirstName(goName string) string {
	if goName == "" {
		return ""
	}
	r := []rune(goName)
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

// tableQueryFieldNames returns the Query.* method names sqlgen emits for one
// API table — Get / Connection / List variants based on the resolved ops.
func tableQueryFieldNames(t APITableContext) []APIRootField {
	var names []APIRootField
	o := t.Operations
	if o.Get {
		names = append(names, APIRootField{GraphQLName: t.QueryName, GoName: t.StructName})
	}
	if o.Connection {
		names = append(names, APIRootField{GraphQLName: t.QueryNamePlural, GoName: t.StructNamePlural})
	}
	if o.Paginate {
		names = append(names, APIRootField{GraphQLName: t.ListQueryName, GoName: t.StructName + "List"})
	}
	return names
}

// tableMutationFieldNames returns the Mutation.* method names sqlgen emits
// for one API table — create / update / upsert / delete / restore variants.
// Delete naming follows PRD §26.5.1: hard-only and soft-only modes both
// emit a bare `Delete<Table>`; both-mode emits explicit `HardDelete<Table>`
// + `SoftDelete<Table>`.
func tableMutationFieldNames(t APITableContext) []APIRootField {
	var names []APIRootField
	add := func(goName string) {
		names = append(names, APIRootField{GraphQLName: lcFirstName(goName), GoName: goName})
	}
	o := t.Operations
	if o.Create {
		add("Create" + t.StructName)
	}
	if o.CreateMany {
		add("Create" + t.StructNamePlural)
	}
	if o.Update {
		add("Update" + t.StructName)
	}
	if o.UpdateWhere {
		add("Update" + t.StructNamePlural)
	}
	if o.Upsert && t.HasConflictPK {
		add("Upsert" + t.StructName)
	}
	switch {
	case o.HardDelete && !o.SoftDelete, o.SoftDelete && !o.HardDelete:
		add("Delete" + t.StructName)
	case o.HardDelete && o.SoftDelete:
		add("HardDelete" + t.StructName)
		add("SoftDelete" + t.StructName)
	}
	if o.Restore && t.HasSoftDelete {
		add("Restore" + t.StructName)
	}
	return append(names, nestedMutationFieldNames(t)...)
}

// nestedMutationFieldNames returns the nested-mutation root fields (PRD
// §26.5.1), gated exactly as the schema template's own `extend type Mutation`
// block for them is.
func nestedMutationFieldNames(t APITableContext) []APIRootField {
	n := t.Nested
	if n == nil {
		return nil
	}
	var names []APIRootField
	for _, f := range []struct {
		emit bool
		verb string
	}{{n.EmitCreate, "Create"}, {n.EmitUpdate, "Update"}, {n.EmitUpsert, "Upsert"}} {
		if f.emit {
			goName := f.verb + t.StructName + "WithRelated"
			names = append(names, APIRootField{GraphQLName: lcFirstName(goName), GoName: goName})
		}
	}
	return names
}

// buildAPIWalkerFile assembles field_options_gen.go with the explicit aliased
// import block for the consumer's models package, mirroring buildResolverFile.
// FormatOnly is used downstream to skip goimports' module-cache scan since
// the consumer's models package may not yet exist when sqlgen runs.
func buildAPIWalkerFile(pkg string, apiCtx *APIContext, body string) []byte {
	var b strings.Builder
	b.WriteString("package ")
	b.WriteString(pkg)
	b.WriteString("\n\nimport (\n")
	b.WriteString("\t\"context\"\n")
	b.WriteString("\n\t\"github.com/99designs/gqlgen/graphql\"\n")
	b.WriteString("\n\t")
	b.WriteString(apiCtx.ModelsPackage)
	b.WriteString(" \"")
	b.WriteString(apiCtx.ModelsImportPath)
	b.WriteString("\"\n")
	b.WriteString(")\n\n")
	b.WriteString(body)
	b.WriteString("\n")
	return []byte(b.String())
}

// buildResolverFile assembles the complete resolvers_gen.go source: package
// declaration, an explicit aliased import block (consumer's models package
// pinned to cfg.Output.Package), and the rendered template body. Imports
// are scoped to what the rendered body actually references so FormatOnly
// (no goimports cache scan) leaves a clean file.
//
// The file emits to <resolver_dir>/sqlgenresolver/resolvers_gen.go
// with package "sqlgenresolver" and contains Q + M structs plus per-table
// methods. Q/M live outside gqlgen's resolvergen scope so the resolvergen
// plugin never scans, copies, or stubs them. Seed files in the parent graph
// package delegate from gqlgen's *queryResolver / *mutationResolver methods
// into r.Q.<Method> / r.M.<Method>. Translation between gqlgen-input shapes
// and the consumer's omittable-wrapped Create/Update inputs happens in the
// graph-package input_translate_gen.go helpers, which is why this file no
// longer needs the omittable import.
func buildResolverFile(pkg string, apiCtx *APIContext, body string) []byte {
	feat := computeResolverFileFeatures(apiCtx)

	var b strings.Builder
	b.WriteString("package ")
	b.WriteString(pkg)
	b.WriteString("\n\nimport (\n")
	if feat.HasResolverMethod {
		b.WriteString("\t\"context\"\n")
	}
	if feat.NeedsErrorsImport {
		b.WriteString("\t\"errors\"\n")
	}
	b.WriteString("\n\t\"github.com/vektah/gqlparser/v2/gqlerror\"\n")
	if feat.HasResolverMethod {
		b.WriteString("\n\t")
		b.WriteString(apiCtx.ModelsPackage)
		b.WriteString(" \"")
		b.WriteString(apiCtx.ModelsImportPath)
		b.WriteString("\"\n")
		// Third-party PK arg types (uuid.UUID, decimal.Decimal,
		// time.Time, …) require explicit imports beyond the consumer's models
		// package. Without this, every table whose PK binds to a custom
		// scalar emitted unparseable resolver methods like
		// `func (q *Q) Event(ctx context.Context, id uuid.UUID) ...`
		// against an import block that didn't reference `uuid`.
		for _, imp := range pkArgImports(apiCtx) {
			b.WriteString("\t\"")
			b.WriteString(imp)
			b.WriteString("\"\n")
		}
		// Q.<T>List takes `sort []sql.Sort` to thread the
		// gqlgen-emitted sort input into PaginateInput.Sort.
		if feat.HasList {
			b.WriteString("\t\"github.com/teandresmith/sqlgen/sql\"\n")
		}
	}
	b.WriteString(")\n\n")
	b.WriteString(body)
	b.WriteString("\n")
	return []byte(b.String())
}

// pkArgImports collects the unique non-empty PK-column GoImport paths across
// every API table's PKArgs slice, sorted for deterministic emission. Every PK
// column on every table is collected, not just the first, so composite PKs whose columns span distinct third-party packages
// (e.g. `user_categories(user_id uuid.UUID, category_id int64)`) all resolve
// in the resolver file's import block.
func pkArgImports(apiCtx *APIContext) []string {
	seen := make(map[string]bool)
	var out []string
	for _, t := range apiCtx.Tables {
		for _, a := range t.PKArgs {
			if a.GoImport == "" || seen[a.GoImport] {
				continue
			}
			seen[a.GoImport] = true
			out = append(out, a.GoImport)
		}
	}
	slices.Sort(out)
	return out
}

// resolverFileFeatures summarises which feature groups are referenced by the
// rendered resolver body so buildResolverFile can scope the import block to
// what is actually used. Each flag corresponds to one conditional import.
type resolverFileFeatures struct {
	HasResolverMethod bool // any query or mutation resolver method (drives `context`)
	NeedsErrorsImport bool // any resolver branch calls errors.Is — Get, HardDelete/SoftDelete/Restore (`errors`)
	HasList           bool // any <T>List (Paginate) resolver (drives `sql`)
}

func computeResolverFileFeatures(apiCtx *APIContext) resolverFileFeatures {
	var f resolverFileFeatures
	for _, t := range apiCtx.Tables {
		o := t.Operations
		if hasAnyResolverOp(o) {
			f.HasResolverMethod = true
		}
		// The Get branch collapses ErrNotFound → (nil, nil), so
		// any table with Get enabled — not just delete-class ops — pulls in
		// the `errors` package.
		if o.Get || o.HardDelete || o.SoftDelete || o.Restore {
			f.NeedsErrorsImport = true
		}
		if o.Paginate {
			f.HasList = true
		}
	}
	return f
}

func hasAnyResolverOp(o ResolvedOperations) bool {
	return o.Get || o.Connection || o.Paginate ||
		hasAnyMutationOp(o) || o.HardDelete || o.SoftDelete || o.Restore
}

func hasAnyMutationOp(o ResolvedOperations) bool {
	return o.Create || o.CreateMany || o.Update || o.UpdateWhere || o.Upsert
}

// buildOptions assembles the per-run Options from the loaded config. The
// SQLGEN_OUTPUT_DIR_IMPORT_OVERRIDE env var, when set, overrides the dir used
// for API import-path computation only — the *write* dir continues to follow
// cfg.Output.Dir. Used by the E2E golden harness to keep import paths stable
// when output is redirected to a temp directory.
func buildOptions(cfg *config.RootConfig, version, writeRoot string) *Options {
	importOutputDir := cfg.Output.Dir
	if override := os.Getenv("SQLGEN_OUTPUT_DIR_IMPORT_OVERRIDE"); override != "" {
		importOutputDir = override
	}
	return &Options{
		Version: version,
		// Only the write destination moves under a redirect. OriginalOutputDir
		// stays on the configured dir so apiImportDir keeps computing the
		// consumer's models import path from where the package really lives —
		// without that split, a redirected run bakes the throwaway dir into
		// the graph package's import block and the tree stops compiling.
		OutputDir:         SandboxPath(writeRoot, cfg.Output.Dir),
		OriginalOutputDir: importOutputDir,
		WriteRoot:         writeRoot,
		// Root-relative graph dirs (§26.5.8) resolve against the module root
		// sqlgen runs in — the cwd, the same anchor output.dir writes assume.
		ProjectRoot: ".",
		Package:     cfg.Output.Package,
	}
}

// resolveGraphDir resolves a root-relative GraphQL output directory
// (api.graphql.schema_dir / resolver_dir, §26.5.8) against the project root.
// Absolute paths are returned verbatim; relative paths are joined onto
// projectRoot — the module root, the same anchor output.dir writes assume.
//
// Unlike the retired output.dir-relative resolution, graph is NOT pinned under
// output.dir: with projectRoot "." a top-level `schema_dir: graph` resolves to
// `graph` (a sibling of the models tree), while the nested default
// `<output.dir>/graph` resolves right back under output.dir. The topology is
// entirely a function of the resolved path, with no mode flag.
func resolveGraphDir(projectRoot, dir string) string {
	if filepath.IsAbs(dir) {
		return dir
	}
	return filepath.Join(projectRoot, dir)
}

// apiImportDir returns the dir used to compute the consumer's models package
// import path. Prefers opts.OriginalOutputDir when set so callers redirecting
// the *write* location (e.g. the E2E golden harness rewriting output.dir to a
// per-test temp directory via SQLGEN_OUTPUT_DIR_IMPORT_OVERRIDE) keep import
// paths stable; otherwise falls back to cfg.Output.Dir for the default case
// where write and import paths agree.
func apiImportDir(opts *Options, cfg *config.RootConfig) string {
	if opts.OriginalOutputDir != "" {
		return opts.OriginalOutputDir
	}
	return cfg.Output.Dir
}

// scalarImports returns the deduped, sorted import paths needed by the
// generated `graph/scalars_gen.go`.
//
// Derived from builtInScalarRegistry rather than from a switch on the scalar
// name: an entry declares the import its bound Go type needs (`Import`) and
// any the marshaler BODY needs on top (`MarshalerImports`, e.g.
// `encoding/base64` for Bytes), so adding a scalar cannot leave its import
// behind. One registry replaces several hand-maintained lists that adding a
// scalar would otherwise have to touch in step.
//
// Keyed on the Go type rather than the scalar name because the registry is —
// `types.JSON` and `json.RawMessage` are two entries producing one `JSON`
// scalar with different imports, and only the resolved binding distinguishes
// them. APIScalarUse.GoImport carries the column's own resolved import, which
// is what a third-party binding (uuid, decimal) needs and the registry cannot
// know.
func scalarImports(externals []APIScalarUse) []string {
	base := []string{
		"fmt",
		"io",
		"strconv",
		"github.com/99designs/gqlgen/graphql",
	}
	for _, s := range externals {
		if s.GoImport != "" {
			base = append(base, s.GoImport)
		}
		reg, ok := builtInScalarRegistry[s.GoType]
		if !ok {
			continue
		}
		if reg.Import != "" {
			base = append(base, reg.Import)
		}
		base = append(base, reg.MarshalerImports...)
	}
	return UniqueImports(base)
}

// BuildTableContextsFromSchema returns only the table half of
// BuildEntityContextsFromSchema. Retained as the narrower entry point for
// callers with no view surface to build.
func BuildTableContextsFromSchema(schema *parser.Schema, cfg *config.RootConfig) ([]TableContext, error) {
	tables, _, err := BuildEntityContextsFromSchema(schema, cfg)
	return tables, err
}

// BuildEntityContextsFromSchema is the public entry point used by the
// `sqlgen graphql gen` wrapper subcommand. It runs the same prep steps as
// Generate (resolver, collisions, schema-type registration, tenancy
// attachment, relationship-options dedup) up to the point of producing
// the table AND view contexts the API context builder consumes. Templates and
// the per-entity file emission step are skipped — the wrapper only needs the
// data, not the resolver-level files.
//
// The view contexts are returned rather than discarded because views are part
// of the GraphQL surface (PRD §26.4 "Views on the GraphQL surface"): the
// wrapper's `models:` merge binds a view's four envelope types exactly as it
// binds a table's, and it can only do that if BuildAPIContext saw the views.
//
// Tenancy is attached to views as well as tables, matching Generate. A view's
// GraphQL surface reads no tenancy metadata of its own — the resolver calls the
// generated client method, which carries the §29.2.5 predicate itself — but the
// attach is still run so the package-level tenant type this path resolves is the
// one Generate resolves, including when the only tenanted entity is a view.
func BuildEntityContextsFromSchema(schema *parser.Schema, cfg *config.RootConfig) ([]TableContext, []ViewContext, error) {
	input, collisions := newContextBuildInput(schema, cfg)

	tenancyMap, _, err := BuildTenancyContext(cfg, schema, input.Resolver)
	if err != nil {
		return nil, nil, fmt.Errorf("building tenancy context: %w", err)
	}

	contexts, err := BuildTableContexts(input, collisions)
	if err != nil {
		return nil, nil, fmt.Errorf("building table contexts: %w", err)
	}

	// The package-scope name rules run here too. `sqlgen graphql gen` reaches
	// this entry point without going through Generate or ValidateGeneration, so
	// without this the one path that emits `<stem>_gen.graphqls` files would be
	// the one path that never checked whether two entities share a stem.
	viewContexts, err := BuildViewContexts(input, collisions)
	if err != nil {
		return nil, nil, fmt.Errorf("building view contexts: %w", err)
	}
	ctxs := generationContexts{
		enums:  BuildEnumContexts(schema, collisions, cfg),
		sets:   BuildSetContexts(schema, collisions),
		types:  BuildTypeContext(input, collisions),
		tables: contexts,
		views:  viewContexts,
	}
	if err := validateResolvedPackage(cfg, ctxs); err != nil {
		return nil, nil, err
	}

	// Run here too so a context this function hands back is the same shape
	// Generate builds. The wrapper emits no table file today, but a returned
	// context with an empty PKAutoGenExpr is a trap for whoever renders one
	// next (PRD §7.4).
	attachUUIDGeneration(contexts, selectUUIDIntegration(cfg, ctxs))

	attachTenancyToTables(contexts, tenancyMap)
	// Views need the attach too, though this path renders no view file:
	// finalizeTenancyWiring draws the package's uniform tenant type from any
	// tenanted entity, and a package whose only tenanted entity is a view
	// (§29.2.5) would otherwise resolve a different ExplicitTenantGoType here
	// than under Generate.
	attachTenancyToViews(viewContexts, tenancyMap)
	contexts = deduplicateRelationshipOptionsDefs(contexts)
	if err := wireRelationshipFilters(contexts, viewContexts); err != nil {
		return nil, nil, fmt.Errorf("building relationship filters: %w", err)
	}
	if err := validateDiscriminatorBindings(contexts, viewContexts, cfg.Input.Dialect, input.Resolver); err != nil {
		return nil, nil, fmt.Errorf("validating relationship discriminators: %w", err)
	}
	// The §9.9.4 hard error reaches `sqlgen graphql gen` too: this entry point
	// bypasses both Generate and ValidateGeneration, so a
	// check only they perform is one this path reports clean on.
	if err := ValidateNestedWriteEligibility(contexts, cfg, input.Resolver); err != nil {
		return nil, nil, fmt.Errorf("validating nested mutations: %w", err)
	}
	wireNestedMutations(contexts, cfg, input.Resolver)
	finalizeTenancyWiring(contexts, viewContexts)
	return contexts, viewContexts, nil
}

// newContextBuildInput performs the prep every context-building entry point
// needs: the type resolver, the cross-schema name-collision set, and the
// schema-declared types registered on the resolver.
func newContextBuildInput(schema *parser.Schema, cfg *config.RootConfig) (*GenerateInput, map[string]bool) {
	usePointers := cfg.Overrides.UsePointers == nil || *cfg.Overrides.UsePointers
	resolver := gotype.NewResolver(cfg.Input.Dialect, usePointers, cfg.Overrides.Types)
	collisions := computeNameCollisions(schema)
	registerSchemaTypes(resolver, schema, cfg, collisions)

	return &GenerateInput{
		Schema:   schema,
		Config:   cfg,
		Resolver: resolver,
	}, collisions
}

// ValidateGeneration runs every generation-phase check that can reject a
// config or a schema, and returns the warnings those checks raise. No
// templates are loaded and no files are written — it builds the contexts,
// reports what they refuse, and discards them.
//
// It exists so `sqlgen validate` can answer for the whole pipeline rather
// than only for the two config-validation passes. Before it,
// `validate` stopped at ValidatePostParse and printed "config and schema are
// valid" for configs that `generate` then rejected — a resolved-field-name
// collision (PRD §8.5), a per-column type literal with no importable package,
// an ambiguous soft-delete column, an unresolvable `api.operations` mask,
// a bad tenancy declaration, an o2o join that cannot be built, or an API
// context that will not assemble. All of those are config or schema errors,
// so validate time is where they belong.
//
// The cost is a full in-memory context build: sub-millisecond on the largest
// example schema in the tree, because the expensive halves of `generate`
// (template parsing, rendering, formatting, file IO) are not on this path.
func ValidateGeneration(schema *parser.Schema, cfg *config.RootConfig) ([]string, error) {
	input, collisions := newContextBuildInput(schema, cfg)

	// Errors accumulate: validate reports everything it can see in one pass.
	// A failure in one stage does not stop the next unless the next needs its
	// output — only the API context does, and it is skipped when the table
	// contexts did not build.
	var errs []error

	tenancyMap, tenancyWarnings, err := BuildTenancyContext(cfg, schema, input.Resolver)
	input.Warnings = append(input.Warnings, tenancyWarnings...)
	if err != nil {
		errs = append(errs, fmt.Errorf("building tenancy context: %w", err))
	}

	tableContexts, err := BuildTableContexts(input, collisions)
	if err != nil {
		errs = append(errs, err)
	}

	viewContexts, err := BuildViewContexts(input, collisions)
	if err != nil {
		errs = append(errs, err)
	}

	enumContexts := BuildEnumContexts(schema, collisions, cfg)

	// The package-scope name rules need every category at once, and the
	// categories that did not build contribute nothing rather than blocking
	// the ones that did.
	setContexts := BuildSetContexts(schema, collisions)
	err = validateResolvedPackage(cfg, generationContexts{
		enums:  enumContexts,
		sets:   setContexts,
		types:  BuildTypeContext(input, collisions),
		tables: tableContexts,
		views:  viewContexts,
	})
	if err != nil {
		errs = append(errs, err)
	}

	if tableContexts != nil {
		// The post-build wiring the generate path runs that can *reject* a
		// config, mirrored here in the same order and for the same reason
		// validate exists at all: a check that only `generate` performs is a
		// check `validate` reports clean on. attachUUIDGeneration is the one
		// pass deliberately left out — it rejects nothing, and validate
		// renders no table file, so an unfilled PKAutoGenExpr is unobservable
		// here (PRD §7.4).
		//
		// wireRelationshipFilters is the one that can reject a config outright
		// (a relationship `filter:` containing `$` — PRD §11.1), and it reads
		// the *target's* tenancy, so the two attach passes have to precede it.
		// Running it also means the API context below sees the same
		// FilterRelationships `generate` would build, rather than an empty set —
		// which is what keeps a future relationship-aware §26.5.3 completeness
		// lint from silently no-opping under validate.
		attachTenancyToTables(tableContexts, tenancyMap)
		attachTenancyToViews(viewContexts, tenancyMap)
		tableContexts = deduplicateRelationshipOptionsDefs(tableContexts)
		if err := wireRelationshipFilters(tableContexts, viewContexts); err != nil {
			errs = append(errs, fmt.Errorf("building relationship filters: %w", err))
		}
		if err := validateDiscriminatorBindings(tableContexts, viewContexts, cfg.Input.Dialect, input.Resolver); err != nil {
			errs = append(errs, fmt.Errorf("validating relationship discriminators: %w", err))
		}

		// PRD §9.9.4 makes ValidateNestedWriteEligibility answerable under
		// `sqlgen validate` as well as `sqlgen generate`: an explicitly-listed
		// ineligible edge is a hard error, and a check only `generate`
		// performs is one `validate` reports clean on.
		if err := ValidateNestedWriteEligibility(tableContexts, cfg, input.Resolver); err != nil {
			errs = append(errs, fmt.Errorf("validating nested mutations: %w", err))
		}
		wireNestedMutations(tableContexts, cfg, input.Resolver)
		finalizeTenancyWiring(tableContexts, viewContexts)

		// viewContexts threads in so `sqlgen validate` answers the view half of
		// the API rules too — a view/table GraphQL type-name collision, a view
		// whose walker disagrees with its schema type, an unresolvable
		// `views.<n>.api.operations` preset. PRD §4.13 note 3 makes validate the
		// place every phase-3 rule is answered without writing a file.
		apiCtx, err := BuildAPIContext(tableContexts, viewContexts, enumContexts, setContexts, cfg)
		if err != nil {
			errs = append(errs, fmt.Errorf("building api context: %w", err))
		}
		input.Warnings = append(input.Warnings, unusedScalarWarnings(cfg, apiCtx)...)
		input.Warnings = append(input.Warnings, apiOverReachWarnings(cfg, tableContexts, apiCtx)...)
		input.Warnings = append(input.Warnings, flatUpsertOmittedWarnings(cfg, tableContexts, apiCtx)...)
	}

	return input.Warnings, errors.Join(errs...)
}

// resolveDialect returns the sql.Dialect implementation for the given dialect name.
func resolveDialect(name config.Dialect) sql.Dialect {
	switch name {
	case config.DialectMySQL:
		return sql.NewMySQLDialect()
	case config.DialectSQLite:
		return sql.NewSQLiteDialect()
	default:
		return sql.NewPostgresDialect()
	}
}

// registerSchemaTypes registers schema enums, sets, and domains with the resolver
// so columns using these types resolve without explicit type_map overrides.
//
// Enums resolve through EnumGoTypeName rather than StructName so an
// `enums.<name>.struct_name` override reaches the COLUMN as well as the
// generated type (PRD §4.11). The two must agree: a column resolving to the
// derived spelling while the type is generated under the overridden one emits
// a models package that does not compile.
func registerSchemaTypes(resolver *gotype.Resolver, schema *parser.Schema, cfg *config.RootConfig, collisions map[string]bool) {
	for _, e := range schema.Enums {
		resolver.RegisterEnum(e.Name, EnumGoTypeName(cfg, e.Name, e.Schema, collisions))
	}
	for _, s := range schema.Sets {
		resolver.RegisterSet(s.Name, StructName(s.Name, s.Schema, collisions))
	}
	for _, d := range schema.DomainTypes {
		resolver.RegisterDomain(d.Name, d.BaseType)
	}
}

// ComputeNameCollisions is the exported entry point for the unexported
// `computeNameCollisions` helper, used by cli callers that build a parser
// schema directly (e.g. the `sqlgen graphql gen` subcommand) and need the
// same disambiguation set the main `Generate` pipeline computes.
func ComputeNameCollisions(schema *parser.Schema) map[string]bool {
	return computeNameCollisions(schema)
}

// computeNameCollisions returns the set of bare SQL names that need a schema
// prefix to disambiguate: those whose *resolved* Go name is claimed by objects
// in more than one schema, across all schema object categories (tables, enums,
// SET types, views, composite types, domain types).
//
// The grouping is by resolved name rather than by the SQL name, and the two are
// not the same relation — resolution is many-to-one. `public.people` and
// `audit.person` are different strings that both land on Person, so grouping by
// the string left them unprefixed and colliding, in a package that then did not
// compile (PRD §5.5).
//
// Within a naming family this only widens: equal SQL names always resolve
// equally, so every prefix the SQL-name grouping produced is still produced.
// *Across* families it can also narrow, because composites and domains are not
// singularized — a `public.addresses` table and an `audit.addresses` composite
// grouped as one SQL name but resolve to Address and Addresses, so the table no
// longer takes a prefix it never needed. That is the correct answer: they were
// two distinct Go names all along.
//
// The result stays keyed by the SQL name, which is what StructName and
// SnakeName are handed. Only the grouping moved.
//
// Cross-schema duplicates are disambiguated here; a duplicate *within* one
// schema has no prefix to reach for and is rejected by validateResolvedNames.
func computeNameCollisions(schema *parser.Schema) map[string]bool {
	// resolved Go name → schemas claiming it → the SQL names that resolve to it
	claims := make(map[string]map[string]bool)
	sqlNames := make(map[string]map[string]bool)
	add := func(resolved, sqlName, s string) {
		if claims[resolved] == nil {
			claims[resolved] = make(map[string]bool)
			sqlNames[resolved] = make(map[string]bool)
		}
		claims[resolved][s] = true
		sqlNames[resolved][sqlName] = true
	}
	// Tables, views, enums and SET types resolve through StructName, so their
	// resolved name is the singularized PascalCase form. Composite and domain
	// types are spelled by toPascalCase alone (buildCompositeContexts /
	// buildDomainContexts do not singularize), so grouping them the same way
	// would compare against a name nothing ever emits.
	for _, t := range schema.Tables {
		add(StructName(t.Name, "", nil), t.Name, t.Schema)
	}
	for _, e := range schema.Enums {
		add(StructName(e.Name, "", nil), e.Name, e.Schema)
	}
	for _, s := range schema.Sets {
		add(StructName(s.Name, "", nil), s.Name, s.Schema)
	}
	for _, v := range schema.Views {
		add(StructName(v.Name, "", nil), v.Name, v.Schema)
	}
	for _, c := range schema.CompositeTypes {
		add(toPascalCase(c.Name), c.Name, c.Schema)
	}
	for _, d := range schema.DomainTypes {
		add(toPascalCase(d.Name), d.Name, d.Schema)
	}

	colliding := make(map[string]bool)
	for resolved, schemas := range claims {
		if len(schemas) > 1 {
			maps.Copy(colliding, sqlNames[resolved])
		}
	}
	return colliding
}

// buildTableNameFileContext builds the TableNameFileContext from the built
// contexts rather than from the schema.
//
// The contexts are the set of entities that actually generate: BuildTableContexts
// drops an `exclude_tables` match and a table with no resolved primary key
// (PRD §6.4, §9.4b), and neither produces a model, a client, a cache entry or an
// event hook. Declaring a `hook.TableName` for one was dead weight — there is no
// hook to register against it — and it put entities in the constant namespace
// that nothing else in the package could see, so a PK-less `people` beside a
// generated `person` emitted `TablePeople` twice and no resolved-name check
// could reach it. Reading the contexts makes the file and validateResolvedNames
// the same list, so they cannot disagree.
//
// Each constant's value is qualifiedTableName(schema, name) — "public.orders"
// on PostgreSQL, the bare name on MySQL / SQLite (PRD §8.5). The identifier
// rule above keeps two same-named tables apart in Go; the value has to keep
// them apart at runtime too, because hook.ForMutation / ForTable and the cache
// facade compare it and nothing else. It is computable from
// (schema, table) alone, which is what lets cache.FromEventSubscriber rebuild
// it from an event.
func buildTableNameFileContext(tables []TableContext, views []ViewContext, pkg string) TableNameFileContext {
	tableEntries := make([]TableNameEntry, 0, len(tables))
	for _, tc := range tables {
		tableEntries = append(tableEntries, TableNameEntry{
			ConstantName: tc.TableNameConstant,
			SQLName:      tc.TableName,
			Value:        qualifiedTableName(tc.Schema, tc.TableName),
		})
	}

	viewEntries := make([]TableNameEntry, 0, len(views))
	for _, vc := range views {
		viewEntries = append(viewEntries, TableNameEntry{
			ConstantName: vc.TableNameConstant,
			SQLName:      vc.ViewName,
			Value:        qualifiedTableName(vc.Schema, vc.ViewName),
		})
	}

	return TableNameFileContext{
		Package: pkg,
		Imports: []string{"github.com/teandresmith/sqlgen/hook"},
		Tables:  tableEntries,
		Views:   viewEntries,
	}
}

// FirstTenantedEntityType returns the uniform tenant Go type + import from the
// first tenanted entity context (§29.2.4 guarantees uniformity across tables
// *and* views), or empty strings when nothing is tenanted. Contexts arrive
// sorted, so the answer is deterministic.
//
// Exported because it is the *definition* of "this package emits
// CallOptions.Tenant": sharedTypeDefinitions gates the field on a non-empty
// result, and the manifest's conventions.call_options.fields list must gate its
// entry on the same answer. Two implementations of one predicate is the drift
// the list already shipped once (a `Tx` field that never existed), so the
// manifest calls this rather than re-deriving it.
//
// Views are scanned because they can be the only tenanted entities in a
// package: a schema whose tables all opt out but whose views carry the tenant
// column still needs the concretely-typed `CallOptions.Tenant` field, or
// §29.2.5's "CallOptions parity" promise is unmet and the view client's
// resolveTenant has no explicit-tenant field to read.
func FirstTenantedEntityType(tables []TableContext, views []ViewContext) (goType, importPath string) {
	for _, t := range tables {
		if t.Tenancy != nil && t.Tenancy.Tenanted {
			return t.Tenancy.GoType, t.Tenancy.Import
		}
	}
	for _, v := range views {
		if v.Tenancy != nil && v.Tenancy.Tenanted {
			return v.Tenancy.GoType, v.Tenancy.Import
		}
	}
	return "", ""
}
