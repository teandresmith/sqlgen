package gen

import (
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
)

// Test-only export surface. Bridges package-internal helpers to the external
// `gen_test` test package without widening the production API.

// AttachTenancyToTablesForTest invokes the unexported attachTenancyToTables
// so external tests can exercise the annotation pass (including the
// annotateO2OChildTenancy side-effect) end-to-end.
func AttachTenancyToTablesForTest(tables []TableContext, tenancyMap map[string]TenancyContext) {
	attachTenancyToTables(tables, tenancyMap)
}

// AttachTenancyToViewsForTest invokes the unexported attachTenancyToViews so
// external tests can exercise the view annotation pass (PRD §29.2.5).
func AttachTenancyToViewsForTest(views []ViewContext, tenancyMap map[string]TenancyContext) {
	attachTenancyToViews(views, tenancyMap)
}

// APIImportDirForTest exposes the unexported apiImportDir helper so external
// tests can verify the fallback logic (prefers opts.OriginalOutputDir
// over cfg.Output.Dir).
func APIImportDirForTest(opts *Options, cfg *config.RootConfig) string {
	return apiImportDir(opts, cfg)
}

// ResolveGraphDirForTest exposes the unexported resolveGraphDir helper so
// external tests can pin the §26.5.8 root-relative resolution (join onto the
// project root, not output.dir; absolute paths verbatim).
func ResolveGraphDirForTest(projectRoot, dir string) string {
	return resolveGraphDir(projectRoot, dir)
}

// ParenIfCompositeForTest exposes the unexported parenIfComposite helper so
// external tests can verify the wrapping rule for composite-literal
// zero values used in `if`-condition expression position.
func ParenIfCompositeForTest(zeroValue string) string {
	return parenIfComposite(zeroValue)
}

// FKToStringByGoTypeForTest exposes the unexported funcFKToStringByGoType
// helper so external tests can pin the conversion table (target's
// PK Go type → string-conversion expression) without reaching into the
// template funcmap.
func FKToStringByGoTypeForTest(goType, varExpr string) string {
	return funcFKToStringByGoType(goType, varExpr)
}

// FKLoadGuardForTest exposes the fkLoadGuard helper closed over a
// caller-supplied resolver so tests can pin guard expressions for both
// built-in wrappers (resolver=nil) and user-declared wrappers.
func FKLoadGuardForTest(resolver *gotype.Resolver, fkColumnGoType, varExpr string) string {
	return funcFKLoadGuard(resolver)(fkColumnGoType, varExpr)
}

// FKLoadKeyForTest exposes the fkLoadKey helper closed over a
// caller-supplied resolver so tests can pin bucket-key expressions for the
// O2M loader's null-aware FK extraction path.
func FKLoadKeyForTest(resolver *gotype.Resolver, fkColumnGoType, varExpr string) string {
	return funcFKLoadKey(resolver)(fkColumnGoType, varExpr)
}

// MapRelationshipToGraphQLForTest exposes the unexported mapRelationshipToGraphQL
// so external tests can pin the (Type, Side, FKNullable) dispatch and
// prove it ignores the string spelling of GoType.
func MapRelationshipToGraphQLForTest(r RelationshipContext, casing string) APIRelationshipContext {
	return mapRelationshipToGraphQL(r, casing)
}

// BuildAPIComparatorTranslateFileForTest exposes the unexported
// buildAPIComparatorTranslateFile so external tests can assert the import
// block the orchestrator assembles — the rendered body alone cannot show a
// missing import, and the file is emitted with FormatOnly, so goimports never
// repairs one.
func BuildAPIComparatorTranslateFileForTest(pkg string, apiCtx *APIContext, body string) []byte {
	return buildAPIComparatorTranslateFile(pkg, apiCtx, body)
}

// BuiltInScalarRegistryNamesForTest exposes the Go-type → scalar-name key set
// of the unexported builtInScalarRegistry so external tests can pin
// config.BuiltInScalarGoTypes against it. The mirror exists because config
// must not import gen, and drift would silently let a consumer declare a
// scalar for a Go type the registry already owns.
func BuiltInScalarRegistryNamesForTest() map[string]string {
	out := make(map[string]string, len(builtInScalarRegistry))
	for goType, binding := range builtInScalarRegistry {
		out[goType] = binding.Name
	}
	return out
}

// UnusedScalarWarningsForTest exposes the unexported unusedScalarWarnings so
// external tests can pin the declared-but-unresolved diagnostic, including the
// §32.2 access-role case where an unused declaration is legitimate.
func UnusedScalarWarningsForTest(cfg *config.RootConfig, apiCtx *APIContext) []string {
	return unusedScalarWarnings(cfg, apiCtx)
}

// APIScalarTemplateSourceForTest returns the raw source of the API scalars
// template so external tests can pin that every registry entry needing an
// emitted marshaler actually has one. Reading the source rather than rendering
// is deliberate: a missing body renders as nothing at all, so a render-based
// check would have to know what it was looking for and would pass vacuously.
func APIScalarTemplateSourceForTest(t *testing.T) string {
	t.Helper()
	src, err := templateFS.ReadFile("templates/api/scalars.go.tmpl")
	if err != nil {
		t.Fatalf("reading api scalars template: %v", err)
	}
	return string(src)
}

// ScalarNeedsEmittedBodyForTest reports whether the registry entry for goType
// is one sqlgen emits a Marshal/Unmarshal body for — the §26.4.1 category-4
// entries. Method-based and gqlgen-bundled bindings carry their marshaling
// elsewhere and correctly have no body in the template.
func ScalarNeedsEmittedBodyForTest(goType string) bool {
	return builtInScalarRegistry[goType].Marshaling == config.ScalarMarshalingExternal
}

