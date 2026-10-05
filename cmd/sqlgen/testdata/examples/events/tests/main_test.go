package tests

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"
	"github.com/teandresmith/sqlgen/event"
	"github.com/teandresmith/sqlgen/event/memorybus"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/events/models"
)

var testDB *sql.DB

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(0)
	}

	var err error
	testDB, err = sql.Open("sqlite", ":memory:")
	if err != nil {
		panic(fmt.Sprintf("opening database: %v", err))
	}
	if _, err := testDB.Exec("PRAGMA foreign_keys=ON"); err != nil {
		panic(fmt.Sprintf("enabling foreign keys: %v", err))
	}

	schema, err := os.ReadFile("../schema.sql")
	if err != nil {
		panic(fmt.Sprintf("reading schema: %v", err))
	}
	if _, err := testDB.Exec(string(schema)); err != nil {
		panic(fmt.Sprintf("applying schema: %v", err))
	}

	code := m.Run()

	testDB.Close()
	os.Exit(code)
}

// newClientWithBus returns a client wired to a fresh in-memory bus plus the bus
// itself. Outside transactions, events fire synchronously inside the mutation.
// Inside transactions, events fire from a goroutine after commit — tests should
// use collector.waitFor to await expected event counts.
func newClientWithBus(t *testing.T, opts ...models.ClientOption) (*models.Client, *memorybus.Bus) {
	t.Helper()
	bus := memorybus.New()
	allOpts := []models.ClientOption{
		models.WithEventPublisher(bus),
	}
	allOpts = append(allOpts, opts...)
	client := models.New(dbstdlib.New(testDB), allOpts...)
	t.Cleanup(func() { _ = bus.Close() })
	return client, bus
}

// collector accumulates events delivered to a subscription. Safe for
// concurrent use because async callback mode may dispatch from a goroutine.
type collector struct {
	mu     sync.Mutex
	events []event.Event
}

func (c *collector) handler(_ context.Context, e event.Event) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e)
	return nil
}

func (c *collector) snapshot() []event.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]event.Event, len(c.events))
	copy(out, c.events)
	return out
}

// waitFor blocks until the collector has at least n events or the deadline
// elapses. Returns the collected events. Use this after a transaction commit
// because OnCommit callbacks fire asynchronously in a detached goroutine.
func (c *collector) waitFor(t *testing.T, n int) []event.Event {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		if len(c.events) >= n {
			out := make([]event.Event, len(c.events))
			copy(out, c.events)
			c.mu.Unlock()
			return out
		}
		c.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	t.Fatalf("waitFor(%d): only %d events delivered within deadline", n, len(c.events))
	return nil
}

// waitStable waits briefly for any async dispatch to settle, then returns the
// collected events. Use this when the expected count is zero (e.g., rollback
// should discard events) to guard against races.
func (c *collector) waitStable() []event.Event {
	time.Sleep(100 * time.Millisecond)
	return c.snapshot()
}

// subscribe registers a collector for the given options and returns the
// collector plus an Unsubscribe func (auto-called via t.Cleanup).
func subscribe(t *testing.T, bus *memorybus.Bus, opts event.SubscribeOptions) *collector {
	t.Helper()
	c := &collector{}
	sub, err := bus.Subscribe(opts, c.handler)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })
	return c
}
