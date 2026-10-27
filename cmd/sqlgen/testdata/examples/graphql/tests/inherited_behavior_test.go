package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/99designs/gqlgen/graphql/handler"

	dbpgx "github.com/teandresmith/sqlgen/database/pgx"
	"github.com/teandresmith/sqlgen/event"
	"github.com/teandresmith/sqlgen/event/memorybus"
	"github.com/teandresmith/sqlgen/hook"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models"
	graph "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models/graph"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models/graph/sqlgenresolver"
)

// tscServe stands up the generated GraphQL stack over client — the same
// wiring TestMain uses — and returns its URL.
func tscServe(t *testing.T, client *models.Client) string {
	t.Helper()
	resolver := &graph.Resolver{
		Client: client,
		Q:      &sqlgenresolver.Q{Client: client},
		M:      &sqlgenresolver.M{Client: client},
	}
	srv := handler.NewDefaultServer(graph.NewExecutableSchema(graph.Config{Resolvers: resolver}))
	httpsrv := httptest.NewServer(sqlgenresolver.WithCallOptionsMiddleware(srv))
	t.Cleanup(httpsrv.Close)
	return httpsrv.URL
}

// tscCategoryCount counts categories rows named name, straight from the
// database.
func tscCategoryCount(t *testing.T, name string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM categories WHERE name = $1`, name).Scan(&n); err != nil {
		t.Fatalf("counting categories: %v", err)
	}
	return n
}

const tscCreateCategory = `mutation ($name: String!) { createCategory(input: { name: $name }) { id name } }`

// tscCreateCategoryID runs tscCreateCategory against url and returns the new
// row's id, failing on any GraphQL error.
func tscCreateCategoryID(t *testing.T, url, name string) int64 {
	t.Helper()
	resp := postGQL(t, url, tscCreateCategory, map[string]any{"name": name})
	if len(resp.Errors) != 0 {
		t.Fatalf("createCategory(%q) errors: %+v", name, resp.Errors)
	}
	var out struct {
		CreateCategory struct {
			ID int64 `json:"id"`
		} `json:"createCategory"`
	}
	if err := json.Unmarshal(resp.Data, &out); err != nil {
		t.Fatalf("decoding createCategory (raw=%s): %v", resp.Data, err)
	}
	return out.CreateCategory.ID
}

// TestGraphQLMutation_runsClientMutationHooks pins PRD §26: the API layer is
// built on the unified client and "inherits that client's hooks ... unchanged".
// A mutation hook registered on the client the resolvers wrap sees the
// GraphQL write as the same operation a direct client call is, and its veto
// stops the write.
func TestGraphQLMutation_runsClientMutationHooks(t *testing.T) {
	truncateAll(t)
	errVetoed := errors.New("vetoed by the client hook")
	type seen struct {
		op    hook.MutationOp
		table hook.TableName
		name  string
	}
	var (
		mu   sync.Mutex
		ops  []seen
		veto = "tsc-hook-vetoed"
	)
	h := func(next hook.MutationHandler) hook.MutationHandler {
		return func(ctx context.Context, m *hook.MutationContext) (any, error) {
			s := seen{op: m.Op, table: m.Table}
			if in, ok := m.Input.(*models.CreateCategoryInput); ok {
				s.name = in.Name
			}
			mu.Lock()
			ops = append(ops, s)
			mu.Unlock()
			if s.name == veto {
				return nil, errVetoed
			}
			return next(ctx, m)
		}
	}
	url := tscServe(t, models.New(dbpgx.New(testPool), models.WithMutationHook(h)))

	t.Run("the hook observes the GraphQL create", func(t *testing.T) {
		tscCreateCategoryID(t, url, "tsc-hook-observed")
		mu.Lock()
		defer mu.Unlock()
		want := seen{op: hook.OpCreate, table: models.TableCategories, name: "tsc-hook-observed"}
		if !slices.Contains(ops, want) {
			t.Errorf("hook saw %+v, want it to include %+v", ops, want)
		}
	})

	t.Run("a hook veto aborts the GraphQL create", func(t *testing.T) {
		resp := postGQL(t, url, tscCreateCategory, map[string]any{"name": veto})
		if len(resp.Errors) == 0 {
			t.Errorf("createCategory(%q) succeeded, want the hook's error surfaced", veto)
		}
		if n := tscCategoryCount(t, veto); n != 0 {
			t.Errorf("categories named %q = %d after a vetoed create, want 0", veto, n)
		}
	})
}

// TestGraphQLMutation_publishesClientEvents pins PRD §26: the API layer
// "inherits that client's ... event ... behavior unchanged". A GraphQL write
// through a client configured with an event publisher publishes the event a
// direct client call would.
func TestGraphQLMutation_publishesClientEvents(t *testing.T) {
	truncateAll(t)
	bus := memorybus.New()
	t.Cleanup(func() { _ = bus.Close() })
	var (
		mu     sync.Mutex
		events []event.Event
	)
	sub, err := bus.Subscribe(event.SubscribeOptions{Tables: []string{"categories"}}, func(_ context.Context, e event.Event) error {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, e)
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })
	snapshot := func() []event.Event {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(events)
	}

	url := tscServe(t, models.New(dbpgx.New(testPool), models.WithEventPublisher(bus)))

	id := tscCreateCategoryID(t, url, "tsc-events-created")
	got := snapshot()
	if len(got) != 1 {
		t.Fatalf("events after createCategory = %d, want 1: %+v", len(got), got)
	}
	if got[0].Action != event.Create || got[0].Table != "categories" {
		t.Errorf("create event = {Action:%q Table:%q}, want {create categories}", got[0].Action, got[0].Table)
	}
	if in, ok := got[0].Input.(*models.CreateCategoryInput); !ok || in.Name != "tsc-events-created" {
		t.Errorf("create event Input = %#v, want *models.CreateCategoryInput named tsc-events-created", got[0].Input)
	}

	resp := postGQL(t, url, `mutation ($id: Int!) { updateCategory(id: $id, input: { name: "tsc-events-updated" }) { id } }`, map[string]any{"id": id})
	if len(resp.Errors) != 0 {
		t.Fatalf("updateCategory errors: %+v", resp.Errors)
	}
	got = snapshot()
	if len(got) != 2 {
		t.Fatalf("events after updateCategory = %d, want 2: %+v", len(got), got)
	}
	if got[1].Action != event.Update || got[1].Table != "categories" {
		t.Errorf("update event = {Action:%q Table:%q}, want {update categories}", got[1].Action, got[1].Table)
	}
}

// TestMarshalIP_nilIsEmptyString pins the §26.4 `inet` row: "A nil IP
// marshals as `""`, never `net.IP(nil).String()`'s unparseable `"<nil>"`."
func TestMarshalIP_nilIsEmptyString(t *testing.T) {
	tests := []struct {
		name string
		ip   net.IP
		want string
	}{
		{name: "nil IP", ip: nil, want: `""`},
		{name: "empty IP", ip: net.IP{}, want: `""`},
		{name: "IPv4 address", ip: net.ParseIP("10.0.0.1"), want: `"10.0.0.1"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			graph.MarshalIP(tt.ip).MarshalGQL(&buf)
			if got := buf.String(); got != tt.want {
				t.Errorf("MarshalIP(%#v) wrote %s, want %s", tt.ip, got, tt.want)
			}
		})
	}
}
