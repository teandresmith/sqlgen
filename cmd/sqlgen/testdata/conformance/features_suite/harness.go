// Package featuressuite is the conformance suite for the generated client with
// events, cache and tenancy enabled (docs/design/CONFORMANCE_VARIANTS.md). Each
// Features module generates the Features schema into a package at the module
// path this suite imports, replaces that path with its own generated code, and
// calls Run with a Harness wired to its database. The tree sits under one
// subtest, Features, so a registry citation reads
// `conformance: Features/<area>/<behavior>`, and each name is a string literal.
package featuressuite

import (
	"context"
	"sync/atomic"
	"testing"

	cachememory "github.com/teandresmith/sqlgen/cache/memory"
	"github.com/teandresmith/sqlgen/database"
	"github.com/teandresmith/sqlgen/event"
	"github.com/teandresmith/sqlgen/event/memorybus"
	"github.com/teandresmith/sqlgen/hook"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/features/models"
)

// Harness is the per-dialect wiring the suite runs against.
type Harness struct {
	// Dialect is postgres, mysql or sqlite; Driver is pgx or stdlib. They label
	// failures only: no test branches on them (CONFORMANCE.md §4.4).
	Dialect string
	Driver  string
	// Database names the server and its version, e.g. "PostgreSQL 16.4". Run
	// logs it for the conformance report.
	Database string
	// Querier is the counting querier clients' statements are recorded on.
	// Statements inside a transaction run on the transaction's connection and
	// are not recorded.
	Querier *CountingQuerier
	// Base is the querier every client is built on: Querier itself, or a
	// driver-specific wrapper around it that records what it sends.
	Base database.Querier
	// Caps are the dialect's capabilities. A subtest whose expectation differs
	// by dialect derives it from these, never from Dialect.
	Caps Capabilities
	// Reset empties every Features table.
	Reset func(t *testing.T)
}

// Capabilities are the dialect differences the PRD states, by name.
type Capabilities struct {
	// RowLocks: a LockMode other than LockNone adds a row-lock clause to a
	// read inside a transaction.
	RowLocks bool
	// CaseInsensitiveText: the Features schema's text columns compare
	// case-insensitively.
	CaseInsensitiveText bool
	// KeySchema is the schema segment of a cache key: "public" on
	// PostgreSQL, empty on MySQL and SQLite (PRD §27.5).
	KeySchema string
}

// Capability names one Capabilities field.
type Capability string

// Capability names.
const (
	CapRowLocks            Capability = "RowLocks"
	CapCaseInsensitiveText Capability = "CaseInsensitiveText"
)

// Require skips t, naming the capability, when the dialect lacks it.
func (h *Harness) Require(t *testing.T, c Capability) {
	t.Helper()
	if !h.has(c) {
		t.Skipf("requires capability %s, which %s lacks", c, h.Dialect)
	}
}

// Lacks skips t, naming the capability, when the dialect has it: the subtest
// covers the dialects without it.
func (h *Harness) Lacks(t *testing.T, c Capability) {
	t.Helper()
	if h.has(c) {
		t.Skipf("covers dialects without capability %s, which %s has", c, h.Dialect)
	}
}

func (h *Harness) has(c Capability) bool {
	switch c {
	case CapRowLocks:
		return h.Caps.RowLocks
	case CapCaseInsensitiveText:
		return h.Caps.CaseInsensitiveText
	}
	panic("unknown capability " + string(c))
}

// Env is one client and the collaborators it was built with.
type Env struct {
	Client   *models.Client
	Resolver *CountingResolver
	// Cache, Backend and CacheErrors are nil unless the client was built
	// WithCache or WithCacheOptions. CacheErrors records the cache's
	// OnErrorFunc reports.
	Cache       *models.Cache
	Backend     *RecordingBackend
	CacheErrors *CacheErrors
	// Bus is nil unless the client was built WithPublisher.
	Bus *memorybus.Bus
	// Recorder is nil unless the client was built WithRecorder.
	Recorder *Recorder
}

// Option adds a collaborator to an Env's client.
type Option func(*envConfig)

type envConfig struct {
	cache, publisher, recorder bool
	cacheOpts                  []models.CacheOption
	eventPublishers            []event.Publisher
	mutationHooks              []hook.MutationHook
	clientOpts                 []models.ClientOption
}

// WithCache builds the client with a cache on a fresh RecordingBackend over a
// memory backend.
func WithCache() Option { return func(c *envConfig) { c.cache = true } }

// WithCacheOptions builds the client WithCache and passes opts to NewCache,
// after the option that records OnErrorFunc reports.
func WithCacheOptions(opts ...models.CacheOption) Option {
	return func(c *envConfig) {
		c.cache = true
		c.cacheOpts = append(c.cacheOpts, opts...)
	}
}

// WithPublisher builds the client with a fresh memorybus as its event
// publisher.
func WithPublisher() Option { return func(c *envConfig) { c.publisher = true } }

// WithRecorder builds the client with a fresh Recorder as its event
// publisher. It cannot be combined with WithPublisher.
func WithRecorder() Option { return func(c *envConfig) { c.recorder = true } }

// WithEventPublisher builds the client with p as its event publisher. It
// cannot be combined with WithPublisher or WithRecorder.
func WithEventPublisher(p event.Publisher) Option {
	return func(c *envConfig) { c.eventPublishers = append(c.eventPublishers, p) }
}

// WithClientOption adds o to the client's options, after the others.
func WithClientOption(o models.ClientOption) Option {
	return func(c *envConfig) { c.clientOpts = append(c.clientOpts, o) }
}

// WithMutationHook adds h to the client's mutation hooks, inside the event
// hook.
func WithMutationHook(h hook.MutationHook) Option {
	return func(c *envConfig) { c.mutationHooks = append(c.mutationHooks, h) }
}

