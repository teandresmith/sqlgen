package suite

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	gocmp "github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/database"
	"github.com/teandresmith/sqlgen/hook"
	"github.com/teandresmith/sqlgen/omittable"
	"github.com/teandresmith/sqlgen/sql"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/models"
)

// get: Get returns the row with the given key.
func get(t *testing.T, h *Harness) {
	ctx := context.Background()

	t.Run("returns the row for its key with every column", func(t *testing.T) {
		h.Reset(t)
		start := time.Now()
		createPricedProduct(t, h, "a", 100)
		b := createPricedProduct(t, h, "b", 150)
		createPricedProduct(t, h, "c", 250)
		end := time.Now()
		got, err := h.Client.Products().Get(ctx, b.ID)
		if err != nil {
			t.Fatalf("Get(b) error = %v, want nil", err)
		}
		getAssertCreatedAt(t, got, start, end)
		if diff := gocmp.Diff(&models.Product{ID: b.ID, Name: "b", Price: 150}, getScalars(got)); diff != "" {
			t.Errorf("Get(b) without CreatedAt (-want +got):\n%s", diff)
		}
	})

	t.Run("a missing key returns ErrNotFound", func(t *testing.T) {
		h.Reset(t)
		// Rows sit on both sides of the missing key.
		missing := getMissingProductKey(t, h)
		got, err := h.Client.Products().Get(ctx, missing)
		if !errors.Is(err, models.ErrNotFound) {
			t.Fatalf("Get(missing) = %+v, %v, want ErrNotFound", got, err)
		}
	})

	t.Run("a soft-deleted row returns ErrNotFound", func(t *testing.T) {
		h.Reset(t)
		deleted, live := createDocument(t, h, "deleted"), createDocument(t, h, "live")
		if _, err := h.Client.Documents().SoftDelete(ctx, deleted.ID); err != nil {
			t.Fatalf("SoftDelete(deleted) error = %v", err)
		}
		if got, err := h.Client.Documents().Get(ctx, deleted.ID); !errors.Is(err, models.ErrNotFound) {
			t.Errorf("Get(soft-deleted) = %+v, %v, want ErrNotFound", got, err)
		}
		got, err := h.Client.Documents().Get(ctx, live.ID)
		if err != nil {
			t.Fatalf("Get(live) error = %v, want nil", err)
		}
		if got.ID != live.ID || got.Title != "live" {
			t.Errorf("Get(live) = %+v, want document %d titled %q", got, live.ID, "live")
		}
	})

	t.Run("reads a row written earlier in the same transaction", func(t *testing.T) {
		h.Reset(t)
		errRollback := errors.New("roll back")
		var id int64
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			p, err := h.Client.Products().Create(txCtx, &models.CreateProductInput{Name: "in-tx", Price: 175})
			if err != nil {
				return err
			}
			id = p.ID
			got, err := h.Client.Products().Get(txCtx, id)
			if err != nil {
				t.Errorf("Get inside the transaction error = %v, want the row", err)
				return errRollback
			}
			if got.ID != id || got.Name != "in-tx" || got.Price != 175 {
				t.Errorf("Get inside the transaction = %+v, want product %d in-tx/175", got, id)
			}
			return errRollback
		})
		if !errors.Is(err, errRollback) {
			t.Fatalf("WithTransaction error = %v, want the rollback sentinel", err)
		}
		if got, err := h.Client.Products().Get(ctx, id); !errors.Is(err, models.ErrNotFound) {
			t.Errorf("Get after the rollback = %+v, %v, want ErrNotFound", got, err)
		}
	})

	t.Run("fires one get query hook with the key", func(t *testing.T) {
		h.Reset(t)
		a := createProduct(t, h, "a")
		client, seen := queryRecorder(h)
		if _, err := client.Products().Get(ctx, a.ID); err != nil {
			t.Fatalf("Get(a) error = %v", err)
		}
		qs := seen()
		if len(qs) != 1 {
			t.Fatalf("query hook fired %d times (%v), want 1", len(qs), getOps(qs))
		}
		q := qs[0]
		if q.Op != hook.OpGet || q.Table != models.TableProducts || q.PK != any(a.ID) {
			t.Errorf("QueryContext = {Op: %q, Table: %q, PK: %v}, want {Op: %q, Table: %q, PK: %d}",
				q.Op, q.Table, q.PK, hook.OpGet, models.TableProducts, a.ID)
		}
	})

	t.Run("returns only the selected columns", func(t *testing.T) {
		h.Reset(t)
		a := createPricedProduct(t, h, "a", 150)
		got, err := h.Client.Products().Get(ctx, a.ID, func(o *models.CallOptions[models.ProductFieldOptions]) {
			o.FieldOptions = &models.ProductFieldOptions{Name: true, Price: true}
		})
		if err != nil {
			t.Fatalf("Get(a, {Name, Price}) error = %v", err)
		}
		if diff := gocmp.Diff(&models.Product{Name: "a", Price: 150}, got); diff != "" {
			t.Errorf("Get(a, {Name, Price}) (-want +got):\n%s", diff)
		}
	})

	t.Run("empty field options returns ErrNotFound", func(t *testing.T) {
		h.Reset(t)
		a := createProduct(t, h, "a")
		got, err := h.Client.Products().Get(ctx, a.ID, func(o *models.CallOptions[models.ProductFieldOptions]) {
			o.FieldOptions = &models.ProductFieldOptions{}
		})
		if !errors.Is(err, models.ErrNotFound) {
			t.Errorf("Get(a, {}) = %+v, %v, want ErrNotFound", got, err)
		}
	})

	t.Run("a lock mode outside a transaction returns an error before any SQL", func(t *testing.T) {
		h.Require(t, CapRowLocks)
		h.Reset(t)
		a := createProduct(t, h, "a")
		for _, mode := range lockModes {
			h.Querier.ResetCount()
			got, err := h.Client.Products().Get(ctx, a.ID, withLock[models.ProductFieldOptions](mode))
			if err == nil {
				t.Errorf("Get(a, %s) outside a transaction = %+v, nil, want an error", mode, got)
			}
			if stmts := h.Querier.Statements(); len(stmts) != 0 {
				t.Errorf("Get(a, %s) outside a transaction sent %q, want no statement", mode, stmts)
			}
		}
	})

	t.Run("an unsupported lock mode outside a transaction returns an error before any SQL", func(t *testing.T) {
		h.Lacks(t, CapRowLocks)
		h.Reset(t)
		a := createProduct(t, h, "a")
		for _, mode := range lockModes {
			h.Querier.ResetCount()
			got, err := h.Client.Products().Get(ctx, a.ID, withLock[models.ProductFieldOptions](mode))
			if err == nil {
				t.Errorf("Get(a, %s) = %+v, nil, want an error", mode, got)
			}
			if stmts := h.Querier.Statements(); len(stmts) != 0 {
				t.Errorf("Get(a, %s) sent %q, want no statement", mode, stmts)
			}
		}
	})

	t.Run("a lock mode inside a transaction locks the row", func(t *testing.T) {
		h.Require(t, CapRowLocks)
		h.Reset(t)
		a, b := createProduct(t, h, "a"), createProduct(t, h, "b")
		for _, mode := range lockModes {
			txCtx := beginBoundedTx(t, h)
			got, err := h.Client.Products().Get(txCtx, a.ID, withLock[models.ProductFieldOptions](mode))
			if err != nil || got == nil || got.ID != a.ID {
				t.Errorf("Get(a, %s) in a transaction = %+v, %v, want product a", mode, got, err)
			}
			if err := getProbeLock(t, h, a.ID); err == nil {
				t.Errorf("after Get(a, %s) another transaction's NOWAIT read of a succeeded, want it to fail on the lock", mode)
			}
			if err := getProbeLock(t, h, b.ID); err != nil {
				t.Errorf("after Get(a, %s) another transaction's NOWAIT read of b = %v, want b unlocked", mode, err)
			}
			if err := database.Rollback(txCtx); err != nil {
				t.Fatalf("roll back: %v", err)
			}
		}
	})

	t.Run("a locked read through a one-to-one join locks only the parent", func(t *testing.T) {
		h.Require(t, CapRowLocks)
		h.Reset(t)
		a := createAuthor(t, h, "a")
		prof := createProfile(t, h, "bio", new(a.ID))
		withProfile := func(mode sql.LockMode) func(*models.CallOptions[models.AuthorFieldOptions]) {
			return func(o *models.CallOptions[models.AuthorFieldOptions]) {
				o.LockMode = mode
				o.FieldOptions = &models.AuthorFieldOptions{ID: true, Name: true, Profile: &models.AuthorProfileFieldOptions{ID: true, AuthorID: true, Bio: true}}
			}
		}
		check := func(t *testing.T, txCtx context.Context, mode sql.LockMode, label string) {
			t.Helper()
			got, err := h.Client.Authors().Get(txCtx, a.ID, withProfile(mode))
			if err != nil || got == nil {
				t.Errorf("Authors().Get(a, %s, {Profile}) %s = %+v, %v, want a with its profile", mode, label, got, err)
				return
			}
			if diff := gocmp.Diff(prof, got.Profile); diff != "" {
				t.Errorf("Authors().Get(a, %s) %s Profile (-want +got):\n%s", mode, label, diff)
			}
			if err := authorProbeLock(t, h, a.ID); err == nil {
				t.Errorf("after Authors().Get(a, %s) %s another transaction's NOWAIT read of a succeeded, want a locked", mode, label)
			}
		}
		for _, mode := range lockModes {
			txCtx := beginBoundedTx(t, h)
			check(t, txCtx, mode, "")
			if err := profileProbeLock(t, h, prof.ID); err != nil {
				t.Errorf("after Authors().Get(a, %s, {Profile}) another transaction's NOWAIT read of the profile = %v, want it unlocked", mode, err)
			}
			if err := database.Rollback(txCtx); err != nil {
				t.Fatalf("roll back: %v", err)
			}
		}
		// NOWAIT would fail on a locked joined row; SKIP LOCKED would drop it.
		contender := otherTx(t, h)
		if _, err := h.Client.AuthorProfiles().Get(contender, prof.ID, withLock[models.AuthorProfileFieldOptions](sql.LockForUpdate)); err != nil {
			t.Fatalf("the contender's AuthorProfiles().Get(LockForUpdate) error = %v", err)
		}
		for _, mode := range []sql.LockMode{sql.LockForUpdateNoWait, sql.LockForUpdateSkipLocked} {
			txCtx := beginBoundedTx(t, h)
			check(t, txCtx, mode, "with the profile locked elsewhere")
			if err := database.Rollback(txCtx); err != nil {
				t.Fatalf("roll back: %v", err)
			}
		}
	})

	t.Run("a locked row makes another transaction's locking read wait until commit", func(t *testing.T) {
		h.Require(t, CapRowLocks)
		h.Reset(t)
		a := createPricedProduct(t, h, "a", 100)
		txCtx := beginBoundedTx(t, h)
		if _, err := h.Client.Products().Get(txCtx, a.ID, withLock[models.ProductFieldOptions](sql.LockForUpdate)); err != nil {
			t.Fatalf("Get(a, LockForUpdate) error = %v", err)
		}
		type result struct {
			p   *models.Product
			err error
		}
		done := make(chan result, 1)
		contender := beginBoundedTx(t, h) // bounded: a contender that never gets the lock gives up
		go func() {
			p, err := h.Client.Products().Get(contender, a.ID, withLock[models.ProductFieldOptions](sql.LockForUpdate))
			done <- result{p, err}
		}()
		select {
		case r := <-done:
			t.Fatalf("the contender's Get(a, LockForUpdate) returned %+v, %v while a was locked, want it to wait", r.p, r.err)
		case <-time.After(300 * time.Millisecond):
		}
		if _, err := h.Client.Products().Update(txCtx, a.ID, &models.UpdateProductInput{Price: omittable.Set(int64(300))}); err != nil {
			t.Fatalf("Update(a) error = %v", err)
		}
		if err := database.Commit(txCtx); err != nil {
			t.Fatalf("commit: %v", err)
		}
		select {
		case r := <-done:
			if r.err != nil || r.p == nil || r.p.Price != 300 {
				t.Errorf("the contender's Get(a) after commit = %+v, %v, want the committed price 300", r.p, r.err)
			}
		case <-time.After(lockWait):
			t.Fatal("the contender's Get(a) still waits after the commit")
		}
	})

	t.Run("a rolled-back savepoint keeps the lock", func(t *testing.T) {
		h.Require(t, CapRowLocks)
		h.Reset(t)
		a, b := createProduct(t, h, "a"), createProduct(t, h, "b")
		c := createPricedProduct(t, h, "c", 100)
		txCtx := beginBoundedTx(t, h)
		if _, err := h.Client.Products().Get(txCtx, a.ID, withLock[models.ProductFieldOptions](sql.LockForUpdate)); err != nil {
			t.Fatalf("Get(a, LockForUpdate) error = %v", err)
		}
		errInner := errors.New("roll back the savepoint")
		err := database.WithTransaction(txCtx, h.Querier, "sp_inner", func(spCtx context.Context) error {
			if _, err := h.Client.Products().Update(spCtx, c.ID, &models.UpdateProductInput{Price: omittable.Set(int64(999))}); err != nil {
				t.Errorf("Update(c) inside the savepoint error = %v", err)
			}
			return errInner
		})
		if !errors.Is(err, errInner) {
			t.Fatalf("savepoint WithTransaction error = %v, want the sentinel", err)
		}
		// The savepoint really rolled back: its write is gone from the outer transaction.
		got, err := h.Client.Products().Get(txCtx, c.ID)
		if err != nil || got.Price != 100 {
			t.Errorf("Get(c) in the outer transaction after the savepoint rolled back = %+v, %v, want price 100", got, err)
		}
		if err := getProbeLock(t, h, a.ID); err == nil {
			t.Errorf("after the savepoint rolled back another transaction's NOWAIT read of a succeeded, want a still locked")
		}
		if err := getProbeLock(t, h, b.ID); err != nil {
			t.Errorf("another transaction's NOWAIT read of b = %v, want b unlocked", err)
		}
	})

	t.Run("an unsupported lock mode inside a transaction returns an error before any query hook runs", func(t *testing.T) {
		h.Lacks(t, CapRowLocks)
		h.Reset(t)
		a := createProduct(t, h, "a")
		client, seen := queryRecorder(h)
		for _, mode := range lockModes {
			var got *models.Product
			err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
				var err error
				got, err = client.Products().Get(txCtx, a.ID, withLock[models.ProductFieldOptions](mode))
				return err
			})
			if err == nil {
				t.Errorf("Get(a, %s) in a transaction = %+v, nil, want an error", mode, got)
			}
		}
		if qs := seen(); len(qs) != 0 {
			t.Errorf("Get with a lock mode ran the query hook chain %d times (%v), want the guard to return first", len(qs), getOps(qs))
		}
	})

	t.Run("a composite key returns only its own tuple", func(t *testing.T) {
		h.Reset(t)
		p1, p2 := createProduct(t, h, "p1").ID, createProduct(t, h, "p2").ID
		getCreateOrderItem(t, h, 1, p1, 11)
		getCreateOrderItem(t, h, 2, p2, 22)
		crossing := models.OrderItemPK{OrderID: 1, ProductID: p2}
		if got, err := h.Client.OrderItems().Get(ctx, crossing); !errors.Is(err, models.ErrNotFound) {
			t.Errorf("Get(%+v) = %+v, %v, want ErrNotFound", crossing, got, err)
		}
		got, err := h.Client.OrderItems().Get(ctx, models.OrderItemPK{OrderID: 1, ProductID: p1})
		if err != nil {
			t.Fatalf("Get(1, p1) error = %v", err)
		}
		if diff := gocmp.Diff(&models.OrderItem{OrderID: 1, ProductID: p1, Quantity: 11}, got); diff != "" {
			t.Errorf("Get(1, p1) (-want +got):\n%s", diff)
		}
	})

	t.Run("a repeated read queries the database again and sees a write made outside the client", func(t *testing.T) {
		h.Reset(t)
		p := createProduct(t, h, "before")
		if _, err := h.Client.Products().Get(ctx, p.ID); err != nil {
			t.Fatalf("first Get error = %v", err)
		}
		renameProductsOutOfBand(t, h)
		h.Querier.ResetCount()
		got, err := h.Client.Products().Get(ctx, p.ID)
		if err != nil {
			t.Fatalf("second Get error = %v", err)
		}
		if got.Name != "after" {
			t.Errorf("second Get name = %q, want %q", got.Name, "after")
		}
		if n := len(statementsWithPrefix(h, "SELECT")); n != 1 {
			t.Errorf("second Get sent %d SELECT statements, want 1", n)
		}
	})

	t.Run("overlapping identical reads each query the database", func(t *testing.T) {
		h.Reset(t)
		p := createProduct(t, h, "p")
		const n = 4
		barrier, overlapped := queryBarrier(n, readOf(hook.OpGet, models.TableProducts))
		c := models.New(h.Base, models.WithQueryHook(barrier))
		h.Querier.ResetCount()
		errs := make([]error, n)
		var wg sync.WaitGroup
		for i := range n {
			wg.Go(func() { _, errs[i] = c.Products().Get(ctx, p.ID) })
		}
		wg.Wait()
		if !overlapped() {
			t.Fatalf("the %d reads never overlapped", n)
		}
		if err := errors.Join(errs...); err != nil {
			t.Fatalf("overlapping Gets: %v", err)
		}
		if got := len(statementsWithPrefix(h, "SELECT")); got != n {
			t.Errorf("%d overlapping Gets sent %d SELECT statements, want %d", n, got, n)
		}
	})

	t.Run("selecting two fanned-out relationships inside a transaction loads both", func(t *testing.T) {
		h.Reset(t)
		a := seedAuthorEdges(t, h)
		// The barrier holds the posts and tags loads until both are in flight, so
		// their statements contend for the one connection. On SQLite the driver
		// serializes them as well, so there the subtest passes without the
		// transaction's reservation.
		barrier, overlapped := queryBarrier(2, func(q *hook.QueryContext) bool {
			return q.Op == hook.OpGetMany && (q.Table == models.TablePosts || q.Table == models.TableTags)
		})
		c := models.New(h.Base, models.WithQueryHook(barrier))
		txCtx := beginBoundedTx(t, h)
		got, err := c.Authors().Get(txCtx, a.ID, withPostsAndTags)
		if !overlapped() {
			t.Fatal("the posts and tags loads never overlapped")
		}
		if err != nil {
			t.Fatalf("Get inside a transaction error = %v", err)
		}
		if len(got.Posts) != 1 || len(got.Tags) != 1 {
			t.Errorf("Get inside a transaction loaded %d posts and %d tags, want 1 and 1", len(got.Posts), len(got.Tags))
		}
	})

	getCompositeKey(t, h)
}

