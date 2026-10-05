package gen_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
)

func TestBuildSharedTypesContext_allTypesPresent(t *testing.T) {
	ctx := gen.BuildSharedTypesContext("db", false, "", "", false, false, false)

	if ctx.Package != "db" {
		t.Errorf("Package = %q, want %q", ctx.Package, "db")
	}

	wantTypes := []string{
		"CallOptions",
		"IncrementInput",
	}

	if len(ctx.Types) != len(wantTypes) {
		t.Fatalf("Types has %d entries, want %d", len(ctx.Types), len(wantTypes))
	}

	for i, wantName := range wantTypes {
		if ctx.Types[i].Name != wantName {
			t.Errorf("Types[%d].Name = %q, want %q", i, ctx.Types[i].Name, wantName)
		}
	}
}

func TestBuildSharedTypesContext_allHelpersPresent(t *testing.T) {
	ctx := gen.BuildSharedTypesContext("db", false, "", "", false, false, false)

	wantHelpers := []string{
		"excludeColumns",
		"resolveCallOptions",
		"toAnySlice",
		"unionColumns",
	}

	if len(ctx.Helpers) != len(wantHelpers) {
		t.Fatalf("Helpers has %d entries, want %d", len(ctx.Helpers), len(wantHelpers))
	}

	for i, wantName := range wantHelpers {
		if ctx.Helpers[i].Name != wantName {
			t.Errorf("Helpers[%d].Name = %q, want %q", i, ctx.Helpers[i].Name, wantName)
		}
	}
}

func TestBuildSharedTypesContext_callOptionsFields(t *testing.T) {
	ctx := gen.BuildSharedTypesContext("db", false, "", "", false, false, false)

	var callOpts *gen.SharedTypeDefinition
	for i := range ctx.Types {
		if ctx.Types[i].Name == "CallOptions" {
			callOpts = &ctx.Types[i]
			break
		}
	}
	if callOpts == nil {
		t.Fatal("CallOptions type not found")
	}

	if callOpts.TypeParams != "[FO any]" {
		t.Errorf("CallOptions.TypeParams = %q, want %q", callOpts.TypeParams, "[FO any]")
	}

	wantFields := []gen.SharedFieldDefinition{
		{Name: "SkipCache", GoType: "bool", JSONTag: "skip_cache", Doc: "bypass cache read-through and write-through"},
		{Name: "SkipEvents", GoType: "bool", JSONTag: "skip_events", Doc: "suppress event publishing for this mutation"},
		{Name: "SkipHooks", GoType: "bool", JSONTag: "skip_hooks", Doc: "bypass all hooks except panic recovery"},
		{Name: "FieldOptions", GoType: "*FO", JSONTag: "field_options", Doc: "column selection and relationship loading"},
		{Name: "LockMode", GoType: "sql.LockMode", JSONTag: "lock_mode", Doc: "row-level lock clause for read methods"},
		{Name: "AllowInTransaction", GoType: "bool", JSONTag: "allow_in_transaction", Doc: "permit Stream inside a transaction; commits the caller to issuing nothing else on that txCtx"},
	}

	if diff := cmp.Diff(wantFields, callOpts.Fields); diff != "" {
		t.Errorf("CallOptions.Fields mismatch (-want +got):\n%s", diff)
	}
}

