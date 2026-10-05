package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/template"
	"unicode"
)

// Sentinel-marker contract (MCP.md §3.4): the three x-sqlgen-* extension
// fields distinguish sqlgen-owned entries from consumer-authored ones, key the
// merge match, and record the emitting generator version. Standard MCP clients
// ignore unknown x-* fields, so they are pure metadata for the merge logic.
const (
	sentinelManaged          = "x-sqlgen-managed"
	sentinelManifestPath     = "x-sqlgen-manifest"
	sentinelGeneratorVersion = "x-sqlgen-generator-version"
)

// MergeAction selects what Merge does to the sqlgen entry in the target file.
type MergeAction int

const (
	// MergeUpsert creates or updates the sqlgen-managed entry (MCP.md §3.4).
	MergeUpsert MergeAction = iota
	// MergeRemove removes the sqlgen-managed entry for the current manifest;
	// the file is deleted when mcpServers empties and no other top-level
	// fields remain (MCP.md §3.4 opt-out).
	MergeRemove
)

// MergeOptions parameterizes one Merge call against a single project-local
// MCP config file (MCP.md §6.6). The pipeline caller loops over
// manifest.mcp.project_configs and invokes Merge once per target.
type MergeOptions struct {
	// ConfigPath is the target file (.mcp.json, .cursor/mcp.json, ...).
	// Relative paths resolve against ModuleRoot.
	ConfigPath string
	// ModuleRoot anchors relative paths (ConfigPath, ManifestPath, and the
	// x-sqlgen-manifest values read back from existing entries). Empty means
	// "." — the directory sqlgen runs in, the same anchor output.dir assumes.
	ModuleRoot string
	// ServerKey is the preferred key under mcpServers ("sqlgen" by default).
	ServerKey string
	// ManifestPath is the manifest the emitted entry serves, absolute or
	// module-root-relative; it is stored on the entry and is the merge match
	// key (absolute-path comparison).
	ManifestPath string
	// CommandTemplate optionally overrides the default
	// `sqlgen mcp serve --manifest <path>` command shape; rendered via
	// text/template with {{ manifest_path }} then shell-style tokenized.
	CommandTemplate string
	// Version is the current sqlgen version, recorded as
	// x-sqlgen-generator-version.
	Version string
	// Action selects upsert vs remove.
	Action MergeAction
}

// Merge applies the MCP.md §3.4 merge contract to a single project-local MCP
// config file: it creates the file when absent (upsert only), matches the
// sqlgen-managed entry by absolute manifest path, updates it in place
// (preserving key and position), appends under a free key on an unmanaged
// collision, sweeps stale sqlgen entries whose manifest no longer exists, and
// never touches consumer-authored or other-tool entries. Output is canonical
// (recursive alpha key-sort, 2-space indent) and written atomically
// (tmp → fsync → rename).
//
// The returned warnings are for the caller to print to stderr (the
// unmanaged-collision warning per §6.6).
func Merge(opts MergeOptions) ([]string, error) {
	cfgPath := resolveAgainstRoot(opts.ModuleRoot, opts.ConfigPath)
	raw, err := os.ReadFile(cfgPath) //nolint:gosec // path comes from validated project config
	if errors.Is(err, fs.ErrNotExist) {
		if opts.Action == MergeRemove {
			return nil, nil
		}
		entry, err := buildEntry(opts)
		if err != nil {
			return nil, err
		}
		root := map[string]any{"mcpServers": map[string]any{opts.ServerKey: entry}}
		return nil, writeAtomic(cfgPath, root)
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", opts.ConfigPath, err)
	}

	// UseNumber keeps sibling entries' numeric values in their exact source
	// form across the re-encode — §3.4 promises other tools' entries are
	// never touched, and a float64 round-trip could rewrite large integers.
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var root map[string]any
	if err := dec.Decode(&root); err != nil {
		return nil, invalidJSONError(opts.ConfigPath, raw, err)
	}
	if root == nil {
		// A literal `null` document parses cleanly into a nil map; treat it
		// like an empty object so the upsert below can populate it.
		root = map[string]any{}
	}
	servers, err := serversMap(root, opts.ConfigPath)
	if err != nil {
		return nil, err
	}

	manifestAbs := resolveAgainstRoot(opts.ModuleRoot, opts.ManifestPath)
	changed := sweepStaleEntries(servers, opts.ModuleRoot, manifestAbs)

	var warnings []string
	if opts.Action == MergeUpsert {
		w, err := applyUpsert(servers, opts, manifestAbs)
		if err != nil {
			return w, err
		}
		warnings = w
		changed = true
	} else {
		changed = applyRemove(servers, opts, manifestAbs) || changed
	}

	// Never rewrite (and canonicalize) a file we did not actually change —
	// a removal that matched nothing must leave a consumer-authored file
	// byte-untouched.
	if !changed {
		return warnings, nil
	}
	if len(servers) == 0 && len(root) == 1 {
		if err := os.Remove(cfgPath); err != nil {
			return warnings, fmt.Errorf("removing empty %s: %w", opts.ConfigPath, err)
		}
		return warnings, nil
	}
	return warnings, writeAtomic(cfgPath, root)
}

