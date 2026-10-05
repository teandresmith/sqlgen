package wrapper

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// GenOptions configures one wrapper invocation.
type GenOptions struct {
	// GqlgenConfigPath is the path to the consumer-owned gqlgen.yml
	// (cfg.API.GraphQL.GqlgenConfig).
	GqlgenConfigPath string
	// GqlgenBin is the gqlgen invocation form (cfg.API.GraphQL.GqlgenBin).
	// Whitespace-split and exec'd directly: e.g. "go run github.com/99designs/gqlgen"
	// produces argv ["go", "run", "github.com/99designs/gqlgen"].
	GqlgenBin string
	// MergeInput holds the sqlgen-owned entries to merge into the temp config.
	MergeInput MergeInput
	// WorkingDir is the directory the gqlgen subprocess runs in. Empty means
	// the current process working directory.
	WorkingDir string
	// Stdout / Stderr forward gqlgen subprocess output. Nil falls back to the
	// parent's os.Stdout / os.Stderr.
	Stdout io.Writer
	Stderr io.Writer
	// ExtraArgs are appended after "generate --config <temp>" when invoking
	// the gqlgen binary. Reserved for forward compatibility.
	ExtraArgs []string
	// Seeds describes the per-table delegating bodies that sqlgen emits into
	// gqlgen-shaped seed files. Each entry holds the
	// table's base name (e.g. "products") and the rendered body text. The
	// wrapper writes seeds before invoking the gqlgen subprocess, only when
	// the destination file does not already exist — gqlgen's preserve-bodies
	// behavior owns subsequent updates.
	Seeds []TableSeed
	// SqlgenManagedFields is the concrete set of Query.* / Mutation.* field
	// names sqlgen owns this run (PascalCase, matching gqlgen's emitted
	// resolver-method names). The post-gqlgen panic-stub rewriter uses this
	// to convert gqlgen's `panic("not implemented…")` stubs into delegations
	// when an existing seed file gains a new sqlgen-managed operation.
	SqlgenManagedFields SqlgenManagedFieldSet
	// SeedPackage is the Go package name to emit at the top of each seed
	// file (matches the consumer's resolver package — e.g. "graph"). Read
	// from cfg.API.GraphQL.Package by the caller.
	SeedPackage string
	// SeedImports is the list of additional import paths each seed needs
	// beyond the in-package translators: the consumer's models import path
	// and the sqlgenresolver helper sub-package.
	SeedImports SeedImports
}

// TableSeed is one rendered per-table delegation body destined for the
// gqlgen-shaped seed file. The wrapper joins per-table bodies into either a
// per-schema file (follow-schema layout) or a single combined file
// (single-file layout) before writing.
type TableSeed struct {
	// SnakeName is the snake_case form of the table's struct name, used to
	// derive the seed filename (`<snake>_gen.resolvers.go`) under
	// follow-schema layout. Matches the per-table .graphqls filename stem
	// emitted by the schema generator.
	SnakeName string
	// Body is the rendered delegation block: a sequence of receiver methods
	// on *queryResolver / *mutationResolver that call into r.Q / r.M. No
	// package declaration or import block — the wrapper wraps with the
	// preamble derived from SeedPackage / SeedImports.
	Body string
}

// SeedImports enumerates the import-path metadata seed files need. Empty
// strings disable the corresponding import line — the wrapper's preamble
// builder gates each import on its presence.
type SeedImports struct {
	// ModelsAlias is the package alias for the consumer's models package
	// (typically "models"). Empty string skips the alias spelling.
	ModelsAlias string
	// ModelsImportPath is the Go import path for the consumer's generated
	// models package (e.g. "example.com/foo/gen").
	ModelsImportPath string
	// SqlgenResolverImportPath is the import path of the sqlgen-owned
	// helper sub-package (`<resolver_dir>/sqlgenresolver`). Seeds reference
	// it as the unqualified `sqlgenresolver` package.
	SqlgenResolverImportPath string
	// GqlgenModelImportPath is the Go import path for the consumer's
	// gqlgen-emitted model package — the directory derived from
	// gqlgen.yml's `model.filename` (defaults to "<module>/graph/model").
	// Seed files reference gqlgen-emitted input types (`Create<T>Input`,
	// `<T>Filter`, `StringComparator`, …) under the alias spelled in
	// GqlgenModelAlias. Empty string skips the import line — the seed
	// emitter gates emission on this field.
	GqlgenModelImportPath string
	// GqlgenModelAlias is the Go import alias used to qualify gqlgen-emitted
	// input type references in seed bodies. Always GqlgenModelImportAlias
	// (`"gqlmodel"`) when GqlgenModelImportPath is set, or empty otherwise.
	GqlgenModelAlias string
}

