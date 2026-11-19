package featuressuite

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cache"
	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/database"
	"github.com/teandresmith/sqlgen/event"
	"github.com/teandresmith/sqlgen/event/memorybus"
	"github.com/teandresmith/sqlgen/hook"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/features/models"
)

// cacheTree: mutation invalidation and write-through (PRD §27.5, §27.7,
// §27.9, §29.5) and event-driven invalidation (§28.11). MySQL captures the
// affected keys and tenants by pre-read where PostgreSQL and SQLite use
// RETURNING, and PostgreSQL keys carry a schema segment, so every case runs on
// every target.
func cacheTree(t *testing.T, h *Harness) {
	t.Run("Create", func(t *testing.T) {
		t.Run("writes the created row through under its own key", func(t *testing.T) {
			checkWriteThrough(t, h, "notes Create", func(r *cacheRun) []cachedRow {
				n, err := r.env.Client.Notes().Create(r.ctx, &models.CreateNoteInput{WorkspaceID: r.workspace(0), Slug: "a", Body: "a"}, r.noteOpts()...)
				if err != nil {
					r.t.Fatalf("Create() error = %v", err)
				}
				return []cachedRow{{r.tenants[0], n.ID}}
			})
		})
	})
	t.Run("CreateMany", func(t *testing.T) {
		t.Run("writes the created row through under its own key", func(t *testing.T) {
			checkWriteThrough(t, h, "notes CreateMany", func(r *cacheRun) []cachedRow {
				var inputs []*models.CreateNoteInput
				for i := range r.tenants {
					s := fmt.Sprintf("new-%d", i)
					inputs = append(inputs, &models.CreateNoteInput{WorkspaceID: r.workspace(i), Slug: s, Body: s})
				}
				created, err := r.env.Client.Notes().CreateMany(r.ctx, inputs, r.noteOpts()...)
				if err != nil {
					r.t.Fatalf("CreateMany() error = %v", err)
				}
				var rows []cachedRow
				for i, n := range created {
					rows = append(rows, cachedRow{r.tenants[i], n.ID})
				}
				return rows
			})
		})
	})

	evictions := []struct {
		method string
		table  string
		write  func(r *cacheRun) []cachedRow
	}{
		{"Update", "notes", func(r *cacheRun) []cachedRow {
			rows := r.seedNotes()
			if _, err := r.env.Client.Notes().Update(r.ctx, rows[0].pk.(int64), &models.UpdateNoteInput{Body: omittable.Set("changed")}, r.noteOpts()...); err != nil {
				r.t.Fatalf("Update() error = %v", err)
			}
			return rows[:1]
		}},
		{"UpdateMany", "notes", func(r *cacheRun) []cachedRow {
			rows := r.seedNotes()
			var items []models.UpdateNoteItem
			for _, row := range rows {
				items = append(items, models.UpdateNoteItem{ID: row.pk.(int64), Input: &models.UpdateNoteInput{Body: omittable.Set("changed")}})
			}
			if _, err := r.env.Client.Notes().UpdateMany(r.ctx, items, r.noteOpts()...); err != nil {
				r.t.Fatalf("UpdateMany() error = %v", err)
			}
			return rows
		}},
		{"UpdateWhere", "notes", func(r *cacheRun) []cachedRow {
			rows := r.seedNotes()
			if _, err := r.env.Client.Notes().UpdateWhere(r.ctx, &models.NoteFilter{ID: &comparator.Number[int64]{In: rowIDs(rows)}},
				&models.UpdateNoteInput{Body: omittable.Set("changed")}, r.noteOpts()...); err != nil {
				r.t.Fatalf("UpdateWhere() error = %v", err)
			}
			return rows
		}},
		{"Upsert", "notes", func(r *cacheRun) []cachedRow {
			rows := r.seedNotes()
			notes := r.env.Client.Notes()
			// The first takes the update branch on the seeded slug, the second
			// inserts.
			updated, err := notes.Upsert(r.ctx, &models.CreateNoteInput{WorkspaceID: r.workspace(0), Slug: "seed-0", Body: "changed"},
				models.NoteConflictWorkspaceIDSlug, r.noteOpts()...)
			if err != nil {
				r.t.Fatalf("Upsert() updating error = %v", err)
			}
			if updated.ID != rows[0].pk {
				r.t.Fatalf("Upsert() updating returned key %d, want the seeded %v", updated.ID, rows[0].pk)
			}
			inserted, err := notes.Upsert(r.ctx, &models.CreateNoteInput{WorkspaceID: r.workspace(1), Slug: "fresh", Body: "fresh"},
				models.NoteConflictWorkspaceIDSlug, r.noteOpts()...)
			if err != nil {
				r.t.Fatalf("Upsert() inserting error = %v", err)
			}
			r.calls = 2
			return []cachedRow{rows[0], {r.tenants[1], inserted.ID}}
		}},
		{"UpsertMany", "notes", func(r *cacheRun) []cachedRow {
			rows := r.seedNotes()
			// Every seeded slug conflicts, so each row takes the update branch.
			var inputs []*models.CreateNoteInput
			for i := range rows {
				inputs = append(inputs, &models.CreateNoteInput{WorkspaceID: r.workspace(i), Slug: fmt.Sprintf("seed-%d", i), Body: "changed"})
			}
			if _, err := r.env.Client.Notes().UpsertMany(r.ctx, inputs, models.NoteConflictWorkspaceIDSlug, r.noteOpts()...); err != nil {
				r.t.Fatalf("UpsertMany() error = %v", err)
			}
			return rows
		}},
		{"Increment", "notes", func(r *cacheRun) []cachedRow {
			rows := r.seedNotes()
			if err := r.env.Client.Notes().Increment(r.ctx, rows[0].pk.(int64),
				models.IncrementInput[models.NoteIncrementColumn]{Column: models.NoteIncrementViews, Amount: 1}, r.noteOpts()...); err != nil {
				r.t.Fatalf("Increment() error = %v", err)
			}
			return rows[:1]
		}},
		{"SoftDelete", "tasks", func(r *cacheRun) []cachedRow {
			rows := r.seedTasks()
			if _, err := r.env.Client.Tasks().SoftDelete(r.ctx, rows[0].pk.(int64), r.taskOpts()...); err != nil {
				r.t.Fatalf("SoftDelete() error = %v", err)
			}
			return rows[:1]
		}},
		{"SoftDeleteMany", "tasks", func(r *cacheRun) []cachedRow {
			rows := r.seedTasks()
			if _, err := r.env.Client.Tasks().SoftDeleteMany(r.ctx, rowIDs(rows), r.taskOpts()...); err != nil {
				r.t.Fatalf("SoftDeleteMany() error = %v", err)
			}
			return rows
		}},
		{"SoftDeleteWhere", "tasks", func(r *cacheRun) []cachedRow {
			rows := r.seedTasks()
			if _, err := r.env.Client.Tasks().SoftDeleteWhere(r.ctx, &models.TaskFilter{ID: &comparator.Number[int64]{In: rowIDs(rows)}}, r.taskOpts()...); err != nil {
				r.t.Fatalf("SoftDeleteWhere() error = %v", err)
			}
			return rows
		}},
		{"HardDelete", "notes", func(r *cacheRun) []cachedRow {
			rows := r.seedNotes()
			if err := r.env.Client.Notes().HardDelete(r.ctx, rows[0].pk.(int64), r.noteOpts()...); err != nil {
				r.t.Fatalf("HardDelete() error = %v", err)
			}
			return rows[:1]
		}},
		{"HardDeleteMany", "notes", func(r *cacheRun) []cachedRow {
			rows := r.seedNotes()
			if err := r.env.Client.Notes().HardDeleteMany(r.ctx, rowIDs(rows), r.noteOpts()...); err != nil {
				r.t.Fatalf("HardDeleteMany() error = %v", err)
			}
			return rows
		}},
		{"HardDeleteWhere", "notes", func(r *cacheRun) []cachedRow {
			rows := r.seedNotes()
			if err := r.env.Client.Notes().HardDeleteWhere(r.ctx, &models.NoteFilter{ID: &comparator.Number[int64]{In: rowIDs(rows)}}, r.noteOpts()...); err != nil {
				r.t.Fatalf("HardDeleteWhere() error = %v", err)
			}
			return rows
		}},
		{"Restore", "tasks", func(r *cacheRun) []cachedRow {
			rows := r.seedTrashedTasks()
			if _, err := r.env.Client.Tasks().Restore(r.ctx, rows[0].pk.(int64), r.taskOpts()...); err != nil {
				r.t.Fatalf("Restore() error = %v", err)
			}
			return rows[:1]
		}},
		{"RestoreMany", "tasks", func(r *cacheRun) []cachedRow {
			rows := r.seedTrashedTasks()
			if _, err := r.env.Client.Tasks().RestoreMany(r.ctx, rowIDs(rows), r.taskOpts()...); err != nil {
				r.t.Fatalf("RestoreMany() error = %v", err)
			}
			return rows
		}},
		{"RestoreWhere", "tasks", func(r *cacheRun) []cachedRow {
			rows := r.seedTrashedTasks()
			if _, err := r.env.Client.Tasks().RestoreWhere(r.ctx, &models.TaskFilter{ID: &comparator.Number[int64]{In: rowIDs(rows)}}, r.taskOpts()...); err != nil {
				r.t.Fatalf("RestoreWhere() error = %v", err)
			}
			return rows
		}},
	}
	for _, e := range evictions {
		t.Run(e.method, func(t *testing.T) {
			t.Run("evicts each affected row's key", func(t *testing.T) {
				checkEvictions(t, h, e.table+" "+e.method, e.table, e.write)
			})
		})
	}

	cacheUnbuildableKey(t, h)

	t.Run("a write that affects no row invalidates no key and no view pattern", func(t *testing.T) {
		h.Reset(t)
		env := h.NewEnv(t, WithCache())
		ctx := WithTenant(context.Background(), models.NewTenant())
		tasks := env.Client.Tasks()
		created, err := tasks.CreateMany(ctx, []*models.CreateTaskInput{{Title: "live"}, {Title: "trashed"}})
		if err != nil {
			t.Fatalf("CreateMany() error = %v", err)
		}
		live, trashed := created[0], created[1]
		if _, err := tasks.SoftDelete(ctx, trashed.ID); err != nil {
			t.Fatalf("SoftDelete() error = %v", err)
		}
		const missing int64 = 987_654_321
		noTask := &models.TaskFilter{ID: &comparator.Number[int64]{Eq: new(missing)}}
		// tasks is the source table of the cached view active_tasks, so each
		// of these writes would cascade its view pattern if it invalidated.
		writes := []struct {
			name  string
			write func() error
		}{
			{"Update of a missing key", func() error {
				_, err := tasks.Update(ctx, missing, &models.UpdateTaskInput{Title: omittable.Set("x")})
				return err
			}},
			{"UpdateMany of a missing key", func() error {
				_, err := tasks.UpdateMany(ctx, []models.UpdateTaskItem{{ID: missing, Input: &models.UpdateTaskInput{Title: omittable.Set("x")}}})
				return err
			}},
			{"UpdateWhere matching no row", func() error {
				_, err := tasks.UpdateWhere(ctx, noTask, &models.UpdateTaskInput{Title: omittable.Set("x")})
				return err
			}},
			{"SoftDelete of a missing key", func() error { _, err := tasks.SoftDelete(ctx, missing); return err }},
			{"SoftDeleteMany of a missing key", func() error { _, err := tasks.SoftDeleteMany(ctx, []int64{missing}); return err }},
			{"SoftDeleteWhere matching no row", func() error { _, err := tasks.SoftDeleteWhere(ctx, noTask); return err }},
			{"SoftDeleteWhere matching only a soft-deleted row", func() error {
				_, err := tasks.SoftDeleteWhere(ctx, &models.TaskFilter{ID: &comparator.Number[int64]{Eq: &trashed.ID}})
				return err
			}},
			{"Restore of a missing key", func() error { _, err := tasks.Restore(ctx, missing); return err }},
			{"RestoreMany of a missing key", func() error { _, err := tasks.RestoreMany(ctx, []int64{missing}); return err }},
			{"RestoreWhere matching no row", func() error { _, err := tasks.RestoreWhere(ctx, noTask); return err }},
			{"RestoreWhere matching only a live row", func() error {
				_, err := tasks.RestoreWhere(ctx, &models.TaskFilter{ID: &comparator.Number[int64]{Eq: &live.ID}})
				return err
			}},
			{"HardDelete of a missing key", func() error { return tasks.HardDelete(ctx, missing) }},
			{"HardDeleteMany of a missing key", func() error { return tasks.HardDeleteMany(ctx, []int64{missing}) }},
			{"HardDeleteWhere matching no row", func() error { return tasks.HardDeleteWhere(ctx, noTask) }},
		}
		for _, w := range writes {
			env.Backend.Clear()
			// A strict Update and a single-row delete report the missing row;
			// either way the write affected nothing.
			if err := w.write(); err != nil && !errors.Is(err, models.ErrNotFound) {
				t.Errorf("%s: error = %v, want nil or ErrNotFound", w.name, err)
			}
			if got := env.Backend.Invalidations(); len(got) != 0 {
				t.Errorf("%s: backend invalidations = %+v, want none", w.name, got)
			}
		}
	})

	t.Run("event-driven invalidation", func(t *testing.T) { cacheReceive(t, h) })
}

