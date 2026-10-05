package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/teandresmith/sqlgen/manifest"
)

// connectWithSurface stands up a Server with the fixture store and the full
// registered surface (tools + resources + prompts) on one end of an in-memory
// transport, returning an initialized client session. Resource/prompt dispatch
// tests use this to exercise the real SDK read path (template matching, _meta
// passthrough, protocol errors) rather than the pure handler functions.
func connectWithSurface(ctx context.Context, t *testing.T, store *Store) *mcp.ClientSession {
	t.Helper()
	serverT, clientT := mcp.NewInMemoryTransports()

	srv := New("test-version", nil)
	srv.store = store
	srv.watchEnabled = true
	srv.registerTools()
	srv.registerResources()
	srv.registerPrompts()

	go func() { _ = srv.Serve(ctx, serverT) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// storeWithRaw builds a Store whose doc + raw are the given values, without
// touching disk. Used to control the generation_config presence probe and the
// manifest-size warning independently of the shared fixture.
func storeWithRaw(doc *manifest.Document, raw []byte) *Store {
	s := NewStore("/fixture/manifest/manifest_gen.json", nil)
	s.install(doc, raw, fixtureEntities())
	s.ok = true
	return s
}

func TestManifestResourceServesRawWithoutWarningWhenSmall(t *testing.T) {
	d := fixtureDeps()
	res, err := manifestResource(d, resourceManifestURI)
	if err != nil {
		t.Fatalf("manifestResource: %v", err)
	}
	if len(res.Contents) != 1 {
		t.Fatalf("got %d contents, want 1", len(res.Contents))
	}
	c := res.Contents[0]
	if c.MIMEType != mimeJSON {
		t.Errorf("mime = %q, want %q", c.MIMEType, mimeJSON)
	}
	if c.Text != string(fixtureRaw) {
		t.Errorf("body = %q, want the raw manifest %q", c.Text, fixtureRaw)
	}
	if _, ok := c.Meta[metaWarningKey]; ok {
		t.Errorf("small manifest must not carry a warning, got %v", c.Meta[metaWarningKey])
	}
}

func TestManifestResourceWarnsAboveThreshold(t *testing.T) {
	// A raw body just over the 256 KB threshold triggers the warning while the
	// full document is still served verbatim.
	big := make([]byte, manifestWarnThreshold+1)
	for i := range big {
		big[i] = 'x'
	}
	d := &toolDeps{store: storeWithRaw(fixtureDoc(), big)}
	res, err := manifestResource(d, resourceManifestURI)
	if err != nil {
		t.Fatalf("manifestResource: %v", err)
	}
	c := res.Contents[0]
	if len(c.Text) != len(big) {
		t.Errorf("body length = %d, want the full %d bytes", len(c.Text), len(big))
	}
	w, ok := c.Meta[metaWarningKey].(string)
	if !ok || !strings.Contains(w, "threshold") {
		t.Errorf("warning = %v, want a threshold warning", c.Meta[metaWarningKey])
	}
}

func TestConventionsResourceMatchesManifest(t *testing.T) {
	d := fixtureDeps()
	res, err := conventionsResource(d, resourceConventionsURI)
	if err != nil {
		t.Fatalf("conventionsResource: %v", err)
	}
	var got manifest.Conventions
	if err := json.Unmarshal([]byte(res.Contents[0].Text), &got); err != nil {
		t.Fatalf("decode conventions: %v", err)
	}
	want := d.store.Manifest().Conventions
	if got.ClientEntryPoints != want.ClientEntryPoints {
		t.Errorf("client entry points = %+v, want %+v", got.ClientEntryPoints, want.ClientEntryPoints)
	}
	if len(got.ErrorSentinels) != 1 || got.ErrorSentinels[0].Name != "ErrNotFound" {
		t.Errorf("error sentinels = %+v, want [ErrNotFound]", got.ErrorSentinels)
	}
}

func TestConfigResourceServesGenerationConfigWhenPresent(t *testing.T) {
	// A raw manifest carrying generation_config surfaces the resolved block with
	// no warning.
	raw := []byte(`{"schema_version":"0.1.0","generation_config":{"cache":true,"tenancy":true}}`)
	d := &toolDeps{store: storeWithRaw(fixtureDoc(), raw)}
	res, err := configResource(d, resourceConfigURI)
	if err != nil {
		t.Fatalf("configResource: %v", err)
	}
	c := res.Contents[0]
	if _, ok := c.Meta[metaWarningKey]; ok {
		t.Errorf("present generation_config must not warn, got %v", c.Meta[metaWarningKey])
	}
	var got manifest.GenerationConfig
	if err := json.Unmarshal([]byte(c.Text), &got); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	// The served block is the resolved struct from the doc (fixtureDoc sets
	// SoftDelete/Tenancy/Views), not the abbreviated raw probe.
	if !got.Tenancy || !got.SoftDelete || !got.Views {
		t.Errorf("config = %+v, want the resolved generation config", got)
	}
}

func TestConfigResourceFallsBackWhenAbsent(t *testing.T) {
	// The shared fixtureRaw omits generation_config: fall back to {} + warning
	// rather than a misleading all-false struct.
	d := fixtureDeps()
	res, err := configResource(d, resourceConfigURI)
	if err != nil {
		t.Fatalf("configResource: %v", err)
	}
	c := res.Contents[0]
	if strings.TrimSpace(c.Text) != "{}" {
		t.Errorf("absent config body = %q, want {}", c.Text)
	}
	w, ok := c.Meta[metaWarningKey].(string)
	if !ok || !strings.Contains(w, "generation_config field absent") {
		t.Errorf("warning = %v, want an absence warning", c.Meta[metaWarningKey])
	}
}

func TestEntityResourceServesFullRecord(t *testing.T) {
	d := fixtureDeps()
	res, err := entityResource(d, resourceEntityPrefix+"User")
	if err != nil {
		t.Fatalf("entityResource: %v", err)
	}
	var got manifest.Entity
	if err := json.Unmarshal([]byte(res.Contents[0].Text), &got); err != nil {
		t.Fatalf("decode entity: %v", err)
	}
	if got.Name != "User" {
		t.Errorf("entity name = %q, want User", got.Name)
	}
	if len(got.Relationships) != 2 {
		t.Errorf("relationships = %d, want 2 (full record, not the lightweight index)", len(got.Relationships))
	}
}

func TestEntityResourceUnknownNameIsCodedErrorWithSuggestions(t *testing.T) {
	d := fixtureDeps()
	_, err := entityResource(d, resourceEntityPrefix+"Usr")
	if err == nil {
		t.Fatal("expected an error for an unknown entity")
	}
	var je *jsonrpc.Error
	if !errors.As(err, &je) {
		t.Fatalf("error = %T, want *jsonrpc.Error", err)
	}
	if je.Code != int64(CodeEntityNotFound) {
		t.Errorf("code = %d, want %d", je.Code, CodeEntityNotFound)
	}
	var data struct {
		Suggestions []string `json:"suggestions"`
	}
	if err := json.Unmarshal(je.Data, &data); err != nil {
		t.Fatalf("decode error data: %v", err)
	}
	if len(data.Suggestions) != 1 || data.Suggestions[0] != "User" {
		t.Errorf("suggestions = %v, want [User]", data.Suggestions)
	}
	// Suggestions are also inlined into the message so they survive a client
	// library that collapses the coded error (see entityResource doc).
	if !strings.Contains(je.Message, "User") {
		t.Errorf("message = %q, want the suggestion inlined", je.Message)
	}
}

func TestEntityNameFromURI(t *testing.T) {
	if got := entityNameFromURI(resourceEntityPrefix + "User"); got != "User" {
		t.Errorf("name = %q, want User", got)
	}
	if got := entityNameFromURI("sqlgen://manifest"); got != "sqlgen://manifest" {
		t.Errorf("non-entity URI = %q, want it returned unchanged", got)
	}
}

// --- SDK dispatch round-trips (real read path) ---

func TestDispatchListsThreeFixedResources(t *testing.T) {
	cs := connectWithSurface(t.Context(), t, newFixtureStore())
	res, err := cs.ListResources(t.Context(), nil)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	got := make(map[string]bool)
	for _, r := range res.Resources {
		got[r.URI] = true
	}
	for _, uri := range []string{resourceManifestURI, resourceConventionsURI, resourceConfigURI} {
		if !got[uri] {
			t.Errorf("resource %q not listed", uri)
		}
	}
	// The entity resource is a template, listed under resources/templates/list.
	if got[resourceEntityTemplate] {
		t.Errorf("entity template must not appear in resources/list")
	}
}

func TestDispatchReadsEntityViaTemplate(t *testing.T) {
	cs := connectWithSurface(t.Context(), t, newFixtureStore())
	res, err := cs.ReadResource(t.Context(), &mcp.ReadResourceParams{URI: resourceEntityPrefix + "Post"})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if len(res.Contents) != 1 || res.Contents[0].MIMEType != mimeJSON {
		t.Fatalf("unexpected contents: %+v", res.Contents)
	}
	var got manifest.Entity
	if err := json.Unmarshal([]byte(res.Contents[0].Text), &got); err != nil {
		t.Fatalf("decode entity: %v", err)
	}
	if got.Name != "Post" {
		t.Errorf("entity name = %q, want Post", got.Name)
	}
}

func TestDispatchManifestWarningSurvivesToClient(t *testing.T) {
	// The 256 KB warning rides in _meta.warning; assert it survives the full SDK
	// read path to a client (not just the handler), and the full body is served.
	big := make([]byte, manifestWarnThreshold+1)
	for i := range big {
		big[i] = 'x'
	}
	cs := connectWithSurface(t.Context(), t, storeWithRaw(fixtureDoc(), big))
	res, err := cs.ReadResource(t.Context(), &mcp.ReadResourceParams{URI: resourceManifestURI})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	c := res.Contents[0]
	if len(c.Text) != len(big) {
		t.Errorf("body length = %d, want the full %d bytes", len(c.Text), len(big))
	}
	w, ok := c.Meta[metaWarningKey].(string)
	if !ok || !strings.Contains(w, "threshold") {
		t.Errorf("client _meta warning = %v, want a threshold warning", c.Meta[metaWarningKey])
	}
}

func TestDispatchUnknownEntityResourceSurfacesError(t *testing.T) {
	// Through the go-sdk client library, the coded -32003 wire error is collapsed
	// to an opaque error keeping only the message (the same collapse tools
	// see; resources have no IsError escape hatch). The wire-level
	// code + data.suggestions are asserted by the direct-handler test and the
	// raw-JSON-RPC leg. Here we assert the client still gets an error, the inlined
	// suggestion survives in the message, and the session stays usable.
	cs := connectWithSurface(t.Context(), t, newFixtureStore())
	_, err := cs.ReadResource(t.Context(), &mcp.ReadResourceParams{URI: resourceEntityPrefix + "Usr"})
	if err == nil {
		t.Fatal("expected an error for an unknown entity resource")
	}
	if !strings.Contains(err.Error(), "User") {
		t.Errorf("error %q should inline the suggestion for agent self-correction", err.Error())
	}
	// The session must remain usable after a resource error.
	if _, err := cs.ListResources(t.Context(), nil); err != nil {
		t.Errorf("session unusable after a resource error: %v", err)
	}
}
