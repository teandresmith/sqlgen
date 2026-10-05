package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
)

// makeDiffRunE returns the RunE for the diff command. It runs generation with
// every write redirected into a temp tree and compares the result against the
// working tree, reporting created, modified, and deleted files without
// touching anything on disk (PRD §23.6).
//
// The redirect is gen.GenerateInto's, not this command's. Redirecting
// output.dir here — the obvious-looking shape — only moves the models tree:
// api.graphql.schema_dir / resolver_dir are root-relative (§26.5.8) and
// resolve independently, so a GraphQL-enabled project had its whole graph
// package rewritten by a command specified to write nothing, with the temp dir
// baked into the models import path, leaving the tree uncompilable until the
// next real generate.
//
// Scope note: the manifest stage (PRD §30.3) runs after gen.Generate in
// `sqlgen generate` and is not previewed here. Its JSON and markdown artifacts
// carry a generated_at timestamp, so byte-comparing them would report a change
// on every run; the one artifact that would otherwise read as stale — the
// manifest embed file, which gen does not write — is accounted for below.
func makeDiffRunE(flags *cliFlags) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		out := cmd.OutOrStdout()
		errOut := cmd.ErrOrStderr()

		cfg, schema, err := loadAndValidate(flags, errOut)
		if err != nil {
			return err
		}

		tmpDir, err := os.MkdirTemp("", "sqlgen-diff-*")
		if err != nil {
			return &exitError{code: ExitGeneration, err: fmt.Errorf("creating temp dir: %w", err)}
		}
		defer func() { _ = os.RemoveAll(tmpDir) }()

		result, err := gen.GenerateInto(schema, cfg, Version, tmpDir)
		if err != nil {
			_, _ = fmt.Fprintf(errOut, "Generation error: %v\n", err)
			return &exitError{code: ExitGeneration, err: err}
		}

		changes, err := diffFiles(result, cfg)
		if err != nil {
			return &exitError{code: ExitGeneration, err: fmt.Errorf("comparing files: %w", err)}
		}

		if len(changes) == 0 {
			if !flags.quiet {
				_, _ = fmt.Fprintln(out, "no changes detected")
			}
			return nil
		}

		printDiffSummary(out, changes)
		return &exitError{code: ExitGeneration, err: fmt.Errorf("%d files would change", len(changes))}
	}
}

// fileChange represents a single file difference in the diff output.
type fileChange struct {
	kind string // "create", "modify", or "delete"
	path string // path the file occupies in the working tree
}

// diffFiles compares the files a redirected run wrote against their
// working-tree counterparts, then adds the deletions the same run's stale
// sweep would perform.
func diffFiles(result *gen.GenerateResult, cfg *config.RootConfig) ([]fileChange, error) {
	changes, err := compareGeneratedFiles(result)
	if err != nil {
		return nil, err
	}

	if cfg.Output.Layout == config.LayoutFilePerTable {
		deleted, err := findDeletedFiles(result, cfg)
		if err != nil {
			return nil, err
		}
		changes = append(changes, deleted...)
	}

	return changes, nil
}

// compareGeneratedFiles classifies each generated file as "create" (absent
// from the working tree) or "modify" (present with different content). Files
// that match byte for byte contribute nothing.
//
// Every file is covered, not just those under output.dir: the GraphQL schema,
// scalar, translator and resolver files are part of what `generate` writes, so
// leaving them out would under-report drift on exactly the projects whose
// graph package this command used to corrupt.
func compareGeneratedFiles(result *gen.GenerateResult) ([]fileChange, error) {
	var changes []fileChange
	for _, written := range result.Files {
		target, err := workingTreePath(result, written)
		if err != nil {
			return nil, err
		}

		generated, err := os.ReadFile(written) //nolint:gosec // path comes from the generator's own written-file list
		if err != nil {
			return nil, fmt.Errorf("reading generated file %s: %w", filepath.Base(written), err)
		}

		existing, err := os.ReadFile(target) //nolint:gosec // paths constructed from config output dirs
		if err != nil {
			if os.IsNotExist(err) {
				changes = append(changes, fileChange{kind: "create", path: target})
				continue
			}
			return nil, fmt.Errorf("reading existing file %s: %w", target, err)
		}

		if !bytes.Equal(generated, existing) {
			changes = append(changes, fileChange{kind: "modify", path: target})
		}
	}

	slices.SortFunc(changes, func(a, b fileChange) int { return strings.Compare(a.path, b.path) })
	return changes, nil
}

// workingTreePath maps a file written by a redirected run back to the path it
// would occupy in the working tree, using the directory mapping the generator
// reported.
//
// An unmapped directory means a write landed somewhere gen.GenerateInto did
// not declare, i.e. somewhere the redirect may not have reached. That is
// reported rather than skipped: silently ignoring such a file is what would
// let a future write path escape the sandbox unnoticed.
func workingTreePath(result *gen.GenerateResult, written string) (string, error) {
	dir, ok := result.WriteDirs[filepath.Dir(written)]
	if !ok {
		return "", fmt.Errorf("generated file %s is outside the preview sandbox", written)
	}
	return filepath.Join(dir, filepath.Base(written)), nil
}

// findDeletedFiles reports the *_gen.go files in the output directory that a
// real run's stale sweep would remove, using the same rule gen applies.
//
// The manifest embed file is added to the written set when it is enabled: gen
// does not emit it — the manifest stage does, after the sweep — so without
// this it reads as orphaned and `diff` claims a deletion that `generate` never
// performs.
func findDeletedFiles(result *gen.GenerateResult, cfg *config.RootConfig) ([]fileChange, error) {
	written := result.Files
	if gen.ManifestEmbedEnabled(cfg) {
		written = append(append([]string(nil), written...), gen.ManifestEmbedFilename)
	}

	stale, err := gen.StaleFiles(cfg.Output.Dir, written)
	if err != nil {
		return nil, fmt.Errorf("scanning %s for stale files: %w", cfg.Output.Dir, err)
	}

	changes := make([]fileChange, 0, len(stale))
	for _, p := range stale {
		changes = append(changes, fileChange{kind: "delete", path: p})
	}
	return changes, nil
}

// printDiffSummary writes the file-level diff listing and counts to w.
func printDiffSummary(w io.Writer, changes []fileChange) {
	var created, modified, deleted int
	for _, c := range changes {
		_, _ = fmt.Fprintf(w, "  %-8s%s\n", c.kind, c.path)
		switch c.kind {
		case "create":
			created++
		case "modify":
			modified++
		case "delete":
			deleted++
		}
	}

	var parts []string
	if created > 0 {
		parts = append(parts, fmt.Sprintf("%d created", created))
	}
	if modified > 0 {
		parts = append(parts, fmt.Sprintf("%d modified", modified))
	}
	if deleted > 0 {
		parts = append(parts, fmt.Sprintf("%d deleted", deleted))
	}
	_, _ = fmt.Fprintf(w, "\n%d files would change (%s)\n", len(changes), strings.Join(parts, ", "))
}
