package tests

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
)

// nestedClients is one client per tenant.
func nestedClients(t *testing.T) (a, b *models.Client) {
	t.Helper()
	resetDB(t)
	return newEnv(t, withCache()).client, newEnv(t, withCache(), withResolver(staticResolver(tenantB))).client
}

// readTask reads a tasks row past the client, so no tenant filter or cache
// stands between a test and what the writes left behind.
func readTask(t *testing.T, id int64) (uuid.UUID, *int64) {
	t.Helper()
	var workspace uuid.UUID
	var cycle *int64
	err := testDB.QueryRow("SELECT workspace_id, cycle_id FROM tasks WHERE id = ?", id).Scan(&workspace, &cycle)
	if err != nil {
		t.Fatalf("read task %d: %v", id, err)
	}
	return workspace, cycle
}

func onlyTask(t *testing.T, c *models.Client) *models.Task {
	t.Helper()
	tasks, err := c.Tasks().GetMany(context.Background(), &models.GetTasksInput{Filter: &models.TaskFilter{}})
	if err != nil {
		t.Fatalf("GetMany tasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks = %d, want 1", len(tasks))
	}
	return tasks[0]
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
	ctx := context.Background()

	t.Run("under tenancy", func(t *testing.T) {
		a, _ := nestedClients(t)
		cycle, err := a.Cycles().CreateWithRelated(ctx, &models.CreateCycleWithRelatedInput{
			Cycle: models.CreateCycleInput{Name: "c"},
			Tasks: &models.CycleTasksCreateNested{Create: []*models.CycleTasksCreateInput{{Title: "t"}}},
		})
		if err != nil {
			t.Fatalf("CreateWithRelated: %v", err)
		}
		workspace, got := readTask(t, onlyTask(t, a).ID)
		if workspace != tenantA || got == nil || *got != cycle.ID {
			t.Errorf("task key = %v, %v, want %v, %d", workspace, got, tenantA, cycle.ID)
		}
	})

	t.Run("under SkipTenancy the child takes the parent's workspace", func(t *testing.T) {
		a, b := nestedClients(t)
		if _, err := a.Cycles().CreateWithRelated(ctx, &models.CreateCycleWithRelatedInput{
			Cycle: models.CreateCycleInput{Name: "c", WorkspaceID: omittable.Set(tenantB)},
			Tasks: &models.CycleTasksCreateNested{Create: []*models.CycleTasksCreateInput{{Title: "t"}}},
		}, skipTenancy[models.CycleFieldOptions]); err != nil {
			t.Fatalf("CreateWithRelated: %v", err)
		}
		if workspace, _ := readTask(t, onlyTask(t, b).ID); workspace != tenantB {
			t.Errorf("task workspace = %v, want the parent's %v", workspace, tenantB)
		}
	})
}

// The parent is a shared table, so tenancy does not guard the column the key
// copies. The child's tenant comes from the parent and from the resolver, and
// the two disagree.
func TestUpdateWithRelated_TenancySetTenantMismatchIsAlreadyRelated(t *testing.T) {
	a, b := nestedClients(t)
	ctx := context.Background()

	template, err := a.Templates().Create(ctx, &models.CreateTemplateInput{OwnerWorkspace: tenantB, Name: "b's"})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	block := &models.UpdateTemplateWithRelatedInput{
		TemplateSteps: &models.TemplateTemplateStepsUpdateNested{
			Create: []*models.TemplateTemplateStepsCreateInput{{Title: "step"}},
		},
	}

	_, err = a.Templates().UpdateWithRelated(ctx, template.ID, block)
	assertNestedError(t, err, models.ErrAlreadyRelated, "TemplateSteps", "create", nil)

	var steps int
	if err := testDB.QueryRow("SELECT count(*) FROM template_steps").Scan(&steps); err != nil {
		t.Fatalf("count steps: %v", err)
	}
	if steps != 0 {
		t.Errorf("template_steps rows = %d, want none", steps)
	}

	if _, err := b.Templates().UpdateWithRelated(ctx, template.ID, block); err != nil {
		t.Fatalf("the owner's own create: %v", err)
	}
}

