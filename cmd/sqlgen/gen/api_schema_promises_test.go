package gen_test

import (
	"go/ast"
	goparser "go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/sql"
)

// tscConfig is a sqlgen.yml for dialect with the GraphQL API enabled under
// ./models/graph; extra is appended verbatim (top-level keys) and graphqlExtra
// under api.graphql (indented four spaces).
func tscConfig(dialect config.Dialect, casing, extra, graphqlExtra string) string {
	var driver string
	switch dialect {
	case config.DialectPostgres:
		driver = "pgx"
	case config.DialectMySQL:
		driver = "mysql"
	case config.DialectSQLite:
		driver = "sqlite"
	}
	return `version: v1
input:
  dialect: ` + string(dialect) + `
  paths:
    - ./schema.sql
output:
  driver: ` + driver + `
  dir: ./models
  package: models
` + extra + `
api:
  enabled: true
  graphql:
    enabled: true
    schema_dir: ./models/graph
    resolver_dir: ./models/graph
    package: graph
    field_casing: ` + casing + `
` + graphqlExtra
}

// tscGen chdirs into a fresh module root, parses ddl under dialect and runs
// relationship detection as the CLI pipeline does, loads
// body through the production loader and runs Generate. It returns the
// module root and the Generate error.
func tscGen(t *testing.T, dialect config.Dialect, ddl, body string) (string, *gen.GenerateResult, error) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/shop\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatalf("writing go.mod: %v", err)
	}
	t.Chdir(root)
	schema := parseDDLForTest(t, dialect, ddl)
	parser.DetectRelationships(schema)
	cfgPath := filepath.Join(t.TempDir(), "sqlgen.yml")
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig() error: %v\n%s", err, body)
	}
	res, err := gen.Generate(schema, cfg, "test")
	return root, res, err
}

// tscMustGen is tscGen failing the test on a Generate error.
func tscMustGen(t *testing.T, dialect config.Dialect, ddl, body string) string {
	t.Helper()
	root, _, err := tscGen(t, dialect, ddl, body)
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}
	return root
}

// tscGraphFiles returns every file under root/models/graph keyed by its
// slash-separated path relative to that directory.
func tscGraphFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	dir := filepath.Join(root, "models", "graph")
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(path) //nolint:gosec // a generated file under the test's temp project root
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", dir, err)
	}
	return out
}

// tscSchemaText concatenates every .graphqls file under the graph dir, in
// file-name order.
func tscSchemaText(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	files := tscGraphFiles(t, root)
	for _, name := range slices.Sorted(maps.Keys(files)) {
		if strings.HasSuffix(name, ".graphqls") {
			b.WriteString(files[name])
			b.WriteString("\n")
		}
	}
	return b.String()
}

// tscBlock returns the GraphQL block opened by header ("type Product {"),
// through its closing brace, or "" when the schema declares no such block.
func tscBlock(schema, header string) string {
	start := strings.Index(schema, header)
	if start < 0 {
		return ""
	}
	rest := schema[start:]
	if end := strings.Index(rest, "\n}"); end >= 0 {
		return rest[:end+2]
	}
	return rest
}

// tscRootFieldNames returns the field names declared in every `extend type
// <root> {` block of schema.
func tscRootFieldNames(schema, root string) []string {
	var names []string
	rest := schema
	for {
		block := tscBlock(rest, "extend type "+root+" {")
		if block == "" {
			return names
		}
		for _, line := range strings.Split(block, "\n")[1:] {
			line = strings.TrimSpace(line)
			name, _, ok := strings.Cut(line, "(")
			if !ok || strings.HasPrefix(line, `"`) {
				continue
			}
			names = append(names, name)
		}
		rest = rest[strings.Index(rest, block)+len(block):]
	}
}

// tscObjectFieldNames returns the field names of `type <name> {`.
func tscObjectFieldNames(t *testing.T, schema, name string) []string {
	t.Helper()
	block := tscBlock(schema, "type "+name+" {")
	if block == "" {
		t.Fatalf("schema declares no `type %s`:\n%s", name, schema)
	}
	var names []string
	for _, line := range strings.Split(block, "\n")[1:] {
		line = strings.TrimSpace(line)
		if line == "" || line == "}" || strings.HasPrefix(line, `"`) {
			continue
		}
		if field, _, ok := strings.Cut(line, ":"); ok {
			names = append(names, field)
		}
	}
	return names
}

