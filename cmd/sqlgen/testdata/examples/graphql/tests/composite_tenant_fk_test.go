package tests

import (
	"cmp"
	"context"
	"slices"
	"testing"
	"uuid"

	gocmp "github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/comparator"
	dbpgx "github.com/teandresmith/sqlgen/database/pgx"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models"
)

// The composite tenant foreign keys in schema.sql reach the GraphQL surface:
// cycles → tasks (O2M), task_reports → tasks (O2O) and tasks ↔ labels (M2M
// through task_labels). Each child references its parent through
// (workspace_id, <parent>_id).

// compositeSeed is one tenant's rows, keyed by the name each row was seeded
// under.
type compositeSeed struct {
	cycles, tasks, labels, reports map[string]uuid.UUID
}

// seedCompositeTenant seeds the same shape in one tenant: cycle c1 holds t1 and
// t2, c2 holds t3, t4 has no cycle; t1 carries l1 and l2, t2 carries l2; r1
// reports on t1 and r3 on t3.
func seedCompositeTenant(t *testing.T, tenant uuid.UUID) compositeSeed {
	t.Helper()
	s := compositeSeed{
		cycles: map[string]uuid.UUID{}, tasks: map[string]uuid.UUID{},
		labels: map[string]uuid.UUID{}, reports: map[string]uuid.UUID{},
	}
	insert := func(sql string, args ...any) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		if err := testPool.QueryRow(context.Background(), sql, args...).Scan(&id); err != nil {
			t.Fatalf("seed %q: %v", sql, err)
		}
		return id
	}
	for _, name := range []string{"c1", "c2"} {
		s.cycles[name] = insert(`INSERT INTO cycles (workspace_id, name) VALUES ($1, $2) RETURNING id`, tenant, name)
	}
	for _, task := range []struct{ title, cycle string }{{"t1", "c1"}, {"t2", "c1"}, {"t3", "c2"}, {"t4", ""}} {
		var cycle *uuid.UUID
		if task.cycle != "" {
			cycle = new(s.cycles[task.cycle])
		}
		s.tasks[task.title] = insert(`INSERT INTO tasks (workspace_id, cycle_id, title) VALUES ($1, $2, $3) RETURNING id`, tenant, cycle, task.title)
	}
	for _, name := range []string{"l1", "l2"} {
		s.labels[name] = insert(`INSERT INTO labels (workspace_id, name) VALUES ($1, $2) RETURNING id`, tenant, name)
	}
	for _, link := range [][2]string{{"t1", "l1"}, {"t1", "l2"}, {"t2", "l2"}} {
		if _, err := testPool.Exec(context.Background(),
			`INSERT INTO task_labels (workspace_id, task_id, label_id) VALUES ($1, $2, $3)`,
			tenant, s.tasks[link[0]], s.labels[link[1]]); err != nil {
			t.Fatalf("seed task_labels %v: %v", link, err)
		}
	}
	for _, report := range [][2]string{{"r1", "t1"}, {"r3", "t3"}} {
		s.reports[report[0]] = insert(`INSERT INTO task_reports (workspace_id, task_id, summary) VALUES ($1, $2, $3) RETURNING id`,
			tenant, s.tasks[report[1]], report[0])
	}
	return s
}

