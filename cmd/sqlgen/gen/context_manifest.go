package gen

import (
	"fmt"
	"path"
	"path/filepath"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/sql"
)

// manifestRuntimeImportPath is the import path of the runtime manifest package
// referenced by the generated manifest_embed_gen.go (PRD §30.6).
const manifestRuntimeImportPath = "github.com/teandresmith/sqlgen/manifest"

// ManifestEmbedFilename is the fixed name of the runtime-embed file the
// manifest stage writes into output.dir (PRD §30.6).
const ManifestEmbedFilename = "manifest_embed_gen.go"

// ManifestEmbedEnabled reports whether a generate run writes
// ManifestEmbedFilename into output.dir.
//
// It is the gate BuildManifestEmbedContext applies, named separately for
// callers that need the answer without a document to build a context from.
// `sqlgen diff` is one: gen.Generate does not emit the embed file — the
// manifest stage does, afterwards — so a preview run's written set omits it,
// and the stale sweep would report an existing manifest_embed_gen.go as a
// deletion that `sqlgen generate` never performs.
func ManifestEmbedEnabled(cfg *config.RootConfig) bool {
	m := cfg.Generation.Manifest
	return m != nil && boolPtr(m.Enabled, false) && boolPtr(m.EmbedInClient, true)
}

// ManifestEmbedContext is the data the "manifest-embed" template consumes to
// emit manifest_embed_gen.go — the file that embeds the on-disk manifest JSON
// and exposes it via the generated Client.Manifest / ManifestEntity /
// ManifestVersion methods (PRD §30.6). Paths are forward-slashed and relative
// to the output package directory (where //go:embed resolves them), matching
// where the JSON emitter writes.
type ManifestEmbedContext struct {
	Package           string   // output package name (for the file preamble)
	Imports           []string // preamble imports for renderAndWrite
	PerEntity         bool     // true when json_layout: per_entity
	EmbedJSONPath     string   // //go:embed target for the top-level JSON
	EmbedEntitiesPath string   // //go:embed all: target for the entities dir (per_entity only)
}

// BuildManifestEmbedContext returns the template context for the runtime-embed
// file, or nil when embedding is disabled (manifest off, or
// embed_in_client: false). The orchestrator calls this with the emitted
// entity count and renders the "manifest-embed" template only when the result
// is non-nil.
//
// entityCount is the number of per-entity files the JSON emitter writes (i.e.
// len(doc.Entities)). It guards a compile trap: in per_entity layout with zero
// entities the emitter never creates the entities directory, so a
// "//go:embed all:<dir>/entities" directive would fail to compile ("no matching
// files"). Such a package is downgraded to the single-file embed (top-level
// index only); LoadInto then reads the empty index and yields an empty entity
// map — behaviorally identical to the per_entity path with no entities.
func BuildManifestEmbedContext(cfg *config.RootConfig, entityCount int) *ManifestEmbedContext {
	if !ManifestEmbedEnabled(cfg) {
		return nil
	}
	m := cfg.Generation.Manifest

	markdownDir := strOrDefault(m.MarkdownDir, "manifest")
	jsonFilename := strOrDefault(m.JSONFilename, "manifest_gen.json")
	perEntity := m.JSONLayout == config.JSONLayoutPerEntity && entityCount > 0

	ctx := &ManifestEmbedContext{
		Package:       cfg.Output.Package,
		Imports:       []string{"embed", "io/fs", manifestRuntimeImportPath},
		PerEntity:     perEntity,
		EmbedJSONPath: path.Join(markdownDir, jsonFilename),
	}
	if perEntity {
		ctx.EmbedEntitiesPath = path.Join(markdownDir, strOrDefault(m.JSONPerEntityDir, "entities"))
	}
	return ctx
}

// RenderManifestEmbed writes manifest_embed_gen.go into outputDir from the
// given context (PRD §30.6). It is a no-op when ctx is nil (embedding disabled).
// The rendering lives in this package because the "manifest-embed" template and
// the renderAndWrite pipeline (preamble + goimports) are package-private; the
// manifest stage — which lives in cmd/sqlgen/manifest and therefore cannot be
// invoked from inside Generate — calls this exported entry point.
//
// The manifest-embed template uses no dialect-specific funcmap helpers, so the
// template set is loaded with the Postgres dialect purely to satisfy
// loadTemplates; the rendered output is dialect-independent.
func RenderManifestEmbed(ctx *ManifestEmbedContext, outputDir, version string) error {
	if ctx == nil {
		return nil
	}
	tmpl, err := loadTemplates(sql.NewPostgresDialect())
	if err != nil {
		return fmt.Errorf("loading templates for manifest embed: %w", err)
	}
	filePath := filepath.Join(outputDir, ManifestEmbedFilename)
	return renderAndWrite(tmpl, "manifest-embed", ctx, ctx.Package, ctx.Imports, filePath, version)
}

// boolPtr resolves a tri-state *bool against a default.
func boolPtr(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

// strOrDefault returns v when non-empty, else fallback.
func strOrDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