// tscNoDBAccessDDL is a fixture carrying every API shape the generated graph
// package has a file for: a UUID-PK table, a composite-PK junction, an O2M /
// M2O pair, an M2M, a soft-delete column, an enum, JSON and a decimal.
const tscNoDBAccessDDL = `
CREATE TYPE order_status AS ENUM ('open', 'closed');
CREATE TABLE companies (id uuid PRIMARY KEY, name text NOT NULL, meta jsonb, deleted_at timestamptz);
CREATE TABLE tags (id bigserial PRIMARY KEY, label text NOT NULL);
CREATE TABLE orders (
	id uuid PRIMARY KEY,
	company_id uuid NOT NULL REFERENCES companies(id),
	status order_status NOT NULL,
	total numeric NOT NULL,
	created_at timestamptz NOT NULL
);
CREATE TABLE order_tags (
	order_id uuid NOT NULL REFERENCES orders(id),
	tag_id bigint NOT NULL REFERENCES tags(id),
	PRIMARY KEY (order_id, tag_id)
);`

// TestGraphQLLayer_addsNoDatabaseAccessOfItsOwn pins PRD §26: the API layer
// "is built entirely on the unified client ... and adds no database access of
// its own". Every Go file sqlgen writes into the graph package and its
// sqlgenresolver helper is parsed and checked for the three ways a file could
// reach the database without the client: importing a driver or database/sql,
// referencing a Querier, or building SQL — through the runtime sql package's
// builders and dialects, or as a statement literal.
func TestGraphQLLayer_addsNoDatabaseAccessOfItsOwn(t *testing.T) {
	root := tscMustGen(t, config.DialectPostgres, tscNoDBAccessDDL, tscConfig(config.DialectPostgres, "camel_case",
		"generation:\n  soft_delete_columns:\n    - name: deleted_at\n      type: timestamptz\n  nested_mutations:\n    enabled: true\n", ""))

	forbiddenImports := []string{"database/sql", "github.com/jackc/pgx", "github.com/go-sql-driver/mysql", "modernc.org/sqlite", "github.com/mattn/go-sqlite3"}
	sqlLiteral := regexp.MustCompile(`\b(SELECT\s|INSERT\s+INTO|UPDATE\s+\S+\s+SET|DELETE\s+FROM)`)
	querierMethods := map[string]bool{"Exec": true, "Query": true, "QueryRow": true, "Begin": true, "BeginTx": true}

	files := tscGraphFiles(t, root)
	checked := 0
	for name, src := range files {
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		checked++
		f, err := goparser.ParseFile(token.NewFileSet(), name, src, goparser.SkipObjectResolution)
		if err != nil {
			t.Errorf("%s does not parse: %v", name, err)
			continue
		}
		runtimeSQL := ""
		for _, imp := range f.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			for _, bad := range forbiddenImports {
				if path == bad || strings.HasPrefix(path, bad+"/") {
					t.Errorf("%s imports %s", name, path)
				}
			}
			if strings.HasPrefix(path, "github.com/teandresmith/sqlgen/database/") {
				t.Errorf("%s imports the driver adapter %s", name, path)
			}
			if path == "github.com/teandresmith/sqlgen/sql" {
				runtimeSQL = "sql"
				if imp.Name != nil {
					runtimeSQL = imp.Name.Name
				}
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.Ident:
				if n.Name == "Querier" {
					t.Errorf("%s references a Querier", name)
				}
			case *ast.SelectorExpr:
				if x, ok := n.X.(*ast.Ident); ok && runtimeSQL != "" && x.Name == runtimeSQL {
					sel := n.Sel.Name
					if strings.HasPrefix(sel, "Build") || strings.HasPrefix(sel, "New") || strings.Contains(sel, "Dialect") {
						t.Errorf("%s builds SQL through %s.%s", name, runtimeSQL, sel)
					}
				}
			case *ast.CallExpr:
				if sel, ok := n.Fun.(*ast.SelectorExpr); ok && querierMethods[sel.Sel.Name] {
					t.Errorf("%s calls a Querier-shaped method .%s(...)", name, sel.Sel.Name)
				}
			case *ast.BasicLit:
				if n.Kind == token.STRING && sqlLiteral.MatchString(n.Value) {
					t.Errorf("%s carries a SQL statement literal %s", name, n.Value)
				}
			}
			return true
		})
	}
	if checked < 5 {
		t.Fatalf("checked %d generated Go files under models/graph, want the full graph package; got files %v", checked, slices.Sorted(maps.Keys(files)))
	}
}

// tscCasingDDL carries a composite primary key whose columns are multi-word
// (so argument names show the casing), a multi-word scalar column and an M2O
// relationship (so the walker shows it).
const tscCasingDDL = `
CREATE TABLE companies (id uuid PRIMARY KEY, company_name text NOT NULL, created_at timestamptz NOT NULL);
CREATE TABLE order_lines (
	order_id bigint NOT NULL,
	line_no integer NOT NULL,
	product_name text,
	company_id uuid REFERENCES companies(id),
	PRIMARY KEY (order_id, line_no)
);`

