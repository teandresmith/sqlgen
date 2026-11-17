package gen_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// compositeKeyAPIContext builds the API context for compositeLoadDDL: a
// composite tenant O2M (cycles → tasks), a composite O2O (task_reports →
// tasks), a tenant junction (tasks ↔ labels) and a composite-PK parent
// (order_items → item_notes).
func compositeKeyAPIContext(t *testing.T) *gen.APIContext {
	t.Helper()
	schema := parseDDLForTest(t, config.DialectPostgres, compositeLoadDDL)
	parser.DetectRelationships(schema)
	in := apiTestInput(t, schema)
	tables, views, err := gen.BuildEntityContextsFromSchema(in.Schema, in.Config)
	if err != nil {
		t.Fatalf("BuildEntityContextsFromSchema: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, views, nil, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}
	if apiCtx == nil {
		t.Fatal("BuildAPIContext returned nil")
	}
	return apiCtx
}

// A composite-key edge is a field on its object type in the shape its
// one-column form has, and a list edge is a member of the parent's filter.
func TestGraphQLSchema_CompositeKeyRelationshipFields(t *testing.T) {
	apiCtx := compositeKeyAPIContext(t)
	for _, tt := range []struct {
		name, entity, block, want string
	}{
		{"composite o2m", "Cycle", "type Cycle", "tasks: [Task!]!"},
		{"composite o2o", "TaskReport", "type TaskReport", "tasks: Task"},
		{"composite m2m", "Task", "type Task", "labels: [Label!]!"},
		{"composite-PK parent", "OrderItem", "type OrderItem", "itemNotes: [ItemNote!]!"},
		{"composite o2m filter", "Cycle", "input CycleFilter", "tasks: TaskFilter"},
		{"composite m2m filter", "Task", "input TaskFilter", "labels: LabelFilter"},
		{"composite-PK parent filter", "OrderItem", "input OrderItemFilter", "itemNotes: ItemNoteFilter"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out := renderAPITableSchema(t, apiTableByName(t, apiCtx, tt.entity))
			block := graphQLBlock(t, out, tt.block+" {")
			if !strings.Contains(block, tt.want) {
				t.Errorf("%s is missing %q:\n%s", tt.block, tt.want, block)
			}
		})
	}
}

// A composite-key edge's filter member dispatches into the target's
// translator, as a one-column member does.
func TestGraphQLFilterTranslation_RelationshipMember(t *testing.T) {
	out := renderFilterTranslate(t, compositeKeyAPIContext(t))
	for _, tt := range []struct {
		name, translator, want string
	}{
		{"composite foreign key", "translateCycleFilter", "out.Tasks = translateTaskFilter(in.Tasks)"},
		{"composite junction", "translateTaskFilter", "out.Labels = translateLabelFilter(in.Labels)"},
		{"composite-PK parent", "translateOrderItemFilter", "out.ItemNotes = translateItemNoteFilter(in.ItemNotes)"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, body, found := strings.Cut(out, "func "+tt.translator+"(")
			if !found {
				t.Fatalf("%s not emitted:\n%s", tt.translator, out)
			}
			if end := strings.Index(body, "\nfunc "); end >= 0 {
				body = body[:end]
			}
			if !strings.Contains(body, tt.want) {
				t.Errorf("%s is missing %q:\n%s", tt.translator, tt.want, body)
			}
		})
	}
}
