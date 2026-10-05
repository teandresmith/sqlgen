package manifest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
)

// CleanInput carries what CleanStale needs to prune the manifest surface.
// Doc is nil when the manifest is disabled — the signal to remove the whole
// manifest directory rather than diff against the current entity set.
type CleanInput struct {
	Config    *config.RootConfig
	Doc       *Document
	OutputDir string
	GraphDir  string
}

// CleanStale removes orphaned manifest artifacts per PRD §23.8 / §30.3 and
// returns the paths it deleted (directories with a trailing "/") for the CLI to
// report. It runs on every generation (enabled or not):
//
//   - Disabled (Doc nil or manifest.enabled false): the
//     <output.dir>/<markdown_dir>/ directory is removed *when sqlgen can prove
//     it wrote it*, the sqlgen-owned breadcrumbs are deleted, and
//     manifest_embed_gen.go is removed. Breadcrumbs lacking the provenance
//     marker (user-authored or user-adopted) are left untouched, as is a
//     manifest directory sqlgen does not own (PRD §30.3 ownership rule).
//   - Enabled: per-flag breadcrumb opt-outs are honored (marker-gated), the
//     embed file is removed when embed_in_client is false, per-entity markdown /
//     JSON files for tables no longer in the schema are removed, and the
//     per-entity JSON directory is pruned and dropped when the layout is single.
//
// Deleting only what sqlgen can show it authored keeps the deletion side of the
// pipeline symmetric with the emission side. §23.8's governing rule is that
// files sqlgen did not generate are never touched; §30.3 is the manifest's
// governed exception to the `_gen.go`-only form of that rule, not an exception
// to the rule itself.
func CleanStale(in CleanInput) ([]string, error) {
	cfg := in.Config
	m := cfg.Generation.Manifest
	markdownDir := filepath.Join(in.OutputDir, dirOrDefault(markdownDirOf(m), "manifest"))
	embedFile := filepath.Join(in.OutputDir, gen.ManifestEmbedFilename)

	var r remover

	if in.Doc == nil || !manifestEnabled(cfg) {
		if err := r.manifestDir(in.OutputDir, markdownDir, jsonFilenameOf(m)); err != nil {
			return r.deleted, err
		}
		if err := r.breadcrumbs(in.OutputDir, in.GraphDir, true, true); err != nil {
			return r.deleted, err
		}
		return r.deleted, r.file(embedFile)
	}

	// Enabled — prune per-flag opt-outs and orphans.
	if err := r.breadcrumbs(in.OutputDir, in.GraphDir, !deref(m.Breadcrumbs.ClaudeMD), !deref(m.Breadcrumbs.AgentsMD)); err != nil {
		return r.deleted, err
	}
	if m.EmbedInClient != nil && !*m.EmbedInClient {
		if err := r.file(embedFile); err != nil {
			return r.deleted, err
		}
	}

	// Stale markdown: keep _index.md, _conventions.md, and one <file_prefix>.md
	// per current entity; remove any other *.md (a removed table's file).
	//
	// Containment-guarded like the two directory removals below. Validation
	// rejects a markdown_dir that escapes output.dir, but this pass also runs
	// against directly-constructed configs that never went through LoadConfig,
	// and an unguarded glob-and-delete outside the output tree is the same
	// defect this file already exists to prevent.
	expectedMD := map[string]bool{"_index.md": true, "_conventions.md": true}
	for _, e := range in.Doc.Entities {
		expectedMD[e.FilePrefix+".md"] = true
	}
	if within(in.OutputDir, markdownDir) {
		if err := r.prune(markdownDir, "*.md", expectedMD); err != nil {
			return r.deleted, err
		}
	}

	// Stale per-entity JSON. In per_entity layout keep one <file_prefix>.json per
	// current entity; in single layout the entities directory should not exist,
	// so drop it (e.g. after a per_entity → single flip).
	entitiesDir := filepath.Join(markdownDir, dirOrDefault(perEntityDirOf(m), "entities"))
	if m.JSONLayout == config.JSONLayoutPerEntity {
		if !within(markdownDir, entitiesDir) {
			return r.deleted, nil
		}
		expectedJSON := make(map[string]bool, len(in.Doc.Entities))
		for _, e := range in.Doc.Entities {
			expectedJSON[e.FilePrefix+".json"] = true
		}
		return r.deleted, r.prune(entitiesDir, "*.json", expectedJSON)
	}
	return r.deleted, r.entitiesDir(markdownDir, entitiesDir)
}

