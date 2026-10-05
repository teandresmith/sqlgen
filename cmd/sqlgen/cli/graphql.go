package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/wrapper"
	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/sql"
)

// newGraphQLCmd builds the `sqlgen graphql` parent command and its
// `init` / `gen` subcommands. Both subcommands honour the persistent
// --config flag from the root command so the consumer's sqlgen.yml drives
// gqlgen path defaults.
func newGraphQLCmd(flags *cliFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "graphql",
		Short: "GraphQL wrapper subcommands (init / gen)",
	}
	cmd.AddCommand(newGraphQLInitCmd(flags))
	cmd.AddCommand(newGraphQLGenCmd(flags))
	return cmd
}

// newGraphQLInitCmd builds the one-shot scaffold subcommand. The scaffold
// only writes when no gqlgen.yml exists at the configured path; rerunning
// is a no-op (no overwrite, no error).
func newGraphQLInitCmd(flags *cliFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Scaffold a starter gqlgen.yml (no-op if the file already exists)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.LoadConfig(flags.config)
			if err != nil {
				return &exitError{code: ExitConfig, err: err}
			}
			cfgPath := graphQLConfigPath(cfg)
			if err := wrapper.Init(cfgPath); err != nil {
				return &exitError{code: ExitGeneration, err: err}
			}
			if !flags.quiet {
				out := cmd.OutOrStdout()
				_, _ = fmt.Fprintf(out, "sqlgen graphql init: %s ready\n", cfgPath)
				_, _ = fmt.Fprintln(out, "Note: 'sqlgen graphql gen' uses 'go run github.com/99designs/gqlgen' by default and")
				_, _ = fmt.Fprintln(out, "      sets GOFLAGS=-mod=mod on the subprocess so gqlgen's transitive build deps")
				_, _ = fmt.Fprintln(out, "      resolve on demand without needing entries in your go.sum.")
				_, _ = fmt.Fprintln(out, "      To pin them in CI, run 'go get -tool github.com/99designs/gqlgen'.")
				_, _ = fmt.Fprintln(out, "")
				_, _ = fmt.Fprintln(out, "Resolver wiring: sqlgen-managed query/mutation logic lives in a sub-package")
				_, _ = fmt.Fprintln(out, "      at <resolver_dir>/sqlgenresolver/. gqlgen's own one-shot scaffold writes")
				_, _ = fmt.Fprintln(out, "      <resolver_dir>/resolver.go with an empty Resolver struct; AFTER the gqlgen")
				_, _ = fmt.Fprintln(out, "      subprocess returns, 'sqlgen graphql gen' AST-merges the Client / Q / M fields")
				_, _ = fmt.Fprintln(out, "      plus the sqlgenresolver import so seed delegations compile. Initialize all")
				_, _ = fmt.Fprintln(out, "      three at app boot with the same *<modelspkg>.Client used elsewhere. The")
				_, _ = fmt.Fprintln(out, "      merge preserves consumer-added fields, methods, and imports verbatim.")
			}
			return nil
		},
	}
}

// newGraphQLGenCmd builds the wrapper-invocation subcommand. It runs the
// full sqlgen pipeline (config → schema → API context), derives the
// sqlgen-owned merge entries, and hands them off to wrapper.Gen which
// invokes gqlgen as a subprocess against a temp config.
func newGraphQLGenCmd(flags *cliFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "gen",
		Short: "Run gqlgen against a merged config (sqlgen + consumer entries)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			errOut := cmd.ErrOrStderr()

			cfg, schema, err := loadAndValidate(flags, errOut)
			if err != nil {
				return err
			}
			if cfg.API == nil || !cfg.API.Enabled || cfg.API.GraphQL == nil || !cfg.API.GraphQL.Enabled {
				err := fmt.Errorf("api.graphql.enabled must be true to run 'sqlgen graphql gen'")
				_, _ = fmt.Fprintf(errOut, "Error: %v\n", err)
				return &exitError{code: ExitConfig, err: err}
			}

			_, err = runGraphQLGen(cmd.Context(), cfg, schema, runGraphQLGenOptions{
				Stdout: cmd.OutOrStdout(),
				Stderr: errOut,
			})
			return err
		},
	}
}