// NewEnv builds a client on h.Base with a fresh CountingResolver and the
// collaborators opts name. They are closed when t ends.
func (h *Harness) NewEnv(t *testing.T, opts ...Option) *Env {
	t.Helper()
	var cfg envConfig
	for _, o := range opts {
		o(&cfg)
	}
	env := &Env{Resolver: &CountingResolver{}}
	clientOpts := []models.ClientOption{models.WithTenantResolver(env.Resolver.Resolve)}
	if cfg.cache {
		backend, err := cachememory.New(cachememory.Options{MaxSize: 10_000})
		if err != nil {
			t.Fatalf("cachememory.New() unexpected error: %v", err)
		}
		t.Cleanup(func() { _ = backend.Close() })
		env.Backend, env.CacheErrors = &RecordingBackend{Inner: backend}, &CacheErrors{}
		cacheOpts := append([]models.CacheOption{models.WithOnError(env.CacheErrors.OnError)}, cfg.cacheOpts...)
		c, err := models.NewCache(env.Backend, cacheOpts...)
		if err != nil {
			t.Fatalf("NewCache() unexpected error: %v", err)
		}
		t.Cleanup(func() { _ = c.Close() })
		env.Cache = c
		clientOpts = append(clientOpts, models.WithCache(c))
	}
	publishers := len(cfg.eventPublishers)
	for _, set := range []bool{cfg.publisher, cfg.recorder} {
		if set {
			publishers++
		}
	}
	if publishers > 1 {
		t.Fatal("NewEnv: more than one option sets the event publisher")
	}
	for _, p := range cfg.eventPublishers {
		clientOpts = append(clientOpts, models.WithEventPublisher(p))
	}
	if cfg.publisher {
		env.Bus = memorybus.New()
		t.Cleanup(func() { _ = env.Bus.Close() })
		clientOpts = append(clientOpts, models.WithEventPublisher(env.Bus))
	}
	if cfg.recorder {
		env.Recorder = &Recorder{}
		clientOpts = append(clientOpts, models.WithEventPublisher(env.Recorder))
	}
	for _, mh := range cfg.mutationHooks {
		clientOpts = append(clientOpts, models.WithMutationHook(mh))
	}
	clientOpts = append(clientOpts, cfg.clientOpts...)
	env.Client = models.New(h.Base, clientOpts...)
	return env
}

// Client is a client with no cache and no publisher.
func (h *Harness) Client(t *testing.T) *models.Client {
	t.Helper()
	return h.NewEnv(t).Client
}

type tenantKey struct{}

// WithTenant returns ctx carrying tenant, which CountingResolver resolves.
func WithTenant(ctx context.Context, tenant models.Tenant) context.Context {
	return context.WithValue(ctx, tenantKey{}, tenant)
}

// CountingResolver resolves the tenant WithTenant put on the context, or the
// zero tenant when there is none, and counts its calls.
type CountingResolver struct{ calls atomic.Int64 }

// Resolve is the client's tenancy.TenantResolver.
func (r *CountingResolver) Resolve(ctx context.Context) (models.Tenant, error) {
	r.calls.Add(1)
	tenant, _ := ctx.Value(tenantKey{}).(models.Tenant)
	return tenant, nil
}

// Calls returns how many times Resolve ran.
func (r *CountingResolver) Calls() int64 { return r.calls.Load() }

// Run runs the Features suite against h. It first checks the wiring: every
// table and the view are reachable and count zero for one tenant after Reset.
func Run(t *testing.T, h *Harness) {
	// make conformance reads this line into the report header; keep its prefix.
	t.Logf("conformance harness: database %s", h.Database)
	checkWiring(t, h)
	t.Run("Features", func(t *testing.T) {
		t.Run("Events", func(t *testing.T) { events(t, h) })
		t.Run("Tenancy", func(t *testing.T) { tenancy(t, h) })
		t.Run("Cache", func(t *testing.T) { cacheTree(t, h) })
	})
}

func checkWiring(t *testing.T, h *Harness) {
	t.Helper()
	h.Reset(t)
	c := h.Client(t)
	ctx := WithTenant(context.Background(), models.NewTenant())
	counts := []struct {
		name  string
		count func() (int64, error)
	}{
		{"notes", func() (int64, error) { return c.Notes().Count(ctx, nil) }},
		{"memberships", func() (int64, error) { return c.Memberships().Count(ctx, nil) }},
		{"tasks", func() (int64, error) { return c.Tasks().Count(ctx, nil) }},
		{"boards", func() (int64, error) { return c.Boards().Count(ctx, nil) }},
		{"cards", func() (int64, error) { return c.Cards().Count(ctx, nil) }},
		{"tags", func() (int64, error) { return c.Tags().Count(ctx, nil) }},
		{"board_tags", func() (int64, error) { return c.BoardTags().Count(ctx, nil) }},
		{"templates", func() (int64, error) { return c.Templates().Count(ctx, nil) }},
		{"template_notes", func() (int64, error) { return c.TemplateNotes().Count(ctx, nil) }},
		{"template_tags", func() (int64, error) { return c.TemplateTags().Count(ctx, nil) }},
		{"bulletins", func() (int64, error) { return c.Bulletins().Count(ctx, nil) }},
		{"regions", func() (int64, error) { return c.Regions().Count(ctx, nil) }},
		{"active_tasks", func() (int64, error) { return c.ActiveTask().Count(ctx, nil) }},
	}
	for _, tc := range counts {
		if n, err := tc.count(); err != nil || n != 0 {
			t.Fatalf("harness wiring: %s Count() = %d, %v, want 0, nil after Reset", tc.name, n, err)
		}
	}
}
