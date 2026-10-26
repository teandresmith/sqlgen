package tests

import (
	"context"
	"testing"
	"time"

	"github.com/teandresmith/sqlgen/comparator"
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

func TestEvent_CreateMany(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t)
	got := subscribe(t, bus, event.SubscribeOptions{Tables: []string{"products"}})

	inputs := []*models.CreateProductInput{
		{Name: "Batch1", SKU: "EV-CM-1", Price: 1.00},
		{Name: "Batch2", SKU: "EV-CM-2", Price: 2.00},
		{Name: "Batch3", SKU: "EV-CM-3", Price: 3.00},
	}
	created, err := client.Products().CreateMany(ctx, inputs)
	if err != nil {
		t.Fatalf("CreateMany: %v", err)
	}
	t.Cleanup(func() {
		ids := make([]int64, len(created))
		for i, p := range created {
			ids[i] = p.ID
		}
		_ = client.Products().HardDeleteMany(ctx, ids)
	})

	events := got.snapshot()
	if len(events) != len(inputs) {
		t.Fatalf("got %d events, want %d", len(events), len(inputs))
	}
	wantPKs := map[int64]bool{}
	for _, p := range created {
		wantPKs[p.ID] = true
	}
	for i, e := range events {
		if e.Action != event.Create {
			t.Errorf("event[%d].Action = %q, want Create", i, e.Action)
		}
		pk, ok := e.PK.(int64)
		if !ok || !wantPKs[pk] {
			t.Errorf("event[%d].PK = %v; not in expected set", i, e.PK)
		}
	}
}

func TestEvent_Update(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t)

	p, err := client.Products().Create(ctx, &models.CreateProductInput{
		Name: "PreUpdate", SKU: "EV-UPD-1", Price: 5.00,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, p.ID) })

	got := subscribe(t, bus, event.SubscribeOptions{Actions: []event.Action{event.Update}})

	updInput := &models.UpdateProductInput{Name: omittable.Set("PostUpdate")}
	if _, err := client.Products().Update(ctx, p.ID, updInput); err != nil {
		t.Fatalf("Update: %v", err)
	}

	events := got.snapshot()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	e := events[0]
	if e.Action != event.Update {
		t.Errorf("Action = %q, want Update", e.Action)
	}
	if e.PK != p.ID {
		t.Errorf("PK = %v, want %v", e.PK, p.ID)
	}
	if gotInput, _ := e.Input.(*models.UpdateProductInput); gotInput != updInput {
		t.Errorf("Input = %p, want %p", gotInput, updInput)
	}
}

func TestEvent_UpdateMany(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t)

	p1, _ := client.Products().Create(ctx, &models.CreateProductInput{Name: "UM1", SKU: "EV-UM-1", Price: 1})
	p2, _ := client.Products().Create(ctx, &models.CreateProductInput{Name: "UM2", SKU: "EV-UM-2", Price: 2})
	t.Cleanup(func() {
		_ = client.Products().HardDeleteMany(ctx, []int64{p1.ID, p2.ID})
	})

	got := subscribe(t, bus, event.SubscribeOptions{
		Tables:  []string{"products"},
		Actions: []event.Action{event.Update},
	})

	_, err := client.Products().UpdateMany(ctx, []models.UpdateProductItem{
		{ID: p1.ID, Input: &models.UpdateProductInput{Name: omittable.Set("UM1x")}},
		{ID: p2.ID, Input: &models.UpdateProductInput{Name: omittable.Set("UM2x")}},
	})
	if err != nil {
		t.Fatalf("UpdateMany: %v", err)
	}

	events := got.snapshot()
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	seen := map[int64]bool{}
	for _, e := range events {
		if e.Action != event.Update {
			t.Errorf("Action = %q, want Update", e.Action)
		}
		pk, _ := e.PK.(int64)
		seen[pk] = true
	}
	if !seen[p1.ID] || !seen[p2.ID] {
		t.Errorf("expected events for both PKs; seen=%v", seen)
	}
}

