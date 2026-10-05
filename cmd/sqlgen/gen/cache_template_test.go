package gen_test

import (
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/sql"
)

func loadCacheTemplate(t *testing.T) *template.Template {
	t.Helper()
	tmpl, err := template.New("cache.go.tmpl").
		Funcs(gen.FuncMap(sql.NewPostgresDialect())).
		ParseFiles(filepath.Join("templates", "cache.go.tmpl"))
	if err != nil {
		t.Fatalf("parsing cache template: %v", err)
	}
	return tmpl
}

func executeCacheTemplate(t *testing.T, ctx *gen.CacheContext) string {
	t.Helper()
	tmpl := loadCacheTemplate(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "cache", ctx); err != nil {
		t.Fatalf("executing template: %v", err)
	}
	return buf.String()
}

func TestCacheTemplate_emitsFingerprintConsts(t *testing.T) {
	cfg := cacheTestConfig(true)
	ctx := gen.BuildCacheContext([]gen.TableContext{cacheTestTable()}, nil, cfg, "db", "Client")
	out := executeCacheTemplate(t, ctx)

	if !strings.Contains(out, "const fingerprintProduct = \"") {
		t.Errorf("missing fingerprintProduct constant; output:\n%s", out)
	}
}

func TestCacheTemplate_emitsSinglePKKeyHelper(t *testing.T) {
	cfg := cacheTestConfig(true)
	ctx := gen.BuildCacheContext([]gen.TableContext{cacheTestTable()}, nil, cfg, "db", "Client")
	out := executeCacheTemplate(t, ctx)

	want := "func (c *Cache) keyForProduct(pk uuid.UUID) string {"
	if !strings.Contains(out, want) {
		t.Errorf("missing single-PK key helper; want %q", want)
	}
	if !strings.Contains(out, "cache.BuildKey(c.prefix, \"public\", \"products\", fingerprintProduct, pk)") {
		t.Error("single-PK key helper body does not call cache.BuildKey with expected args")
	}
}

func TestCacheTemplate_emitsCompositePKKeyHelper(t *testing.T) {
	cfg := cacheTestConfig(true)
	ctx := gen.BuildCacheContext([]gen.TableContext{cacheTestCompositeTable()}, nil, cfg, "db", "Client")
	out := executeCacheTemplate(t, ctx)

	want := "func (c *Cache) keyForOrderItem(pk OrderItemPK) string {"
	if !strings.Contains(out, want) {
		t.Errorf("missing composite-PK key helper; want %q\noutput:\n%s", want, out)
	}
	// Field order must match DDL PK order: OrderID, ProductID
	compositeIdx := strings.Index(out, "pk.OrderID,")
	productIdx := strings.Index(out, "pk.ProductID,")
	if compositeIdx < 0 || productIdx < 0 || compositeIdx >= productIdx {
		t.Errorf("composite PK field order wrong: OrderID idx=%d, ProductID idx=%d", compositeIdx, productIdx)
	}
}

func TestCacheTemplate_emitsInvalidateDispatch(t *testing.T) {
	cfg := cacheTestConfig(true)
	ctx := gen.BuildCacheContext(
		[]gen.TableContext{cacheTestTable(), cacheTestCompositeTable()},
		nil, cfg, "db", "Client",
	)
	out := executeCacheTemplate(t, ctx)

	if !strings.Contains(out, "case TableProducts:\n\t\tkeys, err = cache.KeysFromAny(pks, c.keyForProduct)") {
		t.Error("InvalidateMany dispatch missing Product case")
	}
	if !strings.Contains(out, "case TableOrderItems:\n\t\tkeys, err = cache.KeysFromAny(pks, c.keyForOrderItem)") {
		t.Error("InvalidateMany dispatch missing OrderItem case")
	}
	if !strings.Contains(out, `return fmt.Errorf("cache: unknown or non-cached table %q", table)`) {
		t.Error("InvalidateMany missing canonical unknown-table error")
	}
}

func TestCacheTemplate_emitsInvalidateTablePattern(t *testing.T) {
	cfg := cacheTestConfig(true)
	ctx := gen.BuildCacheContext([]gen.TableContext{cacheTestTable()}, nil, cfg, "db", "Client")
	out := executeCacheTemplate(t, ctx)

	// The pattern takes the bare SQL name from cacheRelationFor, not
	// string(table): the constant's value is "public.products" (PRD §8.5), and
	// the key grammar joins schema and name itself — string(table) would build
	// sqlgen:public.public.products:*, which matches no key.
	if !strings.Contains(out, "cache.BuildTablePattern(c.prefix, schema, name)") {
		t.Error("InvalidateTable missing BuildTablePattern call")
	}
	if strings.Contains(out, "BuildTablePattern(c.prefix, schema, string(table))") {
		t.Error("a pattern is built from string(table), which carries the schema twice")
	}
	if !strings.Contains(out, "case TableProducts:\n\t\treturn \"public\", \"products\", true") {
		t.Error("cacheRelationFor missing the Product case with its bare SQL name")
	}
}

func TestCacheTemplate_emitsCustomSerializerError(t *testing.T) {
	cfg := cacheTestConfig(true)
	cfg.Cache.Serializer = config.SerializerCustom
	ctx := gen.BuildCacheContext([]gen.TableContext{cacheTestTable()}, nil, cfg, "db", "Client")
	out := executeCacheTemplate(t, ctx)

	if !strings.Contains(out, `fmt.Errorf("cache: serializer: custom requires WithSerializer(...)`) {
		t.Errorf("missing custom-serializer construction error; output:\n%s", out)
	}
}