// TestGraphQLCasing_perElementConventions pins PRD §26.3 "Per-element
// conventions" under both field_casing values: type names are always
// PascalCase, Query / Mutation names always camelCase, argument names follow
// field_casing, and the fieldOptionsFromCollected walkers match the casing
// the schema was emitted in.
func TestGraphQLCasing_perElementConventions(t *testing.T) {
	tests := []struct {
		name         string
		casing       string
		wantArgs     string // the get query's PK argument list
		wantWalker   []string
		absentWalker []string
		wantRelCase  string // the company walker's case for its O2M edge
	}{
		{
			name:         "camel_case",
			casing:       "camel_case",
			wantArgs:     "orderLine(orderID: Int!, lineNo: Int!): OrderLine",
			wantWalker:   []string{`case "orderID":`, `case "lineNo":`, `case "productName":`},
			absentWalker: []string{`case "order_id":`, `case "line_no":`, `case "product_name":`},
			wantRelCase:  `case "orderLines":`,
		},
		{
			name:         "snake_case",
			casing:       "snake_case",
			wantArgs:     "orderLine(order_id: Int!, line_no: Int!): OrderLine",
			wantWalker:   []string{`case "order_id":`, `case "line_no":`, `case "product_name":`},
			absentWalker: []string{`case "orderID":`, `case "lineNo":`, `case "productName":`},
			wantRelCase:  `case "order_lines":`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := tscMustGen(t, config.DialectPostgres, tscCasingDDL, tscConfig(config.DialectPostgres, tt.casing, "", ""))
			schema := tscSchemaText(t, root)

			t.Run("type names are PascalCase", func(t *testing.T) {
				for _, header := range []string{
					"type OrderLine {", "type OrderLineConnection {", "type OrderLineEdge {",
					"type OrderLineListResult {", "input OrderLineFilter {", "input OrderLineSort {",
					"enum OrderLineSortField {", "input CreateOrderLineInput {", "input UpdateOrderLineInput {",
					"type Company {",
				} {
					if !strings.Contains(schema, header) {
						t.Errorf("schema lacks %q under %s", header, tt.casing)
					}
				}
			})

			t.Run("root field names are camelCase", func(t *testing.T) {
				want := map[string][]string{
					"Query":    {"orderLine", "orderLines", "orderLineList"},
					"Mutation": {"createOrderLine", "createOrderLines", "updateOrderLine", "updateOrderLines", "upsertOrderLine", "deleteOrderLine"},
				}
				for _, rootType := range []string{"Query", "Mutation"} {
					got := tscRootFieldNames(schema, rootType)
					for _, name := range want[rootType] {
						if !slices.Contains(got, name) {
							t.Errorf("%s fields %v lack %q under %s", rootType, got, name, tt.casing)
						}
					}
					for _, name := range got {
						if strings.Contains(name, "_") || name == "" || strings.ToLower(name[:1]) != name[:1] {
							t.Errorf("%s field %q is not camelCase under %s", rootType, name, tt.casing)
						}
					}
				}
			})

			t.Run("argument names follow field_casing", func(t *testing.T) {
				if !strings.Contains(schema, tt.wantArgs) {
					t.Errorf("schema lacks %q under %s:\n%s", tt.wantArgs, tt.casing, tscBlock(schema, "extend type Query {"))
				}
				// The PK argument names are the object type's own field names.
				fields := tscObjectFieldNames(t, schema, "OrderLine")
				args := tt.wantArgs[strings.Index(tt.wantArgs, "(")+1 : strings.Index(tt.wantArgs, ")")]
				for _, arg := range strings.Split(args, ", ") {
					name, _, _ := strings.Cut(arg, ":")
					if !slices.Contains(fields, name) {
						t.Errorf("argument %q is not spelled like any OrderLine field %v", name, fields)
					}
				}
			})

			t.Run("walker cases match the casing", func(t *testing.T) {
				walker := walkerBody(t, tscGraphFiles(t, root)["sqlgenresolver/field_options_gen.go"], "orderLineFieldOptionsFromCollected")
				for _, want := range tt.wantWalker {
					if !strings.Contains(walker, want) {
						t.Errorf("walker lacks %s under %s:\n%s", want, tt.casing, walker)
					}
				}
				for _, absent := range tt.absentWalker {
					if strings.Contains(walker, absent) {
						t.Errorf("walker carries %s, the other casing's spelling, under %s", absent, tt.casing)
					}
				}
				// Every field the schema advertises — columns and the
				// relationship alike — has a walker case spelled the same way.
				for _, field := range tscObjectFieldNames(t, schema, "OrderLine") {
					if !strings.Contains(walker, `case "`+field+`":`) {
						t.Errorf("OrderLine field %q has no walker case under %s", field, tt.casing)
					}
				}
				company := walkerBody(t, tscGraphFiles(t, root)["sqlgenresolver/field_options_gen.go"], "companyFieldOptionsFromCollected")
				if !strings.Contains(company, tt.wantRelCase) {
					t.Errorf("company walker lacks %s under %s:\n%s", tt.wantRelCase, tt.casing, company)
				}
				for _, field := range tscObjectFieldNames(t, schema, "Company") {
					if !strings.Contains(company, `case "`+field+`":`) {
						t.Errorf("Company field %q has no walker case under %s", field, tt.casing)
					}
				}
			})
		})
	}
}

