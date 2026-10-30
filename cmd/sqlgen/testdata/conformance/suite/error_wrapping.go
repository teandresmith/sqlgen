package suite

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/hook"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/models"
)

// errorWrapping checks that every client method's error starts with its own
// operation and table prefix and keeps its cause reachable.
func errorWrapping(t *testing.T, h *Harness) {
	bg := context.Background()
	canceled, cancel := context.WithCancel(bg)
	cancel()
	productByID := func(id int64) *models.ProductFilter {
		return &models.ProductFilter{ID: &comparator.Number[int64]{Eq: new(id)}}
	}
	documentByID := func(id int64) *models.DocumentFilter {
		return &models.DocumentFilter{ID: &comparator.Number[int64]{Eq: new(id)}}
	}
	createCounter := func(t *testing.T, name string) *models.Counter {
		t.Helper()
		c, err := h.Client.Counters().Create(bg, &models.CreateCounterInput{Name: name, UpdatedAt: omittable.Set(models.Now())})
		if err != nil {
			t.Fatalf("Create counter %q: %v", name, err)
		}
		return c
	}
	// referencedProduct is a product an order item references, so deleting it
	// violates order_items' foreign key.
	referencedProduct := func(t *testing.T) *models.Product {
		t.Helper()
		h.Reset(t)
		p := createProduct(t, h, "referenced")
		createOrderItem(t, h, models.OrderItemPK{OrderID: 1, ProductID: p.ID})
		return p
	}

	t.Run("Get prefixes a database failure with get and the singular, ahead of the get many cause", func(t *testing.T) {
		h.Reset(t)
		p := createProduct(t, h, "p")
		_, err := h.Client.Products().Get(canceled, p.ID)
		assertErrorPrefix(t, err, "get product: get many products: ", context.Canceled)
	})
	t.Run("Get prefixes a missing key with get and the singular", func(t *testing.T) {
		h.Reset(t)
		_, err := h.Client.Products().Get(bg, -1)
		assertErrorPrefix(t, err, "get product: ", models.ErrNotFound)
	})
	t.Run("Get prefixes a missing key on a multi-word table with get and the singularized table name", func(t *testing.T) {
		h.Reset(t)
		_, err := h.Client.OrderItems().Get(bg, models.OrderItemPK{OrderID: -1, ProductID: -1})
		assertErrorPrefix(t, err, "get order_item: ", models.ErrNotFound)
	})
	t.Run("Get prefixes a failed relationship load with get and the singular", func(t *testing.T) {
		h.Reset(t)
		a := createAuthor(t, h, "a")
		createPost(t, h, "p", new(a.ID))
		c, errLoad := failingPostsReads(h)
		_, err := c.Authors().Get(bg, a.ID, selectAuthorPosts)
		assertErrorPrefix(t, err, "get author: ", errLoad)
	})
	t.Run("GetMany prefixes a database failure with get many and the table", func(t *testing.T) {
		h.Reset(t)
		_, err := h.Client.Products().GetMany(canceled, nil)
		assertErrorPrefix(t, err, "get many products: ", context.Canceled)
	})
	t.Run("GetMany prefixes a failed relationship load with get many and the table", func(t *testing.T) {
		h.Reset(t)
		a := createAuthor(t, h, "a")
		createPost(t, h, "p", new(a.ID))
		c, errLoad := failingPostsReads(h)
		_, err := c.Authors().GetMany(bg, nil, selectAuthorPosts)
		assertErrorPrefix(t, err, "get many authors: ", errLoad)
	})
	t.Run("Count prefixes a database failure with count and the table", func(t *testing.T) {
		h.Reset(t)
		_, err := h.Client.Products().Count(canceled, nil)
		assertErrorPrefix(t, err, "count products: ", context.Canceled)
	})
	t.Run("Exists prefixes a database failure with exists and the singular", func(t *testing.T) {
		h.Reset(t)
		_, err := h.Client.Products().Exists(canceled, 1)
		assertErrorPrefix(t, err, "exists product: ", context.Canceled)
	})
	t.Run("ExistsWhere prefixes a database failure with exists, the table and where", func(t *testing.T) {
		h.Reset(t)
		_, err := h.Client.Products().ExistsWhere(canceled, productByID(1))
		assertErrorPrefix(t, err, "exists products where: ", context.Canceled)
	})
	t.Run("Stream prefixes a yielded database failure with stream and the table", func(t *testing.T) {
		h.Reset(t)
		createProduct(t, h, "p")
		var err error
		for _, e := range h.Client.Products().Stream(canceled, nil) {
			if e != nil {
				err = e
				break
			}
		}
		assertErrorPrefix(t, err, "stream products: ", context.Canceled)
	})
	t.Run("Paginate prefixes a database failure with paginate and the table", func(t *testing.T) {
		h.Reset(t)
		_, err := h.Client.Products().Paginate(canceled, models.PaginateInput[models.ProductFilter]{Limit: 1})
		assertErrorPrefix(t, err, "paginate products: ", context.Canceled)
	})
	t.Run("Connection prefixes a database failure with connection and the table", func(t *testing.T) {
		h.Reset(t)
		_, err := h.Client.Products().Connection(canceled, models.ConnectionInput[models.ProductFilter]{First: new(1)})
		assertErrorPrefix(t, err, "connection products: ", context.Canceled)
	})
	t.Run("Connection prefixes an invalid cursor with connection and the table", func(t *testing.T) {
		h.Reset(t)
		_, err := h.Client.Products().Connection(bg, models.ConnectionInput[models.ProductFilter]{First: new(1), After: new("not a cursor")})
		assertErrorPrefix(t, err, "connection products: ", models.ErrInvalidCursor)
	})

	t.Run("Create prefixes a constraint violation with create and the singular", func(t *testing.T) {
		h.Reset(t)
		createCounter(t, "taken")
		_, err := h.Client.Counters().Create(bg, &models.CreateCounterInput{Name: "taken", UpdatedAt: omittable.Set(models.Now())})
		assertErrorPrefix(t, err, "create counter: ", models.ErrConstraintViolation)
	})
	t.Run("Create prefixes a nil input with create and the singular", func(t *testing.T) {
		_, err := h.Client.Products().Create(bg, nil)
		assertErrorPrefix(t, err, "create product: ", models.ErrNilInput)
	})
	t.Run("Create prefixes a recovered panic with create and the singular", func(t *testing.T) {
		h.Reset(t)
		c := models.New(h.Base, models.WithMutationHook(func(next hook.MutationHandler) hook.MutationHandler {
			return func(context.Context, *hook.MutationContext) (any, error) { panic("boom") }
		}))
		_, err := c.Products().Create(bg, &models.CreateProductInput{Name: "p", Price: 1})
		want := fmt.Sprintf("create product: sqlgen: panic in create %s: boom", models.TableProducts)
		if err == nil || err.Error() != want {
			t.Errorf("err = %v, want %q", err, want)
		}
	})
	t.Run("CreateMany prefixes a constraint violation with create many and the table", func(t *testing.T) {
		h.Reset(t)
		createCounter(t, "taken")
		_, err := h.Client.Counters().CreateMany(bg, []*models.CreateCounterInput{{Name: "taken", UpdatedAt: omittable.Set(models.Now())}})
		assertErrorPrefix(t, err, "create many counters: ", models.ErrConstraintViolation)
	})
	t.Run("CreateMany prefixes a nil element with create many and the table", func(t *testing.T) {
		_, err := h.Client.Products().CreateMany(bg, []*models.CreateProductInput{nil})
		assertErrorPrefix(t, err, "create many products: ", models.ErrNilInput)
	})

	t.Run("Update prefixes a constraint violation with update and the singular", func(t *testing.T) {
		h.Reset(t)
		createCounter(t, "taken")
		c := createCounter(t, "free")
		_, err := h.Client.Counters().Update(bg, c.ID, &models.UpdateCounterInput{Name: omittable.Set("taken")})
		assertErrorPrefix(t, err, "update counter: ", models.ErrConstraintViolation)
	})
	t.Run("Update prefixes a missing key with update and the singular", func(t *testing.T) {
		h.Reset(t)
		_, err := h.Client.Products().Update(bg, -1, &models.UpdateProductInput{Name: omittable.Set("y")})
		assertErrorPrefix(t, err, "update product: ", models.ErrNotFound)
	})
	t.Run("Update prefixes a nil input with update and the singular", func(t *testing.T) {
		_, err := h.Client.Products().Update(bg, 1, nil)
		assertErrorPrefix(t, err, "update product: ", models.ErrNilInput)
	})
	t.Run("UpdateMany prefixes a transaction that cannot begin with update many and the table", func(t *testing.T) {
		h.Reset(t)
		p := createProduct(t, h, "p")
		refuser := &beginRefuser{Querier: h.Base}
		c := models.New(refuser)
		_, err := c.Products().UpdateMany(bg, []models.UpdateProductItem{{ID: p.ID, Input: &models.UpdateProductInput{Name: omittable.Set("y")}}})
		assertErrorPrefix(t, err, "update many products: ", errBeginRefused)
		if refuser.n.Load() == 0 {
			t.Error("UpdateMany never began a transaction, so the begin failure was not exercised")
		}
	})
	t.Run("UpdateMany prefixes a failing item with update many and the table", func(t *testing.T) {
		h.Reset(t)
		createCounter(t, "taken")
		c := createCounter(t, "free")
		_, err := h.Client.Counters().UpdateMany(bg, []models.UpdateCounterItem{{ID: c.ID, Input: &models.UpdateCounterInput{Name: omittable.Set("taken")}}})
		assertErrorPrefix(t, err, "update many counters: ", models.ErrConstraintViolation)
	})
	t.Run("UpdateMany prefixes a nil item input with update many and the table", func(t *testing.T) {
		_, err := h.Client.Products().UpdateMany(bg, []models.UpdateProductItem{{ID: 1}})
		assertErrorPrefix(t, err, "update many products: ", models.ErrNilInput)
	})
	t.Run("UpdateWhere prefixes a constraint violation with update, the table and where", func(t *testing.T) {
		h.Reset(t)
		createCounter(t, "taken")
		c := createCounter(t, "free")
		_, err := h.Client.Counters().UpdateWhere(bg, &models.CounterFilter{ID: &comparator.Number[int64]{Eq: new(c.ID)}}, &models.UpdateCounterInput{Name: omittable.Set("taken")})
		assertErrorPrefix(t, err, "update counters where: ", models.ErrConstraintViolation)
	})
	t.Run("UpdateWhere prefixes an empty filter on a multi-word table with update, the table name as written and where", func(t *testing.T) {
		_, err := h.Client.OrderItems().UpdateWhere(bg, nil, &models.UpdateOrderItemInput{Quantity: omittable.Set(int64(2))})
		assertErrorPrefix(t, err, "update order_items where: ", models.ErrEmptyFilter)
	})
	t.Run("UpdateWhere prefixes a nil input with update, the table and where", func(t *testing.T) {
		_, err := h.Client.Products().UpdateWhere(bg, productByID(1), nil)
		assertErrorPrefix(t, err, "update products where: ", models.ErrNilInput)
	})

	t.Run("Upsert prefixes a constraint violation with upsert and the singular", func(t *testing.T) {
		h.Reset(t)
		_, err := h.Client.Counters().Upsert(bg, &models.CreateCounterInput{Name: "no timestamp"}, models.CounterConflictName)
		assertErrorPrefix(t, err, "upsert counter: ", models.ErrConstraintViolation)
	})
	t.Run("Upsert prefixes a nil input with upsert and the singular", func(t *testing.T) {
		_, err := h.Client.Products().Upsert(bg, nil, models.ProductConflictPK)
		assertErrorPrefix(t, err, "upsert product: ", models.ErrNilInput)
	})
	t.Run("UpsertMany prefixes a constraint violation with upsert many and the table", func(t *testing.T) {
		h.Reset(t)
		_, err := h.Client.Counters().UpsertMany(bg, []*models.CreateCounterInput{{Name: "no timestamp"}}, models.CounterConflictName)
		assertErrorPrefix(t, err, "upsert many counters: ", models.ErrConstraintViolation)
	})
	t.Run("UpsertMany prefixes a nil element with upsert many and the table", func(t *testing.T) {
		_, err := h.Client.Products().UpsertMany(bg, []*models.CreateProductInput{nil}, models.ProductConflictPK)
		assertErrorPrefix(t, err, "upsert many products: ", models.ErrNilInput)
	})

	t.Run("Increment prefixes a constraint violation with increment and the singular, without the column", func(t *testing.T) {
		h.Reset(t)
		c := createCounter(t, "c")
		err := h.Client.Counters().Increment(bg, c.ID, models.IncrementInput[models.CounterIncrementColumn]{Column: models.CounterIncrementHits, Amount: -5})
		assertErrorPrefix(t, err, "increment counter: ", models.ErrConstraintViolation)
	})
	t.Run("Increment prefixes a missing key with increment and the singular", func(t *testing.T) {
		h.Reset(t)
		err := h.Client.Products().Increment(bg, -1, models.IncrementInput[models.ProductIncrementColumn]{Column: models.ProductIncrementPrice, Amount: 1})
		assertErrorPrefix(t, err, "increment product: ", models.ErrNotFound)
	})

	t.Run("SoftDelete prefixes a constraint violation with soft delete and the singular", func(t *testing.T) {
		h.Reset(t)
		d := createDocument(t, h, "pinned")
		_, err := h.Client.Documents().SoftDelete(bg, d.ID)
		assertErrorPrefix(t, err, "soft delete document: ", models.ErrConstraintViolation)
	})
	t.Run("SoftDeleteMany prefixes a constraint violation with soft delete many and the table", func(t *testing.T) {
		h.Reset(t)
		d := createDocument(t, h, "pinned")
		_, err := h.Client.Documents().SoftDeleteMany(bg, []int64{d.ID})
		assertErrorPrefix(t, err, "soft delete many documents: ", models.ErrConstraintViolation)
	})
	t.Run("SoftDeleteWhere prefixes a constraint violation with soft delete, the table and where", func(t *testing.T) {
		h.Reset(t)
		d := createDocument(t, h, "pinned")
		_, err := h.Client.Documents().SoftDeleteWhere(bg, documentByID(d.ID))
		assertErrorPrefix(t, err, "soft delete documents where: ", models.ErrConstraintViolation)
	})
	t.Run("SoftDeleteWhere prefixes an empty filter with soft delete, the table and where", func(t *testing.T) {
		_, err := h.Client.Documents().SoftDeleteWhere(bg, nil)
		assertErrorPrefix(t, err, "soft delete documents where: ", models.ErrEmptyFilter)
	})
	t.Run("Restore prefixes a constraint violation with restore and the singular", func(t *testing.T) {
		h.Reset(t)
		id := seedArchivedDocument(t, h)
		_, err := h.Client.Documents().Restore(bg, id)
		assertErrorPrefix(t, err, "restore document: ", models.ErrConstraintViolation)
	})
	t.Run("RestoreMany prefixes a constraint violation with restore many and the table", func(t *testing.T) {
		h.Reset(t)
		id := seedArchivedDocument(t, h)
		_, err := h.Client.Documents().RestoreMany(bg, []int64{id})
		assertErrorPrefix(t, err, "restore many documents: ", models.ErrConstraintViolation)
	})
	t.Run("RestoreWhere prefixes a constraint violation with restore, the table and where", func(t *testing.T) {
		h.Reset(t)
		id := seedArchivedDocument(t, h)
		_, err := h.Client.Documents().RestoreWhere(bg, documentByID(id))
		assertErrorPrefix(t, err, "restore documents where: ", models.ErrConstraintViolation)
	})
	t.Run("RestoreWhere prefixes an empty filter with restore, the table and where", func(t *testing.T) {
		_, err := h.Client.Documents().RestoreWhere(bg, nil)
		assertErrorPrefix(t, err, "restore documents where: ", models.ErrEmptyFilter)
	})
	t.Run("HardDelete prefixes a constraint violation with hard delete and the singular", func(t *testing.T) {
		p := referencedProduct(t)
		err := h.Client.Products().HardDelete(bg, p.ID)
		assertErrorPrefix(t, err, "hard delete product: ", models.ErrConstraintViolation)
	})
	t.Run("HardDeleteMany prefixes a constraint violation with hard delete many and the table", func(t *testing.T) {
		p := referencedProduct(t)
		err := h.Client.Products().HardDeleteMany(bg, []int64{p.ID})
		assertErrorPrefix(t, err, "hard delete many products: ", models.ErrConstraintViolation)
	})
	t.Run("HardDeleteWhere prefixes a constraint violation with hard delete, the table and where", func(t *testing.T) {
		p := referencedProduct(t)
		err := h.Client.Products().HardDeleteWhere(bg, productByID(p.ID))
		assertErrorPrefix(t, err, "hard delete products where: ", models.ErrConstraintViolation)
	})
	t.Run("HardDeleteWhere prefixes an empty filter with hard delete, the table and where", func(t *testing.T) {
		err := h.Client.Products().HardDeleteWhere(bg, nil)
		assertErrorPrefix(t, err, "hard delete products where: ", models.ErrEmptyFilter)
	})

	t.Run("CreateWithRelated prefixes an edge failure once with create with related, the edge and the verb", func(t *testing.T) {
		h.Reset(t)
		_, err := h.Client.Authors().CreateWithRelated(bg, &models.CreateAuthorWithRelatedInput{
			Author: models.CreateAuthorInput{Name: "a"},
			Posts:  &models.AuthorPostsCreateNested{Connect: []int64{-1}},
		})
		assertNestedErrorPrefix(t, err, "create author with related: ")
	})
	t.Run("CreateWithRelated prefixes a nil input with create with related and the singular", func(t *testing.T) {
		_, err := h.Client.Authors().CreateWithRelated(bg, nil)
		assertErrorPrefix(t, err, "create author with related: ", models.ErrNilInput)
	})
	t.Run("UpdateWithRelated prefixes an edge failure once with update with related, the edge and the verb", func(t *testing.T) {
		h.Reset(t)
		a := createAuthor(t, h, "a")
		_, err := h.Client.Authors().UpdateWithRelated(bg, a.ID, &models.UpdateAuthorWithRelatedInput{
			Posts: &models.AuthorPostsUpdateNested{Connect: []int64{-1}},
		})
		assertNestedErrorPrefix(t, err, "update author with related: ")
	})
	t.Run("UpsertWithRelated prefixes an edge failure once with upsert with related, the edge and the verb", func(t *testing.T) {
		h.Reset(t)
		_, err := h.Client.Authors().UpsertWithRelated(bg, &models.UpsertAuthorWithRelatedInput{
			Author: models.CreateAuthorInput{Name: "a"},
			Posts:  &models.AuthorPostsUpdateNested{Connect: []int64{-1}},
		}, models.AuthorConflictName)
		assertNestedErrorPrefix(t, err, "upsert author with related: ")
	})
}