// manifestIndexMarker is the literal a sqlgen-written manifest/_index.md
// carries, rendered by templates/_index.md.tmpl from Document.Generator. It is
// the markdown half of the ownership proof below; TestManifestIndexAnchorDrift
// pins the emitted output against it, so a template edit that drops the
// Generator line fails a test rather than silently disowning every manifest
// directory in the wild.
const manifestIndexMarker = "**Generator:** " + generatorName

// manifestIndexFilename is the fixed name of the manifest index. Unlike the
// JSON filename it is not configurable, which is what lets the disabled path
// recognize a manifest directory it has no config block for.
const manifestIndexFilename = "_index.md"

// sqlgenOwnsManifestDir reports whether dir was written by a prior
// `manifest.enabled: true` run — the precondition PRD §30.3 states for removing
// the manifest directory ("Setting manifest.enabled: false *after a prior
// true*") and the one thing a containment check cannot establish.
//
// Proof is the emitted content itself, so no marker file has to be introduced
// and no on-disk state has to be carried between runs: an enabled run always
// writes _index.md (markdown format) or <json_filename> (json format), and
// validateManifestEnabledFields requires at least one format, so at least one
// anchor is always present. A directory holding neither is not sqlgen's and is
// left alone — the same adoptable, soft ownership the breadcrumbs use, where
// deleting the marker disowns the file.
//
// It genuinely fails closed: probing cannot fail, so it returns a plain bool.
// A directory that does not exist, that cannot be read, that holds unparseable
// content, or that holds a manifest written under a json_filename the current
// config no longer names all report false. That costs at worst a stale
// directory the consumer can delete by hand, and it never costs a directory
// sqlgen did not write — nor does it abort a generation over a file sqlgen has
// no claim to in the first place.
//
// jsonFilename is reduced to its base name before use. It is otherwise
// unvalidated config (applyManifestDefaults only fills it in), so a value
// carrying separators would look for the anchor outside dir and could
// re-authorize removing a directory on the strength of a manifest somewhere
// else entirely. The proof has to come from inside the directory being removed.
func sqlgenOwnsManifestDir(dir, jsonFilename string) bool {
	if fileContains(filepath.Join(dir, manifestIndexFilename), manifestIndexMarker) {
		return true
	}
	return isSqlgenManifestJSON(filepath.Join(dir, filepath.Base(jsonFilename)))
}

// fileContains reports whether the file at path can be read and contains
// marker. Absence — and unreadability — is not ownership.
func fileContains(path, marker string) bool {
	return bytes.Contains(probeFile(path), []byte(marker))
}

// isSqlgenManifestJSON reports whether path is a manifest JSON sqlgen emitted,
// read off the generator.name field of PRD §30.4 rather than off any formatting
// detail. Content that is absent or is not the expected shape reports false:
// an unreadable or unparseable file is simply not proof of ownership.
func isSqlgenManifestJSON(path string) bool {
	content := probeFile(path)
	if content == nil {
		return false
	}
	var probe struct {
		Generator struct {
			Name string `json:"name"`
		} `json:"generator"`
	}
	if json.Unmarshal(content, &probe) != nil {
		return false
	}
	return probe.Generator.Name == generatorName
}

// probeFile reads path for an ownership check, returning nil when it cannot be
// read for any reason. Swallowing the error is the point: this is a probe of a
// path sqlgen may have no claim to, and the only thing a failed read can
// justify is declining to delete. Surfacing it instead would let an unreadable
// file in someone else's directory fail the whole generation.
func probeFile(path string) []byte {
	content, err := os.ReadFile(path) //nolint:gosec // path is a config-derived manifest artifact path
	if err != nil {
		return nil
	}
	return content
}

// within reports whether target is strictly contained in base (not equal to it,
// not escaping it via "..").
func within(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return rel != "." && !strings.HasPrefix(rel, "..")
}

// remover carries out the removals CleanStale decides on and accumulates the
// paths it actually deleted, so a manifest removal is reported alongside the
// stale _gen.go sweep instead of happening silently (PRD §23.8). Directories
// are recorded with a trailing "/" to distinguish them from files in that
// report. Only paths genuinely removed are recorded — a missing file, a
// user-owned breadcrumb, and an unowned manifest directory all record nothing.
type remover struct {
	deleted []string
}

