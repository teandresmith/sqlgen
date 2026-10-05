package gen_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/sql"
)

func loadUnifiedClientTemplate(t *testing.T) *template.Template {
	t.Helper()
	tmpl, err := template.New("client.go.tmpl").
		Funcs(gen.FuncMap(sql.NewPostgresDialect())).
		ParseFiles(filepath.Join("templates", "client.go.tmpl"))
	if err != nil {
		t.Fatalf("parsing unified client template: %v", err)
	}
	return tmpl
}

func executeUnifiedClientTemplate(t *testing.T, ctx gen.ClientContext) string {
	t.Helper()
	tmpl := loadUnifiedClientTemplate(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "client", ctx); err != nil {
		t.Fatalf("executing template: %v", err)
	}
	wrapped := gen.WrapWithPreamble(ctx.Package, ctx.Imports, []byte(buf.String()))
	return string(wrapped)
}

func testUnifiedClientContext_tablesAndViews() gen.ClientContext {
	tables := []gen.TableContext{
		{StructName: "Product"},
		{StructName: "Order"},
		{StructName: "User"},
	}
	views := []gen.ViewContext{
		{StructName: "ProductSummary"},
	}
	return gen.BuildClientContext(tables, views, "db", "Client", false, nil, nil, nil)
}

func testUnifiedClientContext_tablesOnly() gen.ClientContext {
	tables := []gen.TableContext{
		{StructName: "Product"},
	}
	return gen.BuildClientContext(tables, nil, "db", "Client", false, nil, nil, nil)
}

func testUnifiedClientContext_viewsOnly() gen.ClientContext {
	views := []gen.ViewContext{
		{StructName: "ProductSummary"},
	}
	return gen.BuildClientContext(nil, views, "db", "Client", false, nil, nil, nil)
}

