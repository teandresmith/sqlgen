package suite

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"
	"github.com/teandresmith/sqlgen/sql"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/models"
)

// compositeItems seeds the three order items whose keys overlap pairwise, so a
// load or filter that matched one column would pick up a neighbor's rows.
func compositeItems(t *testing.T, h *Harness) (a, b, c models.OrderItemPK) {
	t.Helper()
	p1, p2 := createProduct(t, h, "p1"), createProduct(t, h, "p2")
	a = models.OrderItemPK{OrderID: 1, ProductID: p1.ID}
	b = models.OrderItemPK{OrderID: 1, ProductID: p2.ID}
	c = models.OrderItemPK{OrderID: 2, ProductID: p1.ID}
	for _, pk := range []models.OrderItemPK{a, b, c} {
		createOrderItem(t, h, pk)
	}
	return a, b, c
}

func createItemNote(t *testing.T, h *Harness, pk models.OrderItemPK, body string) {
	t.Helper()
	in := &models.CreateItemNoteInput{OrderID: pk.OrderID, ProductID: pk.ProductID, Body: body}
	if _, err := h.Client.ItemNotes().Create(context.Background(), in); err != nil {
		t.Fatalf("Create item note %q: %v", body, err)
	}
}

func createStation(t *testing.T, h *Harness, productID int64, name string) *models.Station {
	t.Helper()
	s, err := h.Client.Stations().Create(context.Background(), &models.CreateStationInput{ProductID: productID, Name: name})
	if err != nil {
		t.Fatalf("Create station %q: %v", name, err)
	}
	return s
}

func linkItemStation(t *testing.T, h *Harness, pk models.OrderItemPK, stationID int64) {
	t.Helper()
	in := &models.CreateItemStationInput{OrderID: pk.OrderID, ProductID: pk.ProductID, StationID: stationID}
	if _, err := h.Client.ItemStations().Create(context.Background(), in); err != nil {
		t.Fatalf("Link item %+v to station %d: %v", pk, stationID, err)
	}
}

func createShelf(t *testing.T, h *Harness, region string, bin *int64) *models.Shelf {
	t.Helper()
	s, err := h.Client.Shelves().Create(context.Background(), &models.CreateShelfInput{Region: region, Bin: omittable.Set(bin)})
	if err != nil {
		t.Fatalf("Create shelf %q: %v", region, err)
	}
	return s
}

func createParcel(t *testing.T, h *Harness, region *string, bin *int64, label string) {
	t.Helper()
	in := &models.CreateParcelInput{Region: omittable.Set(region), Bin: omittable.Set(bin), Label: label}
	if _, err := h.Client.Parcels().Create(context.Background(), in); err != nil {
		t.Fatalf("Create parcel %q: %v", label, err)
	}
}

// noteBodies keys each item's loaded note bodies on its primary key.
func noteBodies(items []*models.OrderItem) map[models.OrderItemPK][]string {
	got := make(map[models.OrderItemPK][]string, len(items))
	for _, it := range items {
		pk := models.OrderItemPK{OrderID: it.OrderID, ProductID: it.ProductID}
		got[pk] = []string{}
		for _, n := range it.ItemNotes {
			got[pk] = append(got[pk], n.Body)
		}
	}
	return got
}

func parcelLabels(s *models.Shelf) []string {
	labels := []string{}
	for _, p := range s.Parcels {
		labels = append(labels, p.Label)
	}
	return labels
}

// statementsReading counts the recorded statements whose outermost FROM names
// table, with or without a schema, in the dialect's quoting.
func statementsReading(h *Harness, table string) int {
	n := 0
	for _, s := range h.Querier.Statements() {
		_, rest, ok := strings.Cut(s, "FROM ")
		if !ok {
			continue
		}
		name, _, _ := strings.Cut(rest, " ")
		parts := strings.Split(name, ".")
		if strings.Trim(parts[len(parts)-1], "\"`") == table {
			n++
		}
	}
	return n
}

