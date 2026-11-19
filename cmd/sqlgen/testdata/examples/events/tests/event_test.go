package tests

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/teandresmith/sqlgen/event"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/events/models"
)

func TestEvent_Create(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t)
	got := subscribe(t, bus, event.SubscribeOptions{})

	input := &models.CreateProductInput{Name: "Widget", SKU: "EV-CREATE-1", Price: 9.99}
	p, err := client.Products().Create(ctx, input)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, p.ID) })

	events := got.snapshot()
	// Expect 1 Create event. The HardDelete from t.Cleanup runs later.
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	e := events[0]
	if e.Action != event.Create {
		t.Errorf("Action = %q, want %q", e.Action, event.Create)
	}
	if e.Table != "products" {
		t.Errorf("Table = %q, want %q", e.Table, "products")
	}
	if e.Schema != "" {
		t.Errorf("Schema = %q, want empty", e.Schema)
	}
	if e.ID == "" {
		t.Error("event ID must be non-empty")
	}
	if e.Timestamp.IsZero() {
		t.Error("event Timestamp must be set")
	}
	if time.Since(e.Timestamp) > time.Minute {
		t.Errorf("event Timestamp looks stale: %v", e.Timestamp)
	}
	if e.PK != p.ID {
		t.Errorf("PK = %v, want %v", e.PK, p.ID)
	}
	gotInput, ok := e.Input.(*models.CreateProductInput)
	if !ok {
		t.Fatalf("Input type = %T, want *CreateProductInput", e.Input)
	}
	if gotInput != input {
		t.Errorf("Input should pass through by reference; got %p want %p", gotInput, input)
	}
}

// A by-key write whose key matches no row writes nothing, so it publishes
// nothing, though it still returns no error.
func TestEvent_MissingKeyPublishesNothing(t *testing.T) {
	ctx := context.Background()
	const missing = int64(987654321)
	cases := []struct {
		name  string
		write func(client *models.Client) error
	}{
		{name: "soft delete", write: func(client *models.Client) error {
			_, err := client.Articles().SoftDelete(ctx, missing)
			return err
		}},
		{name: "restore", write: func(client *models.Client) error {
			_, err := client.Articles().Restore(ctx, missing)
			return err
		}},
		{name: "hard delete", write: func(client *models.Client) error {
			return client.Products().HardDelete(ctx, missing)
		}},
		{name: "soft delete many", write: func(client *models.Client) error {
			_, err := client.Articles().SoftDeleteMany(ctx, []int64{missing, missing + 1})
			return err
		}},
		{name: "restore many", write: func(client *models.Client) error {
			_, err := client.Articles().RestoreMany(ctx, []int64{missing, missing + 1})
			return err
		}},
		{name: "hard delete many", write: func(client *models.Client) error {
			return client.Products().HardDeleteMany(ctx, []int64{missing, missing + 1})
		}},
		// articles is non-strict, so this Update returns nil. An empty
		// FieldOptions skips the re-read, which would report the missing row.
		{name: "non-strict update", write: func(client *models.Client) error {
			_, err := client.Articles().Update(ctx, missing, &models.UpdateArticleInput{Title: omittable.Set("gone")},
				func(o *models.CallOptions[models.ArticleFieldOptions]) {
					o.FieldOptions = &models.ArticleFieldOptions{}
				})
			return err
		}},
		{name: "update many", write: func(client *models.Client) error {
			_, err := client.Products().UpdateMany(ctx, []models.UpdateProductItem{
				{ID: missing, Input: &models.UpdateProductInput{Name: omittable.Set("gone")}},
			})
			return err
		}},
		// products is non-strict, so this Increment returns nil.
		{name: "non-strict increment", write: func(client *models.Client) error {
			return client.Products().Increment(ctx, missing, models.IncrementInput[models.ProductIncrementColumn]{
				Column: models.ProductIncrementStock, Amount: 1,
			})
		}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			client, bus := newClientWithBus(t)
			got := subscribe(t, bus, event.SubscribeOptions{})
			if err := tt.write(client); err != nil {
				t.Fatalf("write: %v", err)
			}
			if events := got.waitStable(); len(events) != 0 {
				t.Errorf("got %d events, want 0: %+v", len(events), events)
			}
		})
	}
}

func TestEvent_RestoreMany(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t)

	a1, _ := client.Articles().Create(ctx, &models.CreateArticleInput{Title: "RM1", Author: "rm"})
	a2, _ := client.Articles().Create(ctx, &models.CreateArticleInput{Title: "RM2", Author: "rm"})
	ids := []int64{a1.ID, a2.ID}
	if _, err := client.Articles().SoftDeleteMany(ctx, ids); err != nil {
		t.Fatalf("SoftDeleteMany: %v", err)
	}
	t.Cleanup(func() { _ = client.Articles().HardDeleteMany(ctx, ids) })

	got := subscribe(t, bus, event.SubscribeOptions{Actions: []event.Action{event.Update}})

	if _, err := client.Articles().RestoreMany(ctx, ids); err != nil {
		t.Fatalf("RestoreMany: %v", err)
	}

	events := got.snapshot()
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	var pks []int64
	for _, e := range events {
		if e.Action != event.Update {
			t.Errorf("Action = %q, want Update", e.Action)
		}
		if e.Input != nil {
			t.Errorf("Input = %v (%T), want nil (the PK travels on Event.PK)", e.Input, e.Input)
		}
		pk, _ := e.PK.(int64)
		pks = append(pks, pk)
	}
	if !slices.Equal(ids, pks) {
		t.Errorf("event PKs = %v, want %v", pks, ids)
	}
}

// batchRecorder records each PublishBatch call's batch size.
type batchRecorder struct {
	mu      sync.Mutex
	batches []int
}

func (r *batchRecorder) Publish(context.Context, event.Event) error { return nil }
func (r *batchRecorder) PublishBatch(_ context.Context, events []event.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.batches = append(r.batches, len(events))
	return nil
}
func (r *batchRecorder) Close() error { return nil }
