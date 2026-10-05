package event_test

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/event"
)

func TestActionConstants(t *testing.T) {
	tests := []struct {
		name   string
		action event.Action
		want   string
	}{
		{name: "create", action: event.Create, want: "create"},
		{name: "update", action: event.Update, want: "update"},
		{name: "delete", action: event.Delete, want: "delete"},
		{name: "upsert", action: event.Upsert, want: "upsert"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(tt.action); got != tt.want {
				t.Errorf("Action constant %q = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestEventConstruction(t *testing.T) {
	now := time.Now()
	meta := map[string]string{"user_id": "123"}

	e := event.Event{
		ID:        "evt-abc",
		Table:     "products",
		Schema:    "public",
		Action:    event.Create,
		PK:        int64(42),
		Input:     "some-input",
		Timestamp: now,
		Metadata:  meta,
	}

	if e.ID != "evt-abc" {
		t.Errorf("Event.ID = %q, want %q", e.ID, "evt-abc")
	}
	if e.Table != "products" {
		t.Errorf("Event.Table = %q, want %q", e.Table, "products")
	}
	if e.Schema != "public" {
		t.Errorf("Event.Schema = %q, want %q", e.Schema, "public")
	}
	if e.Action != event.Create {
		t.Errorf("Event.Action = %q, want %q", e.Action, event.Create)
	}
	if e.PK != int64(42) {
		t.Errorf("Event.PK = %v, want %v", e.PK, int64(42))
	}
	if e.Input != "some-input" {
		t.Errorf("Event.Input = %v, want %v", e.Input, "some-input")
	}
	if !e.Timestamp.Equal(now) {
		t.Errorf("Event.Timestamp = %v, want %v", e.Timestamp, now)
	}
	if diff := cmp.Diff(meta, e.Metadata); diff != "" {
		t.Errorf("Event.Metadata mismatch (-want +got):\n%s", diff)
	}
}

func TestEventZeroValue(t *testing.T) {
	var e event.Event

	if e.ID != "" {
		t.Errorf("zero Event.ID = %q, want empty string", e.ID)
	}
	if e.Table != "" {
		t.Errorf("zero Event.Table = %q, want empty string", e.Table)
	}
	if e.Schema != "" {
		t.Errorf("zero Event.Schema = %q, want empty string", e.Schema)
	}
	if e.Action != "" {
		t.Errorf("zero Event.Action = %q, want empty string", e.Action)
	}
	if e.PK != nil {
		t.Errorf("zero Event.PK = %v, want nil", e.PK)
	}
	if e.Input != nil {
		t.Errorf("zero Event.Input = %v, want nil", e.Input)
	}
	if !e.Timestamp.IsZero() {
		t.Errorf("zero Event.Timestamp = %v, want zero time", e.Timestamp)
	}
	if e.Metadata != nil {
		t.Errorf("zero Event.Metadata = %v, want nil", e.Metadata)
	}
}

// TestEventJSONWireFormat pins the serialized wire form of Event: stable
// snake_case keys independent of Go field names (PRD §28.3), so transports
// (natsbus, Redis, Kafka adapters) marshal to a form that survives generator
// upgrades. Metadata is omitted when empty rather than emitted as null.
func TestEventJSONWireFormat(t *testing.T) {
	t.Run("all keys are snake_case", func(t *testing.T) {
		e := event.Event{
			ID:        "evt-abc",
			Table:     "products",
			Schema:    "public",
			Action:    event.Create,
			PK:        int64(42),
			Input:     map[string]any{"name": "widget"},
			Timestamp: time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC),
			Metadata:  map[string]string{"user": "alice"},
		}

		data, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("json.Marshal(Event) unexpected error: %v", err)
		}

		var keyed map[string]json.RawMessage
		if err := json.Unmarshal(data, &keyed); err != nil {
			t.Fatalf("json.Unmarshal into map unexpected error: %v", err)
		}

		gotKeys := slices.Sorted(maps.Keys(keyed))
		wantKeys := []string{"action", "id", "input", "metadata", "pk", "schema", "table", "timestamp"}
		if diff := cmp.Diff(wantKeys, gotKeys); diff != "" {
			t.Errorf("Event wire keys mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("empty metadata is omitted", func(t *testing.T) {
		e := event.Event{
			ID:        "evt-abc",
			Table:     "products",
			Action:    event.Delete,
			Timestamp: time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC),
			// Metadata left nil.
		}

		data, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("json.Marshal(Event) unexpected error: %v", err)
		}

		var keyed map[string]json.RawMessage
		if err := json.Unmarshal(data, &keyed); err != nil {
			t.Fatalf("json.Unmarshal into map unexpected error: %v", err)
		}

		if _, present := keyed["metadata"]; present {
			t.Errorf("empty Metadata must be omitted from the wire form, got %q", data)
		}
	})
}

func TestConfigResolveOnError_Default(t *testing.T) {
	cfg := &event.Config{}

	resolved := cfg.ResolveOnError()
	if resolved == nil {
		t.Fatal("Config.ResolveOnError() returned nil, want non-nil default")
	}

	// Verify the default doesn't panic when called.
	resolved(context.Background(), []event.Event{{ID: "test"}}, nil)
}

func TestConfigResolveOnError_Custom(t *testing.T) {
	var called bool
	var gotEvents []event.Event
	var gotErr error

	custom := func(ctx context.Context, events []event.Event, err error) {
		called = true
		gotEvents = events
		gotErr = err
	}

	cfg := &event.Config{OnError: custom}
	resolved := cfg.ResolveOnError()

	testEvents := []event.Event{{ID: "evt-1"}, {ID: "evt-2"}}
	testErr := context.DeadlineExceeded
	resolved(context.Background(), testEvents, testErr)

	if !called {
		t.Error("custom OnError was not called")
	}
	if diff := cmp.Diff(testEvents, gotEvents); diff != "" {
		t.Errorf("OnError events mismatch (-want +got):\n%s", diff)
	}
	if !errors.Is(gotErr, testErr) {
		t.Errorf("OnError err = %v, want %v", gotErr, testErr)
	}
}
