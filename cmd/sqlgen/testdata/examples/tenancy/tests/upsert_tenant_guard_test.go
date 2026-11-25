package tests

import (
	"context"
	"errors"
	"testing"

	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
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
	return newEnv(t, withResolver(staticResolver(tenantA))).client,
		newEnv(t, withResolver(staticResolver(tenantB))).client
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

// A UNIQUE beside the PK can conflict with another tenant's row that holds
// neither the input PK nor any row A owns, or with B's row while the input PK
// names one of A's rows. Either way nothing is written.
func TestUpsertTenantGuard_UniqueBesidePKConflictWithForeignRow(t *testing.T) {
	for _, tc := range []struct {
		name    string
		ownsKey bool
		opts    []func(*models.CallOptions[models.LineItemFieldOptions])
	}{
		{name: "new key read back"},
		{name: "new key no fields selected", opts: []func(*models.CallOptions[models.LineItemFieldOptions]){noLineItemFields}},
		{name: "A's own key read back", ownsKey: true},
		{name: "A's own key no fields selected", ownsKey: true, opts: []func(*models.CallOptions[models.LineItemFieldOptions]){noLineItemFields}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := guardEnvs(t)
			ctx := context.Background()
			foreign, err := b.LineItems().Create(ctx, &models.CreateLineItemInput{OrderID: 1, ProductID: 1, Quantity: 7, SKU: omittable.Set(new("b-sku"))})
			if err != nil {
				t.Fatalf("seed B line item: %v", err)
			}
			key := models.LineItemPK{OrderID: 5, ProductID: 5}
			if tc.ownsKey {
				if _, err := a.LineItems().Create(ctx, &models.CreateLineItemInput{OrderID: 3, ProductID: 3, Quantity: 1}); err != nil {
					t.Fatalf("seed A line item: %v", err)
				}
				key = models.LineItemPK{OrderID: 3, ProductID: 3}
			}

			_, err = a.LineItems().Upsert(ctx, &models.CreateLineItemInput{OrderID: key.OrderID, ProductID: key.ProductID, Quantity: 99, SKU: omittable.Set(new("b-sku"))}, models.LineItemConflictSKU, tc.opts...)
			if !errors.Is(err, models.ErrNotFound) {
				t.Errorf("Upsert onto B's sku as A: err = %v, want ErrNotFound", err)
			}
			assertLineItemUnchanged(t, a, models.LineItemPK{OrderID: foreign.OrderID, ProductID: foreign.ProductID})
			got, err := a.LineItems().Get(ctx, key)
			switch {
			case tc.ownsKey && err != nil:
				t.Fatalf("read A's line item: %v", err)
			case tc.ownsKey && got.Quantity != 1:
				t.Errorf("A's line item quantity = %d, want 1 (unchanged)", got.Quantity)
			case !tc.ownsKey && !errors.Is(err, models.ErrNotFound):
				t.Errorf("A's line item at the input key: Get err = %v, want ErrNotFound (nothing written)", err)
			}
		})
	}
}

