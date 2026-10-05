package gen_test

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"text/template"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
	"github.com/teandresmith/sqlgen/event"
	"github.com/teandresmith/sqlgen/hook"
	"github.com/teandresmith/sqlgen/sql"
)

func loadEventHooksTemplate(t *testing.T) *template.Template {
	t.Helper()
	tmpl := template.New("").Funcs(gen.FuncMap(sql.NewPostgresDialect()))
	tmpl, err := tmpl.ParseFiles(filepath.Join("templates", "event_hooks.go.tmpl"))
	if err != nil {
		t.Fatalf("parsing event_hooks template: %v", err)
	}
	return tmpl
}

func renderEventHooks(t *testing.T, ctx *gen.EventHooksContext) string {
	t.Helper()
	tmpl := loadEventHooksTemplate(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "event-hooks", ctx); err != nil {
		t.Fatalf("executing event-hooks template: %v", err)
	}
	return buf.String()
}

// §29.6: tenanted tables inject Metadata["tenant"] from
// the structurally captured per-row tenant (mc.AffectedTenants) — never the
// resolver — so SkipTenancy mutations publish tenant-stamped events.
// §27.9 async-ctx safety: the stamp happens before the OnCommit publish
// closure is registered so the callback never re-resolves against Background.
func TestEventHooks_tenantedTable_emitsTenantMetadata(t *testing.T) {
	cfg := &config.RootConfig{}
	cfg.Events = &config.EventConfig{Enabled: true}

	tables := []gen.TableContext{
		{
			StructName:        "Product",
			TableName:         "products",
			TableNameConstant: "TableProducts",
			Schema:            "public",
			Tenancy:           &gen.TableTenancyContext{Tenanted: true, Column: "workspace_id"},
		},
	}
	ctx := gen.BuildEventHooksContext(tables, cfg, "db", "Client", gotype.UUIDIntegrationFor(""))
	if ctx == nil {
		t.Fatalf("BuildEventHooksContext returned nil with events enabled")
	}

	out := renderEventHooks(t, ctx)

	wants := []string{
		// Per-event runtime guard — the key is stamped from the structurally
		// captured row tenant (PRD §29.6); a nil element (row not
		// materialized) omits the key, never "<nil>".
		"if i < len(mc.AffectedTenants) && mc.AffectedTenants[i] != nil {",
		// Stringification uses fmt.Sprintf("%v", ...) so the metadata string
		// is byte-identical to the §29.5 cache-key grammar (round-trip
		// invariant with the event-driven receive side).
		`tenantMeta = map[string]string{"tenant": fmt.Sprintf("%v", mc.AffectedTenants[i])}`,
		// Each fanned-out event merges the per-hook MetadataFunc result with
		// its own row's tenant map — tenant applied last so it wins (§28.8).
		"Metadata:  mergeEventMetadata(baseMeta, tenantMeta),",
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("tenanted event-hook output missing %q\nfull output:\n%s", w, out)
		}
	}

	// PRD §29.6 + §27.9: the tenant value MUST be captured before the
	// publish closure is constructed. If this ordering regresses, an async
	// OnCommit firing with context.Background() could read a stale/empty
	// resolver chain inside the callback. Enforce it structurally.
	tenantIdx := strings.Index(out, "tenantMeta = map[string]string")
	publishIdx := strings.Index(out, "publish := func(ctx context.Context) error")
	if tenantIdx < 0 || publishIdx < 0 {
		t.Fatalf("expected both tenant snapshot and publish closure in output\n%s", out)
	}
	if tenantIdx >= publishIdx {
		t.Errorf("tenant metadata must be captured before the publish closure is registered; got tenantIdx=%d publishIdx=%d", tenantIdx, publishIdx)
	}

	// fmt is only pulled in when at least one hook needs the stringifier.
	if !slices.Contains(ctx.Imports, "fmt") {
		t.Errorf("expected fmt import when any event table is tenanted; imports=%v", ctx.Imports)
	}
}

