package tests

// TestWalkerCompleteness pins the PRD §26.5.2 walker-completeness contract
// at the artifact level: every column AND relationship field declared on a
// `type <Table> { ... }` block in a sqlgen-emitted `*_gen.graphqls` file
// MUST have a corresponding `case "<field>"` clause in the per-table
// walker emitted into `models/graph/sqlgenresolver/field_options_gen.go`.
//
// The §26.5.2 walker is the load-bearing piece behind the §25.1 query-
// count contract — a missing case would silently produce a nil
// FieldOptions entry, the resolver would not pre-load the relationship,
// and a deeply-nested query would issue more queries than the contract
// promises. The codegen-time lint (`gen.ValidateAPIWalkerCompleteness`)
// is the fail-fast guard against that class of bug; this test pins the
// same invariant at the artifact level — verifying that every CHECKED-IN
// walker matches the CHECKED-IN per-table schema field-for-field.
//
// **Why there is no broken-fixture E2E test:** the natural shape for
// this check is a deliberately broken fixture (e.g., a column added to
// the schema without regenerating the walker) that asserts codegen
// returns the §26.5.2-defined error message naming the missing column.
// The lint-firing path is exercised at unit level in
// `cmd/sqlgen/gen/api_walker_test.go::TestWalker_CompletenessLint_NegativeTest`,
// which constructs in-memory contexts with a deliberately-missing column
// case and asserts the exact §26.5.2-defined error message. Driving the
// same path via a live `sqlgen generate` subprocess is not reachable —
// `buildAPITableContext` deterministically derives the walker from the
// same `TableContext.Columns` slice the lint checks against, so there is
// no fixture shape that triggers the divergence without modifying the
// codegen pipeline itself. The E2E-equivalent guard is artifact-level
// verification (this file): the per-table schema and walker MUST agree
// field-for-field; if they ever drift, this test fails. The unit test
// owns "lint fires with the correct message"; this test owns "the lint's
// invariant holds in the shipped artifact". The error-message format
// itself is pinned in TestWalkerCompletenessLint_ErrorMessageFormat
// below by reading the lint's source file directly.

import (
	"maps"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/gobuffalo/flect"
)

func TestWalkerCompleteness_AllFieldsCovered(t *testing.T) {
	exampleRoot := filepath.Dir(thisFileDir(t))
	graphDir := filepath.Join(exampleRoot, "models", "graph")

	schema := parseGraphQLSchemaTypes(t, graphDir)
	walker := parseWalkerCases(t, string(readFile(t, filepath.Join(graphDir, "sqlgenresolver", "field_options_gen.go"))))

	// Verify the *_gen.graphqls files and the walker agree on which
	// per-table types are managed. Drift here means either a schema type
	// got added without its walker pair (the lint catches this at codegen
	// time) or the walker has a function for a type that no schema
	// declares (a sqlgen bug — also caught by the existing unit lint).
	schemaTypes := sortedKeys(schema)
	walkerTypes := sortedKeys(walker)
	if strings.Join(schemaTypes, ",") != strings.Join(walkerTypes, ",") {
		t.Fatalf("schema types (%v) and walker types (%v) diverge", schemaTypes, walkerTypes)
	}

	for typeName, fields := range schema {
		t.Run(typeName, func(t *testing.T) {
			cases, ok := walker[typeName]
			if !ok {
				t.Fatalf("walker has no function for GraphQL type %q (expected `%sFieldOptionsFromCollected`)", typeName, flect.Camelize(typeName))
			}
			caseSet := make(map[string]bool, len(cases))
			for _, c := range cases {
				caseSet[c] = true
			}
			for _, field := range fields {
				if !caseSet[field] {
					t.Errorf("walker for type %q has no case for field %q (declared in %s_gen.graphqls); see PRD §26.5.2",
						typeName, field, flect.Underscore(typeName))
				}
			}
		})
	}
}

// TestWalkerCompletenessLint_ErrorMessageFormat pins the §26.5.2 error-
// message format the lint emits when it fires. The codegen-time lint is
// the runtime expression of the §25.1 query-count contract — when a
// future refactor changes the message format (or worse, removes the
// lint), this regression alerts before consumer-facing breakage.
//
// The lint message format pinned here is the one
// `cmd/sqlgen/gen/api_walker.go::ValidateAPIWalkerCompleteness` produces.
// The unit test in `cmd/sqlgen/gen/api_walker_test.go` exercises the
// firing path; this test re-pins the user-visible string so accidental
// message reshapes surface in the example's verification matrix too.
//
// The entity noun is a `%s` and not the literal "table": the lint runs
// over views on the same code path (PRD §26.4 "Views on the GraphQL
// surface"), and a message that called a view a table would send the
// consumer looking for a `tables:` key that cannot rename it. The
// negative assertion below is what keeps a future refactor from quietly
// hard-coding the noun back in.
func TestWalkerCompletenessLint_ErrorMessageFormat(t *testing.T) {
	walkerFile := filepath.Join(repoRoot(t), "cmd", "sqlgen", "gen", "api_walker.go")
	src := string(readFile(t, walkerFile))

	mustContain := []string{
		`api: walker for %s %q has no case for column %q`,
		`api: walker for %s %q has a case for access-restricted column %q`,
		`api: walker for %s %q has no case for relationship %q`,
		// The row-identity block the walker ends with is projected outside
		// the switch, so it carries its own messages.
		`api: walker for %s %q has API-readable columns but projects no row identity`,
		`api: walker for %s %q projects row-identity field %q, which names no column of the entity`,
		`api: walker for %s %q projects access-restricted column %q as its row identity`,
		`PRD §26.5.2`,
	}
	for _, want := range mustContain {
		if !strings.Contains(src, want) {
			t.Errorf("walker-completeness lint message format drifted: missing %q in %s", want, walkerFile)
		}
	}
	if strings.Contains(src, `api: walker for table %q`) {
		t.Errorf("walker-completeness lint hard-codes the entity noun again in %s — a view linted by this path would be reported as a table (PRD §26.4)", walkerFile)
	}
}

