package wrapper

import (
	"fmt"
	"os"
)

// InitConfigContent is the starter gqlgen.yml that Init writes when the
// target path does not yet exist. The schema glob points at the directory
// where sqlgen emits its generated *_gen.graphqls files; consumer-authored
// schema files placed alongside are picked up by the same glob.
const InitConfigContent = `# gqlgen.yml — owned by the consumer.
# sqlgen merges its required entries into a temp config at wrapper-invocation
# time; the on-disk file is never modified by 'sqlgen graphql gen'.
#
# resolver wiring:
#   sqlgen emits per-table query/mutation logic into a sub-package at
#   <resolver_dir>/sqlgenresolver/. Seed files in <resolver_dir> delegate
#   gqlgen's resolver methods into r.Q.<Field> / r.M.<Field>. gqlgen's own
#   one-shot scaffold writes <resolver_dir>/resolver.go with the empty
#   Resolver struct; AFTER the gqlgen subprocess returns, sqlgen AST-merges
#   the Client / Q / M fields plus the sqlgenresolver import so the seeds
#   compile against the consumer's package:
#
#     type Resolver struct {
#         Client *<modelspkg>.Client
#         Q      *sqlgenresolver.Q
#         M      *sqlgenresolver.M
#     }
#
#   Initialize all three at app boot before serving traffic. Add additional
#   dependency fields (loggers, auth clients, etc.) below the Q/M lines as
#   your code requires — sqlgen's merge step preserves consumer-added
#   fields, methods, and imports verbatim across regenerations.
#
# go.sum requirement:
#   The default 'gqlgen_bin: go run github.com/99designs/gqlgen' invocation
#   pulls gqlgen's transitive build dependencies (golang.org/x/tools/go/packages,
#   golang.org/x/text/cases, github.com/urfave/cli/v2, github.com/agnivade/levenshtein,
#   …) which the consumer's runtime go.sum does not pin. sqlgen sets
#   GOFLAGS=-mod=mod on the subprocess so 'go run' resolves them on demand;
#   no consumer action is required for the default invocation.
#
#   If you prefer to pin the build deps in your go.sum (e.g. for hermetic builds
#   in CI), record gqlgen as a tool dependency of your module:
#
#     go get -tool github.com/99designs/gqlgen
#
#   That adds a 'tool' directive to go.mod and populates the go.sum entries
#   gqlgen needs at build time, and you can drop the GOFLAGS override.
#
# models.<Type>.fields.<field>.fieldName (sqlgen-authored):
#   sqlgen writes a fieldName entry into the temp config for every Go
#   identifier its generated code references on a type gqlgen generates —
#   the filter and input types. That is how the two generators agree on a
#   spelling instead of each deriving one and hoping they match. gqlgen names
#   the Query/Mutation resolver methods itself; when its spelling differs from
#   sqlgen's, 'sqlgen graphql gen' fails and names the table to rename.
#
#   These merge per field, so an entry you write here for the same field wins
#   and is preserved. Renaming a field sqlgen's own translators reference will
#   not compile, though: they emit the name sqlgen chose.

schema:
  - "graph/*.graphqls"

exec:
  filename: graph/generated_gen.go
  package: graph

model:
  filename: graph/model/models_gen.go
  package: model

resolver:
  layout: follow-schema
  dir: graph
  package: graph
  filename_template: "{name}.resolvers.go"
`

// Init writes the starter gqlgen.yml at path if and only if no file exists
// there already. When the file is present, Init is a no-op (no overwrite,
// no error) — the consumer is the canonical owner of gqlgen.yml.
func Init(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("checking %s: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(InitConfigContent), 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
