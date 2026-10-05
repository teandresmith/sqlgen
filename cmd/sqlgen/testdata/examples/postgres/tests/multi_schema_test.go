package tests

import (
	"context"
	"net/netip"
	"testing"
	"time"
	"uuid"

	"github.com/teandresmith/sqlgen/cache"
	cachememory "github.com/teandresmith/sqlgen/cache/memory"
	dbpgx "github.com/teandresmith/sqlgen/database/pgx"
	"github.com/teandresmith/sqlgen/event/memorybus"
	"github.com/teandresmith/sqlgen/hook"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres/models"
)

// --- Multi-schema: AuditUser CRUD ---

func TestAuditUserCRUD(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	// source_ip is the column_map fixture: an INET column retyped to netip.Addr
	// by column_map.<col>.type + .import. Round-tripping a value proves the
	// override reaches the driver, not just the struct definition.
	sourceIP := netip.MustParseAddr("203.0.113.42")
	auditUser, err := client.AuditUsers().Create(ctx, &models.CreateAuditUserInput{
		Action:   "user_created",
		Details:  omittable.Set(mustJSON(map[string]any{"user": "alice@multi.test"})),
		SourceIP: omittable.Set(&sourceIP),
	})
	if err != nil {
		t.Fatalf("Create audit user: %v", err)
	}
	if auditUser.ID == (uuid.UUID{}) {
		t.Fatal("Create audit user: expected non-empty ID")
	}
	if auditUser.Action != "user_created" {
		t.Errorf("Action = %q, want %q", auditUser.Action, "user_created")
	}

	got, err := client.AuditUsers().Get(ctx, auditUser.ID)
	if err != nil {
		t.Fatalf("Get audit user: %v", err)
	}
	if got.Action != "user_created" {
		t.Errorf("Action = %q, want %q", got.Action, "user_created")
	}
	if got.SourceIP == nil {
		t.Errorf("SourceIP = nil, want %v", sourceIP)
	} else if *got.SourceIP != sourceIP {
		t.Errorf("SourceIP = %v, want %v", *got.SourceIP, sourceIP)
	}

	updated, err := client.AuditUsers().Update(ctx, auditUser.ID, &models.UpdateAuditUserInput{
		Action: omittable.Set("user_updated"),
	})
	if err != nil {
		t.Fatalf("Update audit user: %v", err)
	}
	if updated.Action != "user_updated" {
		t.Errorf("Action = %q, want %q", updated.Action, "user_updated")
	}

	if err := client.AuditUsers().HardDelete(ctx, auditUser.ID); err != nil {
		t.Fatalf("HardDelete audit user: %v", err)
	}
}

// --- Multi-schema: Event CRUD (unique name, no prefix) ---

func TestEventCRUD(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	event, err := client.Events().Create(ctx, &models.CreateEventInput{
		Name:    "login",
		Payload: omittable.Set(mustJSON(map[string]any{"ip": "127.0.0.1"})),
	})
	if err != nil {
		t.Fatalf("Create event: %v", err)
	}
	if event.Name != "login" {
		t.Errorf("Name = %q, want %q", event.Name, "login")
	}

	got, err := client.Events().Get(ctx, event.ID)
	if err != nil {
		t.Fatalf("Get event: %v", err)
	}
	if got.Name != "login" {
		t.Errorf("Name = %q, want %q", got.Name, "login")
	}

	if err := client.Events().HardDelete(ctx, event.ID); err != nil {
		t.Fatalf("HardDelete event: %v", err)
	}
}

// --- Multi-schema: cross-contamination check ---

func TestMultiSchemaIsolation(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	publicUser, err := client.PublicUsers().Create(ctx, &models.CreatePublicUserInput{
		Name:  "IsolationTest",
		Email: "isolation@multi.test",
	})
	if err != nil {
		t.Fatalf("Create public user: %v", err)
	}

	auditUser, err := client.AuditUsers().Create(ctx, &models.CreateAuditUserInput{
		Action:  "account_created",
		Details: omittable.Set(mustJSON(map[string]any{"name": "IsolationTest"})),
	})
	if err != nil {
		t.Fatalf("Create audit user: %v", err)
	}

	gotPublic, err := client.PublicUsers().Get(ctx, publicUser.ID)
	if err != nil {
		t.Fatalf("Get public user: %v", err)
	}
	if gotPublic.Name != "IsolationTest" {
		t.Errorf("Public user Name = %q, want %q", gotPublic.Name, "IsolationTest")
	}

	gotAudit, err := client.AuditUsers().Get(ctx, auditUser.ID)
	if err != nil {
		t.Fatalf("Get audit user: %v", err)
	}
	if gotAudit.Action != "account_created" {
		t.Errorf("Audit user Action = %q, want %q", gotAudit.Action, "account_created")
	}

	_ = client.PublicUsers().HardDelete(ctx, publicUser.ID)
	_ = client.AuditUsers().HardDelete(ctx, auditUser.ID)
}

