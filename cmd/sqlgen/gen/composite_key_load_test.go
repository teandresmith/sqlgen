package gen_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// compositeLoadDDL is the tenant pattern: tasks reach cycles through
// (workspace_id, cycle_id), task_reports reach tasks through a key with a
// unique member (O2O), task_labels is a tenant junction, and order_items has a
// composite primary key that item_notes references and item_tags links to.
const compositeLoadDDL = `
CREATE TABLE workspaces (id bigint PRIMARY KEY);
CREATE TABLE cycles (id bigint PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES workspaces(id),
  UNIQUE (workspace_id, id));
CREATE TABLE tasks (id bigint PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES workspaces(id), cycle_id bigint,
  UNIQUE (workspace_id, id),
  FOREIGN KEY (workspace_id, cycle_id) REFERENCES cycles (workspace_id, id));
CREATE TABLE task_reports (id bigint PRIMARY KEY, workspace_id bigint NOT NULL, task_id bigint NOT NULL UNIQUE,
  FOREIGN KEY (workspace_id, task_id) REFERENCES tasks (workspace_id, id));
CREATE TABLE labels (id bigint PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES workspaces(id),
  UNIQUE (workspace_id, id));
CREATE TABLE task_labels (workspace_id bigint NOT NULL, task_id bigint NOT NULL, label_id bigint NOT NULL,
  PRIMARY KEY (workspace_id, task_id, label_id),
  FOREIGN KEY (workspace_id, task_id) REFERENCES tasks (workspace_id, id),
  FOREIGN KEY (workspace_id, label_id) REFERENCES labels (workspace_id, id));
CREATE TABLE order_items (order_id bigint NOT NULL, product_id bigint NOT NULL, PRIMARY KEY (order_id, product_id));
CREATE TABLE item_notes (id bigint PRIMARY KEY, order_id bigint NOT NULL, product_id bigint NOT NULL,
  FOREIGN KEY (order_id, product_id) REFERENCES order_items (order_id, product_id));
CREATE TABLE item_receipts (id bigint PRIMARY KEY, order_id bigint NOT NULL UNIQUE, product_id bigint NOT NULL,
  FOREIGN KEY (order_id, product_id) REFERENCES order_items (order_id, product_id));
CREATE TABLE tags (id bigint PRIMARY KEY);
CREATE TABLE item_tags (tag_id bigint NOT NULL REFERENCES tags(id), order_id bigint NOT NULL, product_id bigint NOT NULL,
  PRIMARY KEY (tag_id, order_id, product_id),
  FOREIGN KEY (order_id, product_id) REFERENCES order_items (order_id, product_id));
CREATE TABLE teams (id bigint PRIMARY KEY, code text UNIQUE);
CREATE TABLE members (id bigint PRIMARY KEY, team_code text REFERENCES teams(code));
`

// compositeLoadTables builds compositeLoadDDL's contexts by table name.
func compositeLoadTables(t *testing.T, driver config.Driver) map[string]gen.TableContext {
	t.Helper()
	schema := parseDDLForTest(t, config.DialectPostgres, compositeLoadDDL)
	parser.DetectRelationships(schema)
	input := testInput(schema)
	input.Config.Output.Driver = driver
	// Detection gives an O2O its belongs-to side; the has-one side is declared.
	input.Config.Tables["order_items"] = config.TableConfig{Relationships: []config.TableRelationship{
		{Name: "Receipt", Type: "o2o", Table: "item_receipts", FK: config.KeyColumns{"order_id", "product_id"}},
	}}
	contexts, err := gen.BuildTableContextsFromSchema(schema, input.Config)
	if err != nil {
		t.Fatalf("BuildTableContextsFromSchema() error: %v", err)
	}
	byName := make(map[string]gen.TableContext, len(contexts))
	for _, tc := range contexts {
		byName[tc.TableName] = tc
	}
	return byName
}

func assertContainsAll(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Errorf("output is missing %q\n\nfull output:\n%s", want, got)
		}
	}
}

