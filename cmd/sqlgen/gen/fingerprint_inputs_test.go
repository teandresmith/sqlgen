package gen_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
)

// Columns are fingerprint inputs in declaration order: a positional encoding
// (msgpack's array-encoded structs) depends on field order, so swapping two
// columns must move the table to a new key namespace.
func TestFingerprint_columnDeclarationOrderChanges(t *testing.T) {
	cfg := cacheTestConfig(true)
	base := cacheTestTable()
	swapped := cacheTestTable()
	swapped.Columns[1], swapped.Columns[2] = swapped.Columns[2], swapped.Columns[1]

	c1 := gen.BuildCacheContext([]gen.TableContext{base}, nil, cfg, "db", "Client")
	c2 := gen.BuildCacheContext([]gen.TableContext{swapped}, nil, cfg, "db", "Client")
	if c1.CachedTables[0].Fingerprint == c2.CachedTables[0].Fingerprint {
		t.Errorf("fingerprint %q unchanged after swapping two non-PK columns", c1.CachedTables[0].Fingerprint)
	}
}

// The generated cache file bakes each cached table's fingerprint into a
// constant, and the key helpers read that constant.
func TestCacheTemplate_fingerprintConstantCarriesTheTableFingerprint(t *testing.T) {
	cfg := cacheTestConfig(true)
	ctx := gen.BuildCacheContext([]gen.TableContext{cacheTestTable()}, nil, cfg, "db", "Client")
	fp := ctx.CachedTables[0].Fingerprint
	if fp == "" {
		t.Fatal("cache context carries an empty fingerprint")
	}
	out := executeCacheTemplate(t, ctx)

	want := `const fingerprintProduct = "` + fp + `"`
	if !strings.Contains(out, want) {
		t.Errorf("generated cache file lacks %q; output:\n%s", want, out)
	}
	if !strings.Contains(out, `cache.BuildKey(c.prefix, "public", "products", fingerprintProduct, pk)`) {
		t.Error("single-PK key helper does not build its key from the fingerprint constant")
	}
}