// --- Multi-schema: one SQL name, two hook.TableName values ---

// TestMultiSchema_TableNameValuesDiffer pins what every test below relies on.
// The runtime identifies a table by its hook.TableName value alone, so two
// tables that share a SQL name must not share a value (PRD §8.5).
func TestMultiSchema_TableNameValuesDiffer(t *testing.T) {
	if models.TablePublicUsers == models.TableAuditUsers {
		t.Fatalf("TablePublicUsers == TableAuditUsers (both %q): hooks and the cache cannot tell public.users from audit.users",
			models.TablePublicUsers)
	}
}

// TestMultiSchema_HooksScopedToOneSchema registers hooks scoped to
// public.users and writes a row to audit.users. While both constants were
// valued "users", hook.ForTable fired on the audit write — an audit,
// authorization or validation hook silently ran on the wrong table — and
// hook.ForMutation failed it: "type assertion failed for create users:
// expected *models.CreatePublicUserInput, got *models.CreateAuditUserInput".
func TestMultiSchema_HooksScopedToOneSchema(t *testing.T) {
	ctx := context.Background()

	var forTable, forMutation int
	scoped := hook.ForTable(func(next hook.MutationHandler) hook.MutationHandler {
		return func(ctx context.Context, m *hook.MutationContext) (any, error) {
			forTable++
			return next(ctx, m)
		}
	}, models.TablePublicUsers)
	typed := hook.OnMutation(hook.ForMutation(models.TablePublicUsers,
		func(ctx context.Context, _ *hook.MutationContext, _ *models.CreatePublicUserInput, next func(context.Context) (*models.PublicUser, error)) (*models.PublicUser, error) {
			forMutation++
			return next(ctx)
		}), hook.OpCreate)
	client := models.New(dbpgx.New(testPool), models.WithMutationHook(scoped), models.WithMutationHook(typed))

	auditUser, err := client.AuditUsers().Create(ctx, &models.CreateAuditUserInput{Action: "twoschema_hooks"})
	if err != nil {
		t.Fatalf("Create audit user through public.users-scoped hooks: %v", err)
	}
	t.Cleanup(func() { _ = newClient().AuditUsers().HardDelete(context.Background(), auditUser.ID) })
	if forTable != 0 || forMutation != 0 {
		t.Errorf("an audit.users create fired the public.users hooks: ForTable %d times, ForMutation %d times, want 0",
			forTable, forMutation)
	}

	publicUser, err := client.PublicUsers().Create(ctx, &models.CreatePublicUserInput{
		Name:  "TwoSchemaHooks",
		Email: "twoschema-hooks@multi.test",
	})
	if err != nil {
		t.Fatalf("Create public user: %v", err)
	}
	t.Cleanup(func() { _ = newClient().PublicUsers().HardDelete(context.Background(), publicUser.ID) })
	if forTable != 1 || forMutation != 1 {
		t.Errorf("a public.users create fired ForTable %d times and ForMutation %d times, want 1 each",
			forTable, forMutation)
	}
}