// getMany: GetMany returns the rows a filter matches, sorted and paged.
func getMany(t *testing.T, h *Harness) {
	ctx := context.Background()

	t.Run("a loaded key that differs from its parent's only in case is an error, and from its target's loads", func(t *testing.T) {
		h.Require(t, CapCaseInsensitiveText)
		h.Reset(t)
		const want = "match no parent key byte for byte"
		// loadsOne asserts that the M2M load of one van returns its one
		// target in the parent read, junction read and target read.
		loadsOne := func(t *testing.T, edge string, fo *models.VanFieldOptions, id int64, got func(*models.Van) int) {
			t.Helper()
			h.Querier.ResetCount()
			vans, err := h.Client.Vans().GetMany(ctx, &models.GetVansInput{
				Filter: &models.VanFilter{ID: &comparator.Number[int64]{Eq: &id}},
			}, func(o *models.CallOptions[models.VanFieldOptions]) { o.FieldOptions = fo })
			if err != nil || len(vans) != 1 || got(vans[0]) != 1 {
				n := -1
				if len(vans) == 1 {
					n = got(vans[0])
				}
				t.Errorf("GetMany vans with %s = %d vans, %d %s, %v; want 1 van with 1", edge, len(vans), n, edge, err)
			}
			if stmts := h.Querier.Statements(); len(stmts) != 3 {
				t.Errorf("GetMany vans with %s sent %d statements %q, want 3", edge, len(stmts), stmts)
			}
		}
		depot := createDepot(t, h, "EU", "north")
		van, err := h.Client.Vans().Create(ctx, &models.CreateVanInput{Region: "eu", Plate: "held", DepotID: omittable.Set(&depot.ID)})
		if err != nil {
			t.Fatalf("Create van the foreign key matches case-insensitively: %v", err)
		}
		_, err = h.Client.Depots().GetMany(ctx, nil, func(o *models.CallOptions[models.DepotFieldOptions]) {
			o.FieldOptions = &models.DepotFieldOptions{ID: true, Vans: &models.VanRelationshipOptions{}}
		})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("GetMany depots with Vans error = %v, want one containing %q", err, want)
		}

		permit, err := h.Client.Permits().Create(ctx, &models.CreatePermitInput{Code: "ABC"})
		if err != nil {
			t.Fatalf("Create permit: %v", err)
		}
		if _, err := h.Client.VanPermits().Create(ctx, &models.CreateVanPermitInput{VanID: van.ID, PermitCode: "abc"}); err != nil {
			t.Fatalf("Create van permit the foreign key matches case-insensitively: %v", err)
		}
		_, err = h.Client.Permits().GetMany(ctx, &models.GetPermitsInput{
			Filter: &models.PermitFilter{ID: &comparator.Number[int64]{Eq: &permit.ID}},
		}, func(o *models.CallOptions[models.PermitFieldOptions]) {
			o.FieldOptions = &models.PermitFieldOptions{ID: true, Vans: &models.VanRelationshipOptions{}}
		})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("GetMany permits with Vans error = %v, want one containing %q", err, want)
		}
		// The junction read joins the permit, so the target key is the
		// permit's own spelling.
		loadsOne(t, "Permits", &models.VanFieldOptions{ID: true, Permits: &models.PermitRelationshipOptions{}}, van.ID,
			func(v *models.Van) int { return len(v.Permits) })

		tool := createTool(t, h, "eu", "tool")
		linked := createVan(t, h, "EU", "linked")
		if _, err := h.Client.VanTools().Create(ctx, &models.CreateVanToolInput{Region: "EU", VanID: linked.ID, ToolID: tool.ID}); err != nil {
			t.Fatalf("Create van tool the foreign keys match case-insensitively: %v", err)
		}
		_, err = h.Client.Tools().GetMany(ctx, nil, func(o *models.CallOptions[models.ToolFieldOptions]) {
			o.FieldOptions = &models.ToolFieldOptions{ID: true, Vans: &models.VanRelationshipOptions{}}
		})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("GetMany tools with Vans error = %v, want one containing %q", err, want)
		}
		// The junction shares region between its two keys: EU matches the
		// van, and the join reads the tool's eu.
		loadsOne(t, "Tools", &models.VanFieldOptions{ID: true, Tools: &models.ToolRelationshipOptions{}}, linked.ID,
			func(v *models.Van) int { return len(v.Tools) })
	})

	t.Run("returns exactly the rows the filter matches", func(t *testing.T) {
		h.Reset(t)
		createPricedProduct(t, h, "a", 100)
		b := createPricedProduct(t, h, "b", 200)
		c := createPricedProduct(t, h, "c", 300)
		got, err := h.Client.Products().GetMany(ctx, &models.GetProductsInput{
			Filter: &models.ProductFilter{Price: &comparator.Number[int64]{Gte: new(int64(200))}},
		})
		if err != nil {
			t.Fatalf("GetMany(price >= 200) error = %v", err)
		}
		if diff := gocmp.Diff([]int64{b.ID, c.ID}, getSortedIDs(got)); diff != "" {
			t.Errorf("GetMany(price >= 200) keys (-want +got):\n%s", diff)
		}
	})

	t.Run("limit and offset page through the sorted rows", func(t *testing.T) {
		h.Reset(t)
		// Inserted out of price order, so the page depends on the sort.
		createPricedProduct(t, h, "p400", 400)
		createPricedProduct(t, h, "p100", 100)
		createPricedProduct(t, h, "p300", 300)
		createPricedProduct(t, h, "p200", 200)
		got, err := h.Client.Products().GetMany(ctx, &models.GetProductsInput{
			Sorts:  []sql.Sort{{Column: "price", Direction: sql.Asc}},
			Limit:  new(2),
			Offset: new(1),
		})
		if err != nil {
			t.Fatalf("GetMany(limit 2, offset 1) error = %v", err)
		}
		if diff := gocmp.Diff([]string{"p200", "p300"}, getNames(got)); diff != "" {
			t.Errorf("GetMany(price asc, limit 2, offset 1) (-want +got):\n%s", diff)
		}
	})

	t.Run("multiple sorts apply in order, primary then secondary", func(t *testing.T) {
		h.Reset(t)
		// The secondary sort orders the three price-100 rows; inserted in neither order.
		createPricedProduct(t, h, "b", 100)
		createPricedProduct(t, h, "d", 200)
		createPricedProduct(t, h, "c", 100)
		createPricedProduct(t, h, "a", 100)
		got, err := h.Client.Products().GetMany(ctx, &models.GetProductsInput{
			Sorts: []sql.Sort{{Column: "price", Direction: sql.Asc}, {Column: "name", Direction: sql.Desc}},
		})
		if err != nil {
			t.Fatalf("GetMany(price asc, name desc) error = %v", err)
		}
		if diff := gocmp.Diff([]string{"c", "b", "a", "d"}, getNames(got)); diff != "" {
			t.Errorf("GetMany(price asc, name desc) (-want +got):\n%s", diff)
		}
	})

	t.Run("nil input reads the same rows as the zero-value input", func(t *testing.T) {
		h.Reset(t)
		ids := []int64{createProduct(t, h, "a").ID, createProduct(t, h, "b").ID, createProduct(t, h, "c").ID}
		client, seen := queryRecorder(h)
		fromNil, err := client.Products().GetMany(ctx, nil)
		if err != nil {
			t.Fatalf("GetMany(nil) error = %v", err)
		}
		fromZero, err := h.Client.Products().GetMany(ctx, &models.GetProductsInput{})
		if err != nil {
			t.Fatalf("GetMany(&GetProductsInput{}) error = %v", err)
		}
		if diff := gocmp.Diff(ids, getSortedIDs(fromNil)); diff != "" {
			t.Errorf("GetMany(nil) keys (-want +got):\n%s", diff)
		}
		if diff := gocmp.Diff(getSortedIDs(fromZero), getSortedIDs(fromNil)); diff != "" {
			t.Errorf("GetMany(nil) against GetMany(&GetProductsInput{}) keys (-zero +nil):\n%s", diff)
		}
		qs := seen()
		if len(qs) != 1 {
			t.Fatalf("query hook fired %d times (%v), want 1", len(qs), getOps(qs))
		}
		in, ok := qs[0].Input.(*models.GetProductsInput)
		if !ok || in == nil {
			t.Fatalf("GetMany(nil) hook Input = %#v, want a non-nil *GetProductsInput", qs[0].Input)
		}
		if in.Filter != nil || in.Sorts != nil || in.Limit != nil || in.Offset != nil {
			t.Errorf("GetMany(nil) hook Input = %+v, want the zero value", in)
		}
	})

	t.Run("a filter matching nothing returns no entities and no error", func(t *testing.T) {
		h.Reset(t)
		createPricedProduct(t, h, "a", 100)
		got, err := h.Client.Products().GetMany(ctx, &models.GetProductsInput{
			Filter: &models.ProductFilter{Price: &comparator.Number[int64]{Gt: new(int64(100))}},
		})
		if err != nil || len(got) != 0 {
			t.Errorf("GetMany(price > 100) = %d entities, %v, want none and no error", len(got), err)
		}
	})

	t.Run("soft-deleted rows are left out unless the filter sets the deleted-at comparator", func(t *testing.T) {
		h.Reset(t)
		deleted, live := createDocument(t, h, "deleted"), createDocument(t, h, "live")
		if _, err := h.Client.Documents().SoftDelete(ctx, deleted.ID); err != nil {
			t.Fatalf("SoftDelete(deleted) error = %v", err)
		}
		byDefault, err := h.Client.Documents().GetMany(ctx, nil)
		if err != nil {
			t.Fatalf("GetMany(nil) error = %v", err)
		}
		if diff := gocmp.Diff([]int64{live.ID}, getDocumentIDs(byDefault)); diff != "" {
			t.Errorf("GetMany(nil) keys (-want +got):\n%s", diff)
		}
		overridden, err := h.Client.Documents().GetMany(ctx, &models.GetDocumentsInput{
			Filter: &models.DocumentFilter{
				ID:        &comparator.Number[int64]{Eq: new(deleted.ID)},
				DeletedAt: &comparator.NullableTime{Null: new(false)},
			},
		})
		if err != nil {
			t.Fatalf("GetMany(deleted, deleted_at not null) error = %v", err)
		}
		if diff := gocmp.Diff([]int64{deleted.ID}, getDocumentIDs(overridden)); diff != "" {
			t.Errorf("GetMany(deleted, deleted_at not null) keys (-want +got):\n%s", diff)
		}
	})

	t.Run("reads a row written earlier in the same transaction", func(t *testing.T) {
		h.Reset(t)
		before := createPricedProduct(t, h, "before", 100)
		errRollback := errors.New("roll back")
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			p, err := h.Client.Products().Create(txCtx, &models.CreateProductInput{Name: "in-tx", Price: 200})
			if err != nil {
				return err
			}
			got, err := h.Client.Products().GetMany(txCtx, nil)
			if err != nil {
				t.Errorf("GetMany inside the transaction error = %v", err)
				return errRollback
			}
			if diff := gocmp.Diff([]int64{before.ID, p.ID}, getSortedIDs(got)); diff != "" {
				t.Errorf("GetMany inside the transaction keys (-want +got):\n%s", diff)
			}
			return errRollback
		})
		if !errors.Is(err, errRollback) {
			t.Fatalf("WithTransaction error = %v, want the rollback sentinel", err)
		}
		got, err := h.Client.Products().GetMany(ctx, nil)
		if err != nil {
			t.Fatalf("GetMany after the rollback error = %v", err)
		}
		if diff := gocmp.Diff([]int64{before.ID}, getSortedIDs(got)); diff != "" {
			t.Errorf("GetMany after the rollback keys (-want +got):\n%s", diff)
		}
	})

	t.Run("fires one get_many query hook with the caller's input", func(t *testing.T) {
		h.Reset(t)
		createProduct(t, h, "a")
		client, seen := queryRecorder(h)
		in := &models.GetProductsInput{Filter: &models.ProductFilter{Name: &comparator.String{Eq: new("a")}}}
		if _, err := client.Products().GetMany(ctx, in); err != nil {
			t.Fatalf("GetMany error = %v", err)
		}
		qs := seen()
		if len(qs) != 1 {
			t.Fatalf("query hook fired %d times (%v), want 1", len(qs), getOps(qs))
		}
		q := qs[0]
		if q.Op != hook.OpGetMany || q.Table != models.TableProducts {
			t.Errorf("QueryContext = {Op: %q, Table: %q}, want {Op: %q, Table: %q}", q.Op, q.Table, hook.OpGetMany, models.TableProducts)
		}
		if q.Input != any(in) {
			t.Errorf("QueryContext.Input = %#v, want the caller's input %p", q.Input, in)
		}
	})

	t.Run("returns only the selected columns", func(t *testing.T) {
		h.Reset(t)
		createPricedProduct(t, h, "a", 150)
		got, err := h.Client.Products().GetMany(ctx, nil, func(o *models.CallOptions[models.ProductFieldOptions]) {
			o.FieldOptions = &models.ProductFieldOptions{Name: true, Price: true}
		})
		if err != nil {
			t.Fatalf("GetMany({Name, Price}) error = %v", err)
		}
		if diff := gocmp.Diff([]*models.Product{{Name: "a", Price: 150}}, got); diff != "" {
			t.Errorf("GetMany({Name, Price}) (-want +got):\n%s", diff)
		}
	})

	t.Run("empty field options returns no entities and no error", func(t *testing.T) {
		h.Reset(t)
		createProduct(t, h, "a")
		got, err := h.Client.Products().GetMany(ctx, nil, func(o *models.CallOptions[models.ProductFieldOptions]) {
			o.FieldOptions = &models.ProductFieldOptions{}
		})
		if err != nil || len(got) != 0 {
			t.Errorf("GetMany({}) = %d entities, %v, want none and no error", len(got), err)
		}
	})

	t.Run("default field options load no relationship", func(t *testing.T) {
		h.Reset(t)
		p := createProduct(t, h, "p")
		getCreateOrderItem(t, h, 1, p.ID, 11)
		got, err := h.Client.Products().GetMany(ctx, nil)
		if err != nil || len(got) != 1 {
			t.Fatalf("GetMany(nil) = %d entities, %v, want 1", len(got), err)
		}
		if got[0].OrderItems != nil {
			t.Errorf("GetMany(nil) loaded OrderItems %+v, want nil", got[0].OrderItems)
		}
	})

	t.Run("selecting a relationship loads each row's own children", func(t *testing.T) {
		h.Reset(t)
		start := time.Now()
		p1, p2 := createPricedProduct(t, h, "p1", 100), createPricedProduct(t, h, "p2", 200)
		p3 := createPricedProduct(t, h, "p3", 300)
		end := time.Now()
		getCreateOrderItem(t, h, 1, p1.ID, 11)
		getCreateOrderItem(t, h, 2, p1.ID, 21)
		getCreateOrderItem(t, h, 1, p2.ID, 12)
		got, err := h.Client.Products().GetMany(ctx, nil, func(o *models.CallOptions[models.ProductFieldOptions]) {
			o.FieldOptions = &models.ProductFieldOptions{OrderItems: &models.OrderItemRelationshipOptions{}}
		})
		if err != nil {
			t.Fatalf("GetMany({OrderItems}) error = %v", err)
		}
		byName := getByName(t, got)
		// A selection with a relationship and no column reads every column.
		for _, want := range []*models.Product{
			{ID: p1.ID, Name: "p1", Price: 100}, {ID: p2.ID, Name: "p2", Price: 200}, {ID: p3.ID, Name: "p3", Price: 300},
		} {
			g := byName[want.Name]
			if g == nil {
				t.Errorf("GetMany({OrderItems}) has no row %q", want.Name)
				continue
			}
			getAssertCreatedAt(t, g, start, end)
			if diff := gocmp.Diff(want, getScalars(g)); diff != "" {
				t.Errorf("GetMany({OrderItems}) row %q without CreatedAt (-want +got):\n%s", want.Name, diff)
			}
		}
		wantItems := map[string][]models.OrderItem{
			"p1": {{OrderID: 1, ProductID: p1.ID, Quantity: 11}, {OrderID: 2, ProductID: p1.ID, Quantity: 21}},
			"p2": {{OrderID: 1, ProductID: p2.ID, Quantity: 12}},
			"p3": nil,
		}
		for name, want := range wantItems {
			if diff := gocmp.Diff(want, getSortedItems(byName[name])); diff != "" {
				t.Errorf("%s.OrderItems (-want +got):\n%s", name, diff)
			}
		}
	})

	t.Run("a narrowed selection without the key still loads the relationship", func(t *testing.T) {
		h.Reset(t)
		p1, p2 := createProduct(t, h, "p1"), createProduct(t, h, "p2")
		getCreateOrderItem(t, h, 1, p1.ID, 11)
		getCreateOrderItem(t, h, 1, p2.ID, 12)
		getCreateOrderItem(t, h, 2, p2.ID, 22)
		got, err := h.Client.Products().GetMany(ctx, nil, func(o *models.CallOptions[models.ProductFieldOptions]) {
			o.FieldOptions = &models.ProductFieldOptions{Name: true, OrderItems: &models.OrderItemRelationshipOptions{}}
		})
		if err != nil {
			t.Fatalf("GetMany({Name, OrderItems}) error = %v", err)
		}
		byName := getByName(t, got)
		if diff := gocmp.Diff([]int64{11}, getQuantities(byName["p1"])); diff != "" {
			t.Errorf("p1 order item quantities (-want +got):\n%s", diff)
		}
		if diff := gocmp.Diff([]int64{12, 22}, getQuantities(byName["p2"])); diff != "" {
			t.Errorf("p2 order item quantities (-want +got):\n%s", diff)
		}
	})

	t.Run("a narrowed child selection still maps each child to its row", func(t *testing.T) {
		h.Reset(t)
		p1, p2 := createProduct(t, h, "p1"), createProduct(t, h, "p2")
		getCreateOrderItem(t, h, 1, p1.ID, 11)
		getCreateOrderItem(t, h, 2, p1.ID, 21)
		getCreateOrderItem(t, h, 1, p2.ID, 12)
		got, err := h.Client.Products().GetMany(ctx, nil, func(o *models.CallOptions[models.ProductFieldOptions]) {
			o.FieldOptions = &models.ProductFieldOptions{
				ID:         true,
				Name:       true,
				OrderItems: &models.OrderItemRelationshipOptions{FieldOptions: &models.OrderItemFieldOptions{Quantity: true}},
			}
		})
		if err != nil {
			t.Fatalf("GetMany({ID, Name, OrderItems{Quantity}}) error = %v", err)
		}
		byName := getByName(t, got)
		if diff := gocmp.Diff([]int64{11, 21}, getQuantities(byName["p1"])); diff != "" {
			t.Errorf("p1 order item quantities (-want +got):\n%s", diff)
		}
		if diff := gocmp.Diff([]int64{12}, getQuantities(byName["p2"])); diff != "" {
			t.Errorf("p2 order item quantities (-want +got):\n%s", diff)
		}
	})

	t.Run("an empty child selection skips the child fetch and leaves the relationship nil", func(t *testing.T) {
		h.Reset(t)
		p := createProduct(t, h, "p")
		getCreateOrderItem(t, h, 1, p.ID, 11)
		h.Querier.ResetCount()
		got, err := h.Client.Products().GetMany(ctx, nil, func(o *models.CallOptions[models.ProductFieldOptions]) {
			o.FieldOptions = &models.ProductFieldOptions{
				Name:       true,
				OrderItems: &models.OrderItemRelationshipOptions{FieldOptions: &models.OrderItemFieldOptions{}},
			}
		})
		if err != nil || len(got) != 1 {
			t.Fatalf("GetMany({Name, OrderItems{}}) = %d entities, %v, want 1", len(got), err)
		}
		if got[0].Name != "p" || got[0].OrderItems != nil {
			t.Errorf("GetMany({Name, OrderItems{}}) = %+v, want p with OrderItems nil", got[0])
		}
		if stmts := h.Querier.Statements(); len(stmts) != 1 {
			t.Errorf("GetMany({Name, OrderItems{}}) sent %d statements %q, want only the products read", len(stmts), stmts)
		}
	})

	t.Run("a read leaves the caller's field options unchanged", func(t *testing.T) {
		h.Reset(t)
		p := createProduct(t, h, "p")
		getCreateOrderItem(t, h, 1, p.ID, 11)
		newOptions := func() *models.ProductFieldOptions {
			return &models.ProductFieldOptions{
				Name:       true,
				OrderItems: &models.OrderItemRelationshipOptions{FieldOptions: &models.OrderItemFieldOptions{Quantity: true}},
			}
		}
		parent := newOptions()
		child := parent.OrderItems.FieldOptions
		got, err := h.Client.Products().GetMany(ctx, nil, func(o *models.CallOptions[models.ProductFieldOptions]) {
			o.FieldOptions = parent
		})
		if err != nil || len(got) != 1 || len(got[0].OrderItems) != 1 {
			t.Fatalf("GetMany({Name, OrderItems{Quantity}}) = %+v, %v, want p with its order item", got, err)
		}
		want := newOptions()
		if diff := gocmp.Diff(want, parent); diff != "" {
			t.Errorf("the caller's ProductFieldOptions after GetMany (-before +after):\n%s", diff)
		}
		if diff := gocmp.Diff(want.OrderItems.FieldOptions, child); diff != "" {
			t.Errorf("the caller's OrderItemFieldOptions after GetMany (-before +after):\n%s", diff)
		}
	})

	t.Run("a lock mode outside a transaction returns an error before any SQL", func(t *testing.T) {
		h.Require(t, CapRowLocks)
		h.Reset(t)
		createProduct(t, h, "a")
		for _, mode := range lockModes {
			h.Querier.ResetCount()
			got, err := h.Client.Products().GetMany(ctx, nil, withLock[models.ProductFieldOptions](mode))
			if err == nil {
				t.Errorf("GetMany(%s) outside a transaction = %d entities, nil, want an error", mode, len(got))
			}
			if stmts := h.Querier.Statements(); len(stmts) != 0 {
				t.Errorf("GetMany(%s) outside a transaction sent %q, want no statement", mode, stmts)
			}
		}
	})

	t.Run("an unsupported lock mode outside a transaction returns an error before any SQL", func(t *testing.T) {
		h.Lacks(t, CapRowLocks)
		h.Reset(t)
		createProduct(t, h, "a")
		for _, mode := range lockModes {
			h.Querier.ResetCount()
			got, err := h.Client.Products().GetMany(ctx, nil, withLock[models.ProductFieldOptions](mode))
			if err == nil {
				t.Errorf("GetMany(%s) = %d entities, nil, want an error", mode, len(got))
			}
			if stmts := h.Querier.Statements(); len(stmts) != 0 {
				t.Errorf("GetMany(%s) sent %q, want no statement", mode, stmts)
			}
		}
	})

	t.Run("a lock mode inside a transaction locks the matched rows", func(t *testing.T) {
		h.Require(t, CapRowLocks)
		h.Reset(t)
		a, b := createProduct(t, h, "a"), createProduct(t, h, "b")
		for _, mode := range lockModes {
			txCtx := beginBoundedTx(t, h)
			got, err := h.Client.Products().GetMany(txCtx, &models.GetProductsInput{
				Filter: &models.ProductFilter{ID: &comparator.Number[int64]{Eq: new(a.ID)}},
			}, withLock[models.ProductFieldOptions](mode))
			if err != nil || len(got) != 1 || got[0].ID != a.ID {
				t.Errorf("GetMany(a, %s) in a transaction = %+v, %v, want product a", mode, got, err)
			}
			if err := getProbeLock(t, h, a.ID); err == nil {
				t.Errorf("after GetMany(a, %s) another transaction's NOWAIT read of a succeeded, want it to fail on the lock", mode)
			}
			if err := getProbeLock(t, h, b.ID); err != nil {
				t.Errorf("after GetMany(a, %s) another transaction's NOWAIT read of b = %v, want b unlocked", mode, err)
			}
			if err := database.Rollback(txCtx); err != nil {
				t.Fatalf("roll back: %v", err)
			}
		}
	})

	t.Run("LockForUpdateNoWait fails at once on a row another transaction holds", func(t *testing.T) {
		h.Require(t, CapRowLocks)
		h.Reset(t)
		a := createProduct(t, h, "a")
		contender := otherTx(t, h)
		if _, err := h.Client.Products().Get(contender, a.ID, withLock[models.ProductFieldOptions](sql.LockForUpdate)); err != nil {
			t.Fatalf("the contender's Get(a, LockForUpdate) error = %v", err)
		}
		txCtx := beginBoundedTx(t, h)
		got, err := h.Client.Products().GetMany(txCtx, &models.GetProductsInput{
			Filter: &models.ProductFilter{ID: &comparator.Number[int64]{Eq: new(a.ID)}},
		}, withLock[models.ProductFieldOptions](sql.LockForUpdateNoWait))
		if err == nil {
			t.Errorf("GetMany(a, LockForUpdateNoWait) on a held row = %+v, nil, want an error", got)
		}
		if ctxErr := txCtx.Err(); ctxErr != nil {
			t.Errorf("GetMany(a, LockForUpdateNoWait) waited for the lock until %v, want an immediate error", ctxErr)
		}
		// Once the contender lets go, the same read succeeds: the error was the lock.
		if err := database.Rollback(contender); err != nil {
			t.Fatalf("roll back the contender: %v", err)
		}
		retryCtx := beginBoundedTx(t, h)
		got, err = h.Client.Products().GetMany(retryCtx, &models.GetProductsInput{
			Filter: &models.ProductFilter{ID: &comparator.Number[int64]{Eq: new(a.ID)}},
		}, withLock[models.ProductFieldOptions](sql.LockForUpdateNoWait))
		if err != nil || len(got) != 1 || got[0].ID != a.ID {
			t.Errorf("GetMany(a, LockForUpdateNoWait) after the contender rolled back = %+v, %v, want product a", got, err)
		}
	})

	t.Run("LockForUpdateSkipLocked skips rows another transaction holds", func(t *testing.T) {
		h.Require(t, CapRowLocks)
		h.Reset(t)
		ids := make([]int64, 4)
		for i := range ids {
			ids[i] = createProduct(t, h, "job").ID
		}
		claim := func(txCtx context.Context) []int64 {
			t.Helper()
			got, err := h.Client.Products().GetMany(txCtx, &models.GetProductsInput{
				Sorts: []sql.Sort{{Column: "id", Direction: sql.Asc}},
				Limit: new(2),
			}, withLock[models.ProductFieldOptions](sql.LockForUpdateSkipLocked))
			if err != nil {
				t.Fatalf("GetMany(limit 2, LockForUpdateSkipLocked) error = %v", err)
			}
			return getSortedIDs(got)
		}
		first := claim(otherTx(t, h))
		if diff := gocmp.Diff(ids[:2], first); diff != "" {
			t.Errorf("first claim (-want +got):\n%s", diff)
		}
		second := claim(beginBoundedTx(t, h))
		if diff := gocmp.Diff(ids[2:], second); diff != "" {
			t.Errorf("second claim while the first is held (-want +got):\n%s", diff)
		}
	})

	t.Run("a locked read loads relationships without locking them", func(t *testing.T) {
		h.Require(t, CapRowLocks)
		h.Reset(t)
		p, q := createProduct(t, h, "p"), createProduct(t, h, "q")
		getCreateOrderItem(t, h, 1, p.ID, 11)
		contender := otherTx(t, h)
		pk := models.OrderItemPK{OrderID: 1, ProductID: p.ID}
		if _, err := h.Client.OrderItems().Get(contender, pk, withLock[models.OrderItemFieldOptions](sql.LockForUpdate)); err != nil {
			t.Fatalf("the contender's OrderItems().Get(LockForUpdate) error = %v", err)
		}
		// NOWAIT would fail on a locked child read; SKIP LOCKED would drop the held child.
		for _, mode := range []sql.LockMode{sql.LockForUpdateNoWait, sql.LockForUpdateSkipLocked} {
			txCtx := beginBoundedTx(t, h)
			got, err := h.Client.Products().GetMany(txCtx, &models.GetProductsInput{
				Filter: &models.ProductFilter{ID: &comparator.Number[int64]{Eq: new(p.ID)}},
			}, func(o *models.CallOptions[models.ProductFieldOptions]) {
				o.LockMode = mode
				o.FieldOptions = &models.ProductFieldOptions{OrderItems: &models.OrderItemRelationshipOptions{}}
			})
			if err != nil || len(got) != 1 {
				t.Errorf("GetMany(p, %s, {OrderItems}) with the child locked elsewhere = %d entities, %v, want p", mode, len(got), err)
			} else if diff := gocmp.Diff([]models.OrderItem{{OrderID: 1, ProductID: p.ID, Quantity: 11}}, getSortedItems(got[0])); diff != "" {
				t.Errorf("GetMany(p, %s) p.OrderItems (-want +got):\n%s", mode, diff)
			}
			// The parent read itself took the lock.
			if err := getProbeLock(t, h, p.ID); err == nil {
				t.Errorf("after GetMany(p, %s) another transaction's NOWAIT read of p succeeded, want p locked", mode)
			}
			if err := getProbeLock(t, h, q.ID); err != nil {
				t.Errorf("after GetMany(p, %s) another transaction's NOWAIT read of q = %v, want q unlocked", mode, err)
			}
			if err := database.Rollback(txCtx); err != nil {
				t.Fatalf("roll back: %v", err)
			}
		}
	})

	t.Run("an unsupported lock mode inside a transaction returns an error before any query hook runs", func(t *testing.T) {
		h.Lacks(t, CapRowLocks)
		h.Reset(t)
		createProduct(t, h, "a")
		client, seen := queryRecorder(h)
		for _, mode := range lockModes {
			var got []*models.Product
			err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
				var err error
				got, err = client.Products().GetMany(txCtx, nil, withLock[models.ProductFieldOptions](mode))
				return err
			})
			if err == nil {
				t.Errorf("GetMany(%s) in a transaction = %d entities, nil, want an error", mode, len(got))
			}
		}
		if qs := seen(); len(qs) != 0 {
			t.Errorf("GetMany with a lock mode ran the query hook chain %d times (%v), want the guard to return first", len(qs), getOps(qs))
		}
	})

	t.Run("composite keys return only the listed tuples", func(t *testing.T) {
		h.Reset(t)
		p1, p2 := createProduct(t, h, "p1").ID, createProduct(t, h, "p2").ID
		getCreateOrderItem(t, h, 1, p1, 11)
		getCreateOrderItem(t, h, 1, p2, 12)
		getCreateOrderItem(t, h, 2, p1, 21)
		getCreateOrderItem(t, h, 3, p2, 32)
		// Every listed order and product appears in some stored tuple, and
		// (2, p2) is not stored: a per-column match returns all four rows.
		got, err := h.Client.OrderItems().GetMany(ctx, &models.GetOrderItemsInput{
			Filter: &models.OrderItemFilter{PKs: []models.OrderItemPK{
				{OrderID: 1, ProductID: p1}, {OrderID: 3, ProductID: p2}, {OrderID: 2, ProductID: p2},
			}},
		})
		if err != nil {
			t.Fatalf("GetMany(PKs) error = %v", err)
		}
		want := []models.OrderItem{{OrderID: 1, ProductID: p1, Quantity: 11}, {OrderID: 3, ProductID: p2, Quantity: 32}}
		if diff := gocmp.Diff(want, getSortItems(got)); diff != "" {
			t.Errorf("GetMany(PKs) (-want +got):\n%s", diff)
		}
	})

	t.Run("a filter on one column of a composite key returns every tuple with that value", func(t *testing.T) {
		h.Reset(t)
		p1, p2 := createProduct(t, h, "p1").ID, createProduct(t, h, "p2").ID
		getCreateOrderItem(t, h, 1, p1, 11)
		getCreateOrderItem(t, h, 1, p2, 12)
		getCreateOrderItem(t, h, 2, p1, 21)
		got, err := h.Client.OrderItems().GetMany(ctx, &models.GetOrderItemsInput{
			Filter: &models.OrderItemFilter{OrderID: &comparator.Number[int64]{Eq: new(int64(1))}},
		})
		if err != nil {
			t.Fatalf("GetMany(order_id = 1) error = %v", err)
		}
		want := []models.OrderItem{{OrderID: 1, ProductID: p1, Quantity: 11}, {OrderID: 1, ProductID: p2, Quantity: 12}}
		if diff := gocmp.Diff(want, getSortItems(got)); diff != "" {
			t.Errorf("GetMany(order_id = 1) (-want +got):\n%s", diff)
		}
	})

	t.Run("a repeated read queries the database again and sees a write made outside the client", func(t *testing.T) {
		h.Reset(t)
		createProduct(t, h, "before")
		if _, err := h.Client.Products().GetMany(ctx, nil); err != nil {
			t.Fatalf("first GetMany error = %v", err)
		}
		renameProductsOutOfBand(t, h)
		h.Querier.ResetCount()
		got, err := h.Client.Products().GetMany(ctx, nil)
		if err != nil {
			t.Fatalf("second GetMany error = %v", err)
		}
		if len(got) != 1 || got[0].Name != "after" {
			t.Errorf("second GetMany = %+v, want the one row renamed %q", got, "after")
		}
		if n := len(statementsWithPrefix(h, "SELECT")); n != 1 {
			t.Errorf("second GetMany sent %d SELECT statements, want 1", n)
		}
	})

	t.Run("overlapping identical reads each query the database", func(t *testing.T) {
		h.Reset(t)
		createProduct(t, h, "p")
		const n = 4
		barrier, overlapped := queryBarrier(n, readOf(hook.OpGetMany, models.TableProducts))
		c := models.New(h.Base, models.WithQueryHook(barrier))
		h.Querier.ResetCount()
		errs := make([]error, n)
		var wg sync.WaitGroup
		for i := range n {
			wg.Go(func() { _, errs[i] = c.Products().GetMany(ctx, nil) })
		}
		wg.Wait()
		if !overlapped() {
			t.Fatalf("the %d reads never overlapped", n)
		}
		if err := errors.Join(errs...); err != nil {
			t.Fatalf("overlapping GetManys: %v", err)
		}
		if got := len(statementsWithPrefix(h, "SELECT")); got != n {
			t.Errorf("%d overlapping GetManys sent %d SELECT statements, want %d", n, got, n)
		}
	})

	t.Run("a failed relationship load returns that error and no rows", func(t *testing.T) {
		h.Reset(t)
		seedAuthorEdges(t, h)
		boom := errors.New("tags load failed")
		c, postsCanceled := failingTagsLoad(h, boom)
		got, err := c.Authors().GetMany(ctx, nil, withPostsAndTags)
		if got != nil {
			t.Errorf("GetMany = %+v, want nil", got)
		}
		if !errors.Is(err, boom) || errors.Is(err, context.Canceled) {
			t.Errorf("GetMany error = %v, want the tags load's error and not the canceled posts load's", err)
		}
		if !postsCanceled() {
			t.Error("the posts load was not canceled when the tags load failed")
		}
	})

	getManyCompositeKey(t, h)
}

