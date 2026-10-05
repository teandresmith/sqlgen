package gen

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// This file enforces the one-UUID-library-per-generated-package rule (PRD
// §4.13 "Two UUID libraries resolved in one generated package", §7.4 "One UUID
// library per generated package").
//
// The rule exists because the three supported integrations are
// indistinguishable in the emitted text. All of them bind the file-local
// package name `uuid`, all of them export a type named `UUID`, and the
// consumer spells every one of them `type: uuid.UUID` — so a package that
// resolves two has no way to say which import a given `uuid.UUID` meant.
// Imports are resolved per package rather than per file (resolvePackageImports
// resolves once over the concatenated bodies and seeds every emitted file), so
// `layout: file_per_table` fails exactly as `single_file` does; the per-file
// prune that follows is usage-based and cannot separate two packages both
// spelled `uuid.`. The `database/sql` → `stdsql` aliasing in format.go does not
// generalize here: that collision is against a fixed set of identifiers sqlgen
// itself spells, where this one is between two consumer-chosen imports whose
// per-column provenance emission does not carry.
//
// The two failures it replaces are both bad diagnostics. The visible one is
// `uuid redeclared in this block`, reported from a generated file against a
// config key it never names. The quieter one is worse: the surviving `uuid`
// binds to whichever import wins, so a `uuid.NullUUID` a google-backed table
// needs resolves against a standard library that has no such identifier.
//
// It is a resolution-level (phase 3) rule because it cannot be answered from
// the config text: it has to collect the import path every column actually
// resolved to across the whole package before it can count the distinct ones.

// uuidPackageName is the file-local package name every supported UUID
// integration binds — `uuid` for the standard library, for
// github.com/google/uuid, and for github.com/gofrs/uuid/v5 alike (the `/v5`
// is a module major-version suffix, not part of the package name).
const uuidPackageName = "uuid"

// uuidClaim is one resolved reference to a uuid-qualified type, paired with the
// import path the generated package would have to import to satisfy it.
type uuidClaim struct {
	// importPath is the resolved import path, never empty.
	importPath string
	// by identifies the entity whose declaration resolved to it.
	by entityRef
	// site reads as `column "id"` — the member within the entity, so the
	// consumer has somewhere to look after the entity name. Empty for an
	// entity that claims as a whole (a domain type is one declaration).
	site string
}

// describe renders one claim for the error message.
func (c uuidClaim) describe() string {
	if c.site == "" {
		return fmt.Sprintf("%q (selected by %s)", c.importPath, c.by)
	}
	return fmt.Sprintf("%q (selected by %s, %s)", c.importPath, c.by, c.site)
}

// collectUUIDClaims reads every uuid-qualified type out of the built contexts,
// paired with the import path that resolved it.
//
// Reading the contexts rather than the config text is what makes the rule
// complete, and is the same choice collectResolvedNames makes. Each of the
// config surfaces §4.13 enumerates — global `overrides.types`, table-scoped
// `overrides.types`, and `column_map.<col>.import` — has already
// been through the resolution chain by the time the contexts exist and lands in
// ColumnContext.Import, as does a nullable variant's own `nullable.import` when
// one is declared. A rule written against the config would have to re-implement
// that chain to know which surface won for a given column, and would still have
// to guess which columns the schema actually has.
//
// Reading the contexts is also what scopes the rule correctly. An override for
// a SQL type no column in the package uses resolves nowhere, imports nothing,
// and is not a claim — and a table `exclude_tables` drops, or one with no
// resolved primary key, never reaches a context at all.
//
// The qualifier is taken from the resolved Go type rather than derived from the
// import path because the qualifier is what the generated file actually spells.
// An import binds whatever package name its `package` clause declares, which
// sqlgen cannot know without loading consumer packages — but `uuid.UUID` in a
// generated struct field is unambiguous about the name it needs bound.
func collectUUIDClaims(ctxs generationContexts) []uuidClaim {
	var claims []uuidClaim

	// claim records one resolved declaration, keeping only the uuid-qualified
	// ones that actually carry an import.
	claim := func(importPath, goType string, by entityRef, site string) {
		if importPath == "" || typeQualifier(goType) != uuidPackageName {
			return
		}
		claims = append(claims, uuidClaim{importPath: importPath, by: by, site: site})
	}
	claimColumns := func(ref entityRef, cols []ColumnContext) {
		for i := range cols {
			claim(cols[i].Import, cols[i].GoType, ref, fmt.Sprintf("column %q", cols[i].Name))
		}
	}

	for _, tc := range ctxs.tables {
		claimColumns(tableRef(tc.Schema, tc.TableName), tc.Columns)
	}
	for _, vc := range ctxs.views {
		claimColumns(viewRef(vc.Schema, vc.ViewName), vc.Columns)
	}

	// The three members of TypeContext all render into one file — the
	// `output.types.file`, which goimports resolves with a single import block
	// — so a composite attribute, a domain's base type and an `extras` field
	// select a library on exactly the same terms a column does, and on the same
	// terms as each other. §4.13's em-dash list names the three column-bearing
	// config surfaces, but the rule it states is over "every UUID import path
	// resolved across the generated package", and all three of these are
	// resolved across it.
	//
	// Composites and domains resolve with no table overrides in view
	// (buildCompositeContexts / buildDomainContexts pass nil), so they always
	// take the *global* binding. That is what makes them worth collecting
	// rather than assuming redundant: a package whose only disagreement is a
	// uuid-typed composite attribute on the global library against a
	// table-scoped or `column_map` override on a different one would otherwise
	// pass, and then mis-bind exactly as this file's header describes.
	for _, ct := range ctxs.types.Composites {
		ref := entityRef{kind: "composite type", qualified: qualifiedOrBare(ct.Schema, ct.Name)}
		for i := range ct.Fields {
			claim(ct.Fields[i].Import, ct.Fields[i].GoType, ref, fmt.Sprintf("field %q", ct.Fields[i].FieldName))
		}
	}
	for _, dt := range ctxs.types.Domains {
		// A domain is a one-line alias, so the type itself is the claim and
		// there is no member to name.
		ref := entityRef{kind: "domain type", qualified: qualifiedOrBare(dt.Schema, dt.Name)}
		claim(dt.Import, dt.BaseGoType, ref, "")
	}
	for _, et := range ctxs.types.Extras {
		// An extra is declared in the config rather than the schema, so the
		// rename lives there too.
		ref := entityRef{
			kind:      "extra type",
			qualified: et.GoTypeName,
			escape:    "extras." + et.GoTypeName,
		}
		for i := range et.Fields {
			claim(et.Fields[i].Import, et.Fields[i].GoType, ref, fmt.Sprintf("field %q", et.Fields[i].FieldName))
		}
	}

	return claims
}

