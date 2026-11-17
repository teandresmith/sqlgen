package tests

import (
	"context"
	"slices"
	"testing"
	"uuid"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/comparator"
	sqlgenpgx "github.com/teandresmith/sqlgen/database/pgx"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy_postgres/models"
)

// compositeTask is one seeded task: its cycle, project and labels by name.
type compositeTask struct {
	title, cycle, project string
	labels                []string
}

// compositeTasks is the data both tenants hold. The two tenants share the
// project codes, so only workspace_id tells their projects apart.
var compositeTasks = []compositeTask{
	{"t1", "c1", "P1", []string{"l1", "l2"}},
	{"t2", "c1", "", []string{"l1"}},
	{"t3", "c2", "P1", nil},
	{"t4", "", "", nil},
}

// tenantBOnly is the task only tenant B holds, under its c1 and P1, with a
// label only tenant B has.
var tenantBOnly = compositeTask{"b-only", "c1", "P1", []string{"l1", "lb"}}

// compositeSeed maps a tenant's names to the ids the database gave them.
type compositeSeed struct {
	cycles, tasks, labels map[string]int64
	rows                  []compositeTask
}

func seedCompositeTenant(t *testing.T, tenant uuid.UUID, tasks []compositeTask) compositeSeed {
	t.Helper()
	ctx := context.Background()
	s := compositeSeed{cycles: map[string]int64{}, tasks: map[string]int64{}, labels: map[string]int64{}, rows: tasks}
	insertID := func(query string, args ...any) int64 {
		t.Helper()
		var id int64
		if err := testPool.QueryRow(ctx, query, args...).Scan(&id); err != nil {
			t.Fatalf("seed %q: %v", query, err)
		}
		return id
	}
	for _, task := range tasks {
		if task.cycle != "" {
			if _, ok := s.cycles[task.cycle]; !ok {
				s.cycles[task.cycle] = insertID(`INSERT INTO cycles (workspace_id, name) VALUES ($1, $2) RETURNING id`, tenant, task.cycle)
			}
		}
		for _, l := range task.labels {
			if _, ok := s.labels[l]; !ok {
				s.labels[l] = insertID(`INSERT INTO labels (workspace_id, name) VALUES ($1, $2) RETURNING id`, tenant, l)
			}
		}
	}
	for _, code := range []string{"P1", "P2"} {
		if _, err := testPool.Exec(ctx, `INSERT INTO projects (workspace_id, code, name) VALUES ($1, $2, $2)`, tenant, code); err != nil {
			t.Fatalf("seed project %s: %v", code, err)
		}
	}
	for _, task := range tasks {
		var cycle *int64
		if task.cycle != "" {
			cycle = new(s.cycles[task.cycle])
		}
		var project *string
		if task.project != "" {
			project = new(task.project)
		}
		id := insertID(`INSERT INTO tasks (workspace_id, cycle_id, project_code, title) VALUES ($1, $2, $3, $4) RETURNING id`,
			tenant, cycle, project, task.title)
		s.tasks[task.title] = id
		for _, l := range task.labels {
			if _, err := testPool.Exec(ctx, `INSERT INTO task_labels (workspace_id, task_id, label_id) VALUES ($1, $2, $3)`,
				tenant, id, s.labels[l]); err != nil {
				t.Fatalf("seed task_label %s/%s: %v", task.title, l, err)
			}
		}
	}
	return s
}

// seedCompositeTenants seeds both tenants, then plants two rows in tenant B
// under session_replication_role = replica, so the composite FKs do not refuse
// them: a task whose cycle_id is A's c1, and a task_labels row linking A's t1 to
// B's lb. Only a load or filter that drops workspace_id from a key reaches
// either one from tenant A's rows.
func seedCompositeTenants(t *testing.T) (a, b compositeSeed) {
	t.Helper()
	resetDB(t)
	a = seedCompositeTenant(t, tenantA, compositeTasks)
	b = seedCompositeTenant(t, tenantB, append(slices.Clone(compositeTasks), tenantBOnly))

	ctx := context.Background()
	conn, err := testPool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer conn.Release()
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`SET session_replication_role = replica`, nil},
		{`INSERT INTO tasks (workspace_id, cycle_id, title) VALUES ($1, $2, 'drift')`, []any{tenantB, a.cycles["c1"]}},
		{`INSERT INTO task_labels (workspace_id, task_id, label_id) VALUES ($1, $2, $3)`, []any{tenantB, a.tasks["t1"], b.labels["lb"]}},
		{`RESET session_replication_role`, nil},
	} {
		if _, err := conn.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatalf("plant drifted task: %q: %v", stmt.sql, err)
		}
	}
	var drift int64
	if err := testPool.QueryRow(ctx, `SELECT id FROM tasks WHERE title = 'drift'`).Scan(&drift); err != nil {
		t.Fatalf("read drifted task: %v", err)
	}
	b.tasks["drift"] = drift
	b.rows = append(b.rows, compositeTask{title: "drift"})
	return a, b
}