// TestGraphQLSchema_domainColumnTakesUnderlyingScalar pins the §26.4 mapping
// row "Domain type → Underlying scalar (e.g., email_address → String)".
func TestGraphQLSchema_domainColumnTakesUnderlyingScalar(t *testing.T) {
	root := tscMustGen(t, config.DialectPostgres, `
CREATE DOMAIN email_address AS text;
CREATE DOMAIN quantity AS integer CHECK (VALUE >= 0);
CREATE TABLE contacts (
	id bigserial PRIMARY KEY,
	email email_address NOT NULL,
	alt_email email_address,
	stock quantity NOT NULL
);`, tscConfig(config.DialectPostgres, "camel_case", "", ""))
	block := tscBlock(tscSchemaText(t, root), "type Contact {")
	for _, want := range []string{"email: String!", "altEmail: String\n", "stock: Int!"} {
		if !strings.Contains(block, want) {
			t.Errorf("type Contact lacks %q:\n%s", strings.TrimSpace(want), block)
		}
	}
}

// TestGraphQLSchema_uuidRetypedToStringIsString pins the §26.4 mapping row
// "UUID column (non-PK) — Go string → String (or String! if NOT NULL)",
// reached through either retyping mechanism. The column still filters as an
// identifier: its filter input field is an IDComparator, keyed on the SQL type.
func TestGraphQLSchema_uuidRetypedToStringIsString(t *testing.T) {
	tests := []struct {
		name  string
		extra string
	}{
		{
			name:  "column_map retype",
			extra: "tables:\n  contacts:\n    column_map:\n      ref:\n        type: string\n      alt_ref:\n        type: string\n",
		},
		{
			name:  "overrides.types retype",
			extra: "overrides:\n  types:\n    uuid:\n      type: string\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := tscMustGen(t, config.DialectPostgres,
				`CREATE TABLE contacts (id bigserial PRIMARY KEY, ref uuid NOT NULL, alt_ref uuid);`,
				tscConfig(config.DialectPostgres, "camel_case", tt.extra, ""))
			block := tscBlock(tscSchemaText(t, root), "type Contact {")
			for _, want := range []string{"ref: String!", "altRef: String\n"} {
				if !strings.Contains(block, want) {
					t.Errorf("type Contact lacks %q:\n%s", strings.TrimSpace(want), block)
				}
			}
			if strings.Contains(block, "UUID") {
				t.Errorf("a column retyped to string still advertises UUID:\n%s", block)
			}
			filter := tscBlock(tscSchemaText(t, root), "input ContactFilter {")
			for _, want := range []string{"ref: IDComparator\n", "altRef: NullableIDComparator\n"} {
				if !strings.Contains(filter, want) {
					t.Errorf("input ContactFilter lacks %q:\n%s", strings.TrimSpace(want), filter)
				}
			}
		})
	}
}

// TestGraphQLObjectType_unreadableTargetHasNoRelationshipField pins §26.4 "A
// relationship field exists only when its target is on the API": a table
// target with every column dropped by a §32.2 role is hidden, so the parent's
// object type omits the field. The exposed case is the control.
func TestGraphQLObjectType_unreadableTargetHasNoRelationshipField(t *testing.T) {
	// companies → products is the O2M edge relationship detection infers;
	// products is the target whose columns the roles drop.
	const ddl = `
CREATE TABLE companies (id uuid PRIMARY KEY, name text NOT NULL);
CREATE TABLE products (id uuid PRIMARY KEY, title text NOT NULL, company_id uuid NOT NULL REFERENCES companies(id));`
	every := func(role string) string {
		return "tables:\n  products:\n    column_map:\n" +
			"      id:\n        access: " + role + "\n" +
			"      title:\n        access: " + role + "\n" +
			"      company_id:\n        access: " + role + "\n"
	}
	tests := []struct {
		name      string
		extra     string
		wantField bool
	}{
		{name: "exposed target keeps the field", extra: "", wantField: true},
		{name: "every target column hidden drops the field", extra: every("hidden")},
		{name: "every target column internal drops the field", extra: every("internal")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := tscMustGen(t, config.DialectPostgres, ddl, tscConfig(config.DialectPostgres, "camel_case", tt.extra, ""))
			schema := tscSchemaText(t, root)
			fields := tscObjectFieldNames(t, schema, "Company")
			if got := slices.Contains(fields, "products"); got != tt.wantField {
				t.Errorf("type Company has a products field = %v, want %v (fields %v)", got, tt.wantField, fields)
			}
			walker := walkerBody(t, tscGraphFiles(t, root)["sqlgenresolver/field_options_gen.go"], "companyFieldOptionsFromCollected")
			if got := strings.Contains(walker, `case "products":`); got != tt.wantField {
				t.Errorf("company walker has a products case = %v, want %v", got, tt.wantField)
			}
		})
	}
}

