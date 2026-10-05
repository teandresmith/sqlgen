package tests

import (
	"context"
	"testing"
	"uuid"

	"github.com/google/go-cmp/cmp"
	"github.com/segmentio/ksuid"
	"github.com/shopspring/decimal"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres/models"
)

// --- UUID override round-trip ---

func TestUUIDOverrideRoundTrip(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	wantID := uuid.New()
	created, err := client.Warehouses().Create(ctx, &models.CreateWarehouseInput{
		ID:         omittable.Set(wantID),
		ExternalID: ksuid.New(),
		Name:       "UUID Test Warehouse",
		Price:      decimal.NewFromFloat(10.00),
	})
	if err != nil {
		t.Fatalf("Create warehouse: %v", err)
	}
	if created.ID != wantID {
		t.Errorf("Create warehouse ID = %v, want %v", created.ID, wantID)
	}

	got, err := client.Warehouses().Get(ctx, wantID)
	if err != nil {
		t.Fatalf("Get warehouse: %v", err)
	}
	if got.ID != wantID {
		t.Errorf("Get warehouse ID = %v, want %v", got.ID, wantID)
	}
}

// --- Decimal override round-trip ---

func TestDecimalOverrideRoundTrip(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	wantPrice := decimal.NewFromFloat(19999.99)

	created, err := client.Warehouses().Create(ctx, &models.CreateWarehouseInput{
		ExternalID: ksuid.New(),
		Name:       "Decimal Test Warehouse",
		Price:      wantPrice,
	})
	if err != nil {
		t.Fatalf("Create warehouse: %v", err)
	}
	if !created.Price.Equal(wantPrice) {
		t.Errorf("Create warehouse price = %s, want %s", created.Price, wantPrice)
	}

	got, err := client.Warehouses().Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get warehouse: %v", err)
	}
	if !got.Price.Equal(wantPrice) {
		t.Errorf("Get warehouse price = %s, want %s", got.Price, wantPrice)
	}
}

// --- KSUID override round-trip ---

func TestKSUIDOverrideRoundTrip(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	wantKSUID := ksuid.New()

	created, err := client.Warehouses().Create(ctx, &models.CreateWarehouseInput{
		ExternalID: wantKSUID,
		Name:       "KSUID Test Warehouse",
		Price:      decimal.NewFromFloat(5.00),
	})
	if err != nil {
		t.Fatalf("Create warehouse: %v", err)
	}
	if created.ExternalID != wantKSUID {
		t.Errorf("Create warehouse external_id = %s, want %s", created.ExternalID, wantKSUID)
	}

	got, err := client.Warehouses().Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get warehouse: %v", err)
	}
	if got.ExternalID != wantKSUID {
		t.Errorf("Get warehouse external_id = %s, want %s", got.ExternalID, wantKSUID)
	}
}

// --- UUID array column ---

func TestUUIDArrayOverride(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	wantIDs := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}

	created, err := client.Warehouses().Create(ctx, &models.CreateWarehouseInput{
		ExternalID: ksuid.New(),
		Name:       "UUID Array Warehouse",
		Price:      decimal.NewFromFloat(1.00),
		RelatedIDS: omittable.Set(wantIDs),
	})
	if err != nil {
		t.Fatalf("Create warehouse: %v", err)
	}
	if diff := cmp.Diff(wantIDs, created.RelatedIDS); diff != "" {
		t.Errorf("Create warehouse related_ids mismatch (-want +got):\n%s", diff)
	}

	got, err := client.Warehouses().Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get warehouse: %v", err)
	}
	if diff := cmp.Diff(wantIDs, got.RelatedIDS); diff != "" {
		t.Errorf("Get warehouse related_ids mismatch (-want +got):\n%s", diff)
	}
}

// --- Nullable UUID and Decimal variants ---