// exists: Exists reports whether a live row has the given key.
func exists(t *testing.T, h *Harness) {
	ctx := context.Background()

	t.Run("a present key returns true", func(t *testing.T) {
		h.Reset(t)
		// The present row sits between others, and a deleted key next to it reads false.
		createProduct(t, h, "before")
		present := createProduct(t, h, "present").ID
		missing := createProduct(t, h, "missing").ID
		createProduct(t, h, "after")
		if err := h.Client.Products().HardDelete(ctx, missing); err != nil {
			t.Fatalf("HardDelete(missing) error = %v", err)
		}
		getAssertExists(t, true)(h.Client.Products().Exists(ctx, present))
		getAssertExists(t, false)(h.Client.Products().Exists(ctx, missing))
	})

	t.Run("a missing key returns false", func(t *testing.T) {
		h.Reset(t)
		// Rows sit on both sides of the missing key.
		missing := getMissingProductKey(t, h)
		getAssertExists(t, false)(h.Client.Products().Exists(ctx, missing))
	})

	t.Run("a soft-deleted row returns false", func(t *testing.T) {
		h.Reset(t)
		deleted, live := createDocument(t, h, "deleted"), createDocument(t, h, "live")
		if _, err := h.Client.Documents().SoftDelete(ctx, deleted.ID); err != nil {
			t.Fatalf("SoftDelete(deleted) error = %v", err)
		}
		getAssertExists(t, false)(h.Client.Documents().Exists(ctx, deleted.ID))
		getAssertExists(t, true)(h.Client.Documents().Exists(ctx, live.ID))
	})

	t.Run("sees a row written earlier in the same transaction", func(t *testing.T) {
		h.Reset(t)
		errRollback := errors.New("roll back")
		var id int64
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			p, err := h.Client.Products().Create(txCtx, &models.CreateProductInput{Name: "in-tx", Price: 100})
			if err != nil {
				return err
			}
			id = p.ID
			// No t.Fatal in here: it would skip the rollback.
			if got, err := h.Client.Products().Exists(txCtx, id); err != nil || !got {
				t.Errorf("Exists inside the transaction = %v, %v, want true", got, err)
			}
			return errRollback
		})
		if !errors.Is(err, errRollback) {
			t.Fatalf("WithTransaction error = %v, want the rollback sentinel", err)
		}
		getAssertExists(t, false)(h.Client.Products().Exists(ctx, id))
	})

	t.Run("fires one exists query hook with the key and no input", func(t *testing.T) {
		h.Reset(t)
		a := createProduct(t, h, "a")
		client, seen := queryRecorder(h)
		if _, err := client.Products().Exists(ctx, a.ID); err != nil {
			t.Fatalf("Exists(a) error = %v", err)
		}
		qs := seen()
		if len(qs) != 1 {
			t.Fatalf("query hook fired %d times (%v), want 1", len(qs), getOps(qs))
		}
		q := qs[0]
		if q.Op != hook.OpExists || q.Table != models.TableProducts || q.PK != any(a.ID) || q.Input != nil {
			t.Errorf("QueryContext = {Op: %q, Table: %q, PK: %v, Input: %#v}, want {Op: %q, Table: %q, PK: %d, Input: nil}",
				q.Op, q.Table, q.PK, q.Input, hook.OpExists, models.TableProducts, a.ID)
		}
	})

	t.Run("a composite key matches only its own tuple", func(t *testing.T) {
		h.Reset(t)
		p1, p2 := createProduct(t, h, "p1").ID, createProduct(t, h, "p2").ID
		getCreateOrderItem(t, h, 1, p1, 11)
		getCreateOrderItem(t, h, 2, p2, 22)
		getAssertExists(t, false)(h.Client.OrderItems().Exists(ctx, models.OrderItemPK{OrderID: 1, ProductID: p2}))
		getAssertExists(t, true)(h.Client.OrderItems().Exists(ctx, models.OrderItemPK{OrderID: 1, ProductID: p1}))
	})
}