func cacheUnbuildableKey(t *testing.T, h *Harness) {
	// The hook runs inside the cache hook: after the write it swaps each
	// affected key of the untenanted regions table for one of the wrong type.
	corrupt := func(next hook.MutationHandler) hook.MutationHandler {
		return func(ctx context.Context, m *hook.MutationContext) (any, error) {
			res, err := next(ctx, m)
			if m.Table == models.TableRegions && m.Op == hook.OpUpdate {
				for i, pk := range m.AffectedPKs {
					m.AffectedPKs[i] = int32(pk.(int64))
				}
			}
			return res, err
		}
	}
	modes := []struct {
		name string
		inTx bool
	}{
		{"an unbuildable key is reported once outside a transaction", false},
		{"an unbuildable key is reported once inside a sync transaction", true},
	}
	for _, m := range modes {
		t.Run(m.name, func(t *testing.T) {
			h.Reset(t)
			env := h.NewEnv(t, WithCache(), WithMutationHook(corrupt), WithClientOption(models.WithCallbackMode(database.CallbackSync)))
			ctx := context.Background()
			region, err := env.Client.Regions().Create(ctx, &models.CreateRegionInput{Name: "before"})
			if err != nil {
				t.Fatalf("Create() error = %v", err)
			}
			env.Backend.Clear()
			update := func(ctx context.Context) error {
				_, err := env.Client.Regions().Update(ctx, region.ID, &models.UpdateRegionInput{Name: omittable.Set("after")})
				return err
			}
			if m.inTx {
				err = env.Client.WithTx(ctx, "bad-key", update)
			} else {
				err = update(ctx)
			}
			if err != nil {
				t.Fatalf("write error = %v, want nil", err)
			}
			want := []string{fmt.Sprintf("invalidate_many %s: cache: InvalidateMany(%q): cache: pk at index 0: expected int64, got int32",
				models.TableRegions, models.TableRegions)}
			var got []string
			for _, e := range env.CacheErrors.All() {
				got = append(got, fmt.Sprintf("%s %s: %v", e.Op, e.Table, e.Err))
			}
			if !slices.Equal(got, want) {
				t.Errorf("OnErrorFunc reports = %q, want %q", got, want)
			}
			if calls := env.Backend.Invalidations(); len(calls) != 0 {
				t.Errorf("backend invalidations = %+v, want none: the key was never built", calls)
			}
		})
	}
}

