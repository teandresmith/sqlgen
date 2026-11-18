package cli

import (
	"bytes"
	"go/parser"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tlnSchema is the shared lint fixture schema. users has no soft-delete
// column and no incrementable column; products has both; events' key is
// declared in config with no UNIQUE beside it, so it has no conflict target
// and its client generates no Upsert.
const tlnSchema = tlnBaseSchema + "CREATE TABLE events (id integer NOT NULL, message text NOT NULL);\n"

// tlnBaseSchema is tlnSchema without events, for runs whose stderr is
// compared whole (events' declared key draws a config warning there).
const tlnBaseSchema = `CREATE TABLE users (id serial PRIMARY KEY, name text NOT NULL);
CREATE TABLE products (id serial PRIMARY KEY, name text NOT NULL, price integer NOT NULL, deleted_at timestamptz);
`

// tlnSchemaConfig declares events' app-enforced key for tlnSchema.
const tlnSchemaConfig = `tables:
  events:
    primary_key:
      columns: [id]
`

// tlnFile is one consumer Go file the lint run scans.
type tlnFile struct {
	dir  string
	name string
	body string
}

// tlnProject creates a project in a fresh directory and makes it the working
// directory. Every path the test names is relative to it, so the file:line in
// the output is spelled the way a user would see it. output.dir is "output"
// and already exists.
func tlnProject(t *testing.T, sql, extraYAML string, files []tlnFile) {
	t.Helper()
	root := t.TempDir()
	t.Chdir(root)

	for _, dir := range []string{"migrations", "output"} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatalf("creating %s: %v", dir, err)
		}
	}
	writeSQLFile(t, "migrations", sql)

	cfg := `input:
  dialect: postgres
  paths:
    - "migrations"
output:
  driver: pgx
  dir: output
  package: models
  layout: file_per_table
` + extraYAML
	if err := os.WriteFile("sqlgen.yml", []byte(cfg), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	for _, f := range files {
		if err := os.MkdirAll(f.dir, 0o750); err != nil {
			t.Fatalf("creating %s: %v", f.dir, err)
		}
		writeGoFile(t, f.dir, f.name, f.body)
	}
}

// tlnRun runs `sqlgen lint` with the given extra args through the real exit
// path and returns stdout, stderr and the process exit code.
func tlnRun(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	rootCmd := NewRootCmd()
	var stdout, stderr bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs(append([]string{"lint", "--config", "sqlgen.yml"}, args...))
	code := execute(rootCmd)
	return stdout.String(), stderr.String(), code
}

// tlnMutation returns a hook.ForMutation registration (the call starts on
// line 10 of the file writeGoFile produces). A non-empty op adds an
// `m.Op == hook.<op>` filter.
func tlnMutation(input, result, table, op string) string {
	return tlnHook("ForMutation", "m", "MutationContext", input, result, table, op)
}

// tlnQuery is tlnMutation for hook.ForQuery.
func tlnQuery(input, result, table, op string) string {
	return tlnHook("ForQuery", "q", "QueryContext", input, result, table, op)
}

func tlnHook(fn, param, ctxType, input, result, table, op string) string {
	filter := ""
	if op != "" {
		filter = "\tif " + param + ".Op == hook." + op + " {\n\t}\n"
	}
	return `
var _ = hook.` + fn + `[` + input + `, ` + result + `](` + table + `, func(
	ctx context.Context,
	` + param + ` *hook.` + ctxType + `,
	input ` + input + `,
	next func(ctx context.Context) (` + result + `, error),
) (` + result + `, error) {
` + filter + `	return next(ctx)
})
`
}

// tlnLoc spells a scanned file's location the way lint prints it.
func tlnLoc(dir, name string) string {
	return filepath.Join(dir, name) + ":10"
}