// getCompositeKey: Get joins an O2O edge on every pair of its key.
func getCompositeKey(t *testing.T, h *Harness) {
	ctx := context.Background()

	t.Run("a has-one through a composite key joins on every pair", func(t *testing.T) {
		h.Reset(t)
		a, b, c := compositeItems(t, h)
		for pk, code := range map[models.OrderItemPK]string{a: "a", c: "c"} {
			if _, err := h.Client.ItemReceipts().Create(ctx, &models.CreateItemReceiptInput{OrderID: pk.OrderID, ProductID: pk.ProductID, Code: code}); err != nil {
				t.Fatalf("Create receipt %q: %v", code, err)
			}
		}
		fields := func(o *models.CallOptions[models.OrderItemFieldOptions]) {
			o.FieldOptions = &models.OrderItemFieldOptions{OrderID: true, Receipt: &models.ItemReceiptFieldOptions{ID: true, Code: true}}
		}
		got, err := h.Client.OrderItems().Get(ctx, c, fields)
		if err != nil {
			t.Fatalf("Get(c) error = %v, want nil", err)
		}
		if got.Receipt == nil || got.Receipt.Code != "c" {
			t.Errorf("Get(c).Receipt = %+v, want the receipt with code c", got.Receipt)
		}
		// b shares its order with a, whose receipt a one-pair join would return.
		got, err = h.Client.OrderItems().Get(ctx, b, fields)
		if err != nil {
			t.Fatalf("Get(b) error = %v, want nil", err)
		}
		if got.Receipt != nil {
			t.Errorf("Get(b).Receipt = %+v, want nil", got.Receipt)
		}
	})

	t.Run("an O2O through a composite key joins on every pair", func(t *testing.T) {
		h.Reset(t)
		a, b, c := compositeItems(t, h)
		for pk, quantity := range map[models.OrderItemPK]int64{a: 10, b: 20, c: 30} {
			if _, err := h.Client.OrderItems().Update(ctx, pk, &models.UpdateOrderItemInput{Quantity: omittable.Set(quantity)}); err != nil {
				t.Fatalf("Update %+v: %v", pk, err)
			}
		}
		receipt, err := h.Client.ItemReceipts().Create(ctx, &models.CreateItemReceiptInput{OrderID: b.OrderID, ProductID: b.ProductID, Code: "r"})
		if err != nil {
			t.Fatalf("Create receipt: %v", err)
		}

		got, err := h.Client.ItemReceipts().Get(ctx, receipt.ID, func(o *models.CallOptions[models.ItemReceiptFieldOptions]) {
			o.FieldOptions = &models.ItemReceiptFieldOptions{Code: true, OrderItems: &models.OrderItemFieldOptions{OrderID: true, ProductID: true, Quantity: true}}
		})
		if err != nil {
			t.Fatalf("Get(receipt) error = %v, want nil", err)
		}
		if got.OrderItems == nil {
			t.Fatal("Get(receipt).OrderItems = nil, want the item the key names")
		}
		if got.OrderItems.Quantity != 20 || got.OrderItems.ProductID != b.ProductID {
			t.Errorf("Get(receipt).OrderItems = %+v, want the item %+v with quantity 20", got.OrderItems, b)
		}
	})
}

