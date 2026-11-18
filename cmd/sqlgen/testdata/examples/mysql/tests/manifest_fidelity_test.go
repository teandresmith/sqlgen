package tests

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/manifest"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/mysql/models"
)

// pin compiles only when its argument has exactly type T, so each call states a
// signature the build checks.
func pin[T any](T) {}

// decodeAny decodes raw JSON into the generic form, so two documents compare by
// content regardless of key order.
func decodeAny(t *testing.T, raw []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("decoding JSON: %v", err)
	}
	return v
}

// onDiskEntityRecords returns the full entity records the generator wrote: the
// inline entities[] in the single layout, one file per entity in per_entity.
func onDiskEntityRecords(t *testing.T) []json.RawMessage {
	t.Helper()
	if manifestWantLayout == "single" {
		var top struct {
			Entities []json.RawMessage `json:"entities"`
		}
		raw, err := os.ReadFile(manifestJSONPath)
		if err != nil {
			t.Fatalf("reading on-disk manifest: %v", err)
		}
		if err := json.Unmarshal(raw, &top); err != nil {
			t.Fatalf("decoding on-disk entities: %v", err)
		}
		return top.Entities
	}
	files, err := filepath.Glob(filepath.Join(filepath.Dir(manifestJSONPath), "entities", "*.json"))
	if err != nil {
		t.Fatalf("listing entity files: %v", err)
	}
	records := make([]json.RawMessage, 0, len(files))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		records = append(records, b)
	}
	return records
}

// TestManifest_MethodSignatures pins the three generated manifest methods'
// signatures, and that the Client has no raw-bytes accessor.
func TestManifest_MethodSignatures(t *testing.T) {
	c := &models.Client{}
	pin[func() *manifest.Document](c.Manifest)
	pin[func(string) (*manifest.Entity, error)](c.ManifestEntity)
	pin[func() string](c.ManifestVersion)
	if _, ok := reflect.TypeFor[*models.Client]().MethodByName("ManifestRaw"); ok {
		t.Error("Client has a ManifestRaw method, want none")
	}
}

// TestManifest_MarshalIsContentEquivalent marshals the embedded document back to
// JSON and compares it by content with the on-disk top-level file. The single
// layout's file carries full entity records inline; the document holds the
// index form of each (name, table, schema when it has one, kind), so the
// expectation projects them.
func TestManifest_MarshalIsContentEquivalent(t *testing.T) {
	raw, err := os.ReadFile(manifestJSONPath)
	if err != nil {
		t.Fatalf("reading on-disk manifest: %v", err)
	}
	want, ok := decodeAny(t, raw).(map[string]any)
	if !ok {
		t.Fatal("on-disk manifest is not a JSON object")
	}
	if manifestWantLayout == "single" {
		records, _ := want["entities"].([]any)
		index := make([]any, 0, len(records))
		for _, r := range records {
			rec, _ := r.(map[string]any)
			entry := map[string]any{"name": rec["name"], "table": rec["table"], "kind": rec["kind"]}
			if s, _ := rec["schema"].(string); s != "" {
				entry["schema"] = s
			}
			index = append(index, entry)
		}
		want["entities"] = index
	}

	got, err := json.Marshal((&models.Client{}).Manifest())
	if err != nil {
		t.Fatalf("marshaling Manifest(): %v", err)
	}
	if diff := cmp.Diff(any(want), decodeAny(t, got)); diff != "" {
		t.Errorf("json.Marshal(Manifest()) differs from the on-disk manifest (-disk +marshaled):\n%s", diff)
	}
}

// TestManifest_EveryEntityRecordIsReachable looks up every full record the
// generator wrote: by schema.table for an entity with a schema and by table
// otherwise, and by bare table, which resolves only when no other schema has a
// table of that name.
func TestManifest_EveryEntityRecordIsReachable(t *testing.T) {
	type identity struct {
		Table  string `json:"table"`
		Schema string `json:"schema"`
	}
	client := &models.Client{}
	records := onDiskEntityRecords(t)
	if len(records) == 0 {
		t.Fatal("no on-disk entity records")
	}
	// A file outside the entities directory read as an entity lands under the
	// empty key; nothing may.
	if phantom, err := client.ManifestEntity(""); err != manifest.ErrEntityNotFound { //nolint:errorlint // identity: an unknown name returns the sentinel itself
		t.Errorf("ManifestEntity(\"\") = (%+v, %v), want ErrEntityNotFound: a non-entity file was loaded as an entity", phantom, err)
	}
	ids := make([]identity, len(records))
	schemasPerTable := map[string]int{}
	for i, rec := range records {
		if err := json.Unmarshal(rec, &ids[i]); err != nil {
			t.Fatalf("decoding entity identity: %v", err)
		}
		if ids[i].Schema != "" {
			schemasPerTable[ids[i].Table]++
		}
	}

	for i, rec := range records {
		id := ids[i]
		key := id.Table
		if id.Schema != "" {
			key = id.Schema + "." + id.Table
		}
		e, err := client.ManifestEntity(key)
		if err != nil {
			t.Errorf("ManifestEntity(%q): %v", key, err)
			continue
		}
		got, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("marshaling entity %q: %v", key, err)
		}
		if diff := cmp.Diff(decodeAny(t, rec), decodeAny(t, got)); diff != "" {
			t.Errorf("ManifestEntity(%q) is not the on-disk record (-disk +got):\n%s", key, diff)
		}

		if id.Schema == "" {
			continue
		}
		bare, err := client.ManifestEntity(id.Table)
		switch {
		case schemasPerTable[id.Table] > 1:
			if bare != nil || !errors.Is(err, manifest.ErrEntityNotFound) || err == manifest.ErrEntityNotFound { //nolint:errorlint // identity: a shared bare name wraps the sentinel rather than returning it
				t.Errorf("ManifestEntity(%q), a table name several schemas share = (%v, %v), want nil and an error wrapping ErrEntityNotFound", id.Table, bare, err)
			}
		case err != nil || bare != e:
			t.Errorf("ManifestEntity(%q) = (%p, %v), want the record ManifestEntity(%q) returns (%p)", id.Table, bare, err, key, e)
		}
	}
}
