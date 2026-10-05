package parser_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/parser"
)

// --- Minimal test parser ---
//
// testParser is a minimal SQL parser for testing multi-file logic.
// It understands:
//   - CREATE TABLE [schema.]name (col1 type1, col2 type2 [REFERENCES ref(col)], ...)
//   - ALTER TABLE [schema.]name ADD COLUMN col type
//   - ALTER TABLE [schema.]name DROP COLUMN col
//   - DROP TABLE [IF EXISTS] [schema.]name
//   - CREATE TYPE [schema.]name AS ENUM (...)
//   - DROP TYPE [IF EXISTS] [schema.]name
//
// Duplicate CREATE statements are errors (matching real parser behavior).

type testParser struct {
	schema        *parser.Schema
	tableIndex    map[string]int
	defaultSchema string
}

func newTestParser(defaultSchema string) *testParser {
	return &testParser{
		schema:        &parser.Schema{},
		tableIndex:    make(map[string]int),
		defaultSchema: defaultSchema,
	}
}

func (p *testParser) Parse(_ string, sql []byte) error {
	for _, stmt := range splitStatements(string(sql)) {
		if err := p.processStmt(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (p *testParser) Schema() *parser.Schema {
	return p.schema
}

var (
	createTableRe   = regexp.MustCompile(`(?i)^\s*CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?(\S+)\s*\(([^)]*)\)`)
	alterAddColRe   = regexp.MustCompile(`(?i)^\s*ALTER\s+TABLE\s+(\S+)\s+ADD\s+COLUMN\s+(\w+)\s+(\S+)`)
	alterDropColRe  = regexp.MustCompile(`(?i)^\s*ALTER\s+TABLE\s+(\S+)\s+DROP\s+COLUMN\s+(\w+)`)
	dropTableTestRe = regexp.MustCompile(`(?i)^\s*DROP\s+TABLE\s+(?:IF\s+EXISTS\s+)?(\S+)`)
	createEnumRe    = regexp.MustCompile(`(?i)^\s*CREATE\s+TYPE\s+(\S+)\s+AS\s+ENUM\s*\(([^)]*)\)`)
	dropTypeTestRe  = regexp.MustCompile(`(?i)^\s*DROP\s+TYPE\s+(?:IF\s+EXISTS\s+)?(\S+)`)
)

func (p *testParser) processStmt(stmt string) error {
	if m := createTableRe.FindStringSubmatch(stmt); m != nil {
		return p.parseCreateTable(m[1], m[2])
	}
	if m := alterAddColRe.FindStringSubmatch(stmt); m != nil {
		return p.parseAlterAddColumn(m[1], m[2], m[3])
	}
	if m := alterDropColRe.FindStringSubmatch(stmt); m != nil {
		return p.parseAlterDropColumn(m[1], m[2])
	}
	if m := dropTableTestRe.FindStringSubmatch(stmt); m != nil {
		return p.parseDropTable(m[1])
	}
	if m := createEnumRe.FindStringSubmatch(stmt); m != nil {
		return p.parseCreateEnum(m[1], m[2])
	}
	if m := dropTypeTestRe.FindStringSubmatch(stmt); m != nil {
		return p.parseDropType(m[1])
	}
	return nil
}

func (p *testParser) parseCreateTable(rawName, colDefs string) error {
	schema, name := p.qualifyName(rawName)
	key := keyFor(schema, name)
	if _, exists := p.tableIndex[key]; exists {
		return fmt.Errorf("table %s already exists", key)
	}

	table := parser.Table{Name: name, Schema: schema}
	for def := range strings.SplitSeq(colDefs, ",") {
		def = strings.TrimSpace(def)
		if def == "" {
			continue
		}
		parts := strings.Fields(def)
		if len(parts) < 2 {
			continue
		}
		col := parser.Column{Name: parts[0], Type: parts[1]}
		for i, part := range parts {
			if strings.EqualFold(part, "REFERENCES") && i+1 < len(parts) {
				refRaw := parts[i+1]
				refTable, refCol, _ := strings.Cut(refRaw, "(")
				refCol = strings.TrimRight(refCol, ")")
				refSchema, refName := p.qualifyName(refTable)
				col.FKReference = &parser.FKReference{
					Table: refName, Schema: refSchema, Column: refCol,
				}
				break
			}
		}
		table.Columns = append(table.Columns, col)
	}
	p.tableIndex[key] = len(p.schema.Tables)
	p.schema.Tables = append(p.schema.Tables, table)
	return nil
}

func (p *testParser) parseAlterAddColumn(rawName, colName, colType string) error {
	schema, name := p.qualifyName(rawName)
	key := keyFor(schema, name)
	idx, ok := p.tableIndex[key]
	if !ok {
		return fmt.Errorf("table %s not found", key)
	}
	p.schema.Tables[idx].Columns = append(p.schema.Tables[idx].Columns, parser.Column{
		Name: colName, Type: colType,
	})
	return nil
}

func (p *testParser) parseAlterDropColumn(rawName, colName string) error {
	schema, name := p.qualifyName(rawName)
	key := keyFor(schema, name)
	idx, ok := p.tableIndex[key]
	if !ok {
		return fmt.Errorf("table %s not found", key)
	}
	p.schema.Tables[idx].Columns = slices.DeleteFunc(
		p.schema.Tables[idx].Columns,
		func(c parser.Column) bool { return c.Name == colName },
	)
	return nil
}

func (p *testParser) parseDropTable(rawName string) error {
	schema, name := p.qualifyName(rawName)
	key := keyFor(schema, name)
	idx, ok := p.tableIndex[key]
	if !ok {
		return nil
	}
	if err := parser.CheckTableDropRefs(p.schema.Tables, schema, name); err != nil {
		return fmt.Errorf("drop table %s: %w", key, err)
	}
	p.schema.Tables = slices.Delete(p.schema.Tables, idx, idx+1)
	delete(p.tableIndex, key)
	for k, v := range p.tableIndex {
		if v > idx {
			p.tableIndex[k] = v - 1
		}
	}
	return nil
}

func (p *testParser) parseCreateEnum(rawName, values string) error {
	schema, name := p.qualifyName(rawName)
	for _, e := range p.schema.Enums {
		if e.Schema == schema && e.Name == name {
			return fmt.Errorf("type %s already exists", keyFor(schema, name))
		}
	}
	var vals []string
	for v := range strings.SplitSeq(values, ",") {
		v = strings.TrimSpace(v)
		v = strings.Trim(v, "'\"")
		if v != "" {
			vals = append(vals, v)
		}
	}
	p.schema.Enums = append(p.schema.Enums, parser.Enum{
		Name: name, Schema: schema, Values: vals,
	})
	return nil
}

func (p *testParser) parseDropType(rawName string) error {
	schema, name := p.qualifyName(rawName)
	if err := parser.CheckTypeDropRefs(p.schema.Tables, schema, name); err != nil {
		return fmt.Errorf("drop type %s: %w", keyFor(schema, name), err)
	}
	p.schema.Enums = slices.DeleteFunc(p.schema.Enums, func(e parser.Enum) bool {
		return e.Schema == schema && e.Name == name
	})
	return nil
}

func (p *testParser) qualifyName(raw string) (string, string) {
	raw = strings.Trim(raw, "\"`")
	if before, after, ok := strings.Cut(raw, "."); ok {
		return strings.Trim(before, "\"`"), strings.Trim(after, "\"`")
	}
	return p.defaultSchema, raw
}

func keyFor(schema, name string) string {
	if schema == "" {
		return name
	}
	return schema + "." + name
}

func splitStatements(sql string) []string {
	var stmts []string
	for s := range strings.SplitSeq(sql, ";") {
		s = strings.TrimSpace(s)
		if s != "" {
			stmts = append(stmts, s)
		}
	}
	return stmts
}

// --- Test helpers ---

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}

// --- DiscoverFiles tests ---

func TestDiscoverFiles_LexicographicOrder(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "0003_reviews.sql", "")
	writeFile(t, dir, "0001_initial.sql", "")
	writeFile(t, dir, "0002_products.sql", "")

	files, err := parser.DiscoverFiles([]string{dir})
	if err != nil {
		t.Fatalf("DiscoverFiles() unexpected error: %v", err)
	}
	want := []string{
		filepath.Join(dir, "0001_initial.sql"),
		filepath.Join(dir, "0002_products.sql"),
		filepath.Join(dir, "0003_reviews.sql"),
	}
	if diff := cmp.Diff(want, files); diff != "" {
		t.Errorf("DiscoverFiles() mismatch (-want +got):\n%s", diff)
	}
}

func TestDiscoverFiles_DownFilePatternsSkipped(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "0001_initial.sql", "")
	writeFile(t, dir, "0001_initial.down.sql", "")
	writeFile(t, dir, "0002_down.sql", "")
	writeFile(t, dir, "0002_products.sql", "")

	files, err := parser.DiscoverFiles([]string{dir})
	if err != nil {
		t.Fatalf("DiscoverFiles() unexpected error: %v", err)
	}
	want := []string{
		filepath.Join(dir, "0001_initial.sql"),
		filepath.Join(dir, "0002_products.sql"),
	}
	if diff := cmp.Diff(want, files); diff != "" {
		t.Errorf("DiscoverFiles() mismatch (-want +got):\n%s", diff)
	}
}

