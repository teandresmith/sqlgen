package tests

import (
	"context"
	"errors"
	"slices"
	"testing"
	"uuid"

	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy_postgres/models"
)

// taskRow is a tasks row read past the client, so no tenant filter or cache
// stands between a test and what the writes left behind.
type taskRow struct {
	workspace uuid.UUID
	cycle     *int64
	sprint    *int64
}

func readTask(t *testing.T, id int64) taskRow {
	t.Helper()
	var r taskRow
	err := testPool.QueryRow(context.Background(),
		"SELECT workspace_id, cycle_id, sprint_id FROM tasks WHERE id = $1", id).
		Scan(&r.workspace, &r.cycle, &r.sprint)
	if err != nil {
		t.Fatalf("read task %d: %v", id, err)
	}
	return r
}

func int64Equal(a, b *int64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func assertTask(t *testing.T, id int64, want taskRow) {
	t.Helper()
	got := readTask(t, id)
	if got.workspace != want.workspace || !int64Equal(got.cycle, want.cycle) || !int64Equal(got.sprint, want.sprint) {
		t.Errorf("task %d = %+v, want %+v", id, got, want)
	}
}

func mkCycle(t *testing.T, c *models.Client, name string) *models.Cycle {
	t.Helper()
	cycle, err := c.Cycles().Create(context.Background(), &models.CreateCycleInput{Name: name})
	if err != nil {
		t.Fatalf("create cycle %s: %v", name, err)
	}
	return cycle
}

func mkSprint(t *testing.T, c *models.Client, name string) *models.Sprint {
	t.Helper()
	sprint, err := c.Sprints().Create(context.Background(), &models.CreateSprintInput{Name: name})
	if err != nil {
		t.Fatalf("create sprint %s: %v", name, err)
	}
	return sprint
}

func mkTask(t *testing.T, c *models.Client, title string) *models.Task {
	t.Helper()
	task, err := c.Tasks().Create(context.Background(), &models.CreateTaskInput{Title: title})
	if err != nil {
		t.Fatalf("create task %s: %v", title, err)
	}
	return task
}

func mkLabel(t *testing.T, c *models.Client, name string) *models.Label {
	t.Helper()
	label, err := c.Labels().Create(context.Background(), &models.CreateLabelInput{Name: name})
	if err != nil {
		t.Fatalf("create label %s: %v", name, err)
	}
	return label
}

// assertNestedError requires err to wrap want, attributed to edge and verb,
// naming id when id is non-nil.
func assertNestedError(t *testing.T, err, want error, edge, verb string, id any) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
	nested, ok := errors.AsType[*models.NestedMutationError](err)
	if !ok {
		t.Fatalf("error %v carries no NestedMutationError", err)
	}
	if nested.Edge != edge || nested.Verb != verb {
		t.Errorf("attribution = %s %s, want %s %s", nested.Edge, nested.Verb, edge, verb)
	}
	if id != nil && nested.ID != id {
		t.Errorf("error names %v, want %v", nested.ID, id)
	}
}

func skipTenancy[T any](o *models.CallOptions[T]) { o.SkipTenancy = true }

func TestCreateWithRelated_CreateSetsEveryKeyMemberFromTheParent(t *testing.T) {
	resetDB(t)
	env := newTenantEnv(t)
	ctx := context.Background()

	t.Run("under tenancy", func(t *testing.T) {
		cycle, err := env.clientA.Cycles().CreateWithRelated(ctx, &models.CreateCycleWithRelatedInput{
			Cycle: models.CreateCycleInput{Name: "c"},
			Tasks: &models.CycleTasksCreateNested{Create: []*models.CycleTasksCreateInput{{Title: "t"}}},
		})
		if err != nil {
			t.Fatalf("CreateWithRelated: %v", err)
		}
		tasks, err := env.clientA.Tasks().GetMany(ctx, &models.GetTasksInput{
			Filter: &models.TaskFilter{},
		})
		if err != nil {
			t.Fatalf("GetMany: %v", err)
		}
		if len(tasks) != 1 {
			t.Fatalf("tasks = %d, want 1", len(tasks))
		}
		assertTask(t, tasks[0].ID, taskRow{workspace: tenantA, cycle: &cycle.ID})
	})

	t.Run("under SkipTenancy the child takes the parent's workspace", func(t *testing.T) {
		cycle, err := env.clientA.Cycles().CreateWithRelated(ctx, &models.CreateCycleWithRelatedInput{
			Cycle: models.CreateCycleInput{Name: "c", WorkspaceID: omittable.Set(tenantB)},
			Tasks: &models.CycleTasksCreateNested{Create: []*models.CycleTasksCreateInput{{Title: "t"}}},
		}, skipTenancy[models.CycleFieldOptions])
		if err != nil {
			t.Fatalf("CreateWithRelated: %v", err)
		}
		var workspace uuid.UUID
		if err := testPool.QueryRow(ctx, "SELECT workspace_id FROM tasks WHERE cycle_id = $1", cycle.ID).Scan(&workspace); err != nil {
			t.Fatalf("read task: %v", err)
		}
		if workspace != tenantB {
			t.Errorf("task workspace = %s, want the parent's %s", workspace, tenantB)
		}
	})
}

