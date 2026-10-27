package gen

import (
	"bytes"
	"fmt"
	"path"
	"path/filepath"
	"slices"

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

// manifestPackageDoc returns the one-line package doc comment that points an
// agent reading the generated Go source at the manifest, or "" when the
// manifest is disabled or breadcrumbs.package_doc resolves false.
//
// The pointer names <markdown_dir>/_index.md, or <markdown_dir>/<json_filename>
// when formats has no markdown (no _index.md is written then). Both paths are
// relative to the output package directory, forward-slashed.
func manifestPackageDoc(cfg *config.RootConfig) string {
	m := cfg.Generation.Manifest
	if m == nil || !boolPtr(m.Enabled, false) || !boolPtr(m.Breadcrumbs.PackageDoc, true) {
		return ""
	}
	dir := path.Clean(filepath.ToSlash(strOrDefault(m.MarkdownDir, "manifest")))
	target := path.Join(dir, "_index.md")
	if len(m.Formats) > 0 && !slices.Contains(m.Formats, config.ManifestFormatMarkdown) {
		target = path.Join(dir, strOrDefault(m.JSONFilename, "manifest_gen.json"))
	}
	return fmt.Sprintf("Package %s is generated by sqlgen. AI agents: read %s before consuming this package.", cfg.Output.Package, target)
}

// withPackageDoc places doc as the package doc comment of raw, a preamble that
// begins with its package clause. A blank line separates it from the generated
// header injectHeader prepends later, so the header stays a standalone comment
// group and the doc comment alone attaches to the package clause. An empty doc
// returns raw unchanged.
func withPackageDoc(doc string, raw []byte) []byte {
	if doc == "" {
		return raw
	}
	var buf bytes.Buffer
	buf.WriteString("\n// ")
	buf.WriteString(doc)
	buf.WriteByte('\n')
	buf.Write(raw)
	return buf.Bytes()
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