// TestGraphQLCreateSurface_accessDroppedColumnsSuppressIt pins §26.4's
// empty-input rule "via §32.2 — for a table whose every writable column is
// dropped by an access role": no Create<T>Input and none of the mutations
// that take it. The all-public case is the control.
func TestGraphQLCreateSurface_accessDroppedColumnsSuppressIt(t *testing.T) {
	const ddl = `CREATE TABLE tokens (id bigserial PRIMARY KEY, label text NOT NULL, issued_at timestamptz NOT NULL);`
	createSurface := []string{"input CreateTokenInput {", "createToken(", "createTokens(", "upsertToken("}
	tests := []struct {
		name       string
		extra      string
		wantCreate bool
	}{
		{name: "public columns keep the create surface", extra: "", wantCreate: true},
		{
			name:  "every writable column read_only",
			extra: "tables:\n  tokens:\n    column_map:\n      label:\n        access: read_only\n      issued_at:\n        access: read_only\n",
		},
		{
			name:  "writable columns split across hidden and internal",
			extra: "tables:\n  tokens:\n    column_map:\n      label:\n        access: hidden\n      issued_at:\n        access: internal\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := tscMustGen(t, config.DialectPostgres, ddl, tscConfig(config.DialectPostgres, "camel_case", tt.extra, ""))
			schema := tscSchemaText(t, root)
			for _, frag := range createSurface {
				if got := strings.Contains(schema, frag); got != tt.wantCreate {
					t.Errorf("schema contains %q = %v, want %v", frag, got, tt.wantCreate)
				}
			}
			if !strings.Contains(schema, "type Token {") {
				t.Errorf("schema lost `type Token` — only the create surface should go")
			}
		})
	}
}

// TestUpsert_allPKTableDegradesToNoOp pins §26.4 "Upsert on an all-PK
// table": the conflict clause's UPDATE half excludes the conflict target, so
// a junction whose columns are exactly its key has nothing to set, and the
// statement degrades to `ON CONFLICT (...) DO NOTHING` on SQLite and a no-op
// self-assignment on MySQL. The generated client's update-column expression
// is evaluated against the generated column lists and handed to the runtime
// builder the generated code calls.
func TestUpsert_allPKTableDegradesToNoOp(t *testing.T) {
	tests := []struct {
		name       string
		dialect    config.Dialect
		ddl        string
		sqlDialect sql.Dialect
		wantClause string
	}{
		{
			name:       "sqlite emits ON CONFLICT DO NOTHING",
			dialect:    config.DialectSQLite,
			ddl:        `CREATE TABLE task_labels (task_id INTEGER NOT NULL, label_id INTEGER NOT NULL, PRIMARY KEY (task_id, label_id));`,
			sqlDialect: sql.NewSQLiteDialect(),
			wantClause: `ON CONFLICT ("task_id", "label_id") DO NOTHING`,
		},
		{
			name:       "mysql emits a self-assignment",
			dialect:    config.DialectMySQL,
			ddl:        "CREATE TABLE task_labels (task_id BIGINT NOT NULL, label_id BIGINT NOT NULL, PRIMARY KEY (task_id, label_id));",
			sqlDialect: sql.NewMySQLDialect(),
			wantClause: "ON DUPLICATE KEY UPDATE `task_id` = `task_id`",
		},
	}
	listRe := regexp.MustCompile(`"([a-z_]+)"`)
	strList := func(s string) []string {
		matches := listRe.FindAllStringSubmatch(s, -1)
		out := make([]string, 0, len(matches))
		for _, m := range matches {
			out = append(out, m[1])
		}
		return out
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := tscMustGen(t, tt.dialect, tt.ddl, tscConfig(tt.dialect, "camel_case", "", ""))
			if !strings.Contains(tscSchemaText(t, root), "upsertTaskLabel(input: CreateTaskLabelInput!): TaskLabel!") {
				t.Fatalf("schema has no upsertTaskLabel mutation")
			}
			models := tscReadModels(t, root)
			body := walkerBody(t, models, "(c *taskLabelClient) Upsert")

			cols := regexp.MustCompile(`columns := \[\]string\{([^}]*)\}`).FindStringSubmatch(body)
			excl := regexp.MustCompile(`updateColumns := excludeColumns\(columns, append\(conflictColumns,?([^)]*)\)\)`).FindStringSubmatch(body)
			target := regexp.MustCompile(`TaskLabelConflictPK: \{([^}]*)\}`).FindStringSubmatch(models)
			if cols == nil || excl == nil || target == nil {
				t.Fatalf("generated Upsert lost the columns / updateColumns / conflict-target shape this test reads:\n%s", body)
			}
			if !strings.Contains(body, "UpsertUpdateColumns: updateColumns") {
				t.Fatalf("generated Upsert does not pass updateColumns to the builder:\n%s", body)
			}
			columns, conflict := strList(cols[1]), strList(target[1])
			excluded := append(slices.Clone(conflict), strList(excl[1])...)
			var update []string
			for _, c := range columns {
				if !slices.Contains(excluded, c) {
					update = append(update, c)
				}
			}
			if len(update) != 0 {
				t.Errorf("an all-PK table's upsert sets %v, want nothing", update)
			}

			stmt, _ := sql.BuildInsert(tt.sqlDialect, sql.Table{Name: "task_labels"}, sql.InsertOptions{
				Columns:             columns,
				Values:              []any{1, 2},
				UpsertConflictKeys:  conflict,
				UpsertUpdateColumns: update,
			})
			if !strings.HasSuffix(stmt, tt.wantClause) {
				t.Errorf("upsert statement = %q, want it to end %q", stmt, tt.wantClause)
			}
		})
	}
}