// runGraphQLGenOptions carries the IO and contextual hooks runGraphQLGen
// needs without coupling it to *cobra.Command. The chained `sqlgen generate`
// path and the dedicated `sqlgen graphql gen` subcommand both call
// runGraphQLGen — the chained path passes its already-loaded cfg + schema
// instead of re-running loadAndValidate.
type runGraphQLGenOptions struct {
	Stdout io.Writer
	Stderr io.Writer
}

// runGraphQLGen builds the sqlgen-owned API context from cfg + schema,
// derives the gqlgen merge entries, seeds the resolver delegation files,
// and invokes the gqlgen subprocess. Returns the wrapper.GenResult so
// callers (the chained `sqlgen generate` path) can append the
// emitted files to their reporting.
//
// Pre-flight: the consumer's gqlgen.yml must exist at graphQLConfigPath(cfg).
// When absent, runGraphQLGen returns an *exitError pointing the consumer at
// `sqlgen graphql init` rather than auto-scaffolding — config files are a
// deliberate one-shot, not a side-effect of generate.
//
// Failure mode: when the gqlgen subprocess fails the sqlgen-side artifacts
// emitted earlier in the run (models, schema, translators) stay on disk; the
// caller is responsible for surfacing the error loudly. We do not roll back
// the partial generation — the consumer fixes the gqlgen issue and re-runs.
func runGraphQLGen(ctx context.Context, cfg *config.RootConfig, schema *parser.Schema, opts runGraphQLGenOptions) (*wrapper.GenResult, error) {
	errOut := opts.Stderr
	if errOut == nil {
		errOut = io.Discard
	}

	cfgPath := graphQLConfigPath(cfg)
	if _, statErr := os.Stat(cfgPath); statErr != nil {
		if os.IsNotExist(statErr) {
			err := fmt.Errorf("api.graphql.enabled is true but %s is missing — run 'sqlgen graphql init' to scaffold a starter gqlgen.yml", cfgPath)
			_, _ = fmt.Fprintf(errOut, "Error: %v\n", err)
			return nil, &exitError{code: ExitConfig, err: err}
		}
		err := fmt.Errorf("checking %s: %w", cfgPath, statErr)
		_, _ = fmt.Fprintf(errOut, "Error: %v\n", err)
		return nil, &exitError{code: ExitConfig, err: err}
	}

	tableCtxs, viewCtxs, err := gen.BuildEntityContextsFromSchema(schema, cfg)
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "Building entity contexts: %v\n", err)
		return nil, &exitError{code: ExitGeneration, err: err}
	}
	collisions := gen.ComputeNameCollisions(schema)
	enumCtxs := gen.BuildEnumContexts(schema, collisions, cfg)
	setCtxs := gen.BuildSetContexts(schema, collisions)
	apiCtx, err := gen.BuildAPIContext(tableCtxs, viewCtxs, enumCtxs, setCtxs, cfg)
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "Building API context: %v\n", err)
		return nil, &exitError{code: ExitGeneration, err: err}
	}
	if apiCtx == nil {
		return nil, &exitError{code: ExitConfig, err: fmt.Errorf("api context is empty — nothing to generate")}
	}
	// BuildAPIContext leaves the resolver-side fields blank; the
	// seed/merge templates need ModelsPackage / ClientName /
	// ModelsImportPath / SqlgenResolverPkgName populated to render
	// qualified type references. The `sqlgen generate` orchestrator wires
	// these via populateAPIContextResolverFields; the wrapper path has to
	// mirror that explicitly so seeds emit `models.Create<T>Input` /
	// `sqlgenresolver.IncrementOp` instead of `.Create<T>Input` /
	// `.IncrementOp`.
	gen.PopulateAPIContextResolverFields(apiCtx, cfg, cfg.Output.Dir)

	module := readModulePath(".")
	input, mergeWarnings := buildMergeInput(cfg, apiCtx, module)
	printManifestWarnings(errOut, mergeWarnings, false)

	seeds, managed, err := buildSeedsAndManagedFields(apiCtx, cfg)
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "Building seed delegations: %v\n", err)
		return nil, &exitError{code: ExitGeneration, err: err}
	}

	stdout := opts.Stdout
	if stdout == nil {
		stdout = io.Discard
	}
	res, err := wrapper.Gen(ctx, wrapper.GenOptions{
		GqlgenConfigPath:    cfgPath,
		GqlgenBin:           cfg.API.GraphQL.GqlgenBin,
		MergeInput:          input,
		Seeds:               seeds,
		SqlgenManagedFields: managed,
		SeedPackage:         cfg.API.GraphQL.Package,
		SeedImports: wrapper.SeedImports{
			ModelsAlias:              cfg.Output.Package,
			ModelsImportPath:         apiCtx.ModelsImportPath,
			SqlgenResolverImportPath: apiCtx.SqlgenResolverImportPath,
			// The seed bodies reference gqlgen-emitted input
			// types under apiCtx.GqlgenModelAlias; the rendered file
			// preamble must include the matching aliased import.
			GqlgenModelImportPath: apiCtx.GqlgenModelImportPath,
			GqlgenModelAlias:      apiCtx.GqlgenModelAlias,
		},
		Stdout: stdout,
		Stderr: errOut,
	})
	if err != nil {
		if _, ok := errors.AsType[*wrapper.UnboundRootFieldsError](err); ok {
			// gqlgen ran; it is the naming handoff that failed.
			_, _ = fmt.Fprintf(errOut, "Error: %v\n", err)
			return nil, &exitError{code: ExitGeneration, err: err}
		}
		_, _ = fmt.Fprintf(errOut, "Error: gqlgen subprocess failed: %v; sqlgen-side artifacts emitted at %s\n", err, cfg.Output.Dir)
		return nil, &exitError{code: ExitGeneration, err: err}
	}
	return res, nil
}