// SqlgenManagedFieldSet enumerates the sqlgen-managed Query.* and Mutation.*
// field names (PascalCase, matching gqlgen-emitted resolver method names) for
// one wrapper invocation. The post-gqlgen rewriter uses this to find panic
// stubs that should be converted to delegations.
type SqlgenManagedFieldSet struct {
	// QueryFields is the set of method names emitted on *queryResolver
	// (e.g. "Product", "Products", "ProductList").
	QueryFields []string
	// MutationFields is the set of method names emitted on *mutationResolver
	// (e.g. "CreateProduct", "UpdateProduct", "DeleteProduct").
	MutationFields []string
	// Owners names the entity behind each managed field, keyed
	// "Q.<Field>" / "M.<Field>". The post-gqlgen stub check reads it to name
	// the table or view, and the struct_name key that renames it, when gqlgen
	// spells a managed resolver differently. A field with no entry
	// is still checked; its error just names no entity.
	Owners map[string]ManagedFieldOwner
}

// ManagedFieldOwner identifies the entity a managed root field belongs to.
type ManagedFieldOwner struct {
	// ConfigKey is the entity's config path: "tables.<sql name>" or
	// "views.<sql name>".
	ConfigKey string
	// GraphQLName is the field as the schema declares it (e.g. "onlyPK").
	GraphQLName string
}

// GenResult holds the outcome of one wrapper invocation. Callers use the
// reported file paths to merge into the broader sqlgen generate report
// (the chained `sqlgen generate` -> `sqlgen graphql gen` path).
type GenResult struct {
	// SeedFiles is the set of resolver seed files sqlgen wrote on this run
	// (one per managed table under follow-schema layout, plus the optional
	// `resolver.go` scaffold; or one combined file under single-file
	// layout). Each entry is a path relative to the process working
	// directory or absolute, matching whatever WorkingDir was set to.
	// Files that already existed on disk are not included — sqlgen never
	// overwrites them after the first run.
	SeedFiles []string
}

