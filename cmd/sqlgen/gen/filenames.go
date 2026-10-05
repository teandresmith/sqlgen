package gen

import "github.com/teandresmith/sqlgen/cmd/sqlgen/config"

// The two combined-body files emitted under config.LayoutSingleFile. Under
// config.LayoutFilePerTable each entity gets its own `<snake>_gen.go` instead,
// but these names stay in use as the goimports resolution anchor for the
// per-file emission paths — import resolution runs once over the concatenated
// bodies, so it needs one stable path to resolve against.
const (
	modelsFileName = "models_gen.go"
	viewsFileName  = "views_gen.go"
)

// TableFileName returns the base name of the generated Go file carrying tc's
// body under layout: `<snake>_gen.go` under config.LayoutFilePerTable, and the
// combined models_gen.go otherwise.
//
// This is the single source of truth for the mapping. The emission path
// (generateTablesPerFile / generateTablesSingleFile) and the manifest builder's
// per-entity `files[]` both read it, so a rename of the emitted file cannot
// leave the manifest naming a path that does not resolve — the drift any
// second derivation of one name invites.
//
// Any layout that is not LayoutFilePerTable resolves to the single-file name,
// mirroring generateTables' own dispatch so an unset Layout (config defaults
// fill it, but Build may be called with a hand-built config) cannot produce a
// third answer.
func TableFileName(tc TableContext, layout config.Layout) string {
	if layout == config.LayoutFilePerTable {
		return tc.SnakeName + "_gen.go"
	}
	return modelsFileName
}

// ViewFileName returns the base name of the generated Go file carrying vc's
// body under layout — the view half of TableFileName, combining into
// views_gen.go rather than models_gen.go.
func ViewFileName(vc ViewContext, layout config.Layout) string {
	if layout == config.LayoutFilePerTable {
		return vc.SnakeName + "_gen.go"
	}
	return viewsFileName
}
