package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/parser/mysql"
	"github.com/teandresmith/sqlgen/parser/postgres"
	"github.com/teandresmith/sqlgen/parser/sqlite"
)

// loadAndValidate runs the shared config → validate → parse → post-validate →
// resolve PK overrides → detect relationships pipeline. On error it writes
// diagnostics to errOut and returns an *exitError.
//
// Post-parse validation runs against the *raw* schema, before
// applyPrimaryKeyOverrides — see that function for why the order is
// load-bearing in both directions.
func loadAndValidate(flags *cliFlags, errOut io.Writer) (*config.RootConfig, *parser.Schema, error) {
	cfg, err := config.LoadConfig(flags.config)
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "Error: %v\n", err)
		if flags.config == "" {
			_, _ = fmt.Fprintln(errOut, "Hint: run 'sqlgen init' to create a config file")
		}
		return nil, nil, &exitError{code: ExitConfig, err: err}
	}

	warnings, err := config.ValidatePreParse(cfg)
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "Config validation error: %v\n", err)
		return nil, nil, &exitError{code: ExitConfig, err: err}
	}
	printWarnings(errOut, warnings, flags.quiet)

	schema, err := parseSchema(cfg)
	if err != nil {
		code := ExitSchema
		if cfg.Input.Source == config.SourceDatabase || cfg.Input.Source == config.SourceBoth {
			code = ExitConnection
		}
		printSchemaError(errOut, err)
		return nil, nil, &exitError{code: code, err: err}
	}
	// Statements the parser skipped, e.g. CREATE VIEW in DDL (PRD §16).
	printManifestWarnings(errOut, schema.Warnings, flags.quiet)

	schemaTables := toSchemaTables(schema)
	schemaViews := toSchemaViews(schema)
	postWarnings, err := config.ValidatePostParse(cfg, schemaTables, schemaViews)
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "Post-parse validation error: %v\n", err)
		return nil, nil, &exitError{code: ExitConfig, err: err}
	}
	printWarnings(errOut, postWarnings, flags.quiet)

	applyPrimaryKeyOverrides(schema, cfg)
	parser.DetectRelationships(schema)

	return cfg, schema, nil
}

// applyPrimaryKeyOverrides resolves every tables.<name>.primary_key.columns
// override onto parser.Column.PrimaryKey, so the resolved primary key is a
// property of the schema rather than a projection only the generator can see.
//
// PRD §8.6 specifies that an override is "functionally identical to a real
// PRIMARY KEY constraint in the schema". Before this ran, that held only for
// consumers reading gen's ColumnContext: gen re-derived the PK set in
// buildTableColumns, while parser.DetectRelationships — which runs earlier, on
// the raw schema — still saw the auto-detected flags. A single-column override
// on a foreign-key column therefore got `caller` from gen (correct, §8.6) and
// O2M from the parser (wrong, §13.1): the parent grew a `[]*Child` list for
// something that structurally holds at most one row per parent.
//
// Both layers read the same config field, so they cannot disagree on content;
// gen's buildTableColumns branch stays because it also orders pkColumns, which
// is load-bearing for composite cache keys and <Table>PK field order, and
// because gen.Generate is a public entry point reachable without this pipeline.
//
// Ordering is load-bearing on both sides and is not enforced by the type system:
//
//   - It must run AFTER config.ValidatePostParse. That pass reads the raw
//     PK flags through toSchemaTables, and uniqueCovers treats a PrimaryKey
//     column as self-covering. Resolving first would make every single-column
//     override satisfy its own UNIQUE-coverage check and silently drop the
//     §8.6 warning that exists for exactly that case.
//   - It must run BEFORE parser.DetectRelationships, which is the consumer
//     this exists to correct.
//
// Columns named by the override are matched against the post-exclude_columns
// set, mirroring gen.buildTableColumns: a name that is excluded or absent
// contributes nothing, which is the shape validatePrimaryKeyOverride already
// hard-errors on.
func applyPrimaryKeyOverrides(schema *parser.Schema, cfg *config.RootConfig) {
	if schema == nil || cfg == nil {
		return
	}
	for i := range schema.Tables {
		table := &schema.Tables[i]
		tableCfg, ok := lookupTableConfig(cfg, table)
		if !ok || tableCfg.PrimaryKey == nil || len(tableCfg.PrimaryKey.Columns) == 0 {
			continue
		}

		excluded := make(map[string]bool)
		for _, name := range config.ResolveTableExcludeColumns(tableCfg, cfg.Generation) {
			excluded[name] = true
		}

		override := make(map[string]bool, len(tableCfg.PrimaryKey.Columns))
		for _, name := range tableCfg.PrimaryKey.Columns {
			if excluded[name] || parser.ColumnByName(table, name) == nil {
				continue
			}
			override[name] = true
		}

		// The override silently replaces the auto-detected key (PRD §8.6), so
		// this assigns rather than ORs — a previously detected PK column that
		// the override omits stops being one.
		for j := range table.Columns {
			table.Columns[j].PrimaryKey = override[table.Columns[j].Name]
		}
	}
}