func plainTenantClient(tenant uuid.UUID) *models.Client {
	return models.New(sqlgenpgx.New(testPool), models.WithTenantResolver(staticResolver(tenant)))
}

// tenantName labels a row by its workspace, so one map can hold both tenants'
// parents under SkipTenancy.
func tenantName(ws uuid.UUID, name string) string {
	switch ws {
	case tenantA:
		return "A/" + name
	case tenantB:
		return "B/" + name
	}
	return ws.String() + "/" + name
}

// children maps "<tenant>/<parent>" to the sorted ids of the children the
// seed puts under it. key names a task's parent, or "" for none.
func (s compositeSeed) children(tenant string, key func(compositeTask) []string) map[string][]int64 {
	out := map[string][]int64{}
	for _, task := range s.rows {
		for _, parent := range key(task) {
			if parent == "" {
				continue
			}
			out[tenant+"/"+parent] = append(out[tenant+"/"+parent], s.tasks[task.title])
		}
	}
	for k := range out {
		slices.Sort(out[k])
	}
	return out
}

// labelsOf maps "<tenant>/<task>" to the sorted ids of its labels.
func (s compositeSeed) labelsOf(tenant string) map[string][]int64 {
	out := map[string][]int64{}
	for _, task := range s.rows {
		ids := []int64{}
		for _, l := range task.labels {
			ids = append(ids, s.labels[l])
		}
		slices.Sort(ids)
		out[tenant+"/"+task.title] = ids
	}
	return out
}