// --- helpers -------------------------------------------------------------

// parseGraphQLSchemaTypes walks every `*_gen.graphqls` file under graphDir
// and parses out the field set declared inside each top-level `type
// <Name> { ... }` block — both leaf columns (`id: UUID!`, `name: String!`)
// and relationships (`orderItems: [OrderItem!]!`, `company: Company`).
// Envelope types (`<T>Connection`, `<T>Edge`, `<T>ListResult`) are
// excluded — the walker only handles the entity types, the envelopes are
// unwrapped by the runtime helper before the walker is invoked.
//
// Returns a map of entity type name → declared field names (in source
// order).
func parseGraphQLSchemaTypes(t *testing.T, graphDir string) map[string][]string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(graphDir, "*_gen.graphqls"))
	if err != nil {
		t.Fatalf("globbing graphqls: %v", err)
	}
	out := make(map[string][]string)
	for _, path := range matches {
		if strings.HasSuffix(filepath.Base(path), "shared_gen.graphqls") {
			// shared_gen.graphqls declares scalar types only — no entity
			// blocks. Skip explicitly so a future shared schema with an
			// accidental `type Foo {}` block surfaces here instead of
			// silently lining up with a non-existent walker.
			continue
		}
		maps.Copy(out, parseEntityTypes(string(readFile(t, path))))
	}
	return out
}

// parseEntityTypes parses one .graphqls file body, returning the field
// list for every entity type (excludes connections / edges / list
// results / inputs / enums / queries / mutations / extensions).
func parseEntityTypes(src string) map[string][]string {
	out := make(map[string][]string)
	typeRe := regexp.MustCompile(`type\s+(\w+)\s*\{`)
	matches := typeRe.FindAllStringSubmatchIndex(src, -1)
	for i, m := range matches {
		name := src[m[2]:m[3]]
		// Skip envelope + extension types — only entity-shaped types get
		// walker functions.
		if isEnvelopeOrExtensionType(name) {
			continue
		}
		open := m[1]
		// Find the matching closing brace for the type block — naive
		// brace counting is enough because the schema files don't
		// nest braces inside type blocks.
		end := findClosingBrace(src, open-1)
		if end < 0 {
			continue
		}
		body := src[open:end]
		out[name] = parseFieldNames(body)
		_ = i
	}
	return out
}

// isEnvelopeOrExtensionType returns true for type names sqlgen emits
// alongside the entity type — Connection / Edge / ListResult envelopes,
// plus the `Query` / `Mutation` extension targets. None of these have
// walker functions.
func isEnvelopeOrExtensionType(name string) bool {
	switch {
	case strings.HasSuffix(name, "Connection"),
		strings.HasSuffix(name, "Edge"),
		strings.HasSuffix(name, "ListResult"),
		name == "Query",
		name == "Mutation",
		name == "PageInfo":
		return true
	}
	return false
}

// findClosingBrace returns the index of the matching `}` for the `{`
// at `openIdx`. Returns -1 if the brace is unbalanced.
func findClosingBrace(src string, openIdx int) int {
	depth := 0
	for i := openIdx; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// parseFieldNames extracts the field-name prefix of every `name: Type`
// line in a type block body. Comments (`""" ... """` or `"..."` doc
// strings) are skipped.
func parseFieldNames(body string) []string {
	var out []string
	fieldRe := regexp.MustCompile(`(?m)^\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*(?:\([^)]*\))?\s*:`)
	for _, m := range fieldRe.FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	return out
}

// parseWalkerCases parses the generated `field_options_gen.go` file and
// returns a map of GraphQL type name → list of field names that appear
// as `case "<name>":` clauses inside the per-type
// `<lowerType>FieldOptionsFromCollected` function. Function-name prefix
// → type name conversion title-cases the first byte (e.g.
// `productFieldOptionsFromCollected` → "Product").
func parseWalkerCases(t *testing.T, src string) map[string][]string {
	t.Helper()
	fnRe := regexp.MustCompile(`func (\w+)FieldOptionsFromCollected\(`)
	caseRe := regexp.MustCompile(`case "([a-zA-Z][a-zA-Z0-9_]*)":`)

	out := make(map[string][]string)
	matches := fnRe.FindAllStringSubmatchIndex(src, -1)
	for i, m := range matches {
		fnName := src[m[2]:m[3]]
		bodyStart := m[1]
		bodyEnd := len(src)
		if i+1 < len(matches) {
			bodyEnd = matches[i+1][0]
		}
		body := src[bodyStart:bodyEnd]
		caseMatches := caseRe.FindAllStringSubmatch(body, -1)
		cases := make([]string, 0, len(caseMatches))
		for _, c := range caseMatches {
			cases = append(cases, c[1])
		}
		typeName := flect.Pascalize(fnName)
		out[typeName] = cases
	}
	return out
}

// sortedKeys returns the keys of a string-keyed map in lexicographic
// order. Used to produce stable failure messages.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// thisFileDir returns the absolute path to the directory containing this
// test file. Used to anchor the schema + walker artifact paths against
// the example's source tree, independent of the test runner's cwd.
func thisFileDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("thisFileDir: runtime.Caller(0) failed")
	}
	return filepath.Dir(file)
}