// lookupTableConfig resolves the config entry for a parsed table, preferring
// the schema-qualified key over the bare table name — the same precedence
// gen.tableHasResolvedPK applies.
func lookupTableConfig(cfg *config.RootConfig, table *parser.Table) (config.TableConfig, bool) {
	if table.Schema != "" {
		if tableCfg, ok := cfg.Tables[table.Schema+"."+table.Name]; ok {
			return tableCfg, true
		}
	}
	tableCfg, ok := cfg.Tables[table.Name]
	return tableCfg, ok
}

// printSchemaError writes a parseSchema error with the "Schema error:" prefix
// on every line, so each of several joined unresolved-FK errors carries it,
// as `validate` prefixes each line "Error:".
func printSchemaError(w io.Writer, err error) {
	for _, line := range splitJoinedErrors(err) {
		_, _ = fmt.Fprintf(w, "Schema error: %s\n", line)
	}
}

// parseSchema creates the appropriate parser, discovers files, parses DDL
// and view annotation files, and returns the complete schema.
//
// A PostgreSQL foreign key whose target no parsed table defines is the one
// error returned alongside a non-nil schema: the schema is otherwise whole,
// so `validate` keeps running its post-parse and generation-phase checks and
// reports every error together (PRD §4.13, §8.1.7). Every other
// error returns a nil schema, and `generate` and loadAndValidate stop on any
// error.
func parseSchema(cfg *config.RootConfig) (*parser.Schema, error) {
	// input.schema filters nothing: it names the schema that unqualified
	// names resolve to (PRD §4.2, §5.5). Its default, "*", passes empty so
	// the parser uses its dialect default ("public" for PostgreSQL).
	schemaDefault := cfg.Input.Schema
	if schemaDefault == "*" {
		schemaDefault = ""
	}
	p, err := newParser(cfg.Input.Dialect, schemaDefault)
	if err != nil {
		return nil, err
	}

	files, err := parser.DiscoverFiles(cfg.Input.Paths)
	if err != nil {
		return nil, fmt.Errorf("discovering schema files: %w", err)
	}

	if err := parser.ParseFiles(p, files); err != nil {
		return nil, fmt.Errorf("parsing schema files: %w", err)
	}

	schema := p.Schema()

	// PostgreSQL rejects a REFERENCES target that does not exist, so a
	// foreign key whose target no parsed table defines is a schema error.
	// Left in, relationship detection would drop its edge without a trace.
	// MySQL (FOREIGN_KEY_CHECKS=0) and SQLite accept such DDL.
	var fkErr error
	if cfg.Input.Dialect == config.DialectPostgres {
		fkErr = errors.Join(parser.UnresolvedForeignKeys(schema.Tables)...)
	}

	// Parse view annotation files (input.views) — separate from DDL paths.
	if len(cfg.Input.Views) > 0 {
		if err := parseViewFiles(cfg.Input.Dialect, schemaDefault, cfg.Input.Views, schema); err != nil {
			return nil, errors.Join(fkErr, fmt.Errorf("parsing views: %w", err))
		}
	}

	schema.Sort()
	return schema, fkErr
}

// viewSQLParser is the function signature for dialect-specific view SQL parsers.
type viewSQLParser func(sql []byte) (parser.ViewSQL, error)

// viewParserForDialect returns the ParseViewSQL function for the given
// dialect. defaultSchema is the schema an unqualified PostgreSQL view name
// resolves to — the one the DDL parser gives an unqualified table (PRD §5.5).
func viewParserForDialect(dialect config.Dialect, defaultSchema string) (viewSQLParser, error) {
	switch dialect {
	case config.DialectPostgres:
		return func(sql []byte) (parser.ViewSQL, error) {
			return postgres.ParseViewSQL(sql, defaultSchema)
		}, nil
	case config.DialectMySQL:
		return mysql.ParseViewSQL, nil
	case config.DialectSQLite:
		return sqlite.ParseViewSQL, nil
	default:
		return nil, fmt.Errorf("unsupported dialect for views: %q", dialect)
	}
}

// parseViewFiles discovers and parses view annotation files, appending the
// resulting views to the schema. Each file contains a CREATE VIEW statement
// with optional annotation comments (@pk, @type, @nullable).
func parseViewFiles(dialect config.Dialect, defaultSchema string, viewPaths []string, schema *parser.Schema) error {
	parseSQL, err := viewParserForDialect(dialect, defaultSchema)
	if err != nil {
		return err
	}

	files, err := parser.DiscoverFiles(viewPaths)
	if err != nil {
		return fmt.Errorf("discovering view files: %w", err)
	}

	for _, file := range files {
		data, err := os.ReadFile(filepath.Clean(file))
		if err != nil {
			return fmt.Errorf("reading view file %s: %w", file, err)
		}

		annotations, err := parser.ParseViewAnnotations(string(data))
		if err != nil {
			return fmt.Errorf("parsing annotations in %s: %w", file, err)
		}

		parsed, err := parseSQL(data)
		if err != nil {
			return fmt.Errorf("parsing view SQL in %s: %w", file, err)
		}

		view, err := parser.BuildView(parsed, string(data), annotations, schema.Tables)
		if err != nil {
			return fmt.Errorf("building view from %s: %w", file, err)
		}

		schema.Views = append(schema.Views, *view)
	}

	return nil
}