// graphQLConfigPath returns the configured gqlgen.yml path, falling back
// to the §26.3 default when unset.
func graphQLConfigPath(cfg *config.RootConfig) string {
	if cfg.API != nil && cfg.API.GraphQL != nil && cfg.API.GraphQL.GqlgenConfig != "" {
		return cfg.API.GraphQL.GqlgenConfig
	}
	return "./gqlgen.yml"
}

// buildMergeInput translates the API context + config into the wrapper's
// MergeInput. Consumer-authored entries in gqlgen.yml take precedence on
// any key collision; this function only emits sqlgen-derived bindings.
//
// Row structs and scalars are aliased here so the
// gqlgen-generated resolver interfaces (`QueryResolver` / `MutationResolver`)
// derive Go types from the consumer's models package — without those aliases
// gqlgen autobinds to its own `graph/model/` autogen dir and our resolver
// signatures (which return `*<consumer-models-pkg>.<Table>`) fail to satisfy
// the interface.
//
// Envelope binding. Per PRD §26.5.6, the GraphQL envelope types
// (`<Table>Connection`, `<Table>Edge`, `<Table>ListResult`, and the shared
// non-generic `PageInfo`) bind to runtime generic types parameterized by the
// consumer's row struct. The codegen emits per-table type aliases into the
// consumer's models package (`type <T>Connection = Connection[<T>]`,
// `type <T>Edge = Edge[<T>]`, `type <T>ListResult = PaginateResult[<T>]` —
// Go 1.24+ generic type aliases), and the wrapper merges
// `<T>Connection: { model: <pkg>.<T>Connection }` etc. into gqlgen.yml.
// gqlgen's binder calls `code.Unalias()` on resolved types so each alias
// resolves to the concrete instantiated struct at zero cost. `PageInfo` is
// non-generic so it binds directly to `<pkg>.PageInfo`.
//
// All envelope levels are bound so gqlgen reuses the runtime types
// throughout the connection traversal: the seed resolver's
// `r.Q.<T>s(...) → *Connection[<T>]` and `r.Q.<T>List(...) → *PaginateResult[<T>]`
// return values flow through unchanged with no `translate<T>Connection` /
// `translate<T>Edge` / `translate<T>ListResult` helpers emitted.
//
// The bracketed instantiation form (`<pkg>.Connection[<pkg>.<T>]`) is not
// used in `model:` paths because gqlgen's `internal/code/util.go::PkgAndType`
// splits on `.` and fails on the bracket form (verified empirically against
// gqlgen v0.17.90). The runtime / schema shapes stay field-for-field aligned
// so each alias resolves to the shape gqlgen expects.
//
// Gqlgen-autogen input types (`Create<Table>Input`, `Update<Table>Input`,
// `<Table>Filter`, `<Table>Sort`) stay gqlgen-owned; our resolver
// template uses the gqlgen-autogen versions as method parameters and
// translates them to `*<pkg>.<Op><Table>Input` (omittable) via
// `translateCreate*Input` / `translateUpdate*Input` helpers. Aliasing them to
// consumer models would force the consumer's `omittable.Value[T]` fields to
// round-trip through gqlgen's JSON unmarshaler, which is not compatible.
// The returned warnings name conditions that leave the merged config unable to
// express something sqlgen generated — today only an unspellable marshaler
// anchor. They are advisory: generation proceeds, because the same
// unresolvable module path already degrades the category-4 scalar bindings.
func buildMergeInput(cfg *config.RootConfig, apiCtx *gen.APIContext, modulePath string) (wrapper.MergeInput, []string) {
	models := make(map[string][]string)

	tablePkg := tableModelPackage(modulePath, cfg.Output.Dir)
	for _, t := range apiCtx.Tables {
		if tablePkg == "" {
			continue
		}
		qualifiedRow := tablePkg + "." + t.StructName
		models[t.StructName] = []string{qualifiedRow}
		models[t.StructName+"Connection"] = []string{qualifiedRow + "Connection"}
		models[t.StructName+"Edge"] = []string{qualifiedRow + "Edge"}
		models[t.StructName+"ListResult"] = []string{qualifiedRow + "ListResult"}
	}
	if tablePkg != "" && len(apiCtx.Tables) > 0 {
		models["PageInfo"] = []string{tablePkg + ".PageInfo"}
	}

	for _, s := range apiCtx.UsedScalars {
		if path := scalarModelPath(s, apiCtx.ResolverImportPath); path != "" {
			models[s.Name] = []string{path}
		}
	}

	// Schema-declared enums bind directly to the consumer's models package so
	// gqlgen marshals values through the named Go type round-trip. Without
	// this, gqlgen would treat the GraphQL enum as an unbound type and emit
	// a custom resolver stub for every enum-typed field (`panic("not
	// implemented: <Field>")`). The bound named type
	// already implements UnmarshalGQL / MarshalGQL through the codegen path
	// in `templates/enum.go.tmpl`, so gqlgen's generic enum machinery wires
	// up without a per-table resolver method.
	//
	// List-shaped columns get a SECOND Go type per GraphQL binding, which is
	// what lets a `[<Enum>!]!` field target a named slice directly instead of
	// falling back to a resolver stub. Two shapes reach this: a PostgreSQL
	// enum array, whose slice sibling is `<EnumGoType>Slice`
	// (`templates/enum.go.tmpl`), and a MySQL SET, where the GraphQL enum is
	// the value type and the slice IS the column's Go type
	// (`templates/set.go.tmpl`). Both emit their own MarshalGQL /
	// UnmarshalGQL next to the bare type's, so gqlgen marshals each list value
	// through the same wire identifier round-trip the scalar case uses. The
	// bare-type entry is intentionally listed first so gqlgen picks it for
	// non-list contexts (scalar args, list ELEMENTS in generated inputs).
	if tablePkg != "" {
		for _, e := range apiCtx.UsedEnums {
			paths := []string{tablePkg + "." + e.GoTypeName}
			if e.SliceGoTypeName != "" {
				paths = append(paths, tablePkg+"."+e.SliceGoTypeName)
			}
			models[e.GraphQLName] = paths
		}
	}

	// Numeric-width anchors (read side): a row-struct field whose numeric width gqlgen ships
	// no marshaler for binds to nothing and degrades to a silent panic
	// resolver. Appending sqlgen's own anchors to the spec scalar's list is
	// what makes it bind. gqlgen's entries are restated FIRST — `injectBuiltins`
	// skips any key already present, and `Model[0]` is what every GENERATED
	// position (input field, resolver argument) binds to, so a narrowed width
	// in front would retype every `Int` input field in the schema.
	//
	// Never overwrites: `Int` / `Float` are reserved scalar names
	// (config.ReservedScalarNames), so the only way one of these keys is
	// already taken is a table or enum whose Go name IS `Int`, and clobbering
	// its row-struct binding would be a far worse failure than the panic
	// resolver this avoids.
	widthModels, warnings := numericWidthModels(apiCtx)
	for scalar, paths := range widthModels {
		if _, taken := models[scalar]; !taken {
			models[scalar] = paths
		}
	}

	return wrapper.MergeInput{
		SchemaGlob: schemaGlob(cfg),
		Models:     models,
		// sqlgen's translators and resolver seeds reference the Go
		// identifiers on gqlgen-generated types by name, so sqlgen states
		// those names outright rather than trying to reproduce whatever
		// gqlgen's own capitalizer would have produced from the schema
		// (§26.5.6 "Go field naming"), and states the Go type outright for the
		// input fields whose width gqlgen cannot reconcile.
		ModelFields: modelFieldOverrides(apiCtx),
	}, warnings
}

