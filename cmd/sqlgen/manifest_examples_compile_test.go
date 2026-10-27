package main_test

import (
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// meExamples is the slice of a manifest entity the compile probe reads: its
// name, its example lines, and — under json_layout: per_entity — the file
// that carries them.
type meExamples struct {
	Name     string `json:"name"`
	File     string `json:"file"`
	Examples *struct {
		Read  []string `json:"read"`
		Write []string `json:"write"`
	} `json:"examples"`
}

// TestManifestExampleLinesCompile type-checks every entity's examples.read /
// examples.write lines (PRD §30.4.2) against the package they describe, for
// the postgres, mysql and sqlite example goldens. Each line is pasted into a
// probe function inside a temp copy of the example module — with `client` a
// *models.Client and `ctx` in scope, as the lines assume — and the probe
// package is vetted with GOWORK=off. Between them the three examples carry
// enum-typed fields and filters, a nullable filter column and composite
// primary keys, so a literal that does not compile for any of those shapes
// fails here.
func TestManifestExampleLinesCompile(t *testing.T) {
	if testing.Short() {
		t.Skip("builds each example module; skipped in short mode")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not on PATH")
	}
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"postgres", "mysql", "sqlite"} {
		t.Run(name, func(t *testing.T) {
			exPath, err := filepath.Abs(filepath.Join(examplesDir, name))
			if err != nil {
				t.Fatal(err)
			}
			entities := meReadExamples(t, filepath.Join(exPath, "expected", "manifest"))
			if len(entities) == 0 {
				t.Fatal("the expected manifest carries no example lines")
			}

			root := t.TempDir()
			if err := os.CopyFS(root, os.DirFS(exPath)); err != nil {
				t.Fatalf("copying example %s: %v", name, err)
			}
			module := meRewriteGoMod(t, root, repo)
			src := meProbeSource(t, entities, module, meModelsImports(t, filepath.Join(root, "models")))
			probeDir := filepath.Join(root, "probe")
			if err := os.MkdirAll(probeDir, 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(probeDir, "probe.go"), []byte(src), 0o600); err != nil {
				t.Fatal(err)
			}

			cmd := exec.Command(goBin, "vet", "./probe/") //nolint:gosec // the go binary from PATH, run on a test-written temp module
			cmd.Dir = root
			cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "GOTOOLCHAIN=local")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("go vet on the example-line probe: %v\n%s\nprobe:\n%s", err, out, src)
			}
		})
	}
}

// meReadExamples returns the entities of the manifest in dir that carry
// example lines, reading per-entity files under json_layout: per_entity.
func meReadExamples(t *testing.T, dir string) []meExamples {
	t.Helper()
	var doc struct {
		Entities []meExamples `json:"entities"`
	}
	meReadJSON(t, filepath.Join(dir, "manifest_gen.json"), &doc)
	out := make([]meExamples, 0, len(doc.Entities))
	for _, e := range doc.Entities {
		if e.File != "" {
			meReadJSON(t, filepath.Join(dir, filepath.FromSlash(e.File)), &e)
		}
		if e.Examples != nil && len(e.Examples.Read)+len(e.Examples.Write) > 0 {
			out = append(out, e)
		}
	}
	return out
}

func meReadJSON(t *testing.T, file string, v any) {
	t.Helper()
	data, err := os.ReadFile(file) //nolint:gosec // a checked-in golden
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("decoding %s: %v", file, err)
	}
}

// meRewriteGoMod points the copied module's relative replace directives at the
// repo and returns the module path.
func meRewriteGoMod(t *testing.T, root, repo string) string {
	t.Helper()
	modPath := filepath.Join(root, "go.mod")
	data, err := os.ReadFile(modPath) //nolint:gosec // the temp copy's go.mod
	if err != nil {
		t.Fatal(err)
	}
	mod := strings.ReplaceAll(string(data), "=> ../../../../..", "=> "+filepath.ToSlash(repo))
	if err := os.WriteFile(modPath, []byte(mod), 0o600); err != nil { //nolint:gosec // the temp copy's go.mod under t.TempDir
		t.Fatal(err)
	}
	first, _, _ := strings.Cut(mod, "\n")
	module, ok := strings.CutPrefix(first, "module ")
	if !ok {
		t.Fatalf("go.mod does not open with a module directive: %q", first)
	}
	return strings.TrimSpace(module)
}

var meVersionSuffix = regexp.MustCompile(`^v[0-9]+$`)

// meModelsImports maps each package name the generated models package
// imports to its path — the packages an example line may select from, with
// the same UUID / decimal library the models were generated against.
func meModelsImports(t *testing.T, modelsDir string) map[string]string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(modelsDir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	imports := map[string]string{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parsing %s: %v", f, err)
		}
		for _, spec := range file.Imports {
			p, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			name := path.Base(p)
			if meVersionSuffix.MatchString(name) {
				name = path.Base(path.Dir(p))
			}
			if spec.Name != nil {
				name = spec.Name.Name
			}
			imports[name] = p
		}
	}
	return imports
}

var meSelector = regexp.MustCompile(`\b([a-z][a-z0-9]*)\.`)

// meProbeSource renders the probe package: one function per entity, each
// example line in its own block with the names it declares consumed.
func meProbeSource(t *testing.T, entities []meExamples, module string, modelsImports map[string]string) string {
	t.Helper()
	used := map[string]string{"context": "context", "models": module + "/models"}
	var body strings.Builder
	for _, e := range entities {
		fmt.Fprintf(&body, "func probe%s(ctx context.Context, client *models.Client) {\n", e.Name)
		for _, line := range slices.Concat(e.Examples.Read, e.Examples.Write) {
			lhs, _, ok := strings.Cut(line, " := ")
			if !ok {
				t.Fatalf("%s example line declares nothing: %q", e.Name, line)
			}
			blanks := strings.Repeat("_, ", strings.Count(lhs, ",")) + "_"
			fmt.Fprintf(&body, "\t{\n\t\t%s\n\t\t%s = %s\n\t}\n", line, blanks, lhs)
			for _, m := range meSelector.FindAllStringSubmatch(line, -1) {
				if p, ok := modelsImports[m[1]]; ok {
					used[m[1]] = p
				}
			}
		}
		body.WriteString("}\n\n")
	}
	names := make([]string, 0, len(used))
	for name := range used {
		names = append(names, name)
	}
	slices.Sort(names)
	var src strings.Builder
	src.WriteString("package probe\n\nimport (\n")
	for _, name := range names {
		fmt.Fprintf(&src, "\t%s %q\n", name, used[name])
	}
	src.WriteString(")\n\n")
	src.WriteString(body.String())
	return src.String()
}
