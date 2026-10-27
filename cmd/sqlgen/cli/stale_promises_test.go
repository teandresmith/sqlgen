package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
)

// tstSqlgenOrphan is the body of a _gen.go file sqlgen itself would have
// written: its own header above the package clause. Every orphan the stale
// tests plant carries it, so the header rule alone can never be what keeps a
// file alive.
func tstSqlgenOrphan() string {
	return gen.Header(Version) + "\n\npackage models\n"
}

// tstStaleProject writes a one-table project with output.dir at
// <root>/output under the given layout, runs a first generate, and returns the
// project root, the config path and the output dir.
func tstStaleProject(t *testing.T, layout string) (root, cfgPath, outputDir string) {
	t.Helper()
	root = t.TempDir()
	cfgPath = filepath.Join(root, "sqlgen.yml")
	sqlDir := filepath.Join(root, "migrations")
	if err := os.MkdirAll(sqlDir, 0o750); err != nil {
		t.Fatalf("creating migrations dir: %v", err)
	}
	outputDir = filepath.Join(root, "output")
	writeTestConfigWithLayout(t, cfgPath, "postgres", sqlDir, outputDir, layout)
	writeSQLFile(t, sqlDir, `CREATE TABLE users (id serial PRIMARY KEY, name text NOT NULL);`)
	if code, stderr := useRun("generate", "--config", cfgPath); code != ExitSuccess {
		t.Fatalf("first generate exit code = %d, want %d; stderr:\n%s", code, ExitSuccess, stderr)
	}
	return root, cfgPath, outputDir
}

func tstExists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Lstat(path)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	t.Fatalf("stat %s: %v", path, err)
	return false
}

// TestStaleSweep_RunsOnlyUnderFilePerTable pins that the sweep is a
// file_per_table behavior: the same sqlgen-headed orphan survives a
// single_file generate and is deleted by a file_per_table one.
func TestStaleSweep_RunsOnlyUnderFilePerTable(t *testing.T) {
	tests := []struct {
		name     string
		layout   string
		wantKept bool
	}{
		{name: "single_file keeps a sqlgen orphan", layout: "single_file", wantKept: true},
		{name: "file_per_table deletes the same orphan", layout: "file_per_table", wantKept: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, cfgPath, outputDir := tstStaleProject(t, tt.layout)
			orphan := filepath.Join(outputDir, "old_table_gen.go")
			writeFileOrFail(t, orphan, tstSqlgenOrphan())

			if code, stderr := useRun("generate", "--config", cfgPath); code != ExitSuccess {
				t.Fatalf("generate exit code = %d, want %d; stderr:\n%s", code, ExitSuccess, stderr)
			}
			if got := tstExists(t, orphan); got != tt.wantKept {
				t.Errorf("layout %s: orphan %s exists after generate = %v, want %v", tt.layout, orphan, got, tt.wantKept)
			}
		})
	}
}

// TestStaleSweep_OnlyConsidersTheOutputDir pins that only files within
// output.dir are swept: sqlgen-headed orphans in a sibling directory and in
// the parent of output.dir survive, while one inside output.dir is deleted by
// the same run.
func TestStaleSweep_OnlyConsidersTheOutputDir(t *testing.T) {
	root, cfgPath, outputDir := tstStaleProject(t, "file_per_table")
	sibling := filepath.Join(root, "sibling")
	if err := os.MkdirAll(sibling, 0o750); err != nil {
		t.Fatalf("creating sibling dir: %v", err)
	}
	tests := []struct {
		name     string
		path     string
		wantKept bool
	}{
		{name: "orphan in a sibling directory survives", path: filepath.Join(sibling, "old_table_gen.go"), wantKept: true},
		{name: "orphan in the parent of output.dir survives", path: filepath.Join(root, "old_table_gen.go"), wantKept: true},
		{name: "orphan inside output.dir is deleted", path: filepath.Join(outputDir, "old_table_gen.go"), wantKept: false},
	}
	for _, tt := range tests {
		writeFileOrFail(t, tt.path, tstSqlgenOrphan())
	}

	if code, stderr := useRun("generate", "--config", cfgPath); code != ExitSuccess {
		t.Fatalf("generate exit code = %d, want %d; stderr:\n%s", code, ExitSuccess, stderr)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tstExists(t, tt.path); got != tt.wantKept {
				t.Errorf("%s exists after generate = %v, want %v", tt.path, got, tt.wantKept)
			}
		})
	}
}

