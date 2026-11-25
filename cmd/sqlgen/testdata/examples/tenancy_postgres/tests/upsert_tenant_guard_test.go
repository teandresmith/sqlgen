package tests

import (
	"context"
	"errors"
	"testing"

	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy_postgres/models"
)

// An upsert resolved to one tenant must not update another tenant's row when
// its conflict target matches that row (PRD §29.4.2). The conflict is guarded
// away and reported as ErrNotFound, the answer a row the caller cannot see
// gets everywhere else (§29.4.2a). line_items covers a primary-key conflict
// with the tenant outside the key; tasks.code a UNIQUE without the tenant
// column.

func skipTenancyLineItems(o *models.CallOptions[models.LineItemFieldOptions]) { o.SkipTenancy = true }

func skipTenancyTasks(o *models.CallOptions[models.TaskFieldOptions]) { o.SkipTenancy = true }

func noLineItemFields(o *models.CallOptions[models.LineItemFieldOptions]) {
	o.FieldOptions = &models.LineItemFieldOptions{}
}

func noTaskFields(o *models.CallOptions[models.TaskFieldOptions]) {
	o.FieldOptions = &models.TaskFieldOptions{}
}

func guardEnvs(t *testing.T) (a, b *models.Client) {
	t.Helper()
	resetDB(t)
	env := newTenantEnv(t)
	return env.clientA, env.clientB
}

func seedForeignLineItem(t *testing.T, b *models.Client) models.LineItemPK {
	t.Helper()
	row, err := b.LineItems().Create(context.Background(), &models.CreateLineItemInput{OrderID: 1, ProductID: 1, Quantity: 7})
	if err != nil {
		t.Fatalf("seed B line item: %v", err)
	}
	return models.LineItemPK{OrderID: row.OrderID, ProductID: row.ProductID}
}

func seedCodedTask(t *testing.T, b *models.Client) *models.Task {
	t.Helper()
	row, err := b.Tasks().Create(context.Background(), &models.CreateTaskInput{Title: "b-title", Code: omittable.Set(new("b-code"))})
	if err != nil {
		t.Fatalf("seed task: %v", err)
	}
	return row
}

func assertLineItemUnchanged(t *testing.T, a *models.Client, pk models.LineItemPK) {
	t.Helper()
	got, err := a.LineItems().Get(context.Background(), pk, skipTenancyLineItems)
	if err != nil {
		t.Fatalf("read B line item: %v", err)
	}
	if got.WorkspaceID != tenantB || got.Quantity != 7 {
		t.Errorf("B line item = {workspace %v, quantity %d}, want {%v, 7}: another tenant's upsert wrote it", got.WorkspaceID, got.Quantity, tenantB)
	}
}

func assertTaskUnchanged(t *testing.T, a *models.Client, id int64) {
	t.Helper()
	got, err := a.Tasks().Get(context.Background(), id, skipTenancyTasks)
	if err != nil {
		t.Fatalf("read B task: %v", err)
	}
	if got.WorkspaceID != tenantB || got.Title != "b-title" {
		t.Errorf("B task = {workspace %v, title %q}, want {%v, \"b-title\"}: another tenant's upsert wrote it", got.WorkspaceID, got.Title, tenantB)
	}
}

func countTasks(t *testing.T, a *models.Client) int64 {
	t.Helper()
	n, err := a.Tasks().Count(context.Background(), nil, skipTenancyTasks)
	if err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	return n
}

func TestUpsertTenantGuard_PKConflictWithForeignRow(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts []func(*models.CallOptions[models.LineItemFieldOptions])
	}{
		{name: "read back"},
		{name: "no fields selected", opts: []func(*models.CallOptions[models.LineItemFieldOptions]){noLineItemFields}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := guardEnvs(t)
			pk := seedForeignLineItem(t, b)

			_, err := a.LineItems().Upsert(context.Background(), &models.CreateLineItemInput{OrderID: pk.OrderID, ProductID: pk.ProductID, Quantity: 99}, models.LineItemConflictPK, tc.opts...)
			if !errors.Is(err, models.ErrNotFound) {
				t.Errorf("Upsert onto B's key as A: err = %v, want ErrNotFound", err)
			}
			assertLineItemUnchanged(t, a, pk)
		})
	}
}

