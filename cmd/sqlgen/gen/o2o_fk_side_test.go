package gen_test

import (
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// fkSideContexts parses ddl in dialect, runs detection, declares rels on
// table and returns the table's context.
func fkSideContexts(t *testing.T, dialect config.Dialect, ddl, table string, rels ...config.TableRelationship) gen.TableContext {
	t.Helper()
	schema := parseDDLForTest(t, dialect, ddl)
	parser.DetectRelationships(schema)
	in := testInput(schema)
	in.Config.Input.Dialect = dialect
	if len(rels) > 0 {
		in.Config.Tables[table] = config.TableConfig{Relationships: rels}
	}
	contexts, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() error: %v", err)
	}
	tc, ok := tableContextByName(contexts, table)
	if !ok {
		t.Fatalf("no %s context", table)
	}
	return tc
}

// o2oJoin returns the top-level O2O join detail whose target is reached
// through field.
func o2oJoin(t *testing.T, tc gen.TableContext, field string) gen.O2OJoinDetail {
	t.Helper()
	i := slices.IndexFunc(tc.O2OJoinDetails, func(d gen.O2OJoinDetail) bool { return d.FieldOptionsCheck == "fo."+field+" != nil" })
	if i < 0 {
		t.Fatalf("%s has no O2O join through %s", tc.TableName, field)
	}
	return tc.O2OJoinDetails[i]
}

func relationshipByField(t *testing.T, tc gen.TableContext, field string) gen.RelationshipContext {
	t.Helper()
	i := slices.IndexFunc(tc.Relationships, func(r gen.RelationshipContext) bool { return r.FieldName == field })
	if i < 0 {
		t.Fatalf("%s relationships = %v, want %s", tc.TableName, relationshipNames(tc.Relationships), field)
	}
	return tc.Relationships[i]
}

var allDialects = []config.Dialect{config.DialectPostgres, config.DialectMySQL, config.DialectSQLite}

// A detected self-referential belongs-to holds its key on the source, so the
// join reads the parent row: target.id = this.parent_task_id. Both ends are
// one table, so the target having the FK column says nothing about the side
// (PRD §13.1).
func TestBuildO2OJoinDetails_DetectedSelfReferentialBelongsToJoinsTheParent(t *testing.T) {
	const ddl = `
CREATE TABLE tasks (id bigint PRIMARY KEY, title varchar(64) NOT NULL, parent_task_id bigint UNIQUE,
	CONSTRAINT tasks_parent FOREIGN KEY (parent_task_id) REFERENCES tasks (id));
`
	for _, dialect := range allDialects {
		t.Run(string(dialect), func(t *testing.T) {
			tc := fkSideContexts(t, dialect, ddl, "tasks")
			i := slices.IndexFunc(tc.Relationships, func(r gen.RelationshipContext) bool { return r.Type == parser.OneToOne })
			if i < 0 {
				t.Fatalf("tasks relationships = %v, want a detected O2O edge", relationshipNames(tc.Relationships))
			}
			rel := tc.Relationships[i]
			if rel.FKOnTarget {
				t.Errorf("%s FKOnTarget = true, want false (the key is on the source)", rel.FieldName)
			}
			d := o2oJoin(t, tc, rel.FieldName)
			if d.OnLocal != "parent_task_id" || d.OnRemote != "id" {
				t.Errorf("%s ON = this.%s = target.%s, want this.parent_task_id = target.id", rel.FieldName, d.OnLocal, d.OnRemote)
			}
		})
	}
}