// validateUUIDLibraries rejects a generated package that resolves more than one
// UUID library (PRD §4.13, §7.4).
//
// Per generated package, not per module: a project that genuinely needs two
// libraries gives each its own `sqlgen.yml` and `output.dir`, and that
// arrangement stays supported — nothing here can see across packages, which is
// exactly right.
func validateUUIDLibraries(ctxs generationContexts) error {
	// The first claim on each path is the one the error names. Every context
	// list is built in a deterministic order — tables, views, composites and
	// domains sorted by (schema, name), extras by Go type name, and the members
	// within each in the order the schema or config declared them — so "first"
	// is stable across runs and the message does not churn (PRD §5.7).
	first := make(map[string]uuidClaim)
	for _, claim := range collectUUIDClaims(ctxs) {
		if _, seen := first[claim.importPath]; !seen {
			first[claim.importPath] = claim
		}
	}
	if len(first) < 2 {
		return nil
	}

	paths := slices.Sorted(maps.Keys(first))
	selectors := make([]string, 0, len(paths))
	for _, path := range paths {
		selectors = append(selectors, first[path].describe())
	}

	return fmt.Errorf(
		"%d UUID libraries resolved in one generated package: %s — every supported UUID integration binds the file-local package name %q and exports a type named UUID, so a package importing more than one cannot compile; split them across separate output.dir packages",
		len(paths), joinClaims(selectors), uuidPackageName,
	)
}

// typeQualifier returns the package qualifier of a Go type expression — "uuid"
// for `uuid.UUID`, `*uuid.UUID`, `[]uuid.UUID` and `map[string]uuid.UUID`
// alike — or "" when the type is unqualified.
//
// It reads the identifier immediately before the first ".", which is the
// package name in every shape the resolver produces: a qualified type is
// always `[prefix]pkg.Name`, and the prefix is punctuation or a builtin type
// name, never something that could be mistaken for the qualifier.
//
// The one expression this would read wrongly is a map with a *qualified key*
// (`map[a.K]uuid.UUID`), where the first "." belongs to the key. No resolver
// output has one — the only map the resolver emits is `map[string]any` — and a
// two-package type expression could only arrive through a single
// `column_map.<col>.import`, which cannot name the second package either way.
func typeQualifier(goType string) string {
	dot := strings.IndexByte(goType, '.')
	if dot < 0 {
		return ""
	}
	start := dot
	for start > 0 && isGoIdentByte(goType[start-1]) {
		start--
	}
	return goType[start:dot]
}

// isGoIdentByte reports whether b can appear in a Go identifier. ASCII is
// enough: a package name outside it would not survive the qualifier comparison
// this feeds either way.
func isGoIdentByte(b byte) bool {
	return b == '_' ||
		(b >= '0' && b <= '9') ||
		(b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z')
}