// newMultiSchemaCache builds a cache on a fresh in-process backend.
func newMultiSchemaCache(t *testing.T, opts ...models.CacheOption) *models.Cache {
	t.Helper()
	backend, err := cachememory.New(cachememory.Options{MaxSize: 1_000})
	if err != nil {
		t.Fatalf("memory.New: %v", err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	c, err := models.NewCache(backend, opts...)
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// waitFor polls get until it reports the wanted value or the deadline passes.
func waitFor(t *testing.T, what, want string, get func() (string, error)) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		got, err := get()
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Errorf("%s = %q, want %q — the cached entry was never invalidated", what, got, want)
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestMultiSchema_EventDrivenInvalidation is the cross-instance round trip for
// both tables. The writer has no cache and publishes; the reader's cache is
// wired only through cache.FromEventSubscriber, so every eviction it sees came
// through the bus. An event carries the bare SQL name in Table and the schema
// in Schema (PRD §28.3), so the adapter has to rebuild the constant's value
// from the two. Cast from Table alone, "users" named no cached table: both
// entries stayed stale, and the facade's error went back to the bus handler,
// not to WithOnError.
func TestMultiSchema_EventDrivenInvalidation(t *testing.T) {
	ctx := context.Background()
	bus := memorybus.New()
	t.Cleanup(func() { _ = bus.Close() })

	writer := models.New(dbpgx.New(testPool), models.WithEventPublisher(bus))
	reader := models.New(dbpgx.New(testPool), models.WithCache(
		newMultiSchemaCache(t, models.WithInvalidationSource(cache.FromEventSubscriber(bus))),
	))

	publicUser, err := writer.PublicUsers().Create(ctx, &models.CreatePublicUserInput{
		Name:  "TwoSchemaBefore",
		Email: "twoschema-events@multi.test",
	})
	if err != nil {
		t.Fatalf("Create public user: %v", err)
	}
	t.Cleanup(func() { _ = newClient().PublicUsers().HardDelete(context.Background(), publicUser.ID) })
	auditUser, err := writer.AuditUsers().Create(ctx, &models.CreateAuditUserInput{Action: "twoschema_before"})
	if err != nil {
		t.Fatalf("Create audit user: %v", err)
	}
	t.Cleanup(func() { _ = newClient().AuditUsers().HardDelete(context.Background(), auditUser.ID) })

	// Warm the reader's cache for both rows.
	if _, err := reader.PublicUsers().Get(ctx, publicUser.ID); err != nil {
		t.Fatalf("warm public user: %v", err)
	}
	if _, err := reader.AuditUsers().Get(ctx, auditUser.ID); err != nil {
		t.Fatalf("warm audit user: %v", err)
	}

	if _, err := writer.PublicUsers().Update(ctx, publicUser.ID, &models.UpdatePublicUserInput{Name: omittable.Set("TwoSchemaAfter")}); err != nil {
		t.Fatalf("Update public user: %v", err)
	}
	if _, err := writer.AuditUsers().Update(ctx, auditUser.ID, &models.UpdateAuditUserInput{Action: omittable.Set("twoschema_after")}); err != nil {
		t.Fatalf("Update audit user: %v", err)
	}

	waitFor(t, "reader's public.users name", "TwoSchemaAfter", func() (string, error) {
		u, err := reader.PublicUsers().Get(ctx, publicUser.ID)
		if err != nil {
			return "", err
		}
		return u.Name, nil
	})
	waitFor(t, "reader's audit.users action", "twoschema_after", func() (string, error) {
		u, err := reader.AuditUsers().Get(ctx, auditUser.ID)
		if err != nil {
			return "", err
		}
		return u.Action, nil
	})
}

// TestMultiSchema_InvalidateTableIsSchemaScoped clears one table's entries by
// pattern. The pattern is built from the schema and the bare SQL name; built
// from the constant's value it would be "sqlgen:public.public.users:*", which
// matches no key. It must also leave the other schema's table alone.
func TestMultiSchema_InvalidateTableIsSchemaScoped(t *testing.T) {
	ctx := context.Background()
	c := newMultiSchemaCache(t)
	reader := models.New(dbpgx.New(testPool), models.WithCache(c))

	publicUser, err := newClient().PublicUsers().Create(ctx, &models.CreatePublicUserInput{
		Name:  "TwoSchemaBefore",
		Email: "twoschema-pattern@multi.test",
	})
	if err != nil {
		t.Fatalf("Create public user: %v", err)
	}
	t.Cleanup(func() { _ = newClient().PublicUsers().HardDelete(context.Background(), publicUser.ID) })
	auditUser, err := newClient().AuditUsers().Create(ctx, &models.CreateAuditUserInput{Action: "twoschema_before"})
	if err != nil {
		t.Fatalf("Create audit user: %v", err)
	}
	t.Cleanup(func() { _ = newClient().AuditUsers().HardDelete(context.Background(), auditUser.ID) })

	if _, err := reader.PublicUsers().Get(ctx, publicUser.ID); err != nil {
		t.Fatalf("warm public user: %v", err)
	}
	if _, err := reader.AuditUsers().Get(ctx, auditUser.ID); err != nil {
		t.Fatalf("warm audit user: %v", err)
	}
	// Change both rows behind the cache's back.
	if _, err := testPool.Exec(ctx, `UPDATE public.users SET name = 'TwoSchemaAfter' WHERE id = $1`, publicUser.ID); err != nil {
		t.Fatalf("update public.users: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE audit.users SET action = 'twoschema_after' WHERE id = $1`, auditUser.ID); err != nil {
		t.Fatalf("update audit.users: %v", err)
	}

	if err := c.InvalidateTable(ctx, models.TablePublicUsers); err != nil {
		t.Fatalf("InvalidateTable(public.users): %v", err)
	}
	gotPublic, err := reader.PublicUsers().Get(ctx, publicUser.ID)
	if err != nil {
		t.Fatalf("Get public user: %v", err)
	}
	if gotPublic.Name != "TwoSchemaAfter" {
		t.Errorf("public.users name after InvalidateTable = %q, want %q — the pattern matched no key", gotPublic.Name, "TwoSchemaAfter")
	}
	gotAudit, err := reader.AuditUsers().Get(ctx, auditUser.ID)
	if err != nil {
		t.Fatalf("Get audit user: %v", err)
	}
	if gotAudit.Action != "twoschema_before" {
		t.Errorf("audit.users action after clearing public.users = %q, want the cached %q — the pattern reached the other schema",
			gotAudit.Action, "twoschema_before")
	}
}