func TestUpsertTenantGuard_UniqueConflictWithForeignRow(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts []func(*models.CallOptions[models.TaskFieldOptions])
	}{
		{name: "read back"},
		{name: "no fields selected", opts: []func(*models.CallOptions[models.TaskFieldOptions]){noTaskFields}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := guardEnvs(t)
			foreign := seedCodedTask(t, b)

			_, err := a.Tasks().Upsert(context.Background(), &models.CreateTaskInput{Title: "pwned", Code: omittable.Set(new("b-code"))}, models.TaskConflictCode, tc.opts...)
			if !errors.Is(err, models.ErrNotFound) {
				t.Errorf("Upsert onto B's code as A: err = %v, want ErrNotFound", err)
			}
			assertTaskUnchanged(t, a, foreign.ID)
		})
	}
}

// One foreign-tenant conflict fails the whole batch before anything is written.
func TestUpsertManyTenantGuard_PKConflictWritesNothing(t *testing.T) {
	a, b := guardEnvs(t)
	pk := seedForeignLineItem(t, b)
	ctx := context.Background()

	_, err := a.LineItems().UpsertMany(ctx, []*models.CreateLineItemInput{
		{OrderID: 2, ProductID: 2, Quantity: 1},
		{OrderID: pk.OrderID, ProductID: pk.ProductID, Quantity: 99},
	}, models.LineItemConflictPK, noLineItemFields)
	if !errors.Is(err, models.ErrNotFound) {
		t.Errorf("UpsertMany with B's key as A: err = %v, want ErrNotFound", err)
	}
	assertLineItemUnchanged(t, a, pk)
	if _, err := a.LineItems().Get(ctx, models.LineItemPK{OrderID: 2, ProductID: 2}); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("A's own row from the failed batch: Get err = %v, want ErrNotFound (nothing written)", err)
	}
}

func TestUpsertManyTenantGuard_UniqueConflictWritesNothing(t *testing.T) {
	a, b := guardEnvs(t)
	foreign := seedCodedTask(t, b)

	_, err := a.Tasks().UpsertMany(context.Background(), []*models.CreateTaskInput{
		{Title: "a-new", Code: omittable.Set(new("a-code"))},
		{Title: "pwned", Code: omittable.Set(new("b-code"))},
	}, models.TaskConflictCode)
	if !errors.Is(err, models.ErrNotFound) {
		t.Errorf("UpsertMany with B's code as A: err = %v, want ErrNotFound", err)
	}
	assertTaskUnchanged(t, a, foreign.ID)
	if n := countTasks(t, a); n != 1 {
		t.Errorf("task rows = %d, want 1 (the failed batch wrote A's new row)", n)
	}
}

// SkipTenancy keeps the cross-tenant upsert reachable (§29.4.2, §29.4.4). The
// input names A as its tenant, so only a missing guard lets it update B's row,
// whose tenant stays B.
func TestUpsertTenantGuard_SkipTenancyUpdatesForeignRow(t *testing.T) {
	a, b := guardEnvs(t)
	pk := seedForeignLineItem(t, b)
	foreign := seedCodedTask(t, b)
	ctx := context.Background()

	got, err := a.LineItems().Upsert(ctx, &models.CreateLineItemInput{OrderID: pk.OrderID, ProductID: pk.ProductID, WorkspaceID: omittable.Set(tenantA), Quantity: 99}, models.LineItemConflictPK, skipTenancyLineItems)
	if err != nil {
		t.Fatalf("SkipTenancy Upsert: %v", err)
	}
	if got.Quantity != 99 || got.WorkspaceID != tenantB {
		t.Errorf("SkipTenancy Upsert = {quantity %d, workspace %v}, want {99, %v}", got.Quantity, got.WorkspaceID, tenantB)
	}

	written, err := a.Tasks().UpsertMany(ctx, []*models.CreateTaskInput{
		{WorkspaceID: omittable.Set(tenantA), Title: "admin", Code: omittable.Set(new("b-code"))},
	}, models.TaskConflictCode, skipTenancyTasks)
	if err != nil {
		t.Fatalf("SkipTenancy UpsertMany: %v", err)
	}
	if len(written) != 1 || written[0].ID != foreign.ID || written[0].Title != "admin" || written[0].WorkspaceID != tenantB {
		t.Errorf("SkipTenancy UpsertMany = %+v, want B's task %d titled \"admin\" in B", written, foreign.ID)
	}
}

