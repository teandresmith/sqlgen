package config

// TenancyConfig controls global tenancy behavior (PRD §29.2.1).
//
// When Enabled is true, every generated read and mutation on a detected
// tenanted table is auto-filtered by the tenant column. Column names the
// default tenant column checked against every parsed table during schema
// detection (§29.2.3). Required controls fail-closed semantics — when true
// (the safe default), a missing tenant in context is a hard error; when
// false, a missing tenant silently skips the filter.
type TenancyConfig struct {
	Enabled  bool          `yaml:"enabled"`
	Column   string        `yaml:"column"`
	Required *bool         `yaml:"required"`
	Type     *TypeOverride `yaml:"type"`
}

// TableTenancyConfig overrides tenancy behavior for a specific table.
// All fields are pointers for tri-state semantics: nil means inherit from
// global. A per-table Enabled: false opt-out wins over global Enabled: true
// even when the tenant column exists on the table (PRD §29.2.3 rule 4).
type TableTenancyConfig struct {
	Enabled  *bool         `yaml:"enabled"`
	Column   *string       `yaml:"column"`
	Required *bool         `yaml:"required"`
	Type     *TypeOverride `yaml:"type"`
}

// applyTenancyDefaults fills unset fields on cfg.Tenancy with the built-in
// defaults documented in PRD §29.2.1. Nothing is done when the YAML has no
// `tenancy:` block. When the block is present but `required` is unset, the
// safe default true is applied — missing-tenant silently skipping the filter
// would recreate the exact footgun tenancy is meant to prevent.
func applyTenancyDefaults(cfg *RootConfig) {
	if cfg.Tenancy == nil {
		return
	}
	if cfg.Tenancy.Required == nil {
		cfg.Tenancy.Required = new(true)
	}
}

// ResolveTableTenancyEnabled returns whether tenancy is enabled for a
// specific table. Resolution order: per-table override → global
// tenancy.enabled → false.
func ResolveTableTenancyEnabled(table TableConfig, global *TenancyConfig) bool {
	if table.Tenancy != nil && table.Tenancy.Enabled != nil {
		return *table.Tenancy.Enabled
	}
	if global != nil {
		return global.Enabled
	}
	return false
}

// ResolveTableTenancyColumn returns the effective tenant column name for a
// table. Resolution order: per-table override → global tenancy.column → "".
// An empty return value means tenancy is not configured for this table.
func ResolveTableTenancyColumn(table TableConfig, global *TenancyConfig) string {
	if table.Tenancy != nil && table.Tenancy.Column != nil {
		return *table.Tenancy.Column
	}
	if global != nil {
		return global.Column
	}
	return ""
}

// ResolveTableTenancyRequired returns whether a missing tenant in context is
// a hard error for this table. Resolution order: per-table override → global
// tenancy.required → true (safe default).
//
// The default is true because silently skipping the filter on a missing
// tenant would recreate the exact footgun tenancy is meant to prevent
// (PRD §29.2.1).
func ResolveTableTenancyRequired(table TableConfig, global *TenancyConfig) bool {
	if table.Tenancy != nil && table.Tenancy.Required != nil {
		return *table.Tenancy.Required
	}
	if global != nil && global.Required != nil {
		return *global.Required
	}
	return true
}

// ResolveTableTenancyType returns the effective TypeOverride for the tenant
// column on a table, or nil when no YAML-level override is configured.
// Resolution order: per-table override → global tenancy.type → nil.
//
// A nil return means the generator falls through to parsed-schema type
// detection (§29.2.4) — per-table overrides, global overrides, the
// overrides.types map, and finally the parser's default for the SQL type.
func ResolveTableTenancyType(table TableConfig, global *TenancyConfig) *TypeOverride {
	if table.Tenancy != nil && table.Tenancy.Type != nil {
		return table.Tenancy.Type
	}
	if global != nil && global.Type != nil {
		return global.Type
	}
	return nil
}

// ResolveViewTenancyEnabled returns whether tenancy is enabled for a specific
// view. Resolution order: per-view override → global tenancy.enabled → false.
//
// Views resolve tenancy through their own set of accessors rather than
// borrowing the table ones: ViewConfig and TableConfig are distinct types, and
// a view's block is deliberately read-path-only (PRD §29.2.5).
func ResolveViewTenancyEnabled(view ViewConfig, global *TenancyConfig) bool {
	if view.Tenancy != nil && view.Tenancy.Enabled != nil {
		return *view.Tenancy.Enabled
	}
	if global != nil {
		return global.Enabled
	}
	return false
}

// ResolveViewTenancyColumn returns the effective tenant column name for a
// view. Resolution order: per-view override → global tenancy.column → "".
// An empty return value means tenancy is not configured for this view.
func ResolveViewTenancyColumn(view ViewConfig, global *TenancyConfig) string {
	if view.Tenancy != nil && view.Tenancy.Column != nil {
		return *view.Tenancy.Column
	}
	if global != nil {
		return global.Column
	}
	return ""
}

// ResolveViewTenancyRequired returns whether a missing tenant in context is a
// hard error for this view. Resolution order: per-view override → global
// tenancy.required → true (safe default), matching the table rule in
// PRD §29.2.1.
func ResolveViewTenancyRequired(view ViewConfig, global *TenancyConfig) bool {
	if view.Tenancy != nil && view.Tenancy.Required != nil {
		return *view.Tenancy.Required
	}
	if global != nil && global.Required != nil {
		return *global.Required
	}
	return true
}

// ResolveViewTenancyType returns the effective TypeOverride for the tenant
// column on a view, or nil when no YAML-level override is configured.
// Resolution order: per-view override → global tenancy.type → nil.
//
// A nil return means the generator falls through to the view column's own
// type resolution (§29.2.4 step 3) — the @type annotation literal when the
// view declares one, otherwise the parser's default for the SQL type.
func ResolveViewTenancyType(view ViewConfig, global *TenancyConfig) *TypeOverride {
	if view.Tenancy != nil && view.Tenancy.Type != nil {
		return view.Tenancy.Type
	}
	if global != nil && global.Type != nil {
		return global.Type
	}
	return nil
}