// getAssertExists checks an Exists result: getAssertExists(t, want)(c.Exists(…)).
func getAssertExists(t *testing.T, want bool) func(got bool, err error) {
	t.Helper()
	return func(got bool, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("Exists error = %v, want nil", err)
		}
		if got != want {
			t.Errorf("Exists = %v, want %v", got, want)
		}
	}
}

// getProbeLock reads product id with LockForUpdateNoWait in a transaction of
// its own and rolls it back. It returns the read's error, which is non-nil
// when another transaction holds a conflicting lock on the row.
func getProbeLock(t *testing.T, h *Harness, id int64) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), lockWait)
	defer cancel()
	txCtx, err := database.NewTransaction(ctx, h.Querier, "probe")
	if err != nil {
		t.Fatalf("begin the probe transaction: %v", err)
	}
	defer func() { _ = database.Rollback(txCtx) }()
	_, err = h.Client.Products().Get(txCtx, id, withLock[models.ProductFieldOptions](sql.LockForUpdateNoWait))
	return err
}

// authorProbeLock is getProbeLock for an author row.
func authorProbeLock(t *testing.T, h *Harness, id int64) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), lockWait)
	defer cancel()
	txCtx, err := database.NewTransaction(ctx, h.Querier, "probe")
	if err != nil {
		t.Fatalf("begin the probe transaction: %v", err)
	}
	defer func() { _ = database.Rollback(txCtx) }()
	_, err = h.Client.Authors().Get(txCtx, id, withLock[models.AuthorFieldOptions](sql.LockForUpdateNoWait))
	return err
}

