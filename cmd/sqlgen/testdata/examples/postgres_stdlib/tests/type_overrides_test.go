package tests

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/segmentio/ksuid"
	"github.com/shopspring/decimal"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres_stdlib/models"
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

	wantNullablePrice := decimal.NullDecimal{Decimal: decimal.NewFromFloat(42.50), Valid: true}
	wantNullableRef := uuid.NullUUID{UUID: uuid.New(), Valid: true}

	// Create with non-null values
	created, err := client.Warehouses().Create(ctx, &models.CreateWarehouseInput{
		ExternalID:    ksuid.New(),
		Name:          "Nullable Set Warehouse",
		Price:         decimal.NewFromFloat(10.00),
		NullablePrice: omittable.Set(wantNullablePrice),
		NullableRef:   omittable.Set(wantNullableRef),
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
	if !created.NullableRef.Valid {
		t.Fatal("Create warehouse nullable_ref should be valid")
	}
	if created.NullableRef.UUID != wantNullableRef.UUID {
		t.Errorf("Create warehouse nullable_ref = %v, want %v", created.NullableRef.UUID, wantNullableRef.UUID)
	}

	got, err := client.Warehouses().Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get warehouse (non-null): %v", err)
	}
	if !got.NullablePrice.Valid || !got.NullablePrice.Decimal.Equal(wantNullablePrice.Decimal) {
		t.Errorf("Get warehouse nullable_price = %v, want %v", got.NullablePrice, wantNullablePrice)
	}
	if !got.NullableRef.Valid || got.NullableRef.UUID != wantNullableRef.UUID {
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
	if createdNull.NullableRef.Valid {
		t.Error("nullable_ref should be null when not set")
	}
}

// TestNullableUUIDWrapper_NullThenValueRoundTrip exercises uuid.NullUUID
// through INSERT-NULL → UPDATE-to-value → UPDATE-back-to-NULL. Each transition
// asserts both the update return and a fresh Get readback, so a divergence
// between the RETURNING projection and the column-name scan path shows up.
//
// The two UPDATE transitions are the half TestNullableOverrideVariants above
// does not reach, and the assertion that `omittable.Set(uuid.NullUUID{})`
// writes SQL NULL — which rides the wrapper's driver.Valuer returning nil for
// !Valid — exists only here. The `postgres` example carries the same round-trip
// on the *pointer* shape, since it binds the standard library, which ships no
// NullUUID (PRD §7.3, §7.4).
func TestNullableUUIDWrapper_NullThenValueRoundTrip(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	// 1. Create with nullable_ref unset → the column persists as NULL.
	created, err := client.Warehouses().Create(ctx, &models.CreateWarehouseInput{
		ExternalID: ksuid.New(),
		Name:       "NullUUID round-trip",
		Price:      decimal.NewFromFloat(1.00),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.NullableRef.Valid {
		t.Errorf("Create: NullableRef should be {Valid:false} when unset, got %+v", created.NullableRef)
	}
	if created.NullableRef.UUID != uuid.Nil {
		t.Errorf("Create: NullableRef.UUID should be uuid.Nil when invalid, got %v", created.NullableRef.UUID)
	}

	// 2. Update NULL → value. warehouses.nullable_ref is a self-referential FK,
	// so the target has to be a real warehouse.
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
		NullableRef: omittable.Set(uuid.NullUUID{UUID: want, Valid: true}),
	})
	if err != nil {
		t.Fatalf("Update null→value: %v", err)
	}
	if !updated.NullableRef.Valid || updated.NullableRef.UUID != want {
		t.Errorf("Update: NullableRef = %+v, want {Valid:true, UUID:%v}", updated.NullableRef, want)
	}

	got, err := client.Warehouses().Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get after value update: %v", err)
	}
	if !got.NullableRef.Valid || got.NullableRef.UUID != want {
		t.Errorf("Get: NullableRef = %+v, want {Valid:true, UUID:%v}", got.NullableRef, want)
	}

	// 3. Update value → NULL. Setting NullUUID{Valid:false} writes SQL NULL
	// because uuid.NullUUID's driver.Valuer returns nil for !Valid.
	cleared, err := client.Warehouses().Update(ctx, created.ID, &models.UpdateWarehouseInput{
		NullableRef: omittable.Set(uuid.NullUUID{}),
	})
	if err != nil {
		t.Fatalf("Update value→null: %v", err)
	}
	if cleared.NullableRef.Valid {
		t.Errorf("Update: NullableRef should be {Valid:false} after clearing, got %+v", cleared.NullableRef)
	}

	got, err = client.Warehouses().Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get after clearing: %v", err)
	}
	if got.NullableRef.Valid {
		t.Errorf("Get: NullableRef should be {Valid:false} after clearing, got %+v", got.NullableRef)
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
