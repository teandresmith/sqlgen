package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/teandresmith/sqlgen/manifest"
)

// errManifestDiff is returned (with exit code 1) when two manifests differ. It
// is never printed — SilenceErrors is set on the root command — it only carries
// the exit code, mirroring git-style "1 = differences found".
var errManifestDiff = errors.New("manifest diff: differences found")

// newManifestDiffCmd builds `sqlgen manifest diff <old> <new>`. It walks
// entities / columns / methods / sentinels / features and prints a structured
// diff. Exit code 0 when identical, 1 when they differ (PRD §30.8). Both disk
// layouts are honored transparently.
func newManifestDiffCmd(flags *cliFlags) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "diff <old> <new>",
		Short: "Show a structured schema-evolution diff between two manifests",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runManifestDiff(cmd, flags, args[0], args[1], jsonOut)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit the diff as machine-readable JSON")
	return cmd
}

func runManifestDiff(cmd *cobra.Command, flags *cliFlags, oldPath, newPath string, jsonOut bool) error {
	errOut := cmd.ErrOrStderr()

	oldDoc, oldEnts, err := loadManifestForDiff(oldPath)
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "Error: %v\n", err)
		return &exitError{code: manifestExitIOFail, err: err}
	}
	newDoc, newEnts, err := loadManifestForDiff(newPath)
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "Error: %v\n", err)
		return &exitError{code: manifestExitIOFail, err: err}
	}

	d := diffManifests(oldDoc, oldEnts, newDoc, newEnts)
	changed := d.hasChanges()

	out := cmd.OutOrStdout()
	switch {
	case jsonOut:
		if err := writeDiffJSON(out, d); err != nil {
			_, _ = fmt.Fprintf(errOut, "Error: %v\n", err)
			return &exitError{code: manifestExitIOFail, err: err}
		}
	case changed && !flags.quiet:
		writeDiffText(out, oldPath, newPath, d)
	}

	if changed {
		return &exitError{code: manifestExitFail, err: errManifestDiff}
	}
	return nil
}

// --- diff model -------------------------------------------------------------

// kvChange is a single old→new value change keyed by name/path.
type kvChange struct {
	Key string `json:"key"`
	Old string `json:"old"`
	New string `json:"new"`
}

// setDiff is the added / removed / changed breakdown for one dimension.
type setDiff struct {
	Added   []string   `json:"added"`
	Removed []string   `json:"removed"`
	Changed []kvChange `json:"changed"`
}

func (s setDiff) empty() bool {
	return len(s.Added) == 0 && len(s.Removed) == 0 && len(s.Changed) == 0
}

// entityDiff bundles the per-entity column / method / feature diffs.
type entityDiff struct {
	Table    string  `json:"table"`
	Columns  setDiff `json:"columns"`
	Methods  setDiff `json:"methods"`
	Features setDiff `json:"features"`
}

func (e entityDiff) empty() bool {
	return e.Columns.empty() && e.Methods.empty() && e.Features.empty()
}

// entitiesDiff is the top-level entity breakdown.
type entitiesDiff struct {
	Added   []string     `json:"added"`
	Removed []string     `json:"removed"`
	Changed []entityDiff `json:"changed"`
}

// manifestDiff is the complete structured diff between two manifests. It is the
// shape emitted by `--json`.
type manifestDiff struct {
	Entities         entitiesDiff `json:"entities"`
	Sentinels        setDiff      `json:"sentinels"`
	GenerationConfig []kvChange   `json:"generation_config"`
}

func (d manifestDiff) hasChanges() bool {
	return len(d.Entities.Added) > 0 || len(d.Entities.Removed) > 0 || len(d.Entities.Changed) > 0 ||
		!d.Sentinels.empty() || len(d.GenerationConfig) > 0
}

// --- diff engine ------------------------------------------------------------

func diffManifests(oldDoc *manifest.Document, oldEnts map[string]*manifest.Entity, newDoc *manifest.Document, newEnts map[string]*manifest.Entity) manifestDiff {
	var d manifestDiff
	d.Entities = diffEntities(oldEnts, newEnts)
	d.Sentinels = diffStringMapNamed(sentinelMap(oldDoc), sentinelMap(newDoc))
	d.GenerationConfig = changedOnly(generationConfigMap(oldDoc), generationConfigMap(newDoc))
	return d
}