// A composite-key O2M binds one parameter per key column on every driver, pgx
// included, so it takes one statement per chunk of keys, 4000 on PostgreSQL.
func TestGetTemplate_o2mCompositeKeyLoading(t *testing.T) {
	want := []string{
		"for start := 0; start < len(cycleKeys); start += 4000 {",
		"chunk := cycleKeys[start:min(start+4000, len(cycleKeys))]",
		`sql.BuildCompositePKBatchCondition(c.dialect, []string{"workspace_id", "cycle_id"}, chunk),`,
		`requiredColumns: []string{"workspace_id", "cycle_id"},`,
		"byKey := make(map[[2]string][]*Task)",
		"key := [2]string{fmt.Sprint(r.WorkspaceID), fmt.Sprint((*r.CycleID))}",
		"filter.And = append(filter.And, fo.Tasks.Filter)",
	}
	for _, tt := range []struct {
		name   string
		driver config.Driver
	}{
		{"pgx", config.DriverPgx},
		{"stdlib", config.DriverStdlib},
	} {
		t.Run(tt.name, func(t *testing.T) {
			output := executeGetTemplate(t, compositeLoadTables(t, tt.driver)["cycles"])
			assertContainsAll(t, output, want...)
			if strings.Contains(output, "In: cycleKeys") || strings.Contains(output, "CycleID: &comparator") {
				t.Errorf("the key condition went through the child's filter\n\nfull output:\n%s", output)
			}
		})
	}

	t.Run("nullable member is guarded", func(t *testing.T) {
		output := executeGetTemplate(t, compositeLoadTables(t, config.DriverPgx)["cycles"])
		assertContainsAll(t, output, "if !(r.CycleID != nil) {\n\t\t\t\t\tcontinue\n\t\t\t\t}\n\t\t\t\tkey := [2]string{")
	})
}

// An M2M load reads the junction by tuple, scans each distinct junction column
// once (a tenant junction shares its tenant column between both keys), and
// maps targets on every key column.
func TestGetTemplate_m2mCompositeKeyLoading(t *testing.T) {
	output := executeGetTemplate(t, compositeLoadTables(t, config.DriverPgx)["tasks"])
	assertContainsAll(t, output,
		"for start := 0; start < len(taskKeys); start += 4000 {",
		`Columns:    []string{"workspace_id", "task_id", "label_id"},`,
		`Conditions: []sql.Condition{sql.BuildCompositePKBatchCondition(c.dialect, []string{"workspace_id", "task_id"}, chunk)},`,
		"var key0 int64",
		"var key1 int64",
		"var key2 int64",
		`case "label_id":`,
		"parentToTargetIDs := make(map[[2]string][][2]string)",
		"parentID, targetID := [2]string{fmt.Sprint(key0), fmt.Sprint(key1)}, [2]string{fmt.Sprint(key0), fmt.Sprint(key2)}",
		`sql.TupleInSubquery(c.dialect, []string{"workspace_id", "id"}, sql.Subquery{`,
		`SQL: "SELECT j." + c.dialect.QuoteIdentifier("workspace_id") + ", j." + c.dialect.QuoteIdentifier("label_id") +`,
		`requiredColumns: []string{"workspace_id", "id"},`,
		"parentsByTarget := make(map[[2]string][]int, len(relatedLabels))",
		"for _, i := range parentsByTarget[[2]string{fmt.Sprint(t.WorkspaceID), fmt.Sprint(t.ID)}] {",
	)
}

// A keyed load runs the child's tenant-scoped GetMany under the parent's tenant
// options, as a one-column load does.
func TestTenancyTemplate_keyedLoadPropagatesTenantOptions(t *testing.T) {
	tables := compositeLoadTables(t, config.DriverPgx)
	for _, tt := range []struct{ name, table, loader string }{
		{"o2m", "cycles", "var relatedTasks []*Task"},
		{"m2m", "tasks", "relatedLabels, err := c."},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tc := tables[tt.table]
			tc.Tenancy = tenantedProductContext().Tenancy
			tc.ExplicitTenantGoType = "int64"
			output := executeGetTemplate(t, tc)
			_, load, found := strings.Cut(output, tt.loader)
			if !found {
				t.Fatalf("output is missing %q\n\nfull output:\n%s", tt.loader, output)
			}
			assertContainsAll(t, load, "co.SkipTenancy = options.SkipTenancy", "co.Tenant = options.Tenant")
		})
	}
}

// An M2M with a one-column key on one side and two on the other chunks on the
// parent's key and maps through arrays of each side's length.
func TestGetTemplate_m2mIntoCompositePKTarget(t *testing.T) {
	output := executeGetTemplate(t, compositeLoadTables(t, config.DriverPgx)["tags"])
	assertContainsAll(t, output,
		"for start := 0; start < len(tagKeys); start += 32767 {",
		"parentToTargetIDs := make(map[[1]string][][2]string)",
		`sql.TupleInSubquery(c.dialect, []string{"order_id", "product_id"}, sql.Subquery{`,
	)
}

// A narrowed projection unions every referenced parent column, and the child
// fetch declares every FK member.
func TestRequiredColumns_compositeKeyMembers(t *testing.T) {
	tables := compositeLoadTables(t, config.DriverPgx)
	assertContainsAll(t, executeGetTemplate(t, tables["cycles"]),
		`columns = unionColumns(columns, "workspace_id", "id")`,
		`requiredColumns: []string{"workspace_id", "cycle_id"},`,
	)
	assertContainsAll(t, executeGetTemplate(t, tables["tasks"]),
		`columns = unionColumns(columns, "workspace_id", "id")`,
	)
}