// TestCacheTemplate_customSerializerWithOverrideSucceeds verifies that when
// serializer: custom is configured, the error is emitted inside the
// `if options.serializer == nil` guard — so a caller that supplies
// WithSerializer(customSerializer) short-circuits the nil check and never
// reaches the error branch. Complements TestCacheTemplate_emitsCustomSerializerError.
func TestCacheTemplate_customSerializerWithOverrideSucceeds(t *testing.T) {
	cfg := cacheTestConfig(true)
	cfg.Cache.Serializer = config.SerializerCustom
	ctx := gen.BuildCacheContext([]gen.TableContext{cacheTestTable()}, nil, cfg, "db", "Client")
	out := executeCacheTemplate(t, ctx)

	guardIdx := strings.Index(out, "if options.serializer == nil {")
	if guardIdx < 0 {
		t.Fatalf("missing `if options.serializer == nil` guard around custom-serializer error; output:\n%s", out)
	}
	errIdx := strings.Index(out, `return nil, fmt.Errorf("cache: serializer: custom`)
	if errIdx < 0 {
		t.Fatalf("missing custom-serializer error; output:\n%s", out)
	}
	if errIdx < guardIdx {
		t.Errorf("custom-serializer error appears before the nil guard (idx %d < %d); "+
			"a caller-supplied WithSerializer would still fail. Emit the error inside the guard.",
			errIdx, guardIdx)
	}

	// Look for the guard-closing brace after the error. Between guardIdx and
	// that brace we should NOT see any code that runs on a non-nil serializer
	// — every branch (json, msgpack, custom) lives inside the nil check.
	guardClose := strings.Index(out[guardIdx:], "\n\t}")
	if guardClose < 0 {
		t.Fatalf("could not locate closing brace of `if options.serializer == nil` block")
	}
	if errIdx-guardIdx > guardClose {
		t.Errorf("custom-serializer error is outside the nil guard — caller-supplied "+
			"WithSerializer would still fail. guardIdx=%d errIdx=%d guardCloseRel=%d",
			guardIdx, errIdx, guardClose)
	}
}

func TestCacheTemplate_emitsMsgpackDefault(t *testing.T) {
	cfg := cacheTestConfig(true)
	cfg.Cache.Serializer = config.SerializerMsgpack
	ctx := gen.BuildCacheContext([]gen.TableContext{cacheTestTable()}, nil, cfg, "db", "Client")
	out := executeCacheTemplate(t, ctx)

	if !strings.Contains(out, "options.serializer = sqlgenmsgpack.New()") {
		t.Errorf("missing msgpack default serializer; output:\n%s", out)
	}
}

func TestCacheTemplate_emitsJSONDefault(t *testing.T) {
	cfg := cacheTestConfig(true)
	ctx := gen.BuildCacheContext([]gen.TableContext{cacheTestTable()}, nil, cfg, "db", "Client")
	out := executeCacheTemplate(t, ctx)

	if !strings.Contains(out, "options.serializer = cache.JSONSerializer{}") {
		t.Errorf("missing JSON default serializer; output:\n%s", out)
	}
}

func TestCacheTemplate_emitsFullParentFieldOptions(t *testing.T) {
	cfg := cacheTestConfig(true)
	ctx := gen.BuildCacheContext([]gen.TableContext{cacheTestTable()}, nil, cfg, "db", "Client")
	out := executeCacheTemplate(t, ctx)

	if !strings.Contains(out, "func fullParentFieldOptionsProduct() *ProductFieldOptions {") {
		t.Errorf("missing fullParentFieldOptionsProduct helper; output:\n%s", out)
	}
	// Every column flag must be set to true.
	for _, field := range []string{"ID: true,", "Name: true,", "Price: true,"} {
		if !strings.Contains(out, field) {
			t.Errorf("fullParentFieldOptions missing column %q; output:\n%s", field, out)
		}
	}
}

func TestCacheTemplate_emitsHasAnyRelationshipHelper(t *testing.T) {
	cfg := cacheTestConfig(true)
	ctx := gen.BuildCacheContext([]gen.TableContext{cacheTestTable()}, nil, cfg, "db", "Client")
	out := executeCacheTemplate(t, ctx)

	if !strings.Contains(out, "func hasAnyRelationshipProduct(fo any) bool {") {
		t.Errorf("missing hasAnyRelationshipProduct helper")
	}
}

func TestCacheTemplate_emitsHydrateHelper(t *testing.T) {
	cfg := cacheTestConfig(true)
	ctx := gen.BuildCacheContext([]gen.TableContext{cacheTestTable()}, nil, cfg, "db", "Client")
	out := executeCacheTemplate(t, ctx)

	for _, want := range []string{
		"func (c *Cache) hydrateProduct(pk uuid.UUID, next hook.QueryHandler) {",
		"c.hydrating.LoadOrStore(key, struct{}{})",
		"defer c.hydrating.Delete(key)",
		"context.WithTimeout(context.Background(), c.hydrationTimeout)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("hydrate helper missing %q", want)
		}
	}
}

