package livedb

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// ParityException is a documented way the generated tree differs by source
// (PRD §6.1). Normalize removes it from one file's content, and NormalizeTree
// applies it to both trees, so the difference compares equal.
type ParityException struct {
	Name   string
	Reason string

	// Normalize returns content with the difference removed. dialect is the
	// tree's input.dialect and path the file's slash path from the output dir.
	Normalize func(dialect, path, content string) (string, error)
}

// ParityExceptions is every allowed difference between the files and
// database trees.
var ParityExceptions = []ParityException{
	{
		Name: "view source",
		Reason: "a view's manifest source is its definition as the database stores it: PostgreSQL's " +
			"pg_get_viewdef deparse, MySQL's qualified and backticked text, SQLite's statement without " +
			"its terminator. Under files it is the view file's text, annotations included.",
		Normalize: dropViewSource,
	},
	{
		Name: "MySQL literal default",
		Reason: "MySQL stores a literal default converted to its column's type, so a BOOLEAN's " +
			"DEFAULT TRUE reads back as 1: \"a literal default is stored converted to its column's " +
			"type … `TRUE` as `1`\" (PRD §6.1).",
		Normalize: mysqlBooleanDefault,
	},
}

// NormalizeTree applies every ParityException to tree, the generated files of
// a dialect's output dir keyed by slash path.
func NormalizeTree(t testing.TB, dialect string, tree map[string]string) map[string]string {
	t.Helper()
	out, err := normalizeTree(dialect, tree)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func normalizeTree(dialect string, tree map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(tree))
	for path, content := range tree {
		for _, e := range ParityExceptions {
			var err error
			if content, err = e.Normalize(dialect, path, content); err != nil {
				return nil, fmt.Errorf("parity exception %q on %s: %w", e.Name, path, err)
			}
		}
		out[path] = content
	}
	return out, nil
}

const manifestJSON = "manifest/manifest_gen.json"

// isManifestJSON reports whether path is the JSON manifest, or one entity
// file of its split layout.
func isManifestJSON(path string) bool {
	return path == manifestJSON ||
		strings.HasPrefix(path, "manifest/entities/") && strings.HasSuffix(path, ".json")
}

func isManifestMarkdown(path string) bool {
	return strings.HasPrefix(path, "manifest/") && strings.HasSuffix(path, ".md")
}

// editJSON decodes content, applies edit, and encodes the result.
func editJSON(content string, edit func(doc map[string]any) error) (string, error) {
	var doc map[string]any
	if err := json.Unmarshal([]byte(content), &doc); err != nil {
		return "", fmt.Errorf("decoding manifest: %w", err)
	}
	if err := edit(doc); err != nil {
		return "", err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return "", fmt.Errorf("encoding manifest: %w", err)
	}
	return buf.String(), nil
}

// markdownViewSource is the Source line of a manifest view page, which the
// Files section follows.
var markdownViewSource = regexp.MustCompile("(?m)^- \\*\\*Source:\\*\\* `(?s:.*?)`\n\n## Files\n")

func dropViewSource(_, path, content string) (string, error) {
	switch {
	case path == manifestJSON:
		return editJSON(content, func(doc map[string]any) error {
			entities, _ := doc["entities"].([]any)
			for _, e := range entities {
				if e, ok := e.(map[string]any); ok {
					if err := dropEntitySource(e); err != nil {
						return err
					}
				}
			}
			return nil
		})
	case isManifestJSON(path):
		return editJSON(content, dropEntitySource)
	case isManifestMarkdown(path):
		return markdownViewSource.ReplaceAllString(content, "## Files\n"), nil
	default:
		return content, nil
	}
}

// dropEntitySource removes a view entity's source. An entry of the split
// layout's index points at its entity file instead, and has no source.
func dropEntitySource(e map[string]any) error {
	if e["kind"] != "view" {
		return nil
	}
	if _, ok := e["file"]; ok {
		return nil
	}
	if e["source"] == "" || e["source"] == nil {
		return fmt.Errorf("view %v has no source", e["table"])
	}
	delete(e, "source")
	return nil
}

// mysqlBooleanLiteral is the value MySQL stores for a boolean literal
// default on a tinyint(1) column.
var mysqlBooleanLiteral = map[string]string{"true": "1", "false": "0"}

// markdownBooleanDefault is a tinyint(1) column row of a manifest page up to
// its Default cell, three cells (nullable, pk, unique) past the DB type.
var markdownBooleanDefault = regexp.MustCompile("(`tinyint\\(1\\)` \\|(?:[^|\n]*\\|){3} )`(true|false)`")

func mysqlBooleanDefault(dialect, path, content string) (string, error) {
	if dialect != "mysql" {
		return content, nil
	}
	switch {
	case isManifestJSON(path):
		return editJSON(content, func(doc map[string]any) error {
			rewriteBooleanDefaults(doc)
			return nil
		})
	case isManifestMarkdown(path):
		return markdownBooleanDefault.ReplaceAllStringFunc(content, func(row string) string {
			m := markdownBooleanDefault.FindStringSubmatch(row)
			return m[1] + "`" + mysqlBooleanLiteral[m[2]] + "`"
		}), nil
	default:
		return content, nil
	}
}

// rewriteBooleanDefaults rewrites the default of every tinyint(1) column
// under v from true or false to its stored form.
func rewriteBooleanDefaults(v any) {
	switch v := v.(type) {
	case map[string]any:
		if v["db_type"] == "tinyint(1)" {
			if d, ok := v["default"].(string); ok && mysqlBooleanLiteral[d] != "" {
				v["default"] = mysqlBooleanLiteral[d]
			}
		}
		for _, child := range v {
			rewriteBooleanDefaults(child)
		}
	case []any:
		for _, child := range v {
			rewriteBooleanDefaults(child)
		}
	}
}