// profileProbeLock is getProbeLock for an author profile row.
func profileProbeLock(t *testing.T, h *Harness, id int64) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), lockWait)
	defer cancel()
	txCtx, err := database.NewTransaction(ctx, h.Querier, "probe")
	if err != nil {
		t.Fatalf("begin the probe transaction: %v", err)
	}
	defer func() { _ = database.Rollback(txCtx) }()
	_, err = h.Client.AuthorProfiles().Get(txCtx, id, withLock[models.AuthorProfileFieldOptions](sql.LockForUpdateNoWait))
	return err
}

// getScalars returns p without its relationships and CreatedAt, for comparing
// the columns a subtest states exactly.
func getScalars(p *models.Product) *models.Product {
	var zero models.Product
	c := *p
	c.OrderItems = nil
	c.CreatedAt = zero.CreatedAt
	return &c
}

// getAssertCreatedAt checks that p's CreatedAt, the column's database default,
// falls near the window the row was created in. The margin absorbs clock skew
// between the test host and a database container.
func getAssertCreatedAt(t *testing.T, p *models.Product, start, end time.Time) {
	t.Helper()
	const skew = 10 * time.Minute
	if p.CreatedAt.Before(start.Add(-skew)) || p.CreatedAt.After(end.Add(skew)) {
		t.Errorf("%q CreatedAt = %v, want within %v of [%v, %v]", p.Name, p.CreatedAt, skew, start, end)
	}
}