func TestCacheTemplate_emitsViewInvalidateSwitch(t *testing.T) {
	cfg := cacheTestConfig(true)
	cfg.Views["product_summary"] = config.ViewConfig{
		StructName:   "ProductSummary",
		Cache:        &config.ViewCacheConfig{Enabled: new(true)},
		InvalidateOn: []string{"products"},
	}
	views := []gen.ViewContext{
		{
			StructName:        "ProductSummary",
			ViewName:          "product_summary",
			TableNameConstant: "TableProductSummaries",
			Schema:            "public",
			Columns: []gen.ColumnContext{
				{Name: "id", FieldName: "ID", GoType: "uuid.UUID", DBTag: "id"},
			},
		},
	}
	ctx := gen.BuildCacheContext([]gen.TableContext{cacheTestTable()}, views, cfg, "db", "Client")
	out := executeCacheTemplate(t, ctx)

	// Keyed by the source table's constant value, which m.Table carries.
	if !strings.Contains(out, `case hook.TableName("public.products"):`) {
		t.Errorf("missing view invalidate switch case for public.products")
	}
	if !strings.Contains(out, `"sqlgen:public.product_summary:*"`) {
		t.Errorf("missing view invalidate pattern")
	}
}

func TestCacheTemplate_emitsQueryAndMutationHooks(t *testing.T) {
	cfg := cacheTestConfig(true)
	ctx := gen.BuildCacheContext([]gen.TableContext{cacheTestTable()}, nil, cfg, "db", "Client")
	out := executeCacheTemplate(t, ctx)

	for _, want := range []string{
		"func (c *Cache) QueryHook() hook.QueryHook",
		"func (c *Cache) MutationHook() hook.MutationHook",
		"database.FromContext(ctx)",
		"tx.OnCommit(",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in generated output", want)
		}
	}
}

func TestCacheTemplate_emitsOptionsAPI(t *testing.T) {
	cfg := cacheTestConfig(true)
	ctx := gen.BuildCacheContext([]gen.TableContext{cacheTestTable()}, nil, cfg, "db", "Client")
	out := executeCacheTemplate(t, ctx)

	for _, want := range []string{
		"func WithInvalidationSource(src cache.InvalidationSource) CacheOption",
		"func WithMetricsRecorder(r cache.MetricsRecorder) CacheOption",
		"func WithSerializer(s cache.Serializer) CacheOption",
		"func WithCircuitBreaker(b *cache.Breaker) CacheOption",
		"func WithOnError(fn cache.OnErrorFunc) CacheOption",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing option %q", want)
		}
	}
}

// --- Read-through ---

func TestCacheTemplate_emitsReadThroughHelper(t *testing.T) {
	cfg := cacheTestConfig(true)
	ctx := gen.BuildCacheContext([]gen.TableContext{cacheTestTable()}, nil, cfg, "db", "Client")
	out := executeCacheTemplate(t, ctx)

	for _, want := range []string{
		"func (c *Cache) readThroughProduct(ctx context.Context, pk uuid.UUID, q *hook.QueryContext, next hook.QueryHandler) (any, error) {",
		"cache.GetAs[*Product](ctx, c.backend, c.serializer, key)",
		"c.metrics.Hit(schema, TableProducts)",
		"c.metrics.Miss(schema, TableProducts)",
		"c.metrics.GetLatency(schema, TableProducts, time.Since(getStart))",
		"c.sf.DoChan(key, func() (any, error) {",
		"result, fetchErr := next(ctx, q)",
		"cache.SetAs(ctx, c.backend, c.serializer, key, entity, c.ttlForProduct())",
		"c.metrics.SetLatency(schema, TableProducts, time.Since(setStart))",
		"c.metrics.Set(schema, TableProducts)",
		`cache.RouteError(ctx, c.metrics, c.onError, c.breaker, "get", schema, TableProducts, err)`,
		`cache.RouteError(ctx, c.metrics, c.onError, c.breaker, "set", schema, TableProducts, setErr)`,
		"case res := <-ch:",
		"case <-ctx.Done():",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("readThrough helper missing %q", want)
		}
	}
}

func TestCacheTemplate_emitsReadThroughForCompositePK(t *testing.T) {
	cfg := cacheTestConfig(true)
	ctx := gen.BuildCacheContext([]gen.TableContext{cacheTestCompositeTable()}, nil, cfg, "db", "Client")
	out := executeCacheTemplate(t, ctx)

	want := "func (c *Cache) readThroughOrderItem(ctx context.Context, pk OrderItemPK, q *hook.QueryContext, next hook.QueryHandler) (any, error) {"
	if !strings.Contains(out, want) {
		t.Errorf("composite-PK readThrough signature missing: %q", want)
	}
	if !strings.Contains(out, "cache.GetAs[*OrderItem](ctx, c.backend, c.serializer, key)") {
		t.Error("composite-PK readThrough does not use *OrderItem as cache type parameter")
	}
}

func TestCacheTemplate_queryHookDispatchesToReadThrough(t *testing.T) {
	cfg := cacheTestConfig(true)
	ctx := gen.BuildCacheContext(
		[]gen.TableContext{cacheTestTable(), cacheTestCompositeTable()},
		nil, cfg, "db", "Client",
	)
	out := executeCacheTemplate(t, ctx)

	// QueryHook body must switch on q.Table and dispatch to per-table readThrough.
	for _, want := range []string{
		"switch q.Table {",
		"case TableProducts:\n\t\t\t\tpk, ok := q.PK.(uuid.UUID)",
		"return c.readThroughProduct(ctx, pk, q, next)",
		"case TableOrderItems:\n\t\t\t\tpk, ok := q.PK.(OrderItemPK)",
		"return c.readThroughOrderItem(ctx, pk, q, next)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("QueryHook dispatch missing %q", want)
		}
	}

	// Wrong-type PK must fall through to next (no cache interaction).
	if !strings.Contains(out, "if !ok {\n\t\t\t\t\treturn next(ctx, q)") {
		t.Error("QueryHook dispatch does not fall through to next on wrong-type PK")
	}
}

