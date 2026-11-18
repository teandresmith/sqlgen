package gen

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/template"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// apiLimitsFileName is the regenerated query-limits file under
// sqlgenresolver/. It carries the configured limits, so editing
// api.graphql.max_depth / max_complexity reaches the server on the next run
// even though server.go itself is written only once.
const apiLimitsFileName = "limits_gen.go"

// apiServerFileName is the one-shot server bootstrap in the graph package
// (PRD §26.5.6): written only when missing, consumer-owned afterwards.
const apiServerFileName = "server.go"

// APILimitsContext feeds api/limits (PRD §26.6).
type APILimitsContext struct {
	MaxDepth      int
	MaxComplexity int
	// RelationshipFields lists every relationship field on the GraphQL surface
	// as "<ObjectType>.<field>", sorted. Only these fields add to an
	// operation's depth.
	RelationshipFields []string
}

// APIServerContext feeds api/server.
type APIServerContext struct {
	ModelsPackage string
	ClientName    string
	// HasMutations mirrors the resolver scaffold, which declares the M field
	// only when sqlgen manages at least one Mutation field.
	HasMutations bool
}

// buildAPILimitsContext collects the configured limits and the relationship
// fields the depth walker counts.
func buildAPILimitsContext(apiCtx *APIContext, g *config.GraphQLAPIConfig) APILimitsContext {
	var fields []string
	for _, t := range apiCtx.Tables {
		for _, r := range t.Relationships {
			fields = append(fields, t.StructName+"."+r.GraphQLName)
		}
	}
	slices.Sort(fields)
	return APILimitsContext{
		MaxDepth:           g.MaxDepth,
		MaxComplexity:      g.MaxComplexity,
		RelationshipFields: slices.Compact(fields),
	}
}

// generateAPILimitsFile emits sqlgenresolver/limits_gen.go: the configured
// query depth and complexity limits as gqlgen server configuration (PRD §26.6).
func generateAPILimitsFile(tmpl *template.Template, apiCtx *APIContext, g *config.GraphQLAPIConfig, sqlgenResolverDir, version string) (string, error) {
	path := filepath.Join(sqlgenResolverDir, apiLimitsFileName)
	imps := []string{
		"context",
		"github.com/99designs/gqlgen/graphql",
		"github.com/99designs/gqlgen/graphql/errcode",
		"github.com/99designs/gqlgen/graphql/handler",
		"github.com/99designs/gqlgen/graphql/handler/extension",
		"github.com/vektah/gqlparser/v2/ast",
		"github.com/vektah/gqlparser/v2/gqlerror",
	}
	if err := renderAndWrite(tmpl, "api/limits", buildAPILimitsContext(apiCtx, g), sqlgenResolverPackageName, imps, path, version); err != nil {
		return "", fmt.Errorf("generating api limits: %w", err)
	}
	return path, nil
}

// generateAPIServerFile writes graph/server.go when the consumer's tree has
// none, and returns "" when it already exists. Existence is checked at the
// configured path, not the write path, so a redirected run (sqlgen diff)
// never proposes a server.go the consumer already owns.
func generateAPIServerFile(tmpl *template.Template, apiCtx *APIContext, pkg, resolverDir, configuredResolverDir string) (string, error) {
	if apiCtx.ModelsImportPath == "" || apiCtx.SqlgenResolverImportPath == "" || len(apiCtx.Tables) == 0 {
		return "", nil
	}
	if _, err := os.Stat(filepath.Join(configuredResolverDir, apiServerFileName)); err == nil {
		return "", nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("checking %s: %w", apiServerFileName, err)
	}

	_, mutations := CollectSqlgenManagedFields(apiCtx)
	body, err := executeTemplateSafe(tmpl, "api/server", APIServerContext{
		ModelsPackage: apiCtx.ModelsPackage,
		ClientName:    apiCtx.ClientName,
		HasMutations:  len(mutations) > 0,
	})
	if err != nil {
		return "", fmt.Errorf("rendering api server: %w", err)
	}

	path := filepath.Join(resolverDir, apiServerFileName)
	// No generated header: the file is consumer-owned. The import block is
	// complete, so goimports prunes but never has to resolve.
	formatted, err := processImports(buildAPIServerFile(pkg, apiCtx, body), path, false)
	if err != nil {
		return "", fmt.Errorf("formatting %s: %w", apiServerFileName, err)
	}
	if err := writeFile(path, formatted); err != nil {
		return "", err
	}
	return path, nil
}

// buildAPIServerFile assembles server.go with its full import block, the
// consumer's models package under its configured alias.
func buildAPIServerFile(pkg string, apiCtx *APIContext, body string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n\nimport (\n", pkg)
	b.WriteString("\t\"net/http\"\n\n")
	for _, imp := range []string{
		"github.com/99designs/gqlgen/graphql/handler",
		"github.com/99designs/gqlgen/graphql/handler/extension",
		"github.com/99designs/gqlgen/graphql/handler/lru",
		"github.com/99designs/gqlgen/graphql/handler/transport",
		"github.com/vektah/gqlparser/v2/ast",
	} {
		fmt.Fprintf(&b, "\t%q\n", imp)
	}
	fmt.Fprintf(&b, "\n\t%s %q\n", apiCtx.ModelsPackage, apiCtx.ModelsImportPath)
	fmt.Fprintf(&b, "\t%q\n", apiCtx.SqlgenResolverImportPath)
	b.WriteString(")\n\n")
	b.WriteString(body)
	b.WriteString("\n")
	return []byte(b.String())
}