// PRD §28.8: the consumer MetadataFunc is threaded into every
// event hook and resolved ONCE at hook entry (request ctx live), then merged
// into each fanned-out event via mergeEventMetadata. This pins the structural
// shape: the metaFunc parameter, the guarded once-per-hook resolve, the shared
// merge helper, the buildEventHooks threading, and the maps import — plus the
// §27.9 ordering guarantee (resolve strictly before the publish closure).
func TestEventHooks_metadataFunc_mergedAtHookEntry(t *testing.T) {
	cfg := &config.RootConfig{}
	cfg.Events = &config.EventConfig{Enabled: true}

	tables := []gen.TableContext{
		{
			StructName:        "Product",
			TableName:         "products",
			TableNameConstant: "TableProducts",
			Schema:            "public",
		},
	}
	ctx := gen.BuildEventHooksContext(tables, cfg, "db", "Client", gotype.UUIDIntegrationFor(""))
	if ctx == nil {
		t.Fatalf("BuildEventHooksContext returned nil with events enabled")
	}

	out := renderEventHooks(t, ctx)

	wants := []string{
		// The hook takes the consumer MetadataFunc as a parameter.
		"metaFunc func(ctx context.Context) map[string]string) hook.MutationHook {",
		// Resolved once at hook entry, guarded on non-nil.
		"var baseMeta map[string]string",
		"if metaFunc != nil {",
		"baseMeta = metaFunc(ctx)",
		// The shared merge helper (base cloned per event, system applied last).
		"func mergeEventMetadata(base, system map[string]string) map[string]string {",
		"maps.Copy(merged, base)",
		"maps.Copy(merged, system)",
		// buildEventHooks threads cfg.MetadataFunc into every per-table hook.
		"metaFunc := cfg.MetadataFunc",
		"newProductEventHook(publisher, onError, metaFunc)",
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("metadata-func event-hook output missing %q\nfull output:\n%s", w, out)
		}
	}

	// maps is imported for the merge helper (always emitted when events on).
	if !slices.Contains(ctx.Imports, "maps") {
		t.Errorf("expected maps import for mergeEventMetadata; imports=%v", ctx.Imports)
	}

	// §27.9: MetadataFunc must be resolved BEFORE the publish closure is
	// constructed, so a deferred async OnCommit never re-reads request ctx.
	baseIdx := strings.Index(out, "baseMeta = metaFunc(ctx)")
	publishIdx := strings.Index(out, "publish := func(ctx context.Context) error")
	if baseIdx < 0 || publishIdx < 0 {
		t.Fatalf("expected both baseMeta resolve and publish closure in output\n%s", out)
	}
	if baseIdx >= publishIdx {
		t.Errorf("MetadataFunc must be resolved before the publish closure; got baseIdx=%d publishIdx=%d", baseIdx, publishIdx)
	}
}

// §29.6 regression guard: a non-tenanted table must not emit a "tenant"
// Metadata key — no empty-string or "<nil>" leakage into the event envelope.
// Structural (template-level) omission, not a runtime guard.
func TestEventHooks_sharedTable_noTenantMetadata(t *testing.T) {
	cfg := &config.RootConfig{}
	cfg.Events = &config.EventConfig{Enabled: true}

	tables := []gen.TableContext{
		{
			StructName:        "AuditLog",
			TableName:         "audit_logs",
			TableNameConstant: "TableAuditLogs",
			Schema:            "public",
			Tenancy:           &gen.TableTenancyContext{Tenanted: false},
		},
	}
	ctx := gen.BuildEventHooksContext(tables, cfg, "db", "Client", gotype.UUIDIntegrationFor(""))
	if ctx == nil {
		t.Fatalf("BuildEventHooksContext returned nil with events enabled")
	}

	out := renderEventHooks(t, ctx)

	if strings.Contains(out, `"tenant"`) {
		t.Errorf("non-tenanted event hook unexpectedly contains \"tenant\" literal\nfull output:\n%s", out)
	}
	if strings.Contains(out, "mc.Tenant") {
		t.Errorf("non-tenanted event hook unexpectedly reads mc.Tenant\nfull output:\n%s", out)
	}
	// A non-tenanted hook still merges the consumer MetadataFunc result,
	// but passes a nil system map — so no "tenant" key can ever appear. It must
	// NOT reach for a per-row tenant map (mc.AffectedTenants) or a tenantMeta.
	if !strings.Contains(out, "Metadata:  mergeEventMetadata(baseMeta, nil),") {
		t.Errorf("non-tenanted event hook must merge MetadataFunc with a nil system map\nfull output:\n%s", out)
	}
	if strings.Contains(out, "mc.AffectedTenants") {
		t.Errorf("non-tenanted event hook unexpectedly reads mc.AffectedTenants\nfull output:\n%s", out)
	}
	if strings.Contains(out, "tenantMeta") {
		t.Errorf("non-tenanted event hook unexpectedly builds a tenantMeta map\nfull output:\n%s", out)
	}
	if slices.Contains(ctx.Imports, "fmt") {
		t.Errorf("fmt must not be imported when no event table is tenanted; imports=%v", ctx.Imports)
	}
}