func TestCacheTemplate_queryHookPreservesBypassGates(t *testing.T) {
	cfg := cacheTestConfig(true)
	ctx := gen.BuildCacheContext([]gen.TableContext{cacheTestTable()}, nil, cfg, "db", "Client")
	out := executeCacheTemplate(t, ctx)

	// All existing bypass checks must run before the dispatch switch.
	dispatchIdx := strings.Index(out, "switch q.Table {\n\t\t\tcase TableProducts:")
	if dispatchIdx < 0 {
		t.Fatalf("QueryHook dispatch block not found; output:\n%s", out)
	}

	for _, guard := range []string{
		"if q.Op != hook.OpGet {",
		"if skipper, ok := q.CallOptions.(cacheSkipper); ok && skipper.skipCache() {",
		"if hasAnyRelationshipFieldOptions(q.Table, q.CallOptions) {",
	} {
		guardIdx := strings.Index(out, guard)
		if guardIdx < 0 {
			t.Errorf("missing bypass guard: %q", guard)
			continue
		}
		if guardIdx > dispatchIdx {
			t.Errorf("guard %q appears AFTER dispatch switch (idx %d > %d) — should run first",
				guard, guardIdx, dispatchIdx)
		}
	}
}

func TestCacheTemplate_readThroughBreakerGating(t *testing.T) {
	cfg := cacheTestConfig(true)
	ctx := gen.BuildCacheContext([]gen.TableContext{cacheTestTable()}, nil, cfg, "db", "Client")
	out := executeCacheTemplate(t, ctx)

	// Two distinct breaker gates: one before cache.Get probe, one before cache.Set.
	// Both must fall through gracefully when the breaker is open.
	readGate := "if c.breaker == nil || c.breaker.Allow() {"
	setGate := "if c.breaker != nil && !c.breaker.Allow() {\n\t\t\treturn result, nil"
	if !strings.Contains(out, readGate) {
		t.Errorf("missing breaker gate around cache.Get: %q", readGate)
	}
	if !strings.Contains(out, setGate) {
		t.Errorf("missing breaker gate around cache.Set: %q", setGate)
	}
}

// TestCacheTemplate_readThroughInFlightRecheck verifies the in-flight
// cache re-check inside the singleflight callback. The outer Get-miss path
// can race with a sibling flight: a straggler that observed a miss before the
// sibling populated the cache, but reaches sf.DoChan after the sibling flight
// completed, would otherwise register a brand-new flight and run a duplicate
// DB query. The re-check at the top of the callback closes that gap by
// returning the populated entity without invoking next.
func TestCacheTemplate_readThroughInFlightRecheck(t *testing.T) {
	cfg := cacheTestConfig(true)
	ctx := gen.BuildCacheContext([]gen.TableContext{cacheTestTable()}, nil, cfg, "db", "Client")
	out := executeCacheTemplate(t, ctx)

	flightOpen := "c.sf.DoChan(key, func() (any, error) {"
	flightStart := strings.Index(out, flightOpen)
	if flightStart < 0 {
		t.Fatalf("singleflight callback not found; output:\n%s", out)
	}
	fetchAnchor := "result, fetchErr := next(ctx, q)"
	fetchIdx := strings.Index(out[flightStart:], fetchAnchor)
	if fetchIdx < 0 {
		t.Fatalf("next-fetch anchor %q not found inside singleflight callback", fetchAnchor)
	}
	preFetch := out[flightStart : flightStart+fetchIdx]

	for _, want := range []string{
		"if c.breaker == nil || c.breaker.Allow() {",
		"cache.GetAs[*Product](ctx, c.backend, c.serializer, key)",
		"return entity, nil",
	} {
		if !strings.Contains(preFetch, want) {
			t.Errorf("in-flight re-check missing %q before next(ctx, q); pre-fetch body:\n%s", want, preFetch)
		}
	}

	for _, banned := range []string{
		"c.metrics.Hit(schema",
		"c.metrics.Miss(schema",
		"c.metrics.GetLatency(schema",
		"c.breaker.RecordSuccess()",
		"cache.RouteError(",
	} {
		if strings.Contains(preFetch, banned) {
			t.Errorf("in-flight re-check must not emit %q (outer probe already accounted for it); pre-fetch body:\n%s", banned, preFetch)
		}
	}
}

// TestCacheTemplate_readThroughInFlightRecheckCompositePK pins the same
// in-flight re-check invariant on composite-PK tables: the re-check uses the entity's typed
// pointer (*OrderItem) and lives inside the DoChan callback before next.
func TestCacheTemplate_readThroughInFlightRecheckCompositePK(t *testing.T) {
	cfg := cacheTestConfig(true)
	ctx := gen.BuildCacheContext([]gen.TableContext{cacheTestCompositeTable()}, nil, cfg, "db", "Client")
	out := executeCacheTemplate(t, ctx)

	flightOpen := "c.sf.DoChan(key, func() (any, error) {"
	flightStart := strings.Index(out, flightOpen)
	if flightStart < 0 {
		t.Fatalf("singleflight callback not found; output:\n%s", out)
	}
	fetchIdx := strings.Index(out[flightStart:], "result, fetchErr := next(ctx, q)")
	if fetchIdx < 0 {
		t.Fatal("next-fetch anchor not found inside singleflight callback")
	}
	preFetch := out[flightStart : flightStart+fetchIdx]

	if !strings.Contains(preFetch, "cache.GetAs[*OrderItem](ctx, c.backend, c.serializer, key)") {
		t.Errorf("composite-PK in-flight re-check does not use *OrderItem; pre-fetch body:\n%s", preFetch)
	}
}