// applyUpsert creates or refreshes the sqlgen-managed entry: an entry
// matching the current manifest is updated in place (key and position
// preserved); otherwise the entry lands under the preferred key, or the next
// free key on a collision (warning when the occupant is consumer-authored).
func applyUpsert(servers map[string]any, opts MergeOptions, manifestAbs string) ([]string, error) {
	var warnings []string
	matched := findMatchingEntry(servers, opts.ModuleRoot, manifestAbs)
	if matched == "" {
		if w := checkUnmanagedCollision(servers, opts); w != "" {
			warnings = append(warnings, w)
		}
		matched = pickFreeKey(servers, opts.ServerKey)
	}
	entry, err := buildEntry(opts)
	if err != nil {
		return warnings, err
	}
	servers[matched] = entry
	return warnings, nil
}

// applyRemove deletes every sqlgen-managed entry serving the current
// manifest, reporting whether anything was removed.
func applyRemove(servers map[string]any, opts MergeOptions, manifestAbs string) bool {
	changed := false
	for _, k := range slices.Sorted(maps.Keys(servers)) {
		e, ok := servers[k].(map[string]any)
		if !ok || !isManaged(e) {
			continue
		}
		p, _ := e[sentinelManifestPath].(string)
		if resolveAgainstRoot(opts.ModuleRoot, p) == manifestAbs {
			delete(servers, k)
			changed = true
		}
	}
	return changed
}

// resolveAgainstRoot resolves a possibly-relative path against the module
// root (defaulting to "."), returning a cleaned absolute path for comparison.
func resolveAgainstRoot(moduleRoot, path string) string {
	if !filepath.IsAbs(path) {
		root := moduleRoot
		if root == "" {
			root = "."
		}
		path = filepath.Join(root, path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		// filepath.Abs only fails when the CWD is unreadable; fall back to
		// the joined path so comparisons stay deterministic.
		return filepath.Clean(path)
	}
	return abs
}

// serversMap returns root's mcpServers object, installing an empty one when
// the field is absent. A present-but-non-object mcpServers is a malformed
// config: fail loudly rather than clobber it (MCP.md §3.4 invalid-JSON rule).
func serversMap(root map[string]any, configPath string) (map[string]any, error) {
	v, exists := root["mcpServers"]
	if !exists {
		servers := map[string]any{}
		root["mcpServers"] = servers
		return servers, nil
	}
	servers, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("parse %s: mcpServers is not an object (refusing to overwrite)", configPath)
	}
	return servers, nil
}

// isManaged reports whether an entry carries the x-sqlgen-managed: true
// sentinel.
func isManaged(entry map[string]any) bool {
	managed, _ := entry[sentinelManaged].(bool)
	return managed
}

