package manifest

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
)

// LoadInto parses the embedded manifest bytes into dst and populates entityMap
// with the full per-entity records, keyed by schema.table for an entity with a
// schema and by the bare table otherwise; read it with FindEntity. It is the
// helper the generated package's init() calls once at package-load time.
//
// dst.Entities always ends up in the lightweight index form (that is how both
// layouts serialize the top-level document). The full entity records are
// resolved independently into entityMap so the Client.ManifestEntity lookup is
// layout-agnostic:
//
//   - Per-entity layout: entityFS is the embedded entities/ tree; each
//     <file_prefix>.json is parsed into a full Entity.
//   - Single layout: entityFS is nil and the full records are re-read from the
//     inline entities[] carried in topJSON.
//
// entityMap must be non-nil. Any parse failure is wrapped in ErrParseManifest.
func LoadInto(topJSON []byte, entityFS fs.FS, dst *Document, entityMap map[string]*Entity) error {
	if err := json.Unmarshal(topJSON, dst); err != nil {
		return fmt.Errorf("%w: %w", ErrParseManifest, err)
	}

	if entityFS != nil {
		return loadPerEntity(entityFS, entityMap)
	}
	return loadInline(topJSON, entityMap)
}

// loadInline resolves full entity records from the inline entities[] carried in
// the single-layout top-level document. Document.Entities decodes to the
// lightweight index form, so the raw bytes are decoded a second time through a
// full-entity view to recover every field.
func loadInline(topJSON []byte, entityMap map[string]*Entity) error {
	var full struct {
		Entities []Entity `json:"entities"`
	}
	if err := json.Unmarshal(topJSON, &full); err != nil {
		return fmt.Errorf("%w: %w", ErrParseManifest, err)
	}
	for i := range full.Entities {
		e := full.Entities[i]
		entityMap[entityKey(&e)] = &e
	}
	return nil
}

// loadPerEntity walks the embedded per-entity FS, parsing every *.json file
// into a full Entity keyed by its SQL table name.
func loadPerEntity(entityFS fs.FS, entityMap map[string]*Entity) error {
	// The walk func wraps every error in ErrParseManifest; WalkDir only
	// propagates those, so the outer return needs no further wrapping.
	err := fs.WalkDir(entityFS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("%w: %w", ErrParseManifest, err)
		}
		if d.IsDir() || !strings.HasSuffix(path.Base(p), ".json") {
			return nil
		}
		data, err := fs.ReadFile(entityFS, p)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrParseManifest, err)
		}
		var e Entity
		if err := json.Unmarshal(data, &e); err != nil {
			return fmt.Errorf("%w: %w", ErrParseManifest, err)
		}
		entityMap[entityKey(&e)] = &e
		return nil
	})
	return err //nolint:wrapcheck // walk func already wraps every error in ErrParseManifest
}

// entityKey is the key LoadInto stores an entity under: schema.table when the
// entity has a schema, so two schemas' tables of one name stay apart, and the
// bare table otherwise.
func entityKey(e *Entity) string {
	if e.Schema == "" {
		return e.Table
	}
	return e.Schema + "." + e.Table
}

// FindEntity returns the record LoadInto stored for name; it is the lookup
// behind the generated Client.ManifestEntity. name is an entity's key
// (schema.table for an entity with a schema, the table otherwise) or the bare
// table of a schema'd entity whose table name no other schema shares.
//
// An unknown name returns ErrEntityNotFound itself. A bare table that two or
// more schemas share returns an error wrapping ErrEntityNotFound that names each
// schema.table it could mean.
func FindEntity(entityMap map[string]*Entity, name string) (*Entity, error) {
	if e, ok := entityMap[name]; ok {
		return e, nil
	}
	var (
		found      *Entity
		candidates []string
	)
	for key, e := range entityMap {
		if e.Schema != "" && e.Table == name {
			found = e
			candidates = append(candidates, key)
		}
	}
	switch len(candidates) {
	case 0:
		return nil, ErrEntityNotFound
	case 1:
		return found, nil
	default:
		slices.Sort(candidates)
		return nil, fmt.Errorf("%w: table %q is in more than one schema (%s); name it as schema.table",
			ErrEntityNotFound, name, strings.Join(candidates, ", "))
	}
}