// The parent is a shared table, so tenancy does not guard the column the key
// copies. The child's tenant comes from the parent and from the resolver, and
// the two disagree.
func TestCreateWithRelated_TenancySetTenantMismatchIsAlreadyRelated(t *testing.T) {
	resetDB(t)
	env := newTenantEnv(t)
	ctx := context.Background()

	template, err := env.clientA.Templates().Create(ctx, &models.CreateTemplateInput{OwnerWorkspace: tenantB, Name: "b's"})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}

	_, err = env.clientA.Templates().UpdateWithRelated(ctx, template.ID, &models.UpdateTemplateWithRelatedInput{
		TemplateSteps: &models.TemplateTemplateStepsUpdateNested{
			Create: []*models.TemplateTemplateStepsCreateInput{{Title: "step"}},
		},
	})
	assertNestedError(t, err, models.ErrAlreadyRelated, "TemplateSteps", "create", nil)

	var steps int
	if err := testPool.QueryRow(ctx, "SELECT count(*) FROM template_steps").Scan(&steps); err != nil {
		t.Fatalf("count steps: %v", err)
	}
	if steps != 0 {
		t.Errorf("template_steps rows = %d, want none", steps)
	}

	if _, err := env.clientB.Templates().UpdateWithRelated(ctx, template.ID, &models.UpdateTemplateWithRelatedInput{
		TemplateSteps: &models.TemplateTemplateStepsUpdateNested{
			Create: []*models.TemplateTemplateStepsCreateInput{{Title: "step"}},
		},
	}); err != nil {
		t.Fatalf("the owner's own create: %v", err)
	}
}

func TestUpdateWithRelated_PinnedMemberMismatchIsAlreadyRelated(t *testing.T) {
	resetDB(t)
	env := newTenantEnv(t)
	ctx := context.Background()

	cycle := mkCycle(t, env.clientA, "a")
	sprint := mkSprint(t, env.clientA, "a")
	task := mkTask(t, env.clientB, "b's")

	t.Run("default edge", func(t *testing.T) {
		_, err := env.clientA.Cycles().UpdateWithRelated(ctx, cycle.ID, &models.UpdateCycleWithRelatedInput{
			Tasks: &models.CycleTasksUpdateNested{Connect: []int64{task.ID}},
		}, skipTenancy[models.CycleFieldOptions])
		assertNestedError(t, err, models.ErrAlreadyRelated, "Tasks", "connect", task.ID)
	})

	t.Run("allow_reparent never moves a pinned member", func(t *testing.T) {
		_, err := env.clientA.Sprints().UpdateWithRelated(ctx, sprint.ID, &models.UpdateSprintWithRelatedInput{
			Tasks: &models.SprintTasksUpdateNested{Connect: []int64{task.ID}},
		}, skipTenancy[models.SprintFieldOptions])
		assertNestedError(t, err, models.ErrAlreadyRelated, "Tasks", "connect", task.ID)
	})

	assertTask(t, task.ID, taskRow{workspace: tenantB})
}

