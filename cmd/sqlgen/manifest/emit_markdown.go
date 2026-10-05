package manifest

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

//go:embed templates/*.md.tmpl
var markdownTemplates embed.FS

// markdownTmpl is the parsed template set for the three manifest markdown
// artifacts (_index.md, _conventions.md, per-entity <file_prefix>.md). text/template
// (not html/template) is used deliberately: the output is markdown, and HTML
// escaping would mangle comments, SQL, and code samples. Determinism is preserved
// by rendering only pre-sorted slices — every map is projected to a sorted slice
// via a funcmap helper (sortedPairs / sqlBlocks); no template ranges a Go map.
var markdownTmpl = template.Must(
	template.New("manifest-markdown").Funcs(markdownFuncs).ParseFS(markdownTemplates, "templates/*.md.tmpl"),
)

// EmitMarkdown writes the manifest's on-disk markdown artifacts (PRD §30.3):
// _index.md (entity scan table + Features line), _conventions.md (the package-wide
// reference block, PRD §30.4.1), and one <file_prefix>.md per entity. All land
// under <outputDir>/<mfst.MarkdownDir>/ (default markdown_dir "manifest").
//
// Signature note: like EmitJSON, this reads MarkdownDir off the resolved
// ManifestConfig rather than taking a loose scalar, so config overrides are
// honored from a single source of truth. include_examples is already applied at
// build time (the builder only populates Entity.Examples when the flag is set), so
// the emitter simply renders Examples when present.
//
// Output is deterministic byte-for-byte given an unchanged Document.
func EmitMarkdown(doc *Document, mfst *config.ManifestConfig, outputDir string) error {
	if doc == nil {
		return fmt.Errorf("emit manifest markdown: nil document")
	}
	if mfst == nil {
		return fmt.Errorf("emit manifest markdown: nil manifest config")
	}

	dir := filepath.Join(outputDir, dirOrDefault(mfst.MarkdownDir, "manifest"))

	index, err := renderMarkdown("_index.md.tmpl", doc)
	if err != nil {
		return err
	}
	if err := writeMarkdownFile(filepath.Join(dir, "_index.md"), index); err != nil {
		return err
	}

	conventions, err := renderMarkdown("_conventions.md.tmpl", doc)
	if err != nil {
		return err
	}
	if err := writeMarkdownFile(filepath.Join(dir, "_conventions.md"), conventions); err != nil {
		return err
	}

	for i := range doc.Entities {
		e := &doc.Entities[i]
		body, err := renderMarkdown("entity.md.tmpl", e)
		if err != nil {
			return fmt.Errorf("emit manifest markdown entity %s: %w", e.Table, err)
		}
		if err := writeMarkdownFile(filepath.Join(dir, e.FilePrefix+".md"), body); err != nil {
			return err
		}
	}
	return nil
}

// renderMarkdown executes the named template against data and returns the
// normalized bytes.
func renderMarkdown(name string, data any) ([]byte, error) {
	var buf bytes.Buffer
	if err := markdownTmpl.ExecuteTemplate(&buf, name, data); err != nil {
		return nil, fmt.Errorf("render manifest markdown %s: %w", name, err)
	}
	return normalizeMarkdown(buf.Bytes()), nil
}

// normalizeMarkdown collapses consecutive blank lines to a single blank line and
// guarantees the file ends with exactly one trailing newline, so section-boundary
// whitespace in the templates never leaks more than one blank line. Blank lines
// *inside* a fenced code block (```sql / ```go) are preserved verbatim — an
// intentional blank line in a multi-line SQL body or code example is never lost.
func normalizeMarkdown(b []byte) []byte {
	out := make([]string, 0, bytes.Count(b, []byte("\n"))+1)
	inFence := false
	prevBlank := false
	for line := range strings.SplitSeq(string(b), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			prevBlank = false
			out = append(out, line)
			continue
		}
		if !inFence && strings.TrimSpace(line) == "" {
			if prevBlank {
				continue // collapse: at most one blank line between blocks
			}
			prevBlank = true
			out = append(out, line)
			continue
		}
		prevBlank = false
		out = append(out, line)
	}
	joined := strings.TrimRight(strings.Join(out, "\n"), "\n")
	return []byte(joined + "\n")
}

// writeMarkdownFile creates the parent directory and writes data, matching the
// codegen file-permission convention (0o750 dirs, 0o600 files).
func writeMarkdownFile(target string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return fmt.Errorf("emit manifest markdown: %w", err)
	}
	if err := os.WriteFile(target, data, 0o600); err != nil {
		return fmt.Errorf("emit manifest markdown: %w", err)
	}
	return nil
}