// getMissingProductKey creates three products, hard-deletes the middle one and
// returns its key, so live rows sit on both sides of it.
func getMissingProductKey(t *testing.T, h *Harness) int64 {
	t.Helper()
	createProduct(t, h, "before")
	missing := createProduct(t, h, "missing").ID
	createProduct(t, h, "after")
	if err := h.Client.Products().HardDelete(context.Background(), missing); err != nil {
		t.Fatalf("HardDelete(missing) error = %v", err)
	}
	return missing
}

func getCreateOrderItem(t *testing.T, h *Harness, orderID, productID, quantity int64) {
	t.Helper()
	in := &models.CreateOrderItemInput{OrderID: orderID, ProductID: productID, Quantity: quantity}
	if _, err := h.Client.OrderItems().Create(context.Background(), in); err != nil {
		t.Fatalf("Create order item (%d, %d): %v", orderID, productID, err)
	}
}

func getSortedIDs(ps []*models.Product) []int64 {
	ids := make([]int64, len(ps))
	for i, p := range ps {
		ids[i] = p.ID
	}
	slices.Sort(ids)
	return ids
}

func getDocumentIDs(ds []*models.Document) []int64 {
	ids := make([]int64, len(ds))
	for i, d := range ds {
		ids[i] = d.ID
	}
	slices.Sort(ids)
	return ids
}