func TestEvent_UpdateWhere(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t)

	p1, _ := client.Products().Create(ctx, &models.CreateProductInput{Name: "UW-A", SKU: "EV-UW-1", Price: 1})
	p2, _ := client.Products().Create(ctx, &models.CreateProductInput{Name: "UW-B", SKU: "EV-UW-2", Price: 2})
	p3, _ := client.Products().Create(ctx, &models.CreateProductInput{Name: "UW-C", SKU: "EV-UW-3", Price: 3})
	t.Cleanup(func() {
		_ = client.Products().HardDeleteMany(ctx, []int64{p1.ID, p2.ID, p3.ID})
	})

	got := subscribe(t, bus, event.SubscribeOptions{
		Tables:  []string{"products"},
		Actions: []event.Action{event.Update},
	})

	_, err := client.Products().UpdateWhere(
		ctx,
		&models.ProductFilter{ID: &comparator.Number[int64]{In: []int64{p1.ID, p2.ID}}},
		&models.UpdateProductInput{Price: omittable.Set(99.99)},
	)
	if err != nil {
		t.Fatalf("UpdateWhere: %v", err)
	}

	events := got.snapshot()
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2 (affected row count)", len(events))
	}
}

func TestEvent_Upsert(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t)
	got := subscribe(t, bus, event.SubscribeOptions{Actions: []event.Action{event.Upsert}})

	inserted, err := client.Products().Upsert(
		ctx,
		&models.CreateProductInput{Name: "UpsertOne", SKU: "EV-UPS-1", Price: 10.00},
		models.ProductConflictSKU,
	)
	if err != nil {
		t.Fatalf("Upsert insert: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, inserted.ID) })

	if _, err := client.Products().Upsert(
		ctx,
		&models.CreateProductInput{Name: "UpsertTwo", SKU: "EV-UPS-1", Price: 20.00},
		models.ProductConflictSKU,
	); err != nil {
		t.Fatalf("Upsert update: %v", err)
	}

	events := got.snapshot()
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	for i, e := range events {
		if e.Action != event.Upsert {
			t.Errorf("event[%d].Action = %q, want Upsert", i, e.Action)
		}
	}
}

func TestEvent_SoftDelete(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t)

	a, err := client.Articles().Create(ctx, &models.CreateArticleInput{Title: "SDT", Author: "t"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = client.Articles().HardDelete(ctx, a.ID) })

	got := subscribe(t, bus, event.SubscribeOptions{Actions: []event.Action{event.Delete}})

	if _, err := client.Articles().SoftDelete(ctx, a.ID); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}

	events := got.snapshot()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].Action != event.Delete {
		t.Errorf("Action = %q, want Delete", events[0].Action)
	}
	if events[0].Table != "articles" {
		t.Errorf("Table = %q, want articles", events[0].Table)
	}
}

func TestEvent_HardDelete(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t)

	p, err := client.Products().Create(ctx, &models.CreateProductInput{Name: "HD", SKU: "EV-HD-1", Price: 1})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got := subscribe(t, bus, event.SubscribeOptions{Actions: []event.Action{event.Delete}})

	if err := client.Products().HardDelete(ctx, p.ID); err != nil {
		t.Fatalf("HardDelete: %v", err)
	}

	events := got.snapshot()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].Action != event.Delete {
		t.Errorf("Action = %q, want Delete", events[0].Action)
	}
	if events[0].PK != p.ID {
		t.Errorf("PK = %v, want %v", events[0].PK, p.ID)
	}
}

func TestEvent_SoftDeleteMany(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t)

	a1, _ := client.Articles().Create(ctx, &models.CreateArticleInput{Title: "SDM1", Author: "t"})
	a2, _ := client.Articles().Create(ctx, &models.CreateArticleInput{Title: "SDM2", Author: "t"})
	t.Cleanup(func() { _ = client.Articles().HardDeleteMany(ctx, []int64{a1.ID, a2.ID}) })

	got := subscribe(t, bus, event.SubscribeOptions{Actions: []event.Action{event.Delete}})

	if _, err := client.Articles().SoftDeleteMany(ctx, []int64{a1.ID, a2.ID}); err != nil {
		t.Fatalf("SoftDeleteMany: %v", err)
	}

	events := got.snapshot()
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
}

func TestEvent_HardDeleteMany(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t)

	p1, _ := client.Products().Create(ctx, &models.CreateProductInput{Name: "HDM1", SKU: "EV-HDM-1", Price: 1})
	p2, _ := client.Products().Create(ctx, &models.CreateProductInput{Name: "HDM2", SKU: "EV-HDM-2", Price: 2})

	got := subscribe(t, bus, event.SubscribeOptions{Actions: []event.Action{event.Delete}})

	if err := client.Products().HardDeleteMany(ctx, []int64{p1.ID, p2.ID}); err != nil {
		t.Fatalf("HardDeleteMany: %v", err)
	}

	events := got.snapshot()
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
}

