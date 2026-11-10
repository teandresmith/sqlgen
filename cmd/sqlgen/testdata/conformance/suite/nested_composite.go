package suite

import (
	"context"
	"slices"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/models"
)

// narrowedParents are selections that leave out the columns a nested write keys
// on. Neither may change what is written (PRD §9.6).
var narrowedParents = []struct {
	name  string
	depot *models.DepotFieldOptions
	van   *models.VanFieldOptions
}{
	{name: "one other column", depot: &models.DepotFieldOptions{Name: true}, van: &models.VanFieldOptions{Plate: true}},
	{name: "nothing selected", depot: &models.DepotFieldOptions{}, van: &models.VanFieldOptions{}},
}

func selectDepot(fo *models.DepotFieldOptions) func(*models.CallOptions[models.DepotFieldOptions]) {
	return func(o *models.CallOptions[models.DepotFieldOptions]) { o.FieldOptions = fo }
}

func selectVan(fo *models.VanFieldOptions) func(*models.CallOptions[models.VanFieldOptions]) {
	return func(o *models.CallOptions[models.VanFieldOptions]) { o.FieldOptions = fo }
}

func createDepot(t *testing.T, h *Harness, region, name string) *models.Depot {
	t.Helper()
	d, err := h.Client.Depots().Create(context.Background(), &models.CreateDepotInput{Region: region, Name: name})
	if err != nil {
		t.Fatalf("Create depot %q: %v", name, err)
	}
	return d
}

func createRoute(t *testing.T, h *Harness, region, name string) *models.Route {
	t.Helper()
	r, err := h.Client.Routes().Create(context.Background(), &models.CreateRouteInput{Region: region, Name: name})
	if err != nil {
		t.Fatalf("Create route %q: %v", name, err)
	}
	return r
}

func createVan(t *testing.T, h *Harness, region, plate string) *models.Van {
	t.Helper()
	v, err := h.Client.Vans().Create(context.Background(), &models.CreateVanInput{Region: region, Plate: plate})
	if err != nil {
		t.Fatalf("Create van %q: %v", plate, err)
	}
	return v
}

func createTool(t *testing.T, h *Harness, region, name string) *models.Tool {
	t.Helper()
	tool, err := h.Client.Tools().Create(context.Background(), &models.CreateToolInput{Region: region, Name: name})
	if err != nil {
		t.Fatalf("Create tool %q: %v", name, err)
	}
	return tool
}

func createBay(t *testing.T, h *Harness, region string) *models.Bay {
	t.Helper()
	b, err := h.Client.Bays().Create(context.Background(), &models.CreateBayInput{Region: region})
	if err != nil {
		t.Fatalf("Create bay %q: %v", region, err)
	}
	return b
}

func createLocker(t *testing.T, h *Harness, label string, region *string, bay *int64) *models.Locker {
	t.Helper()
	l, err := h.Client.Lockers().Create(context.Background(), &models.CreateLockerInput{
		Label:     label,
		BayRegion: omittable.Set(region),
		BayID:     omittable.Set(bay),
	})
	if err != nil {
		t.Fatalf("Create locker %q: %v", label, err)
	}
	return l
}

// vanKey is a vans row's key columns.
type vanKey struct {
	Region string
	Depot  *int64
	Route  *int64
}

func assertVan(t *testing.T, h *Harness, id int64, want vanKey) {
	t.Helper()
	v, err := h.Client.Vans().Get(context.Background(), id)
	if err != nil {
		t.Fatalf("Get van %d: %v", id, err)
	}
	if diff := gocmp.Diff(want, vanKey{Region: v.Region, Depot: v.DepotID, Route: v.RouteID}); diff != "" {
		t.Errorf("van %d key (-want +got):\n%s", id, diff)
	}
}