// gqlgenSpecNumericModels restates gqlgen's own `models:` entries for the two
// spec numeric scalars, in gqlgen's own order.
//
// Restating is forced: `codegen/config/config.go::injectBuiltins` injects a
// builtin only when the key is ABSENT, so the moment sqlgen writes an `Int:`
// entry it owns the whole list.
//
// Verified byte-for-byte against gqlgen v0.17.90 and v0.17.94. cmd/sqlgen has
// no gqlgen dependency (it invokes gqlgen as a subprocess), so this cannot be
// asserted against the real TypeMap in a unit test.
//
// What the graphql E2E example catches is a REMOVED path: `FindObject` errors
// and `TypeReference` returns immediately, so the run fails loudly. It does
// NOT catch an added entry or a reordered `Model[0]` — an anchored schema
// would silently freeze on the stale order while an unanchored one follows
// gqlgen, and both still compile. Upstream carries a FIXME to default `Int` to
// `int32`, and sqlgen enforces no minimum gqlgen version, so a bump is the
// thing to re-check this map against by hand.
var gqlgenSpecNumericModels = map[string][]string{
	"Int": {
		"github.com/99designs/gqlgen/graphql.Int",
		"github.com/99designs/gqlgen/graphql.Int32",
		"github.com/99designs/gqlgen/graphql.Int64",
	},
	"Float": {
		"github.com/99designs/gqlgen/graphql.FloatContext",
	},
}