// tscReadModels returns every generated Go file directly under root/models,
// concatenated.
func tscReadModels(t *testing.T, root string) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, "models"))
	if err != nil {
		t.Fatalf("reading models dir: %v", err)
	}
	var b strings.Builder
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, "models", e.Name())) //nolint:gosec // a generated file under the test's temp project root
		if err != nil {
			t.Fatalf("reading %s: %v", e.Name(), err)
		}
		b.Write(data)
		b.WriteString("\n")
	}
	return b.String()
}

// TestBytesComparator_emittedOnEveryDialect pins §26.4 "Dialect gating":
// "`BytesComparator` is emitted on all dialects — `bytea` on PostgreSQL and
// the `blob` / `binary` / `varbinary` family on MySQL and SQLite all resolve
// to `[]byte`."
func TestBytesComparator_emittedOnEveryDialect(t *testing.T) {
	tests := []struct {
		name    string
		dialect config.Dialect
		ddl     string
	}{
		{name: "postgres bytea", dialect: config.DialectPostgres, ddl: `CREATE TABLE blobs (id bigserial PRIMARY KEY, payload bytea NOT NULL, extra bytea);`},
		{name: "mysql blob and varbinary", dialect: config.DialectMySQL, ddl: "CREATE TABLE blobs (id BIGINT PRIMARY KEY AUTO_INCREMENT, payload BLOB NOT NULL, extra VARBINARY(64));"},
		{name: "sqlite blob", dialect: config.DialectSQLite, ddl: `CREATE TABLE blobs (id INTEGER PRIMARY KEY, payload BLOB NOT NULL, extra BLOB);`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := tscMustGen(t, tt.dialect, tt.ddl, tscConfig(tt.dialect, "camel_case", "", ""))
			schema := tscSchemaText(t, root)
			filter := tscBlock(schema, "input BlobFilter {")
			for _, want := range []string{"payload: BytesComparator", "extra: NullableBytesComparator"} {
				if !strings.Contains(filter, want) {
					t.Errorf("BlobFilter lacks %q:\n%s", want, filter)
				}
			}
			for _, decl := range []string{"input BytesComparator {", "input NullableBytesComparator {"} {
				if !strings.Contains(schema, decl) {
					t.Errorf("schema does not declare %q", decl)
				}
			}
		})
	}
}