// vansOf returns the vans a depot holds, keyed by plate.
func vansOf(t *testing.T, h *Harness, depotID int64) map[string]vanKey {
	t.Helper()
	rows, err := h.Client.Vans().GetMany(context.Background(), &models.GetVansInput{
		Filter: &models.VanFilter{DepotID: &comparator.NullableNumber[int64]{Eq: new(depotID)}},
	})
	if err != nil {
		t.Fatalf("GetMany vans of depot %d: %v", depotID, err)
	}
	got := make(map[string]vanKey, len(rows))
	for _, v := range rows {
		got[v.Plate] = vanKey{Region: v.Region, Depot: v.DepotID, Route: v.RouteID}
	}
	return got
}

// assertLocker compares a lockers row's two key members.
func assertLocker(t *testing.T, h *Harness, id int64, wantRegion *string, wantBay *int64) {
	t.Helper()
	l, err := h.Client.Lockers().Get(context.Background(), id)
	if err != nil {
		t.Fatalf("Get locker %d: %v", id, err)
	}
	if diff := gocmp.Diff(wantRegion, l.BayRegion); diff != "" {
		t.Errorf("locker %d bay_region (-want +got):\n%s", id, diff)
	}
	if diff := gocmp.Diff(wantBay, l.BayID); diff != "" {
		t.Errorf("locker %d bay_id (-want +got):\n%s", id, diff)
	}
}

// vanToolLinks returns the van_tools rows of one van as tool id to region.
func vanToolLinks(t *testing.T, h *Harness, vanID int64) map[int64]string {
	t.Helper()
	rows, err := h.Client.VanTools().GetMany(context.Background(), &models.GetVanToolsInput{
		Filter: &models.VanToolFilter{VanID: &comparator.Number[int64]{Eq: new(vanID)}},
	})
	if err != nil {
		t.Fatalf("GetMany van tools: %v", err)
	}
	got := make(map[int64]string, len(rows))
	for _, r := range rows {
		got[r.ToolID] = r.Region
	}
	return got
}

// vanPermitCodes returns the codes a van's van_permits rows hold, sorted.
func vanPermitCodes(t *testing.T, h *Harness, vanID int64) []string {
	t.Helper()
	rows, err := h.Client.VanPermits().GetMany(context.Background(), &models.GetVanPermitsInput{
		Filter: &models.VanPermitFilter{VanID: &comparator.Number[int64]{Eq: new(vanID)}},
	})
	if err != nil {
		t.Fatalf("GetMany van permits: %v", err)
	}
	got := make([]string, 0, len(rows))
	for _, r := range rows {
		got = append(got, r.PermitCode)
	}
	slices.Sort(got)
	return got
}