// manifestDir removes target only when it passes both gates: it is strictly
// inside base, and sqlgen can prove it wrote it.
//
// Containment stops a misconfigured dir *name* from escalating into a wipe of
// the output tree — a disabled config carrying an explicit markdown_dir "." (or
// one escaping via "..") resolves target back to, or above, base, and the
// disabled path gets no placement validation (checkManifestDirPlacement runs
// only when enabled). Ownership stops a correctly-configured name from wiping a
// directory that was never sqlgen's: output.dir defaults to "." (PRD §4.2), so
// <output.dir>/manifest/ routinely resolves onto a consumer's own directory,
// and the disabled branch is what every default run takes.
func (r *remover) manifestDir(base, target, jsonFilename string) error {
	if !within(base, target) {
		return nil
	}
	if !sqlgenOwnsManifestDir(target, jsonFilename) {
		return nil
	}
	return r.tree(target)
}

// entitiesDir drops the per-entity JSON directory after a per_entity → single
// layout flip. It prunes the *.json files sqlgen emits there and removes the
// directory only once empty, which needs no entity prefixes and leaves anything
// else a consumer put inside untouched.
func (r *remover) entitiesDir(base, target string) error {
	if !within(base, target) {
		return nil
	}
	if err := r.prune(target, "*.json", nil); err != nil {
		return err
	}
	return r.dirIfEmpty(target)
}

// breadcrumbs removes CLAUDE.md / AGENTS.md from the output dir (and the graph
// package dir when set — nested or sibling), each only when it carries the
// sqlgen provenance marker. removeClaude / removeAgents gate which of the pair
// is considered.
func (r *remover) breadcrumbs(outputDir, graphDir string, removeClaude, removeAgents bool) error {
	dirs := []string{outputDir}
	if graphDir != "" {
		dirs = append(dirs, graphDir)
	}
	for _, d := range dirs {
		if removeClaude {
			if err := r.ownedFile(filepath.Join(d, "CLAUDE.md")); err != nil {
				return err
			}
		}
		if removeAgents {
			if err := r.ownedFile(filepath.Join(d, "AGENTS.md")); err != nil {
				return err
			}
		}
	}
	return nil
}

// ownedFile removes path only when it carries the provenance marker (PRD §30.3
// ownership rule). A missing file or a user-owned file is a no-op.
func (r *remover) ownedFile(path string) error {
	owned, err := BreadcrumbIsSqlgenOwned(path)
	if err != nil || !owned {
		return err
	}
	return r.file(path)
}

// prune removes every file in dir matching pattern whose base name is not in
// expected. A nil expected map prunes every match. A non-existent dir yields no
// matches (no error).
func (r *remover) prune(dir, pattern string, expected map[string]bool) error {
	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		return fmt.Errorf("globbing %s/%s: %w", dir, pattern, err)
	}
	for _, match := range matches {
		if expected[filepath.Base(match)] {
			continue
		}
		if err := r.file(match); err != nil {
			return err
		}
	}
	return nil
}

// file removes path, tolerating a missing file.
func (r *remover) file(path string) error {
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("removing %s: %w", path, err)
	}
	r.record(path)
	return nil
}

// tree removes dir and its contents, tolerating a missing directory.
func (r *remover) tree(dir string) error {
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("removing %s: %w", dir, err)
	}
	r.recordDir(dir)
	return nil
}

// dirIfEmpty removes dir when it holds nothing, leaving a directory a consumer
// still has files in. A missing directory is a no-op.
func (r *remover) dirIfEmpty(dir string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading %s: %w", dir, err)
	}
	if len(entries) > 0 {
		return nil
	}
	if err := os.Remove(dir); err != nil {
		return fmt.Errorf("removing %s: %w", dir, err)
	}
	r.recordDir(dir)
	return nil
}

func (r *remover) record(path string) {
	r.deleted = append(r.deleted, filepath.ToSlash(path))
}

func (r *remover) recordDir(dir string) {
	r.deleted = append(r.deleted, filepath.ToSlash(dir)+"/")
}

// markdownDirOf / perEntityDirOf / jsonFilenameOf read the configured names,
// tolerating a nil manifest config (the disabled-cleanup path may run with
// m == nil).
func markdownDirOf(m *config.ManifestConfig) string {
	if m == nil {
		return ""
	}
	return m.MarkdownDir
}

func perEntityDirOf(m *config.ManifestConfig) string {
	if m == nil {
		return ""
	}
	return m.JSONPerEntityDir
}

func jsonFilenameOf(m *config.ManifestConfig) string {
	if m == nil {
		return defaultJSONFilename
	}
	return dirOrDefault(m.JSONFilename, defaultJSONFilename)
}