// TestLint_OutputFormat compares lint's whole stdout for a run with one error
// and one warning, and for runs with a single finding.
func TestLint_OutputFormat(t *testing.T) {
	tests := []struct {
		name     string
		files    []tlnFile
		want     string
		wantCode int
	}{
		{
			name: "one error and one warning print severity, file:line, the rebuilt call, the explanation and the summary",
			files: []tlnFile{
				{dir: "hooks", name: "products.go", body: tlnMutation("*CreateProductInput", "*User", "TableUsers", "")},
				{dir: "hooks", name: "audit.go", body: tlnMutation("*CreateUserInput", "*User", "TableUsers", "OpSoftDelete")},
			},
			want: "  error  " + tlnLoc("hooks", "products.go") + "  ForMutation[*CreateProductInput, *User](TableUsers, ...)\n" +
				"         type mismatch: CreateProductInput belongs to TableProducts, not TableUsers\n" +
				"\n" +
				"  warn   " + tlnLoc("hooks", "audit.go") + "  ForMutation[*CreateUserInput, *User](TableUsers, ...)\n" +
				"         table \"users\" has no soft delete column — hook will never fire for OpSoftDelete\n" +
				"\n" +
				"2 issues (1 error, 1 warning)\n",
			wantCode: 1,
		},
		{
			name: "a single soft-delete warning names OpSoftDelete and is counted as 1 issues",
			files: []tlnFile{
				{dir: "hooks", name: "users.go", body: tlnMutation("*CreateUserInput", "*User", "TableUsers", "OpSoftDelete")},
			},
			want: "  warn   " + tlnLoc("hooks", "users.go") + "  ForMutation[*CreateUserInput, *User](TableUsers, ...)\n" +
				"         table \"users\" has no soft delete column — hook will never fire for OpSoftDelete\n" +
				"\n" +
				"1 issues (1 warning)\n",
		},
		{
			name: "a single increment warning names OpIncrement",
			files: []tlnFile{
				{dir: "hooks", name: "users.go", body: tlnMutation("*CreateUserInput", "*User", "TableUsers", "OpIncrement")},
			},
			want: "  warn   " + tlnLoc("hooks", "users.go") + "  ForMutation[*CreateUserInput, *User](TableUsers, ...)\n" +
				"         table \"users\" has no incrementable columns — hook will never fire for OpIncrement\n" +
				"\n" +
				"1 issues (1 warning)\n",
		},
		{
			name: "a single upsert warning names OpUpsert",
			files: []tlnFile{
				{dir: "hooks", name: "events.go", body: tlnMutation("*CreateEventInput", "*Event", "TableEvents", "OpUpsert")},
			},
			want: "  warn   " + tlnLoc("hooks", "events.go") + "  ForMutation[*CreateEventInput, *Event](TableEvents, ...)\n" +
				"         table \"events\" has no conflict target — hook will never fire for OpUpsert\n" +
				"\n" +
				"1 issues (1 warning)\n",
		},
		{
			name: "a ForQuery call whose types belong to another table is found and reported",
			files: []tlnFile{
				{dir: "hooks", name: "queries.go", body: tlnQuery("*GetProductsInput", "*User", "TableUsers", "")},
			},
			want: "  error  " + tlnLoc("hooks", "queries.go") + "  ForQuery[*GetProductsInput, *User](TableUsers, ...)\n" +
				"         type mismatch: GetProductsInput belongs to TableProducts, not TableUsers\n" +
				"\n" +
				"1 issues (1 error)\n",
			wantCode: 1,
		},
		{
			name: "the finding rebuilds the call from its extracted type arguments and table constant",
			files: []tlnFile{
				{dir: "hooks", name: "qualified.go", body: tlnMutation("*models.UpdateProductInput", "*models.User", "models.TableUsers", "")},
			},
			want: "  error  " + tlnLoc("hooks", "qualified.go") + "  ForMutation[*UpdateProductInput, *User](TableUsers, ...)\n" +
				"         type mismatch: UpdateProductInput belongs to TableProducts, not TableUsers\n" +
				"\n" +
				"1 issues (1 error)\n",
			wantCode: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tlnProject(t, tlnSchema, tlnSchemaConfig, tt.files)
			stdout, stderr, code := tlnRun(t, "--paths", "hooks")
			if stdout != tt.want {
				t.Errorf("sqlgen lint stdout =\n%s\nwant\n%s\nstderr: %s", stdout, tt.want, stderr)
			}
			if code != tt.wantCode {
				t.Errorf("sqlgen lint exit code = %d, want %d; stderr: %s", code, tt.wantCode, stderr)
			}
		})
	}
}