func createWithRelatedCompositeKey(t *testing.T, h *Harness) {
	ctx := context.Background()

	t.Run("a nested create sets every member of a composite key from the parent", func(t *testing.T) {
		h.Reset(t)
		depot, err := h.Client.Depots().CreateWithRelated(ctx, &models.CreateDepotWithRelatedInput{
			Depot: models.CreateDepotInput{Region: "eu", Name: "north"},
			Vans:  &models.DepotVansCreateNested{Create: []*models.DepotVansCreateInput{{Plate: "a"}, {Plate: "b"}}},
		})
		if err != nil {
			t.Fatalf("CreateWithRelated error = %v, want nil", err)
		}
		want := map[string]vanKey{
			"a": {Region: "eu", Depot: &depot.ID},
			"b": {Region: "eu", Depot: &depot.ID},
		}
		if diff := gocmp.Diff(want, vansOf(t, h, depot.ID)); diff != "" {
			t.Errorf("vans (-want +got):\n%s", diff)
		}
	})

	t.Run("a selection that leaves out the key columns still writes the composite edge", func(t *testing.T) {
		for _, sel := range narrowedParents {
			t.Run(sel.name, func(t *testing.T) {
				h.Reset(t)
				orphan := createVan(t, h, "eu", "orphan")
				if _, err := h.Client.Depots().CreateWithRelated(ctx, &models.CreateDepotWithRelatedInput{
					Depot: models.CreateDepotInput{Region: "eu", Name: "north"},
					Vans: &models.DepotVansCreateNested{
						Create:  []*models.DepotVansCreateInput{{Plate: "made"}},
						Connect: []int64{orphan.ID},
					},
				}, selectDepot(sel.depot)); err != nil {
					t.Fatalf("CreateWithRelated error = %v, want nil", err)
				}
				depots, err := h.Client.Depots().GetMany(ctx, &models.GetDepotsInput{})
				if err != nil || len(depots) != 1 {
					t.Fatalf("GetMany depots = %v, %v, want one depot", depots, err)
				}
				id := depots[0].ID
				want := map[string]vanKey{
					"made":   {Region: "eu", Depot: &id},
					"orphan": {Region: "eu", Depot: &id},
				}
				if diff := gocmp.Diff(want, vansOf(t, h, id)); diff != "" {
					t.Errorf("vans (-want +got):\n%s", diff)
				}

				tool := createTool(t, h, "eu", "connected")
				if _, err := h.Client.Vans().CreateWithRelated(ctx, &models.CreateVanWithRelatedInput{
					Van:     models.CreateVanInput{Region: "eu", Plate: "linked"},
					Tools:   &models.VanToolsCreateNested{Create: []*models.CreateToolInput{{Region: "eu", Name: "made"}}, Connect: []int64{tool.ID}},
					Permits: &models.VanPermitsCreateNested{Create: []*models.CreatePermitInput{{Code: "p1"}}},
				}, selectVan(sel.van)); err != nil {
					t.Fatalf("CreateWithRelated van error = %v, want nil", err)
				}
				vans, err := h.Client.Vans().GetMany(ctx, &models.GetVansInput{
					Filter: &models.VanFilter{Plate: &comparator.String{Eq: new("linked")}},
				})
				if err != nil || len(vans) != 1 {
					t.Fatalf("GetMany vans = %v, %v, want one van", vans, err)
				}
				links := vanToolLinks(t, h, vans[0].ID)
				if len(links) != 2 {
					t.Errorf("van tools = %v, want the made and the connected tool", links)
				}
				for tool, region := range links {
					if region != "eu" {
						t.Errorf("link to tool %d has region %q, want eu", tool, region)
					}
				}
				if diff := gocmp.Diff([]string{"p1"}, vanPermitCodes(t, h, vans[0].ID)); diff != "" {
					t.Errorf("van permits (-want +got):\n%s", diff)
				}
			})
		}
	})
}