func TestUnifiedClientTemplate_structWithAllEntities(t *testing.T) {
	ctx := testUnifiedClientContext_tablesAndViews()
	output := executeUnifiedClientTemplate(t, ctx)

	wantPatterns := []string{
		"type Client struct {",
		"querier database.Querier",
		"order *orderClient",
		"product *productClient",
		"productSummary *productSummaryClient",
		"user *userClient",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestUnifiedClientTemplate_newConstructor(t *testing.T) {
	ctx := testUnifiedClientContext_tablesAndViews()
	output := executeUnifiedClientTemplate(t, ctx)

	wantPatterns := []string{
		"func New(querier database.Querier, opts ...ClientOption) *Client {",
		"order: newOrderClient(querier, options.mutationHooks, options.queryHooks, options.panicHandler)",
		"product: newProductClient(querier, options.mutationHooks, options.queryHooks, options.panicHandler)",
		"productSummary: newProductSummaryClient(querier, options.mutationHooks, options.queryHooks, options.panicHandler)",
		"user: newUserClient(querier, options.mutationHooks, options.queryHooks, options.panicHandler)",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestUnifiedClientTemplate_accessorsReturnInterfaces(t *testing.T) {
	ctx := testUnifiedClientContext_tablesAndViews()
	output := executeUnifiedClientTemplate(t, ctx)

	wantPatterns := []string{
		"func (c *Client) Orders() OrderClient {",
		"func (c *Client) Products() ProductClient {",
		"func (c *Client) ProductSummary() ProductSummaryClient {",
		"func (c *Client) Users() UserClient {",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing accessor: %q\n\nfull output:\n%s", want, output)
		}
	}

	// Accessors return the field value.
	wantBodies := []string{
		"return c.order",
		"return c.product",
		"return c.productSummary",
		"return c.user",
	}
	for _, want := range wantBodies {
		if !strings.Contains(output, want) {
			t.Errorf("output missing accessor body: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestUnifiedClientTemplate_transactionMethods(t *testing.T) {
	ctx := testUnifiedClientContext_tablesAndViews()
	output := executeUnifiedClientTemplate(t, ctx)

	wantPatterns := []string{
		"func (c *Client) Begin(ctx context.Context, name string, opts ...database.TxOptions) (context.Context, error) {",
		"return database.NewTransaction(ctx, c.querier, name, c.applyTxDefaults(opts)...)",
		"func (c *Client) Commit(ctx context.Context) error {",
		"return database.Commit(ctx)",
		"func (c *Client) Rollback(ctx context.Context) error {",
		"return database.Rollback(ctx)",
		"func (c *Client) WithTx(ctx context.Context, name string, fn func(context.Context) error, opts ...database.TxOptions) error {",
		"return database.WithTransaction(ctx, c.querier, name, fn, c.applyTxDefaults(opts)...)",
		"func (c *Client) applyTxDefaults(opts []database.TxOptions) []database.TxOptions {",
		"merged.CallbackMode = c.opts.callbackMode",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing transaction method: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestUnifiedClientTemplate_querierReturnsConnFromContext(t *testing.T) {
	ctx := testUnifiedClientContext_tablesAndViews()
	output := executeUnifiedClientTemplate(t, ctx)

	wantPatterns := []string{
		"func (c *Client) Querier(ctx context.Context) database.Querier {",
		"return database.Conn(ctx, c.querier)",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing Querier method: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestUnifiedClientTemplate_querierReturnsOriginalWhenNoTx(t *testing.T) {
	// The Querier method delegates to database.Conn which handles both cases.
	// This test verifies the method signature and delegation are correct.
	ctx := testUnifiedClientContext_tablesAndViews()
	output := executeUnifiedClientTemplate(t, ctx)

	if !strings.Contains(output, "database.Conn(ctx, c.querier)") {
		t.Error("Querier should delegate to database.Conn")
	}
}

func TestUnifiedClientTemplate_rawQuery(t *testing.T) {
	ctx := testUnifiedClientContext_tablesAndViews()
	output := executeUnifiedClientTemplate(t, ctx)

	// PRD §20.2: Raw takes a scan callback, and scanning happens inside the
	// terminal so the result set — and a transaction's connection reservation
	// with it — cannot outlive the call.
	wantPatterns := []string{
		"func (c *Client) Raw(ctx context.Context, rawSQL string, args []any, fn func(database.Rows) error) error {",
		"return nil, database.QueryFunc(ctx, database.Conn(ctx, c.querier), rawSQL, args, fn)",
		"hook.BuildQueryChain",
		`Op:    hook.QueryOp("raw_query"),`,
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing Raw method: %q\n\nfull output:\n%s", want, output)
		}
	}

	// No generated code hands a live result set to a consumer: the old
	// signature returned database.Rows through a type assertion on the chain's
	// result, the only one on that interface in the generated tree.
	for _, unwanted := range []string{
		"(database.Rows, error)",
		"result.(database.Rows)",
	} {
		if strings.Contains(output, unwanted) {
			t.Errorf("output still contains %q\n\nfull output:\n%s", unwanted, output)
		}
	}
}

func TestUnifiedClientTemplate_rawExec(t *testing.T) {
	ctx := testUnifiedClientContext_tablesAndViews()
	output := executeUnifiedClientTemplate(t, ctx)

	wantPatterns := []string{
		"func (c *Client) RawExec(ctx context.Context, rawSQL string, args ...any) (database.Result, error) {",
		"return database.Conn(ctx, c.querier).Exec(ctx, rawSQL, args...)",
		"hook.BuildMutationChain",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing RawExec method: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestUnifiedClientTemplate_pingWithNativeMethod(t *testing.T) {
	ctx := testUnifiedClientContext_tablesAndViews()
	output := executeUnifiedClientTemplate(t, ctx)

	wantPatterns := []string{
		"func (c *Client) Ping(ctx context.Context) error {",
		"if p, ok := c.querier.(pinger); ok {",
		"return p.Ping(ctx)",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing Ping native path: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestUnifiedClientTemplate_pingFallbackSelect1(t *testing.T) {
	ctx := testUnifiedClientContext_tablesAndViews()
	output := executeUnifiedClientTemplate(t, ctx)

	wantPatterns := []string{
		`QueryRow(ctx, "SELECT 1")`,
		"row.Scan(&n)",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing Ping fallback: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestUnifiedClientTemplate_closeWithIOCloser(t *testing.T) {
	ctx := testUnifiedClientContext_tablesAndViews()
	output := executeUnifiedClientTemplate(t, ctx)

	wantPatterns := []string{
		"func (c *Client) Close() error {",
		"if closer, ok := c.querier.(io.Closer); ok {",
		"errs = append(errs, closer.Close())",
		"return errors.Join(errs...)",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing Close method: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestUnifiedClientTemplate_closeJoinsMultipleErrors(t *testing.T) {
	ctx := testUnifiedClientContext_tablesAndViews()
	output := executeUnifiedClientTemplate(t, ctx)

	// Verify errors.Join is used to collect errors.
	if !strings.Contains(output, "errors.Join(errs...)") {
		t.Error("Close should use errors.Join to join multiple errors")
	}
}

func TestUnifiedClientTemplate_clientOption(t *testing.T) {
	ctx := testUnifiedClientContext_tablesAndViews()
	output := executeUnifiedClientTemplate(t, ctx)

	wantPatterns := []string{
		"type ClientOption func(*clientOptions)",
		"type clientOptions struct {",
		"callbackMode  database.CallbackMode",
		"mutationHooks []hook.MutationHook",
		"queryHooks    []hook.QueryHook",
		"func WithCallbackMode(mode database.CallbackMode) ClientOption {",
		"func WithPanicHandler(handler func(ctx context.Context, r any, table hook.TableName, op string) error) ClientOption {",
		"func WithMutationHook(h hook.MutationHook) ClientOption {",
		"func WithQueryHook(h hook.QueryHook) ClientOption {",
	}
	for _, want := range wantPatterns {
		if !strings.Contains(output, want) {
			t.Errorf("output missing ClientOption pattern: %q\n\nfull output:\n%s", want, output)
		}
	}
}

func TestUnifiedClientTemplate_compilesCleanly(t *testing.T) {
	ctx := testUnifiedClientContext_tablesAndViews()
	output := executeUnifiedClientTemplate(t, ctx)

	_, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "client_gen.go")
	if err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

func TestUnifiedClientTemplate_compilesCleanly_tablesOnly(t *testing.T) {
	ctx := testUnifiedClientContext_tablesOnly()
	output := executeUnifiedClientTemplate(t, ctx)

	_, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "client_gen.go")
	if err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

func TestUnifiedClientTemplate_compilesCleanly_viewsOnly(t *testing.T) {
	ctx := testUnifiedClientContext_viewsOnly()
	output := executeUnifiedClientTemplate(t, ctx)

	_, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "client_gen.go")
	if err != nil {
		t.Fatalf("Format() failed — generated code has syntax errors: %v", err)
	}
}

func TestUnifiedClientTemplate_goldenFile(t *testing.T) {
	ctx := testUnifiedClientContext_tablesAndViews()
	output := executeUnifiedClientTemplate(t, ctx)

	formatted, err := gen.FormatOnly([]byte(output), "v0.0.0-test", "client_gen.go")
	if err != nil {
		t.Fatalf("formatting: %v", err)
	}

	got := string(formatted)
	goldenPath := filepath.Join("testdata", "golden", "unified_client_gen.go")

	if *update {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o750); err != nil { //nolint:gosec // test helper
			t.Fatalf("creating golden dir: %v", err)
		}
		if err := os.WriteFile(goldenPath, formatted, 0o600); err != nil {
			t.Fatalf("writing golden file: %v", err)
		}
		t.Log("golden file updated")
		return
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // golden file path is not user-controlled
	if err != nil {
		t.Fatalf("reading golden file (run with -update to create): %v", err)
	}

	if diff := cmp.Diff(string(want), got); diff != "" {
		t.Errorf("unified_client_gen.go mismatch (-want +got):\n%s", diff)
	}
}
