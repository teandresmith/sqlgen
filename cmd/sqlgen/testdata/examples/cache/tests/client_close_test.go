package tests

import (
	"sync/atomic"
	"testing"

	"github.com/teandresmith/sqlgen/cache"
	cachememory "github.com/teandresmith/sqlgen/cache/memory"
	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"
	"github.com/teandresmith/sqlgen/event"
	"github.com/teandresmith/sqlgen/event/memorybus"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/cache/models"
)

// closeRecordingBackend is a cache.Backend that also implements io.Closer and
// records whether Close was called.
type closeRecordingBackend struct {
	cache.Backend
	closed atomic.Bool
}

func (b *closeRecordingBackend) Close() error {
	b.closed.Store(true)
	return nil
}

// closeRecordingPublisher is an event.Publisher that also implements io.Closer
// and records whether Close was called.
type closeRecordingPublisher struct {
	event.Publisher
	closed atomic.Bool
}

func (p *closeRecordingPublisher) Close() error {
	p.closed.Store(true)
	return nil
}

// Client.Close is the single shutdown point for every dependency the client
// holds: the querier, the cache, and the event publisher each get closed when
// they implement io.Closer.
func TestClientClose_ClosesCacheAndEventPublisher(t *testing.T) {
	inner, err := cachememory.New(cachememory.Options{MaxSize: 100})
	if err != nil {
		t.Fatalf("cachememory.New: %v", err)
	}
	backend := &closeRecordingBackend{Backend: inner}

	c, err := models.NewCache(backend)
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}

	bus := memorybus.New()
	t.Cleanup(func() { _ = bus.Close() })
	publisher := &closeRecordingPublisher{Publisher: bus}

	// The counting querier does not implement io.Closer, so Close leaves the
	// shared test database open for the rest of the package.
	client := models.New(newCountingQuerier(dbstdlib.New(testDB)),
		models.WithCache(c),
		models.WithEventPublisher(publisher),
	)

	if err := client.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if !backend.closed.Load() {
		t.Error("Close did not close the cache (its io.Closer backend was never closed)")
	}
	if !publisher.closed.Load() {
		t.Error("Close did not close the event publisher")
	}
}