// getManyCompositeKey: GetMany loads and filters through composite keys.
func getManyCompositeKey(t *testing.T, h *Harness) {
	ctx := context.Background()

	t.Run("a composite foreign key loads each parent's children on every pair", func(t *testing.T) {
		h.Reset(t)
		a, b, c := compositeItems(t, h)
		createItemNote(t, h, a, "a-1")
		createItemNote(t, h, a, "a-2")
		createItemNote(t, h, c, "c-1")

		items, err := h.Client.OrderItems().GetMany(ctx, nil, func(o *models.CallOptions[models.OrderItemFieldOptions]) {
			o.FieldOptions = &models.OrderItemFieldOptions{OrderID: true, ProductID: true, ItemNotes: &models.ItemNoteRelationshipOptions{
				Sorts: []sql.Sort{{Column: "body", Direction: sql.Asc}},
			}}
		})
		if err != nil {
			t.Fatalf("GetMany error = %v, want nil", err)
		}
		want := map[models.OrderItemPK][]string{a: {"a-1", "a-2"}, b: {}, c: {"c-1"}}
		if diff := gocmp.Diff(want, noteBodies(items)); diff != "" {
			t.Errorf("notes by item (-want +got):\n%s", diff)
		}
	})

	t.Run("a consumer filter on a composite edge applies with the key condition", func(t *testing.T) {
		h.Reset(t)
		a, _, c := compositeItems(t, h)
		createItemNote(t, h, a, "keep")
		createItemNote(t, h, a, "drop")
		createItemNote(t, h, c, "keep")

		items, err := h.Client.OrderItems().GetMany(ctx, &models.GetOrderItemsInput{
			Filter: &models.OrderItemFilter{PKs: []models.OrderItemPK{a}},
		}, func(o *models.CallOptions[models.OrderItemFieldOptions]) {
			o.FieldOptions = &models.OrderItemFieldOptions{OrderID: true, ProductID: true, ItemNotes: &models.ItemNoteRelationshipOptions{
				Filter: &models.ItemNoteFilter{Body: &comparator.String{Eq: new("keep")}},
			}}
		})
		if err != nil {
			t.Fatalf("GetMany error = %v, want nil", err)
		}
		if diff := gocmp.Diff(map[models.OrderItemPK][]string{a: {"keep"}}, noteBodies(items)); diff != "" {
			t.Errorf("notes by item (-want +got):\n%s", diff)
		}
	})

	t.Run("a soft-delete comparator in a consumer filter narrows a composite edge's default", func(t *testing.T) {
		h.Reset(t)
		a, _, _ := compositeItems(t, h)
		createItemNote(t, h, a, "live")
		createItemNote(t, h, a, "gone")
		if _, err := h.Client.ItemNotes().SoftDeleteWhere(ctx, &models.ItemNoteFilter{Body: &comparator.String{Eq: new("gone")}}); err != nil {
			t.Fatalf("SoftDeleteWhere: %v", err)
		}
		load := func(filter *models.ItemNoteFilter) []string {
			t.Helper()
			items, err := h.Client.OrderItems().GetMany(ctx, &models.GetOrderItemsInput{
				Filter: &models.OrderItemFilter{PKs: []models.OrderItemPK{a}},
			}, func(o *models.CallOptions[models.OrderItemFieldOptions]) {
				o.FieldOptions = &models.OrderItemFieldOptions{OrderID: true, ProductID: true, ItemNotes: &models.ItemNoteRelationshipOptions{Filter: filter}}
			})
			if err != nil {
				t.Fatalf("GetMany error = %v, want nil", err)
			}
			return noteBodies(items)[a]
		}
		if got := load(nil); !slices.Equal(got, []string{"live"}) {
			t.Errorf("default load = %q, want only the live note", got)
		}
		deleted := &models.ItemNoteFilter{DeletedAt: &comparator.NullableTime{Null: new(false)}}
		if got := load(deleted); len(got) != 0 {
			t.Errorf("load with a deleted-only comparator = %q, want none: it narrows the default", got)
		}
	})

	t.Run("a one-column key into a nullable unique column skips a NULL parent", func(t *testing.T) {
		h.Reset(t)
		tagged, err := h.Client.Shelves().Create(ctx, &models.CreateShelfInput{Region: "tagged", Tag: omittable.Set(new("t1"))})
		if err != nil {
			t.Fatalf("Create shelf: %v", err)
		}
		untagged := createShelf(t, h, "untagged", nil)
		for label, tag := range map[string]*string{"in": new("t1"), "loose": nil} {
			if _, err := h.Client.Crates().Create(ctx, &models.CreateCrateInput{ShelfTag: omittable.Set(tag), Label: label}); err != nil {
				t.Fatalf("Create crate %q: %v", label, err)
			}
		}
		shelves, err := h.Client.Shelves().GetMany(ctx, nil, func(o *models.CallOptions[models.ShelfFieldOptions]) {
			o.FieldOptions = &models.ShelfFieldOptions{ID: true, Tag: true, Crates: &models.CrateRelationshipOptions{}}
		})
		if err != nil {
			t.Fatalf("GetMany error = %v, want nil", err)
		}
		got := map[int64][]string{}
		for _, s := range shelves {
			got[s.ID] = []string{}
			for _, c := range s.Crates {
				got[s.ID] = append(got[s.ID], c.Label)
			}
		}
		want := map[int64][]string{tagged.ID: {"in"}, untagged.ID: {}}
		if diff := gocmp.Diff(want, got); diff != "" {
			t.Errorf("crates by shelf (-want +got):\n%s", diff)
		}
	})

	t.Run("an M2M from a one-column key into a composite key maps each target to its parents", func(t *testing.T) {
		h.Reset(t)
		a, b, c := compositeItems(t, h)
		first, err := h.Client.Crates().Create(ctx, &models.CreateCrateInput{Label: "first"})
		if err != nil {
			t.Fatalf("Create crate: %v", err)
		}
		second, err := h.Client.Crates().Create(ctx, &models.CreateCrateInput{Label: "second"})
		if err != nil {
			t.Fatalf("Create crate: %v", err)
		}
		third, err := h.Client.Crates().Create(ctx, &models.CreateCrateInput{Label: "third"})
		if err != nil {
			t.Fatalf("Create crate: %v", err)
		}
		for crate, pk := range map[int64]models.OrderItemPK{first.ID: a, second.ID: b, third.ID: c} {
			if _, err := h.Client.CrateItems().Create(ctx, &models.CreateCrateItemInput{CrateID: crate, OrderID: pk.OrderID, ProductID: pk.ProductID}); err != nil {
				t.Fatalf("Link crate %d: %v", crate, err)
			}
		}
		crates, err := h.Client.Crates().GetMany(ctx, nil, func(o *models.CallOptions[models.CrateFieldOptions]) {
			o.FieldOptions = &models.CrateFieldOptions{ID: true, OrderItems: &models.OrderItemRelationshipOptions{}}
		})
		if err != nil {
			t.Fatalf("GetMany error = %v, want nil", err)
		}
		got := map[int64][]models.OrderItemPK{}
		for _, crate := range crates {
			for _, it := range crate.OrderItems {
				got[crate.ID] = append(got[crate.ID], models.OrderItemPK{OrderID: it.OrderID, ProductID: it.ProductID})
			}
		}
		want := map[int64][]models.OrderItemPK{first.ID: {a}, second.ID: {b}, third.ID: {c}}
		if diff := gocmp.Diff(want, got); diff != "" {
			t.Errorf("items by crate (-want +got):\n%s", diff)
		}
	})

	t.Run("a projection without key columns still loads a composite edge", func(t *testing.T) {
		h.Reset(t)
		a, b, c := compositeItems(t, h)
		createItemNote(t, h, b, "b-1")
		station := createStation(t, h, a.ProductID, "s")
		linkItemStation(t, h, a, station.ID)
		linkItemStation(t, h, c, station.ID)

		items, err := h.Client.OrderItems().GetMany(ctx, nil, func(o *models.CallOptions[models.OrderItemFieldOptions]) {
			o.FieldOptions = &models.OrderItemFieldOptions{
				Quantity:  true,
				ItemNotes: &models.ItemNoteRelationshipOptions{FieldOptions: &models.ItemNoteFieldOptions{Body: true}},
				Stations:  &models.StationRelationshipOptions{FieldOptions: &models.StationFieldOptions{Name: true}},
			}
		})
		if err != nil {
			t.Fatalf("GetMany error = %v, want nil", err)
		}
		if got := noteBodies(items)[b]; !slices.Equal(got, []string{"b-1"}) {
			t.Errorf("notes of %+v = %q, want [b-1]", b, got)
		}
		stations := 0
		for _, it := range items {
			stations += len(it.Stations)
		}
		if stations != 2 {
			t.Errorf("loaded %d station links, want 2", stations)
		}
	})

	t.Run("a child with a NULL key member loads under no parent", func(t *testing.T) {
		h.Reset(t)
		createShelf(t, h, "north", new(int64(1)))
		createParcel(t, h, new("north"), new(int64(1)), "linked")
		createParcel(t, h, new("north"), nil, "no bin")
		createParcel(t, h, nil, new(int64(1)), "no region")

		shelves, err := h.Client.Shelves().GetMany(ctx, nil, func(o *models.CallOptions[models.ShelfFieldOptions]) {
			o.FieldOptions = &models.ShelfFieldOptions{Region: true, Bin: true, Parcels: &models.ParcelRelationshipOptions{}}
		})
		if err != nil {
			t.Fatalf("GetMany error = %v, want nil", err)
		}
		if len(shelves) != 1 || !slices.Equal(parcelLabels(shelves[0]), []string{"linked"}) {
			t.Errorf("GetMany shelves = %+v, want one shelf with the linked parcel", shelves)
		}
	})

	t.Run("a parent whose referenced unique key holds NULL loads no children", func(t *testing.T) {
		h.Reset(t)
		open := createShelf(t, h, "south", nil)
		createShelf(t, h, "north", new(int64(1)))
		createParcel(t, h, new("north"), new(int64(1)), "linked")
		createParcel(t, h, new("south"), nil, "unbinned")

		shelves, err := h.Client.Shelves().GetMany(ctx, nil, func(o *models.CallOptions[models.ShelfFieldOptions]) {
			o.FieldOptions = &models.ShelfFieldOptions{ID: true, Region: true, Bin: true, Parcels: &models.ParcelRelationshipOptions{}}
		})
		if err != nil {
			t.Fatalf("GetMany error = %v, want nil", err)
		}
		if len(shelves) != 2 {
			t.Fatalf("GetMany returned %d shelves, want both", len(shelves))
		}
		for _, s := range shelves {
			want := []string{"linked"}
			if s.ID == open.ID {
				want = []string{}
			}
			if got := parcelLabels(s); !slices.Equal(got, want) {
				t.Errorf("shelf %q parcels = %q, want %q", s.Region, got, want)
			}
		}
	})

	t.Run("an M2M through composite junction keys maps each target to its parents", func(t *testing.T) {
		h.Reset(t)
		a, b, c := compositeItems(t, h)
		// Both stations are on a's product, so a key that dropped order_id
		// would give c a's stations. b's product has its own.
		first, second := createStation(t, h, a.ProductID, "first"), createStation(t, h, a.ProductID, "second")
		other := createStation(t, h, b.ProductID, "other")
		linkItemStation(t, h, a, first.ID)
		linkItemStation(t, h, a, second.ID)
		linkItemStation(t, h, b, other.ID)
		linkItemStation(t, h, c, second.ID)

		items, err := h.Client.OrderItems().GetMany(ctx, nil, func(o *models.CallOptions[models.OrderItemFieldOptions]) {
			o.FieldOptions = &models.OrderItemFieldOptions{OrderID: true, ProductID: true, Stations: &models.StationRelationshipOptions{
				Sorts: []sql.Sort{{Column: "name", Direction: sql.Desc}},
			}}
		})
		if err != nil {
			t.Fatalf("GetMany error = %v, want nil", err)
		}
		got := map[models.OrderItemPK][]string{}
		for _, it := range items {
			pk := models.OrderItemPK{OrderID: it.OrderID, ProductID: it.ProductID}
			for _, s := range it.Stations {
				got[pk] = append(got[pk], s.Name)
			}
		}
		want := map[models.OrderItemPK][]string{a: {"second", "first"}, b: {"other"}, c: {"second"}}
		if diff := gocmp.Diff(want, got); diff != "" {
			t.Errorf("stations by item (-want +got):\n%s", diff)
		}

		// The other direction walks two-column parents into a composite target.
		stations, err := h.Client.Stations().GetMany(ctx, &models.GetStationsInput{
			Filter: &models.StationFilter{ID: &comparator.Number[int64]{Eq: new(second.ID)}},
		}, func(o *models.CallOptions[models.StationFieldOptions]) {
			o.FieldOptions = &models.StationFieldOptions{ID: true, ProductID: true, OrderItems: &models.OrderItemRelationshipOptions{}}
		})
		if err != nil {
			t.Fatalf("GetMany stations error = %v, want nil", err)
		}
		if len(stations) != 1 || len(stations[0].OrderItems) != 2 {
			t.Errorf("station %d loaded %+v, want its two order items", second.ID, stations)
		}
	})

	t.Run("a relationship filter through a composite key matches on every pair", func(t *testing.T) {
		h.Reset(t)
		a, b, c := compositeItems(t, h)
		createItemNote(t, h, a, "match")
		createItemNote(t, h, b, "other")
		createItemNote(t, h, c, "other")
		station := createStation(t, h, c.ProductID, "s")
		linkItemStation(t, h, c, station.ID)

		byNote, err := h.Client.OrderItems().GetMany(ctx, &models.GetOrderItemsInput{
			Filter: &models.OrderItemFilter{ItemNotes: &models.ItemNoteFilter{Body: &comparator.String{Eq: new("match")}}},
		})
		if err != nil {
			t.Fatalf("GetMany by note error = %v, want nil", err)
		}
		if len(byNote) != 1 || byNote[0].OrderID != a.OrderID || byNote[0].ProductID != a.ProductID {
			t.Errorf("GetMany by note = %+v, want only %+v", byNote, a)
		}

		byStation, err := h.Client.OrderItems().GetMany(ctx, &models.GetOrderItemsInput{
			Filter: &models.OrderItemFilter{Stations: &models.StationFilter{Name: &comparator.String{Eq: new("s")}}},
		})
		if err != nil {
			t.Fatalf("GetMany by station error = %v, want nil", err)
		}
		if len(byStation) != 1 || byStation[0].OrderID != c.OrderID || byStation[0].ProductID != c.ProductID {
			t.Errorf("GetMany by station = %+v, want only %+v", byStation, c)
		}
	})

	t.Run("a composite key load past one chunk binds at most its chunk per statement", func(t *testing.T) {
		h.Reset(t)
		chunk := h.Caps.CompositeLoadChunk
		parents := chunk + 17
		product := createProduct(t, h, "bulk")
		station := createStation(t, h, product.ID, "bulk")
		items := make([]*models.CreateOrderItemInput, parents)
		for i := range items {
			items[i] = &models.CreateOrderItemInput{OrderID: int64(i + 1), ProductID: product.ID, Quantity: 1}
		}
		// In batches: CreateMany's read-back renders Filter.PKs as an OR of
		// ANDs, which SQLite's expression depth limit caps near 1000 keys.
		for batch := range slices.Chunk(items, 500) {
			if _, err := h.Client.OrderItems().CreateMany(ctx, batch); err != nil {
				t.Fatalf("CreateMany order items: %v", err)
			}
		}
		// One parent on each side of the chunk boundary, and the last.
		seeded := []int64{1, int64(chunk), int64(chunk + 1), int64(parents)}
		for _, order := range seeded {
			pk := models.OrderItemPK{OrderID: order, ProductID: product.ID}
			createItemNote(t, h, pk, fmt.Sprintf("keep-%d", order))
			createItemNote(t, h, pk, fmt.Sprintf("drop-%d", order))
			linkItemStation(t, h, pk, station.ID)
		}

		h.Querier.ResetCount()
		got, err := h.Client.OrderItems().GetMany(ctx, &models.GetOrderItemsInput{Limit: new(0)}, func(o *models.CallOptions[models.OrderItemFieldOptions]) {
			o.FieldOptions = &models.OrderItemFieldOptions{
				OrderID:   true,
				ProductID: true,
				ItemNotes: &models.ItemNoteRelationshipOptions{Filter: &models.ItemNoteFilter{Body: &comparator.String{StartsWith: new("keep-")}}},
				Stations:  &models.StationRelationshipOptions{},
			}
		})
		if err != nil {
			t.Fatalf("GetMany error = %v, want nil", err)
		}
		if len(got) != parents {
			t.Fatalf("GetMany returned %d items, want %d", len(got), parents)
		}
		for _, it := range got {
			notes, stations := 0, 0
			if slices.Contains(seeded, it.OrderID) {
				notes, stations = 1, 1
				if want := fmt.Sprintf("keep-%d", it.OrderID); len(it.ItemNotes) == 1 && it.ItemNotes[0].Body != want {
					t.Errorf("item %d note = %q, want %q", it.OrderID, it.ItemNotes[0].Body, want)
				}
			}
			if len(it.ItemNotes) != notes || len(it.Stations) != stations {
				t.Errorf("item %d loaded %d notes and %d stations, want %d and %d", it.OrderID, len(it.ItemNotes), len(it.Stations), notes, stations)
			}
		}

		// PRD §25.1: s = C for a composite key on every driver, pgx included.
		const chunks = 2
		for _, table := range []string{"item_notes", "item_stations", "stations"} {
			if got := statementsReading(h, table); got != chunks {
				t.Errorf("statements reading %s = %d, want %d", table, got, chunks)
			}
		}
	})
}