// cacheReceive: a cache fed by FromEventSubscriber evicts what the events of
// another client's writes name (PRD §28.11). The writing client has no cache,
// so every eviction on the receiving cache came through the bus.
func cacheReceive(t *testing.T, h *Harness) {
	type instances struct {
		writer, receiver *Env
		results          *HandlerResults
		seed             *models.Client
	}
	setup := func(t *testing.T, stripTenant bool) instances {
		t.Helper()
		h.Reset(t)
		bus := memorybus.New()
		t.Cleanup(func() { _ = bus.Close() })
		var pub event.Publisher = bus
		if stripTenant {
			pub = tenantStripper{bus}
		}
		results := &HandlerResults{Inner: cache.FromEventSubscriber(bus)}
		return instances{
			writer:   h.NewEnv(t, WithEventPublisher(pub)),
			receiver: h.NewEnv(t, WithCacheOptions(models.WithInvalidationSource(results))),
			results:  results,
			// Seeds publish nothing, so the receiver sees only the write.
			seed: h.Client(t),
		}
	}
	twoNotes := func(t *testing.T, in instances) (a, b models.Tenant, noteA, noteB *models.Note, keyA string) {
		t.Helper()
		a, b = models.NewTenant(), models.NewTenant()
		var err error
		if noteA, err = in.seed.Notes().Create(WithTenant(context.Background(), a), &models.CreateNoteInput{Slug: "a", Body: "a"}); err != nil {
			t.Fatalf("Create(note a) error = %v", err)
		}
		if noteB, err = in.seed.Notes().Create(WithTenant(context.Background(), b), &models.CreateNoteInput{Slug: "b", Body: "b"}); err != nil {
			t.Fatalf("Create(note b) error = %v", err)
		}
		keyA = warm(t, in.receiver, func() error {
			_, err := in.receiver.Client.Notes().Get(WithTenant(context.Background(), a), noteA.ID)
			return err
		})
		warm(t, in.receiver, func() error {
			_, err := in.receiver.Client.Notes().Get(WithTenant(context.Background(), b), noteB.ID)
			return err
		})
		h.checkKey(t, keyA, "notes", a, noteA.ID)
		in.receiver.Backend.Clear()
		return a, b, noteA, noteB, keyA
	}
	handlerReturnedNil := func(t *testing.T, in instances) {
		t.Helper()
		results := in.results.Results()
		if len(results) != 1 || results[0] != nil {
			t.Errorf("handler results = %v, want one nil", results)
		}
	}

	t.Run("a tenant-stamped event evicts that row's key in its tenant only", func(t *testing.T) {
		in := setup(t, false)
		_, b, noteA, noteB, keyA := twoNotes(t, in)
		// Under SkipTenancy with the resolver answering b, only the row can
		// supply the stamp.
		if _, err := in.writer.Client.Notes().Update(WithTenant(context.Background(), b), noteA.ID,
			&models.UpdateNoteInput{Body: omittable.Set("changed")}, skipTenancy); err != nil {
			t.Fatalf("Update() error = %v", err)
		}
		assertInvalidations(t, "receiver", in.receiver.Backend, []BackendCall{{Op: "invalidate_many", Keys: []string{keyA}}})
		assertNoCacheErrors(t, in.receiver)
		handlerReturnedNil(t, in)
		assertHit(t, in.receiver, "note b", func() error {
			_, err := in.receiver.Client.Notes().Get(WithTenant(context.Background(), b), noteB.ID)
			return err
		})
	})

	t.Run("a tenant-in-PK event with no stamp evicts that PK's key only", func(t *testing.T) {
		in := setup(t, true)
		a, b := models.NewTenant(), models.NewTenant()
		pkA := models.MembershipPK{WorkspaceID: a, Member: "m"}
		pkB := models.MembershipPK{WorkspaceID: b, Member: "m"}
		var keyA string
		for _, pk := range []models.MembershipPK{pkA, pkB} {
			ctx := WithTenant(context.Background(), pk.WorkspaceID)
			if _, err := in.seed.Memberships().Create(ctx, &models.CreateMembershipInput{WorkspaceID: pk.WorkspaceID, Member: pk.Member, Role: "r"}); err != nil {
				t.Fatalf("Create(%v) error = %v", pk, err)
			}
			key := warm(t, in.receiver, func() error { _, err := in.receiver.Client.Memberships().Get(ctx, pk); return err })
			if pk == pkA {
				keyA = key
			}
		}
		h.checkKey(t, keyA, "memberships", a, fmt.Sprintf("%v:%v", pkA.WorkspaceID, pkA.Member))
		in.receiver.Backend.Clear()

		if _, err := in.writer.Client.Memberships().Update(WithTenant(context.Background(), a), pkA,
			&models.UpdateMembershipInput{Role: omittable.Set("admin")}); err != nil {
			t.Fatalf("Update() error = %v", err)
		}
		assertInvalidations(t, "receiver", in.receiver.Backend, []BackendCall{{Op: "invalidate_many", Keys: []string{keyA}}})
		assertNoCacheErrors(t, in.receiver)
		handlerReturnedNil(t, in)
		assertHit(t, in.receiver, "membership b", func() error {
			_, err := in.receiver.Client.Memberships().Get(WithTenant(context.Background(), b), pkB)
			return err
		})
	})

	t.Run("a tenant-less event clears the table across tenants and returns nil", func(t *testing.T) {
		in := setup(t, true)
		a, b, noteA, noteB, _ := twoNotes(t, in)
		if _, err := in.writer.Client.Notes().Update(WithTenant(context.Background(), a), noteA.ID,
			&models.UpdateNoteInput{Body: omittable.Set("changed")}); err != nil {
			t.Fatalf("Update() error = %v", err)
		}
		assertInvalidations(t, "receiver", in.receiver.Backend, []BackendCall{{Op: "invalidate_pattern", Keys: []string{"sqlgen:" + h.keyTable("notes") + ":*"}}})
		errs := in.receiver.CacheErrors.All()
		if len(errs) != 1 || errs[0].Op != "invalidate_tenant_fallback" || errs[0].Table != models.TableNotes {
			t.Errorf("OnErrorFunc reports = %+v, want one invalidate_tenant_fallback on %s", errs, models.TableNotes)
		}
		handlerReturnedNil(t, in)
		for _, get := range []struct {
			name   string
			tenant models.Tenant
			id     int64
		}{{"note a", a, noteA.ID}, {"note b", b, noteB.ID}} {
			in.receiver.Backend.Clear()
			if _, err := in.receiver.Client.Notes().Get(WithTenant(context.Background(), get.tenant), get.id); err != nil {
				t.Fatalf("Get(%s) error = %v", get.name, err)
			}
			if gets := in.receiver.Backend.Calls("get"); len(gets) == 0 || slices.ContainsFunc(gets, func(c BackendCall) bool { return c.Hit }) {
				t.Errorf("Get(%s) backend gets = %+v, want a miss: the table pattern clears every tenant", get.name, gets)
			}
		}
	})
}

