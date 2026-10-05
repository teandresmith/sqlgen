package manifest

import (
	"fmt"
	"path/filepath"
	"slices"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// StageInput bundles the read-only state RunStage needs. Tables / Views / API
// are the in-memory contexts that produced the _gen.go files (from
// gen.GenerateResult) so the manifest reflects the exact generated surface
// without re-parsing. Timestamp pins Document.GeneratedAt (from the
// --manifest-timestamp flag, or time.Now().UTC() at the caller).
type StageInput struct {
	Schema    *parser.Schema
	Config    *config.RootConfig
	Tables    []gen.TableContext
	Views     []gen.ViewContext
	Enums     []gen.EnumContext
	API       *gen.APIContext
	Version   string
	Timestamp string
}

// StageResult carries what the manifest stage produced for the CLI to report:
// the user-owned-file warnings breadcrumb emission surfaced, and the artifact
// paths stale cleanup deleted (directories with a trailing "/"). Deleted folds
// into the same reported set as the stale _gen.go sweep so a manifest removal
// is never silent (PRD §23.8).
type StageResult struct {
	Warnings []string
	Deleted  []string
}

// RunStage builds and emits every manifest artifact (JSON, markdown, agent
// breadcrumbs, and the runtime-embed file) then prunes stale files. It is the
// manifest pipeline stage (PRD §30.3) and runs after all _gen.go files are
// written. Because the manifest package imports gen, this stage cannot live
// inside gen.Generate (import cycle); the CLI invokes it after gen.Generate
// returns.
//
// It returns the user-owned-file warnings surfaced by breadcrumb emission and
// the paths stale cleanup deleted, both for the caller to print. The result is
// non-nil whenever cleanup ran, error or not, so a partial sweep is still
// reported; it is nil only when the stage failed before reaching cleanup. When the
// manifest is disabled (no manifest block, or manifest.enabled: false) RunStage
// emits nothing and only runs cleanup — which clears any artifacts a prior
// enabled run left behind — so disabled-by-default produces zero artifacts and
// zero warnings.
func RunStage(in StageInput) (*StageResult, error) {
	cfg := in.Config
	outputDir := cfg.Output.Dir
	graphDir := ResolveGraphBreadcrumbDir(cfg)

	if !manifestEnabled(cfg) {
		// The deleted set travels with the error too: cleanup removes files one
		// at a time, so a mid-sweep failure still leaves some gone, and dropping
		// the list there would reopen the silent-deletion hole on the one path
		// where the consumer most needs to know what happened.
		deleted, err := CleanStale(CleanInput{Config: cfg, OutputDir: outputDir, GraphDir: graphDir})
		return &StageResult{Deleted: deleted}, err
	}

	m := cfg.Generation.Manifest
	doc, err := Build(BuildInput{
		Schema:    in.Schema,
		Config:    cfg,
		Tables:    in.Tables,
		Views:     in.Views,
		Enums:     in.Enums,
		API:       in.API,
		Version:   in.Version,
		Timestamp: in.Timestamp,
	})
	if err != nil {
		return nil, fmt.Errorf("building manifest: %w", err)
	}

	if formatEnabled(m, config.ManifestFormatJSON) {
		if err := EmitJSON(doc, m, outputDir); err != nil {
			return nil, err
		}
	}
	if formatEnabled(m, config.ManifestFormatMarkdown) {
		if err := EmitMarkdown(doc, m, outputDir); err != nil {
			return nil, err
		}
	}

	warnings, err := EmitBreadcrumbs(BreadcrumbsInput{
		Doc:         doc,
		Breadcrumbs: m.Breadcrumbs,
		OutputDir:   outputDir,
		MarkdownDir: m.MarkdownDir,
		GraphDir:    graphDir,
	})
	if err != nil {
		return nil, err
	}

	// PRD §30.6: manifest_embed_gen.go embeds the on-disk JSON onto the Client.
	// BuildManifestEmbedContext returns nil (→ RenderManifestEmbed no-ops) when
	// embed_in_client is false; the stale-cleanup pass then removes any prior
	// embed file. The entity count downgrades a zero-entity per_entity package
	// to the single-file embed so the //go:embed directive never targets a
	// missing directory.
	embedCtx := gen.BuildManifestEmbedContext(cfg, len(doc.Entities))
	if err := gen.RenderManifestEmbed(embedCtx, outputDir, in.Version); err != nil {
		return nil, fmt.Errorf("rendering manifest embed: %w", err)
	}

	deleted, err := CleanStale(CleanInput{Config: cfg, Doc: doc, OutputDir: outputDir, GraphDir: graphDir})
	return &StageResult{Warnings: warnings, Deleted: deleted}, err
}

// ResolveGraphBreadcrumbDir returns the resolved graph package directory when
// GraphQL is enabled and the graph package occupies a directory distinct from
// the models output dir, else "". A non-empty result drives EmitBreadcrumbs /
// CleanStale to also manage the graph/CLAUDE.md + graph/AGENTS.md breadcrumbs,
// whose models-manifest pointer adapts to the graph package's location — nested
// under models (../manifest/_index.md) or a top-level sibling
// (../models/manifest/_index.md) — via a relative path (PRD §30.3 GraphQL
// breadcrumbs).
//
// It returns "" when GraphQL is disabled, when the graph dir is unresolved, or
// when the graph dir resolves to the models output dir itself: in that last case
// the models breadcrumb already covers the package, and a separate graph
// breadcrumb would collide with it on <output.dir>/CLAUDE.md.
func ResolveGraphBreadcrumbDir(cfg *config.RootConfig) string {
	if !graphQLEnabled(cfg) {
		return ""
	}
	graphDir := cfg.API.GraphQL.ResolverDir
	if graphDir == "" {
		return ""
	}
	rel, err := filepath.Rel(cfg.Output.Dir, graphDir)
	if err != nil || rel == "." {
		return ""
	}
	return graphDir
}

// manifestEnabled reports whether the global manifest toggle resolves true.
func manifestEnabled(cfg *config.RootConfig) bool {
	m := cfg.Generation.Manifest
	return m != nil && deref(m.Enabled)
}

// formatEnabled reports whether the given output format (json / markdown) is
// selected. An unset Formats list means defaults were not applied (e.g. a
// hand-built config); it is treated as the PRD §30.2 default of both formats so
// emission never silently drops an artifact.
func formatEnabled(m *config.ManifestConfig, format string) bool {
	if len(m.Formats) == 0 {
		return true
	}
	return slices.Contains(m.Formats, format)
}
