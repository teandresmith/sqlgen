package featuressuite

import (
	"context"
	"slices"
	"sync"

	"github.com/teandresmith/sqlgen/event"
)

// Recorder is an event.Publisher that records what it is handed. Outside a
// transaction the generated hook publishes before the mutation returns, so a
// test reads the Recorder right after the write.
type Recorder struct {
	mu        sync.Mutex
	batches   [][]event.Event
	published []event.Event
}

// Publish records e as a single publish.
func (r *Recorder) Publish(_ context.Context, e event.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.published = append(r.published, e)
	return nil
}

// PublishBatch records events as one batch.
func (r *Recorder) PublishBatch(_ context.Context, events []event.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.batches = append(r.batches, slices.Clone(events))
	return nil
}

// Close does nothing.
func (r *Recorder) Close() error { return nil }

// Batches returns each PublishBatch call's events, in call order.
func (r *Recorder) Batches() [][]event.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.batches)
}

// Published returns the events handed to Publish, in call order.
func (r *Recorder) Published() []event.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.published)
}

// Events returns every recorded event: the batches in call order, then the
// single publishes.
func (r *Recorder) Events() []event.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append(slices.Concat(r.batches...), r.published...)
}

// Clear forgets everything recorded so far.
func (r *Recorder) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.batches, r.published = nil, nil
}