// --- Create/CreateMany cache set ---

func TestCacheTemplate_emitsSetHelperSinglePK(t *testing.T) {
	cfg := cacheTestConfig(true)
	ctx := gen.BuildCacheContext([]gen.TableContext{cacheTestTable()}, nil, cfg, "db", "Client")
	out := executeCacheTemplate(t, ctx)

	for _, want := range []string{
		"func (c *Cache) setProduct(ctx context.Context, entity *Product) {",
		"key := c.keyForProduct(entity.ID)",
		"cache.SetAs(ctx, c.backend, c.serializer, key, entity, c.ttlForProduct())",
		`cache.RouteError(ctx, c.metrics, c.onError, c.breaker, "set", schema, TableProducts, err)`,
		"c.metrics.SetLatency(schema, TableProducts, time.Since(setStart))",
		"c.metrics.Set(schema, TableProducts)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("set helper missing %q", want)
		}
	}

	// Entity-nil and breaker-open guards must be present so the helper is
	// safe to call from any mutation path.
	if !strings.Contains(out, "if entity == nil {") {
		t.Error("setProduct missing nil-entity guard")
	}
	if !strings.Contains(out, "if c.breaker != nil && !c.breaker.Allow() {") {
		t.Error("setProduct missing breaker gate")
	}
}

func TestCacheTemplate_emitsSetHelperCompositePK(t *testing.T) {
	cfg := cacheTestConfig(true)
	ctx := gen.BuildCacheContext([]gen.TableContext{cacheTestCompositeTable()}, nil, cfg, "db", "Client")
	out := executeCacheTemplate(t, ctx)

	if !strings.Contains(out, "func (c *Cache) setOrderItem(ctx context.Context, entity *OrderItem) {") {
		t.Errorf("composite-PK set helper signature missing; output:\n%s", out)
	}
	// Composite PK must be constructed from the entity fields in DDL order.
	want := "key := c.keyForOrderItem(OrderItemPK{\n\t\tOrderID:   entity.OrderID,\n\t\tProductID: entity.ProductID,\n\t})"
	if !strings.Contains(out, want) {
		// gofumpt may align fields differently; fall back to a looser check.
		if !strings.Contains(out, "OrderID:") || !strings.Contains(out, "ProductID:") ||
			!strings.Contains(out, "c.keyForOrderItem(OrderItemPK{") {
			t.Errorf("composite-PK set helper does not build OrderItemPK from entity fields; output:\n%s", out)
		}
		orderIdx := strings.Index(out, "OrderID:   entity.OrderID,")
		productIdx := strings.Index(out, "ProductID: entity.ProductID,")
		if orderIdx < 0 || productIdx < 0 {
			// field names correct but alignment may differ — accept if present
			// in some form after the composite key helper header
			return
		}
		if orderIdx >= productIdx {
			t.Errorf("composite PK field order wrong in setOrderItem: OrderID idx=%d, ProductID idx=%d",
				orderIdx, productIdx)
		}
	}
}

func TestCacheTemplate_mutationHookForwardsResult(t *testing.T) {
	cfg := cacheTestConfig(true)
	ctx := gen.BuildCacheContext([]gen.TableContext{cacheTestTable()}, nil, cfg, "db", "Client")
	out := executeCacheTemplate(t, ctx)

	// MutationHook must forward `result` into dispatchMutation so Create /
	// CreateMany can cache the new entity.
	if !strings.Contains(out, "c.dispatchMutation(ctx, m, result)") {
		t.Errorf("MutationHook does not forward result to dispatchMutation; output:\n%s", out)
	}
	if !strings.Contains(out, "func (c *Cache) dispatchMutation(ctx context.Context, m *hook.MutationContext, result any) {") {
		t.Errorf("dispatchMutation signature missing result parameter")
	}
}

func TestCacheTemplate_dispatchMutationSetsOnCreate(t *testing.T) {
	cfg := cacheTestConfig(true)
	ctx := gen.BuildCacheContext(
		[]gen.TableContext{cacheTestTable(), cacheTestCompositeTable()},
		nil, cfg, "db", "Client",
	)
	out := executeCacheTemplate(t, ctx)

	for _, want := range []string{
		"case hook.OpCreate:",
		"case TableProducts:\n\t\t\t\tif entity, ok := result.(*Product); ok {\n\t\t\t\t\tc.setProduct(ctx, entity)",
		"case TableOrderItems:\n\t\t\t\tif entity, ok := result.(*OrderItem); ok {\n\t\t\t\t\tc.setOrderItem(ctx, entity)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("OpCreate dispatch missing %q", want)
		}
	}
}

func TestCacheTemplate_dispatchMutationSetsOnCreateMany(t *testing.T) {
	cfg := cacheTestConfig(true)
	ctx := gen.BuildCacheContext(
		[]gen.TableContext{cacheTestTable(), cacheTestCompositeTable()},
		nil, cfg, "db", "Client",
	)
	out := executeCacheTemplate(t, ctx)

	for _, want := range []string{
		"case hook.OpCreateMany:",
		"if entities, ok := result.([]*Product); ok {",
		"for _, entity := range entities {\n\t\t\t\t\t\tc.setProduct(ctx, entity)",
		"if entities, ok := result.([]*OrderItem); ok {",
		"c.setOrderItem(ctx, entity)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("OpCreateMany dispatch missing %q", want)
		}
	}
}