func TestNullableOverrideVariants(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	// Seed a parent warehouse so nullable_ref's self-FK has a real target.
	parent, err := client.Warehouses().Create(ctx, &models.CreateWarehouseInput{
		ExternalID: ksuid.New(),
		Name:       "Nullable Parent Warehouse",
		Price:      decimal.NewFromFloat(1.00),
	})
	if err != nil {
		t.Fatalf("Create parent warehouse: %v", err)
	}

	wantNullablePrice := decimal.NullDecimal{Decimal: decimal.NewFromFloat(42.50), Valid: true}
	wantNullableRef := parent.ID

	// Create with non-null values
	created, err := client.Warehouses().Create(ctx, &models.CreateWarehouseInput{
		ExternalID:    ksuid.New(),
		Name:          "Nullable Set Warehouse",
		Price:         decimal.NewFromFloat(10.00),
		NullablePrice: omittable.Set(wantNullablePrice),
		NullableRef:   omittable.Set(new(wantNullableRef)),
	})
	if err != nil {
		t.Fatalf("Create warehouse (non-null): %v", err)
	}
	if !created.NullablePrice.Valid {
		t.Fatal("Create warehouse nullable_price should be valid")
	}
	if !created.NullablePrice.Decimal.Equal(wantNullablePrice.Decimal) {
		t.Errorf("Create warehouse nullable_price = %s, want %s", created.NullablePrice.Decimal, wantNullablePrice.Decimal)
	}
	if created.NullableRef == nil {
		t.Fatal("Create warehouse nullable_ref should be set")
	}
	if *created.NullableRef != wantNullableRef {
		t.Errorf("Create warehouse nullable_ref = %v, want %v", *created.NullableRef, wantNullableRef)
	}

	got, err := client.Warehouses().Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get warehouse (non-null): %v", err)
	}
	if !got.NullablePrice.Valid || !got.NullablePrice.Decimal.Equal(wantNullablePrice.Decimal) {
		t.Errorf("Get warehouse nullable_price = %v, want %v", got.NullablePrice, wantNullablePrice)
	}
	if got.NullableRef == nil || *got.NullableRef != wantNullableRef {
		t.Errorf("Get warehouse nullable_ref = %v, want %v", got.NullableRef, wantNullableRef)
	}

	// Create with null values (no optional fields set)
	createdNull, err := client.Warehouses().Create(ctx, &models.CreateWarehouseInput{
		ExternalID: ksuid.New(),
		Name:       "Nullable Unset Warehouse",
		Price:      decimal.NewFromFloat(10.00),
	})
	if err != nil {
		t.Fatalf("Create warehouse (null): %v", err)
	}
	if createdNull.NullablePrice.Valid {
		t.Error("nullable_price should be null when not set")
	}
	if createdNull.NullableRef != nil {
		t.Error("nullable_ref should be null when not set")
	}
}

// --- Filtering on decimal.Decimal (comparator.String) ---