// TestLint_IncrementEligibility runs an OpIncrement hook through lint on
// tables that differ only in what makes a column incrementable.
func TestLint_IncrementEligibility(t *testing.T) {
	warn := func(table string) string {
		return "  warn   " + tlnLoc("hooks", "counters.go") + "  ForMutation[*CreateCounterInput, *Counter](TableCounters, ...)\n" +
			"         table \"" + table + "\" has no incrementable columns — hook will never fire for OpIncrement\n" +
			"\n" +
			"1 issues (1 warning)\n"
	}
	tests := []struct {
		name string
		sql  string
		want string
	}{
		{
			name: "a table with no numeric column draws the OpIncrement warning",
			sql:  "CREATE TABLE counters (code text PRIMARY KEY, label text NOT NULL);",
			want: warn("counters"),
		},
		{
			name: "a plain numeric column makes the table incrementable",
			sql:  "CREATE TABLE counters (code text PRIMARY KEY, hits integer NOT NULL);",
			want: "",
		},
		{
			name: "a table whose only numeric column is its primary key draws the OpIncrement warning",
			sql:  "CREATE TABLE counters (id serial PRIMARY KEY, label text NOT NULL);",
			want: warn("counters"),
		},
		{
			name: "a table whose only numeric column is generated draws the OpIncrement warning",
			sql:  "CREATE TABLE counters (code text PRIMARY KEY, label text NOT NULL, label_len integer GENERATED ALWAYS AS (length(label)) STORED);",
			want: warn("counters"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tlnProject(t, tt.sql, "", []tlnFile{
				{dir: "hooks", name: "counters.go", body: tlnMutation("*CreateCounterInput", "*Counter", "TableCounters", "OpIncrement")},
			})
			// --fail-on info turns any finding into exit 1, so a clean run
			// is exit 0 with empty stdout.
			stdout, stderr, code := tlnRun(t, "--paths", "hooks", "--fail-on", "info")
			if stdout != tt.want {
				t.Errorf("sqlgen lint stdout =\n%s\nwant\n%s\nstderr: %s", stdout, tt.want, stderr)
			}
			wantCode := 0
			if tt.want != "" {
				wantCode = 1
			}
			if code != wantCode {
				t.Errorf("sqlgen lint exit code = %d, want %d; stderr: %s", code, wantCode, stderr)
			}
		})
	}
}

// TestLint_TablesThatGenerateNothing pins that a hook naming a table with no
// generated client draws no finding, using a hook that is flagged as a type
// mismatch when the table is generated.
func TestLint_TablesThatGenerateNothing(t *testing.T) {
	const users = "CREATE TABLE users (id serial PRIMARY KEY, name text NOT NULL);\n"
	hookFile := []tlnFile{
		{dir: "hooks", name: "products.go", body: tlnMutation("*CreateUserInput", "*User", "TableProducts", "")},
	}
	tests := []struct {
		name      string
		sql       string
		extraYAML string
		want      string
		wantCode  int
	}{
		{
			name: "the hook is flagged while the table is generated",
			sql:  users + "CREATE TABLE products (id serial PRIMARY KEY, name text NOT NULL);",
			want: "  error  " + tlnLoc("hooks", "products.go") + "  ForMutation[*CreateUserInput, *User](TableProducts, ...)\n" +
				"         type mismatch: CreateUserInput belongs to TableUsers, not TableProducts\n" +
				"\n" +
				"  error  " + tlnLoc("hooks", "products.go") + "  ForMutation[*CreateUserInput, *User](TableProducts, ...)\n" +
				"         type mismatch: User belongs to TableUsers, not TableProducts\n" +
				"\n" +
				"2 issues (2 errors)\n",
			wantCode: 1,
		},
		{
			name:      "a table removed by exclude_tables produces no findings",
			sql:       users + "CREATE TABLE products (id serial PRIMARY KEY, name text NOT NULL);",
			extraYAML: "exclude_tables:\n  - products\n",
		},
		{
			name: "a table with no resolvable primary key produces no findings",
			sql:  users + "CREATE TABLE products (name text NOT NULL);",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tlnProject(t, tt.sql, tt.extraYAML, hookFile)
			stdout, stderr, code := tlnRun(t, "--paths", "hooks", "--fail-on", "info")
			if stdout != tt.want {
				t.Errorf("sqlgen lint stdout =\n%s\nwant\n%s\nstderr: %s", stdout, tt.want, stderr)
			}
			if code != tt.wantCode {
				t.Errorf("sqlgen lint exit code = %d, want %d; stderr: %s", code, tt.wantCode, stderr)
			}
		})
	}
}