func TestUpdateWithRelated_ConnectWritesOnlyFreeMembers(t *testing.T) {
	resetDB(t)
	env := newTenantEnv(t)
	ctx := context.Background()

	cycle := mkCycle(t, env.clientA, "c")
	first := mkSprint(t, env.clientA, "first")
	second := mkSprint(t, env.clientA, "second")
	task := mkTask(t, env.clientA, "t")

	connectCycle := func() error {
		_, err := env.clientA.Cycles().UpdateWithRelated(ctx, cycle.ID, &models.UpdateCycleWithRelatedInput{
			Tasks: &models.CycleTasksUpdateNested{Connect: []int64{task.ID, task.ID}},
		})
		return err
	}
	if err := connectCycle(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	assertTask(t, task.ID, taskRow{workspace: tenantA, cycle: &cycle.ID})

	if err := connectCycle(); err != nil {
		t.Fatalf("connecting a task the cycle holds is a no-op, got: %v", err)
	}

	for _, sprint := range []*models.Sprint{first, second} {
		if _, err := env.clientA.Sprints().UpdateWithRelated(ctx, sprint.ID, &models.UpdateSprintWithRelatedInput{
			Tasks: &models.SprintTasksUpdateNested{Connect: []int64{task.ID}},
		}); err != nil {
			t.Fatalf("connect to sprint %d: %v", sprint.ID, err)
		}
		assertTask(t, task.ID, taskRow{workspace: tenantA, cycle: &cycle.ID, sprint: &sprint.ID})
	}
}

func TestUpdateWithRelated_CrossTenantConnectIsNotFoundBeforeAnyWrite(t *testing.T) {
	resetDB(t)
	env := newTenantEnv(t)
	ctx := context.Background()

	cycle := mkCycle(t, env.clientA, "a")
	sprint := mkSprint(t, env.clientA, "a")
	task := mkTask(t, env.clientB, "b's")

	_, err := env.clientA.Cycles().UpdateWithRelated(ctx, cycle.ID, &models.UpdateCycleWithRelatedInput{
		Tasks: &models.CycleTasksUpdateNested{Connect: []int64{task.ID}},
	})
	assertNestedError(t, err, models.ErrNotFound, "Tasks", "connect", task.ID)

	_, err = env.clientA.Sprints().UpdateWithRelated(ctx, sprint.ID, &models.UpdateSprintWithRelatedInput{
		Tasks: &models.SprintTasksUpdateNested{Connect: []int64{task.ID}},
	})
	assertNestedError(t, err, models.ErrNotFound, "Tasks", "connect", task.ID)

	assertTask(t, task.ID, taskRow{workspace: tenantB})
}

func TestUpdateWithRelated_ConnectTargetHeldByAnotherParentIsAlreadyRelated(t *testing.T) {
	resetDB(t)
	env := newTenantEnv(t)
	ctx := context.Background()

	holder := mkCycle(t, env.clientA, "holder")
	other := mkCycle(t, env.clientA, "other")
	task := mkTask(t, env.clientA, "t")
	if _, err := env.clientA.Cycles().UpdateWithRelated(ctx, holder.ID, &models.UpdateCycleWithRelatedInput{
		Tasks: &models.CycleTasksUpdateNested{Connect: []int64{task.ID}},
	}); err != nil {
		t.Fatalf("connect: %v", err)
	}

	_, err := env.clientA.Cycles().UpdateWithRelated(ctx, other.ID, &models.UpdateCycleWithRelatedInput{
		Tasks: &models.CycleTasksUpdateNested{Connect: []int64{task.ID}},
	})
	assertNestedError(t, err, models.ErrAlreadyRelated, "Tasks", "connect", task.ID)
	assertTask(t, task.ID, taskRow{workspace: tenantA, cycle: &holder.ID})
}

func TestUpdateWithRelated_DisconnectNullsOnlyFreeMembers(t *testing.T) {
	resetDB(t)
	env := newTenantEnv(t)
	ctx := context.Background()

	cycle := mkCycle(t, env.clientA, "c")
	other := mkCycle(t, env.clientA, "other")
	sprint := mkSprint(t, env.clientA, "s")
	one := mkTask(t, env.clientA, "one")
	two := mkTask(t, env.clientA, "two")
	held := mkTask(t, env.clientA, "held")
	if _, err := env.clientA.Cycles().UpdateWithRelated(ctx, cycle.ID, &models.UpdateCycleWithRelatedInput{
		Tasks: &models.CycleTasksUpdateNested{Connect: []int64{one.ID, two.ID}},
	}); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := env.clientA.Cycles().UpdateWithRelated(ctx, other.ID, &models.UpdateCycleWithRelatedInput{
		Tasks: &models.CycleTasksUpdateNested{Connect: []int64{held.ID}},
	}); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := env.clientA.Sprints().UpdateWithRelated(ctx, sprint.ID, &models.UpdateSprintWithRelatedInput{
		Tasks: &models.SprintTasksUpdateNested{Connect: []int64{one.ID}},
	}); err != nil {
		t.Fatalf("connect: %v", err)
	}

	// A disconnect naming another parent's task is a no-op.
	if _, err := env.clientA.Cycles().UpdateWithRelated(ctx, cycle.ID, &models.UpdateCycleWithRelatedInput{
		Tasks: &models.CycleTasksUpdateNested{Disconnect: []int64{one.ID, held.ID}},
	}); err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	assertTask(t, one.ID, taskRow{workspace: tenantA, sprint: &sprint.ID})
	assertTask(t, two.ID, taskRow{workspace: tenantA, cycle: &cycle.ID})
	assertTask(t, held.ID, taskRow{workspace: tenantA, cycle: &other.ID})

	if _, err := env.clientA.Cycles().UpdateWithRelated(ctx, cycle.ID, &models.UpdateCycleWithRelatedInput{
		Tasks: &models.CycleTasksUpdateNested{Clear: true},
	}); err != nil {
		t.Fatalf("clear: %v", err)
	}
	assertTask(t, two.ID, taskRow{workspace: tenantA})
	assertTask(t, held.ID, taskRow{workspace: tenantA, cycle: &other.ID})
}

func TestUpdateWithRelated_MatchFullDisconnectNullsEveryMember(t *testing.T) {
	resetDB(t)
	env := newTenantEnv(t)
	ctx := context.Background()

	milestone, err := env.clientA.Milestones().Create(ctx, &models.CreateMilestoneInput{Code: "m1"})
	if err != nil {
		t.Fatalf("create milestone: %v", err)
	}
	checkpoint, err := env.clientA.Checkpoints().Create(ctx, &models.CreateCheckpointInput{Title: "cp"})
	if err != nil {
		t.Fatalf("create checkpoint: %v", err)
	}
	read := func() (code *string, id *int64) {
		t.Helper()
		if err := testPool.QueryRow(ctx, "SELECT milestone_code, milestone_id FROM checkpoints WHERE id = $1", checkpoint.ID).Scan(&code, &id); err != nil {
			t.Fatalf("read checkpoint: %v", err)
		}
		return code, id
	}

	connect := func() {
		t.Helper()
		if _, err := env.clientA.Milestones().UpdateWithRelated(ctx, milestone.ID, &models.UpdateMilestoneWithRelatedInput{
			Checkpoints: &models.MilestoneCheckpointsUpdateNested{Connect: []int64{checkpoint.ID}},
		}); err != nil {
			t.Fatalf("connect: %v", err)
		}
		if code, id := read(); code == nil || *code != "m1" || id == nil || *id != milestone.ID {
			t.Fatalf("after connect: code %v id %v, want both members written", code, id)
		}
	}

	connect()
	if _, err := env.clientA.Milestones().UpdateWithRelated(ctx, milestone.ID, &models.UpdateMilestoneWithRelatedInput{
		Checkpoints: &models.MilestoneCheckpointsUpdateNested{Disconnect: []int64{checkpoint.ID}},
	}); err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	if code, id := read(); code != nil || id != nil {
		t.Errorf("after disconnect: code %v id %v, want both NULL", code, id)
	}

	connect()
	if _, err := env.clientA.Milestones().UpdateWithRelated(ctx, milestone.ID, &models.UpdateMilestoneWithRelatedInput{
		Checkpoints: &models.MilestoneCheckpointsUpdateNested{Clear: true},
	}); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if code, id := read(); code != nil || id != nil {
		t.Errorf("after clear: code %v id %v, want both NULL", code, id)
	}
}

// A surrogate-keyed junction links through the UNIQUE over the union of its two
// keys. A re-link keeps the row and its surrogate, and the tenant comes from the
// resolver as it does on a junction keyed by that union.
func TestUpdateWithRelated_SurrogateTenantJunctionLinksThroughItsUnique(t *testing.T) {
	resetDB(t)
	env := newTenantEnv(t)
	ctx := context.Background()

	task := mkTask(t, env.clientA, "t")
	connected, err := env.clientA.Reviewers().Create(ctx, &models.CreateReviewerInput{Name: "connected"})
	if err != nil {
		t.Fatalf("create reviewer: %v", err)
	}
	foreign, err := env.clientB.Reviewers().Create(ctx, &models.CreateReviewerInput{Name: "foreign"})
	if err != nil {
		t.Fatalf("create reviewer: %v", err)
	}

	links := func() map[int64]int64 {
		t.Helper()
		rows, err := testPool.Query(ctx, "SELECT reviewer_id, id FROM task_reviewers WHERE task_id = $1 AND workspace_id = $2", task.ID, tenantA)
		if err != nil {
			t.Fatalf("read links: %v", err)
		}
		defer rows.Close()
		got := map[int64]int64{}
		for rows.Next() {
			var reviewer, id int64
			if err := rows.Scan(&reviewer, &id); err != nil {
				t.Fatalf("scan link: %v", err)
			}
			got[reviewer] = id
		}
		return got
	}
	update := func(block *models.TaskReviewersUpdateNested) error {
		_, err := env.clientA.Tasks().UpdateWithRelated(ctx, task.ID, &models.UpdateTaskWithRelatedInput{Reviewers: block})
		return err
	}

	if err := update(&models.TaskReviewersUpdateNested{
		Connect: []int64{connected.ID},
		Create:  []*models.CreateReviewerInput{{Name: "created"}},
	}); err != nil {
		t.Fatalf("link: %v", err)
	}
	first := links()
	if len(first) != 2 {
		t.Fatalf("links = %v, want the connected and the created reviewer", first)
	}

	if err := update(&models.TaskReviewersUpdateNested{Connect: []int64{connected.ID}}); err != nil {
		t.Fatalf("re-link: %v", err)
	}
	got := links()
	if len(got) != 2 || got[connected.ID] != first[connected.ID] {
		t.Errorf("links after a re-link = %v, want %v", got, first)
	}

	err = update(&models.TaskReviewersUpdateNested{Connect: []int64{foreign.ID}})
	assertNestedError(t, err, models.ErrNotFound, "Reviewers", "connect", foreign.ID)

	if err := update(&models.TaskReviewersUpdateNested{Clear: true}); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if n := len(links()); n != 0 {
		t.Errorf("links after clear = %d, want 0", n)
	}
}

func TestUpdateWithRelated_TenantJunctionLinksAndUnlinksThroughEveryColumn(t *testing.T) {
	resetDB(t)
	env := newTenantEnv(t)
	ctx := context.Background()

	task := mkTask(t, env.clientA, "t")
	connected := mkLabel(t, env.clientA, "connected")
	foreign := mkLabel(t, env.clientB, "foreign")

	links := func() map[int64]uuid.UUID {
		t.Helper()
		rows, err := testPool.Query(ctx, "SELECT label_id, workspace_id FROM task_labels WHERE task_id = $1", task.ID)
		if err != nil {
			t.Fatalf("read links: %v", err)
		}
		defer rows.Close()
		got := map[int64]uuid.UUID{}
		for rows.Next() {
			var label int64
			var workspace uuid.UUID
			if err := rows.Scan(&label, &workspace); err != nil {
				t.Fatalf("scan link: %v", err)
			}
			got[label] = workspace
		}
		return got
	}

	update := func(block *models.TaskLabelsUpdateNested) error {
		_, err := env.clientA.Tasks().UpdateWithRelated(ctx, task.ID, &models.UpdateTaskWithRelatedInput{Labels: block})
		return err
	}

	if err := update(&models.TaskLabelsUpdateNested{
		Connect: []int64{connected.ID},
		Create:  []*models.CreateLabelInput{{Name: "created"}},
	}); err != nil {
		t.Fatalf("link: %v", err)
	}
	got := links()
	if len(got) != 2 {
		t.Fatalf("links = %v, want the connected and the created label", got)
	}
	for label, workspace := range got {
		if workspace != tenantA {
			t.Errorf("link to label %d has workspace %s, want %s", label, workspace, tenantA)
		}
	}

	if err := update(&models.TaskLabelsUpdateNested{Connect: []int64{connected.ID}}); err != nil {
		t.Fatalf("re-link: %v", err)
	}
	if n := len(links()); n != 2 {
		t.Errorf("links after a re-link = %d, want 2", n)
	}

	err := update(&models.TaskLabelsUpdateNested{Connect: []int64{foreign.ID}})
	assertNestedError(t, err, models.ErrNotFound, "Labels", "connect", foreign.ID)

	if err := update(&models.TaskLabelsUpdateNested{Disconnect: []int64{connected.ID}}); err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	got = links()
	if _, kept := got[connected.ID]; kept || len(got) != 1 {
		t.Errorf("links after disconnect = %v, want the created label alone", got)
	}

	if err := update(&models.TaskLabelsUpdateNested{Clear: true}); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if n := len(links()); n != 0 {
		t.Errorf("links after clear = %d, want 0", n)
	}
}

// The parent write takes the caller's selection, and the executors read the
// parent's key columns off what it returns. A narrowed selection must not change
// what is written (PRD §9.6).
func TestWithRelated_NarrowedSelectionStillWritesTheEdge(t *testing.T) {
	ctx := context.Background()
	selections := []struct {
		name  string
		cycle *models.CycleFieldOptions
		task  *models.TaskFieldOptions
	}{
		{name: "one other column", cycle: &models.CycleFieldOptions{Name: true}, task: &models.TaskFieldOptions{Title: true}},
		{name: "nothing selected", cycle: &models.CycleFieldOptions{}, task: &models.TaskFieldOptions{}},
	}
	for _, sel := range selections {
		t.Run(sel.name, func(t *testing.T) {
			resetDB(t)
			env := newTenantEnv(t)
			cycle := mkCycle(t, env.clientA, "c")
			task := mkTask(t, env.clientA, "t")
			narrow := func(o *models.CallOptions[models.CycleFieldOptions]) { o.FieldOptions = sel.cycle }
			update := func(block *models.CycleTasksUpdateNested) error {
				_, err := env.clientA.Cycles().UpdateWithRelated(ctx, cycle.ID, &models.UpdateCycleWithRelatedInput{Tasks: block}, narrow)
				return err
			}

			if err := update(&models.CycleTasksUpdateNested{Connect: []int64{task.ID}}); err != nil {
				t.Fatalf("connect: %v", err)
			}
			assertTask(t, task.ID, taskRow{workspace: tenantA, cycle: &cycle.ID})

			if err := update(&models.CycleTasksUpdateNested{Disconnect: []int64{task.ID}}); err != nil {
				t.Fatalf("disconnect: %v", err)
			}
			assertTask(t, task.ID, taskRow{workspace: tenantA})

			if err := update(&models.CycleTasksUpdateNested{Connect: []int64{task.ID}}); err != nil {
				t.Fatalf("reconnect: %v", err)
			}
			if err := update(&models.CycleTasksUpdateNested{Clear: true}); err != nil {
				t.Fatalf("clear: %v", err)
			}
			assertTask(t, task.ID, taskRow{workspace: tenantA})

			if err := update(&models.CycleTasksUpdateNested{Create: []*models.CycleTasksCreateInput{{Title: "made"}}}); err != nil {
				t.Fatalf("create: %v", err)
			}

			label := mkLabel(t, env.clientA, "l")
			labels := func(block *models.TaskLabelsUpdateNested) error {
				_, err := env.clientA.Tasks().UpdateWithRelated(ctx, task.ID, &models.UpdateTaskWithRelatedInput{Labels: block},
					func(o *models.CallOptions[models.TaskFieldOptions]) { o.FieldOptions = sel.task })
				return err
			}
			count := func() (n int) {
				t.Helper()
				if err := testPool.QueryRow(ctx, "SELECT count(*) FROM task_labels WHERE task_id = $1", task.ID).Scan(&n); err != nil {
					t.Fatalf("count links: %v", err)
				}
				return n
			}
			if err := labels(&models.TaskLabelsUpdateNested{Connect: []int64{label.ID}}); err != nil {
				t.Fatalf("link: %v", err)
			}
			if n := count(); n != 1 {
				t.Fatalf("links after connect = %d, want 1", n)
			}
			if err := labels(&models.TaskLabelsUpdateNested{Disconnect: []int64{label.ID}}); err != nil {
				t.Fatalf("unlink: %v", err)
			}
			if n := count(); n != 0 {
				t.Errorf("links after disconnect = %d, want 0", n)
			}
			if err := labels(&models.TaskLabelsUpdateNested{Connect: []int64{label.ID}}); err != nil {
				t.Fatalf("relink: %v", err)
			}
			if err := labels(&models.TaskLabelsUpdateNested{Clear: true}); err != nil {
				t.Fatalf("clear links: %v", err)
			}
			if n := count(); n != 0 {
				t.Errorf("links after clear = %d, want 0", n)
			}
		})
	}
}

// A junction column the parent does not supply comes off the target, on both
// verbs that end in the link step.
func TestUpdateWithRelated_LinkStepReadsTheTargetsReferencedColumn(t *testing.T) {
	resetDB(t)
	env := newTenantEnv(t)
	ctx := context.Background()

	task := mkTask(t, env.clientA, "t")
	badge, err := env.clientA.Badges().Create(ctx, &models.CreateBadgeInput{Code: "connected"})
	if err != nil {
		t.Fatalf("create badge: %v", err)
	}

	if _, err := env.clientA.Tasks().UpdateWithRelated(ctx, task.ID, &models.UpdateTaskWithRelatedInput{
		Badges: &models.TaskBadgesUpdateNested{
			Connect: []int64{badge.ID},
			Create:  []*models.CreateBadgeInput{{Code: "created"}},
		},
	}); err != nil {
		t.Fatalf("link: %v", err)
	}

	rows, err := testPool.Query(ctx, "SELECT badge_code FROM task_badges WHERE task_id = $1 ORDER BY badge_code", task.ID)
	if err != nil {
		t.Fatalf("read links: %v", err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			t.Fatalf("scan link: %v", err)
		}
		got = append(got, code)
	}
	if want := []string{"connected", "created"}; !slices.Equal(got, want) {
		t.Errorf("linked codes = %q, want %q", got, want)
	}
}

// Two shared tables agree on the junction's tenant column and the resolver does
// not, so no comparison before the link step sees it and the link row is bound
// to two keys. `create` and `connect` alike reach `link` (PRD §9.9.6).
func TestUpdateWithRelated_LinkTenantMismatchIsAlreadyRelated(t *testing.T) {
	resetDB(t)
	env := newTenantEnv(t)
	ctx := context.Background()

	template, err := env.clientA.Templates().Create(ctx, &models.CreateTemplateInput{OwnerWorkspace: tenantB, Name: "b's"})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	stamp, err := env.clientA.Stamps().Create(ctx, &models.CreateStampInput{OwnerWorkspace: tenantB, Name: "b's"})
	if err != nil {
		t.Fatalf("create stamp: %v", err)
	}

	_, err = env.clientA.Templates().UpdateWithRelated(ctx, template.ID, &models.UpdateTemplateWithRelatedInput{
		Stamps: &models.TemplateStampsUpdateNested{Connect: []int64{stamp.ID}},
	})
	assertNestedError(t, err, models.ErrAlreadyRelated, "Stamps", "link", nil)

	_, err = env.clientA.Templates().UpdateWithRelated(ctx, template.ID, &models.UpdateTemplateWithRelatedInput{
		Stamps: &models.TemplateStampsUpdateNested{Create: []*models.CreateStampInput{{OwnerWorkspace: tenantB, Name: "created"}}},
	})
	assertNestedError(t, err, models.ErrAlreadyRelated, "Stamps", "link", nil)
}

// `connect` compares a shared column before the link step, so under a shared
// parent a target of the resolved tenant stops there.
func TestUpdateWithRelated_ConnectUnderSharedParentMismatchIsAlreadyRelated(t *testing.T) {
	resetDB(t)
	env := newTenantEnv(t)
	ctx := context.Background()

	template, err := env.clientA.Templates().Create(ctx, &models.CreateTemplateInput{OwnerWorkspace: tenantB, Name: "b's"})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	label := mkLabel(t, env.clientA, "a's")

	_, err = env.clientA.Templates().UpdateWithRelated(ctx, template.ID, &models.UpdateTemplateWithRelatedInput{
		Labels: &models.TemplateLabelsUpdateNested{Connect: []int64{label.ID}},
	})
	assertNestedError(t, err, models.ErrAlreadyRelated, "Labels", "connect", label.ID)
}

// A created target is compared on a shared column as a connected one is, and is
// refused before the link step with no id, since it never existed for the caller
// (PRD §9.9.6). It covers both ways to disagree: tenancy setting the target under
// a shared parent, and a caller's value under SkipTenancy.
func TestUpdateWithRelated_CreateSharedJunctionColumnMismatchIsAlreadyRelated(t *testing.T) {
	resetDB(t)
	env := newTenantEnv(t)
	ctx := context.Background()

	count := func(table string) int {
		t.Helper()
		var n int
		if err := testPool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		return n
	}
	assertRefused := func(t *testing.T, err error) {
		t.Helper()
		assertNestedError(t, err, models.ErrAlreadyRelated, "Labels", "create", nil)
		if nested, ok := errors.AsType[*models.NestedMutationError](err); ok && nested.ID != nil {
			t.Errorf("error names %v, want no id", nested.ID)
		}
		if got := count("labels"); got != 0 {
			t.Errorf("labels after the refusal = %d, want 0", got)
		}
		if got := count("task_labels") + count("template_labels"); got != 0 {
			t.Errorf("links after the refusal = %d, want 0", got)
		}
	}

	template, err := env.clientA.Templates().Create(ctx, &models.CreateTemplateInput{OwnerWorkspace: tenantB, Name: "b's"})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	_, err = env.clientA.Templates().UpdateWithRelated(ctx, template.ID, &models.UpdateTemplateWithRelatedInput{
		Labels: &models.TemplateLabelsUpdateNested{Create: []*models.CreateLabelInput{{Name: "set by tenancy"}}},
	})
	assertRefused(t, err)

	task := mkTask(t, env.clientA, "t")
	_, err = env.clientA.Tasks().UpdateWithRelated(ctx, task.ID, &models.UpdateTaskWithRelatedInput{
		Labels: &models.TaskLabelsUpdateNested{Create: []*models.CreateLabelInput{
			{WorkspaceID: omittable.Set(tenantB), Name: "set by the caller"},
		}},
	}, skipTenancy)
	assertRefused(t, err)

	// The check reads the created row's own value: a match still links.
	if _, err := env.clientA.Tasks().UpdateWithRelated(ctx, task.ID, &models.UpdateTaskWithRelatedInput{
		Labels: &models.TaskLabelsUpdateNested{Create: []*models.CreateLabelInput{{Name: "matching"}}},
	}); err != nil {
		t.Fatalf("create matching label: %v", err)
	}
	if got := count("task_labels"); got != 1 {
		t.Errorf("links after a matching create = %d, want 1", got)
	}
}

// The visibility read cannot hide the target under SkipTenancy, so without the
// check the link row reaches the junction's foreign key, or, where the database
// does not enforce one, links another workspace's label (PRD §9.9.6).
func TestUpdateWithRelated_ConnectSharedJunctionColumnMismatchIsAlreadyRelated(t *testing.T) {
	resetDB(t)
	env := newTenantEnv(t)
	ctx := context.Background()

	task := mkTask(t, env.clientA, "t")
	own := mkLabel(t, env.clientA, "own")
	foreign := mkLabel(t, env.clientB, "foreign")

	links := func() int {
		t.Helper()
		var n int
		if err := testPool.QueryRow(ctx, "SELECT count(*) FROM task_labels WHERE task_id = $1", task.ID).Scan(&n); err != nil {
			t.Fatalf("count links: %v", err)
		}
		return n
	}

	// `own` first: the refusal comes before the link step, so neither links.
	_, err := env.clientA.Tasks().UpdateWithRelated(ctx, task.ID, &models.UpdateTaskWithRelatedInput{
		Labels: &models.TaskLabelsUpdateNested{Connect: []int64{own.ID, foreign.ID}},
	}, skipTenancy)
	assertNestedError(t, err, models.ErrAlreadyRelated, "Labels", "connect", foreign.ID)
	if got := links(); got != 0 {
		t.Errorf("links after the refusal = %d, want 0", got)
	}

	// The check reads the target's own value: a match still links.
	if _, err := env.clientA.Tasks().UpdateWithRelated(ctx, task.ID, &models.UpdateTaskWithRelatedInput{
		Labels: &models.TaskLabelsUpdateNested{Connect: []int64{own.ID}},
	}, skipTenancy); err != nil {
		t.Fatalf("connect own label: %v", err)
	}
	if got := links(); got != 1 {
		t.Errorf("links after connect = %d, want 1", got)
	}
}

// mkLinkedTemplate writes a template owned by tenantB with one note and one
// label on it, past the client, so a test starts from rows only tenantB can reach.
func mkLinkedTemplate(t *testing.T, c *models.Client) (template, note, label int64) {
	t.Helper()
	ctx := context.Background()
	created, err := c.Templates().Create(ctx, &models.CreateTemplateInput{OwnerWorkspace: tenantB, Name: "b's"})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	template = created.ID
	if err := testPool.QueryRow(ctx,
		"INSERT INTO template_notes (workspace_id, template_id, body) VALUES ($1, $2, 'n') RETURNING id",
		tenantB, template).Scan(&note); err != nil {
		t.Fatalf("seed note: %v", err)
	}
	if err := testPool.QueryRow(ctx,
		"INSERT INTO labels (workspace_id, name) VALUES ($1, 'l') RETURNING id", tenantB).Scan(&label); err != nil {
		t.Fatalf("seed label: %v", err)
	}
	if _, err := testPool.Exec(ctx,
		"INSERT INTO template_labels (workspace_id, template_id, label_id) VALUES ($1, $2, $3)",
		tenantB, template, label); err != nil {
		t.Fatalf("seed link: %v", err)
	}
	return template, note, label
}

// linkedRows counts what is still on template's two unlinkable edges.
func linkedRows(t *testing.T, template int64) (notes, labels int) {
	t.Helper()
	ctx := context.Background()
	if err := testPool.QueryRow(ctx, "SELECT count(*) FROM template_notes WHERE template_id = $1", template).Scan(&notes); err != nil {
		t.Fatalf("count notes: %v", err)
	}
	if err := testPool.QueryRow(ctx, "SELECT count(*) FROM template_labels WHERE template_id = $1", template).Scan(&labels); err != nil {
		t.Fatalf("count labels: %v", err)
	}
	return notes, labels
}

// A shared parent's column and the resolver disagree on the tenant column in
// the key. Each unlink fails loud, where it would otherwise match nothing and
// report success (PRD §9.9.8).
func TestUpdateWithRelated_UnlinkTenantMismatchIsAlreadyRelated(t *testing.T) {
	tests := []struct {
		name, edge, verb string
		block            func(note, label int64) *models.UpdateTemplateWithRelatedInput
	}{
		{
			name: "o2m disconnect", edge: "TemplateNotes", verb: "disconnect",
			block: func(note, _ int64) *models.UpdateTemplateWithRelatedInput {
				return &models.UpdateTemplateWithRelatedInput{TemplateNotes: &models.TemplateTemplateNotesUpdateNested{Disconnect: []int64{note}}}
			},
		},
		{
			name: "o2m disconnect naming no row", edge: "TemplateNotes", verb: "disconnect",
			block: func(_, _ int64) *models.UpdateTemplateWithRelatedInput {
				return &models.UpdateTemplateWithRelatedInput{TemplateNotes: &models.TemplateTemplateNotesUpdateNested{Disconnect: []int64{-1}}}
			},
		},
		{
			name: "o2m clear", edge: "TemplateNotes", verb: "clear",
			block: func(_, _ int64) *models.UpdateTemplateWithRelatedInput {
				return &models.UpdateTemplateWithRelatedInput{TemplateNotes: &models.TemplateTemplateNotesUpdateNested{Clear: true}}
			},
		},
		{
			name: "m2m disconnect", edge: "Labels", verb: "disconnect",
			block: func(_, label int64) *models.UpdateTemplateWithRelatedInput {
				return &models.UpdateTemplateWithRelatedInput{Labels: &models.TemplateLabelsUpdateNested{Disconnect: []int64{label}}}
			},
		},
		{
			name: "m2m disconnect naming no row", edge: "Labels", verb: "disconnect",
			block: func(_, _ int64) *models.UpdateTemplateWithRelatedInput {
				return &models.UpdateTemplateWithRelatedInput{Labels: &models.TemplateLabelsUpdateNested{Disconnect: []int64{-1}}}
			},
		},
		{
			name: "m2m clear", edge: "Labels", verb: "clear",
			block: func(_, _ int64) *models.UpdateTemplateWithRelatedInput {
				return &models.UpdateTemplateWithRelatedInput{Labels: &models.TemplateLabelsUpdateNested{Clear: true}}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetDB(t)
			env := newTenantEnv(t)
			template, note, label := mkLinkedTemplate(t, env.clientB)

			_, err := env.clientA.Templates().UpdateWithRelated(context.Background(), template, tt.block(note, label))
			assertNestedError(t, err, models.ErrAlreadyRelated, tt.edge, tt.verb, nil)

			if notes, labels := linkedRows(t, template); notes != 1 || labels != 1 {
				t.Errorf("rows left = %d notes, %d labels, want 1 of each", notes, labels)
			}
		})
	}
}

// The tenant the key holds is the resolved one, or tenancy is off for the
// call: both unlink, and neither reaches the comparison's refusal.
func TestUpdateWithRelated_UnlinkOnTheParentsTenantWrites(t *testing.T) {
	type option = func(*models.CallOptions[models.TemplateFieldOptions])
	explicit := func(o *models.CallOptions[models.TemplateFieldOptions]) { o.Tenant = new(tenantB) }
	tests := []struct {
		name   string
		client func(*tenantEnv) *models.Client
		opts   []option
	}{
		{name: "the resolved tenant", client: func(e *tenantEnv) *models.Client { return e.clientB }},
		{name: "SkipTenancy", client: func(e *tenantEnv) *models.Client { return e.clientA }, opts: []option{skipTenancy[models.TemplateFieldOptions]}},
		{name: "an explicit tenant", client: func(e *tenantEnv) *models.Client { return e.clientA }, opts: []option{explicit}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetDB(t)
			env := newTenantEnv(t)
			ctx := context.Background()

			template, note, label := mkLinkedTemplate(t, env.clientB)
			if _, err := tt.client(env).Templates().UpdateWithRelated(ctx, template, &models.UpdateTemplateWithRelatedInput{
				TemplateNotes: &models.TemplateTemplateNotesUpdateNested{Disconnect: []int64{note}},
				Labels:        &models.TemplateLabelsUpdateNested{Disconnect: []int64{label}},
			}, tt.opts...); err != nil {
				t.Fatalf("disconnect: %v", err)
			}
			if notes, labels := linkedRows(t, template); notes != 0 || labels != 0 {
				t.Errorf("rows left after disconnect = %d notes, %d labels, want none", notes, labels)
			}

			template, _, _ = mkLinkedTemplate(t, env.clientB)
			if _, err := tt.client(env).Templates().UpdateWithRelated(ctx, template, &models.UpdateTemplateWithRelatedInput{
				TemplateNotes: &models.TemplateTemplateNotesUpdateNested{Clear: true},
				Labels:        &models.TemplateLabelsUpdateNested{Clear: true},
			}, tt.opts...); err != nil {
				t.Fatalf("clear: %v", err)
			}
			if notes, labels := linkedRows(t, template); notes != 0 || labels != 0 {
				t.Errorf("rows left after clear = %d notes, %d labels, want none", notes, labels)
			}
		})
	}
}