func updateWithRelatedCompositeKey(t *testing.T, h *Harness) {
	ctx := context.Background()

	connect := func(t *testing.T, depot *models.Depot, ids ...int64) error {
		t.Helper()
		_, err := h.Client.Depots().UpdateWithRelated(ctx, depot.ID, &models.UpdateDepotWithRelatedInput{
			Vans: &models.DepotVansUpdateNested{Connect: ids},
		})
		return err
	}
	connectRoute := func(t *testing.T, route *models.Route, ids ...int64) error {
		t.Helper()
		_, err := h.Client.Routes().UpdateWithRelated(ctx, route.ID, &models.UpdateRouteWithRelatedInput{
			Vans: &models.RouteVansUpdateNested{Connect: ids},
		})
		return err
	}

	t.Run("connect writes only the free member of a composite key, and a repeat is a no-op", func(t *testing.T) {
		h.Reset(t)
		depot := createDepot(t, h, "eu", "north")
		a, b, c := createVan(t, h, "eu", "a"), createVan(t, h, "eu", "b"), createVan(t, h, "eu", "c")

		// Longer than the depot's batch size, with a repeated id.
		if err := connect(t, depot, a.ID, b.ID, c.ID, a.ID); err != nil {
			t.Fatalf("UpdateWithRelated(connect) error = %v, want nil", err)
		}
		for _, id := range []int64{a.ID, b.ID, c.ID} {
			assertVan(t, h, id, vanKey{Region: "eu", Depot: &depot.ID})
		}
		if err := connect(t, depot, a.ID); err != nil {
			t.Errorf("connecting a van the depot holds error = %v, want nil", err)
		}
	})

	t.Run("allow_reparent moves the free member of a composite key and leaves the other edge's member", func(t *testing.T) {
		h.Reset(t)
		depot := createDepot(t, h, "eu", "north")
		first, second := createRoute(t, h, "eu", "first"), createRoute(t, h, "eu", "second")
		van := createVan(t, h, "eu", "a")
		if err := connect(t, depot, van.ID); err != nil {
			t.Fatalf("connect to depot: %v", err)
		}
		for _, route := range []*models.Route{first, second} {
			if err := connectRoute(t, route, van.ID); err != nil {
				t.Fatalf("connect to route %q: %v", route.Name, err)
			}
			assertVan(t, h, van.ID, vanKey{Region: "eu", Depot: &depot.ID, Route: &route.ID})
		}
	})

	t.Run("connect naming a target whose pinned member differs is ErrAlreadyRelated naming it", func(t *testing.T) {
		h.Reset(t)
		depot, route := createDepot(t, h, "eu", "north"), createRoute(t, h, "eu", "north")
		van := createVan(t, h, "us", "a")

		uwrWantNested(t, connect(t, depot, van.ID), models.ErrAlreadyRelated, "Vans", "connect", van.ID)
		// allow_reparent never moves a pinned member.
		uwrWantNested(t, connectRoute(t, route, van.ID), models.ErrAlreadyRelated, "Vans", "connect", van.ID)
		assertVan(t, h, van.ID, vanKey{Region: "us"})
	})

	t.Run("connect naming a target held through a composite key by another parent is ErrAlreadyRelated naming it", func(t *testing.T) {
		h.Reset(t)
		holder, other := createDepot(t, h, "eu", "holder"), createDepot(t, h, "eu", "other")
		van := createVan(t, h, "eu", "a")
		if err := connect(t, holder, van.ID); err != nil {
			t.Fatalf("connect: %v", err)
		}
		uwrWantNested(t, connect(t, other, van.ID), models.ErrAlreadyRelated, "Vans", "connect", van.ID)
		assertVan(t, h, van.ID, vanKey{Region: "eu", Depot: &holder.ID})
	})

	t.Run("connect naming a target holding a partly NULL key is ErrAlreadyRelated naming it", func(t *testing.T) {
		h.Reset(t)
		bay := createBay(t, h, "eu")
		locker := createLocker(t, h, "a", new("eu"), nil)

		_, err := h.Client.Bays().UpdateWithRelated(ctx, bay.ID, &models.UpdateBayWithRelatedInput{
			Lockers: &models.BayLockersUpdateNested{Connect: []int64{locker.ID}},
		})
		uwrWantNested(t, err, models.ErrAlreadyRelated, "Lockers", "connect", locker.ID)
		assertLocker(t, h, locker.ID, new("eu"), nil)
	})

	t.Run("allow_reparent adopts a target holding a partly NULL key", func(t *testing.T) {
		tests := []struct {
			name   string
			region *string
			holdID bool // the slot holds another bay's id
		}{
			{name: "region held", region: new("us")},
			{name: "id held", holdID: true},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				h.Reset(t)
				bay, other := createBay(t, h, "eu"), createBay(t, h, "us")
				var heldID *int64
				if tt.holdID {
					heldID = &other.ID
				}
				slot, err := h.Client.Slots().Create(ctx, &models.CreateSlotInput{
					Label:     "s",
					BayRegion: omittable.Set(tt.region),
					BayID:     omittable.Set(heldID),
				})
				if err != nil {
					t.Fatalf("Create slot: %v", err)
				}

				if _, err := h.Client.Bays().UpdateWithRelated(ctx, bay.ID, &models.UpdateBayWithRelatedInput{
					Slots: &models.BaySlotsUpdateNested{Connect: []int64{slot.ID}},
				}); err != nil {
					t.Fatalf("UpdateWithRelated(connect) error = %v, want nil", err)
				}
				got, err := h.Client.Slots().Get(ctx, slot.ID)
				if err != nil {
					t.Fatalf("Get slot: %v", err)
				}
				if diff := gocmp.Diff(new("eu"), got.BayRegion); diff != "" {
					t.Errorf("slot bay_region (-want +got):\n%s", diff)
				}
				if diff := gocmp.Diff(&bay.ID, got.BayID); diff != "" {
					t.Errorf("slot bay_id (-want +got):\n%s", diff)
				}
			})
		}
	})

	t.Run("disconnect and clear null only the free member and leave another parent's row", func(t *testing.T) {
		h.Reset(t)
		depot, other := createDepot(t, h, "eu", "north"), createDepot(t, h, "eu", "other")
		route := createRoute(t, h, "eu", "r")
		one, two, held := createVan(t, h, "eu", "one"), createVan(t, h, "eu", "two"), createVan(t, h, "eu", "held")
		if err := connect(t, depot, one.ID, two.ID); err != nil {
			t.Fatalf("connect: %v", err)
		}
		if err := connect(t, other, held.ID); err != nil {
			t.Fatalf("connect: %v", err)
		}
		if err := connectRoute(t, route, one.ID); err != nil {
			t.Fatalf("connect: %v", err)
		}

		// Naming another parent's van is a no-op.
		if _, err := h.Client.Depots().UpdateWithRelated(ctx, depot.ID, &models.UpdateDepotWithRelatedInput{
			Vans: &models.DepotVansUpdateNested{Disconnect: []int64{one.ID, held.ID}},
		}); err != nil {
			t.Fatalf("UpdateWithRelated(disconnect) error = %v, want nil", err)
		}
		assertVan(t, h, one.ID, vanKey{Region: "eu", Route: &route.ID})
		assertVan(t, h, two.ID, vanKey{Region: "eu", Depot: &depot.ID})
		assertVan(t, h, held.ID, vanKey{Region: "eu", Depot: &other.ID})

		if _, err := h.Client.Depots().UpdateWithRelated(ctx, depot.ID, &models.UpdateDepotWithRelatedInput{
			Vans: &models.DepotVansUpdateNested{Clear: true},
		}); err != nil {
			t.Fatalf("UpdateWithRelated(clear) error = %v, want nil", err)
		}
		assertVan(t, h, two.ID, vanKey{Region: "eu"})
		assertVan(t, h, held.ID, vanKey{Region: "eu", Depot: &other.ID})
	})

	t.Run("connect, disconnect and clear write every free member of a key", func(t *testing.T) {
		h.Reset(t)
		bay := createBay(t, h, "eu")
		one, two := createLocker(t, h, "one", nil, nil), createLocker(t, h, "two", nil, nil)
		update := func(block *models.BayLockersUpdateNested) {
			t.Helper()
			if _, err := h.Client.Bays().UpdateWithRelated(ctx, bay.ID, &models.UpdateBayWithRelatedInput{Lockers: block}); err != nil {
				t.Fatalf("UpdateWithRelated(%+v) error = %v, want nil", block, err)
			}
		}

		update(&models.BayLockersUpdateNested{Connect: []int64{one.ID, two.ID}})
		assertLocker(t, h, one.ID, new("eu"), &bay.ID)
		assertLocker(t, h, two.ID, new("eu"), &bay.ID)

		update(&models.BayLockersUpdateNested{Disconnect: []int64{one.ID}})
		assertLocker(t, h, one.ID, nil, nil)
		assertLocker(t, h, two.ID, new("eu"), &bay.ID)

		update(&models.BayLockersUpdateNested{Clear: true})
		assertLocker(t, h, two.ID, nil, nil)
	})

	t.Run("a junction whose keys share a column links and unlinks through every column", func(t *testing.T) {
		h.Reset(t)
		van := createVan(t, h, "eu", "a")
		connected := createTool(t, h, "eu", "connected")
		update := func(block *models.VanToolsUpdateNested) {
			t.Helper()
			if _, err := h.Client.Vans().UpdateWithRelated(ctx, van.ID, &models.UpdateVanWithRelatedInput{Tools: block}); err != nil {
				t.Fatalf("UpdateWithRelated(%+v) error = %v, want nil", block, err)
			}
		}

		update(&models.VanToolsUpdateNested{
			Connect: []int64{connected.ID},
			Create:  []*models.CreateToolInput{{Region: "eu", Name: "made"}},
		})
		got := vanToolLinks(t, h, van.ID)
		if len(got) != 2 || got[connected.ID] != "eu" {
			t.Fatalf("links = %v, want the connected and the made tool in eu", got)
		}

		update(&models.VanToolsUpdateNested{Connect: []int64{connected.ID}})
		if n := len(vanToolLinks(t, h, van.ID)); n != 2 {
			t.Errorf("links after a re-link = %d, want 2", n)
		}

		update(&models.VanToolsUpdateNested{Disconnect: []int64{connected.ID}})
		got = vanToolLinks(t, h, van.ID)
		if _, kept := got[connected.ID]; kept || len(got) != 1 {
			t.Errorf("links after disconnect = %v, want the made tool alone", got)
		}

		update(&models.VanToolsUpdateNested{Clear: true})
		if n := len(vanToolLinks(t, h, van.ID)); n != 0 {
			t.Errorf("links after clear = %d, want 0", n)
		}
	})

	t.Run("connect naming a target that holds another value of the shared column is ErrAlreadyRelated before the link step, and the junction's foreign key refuses the row", func(t *testing.T) {
		h.Reset(t)
		van := createVan(t, h, "eu", "a")
		own := createTool(t, h, "eu", "own")
		foreign := createTool(t, h, "us", "foreign")

		_, err := h.Client.Vans().UpdateWithRelated(ctx, van.ID, &models.UpdateVanWithRelatedInput{
			Tools: &models.VanToolsUpdateNested{Connect: []int64{own.ID, foreign.ID}},
		})
		uwrWantNested(t, err, models.ErrAlreadyRelated, "Tools", "connect", foreign.ID)
		if n := len(vanToolLinks(t, h, van.ID)); n != 0 {
			t.Errorf("links = %d, want 0", n)
		}

		_, err = h.Client.VanTools().Create(ctx, &models.CreateVanToolInput{Region: "eu", VanID: van.ID, ToolID: foreign.ID})
		uwrAssertConstraint(t, err, models.ConstraintForeignKey)
	})

	t.Run("the link step reads a junction column off the target on create and connect", func(t *testing.T) {
		h.Reset(t)
		van := createVan(t, h, "eu", "a")
		permit, err := h.Client.Permits().Create(ctx, &models.CreatePermitInput{Code: "connected"})
		if err != nil {
			t.Fatalf("Create permit error = %v, want nil", err)
		}
		if _, err := h.Client.Vans().UpdateWithRelated(ctx, van.ID, &models.UpdateVanWithRelatedInput{
			Permits: &models.VanPermitsUpdateNested{
				Connect: []int64{permit.ID},
				Create:  []*models.CreatePermitInput{{Code: "created"}},
			},
		}); err != nil {
			t.Fatalf("UpdateWithRelated error = %v, want nil", err)
		}
		if diff := gocmp.Diff([]string{"connected", "created"}, vanPermitCodes(t, h, van.ID)); diff != "" {
			t.Errorf("van permits (-want +got):\n%s", diff)
		}
	})

	t.Run("a selection that leaves out the key columns still writes the composite edge", func(t *testing.T) {
		for _, sel := range narrowedParents {
			t.Run(sel.name, func(t *testing.T) {
				h.Reset(t)
				depot := createDepot(t, h, "eu", "north")
				van := createVan(t, h, "eu", "a")
				tool := createTool(t, h, "eu", "t")
				update := func(block *models.DepotVansUpdateNested) {
					t.Helper()
					if _, err := h.Client.Depots().UpdateWithRelated(ctx, depot.ID, &models.UpdateDepotWithRelatedInput{Vans: block}, selectDepot(sel.depot)); err != nil {
						t.Fatalf("UpdateWithRelated(%+v) error = %v, want nil", block, err)
					}
				}
				tools := func(block *models.VanToolsUpdateNested) {
					t.Helper()
					if _, err := h.Client.Vans().UpdateWithRelated(ctx, van.ID, &models.UpdateVanWithRelatedInput{Tools: block}, selectVan(sel.van)); err != nil {
						t.Fatalf("UpdateWithRelated(%+v) error = %v, want nil", block, err)
					}
				}

				update(&models.DepotVansUpdateNested{Connect: []int64{van.ID}})
				assertVan(t, h, van.ID, vanKey{Region: "eu", Depot: &depot.ID})
				update(&models.DepotVansUpdateNested{Disconnect: []int64{van.ID}})
				assertVan(t, h, van.ID, vanKey{Region: "eu"})
				update(&models.DepotVansUpdateNested{Connect: []int64{van.ID}})
				update(&models.DepotVansUpdateNested{Clear: true})
				assertVan(t, h, van.ID, vanKey{Region: "eu"})
				update(&models.DepotVansUpdateNested{Create: []*models.DepotVansCreateInput{{Plate: "made"}}})
				if diff := gocmp.Diff(map[string]vanKey{"made": {Region: "eu", Depot: &depot.ID}}, vansOf(t, h, depot.ID)); diff != "" {
					t.Errorf("vans (-want +got):\n%s", diff)
				}

				tools(&models.VanToolsUpdateNested{Connect: []int64{tool.ID}})
				if diff := gocmp.Diff(map[int64]string{tool.ID: "eu"}, vanToolLinks(t, h, van.ID)); diff != "" {
					t.Errorf("links (-want +got):\n%s", diff)
				}
				tools(&models.VanToolsUpdateNested{Disconnect: []int64{tool.ID}})
				if n := len(vanToolLinks(t, h, van.ID)); n != 0 {
					t.Errorf("links after disconnect = %d, want 0", n)
				}
				tools(&models.VanToolsUpdateNested{Connect: []int64{tool.ID}})
				tools(&models.VanToolsUpdateNested{Clear: true})
				if n := len(vanToolLinks(t, h, van.ID)); n != 0 {
					t.Errorf("links after clear = %d, want 0", n)
				}
			})
		}
	})
}