// TestDurationComparator_ungatedOnEveryDialect pins §26.4 "Dialect gating":
// "`DurationComparator` is likewise ungated: `interval` is PostgreSQL-only,
// but a `time.Duration` column is reachable on any dialect through
// `type_map` / `overrides.types`."
func TestDurationComparator_ungatedOnEveryDialect(t *testing.T) {
	tests := []struct {
		name    string
		dialect config.Dialect
		ddl     string
		extra   string
	}{
		{
			name:    "mysql through type_map",
			dialect: config.DialectMySQL,
			ddl:     "CREATE TABLE jobs (id BIGINT PRIMARY KEY AUTO_INCREMENT, timeout BIGINT NOT NULL);",
			extra:   "tables:\n  jobs:\n    type_map:\n      timeout: time.Duration\n",
		},
		{
			name:    "mysql through overrides.types",
			dialect: config.DialectMySQL,
			ddl:     "CREATE TABLE jobs (id INT PRIMARY KEY AUTO_INCREMENT, timeout BIGINT NOT NULL);",
			extra:   "overrides:\n  types:\n    bigint:\n      type: time.Duration\n      import: time\n",
		},
		{
			name:    "sqlite through type_map",
			dialect: config.DialectSQLite,
			ddl:     `CREATE TABLE jobs (id INTEGER PRIMARY KEY, timeout INTEGER NOT NULL);`,
			extra:   "tables:\n  jobs:\n    type_map:\n      timeout: time.Duration\n",
		},
		{
			name:    "sqlite through overrides.types",
			dialect: config.DialectSQLite,
			ddl:     `CREATE TABLE jobs (id INTEGER PRIMARY KEY, timeout BIGINT NOT NULL);`,
			extra:   "overrides:\n  types:\n    bigint:\n      type: time.Duration\n      import: time\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := tscMustGen(t, tt.dialect, tt.ddl, tscConfig(tt.dialect, "camel_case", tt.extra, ""))
			schema := tscSchemaText(t, root)
			if block := tscBlock(schema, "type Job {"); !strings.Contains(block, "timeout: Duration!") {
				t.Fatalf("the fixture did not reach time.Duration — type Job:\n%s", block)
			}
			if filter := tscBlock(schema, "input JobFilter {"); !strings.Contains(filter, "timeout: DurationComparator") {
				t.Errorf("JobFilter lacks `timeout: DurationComparator`:\n%s", filter)
			}
			for _, decl := range []string{"input DurationComparator {", "input DurationRange {"} {
				if !strings.Contains(schema, decl) {
					t.Errorf("schema does not declare %q", decl)
				}
			}
		})
	}
}

// TestGraphQLTypeNames_excludingTheEntityResolvesACollision pins §26.4 "GraphQL
// type name ownership": "Excluding the entity from the API surface also
// resolves it, since an unreferenced name is never declared". Each fixture
// collides as written; the same config with the offending table set
// `api.enabled: false` generates cleanly and keeps the Go client for it.
func TestGraphQLTypeNames_excludingTheEntityResolvesACollision(t *testing.T) {
	tests := []struct {
		name      string
		table     string
		accessor  string
		ddl       string
		contested string
	}{
		{
			name:      "table against a fixed structural type",
			table:     "sort_directions",
			accessor:  "SortDirections",
			ddl:       `CREATE TABLE sort_directions (id uuid PRIMARY KEY, label text NOT NULL); CREATE TABLE notes (id uuid PRIMARY KEY, body text NOT NULL);`,
			contested: "SortDirection",
		},
		{
			name:      "table against an emitted comparator input",
			table:     "string_comparators",
			accessor:  "StringComparators",
			ddl:       `CREATE TABLE string_comparators (id uuid PRIMARY KEY, label text NOT NULL); CREATE TABLE notes (id uuid PRIMARY KEY, body text NOT NULL);`,
			contested: "StringComparator",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := tscGen(t, config.DialectPostgres, tt.ddl, tscConfig(config.DialectPostgres, "camel_case", "", "")); err == nil || !strings.Contains(err.Error(), tt.contested) {
				t.Fatalf("Generate() without the exclusion = %v, want a collision naming %s", err, tt.contested)
			}

			extra := "tables:\n  " + tt.table + ":\n    api:\n      enabled: false\n"
			root, _, err := tscGen(t, config.DialectPostgres, tt.ddl, tscConfig(config.DialectPostgres, "camel_case", extra, ""))
			if err != nil {
				t.Fatalf("Generate() with %s off the API: %v", tt.table, err)
			}
			schema := tscSchemaText(t, root)
			if n := strings.Count(schema, " "+tt.contested+" {"); n != 1 {
				t.Errorf("schema declares %s %d times, want once (the fixed claimant only)", tt.contested, n)
			}
			if !strings.Contains(tscReadModels(t, root), "func (c *Client) "+tt.accessor+"()") {
				t.Errorf("the Go client lost the %s accessor — api gates the API, never the client", tt.table)
			}
		})
	}
}