// findMatchingEntry returns the key of the sqlgen-managed entry whose
// x-sqlgen-manifest resolves to manifestAbs, or "" when none matches. Keys are
// scanned in sorted order so a (pathological) multi-match resolves
// deterministically.
func findMatchingEntry(servers map[string]any, moduleRoot, manifestAbs string) string {
	for _, k := range slices.Sorted(maps.Keys(servers)) {
		e, ok := servers[k].(map[string]any)
		if !ok || !isManaged(e) {
			continue
		}
		candidate, _ := e[sentinelManifestPath].(string)
		if candidate != "" && resolveAgainstRoot(moduleRoot, candidate) == manifestAbs {
			return k
		}
	}
	return ""
}

// sweepStaleEntries removes sqlgen-managed entries whose manifest file no
// longer exists on disk (MCP.md §3.4: output.dir moved between regenerations).
// The existence check is what distinguishes a stale entry from a live
// multi-package monorepo sibling — a sibling's manifest is on disk, so it
// survives. The entry matching the current manifest is exempt (it is about to
// be upserted or removed explicitly). Reports whether anything was removed.
func sweepStaleEntries(servers map[string]any, moduleRoot, manifestAbs string) bool {
	changed := false
	for _, k := range slices.Sorted(maps.Keys(servers)) {
		e, ok := servers[k].(map[string]any)
		if !ok || !isManaged(e) {
			continue
		}
		p, _ := e[sentinelManifestPath].(string)
		pAbs := resolveAgainstRoot(moduleRoot, p)
		if p == "" || pAbs == manifestAbs {
			continue
		}
		if _, err := os.Stat(pAbs); errors.Is(err, fs.ErrNotExist) {
			delete(servers, k)
			changed = true
		}
	}
	return changed
}

// checkUnmanagedCollision returns the §6.6 warning when the preferred server
// key is taken by an entry without the x-sqlgen-managed marker — a
// consumer-authored entry sqlgen must not overwrite. A managed occupant (a
// monorepo sibling serving a different manifest) is not a collision worth
// warning about; pickFreeKey just walks past it.
func checkUnmanagedCollision(servers map[string]any, opts MergeOptions) string {
	e, ok := servers[opts.ServerKey].(map[string]any)
	if !ok || isManaged(e) {
		return ""
	}
	free := pickFreeKey(servers, opts.ServerKey)
	return fmt.Sprintf("found unmanaged %q entry in %s; sqlgen will register as %q to avoid overwriting your entry (add %q: true to let sqlgen manage your existing entry)",
		opts.ServerKey, opts.ConfigPath, free, sentinelManaged)
}

// pickFreeKey returns base when unused, else base-2, base-3, ... (§6.6).
func pickFreeKey(servers map[string]any, base string) string {
	if _, taken := servers[base]; !taken {
		return base
	}
	for i := 2; ; i++ {
		key := fmt.Sprintf("%s-%d", base, i)
		if _, taken := servers[key]; !taken {
			return key
		}
	}
}

// buildEntry assembles the emitted entry shape per MCP.md §3.4: command +
// args (default `sqlgen mcp serve --manifest <path>`, or the rendered
// CommandTemplate) plus the three sentinel markers.
func buildEntry(opts MergeOptions) (map[string]any, error) {
	stored := storedManifestPath(opts.ManifestPath)
	command := "sqlgen"
	args := []string{"mcp", "serve", "--manifest", stored}
	if opts.CommandTemplate != "" {
		tokens, err := renderCommandTemplate(opts.CommandTemplate, stored)
		if err != nil {
			return nil, err
		}
		command = tokens[0]
		args = tokens[1:]
	}
	anyArgs := make([]any, len(args))
	for i, a := range args {
		anyArgs[i] = a
	}
	return map[string]any{
		"command":                command,
		"args":                   anyArgs,
		sentinelManaged:          true,
		sentinelManifestPath:     stored,
		sentinelGeneratorVersion: opts.Version,
	}, nil
}