// tenantStripper publishes through Inner with each event's tenant stamp
// removed, as a publisher that predates the stamp would.
type tenantStripper struct{ Inner event.Publisher }

func (s tenantStripper) Publish(ctx context.Context, e event.Event) error {
	return s.Inner.Publish(ctx, stripTenant(e))
}

func (s tenantStripper) PublishBatch(ctx context.Context, events []event.Event) error {
	out := make([]event.Event, len(events))
	for i, e := range events {
		out[i] = stripTenant(e)
	}
	return s.Inner.PublishBatch(ctx, out)
}

func (s tenantStripper) Close() error { return nil }

func stripTenant(e event.Event) event.Event {
	e.Metadata = maps.Clone(e.Metadata)
	delete(e.Metadata, "tenant")
	return e
}

// cachedRow is a row of a tenanted table and the tenant its key carries.
type cachedRow struct {
	tenant models.Tenant
	pk     any
}

// cacheRun is one pass of a cache check: a cached client, the context the
// write runs under, the tenant of each seeded row, and whether the write runs
// under SkipTenancy.
type cacheRun struct {
	t       *testing.T
	h       *Harness
	env     *Env
	ctx     context.Context
	tenants []models.Tenant
	skip    bool
	// bystander reads a seeded row no write touches.
	bystander func() error
	// calls is the number of writes the check's write makes, 1 unless it sets
	// it.
	calls int
}