// A declared o2o whose fk columns exist on both tables takes its side from the
// FOREIGN KEY over them: the table declaring it holds the key. With none, it
// is belongs-to (PRD §13.1, §13.4).
func TestConfigRelationship_O2OSideFromForeignKey(t *testing.T) {
	tests := []struct {
		name, ddl, table string
		rel              config.TableRelationship
		wantFKOnTarget   bool
		wantKey          []parser.KeyPair
		wantConstraint   string
		wantLocal        string
		wantRemote       string
	}{
		{
			name: "self-referential",
			ddl: `
CREATE TABLE tasks (id bigint PRIMARY KEY, title varchar(64) NOT NULL, parent_task_id bigint,
	CONSTRAINT tasks_parent FOREIGN KEY (parent_task_id) REFERENCES tasks (id));
`,
			table:          "tasks",
			rel:            config.TableRelationship{Name: "Parent", Type: "o2o", Table: "tasks", FK: config.KeyColumns{"parent_task_id"}},
			wantKey:        []parser.KeyPair{{Local: "parent_task_id", Referenced: "id"}},
			wantConstraint: "tasks_parent",
			wantLocal:      "parent_task_id",
			wantRemote:     "id",
		},
		{
			name: "same-name key column",
			ddl: `
CREATE TABLE codes (code varchar(16) PRIMARY KEY, label varchar(64) NOT NULL);
CREATE TABLE items (id bigint PRIMARY KEY, code varchar(16),
	CONSTRAINT items_code FOREIGN KEY (code) REFERENCES codes (code));
`,
			table:          "items",
			rel:            config.TableRelationship{Name: "CodeRow", Type: "o2o", Table: "codes", FK: config.KeyColumns{"code"}},
			wantKey:        []parser.KeyPair{{Local: "code", Referenced: "code"}},
			wantConstraint: "items_code",
			wantLocal:      "code",
			wantRemote:     "code",
		},
		{
			name: "no foreign key",
			ddl: `
CREATE TABLE tasks (id bigint PRIMARY KEY, title varchar(64) NOT NULL, parent_task_id bigint);
`,
			table:          "tasks",
			rel:            config.TableRelationship{Name: "Parent", Type: "o2o", Table: "tasks", FK: config.KeyColumns{"parent_task_id"}},
			wantKey:        []parser.KeyPair{{Local: "parent_task_id", Referenced: "id"}},
			wantConstraint: "",
			wantLocal:      "parent_task_id",
			wantRemote:     "id",
		},
		{
			name: "has-one over a same-name key column",
			ddl: `
CREATE TABLE codes (code varchar(16) PRIMARY KEY, label varchar(64) NOT NULL);
CREATE TABLE items (id bigint PRIMARY KEY, code varchar(16) UNIQUE,
	CONSTRAINT items_code FOREIGN KEY (code) REFERENCES codes (code));
`,
			table:          "codes",
			rel:            config.TableRelationship{Name: "ItemRow", Type: "o2o", Table: "items", FK: config.KeyColumns{"code"}},
			wantFKOnTarget: true,
			wantKey:        []parser.KeyPair{{Local: "code", Referenced: "code"}},
			wantConstraint: "items_code",
			wantLocal:      "code",
			wantRemote:     "code",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, dialect := range allDialects {
				t.Run(string(dialect), func(t *testing.T) {
					tc := fkSideContexts(t, dialect, tt.ddl, tt.table, tt.rel)
					rel := relationshipByField(t, tc, tt.rel.Name)
					if rel.FKOnTarget != tt.wantFKOnTarget {
						t.Errorf("%s FKOnTarget = %v, want %v", tt.rel.Name, rel.FKOnTarget, tt.wantFKOnTarget)
					}
					if diff := cmp.Diff(tt.wantKey, rel.Key); diff != "" {
						t.Errorf("%s key (-want +got):\n%s", tt.rel.Name, diff)
					}
					if diff := cmp.Diff([]string{tt.wantConstraint}, rel.Constraints); diff != "" {
						t.Errorf("%s constraints (-want +got):\n%s", tt.rel.Name, diff)
					}
					d := o2oJoin(t, tc, tt.rel.Name)
					if d.OnLocal != tt.wantLocal || d.OnRemote != tt.wantRemote {
						t.Errorf("%s ON = this.%s = target.%s, want this.%s = target.%s", tt.rel.Name, d.OnLocal, d.OnRemote, tt.wantLocal, tt.wantRemote)
					}
				})
			}
		})
	}
}