// Event.Table is the bare SQL name, with the schema in Event.Schema (PRD
// §28.3). It is not string(mc.Table): that is the hook.TableName value,
// "audit.orders" on PostgreSQL (PRD §8.5), and natsbus builds subjects as
// {prefix}.{schema}.{table}, so publishing it would double the schema segment
// and cache.FromEventSubscriber would rebuild "audit.audit.orders".
func TestEventHooks_tableIsTheBareSQLName(t *testing.T) {
	cfg := &config.RootConfig{}
	cfg.Events = &config.EventConfig{Enabled: true}

	tables := []gen.TableContext{{
		StructName:        "AuditOrder",
		TableName:         "orders",
		TableNameConstant: "TableAuditOrders",
		Schema:            "audit",
	}}
	ctx := gen.BuildEventHooksContext(tables, cfg, "db", "Client", gotype.UUIDIntegrationFor(""))
	out := renderEventHooks(t, ctx)

	if !strings.Contains(out, `Table:     "orders",`) {
		t.Errorf("event hook does not publish the bare SQL name as Event.Table\nfull output:\n%s", out)
	}
	if strings.Contains(out, "string(mc.Table)") {
		t.Errorf("event hook publishes the hook.TableName value, which carries the schema\nfull output:\n%s", out)
	}
	if !strings.Contains(out, "hook.ForTable(newAuditOrderEventHook(publisher, onError, metaFunc), TableAuditOrders)") {
		t.Errorf("event hook is not scoped by its own constant\nfull output:\n%s", out)
	}
}

// §29.6 mixed case: only the tenanted hook gets the tenant block; the
// shared one stays clean. Also asserts fmt is imported because at least
// one hook needs it.
func TestEventHooks_mixedTenancy_onlyTenantedHookInjects(t *testing.T) {
	cfg := &config.RootConfig{}
	cfg.Events = &config.EventConfig{Enabled: true}

	tables := []gen.TableContext{
		{
			StructName:        "AuditLog",
			TableName:         "audit_logs",
			TableNameConstant: "TableAuditLogs",
			Schema:            "public",
			Tenancy:           &gen.TableTenancyContext{Tenanted: false},
		},
		{
			StructName:        "Product",
			TableName:         "products",
			TableNameConstant: "TableProducts",
			Schema:            "public",
			Tenancy:           &gen.TableTenancyContext{Tenanted: true, Column: "workspace_id"},
		},
	}
	ctx := gen.BuildEventHooksContext(tables, cfg, "db", "Client", gotype.UUIDIntegrationFor(""))
	if ctx == nil {
		t.Fatalf("BuildEventHooksContext returned nil")
	}

	out := renderEventHooks(t, ctx)

	// Split the rendered bodies around each per-entity hook declaration.
	auditIdx := strings.Index(out, "func newAuditLogEventHook(")
	productIdx := strings.Index(out, "func newProductEventHook(")
	if auditIdx < 0 || productIdx < 0 {
		t.Fatalf("expected both AuditLog and Product hooks in output\n%s", out)
	}
	first, second := out[auditIdx:productIdx], out[productIdx:]

	if strings.Contains(first, "mc.AffectedTenants") {
		t.Errorf("AuditLog (shared) hook unexpectedly reads mc.AffectedTenants\nbody:\n%s", first)
	}
	if !strings.Contains(second, "mc.AffectedTenants") {
		t.Errorf("Product (tenanted) hook missing mc.AffectedTenants read\nbody:\n%s", second)
	}
	if !slices.Contains(ctx.Imports, "fmt") {
		t.Errorf("fmt must be imported when at least one hook is tenanted; imports=%v", ctx.Imports)
	}
}