func mergeChildren(maps ...map[string][]int64) map[string][]int64 {
	out := map[string][]int64{}
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

func sortedChildIDs[T any](rows []*T, id func(*T) int64) []int64 {
	ids := []int64{}
	for _, r := range rows {
		ids = append(ids, id(r))
	}
	slices.Sort(ids)
	return ids
}

// loadCycles reads every visible cycle with its tasks; a parent with no
// children is left out, as the seed's expectations leave it out.
func loadCycles(t *testing.T, c *models.Client, opts ...func(*models.CallOptions[models.CycleFieldOptions])) map[string][]int64 {
	t.Helper()
	rows, err := c.Cycles().GetMany(context.Background(), &models.GetCyclesInput{}, append([]func(*models.CallOptions[models.CycleFieldOptions]){
		func(o *models.CallOptions[models.CycleFieldOptions]) {
			o.FieldOptions = &models.CycleFieldOptions{Name: true, Tasks: &models.TaskRelationshipOptions{
				FieldOptions: &models.TaskFieldOptions{ID: true},
			}}
		},
	}, opts...)...)
	if err != nil {
		t.Fatalf("Cycles().GetMany: %v", err)
	}
	out := map[string][]int64{}
	for _, r := range rows {
		if len(r.Tasks) > 0 {
			out[tenantName(r.WorkspaceID, r.Name)] = sortedChildIDs(r.Tasks, func(t *models.Task) int64 { return t.ID })
		}
	}
	return out
}

func loadProjects(t *testing.T, c *models.Client, opts ...func(*models.CallOptions[models.ProjectFieldOptions])) map[string][]int64 {
	t.Helper()
	rows, err := c.Projects().GetMany(context.Background(), &models.GetProjectsInput{}, append([]func(*models.CallOptions[models.ProjectFieldOptions]){
		func(o *models.CallOptions[models.ProjectFieldOptions]) {
			o.FieldOptions = &models.ProjectFieldOptions{Name: true, Tasks: &models.TaskRelationshipOptions{
				FieldOptions: &models.TaskFieldOptions{ID: true},
			}}
		},
	}, opts...)...)
	if err != nil {
		t.Fatalf("Projects().GetMany: %v", err)
	}
	out := map[string][]int64{}
	for _, r := range rows {
		if len(r.Tasks) > 0 {
			out[tenantName(r.WorkspaceID, r.Code)] = sortedChildIDs(r.Tasks, func(t *models.Task) int64 { return t.ID })
		}
	}
	return out
}

func loadTaskLabels(t *testing.T, c *models.Client, opts ...func(*models.CallOptions[models.TaskFieldOptions])) map[string][]int64 {
	t.Helper()
	rows, err := c.Tasks().GetMany(context.Background(), &models.GetTasksInput{}, append([]func(*models.CallOptions[models.TaskFieldOptions]){
		func(o *models.CallOptions[models.TaskFieldOptions]) {
			o.FieldOptions = &models.TaskFieldOptions{Title: true, Labels: &models.LabelRelationshipOptions{
				FieldOptions: &models.LabelFieldOptions{ID: true},
			}}
		},
	}, opts...)...)
	if err != nil {
		t.Fatalf("Tasks().GetMany: %v", err)
	}
	out := map[string][]int64{}
	for _, r := range rows {
		out[tenantName(r.WorkspaceID, r.Title)] = sortedChildIDs(r.Labels, func(l *models.Label) int64 { return l.ID })
	}
	return out
}

// Each composite tenant edge loads its parent's children from the parent's
// tenant only. Under SkipTenancy the key itself keeps the tenants apart: A and
// B share project codes, and B's drifted task names A's c1 by id.
func TestCompositeTenantFK_LoadStaysInTenant(t *testing.T) {
	a, b := seedCompositeTenants(t)
	clientA, clientB := plainTenantClient(tenantA), plainTenantClient(tenantB)
	byCycle := func(task compositeTask) []string { return []string{task.cycle} }
	byProject := func(task compositeTask) []string { return []string{task.project} }

	for _, tt := range []struct {
		name string
		load func(*testing.T, *models.Client, bool) map[string][]int64
		want func(s compositeSeed, tenant string) map[string][]int64
	}{
		{
			"composite o2m",
			func(t *testing.T, c *models.Client, skip bool) map[string][]int64 {
				if skip {
					return loadCycles(t, c, skipTenancy)
				}
				return loadCycles(t, c)
			},
			func(s compositeSeed, tenant string) map[string][]int64 { return s.children(tenant, byCycle) },
		},
		{
			"composite-PK target",
			func(t *testing.T, c *models.Client, skip bool) map[string][]int64 {
				if skip {
					return loadProjects(t, c, skipTenancy)
				}
				return loadProjects(t, c)
			},
			func(s compositeSeed, tenant string) map[string][]int64 { return s.children(tenant, byProject) },
		},
		{
			"tenant junction",
			func(t *testing.T, c *models.Client, skip bool) map[string][]int64 {
				if skip {
					return loadTaskLabels(t, c, skipTenancy)
				}
				return loadTaskLabels(t, c)
			},
			func(s compositeSeed, tenant string) map[string][]int64 { return s.labelsOf(tenant) },
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, run := range []struct {
				name   string
				client *models.Client
				skip   bool
				want   map[string][]int64
			}{
				{"tenant A", clientA, false, tt.want(a, "A")},
				{"tenant B", clientB, false, tt.want(b, "B")},
				{"skip tenancy", clientA, true, mergeChildren(tt.want(a, "A"), tt.want(b, "B"))},
			} {
				t.Run(run.name, func(t *testing.T) {
					if diff := cmp.Diff(run.want, tt.load(t, run.client, run.skip)); diff != "" {
						t.Errorf("loaded children (-want +got):\n%s", diff)
					}
				})
			}
		})
	}
}

func cycleNames(t *testing.T, c *models.Client, f *models.CycleFilter, opts ...func(*models.CallOptions[models.CycleFieldOptions])) []string {
	t.Helper()
	rows, err := c.Cycles().GetMany(context.Background(), &models.GetCyclesInput{Filter: f}, opts...)
	if err != nil {
		t.Fatalf("Cycles().GetMany: %v", err)
	}
	out := []string{}
	for _, r := range rows {
		out = append(out, tenantName(r.WorkspaceID, r.Name))
	}
	slices.Sort(out)
	return out
}

