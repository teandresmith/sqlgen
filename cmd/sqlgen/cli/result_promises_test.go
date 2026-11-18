package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/teandresmith/sqlgen/manifest"
)

// srpRun runs sqlgen through execute, the path main takes, and returns the
// exit code with both output streams.
func srpRun(args ...string) (code int, stdout, stderr string) {
	rootCmd := NewRootCmd()
	var out, errOut bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&errOut)
	rootCmd.SetArgs(args)
	code = execute(rootCmd)
	return code, out.String(), errOut.String()
}

// TestDiff_ChangesFoundIsAResult pins two diff promises for a run that finds
// one modified file: a category with no files is left out of the summary and
// the count reads "1 files", and exiting 1 for changes is a result, so the
// listing goes to stdout and nothing to stderr.
func TestDiff_ChangesFoundIsAResult(t *testing.T) {
	cfgPath := setupDiffProject(t, diffProjectOptions{noGraphQL: true})
	generateForDiff(t, cfgPath)
	writeFileOrFail(t, filepath.Join("internal", "database", "user_gen.go"), "// modified\n")

	code, stdout, stderr := srpRun("diff", "--config", cfgPath)

	t.Run("summary leaves out empty categories", func(t *testing.T) {
		want := "  modify  internal/database/user_gen.go\n\n1 files would change (1 modified)\n"
		if stdout != want {
			t.Errorf("stdout =\n%q\nwant\n%q", stdout, want)
		}
	})
	t.Run("exit 1 with nothing on stderr", func(t *testing.T) {
		if code != ExitGeneration {
			t.Errorf("exit code = %d, want %d", code, ExitGeneration)
		}
		if stderr != "" {
			t.Errorf("stderr = %q, want empty", stderr)
		}
	})
}

// TestManifestValidate_ReadsNoConfig pins that `manifest validate` is always
// available: run where no sqlgen.yml exists, it validates the file it is
// given.
func TestManifestValidate_ReadsNoConfig(t *testing.T) {
	stubSchemaFetcher(t, errFetcher)
	manifestPath, err := filepath.Abs(validFixture)
	if err != nil {
		t.Fatalf("resolving %s: %v", validFixture, err)
	}
	dir := t.TempDir()
	t.Chdir(dir)
	for _, name := range []string{"sqlgen.yml", "sqlgen.yaml"} {
		if _, statErr := os.Stat(name); statErr == nil {
			t.Fatalf("%s exists in the empty working directory", name)
		}
	}

	code, _, stderr := srpRun("manifest", "validate", manifestPath)
	if code != ExitSuccess {
		t.Errorf("exit code = %d, want 0; stderr = %q", code, stderr)
	}
}

// TestManifestDiff_ChangesFoundIsAResult pins that `manifest diff` exiting 1
// because the manifests differ is a result: the diff goes to stdout and
// nothing to stderr.
func TestManifestDiff_ChangesFoundIsAResult(t *testing.T) {
	oldM := baseManifest()
	newM := cloneManifest(t, oldM)
	newM.Entities[0].Columns = append(newM.Entities[0].Columns,
		manifest.Column{Name: "phone", GoField: "Phone", GoType: "string", DBType: "text"})
	oldPath := writeManifest(t, t.TempDir(), oldM)
	newPath := writeManifest(t, t.TempDir(), newM)

	code, stdout, stderr := srpRun("manifest", "diff", oldPath, newPath)
	if code != manifestExitFail {
		t.Errorf("exit code = %d, want %d", code, manifestExitFail)
	}
	if stdout == "" {
		t.Error("stdout is empty, want the diff")
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
}