func TestDiscoverFiles_UpFilePatternsNotSkipped(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "0001_initial.up.sql", "")
	writeFile(t, dir, "0001_up.sql", "")
	writeFile(t, dir, "0002_schema.sql", "")

	files, err := parser.DiscoverFiles([]string{dir})
	if err != nil {
		t.Fatalf("DiscoverFiles() unexpected error: %v", err)
	}
	want := []string{
		filepath.Join(dir, "0001_initial.up.sql"),
		filepath.Join(dir, "0001_up.sql"),
		filepath.Join(dir, "0002_schema.sql"),
	}
	if diff := cmp.Diff(want, files); diff != "" {
		t.Errorf("DiscoverFiles() mismatch (-want +got):\n%s", diff)
	}
}

func TestDiscoverFiles_MixedFilesAndDirectories(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "migrations")
	if err := os.Mkdir(subdir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, subdir, "0002_second.sql", "")
	writeFile(t, subdir, "0001_first.sql", "")
	explicit := writeFile(t, dir, "schema.sql", "")

	files, err := parser.DiscoverFiles([]string{explicit, subdir})
	if err != nil {
		t.Fatalf("DiscoverFiles() unexpected error: %v", err)
	}
	want := []string{
		explicit,
		filepath.Join(subdir, "0001_first.sql"),
		filepath.Join(subdir, "0002_second.sql"),
	}
	if diff := cmp.Diff(want, files); diff != "" {
		t.Errorf("DiscoverFiles() mismatch (-want +got):\n%s", diff)
	}
}