// assertErrorPrefix checks that err starts with prefix, carries it exactly
// once, and still wraps cause.
func assertErrorPrefix(t *testing.T, err error, prefix string, cause error) {
	t.Helper()
	if err == nil {
		t.Fatalf("err = nil, want an error starting %q", prefix)
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, prefix) {
		t.Errorf("err = %q, want it to start %q", msg, prefix)
	}
	if n := strings.Count(msg, prefix); n != 1 {
		t.Errorf("err = %q carries %q %d times, want once", msg, prefix, n)
	}
	if !errors.Is(err, cause) {
		t.Errorf("err = %q does not wrap %q", msg, cause)
	}
}

// assertNestedErrorPrefix checks a failed connect on the Posts edge: the
// method's prefix, then the edge and verb exactly once, with the not-found
// cause and the *NestedMutationError both reachable.
func assertNestedErrorPrefix(t *testing.T, err error, prefix string) {
	t.Helper()
	assertErrorPrefix(t, err, prefix+"Posts: connect: ", models.ErrNotFound)
	if err == nil {
		return
	}
	if n := strings.Count(err.Error(), "Posts: connect:"); n != 1 {
		t.Errorf("err = %q names the edge and verb %d times, want once", err, n)
	}
	nme, ok := errors.AsType[*models.NestedMutationError](err)
	if !ok {
		t.Fatalf("err = %q does not wrap a *NestedMutationError", err)
	}
	if nme.Edge != "Posts" || nme.Verb != "connect" {
		t.Errorf("NestedMutationError = {Edge: %q, Verb: %q}, want {Posts, connect}", nme.Edge, nme.Verb)
	}
}
