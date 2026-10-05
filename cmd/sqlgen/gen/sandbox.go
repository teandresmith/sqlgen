package gen

import (
	"path/filepath"
	"strings"
)

// SandboxPath maps a generation output directory under root, so a run
// redirected into a throwaway tree writes the same relative shape it would
// have written for real: output.dir "internal/models" becomes
// "<root>/internal/models", and a top-level `schema_dir: graph` becomes
// "<root>/graph" instead of landing in the consumer's working tree.
//
// An empty root returns p untouched — the normal, un-redirected run. It is
// returned verbatim rather than cleaned because GenerateResult.Files is
// user-visible in `sqlgen generate --verbose`, and a run that redirects
// nothing should not reword the paths it reports.
//
// Every shape a config can spell is anchored under root, including the two
// that would otherwise escape it: an absolute dir (api.graphql.schema_dir may
// be absolute, PRD §26.5.8) loses its volume name and leading separator, and
// a dir that climbs above the project root (output.dir: ../shared/models)
// loses its leading ".." run. Two configured dirs that differ only in a
// stripped prefix therefore land on one sandbox path. That is why the pairing
// a caller needs is GenerateResult.WriteDirs, which records the mapping this
// function actually produced, rather than an inverse of it: re-deriving where
// the generator writes, instead of asking it, is the duplication that let the
// GraphQL dirs escape `sqlgen diff`'s output.dir redirect to begin with.
func SandboxPath(root, p string) string {
	if root == "" {
		return p
	}
	rel := filepath.Clean(p)
	rel = strings.TrimPrefix(rel, filepath.VolumeName(rel))
	rel = strings.TrimPrefix(rel, string(filepath.Separator))
	// Clean gathers every ".." of a relative path at the front, so dropping
	// that leading run is enough to keep the result inside root.
	for rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		rel = strings.TrimPrefix(rel[2:], string(filepath.Separator))
	}
	if rel == "" {
		rel = "."
	}
	return filepath.Join(root, rel)
}