func (r *cacheRun) workspace(i int) omittable.Value[models.Tenant] {
	if !r.skip {
		return omittable.Value[models.Tenant]{}
	}
	return omittable.Set(r.tenants[i])
}

func (r *cacheRun) noteOpts() []func(*models.CallOptions[models.NoteFieldOptions]) {
	return callOpts[models.NoteFieldOptions](r.skip)
}

func (r *cacheRun) taskOpts() []func(*models.CallOptions[models.TaskFieldOptions]) {
	return callOpts[models.TaskFieldOptions](r.skip)
}

// cachePasses runs check under the resolved tenant, then under SkipTenancy over
// rows of two other tenants while the resolver still answers the first.
func cachePasses(t *testing.T, h *Harness, check func(r *cacheRun)) {
	t.Helper()
	resolved := models.NewTenant()
	passes := []struct {
		tenants []models.Tenant
		skip    bool
	}{
		{[]models.Tenant{resolved, resolved}, false},
		{[]models.Tenant{models.NewTenant(), models.NewTenant()}, true},
	}
	for _, p := range passes {
		h.Reset(t)
		env := h.NewEnv(t, WithCache())
		check(&cacheRun{t: t, h: h, env: env, ctx: WithTenant(context.Background(), resolved), tenants: p.tenants, skip: p.skip, calls: 1})
	}
}