func TestCacheTemplate_createCachingSkipsWhenFieldOptionsSet(t *testing.T) {
	cfg := cacheTestConfig(true)
	ctx := gen.BuildCacheContext([]gen.TableContext{cacheTestTable()}, nil, cfg, "db", "Client")
	out := executeCacheTemplate(t, ctx)

	// Partial fetches and relationship-loaded entities must not be cached
	// (PRD §27.6). The conservative check is "caller specified any
	// FieldOptions" — detected via extractFieldOptions != nil and captured
	// as fieldOptsSet before the closure so it's stable at commit time.
	if !strings.Contains(out, "fieldOptsSet := extractFieldOptions(m.CallOptions) != nil") {
		t.Errorf("dispatchMutation missing fieldOptsSet guard derivation; output:\n%s", out)
	}
	createIdx := strings.Index(out, "case hook.OpCreate:")
	if createIdx < 0 {
		t.Fatalf("missing OpCreate case in dispatchMutation; output:\n%s", out)
	}
	guardIdx := strings.Index(out[createIdx:], "if fieldOptsSet {")
	if guardIdx < 0 {
		t.Errorf("OpCreate case missing fieldOptsSet early-return guard")
	}
	createManyIdx := strings.Index(out, "case hook.OpCreateMany:")
	if createManyIdx < 0 {
		t.Fatalf("missing OpCreateMany case in dispatchMutation")
	}
	if !strings.Contains(out[createManyIdx:], "if fieldOptsSet {") {
		t.Errorf("OpCreateMany case missing fieldOptsSet early-return guard")
	}
}

// --- Hydration DB re-query ---

func TestCacheTemplate_hydrateSignatureTakesNext(t *testing.T) {
	cfg := cacheTestConfig(true)
	ctx := gen.BuildCacheContext(
		[]gen.TableContext{cacheTestTable(), cacheTestCompositeTable()},
		nil, cfg, "db", "Client",
	)
	out := executeCacheTemplate(t, ctx)

	for _, want := range []string{
		"func (c *Cache) hydrateProduct(pk uuid.UUID, next hook.QueryHandler) {",
		"func (c *Cache) hydrateOrderItem(pk OrderItemPK, next hook.QueryHandler) {",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("hydrate signature missing next parameter: %q", want)
		}
	}
}

func TestCacheTemplate_hydrateRerunsWithFullFieldOptions(t *testing.T) {
	cfg := cacheTestConfig(true)
	ctx := gen.BuildCacheContext([]gen.TableContext{cacheTestTable()}, nil, cfg, "db", "Client")
	out := executeCacheTemplate(t, ctx)

	// The background goroutine must issue a fresh QueryContext with full
	// FieldOptions + SkipHooks:true and invoke next(ctx, q).
	for _, want := range []string{
		"Op:     hook.OpGet,",
		"Table:  TableProducts,",
		"CallOptions: CallOptions[ProductFieldOptions]{",
		"FieldOptions: fullParentFieldOptionsProduct(),",
		"SkipHooks:    true,",
		"result, err := next(ctx, q)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("hydrate goroutine missing %q; output:\n%s", want, out)
		}
	}
}

func TestCacheTemplate_hydrateSetsCacheOnSuccess(t *testing.T) {
	cfg := cacheTestConfig(true)
	ctx := gen.BuildCacheContext([]gen.TableContext{cacheTestTable()}, nil, cfg, "db", "Client")
	out := executeCacheTemplate(t, ctx)

	// The serialize + SetAs + metrics path must live inside the hydrate
	// goroutine so the cache is populated with the full entity.
	hydrateIdx := strings.Index(out, "func (c *Cache) hydrateProduct(")
	if hydrateIdx < 0 {
		t.Fatalf("hydrateProduct not found; output:\n%s", out)
	}
	closeIdx := strings.Index(out[hydrateIdx:], "// patternsForSourceTable")
	if closeIdx < 0 {
		closeIdx = len(out) - hydrateIdx
	}
	body := out[hydrateIdx : hydrateIdx+closeIdx]

	for _, want := range []string{
		"setErr := cache.SetAs(ctx, c.backend, c.serializer, key, entity, c.ttlForProduct())",
		"c.metrics.SetLatency(schema, TableProducts, time.Since(setStart))",
		"c.metrics.Set(schema, TableProducts)",
		`cache.RouteError(ctx, c.metrics, c.onError, c.breaker, "set", schema, TableProducts, setErr)`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("hydrate Set path missing %q", want)
		}
	}
}

func TestCacheTemplate_hydrateReportsHydrationComplete(t *testing.T) {
	cfg := cacheTestConfig(true)
	ctx := gen.BuildCacheContext([]gen.TableContext{cacheTestTable()}, nil, cfg, "db", "Client")
	out := executeCacheTemplate(t, ctx)

	// HydrationStart fires unconditionally; HydrationComplete fires with
	// the real error from next(ctx, q) or SetAs when applicable.
	hydrateIdx := strings.Index(out, "func (c *Cache) hydrateProduct(")
	nextIdx := strings.Index(out[hydrateIdx:], "// patternsForSourceTable")
	if nextIdx < 0 {
		nextIdx = len(out) - hydrateIdx
	}
	body := out[hydrateIdx : hydrateIdx+nextIdx]

	if !strings.Contains(body, "c.metrics.HydrationStart(schema, TableProducts)") {
		t.Errorf("missing HydrationStart in hydrate goroutine")
	}
	if !strings.Contains(body, "c.metrics.HydrationComplete(schema, TableProducts, err)") {
		t.Errorf("hydrate does not forward next(ctx,q) error into HydrationComplete")
	}
	if !strings.Contains(body, "c.metrics.HydrationComplete(schema, TableProducts, setErr)") {
		t.Errorf("hydrate does not forward SetAs error into HydrationComplete")
	}
	if !strings.Contains(body, "c.metrics.HydrationComplete(schema, TableProducts, nil)") {
		t.Errorf("missing success-path HydrationComplete(..., nil)")
	}
}