// getNames returns the products' names in result order.
func getNames(ps []*models.Product) []string {
	names := make([]string, len(ps))
	for i, p := range ps {
		names[i] = p.Name
	}
	return names
}

// getByName indexes products by their unique name.
func getByName(t *testing.T, ps []*models.Product) map[string]*models.Product {
	t.Helper()
	m := make(map[string]*models.Product, len(ps))
	for _, p := range ps {
		if _, dup := m[p.Name]; dup {
			t.Fatalf("two rows named %q", p.Name)
		}
		m[p.Name] = p
	}
	return m
}

// getSortItems copies order items into key order; none reads as nil.
func getSortItems(items []*models.OrderItem) []models.OrderItem {
	if len(items) == 0 {
		return nil
	}
	out := make([]models.OrderItem, len(items))
	for i, it := range items {
		out[i] = *it
	}
	slices.SortFunc(out, func(a, b models.OrderItem) int {
		return cmp.Or(cmp.Compare(a.OrderID, b.OrderID), cmp.Compare(a.ProductID, b.ProductID))
	})
	return out
}

// getSortedItems returns a product's loaded order items in key order; a
// missing product reads as nil.
func getSortedItems(p *models.Product) []models.OrderItem {
	if p == nil {
		return nil
	}
	return getSortItems(p.OrderItems)
}

// getQuantities returns the sorted quantities of a product's loaded order items.
func getQuantities(p *models.Product) []int64 {
	if p == nil {
		return nil
	}
	var qs []int64
	for _, it := range p.OrderItems {
		qs = append(qs, it.Quantity)
	}
	slices.Sort(qs)
	return qs
}

func getOps(qs []hook.QueryContext) []string {
	ops := make([]string, len(qs))
	for i, q := range qs {
		ops[i] = string(q.Op) + " " + string(q.Table)
	}
	return ops
}
