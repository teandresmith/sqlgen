package wrapper

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestGen_RejectsMalformedMergedConfigBeforeGqlgen pins PRD §26.3: "The
// wrapper additionally validates that the merged gqlgen config is
// well-formed before invoking the gqlgen subprocess." Each consumer file
// below cannot yield a well-formed merged config. The stub gqlgen is put in
// its failing mode, so had Gen invoked it the error would be a
// *SubprocessError; a pre-subprocess rejection is any other error.
func TestGen_RejectsMalformedMergedConfigBeforeGqlgen(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper invocation pattern is POSIX-only")
	}

	tests := []struct {
		name     string
		consumer string
		merge    MergeInput
	}{
		{
			name:     "unparseable yaml",
			consumer: "schema:\n\t- bad\n",
			merge:    MergeInput{SchemaGlob: "graph/*.graphqls"},
		},
		{
			name:     "top level is a sequence",
			consumer: "- graph/*.graphqls\n",
			merge:    MergeInput{SchemaGlob: "graph/*.graphqls"},
		},
		{
			name:     "schema is a mapping",
			consumer: "schema:\n  glob: graph/*.graphqls\n",
			merge:    MergeInput{SchemaGlob: "graph/*.graphqls"},
		},
		{
			name:     "models is a sequence",
			consumer: "schema:\n  - graph/*.graphqls\nmodels:\n  - UUID\n",
			merge:    MergeInput{Models: map[string][]string{"UUID": {"example.com/app/graph.UUID"}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			cfgPath := filepath.Join(dir, "gqlgen.yml")
			if err := os.WriteFile(cfgPath, []byte(tt.consumer), 0o600); err != nil {
				t.Fatalf("seeding gqlgen.yml: %v", err)
			}
			tempDir := isolateTempDir(t)
			t.Setenv("SQLGEN_WRAPPER_TEST_HELPER", "fail")

			_, err := Gen(context.Background(), GenOptions{
				GqlgenConfigPath: cfgPath,
				GqlgenBin:        os.Args[0],
				MergeInput:       tt.merge,
				WorkingDir:       dir,
				Stdout:           &bytes.Buffer{},
				Stderr:           &bytes.Buffer{},
			})
			if err == nil {
				t.Fatalf("Gen(%q) = nil error, want a rejection of the merged config", tt.consumer)
			}
			if _, ok := errors.AsType[*SubprocessError](err); ok {
				t.Errorf("Gen(%q) invoked gqlgen before rejecting the config: %v", tt.consumer, err)
			}
			assertNoLeakedTempFiles(t, tempDir)
		})
	}
}
