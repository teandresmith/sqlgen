package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	buildmanifest "github.com/teandresmith/sqlgen/cmd/sqlgen/manifest"
	"github.com/teandresmith/sqlgen/manifest"
)

// manifestFilename is the fixed file name the loader walks up the tree for when
// --manifest is not given (MCP.md §3.1).
const manifestFilename = "manifest_gen.json"

// Store holds the parsed manifest behind a RWMutex so reads never tear against a
// watch-mode reload (MCP.md §6.3). Every (re)load validates against the embedded
// schema before the swap; a changed-but-invalid manifest is rejected — the last
// good manifest keeps serving and ok flips to false until a subsequent good
// reload.
type Store struct {
	path   string
	logger *slog.Logger

	mu           sync.RWMutex
	doc          *manifest.Document
	raw          []byte
	entities     []*manifest.Entity          // full records, sorted by Name
	byName       map[string]*manifest.Entity // entity Name -> full record
	byTable      map[string]*manifest.Entity // SQL table -> full record
	loadedAt     time.Time
	lastReloadAt time.Time // zero until the first watch-triggered reload
	ok           bool
}

// NewStore builds a Store bound to the resolved manifest path. Reloads re-read
// this path. A nil logger discards output.
func NewStore(path string, logger *slog.Logger) *Store {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Store{path: path, logger: logger}
}

// Path returns the resolved manifest path the store loads from.
func (s *Store) Path() string { return s.path }

// Load reads, validates, and installs the manifest as the initial good manifest.
// It runs once at startup; a failure here is fatal (the server has nothing to
// serve) and is returned as a coded *Error (-32001 not-found, -32002 invalid).
func (s *Store) Load() error {
	doc, raw, entities, err := s.read()
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.install(doc, raw, entities)
	s.loadedAt = time.Now()
	s.ok = true
	s.mu.Unlock()
	s.warnOnSchemaMismatch(doc.SchemaVersion)
	return nil
}

// Reload re-reads the manifest from disk on a watch event. On success it
// atomically swaps in the new manifest and returns nil. On failure the last good
// manifest keeps serving, ok flips to false, and the error is returned — the
// changed-but-invalid manifest is never installed (MCP.md §6.3).
func (s *Store) Reload() error {
	doc, raw, entities, err := s.read()
	if err != nil {
		s.mu.Lock()
		s.ok = false
		s.lastReloadAt = time.Now()
		s.mu.Unlock()
		return err
	}
	s.mu.Lock()
	s.install(doc, raw, entities)
	s.lastReloadAt = time.Now()
	s.ok = true
	s.mu.Unlock()
	s.warnOnSchemaMismatch(doc.SchemaVersion)
	return nil
}

// install swaps in a freshly-read manifest and its resolved entity lookups. The
// caller holds the write lock. The maps are rebuilt whole so a reader either
// sees the entire prior manifest or the entire new one, never a mix.
func (s *Store) install(doc *manifest.Document, raw []byte, entities []*manifest.Entity) {
	sort.Slice(entities, func(i, j int) bool { return entities[i].Name < entities[j].Name })
	byName := make(map[string]*manifest.Entity, len(entities))
	byTable := make(map[string]*manifest.Entity, len(entities))
	for _, e := range entities {
		byName[e.Name] = e
		byTable[e.Table] = e
	}
	s.doc, s.raw = doc, raw
	s.entities, s.byName, s.byTable = entities, byName, byTable
}

// read loads and validates the manifest at the store's path without touching
// shared state, then resolves the full per-entity records (inline for the
// single layout, or the sibling entities/ files for per_entity). Validation
// runs before the JSON is trusted.
func (s *Store) read() (*manifest.Document, []byte, []*manifest.Entity, error) {
	raw, err := os.ReadFile(s.path) //nolint:gosec // the server reads a user-supplied manifest path by design.
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil, nil, &Error{Code: CodeManifestNotFound, Err: err, Message: fmt.Sprintf("manifest not found: %s", s.path)}
		}
		// The path exists but is unreadable (permission denied, a directory,
		// I/O error) — that is an internal failure, not a missing manifest.
		return nil, nil, nil, &Error{Code: CodeInternal, Err: err, Message: fmt.Sprintf("read manifest %s", s.path)}
	}
	if err := buildmanifest.ValidateAgainstSchema(raw); err != nil {
		return nil, nil, nil, &Error{Code: CodeManifestInvalid, Err: err, Message: fmt.Sprintf("manifest %s failed schema validation", s.path)}
	}
	var doc manifest.Document
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, nil, nil, &Error{Code: CodeManifestInvalid, Err: err, Message: fmt.Sprintf("parse manifest %s", s.path)}
	}
	entities, err := resolveEntities(s.path, raw, &doc)
	if err != nil {
		return nil, nil, nil, err
	}
	return &doc, raw, entities, nil
}