func TestEvent_SoftDeleteWhere(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t)

	a1, _ := client.Articles().Create(ctx, &models.CreateArticleInput{Title: "SDW1", Author: "sdw"})
	a2, _ := client.Articles().Create(ctx, &models.CreateArticleInput{Title: "SDW2", Author: "sdw"})
	a3, _ := client.Articles().Create(ctx, &models.CreateArticleInput{Title: "SDW3", Author: "keep"})
	t.Cleanup(func() { _ = client.Articles().HardDeleteMany(ctx, []int64{a1.ID, a2.ID, a3.ID}) })

	got := subscribe(t, bus, event.SubscribeOptions{Actions: []event.Action{event.Delete}})

	author := "sdw"
	if _, err := client.Articles().SoftDeleteWhere(ctx, &models.ArticleFilter{
		Author: &comparator.String{Eq: &author},
	}); err != nil {
		t.Fatalf("SoftDeleteWhere: %v", err)
	}

	events := got.snapshot()
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2 (affected row count)", len(events))
	}
}

func TestEvent_HardDeleteWhere(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t)

	p1, _ := client.Products().Create(ctx, &models.CreateProductInput{Name: "HDW-A", SKU: "EV-HDW-1", Price: 1})
	p2, _ := client.Products().Create(ctx, &models.CreateProductInput{Name: "HDW-A", SKU: "EV-HDW-2", Price: 2})
	p3, _ := client.Products().Create(ctx, &models.CreateProductInput{Name: "HDW-B", SKU: "EV-HDW-3", Price: 3})
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, p3.ID) })

	got := subscribe(t, bus, event.SubscribeOptions{Actions: []event.Action{event.Delete}})

	if err := client.Products().HardDeleteWhere(ctx, &models.ProductFilter{
		ID: &comparator.Number[int64]{In: []int64{p1.ID, p2.ID}},
	}); err != nil {
		t.Fatalf("HardDeleteWhere: %v", err)
	}

	events := got.snapshot()
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2 (affected row count)", len(events))
	}
}