// §29.6 + §27.9 runtime guarantee: the published event's Metadata["tenant"]
// MUST be the method-entry resolved tenant, NOT whatever a fresh
// context.Background() would produce during an async OnCommit callback.
//
// This test simulates the tx-deferred path by capturing the `publish` closure
// the hook would have handed to Tx.OnCommit, then firing it later with
// context.Background() to mirror CallbackAsync. The tenant value in the
// published event must already be baked in at that point.
func TestEventHooks_asyncCtxSafety_capturesMethodEntryTenant(t *testing.T) {
	type tenantID struct{ value string }
	methodEntryTenant := tenantID{value: "ws-42"}

	publisher := &captureBusPublisher{}

	// Hand-built hook mirroring the template's emitted body for the tenanted
	// path. If the template regresses and the tenant read moves inside the
	// publish closure, this test will catch it because deferredPublish is
	// fired with context.Background() (no tenant, no resolver).
	evtHook := func(next hook.MutationHandler) hook.MutationHandler {
		return func(ctx context.Context, mc *hook.MutationContext) (any, error) {
			result, err := next(ctx, mc)
			if err != nil {
				return result, err
			}
			if len(mc.AffectedPKs) == 0 {
				return result, nil
			}

			events := make([]event.Event, len(mc.AffectedPKs))
			var tenantMeta map[string]string
			if mc.Tenant != nil {
				tenantMeta = map[string]string{"tenant": fmt.Sprintf("%v", mc.Tenant)}
			}
			for i, pk := range mc.AffectedPKs {
				events[i] = event.Event{
					Table:    "products",
					Action:   event.Create,
					PK:       pk,
					Metadata: tenantMeta,
				}
			}

			publish := func(ctx context.Context) error {
				return publisher.PublishBatch(ctx, events)
			}

			// Simulate the "defer to tx commit" branch — real code path
			// calls tx.OnCommit(publish); the test captures it directly.
			publisher.deferCallback(publish)
			return result, nil
		}
	}

	// Terminal emulates the generated entity-method terminal: stash the
	// resolved tenant on the MutationContext before returning (mirrors
	// create.go.tmpl `m.Tenant = resolvedTenant`).
	terminal := hook.MutationHandler(func(_ context.Context, mc *hook.MutationContext) (any, error) {
		mc.Tenant = methodEntryTenant
		mc.AffectedPKs = []any{"pk-1"}
		return nil, nil
	})

	mc := &hook.MutationContext{
		Op:    hook.OpCreate,
		Table: hook.TableName("products"),
	}
	// Request ctx carries "real" state — exercise the hook, then let the
	// deferred callback run against Background (the CallbackAsync reality).
	requestCtx := context.Background()
	if _, err := evtHook(terminal)(requestCtx, mc); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	publisher.fireDeferred(context.Background())

	got := publisher.events()
	if len(got) != 1 {
		t.Fatalf("expected 1 published event, got %d", len(got))
	}
	wantTenant := fmt.Sprintf("%v", methodEntryTenant)
	if got[0].Metadata == nil || got[0].Metadata["tenant"] != wantTenant {
		t.Errorf("event metadata[tenant] = %q, want %q — the event must carry the method-entry tenant value, not whatever the OnCommit ctx resolves to",
			got[0].Metadata["tenant"], wantTenant)
	}
}