// resolveEntities recovers the full per-entity records the top-level document
// only carries in lightweight index form (Document.Entities is []EntityIndex).
// For the single layout the records are re-decoded from the inline entities[]
// in raw; for per_entity they are read from the sibling files named by each
// index entry, relative to the manifest directory. The result is sorted by
// entity Name for deterministic iteration (MCP.md §6.5).
func resolveEntities(path string, raw []byte, doc *manifest.Document) ([]*manifest.Entity, error) {
	var out []*manifest.Entity
	switch doc.Layout {
	case manifest.LayoutPerEntity:
		dir := filepath.Dir(path)
		for _, idx := range doc.Entities {
			if idx.File == "" {
				return nil, &Error{Code: CodeManifestInvalid, Message: fmt.Sprintf("per_entity manifest index for %s is missing its file reference", idx.Name)}
			}
			p := filepath.Join(dir, filepath.FromSlash(idx.File))
			data, err := os.ReadFile(p) //nolint:gosec // entity file paths come from the validated manifest index.
			if err != nil {
				return nil, &Error{Code: CodeManifestInvalid, Err: err, Message: fmt.Sprintf("read entity file %s", p)}
			}
			var e manifest.Entity
			if err := json.Unmarshal(data, &e); err != nil {
				return nil, &Error{Code: CodeManifestInvalid, Err: err, Message: fmt.Sprintf("parse entity file %s", p)}
			}
			out = append(out, &e)
		}
	default: // single layout (also the empty-layout fallback)
		var inline struct {
			Entities []manifest.Entity `json:"entities"`
		}
		if err := json.Unmarshal(raw, &inline); err != nil {
			return nil, &Error{Code: CodeManifestInvalid, Err: err, Message: fmt.Sprintf("parse inline entities in %s", path)}
		}
		for i := range inline.Entities {
			out = append(out, &inline.Entities[i])
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Manifest returns the current good manifest document under a read lock. The
// returned pointer is only swapped by a subsequent reload, never mutated in
// place, so callers may read it without further synchronization.
func (s *Store) Manifest() *manifest.Document {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.doc
}

// Raw returns the raw bytes of the current good manifest (backing the
// sqlgen://manifest resource). The slice is never mutated after install.
func (s *Store) Raw() []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.raw
}

// GenerationConfig returns the manifest's resolved generation-config block and
// whether the raw manifest actually carried the generation_config key.
// Presence is probed from the raw bytes because a manifest predating the
// generation_config block decodes into a zero-valued (all-false) struct
// indistinguishable from a genuine all-off config — the sqlgen://config
// resource needs the distinction to fall back to an empty-object-plus-warning
// (MCP.md §4.2). The doc and raw are read under one lock so the pair is a
// consistent snapshot across reloads.
func (s *Store) GenerationConfig() (manifest.GenerationConfig, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.doc == nil {
		return manifest.GenerationConfig{}, false
	}
	var probe struct {
		GenerationConfig json.RawMessage `json:"generation_config"`
	}
	_ = json.Unmarshal(s.raw, &probe)
	return s.doc.GenerationConfig, len(probe.GenerationConfig) > 0
}

// Entities returns the current good manifest's full entity records, sorted by
// Name. The backing slice is swapped whole on reload, never mutated in place, so
// callers may range over the returned value without further synchronization.
func (s *Store) Entities() []*manifest.Entity {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.entities
}

// EntityByName returns the full entity record for the given entity name.
func (s *Store) EntityByName(name string) (*manifest.Entity, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.byName[name]
	return e, ok
}

// EntityByTable returns the full entity record for the given SQL table name.
func (s *Store) EntityByTable(table string) (*manifest.Entity, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.byTable[table]
	return e, ok
}

// EntityNames returns every entity name in sorted order, the candidate set for
// entity-level fuzzy suggestions (MCP.md §5.3).
func (s *Store) EntityNames() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, len(s.entities))
	for i, e := range s.entities {
		names[i] = e.Name
	}
	return names
}

// Health is the diagnostic snapshot backing the sqlgen_health tool (MCP.md
// §4.1). It carries no manifest data.
type Health struct {
	ManifestPath  string
	SchemaVersion string
	GeneratedAt   string
	LoadedAt      time.Time
	LastReloadAt  time.Time // zero when no reload has happened
	OK            bool
}

// Health snapshots the store's current diagnostic state under a read lock.
func (s *Store) Health() Health {
	s.mu.RLock()
	defer s.mu.RUnlock()
	h := Health{
		ManifestPath: s.path,
		LoadedAt:     s.loadedAt,
		LastReloadAt: s.lastReloadAt,
		OK:           s.ok,
	}
	if s.doc != nil {
		h.SchemaVersion = s.doc.SchemaVersion
		h.GeneratedAt = s.doc.GeneratedAt
	}
	return h
}

// warnOnSchemaMismatch logs a warning when the loaded manifest's schema_version
// differs from the version sqlgen was built against. The manifest still serves
// (structural validation already passed); the warning flags that the tools may
// read fields shaped for a different schema revision.
func (s *Store) warnOnSchemaMismatch(version string) {
	if version != buildmanifest.SchemaVersion {
		s.logger.Warn("manifest schema_version differs from the version sqlgen was built against",
			"manifest_schema_version", version,
			"builtin_schema_version", buildmanifest.SchemaVersion)
	}
}

// DiscoverManifest walks up from startDir, returning the first ancestor
// directory that directly contains a manifest_gen.json file (MCP.md §3.1). It
// returns a -32001 coded *Error if none is found up to the filesystem root.
func DiscoverManifest(startDir string) (string, error) {
	dir := startDir
	for {
		candidate := filepath.Join(dir, manifestFilename)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", &Error{
				Code:    CodeManifestNotFound,
				Message: fmt.Sprintf("no %s found in %s or any parent directory; pass --manifest <path>", manifestFilename, startDir),
			}
		}
		dir = parent
	}
}