// The guard leaves a same-tenant conflict alone: it still updates.
func TestUpsertTenantGuard_SameTenantConflictUpdates(t *testing.T) {
	a, _ := guardEnvs(t)
	ctx := context.Background()
	own := seedCodedTask(t, a)

	got, err := a.Tasks().Upsert(ctx, &models.CreateTaskInput{Title: "a-updated", Code: omittable.Set(new("b-code"))}, models.TaskConflictCode)
	if err != nil {
		t.Fatalf("same-tenant Upsert: %v", err)
	}
	if got.ID != own.ID || got.Title != "a-updated" {
		t.Errorf("same-tenant Upsert = {id %d, title %q}, want {%d, \"a-updated\"}", got.ID, got.Title, own.ID)
	}
	written, err := a.Tasks().UpsertMany(ctx, []*models.CreateTaskInput{{Title: "a-many", Code: omittable.Set(new("b-code"))}}, models.TaskConflictCode)
	if err != nil {
		t.Fatalf("same-tenant UpsertMany: %v", err)
	}
	if len(written) != 1 || written[0].ID != own.ID || written[0].Title != "a-many" {
		t.Errorf("same-tenant UpsertMany = %+v, want task %d titled \"a-many\"", written, own.ID)
	}
}

// UpsertWithRelated upserts its parent through Upsert, so it takes the same guard.
func TestUpsertWithRelatedTenantGuard_UniqueConflictWithForeignRow(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts []func(*models.CallOptions[models.TaskFieldOptions])
	}{
		{name: "read back"},
		{name: "no fields selected", opts: []func(*models.CallOptions[models.TaskFieldOptions]){noTaskFields}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := guardEnvs(t)
			foreign := seedCodedTask(t, b)

			_, err := a.Tasks().UpsertWithRelated(context.Background(), &models.UpsertTaskWithRelatedInput{
				Task: models.CreateTaskInput{Title: "pwned", Code: omittable.Set(new("b-code"))},
			}, models.TaskConflictCode, tc.opts...)
			if !errors.Is(err, models.ErrNotFound) {
				t.Errorf("UpsertWithRelated onto B's code as A: err = %v, want ErrNotFound", err)
			}
			assertTaskUnchanged(t, a, foreign.ID)
		})
	}
}