// numericWidthModels returns the `models:` entries for every spec numeric
// scalar that needs a sqlgen-emitted marshaler anchor appended, keyed by
// scalar name. Empty when no exposed column resolves to a width gqlgen does
// not already bind — the common case, which leaves the merged config
// byte-identical to a merge without anchors.
//
// The anchors are Option-B discovery anchors, the same shape scalarModelPath
// produces for the built-in registry: gqlgen resolves `<resolver>.Int16` by
// scanning that package for `MarshalInt16` / `UnmarshalInt16` and infers the
// Go type from the marshaler's signature, so no Go type alias is needed.
func numericWidthModels(apiCtx *gen.APIContext) (map[string][]string, []string) {
	if apiCtx == nil || len(apiCtx.NumericWidthScalars) == 0 {
		return nil, nil
	}
	var warnings []string
	out := make(map[string][]string)
	for _, w := range apiCtx.NumericWidthScalars {
		base, ok := gqlgenSpecNumericModels[w.Scalar]
		if !ok {
			continue
		}
		path := w.MarshalerPath
		if w.SqlgenEmitted() {
			// The pair lives in scalars_gen.go, so the anchor is spelled
			// against the resolver package. Without a resolvable import path
			// it cannot be spelled at all, and emitting a bare `Float:`
			// restatement would strip gqlgen's own builtins to add nothing.
			//
			// Unlike scalarModelPath's fail-soft, skipping here is not
			// harmless: generateAPIScalarFile still emits the marshaler pair,
			// so the column ends up with a body nothing anchors and gqlgen
			// answers it with the very panic resolver this exists to remove.
			// Warn rather than degrade in silence.
			if apiCtx.ResolverImportPath == "" {
				warnings = append(warnings, fmt.Sprintf(
					"api: cannot anchor the %s marshaler for Go %q — the resolver package import path is "+
						"unresolvable (is go.mod reachable?). Columns of that width will be answered by a "+
						"panic(\"not implemented\") field resolver (PRD §26.4.1)", w.Name, w.GoType,
				))
				continue
			}
			path = apiCtx.ResolverImportPath + "." + w.Name
		}
		if _, seen := out[w.Scalar]; !seen {
			out[w.Scalar] = slices.Clone(base)
		}
		out[w.Scalar] = append(out[w.Scalar], path)
	}
	return out, warnings
}

