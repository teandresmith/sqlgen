package config_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// TestLoadConfig_RelationshipKeyForms pins PRD §4.8 / §13.4: `fk`,
// `junction_local_fk` and `junction_reference_fk` take a string or a list,
// and a string is a one-item list.
func TestLoadConfig_RelationshipKeyForms(t *testing.T) {
	tests := []struct {
		name    string
		rel     string
		want    config.TableRelationship
		wantErr string
	}{
		{
			name: "string",
			rel:  "name: Tasks\n        type: o2m\n        table: tasks\n        fk: cycle_id",
			want: config.TableRelationship{Name: "Tasks", Type: "o2m", Table: "tasks", FK: config.KeyColumns{"cycle_id"}},
		},
		{
			name: "flow list",
			rel:  "name: Tasks\n        type: o2m\n        table: tasks\n        fk: [workspace_id, cycle_id]",
			want: config.TableRelationship{Name: "Tasks", Type: "o2m", Table: "tasks", FK: config.KeyColumns{"workspace_id", "cycle_id"}},
		},
		{
			name: "block list",
			rel:  "name: Labels\n        type: m2m\n        table: labels\n        junction: task_labels\n        junction_local_fk:\n          - workspace_id\n          - task_id\n        junction_reference_fk:\n          - workspace_id\n          - label_id",
			want: config.TableRelationship{
				Name: "Labels", Type: "m2m", Table: "labels", Junction: "task_labels",
				JunctionLocalFK:     config.KeyColumns{"workspace_id", "task_id"},
				JunctionReferenceFK: config.KeyColumns{"workspace_id", "label_id"},
			},
		},
		{
			name:    "map is rejected",
			rel:     "name: Tasks\n        type: o2m\n        table: tasks\n        fk:\n          workspace_id: workspace_id",
			wantErr: "cannot unmarshal !!map into []string",
		},
		{
			name: "empty list",
			rel:  "name: Tasks\n        type: o2m\n        table: tasks\n        fk: []",
			want: config.TableRelationship{Name: "Tasks", Type: "o2m", Table: "tasks", FK: config.KeyColumns{}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTestConfig(t, "version: \"v1\"\ninput:\n  dialect: postgres\n  paths: [\"./schema.sql\"]\noutput:\n  dir: ./models\n  package: models\ntables:\n  cycles:\n    relationships:\n      - "+tt.rel+"\n")
			cfg, err := config.LoadConfig(path)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("LoadConfig() error = %v, want one containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadConfig() error = %v", err)
			}
			if diff := cmp.Diff([]config.TableRelationship{tt.want}, cfg.Tables["cycles"].Relationships); diff != "" {
				t.Errorf("relationships mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// relationshipKeySchema is a composite-PK cycles table keyed (workspace_id, id),
// a single-PK labels table, and the tasks and task_labels tables that link them.
func relationshipKeySchema() []config.SchemaTable {
	pk := func(name string) config.SchemaColumn {
		return config.SchemaColumn{Name: name, Type: "bigint", PrimaryKey: true}
	}
	col := func(name string) config.SchemaColumn { return config.SchemaColumn{Name: name, Type: "bigint"} }
	return []config.SchemaTable{
		{Name: "cycles", Columns: []config.SchemaColumn{pk("workspace_id"), pk("id")}},
		{Name: "sprints", Columns: []config.SchemaColumn{pk("workspace_id"), pk("cycle_id"), pk("id")}},
		{Name: "labels", Columns: []config.SchemaColumn{pk("id")}},
		{Name: "tasks", Columns: []config.SchemaColumn{pk("id"), col("workspace_id"), col("cycle_id")}},
		{Name: "task_labels", Columns: []config.SchemaColumn{col("workspace_id"), col("cycle_id"), col("label_id")}},
	}
}

// TestValidatePostParse_RelationshipKeyLength pins the PRD §4.13 rule: a key
// list maps by position onto the primary key of the table it references, so
// its length must equal that key's column count. The error names the
// relationship, the key, the list and the primary key's columns.
func TestValidatePostParse_RelationshipKeyLength(t *testing.T) {
	tests := []struct {
		name  string
		table string
		rel   config.TableRelationship
		want  []string
	}{
		{
			name:  "fk list too short",
			table: "sprints",
			rel:   config.TableRelationship{Name: "Tasks", Type: "o2m", Table: "tasks", FK: config.KeyColumns{"workspace_id", "cycle_id"}},
			want:  []string{`tables.sprints.relationships[0] ("Tasks").fk`, "[workspace_id, cycle_id]", "has 2 columns", `primary key of "sprints"`, "3 columns (workspace_id, cycle_id, id)", "PRD §4.13"},
		},
		{
			name:  "has-one fk references this table",
			table: "cycles",
			rel:   config.TableRelationship{Name: "Task", Type: "o2o", Table: "tasks", FK: config.KeyColumns{"cycle_id"}},
			want:  []string{`("Task").fk`, "has 1 column", `primary key of "cycles"`},
		},
		{
			name: "junction_local_fk list too long",
			rel: config.TableRelationship{
				Name: "Labels", Type: "m2m", Table: "labels", Junction: "task_labels",
				JunctionLocalFK:     config.KeyColumns{"workspace_id", "cycle_id", "label_id"},
				JunctionReferenceFK: config.KeyColumns{"label_id"},
			},
			want: []string{`("Labels").junction_local_fk`, "[workspace_id, cycle_id, label_id]", "3 columns", `primary key of "cycles"`, "(workspace_id, id)"},
		},
		{
			name: "junction_reference_fk list too long",
			rel: config.TableRelationship{
				Name: "Labels", Type: "m2m", Table: "labels", Junction: "task_labels",
				JunctionLocalFK:     config.KeyColumns{"workspace_id", "cycle_id"},
				JunctionReferenceFK: config.KeyColumns{"workspace_id", "label_id"},
			},
			want: []string{`("Labels").junction_reference_fk`, "[workspace_id, label_id]", "has 2 columns", `primary key of "labels"`, "1 column (id)"},
		},
		{
			name: "string into a composite key",
			rel:  config.TableRelationship{Name: "Tasks", Type: "o2m", Table: "tasks", FK: config.KeyColumns{"cycle_id"}},
			want: []string{`("Tasks").fk`, "has 1 column", "2 columns (workspace_id, id)"},
		},
		{
			name: "empty list",
			rel:  config.TableRelationship{Name: "Tasks", Type: "o2m", Table: "tasks", FK: config.KeyColumns{}},
			want: []string{`("Tasks").fk`, "[]", "has 0 columns"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			table := tt.table
			if table == "" {
				table = "cycles"
			}
			cfg := validConfig()
			cfg.Tables[table] = config.TableConfig{Relationships: []config.TableRelationship{tt.rel}}
			_, err := config.ValidatePostParse(cfg, relationshipKeySchema(), nil)
			assertErrContains(t, err, "ValidatePostParse()", tt.want...)
		})
	}
}

// TestValidatePostParse_RelationshipKeyLengthMatches is the passing half: a
// list of the referenced key's length, on either side of an O2O, and a string
// into a single-column key.
func TestValidatePostParse_RelationshipKeyLengthMatches(t *testing.T) {
	tests := []struct {
		name  string
		table string
		rel   config.TableRelationship
	}{
		{
			name:  "o2m list onto a composite key",
			table: "cycles",
			rel:   config.TableRelationship{Name: "Tasks", Type: "o2m", Table: "tasks", FK: config.KeyColumns{"workspace_id", "cycle_id"}},
		},
		{
			name:  "belongs-to list onto the related key",
			table: "tasks",
			rel:   config.TableRelationship{Name: "Cycle", Type: "o2o", Table: "cycles", FK: config.KeyColumns{"workspace_id", "cycle_id"}},
		},
		{
			name:  "has-one list onto this table's key",
			table: "cycles",
			rel:   config.TableRelationship{Name: "Task", Type: "o2o", Table: "tasks", FK: config.KeyColumns{"workspace_id", "cycle_id"}},
		},
		{
			name:  "m2m lists onto both keys",
			table: "cycles",
			rel: config.TableRelationship{
				Name: "Labels", Type: "m2m", Table: "labels", Junction: "task_labels",
				JunctionLocalFK:     config.KeyColumns{"workspace_id", "cycle_id"},
				JunctionReferenceFK: config.KeyColumns{"label_id"},
			},
		},
		{
			name:  "string into a single-column key",
			table: "labels",
			rel:   config.TableRelationship{Name: "Tasks", Type: "o2m", Table: "tasks", FK: config.KeyColumns{"cycle_id"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.Tables[tt.table] = config.TableConfig{Relationships: []config.TableRelationship{tt.rel}}
			if _, err := config.ValidatePostParse(cfg, relationshipKeySchema(), nil); err != nil {
				t.Errorf("ValidatePostParse() = %v, want nil", err)
			}
		})
	}
}

// TestValidatePostParse_RelationshipKeyIntoView pins the PRD §4.13 rule for a
// key into a view: it maps onto the view's @pk, and a view without @pk has no
// key to map onto, so a belongs-to fk or an m2m junction_reference_fk into one
// is an error, set or inferred. An o2m or has-one fk on the view references
// this table and passes.
func TestValidatePostParse_RelationshipKeyIntoView(t *testing.T) {
	views := []config.SchemaView{
		{Name: "task_stats", Columns: []config.SchemaColumn{
			{Name: "stat_workspace_id", Type: "bigint"}, {Name: "stat_cycle_id", Type: "bigint"}, {Name: "total", Type: "bigint"},
		}},
		{Name: "label_summary", Columns: []config.SchemaColumn{
			{Name: "label_id", Type: "bigint", PrimaryKey: true}, {Name: "total", Type: "bigint"},
		}},
	}
	tests := []struct {
		name  string
		table string
		rel   config.TableRelationship
		want  []string // nil: no error
	}{
		{
			name:  "belongs-to fk",
			table: "tasks",
			rel:   config.TableRelationship{Name: "Stats", Type: "o2o", Table: "task_stats", FK: config.KeyColumns{"cycle_id"}},
			want:  []string{`tables.tasks.relationships[0] ("Stats").fk`, `references view "task_stats"`, "declares no @pk", "PRD §4.13"},
		},
		{
			name:  "m2m with an inferred junction_reference_fk",
			table: "labels",
			rel:   config.TableRelationship{Name: "Stats", Type: "m2m", Table: "task_stats", Junction: "task_labels", JunctionLocalFK: config.KeyColumns{"label_id"}},
			want:  []string{`("Stats").junction_reference_fk`, `references view "task_stats"`},
		},
		{
			name:  "belongs-to fk onto the view's @pk",
			table: "tasks",
			rel:   config.TableRelationship{Name: "Summary", Type: "o2o", Table: "label_summary", FK: config.KeyColumns{"cycle_id"}},
		},
		{
			name:  "m2m onto the view's @pk",
			table: "labels",
			rel:   config.TableRelationship{Name: "Summaries", Type: "m2m", Table: "label_summary", Junction: "task_labels", JunctionLocalFK: config.KeyColumns{"label_id"}, JunctionReferenceFK: config.KeyColumns{"label_id"}},
		},
		{
			name:  "belongs-to list longer than the view's @pk",
			table: "tasks",
			rel:   config.TableRelationship{Name: "Summary", Type: "o2o", Table: "label_summary", FK: config.KeyColumns{"workspace_id", "cycle_id"}},
			want:  []string{`("Summary").fk`, "has 2 columns", `primary key of "label_summary"`, "1 column (label_id)"},
		},
		{
			name:  "has-one fk on the view",
			table: "cycles",
			rel:   config.TableRelationship{Name: "Stats", Type: "o2o", Table: "task_stats", FK: config.KeyColumns{"stat_workspace_id", "stat_cycle_id"}},
		},
		{
			name:  "o2m fk on the view",
			table: "cycles",
			rel:   config.TableRelationship{Name: "Stats", Type: "o2m", Table: "task_stats", FK: config.KeyColumns{"stat_workspace_id", "stat_cycle_id"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.Tables[tt.table] = config.TableConfig{Relationships: []config.TableRelationship{tt.rel}}
			_, err := config.ValidatePostParse(cfg, relationshipKeySchema(), views)
			if tt.want == nil {
				if err != nil {
					t.Errorf("ValidatePostParse() = %v, want nil", err)
				}
				return
			}
			assertErrContains(t, err, "ValidatePostParse()", tt.want...)
		})
	}

	t.Run("has-one fk on the view of the wrong length", func(t *testing.T) {
		cfg := validConfig()
		cfg.Tables["cycles"] = config.TableConfig{Relationships: []config.TableRelationship{
			{Name: "Stats", Type: "o2o", Table: "task_stats", FK: config.KeyColumns{"stat_cycle_id"}},
		}}
		_, err := config.ValidatePostParse(cfg, relationshipKeySchema(), views)
		assertErrContains(t, err, "ValidatePostParse()", `("Stats").fk`, "has 1 column", `primary key of "cycles"`)
	})
}

// TestSchemaV1_RelationshipKeyForms pins PRD §4.14 for the three key fields: a
// string or a list of strings validates, a map does not.
func TestSchemaV1_RelationshipKeyForms(t *testing.T) {
	sch := compileSchema(t)
	tests := []struct {
		name  string
		yaml  string
		valid bool
	}{
		{"string", "tables:\n  cycles:\n    relationships:\n      - {name: Tasks, type: o2m, table: tasks, fk: cycle_id}\n", true},
		{"list", "tables:\n  cycles:\n    relationships:\n      - {name: Tasks, type: o2m, table: tasks, fk: [workspace_id, cycle_id]}\n      - {name: Labels, type: m2m, table: labels, junction: task_labels, junction_local_fk: [workspace_id, task_id], junction_reference_fk: [label_id]}\n", true},
		{"map is rejected", "tables:\n  cycles:\n    relationships:\n      - {name: Tasks, type: o2m, table: tasks, fk: {workspace_id: workspace_id}}\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sch.Validate(yamlToJSONValue(t, []byte(tt.yaml)))
			if tt.valid && err != nil {
				t.Errorf("Validate() = %v, want nil", err)
			}
			if !tt.valid && err == nil {
				t.Error("Validate() = nil, want an error")
			}
		})
	}
}

// TestValidateRelationships_EmptyKeyIsRequired pins PRD §4.8: an o2o or o2m
// `fk: []` names no column, so it fails the required rule as an absent one
// does, whatever table it references.
func TestValidateRelationships_EmptyKeyIsRequired(t *testing.T) {
	cfg := validConfig()
	cfg.Tables["cycles"] = config.TableConfig{Relationships: []config.TableRelationship{
		{Name: "View", Type: "o2o", Table: "cycle_view", FK: config.KeyColumns{}},
	}}
	_, err := config.ValidatePreParse(cfg)
	assertErrContains(t, err, "ValidatePreParse()", `("View").fk: required`)
}

// TestValidatePostParse_O2OKeyOnOneSide pins PRD §4.13: an o2o key's columns
// are all on this table or all on the related one.
func TestValidatePostParse_O2OKeyOnOneSide(t *testing.T) {
	cfg := validConfig()
	// cycle_id is on tasks only and code on cycles only, so the key is split
	// across the two tables.
	tables := relationshipKeySchema()
	tables[0].Columns = append(tables[0].Columns, config.SchemaColumn{Name: "code", Type: "text"})
	cfg.Tables["tasks"] = config.TableConfig{Relationships: []config.TableRelationship{
		{Name: "Cycle", Type: "o2o", Table: "cycles", FK: config.KeyColumns{"cycle_id", "code"}},
	}}
	_, err := config.ValidatePostParse(cfg, tables, nil)
	assertErrContains(t, err, "ValidatePostParse()", `("Cycle").fk: [cycle_id, code] has columns on both table "tasks" and related table "cycles"`)
}

// TestValidatePostParse_RelationshipKeyLengthNamesClauseOrder pins PRD §13.4:
// a key list maps onto the primary key in its PRIMARY KEY (...) order, so the
// error names the key in that order, not in column order.
func TestValidatePostParse_RelationshipKeyLengthNamesClauseOrder(t *testing.T) {
	tables := relationshipKeySchema()
	tables[0].PrimaryKey = []string{"id", "workspace_id"}
	cfg := validConfig()
	cfg.Tables["cycles"] = config.TableConfig{Relationships: []config.TableRelationship{
		{Name: "Tasks", Type: "o2m", Table: "tasks", FK: config.KeyColumns{"cycle_id"}},
	}}
	_, err := config.ValidatePostParse(cfg, tables, nil)
	assertErrContains(t, err, "ValidatePostParse()", "2 columns (id, workspace_id)")
}

// TestValidatePostParse_ResolvedPKFollowsClauseOrder pins PRD §27.5: without a
// primary_key.columns override, the resolved key follows its PRIMARY KEY (...)
// order, here through the composite-strategy error.
func TestValidatePostParse_ResolvedPKFollowsClauseOrder(t *testing.T) {
	tables := relationshipKeySchema()
	tables[0].PrimaryKey = []string{"id", "workspace_id"}
	cfg := validConfig()
	cfg.Tables["cycles"] = config.TableConfig{PrimaryKey: &config.TablePrimaryKeyConfig{Strategy: config.PKStrategyDB}}
	_, err := config.ValidatePostParse(cfg, tables, nil)
	assertErrContains(t, err, "ValidatePostParse()", "composite primary key (id, workspace_id)")
}
