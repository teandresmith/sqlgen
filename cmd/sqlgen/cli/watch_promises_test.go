package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// tstMCPImportPath is the package that holds the one file-watching behavior
// sqlgen ships: mcp serve's manifest reload.
const tstMCPImportPath = "github.com/teandresmith/sqlgen/cmd/sqlgen/mcp"

// tstFileWatcherImports are import-path fragments of the Go file-watching
// libraries; any package importing one of them watches files.
var tstFileWatcherImports = []string{"fsnotify", "rjeczalik/notify", "radovskyb/watcher"}

// tstWatchFlags walks the command tree under cmd and returns, per command
// path, the sorted names of every flag it accepts (local, persistent or
// inherited) that mentions watching.
func tstWatchFlags(cmd *cobra.Command, into map[string][]string) {
	seen := map[string]bool{}
	collect := func(f *pflag.Flag) {
		if strings.Contains(strings.ToLower(f.Name), "watch") && !seen[f.Name] {
			seen[f.Name] = true
			into[cmd.CommandPath()] = append(into[cmd.CommandPath()], f.Name)
		}
	}
	cmd.Flags().VisitAll(collect)
	cmd.PersistentFlags().VisitAll(collect)
	cmd.InheritedFlags().VisitAll(collect)
	slices.Sort(into[cmd.CommandPath()])
	for _, sub := range cmd.Commands() {
		tstWatchFlags(sub, into)
	}
}

// TestNoCommandRegeneratesOnFileChange pins that sqlgen has no watch mode
// for code generation: the one file-watching behavior is mcp serve's manifest
// reload. It checks this three ways, each of which a watch mode added to
// any other command would have to break:
//   - across the whole command tree, only mcp serve accepts a watch flag;
//   - in the binary's dependency graph, the only package importing a
//     file-watching library is the mcp package;
//   - within this package, the only file that starts the mcp server (the
//     only route into that watcher) is the mcp command's file.
func TestNoCommandRegeneratesOnFileChange(t *testing.T) {
	t.Run("only mcp serve accepts a watch flag", func(t *testing.T) {
		got := map[string][]string{}
		tstWatchFlags(NewRootCmd(), got)
		want := map[string][]string{"sqlgen mcp serve": {"no-watch", "watch"}}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("commands accepting a watch flag (-want +got):\n%s", diff)
		}
	})

	t.Run("only the mcp package imports a file watcher", func(t *testing.T) {
		cmd := exec.Command("go", "list", "-deps", "-f", `{{.ImportPath}}{{range .Imports}} {{.}}{{end}}`, ".")
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("go list -deps: %v", err)
		}
		var importers []string
		sawMCP := false
		for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 0 {
				continue
			}
			pkg := fields[0]
			if pkg == tstMCPImportPath {
				sawMCP = true
			}
			if tstIsFileWatcher(pkg) {
				continue // the library's own internals
			}
			if slices.ContainsFunc(fields[1:], tstIsFileWatcher) {
				importers = append(importers, pkg)
			}
		}
		if !sawMCP {
			t.Fatalf("go list -deps output does not include %s; the check is not looking at the cli package graph:\n%s", tstMCPImportPath, out)
		}
		if diff := cmp.Diff([]string{tstMCPImportPath}, importers); diff != "" {
			t.Errorf("packages importing a file-watching library (-want +got):\n%s", diff)
		}
	})

	t.Run("only the mcp command starts the mcp server", func(t *testing.T) {
		files, err := filepath.Glob("*.go")
		if err != nil {
			t.Fatalf("listing package files: %v", err)
		}
		fset := token.NewFileSet()
		var callers []string
		parsed := 0
		for _, name := range files {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			src, err := os.ReadFile(name) //nolint:gosec // this package's own source
			if err != nil {
				t.Fatalf("reading %s: %v", name, err)
			}
			f, err := parser.ParseFile(fset, name, src, parser.ImportsOnly|parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parsing %s imports: %v", name, err)
			}
			for _, imp := range f.Imports {
				path, err := strconv.Unquote(imp.Path.Value)
				if err != nil {
					t.Fatalf("%s: unquoting import %s: %v", name, imp.Path.Value, err)
				}
				if tstIsFileWatcher(path) {
					t.Errorf("%s imports file-watching library %s directly", name, path)
				}
			}
			f, err = parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parsing %s: %v", name, err)
			}
			parsed++
			local := tstImportName(f, tstMCPImportPath, "mcp")
			if local == "" {
				continue
			}
			ast.Inspect(f, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == local && (sel.Sel.Name == "Run" || sel.Sel.Name == "NewWatcher") {
					callers = append(callers, name+": "+local+"."+sel.Sel.Name)
				}
				return true
			})
		}
		if parsed == 0 {
			t.Fatal("found no non-test Go files in the package directory")
		}
		want := []string{"cmd_mcp.go: mcp.Run"}
		if diff := cmp.Diff(want, callers); diff != "" {
			t.Errorf("references to the mcp server or its watcher (-want +got):\n%s", diff)
		}
	})
}

func tstIsFileWatcher(importPath string) bool {
	return slices.ContainsFunc(tstFileWatcherImports, func(frag string) bool {
		return strings.Contains(importPath, frag)
	})
}

// tstImportName returns the name f refers to importPath by, or "" when f
// does not import it.
func tstImportName(f *ast.File, importPath, defaultName string) string {
	for _, imp := range f.Imports {
		if path, err := strconv.Unquote(imp.Path.Value); err != nil || path != importPath {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		}
		return defaultName
	}
	return ""
}
