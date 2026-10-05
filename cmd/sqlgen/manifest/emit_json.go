package manifest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// defaultJSONFilename is the top-level manifest JSON name used when
// manifest.json_filename is unset. The stale-cleanup pass resolves the same
// default when deciding whether a manifest directory is sqlgen's, so both sides
// share this constant.
const defaultJSONFilename = "manifest_gen.json"

// EmitJSON writes the manifest's on-disk JSON artifacts (PRD §30.3 / §30.4).
//
// outputDir is the resolved <output.dir>; every file lands under
// <outputDir>/<mfst.MarkdownDir>/. The disk shape is driven by mfst.JSONLayout:
//
//   - single:     one file <markdown_dir>/<json_filename> carrying the full
//     inline entities[].
//   - per_entity: <markdown_dir>/<json_filename> carrying the lightweight
//     entity index ({name, table, kind, file}), plus one full-record file per
//     entity at <markdown_dir>/<json_per_entity_dir>/<file_prefix>.json.
//
// enums[] and extras[] are always inline in both layouts (PRD §30.4).
//
// Output is deterministic byte-for-byte given an unchanged Document: HTML
// escaping is disabled, indentation is two spaces, slices are emitted in the
// builder's stable order, and the only map values (sql_bodies, comparator
// composition/examples) are key-sorted by encoding/json — no map-iteration
// nondeterminism (PRD §5.7). The single non-deterministic field, generated_at,
// is already fixed on doc by the builder (from --manifest-timestamp); EmitJSON
// treats the Document as read-only.
func EmitJSON(doc *Document, mfst *config.ManifestConfig, outputDir string) error {
	if doc == nil {
		return fmt.Errorf("emit manifest json: nil document")
	}
	if mfst == nil {
		return fmt.Errorf("emit manifest json: nil manifest config")
	}

	dir := filepath.Join(outputDir, dirOrDefault(mfst.MarkdownDir, "manifest"))
	filename := dirOrDefault(mfst.JSONFilename, defaultJSONFilename)

	if mfst.JSONLayout == config.JSONLayoutPerEntity {
		return emitPerEntity(doc, dir, filename, dirOrDefault(mfst.JSONPerEntityDir, "entities"))
	}
	return emitSingle(doc, dir, filename)
}

// emitSingle writes the whole Document (full inline entities) to one file.
func emitSingle(doc *Document, dir, filename string) error {
	data, err := encodeJSON(doc)
	if err != nil {
		return fmt.Errorf("emit manifest json: %w", err)
	}
	return writeJSONFile(filepath.Join(dir, filename), data)
}

// emitPerEntity writes the lightweight top-level index plus one full-record
// file per entity under entitiesDir.
func emitPerEntity(doc *Document, dir, filename, entitiesDir string) error {
	data, err := encodeJSON(indexDocumentOf(doc, entitiesDir))
	if err != nil {
		return fmt.Errorf("emit manifest json index: %w", err)
	}
	if err := writeJSONFile(filepath.Join(dir, filename), data); err != nil {
		return err
	}

	for i := range doc.Entities {
		e := &doc.Entities[i]
		body, err := encodeJSON(e)
		if err != nil {
			return fmt.Errorf("emit manifest json entity %s: %w", e.Table, err)
		}
		if err := writeJSONFile(filepath.Join(dir, entitiesDir, e.FilePrefix+".json"), body); err != nil {
			return err
		}
	}
	return nil
}

// entityIndexEntry is the lightweight per-entity pointer emitted in the
// per_entity top-level index (PRD §30.4 per-entity layout).
type entityIndexEntry struct {
	Name  string `json:"name"`
	Table string `json:"table"`
	Kind  string `json:"kind"`
	File  string `json:"file"`
}

// indexDocument mirrors Document's top-level envelope but replaces the full
// entities[] with lightweight index pointers. Its field order matches Document
// so the two layouts render an identical top-level shape.
type indexDocument struct {
	Schema           string             `json:"$schema,omitempty"`
	SchemaVersion    string             `json:"schema_version"`
	GeneratedAt      string             `json:"generated_at"`
	Generator        Generator          `json:"generator"`
	Dialect          string             `json:"dialect"`
	Package          string             `json:"package"`
	Layout           string             `json:"layout"`
	Conventions      Conventions        `json:"conventions"`
	GenerationConfig GenerationConfig   `json:"generation_config"`
	Entities         []entityIndexEntry `json:"entities"`
	Enums            []Enum             `json:"enums"`
	Extras           []Extra            `json:"extras"`
}

// indexDocumentOf projects doc onto the per-entity index envelope. entitiesDir
// is the (markdown-relative) directory holding the full per-entity files; the
// index file pointers use forward slashes so they stay portable across OSes.
func indexDocumentOf(doc *Document, entitiesDir string) *indexDocument {
	entries := make([]entityIndexEntry, len(doc.Entities))
	for i := range doc.Entities {
		e := &doc.Entities[i]
		entries[i] = entityIndexEntry{
			Name:  e.Name,
			Table: e.Table,
			Kind:  e.Kind,
			File:  path.Join(entitiesDir, e.FilePrefix+".json"),
		}
	}
	return &indexDocument{
		Schema:           doc.Schema,
		SchemaVersion:    doc.SchemaVersion,
		GeneratedAt:      doc.GeneratedAt,
		Generator:        doc.Generator,
		Dialect:          doc.Dialect,
		Package:          doc.Package,
		Layout:           doc.Layout,
		Conventions:      doc.Conventions,
		GenerationConfig: doc.GenerationConfig,
		Entities:         entries,
		Enums:            doc.Enums,
		Extras:           doc.Extras,
	}
}

// encodeJSON renders v as indented JSON with HTML escaping disabled. The
// trailing newline emitted by json.Encoder.Encode is retained so files end in
// a newline (PRD §5.7 determinism — the value is fixed across runs).
func encodeJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("encode manifest json: %w", err)
	}
	return buf.Bytes(), nil
}

// writeJSONFile creates the parent directory and writes data, matching the
// codegen file-permission convention (0o750 dirs, 0o600 files).
func writeJSONFile(target string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return fmt.Errorf("emit manifest json: %w", err)
	}
	if err := os.WriteFile(target, data, 0o600); err != nil {
		return fmt.Errorf("emit manifest json: %w", err)
	}
	return nil
}

// dirOrDefault returns v when non-empty, else fallback. Guards against a
// Document emitted with an unresolved (default-unfilled) ManifestConfig.
func dirOrDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