// With the tenant inside the PK, a UNIQUE without it can still conflict with
// another tenant's row. The call is refused before the statement runs, so
// nothing is written even when the input PK names one of A's own rows, where
// MySQL's ON DUPLICATE KEY UPDATE would otherwise fire.
func TestUpsertTenantGuard_TenantInPKUniqueConflictWritesNothing(t *testing.T) {
	noFields := func(o *models.CallOptions[models.LedgerEntryFieldOptions]) {
		o.FieldOptions = &models.LedgerEntryFieldOptions{}
	}
	skip := func(o *models.CallOptions[models.LedgerEntryFieldOptions]) { o.SkipTenancy = true }
	for _, tc := range []struct {
		name    string
		ownsKey bool
		opts    []func(*models.CallOptions[models.LedgerEntryFieldOptions])
	}{
		{name: "new key read back"},
		{name: "new key no fields selected", opts: []func(*models.CallOptions[models.LedgerEntryFieldOptions]){noFields}},
		{name: "A's own key read back", ownsKey: true},
		{name: "A's own key no fields selected", ownsKey: true, opts: []func(*models.CallOptions[models.LedgerEntryFieldOptions]){noFields}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := guardEnvs(t)
			ctx := context.Background()
			if _, err := b.LedgerEntries().Create(ctx, &models.CreateLedgerEntryInput{WorkspaceID: tenantB, EntryID: 2, Amount: 7, Ref: omittable.Set(new("b-ref"))}); err != nil {
				t.Fatalf("seed B entry: %v", err)
			}
			if tc.ownsKey {
				if _, err := a.LedgerEntries().Create(ctx, &models.CreateLedgerEntryInput{WorkspaceID: tenantA, EntryID: 1, Amount: 1, Ref: omittable.Set(new("a-ref"))}); err != nil {
					t.Fatalf("seed A entry: %v", err)
				}
			}

			_, err := a.LedgerEntries().Upsert(ctx, &models.CreateLedgerEntryInput{WorkspaceID: tenantA, EntryID: 1, Amount: 99, Ref: omittable.Set(new("b-ref"))}, models.LedgerEntryConflictRef, tc.opts...)
			if !errors.Is(err, models.ErrNotFound) {
				t.Errorf("Upsert onto B's ref as A: err = %v, want ErrNotFound", err)
			}
			foreign, err := a.LedgerEntries().Get(ctx, models.LedgerEntryPK{WorkspaceID: tenantB, EntryID: 2}, skip)
			if err != nil {
				t.Fatalf("read B entry: %v", err)
			}
			if foreign.Amount != 7 {
				t.Errorf("B entry amount = %d, want 7: another tenant's upsert wrote it", foreign.Amount)
			}
			own, err := a.LedgerEntries().Get(ctx, models.LedgerEntryPK{WorkspaceID: tenantA, EntryID: 1})
			switch {
			case tc.ownsKey && err != nil:
				t.Fatalf("read A entry: %v", err)
			case tc.ownsKey && (own.Amount != 1 || own.Ref == nil || *own.Ref != "a-ref"):
				t.Errorf("A entry = {amount %d, ref %v}, want {1, a-ref}: the refused call wrote it", own.Amount, own.Ref)
			case !tc.ownsKey && !errors.Is(err, models.ErrNotFound):
				t.Errorf("A entry at the input key: Get err = %v, want ErrNotFound (nothing written)", err)
			}
		})
	}
}

// The check before the statement leaves a same-tenant conflict, and an input
// that leaves the target column unset, to the statement.
func TestUpsertTenantGuard_TenantInPKSameTenantAndUnsetTarget(t *testing.T) {
	a, b := guardEnvs(t)
	ctx := context.Background()
	if _, err := b.LedgerEntries().Create(ctx, &models.CreateLedgerEntryInput{WorkspaceID: tenantB, EntryID: 2, Amount: 7, Ref: omittable.Set(new("b-ref"))}); err != nil {
		t.Fatalf("seed B entry: %v", err)
	}
	if _, err := a.LedgerEntries().Create(ctx, &models.CreateLedgerEntryInput{WorkspaceID: tenantA, EntryID: 1, Amount: 1, Ref: omittable.Set(new("a-ref"))}); err != nil {
		t.Fatalf("seed A entry: %v", err)
	}

	got, err := a.LedgerEntries().Upsert(ctx, &models.CreateLedgerEntryInput{WorkspaceID: tenantA, EntryID: 1, Amount: 42, Ref: omittable.Set(new("a-ref"))}, models.LedgerEntryConflictRef)
	if err != nil {
		t.Fatalf("same-tenant Upsert: %v", err)
	}
	if got.Amount != 42 {
		t.Errorf("same-tenant Upsert amount = %d, want 42", got.Amount)
	}
	got, err = a.LedgerEntries().Upsert(ctx, &models.CreateLedgerEntryInput{WorkspaceID: tenantA, EntryID: 3, Amount: 5}, models.LedgerEntryConflictRef)
	if err != nil {
		t.Fatalf("Upsert with ref unset: %v", err)
	}
	if got.WorkspaceID != tenantA || got.Amount != 5 {
		t.Errorf("Upsert with ref unset = {workspace %v, amount %d}, want {%v, 5}", got.WorkspaceID, got.Amount, tenantA)
	}
}