// TestBareBuiltinScalar_isInert pins §26.4.1: "An entry declaring
// `marshaling: builtin` with no `go_type` binds nothing to check — it reaches
// no column, declares no `scalar`". (The `models:` half is pinned in the cli
// package, where the merge input is built.)
func TestBareBuiltinScalar_isInert(t *testing.T) {
	const ddl = `CREATE TABLE contacts (id bigserial PRIMARY KEY, name text NOT NULL, meta jsonb, seen_at timestamptz NOT NULL);`
	bare := "    scalars:\n      Any:\n        marshaling: builtin\n"
	without := tscSchemaText(t, tscMustGen(t, config.DialectPostgres, ddl, tscConfig(config.DialectPostgres, "camel_case", "", "")))
	with := tscSchemaText(t, tscMustGen(t, config.DialectPostgres, ddl, tscConfig(config.DialectPostgres, "camel_case", "", bare)))

	if strings.Contains(with, "scalar Any") {
		t.Errorf("a bare builtin entry declared `scalar Any`")
	}
	if regexp.MustCompile(`:\s*\[?Any\b`).MatchString(with) {
		t.Errorf("a bare builtin entry reached a field or operand:\n%s", with)
	}
	if with != without {
		t.Errorf("a bare builtin entry changed the emitted schema")
	}
}

// TestScalarsGen_notEmittedWithoutRegistryExternalScalars pins §26.4.1
// "Generated file: graph/scalars_gen.go — ... Not emitted at all when every
// scalar in the schema falls into categories 1–3 or is consumer-declared".
// The fixture spans spec built-ins (text, integer, boolean), the
// gqlgen-bundled Time (timestamptz → time.Time), the method-based JSON
// (jsonb → types.JSON) and a consumer-declared external scalar; adding a uuid
// column — a category-4 registry scalar — is the control that brings the file
// back.
func TestScalarsGen_notEmittedWithoutRegistryExternalScalars(t *testing.T) {
	const base = `CREATE TABLE contacts (
	id bigserial PRIMARY KEY,
	name text NOT NULL,
	age integer,
	active boolean NOT NULL,
	seen_at timestamptz NOT NULL,
	meta jsonb,
	addr text NOT NULL`
	const consumer = "tables:\n  contacts:\n    column_map:\n      addr:\n        type: netip.Addr\n        import: net/netip\n"
	const scalars = "    scalars:\n      IPAddr:\n        go_type: net/netip.Addr\n        marshaling: external\n        marshaler_package: example.com/shop/gqlscalars\n"
	tests := []struct {
		name     string
		ddl      string
		wantFile bool
	}{
		{name: "categories one to three plus a consumer scalar", ddl: base + "\n);", wantFile: false},
		{name: "a uuid column brings the file back", ddl: base + ",\n\tref uuid NOT NULL\n);", wantFile: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, res, err := tscGen(t, config.DialectPostgres, tt.ddl, tscConfig(config.DialectPostgres, "camel_case", consumer, scalars))
			if err != nil {
				t.Fatalf("Generate() error: %v", err)
			}
			schema := tscSchemaText(t, root)
			if !strings.Contains(schema, "scalar IPAddr") {
				t.Fatalf("the consumer scalar is not in use — the fixture proves nothing:\n%s", schema)
			}
			_, onDisk := tscGraphFiles(t, root)["scalars_gen.go"]
			listed := slices.ContainsFunc(res.Files, func(f string) bool { return strings.HasSuffix(filepath.ToSlash(f), "graph/scalars_gen.go") })
			if onDisk != tt.wantFile || listed != tt.wantFile {
				t.Errorf("scalars_gen.go on disk = %v, listed = %v, want %v", onDisk, listed, tt.wantFile)
			}
		})
	}
}

// TestGraphQLSchema_noPKTableHasNoSurface pins the api-generation no-pk
// column: a table with no primary key is skipped from generation (§9.4b), so
// it emits no GraphQL type, query or mutation. A keyed sibling is the
// control.
func TestGraphQLSchema_noPKTableHasNoSurface(t *testing.T) {
	root := tscMustGen(t, config.DialectPostgres, `
CREATE TABLE audit_entries (message text NOT NULL, logged_at timestamptz NOT NULL);
CREATE TABLE notes (id bigserial PRIMARY KEY, body text NOT NULL);`, tscConfig(config.DialectPostgres, "camel_case", "", ""))
	schema := tscSchemaText(t, root)
	if !strings.Contains(schema, "type Note {") {
		t.Fatalf("the keyed control table has no object type:\n%s", schema)
	}
	if strings.Contains(schema, "AuditEntr") {
		t.Errorf("schema names the no-PK table:\n%s", schema)
	}
	for _, rootType := range []string{"Query", "Mutation"} {
		for _, name := range tscRootFieldNames(schema, rootType) {
			if strings.Contains(strings.ToLower(name), "auditentr") {
				t.Errorf("%s carries %q for the no-PK table", rootType, name)
			}
		}
	}
	for name := range tscGraphFiles(t, root) {
		if strings.Contains(name, "audit") {
			t.Errorf("graph package has a file %s for the no-PK table", name)
		}
	}
}