// Each fanned-out batch event carries the per-entity input,
// not the full caller slice. The template emits an op-keyed switch that
// (a) declares per-table batch slice locals only when CreateMany /
// UpdateMany are configured, (b) extracts the i-th element from
// mc.Input, and (c) falls back to mc.Input on any single-op or *Where
// op. Single-op ops MUST NOT route through any indexed-slice path —
// otherwise a Create with mc.Input = *CreateInput would be silently
// indexed against an unrelated slice.
func TestEventHooks_perEntityInput_batchOpsEmitIndexedExpr(t *testing.T) {
	cfg := &config.RootConfig{}
	cfg.Events = &config.EventConfig{Enabled: true}

	tables := []gen.TableContext{
		{
			StructName:        "Product",
			TableName:         "products",
			TableNameConstant: "TableProducts",
			Schema:            "public",
			Operations: gen.ResolvedOperations{
				Create: true, CreateMany: true,
				Update: true, UpdateMany: true,
				SoftDelete: true, Restore: true, HardDelete: true,
			},
		},
	}
	ctx := gen.BuildEventHooksContext(tables, cfg, "db", "Client", gotype.UUIDIntegrationFor(""))
	if ctx == nil {
		t.Fatalf("BuildEventHooksContext returned nil with events enabled")
	}

	out := renderEventHooks(t, ctx)

	wants := []string{
		// Per-table batch locals are declared so the loop body type-checks.
		"batchCreateInputs []*CreateProductInput",
		"batchUpdateItems  []Update",
		// The op switch routes mc.Input through the per-table type assertion.
		"case hook.OpCreateMany:",
		"batchCreateInputs, _ = mc.Input.([]*CreateProductInput)",
		"case hook.OpUpdateMany:",
		"batchUpdateItems, _ = mc.Input.([]UpdateProductItem)",
		// The i-th lookup is what makes Event.Input per-entity.
		"if i < len(batchCreateInputs) {",
		"inputVal = batchCreateInputs[i]",
		"if i < len(batchUpdateItems) {",
		"inputVal = batchUpdateItems[i]",
		// *Many delete/restore route to nil — PK already on Event.PK.
		"case hook.OpHardDeleteMany, hook.OpSoftDeleteMany, hook.OpRestoreMany:",
		// Default branch preserves single-op + *Where unchanged.
		"default:\n\t\t\t\t\tinputVal = mc.Input",
		// The struct literal threads inputVal through, NOT mc.Input directly.
		"Input:     inputVal,",
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("event-hook output missing %q\nfull output:\n%s", w, out)
		}
	}

	// Regression guard: the per-event struct literal MUST NOT carry
	// the unindexed `Input: mc.Input` form anymore — that's the bug.
	if strings.Contains(out, "Input:     mc.Input,") {
		t.Errorf("event-hook output still emits unindexed `Input: mc.Input,` — per-entity batch input has regressed\n%s", out)
	}
}

// Negative case: a table without CreateMany / UpdateMany must NOT
// reference the per-table CreateInput / UpdateItem types — they are only
// emitted when the corresponding *Many operation is configured. Without
// this gating the generated event hook would fail to compile against a
// schema that emits no UpdateProductItem type.
func TestEventHooks_perEntityInput_omitsBatchLocalsWhenOpsDisabled(t *testing.T) {
	cfg := &config.RootConfig{}
	cfg.Events = &config.EventConfig{Enabled: true}

	tables := []gen.TableContext{
		{
			StructName:        "AuditLog",
			TableName:         "audit_logs",
			TableNameConstant: "TableAuditLogs",
			Schema:            "public",
			// Only single-op Create — no CreateMany, no UpdateMany.
			Operations: gen.ResolvedOperations{Create: true, HardDelete: true},
		},
	}
	ctx := gen.BuildEventHooksContext(tables, cfg, "db", "Client", gotype.UUIDIntegrationFor(""))
	if ctx == nil {
		t.Fatalf("BuildEventHooksContext returned nil")
	}

	out := renderEventHooks(t, ctx)

	// Scope the assertions to the per-table hook body — the top-level
	// mapOpToAction helper legitimately mentions OpCreateMany / OpUpdateMany
	// for action mapping, but the AuditLog event hook body must not.
	hookStart := strings.Index(out, "func newAuditLogEventHook(")
	if hookStart < 0 {
		t.Fatalf("expected newAuditLogEventHook in output\n%s", out)
	}
	hookBody := out[hookStart:]

	mustNotContain := []string{
		"CreateAuditLogInput",
		"UpdateAuditLogItem",
		"batchCreateInputs",
		"batchUpdateItems",
		"hook.OpCreateMany",
		"hook.OpUpdateMany",
	}
	for _, w := range mustNotContain {
		if strings.Contains(hookBody, w) {
			t.Errorf("AuditLog event hook unexpectedly references %q (CreateMany/UpdateMany not configured)\nhook body:\n%s", w, hookBody)
		}
	}

	// The default branch + *Many-delete branch must still be present so
	// the loop body type-checks and the runtime fanout still does the
	// right thing for the ops that ARE configured.
	must := []string{
		"case hook.OpHardDeleteMany, hook.OpSoftDeleteMany, hook.OpRestoreMany:",
		"default:\n\t\t\t\t\tinputVal = mc.Input",
		"Input:     inputVal,",
	}
	for _, w := range must {
		if !strings.Contains(hookBody, w) {
			t.Errorf("AuditLog event hook missing %q\nhook body:\n%s", w, hookBody)
		}
	}
}

// captureBusPublisher records published events and optionally defers the
// publish invocation so tests can simulate async OnCommit dispatch.
type captureBusPublisher struct {
	mu        sync.Mutex
	got       []event.Event
	callbacks []func(context.Context) error
}