// seedCompositeTenants truncates the composite tables and seeds tenant A and
// tenant B with the same shape, so a row of B's in A's answer is a leak.
func seedCompositeTenants(t *testing.T) (a, b compositeSeed) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(),
		`TRUNCATE TABLE task_labels, task_reports, tasks, labels, cycles RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("truncate composite tables: %v", err)
	}
	return seedCompositeTenant(t, fixedTenantA), seedCompositeTenant(t, fixedTenantB)
}

// compositeTenantClient is a Go client reading as tenant.
func compositeTenantClient(tenant uuid.UUID) (*models.Client, context.Context) {
	client := models.New(dbpgx.New(testPool), models.WithTenantResolver(ctxTenantResolver()))
	return client, context.WithValue(context.Background(), tenantCtxKey{}, tenant)
}

// The snapshots below are what a read returns, sorted, in one shape for the
// seed, the Go client and the GraphQL response.
type labelSnap struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type taskSnap struct {
	ID     string      `json:"id"`
	Title  string      `json:"title"`
	Labels []labelSnap `json:"labels"`
}

type cycleSnap struct {
	ID    string     `json:"id"`
	Name  string     `json:"name"`
	Tasks []taskSnap `json:"tasks"`
}

type reportSnap struct {
	ID      string    `json:"id"`
	Summary string    `json:"summary"`
	Task    *taskSnap `json:"task"`
}

func sortCycles(cycles []cycleSnap) []cycleSnap {
	for i := range cycles {
		cycles[i].Tasks = sortTasks(cycles[i].Tasks)
	}
	slices.SortFunc(cycles, func(a, b cycleSnap) int { return cmp.Compare(a.Name, b.Name) })
	return cycles
}

func sortTasks(tasks []taskSnap) []taskSnap {
	if tasks == nil {
		tasks = []taskSnap{}
	}
	for i := range tasks {
		if tasks[i].Labels != nil {
			slices.SortFunc(tasks[i].Labels, func(a, b labelSnap) int { return cmp.Compare(a.Name, b.Name) })
		}
	}
	slices.SortFunc(tasks, func(a, b taskSnap) int { return cmp.Compare(a.Title, b.Title) })
	return tasks
}

// wantCycles is the seed's cycles with their tasks and each task's labels.
func wantCycles(s compositeSeed) []cycleSnap {
	label := func(name string) labelSnap { return labelSnap{ID: s.labels[name].String(), Name: name} }
	task := func(title string, labels ...labelSnap) taskSnap {
		if labels == nil {
			labels = []labelSnap{}
		}
		return taskSnap{ID: s.tasks[title].String(), Title: title, Labels: labels}
	}
	return sortCycles([]cycleSnap{
		{ID: s.cycles["c1"].String(), Name: "c1", Tasks: []taskSnap{task("t1", label("l1"), label("l2")), task("t2", label("l2"))}},
		{ID: s.cycles["c2"].String(), Name: "c2", Tasks: []taskSnap{task("t3")}},
	})
}

func wantReports(s compositeSeed) []reportSnap {
	return []reportSnap{
		{ID: s.reports["r1"].String(), Summary: "r1", Task: &taskSnap{ID: s.tasks["t1"].String(), Title: "t1"}},
		{ID: s.reports["r3"].String(), Summary: "r3", Task: &taskSnap{ID: s.tasks["t3"].String(), Title: "t3"}},
	}
}

func goCycles(t *testing.T, tenant uuid.UUID) []cycleSnap {
	t.Helper()
	client, ctx := compositeTenantClient(tenant)
	rows, err := client.Cycles().GetMany(ctx, &models.GetCyclesInput{}, func(o *models.CallOptions[models.CycleFieldOptions]) {
		o.FieldOptions = &models.CycleFieldOptions{ID: true, Name: true, Tasks: &models.TaskRelationshipOptions{
			FieldOptions: &models.TaskFieldOptions{ID: true, Title: true, Labels: &models.LabelRelationshipOptions{
				FieldOptions: &models.LabelFieldOptions{ID: true, Name: true},
			}},
		}}
	})
	if err != nil {
		t.Fatalf("Go client Cycles().GetMany: %v", err)
	}
	out := make([]cycleSnap, 0, len(rows))
	for _, c := range rows {
		snap := cycleSnap{ID: c.ID.String(), Name: c.Name, Tasks: []taskSnap{}}
		for _, task := range c.Tasks {
			ts := taskSnap{ID: task.ID.String(), Title: task.Title, Labels: []labelSnap{}}
			for _, l := range task.Labels {
				ts.Labels = append(ts.Labels, labelSnap{ID: l.ID.String(), Name: l.Name})
			}
			snap.Tasks = append(snap.Tasks, ts)
		}
		out = append(out, snap)
	}
	return sortCycles(out)
}

func goReports(t *testing.T, tenant uuid.UUID) []reportSnap {
	t.Helper()
	client, ctx := compositeTenantClient(tenant)
	rows, err := client.TaskReports().GetMany(ctx, &models.GetTaskReportsInput{}, func(o *models.CallOptions[models.TaskReportFieldOptions]) {
		o.FieldOptions = &models.TaskReportFieldOptions{ID: true, Summary: true, Task: &models.TaskFieldOptions{ID: true, Title: true}}
	})
	if err != nil {
		t.Fatalf("Go client TaskReports().GetMany: %v", err)
	}
	out := make([]reportSnap, 0, len(rows))
	for _, r := range rows {
		snap := reportSnap{ID: r.ID.String(), Summary: r.Summary}
		if r.Task != nil {
			snap.Task = &taskSnap{ID: r.Task.ID.String(), Title: r.Task.Title}
		}
		out = append(out, snap)
	}
	slices.SortFunc(out, func(a, b reportSnap) int { return cmp.Compare(a.Summary, b.Summary) })
	return out
}

// gqlAs runs query as tenant and decodes its data into out.
func gqlAs(t *testing.T, url string, tenant uuid.UUID, query string, vars map[string]any, out any) {
	t.Helper()
	resp := postGQLAt(t, url, query, vars, map[string]string{"X-Workspace-ID": tenant.String()})
	if len(resp.Errors) > 0 {
		t.Fatalf("tenant %s: graphql errors: %+v", tenant, resp.Errors)
	}
	decodeData(t, resp, out)
}

func gqlCycles(t *testing.T, url string, tenant uuid.UUID, selection string) []cycleSnap {
	t.Helper()
	var out struct {
		CycleList struct {
			Items []cycleSnap `json:"items"`
		} `json:"cycleList"`
	}
	gqlAs(t, url, tenant, `query { cycleList { items { `+selection+` } } }`, nil, &out)
	return sortCycles(out.CycleList.Items)
}

func gqlReports(t *testing.T, url string, tenant uuid.UUID, selection string) []reportSnap {
	t.Helper()
	var out struct {
		TaskReportList struct {
			Items []reportSnap `json:"items"`
		} `json:"taskReportList"`
	}
	gqlAs(t, url, tenant, `query { taskReportList { items { `+selection+` } } }`, nil, &out)
	slices.SortFunc(out.TaskReportList.Items, func(a, b reportSnap) int { return cmp.Compare(a.Summary, b.Summary) })
	return out.TaskReportList.Items
}

// A query through each composite edge returns, as one tenant, the rows the Go
// client returns, and those are exactly that tenant's seeded rows.
func TestCompositeTenantFK_GraphQLMatchesGoClient(t *testing.T) {
	a, _ := seedCompositeTenants(t)
	url := newHeaderTenantedHandler(t)

	t.Run("o2m and m2m", func(t *testing.T) {
		got := gqlCycles(t, url, fixedTenantA, `id name tasks { id title labels { id name } }`)
		if diff := gocmp.Diff(wantCycles(a), got); diff != "" {
			t.Errorf("GraphQL cycles as tenant A (-want +got):\n%s", diff)
		}
		if diff := gocmp.Diff(goCycles(t, fixedTenantA), got); diff != "" {
			t.Errorf("GraphQL against the Go client (-go +graphql):\n%s", diff)
		}
	})

	t.Run("o2o", func(t *testing.T) {
		got := gqlReports(t, url, fixedTenantA, `id summary task { id title }`)
		if diff := gocmp.Diff(wantReports(a), got); diff != "" {
			t.Errorf("GraphQL task reports as tenant A (-want +got):\n%s", diff)
		}
		if diff := gocmp.Diff(goReports(t, fixedTenantA), got); diff != "" {
			t.Errorf("GraphQL against the Go client (-go +graphql):\n%s", diff)
		}
	})
}

// A query that selects no key column on either side still loads each
// composite edge: the loaders read the referenced columns and the FK members
// themselves.
func TestCompositeTenantFK_EdgeLoadsWithoutSelectedKeys(t *testing.T) {
	a, _ := seedCompositeTenants(t)
	url := newHeaderTenantedHandler(t)

	t.Run("o2m and m2m", func(t *testing.T) {
		want := wantCycles(a)
		for i := range want {
			want[i].ID = ""
			for j := range want[i].Tasks {
				want[i].Tasks[j].ID = ""
				for k := range want[i].Tasks[j].Labels {
					want[i].Tasks[j].Labels[k].ID = ""
				}
			}
		}
		got := gqlCycles(t, url, fixedTenantA, `name tasks { title labels { name } }`)
		if diff := gocmp.Diff(want, got); diff != "" {
			t.Errorf("cycles selected without keys (-want +got):\n%s", diff)
		}
	})

	t.Run("o2o", func(t *testing.T) {
		want := wantReports(a)
		for i := range want {
			want[i].ID = ""
			want[i].Task.ID = ""
		}
		got := gqlReports(t, url, fixedTenantA, `summary task { title }`)
		if diff := gocmp.Diff(want, got); diff != "" {
			t.Errorf("task reports selected without keys (-want +got):\n%s", diff)
		}
	})
}

// A relationship filter through a composite edge matches as the Go client's
// does, inside the tenant, and correlates on every key pair.
func TestCompositeTenantFK_GraphQLRelationshipFilter(t *testing.T) {
	a, b := seedCompositeTenants(t)
	url := newHeaderTenantedHandler(t)

	t.Run("o2m", func(t *testing.T) {
		var out struct {
			CycleList struct {
				Items []struct {
					ID string `json:"id"`
				} `json:"items"`
			} `json:"cycleList"`
		}
		gqlAs(t, url, fixedTenantA, `query ($title: String!) {
			cycleList(filter: {tasks: {title: {eq: $title}}}) { items { id } }
		}`, map[string]any{"title": "t2"}, &out)
		got := make([]string, 0, len(out.CycleList.Items))
		for _, item := range out.CycleList.Items {
			got = append(got, item.ID)
		}

		client, ctx := compositeTenantClient(fixedTenantA)
		rows, err := client.Cycles().GetMany(ctx, &models.GetCyclesInput{Filter: &models.CycleFilter{
			Tasks: &models.TaskFilter{Title: &comparator.String{Eq: new("t2")}},
		}})
		if err != nil {
			t.Fatalf("Go client Cycles().GetMany: %v", err)
		}
		goIDs := make([]string, 0, len(rows))
		for _, r := range rows {
			goIDs = append(goIDs, r.ID.String())
		}

		if diff := gocmp.Diff([]string{a.cycles["c1"].String()}, got); diff != "" {
			t.Errorf("GraphQL cycles with task t2 as tenant A (-want +got):\n%s", diff)
		}
		if diff := gocmp.Diff(goIDs, got); diff != "" {
			t.Errorf("GraphQL against the Go client (-go +graphql):\n%s", diff)
		}
	})

	t.Run("m2m", func(t *testing.T) {
		var out struct {
			TaskList struct {
				Items []struct {
					ID string `json:"id"`
				} `json:"items"`
			} `json:"taskList"`
		}
		gqlAs(t, url, fixedTenantA, `query ($name: String!) {
			taskList(filter: {labels: {name: {eq: $name}}}) { items { id } }
		}`, map[string]any{"name": "l1"}, &out)
		if len(out.TaskList.Items) != 1 || out.TaskList.Items[0].ID != a.tasks["t1"].String() {
			t.Errorf("GraphQL tasks with label l1 as tenant A = %+v, want only t1 (%s)", out.TaskList.Items, a.tasks["t1"])
		}
	})

	// Under tenancy the workspace pair and the tenant predicate agree, so the
	// pair is observed with tenancy skipped. The planted task sits in tenant B
	// but names tenant A's cycle, which the composite key forbids; a filter
	// correlating on cycle_id alone would match A's c1 through it.
	t.Run("every pair", func(t *testing.T) {
		conn, err := testPool.Acquire(context.Background())
		if err != nil {
			t.Fatalf("acquire: %v", err)
		}
		defer conn.Release()
		for _, stmt := range []struct {
			sql  string
			args []any
		}{
			{`SET session_replication_role = replica`, nil},
			{`INSERT INTO tasks (workspace_id, cycle_id, title) VALUES ($1, $2, 'drift')`, []any{fixedTenantB, a.cycles["c1"]}},
			{`RESET session_replication_role`, nil},
		} {
			if _, err := conn.Exec(context.Background(), stmt.sql, stmt.args...); err != nil {
				t.Fatalf("plant drifted task: %q: %v", stmt.sql, err)
			}
		}

		client := models.New(dbpgx.New(testPool))
		skip := func(o *models.CallOptions[models.CycleFieldOptions]) { o.SkipTenancy = true }
		byTitle := func(title string) []string {
			t.Helper()
			rows, err := client.Cycles().GetMany(context.Background(), &models.GetCyclesInput{Filter: &models.CycleFilter{
				Tasks: &models.TaskFilter{Title: &comparator.String{Eq: new(title)}},
			}}, skip)
			if err != nil {
				t.Fatalf("Cycles().GetMany(tasks.title = %q): %v", title, err)
			}
			ids := make([]string, 0, len(rows))
			for _, r := range rows {
				ids = append(ids, r.ID.String())
			}
			slices.Sort(ids)
			return ids
		}

		// Positive control: with tenancy skipped the filter sees both tenants.
		if diff := gocmp.Diff(sortedIDs(a.cycles["c1"], b.cycles["c1"]), byTitle("t2")); diff != "" {
			t.Fatalf("cycles with task t2, tenancy skipped (-want +got):\n%s", diff)
		}
		if got := byTitle("drift"); len(got) != 0 {
			t.Errorf("cycles with the drifted task = %v, want none: the filter matched on cycle_id without workspace_id", got)
		}
	})
}

// A nested connect through a composite edge takes the target's ID, and a
// target in another tenant is NOT_FOUND.
func TestCompositeTenantFK_GraphQLNestedConnect(t *testing.T) {
	a, b := seedCompositeTenants(t)
	url := newHeaderTenantedHandler(t)
	const mutation = `mutation ($id: UUID!, $task: UUID!) {
		updateCycleWithRelated(id: $id, input: {tasks: {connect: [$task]}}) { tasks { id } }
	}`

	t.Run("same tenant", func(t *testing.T) {
		var out struct {
			UpdateCycleWithRelated struct {
				Tasks []struct {
					ID string `json:"id"`
				} `json:"tasks"`
			} `json:"updateCycleWithRelated"`
		}
		gqlAs(t, url, fixedTenantA, mutation, map[string]any{"id": a.cycles["c2"], "task": a.tasks["t4"]}, &out)
		got := make([]string, 0, len(out.UpdateCycleWithRelated.Tasks))
		for _, task := range out.UpdateCycleWithRelated.Tasks {
			got = append(got, task.ID)
		}
		slices.Sort(got)
		if diff := gocmp.Diff(sortedIDs(a.tasks["t3"], a.tasks["t4"]), got); diff != "" {
			t.Errorf("c2's tasks after connecting t4 (-want +got):\n%s", diff)
		}
	})

	t.Run("another tenant's target", func(t *testing.T) {
		resp := postGQLAt(t, url, mutation, map[string]any{"id": a.cycles["c1"], "task": b.tasks["t4"]},
			map[string]string{"X-Workspace-ID": fixedTenantA.String()})
		if len(resp.Errors) != 1 || extensionsCode(resp.Errors[0]) != "NOT_FOUND" {
			t.Fatalf("connecting tenant B's task as tenant A: errors = %+v, want one NOT_FOUND", resp.Errors)
		}
		var cycle *uuid.UUID
		if err := testPool.QueryRow(context.Background(), `SELECT cycle_id FROM tasks WHERE id = $1`, b.tasks["t4"]).Scan(&cycle); err != nil {
			t.Fatalf("read tenant B's t4: %v", err)
		}
		if cycle != nil {
			t.Errorf("tenant B's t4 cycle_id = %s after a refused connect, want NULL", cycle)
		}
	})
}