// --- template funcmap ---

var markdownFuncs = template.FuncMap{
	"featuresLine":   featuresLine,
	"join":           strings.Join,
	"mark":           mark,
	"mdCell":         mdCell,
	"firstLine":      firstLine,
	"sqlBlocks":      sqlBlocks,
	"sortedPairs":    sortedPairs,
	"fkCell":         fkCell,
	"checkedColumns": checkedColumns,
	"marker":         func() string { return breadcrumbMarker },
}

// featuresLine renders the comma-separated list of enabled package-wide toggles
// for _index.md's Features line (PRD §30.4.3 / MANIFEST.md §5.1.1). Labels are
// the generation_config JSON field names, listed in alphabetical order so the
// line is deterministic and matches the JSON snapshot. Returns "none" when every
// toggle is false.
func featuresLine(gc GenerationConfig) string {
	toggles := []struct {
		label string
		on    bool
	}{
		{"audit_columns", gc.AuditColumns},
		{"cache", gc.Cache},
		{"events", gc.Events},
		{"graph_top_level", gc.GraphTopLevel},
		{"graphql", gc.GraphQL},
		{"soft_delete", gc.SoftDelete},
		{"tenancy", gc.Tenancy},
		{"views", gc.Views},
	}
	enabled := make([]string, 0, len(toggles))
	for _, t := range toggles {
		if t.on {
			enabled = append(enabled, t.label)
		}
	}
	if len(enabled) == 0 {
		return "none"
	}
	return strings.Join(enabled, ", ")
}

// mark renders a boolean table cell: "yes" when true, empty otherwise.
func mark(b bool) string {
	if b {
		return "yes"
	}
	return ""
}

// mdCell flattens a free-text value (a comment) for safe placement in a markdown
// table cell: newlines collapse to spaces and pipes are escaped so they do not
// break the table. HTML-special characters pass through verbatim.
func mdCell(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "|", "\\|")
	return strings.TrimSpace(s)
}

// firstLine returns the first line of a multi-line string, used to render a brief
// description in the _index.md entity scan table.
func firstLine(s string) string {
	first, _, _ := strings.Cut(s, "\n")
	return first
}

// sqlBlock pairs a dialect with its canonical SQL for the per-method Generated SQL
// subsection.
type sqlBlock struct {
	Dialect string
	SQL     string
}

// sqlBlocks projects a method's sql_bodies map onto a dialect-sorted slice so the
// entity template renders one fenced block per dialect in a deterministic order
// (PRD §30.4 acceptance: multi-dialect blocks ordered alphabetically by dialect).
func sqlBlocks(bodies map[string]string) []sqlBlock {
	blocks := make([]sqlBlock, 0, len(bodies))
	for dialect, body := range bodies {
		blocks = append(blocks, sqlBlock{Dialect: dialect, SQL: body})
	}
	sort.Slice(blocks, func(i, j int) bool { return blocks[i].Dialect < blocks[j].Dialect })
	return blocks
}

// pair is a sorted key/value entry projected from a string map.
type pair struct {
	Key   string
	Value string
}

// sortedPairs projects a string map onto a key-sorted slice so templates can
// render map contents without ranging a Go map (non-deterministic order).
func sortedPairs(m map[string]string) []pair {
	pairs := make([]pair, 0, len(m))
	for k, v := range m {
		pairs = append(pairs, pair{Key: k, Value: v})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].Key < pairs[j].Key })
	return pairs
}

// fkCell renders a relationship's foreign-key column for the relationships table:
// "[schema.]table.column" for FK-backed edges, "[schema.]table (local_fk →
// target_fk)" for M2M junctions, empty when neither is present.
func fkCell(r Relationship) string {
	if r.Junction != nil {
		return fmt.Sprintf("%s (%s → %s)", qualifiedTable(r.Junction.Schema, r.Junction.Table), r.Junction.LocalFK, r.Junction.TargetFK)
	}
	if r.FK != nil {
		return qualifiedTable(r.FK.Schema, r.FK.Table) + "." + r.FK.Column
	}
	return ""
}

// qualifiedTable prefixes table with its schema when one is set — the spelling
// a relationship's `table:` / `junction:` accepts in config.
func qualifiedTable(schema, table string) string {
	if schema == "" {
		return table
	}
	return schema + "." + table
}

// checkedColumns returns the subset of columns carrying a CHECK-constraint
// expression, so the entity template can render the Validation subsection only
// when at least one column has a check.
func checkedColumns(cols []Column) []Column {
	out := make([]Column, 0, len(cols))
	for _, c := range cols {
		if c.Check != "" {
			out = append(out, c)
		}
	}
	return out
}
