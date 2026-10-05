// Package event provides types and interfaces for the SQLGen event system.
// It defines the Event structure, Publisher/Subscriber interfaces, and
// configuration types used by generated event hooks. Transport implementations
// (Redis, NATS, Kafka, etc.) are provided by consumers or sub-packages.
package event

import (
	"context"
	"log"
	"time"
)

// Action describes what mutation occurred.
type Action string

// Mutation action constants.
const (
	Create Action = "create"
	Update Action = "update"
	Delete Action = "delete" // soft or hard
	Upsert Action = "upsert"
)

// Event represents a mutation lifecycle event published after a successful
// database operation. Each affected entity produces one event.
type Event struct {
	ID        string            `json:"id"`                 // unique event ID (UUID v4 or v7 per generation config)
	Table     string            `json:"table"`              // table name (e.g., "products")
	Schema    string            `json:"schema"`             // schema name (e.g., "public")
	Action    Action            `json:"action"`             // what happened
	PK        any               `json:"pk"`                 // primary key of the affected entity (simple or composite)
	Input     any               `json:"input"`              // mutation input (*CreateInput, *UpdateInput, filter, etc.)
	Timestamp time.Time         `json:"timestamp"`          // when the mutation occurred
	Metadata  map[string]string `json:"metadata,omitempty"` // user-defined metadata from context
}

// Config holds runtime configuration for event publishing.
type Config struct {
	// OnError is called when PublishBatch fails. Mutation always succeeds —
	// publish failure is never propagated to the caller.
	// When nil, defaults to logging to stderr via log.Printf.
	OnError func(ctx context.Context, events []Event, err error)

	// MetadataFunc derives consumer-supplied metadata (e.g. "actor",
	// "request_id", "traceparent") from the request context. The generated
	// mutation hook invokes it once at hook entry — where the request ctx is
	// still live — and merges the result into every resulting Event.Metadata
	// (section 28.8). It is never called from the deferred Tx.OnCommit
	// callback, which runs on context.Background() (sections 18.5, 27.9). The
	// system key "tenant" (section 29.6) is applied last and cannot be
	// overridden. When nil, no consumer metadata is added and event output is
	// unchanged.
	MetadataFunc func(ctx context.Context) map[string]string
}

// DefaultOnError logs publish failures to stderr.
func DefaultOnError(ctx context.Context, events []Event, err error) {
	log.Printf("event publish failed: %v (%d events)", err, len(events))
}

// ResolveOnError returns c.OnError if set, otherwise DefaultOnError.
func (c *Config) ResolveOnError() func(ctx context.Context, events []Event, err error) {
	if c.OnError != nil {
		return c.OnError
	}
	return DefaultOnError
}

// Publisher sends events to a transport backend.
type Publisher interface {
	// Publish sends a single event. Implementations may buffer, batch, or
	// send immediately.
	Publish(ctx context.Context, event Event) error

	// PublishBatch sends multiple events. Simple implementations may call
	// Publish in a loop; transports that support native batching (Redis
	// pipelines, Kafka batch produce) can optimize internally.
	PublishBatch(ctx context.Context, events []Event) error

	// Close flushes pending events and releases resources.
	Close() error
}

// Subscriber registers handlers for mutation events.
type Subscriber interface {
	// Subscribe registers a handler with the given options. Returns a
	// Subscription that can be cancelled via Unsubscribe.
	Subscribe(opts SubscribeOptions, handler Handler) (Subscription, error)

	// Close stops all subscriptions and releases resources.
	Close() error
}

// SubscribeOptions controls which events a handler receives.
type SubscribeOptions struct {
	Tables  []string // filter by table (empty = all tables)
	Actions []Action // filter by action (empty = all actions)
	Group   string   // consumer group for load balancing (empty = broadcast to all)
}

// Handler processes a single event.
type Handler func(ctx context.Context, event Event) error

// Subscription represents an active event subscription that can be cancelled.
type Subscription interface {
	Unsubscribe() error
}