func TestFilterDecimalOverride(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	// Seed warehouses with known prices
	prices := []float64{5.00, 15.50, 100.00, 250.75}
	for _, p := range prices {
		_, err := client.Warehouses().Create(ctx, &models.CreateWarehouseInput{
			ExternalID: ksuid.New(),
			Name:       "Filter Decimal Warehouse",
			Price:      decimal.NewFromFloat(p),
		})
		if err != nil {
			t.Fatalf("Create warehouse (price=%v): %v", p, err)
		}
	}

	// Gte + Lt range: price >= 10 AND price < 200
	gte := "10"
	lt := "200"
	results, err := client.Warehouses().GetMany(ctx, &models.GetWarehousesInput{
		Filter: &models.WarehouseFilter{
			Price: &comparator.String{Gte: &gte, Lt: &lt},
			Name:  &comparator.String{Eq: new("Filter Decimal Warehouse")},
		},
		Limit: new(0),
	})
	if err != nil {
		t.Fatalf("GetMany with decimal filter: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("GetMany decimal Gte/Lt: got %d results, want 2 (15.50, 100.00)", len(results))
	}
	for _, r := range results {
		if r.Price.LessThan(decimal.NewFromFloat(10)) || !r.Price.LessThan(decimal.NewFromFloat(200)) {
			t.Errorf("result price %s outside range [10, 200)", r.Price)
		}
	}

	// Exact match
	exact := "250.75"
	exactResults, err := client.Warehouses().GetMany(ctx, &models.GetWarehousesInput{
		Filter: &models.WarehouseFilter{
			Price: &comparator.String{Eq: &exact},
			Name:  &comparator.String{Eq: new("Filter Decimal Warehouse")},
		},
		Limit: new(0),
	})
	if err != nil {
		t.Fatalf("GetMany with decimal Eq filter: %v", err)
	}
	if len(exactResults) != 1 {
		t.Errorf("GetMany decimal Eq: got %d results, want 1", len(exactResults))
	}
	if len(exactResults) > 0 && !exactResults[0].Price.Equal(decimal.NewFromFloat(250.75)) {
		t.Errorf("GetMany decimal Eq: got price %s, want 250.75", exactResults[0].Price)
	}
}

// --- Filtering on ksuid.KSUID (comparator.String) ---

func TestFilterKSUIDOverride(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	target := ksuid.New()
	_, err := client.Warehouses().Create(ctx, &models.CreateWarehouseInput{
		ExternalID: target,
		Name:       "KSUID Filter Target",
		Price:      decimal.NewFromFloat(1.00),
	})
	if err != nil {
		t.Fatalf("Create warehouse: %v", err)
	}

	// Eq filter on KSUID string representation
	targetStr := target.String()
	results, err := client.Warehouses().GetMany(ctx, &models.GetWarehousesInput{
		Filter: &models.WarehouseFilter{
			ExternalID: &comparator.String{Eq: &targetStr},
		},
		Limit: new(0),
	})
	if err != nil {
		t.Fatalf("GetMany with KSUID Eq filter: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("GetMany KSUID Eq: got %d results, want 1", len(results))
	}
	if results[0].ExternalID != target {
		t.Errorf("GetMany KSUID Eq: got %s, want %s", results[0].ExternalID, target)
	}

	// In filter with multiple KSUIDs
	other := ksuid.New()
	_, err = client.Warehouses().Create(ctx, &models.CreateWarehouseInput{
		ExternalID: other,
		Name:       "KSUID Filter Other",
		Price:      decimal.NewFromFloat(2.00),
	})
	if err != nil {
		t.Fatalf("Create second warehouse: %v", err)
	}

	inResults, err := client.Warehouses().GetMany(ctx, &models.GetWarehousesInput{
		Filter: &models.WarehouseFilter{
			ExternalID: &comparator.String{In: []string{target.String(), other.String()}},
		},
		Limit: new(0),
	})
	if err != nil {
		t.Fatalf("GetMany with KSUID In filter: %v", err)
	}
	if len(inResults) != 2 {
		t.Errorf("GetMany KSUID In: got %d results, want 2", len(inResults))
	}
}

// --- Filtering on UUID[] array column ---

func TestFilterUUIDArrayOverride(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	shared := uuid.New()
	unique1 := uuid.New()
	unique2 := uuid.New()

	_, err := client.Warehouses().Create(ctx, &models.CreateWarehouseInput{
		ExternalID: ksuid.New(),
		Name:       "Array Filter A",
		Price:      decimal.NewFromFloat(1.00),
		RelatedIDS: omittable.Set([]uuid.UUID{shared, unique1}),
	})
	if err != nil {
		t.Fatalf("Create warehouse A: %v", err)
	}
	_, err = client.Warehouses().Create(ctx, &models.CreateWarehouseInput{
		ExternalID: ksuid.New(),
		Name:       "Array Filter B",
		Price:      decimal.NewFromFloat(2.00),
		RelatedIDS: omittable.Set([]uuid.UUID{shared, unique2}),
	})
	if err != nil {
		t.Fatalf("Create warehouse B: %v", err)
	}

	// ContainsAny: both warehouses contain the shared UUID
	results, err := client.Warehouses().GetMany(ctx, &models.GetWarehousesInput{
		Filter: &models.WarehouseFilter{
			RelatedIDS: &comparator.NullableSlice[uuid.UUID]{
				Slice: comparator.Slice[uuid.UUID]{ContainsAny: []uuid.UUID{shared}},
			},
			Name: &comparator.String{StartsWith: new("Array Filter")},
		},
		Limit: new(0),
	})
	if err != nil {
		t.Fatalf("GetMany ContainsAny: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("GetMany ContainsAny shared UUID: got %d results, want 2", len(results))
	}

	// ContainsAll: only warehouse A contains unique1
	resultsAll, err := client.Warehouses().GetMany(ctx, &models.GetWarehousesInput{
		Filter: &models.WarehouseFilter{
			RelatedIDS: &comparator.NullableSlice[uuid.UUID]{
				Slice: comparator.Slice[uuid.UUID]{ContainsAll: []uuid.UUID{shared, unique1}},
			},
		},
		Limit: new(0),
	})
	if err != nil {
		t.Fatalf("GetMany ContainsAll: %v", err)
	}
	if len(resultsAll) != 1 {
		t.Errorf("GetMany ContainsAll [shared, unique1]: got %d results, want 1", len(resultsAll))
	}
}

// --- Enum array (user_role[]) create + get round-trip ---

func TestEnumArrayCreateAndGet(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	wantRoles := models.UserRoleSlice{models.UserRoleAdmin, models.UserRoleEditor}

	created, err := client.Warehouses().Create(ctx, &models.CreateWarehouseInput{
		ExternalID:   ksuid.New(),
		Name:         "Enum Array Warehouse",
		Price:        decimal.NewFromFloat(50.00),
		AllowedRoles: omittable.Set(wantRoles),
	})
	if err != nil {
		t.Fatalf("Create warehouse: %v", err)
	}
	if diff := cmp.Diff(wantRoles, created.AllowedRoles); diff != "" {
		t.Errorf("Create warehouse allowed_roles mismatch (-want +got):\n%s", diff)
	}

	got, err := client.Warehouses().Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get warehouse: %v", err)
	}
	if diff := cmp.Diff(wantRoles, got.AllowedRoles); diff != "" {
		t.Errorf("Get warehouse allowed_roles mismatch (-want +got):\n%s", diff)
	}

	// Default value — empty array
	emptyCreated, err := client.Warehouses().Create(ctx, &models.CreateWarehouseInput{
		ExternalID: ksuid.New(),
		Name:       "Enum Array Empty Warehouse",
		Price:      decimal.NewFromFloat(10.00),
	})
	if err != nil {
		t.Fatalf("Create warehouse (empty roles): %v", err)
	}
	if len(emptyCreated.AllowedRoles) != 0 {
		t.Errorf("Create warehouse empty allowed_roles: got %v, want empty", emptyCreated.AllowedRoles)
	}
}

// --- Enum array update ---

func TestEnumArrayUpdate(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	created, err := client.Warehouses().Create(ctx, &models.CreateWarehouseInput{
		ExternalID:   ksuid.New(),
		Name:         "Enum Array Update Warehouse",
		Price:        decimal.NewFromFloat(25.00),
		AllowedRoles: omittable.Set(models.UserRoleSlice{models.UserRoleViewer}),
	})
	if err != nil {
		t.Fatalf("Create warehouse: %v", err)
	}

	newRoles := models.UserRoleSlice{models.UserRoleAdmin, models.UserRoleEditor, models.UserRoleViewer}
	updated, err := client.Warehouses().Update(ctx, created.ID, &models.UpdateWarehouseInput{
		AllowedRoles: omittable.Set(newRoles),
	})
	if err != nil {
		t.Fatalf("Update warehouse: %v", err)
	}
	if diff := cmp.Diff(newRoles, updated.AllowedRoles); diff != "" {
		t.Errorf("Update warehouse allowed_roles mismatch (-want +got):\n%s", diff)
	}

	got, err := client.Warehouses().Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get updated warehouse: %v", err)
	}
	if diff := cmp.Diff(newRoles, got.AllowedRoles); diff != "" {
		t.Errorf("Get updated warehouse allowed_roles mismatch (-want +got):\n%s", diff)
	}
}

// --- Enum array ContainsAny filter ---

func TestEnumArrayContainsAny(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	// Warehouse A: admin + editor
	_, err := client.Warehouses().Create(ctx, &models.CreateWarehouseInput{
		ExternalID:   ksuid.New(),
		Name:         "ContainsAny A",
		Price:        decimal.NewFromFloat(1.00),
		AllowedRoles: omittable.Set(models.UserRoleSlice{models.UserRoleAdmin, models.UserRoleEditor}),
	})
	if err != nil {
		t.Fatalf("Create warehouse A: %v", err)
	}

	// Warehouse B: viewer only
	_, err = client.Warehouses().Create(ctx, &models.CreateWarehouseInput{
		ExternalID:   ksuid.New(),
		Name:         "ContainsAny B",
		Price:        decimal.NewFromFloat(2.00),
		AllowedRoles: omittable.Set(models.UserRoleSlice{models.UserRoleViewer}),
	})
	if err != nil {
		t.Fatalf("Create warehouse B: %v", err)
	}

	namePrefix := "ContainsAny "

	// Filter: ContainsAny [admin] — should match A only
	results, err := client.Warehouses().GetMany(ctx, &models.GetWarehousesInput{
		Filter: &models.WarehouseFilter{
			AllowedRoles: &comparator.Slice[models.UserRole]{
				ContainsAny: models.UserRoleSlice{models.UserRoleAdmin},
			},
			Name: &comparator.String{StartsWith: &namePrefix},
		},
		Limit: new(0),
	})
	if err != nil {
		t.Fatalf("GetMany ContainsAny [admin]: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("GetMany ContainsAny [admin]: got %d results, want 1", len(results))
	}

	// Filter: ContainsAny [admin, viewer] — should match both A and B
	results, err = client.Warehouses().GetMany(ctx, &models.GetWarehousesInput{
		Filter: &models.WarehouseFilter{
			AllowedRoles: &comparator.Slice[models.UserRole]{
				ContainsAny: models.UserRoleSlice{models.UserRoleAdmin, models.UserRoleViewer},
			},
			Name: &comparator.String{StartsWith: &namePrefix},
		},
		Limit: new(0),
	})
	if err != nil {
		t.Fatalf("GetMany ContainsAny [admin, viewer]: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("GetMany ContainsAny [admin, viewer]: got %d results, want 2", len(results))
	}
}

// --- Enum array ContainsAll filter ---

func TestEnumArrayContainsAll(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	// Warehouse A: all three roles
	_, err := client.Warehouses().Create(ctx, &models.CreateWarehouseInput{
		ExternalID:   ksuid.New(),
		Name:         "ContainsAll A",
		Price:        decimal.NewFromFloat(1.00),
		AllowedRoles: omittable.Set(models.UserRoleSlice{models.UserRoleAdmin, models.UserRoleEditor, models.UserRoleViewer}),
	})
	if err != nil {
		t.Fatalf("Create warehouse A: %v", err)
	}

	// Warehouse B: admin only
	_, err = client.Warehouses().Create(ctx, &models.CreateWarehouseInput{
		ExternalID:   ksuid.New(),
		Name:         "ContainsAll B",
		Price:        decimal.NewFromFloat(2.00),
		AllowedRoles: omittable.Set(models.UserRoleSlice{models.UserRoleAdmin}),
	})
	if err != nil {
		t.Fatalf("Create warehouse B: %v", err)
	}

	namePrefix := "ContainsAll "

	// Filter: ContainsAll [admin, editor] — should match only A
	results, err := client.Warehouses().GetMany(ctx, &models.GetWarehousesInput{
		Filter: &models.WarehouseFilter{
			AllowedRoles: &comparator.Slice[models.UserRole]{
				ContainsAll: models.UserRoleSlice{models.UserRoleAdmin, models.UserRoleEditor},
			},
			Name: &comparator.String{StartsWith: &namePrefix},
		},
		Limit: new(0),
	})
	if err != nil {
		t.Fatalf("GetMany ContainsAll [admin, editor]: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("GetMany ContainsAll [admin, editor]: got %d results, want 1", len(results))
	}

	// Filter: ContainsAll [admin] — should match both A and B
	results, err = client.Warehouses().GetMany(ctx, &models.GetWarehousesInput{
		Filter: &models.WarehouseFilter{
			AllowedRoles: &comparator.Slice[models.UserRole]{
				ContainsAll: models.UserRoleSlice{models.UserRoleAdmin},
			},
			Name: &comparator.String{StartsWith: &namePrefix},
		},
		Limit: new(0),
	})
	if err != nil {
		t.Fatalf("GetMany ContainsAll [admin]: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("GetMany ContainsAll [admin]: got %d results, want 2", len(results))
	}
}

// --- Enum array IsEmpty filter ---

func TestEnumArrayIsEmpty(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	// Warehouse A: empty roles
	_, err := client.Warehouses().Create(ctx, &models.CreateWarehouseInput{
		ExternalID: ksuid.New(),
		Name:       "IsEmpty A",
		Price:      decimal.NewFromFloat(1.00),
	})
	if err != nil {
		t.Fatalf("Create warehouse A (empty): %v", err)
	}

	// Warehouse B: non-empty roles
	_, err = client.Warehouses().Create(ctx, &models.CreateWarehouseInput{
		ExternalID:   ksuid.New(),
		Name:         "IsEmpty B",
		Price:        decimal.NewFromFloat(2.00),
		AllowedRoles: omittable.Set(models.UserRoleSlice{models.UserRoleAdmin}),
	})
	if err != nil {
		t.Fatalf("Create warehouse B (non-empty): %v", err)
	}

	namePrefix := "IsEmpty "

	// Filter: IsEmpty = true — should match A only
	results, err := client.Warehouses().GetMany(ctx, &models.GetWarehousesInput{
		Filter: &models.WarehouseFilter{
			AllowedRoles: &comparator.Slice[models.UserRole]{IsEmpty: new(true)},
			Name:         &comparator.String{StartsWith: &namePrefix},
		},
		Limit: new(0),
	})
	if err != nil {
		t.Fatalf("GetMany IsEmpty=true: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("GetMany IsEmpty=true: got %d results, want 1", len(results))
	}

	// Filter: IsEmpty = false — should match B only
	results, err = client.Warehouses().GetMany(ctx, &models.GetWarehousesInput{
		Filter: &models.WarehouseFilter{
			AllowedRoles: &comparator.Slice[models.UserRole]{IsEmpty: new(false)},
			Name:         &comparator.String{StartsWith: &namePrefix},
		},
		Limit: new(0),
	})
	if err != nil {
		t.Fatalf("GetMany IsEmpty=false: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("GetMany IsEmpty=false: got %d results, want 1", len(results))
	}
}

// --- Nullable & array type-override round-trip ---
//
// These tests pin §7.4 / §7.5 invariants on overridden nullable + array types:
//   - NULL round-trips materialise as {Valid: false}, not zero-value-marked-valid
//   - Array columns scan into []T of the override target type
//   - Empty-result GetMany on a type-overridden table never panics
//
// `TestNullableOverrideVariants` above already covers the basic null + value
// shape; the tests below add explicit insert-NULL, update-to-NULL, and
// update-to-value transitions plus the array + empty-result coverage that
// the §14.9 spec calls for.

// TestNullableUUIDPointer_NullThenValueRoundTrip exercises *uuid.UUID through
// INSERT-NULL → UPDATE-to-value → UPDATE-back-to-NULL. Each transition asserts
// both the create/update return and a fresh Get readback so we catch any
// divergence between the RETURNING projection and the column-name scan path.
//
// This example binds the standard library, which ships no NullUUID, so a
// nullable uuid column is the pointer shape and NULL is a nil pointer
// (PRD §7.3, §7.4). The uuid.NullUUID wrapper half of the same round-trip is
// guarded by the postgres_stdlib example, which holds github.com/google/uuid.
func TestNullableUUIDPointer_NullThenValueRoundTrip(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	// 1. Create with nullable_ref unset → column persists as NULL.
	created, err := client.Warehouses().Create(ctx, &models.CreateWarehouseInput{
		ExternalID: ksuid.New(),
		Name:       "NullUUID round-trip",
		Price:      decimal.NewFromFloat(1.00),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.NullableRef != nil {
		t.Errorf("Create: NullableRef should be nil when unset, got %v", *created.NullableRef)
	}

	got, err := client.Warehouses().Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get after create: %v", err)
	}
	if got.NullableRef != nil {
		t.Errorf("Get: NullableRef should be nil, got %v", *got.NullableRef)
	}

	// 2. Update from NULL → value. Seed a sibling warehouse so the self-FK
	// has a real target (warehouses.nullable_ref REFERENCES warehouses(id)).
	parent, err := client.Warehouses().Create(ctx, &models.CreateWarehouseInput{
		ExternalID: ksuid.New(),
		Name:       "NullUUID round-trip parent",
		Price:      decimal.NewFromFloat(1.00),
	})
	if err != nil {
		t.Fatalf("Create parent: %v", err)
	}
	want := parent.ID
	updated, err := client.Warehouses().Update(ctx, created.ID, &models.UpdateWarehouseInput{
		NullableRef: omittable.Set(new(want)),
	})
	if err != nil {
		t.Fatalf("Update null→value: %v", err)
	}
	if updated.NullableRef == nil || *updated.NullableRef != want {
		t.Errorf("Update: NullableRef = %v, want %v", updated.NullableRef, want)
	}

	got, err = client.Warehouses().Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get after value update: %v", err)
	}
	if got.NullableRef == nil || *got.NullableRef != want {
		t.Errorf("Get: NullableRef = %v, want %v", got.NullableRef, want)
	}

	// 3. Update value → NULL. Setting a nil *uuid.UUID writes SQL NULL — the
	// pointer carries the null state directly, with no wrapper Valuer in the
	// path (PRD §7.3).
	cleared, err := client.Warehouses().Update(ctx, created.ID, &models.UpdateWarehouseInput{
		NullableRef: omittable.Set[*uuid.UUID](nil),
	})
	if err != nil {
		t.Fatalf("Update value→null: %v", err)
	}
	if cleared.NullableRef != nil {
		t.Errorf("Update: NullableRef should be nil after clearing, got %v", *cleared.NullableRef)
	}

	got, err = client.Warehouses().Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get after clearing: %v", err)
	}
	if got.NullableRef != nil {
		t.Errorf("Get: NullableRef should be nil after clearing, got %v", *got.NullableRef)
	}
}

// TestNullableDecimalOverride_NullThenValueRoundTrip mirrors the nullable-UUID
// case for decimal.NullDecimal, which IS a wrapper here: insert-null →
// update-to-value → update-to-null.
func TestNullableDecimalOverride_NullThenValueRoundTrip(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	created, err := client.Warehouses().Create(ctx, &models.CreateWarehouseInput{
		ExternalID: ksuid.New(),
		Name:       "NullDecimal round-trip",
		Price:      decimal.NewFromFloat(1.00),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.NullablePrice.Valid {
		t.Errorf("Create: NullablePrice should be {Valid:false} when unset, got %+v", created.NullablePrice)
	}
	if !created.NullablePrice.Decimal.IsZero() {
		t.Errorf("Create: NullablePrice.Decimal should be zero when invalid, got %s", created.NullablePrice.Decimal)
	}

	got, err := client.Warehouses().Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get after create: %v", err)
	}
	if got.NullablePrice.Valid {
		t.Errorf("Get: NullablePrice should be {Valid:false}, got %+v", got.NullablePrice)
	}

	want := decimal.NewFromFloat(123.45)
	updated, err := client.Warehouses().Update(ctx, created.ID, &models.UpdateWarehouseInput{
		NullablePrice: omittable.Set(decimal.NullDecimal{Decimal: want, Valid: true}),
	})
	if err != nil {
		t.Fatalf("Update null→value: %v", err)
	}
	if !updated.NullablePrice.Valid || !updated.NullablePrice.Decimal.Equal(want) {
		t.Errorf("Update: NullablePrice = %+v, want {Valid:true, Decimal:%s}", updated.NullablePrice, want)
	}

	got, err = client.Warehouses().Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get after value update: %v", err)
	}
	if !got.NullablePrice.Valid || !got.NullablePrice.Decimal.Equal(want) {
		t.Errorf("Get: NullablePrice = %+v, want {Valid:true, Decimal:%s}", got.NullablePrice, want)
	}

	cleared, err := client.Warehouses().Update(ctx, created.ID, &models.UpdateWarehouseInput{
		NullablePrice: omittable.Set(decimal.NullDecimal{}),
	})
	if err != nil {
		t.Fatalf("Update value→null: %v", err)
	}
	if cleared.NullablePrice.Valid {
		t.Errorf("Update: NullablePrice should be {Valid:false} after clearing, got %+v", cleared.NullablePrice)
	}

	got, err = client.Warehouses().Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get after clearing: %v", err)
	}
	if got.NullablePrice.Valid {
		t.Errorf("Get: NullablePrice should be {Valid:false} after clearing, got %+v", got.NullablePrice)
	}
}

// TestKSUIDArrayOverride_RoundTrip exercises a TEXT[] column scanning into
// []ksuid.KSUID under the table-level `text → ksuid.KSUID` override.
// The column is `tag_ids TEXT[] NOT NULL DEFAULT '{}'` — empty default,
// non-empty insert, and value mutation all round-trip element-by-element.
func TestKSUIDArrayOverride_RoundTrip(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	wantTags := []ksuid.KSUID{ksuid.New(), ksuid.New(), ksuid.New()}

	created, err := client.Warehouses().Create(ctx, &models.CreateWarehouseInput{
		ExternalID: ksuid.New(),
		Name:       "KSUID array round-trip",
		Price:      decimal.NewFromFloat(1.00),
		TagIDS:     omittable.Set(wantTags),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if diff := cmp.Diff(wantTags, created.TagIDS); diff != "" {
		t.Errorf("Create TagIDS mismatch (-want +got):\n%s", diff)
	}

	got, err := client.Warehouses().Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if diff := cmp.Diff(wantTags, got.TagIDS); diff != "" {
		t.Errorf("Get TagIDS mismatch (-want +got):\n%s", diff)
	}
	for i, k := range got.TagIDS {
		if k == (ksuid.KSUID{}) {
			t.Errorf("Get TagIDS[%d] is zero KSUID — element scan dropped data", i)
		}
	}

	// Empty default: a warehouse created without TagIDS scans into an empty
	// (non-nil-or-nil) slice — never a panic, never a non-empty result.
	emptyCreated, err := client.Warehouses().Create(ctx, &models.CreateWarehouseInput{
		ExternalID: ksuid.New(),
		Name:       "KSUID array empty default",
		Price:      decimal.NewFromFloat(1.00),
	})
	if err != nil {
		t.Fatalf("Create empty default: %v", err)
	}
	if len(emptyCreated.TagIDS) != 0 {
		t.Errorf("Create empty default TagIDS = %v, want empty", emptyCreated.TagIDS)
	}

	gotEmpty, err := client.Warehouses().Get(ctx, emptyCreated.ID)
	if err != nil {
		t.Fatalf("Get empty default: %v", err)
	}
	if len(gotEmpty.TagIDS) != 0 {
		t.Errorf("Get empty default TagIDS = %v, want empty", gotEmpty.TagIDS)
	}

	// Update path: replace the populated slice with a different shape.
	replacement := []ksuid.KSUID{ksuid.New()}
	updated, err := client.Warehouses().Update(ctx, created.ID, &models.UpdateWarehouseInput{
		TagIDS: omittable.Set(replacement),
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if diff := cmp.Diff(replacement, updated.TagIDS); diff != "" {
		t.Errorf("Update TagIDS mismatch (-want +got):\n%s", diff)
	}
}

// TestOverriddenTypes_EmptyGetManyNoPanic asserts that GetMany on a
// type-overridden table with a filter that matches zero rows returns an
// empty slice / nil error without panicking on the element-scan path —
// regression guard for any future scan loop that assumes ≥1 row.
func TestOverriddenTypes_EmptyGetManyNoPanic(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	// Filter that cannot match: external_id is UNIQUE and we generate a
	// fresh KSUID that has not been inserted in any other test in this run.
	// (KSUIDs are 27-char base62 strings with millisecond timestamps — the
	// collision probability with another freshly-generated KSUID in the same
	// process is negligible.)
	never := ksuid.New().String()

	results, err := client.Warehouses().GetMany(ctx, &models.GetWarehousesInput{
		Filter: &models.WarehouseFilter{
			ExternalID: &comparator.String{Eq: &never},
		},
		Limit: new(0),
	})
	if err != nil {
		t.Fatalf("GetMany on no-match filter: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("GetMany on no-match filter: got %d results, want 0", len(results))
	}
}

// --- Table-scoped overrides do not leak onto other tables ---

// TestNonOverriddenTablesUnchanged pins that the `warehouses` table-scoped
// `overrides.types` block reaches only `warehouses`.
//
// It used to make that point on `uuid`: warehouses bound uuid.UUID while every
// other table's `uuid` column stayed a Go `string`. That contrast is gone —
// `uuid` resolves to `uuid.UUID` package-wide now (PRD §7.2) — so the pin moved
// to the `text` entry, which warehouses overrides to ksuid.KSUID. `categories`
// is a non-overridden table with a TEXT column, so if the scope leaked, `Name`
// would be a ksuid.KSUID.
func TestNonOverriddenTablesUnchanged(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{
		Name: "override-isolation-test",
	})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}

	// The assignment IS the assertion: it does not compile if the table-scoped
	// `text` → ksuid.KSUID override leaked onto this table.
	var name string = cat.Name
	if name != "override-isolation-test" {
		t.Errorf("category name = %q, want the value written", name)
	}
	if cat.ID == (uuid.UUID{}) {
		t.Error("category ID is the zero UUID, want a generated one")
	}

	// Clean up
	if err := client.Categories().HardDelete(ctx, cat.ID); err != nil {
		t.Fatalf("HardDelete category: %v", err)
	}
}