func passName(r *cacheRun) string {
	if r.skip {
		return "under SkipTenancy"
	}
	return "under the resolved tenant"
}

// checkWriteThrough checks that write's created rows are each written to the
// cache once, under the key §29.5 gives the row's tenant and key, and that a
// read of each row then hits.
func checkWriteThrough(t *testing.T, h *Harness, label string, write func(r *cacheRun) []cachedRow) {
	t.Helper()
	cachePasses(t, h, func(r *cacheRun) {
		label := label + " " + passName(r)
		rows := write(r)
		sets := r.env.Backend.Calls("set")
		if len(sets) != len(rows) {
			t.Fatalf("%s: backend sets = %+v, want one per created row (%d)", label, sets, len(rows))
		}
		var got, want []string
		for i, row := range rows {
			key := sets[i].Keys[0]
			got = append(got, key)
			want = append(want, h.tenantKey("notes", fingerprintOf(key), row.tenant, row.pk))
		}
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("%s: written keys = %q, want %q", label, got, want)
		}
		if inv := r.env.Backend.Invalidations(); len(inv) != 0 {
			t.Errorf("%s: backend invalidations = %+v, want none", label, inv)
		}
		for _, row := range rows {
			assertHit(t, r.env, fmt.Sprintf("%s: note %v", label, row.pk), func() error {
				_, err := r.env.Client.Notes().Get(WithTenant(context.Background(), row.tenant), row.pk.(int64))
				return err
			})
		}
	})
}