// With the tenant inside the PK, a UNIQUE without it can still conflict with
// another tenant's row.
func TestUpsertTenantGuard_TenantInPKUniqueConflictWithForeignRow(t *testing.T) {
	noFields := func(o *models.CallOptions[models.OrderItemFieldOptions]) {
		o.FieldOptions = &models.OrderItemFieldOptions{}
	}
	skip := func(o *models.CallOptions[models.OrderItemFieldOptions]) { o.SkipTenancy = true }
	for _, tc := range []struct {
		name string
		opts []func(*models.CallOptions[models.OrderItemFieldOptions])
	}{
		{name: "read back"},
		{name: "no fields selected", opts: []func(*models.CallOptions[models.OrderItemFieldOptions]){noFields}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := guardEnvs(t)
			ctx := context.Background()
			foreignPK := models.OrderItemPK{WorkspaceID: tenantB, OrderID: 1, ProductID: 1}
			if _, err := b.OrderItems().Create(ctx, &models.CreateOrderItemInput{WorkspaceID: tenantB, OrderID: 1, ProductID: 1, Quantity: 7, UnitPrice: 1, Ref: omittable.Set(new("b-ref"))}); err != nil {
				t.Fatalf("seed B order item: %v", err)
			}

			_, err := a.OrderItems().Upsert(ctx, &models.CreateOrderItemInput{WorkspaceID: tenantA, OrderID: 2, ProductID: 2, Quantity: 99, UnitPrice: 1, Ref: omittable.Set(new("b-ref"))}, models.OrderItemConflictRef, tc.opts...)
			if !errors.Is(err, models.ErrNotFound) {
				t.Errorf("Upsert onto B's ref as A: err = %v, want ErrNotFound", err)
			}
			got, err := a.OrderItems().Get(ctx, foreignPK, skip)
			if err != nil {
				t.Fatalf("read B order item: %v", err)
			}
			if got.Quantity != 7 {
				t.Errorf("B order item quantity = %d, want 7: another tenant's upsert wrote it", got.Quantity)
			}

			// A same-tenant conflict on the same target still updates.
			own, err := a.OrderItems().Create(ctx, &models.CreateOrderItemInput{WorkspaceID: tenantA, OrderID: 3, ProductID: 3, Quantity: 1, UnitPrice: 1, Ref: omittable.Set(new("a-ref"))})
			if err != nil {
				t.Fatalf("seed A order item: %v", err)
			}
			if _, err := a.OrderItems().Upsert(ctx, &models.CreateOrderItemInput{WorkspaceID: tenantA, OrderID: 3, ProductID: 3, Quantity: 42, UnitPrice: 1, Ref: omittable.Set(new("a-ref"))}, models.OrderItemConflictRef, tc.opts...); err != nil {
				t.Fatalf("same-tenant Upsert: %v", err)
			}
			got, err = a.OrderItems().Get(ctx, models.OrderItemPK{WorkspaceID: tenantA, OrderID: own.OrderID, ProductID: own.ProductID})
			if err != nil {
				t.Fatalf("read A order item: %v", err)
			}
			if got.Quantity != 42 {
				t.Errorf("A order item quantity = %d, want 42 (same-tenant conflict updates)", got.Quantity)
			}
		})
	}
}

// An input that leaves the target column unset cannot conflict on it, so
// another tenant's rows do not make the upsert fail.
func TestUpsertTenantGuard_TenantInPKUnsetTargetColumnInserts(t *testing.T) {
	a, b := guardEnvs(t)
	ctx := context.Background()
	if _, err := b.OrderItems().Create(ctx, &models.CreateOrderItemInput{WorkspaceID: tenantB, OrderID: 1, ProductID: 1, Quantity: 7, UnitPrice: 1, Ref: omittable.Set(new("b-ref"))}); err != nil {
		t.Fatalf("seed B order item: %v", err)
	}

	got, err := a.OrderItems().Upsert(ctx, &models.CreateOrderItemInput{WorkspaceID: tenantA, OrderID: 2, ProductID: 2, Quantity: 5, UnitPrice: 1}, models.OrderItemConflictRef)
	if err != nil {
		t.Fatalf("Upsert with ref unset: %v", err)
	}
	if got.WorkspaceID != tenantA || got.Quantity != 5 {
		t.Errorf("Upsert with ref unset = {workspace %v, quantity %d}, want {%v, 5}", got.WorkspaceID, got.Quantity, tenantA)
	}
}

// The input PK names one of A's own rows while B holds the input's ref: the
// call is refused before the statement runs, so A's row is left as it was.
func TestUpsertTenantGuard_TenantInPKOwnKeyForeignRefWritesNothing(t *testing.T) {
	a, b := guardEnvs(t)
	ctx := context.Background()
	if _, err := b.OrderItems().Create(ctx, &models.CreateOrderItemInput{WorkspaceID: tenantB, OrderID: 2, ProductID: 2, Quantity: 7, UnitPrice: 1, Ref: omittable.Set(new("b-ref"))}); err != nil {
		t.Fatalf("seed B order item: %v", err)
	}
	if _, err := a.OrderItems().Create(ctx, &models.CreateOrderItemInput{WorkspaceID: tenantA, OrderID: 1, ProductID: 1, Quantity: 1, UnitPrice: 1, Ref: omittable.Set(new("a-ref"))}); err != nil {
		t.Fatalf("seed A order item: %v", err)
	}

	_, err := a.OrderItems().Upsert(ctx, &models.CreateOrderItemInput{WorkspaceID: tenantA, OrderID: 1, ProductID: 1, Quantity: 99, UnitPrice: 1, Ref: omittable.Set(new("b-ref"))}, models.OrderItemConflictRef)
	if !errors.Is(err, models.ErrNotFound) {
		t.Errorf("Upsert onto B's ref as A: err = %v, want ErrNotFound", err)
	}
	own, err := a.OrderItems().Get(ctx, models.OrderItemPK{WorkspaceID: tenantA, OrderID: 1, ProductID: 1})
	if err != nil {
		t.Fatalf("read A order item: %v", err)
	}
	if own.Quantity != 1 {
		t.Errorf("A order item quantity = %d, want 1: the refused call wrote it", own.Quantity)
	}
}
