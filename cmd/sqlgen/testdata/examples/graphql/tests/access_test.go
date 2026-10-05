package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	cachememory "github.com/teandresmith/sqlgen/cache/memory"
	"github.com/teandresmith/sqlgen/comparator"
	dbpgx "github.com/teandresmith/sqlgen/database/pgx"
	"github.com/teandresmith/sqlgen/event"
	"github.com/teandresmith/sqlgen/event/memorybus"
	"github.com/teandresmith/sqlgen/manifest"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models"
)

// PRD §32 — the cross-surface access-control contract, exercised on the
// users table (§32.6-shaped fixture): password_hash internal, new_password
// write_only, last_login_at read_only, internal_score hidden. Each leg pins
// one in-scope surface; the final leg pins the two invariants access must
// NOT touch — the core Go client and the cache.

// schemaBlock extracts one brace-delimited block from a .graphqls artifact.
func schemaBlock(t *testing.T, schema, header string) string {
	t.Helper()
	re := regexp.MustCompile(regexp.QuoteMeta(header) + `\s*\{[^}]*\}`)
	m := re.FindString(schema)
	if m == "" {
		t.Fatalf("schema block %q not found", header)
	}
	return m
}

// TestAccess_SchemaArtifacts pins the §32.3 GraphQL projection on the
// shipped artifact: each surface exposes exactly the columns its capability
// allows.
func TestAccess_SchemaArtifacts(t *testing.T) {
	raw, err := os.ReadFile("../models/graph/user_gen.graphqls")
	if err != nil {
		t.Fatalf("reading user schema artifact: %v", err)
	}
	schema := string(raw)

	tests := []struct {
		surface string
		header  string
		want    []string
		absent  []string
	}{
		{
			surface: "object type",
			header:  "type User",
			want:    []string{"email:", "lastLoginAt:"},
			absent:  []string{"passwordHash", "newPassword", "internalScore"},
		},
		{
			surface: "create input",
			header:  "input CreateUserInput",
			want:    []string{"email:", "newPassword:"},
			absent:  []string{"passwordHash", "lastLoginAt", "internalScore"},
		},
		{
			surface: "update input",
			header:  "input UpdateUserInput",
			want:    []string{"email:", "newPassword:"},
			absent:  []string{"passwordHash", "lastLoginAt", "internalScore"},
		},
		{
			surface: "filter input",
			header:  "input UserFilter",
			want:    []string{"email:", "lastLoginAt:"},
			absent:  []string{"passwordHash", "newPassword", "internalScore"},
		},
		{
			surface: "sort enum",
			header:  "enum UserSortField",
			want:    []string{"EMAIL", "LAST_LOGIN_AT"},
			absent:  []string{"PASSWORD_HASH", "NEW_PASSWORD", "INTERNAL_SCORE"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.surface, func(t *testing.T) {
			b := schemaBlock(t, schema, tt.header)
			for _, w := range tt.want {
				if !strings.Contains(b, w) {
					t.Errorf("%s missing %q:\n%s", tt.surface, w, b)
				}
			}
			for _, a := range tt.absent {
				if strings.Contains(b, a) {
					t.Errorf("%s exposes access-restricted %q:\n%s", tt.surface, a, b)
				}
			}
		})
	}
}

// TestAccess_GraphQLLive pins the live-request behavior: write_only is
// accepted on create and never readable back; restricted columns are not
// even queryable (schema validation rejects them before any resolver runs).
func TestAccess_GraphQLLive(t *testing.T) {
	create := `mutation {
		createUser(input: {email: "access-live@example.com", name: "Access Live", isActive: true, createdAt: "2026-07-13T00:00:00Z", newPassword: "s3cret-in"}) {
			id email name lastLoginAt
		}
	}`
	resp := gqlExec(t, create, nil, nil)
	if len(resp.Errors) != 0 {
		t.Fatalf("createUser with write_only newPassword errored: %+v", resp.Errors)
	}
	var created struct {
		CreateUser struct {
			ID    string `json:"id"`
			Email string `json:"email"`
		} `json:"createUser"`
	}
	if err := json.Unmarshal(resp.Data, &created); err != nil {
		t.Fatalf("decoding createUser response: %v", err)
	}

	// The write_only value reached the database through the API input.
	users, err := testClient.Users().GetMany(ctx(), &models.GetUsersInput{})
	if err != nil {
		t.Fatalf("GetMany: %v", err)
	}
	found := false
	for _, u := range users {
		if u.Email == "access-live@example.com" {
			found = true
			if u.NewPassword == nil || *u.NewPassword != "s3cret-in" {
				t.Errorf("DB NewPassword = %v, want %q (write_only input must reach the write path)", u.NewPassword, "s3cret-in")
			}
		}
	}
	if !found {
		t.Fatal("created user not found through the core client")
	}

	// Selecting a restricted column is a schema validation error — the value
	// has no field to escape through. gqlgen rejects these before any
	// resolver runs (HTTP 422 + GRAPHQL_VALIDATION_FAILED).
	for _, field := range []string{"passwordHash", "newPassword", "internalScore"} {
		q := `query { userList(limit: 1) { items { id ` + field + ` } } }`
		gqlExpectValidationError(t, q, `Cannot query field "`+field+`"`)
	}
	// Supplying a non-writable column on create is equally rejected.
	badCreate := `mutation { createUser(input: {email: "x@example.com", name: "x", isActive: true, createdAt: "2026-07-13T00:00:00Z", passwordHash: "h"}) { id } }`
	gqlExpectValidationError(t, badCreate, `"passwordHash" is not defined`)
}

// gqlExpectValidationError posts a request expected to fail schema
// validation (gqlgen answers HTTP 422 before any resolver runs) and asserts
// the error message names the rejected field.
func gqlExpectValidationError(t *testing.T, query, wantMsg string) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"query": query})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	resp, err := http.Post(testServer.URL, "application/json", bytes.NewReader(body)) //nolint:noctx // test helper against a local httptest server
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if resp.StatusCode == http.StatusOK {
		t.Errorf("query %q returned 200, want a schema validation rejection\nbody: %s", query, raw)
		return
	}
	var out gqlResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode response (raw=%s): %v", raw, err)
	}
	if len(out.Errors) == 0 || !strings.Contains(out.Errors[0].Message, wantMsg) {
		t.Errorf("validation errors = %+v, want message containing %q", out.Errors, wantMsg)
	}
}