// checkEvictions checks that write evicts exactly the keys of the rows it
// returns, each built from that row's own tenant, through one backend
// InvalidateMany call per write and nothing written through; that no pattern
// is invalidated but the cached views table feeds; and that the bystander row
// stays cached.
func checkEvictions(t *testing.T, h *Harness, label, table string, write func(r *cacheRun) []cachedRow) {
	t.Helper()
	cachePasses(t, h, func(r *cacheRun) {
		label := label + " " + passName(r)
		rows := write(r)
		var fp string
		for _, c := range r.env.Backend.Calls("get") {
			fp = fingerprintOf(c.Keys[0])
		}
		var want []string
		for _, row := range rows {
			want = append(want, h.tenantKey(table, fp, row.tenant, row.pk))
		}
		var got, patterns []string
		many := 0
		for _, c := range r.env.Backend.Invalidations() {
			switch c.Op {
			case "invalidate_many":
				many++
				got = append(got, c.Keys...)
			case "invalidate_pattern":
				patterns = append(patterns, c.Keys...)
			default:
				t.Errorf("%s: backend %s(%q), want InvalidateMany only", label, c.Op, c.Keys)
			}
		}
		if sets := r.env.Backend.Calls("set"); len(sets) != 0 {
			t.Errorf("%s: wrote %d entries through, want 0: the write invalidates", label, len(sets))
		}
		if many != r.calls {
			t.Errorf("%s: %d backend InvalidateMany calls, want %d (one per write)", label, many, r.calls)
		}
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("%s: evicted keys = %q, want %q (one per affected row, from its own tenant)", label, got, want)
		}
		var wantPatterns []string
		if table == "tasks" {
			wantPatterns = []string{"sqlgen:" + h.keyTable("active_tasks") + ":*"}
		}
		if !slices.Equal(patterns, wantPatterns) {
			t.Errorf("%s: invalidated patterns = %q, want %q (the cached views of the table only)", label, patterns, wantPatterns)
		}
		assertNoCacheErrors(t, r.env)
		assertHit(t, r.env, label+": the bystander row", r.bystander)
	})
}

// seedNotes creates one note per tenant and a bystander note under the first,
// reads each so it is cached, checks each key, and clears the backend record.
func (r *cacheRun) seedNotes() []cachedRow {
	r.t.Helper()
	notes := r.env.Client.Notes()
	create := func(tenant models.Tenant, slug string) cachedRow {
		n, err := notes.Create(WithTenant(context.Background(), tenant), &models.CreateNoteInput{Slug: slug, Body: slug})
		if err != nil {
			r.t.Fatalf("seed note %s error = %v", slug, err)
		}
		r.warmRow("notes", cachedRow{tenant, n.ID}, func() error { _, err := notes.Get(WithTenant(context.Background(), tenant), n.ID); return err })
		return cachedRow{tenant, n.ID}
	}
	var rows []cachedRow
	for i, tenant := range r.tenants {
		rows = append(rows, create(tenant, fmt.Sprintf("seed-%d", i)))
	}
	by := create(r.tenants[0], "bystander")
	r.bystander = func() error {
		_, err := notes.Get(WithTenant(context.Background(), by.tenant), by.pk.(int64))
		return err
	}
	r.env.Backend.Clear()
	r.keepFingerprint()
	return rows
}

// seedTasks is seedNotes for tasks.
func (r *cacheRun) seedTasks() []cachedRow {
	r.t.Helper()
	rows := r.createTasks()
	r.env.Backend.Clear()
	r.keepFingerprint()
	return rows
}

// seedTrashedTasks seeds as seedTasks does, then soft-deletes the rows the
// write will see. The bystander stays live.
func (r *cacheRun) seedTrashedTasks() []cachedRow {
	r.t.Helper()
	rows := r.createTasks()
	ctx := WithTenant(context.Background(), r.tenants[0])
	if _, err := r.env.Client.Tasks().SoftDeleteMany(ctx, rowIDs(rows), skipTenancy); err != nil {
		r.t.Fatalf("seed SoftDeleteMany() error = %v", err)
	}
	r.env.Backend.Clear()
	r.keepFingerprint()
	return rows
}

func (r *cacheRun) createTasks() []cachedRow {
	r.t.Helper()
	tasks := r.env.Client.Tasks()
	create := func(tenant models.Tenant, title string) cachedRow {
		task, err := tasks.Create(WithTenant(context.Background(), tenant), &models.CreateTaskInput{Title: title})
		if err != nil {
			r.t.Fatalf("seed task %s error = %v", title, err)
		}
		r.warmRow("tasks", cachedRow{tenant, task.ID}, func() error { _, err := tasks.Get(WithTenant(context.Background(), tenant), task.ID); return err })
		return cachedRow{tenant, task.ID}
	}
	var rows []cachedRow
	for i, tenant := range r.tenants {
		rows = append(rows, create(tenant, fmt.Sprintf("seed-%d", i)))
	}
	by := create(r.tenants[0], "bystander")
	r.bystander = func() error {
		_, err := tasks.Get(WithTenant(context.Background(), by.tenant), by.pk.(int64))
		return err
	}
	return rows
}

