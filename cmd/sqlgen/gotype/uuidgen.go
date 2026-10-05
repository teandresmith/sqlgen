package gotype

import (
	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype/uuidgofrs"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype/uuidgoogle"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype/uuidstd"
)

// This file answers one question for the generator: given the UUID library a
// generated package resolved, how does that library spell the calls the
// generated code makes — making a new UUID, and parsing one from a string?
// PRD §7.4 "Generating UUID values" and "Parsing a UUID from a string" are the
// normative tables; the spellings themselves live on the integration packages,
// and this file keys them by import path.
//
// It is a per-import-path lookup rather than a fixed string because the three
// libraries disagree on every axis: only google exports New, only google
// exports NewString, Must exists in google and gofrs but not in the standard
// library, the standard library's constructors return a bare UUID where the
// other two return (UUID, error), and gofrs exports no Parse at all. A
// generator that emits one spelling everywhere — which is what it did before —
// produces code that compiles against exactly one of the three.

// UUIDIntegration is a generated package's binding to a UUID library at
// generation time: the import path its files carry, plus the four generation
// expressions of PRD §7.4 "Generating UUID values" and the string constructor
// of "Parsing a UUID from a string".
//
// The string forms are fields rather than the value forms with ".String()"
// appended because google's v4 string form is uuid.NewString(), which is a
// different call and not a suffix of uuid.New().
type UUIDIntegration struct {
	// ImportPath is what the generated package imports to satisfy the
	// expressions below — the standard library's "uuid" for the default
	// binding.
	ImportPath string
	// V4 and V7 generate a uuid.UUID value, for a target typed uuid.UUID.
	V4 string
	V7 string
	// V4String and V7String generate the string form, for a target typed
	// string — a primary key overridden to string, and every event ID
	// (PRD §28.3).
	V4String string
	V7String string
	// ParseFunc is the bare name of the library's string constructor, which
	// the GraphQL UUID / NullUUID scalar unmarshalers call (PRD §7.4
	// "Parsing a UUID from a string", §26.4.1). It is a name rather than an
	// expression because the caller supplies the argument.
	//
	// It disagrees across the three on a different axis than the generating
	// calls do: the standard library and google both export Parse, and gofrs
	// exports no Parse at all. All three return (uuid.UUID, error), so the
	// call site's shape is identical and only the name varies.
	ParseFunc string
}

// ValueExpr returns the expression for a target typed uuid.UUID. An
// unrecognized version resolves to v4, matching the config default
// (PRD §8.6).
func (i UUIDIntegration) ValueExpr(version config.UUIDVersion) string {
	if version == config.UUIDVersionV7 {
		return i.V7
	}
	return i.V4
}

// StringExpr returns the expression for a target typed string, under the same
// version rule as ValueExpr.
func (i UUIDIntegration) StringExpr(version config.UUIDVersion) string {
	if version == config.UUIDVersionV7 {
		return i.V7String
	}
	return i.V4String
}

// uuidIntegrations holds the generation expressions of every supported UUID
// integration, keyed by the import path a resolved column carries. The
// decimal integration has no entry: nothing in the generated code makes a
// decimal from thin air.
var uuidIntegrations = map[string]UUIDIntegration{
	uuidstd.ImportPath: {
		ImportPath: uuidstd.ImportPath,
		V4:         uuidstd.V4Expr,
		V7:         uuidstd.V7Expr,
		V4String:   uuidstd.V4StringExpr,
		V7String:   uuidstd.V7StringExpr,
		ParseFunc:  uuidstd.ParseFunc,
	},
	uuidgoogle.ImportPath: {
		ImportPath: uuidgoogle.ImportPath,
		V4:         uuidgoogle.V4Expr,
		V7:         uuidgoogle.V7Expr,
		V4String:   uuidgoogle.V4StringExpr,
		V7String:   uuidgoogle.V7StringExpr,
		ParseFunc:  uuidgoogle.ParseFunc,
	},
	uuidgofrs.ImportPath: {
		ImportPath: uuidgofrs.ImportPath,
		V4:         uuidgofrs.V4Expr,
		V7:         uuidgofrs.V7Expr,
		V4String:   uuidgofrs.V4StringExpr,
		V7String:   uuidgofrs.V7StringExpr,
		ParseFunc:  uuidgofrs.ParseFunc,
	},
}

// UUIDIntegrationIn reports the UUID import path an `overrides.types` map
// selects, or "" when it names none.
//
// It exists so a package whose columns resolved no uuid-qualified type can
// still generate values with the library its config declares (PRD §7.4). The
// case it answers is a `uuid` column retyped to a Go `string`: the column is
// then no claim at all, so selection by resolved type alone would fall through
// to the standard library and emit a stdlib call in a package whose config
// plainly says google or gofrs.
//
// Detection is by import path rather than by key, matching detectIntegrationsIn
// and PRD §7.4's "integration detection is scope-independent": a config binds a
// UUID library by naming its import, whether it does so under `uuid` or under
// some other SQL type (the `tenancy` example declares google under `blob`).
//
// The minimum is taken rather than the first seen, so a map naming two UUID
// libraries picks the same one on every run (PRD §5.7) instead of depending on
// map iteration order. That config is odd but not rejectable here: §4.13's rule
// counts the libraries a package *imports*, and this path imports exactly one.
func UUIDIntegrationIn(overrides map[string]config.TypeOverride) string {
	selected := ""
	for _, override := range overrides {
		if _, ok := uuidIntegrations[override.Import]; !ok {
			continue
		}
		if selected == "" || override.Import < selected {
			selected = override.Import
		}
	}
	return selected
}

// UUIDIntegrationFor returns the generation binding for importPath.
//
// An empty path means neither the package's resolved columns nor its config
// named a UUID library, and gets the standard library — so a consumer who
// never mentions one pays no module dependency for enabling events
// (PRD §7.4, §28.8).
//
// A non-empty path sqlgen has no built-in knowledge of keeps its own import
// and takes the standard library's spellings — generating calls and ParseFunc
// alike — which are the only ones the PRD defines. The import always follows
// what the package's columns resolved to: substituting the standard library's
// import instead would put a second package named uuid into the generated
// package, which is the failure the one-library-per-package rule exists to
// prevent (PRD §7.4, see gen/uuid_library.go).
func UUIDIntegrationFor(importPath string) UUIDIntegration {
	if integ, ok := uuidIntegrations[importPath]; ok {
		return integ
	}
	integ := uuidIntegrations[uuidstd.ImportPath]
	if importPath != "" {
		integ.ImportPath = importPath
	}
	return integ
}