// With the tenants in agreement a disconnect naming no row of this parent is a
// miss, and a miss stays a no-op (PRD §9.9.6).
func TestUpdateWithRelated_DisconnectMissOnTheParentsTenantIsANoOp(t *testing.T) {
	resetDB(t)
	env := newTenantEnv(t)
	template, _, _ := mkLinkedTemplate(t, env.clientB)

	if _, err := env.clientB.Templates().UpdateWithRelated(context.Background(), template, &models.UpdateTemplateWithRelatedInput{
		TemplateNotes: &models.TemplateTemplateNotesUpdateNested{Disconnect: []int64{-1}},
		Labels:        &models.TemplateLabelsUpdateNested{Disconnect: []int64{-1}},
	}); err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	if notes, labels := linkedRows(t, template); notes != 1 || labels != 1 {
		t.Errorf("rows left = %d notes, %d labels, want 1 of each", notes, labels)
	}
}

// The executor has no branch detection, so the insert branch refuses the
// mismatch as the conflict branch does, and the new parent rolls back.
func TestUpsertWithRelated_UnlinkTenantMismatchRollsBackTheInsert(t *testing.T) {
	resetDB(t)
	env := newTenantEnv(t)
	ctx := context.Background()

	_, err := env.clientA.Templates().UpsertWithRelated(ctx, &models.UpsertTemplateWithRelatedInput{
		Template:      models.CreateTemplateInput{OwnerWorkspace: tenantB, Name: "fresh"},
		TemplateNotes: &models.TemplateTemplateNotesUpdateNested{Clear: true},
	}, models.TemplateConflictPK)
	assertNestedError(t, err, models.ErrAlreadyRelated, "TemplateNotes", "clear", nil)

	var templates int
	if err := testPool.QueryRow(ctx, "SELECT count(*) FROM templates").Scan(&templates); err != nil {
		t.Fatalf("count templates: %v", err)
	}
	if templates != 0 {
		t.Errorf("templates = %d, want the insert rolled back", templates)
	}
}