// warmRow reads row through get, which must hit the entry its Create wrote
// through, and checks that entry's key.
func (r *cacheRun) warmRow(table string, row cachedRow, get func() error) {
	r.t.Helper()
	r.env.Backend.Clear()
	key := warm(r.t, r.env, get)
	r.h.checkKey(r.t, key, table, row.tenant, row.pk)
}

// keepFingerprint reads the bystander once, leaving one get of its key on
// the backend record for checkEvictions to read the table's fingerprint from.
func (r *cacheRun) keepFingerprint() {
	r.t.Helper()
	if err := r.bystander(); err != nil {
		r.t.Fatalf("Get(bystander) error = %v", err)
	}
}

// warm runs get against env's cache and returns the key it read: the entry a
// write-through left, or the one the read-through miss wrote. A miss reads
// the key twice, once more inside its singleflight.
func warm(t *testing.T, env *Env, get func() error) string {
	t.Helper()
	before := len(env.Backend.Calls("get"))
	if err := get(); err != nil {
		t.Fatalf("warming Get() error = %v", err)
	}
	gets := env.Backend.Calls("get")[before:]
	if len(gets) == 0 {
		t.Fatal("warming Get() made no backend get")
	}
	for _, g := range gets[1:] {
		if g.Keys[0] != gets[0].Keys[0] {
			t.Fatalf("warming Get() read keys %q and %q, want one key", gets[0].Keys[0], g.Keys[0])
		}
	}
	return gets[0].Keys[0]
}

// assertHit checks that get is served from env's cache.
func assertHit(t *testing.T, env *Env, what string, get func() error) {
	t.Helper()
	before := len(env.Backend.Calls(""))
	if err := get(); err != nil {
		t.Fatalf("Get(%s) error = %v", what, err)
	}
	calls := env.Backend.Calls("")[before:]
	if len(calls) != 1 || calls[0].Op != "get" || !calls[0].Hit {
		t.Errorf("Get(%s) backend calls = %+v, want one hit: the entry should stay cached", what, calls)
	}
}

func assertInvalidations(t *testing.T, what string, b *RecordingBackend, want []BackendCall) {
	t.Helper()
	got := b.Invalidations()
	if len(got) != len(want) {
		t.Errorf("%s backend invalidations = %+v, want %+v", what, got, want)
		return
	}
	for i := range got {
		if got[i].Op != want[i].Op || !slices.Equal(got[i].Keys, want[i].Keys) {
			t.Errorf("%s backend invalidations = %+v, want %+v", what, got, want)
			return
		}
	}
}

func assertNoCacheErrors(t *testing.T, env *Env) {
	t.Helper()
	if errs := env.CacheErrors.All(); len(errs) != 0 {
		t.Errorf("OnErrorFunc reports = %+v, want none (no fallback, no backend error)", errs)
	}
}

// keyTable is a cache key's table segment: {schema}.{table}, or the bare name
// when keys carry no schema (PRD §27.5).
func (h *Harness) keyTable(table string) string {
	if h.Caps.KeySchema == "" {
		return table
	}
	return h.Caps.KeySchema + "." + table
}

// tenantKey is the key §29.5 gives a tenanted row: pk is the %v form of its
// key, components joined by ':' for a composite one.
func (h *Harness) tenantKey(table, fingerprint string, tenant, pk any) string {
	return fmt.Sprintf("sqlgen:%s:tenant:%v:fingerprint:v%s:pk:%v", h.keyTable(table), tenant, fingerprint, pk)
}

// checkKey checks key against the §29.5 grammar for table, tenant and pk. The
// fingerprint is generated, so only its presence is checked.
func (h *Harness) checkKey(t *testing.T, key, table string, tenant, pk any) {
	t.Helper()
	fp := fingerprintOf(key)
	if fp == "" {
		t.Errorf("cache key %q has no fingerprint segment", key)
	}
	if want := h.tenantKey(table, fp, tenant, pk); key != want {
		t.Errorf("cache key = %q, want %q", key, want)
	}
}

// fingerprintOf returns key's fingerprint value, or "" when it has none.
func fingerprintOf(key string) string {
	_, rest, ok := strings.Cut(key, ":fingerprint:v")
	if !ok {
		return ""
	}
	fp, _, _ := strings.Cut(rest, ":pk:")
	return fp
}

func rowIDs(rows []cachedRow) []int64 {
	ids := make([]int64, len(rows))
	for i, row := range rows {
		ids[i] = row.pk.(int64)
	}
	return ids
}