func TestBuildSharedTypesContext_callOptionsFieldsTenancyEnabled(t *testing.T) {
	ctx := gen.BuildSharedTypesContext("db", true, "uuid.UUID", "github.com/google/uuid", false, false, false)

	var callOpts *gen.SharedTypeDefinition
	for i := range ctx.Types {
		if ctx.Types[i].Name == "CallOptions" {
			callOpts = &ctx.Types[i]
			break
		}
	}
	if callOpts == nil {
		t.Fatal("CallOptions type not found")
	}

	wantFields := []gen.SharedFieldDefinition{
		{Name: "SkipCache", GoType: "bool", JSONTag: "skip_cache", Doc: "bypass cache read-through and write-through"},
		{Name: "SkipEvents", GoType: "bool", JSONTag: "skip_events", Doc: "suppress event publishing for this mutation"},
		{Name: "SkipHooks", GoType: "bool", JSONTag: "skip_hooks", Doc: "bypass all hooks except panic recovery"},
		{Name: "SkipTenancy", GoType: "bool", JSONTag: "skip_tenancy", Doc: "bypass tenant auto-filter and mutation mismatch check; orthogonal to SkipHooks"},
		{Name: "Tenant", GoType: "*uuid.UUID", JSONTag: "tenant", Doc: "explicit tenant: resolve tenancy to this value (filter + auto-set) instead of the ctx resolver; wins over SkipTenancy; nil = unset"},
		{Name: "FieldOptions", GoType: "*FO", JSONTag: "field_options", Doc: "column selection and relationship loading"},
		{Name: "LockMode", GoType: "sql.LockMode", JSONTag: "lock_mode", Doc: "row-level lock clause for read methods"},
		{Name: "AllowInTransaction", GoType: "bool", JSONTag: "allow_in_transaction", Doc: "permit Stream inside a transaction; commits the caller to issuing nothing else on that txCtx"},
	}

	if diff := cmp.Diff(wantFields, callOpts.Fields); diff != "" {
		t.Errorf("CallOptions.Fields mismatch (-want +got):\n%s", diff)
	}

	// The tenant import must ride along so the concretely-typed field compiles.
	if !slices.Contains(ctx.Imports, "github.com/google/uuid") {
		t.Errorf("Imports missing the tenant type import; imports=%v", ctx.Imports)
	}
}

// The explicit-tenant field needs a uniform tenant type; when tenancy is
// enabled but no table is tenanted (all opted out), only SkipTenancy is
// emitted and resolveCallOptions carries no Tenant normalization.
func TestBuildSharedTypesContext_tenantFieldAbsentWithoutTenantedTables(t *testing.T) {
	ctx := gen.BuildSharedTypesContext("db", true, "", "", false, false, false)

	for _, typ := range ctx.Types {
		if typ.Name != "CallOptions" {
			continue
		}
		for _, f := range typ.Fields {
			if f.Name == "Tenant" {
				t.Errorf("Tenant field emitted with no tenanted tables; got %+v", f)
			}
		}
	}
	for _, h := range ctx.Helpers {
		if h.Name == "resolveCallOptions" && strings.Contains(h.Body, "options.Tenant") {
			t.Errorf("resolveCallOptions normalizes Tenant with no tenanted tables:\n%s", h.Body)
		}
	}
}

func TestBuildSharedTypesContext_skipTenancyAbsentWhenDisabled(t *testing.T) {
	ctx := gen.BuildSharedTypesContext("db", false, "", "", false, false, false)

	var callOpts *gen.SharedTypeDefinition
	for i := range ctx.Types {
		if ctx.Types[i].Name == "CallOptions" {
			callOpts = &ctx.Types[i]
			break
		}
	}
	if callOpts == nil {
		t.Fatal("CallOptions type not found")
	}

	for _, f := range callOpts.Fields {
		if f.Name == "SkipTenancy" {
			t.Errorf("SkipTenancy must not appear in CallOptions when tenancy is disabled; got field %+v", f)
		}
	}
}

// TestResolveCallOptions_skipHooksDoesNotFoldSkipTenancy verifies the §29.4.4
// orthogonality rule: SkipHooks and SkipTenancy are independent toggles.
// Tenancy runs in the SQL builders (not as a hook), so folding SkipHooks into
// SkipTenancy would silently open a cross-tenant read/write path whenever hooks
// were bypassed. The only SkipTenancy write allowed in resolveCallOptions is
// the §29.4.4 explicit-tenant precedence normalization (Tenant != nil clears
// SkipTenancy) — never a set to true and never inside the SkipHooks fold.
func TestResolveCallOptions_skipHooksDoesNotFoldSkipTenancy(t *testing.T) {
	ctx := gen.BuildSharedTypesContext("db", true, "uuid.UUID", "github.com/google/uuid", false, false, false)

	var helper *gen.SharedHelperDefinition
	for i := range ctx.Helpers {
		if ctx.Helpers[i].Name == "resolveCallOptions" {
			helper = &ctx.Helpers[i]
			break
		}
	}
	if helper == nil {
		t.Fatal("resolveCallOptions helper not found")
	}

	if strings.Contains(helper.Body, "SkipTenancy = true") {
		t.Errorf("resolveCallOptions body must never set SkipTenancy (orthogonal to SkipHooks per §29.4.4):\n%s", helper.Body)
	}
	// The explicit-tenant precedence normalization must be present.
	if !strings.Contains(helper.Body, "if options.Tenant != nil {") ||
		!strings.Contains(helper.Body, "options.SkipTenancy = false") {
		t.Errorf("resolveCallOptions body missing the explicit-tenant precedence normalization:\n%s", helper.Body)
	}
}