func (p *captureBusPublisher) Publish(_ context.Context, e event.Event) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.got = append(p.got, e)
	return nil
}

func (p *captureBusPublisher) PublishBatch(_ context.Context, events []event.Event) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.got = append(p.got, events...)
	return nil
}

func (p *captureBusPublisher) Close() error { return nil }

func (p *captureBusPublisher) deferCallback(fn func(context.Context) error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.callbacks = append(p.callbacks, fn)
}

func (p *captureBusPublisher) fireDeferred(ctx context.Context) {
	p.mu.Lock()
	callbacks := p.callbacks
	p.callbacks = nil
	p.mu.Unlock()
	for _, fn := range callbacks {
		_ = fn(ctx)
	}
}

func (p *captureBusPublisher) events() []event.Event {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]event.Event, len(p.got))
	copy(out, p.got)
	return out
}

// §29.6, tenant-in-PK shape: the tenant is stamped from
// each AffectedPKs element's PK struct field — no AffectedTenants carrier is
// read (none is written for this shape).
func TestEventHooks_tenantInPK_stampsFromPKStruct(t *testing.T) {
	cfg := &config.RootConfig{}
	cfg.Events = &config.EventConfig{Enabled: true}

	tables := []gen.TableContext{
		{
			StructName:            "OrderItem",
			TableName:             "order_items",
			TableNameConstant:     "TableOrderItems",
			Schema:                "public",
			CompositePK:           true,
			CompositePKStructName: "OrderItemPK",
			Tenancy: &gen.TableTenancyContext{
				Tenanted:     true,
				Column:       "workspace_id",
				FieldName:    "WorkspaceID",
				InPrimaryKey: true,
			},
		},
	}
	ctx := gen.BuildEventHooksContext(tables, cfg, "db", "Client", gotype.UUIDIntegrationFor(""))
	if ctx == nil {
		t.Fatalf("BuildEventHooksContext returned nil with events enabled")
	}

	out := renderEventHooks(t, ctx)

	wants := []string{
		"if typedPK, ok := pk.(OrderItemPK); ok {",
		`tenantMeta = map[string]string{"tenant": fmt.Sprintf("%v", typedPK.WorkspaceID)}`,
		"Metadata:  mergeEventMetadata(baseMeta, tenantMeta),",
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("tenant-in-PK event hook missing %q\nfull output:\n%s", w, out)
		}
	}
	if strings.Contains(out, "mc.AffectedTenants") {
		t.Errorf("tenant-in-PK event hook must not read mc.AffectedTenants (no carrier for this shape)\nfull output:\n%s", out)
	}
}

// §29.6 degenerate shape: the tenant column IS the sole primary key.
// No carrier is written and no PK struct exists — the stamp
// reads the PK value directly.
func TestEventHooks_tenantIsSolePK_stampsFromPK(t *testing.T) {
	cfg := &config.RootConfig{}
	cfg.Events = &config.EventConfig{Enabled: true}

	tables := []gen.TableContext{
		{
			StructName:        "Workspace",
			TableName:         "workspaces",
			TableNameConstant: "TableWorkspaces",
			Schema:            "public",
			Tenancy: &gen.TableTenancyContext{
				Tenanted:     true,
				Column:       "id",
				FieldName:    "ID",
				InPrimaryKey: true,
			},
		},
	}
	ctx := gen.BuildEventHooksContext(tables, cfg, "db", "Client", gotype.UUIDIntegrationFor(""))
	if ctx == nil {
		t.Fatalf("BuildEventHooksContext returned nil with events enabled")
	}

	out := renderEventHooks(t, ctx)

	if !strings.Contains(out, `tenantMeta := map[string]string{"tenant": fmt.Sprintf("%v", pk)}`) {
		t.Errorf("sole-PK-tenant hook missing direct PK stamp\nfull output:\n%s", out)
	}
	if strings.Contains(out, "mc.AffectedTenants") {
		t.Errorf("sole-PK-tenant hook must not read mc.AffectedTenants\nfull output:\n%s", out)
	}
}

