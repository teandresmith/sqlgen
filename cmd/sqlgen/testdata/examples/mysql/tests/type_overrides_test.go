package tests

import (
	"context"
	"testing"

	"github.com/segmentio/ksuid"
	"github.com/shopspring/decimal"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/mysql/models"
)

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

	got, err := client.Warehouses().Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get warehouse: %v", err)
	}
	if got.ExternalID != wantKSUID {
		t.Errorf("Get warehouse external_id = %s, want %s", got.ExternalID, wantKSUID)
	}
}

// --- Nullable decimal variant ---

func TestNullableDecimalOverride(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	wantNullablePrice := decimal.NullDecimal{Decimal: decimal.NewFromFloat(42.50), Valid: true}

	created, err := client.Warehouses().Create(ctx, &models.CreateWarehouseInput{
		ExternalID:    ksuid.New(),
		Name:          "Nullable Decimal Warehouse",
		Price:         decimal.NewFromFloat(10.00),
		NullablePrice: omittable.Set(wantNullablePrice),
	})
	if err != nil {
		t.Fatalf("Create warehouse: %v", err)
	}
	if !created.NullablePrice.Valid {
		t.Fatal("nullable_price should be valid")
	}
	if !created.NullablePrice.Decimal.Equal(wantNullablePrice.Decimal) {
		t.Errorf("nullable_price = %s, want %s", created.NullablePrice.Decimal, wantNullablePrice.Decimal)
	}

	// Create with null
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
}

// --- Filtering on decimal.Decimal (comparator.String) ---

func TestFilterDecimalOverride(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	prices := []float64{5.00, 15.50, 100.00, 250.75}
	for _, p := range prices {
		_, err := client.Warehouses().Create(ctx, &models.CreateWarehouseInput{
			ExternalID: ksuid.New(),
			Name:       "Filter Decimal MySQL",
			Price:      decimal.NewFromFloat(p),
		})
		if err != nil {
			t.Fatalf("Create warehouse (price=%v): %v", p, err)
		}
	}

	// Range filter: price >= 10 AND price < 200
	gte := "10"
	lt := "200"
	results, err := client.Warehouses().GetMany(ctx, &models.GetWarehousesInput{
		Filter: &models.WarehouseFilter{
			Price: &comparator.String{Gte: &gte, Lt: &lt},
			Name:  &comparator.String{Eq: new("Filter Decimal MySQL")},
		},
		Limit: new(0),
	})
	if err != nil {
		t.Fatalf("GetMany with decimal filter: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("GetMany decimal Gte/Lt: got %d results, want 2 (15.50, 100.00)", len(results))
	}
}

// --- Filtering on ksuid.KSUID (comparator.String) ---

func TestFilterKSUIDOverride(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	target := ksuid.New()
	_, err := client.Warehouses().Create(ctx, &models.CreateWarehouseInput{
		ExternalID: target,
		Name:       "KSUID Filter MySQL",
		Price:      decimal.NewFromFloat(1.00),
	})
	if err != nil {
		t.Fatalf("Create warehouse: %v", err)
	}

	// Eq filter on KSUID string
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
}

// --- Non-overridden tables still use default types ---

func TestNonOverriddenTablesUnchanged(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{
		Name: "override-isolation-mysql",
	})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	if cat.ID == 0 {
		t.Error("category ID should be a non-zero int64")
	}

	if err := client.Categories().HardDelete(ctx, cat.ID); err != nil {
		t.Fatalf("HardDelete category: %v", err)
	}
}