// TestLint_ScanDirectories pins which directories lint scans with and without
// --paths.
func TestLint_ScanDirectories(t *testing.T) {
	mismatch := func(dir, name string) string {
		return "  error  " + tlnLoc(dir, name) + "  ForMutation[*CreateProductInput, *User](TableUsers, ...)\n" +
			"         type mismatch: CreateProductInput belongs to TableProducts, not TableUsers\n" +
			"\n"
	}
	softDelete := func(dir, name string) string {
		return "  warn   " + tlnLoc(dir, name) + "  ForMutation[*CreateUserInput, *User](TableUsers, ...)\n" +
			"         table \"users\" has no soft delete column — hook will never fire for OpSoftDelete\n" +
			"\n"
	}
	tests := []struct {
		name     string
		files    []tlnFile
		args     []string
		want     string
		wantCode int
	}{
		{
			name: "with no --paths lint scans output.dir",
			files: []tlnFile{
				{dir: "output", name: "hooks.go", body: tlnMutation("*CreateProductInput", "*User", "TableUsers", "")},
			},
			want:     mismatch("output", "hooks.go") + "1 issues (1 error)\n",
			wantCode: 1,
		},
		{
			name: "a comma-separated --paths list scans every directory it names",
			files: []tlnFile{
				{dir: "hooks", name: "products.go", body: tlnMutation("*CreateProductInput", "*User", "TableUsers", "")},
				{dir: "server", name: "users.go", body: tlnMutation("*CreateUserInput", "*User", "TableUsers", "OpSoftDelete")},
			},
			args:     []string{"--paths", "hooks,server"},
			want:     mismatch("hooks", "products.go") + softDelete("server", "users.go") + "2 issues (1 error, 1 warning)\n",
			wantCode: 1,
		},
		{
			name: "--paths replaces output.dir rather than adding to it",
			files: []tlnFile{
				{dir: "output", name: "hooks.go", body: tlnMutation("*CreateProductInput", "*User", "TableUsers", "")},
				{dir: "server", name: "users.go", body: tlnMutation("*CreateUserInput", "*User", "TableUsers", "OpSoftDelete")},
			},
			args: []string{"--paths", "server"},
			want: softDelete("server", "users.go") + "1 issues (1 warning)\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tlnProject(t, tlnSchema, tlnSchemaConfig, tt.files)
			stdout, stderr, code := tlnRun(t, tt.args...)
			if stdout != tt.want {
				t.Errorf("sqlgen lint %s stdout =\n%s\nwant\n%s\nstderr: %s", strings.Join(tt.args, " "), stdout, tt.want, stderr)
			}
			if code != tt.wantCode {
				t.Errorf("sqlgen lint %s exit code = %d, want %d; stderr: %s", strings.Join(tt.args, " "), code, tt.wantCode, stderr)
			}
		})
	}
}