func TestEvent_Restore(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t)

	a, _ := client.Articles().Create(ctx, &models.CreateArticleInput{Title: "R1", Author: "r"})
	if _, err := client.Articles().SoftDelete(ctx, a.ID); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}
	t.Cleanup(func() { _ = client.Articles().HardDelete(ctx, a.ID) })

	got := subscribe(t, bus, event.SubscribeOptions{Actions: []event.Action{event.Update}})

	if _, err := client.Articles().Restore(ctx, a.ID); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	events := got.snapshot()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].Action != event.Update {
		t.Errorf("Action = %q, want Update (Restore maps to Update)", events[0].Action)
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

// A batch write listing one existing key and one missing key publishes one
// event, for the existing key: the missing key wrote nothing.
func TestEvent_BatchPublishesOnlyWrittenKeys(t *testing.T) {
	ctx := context.Background()
	const missing = int64(987654321)
	cases := []struct {
		name string
		// write creates the existing row, subscribes, and writes the batch
		// over it and missing, returning the existing row's key.
		write func(t *testing.T, client *models.Client, subscribeNow func()) int64
	}{
		{name: "soft delete many", write: func(t *testing.T, client *models.Client, subscribeNow func()) int64 {
			a, err := client.Articles().Create(ctx, &models.CreateArticleInput{Title: "BW-SD", Author: "bw"})
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			t.Cleanup(func() { _ = client.Articles().HardDelete(ctx, a.ID) })
			subscribeNow()
			if _, err := client.Articles().SoftDeleteMany(ctx, []int64{missing, a.ID}); err != nil {
				t.Fatalf("SoftDeleteMany: %v", err)
			}
			return a.ID
		}},
		{name: "restore many", write: func(t *testing.T, client *models.Client, subscribeNow func()) int64 {
			a, err := client.Articles().Create(ctx, &models.CreateArticleInput{Title: "BW-RS", Author: "bw"})
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			t.Cleanup(func() { _ = client.Articles().HardDelete(ctx, a.ID) })
			if _, err := client.Articles().SoftDelete(ctx, a.ID); err != nil {
				t.Fatalf("SoftDelete: %v", err)
			}
			subscribeNow()
			if _, err := client.Articles().RestoreMany(ctx, []int64{missing, a.ID}); err != nil {
				t.Fatalf("RestoreMany: %v", err)
			}
			return a.ID
		}},
		{name: "hard delete many", write: func(t *testing.T, client *models.Client, subscribeNow func()) int64 {
			p, err := client.Products().Create(ctx, &models.CreateProductInput{Name: "BW-HD", SKU: "EV-BW-HD", Price: 1})
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			t.Cleanup(func() { _ = client.Products().HardDelete(ctx, p.ID) })
			subscribeNow()
			if err := client.Products().HardDeleteMany(ctx, []int64{missing, p.ID}); err != nil {
				t.Fatalf("HardDeleteMany: %v", err)
			}
			return p.ID
		}},
		{name: "update many", write: func(t *testing.T, client *models.Client, subscribeNow func()) int64 {
			p, err := client.Products().Create(ctx, &models.CreateProductInput{Name: "BW-UM", SKU: "EV-BW-UM", Price: 1})
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			t.Cleanup(func() { _ = client.Products().HardDelete(ctx, p.ID) })
			subscribeNow()
			if _, err := client.Products().UpdateMany(ctx, []models.UpdateProductItem{
				{ID: missing, Input: &models.UpdateProductInput{Name: omittable.Set("gone")}},
				{ID: p.ID, Input: &models.UpdateProductInput{Name: omittable.Set("BW-UMx")}},
			}); err != nil {
				t.Fatalf("UpdateMany: %v", err)
			}
			return p.ID
		}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			client, bus := newClientWithBus(t)
			var c *collector
			existing := tt.write(t, client, func() {
				c = subscribe(t, bus, event.SubscribeOptions{})
			})
			events := c.waitStable()
			if len(events) != 1 {
				t.Fatalf("got %d events, want 1: %+v", len(events), events)
			}
			if pk, _ := events[0].PK.(int64); pk != existing {
				t.Errorf("event PK = %v, want %d", events[0].PK, existing)
			}
			// An UpdateMany event carries the item that wrote its row.
			if item, ok := events[0].Input.(models.UpdateProductItem); ok && item.ID != existing {
				t.Errorf("event Input item ID = %d, want %d", item.ID, existing)
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
	for _, e := range events {
		if e.Action != event.Update {
			t.Errorf("Action = %q, want Update", e.Action)
		}
	}
}

func TestEvent_RestoreWhere(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t)

	a1, _ := client.Articles().Create(ctx, &models.CreateArticleInput{Title: "RW1", Author: "rw"})
	a2, _ := client.Articles().Create(ctx, &models.CreateArticleInput{Title: "RW2", Author: "rw"})
	a3, _ := client.Articles().Create(ctx, &models.CreateArticleInput{Title: "RW3", Author: "other"})
	ids := []int64{a1.ID, a2.ID, a3.ID}
	if _, err := client.Articles().SoftDeleteMany(ctx, ids); err != nil {
		t.Fatalf("SoftDeleteMany: %v", err)
	}
	t.Cleanup(func() { _ = client.Articles().HardDeleteMany(ctx, ids) })

	got := subscribe(t, bus, event.SubscribeOptions{Actions: []event.Action{event.Update}})

	author := "rw"
	if _, err := client.Articles().RestoreWhere(ctx, &models.ArticleFilter{
		Author: &comparator.String{Eq: &author},
	}); err != nil {
		t.Fatalf("RestoreWhere: %v", err)
	}

	events := got.snapshot()
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2 (affected row count)", len(events))
	}
}

func TestEvent_Increment(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t)

	p, err := client.Products().Create(ctx, &models.CreateProductInput{
		Name: "INC", SKU: "EV-INC-1", Price: 1.00, Stock: omittable.Set[int64](0),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, p.ID) })

	got := subscribe(t, bus, event.SubscribeOptions{Actions: []event.Action{event.Update}})

	if err := client.Products().Increment(ctx, p.ID, models.IncrementInput[models.ProductIncrementColumn]{
		Column: models.ProductIncrementStock, Amount: 3,
	}); err != nil {
		t.Fatalf("Increment: %v", err)
	}

	events := got.snapshot()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].Action != event.Update {
		t.Errorf("Action = %q, want Update (Increment maps to Update)", events[0].Action)
	}
	if events[0].PK != p.ID {
		t.Errorf("PK = %v, want %v", events[0].PK, p.ID)
	}
}
