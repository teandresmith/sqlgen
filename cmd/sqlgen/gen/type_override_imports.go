package gen

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// This file rejects a by-SQL-type Go type literal whose package could never be
// imported. It is the by-SQL-type twin of validateColumnTypeLiterals
// (context_table.go): same predicate, same division of labour (PRD §8.5),
// applied to the three surfaces that declare a whole TypeOverride or a struct
// field rather than a bare per-column literal — `overrides.types`,
// `tables.<t>.overrides.types`, and `extras.<T>.fields`.
//
// The other surfaces that can name a Go type already carry this rule:
// `type_map` and `column_map.<col>.type` in validateColumnTypeLiterals, a view
// `@type` directive in parser.parseTypeDirective, and `tenancy.type` at all
// three of its scopes in config.validateTenancyTypeOverride. These three were
// the remainder.
//
// What makes the omission quiet is that goimports back-fills whatever the
// declaration left out, so the generated package is not obviously wrong — it is
// wrong in one of three ways, none of them what the config says:
//
//   - `uuid.UUID` with no import binds the *standard library* `uuid` package,
//     which Go 1.27 ships. A config that meant github.com/google/uuid gets a
//     different library and compiles clean.
//   - `decimal.Decimal` with no import binds whatever the generating machine's
//     module cache happens to hold, so one config emits different imports on
//     different machines — the determinism PRD §5.7 requires, lost.
//   - a package goimports cannot place at all is emitted with no import at all:
//     the struct field references a package nothing imports, `generate` exits
//     0, and the Go compiler is the first thing to object.
//
// The third is the `column_map.<col>.type` failure exactly, which is why this is the same rule
// rather than a new one. The first is worse than a compile error: an import-less
// declaration reaches validateUUIDLibraries as a *non*-claim, because
// collectUUIDClaims keys on the resolved import path and there is none. A
// package can therefore bind two UUID libraries — google in models_gen.go,
// the standard library in types_gen.go — and pass the §4.13 rule written to
// forbid precisely that.
//
// Rejecting rather than back-filling the import from the detected integration
// is deliberate. detectIntegrationsIn keys on `override.Import`, so filling it
// would turn entries that are not competing claims into claims, moving two
// shipped rules at once: §4.13's one-library count and enrichment eligibility
// (PRD §7.4; the disposition 26.3 recorded and 26.3a routed here).
//
// It lives in gen rather than config.ValidatePreParse for the reason
// validateColumnTypeLiterals does: the registry it consults is gotype's, and
// gotype imports config, so the reverse edge would be a cycle. `sqlgen validate`
// still reports it — ValidateGeneration runs every generation-phase check.

// validateTypeOverrideImports rejects every by-SQL-type declaration whose Go
// type names a package the generated file would not import.
//
// Config text is the input rather than the built contexts, matching
// validateColumnTypeLiterals: a typo in an override for a SQL type no column
// happens to use is still a typo, and hearing about it here beats hearing about
// it after adding the column. Every violation is reported, not just the first,
// because one `sqlgen validate` run answers for the whole config.
func validateTypeOverrideImports(cfg *config.RootConfig) error {
	errs := typeOverrideImportErrors("overrides.types", cfg.Overrides.Types)

	for _, table := range slices.Sorted(maps.Keys(cfg.Tables)) {
		tableCfg := cfg.Tables[table]
		if tableCfg.Overrides == nil {
			continue
		}
		scope := fmt.Sprintf("tables.%s.overrides.types", table)
		errs = append(errs, typeOverrideImportErrors(scope, tableCfg.Overrides.Types)...)
	}

	for _, name := range slices.Sorted(maps.Keys(cfg.Extras)) {
		extra := cfg.Extras[name]
		for _, field := range slices.Sorted(maps.Keys(extra.Fields)) {
			f := extra.Fields[field]
			if f.Import != "" || !needsExplicitImport(f.Type) {
				continue
			}
			errs = append(errs, unimportableTypeError(
				fmt.Sprintf("extras.%s.fields.%s", name, field), f.Type,
			))
		}
	}

	return errors.Join(errs...)
}

// typeOverrideImportErrors reports the entries of one `overrides.types` map
// whose Go type needs an import and has none. scope is the config path the map
// was written at, so the message names the line to edit.
func typeOverrideImportErrors(scope string, types map[string]config.TypeOverride) []error {
	var errs []error
	// Sorted so `validate` prints the same report twice in a row (PRD §5.7).
	// SQL type keys are reported as written — `double precision`, `varchar(255)`
	// — for the reason typeOverrideSource reports them that way: a normalized
	// spelling appears in no config file.
	for _, sqlType := range slices.Sorted(maps.Keys(types)) {
		override := types[sqlType]
		switch {
		case override.Import == "" && needsExplicitImport(override.Type):
			// The nullable variant inherits this import when it declares none
			// of its own, so a missing parent import is one problem even when
			// both halves are package-qualified. Reporting it once keeps the
			// message pointing at the one field that fixes it.
			errs = append(errs, unimportableTypeError(scope+"."+sqlType, override.Type))
		case override.Import == "" && override.Nullable.Import == "" && needsExplicitImport(override.Nullable.Type):
			// The parent needed no import — a builtin, a registry member or a
			// same-package type — so there is none for the nullable variant to
			// inherit, and `nullable: pkg.NullT` would resolve to a package
			// nothing imports. The `nullable: uuid.NullUUID` shorthand is
			// untouched: its parent carries the import it inherits.
			errs = append(errs, unimportableTypeError(
				scope+"."+sqlType+".nullable", override.Nullable.Type,
			))
		}
	}
	return errs
}

// unimportableTypeError renders one violation. The wording matches the
// `column_map.<col>.type` message so the two halves of the rule read alike.
func unimportableTypeError(path, literal string) error {
	return fmt.Errorf(
		"%s.type: %q names a package-qualified type — set %s.import to the package it comes from",
		path, literal, path,
	)
}
