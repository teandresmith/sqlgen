package wrapper

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"gopkg.in/yaml.v3"
)

// twrCapturingGqlgen writes a stand-in gqlgen binary that copies the config
// it is handed (`generate --config <path>`) to capturePath and exits 0. It
// returns the GqlgenBin value that runs it. The wrapper removes the temp
// config once the subprocess returns, so the copy is the only way to read
// what gqlgen was actually given.
func twrCapturingGqlgen(t *testing.T, capturePath string) string {
	t.Helper()
	script := filepath.Join(t.TempDir(), "gqlgen.sh")
	body := "#!/bin/sh\n[ \"$1\" = generate ] && [ \"$2\" = --config ] || exit 2\ncp \"$3\" '" + capturePath + "'\n"
	if err := os.WriteFile(script, []byte(body), 0o600); err != nil {
		t.Fatalf("writing stand-in gqlgen: %v", err)
	}
	return "/bin/sh " + script
}

// TestGen_SkipValidationOverride pins the PRD §26.5.6 "sqlgen-owned keys"
// row for `skip_validation`: under `resolver.layout: single-file` it is
// overwritten in the merged temp config even when the consumer declares it,
// and "The override exists only in the temp config; the on-disk `gqlgen.yml`
// is never modified."
func TestGen_SkipValidationOverride(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stand-in gqlgen is a POSIX shell script")
	}
	const singleFile = "resolver:\n  layout: single-file\n  dir: graph\n  package: graph\n  filename: graph/resolver.go\n"
	const followSchema = "resolver:\n  layout: follow-schema\n  dir: graph\n  package: graph\n"
	tests := []struct {
		name     string
		consumer string
		want     bool
	}{
		{
			name:     "single-file overrides a consumer skip_validation false",
			consumer: "schema:\n  - graph/*.graphqls\nskip_validation: false\n" + singleFile,
			want:     true,
		},
		{
			name:     "single-file sets skip_validation the consumer left out",
			consumer: "schema:\n  - graph/*.graphqls\n" + singleFile,
			want:     true,
		},
		{
			name:     "follow-schema leaves a consumer skip_validation false alone",
			consumer: "schema:\n  - graph/*.graphqls\nskip_validation: false\n" + followSchema,
			want:     false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.MkdirAll(filepath.Join(dir, "graph"), 0o750); err != nil {
				t.Fatalf("mkdir graph: %v", err)
			}
			cfgPath := filepath.Join(dir, "gqlgen.yml")
			if err := os.WriteFile(cfgPath, []byte(tt.consumer), 0o600); err != nil {
				t.Fatalf("writing gqlgen.yml: %v", err)
			}
			capture := filepath.Join(t.TempDir(), "merged.yml")
			isolateTempDir(t)

			_, err := Gen(context.Background(), GenOptions{
				GqlgenConfigPath: cfgPath,
				GqlgenBin:        twrCapturingGqlgen(t, capture),
				MergeInput:       MergeInput{SchemaGlob: "graph/*.graphqls"},
				WorkingDir:       dir,
				SeedPackage:      "graph",
				Stdout:           &bytes.Buffer{},
				Stderr:           &bytes.Buffer{},
			})
			if err != nil {
				t.Fatalf("Gen: %v", err)
			}

			merged, err := os.ReadFile(capture) //nolint:gosec // path is t.TempDir-derived
			if err != nil {
				t.Fatalf("the stand-in gqlgen captured no config: %v", err)
			}
			var doc struct {
				SkipValidation *bool `yaml:"skip_validation"`
			}
			if err := yaml.Unmarshal(merged, &doc); err != nil {
				t.Fatalf("decoding merged config: %v\n%s", err, merged)
			}
			if doc.SkipValidation == nil || *doc.SkipValidation != tt.want {
				t.Errorf("merged temp config skip_validation = %v, want %v:\n%s", doc.SkipValidation, tt.want, merged)
			}

			onDisk, err := os.ReadFile(cfgPath) //nolint:gosec // path is t.TempDir-derived
			if err != nil {
				t.Fatalf("re-reading gqlgen.yml: %v", err)
			}
			if !bytes.Equal(onDisk, []byte(tt.consumer)) {
				t.Errorf("on-disk gqlgen.yml was modified:\n--- want ---\n%s\n--- got ---\n%s", tt.consumer, onDisk)
			}
		})
	}
}
