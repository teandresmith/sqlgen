package gen_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// junctionAPISchema fixture covers the three PK shapes the create-input rule
// must separate (PRD §26.4):
//
//   - task_assignees — composite PK (caller strategy) plus a non-PK column.
//   - task_labels    — composite PK and nothing else (a pure link table).
//   - labels         — single surrogate UUID PK, no default → app strategy,
//     i.e. server-minted and excluded from the create input.
func junctionAPISchema() *parser.Schema {
	return &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "task_assignees",
				Columns: []parser.Column{
					{Name: "task_id", Type: "uuid", PrimaryKey: true},
					{Name: "user_id", Type: "uuid", PrimaryKey: true},
					{Name: "assigned_at", Type: "timestamp"},
				},
			},
			{
				Name: "task_labels",
				Columns: []parser.Column{
					{Name: "task_id", Type: "uuid", PrimaryKey: true},
					{Name: "label_id", Type: "uuid", PrimaryKey: true},
				},
			},
			{
				Name: "labels",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "name", Type: "text"},
				},
			},
		},
	}
}

func buildJunctionAPITables(t *testing.T) map[string]gen.APITableContext {
	t.Helper()
	in := apiTestInput(t, junctionAPISchema())

	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}
	byName := make(map[string]gen.APITableContext, len(apiCtx.Tables))
	for _, tc := range apiCtx.Tables {
		byName[tc.SQLTable] = tc
	}
	for _, want := range []string{"task_assignees", "task_labels", "labels"} {
		if _, ok := byName[want]; !ok {
			t.Fatalf("table %q missing from API context", want)
		}
	}
	return byName
}

// TestAPIFieldInCreateInput_ByPKStrategy pins the per-field flag the schema
// template, the input translator, and HasCreateInput all read. A PK column
// belongs in the create input only when the caller supplies its value.
func TestAPIFieldInCreateInput_ByPKStrategy(t *testing.T) {
	tables := buildJunctionAPITables(t)

	tests := []struct {
		table string
		field string
		want  bool
	}{
		// Composite PK ⇒ caller strategy ⇒ the FK tuple is the input.
		{"task_assignees", "task_id", true},
		{"task_assignees", "user_id", true},
		{"task_assignees", "assigned_at", true},
		{"task_labels", "task_id", true},
		{"task_labels", "label_id", true},
		// Single UUID PK with no default ⇒ app strategy ⇒ server-minted, so
		// it stays out of the public create input.
		{"labels", "id", false},
		{"labels", "name", true},
	}

	for _, tt := range tests {
		t.Run(tt.table+"."+tt.field, func(t *testing.T) {
			var found bool
			for _, f := range tables[tt.table].Fields {
				if f.SQLName != tt.field {
					continue
				}
				found = true
				if f.InCreateInput != tt.want {
					t.Errorf("InCreateInput = %v, want %v", f.InCreateInput, tt.want)
				}
			}
			if !found {
				t.Fatalf("field %q not found on %q", tt.field, tt.table)
			}
		})
	}
}

// TestHasCreateInput_AllPKJunction pins that an all-PK junction has a create
// input (its key tuple) while its update input stays empty and suppressed — no
// GraphQL input type is ever emitted empty.
func TestHasCreateInput_AllPKJunction(t *testing.T) {
	tables := buildJunctionAPITables(t)

	tests := []struct {
		table           string
		wantCreateInput bool
		wantUpdateInput bool
	}{
		{"task_assignees", true, true},
		// All columns are PK: create carries the key tuple, update has
		// nothing to set and must keep emitting nothing (gqlgen rejects an
		// empty input block).
		{"task_labels", true, false},
		{"labels", true, true},
	}

	for _, tt := range tests {
		t.Run(tt.table, func(t *testing.T) {
			tc := tables[tt.table]
			if tc.HasCreateInput != tt.wantCreateInput {
				t.Errorf("HasCreateInput = %v, want %v", tc.HasCreateInput, tt.wantCreateInput)
			}
			if tc.HasUpdateInput != tt.wantUpdateInput {
				t.Errorf("HasUpdateInput = %v, want %v", tc.HasUpdateInput, tt.wantUpdateInput)
			}
		})
	}
}