func TestBuildSharedTypesContext_incrementInputFields(t *testing.T) {
	ctx := gen.BuildSharedTypesContext("db", false, "", "", false, false, false)

	var incInput *gen.SharedTypeDefinition
	for i := range ctx.Types {
		if ctx.Types[i].Name == "IncrementInput" {
			incInput = &ctx.Types[i]
			break
		}
	}
	if incInput == nil {
		t.Fatal("IncrementInput type not found")
	}

	if incInput.TypeParams != "[C ~string]" {
		t.Errorf("IncrementInput.TypeParams = %q, want %q", incInput.TypeParams, "[C ~string]")
	}

	wantFields := []gen.SharedFieldDefinition{
		{Name: "Column", GoType: "C", JSONTag: "column"},
		{Name: "Amount", GoType: "int", JSONTag: "amount"},
	}

	if diff := cmp.Diff(wantFields, incInput.Fields); diff != "" {
		t.Errorf("IncrementInput.Fields mismatch (-want +got):\n%s", diff)
	}
}

// PaginateResult is now generated by pagination.go.tmpl — see pagination_test.go.

func TestBuildSharedTypesContext_resolveCallOptionsSignature(t *testing.T) {
	ctx := gen.BuildSharedTypesContext("db", false, "", "", false, false, false)

	var helper *gen.SharedHelperDefinition
	for i := range ctx.Helpers {
		if ctx.Helpers[i].Name == "resolveCallOptions" {
			helper = &ctx.Helpers[i]
			break
		}
	}
	if helper == nil {
		t.Fatal("resolveCallOptions helper not found")
	}

	wantSig := "[FO any](opts []func(*CallOptions[FO])) CallOptions[FO]"
	if helper.Signature != wantSig {
		t.Errorf("resolveCallOptions.Signature = %q, want %q", helper.Signature, wantSig)
	}

	if !strings.Contains(helper.Body, "var options CallOptions[FO]") {
		t.Error("resolveCallOptions body should contain 'var options CallOptions[FO]'")
	}
}

func TestBuildSharedTypesContext_toAnySliceSignature(t *testing.T) {
	ctx := gen.BuildSharedTypesContext("db", false, "", "", false, false, false)

	var helper *gen.SharedHelperDefinition
	for i := range ctx.Helpers {
		if ctx.Helpers[i].Name == "toAnySlice" {
			helper = &ctx.Helpers[i]
			break
		}
	}
	if helper == nil {
		t.Fatal("toAnySlice helper not found")
	}

	wantSig := "[T any](s []T) []any"
	if helper.Signature != wantSig {
		t.Errorf("toAnySlice.Signature = %q, want %q", helper.Signature, wantSig)
	}

	if !strings.Contains(helper.Body, "make([]any, len(s))") {
		t.Error("toAnySlice body should contain 'make([]any, len(s))'")
	}
}

func TestBuildSharedTypesContext_typesSorted(t *testing.T) {
	ctx := gen.BuildSharedTypesContext("db", false, "", "", false, false, false)

	for i := 1; i < len(ctx.Types); i++ {
		if ctx.Types[i].Name < ctx.Types[i-1].Name {
			t.Errorf("Types not sorted: %q before %q", ctx.Types[i-1].Name, ctx.Types[i].Name)
		}
	}
}

func TestBuildSharedTypesContext_helpersSorted(t *testing.T) {
	ctx := gen.BuildSharedTypesContext("db", false, "", "", false, false, false)

	for i := 1; i < len(ctx.Helpers); i++ {
		if ctx.Helpers[i].Name < ctx.Helpers[i-1].Name {
			t.Errorf("Helpers not sorted: %q before %q", ctx.Helpers[i-1].Name, ctx.Helpers[i].Name)
		}
	}
}