// Two shared parents agree on the junction's tenant column and the resolver
// does not, so the link row is bound to two keys.
func TestUpdateWithRelated_LinkTenantMismatchIsAlreadyRelated(t *testing.T) {
	a, _ := nestedClients(t)
	ctx := context.Background()

	template, err := a.Templates().Create(ctx, &models.CreateTemplateInput{OwnerWorkspace: tenantB, Name: "b's"})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	stamp, err := a.Stamps().Create(ctx, &models.CreateStampInput{OwnerWorkspace: tenantB, Name: "b's"})
	if err != nil {
		t.Fatalf("create stamp: %v", err)
	}

	_, err = a.Templates().UpdateWithRelated(ctx, template.ID, &models.UpdateTemplateWithRelatedInput{
		Stamps: &models.TemplateStampsUpdateNested{Connect: []int64{stamp.ID}},
	})
	assertNestedError(t, err, models.ErrAlreadyRelated, "Stamps", "link", nil)

	_, err = a.Templates().UpdateWithRelated(ctx, template.ID, &models.UpdateTemplateWithRelatedInput{
		Stamps: &models.TemplateStampsUpdateNested{Create: []*models.CreateStampInput{{OwnerWorkspace: tenantB, Name: "created"}}},
	})
	assertNestedError(t, err, models.ErrAlreadyRelated, "Stamps", "link", nil)
}

// Under a shared parent, tenancy sets a target's shared junction column from
// the resolver, so `connect` refuses it before the link step.
func TestUpdateWithRelated_ConnectUnderSharedParentMismatchIsAlreadyRelated(t *testing.T) {
	a, _ := nestedClients(t)
	ctx := context.Background()

	template, err := a.Templates().Create(ctx, &models.CreateTemplateInput{OwnerWorkspace: tenantB, Name: "b's"})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	label, err := a.Labels().Create(ctx, &models.CreateLabelInput{Name: "a's"})
	if err != nil {
		t.Fatalf("create label: %v", err)
	}

	_, err = a.Templates().UpdateWithRelated(ctx, template.ID, &models.UpdateTemplateWithRelatedInput{
		Labels: &models.TemplateLabelsUpdateNested{Connect: []int64{label.ID}},
	})
	assertNestedError(t, err, models.ErrAlreadyRelated, "Labels", "connect", label.ID)
}

// The same mismatch on a one-column key is the same error (PRD §9.9.8).
func TestUpdateWithRelated_SingleColumnTenantKeyMismatchIsAlreadyRelated(t *testing.T) {
	a, _ := nestedClients(t)
	ctx := context.Background()

	workspace, err := a.Workspaces().Create(ctx, &models.CreateWorkspaceInput{ID: tenantB, Name: "b"})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	_, err = a.Workspaces().UpdateWithRelated(ctx, workspace.ID, &models.UpdateWorkspaceWithRelatedInput{
		WorkspaceNotes: &models.WorkspaceWorkspaceNotesUpdateNested{
			Create: []*models.WorkspaceWorkspaceNotesCreateInput{{Body: "n"}},
		},
	})
	assertNestedError(t, err, models.ErrAlreadyRelated, "WorkspaceNotes", "create", nil)
}

func TestUpdateWithRelated_CrossTenantConnectIsNotFoundBeforeAnyWrite(t *testing.T) {
	a, b := nestedClients(t)
	ctx := context.Background()

	cycle, err := a.Cycles().Create(ctx, &models.CreateCycleInput{Name: "a's"})
	if err != nil {
		t.Fatalf("create cycle: %v", err)
	}
	task, err := b.Tasks().Create(ctx, &models.CreateTaskInput{Title: "b's"})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	_, err = a.Cycles().UpdateWithRelated(ctx, cycle.ID, &models.UpdateCycleWithRelatedInput{
		Tasks: &models.CycleTasksUpdateNested{Connect: []int64{task.ID}},
	})
	assertNestedError(t, err, models.ErrNotFound, "Tasks", "connect", task.ID)

	workspace, got := readTask(t, task.ID)
	if workspace != tenantB || got != nil {
		t.Errorf("task key = %v, %v, want %v, NULL", workspace, got, tenantB)
	}
}

// Past the tenant filter, the tenant column is one more pinned member.
func TestUpdateWithRelated_PinnedTenantMismatchIsAlreadyRelated(t *testing.T) {
	a, b := nestedClients(t)
	ctx := context.Background()

	cycle, err := a.Cycles().Create(ctx, &models.CreateCycleInput{Name: "a's"})
	if err != nil {
		t.Fatalf("create cycle: %v", err)
	}
	task, err := b.Tasks().Create(ctx, &models.CreateTaskInput{Title: "b's"})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	_, err = a.Cycles().UpdateWithRelated(ctx, cycle.ID, &models.UpdateCycleWithRelatedInput{
		Tasks: &models.CycleTasksUpdateNested{Connect: []int64{task.ID}},
	}, skipTenancy[models.CycleFieldOptions])
	assertNestedError(t, err, models.ErrAlreadyRelated, "Tasks", "connect", task.ID)

	workspace, got := readTask(t, task.ID)
	if workspace != tenantB || got != nil {
		t.Errorf("task key = %v, %v, want %v, NULL", workspace, got, tenantB)
	}
}