// The same mismatch on a one-column key is the same error (PRD §9.9.8).
func TestCreateWithRelated_SingleColumnTenantKeyMismatchIsAlreadyRelated(t *testing.T) {
	resetDB(t)
	env := newTenantEnv(t)
	ctx := context.Background()

	workspace, err := env.clientA.Workspaces().Create(ctx, &models.CreateWorkspaceInput{ID: omittable.Set(tenantB), Name: "b"})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	_, err = env.clientA.Workspaces().UpdateWithRelated(ctx, workspace.ID, &models.UpdateWorkspaceWithRelatedInput{
		WorkspaceNotes: &models.WorkspaceWorkspaceNotesUpdateNested{
			Create: []*models.WorkspaceWorkspaceNotesCreateInput{{Body: "n"}},
		},
	})
	assertNestedError(t, err, models.ErrAlreadyRelated, "WorkspaceNotes", "create", nil)
}

// A key is unparented only when every free member is NULL, the state the
// adoption's guard admits, so a partly NULL key is refused rather than adopted.
func TestUpdateWithRelated_PartlyNullKeyIsAlreadyRelated(t *testing.T) {
	resetDB(t)
	env := newTenantEnv(t)
	ctx := context.Background()

	zone, err := env.clientA.Zones().Create(ctx, &models.CreateZoneInput{Region: "eu"})
	if err != nil {
		t.Fatalf("create zone: %v", err)
	}
	site, err := env.clientA.Sites().Create(ctx, &models.CreateSiteInput{ZoneRegion: omittable.Set(new("eu"))})
	if err != nil {
		t.Fatalf("create site: %v", err)
	}

	_, err = env.clientA.Zones().UpdateWithRelated(ctx, zone.ID, &models.UpdateZoneWithRelatedInput{
		Sites: &models.ZoneSitesUpdateNested{Connect: []int64{site.ID}},
	})
	assertNestedError(t, err, models.ErrAlreadyRelated, "Sites", "connect", site.ID)

	var id *int64
	if err := testPool.QueryRow(ctx, "SELECT zone_id FROM sites WHERE id = $1", site.ID).Scan(&id); err != nil {
		t.Fatalf("read site: %v", err)
	}
	if id != nil {
		t.Errorf("zone_id = %d, want NULL", *id)
	}
}