// modelFieldOverrides adapts gen's per-field override map onto the wrapper's.
// The two carry the same information; wrapper keeps its own type so it stays
// independent of gen (see guidelines/ARCHITECTURE.md).
func modelFieldOverrides(apiCtx *gen.APIContext) map[string]map[string]wrapper.ModelField {
	src := gen.APIGoFieldOverrides(apiCtx)
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]map[string]wrapper.ModelField, len(src))
	for typeName, fields := range src {
		converted := make(map[string]wrapper.ModelField, len(fields))
		for graphQLName, f := range fields {
			converted[graphQLName] = wrapper.ModelField{FieldName: f.FieldName, GoType: f.GoType}
		}
		out[typeName] = converted
	}
	return out
}

// schemaGlob returns the schema glob to merge into the gqlgen `schema:` list.
// Uses path (forward-slash) since gqlgen's schema field is glob syntax,
// not a filesystem path.
func schemaGlob(cfg *config.RootConfig) string {
	dir := cfg.API.GraphQL.SchemaDir
	if dir == "" {
		dir = "./graph"
	}
	dir = strings.TrimPrefix(dir, "./")
	return path.Join(dir, "*.graphqls")
}

// tableModelPackage joins the module path and output dir into the full
// import path where generated table structs live. Returns empty string when
// the module path is unknown — the caller skips the table model entries in
// that case rather than emitting bogus bindings.
func tableModelPackage(modulePath, outputDir string) string {
	if modulePath == "" {
		return ""
	}
	rel := strings.TrimPrefix(filepath.ToSlash(outputDir), "./")
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" || rel == "." {
		return modulePath
	}
	return modulePath + "/" + rel
}

// scalarModelPath returns the gqlgen `model:` path for a scalar use, or empty
// string when the use cannot be spelled as one. Every scalar that reaches here
// gets an entry — including the gqlgen-bundled ones, which used to be the
// documented exception and were a defect (see the Builtin bullet).
//
// Binding shape (PRD §26.4.1):
//
//   - Category-3 (method-based, native on the runtime type) — bind to
//     `<GoImport>.<TypeName>` so gqlgen invokes the type's MarshalGQL /
//     UnmarshalGQL methods directly. Applies to JSON / DateTime /
//     NullDateTime, all of which carry their own marshaling methods on the
//     sqlgen runtime type.
//
//   - Category-4 (external marshalers) — bind to `<package>.<ScalarName>` as a
//     discovery anchor (Option B). gqlgen scans that package for
//     `Marshal<ScalarName>` / `Unmarshal<ScalarName>` functions and infers the
//     Go type from the marshaler signature, so no Go type alias is needed.
//     The package is the resolver dir for the built-in registry entries
//     (UUID / NullUUID / Decimal / NullDecimal / JSON), whose bodies sqlgen
//     emits into scalars_gen.go, and the consumer's
//     `api.graphql.scalars.<N>.marshaler_package` for a consumer-declared
//     scalar, whose bodies the consumer wrote. The anchor mechanism
//     is identical either way — gqlgen does not care which package the
//     functions live in.
//
//   - Builtin — gqlgen ships the marshaler, so the binding names gqlgen's own
//     package (`graphql.Int64`, `graphql.Time`) rather than the consumer's.
//     The entry is not redundant with gqlgen's defaults: `injectBuiltins`
//     gives `Int64` the two-entry list `[graphql.Int, graphql.Int64]`, and a
//     GENERATED position takes `Model[0]`, so an `int64` column routed onto
//     `Int64` got an `int` input field it could not be assigned from. Pinning
//     the marshaler collapses the list to the declared Go type, in every
//     position.
//
// Returns empty string when the resolver-dir import path is unresolvable
// (e.g. go.mod missing) — the wrapper merge then skips the entry rather
// than emitting a syntactically-broken `models:` binding. The spec primitives
// need no entry at all and never reach here; they are not registry scalars.
func scalarModelPath(s gen.APIScalarUse, resolverImportPath string) string {
	if s.Marshaling == config.ScalarMarshalingBuiltin {
		// Empty for a pair validateBuiltinScalarBinding rejects, and for one
		// it cannot see: it keys on `QualifiedGoType(import, name)` while a
		// column carries the `overrides.types.<t>.type` literal verbatim
		// (gotype.FromLiteral), so `{type: "gqlgraphql.Upload", import:
		// ".../gqlgen/graphql"}` validates as `graphql.Upload` and arrives
		// here spelled differently. Benign — gqlgen's own single-entry
		// `Upload` extraBuiltin then applies and produces the identical
		// binding — and `Upload` is the only key it can happen to, since the
		// rest are unqualified or registry-owned. Falling through empty is
		// the same fail-soft the unresolvable-import case below takes.
		return config.GqlgenBundledMarshalerPath(s.Name, s.GoType)
	}
	if s.Marshaling == config.ScalarMarshalingExternal {
		if s.Name == "" {
			return ""
		}
		if s.MarshalerPackage != "" {
			return s.MarshalerPackage + "." + s.Name
		}
		if resolverImportPath == "" {
			return ""
		}
		return resolverImportPath + "." + s.Name
	}
	if s.GoImport == "" || s.GoType == "" {
		return ""
	}
	parts := strings.SplitN(s.GoType, ".", 2)
	if len(parts) != 2 {
		return ""
	}
	return s.GoImport + "." + parts[1]
}