func TestDiscoverFiles_NonSQLFilesIgnored(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "schema.sql", "")
	writeFile(t, dir, "readme.md", "")
	writeFile(t, dir, "data.csv", "")

	files, err := parser.DiscoverFiles([]string{dir})
	if err != nil {
		t.Fatalf("DiscoverFiles() unexpected error: %v", err)
	}
	want := []string{filepath.Join(dir, "schema.sql")}
	if diff := cmp.Diff(want, files); diff != "" {
		t.Errorf("DiscoverFiles() mismatch (-want +got):\n%s", diff)
	}
}

// --- ParseFiles: Duplicate CREATE detection ---

func TestParseFiles_DuplicateCreateTable(t *testing.T) {
	dir := t.TempDir()
	f1 := writeFile(t, dir, "001.sql", "CREATE TABLE users (id INT, name TEXT);")
	f2 := writeFile(t, dir, "002.sql", "CREATE TABLE users (id INT, email TEXT);")

	p := newTestParser("")
	err := parser.ParseFiles(p, []string{f1, f2})
	if err == nil {
		t.Fatal("expected error for duplicate CREATE TABLE, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should mention 'already exists', got: %v", err)
	}
}

func TestParseFiles_DuplicateCreateEnum(t *testing.T) {
	dir := t.TempDir()
	f1 := writeFile(t, dir, "001.sql", "CREATE TYPE status AS ENUM ('active', 'inactive');")
	f2 := writeFile(t, dir, "002.sql", "CREATE TYPE status AS ENUM ('active', 'inactive', 'suspended');")

	p := newTestParser("")
	err := parser.ParseFiles(p, []string{f1, f2})
	if err == nil {
		t.Fatal("expected error for duplicate CREATE TYPE, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should mention 'already exists', got: %v", err)
	}
}

