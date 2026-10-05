package gen_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// TestMultiSchema_TypeNamesAndQueryFields confirms that two same-named tables
// living in different schemas (audit.users + public.users) coexist in the
// generated GraphQL schema with separate, disambiguated type names and
// matching query fields. Multi-schema disambiguation is delegated to
// `gen.StructName`, which reuses the same prefix logic Go struct names use —
// PRD §26.4 explicitly directs the GraphQL prefix to match the Go
// prefix.
//
// Divergence from the original acceptance text: it said
// `public.users` should surface as `User` (no prefix). The actual landed
// behavior of `StructName` prefixes BOTH colliding tables with their schema
// (matching the postgres example's expected output: `AuditUser` AND
// `PublicUser`). The "GraphQL prefix matches the Go prefix" directive is the
// load-bearing constraint, and the existing `cmd/sqlgen/testdata/examples/
// postgres/expected/tablenames_gen.go` already pins `TableAuditUsers` and
// `TablePublicUsers` together. This test asserts the actual behavior to
// avoid drifting from the established convention.
func TestMultiSchema_TypeNamesAndQueryFields(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name:   "users",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true, Nullable: false},
					{Name: "email", Type: "text", Nullable: false},
				},
			},
			{
				Name:   "users",
				Schema: "audit",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true, Nullable: false},
					{Name: "action", Type: "text", Nullable: false},
				},
			},
		},
	}
	in := apiTestInput(t, schema)

	// `users` collides across the audit + public schemas. `BuildTableContexts`
	// itself has no view of the full schema's collision map (the orchestrator
	// computes it via the unexported `computeNameCollisions`), so tests that
	// exercise multi-schema disambiguation construct it explicitly.
	collisions := map[string]bool{"users": true}
	tables, err := gen.BuildTableContexts(in, collisions)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}
	if apiCtx == nil {
		t.Fatal("BuildAPIContext returned nil")
	}
	if got := len(apiCtx.Tables); got != 2 {
		t.Fatalf("Tables: got %d, want 2", got)
	}

	// Render both tables and concatenate so a single assertion stream covers
	// the generated `.graphqls` output for the schema as a whole.
	var sb strings.Builder
	for _, tc := range apiCtx.Tables {
		sb.WriteString(renderAPITableSchema(t, tc))
		sb.WriteString("\n")
	}
	out := sb.String()

	// Both disambiguated types appear, and each carries its own Connection /
	// Edge / ListResult / Filter / Sort / Create / Update inputs.
	for _, want := range []string{
		"type AuditUser {",
		"type PublicUser {",
		"type AuditUserConnection {",
		"type PublicUserConnection {",
		"type AuditUserEdge {",
		"type PublicUserEdge {",
		"type AuditUserListResult {",
		"type PublicUserListResult {",
		"input AuditUserFilter {",
		"input PublicUserFilter {",
		"input CreateAuditUserInput {",
		"input CreatePublicUserInput {",
		"input UpdateAuditUserInput {",
		"input UpdatePublicUserInput {",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n--- output ---\n%s", want, out)
		}
	}

	// Per-schema query / mutation fields are camelCased from the disambiguated
	// struct name: `AuditUser` → `auditUser` / `auditUsers` / `auditUserList`,
	// `PublicUser` → `publicUser` / `publicUsers` / `publicUserList`. The
	// PRD §26.4 schema example shows `audit.users` mapping to the `auditUser`
	// query field — same camelCase rule applied to the disambiguated type
	// name produces the correct GraphQL surface. The PK arg is `UUID!` rather
	// than the spec `ID!` because both fixtures key on a `uuid` column, which
	// resolves to `uuid.UUID` with no configuration and so binds the registry
	// UUID scalar (PRD §7.2, §26.4.1); the arg type is independent of the
	// disambiguation this test is about.
	for _, want := range []string{
		"auditUser(id: UUID!): AuditUser",
		"publicUser(id: UUID!): PublicUser",
		"auditUsers(filter: AuditUserFilter",
		"publicUsers(filter: PublicUserFilter",
		"auditUserList(",
		"publicUserList(",
		"createAuditUser(input: CreateAuditUserInput!): AuditUser!",
		"createPublicUser(input: CreatePublicUserInput!): PublicUser!",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n--- output ---\n%s", want, out)
		}
	}
}