// buildSeedsAndManagedFields renders the per-table seed delegation bodies
// and enumerates the sqlgen-managed Query.* / Mutation.* field names for a
// `sqlgen graphql gen` invocation. The wrapper consumes
// both: seeds get written into gqlgen-shaped files when absent, and the
// managed-field set scopes the post-gqlgen panic-stub rewriter.
func buildSeedsAndManagedFields(apiCtx *gen.APIContext, cfg *config.RootConfig) ([]wrapper.TableSeed, wrapper.SqlgenManagedFieldSet, error) {
	dialect := dialectFromConfig(cfg.Input.Dialect)
	bodies, err := gen.RenderAPISeeds(dialect, apiCtx)
	if err != nil {
		return nil, wrapper.SqlgenManagedFieldSet{}, fmt.Errorf("rendering api seeds: %w", err)
	}
	seeds := make([]wrapper.TableSeed, 0, len(bodies))
	for _, b := range bodies {
		seeds = append(seeds, wrapper.TableSeed{SnakeName: b.SnakeName, Body: b.Body})
	}
	queries, mutations := gen.CollectSqlgenManagedFields(apiCtx)
	return seeds, wrapper.SqlgenManagedFieldSet{
		QueryFields:    queries,
		MutationFields: mutations,
		Owners:         managedFieldOwners(apiCtx),
	}, nil
}

// managedFieldOwners keys each managed root field to its entity, for the
// wrapper's post-gqlgen stub check.
func managedFieldOwners(apiCtx *gen.APIContext) map[string]wrapper.ManagedFieldOwner {
	queries, mutations := gen.CollectSqlgenRootFields(apiCtx)
	out := make(map[string]wrapper.ManagedFieldOwner, len(queries)+len(mutations))
	for _, f := range queries {
		out["Q."+f.GoName] = wrapper.ManagedFieldOwner{ConfigKey: f.ConfigKey, GraphQLName: f.GraphQLName}
	}
	for _, f := range mutations {
		out["M."+f.GoName] = wrapper.ManagedFieldOwner{ConfigKey: f.ConfigKey, GraphQLName: f.GraphQLName}
	}
	return out
}

// dialectFromConfig returns the sql.Dialect implementation for the given
// dialect name. Mirrors gen.resolveDialect (which is unexported) so the CLI
// layer can drive seed rendering without exposing internal APIs.
func dialectFromConfig(name config.Dialect) sql.Dialect {
	switch name {
	case config.DialectMySQL:
		return sql.NewMySQLDialect()
	case config.DialectSQLite:
		return sql.NewSQLiteDialect()
	default:
		return sql.NewPostgresDialect()
	}
}

// readModulePath reads the module path declared in go.mod under dir. Returns
// empty string if go.mod is missing, unreadable, or has no module directive.
func readModulePath(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod")) //nolint:gosec // reading go.mod from working directory to derive consumer module path
	if err != nil {
		return ""
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "module ") || trimmed == "module" {
			rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "module"))
			rest = strings.Trim(rest, "\"")
			return rest
		}
	}
	return ""
}
