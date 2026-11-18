package gen_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
	parserpkg "github.com/teandresmith/sqlgen/parser"
)

// PRD §26.12 Known Limitations whose promise is about what the generator
// emits: no aggregate relationship predicates, and the Go-side filter member a
// column keeps when its GraphQL filter projection is dropped.

// twrInputBlocks splits a rendered GraphQL document into its `input` blocks,
// keyed by type name, each holding the lines between the braces.
func twrInputBlocks(doc string) map[string][]string {
	blocks := make(map[string][]string)
	var name string
	for line := range strings.SplitSeq(doc, "\n") {
		switch {
		case strings.HasPrefix(line, "input ") && strings.HasSuffix(strings.TrimSpace(line), "{"):
			name = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(line), "input "), "{"))
			blocks[name] = nil
		case name != "" && strings.HasPrefix(line, "}"):
			name = ""
		case name != "":
			blocks[name] = append(blocks[name], line)
		}
	}
	return blocks
}

// twrFieldDecl matches an input field declaration `  name: Type`, ignoring
// description strings and comments.
var twrFieldDecl = regexp.MustCompile(`^\s+([A-Za-z_][A-Za-z0-9_]*)\s*:\s*(\S+)`)

// TestAPIRelationshipFilter_NoAggregatePredicates pins the §26.12 limitation
// "Aggregate relationship predicates (`assignees: { count: { gt: 3 } }`) are
// not generated". A list relationship's filter member is the target's own
// `<Target>Filter` — a row predicate compiled to EXISTS — and no input type
// in the generated schema carries a `count` member or an aggregate operand.
func TestAPIRelationshipFilter_NoAggregatePredicates(t *testing.T) {
	apiCtx := relFilterAPIContext(t, nil)

	var doc strings.Builder
	doc.WriteString(renderAPISharedSchema(t, apiCtx))
	for _, tbl := range apiCtx.Tables {
		doc.WriteString("\n")
		doc.WriteString(renderAPITableSchema(t, tbl))
	}
	blocks := twrInputBlocks(doc.String())

	// The fixture's list relationships must be projected, or the absence
	// checks below say nothing about relationship filters.
	userFilter, ok := blocks["UserFilter"]
	if !ok {
		t.Fatalf("no input UserFilter in the generated schema:\n%s", doc.String())
	}
	members := make(map[string]string)
	for _, line := range userFilter {
		if m := twrFieldDecl.FindStringSubmatch(line); m != nil {
			members[m[1]] = m[2]
		}
	}
	for member, want := range map[string]string{
		"orders":     "OrderFilter",
		"notes":      "NoteFilter",
		"categories": "CategoryFilter",
	} {
		if got := members[member]; got != want {
			t.Errorf("UserFilter.%s is %q, want the target's row filter %q", member, got, want)
		}
	}

	for name, lines := range blocks {
		if strings.Contains(name, "Count") || strings.Contains(name, "Aggregate") {
			t.Errorf("schema declares input %s, an aggregate predicate type", name)
		}
		for _, line := range lines {
			m := twrFieldDecl.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			if m[1] == "count" || m[1] == "_count" {
				t.Errorf("input %s declares an aggregate member %q", name, strings.TrimSpace(line))
			}
		}
	}
}

// TestFilterProjection_DroppedColumnKeepsGoFilterMember pins the §26.12
// sentence "The column keeps … its member on the Go `<T>Filter` struct: only
// the GraphQL filter face is dropped", for both shapes that reach it. Each
// case first confirms the GraphQL projection really is dropped, so the
// Go-side assertion is about a no-projection column.
func TestFilterProjection_DroppedColumnKeepsGoFilterMember(t *testing.T) {
	tests := []struct {
		name       string
		column     parserpkg.Column
		tweak      func(cfg *config.RootConfig)
		wantMember string
	}{
		{
			name:       "array column whose element binds to a custom scalar",
			column:     parserpkg.Column{Name: "seen_at", Type: "timestamptz[]"},
			wantMember: "SeenAt",
		},
		{
			name:   "unqualified PascalCase column type that is not a schema enum",
			column: parserpkg.Column{Name: "shipping_address", Type: "text"},
			tweak: func(cfg *config.RootConfig) {
				cfg.Tables["probes"] = config.TableConfig{
					ColumnMap: map[string]config.ColumnOverride{
						"shipping_address": {Type: "Address"},
					},
				}
				cfg.API.GraphQL.Scalars = map[string]config.ScalarBinding{
					"Address": {GoType: "Address", Marshaling: config.ScalarMarshalingMethod},
				}
			},
			wantMember: "ShippingAddress",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := apiTestInput(t, probeSchema(tt.column))
			in.Config.Output.Package = "models"
			if tt.tweak != nil {
				tt.tweak(in.Config)
			}
			in.Resolver = gotype.NewResolver(config.DialectPostgres, true, in.Config.Overrides.Types)

			tables, err := gen.BuildTableContexts(in, nil)
			if err != nil {
				t.Fatalf("BuildTableContexts: %v", err)
			}
			apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
			if err != nil {
				t.Fatalf("BuildAPIContext: %v", err)
			}

			field := fieldBySQLName(t, apiTableByName(t, apiCtx, "Probe"), tt.column.Name)
			if field.Filterable || field.Filter.InputTypeName != "" || field.Filter.TranslatorFunc != "" {
				t.Fatalf("column still has a GraphQL filter projection (filterable=%v input=%q translator=%q); the fixture no longer reaches the no-projection shape",
					field.Filterable, field.Filter.InputTypeName, field.Filter.TranslatorFunc)
			}

			var probe *gen.TableContext
			for i := range tables {
				if tables[i].StructName == "Probe" {
					probe = &tables[i]
				}
			}
			if probe == nil {
				t.Fatal("no Probe table context")
			}
			var member *gen.FilterFieldContext
			for i := range probe.FilterFields {
				if probe.FilterFields[i].ColumnName == tt.column.Name {
					member = &probe.FilterFields[i]
				}
			}
			if member == nil {
				t.Fatalf("Go ProbeFilter has no filter field for %s", tt.column.Name)
			}
			if !member.Filterable || member.ComparatorType == "" {
				t.Errorf("Go ProbeFilter field for %s: filterable=%v comparator=%q, want a filterable member",
					tt.column.Name, member.Filterable, member.ComparatorType)
			}

			out := executeFilterTemplate(t, *probe)
			block := graphQLBlock(t, out, "type ProbeFilter struct {")
			want := tt.wantMember + " " + member.ComparatorType
			if !strings.Contains(block, want) {
				t.Errorf("Go ProbeFilter struct is missing %q:\n%s", want, block)
			}
		})
	}
}