// Gen executes the wrapper data flow per PRD §26.5.6:
//
//  1. Read the consumer-owned gqlgen.yml.
//  2. Merge sqlgen-owned entries into a temp config (consumer-wins on key
//     collision; the on-disk file is never touched).
//  3. Write per-schema seed delegation files when absent.
//     Existing files are gqlgen-owned after the first run — sqlgen never
//     overwrites them.
//  4. Invoke the gqlgen binary against the temp config via os/exec.
//  5. Post-process gqlgen-owned files to rewrite panic stubs for
//     sqlgen-managed fields into delegations (handles the case where an
//     existing seed file gains a new sqlgen operation), then fail if a
//     managed field is still a stub because gqlgen named its resolver
//     differently from sqlgen.
//  6. Under single-file layout, AST-merge the seeded Client / Q /
//     M fields back into the Resolver struct that gqlgen's resolvergen
//     plugin wipes on every run.
//  7. Remove the temp file on both success and subprocess failure.
//
// The on-disk gqlgen.yml is never modified.
func Gen(ctx context.Context, opts GenOptions) (*GenResult, error) {
	if opts.GqlgenConfigPath == "" {
		return nil, fmt.Errorf("gqlgen config path is empty")
	}
	if opts.GqlgenBin == "" {
		return nil, fmt.Errorf("gqlgen_bin is empty")
	}

	consumerYAML, err := os.ReadFile(filepath.Clean(opts.GqlgenConfigPath))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", opts.GqlgenConfigPath, err)
	}

	layout, err := parseResolverLayout(consumerYAML)
	if err != nil {
		return nil, fmt.Errorf("parsing gqlgen.yml resolver block: %w", err)
	}

	// Under single-file layout gqlgen's resolvergen wipes the
	// seeded Resolver struct's Client/Q/M fields, then its post-generate
	// validation runs `go build` against the wiped file and fails before
	// sqlgen's post-subprocess merge can restore the fields. Force
	// skip_validation in the temp config (consumer's on-disk file is
	// unchanged) so the subprocess returns cleanly; the consumer's
	// downstream `go build` catches any actual issues.
	mergeInput := opts.MergeInput
	if layout.Layout == layoutSingleFile {
		mergeInput.ForceSkipValidation = true
	}
	merged, err := Merge(consumerYAML, mergeInput)
	if err != nil {
		return nil, fmt.Errorf("merging gqlgen config: %w", err)
	}

	tempPath, err := writeTempConfig(merged)
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.Remove(tempPath) }()

	seeded, err := writeSeedFiles(opts, layout)
	if err != nil {
		return nil, fmt.Errorf("writing seed files: %w", err)
	}

	name, args, err := splitBin(opts.GqlgenBin)
	if err != nil {
		return nil, err
	}
	args = append(args, "generate", "--config", tempPath)
	args = append(args, opts.ExtraArgs...)

	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // gqlgen_bin is consumer-controlled config, intentionally invoked as a subprocess (PRD §26.5.6)
	cmd.Dir = opts.WorkingDir
	cmd.Stdout = resolveWriter(opts.Stdout, os.Stdout)
	cmd.Stderr = resolveWriter(opts.Stderr, os.Stderr)
	cmd.Env = subprocessEnv(name, args)

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("running gqlgen (%s): %w", opts.GqlgenBin, err)
	}

	if err := settleManagedStubs(opts, layout); err != nil {
		return nil, err
	}

	// Under single-file layout gqlgen's resolvergen wipes the
	// seeded Resolver struct's Client/Q/M fields on every run. Re-merge
	// them so the preserved `r.Q.X(...)` / `r.M.X(...)` bodies compile.
	// No-op under follow-schema (the merge for that layout runs before
	// the subprocess, in writeResolverScaffold).
	mergedPath, err := mergeSingleFileResolver(opts, layout)
	if err != nil {
		return nil, fmt.Errorf("merging single-file resolver: %w", err)
	}
	// Dedupe against the pre-subprocess seed write — writeSingleFileSeed
	// reports the same path on a first run, and the post-gqlgen merge then
	// rewrites that file in place. Reporting it twice would inflate the
	// chained-generate file count.
	if mergedPath != "" && !slices.Contains(seeded, mergedPath) {
		seeded = append(seeded, mergedPath)
	}

	return &GenResult{SeedFiles: seeded}, nil
}

// writeTempConfig writes the merged YAML to a uniquely-named temp file and
// returns its path. The caller is responsible for removing it.
func writeTempConfig(yaml []byte) (string, error) {
	f, err := os.CreateTemp("", "sqlgen-gqlgen-*.yml")
	if err != nil {
		return "", fmt.Errorf("creating temp gqlgen config: %w", err)
	}
	path := f.Name()
	if _, err := f.Write(yaml); err != nil {
		_ = f.Close()
		_ = os.Remove(path) //nolint:gosec // path was returned by os.CreateTemp under the test/process temp dir
		return "", fmt.Errorf("writing temp gqlgen config: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path) //nolint:gosec // path was returned by os.CreateTemp under the test/process temp dir
		return "", fmt.Errorf("closing temp gqlgen config: %w", err)
	}
	return path, nil
}

// splitBin splits a gqlgen_bin invocation string ("go run github.com/99designs/gqlgen"
// or "gqlgen") into a command + arg list using whitespace as the delimiter.
func splitBin(bin string) (string, []string, error) {
	fields := strings.Fields(bin)
	if len(fields) == 0 {
		return "", nil, fmt.Errorf("gqlgen_bin is empty")
	}
	return fields[0], fields[1:], nil
}

// resolveWriter returns w when non-nil, otherwise fallback.
func resolveWriter(w, fallback io.Writer) io.Writer {
	if w != nil {
		return w
	}
	return fallback
}

// subprocessEnv builds the gqlgen subprocess environment. When the invocation
// uses `go run …`, it appends `GOFLAGS=-mod=mod` so the subprocess resolves
// gqlgen's transitive build dependencies on demand instead of failing on
// missing-go.sum entries the consumer's runtime go.sum has no reason to pin.
// For non-`go run` invocations the parent's env is inherited
// unchanged, so consumer-set GOFLAGS continue to apply.
func subprocessEnv(name string, args []string) []string {
	env := os.Environ()
	if name == "go" && len(args) > 0 && args[0] == "run" {
		env = append(env, "GOFLAGS=-mod=mod")
	}
	return env
}