func diffEntities(oldEnts, newEnts map[string]*manifest.Entity) entitiesDiff {
	var ed entitiesDiff
	for table := range newEnts {
		if _, ok := oldEnts[table]; !ok {
			ed.Added = append(ed.Added, table)
		}
	}
	for table := range oldEnts {
		if _, ok := newEnts[table]; !ok {
			ed.Removed = append(ed.Removed, table)
		}
	}
	for table, newE := range newEnts {
		oldE, ok := oldEnts[table]
		if !ok {
			continue
		}
		if change := diffEntity(oldE, newE); !change.empty() {
			ed.Changed = append(ed.Changed, change)
		}
	}
	sort.Strings(ed.Added)
	sort.Strings(ed.Removed)
	sort.Slice(ed.Changed, func(i, j int) bool { return ed.Changed[i].Table < ed.Changed[j].Table })
	return ed
}

func diffEntity(oldE, newE *manifest.Entity) entityDiff {
	return entityDiff{
		Table:    newE.Table,
		Columns:  diffStringMapNamed(columnMap(oldE), columnMap(newE)),
		Methods:  diffStringMapNamed(methodMap(oldE), methodMap(newE)),
		Features: diffStringMapKV(flattenFeatures(oldE.Features), flattenFeatures(newE.Features)),
	}
}

// diffStringMapNamed diffs two name→signature maps, reporting added/removed as
// bare names (columns, methods, sentinels).
func diffStringMapNamed(oldM, newM map[string]string) setDiff {
	added, removed, changed := diffStringMaps(oldM, newM)
	return setDiff{Added: added, Removed: removed, Changed: changed}
}

// diffStringMapKV diffs two key→value maps, reporting added/removed as
// "key=value" strings so the value is visible (feature blocks).
func diffStringMapKV(oldM, newM map[string]string) setDiff {
	added, removed, changed := diffStringMaps(oldM, newM)
	sd := setDiff{Changed: changed}
	for _, k := range added {
		sd.Added = append(sd.Added, k+"="+newM[k])
	}
	for _, k := range removed {
		sd.Removed = append(sd.Removed, k+"="+oldM[k])
	}
	return sd
}