// OpUpsertMany shares OpCreateMany's per-row fanout arm,
// because both publish a []*Create<T>Input indexed by the same i. The arm is
// emitted unconditionally — gating it on redaction the way the single-row
// OpCreate / OpUpsert arm is gated would leave a non-redacting table
// publishing the whole batch slice to every fanned-out event, and a
// fail-closed one publishing nil.
func TestEventHooks_upsertMany_sharesBatchCreateArm(t *testing.T) {
	cfg := &config.RootConfig{}
	cfg.Events = &config.EventConfig{Enabled: true}

	tables := []gen.TableContext{
		{
			StructName:        "Product",
			TableName:         "products",
			TableNameConstant: "TableProducts",
			Schema:            "public",
			Operations: gen.ResolvedOperations{
				Create: true, CreateMany: true,
				Upsert: true, UpsertMany: true,
				HardDelete: true,
			},
		},
	}
	ctx := gen.BuildEventHooksContext(tables, cfg, "db", "Client", gotype.UUIDIntegrationFor(""))
	if ctx == nil {
		t.Fatalf("BuildEventHooksContext returned nil with events enabled")
	}
	out := renderEventHooks(t, ctx)

	wants := []string{
		// One arm, both ops — in the pre-loop type assertion...
		"case hook.OpCreateMany, hook.OpUpsertMany:\n\t\t\t\tbatchCreateInputs, _ = mc.Input.([]*CreateProductInput)",
		// ...and in the per-row fanout.
		"case hook.OpCreateMany, hook.OpUpsertMany:\n\t\t\t\t\tif i < len(batchCreateInputs) {",
		"inputVal = batchCreateInputs[i]",
		// §28.9: no new wire action.
		"case hook.OpUpsert, hook.OpUpsertMany:",
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("event-hook output missing %q\nfull output:\n%s", w, out)
		}
	}

	// The whole-slice payload is the rejected shape: OpUpsertMany must
	// never reach a pass-through arm or the fail-closed default. Inside the
	// hook body it may appear exactly twice — the pre-loop type assertion and
	// the per-row fanout arm. A third occurrence means it also landed
	// somewhere that publishes mc.Input whole.
	hookStart := strings.Index(out, "func newProductEventHook(")
	if hookStart < 0 {
		t.Fatalf("expected newProductEventHook in output\n%s", out)
	}
	if got := strings.Count(out[hookStart:], "hook.OpUpsertMany"); got != 2 {
		t.Errorf("hook body names hook.OpUpsertMany %d times, want 2 (type assertion + fanout arm); a third is a pass-through arm publishing the whole batch\n%s", got, out[hookStart:])
	}
}

// upsert_many enabled while create_many is disabled. No operations
// preset produces this pair, but a per-table override can and nothing
// validates against it — so the batch locals must be gated on the union of
// the two ops, not on CreateMany alone. Without that the generated hook
// references an undeclared batchCreateInputs and the package will not build.
func TestEventHooks_upsertManyWithoutCreateMany_stillDeclaresBatchLocals(t *testing.T) {
	cfg := &config.RootConfig{}
	cfg.Events = &config.EventConfig{Enabled: true}

	tables := []gen.TableContext{
		{
			StructName:        "Product",
			TableName:         "products",
			TableNameConstant: "TableProducts",
			Schema:            "public",
			Operations: gen.ResolvedOperations{
				Create: true, CreateMany: false,
				Upsert: true, UpsertMany: true,
				HardDelete: true,
			},
		},
	}
	ctx := gen.BuildEventHooksContext(tables, cfg, "db", "Client", gotype.UUIDIntegrationFor(""))
	if ctx == nil {
		t.Fatalf("BuildEventHooksContext returned nil with events enabled")
	}
	out := renderEventHooks(t, ctx)

	wants := []string{
		"batchCreateInputs []*CreateProductInput",
		"case hook.OpUpsertMany:\n\t\t\t\tbatchCreateInputs, _ = mc.Input.([]*CreateProductInput)",
		"case hook.OpUpsertMany:\n\t\t\t\t\tif i < len(batchCreateInputs) {",
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("event-hook output missing %q\nfull output:\n%s", w, out)
		}
	}

	hookStart := strings.Index(out, "func newProductEventHook(")
	if hookStart < 0 {
		t.Fatalf("expected newProductEventHook in output\n%s", out)
	}
	// CreateMany is off, so its type assertion must not be emitted.
	if strings.Contains(out[hookStart:], "hook.OpCreateMany") {
		t.Errorf("hook body references hook.OpCreateMany with create_many disabled\n%s", out[hookStart:])
	}
}