// --- ParseFiles: Sequential migration application ---

func TestParseFiles_SequentialAlterAddColumn(t *testing.T) {
	dir := t.TempDir()
	f1 := writeFile(t, dir, "001.sql", "CREATE TABLE products (id INT, name TEXT);")
	f2 := writeFile(t, dir, "002.sql", "ALTER TABLE products ADD COLUMN price DECIMAL;")
	f3 := writeFile(t, dir, "003.sql", "ALTER TABLE products ADD COLUMN description TEXT;")

	p := newTestParser("")
	if err := parser.ParseFiles(p, []string{f1, f2, f3}); err != nil {
		t.Fatalf("ParseFiles() unexpected error: %v", err)
	}

	tables := p.Schema().Tables
	if len(tables) != 1 {
		t.Fatalf("expected 1 table, got %d", len(tables))
	}
	wantCols := []string{"id", "name", "price", "description"}
	gotCols := make([]string, 0, len(tables[0].Columns))
	for _, c := range tables[0].Columns {
		gotCols = append(gotCols, c.Name)
	}
	if diff := cmp.Diff(wantCols, gotCols); diff != "" {
		t.Errorf("columns mismatch (-want +got):\n%s", diff)
	}
}

func TestParseFiles_SequentialDropTable(t *testing.T) {
	dir := t.TempDir()
	f1 := writeFile(t, dir, "001.sql", `
		CREATE TABLE users (id INT, name TEXT);
		CREATE TABLE legacy (id INT, data TEXT);
	`)
	f2 := writeFile(t, dir, "002.sql", "DROP TABLE legacy;")

	p := newTestParser("")
	if err := parser.ParseFiles(p, []string{f1, f2}); err != nil {
		t.Fatalf("ParseFiles() unexpected error: %v", err)
	}

	tables := p.Schema().Tables
	if len(tables) != 1 {
		t.Fatalf("expected 1 table after DROP, got %d", len(tables))
	}
	if tables[0].Name != "users" {
		t.Errorf("remaining table = %q, want %q", tables[0].Name, "users")
	}
}

func TestParseFiles_SequentialDropColumn(t *testing.T) {
	dir := t.TempDir()
	f1 := writeFile(t, dir, "001.sql", "CREATE TABLE products (id INT, name TEXT, legacy TEXT);")
	f2 := writeFile(t, dir, "002.sql", "ALTER TABLE products DROP COLUMN legacy;")
	f3 := writeFile(t, dir, "003.sql", "ALTER TABLE products ADD COLUMN description TEXT;")

	p := newTestParser("")
	if err := parser.ParseFiles(p, []string{f1, f2, f3}); err != nil {
		t.Fatalf("ParseFiles() unexpected error: %v", err)
	}

	wantCols := []string{"id", "name", "description"}
	gotCols := make([]string, 0, len(p.Schema().Tables[0].Columns))
	for _, c := range p.Schema().Tables[0].Columns {
		gotCols = append(gotCols, c.Name)
	}
	if diff := cmp.Diff(wantCols, gotCols); diff != "" {
		t.Errorf("columns mismatch (-want +got):\n%s", diff)
	}
}