// diffStringMaps returns sorted added/removed keys and per-key value changes.
func diffStringMaps(oldM, newM map[string]string) (added, removed []string, changed []kvChange) {
	for k, nv := range newM {
		ov, ok := oldM[k]
		switch {
		case !ok:
			added = append(added, k)
		case ov != nv:
			changed = append(changed, kvChange{Key: k, Old: ov, New: nv})
		}
	}
	for k := range oldM {
		if _, ok := newM[k]; !ok {
			removed = append(removed, k)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	sort.Slice(changed, func(i, j int) bool { return changed[i].Key < changed[j].Key })
	return added, removed, changed
}

// changedOnly diffs a fixed-key map (e.g. generation_config toggles) reporting
// only value changes.
func changedOnly(oldM, newM map[string]string) []kvChange {
	_, _, changed := diffStringMaps(oldM, newM)
	if len(changed) == 0 {
		return nil
	}
	return changed
}

// --- projections ------------------------------------------------------------

func columnMap(e *manifest.Entity) map[string]string {
	m := make(map[string]string, len(e.Columns))
	for _, c := range e.Columns {
		m[c.Name] = columnSig(c)
	}
	return m
}

// columnSig renders the change-signature for a column. It captures every
// schema-evolution signal a review gate cares about — Go/DB type, nullability,
// PK / UNIQUE membership, and default / check / comparator — so a constraint
// flip surfaces as a `~ <col>` change even when the type is unchanged.
func columnSig(c manifest.Column) string {
	parts := []string{c.GoType + " (" + c.DBType + ")"}
	if c.Nullable {
		parts = append(parts, "null")
	}
	if c.PK {
		parts = append(parts, "pk")
	}
	if c.Unique {
		parts = append(parts, "unique")
	}
	if c.Default != "" {
		parts = append(parts, "default="+c.Default)
	}
	if c.DefaultKind != "" {
		parts = append(parts, "default_kind="+c.DefaultKind)
	}
	if c.Check != "" {
		parts = append(parts, "check="+c.Check)
	}
	if c.Comparator != "" {
		parts = append(parts, "comparator="+c.Comparator)
	}
	// Access reclassification is a review-gate signal (PRD §32.3) — an
	// access-only change must surface as `~ <col>`. Omitted access decodes
	// as public, so old-shape manifests diff clean against public columns.
	if c.Access != "" {
		parts = append(parts, "access="+c.Access)
	}
	if c.Redacted {
		parts = append(parts, "redacted")
	}
	return strings.Join(parts, " ")
}

func methodMap(e *manifest.Entity) map[string]string {
	m := make(map[string]string, len(e.Methods.Query)+len(e.Methods.Mutation))
	for _, meth := range e.Methods.Query {
		m[meth.Name] = methodSig(meth)
	}
	for _, meth := range e.Methods.Mutation {
		m[meth.Name] = methodSig(meth)
	}
	return m
}

func methodSig(m manifest.Method) string {
	parts := make([]string, len(m.Params))
	for i, p := range m.Params {
		parts[i] = p.Name + " " + p.Type
	}
	sig := m.Name + "(" + strings.Join(parts, ", ") + ") " + m.Returns
	if len(m.Errors) > 0 {
		// Sort so a reordering of the manifest's errors[] is not a spurious diff.
		errs := append([]string(nil), m.Errors...)
		sort.Strings(errs)
		sig += " errors=[" + strings.Join(errs, ",") + "]"
	}
	return sig
}

func flattenFeatures(f manifest.Features) map[string]string {
	m := make(map[string]string)
	if sd := f.SoftDelete; sd != nil {
		m["soft_delete.column"] = sd.Column
		m["soft_delete.type"] = sd.Type
	}
	if c := f.Cache; c != nil {
		m["cache.ttl_seconds"] = strconv.Itoa(c.TTLSeconds)
		m["cache.hydration"] = c.Hydration
		m["cache.key_pattern"] = c.KeyPattern
		m["cache.invalidates_on"] = strings.Join(c.InvalidatesOn, ",")
	}
	if ev := f.Events; ev != nil {
		m["events.enabled"] = strconv.FormatBool(ev.Enabled)
		m["events.types"] = strings.Join(ev.Types, ",")
		m["events.payload_shape"] = ev.PayloadShape
	}
	if t := f.Tenancy; t != nil {
		m["tenancy.column"] = t.Column
		m["tenancy.mode"] = t.Mode
		m["tenancy.missing_resolver_error"] = t.MissingResolverError
		m["tenancy.mismatch_error"] = t.MismatchError
	}
	if len(f.AuditColumns) > 0 {
		m["audit_columns"] = strings.Join(f.AuditColumns, ",")
	}
	return m
}

func sentinelMap(doc *manifest.Document) map[string]string {
	m := make(map[string]string, len(doc.Conventions.ErrorSentinels))
	for _, s := range doc.Conventions.ErrorSentinels {
		m[s.Name] = s.GraphQLCode + " (" + s.Package + ")"
	}
	return m
}

func generationConfigMap(doc *manifest.Document) map[string]string {
	g := doc.GenerationConfig
	return map[string]string{
		"audit_columns":   strconv.FormatBool(g.AuditColumns),
		"cache":           strconv.FormatBool(g.Cache),
		"events":          strconv.FormatBool(g.Events),
		"graphql":         strconv.FormatBool(g.GraphQL),
		"graph_top_level": strconv.FormatBool(g.GraphTopLevel),
		"soft_delete":     strconv.FormatBool(g.SoftDelete),
		"tenancy":         strconv.FormatBool(g.Tenancy),
		"views":           strconv.FormatBool(g.Views),
	}
}

// --- loading ----------------------------------------------------------------

// loadManifestForDiff reads a manifest and resolves its full entity records,
// following entities[].file pointers in per_entity layout and re-reading the
// inline entities[] in single layout.
func loadManifestForDiff(path string) (*manifest.Document, map[string]*manifest.Entity, error) {
	data, err := os.ReadFile(path) //nolint:gosec // CLI diffs a user-supplied manifest path by design.
	if err != nil {
		return nil, nil, fmt.Errorf("read manifest %s: %w", path, err)
	}
	var doc manifest.Document
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, nil, fmt.Errorf("parse manifest %s: %w", path, err)
	}

	ents := make(map[string]*manifest.Entity)
	if doc.Layout == manifest.LayoutPerEntity {
		if err := loadPerEntityForDiff(path, doc.Entities, ents); err != nil {
			return nil, nil, err
		}
	} else if err := loadInlineForDiff(data, ents); err != nil {
		return nil, nil, err
	}
	return &doc, ents, nil
}

// loadInlineForDiff recovers full entity records from the inline entities[] of
// a single-layout document (Document.Entities decodes to the lightweight form).
func loadInlineForDiff(data []byte, ents map[string]*manifest.Entity) error {
	var full struct {
		Entities []manifest.Entity `json:"entities"`
	}
	if err := json.Unmarshal(data, &full); err != nil {
		return fmt.Errorf("parse inline entities: %w", err)
	}
	for i := range full.Entities {
		e := full.Entities[i]
		ents[e.Table] = &e
	}
	return nil
}

// loadPerEntityForDiff follows each entities[].file pointer (relative to the
// top-level manifest file) and decodes the full per-entity record.
func loadPerEntityForDiff(topPath string, index []manifest.EntityIndex, ents map[string]*manifest.Entity) error {
	base := filepath.Dir(topPath)
	for _, idx := range index {
		if idx.File == "" {
			return fmt.Errorf("per-entity index for %q has no file pointer", idx.Table)
		}
		entPath := filepath.Join(base, filepath.FromSlash(idx.File))
		if !withinDir(base, entPath) {
			return fmt.Errorf("per-entity file %q escapes the manifest directory", idx.File)
		}
		data, err := os.ReadFile(entPath) //nolint:gosec // path is contained under the manifest dir by withinDir above.
		if err != nil {
			return fmt.Errorf("read entity %s: %w", entPath, err)
		}
		var e manifest.Entity
		if err := json.Unmarshal(data, &e); err != nil {
			return fmt.Errorf("parse entity %s: %w", entPath, err)
		}
		ents[e.Table] = &e
	}
	return nil
}

// withinDir reports whether target resolves to a path at or below base, guarding
// the per-entity loader against `../` traversal in a manifest's file pointers.
func withinDir(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// --- rendering --------------------------------------------------------------

func writeDiffJSON(w io.Writer, d manifestDiff) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(d); err != nil {
		return fmt.Errorf("encode diff json: %w", err)
	}
	_, err := w.Write(buf.Bytes())
	if err != nil {
		return fmt.Errorf("write diff json: %w", err)
	}
	return nil
}

func writeDiffText(w io.Writer, oldPath, newPath string, d manifestDiff) {
	_, _ = fmt.Fprintf(w, "Manifest diff: %s -> %s\n", oldPath, newPath)

	writeEntitiesText(w, d.Entities)
	writeSetDiffText(w, "Error sentinels", d.Sentinels)
	if len(d.GenerationConfig) > 0 {
		_, _ = fmt.Fprintln(w, "\nGeneration config:")
		for _, c := range d.GenerationConfig {
			_, _ = fmt.Fprintf(w, "  ~ %s: %s -> %s\n", c.Key, c.Old, c.New)
		}
	}
}

func writeEntitiesText(w io.Writer, ed entitiesDiff) {
	if len(ed.Added) == 0 && len(ed.Removed) == 0 && len(ed.Changed) == 0 {
		return
	}
	_, _ = fmt.Fprintln(w, "\nEntities:")
	for _, t := range ed.Added {
		_, _ = fmt.Fprintf(w, "  + %s\n", t)
	}
	for _, t := range ed.Removed {
		_, _ = fmt.Fprintf(w, "  - %s\n", t)
	}
	for _, e := range ed.Changed {
		_, _ = fmt.Fprintf(w, "\n  entity %s:\n", e.Table)
		writeSetDiffText(w, "    Columns", e.Columns)
		writeSetDiffText(w, "    Methods", e.Methods)
		writeSetDiffText(w, "    Features", e.Features)
	}
}

func writeSetDiffText(w io.Writer, label string, s setDiff) {
	if s.empty() {
		return
	}
	_, _ = fmt.Fprintf(w, "%s:\n", label)
	indent := labelIndent(label) + "  "
	for _, a := range s.Added {
		_, _ = fmt.Fprintf(w, "%s+ %s\n", indent, a)
	}
	for _, r := range s.Removed {
		_, _ = fmt.Fprintf(w, "%s- %s\n", indent, r)
	}
	for _, c := range s.Changed {
		_, _ = fmt.Fprintf(w, "%s~ %s: %s -> %s\n", indent, c.Key, c.Old, c.New)
	}
}

// labelIndent returns the leading whitespace of label so nested entries line up
// under their (possibly already-indented) heading.
func labelIndent(label string) string {
	return label[:len(label)-len(strings.TrimLeft(label, " "))]
}