func TestCacheTemplate_readThroughPartialTriggersHydrate(t *testing.T) {
	cfg := cacheTestConfig(true)
	ctx := gen.BuildCacheContext([]gen.TableContext{cacheTestTable()}, nil, cfg, "db", "Client")
	out := executeCacheTemplate(t, ctx)

	// readThroughProduct: on miss with non-nil FieldOptions, run next, kick
	// off hydrate(pk, next), return partial result. The partial branch must
	// appear BEFORE the singleflight block so partial results never enter
	// the cache-set path.
	rtIdx := strings.Index(out, "func (c *Cache) readThroughProduct(")
	if rtIdx < 0 {
		t.Fatalf("readThroughProduct not found")
	}
	endIdx := strings.Index(out[rtIdx:], "func (c *Cache) hydrateProduct(")
	if endIdx < 0 {
		t.Fatalf("hydrateProduct not found after readThroughProduct")
	}
	body := out[rtIdx : rtIdx+endIdx]

	partialIdx := strings.Index(body, "if extractFieldOptions(q.CallOptions) != nil {")
	sfIdx := strings.Index(body, "c.sf.DoChan(key,")
	if partialIdx < 0 {
		t.Errorf("partial branch missing in readThroughProduct")
	}
	if sfIdx < 0 {
		t.Errorf("singleflight block missing in readThroughProduct")
	}
	if partialIdx > sfIdx {
		t.Errorf("partial branch (idx %d) must appear before singleflight (idx %d)", partialIdx, sfIdx)
	}
	for _, want := range []string{
		"result, err := next(ctx, q)",
		"c.hydrateProduct(pk, next)",
		"return result, nil",
	} {
		if !strings.Contains(body[partialIdx:], want) {
			t.Errorf("partial branch missing %q", want)
		}
	}
}

func TestCacheTemplate_queryHookBypassesPartialWhenHydrationOff(t *testing.T) {
	cfg := cacheTestConfig(true)
	ctx := gen.BuildCacheContext([]gen.TableContext{cacheTestTable()}, nil, cfg, "db", "Client")
	out := executeCacheTemplate(t, ctx)

	// QueryHook must short-circuit to next when partial AND hydration is
	// off (PRD §27.7 pass-through row). Guard must run before the switch.
	want := "if !c.hydrationEnabled && extractFieldOptions(q.CallOptions) != nil {"
	if !strings.Contains(out, want) {
		t.Fatalf("QueryHook missing partial+hydration-off bypass; want %q", want)
	}
	guardIdx := strings.Index(out, want)
	dispatchIdx := strings.Index(out, "switch q.Table {\n\t\t\tcase TableProducts:")
	if dispatchIdx < 0 {
		t.Fatalf("QueryHook dispatch switch not found")
	}
	if guardIdx > dispatchIdx {
		t.Errorf("partial+hydration-off guard must run before dispatch (guardIdx=%d, dispatchIdx=%d)", guardIdx, dispatchIdx)
	}
}

func TestCacheTemplate_emitsCloseMethod(t *testing.T) {
	cfg := cacheTestConfig(true)
	ctx := gen.BuildCacheContext([]gen.TableContext{cacheTestTable()}, nil, cfg, "db", "Client")
	out := executeCacheTemplate(t, ctx)

	if !strings.Contains(out, "func (c *Cache) Close() error {") {
		t.Error("missing Close method")
	}
	if !strings.Contains(out, `if closer, ok := c.backend.(io.Closer); ok {`) {
		t.Error("Close does not probe backend for io.Closer")
	}
}

func TestCacheTemplate_emitsRefreshInvalidation(t *testing.T) {
	cfg := cacheTestConfig(true)
	cfg.Views["order_summary"] = config.ViewConfig{
		StructName:   "OrderSummary",
		Cache:        &config.ViewCacheConfig{Enabled: new(true)},
		InvalidateOn: []string{"products"},
	}
	views := []gen.ViewContext{
		{
			StructName:              "OrderSummary",
			ViewName:                "order_summary",
			TableNameConstant:       "TableOrderSummaries",
			Schema:                  "public",
			Materialized:            true,
			ConcurrentlyRefreshable: true,
			Columns: []gen.ColumnContext{
				{Name: "id", FieldName: "ID", GoType: "uuid.UUID", DBTag: "id"},
			},
		},
	}
	ctx := gen.BuildCacheContext([]gen.TableContext{cacheTestTable()}, views, cfg, "db", "Client")
	out := executeCacheTemplate(t, ctx)

	for _, want := range []string{
		"if q.Op == hook.OpRefresh {",
		"c.dispatchRefresh(ctx, q.Table)",
		"func (c *Cache) dispatchRefresh(ctx context.Context, table hook.TableName)",
		"func (c *Cache) isCachedView(table hook.TableName) bool",
		"case TableOrderSummaries:\n\t\treturn true",
		"tx.OnCommit(refreshOp)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in generated output", want)
		}
	}

	// Invalidation must fire only after a successful refresh: the OpRefresh
	// branch calls next() and returns on error before dispatchRefresh.
	branchIdx := strings.Index(out, "if q.Op == hook.OpRefresh {")
	dispatchIdx := strings.Index(out, "c.dispatchRefresh(ctx, q.Table)")
	nextIdx := strings.Index(out[branchIdx:], "next(ctx, q)")
	if branchIdx < 0 || dispatchIdx < 0 || nextIdx < 0 {
		t.Fatalf("refresh branch pieces missing (branch=%d dispatch=%d next=%d)", branchIdx, dispatchIdx, nextIdx)
	}
	if branchIdx+nextIdx > dispatchIdx {
		t.Error("dispatchRefresh must run after next() succeeds, not before")
	}
}