func upsertWithRelatedCompositeKey(t *testing.T, h *Harness) {
	ctx := context.Background()

	t.Run("a selection that leaves out the key columns still writes the composite edge", func(t *testing.T) {
		for _, sel := range narrowedParents {
			t.Run(sel.name, func(t *testing.T) {
				h.Reset(t)
				upsert := func(block *models.DepotVansUpdateNested) {
					t.Helper()
					if _, err := h.Client.Depots().UpsertWithRelated(ctx, &models.UpsertDepotWithRelatedInput{
						Depot: models.CreateDepotInput{Region: "eu", Name: "north"},
						Vans:  block,
					}, models.DepotConflictName, selectDepot(sel.depot)); err != nil {
						t.Fatalf("UpsertWithRelated(%+v) error = %v, want nil", block, err)
					}
				}

				// The insert branch, then the conflict branch against that row.
				upsert(&models.DepotVansUpdateNested{Create: []*models.DepotVansCreateInput{{Plate: "made"}}})
				orphan := createVan(t, h, "eu", "orphan")
				upsert(&models.DepotVansUpdateNested{Connect: []int64{orphan.ID}})

				depots, err := h.Client.Depots().GetMany(ctx, &models.GetDepotsInput{})
				if err != nil || len(depots) != 1 {
					t.Fatalf("GetMany depots = %v, %v, want one depot", depots, err)
				}
				id := depots[0].ID
				want := map[string]vanKey{
					"made":   {Region: "eu", Depot: &id},
					"orphan": {Region: "eu", Depot: &id},
				}
				if diff := gocmp.Diff(want, vansOf(t, h, id)); diff != "" {
					t.Errorf("vans (-want +got):\n%s", diff)
				}
			})
		}
	})
}