// TestCreateInputTranslator_KeepsCallerPKs pins the third gate: the
// translator must copy caller PKs through, or the junction row is written
// with zero-valued keys. Update-side input fields never carry PKs.
func TestCreateInputTranslator_KeepsCallerPKs(t *testing.T) {
	tables := buildJunctionAPITables(t)

	got := make([]string, 0, 3)
	for _, f := range tables["task_assignees"].CreateInputFields {
		got = append(got, f.ModelFieldName)
	}
	want := "TaskID,UserID,AssignedAt"
	if strings.Join(got, ",") != want {
		t.Errorf("CreateInputFields = %v, want %s", got, want)
	}

	for _, f := range tables["task_assignees"].UpdateInputFields {
		if f.ModelFieldName == "TaskID" || f.ModelFieldName == "UserID" {
			t.Errorf("UpdateInputFields must not carry PK column %q", f.ModelFieldName)
		}
	}

	if n := len(tables["labels"].CreateInputFields); n != 1 {
		t.Errorf("labels CreateInputFields = %d fields, want 1 (app-strategy PK excluded)", n)
	}
}

// TestGraphQLSchema_CompositePKCreateInput renders the schema and pins the
// emitted shape end to end.
func TestGraphQLSchema_CompositePKCreateInput(t *testing.T) {
	tables := buildJunctionAPITables(t)

	// The PK columns are `uuid`, which binds to the UUID scalar rather than
	// the spec ID: the registry keys on the resolved Go type, and a `uuid`
	// column resolves to `uuid.UUID` with no configuration (PRD §7.2, §26.4.1).
	// Only a PK that resolves to a Go `string` reaches ID.
	t.Run("junction with a payload column", func(t *testing.T) {
		out := renderAPITableSchema(t, tables["task_assignees"])
		mustContainAll(
			t, out,
			"input CreateTaskAssigneeInput {",
			"taskID: UUID!",
			"userID: UUID!",
			"assignedAt: Time!",
			"createTaskAssignee(input: CreateTaskAssigneeInput!): TaskAssignee!",
			"upsertTaskAssignee(input: CreateTaskAssigneeInput!): TaskAssignee!",
		)
		// The update input keeps dropping PKs — they are addressed through
		// the mutation's PK args.
		updateBlock := inputBlock(t, out, "UpdateTaskAssigneeInput")
		mustNotContain(t, updateBlock, "taskID", "userID")
	})

	t.Run("all-PK junction", func(t *testing.T) {
		out := renderAPITableSchema(t, tables["task_labels"])
		mustContainAll(
			t, out,
			"input CreateTaskLabelInput {",
			"taskID: UUID!",
			"labelID: UUID!",
			"createTaskLabel(input: CreateTaskLabelInput!): TaskLabel!",
			"upsertTaskLabel(input: CreateTaskLabelInput!): TaskLabel!",
		)
		// No update surface: nothing to set.
		mustNotContain(
			t, out,
			"input UpdateTaskLabelInput",
			"updateTaskLabel(",
		)
	})

	t.Run("single surrogate PK is unchanged", func(t *testing.T) {
		out := renderAPITableSchema(t, tables["labels"])
		mustContain(t, out, "input CreateLabelInput {")
		createBlock := inputBlock(t, out, "CreateLabelInput")
		mustNotContain(t, createBlock, "id:")
		mustContain(t, createBlock, "name: String!")
	})
}

// inputBlock returns the body of `input <name> { ... }` from a rendered
// schema so assertions can scope to one block instead of the whole file.
func inputBlock(t *testing.T, out, name string) string {
	t.Helper()
	start := strings.Index(out, "input "+name+" {")
	if start < 0 {
		t.Fatalf("input %s not found in:\n%s", name, out)
	}
	rest := out[start:]
	end := strings.Index(rest, "\n}")
	if end < 0 {
		t.Fatalf("unterminated input %s in:\n%s", name, out)
	}
	return rest[:end]
}
