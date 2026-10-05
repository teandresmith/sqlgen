// Package gen provides the code generation engine: template execution,
// context building, and file writing.
package gen

import (
	"fmt"
	"text/template"
)

// Step represents a single generation step in the pipeline.
type Step struct {
	// Name identifies the step for logging and error messages.
	Name string
	// Fn executes the generation step. It receives the shared template
	// and output configuration, and returns an error if generation fails.
	Fn func(t *template.Template, opts *Options) error
}

// Options holds the configuration for a generation run.
type Options struct {
	// Version is the sqlgen CLI version without its leading "v" (e.g.
	// "1.2.0"), or "dev"; see cli.Version.
	Version string

	// OutputDir is the base directory for generated files.
	OutputDir string

	// OriginalOutputDir is the dir used to compute Go import paths. Normally
	// equal to OutputDir; differs only when an external caller (e.g. the E2E
	// golden harness) redirects the *write* location via the env var
	// SQLGEN_OUTPUT_DIR_IMPORT_OVERRIDE while keeping the originally
	// configured dir for import-path computation. When empty, callers fall
	// back to OutputDir.
	OriginalOutputDir string

	// ProjectRoot is the filesystem anchor that root-relative GraphQL output
	// directories (api.graphql.schema_dir / resolver_dir, §26.5.8) resolve
	// against for *writes*. It is the module root sqlgen runs in — normally
	// "." (the current working directory), the same anchor output.dir writes
	// assume. Kept distinct from OutputDir so the graph package is not pinned
	// under output.dir: a top-level `schema_dir: graph` lands at
	// <ProjectRoot>/graph, a sibling of the models tree. Graph import paths do
	// not use this field — they join the (already root-relative) dir onto the
	// module path directly, so they stay stable regardless of where writes are
	// physically redirected.
	ProjectRoot string

	// WriteRoot, when non-empty, redirects every directory this run writes
	// into underneath it — the models output dir and the GraphQL schema /
	// resolver dirs alike — so the run touches nothing in the working tree.
	// It is the single knob for that redirect: OutputDir and the resolved
	// graph dirs are already SandboxPath'd by the time emission sees them,
	// so a new write path cannot opt out of it by accident.
	//
	// It is deliberately not an input to any import path or rendered body.
	// OriginalOutputDir keeps naming the configured dir, and graph import
	// paths join the (root-relative) configured dir onto the module path, so
	// a redirected run emits files byte-identical to a real one. `sqlgen
	// diff` depends on exactly that: it compares a redirected run against the
	// working tree, which is only meaningful if redirection cannot change
	// what would be written (PRD §23.6, "without writing anything").
	WriteRoot string

	// Package is the Go package name for generated files.
	Package string
}

// Run executes all generation steps in the fixed sequence.
// Steps are executed in order; if any step fails, Run returns
// immediately with the step name in the error context.
func Run(steps []Step, tmpl *template.Template, opts *Options) error {
	for _, s := range steps {
		if err := s.Fn(tmpl, opts); err != nil {
			return fmt.Errorf("generation step %s: %w", s.Name, err)
		}
	}
	return nil
}

// DefaultSteps returns the generation steps in the fixed order defined
// by the PRD (Section 8.1.1). Each step is a placeholder that will be
// wired to real generators as templates are implemented.
//
// Order:
//  1. Enums
//  2. Types (composite, domain, extra)
//  3. Errors
//  4. Table name constants
//  5. Tables (models, clients, filters, field options, inputs)
//  6. Sorters
//  7. Pagination types
//  8. Connection types (cursor pagination)
//  9. Views
//  10. Unified client
func DefaultSteps() []Step {
	return []Step{
		{Name: "enums"},
		{Name: "types"},
		{Name: "errors"},
		{Name: "tablenames"},
		{Name: "tables"},
		{Name: "sorters"},
		{Name: "pagination"},
		{Name: "connections"},
		{Name: "views"},
		{Name: "client"},
	}
}