// TestLint_FailOnThreshold pins the exit code for each --fail-on threshold
// against an error-only and a warning-only run. Lint has no info-severity
// rule, so the info threshold is exercised with a warning, which it counts.
func TestLint_FailOnThreshold(t *testing.T) {
	errorHook := []tlnFile{
		{dir: "hooks", name: "products.go", body: tlnMutation("*CreateProductInput", "*User", "TableUsers", "")},
	}
	warningHook := []tlnFile{
		{dir: "hooks", name: "users.go", body: tlnMutation("*CreateUserInput", "*User", "TableUsers", "OpSoftDelete")},
	}
	tests := []struct {
		name       string
		files      []tlnFile
		args       []string
		wantCode   int
		wantStderr string
	}{
		{
			name:       "the default threshold is error, so an error exits 1",
			files:      errorHook,
			wantCode:   1,
			wantStderr: "Error: lint found issues at or above \"error\" severity\n",
		},
		{
			name:  "under the default threshold a warning alone exits 0",
			files: warningHook,
		},
		{
			name:       "--fail-on warning exits 1 when a warning exists",
			files:      warningHook,
			args:       []string{"--fail-on", "warning"},
			wantCode:   1,
			wantStderr: "Error: lint found issues at or above \"warning\" severity\n",
		},
		{
			name:       "--fail-on warning exits 1 on an error, which is above it",
			files:      errorHook,
			args:       []string{"--fail-on", "warning"},
			wantCode:   1,
			wantStderr: "Error: lint found issues at or above \"warning\" severity\n",
		},
		{
			name:       "--fail-on info exits 1 on a warning, which is above it",
			files:      warningHook,
			args:       []string{"--fail-on", "info"},
			wantCode:   1,
			wantStderr: "Error: lint found issues at or above \"info\" severity\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tlnProject(t, tlnBaseSchema, "", tt.files)
			stdout, stderr, code := tlnRun(t, append([]string{"--paths", "hooks"}, tt.args...)...)
			if code != tt.wantCode {
				t.Errorf("sqlgen lint %s exit code = %d, want %d; stderr: %s", strings.Join(tt.args, " "), code, tt.wantCode, stderr)
			}
			if stderr != tt.wantStderr {
				t.Errorf("sqlgen lint %s stderr = %q, want %q", strings.Join(tt.args, " "), stderr, tt.wantStderr)
			}
			// The findings print whatever the threshold.
			if !strings.HasSuffix(stdout, "1 issues (1 error)\n") && !strings.HasSuffix(stdout, "1 issues (1 warning)\n") {
				t.Errorf("sqlgen lint %s stdout = %q, want the finding and its summary", strings.Join(tt.args, " "), stdout)
			}
		})
	}
}

// TestLint_SnippetKeepsSliceTypeArgument pins that the rebuilt call shows a
// type argument as written. A ForQuery registration for a list operation takes
// `[]*User` as its result, and the finding must not print it as `*[]User`.
func TestLint_SnippetKeepsSliceTypeArgument(t *testing.T) {
	tlnProject(t, tlnBaseSchema, "", []tlnFile{
		{dir: "hooks", name: "queries.go", body: tlnQuery("*GetProductsInput", "[]*User", "TableUsers", "")},
	})
	want := "  error  " + tlnLoc("hooks", "queries.go") + "  ForQuery[*GetProductsInput, []*User](TableUsers, ...)\n" +
		"         type mismatch: GetProductsInput belongs to TableProducts, not TableUsers\n" +
		"\n" +
		"1 issues (1 error)\n"
	stdout, stderr, code := tlnRun(t, "--paths", "hooks")
	if stdout != want {
		t.Errorf("sqlgen lint stdout =\n%s\nwant\n%s\nstderr: %s", stdout, want, stderr)
	}
	if code != 1 {
		t.Errorf("sqlgen lint exit code = %d, want 1; stderr: %s", code, stderr)
	}
}

// TestLint_TypeArgumentSpelling pins how a finding spells each type argument
// of the rebuilt call: as written, pointers, slices, maps and generic
// arguments included, with every package qualifier dropped.
func TestLint_TypeArgumentSpelling(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{name: "pointer", src: "*models.User", want: "*User"},
		{name: "slice of pointers", src: "[]*models.User", want: "[]*User"},
		{name: "value", src: "models.User", want: "User"},
		{name: "array", src: "[2]models.User", want: "[2]User"},
		{name: "map", src: "map[string]*models.User", want: "map[string]*User"},
		{name: "generic with one argument", src: "*models.Box[models.User]", want: "*Box[User]"},
		{name: "generic with two arguments", src: "models.Pair[models.User, *models.Post]", want: "Pair[User, *Post]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, err := parser.ParseExpr(tt.src)
			if err != nil {
				t.Fatalf("parsing %q: %v", tt.src, err)
			}
			if got := typeArgSpelling(expr); got != tt.want {
				t.Errorf("typeArgSpelling(%s) = %q, want %q", tt.src, got, tt.want)
			}
		})
	}
}
