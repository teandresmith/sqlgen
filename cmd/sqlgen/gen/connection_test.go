package gen_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/sql"
)

func loadConnectionTemplate(t *testing.T) *template.Template {
	t.Helper()
	tmplPath := filepath.Join("templates", "connection.go.tmpl")
	tmpl, err := template.New("connection.go.tmpl").
		Funcs(gen.FuncMap(sql.NewPostgresDialect())).
		ParseFiles(tmplPath)
	if err != nil {
		t.Fatalf("parsing connection template: %v", err)
	}
	return tmpl
}

func executeConnectionTemplate(t *testing.T, ctx gen.ConnectionContext) string {
	t.Helper()
	tmpl := loadConnectionTemplate(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "connection", ctx); err != nil {
		t.Fatalf("executing template: %v", err)
	}
	return string(gen.WrapWithPreamble(ctx.Package, ctx.Imports, []byte(buf.String())))
}

func testConnectionContext() gen.ConnectionContext {
	return gen.BuildConnectionContext("db")
}

// --- Test: ConnectionInput generic type ---

func TestConnectionTemplate_connectionInputType(t *testing.T) {
	ctx := testConnectionContext()
	output := executeConnectionTemplate(t, ctx)

	wantPatterns := []string{
		"type ConnectionInput[F any] struct",
		`Filter *F`,
		`First  *int`,
		`Last   *int`,
		`After  *string`,
		`Before *string`,
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// --- Test: Connection/Edge/PageInfo types ---

func TestConnectionTemplate_connectionTypes(t *testing.T) {
	ctx := testConnectionContext()
	output := executeConnectionTemplate(t, ctx)

	// TotalCount lives on Connection (top-level), not PageInfo.
	wantPatterns := []string{
		"type Connection[T any] struct",
		"Edges      []Edge[T]",
		"PageInfo   PageInfo",
		"TotalCount int64",
		"type Edge[T any] struct",
		"Node   *T",
		"Cursor string",
		"type PageInfo struct",
		"HasNextPage     bool",
		"HasPreviousPage bool",
		"StartCursor     *string",
		"EndCursor       *string",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// --- Test: PageInfo.StartCursor and EndCursor are *string ---

func TestConnectionTemplate_pageInfoPointerTypes(t *testing.T) {
	ctx := testConnectionContext()
	output := executeConnectionTemplate(t, ctx)

	if !strings.Contains(output, "StartCursor     *string") {
		t.Errorf("StartCursor should be *string\n\nfull output:\n%s", output)
	}
	if !strings.Contains(output, "EndCursor       *string") {
		t.Errorf("EndCursor should be *string\n\nfull output:\n%s", output)
	}
}

// --- Test: Connection.TotalCount is int64 (lives on Connection, not PageInfo) ---

func TestConnectionTemplate_totalCountAlwaysPopulated(t *testing.T) {
	ctx := testConnectionContext()
	output := executeConnectionTemplate(t, ctx)

	if !strings.Contains(output, "TotalCount int64") {
		t.Errorf("TotalCount should be int64 on Connection\n\nfull output:\n%s", output)
	}

	// Regression guard: TotalCount must NOT live on PageInfo.
	pageInfoStart := strings.Index(output, "type PageInfo struct")
	if pageInfoStart < 0 {
		t.Fatalf("missing PageInfo struct\n\nfull output:\n%s", output)
	}
	pageInfoEnd := strings.Index(output[pageInfoStart:], "}")
	if pageInfoEnd < 0 {
		t.Fatalf("malformed PageInfo struct\n\nfull output:\n%s", output)
	}
	pageInfoBlock := output[pageInfoStart : pageInfoStart+pageInfoEnd]
	if strings.Contains(pageInfoBlock, "TotalCount") {
		t.Errorf("PageInfo must not contain TotalCount\n\nPageInfo block:\n%s", pageInfoBlock)
	}
}

// --- Test: Cursor encode/decode ---

func TestConnectionTemplate_cursorEncodeDecode(t *testing.T) {
	ctx := testConnectionContext()
	output := executeConnectionTemplate(t, ctx)

	wantPatterns := []string{
		"func decodeCursor(s string) (map[string]any, error)",
		"base64.StdEncoding.DecodeString(s)",
		"json.Unmarshal(data, &cursor)",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing cursor decode: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// --- Test: cursorKeyset function ---

func TestConnectionTemplate_cursorKeyset(t *testing.T) {
	ctx := testConnectionContext()
	output := executeConnectionTemplate(t, ctx)

	wantPatterns := []string{
		"func cursorKeyset(d sql.Dialect, keys []string, cursor map[string]any, op string) sql.Condition",
		// Every key is quoted by the dialect, and the quoted token is
		// the Column that PrefixConditions qualifies on the O2O-join path.
		"col := d.QuoteIdentifier(key)",
		`sql.Condition{Clause: col + " " + op + " $", Value: cursor[key], Column: col}`,
		`compare(prev, "=")`,
		"compare(keys[i], op)",
		"sql.And(",
		"sql.Or(",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing cursorKeyset: %q\n\nfull output:\n%s", want, output)
		}
	}
	// A key written into the clause unquoted is the defect guarded here.
	for _, bad := range []string{`fmt.Sprintf("%s %s $", keys`, `keys[j] + " = $"`} {
		if strings.Contains(output, bad) {
			t.Errorf("cursorKeyset writes a cursor key unquoted: found %q\n\nfull output:\n%s", bad, output)
		}
	}
}

// --- Test: cursorSorts function ---

func TestConnectionTemplate_cursorSorts(t *testing.T) {
	ctx := testConnectionContext()
	output := executeConnectionTemplate(t, ctx)

	wantPatterns := []string{
		"func cursorSorts(keys []string, ascending bool) []sql.Sort",
		"direction := sql.Asc",
		"direction = sql.Desc",
		"sql.Sort{Column: key, Direction: direction}",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing cursorSorts: %q\n\nfull output:\n%s", want, output)
		}
	}
}

// --- Test: Compiles cleanly ---

func TestConnectionTemplate_compilesCleanly(t *testing.T) {
	ctx := testConnectionContext()
	output := executeConnectionTemplate(t, ctx)

	_, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "connection_gen.go")
	if err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

// --- Test: Golden file ---

func TestConnectionTemplate_goldenFile(t *testing.T) {
	ctx := testConnectionContext()
	output := executeConnectionTemplate(t, ctx)

	formatted, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "connection_gen.go")
	if err != nil {
		t.Fatalf("formatting: %v", err)
	}

	got := string(formatted)
	goldenPath := filepath.Join("testdata", "golden", "connection_gen.go")

	if *update {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o750); err != nil { //nolint:gosec // test helper
			t.Fatalf("creating golden dir: %v", err)
		}
		if err := os.WriteFile(goldenPath, formatted, 0o600); err != nil {
			t.Fatalf("writing golden file: %v", err)
		}
		t.Log("golden file updated")
		return
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // golden file path is not user-controlled
	if err != nil {
		t.Fatalf("reading golden file (run with -update to create): %v", err)
	}

	if diff := cmp.Diff(string(want), got); diff != "" {
		t.Errorf("connection_gen.go mismatch (-want +got):\n%s", diff)
	}
}

// --- Test: BuildConnectionContext ---

func TestBuildConnectionContext(t *testing.T) {
	ctx := gen.BuildConnectionContext("db")

	if ctx.Package != "db" {
		t.Errorf("Package = %q, want %q", ctx.Package, "db")
	}

	wantImports := []string{
		"encoding/base64",
		"encoding/json",
		"github.com/teandresmith/sqlgen/sql",
	}
	if diff := cmp.Diff(wantImports, ctx.Imports); diff != "" {
		t.Errorf("Imports mismatch (-want +got):\n%s", diff)
	}
}