// storedManifestPath normalizes the manifest path stored on the entry:
// absolute paths verbatim (the Claude Desktop shape), relative paths cleaned,
// slash-separated, and "./"-prefixed (the committed-file shape from §3.4's
// example, resolvable from the module root the agent client launches in).
func storedManifestPath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return "./" + filepath.ToSlash(filepath.Clean(path))
}

// renderCommandTemplate renders the command_template via text/template with
// the {{ manifest_path }} substitution, then splits the result with
// shell-style tokenization (no shell — just whitespace splitting with quote
// handling, §6.6).
func renderCommandTemplate(tmpl, manifestPath string) ([]string, error) {
	t, err := template.New("command_template").Funcs(template.FuncMap{
		"manifest_path": func() string { return manifestPath },
	}).Parse(tmpl)
	if err != nil {
		return nil, fmt.Errorf("parsing mcp.command_template: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, nil); err != nil {
		return nil, fmt.Errorf("rendering mcp.command_template: %w", err)
	}
	tokens, err := tokenizeCommand(buf.String())
	if err != nil {
		return nil, fmt.Errorf("tokenizing mcp.command_template: %w", err)
	}
	if len(tokens) == 0 {
		return nil, fmt.Errorf("mcp.command_template rendered to an empty command")
	}
	return tokens, nil
}

// tokenizeCommand splits a rendered command line into tokens: whitespace
// separates tokens; single or double quotes group whitespace into one token.
// No variable expansion, no escapes — this is deliberately not a shell.
func tokenizeCommand(s string) ([]string, error) {
	var tokens []string
	var cur strings.Builder
	var quote rune
	inToken := false
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
			inToken = true
		case unicode.IsSpace(r):
			if inToken {
				tokens = append(tokens, cur.String())
				cur.Reset()
				inToken = false
			}
		default:
			cur.WriteRune(r)
			inToken = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unbalanced %q quote", quote)
	}
	if inToken {
		tokens = append(tokens, cur.String())
	}
	return tokens, nil
}

// invalidJSONError wraps a JSON parse failure with line/col context (§3.4:
// "fail loudly with line/col; never overwrite silently").
func invalidJSONError(configPath string, raw []byte, err error) error {
	var offset int64 = -1
	if syn, ok := errors.AsType[*json.SyntaxError](err); ok {
		offset = syn.Offset
	} else if typ, ok := errors.AsType[*json.UnmarshalTypeError](err); ok {
		offset = typ.Offset
	}
	if offset >= 0 {
		line, col := offsetLineCol(raw, offset)
		return fmt.Errorf("parse %s:%d:%d: %w (refusing to overwrite invalid JSON)", configPath, line, col, err)
	}
	return fmt.Errorf("parse %s: %w (refusing to overwrite invalid JSON)", configPath, err)
}

// offsetLineCol converts a byte offset into 1-based line and column numbers.
func offsetLineCol(raw []byte, offset int64) (line, col int) {
	if offset > int64(len(raw)) {
		offset = int64(len(raw))
	}
	prefix := raw[:offset]
	line = bytes.Count(prefix, []byte("\n")) + 1
	col = int(offset) - bytes.LastIndexByte(prefix, '\n')
	return line, col
}

// writeAtomic marshals root canonically (encoding/json sorts map keys at
// every level; 2-space indent; HTML escaping off) and writes it atomically:
// tmp file → fsync → rename. A failure leaves the target untouched and cleans
// up the tmp file.
func writeAtomic(path string, root map[string]any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(root); err != nil {
		return fmt.Errorf("encoding %s: %w", path, err)
	}

	// A nested target like .cursor/mcp.json may be the first file in its
	// directory (§3.4 dual-target pattern) — create the parent as needed.
	// 0o750 dirs / 0o600 files matches the codegen file-permission convention.
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("creating directory for %s: %w", path, err)
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) //nolint:gosec // tmp path derives from the validated project-config target
	if err != nil {
		return fmt.Errorf("writing %s: %w (is the directory writable?)", tmp, err)
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("writing %s: %w", tmp, err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("syncing %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("closing %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replacing %s: %w (is the path read-only?)", path, err)
	}
	return nil
}