// TestStaleSweep_HeaderCountsOnlyAbovePackageClause pins that sqlgen's header
// marks a file as sqlgen's only when it sits above the package clause.
func TestStaleSweep_HeaderCountsOnlyAbovePackageClause(t *testing.T) {
	_, cfgPath, outputDir := tstStaleProject(t, "file_per_table")
	tests := []struct {
		name     string
		file     string
		content  string
		wantKept bool
	}{
		{
			name:     "header below the package clause survives",
			file:     "header_below_gen.go",
			content:  "package models\n\n" + gen.Header(Version) + "\n",
			wantKept: true,
		},
		{
			name:     "header above the package clause is deleted",
			file:     "header_above_gen.go",
			content:  tstSqlgenOrphan(),
			wantKept: false,
		},
	}
	for _, tt := range tests {
		writeFileOrFail(t, filepath.Join(outputDir, tt.file), tt.content)
	}

	if code, stderr := useRun("generate", "--config", cfgPath); code != ExitSuccess {
		t.Fatalf("generate exit code = %d, want %d; stderr:\n%s", code, ExitSuccess, stderr)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(outputDir, tt.file)
			if got := tstExists(t, path); got != tt.wantKept {
				t.Errorf("%s exists after generate = %v, want %v", path, got, tt.wantKept)
			}
		})
	}
}

// TestStaleSweep_UnreadableCandidateIsLeftAlone pins that the sweep fails
// closed: a sqlgen-headed orphan that cannot be read is neither deleted nor a
// reason for generate to fail.
func TestStaleSweep_UnreadableCandidateIsLeftAlone(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file mode 000 does not make a file unreadable on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root can read a mode-000 file")
	}
	_, cfgPath, outputDir := tstStaleProject(t, "file_per_table")
	orphan := filepath.Join(outputDir, "old_table_gen.go")
	writeFileOrFail(t, orphan, tstSqlgenOrphan())
	if err := os.Chmod(orphan, 0o000); err != nil {
		t.Fatalf("chmod %s: %v", orphan, err)
	}
	t.Cleanup(func() { _ = os.Chmod(orphan, 0o600) })
	if _, err := os.ReadFile(orphan); err == nil { //nolint:gosec // test-local temp file
		t.Fatalf("%s is still readable after chmod 000; the fixture does not exercise an unreadable file", orphan)
	}

	code, stderr := useRun("generate", "--config", cfgPath)
	if code != ExitSuccess {
		t.Errorf("generate exit code = %d, want %d; stderr:\n%s", code, ExitSuccess, stderr)
	}
	if !tstExists(t, orphan) {
		t.Errorf("unreadable orphan %s was deleted; the sweep must leave a file it cannot read alone", orphan)
	}
}

// tstPostOrphanError is the refusal generate prints when post_gen.graphqls is
// left in schema_dir with no gqlgen resolver file beside it.
const tstPostOrphanError = "Generation error: api: internal/graph holds schema files for entities no longer on the API " +
	"(post_gen.graphqls), which gqlgen would load and fail on; delete them (PRD §23.8)"

// TestGenerate_GraphQLSchemaOrphanRefused pins that every way an entity
// leaves the API — in either layout — leaves a schema orphan that the next
// generate reports and refuses on with exit 1, keeping the file.
func TestGenerate_GraphQLSchemaOrphanRefused(t *testing.T) {
	tests := []struct {
		name   string
		layout string
		// change is applied to the project after the first generate; it
		// takes the posts table off the API one way or another.
		change func(t *testing.T, root, cfgPath string)
	}{
		{
			name:   "dropped table under single_file",
			layout: "single_file",
			change: tstDropPosts,
		},
		{
			name:   "dropped table under file_per_table",
			layout: "file_per_table",
			change: tstDropPosts,
		},
		{
			name:   "table taken off the API with api.enabled false",
			layout: "file_per_table",
			change: func(t *testing.T, _, cfgPath string) {
				t.Helper()
				appendConfig(t, cfgPath, "tables:\n  posts:\n    api:\n      enabled: false\n")
			},
		},
		{
			name:   "table given a new file stem with struct_name",
			layout: "file_per_table",
			change: func(t *testing.T, _, cfgPath string) {
				t.Helper()
				appendConfig(t, cfgPath, "tables:\n  posts:\n    struct_name: Article\n")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgPath := setupDiffProject(t, diffProjectOptions{layout: tt.layout})
			generateForDiff(t, cfgPath)
			root := filepath.Dir(cfgPath)
			orphan := filepath.Join(root, "internal", "graph", "post_gen.graphqls")
			if !tstExists(t, orphan) {
				t.Fatalf("first generate did not write %s; the fixture has no schema file to orphan", orphan)
			}

			tt.change(t, root, cfgPath)
			code, stderr := useRun("generate", "--no-graphql", "--config", cfgPath)

			if code != ExitGeneration {
				t.Errorf("generate exit code = %d, want %d; stderr:\n%s", code, ExitGeneration, stderr)
			}
			if !strings.Contains(stderr, tstPostOrphanError) {
				t.Errorf("generate stderr = %q, want it to contain %q", stderr, tstPostOrphanError)
			}
			if !tstExists(t, orphan) {
				t.Errorf("generate removed %s; it must only report it", orphan)
			}
		})
	}
}

func tstDropPosts(t *testing.T, root, _ string) {
	t.Helper()
	writeFileOrFail(t, filepath.Join(root, "migrations", "001_init.sql"),
		"CREATE TABLE users (id serial PRIMARY KEY, name text NOT NULL);")
}