// newParser creates a dialect-specific parser.
func newParser(dialect config.Dialect, defaultSchema string) (parser.Parser, error) {
	switch dialect {
	case config.DialectPostgres:
		return postgres.New(defaultSchema), nil
	case config.DialectMySQL:
		p, err := mysql.New()
		if err != nil {
			return nil, fmt.Errorf("creating mysql parser: %w", err)
		}
		return p, nil
	case config.DialectSQLite:
		return sqlite.New(), nil
	default:
		return nil, fmt.Errorf("unsupported dialect: %q", dialect)
	}
}

// toSchemaTables converts parsed tables to the lightweight SchemaTable type
// used by post-parse validation. UniqueGroups gathers every table-level UNIQUE
// or PRIMARY KEY constraint's column set so the primary_key.columns override
// validator can detect whether the declared PK is actually unique at the DB
// level (PRD §4.13).
func toSchemaTables(schema *parser.Schema) []config.SchemaTable {
	tables := make([]config.SchemaTable, 0, len(schema.Tables))
	for _, t := range schema.Tables {
		cols := make([]config.SchemaColumn, 0, len(t.Columns))
		for _, c := range t.Columns {
			cols = append(cols, config.SchemaColumn{
				Name:       c.Name,
				Type:       c.Type,
				PrimaryKey: c.PrimaryKey,
				Nullable:   c.Nullable,
				Unique:     c.Unique,
				// GENERATED ALWAYS columns count as defaulted: either way
				// the INSERT may omit them (§32.4 required-on-create rule).
				HasDefault:    c.Default != "" || c.GeneratedExpr != "",
				AutoIncrement: c.AutoIncrement,
			})
		}
		// Partial UNIQUE constraints (Where != "") are excluded: validation
		// uses UniqueGroups to assert PK-coverage and similar invariants that
		// require unconditional uniqueness — a predicate-gated index can't
		// satisfy them.
		var uniqueGroups [][]string
		for _, ct := range t.Constraints {
			if ct.Type == parser.Unique || ct.Type == parser.PrimaryKey {
				if ct.Type == parser.Unique && ct.Where != "" {
					continue
				}
				if len(ct.Columns) > 0 {
					group := make([]string, len(ct.Columns))
					copy(group, ct.Columns)
					uniqueGroups = append(uniqueGroups, group)
				}
			}
		}
		tables = append(tables, config.SchemaTable{
			Name:         t.Name,
			Schema:       t.Schema,
			Columns:      cols,
			UniqueGroups: uniqueGroups,
		})
	}
	return tables
}

// toSchemaViews converts parsed views to the lightweight SchemaView type used
// by post-parse validation.
func toSchemaViews(schema *parser.Schema) []config.SchemaView {
	views := make([]config.SchemaView, 0, len(schema.Views))
	for _, v := range schema.Views {
		cols := make([]config.SchemaColumn, 0, len(v.Columns))
		for _, c := range v.Columns {
			cols = append(cols, config.SchemaColumn{
				Name:       c.Name,
				Type:       c.Type,
				PrimaryKey: c.PrimaryKey,
			})
		}
		views = append(views, config.SchemaView{
			Name:    v.Name,
			Schema:  v.Schema,
			Columns: cols,
		})
	}
	return views
}

// printWarnings prints validation warnings to the writer unless quiet mode is active.
func printWarnings(w io.Writer, warnings []config.Warning, quiet bool) {
	if quiet {
		return
	}
	for _, warn := range warnings {
		_, _ = fmt.Fprintf(w, "Warning: %s\n", warn.Message)
	}
}

// printVerbose prints detailed generation information.
func printVerbose(w io.Writer, schema *parser.Schema, result *gen.GenerateResult, elapsed time.Duration) {
	_, _ = fmt.Fprintf(w, "sqlgen %s\n", Version)
	_, _ = fmt.Fprintf(w, "  tables:  %d\n", result.TableCount)
	_, _ = fmt.Fprintf(w, "  views:   %d\n", result.ViewCount)
	_, _ = fmt.Fprintf(w, "  enums:   %d\n", len(schema.Enums))
	_, _ = fmt.Fprintln(w, "  files:")
	for _, f := range result.Files {
		_, _ = fmt.Fprintf(w, "    %s\n", f)
	}
	_, _ = fmt.Fprintf(w, "  elapsed: %s\n", elapsed.Round(time.Millisecond))
}

// splitJoinedErrors splits an error produced by errors.Join into individual
// error message strings. If the error is not a joined error, it returns the
// single error message.
func splitJoinedErrors(err error) []string {
	if err == nil {
		return nil
	}
	msg := err.Error()
	return strings.Split(msg, "\n")
}