func TestParseFiles_DropTableThenReCreate(t *testing.T) {
	dir := t.TempDir()
	f1 := writeFile(t, dir, "001.sql", "CREATE TABLE products (id INT, legacy TEXT);")
	f2 := writeFile(t, dir, "002.sql", "DROP TABLE products;")
	f3 := writeFile(t, dir, "003.sql", "CREATE TABLE products (id INT, name TEXT, price DECIMAL);")

	p := newTestParser("")
	if err := parser.ParseFiles(p, []string{f1, f2, f3}); err != nil {
		t.Fatalf("ParseFiles() unexpected error: %v", err)
	}

	tables := p.Schema().Tables
	if len(tables) != 1 {
		t.Fatalf("expected 1 table, got %d", len(tables))
	}
	wantCols := []string{"id", "name", "price"}
	gotCols := make([]string, 0, len(tables[0].Columns))
	for _, c := range tables[0].Columns {
		gotCols = append(gotCols, c.Name)
	}
	if diff := cmp.Diff(wantCols, gotCols); diff != "" {
		t.Errorf("columns after drop+re-create mismatch (-want +got):\n%s", diff)
	}
}

func TestParseFiles_DropType(t *testing.T) {
	dir := t.TempDir()
	f1 := writeFile(t, dir, "001.sql", `
		CREATE TABLE users (id INT);
		CREATE TYPE status AS ENUM ('active', 'inactive');
	`)
	f2 := writeFile(t, dir, "002.sql", "DROP TYPE status;")

	p := newTestParser("")
	if err := parser.ParseFiles(p, []string{f1, f2}); err != nil {
		t.Fatalf("ParseFiles() unexpected error: %v", err)
	}

	if len(p.Schema().Enums) != 0 {
		t.Errorf("expected 0 enums after DROP TYPE, got %d", len(p.Schema().Enums))
	}
}

// --- ParseFiles: Drop validation ---

func TestParseFiles_DropTableWithFKReference(t *testing.T) {
	dir := t.TempDir()
	f1 := writeFile(t, dir, "001.sql", `
		CREATE TABLE users (id INT);
		CREATE TABLE orders (id INT, user_id INT REFERENCES users(id));
	`)
	f2 := writeFile(t, dir, "002.sql", "DROP TABLE users;")

	p := newTestParser("")
	err := parser.ParseFiles(p, []string{f1, f2})
	if err == nil {
		t.Fatal("expected error when dropping table referenced by FK, got nil")
	}
	if !strings.Contains(err.Error(), "cannot drop table") {
		t.Errorf("error should mention 'cannot drop table', got: %v", err)
	}
}

func TestParseFiles_DropTypeWithColumnReference(t *testing.T) {
	dir := t.TempDir()
	f1 := writeFile(t, dir, "001.sql", `
		CREATE TYPE status AS ENUM ('active', 'inactive');
		CREATE TABLE users (id INT, status status);
	`)
	f2 := writeFile(t, dir, "002.sql", "DROP TYPE status;")

	p := newTestParser("")
	err := parser.ParseFiles(p, []string{f1, f2})
	if err == nil {
		t.Fatal("expected error when dropping type used by column, got nil")
	}
	if !strings.Contains(err.Error(), "cannot drop type") {
		t.Errorf("error should mention 'cannot drop type', got: %v", err)
	}
}

// --- ParseFiles: Schema qualification ---

func TestParseFiles_DuplicateWithSchemaQualification(t *testing.T) {
	dir := t.TempDir()
	f1 := writeFile(t, dir, "001.sql", "CREATE TABLE users (id INT);")
	f2 := writeFile(t, dir, "002.sql", "CREATE TABLE users (id INT, name TEXT);")

	p := newTestParser("public")
	err := parser.ParseFiles(p, []string{f1, f2})
	if err == nil {
		t.Fatal("expected error for duplicate CREATE TABLE with schema qualification, got nil")
	}
	if !strings.Contains(err.Error(), "public.users") {
		t.Errorf("error should mention qualified name, got: %v", err)
	}
}

// --- ParseFiles: ALTER on nonexistent table ---

func TestParseFiles_AlterNonexistentTable(t *testing.T) {
	dir := t.TempDir()
	f1 := writeFile(t, dir, "001.sql", "ALTER TABLE users ADD COLUMN name TEXT;")

	p := newTestParser("")
	err := parser.ParseFiles(p, []string{f1})
	if err == nil {
		t.Fatal("expected error for ALTER on nonexistent table, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}