// TestAccess_EventRedaction pins the §32.3 events leg on this example:
// Event.Input carries a redacted clone (internal + write_only cleared,
// public preserved, concrete type unchanged).
func TestAccess_EventRedaction(t *testing.T) {
	bus := memorybus.New()
	t.Cleanup(func() { _ = bus.Close() })
	client := models.New(dbpgx.New(testPool), models.WithEventPublisher(bus))

	var (
		mu     sync.Mutex
		events []event.Event
	)
	sub, err := bus.Subscribe(event.SubscribeOptions{Tables: []string{"users"}}, func(_ context.Context, e event.Event) error {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, e)
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	callerInput := &models.CreateUserInput{
		Email:        "access-events@example.com",
		Name:         "Access Events",
		PasswordHash: omittable.Set("hash-secret"),
		NewPassword:  omittable.Set(new("np-secret")),
	}
	if _, err := client.Users().Create(ctx(), callerInput); err != nil {
		t.Fatalf("Create: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 1 {
		t.Fatalf("published events = %d, want 1", len(events))
	}
	in, ok := events[0].Input.(*models.CreateUserInput)
	if !ok {
		t.Fatalf("Event.Input type = %T, want *models.CreateUserInput", events[0].Input)
	}
	if in.PasswordHash.IsSet() {
		t.Error("Event.Input.PasswordHash is set, want unset (internal → redacted)")
	}
	if in.NewPassword.IsSet() {
		t.Error("Event.Input.NewPassword is set, want unset (write_only → redacted)")
	}
	if in.Email != "access-events@example.com" || in.Name != "Access Events" {
		t.Errorf("public fields not preserved: email=%q name=%q", in.Email, in.Name)
	}
	if !callerInput.PasswordHash.IsSet() {
		t.Error("caller's input was mutated by redaction")
	}
	if events[0].PK == nil {
		t.Error("Event.PK is nil — PK must never be redacted")
	}
}

// TestAccess_ManifestMarkers pins the §32.3 manifest leg on the emitted
// per-entity JSON: access + redacted markers per role, public omitted.
func TestAccess_ManifestMarkers(t *testing.T) {
	// Single-schema example, so the entity file carries no schema prefix:
	// the stem is TableContext.SnakeName, the same field the Go file is named
	// from (PRD §30 "File naming").
	raw, err := os.ReadFile("../models/manifest/entities/user.json")
	if err != nil {
		t.Fatalf("reading user manifest entity: %v", err)
	}
	var entity manifest.Entity
	if err := json.Unmarshal(raw, &entity); err != nil {
		t.Fatalf("decoding user manifest entity: %v", err)
	}

	want := map[string]struct {
		access   string
		redacted bool
	}{
		"password_hash":  {access: "internal", redacted: true},
		"new_password":   {access: "write_only", redacted: true},
		"last_login_at":  {access: "read_only", redacted: false},
		"internal_score": {access: "hidden", redacted: false},
		"email":          {access: "", redacted: false}, // public — omitted
	}
	seen := 0
	for _, c := range entity.Columns {
		w, ok := want[c.Name]
		if !ok {
			continue
		}
		seen++
		if c.Access != w.access {
			t.Errorf("manifest column %q access = %q, want %q", c.Name, c.Access, w.access)
		}
		if c.Redacted != w.redacted {
			t.Errorf("manifest column %q redacted = %v, want %v", c.Name, c.Redacted, w.redacted)
		}
	}
	if seen != len(want) {
		t.Errorf("saw %d/%d expected columns in the manifest entity", seen, len(want))
	}
}

// TestAccess_CoreClientAndCacheFaithful pins the two §32.1 invariants access
// must NOT touch: the core Go client round-trips every column including the
// secrets, and a cache-hit Get returns the same entity a cache-miss Get does
// (no field-level cache redaction).
func TestAccess_CoreClientAndCacheFaithful(t *testing.T) {
	backend, err := cachememory.New(cachememory.Options{MaxSize: 1_000})
	if err != nil {
		t.Fatalf("memory.New: %v", err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	spy := &spyBackend{inner: backend}
	c, err := models.NewCache(spy)
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	client := models.New(dbpgx.New(testPool), models.WithCache(c))

	created, err := client.Users().Create(ctx(), &models.CreateUserInput{
		Email:         "access-cache@example.com",
		Name:          "Access Cache",
		PasswordHash:  omittable.Set("cache-hash-secret"),
		NewPassword:   omittable.Set(new("cache-np-secret")),
		InternalScore: omittable.Set(new(int64(42))),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Miss path: full read caches the entity (hydration Set).
	miss, err := client.Users().Get(ctx(), created.ID)
	if err != nil {
		t.Fatalf("Get (miss): %v", err)
	}
	spy.waitForSet(t, 1)

	// Hit path must be indistinguishable from the miss path — including the
	// access-classified secrets.
	hit, err := client.Users().Get(ctx(), created.ID)
	if err != nil {
		t.Fatalf("Get (hit): %v", err)
	}

	if miss.PasswordHash != "cache-hash-secret" {
		t.Errorf("miss PasswordHash = %q, want the stored secret", miss.PasswordHash)
	}
	if hit.PasswordHash != miss.PasswordHash {
		t.Errorf("cache hit PasswordHash = %q, miss = %q — cache must stay faithful", hit.PasswordHash, miss.PasswordHash)
	}
	if hit.NewPassword == nil || miss.NewPassword == nil || *hit.NewPassword != *miss.NewPassword {
		t.Errorf("cache hit NewPassword = %v, miss = %v — cache must stay faithful", hit.NewPassword, miss.NewPassword)
	}
	if hit.InternalScore == nil || *hit.InternalScore != 42 {
		t.Errorf("cache hit InternalScore = %v, want 42", hit.InternalScore)
	}
	if hit.Email != miss.Email || hit.Name != miss.Name {
		t.Errorf("cache hit public fields diverge from miss: %+v vs %+v", hit, miss)
	}
}

// ctx is a tiny helper for the background context used across the legs.
func ctx() context.Context { return context.Background() }

// skipEvents suppresses event publishing for one call, so a test's seed rows
// do not land in the same subscription the assertion reads.
func skipEvents[FO any](o *models.CallOptions[FO]) { o.SkipEvents = true }

// collectEvents subscribes a memorybus to one table and returns a client
// wired to it plus an accessor for what was published.
func collectEvents(t *testing.T, table string) (*models.Client, func() []event.Event) {
	t.Helper()
	bus := memorybus.New()
	t.Cleanup(func() { _ = bus.Close() })

	var (
		mu     sync.Mutex
		events []event.Event
	)
	sub, err := bus.Subscribe(event.SubscribeOptions{Tables: []string{table}}, func(_ context.Context, e event.Event) error {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, e)
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	return models.New(dbpgx.New(testPool), models.WithEventPublisher(bus)), func() []event.Event {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(events)
	}
}

// seedAccessUser creates one user carrying the fixture's access-restricted
// values and returns it with the password_hash it was created with.
// internal_score is set so a `hidden` comparator in a test filter matches —
// a NULL column would drop the row out of the predicate entirely.
func seedAccessUser(t *testing.T, client *models.Client, email string) (*models.User, string) {
	t.Helper()
	const hash = "hash-secret-for-filter-redaction"
	user, err := client.Users().Create(ctx(), &models.CreateUserInput{
		Email:         email,
		Name:          "Filter Redaction",
		PasswordHash:  omittable.Set(hash),
		InternalScore: omittable.Set(new(int64(5))),
	}, skipEvents[models.UserFieldOptions])
	if err != nil {
		t.Fatalf("Create user: %v", err)
	}
	return user, hash
}

// TestAccess_EventRedaction_WhereFilter pins the §32.3 filter leg.
// The *Where delete / restore ops publish the caller's filter as Event.Input
// (§28.6 shape table), and the Go filter carries every column — including the
// internal / write_only ones the API filter surface drops. The published clone
// must keep the concrete type and lose the restricted values, at every depth a
// filter can carry one.
func TestAccess_EventRedaction_WhereFilter(t *testing.T) {
	truncateAll(t)
	client, published := collectEvents(t, "users")
	_, hash := seedAccessUser(t, client, "filter-redaction@example.com")

	// The filter names an `internal` column and a `hidden` one. §32.2 redacts
	// the first and passes the second through, and the nested `and` member is
	// the bypass a top-level-only clone would leave open.
	score := int64(0)
	filter := &models.UserFilter{
		PasswordHash: &comparator.String{Eq: &hash},
		And: []*models.UserFilter{
			{PasswordHash: &comparator.String{Eq: &hash}},
			{InternalScore: &comparator.NullableNumber[int64]{Gte: &score}},
		},
	}
	deleted, err := client.Users().SoftDeleteWhere(ctx(), filter)
	if err != nil {
		t.Fatalf("SoftDeleteWhere: %v", err)
	}
	if len(deleted) != 1 {
		t.Fatalf("SoftDeleteWhere matched %d rows, want 1", len(deleted))
	}

	events := published()
	if len(events) != 1 {
		t.Fatalf("published events = %d, want 1", len(events))
	}

	// §28.6 shape table: still a *UserFilter, so a subscriber's type-switch is
	// unchanged and a bulk delete stays distinguishable from a single-row one.
	got, ok := events[0].Input.(*models.UserFilter)
	if !ok {
		t.Fatalf("Event.Input type = %T, want *models.UserFilter", events[0].Input)
	}
	if got.PasswordHash != nil {
		t.Errorf("Event.Input.PasswordHash = %+v, want nil (internal → redacted)", got.PasswordHash)
	}
	if len(got.And) != 2 {
		t.Fatalf("Event.Input.And length = %d, want 2", len(got.And))
	}
	if got.And[0].PasswordHash != nil {
		t.Errorf("Event.Input.And[0].PasswordHash = %+v, want nil — a nested member leaks past a shallow clone", got.And[0].PasswordHash)
	}
	// §32.2: hidden keeps a pass-through event payload.
	if got.And[1].InternalScore == nil {
		t.Error("Event.Input.And[1].InternalScore is nil, want preserved (hidden → pass-through)")
	}

	// The un-redacted filter is what went to the DB — only the published clone
	// is cleared, at every level.
	if filter.PasswordHash == nil || filter.PasswordHash.Eq == nil || *filter.PasswordHash.Eq != hash {
		t.Error("caller's filter was mutated by redaction")
	}
	if filter.And[0].PasswordHash == nil {
		t.Error("caller's nested and-member was mutated by redaction")
	}
}

// TestAccess_EventRedaction_RelationshipFilter pins the closure leg: a
// relationship member reaches another table's redacted comparators, so the
// helper must recurse through it. The path here is two hops and crosses the
// mutual recursion the fixture produces — UserFilter.Categories is a
// CategoryFilter, whose own Users member is a UserFilter again — so a redacted
// comparator sits below a table (categories) that has nothing redacted itself
// and is in the closure only by propagation.
func TestAccess_EventRedaction_RelationshipFilter(t *testing.T) {
	truncateAll(t)
	client, published := collectEvents(t, "users")
	user, hash := seedAccessUser(t, client, "relationship-redaction@example.com")

	category, err := client.Categories().Create(ctx(), &models.CreateCategoryInput{Name: "redaction-probe"},
		skipEvents[models.CategoryFieldOptions])
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	if _, err := client.UserCategories().Create(ctx(), &models.CreateUserCategoryInput{
		UserID: user.ID, CategoryID: category.ID,
	}); err != nil {
		t.Fatalf("Create user_categories: %v", err)
	}

	filter := &models.UserFilter{
		Categories: &models.CategoryFilter{
			Users: &models.UserFilter{PasswordHash: &comparator.String{Eq: &hash}},
		},
	}
	deleted, err := client.Users().SoftDeleteWhere(ctx(), filter)
	if err != nil {
		t.Fatalf("SoftDeleteWhere: %v", err)
	}
	if len(deleted) != 1 {
		t.Fatalf("SoftDeleteWhere matched %d rows, want 1", len(deleted))
	}

	events := published()
	if len(events) != 1 {
		t.Fatalf("published events = %d, want 1", len(events))
	}
	got, ok := events[0].Input.(*models.UserFilter)
	if !ok {
		t.Fatalf("Event.Input type = %T, want *models.UserFilter", events[0].Input)
	}
	if got.Categories == nil || got.Categories.Users == nil {
		t.Fatal("Event.Input lost a relationship member, want them preserved with restricted comparators cleared")
	}
	if got.Categories.Users.PasswordHash != nil {
		t.Errorf("Event.Input.Categories.Users.PasswordHash = %+v, want nil — a relationship member reaches the target's redacted columns",
			got.Categories.Users.PasswordHash)
	}
	if filter.Categories.Users.PasswordHash == nil {
		t.Error("caller's nested relationship member was mutated by redaction")
	}
}
