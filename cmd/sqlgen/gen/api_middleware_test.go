package gen_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
)

// renderAPIMiddleware renders the api/middleware template against a context
// with ModelsPackage set so the per-call CallOptions[FO] type carries the
// correct package alias.
func renderAPIMiddleware(t *testing.T, ctx *gen.APIContext) string {
	t.Helper()
	tmpl := loadAPIAllTemplates(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "api/middleware", ctx); err != nil {
		t.Fatalf("rendering api/middleware: %v", err)
	}
	return buf.String()
}

// middlewareAPIContext returns an APIContext suitable for rendering the
// middleware template. Reuses the translator fixture so the test does not
// need its own schema — middleware emission is schema-independent.
func middlewareAPIContext(t *testing.T) *gen.APIContext {
	t.Helper()
	return translatorAPIContext(t, config.DialectPostgres)
}

func TestMiddleware_CacheControlNoCache(t *testing.T) {
	// PRD §26.11 — a request with `Cache-Control: no-cache` must produce a
	// CallOptions setter that sets SkipCache = true. The codegen-level
	// assertion pins both the header read and the setter that the
	// middleware records so a future drift in either side fails fast.
	out := renderAPIMiddleware(t, middlewareAPIContext(t))

	mustContain(t, out, `r.Header.Get("Cache-Control") == "no-cache"`)
	mustContain(t, out, "opts.SkipCache = true")
	mustContain(t, out, "func(o *models.CallOptions[FO]) { o.SkipCache = true }")
}

func TestMiddleware_XSkipEvents(t *testing.T) {
	// PRD §26.11 — `X-Skip-Events: true` records SkipEvents = true. The
	// header value comparison must be the exact `"true"` string per the
	// PRD's documented contract.
	out := renderAPIMiddleware(t, middlewareAPIContext(t))

	mustContain(t, out, `r.Header.Get("X-Skip-Events") == "true"`)
	mustContain(t, out, "opts.SkipEvents = true")
	mustContain(t, out, "func(o *models.CallOptions[FO]) { o.SkipEvents = true }")
}

func TestMiddleware_XSkipHooks(t *testing.T) {
	// PRD §26.11 — `X-Skip-Hooks: true` records SkipHooks = true. The
	// implication "SkipHooks ⇒ SkipCache + SkipEvents" is enforced inside
	// resolveCallOptions on the model side (see shared_types_gen.go), so the
	// middleware only records the explicit flag and lets the runtime apply
	// the implication.
	out := renderAPIMiddleware(t, middlewareAPIContext(t))

	mustContain(t, out, `r.Header.Get("X-Skip-Hooks") == "true"`)
	mustContain(t, out, "opts.SkipHooks = true")
	mustContain(t, out, "func(o *models.CallOptions[FO]) { o.SkipHooks = true }")
}

func TestMiddleware_AllThreeHeaders(t *testing.T) {
	// PRD §26.11 — when all three headers are present the resolver must see
	// three setters in declared order (Cache-Control, X-Skip-Events,
	// X-Skip-Hooks). The codegen-level invariant is the *order* of the three
	// `if opts.SkipX { out = append(...) }` blocks inside callOptionsFromHTTP.
	out := renderAPIMiddleware(t, middlewareAPIContext(t))

	cacheIdx := strings.Index(out, "if opts.SkipCache {")
	eventsIdx := strings.Index(out, "if opts.SkipEvents {")
	hooksIdx := strings.Index(out, "if opts.SkipHooks {")
	if cacheIdx < 0 || eventsIdx < 0 || hooksIdx < 0 {
		t.Fatalf("middleware must emit all three SkipX append guards (cache=%d events=%d hooks=%d)\n%s", cacheIdx, eventsIdx, hooksIdx, out)
	}
	if cacheIdx >= eventsIdx || eventsIdx >= hooksIdx {
		t.Fatalf("middleware setter order violated (cache=%d events=%d hooks=%d) — must match PRD §26.11 header order\n%s", cacheIdx, eventsIdx, hooksIdx, out)
	}
}

func TestCallOptionsFromHTTP_RoundTrip(t *testing.T) {
	// PRD §26.11 — middleware stashes the per-request flags under a private
	// context key; callOptionsFromHTTP reads them back out and builds the
	// typed setter slice. A bare context (no middleware) must short-circuit
	// to nil so resolvers running outside the HTTP layer (e.g. unit tests)
	// see no overrides.
	out := renderAPIMiddleware(t, middlewareAPIContext(t))

	// Stash side: middleware writes httpCallOptions into the request ctx
	// under the unexported callOptionsKey type.
	mustContain(t, out, "type callOptionsKey struct{}")
	mustContain(t, out, "type httpCallOptions struct {")
	mustContain(t, out, "context.WithValue(r.Context(), callOptionsKey{}, opts)")

	// Read side: callOptionsFromHTTP is generic over FO, asserts the stored
	// value into httpCallOptions, and returns nil when the assertion fails.
	mustContain(t, out, "func callOptionsFromHTTP[FO any](ctx context.Context) []func(*models.CallOptions[FO]) {")
	mustContain(t, out, "ctx.Value(callOptionsKey{}).(httpCallOptions)")
	mustContain(t, out, "return nil")

	// The middleware wraps the next handler — required for chaining inside
	// the gqlgen server bootstrap.
	mustContain(t, out, "func WithCallOptionsMiddleware(next http.Handler) http.Handler {")
	mustContain(t, out, "next.ServeHTTP(w, r.WithContext(ctx))")
}
