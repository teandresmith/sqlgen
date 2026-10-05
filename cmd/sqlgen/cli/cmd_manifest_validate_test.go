package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// validFixture is the schema-valid single-layout manifest under testdata.
const validFixture = "testdata/manifest/valid_single.json"

// exitCodeOf extracts the process exit code an executeCommand error maps to:
// nil → 0, an *exitError → its code, anything else fails the test.
func exitCodeOf(t *testing.T, err error) int {
	t.Helper()
	if err == nil {
		return 0
	}
	var ee *exitError
	if !errors.As(err, &ee) {
		t.Fatalf("expected *exitError, got %T: %v", err, err)
	}
	return ee.code
}

// stubSchemaFetcher swaps the package-level schema fetcher for the duration of a
// test so validate never touches the network.
func stubSchemaFetcher(t *testing.T, fn schemaFetchFunc) {
	t.Helper()
	prev := manifestSchemaFetcher
	manifestSchemaFetcher = fn
	t.Cleanup(func() { manifestSchemaFetcher = prev })
}

// errFetcher always fails, forcing the embedded-schema fallback.
func errFetcher(context.Context, string) ([]byte, error) {
	return nil, errors.New("network disabled in test")
}

// writeTempJSON marshals v (or writes raw when v is a string/[]byte) to a temp
// file and returns its path.
func writeTempManifest(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write temp manifest: %v", err)
	}
	return path
}

func TestManifestValidate_Valid(t *testing.T) {
	stubSchemaFetcher(t, errFetcher) // force embedded schema; keep hermetic.

	stdout, stderr, err := executeCommand("manifest", "validate", validFixture)
	if code := exitCodeOf(t, err); code != manifestExitClean {
		t.Fatalf("exit code = %d, want 0\nstderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "manifest valid:") {
		t.Errorf("stdout missing summary: %q", stdout)
	}
	for _, want := range []string{"schema_version=0.1.0", "entities=2", "enums=1", "extras=1"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("summary missing %q\ngot: %s", want, stdout)
		}
	}
}

func TestManifestValidate_FetchUsedWhenReachable(t *testing.T) {
	var called bool
	stubSchemaFetcher(t, func(_ context.Context, url string) ([]byte, error) {
		called = true
		if !strings.Contains(url, "schema/v1.json") {
			t.Errorf("fetched unexpected url %q", url)
		}
		return manifestSchemaV1ForTest(t), nil
	})

	_, stderr, err := executeCommand("manifest", "validate", validFixture)
	if code := exitCodeOf(t, err); code != manifestExitClean {
		t.Fatalf("exit code = %d, want 0\nstderr: %s", code, stderr)
	}
	if !called {
		t.Error("expected the $schema URL to be fetched, but the fetcher was never called")
	}
}

func TestManifestValidate_MissingGenerationConfig(t *testing.T) {
	stubSchemaFetcher(t, errFetcher)

	raw, err := os.ReadFile(validFixture)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	delete(m, "generation_config")
	mutated, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal mutated: %v", err)
	}
	path := writeTempManifest(t, "invalid.json", mutated)

	_, stderr, runErr := executeCommand("manifest", "validate", path)
	if code := exitCodeOf(t, runErr); code != manifestExitFail {
		t.Fatalf("exit code = %d, want 1\nstderr: %s", code, stderr)
	}
	if !strings.Contains(stderr, "schema validation") {
		t.Errorf("stderr missing schema-validation error: %q", stderr)
	}
}

func TestManifestValidate_MissingFile(t *testing.T) {
	stubSchemaFetcher(t, errFetcher)

	path := filepath.Join(t.TempDir(), "does_not_exist.json")
	_, _, err := executeCommand("manifest", "validate", path)
	if code := exitCodeOf(t, err); code != manifestExitIOFail {
		t.Fatalf("exit code = %d, want 2", code)
	}
}

func TestManifestValidate_MalformedJSON(t *testing.T) {
	stubSchemaFetcher(t, errFetcher)

	path := writeTempManifest(t, "malformed.json", []byte("{not valid json"))
	_, stderr, err := executeCommand("manifest", "validate", path)
	if code := exitCodeOf(t, err); code != manifestExitFail {
		t.Fatalf("exit code = %d, want 1\nstderr: %s", code, stderr)
	}
	if !strings.Contains(stderr, "parse manifest") {
		t.Errorf("stderr missing parse error: %q", stderr)
	}
}

func TestManifestValidate_JunkFetchedSchemaFallsBack(t *testing.T) {
	// A reachable $schema URL returning a body that is not a compilable schema
	// degrades to the embedded schema rather than failing the gate.
	stubSchemaFetcher(t, func(context.Context, string) ([]byte, error) {
		return []byte("not a json schema at all"), nil
	})

	_, stderr, err := executeCommand("manifest", "validate", validFixture)
	if code := exitCodeOf(t, err); code != manifestExitClean {
		t.Fatalf("exit code = %d, want 0 (embedded fallback)\nstderr: %s", code, stderr)
	}
}

func TestManifestValidate_OfflineFallback(t *testing.T) {
	// $schema URL unreachable → validation still succeeds via the embedded
	// schema.
	stubSchemaFetcher(t, errFetcher)

	_, stderr, err := executeCommand("manifest", "validate", validFixture)
	if code := exitCodeOf(t, err); code != manifestExitClean {
		t.Fatalf("offline validate exit code = %d, want 0\nstderr: %s", code, stderr)
	}
}

// manifestSchemaV1ForTest reads the embedded schema through the same file the
// generator embeds, so the fetch-success test validates against the real shape.
func manifestSchemaV1ForTest(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "manifest", "schema", "v1.json"))
	if err != nil {
		t.Fatalf("read schema/v1.json: %v", err)
	}
	return data
}
