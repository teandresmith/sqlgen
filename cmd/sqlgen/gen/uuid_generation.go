package gen

import (
	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
)

// This file resolves which UUID library a generated package generates values
// with, and wires the answer into the two places that emit a generating call:
// the app-strategy primary key (PRD §8.6) and the event ID (PRD §28.8). The
// per-library spellings themselves are gotype's (PRD §7.4 "Generating UUID
// values").
//
// The selection is read off the *built contexts* first, for the same reason
// validateUUIDLibraries reads them: every config surface that can bind a UUID
// library — global `overrides.types`, table-scoped `overrides.types`,
// `column_map.<col>.import` — has already been through the resolution chain by
// the time the contexts exist, and a rule written against the config text would
// have to re-implement that chain to know which surface won for a given column.
// Reading the contexts is also what makes the event-hooks import and the
// column-resolved import the same library *by construction* rather than by
// validation: they are literally the same string. That matters because the
// event-hooks import is not a claim validateUUIDLibraries can see, so a second
// library introduced there would pass validation and then fail to compile —
// which is exactly what the hardcoded `github.com/google/uuid` did.
//
// The config is consulted only when the contexts answer *nothing* — no column
// resolved uuid-qualified anywhere in the package. That is not the same
// question, and the contexts cannot answer it: a `uuid` column retyped to a Go
// `string` leaves a config that names a library and no resolved type to carry
// it. See selectUUIDIntegration for the full cascade.

// selectUUIDIntegration returns the UUID binding for the package the built
// contexts describe, in three steps (PRD §7.4):
//
//  1. What the package's columns actually resolved to. This wins because it is
//     what the generated files already spell — a generating call in any other
//     library would put a second package named uuid beside them.
//  2. What the global `overrides.types` names, when step 1 found nothing.
//  3. The standard library, when neither named anything.
//
// Step 2 exists because "no column resolved uuid-qualified" and "this package
// uses no UUID library" are different statements, and only step 1 can tell them
// apart. A `uuid` primary key retyped to a Go `string` — the shape an
// app-strategy PK most often takes — leaves the package with an
// explicit `overrides.types` binding and no claim to carry it, so selecting on
// resolved types alone emitted a standard-library call into a package whose
// config plainly said google. Nothing in that output is broken, which is why it
// went unnoticed: it compiles, and both libraries make valid v4 UUIDs. It is
// simply not the library the consumer asked for.
//
// Reading the config here does not reintroduce the duplication uuid_library.go
// avoids. Step 1 still answers "which library did this package resolve", off
// the contexts, exactly as before; step 2 answers a different question the
// contexts genuinely cannot — what the consumer declared when nothing resolved
// — and can only be read from the config text.
//
// validateUUIDLibraries has already rejected a package that resolved more than
// one library by the time this runs, so step 1 has at most one distinct path to
// find. The minimum is taken anyway rather than the first seen, so that if that
// rule is ever relaxed this stays deterministic (PRD §5.7) instead of depending
// on context build order.
func selectUUIDIntegration(cfg *config.RootConfig, ctxs generationContexts) gotype.UUIDIntegration {
	selected := ""
	for _, claim := range collectUUIDClaims(ctxs) {
		if selected == "" || claim.importPath < selected {
			selected = claim.importPath
		}
	}
	if selected == "" {
		selected = gotype.UUIDIntegrationIn(cfg.Overrides.Types)
	}
	return gotype.UUIDIntegrationFor(selected)
}

// attachUUIDGeneration wires the package's UUID binding into every table that
// generates its own primary key, filling PKAutoGenExpr and declaring the
// import that expression needs.
//
// Declaring the import here rather than leaving goimports to infer it is what
// makes the emitted file self-contained, the same reasoning arrayScanImports
// and the errgroup declaration in assembleTableContext use. It matters most
// for a primary key overridden to a Go `string`: no column then resolves to a
// uuid-qualified type, so nothing else in the table's import set mentions the
// library the generated call spells.
//
// It runs as a post-build pass because the binding is a whole-package fact
// derived from the contexts, and an individual table context cannot see the
// package it will be emitted into.
func attachUUIDGeneration(tables []TableContext, integ gotype.UUIDIntegration) {
	for i := range tables {
		t := &tables[i]
		if t.PKStrategy != config.PKStrategyApp || len(t.PKColumns) == 0 {
			continue
		}
		t.PKAutoGenExpr = uuidGenExpr(integ, t.UUIDVersion, t.PKColumns[0].GoType)
		t.Imports = UniqueImports(append(t.Imports, integ.ImportPath))
	}
}

// uuidGenExpr returns the generating call for a primary key of pkGoType. A
// primary key left on the resolved uuid.UUID takes the value form; one
// overridden to a Go `string` takes the string form, so the result stays
// assignable to `var pkValue string` (PRD §7.4).
func uuidGenExpr(integ gotype.UUIDIntegration, version config.UUIDVersion, pkGoType string) string {
	if pkGoType == "string" {
		return integ.StringExpr(version)
	}
	return integ.ValueExpr(version)
}