func projectCodes(t *testing.T, c *models.Client, f *models.ProjectFilter, opts ...func(*models.CallOptions[models.ProjectFieldOptions])) []string {
	t.Helper()
	rows, err := c.Projects().GetMany(context.Background(), &models.GetProjectsInput{Filter: f}, opts...)
	if err != nil {
		t.Fatalf("Projects().GetMany: %v", err)
	}
	out := []string{}
	for _, r := range rows {
		out = append(out, tenantName(r.WorkspaceID, r.Code))
	}
	slices.Sort(out)
	return out
}

func taskTitles(t *testing.T, c *models.Client, f *models.TaskFilter, opts ...func(*models.CallOptions[models.TaskFieldOptions])) []string {
	t.Helper()
	rows, err := c.Tasks().GetMany(context.Background(), &models.GetTasksInput{Filter: f}, opts...)
	if err != nil {
		t.Fatalf("Tasks().GetMany: %v", err)
	}
	out := []string{}
	for _, r := range rows {
		out = append(out, tenantName(r.WorkspaceID, r.Title))
	}
	slices.Sort(out)
	return out
}

// A relationship filter over a composite tenant edge matches children of the
// parent's own tenant only, with tenancy on and with it skipped.
func TestCompositeTenantFK_RelationshipFilterStaysInTenant(t *testing.T) {
	seedCompositeTenants(t)
	clientA, clientB := plainTenantClient(tenantA), plainTenantClient(tenantB)
	title := func(s string) *models.TaskFilter { return &models.TaskFilter{Title: &comparator.String{Eq: new(s)}} }

	for _, tt := range []struct {
		name            string
		query           func(*testing.T, *models.Client, bool) []string
		wantA, wantB    []string
		wantSkipTenancy []string
	}{
		{
			"composite o2m",
			func(t *testing.T, c *models.Client, skip bool) []string {
				f := &models.CycleFilter{Tasks: title("b-only")}
				if skip {
					return cycleNames(t, c, f, skipTenancy)
				}
				return cycleNames(t, c, f)
			},
			[]string{},
			[]string{"B/c1"},
			[]string{"B/c1"},
		},
		{
			// The drifted task lives in B and names A's c1 by id alone.
			"composite o2m matches every key column",
			func(t *testing.T, c *models.Client, skip bool) []string {
				f := &models.CycleFilter{Tasks: title("drift")}
				if skip {
					return cycleNames(t, c, f, skipTenancy)
				}
				return cycleNames(t, c, f)
			},
			[]string{},
			[]string{},
			[]string{},
		},
		{
			// Both tenants hold a project P1; only B's has the task.
			"composite-PK target",
			func(t *testing.T, c *models.Client, skip bool) []string {
				f := &models.ProjectFilter{Tasks: title("b-only")}
				if skip {
					return projectCodes(t, c, f, skipTenancy)
				}
				return projectCodes(t, c, f)
			},
			[]string{},
			[]string{"B/P1"},
			[]string{"B/P1"},
		},
		{
			"tenant junction",
			func(t *testing.T, c *models.Client, skip bool) []string {
				f := &models.TaskFilter{Labels: &models.LabelFilter{Name: &comparator.String{Eq: new("l2")}}}
				if skip {
					return taskTitles(t, c, f, skipTenancy)
				}
				return taskTitles(t, c, f)
			},
			[]string{"A/t1"},
			[]string{"B/t1"},
			[]string{"A/t1", "B/t1"},
		},
		{
			"tenant junction, label in one tenant",
			func(t *testing.T, c *models.Client, skip bool) []string {
				f := &models.TaskFilter{Labels: &models.LabelFilter{Name: &comparator.String{Eq: new("lb")}}}
				if skip {
					return taskTitles(t, c, f, skipTenancy)
				}
				return taskTitles(t, c, f)
			},
			[]string{},
			[]string{"B/b-only"},
			[]string{"B/b-only"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, run := range []struct {
				name   string
				client *models.Client
				skip   bool
				want   []string
			}{
				{"tenant A", clientA, false, tt.wantA},
				{"tenant B", clientB, false, tt.wantB},
				{"skip tenancy", clientA, true, tt.wantSkipTenancy},
			} {
				t.Run(run.name, func(t *testing.T) {
					if diff := cmp.Diff(run.want, tt.query(t, run.client, run.skip)); diff != "" {
						t.Errorf("matched parents (-want +got):\n%s", diff)
					}
				})
			}
		})
	}
}
