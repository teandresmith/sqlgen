package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestManifestValidate_E2EExamples validates every manifest-enabled example's
// real generated manifest artifact against the embedded schema/v1.json via the
// actual `manifest validate` command — the same jsonschema library the CLI
// ships.
//
// This is the first schema check against real orchestrated Build → EmitJSON
// output: the emitter unit tests only validate a hand-built fixture Document,
// so a builder-side regression (a nil slice serializing as null on a
// schema-required array, an unlisted enum value, a renamed/dropped field) would
// slip past them and surface only here. The validated artifact is the real
// generated file, never a hand-built Document — guarding against a fixture
// that diverges from what the generator emits (PRD §30, §30.8).
//
// The schema fetcher is forced offline so the check is hermetic and exercises
// the embedded schema (the same fallback the CLI uses when the $schema URL is
// unreachable). Reads the committed expected/ goldens, which are the byte-exact
// output of the orchestrated pipeline (produced by `make update-golden-e2e`).
func TestManifestValidate_E2EExamples(t *testing.T) {
	orig := manifestSchemaFetcher
	manifestSchemaFetcher = func(context.Context, string) ([]byte, error) {
		return nil, errors.New("offline: forcing embedded schema")
	}
	t.Cleanup(func() { manifestSchemaFetcher = orig })

	const examplesRoot = "../testdata/examples"
	examples := []struct {
		name string
		// perEntity examples also emit one full-shape entities/<table>.json per
		// entity; each must validate against the schema's bare-Entity branch.
		perEntity bool
	}{
		{name: "postgres"},
		{name: "mysql"},
		{name: "sqlite"},
		{name: "graphql", perEntity: true},
		// The tenanted-view fixture. Its view entities carry the
		// shorter features.tenancy object (no mismatch_error), which must
		// validate against the frozen v1 schema with no edit and no version
		// bump (PRD §30.4.2 / §30.5).
		{name: "tenancy_postgres", perEntity: true},
	}

	for _, ex := range examples {
		t.Run(ex.name, func(t *testing.T) {
			manifestDir := filepath.Join(examplesRoot, ex.name, "expected", "manifest")
			paths := []string{filepath.Join(manifestDir, "manifest_gen.json")}

			if ex.perEntity {
				entriesDir := filepath.Join(manifestDir, "entities")
				entries, err := os.ReadDir(entriesDir)
				if err != nil {
					t.Fatalf("reading entities dir %s: %v", entriesDir, err)
				}
				for _, e := range entries {
					if filepath.Ext(e.Name()) == ".json" {
						paths = append(paths, filepath.Join(entriesDir, e.Name()))
					}
				}
				if len(paths) == 1 {
					t.Fatalf("per_entity example %s emitted no entities/*.json", ex.name)
				}
			}

			for _, p := range paths {
				if _, err := os.Stat(p); err != nil {
					t.Fatalf("expected manifest artifact missing: %v", err)
				}
				if err := runManifestValidateCmd(p); err != nil {
					t.Errorf("manifest validate %s: %v", p, err)
				}
			}
		})
	}
}

// runManifestValidateCmd runs `sqlgen manifest validate <path>` in-process and
// returns the command error (non-nil on any non-zero exit code).
func runManifestValidateCmd(path string) error {
	root := NewRootCmd()
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs([]string{"manifest", "validate", "--quiet", path})
	if err := root.Execute(); err != nil {
		return errors.New(errOut.String())
	}
	return nil
}