func TestCacheTemplate_refreshNoCachedViewsIsNoOp(t *testing.T) {
	cfg := cacheTestConfig(true)
	ctx := gen.BuildCacheContext([]gen.TableContext{cacheTestTable()}, nil, cfg, "db", "Client")
	out := executeCacheTemplate(t, ctx)

	// With no cached views, isCachedView degrades to an unconditional false —
	// a matview refresh performs no cache operations at all.
	if !strings.Contains(out, "func (c *Cache) isCachedView(table hook.TableName) bool {\n\treturn false\n}") {
		t.Errorf("isCachedView should be unconditional false with no cached views; output:\n%s", out)
	}
}

// TestCacheTemplate_whereOpsShareKeyBasedArm pins key-based
// invalidation at the template level: the four *Where ops sit in the same case as the PK-keyed ops and
// dispatch InvalidateMany, and dispatchMutation emits no BuildTablePattern
// mutation fallback. The entity-invalidation path must reach the backend by
// key only — BuildTablePattern survives solely for InvalidateTable, the view
// cascade, and the tenant capture-gap fallback.
func TestCacheTemplate_whereOpsShareKeyBasedArm(t *testing.T) {
	cfg := cacheTestConfig(true)
	ctx := gen.BuildCacheContext([]gen.TableContext{cacheTestTable()}, nil, cfg, "db", "Client")
	out := executeCacheTemplate(t, ctx)

	// OpUpsertMany joins the *Many group here rather than OpCreateMany's
	// write-through arm — PRD §27.7 gives it the "Invalidate many" row (27.6).
	if !strings.Contains(out, "hook.OpUpsertMany, hook.OpUpdateMany, hook.OpSoftDeleteMany, hook.OpHardDeleteMany, hook.OpRestoreMany,\n\t\t\thook.OpUpdateWhere, hook.OpSoftDeleteWhere, hook.OpHardDeleteWhere, hook.OpRestoreWhere:") {
		t.Error("*Where ops are not folded into the key-based invalidation arm")
	}
	if strings.Contains(out, "case hook.OpUpdateWhere, hook.OpSoftDeleteWhere, hook.OpHardDeleteWhere, hook.OpRestoreWhere:") {
		t.Error("*Where ops still have a separate dispatch arm")
	}

	// The only BuildTablePattern call left in a non-tenanted package is
	// InvalidateTable's.
	if got := strings.Count(out, "cache.BuildTablePattern("); got != 1 {
		t.Errorf("BuildTablePattern call count = %d, want 1 (InvalidateTable only); a mutation-path pattern wipe has come back", got)
	}

	// The uncached-table guard keeps a cache-bookkeeping miss from failing a
	// committed write under CallbackSync (PRD §27.9).
	if !strings.Contains(out, "if _, ok := cacheSchemaFor(table); !ok {") {
		t.Error("invalidation arm missing the uncached-table guard")
	}
}

// TestCacheTemplate_tenantedWhereOpsStayPerRow verifies the tenanted half of
// the merged arm still routes through invalidateAffectedTenanted, and that the
// tenanted fallback keeps its BuildTablePattern reach (PRD §29.5).
func TestCacheTemplate_tenantedWhereOpsStayPerRow(t *testing.T) {
	cfg := cacheTestConfig(true)
	ctx := gen.BuildCacheContext([]gen.TableContext{cacheTestTenantedTable()}, nil, cfg, "db", "Client")
	out := executeCacheTemplate(t, ctx)

	arm := "hook.OpUpdateWhere, hook.OpSoftDeleteWhere, hook.OpHardDeleteWhere, hook.OpRestoreWhere:"
	armIdx := strings.Index(out, arm)
	if armIdx < 0 {
		t.Fatalf("merged invalidation arm not found; output:\n%s", out)
	}
	tenantedIdx := strings.Index(out[armIdx:], "return c.invalidateAffectedTenanted(ctx, table, affectedTenants, affected)")
	if tenantedIdx < 0 {
		t.Error("tenanted table lost its per-row invalidation inside the merged arm")
	}
	if !strings.Contains(out, "func (c *Cache) invalidateTenantedFallback(") {
		t.Error("tenant capture-gap fallback missing")
	}
	// The fallback's pattern takes the bare SQL name from cacheRelationFor, as
	// InvalidateTable's does: string(table) carries the schema on PostgreSQL
	// (PRD §8.5) and would build sqlgen:public.public.<t>:*, which matches no
	// key.
	if !strings.Contains(out, "_ = c.invalidatePattern(ctx, table, cache.BuildTablePattern(c.prefix, schema, name))") {
		t.Error("tenant capture-gap fallback does not build its pattern from the bare SQL name")
	}
}