func TestBuildO2OJoinDetails_CompositeKeyJoinsEveryPair(t *testing.T) {
	tables := compositeLoadTables(t, config.DriverPgx)
	t.Run("belongs-to", func(t *testing.T) {
		assertContainsAll(t, executeRelationshipsTemplate(t, tables["task_reports"]),
			`fmt.Sprintf("t2.%s = t.%s", d.QuoteIdentifier("workspace_id"), d.QuoteIdentifier("workspace_id"))`,
			`fmt.Sprintf(" AND t2.%s = t.%s", d.QuoteIdentifier("id"), d.QuoteIdentifier("task_id"))`,
		)
	})
	t.Run("has-one", func(t *testing.T) {
		assertContainsAll(t, executeRelationshipsTemplate(t, tables["order_items"]),
			`fmt.Sprintf("r.%s = o.%s", d.QuoteIdentifier("order_id"), d.QuoteIdentifier("order_id"))`,
			`fmt.Sprintf(" AND r.%s = o.%s", d.QuoteIdentifier("product_id"), d.QuoteIdentifier("product_id"))`,
		)
	})
}

// Each composite shape gets the field its one-column form does.
func TestGenerate_CompositeKeyStructFields(t *testing.T) {
	tables := compositeLoadTables(t, config.DriverPgx)
	for _, tt := range []struct {
		name, table, field, goType string
	}{
		{"belongs-to", "task_reports", "Tasks", "*Task"},
		{"has-one", "order_items", "Receipt", "*ItemReceipt"},
		{"m2m", "tasks", "Labels", "[]*Label"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			i := slices.IndexFunc(tables[tt.table].Relationships, func(r gen.RelationshipContext) bool { return r.FieldName == tt.field })
			if i < 0 {
				t.Fatalf("%s relationships = %v, want %s", tt.table, relationshipNames(tables[tt.table].Relationships), tt.field)
			}
			if got := tables[tt.table].Relationships[i].GoType; got != tt.goType {
				t.Errorf("%s.%s type = %s, want %s", tt.table, tt.field, got, tt.goType)
			}
		})
	}
}

func TestFilterTemplate_CompositeKeyCorrelatesEveryPair(t *testing.T) {
	tables := compositeLoadTables(t, config.DriverPgx)

	t.Run("o2m", func(t *testing.T) {
		assertContainsAll(t, renderRelFilter(t, tables["cycles"]),
			`" WHERE " + tgt + "." + dialect.QuoteIdentifier("workspace_id") + " = " + sql.CorrelationTokenAt(0) +`,
			`" AND " + tgt + "." + dialect.QuoteIdentifier("cycle_id") + " = " + sql.CorrelationTokenAt(1)`,
			`return sql.Exists([]string{"workspace_id", "id"}, sql.Subquery{SQL: subSQL, Args: subArgs})`,
		)
	})

	t.Run("m2m", func(t *testing.T) {
		assertContainsAll(t, renderRelFilter(t, tables["tasks"]),
			`" ON " + tgt + "." + dialect.QuoteIdentifier("workspace_id") + " = " + jn + "." + dialect.QuoteIdentifier("workspace_id") +`,
			`" AND " + tgt + "." + dialect.QuoteIdentifier("id") + " = " + jn + "." + dialect.QuoteIdentifier("label_id") +`,
			`" WHERE " + jn + "." + dialect.QuoteIdentifier("workspace_id") + " = " + sql.CorrelationTokenAt(0) +`,
			`" AND " + jn + "." + dialect.QuoteIdentifier("task_id") + " = " + sql.CorrelationTokenAt(1)`,
			`return sql.Exists([]string{"workspace_id", "id"}, sql.Subquery{SQL: subSQL, Args: subArgs})`,
		)
	})
}

// Every list relationship gets a filter member, whatever its key looks like.
func TestRelationshipFilters_MemberForAnyKey(t *testing.T) {
	tables := compositeLoadTables(t, config.DriverPgx)
	for _, tt := range []struct {
		name, table, field string
	}{
		{"composite-PK parent", "order_items", "ItemNotes"},
		{"edge into a unique key", "teams", "Members"},
		{"m2m into a composite-PK target", "tags", "OrderItems"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := relFilterNames(tables[tt.table].RelationshipFilters)
			if !slices.Contains(got, tt.field) {
				t.Errorf("%s relationship filters = %q, want %s among them", tt.table, got, tt.field)
			}
		})
	}
}