// ScalarIsGqlgenBundledForTest reports whether the registry entry for goType
// delegates its marshaling to a function gqlgen itself ships — the §26.4.1
// category-2 entries, whose `models:` binding names gqlgen's own package.
func ScalarIsGqlgenBundledForTest(goType string) bool {
	return builtInScalarRegistry[goType].Marshaling == config.ScalarMarshalingBuiltin
}

// ScalarImportsForTest exposes the unexported scalarImports so external tests
// can pin the declared import set against what each rendered marshaler body
// actually references. Without that pin a registry entry can declare a body
// needing `encoding/base64` and no import, and goimports may silently repair
// it — which is precisely what guidelines/TEMPLATES.md §9 says templates must
// not rely on.
func ScalarImportsForTest(externals []APIScalarUse) []string {
	return scalarImports(externals)
}

// ExternalScalarUsesForTest returns one APIScalarUse per built-in registry
// entry that sqlgen emits a marshaler body for, shaped the way BuildAPIContext
// would populate it.
func ExternalScalarUsesForTest() []APIScalarUse {
	out := make([]APIScalarUse, 0, len(builtInScalarRegistry))
	for goType, reg := range builtInScalarRegistry {
		if reg.Marshaling != config.ScalarMarshalingExternal {
			continue
		}
		out = append(out, APIScalarUse{
			Name:       reg.Name,
			GoType:     goType,
			GoImport:   reg.Import,
			Marshaling: reg.Marshaling,
		})
	}
	return out
}

// OpaqueComparatorBindingsForTest exposes the Opaque[T] binding table
// — Go type -> (GraphQL scalar, gqlgen operand shapes) — so external tests can
// pin it against builtInScalarRegistry and against the model-side predicate.
func OpaqueComparatorBindingsForTest() map[string]struct {
	Scalar     string
	OperandPtr bool
	ElemPtr    bool
} {
	return opaqueComparatorBindings
}

// IsOpaqueComparatorGoTypeForTest exposes the model-side predicate that routes
// a column onto comparator.Opaque[T], so the two halves of the Opaque[T]
// routing can be pinned against each other.
func IsOpaqueComparatorGoTypeForTest(goType string) bool {
	return isOpaqueComparatorGoType(goType)
}

// BuiltInScalarNameForTest returns the GraphQL scalar a built-in registry
// entry binds a Go type to, and whether the type is registered at all.
func BuiltInScalarNameForTest(goType string) (string, bool) {
	reg, ok := builtInScalarRegistry[goType]
	return reg.Name, ok
}

// ComparatorTranslatorStdImportsForTest exposes the import computation for
// graph/comparator_translate_gen.go. FormatOnly does not run goimports over
// that file, so a missing import is a compile error in the consumer's project
// and an unused one is too — this must name exactly what the bodies reference.
func ComparatorTranslatorStdImportsForTest(apiCtx *APIContext) []string {
	return comparatorTranslatorStdImports(apiCtx)
}

// APIEntityFromTable and APIEntityFromView bridge the two unexported APIEntity
// constructors so external tests can drive ValidateAPIWalkerCompleteness (and
// the completeness lints) against either entity kind.
func APIEntityFromTable(tc TableContext) APIEntity { return apiEntityFromTable(tc) }

func APIEntityFromView(vc ViewContext) APIEntity { return apiEntityFromView(vc) }

// ClaimedGraphQLNamesForTest exposes the GraphQL type-name claim set as a
// name → owning-entity map, so a test can hold the seven names a view CLAIMS
// (api_names.go) against the seven it EMITS (templates/api/schema.graphqls).
// The two lists are the same fact written in two places; nothing but a test
// keeps them in step; an emitted but unclaimed name
// would collide silently.
func ClaimedGraphQLNamesForTest(ctx *APIContext) map[string]string {
	claims := collectGraphQLTypeNames(ctx, nil)
	out := make(map[string]string, len(claims))
	for _, c := range claims {
		out[c.name] = c.by.String()
	}
	return out
}

// TypeQualifierForTest exposes the unexported typeQualifier so external tests
// can pin the reader the one-UUID-library-per-package rule keys on. The rule
// takes the qualifier off the resolved Go type rather than off the import
// path, so every shape the resolver emits for a UUID column has to report
// `uuid` (PRD §4.13, §7.4).
func TypeQualifierForTest(goType string) string { return typeQualifier(goType) }

// ObserveImportDriftForTest installs fn as the import-drift observer for the
// duration of the test, so external tests can assert that every emitted file
// declares a complete import set rather than leaning on goimports to resolve
// the remainder (see modelTemplateImports). The observer is package-level
// state, so a test using it must not run in parallel.
func ObserveImportDriftForTest(t *testing.T, fn func(filename string, added []string)) {
	t.Helper()
	importDriftObserver = fn
	t.Cleanup(func() { importDriftObserver = nil })
}

// DiscriminatorContextFor exposes the unexported discriminatorContext so
// external tests can pin both literal renderings of a `discriminator:` value
// (PRD §13.4.1) without going through a full context build. The two spellings
// feed different read paths — GoLiteral is bound, SQLLiteral is interpolated —
// so a rendering bug in either is invisible in the other's output.
func DiscriminatorContextFor(d *config.RelationshipDiscriminator) *DiscriminatorContext {
	return discriminatorContext(d)
}
