package parser

import (
	"fmt"
	"iter"
	"maps"
	"slices"
	"strings"
)

// ViewTypeAnnotation holds a Go type override from an @type directive.
type ViewTypeAnnotation struct {
	GoType     string
	ImportPath string
}

// ViewAnnotations holds all parsed annotations from a view annotation file.
type ViewAnnotations struct {
	PKColumns       []string
	TypeOverrides   map[string]ViewTypeAnnotation
	NullableColumns []string
}

// ViewSelectColumn represents a column extracted from a view's SELECT clause
// by a dialect-specific parser.
type ViewSelectColumn struct {
	Alias        string // output column name (alias or inferred from expression)
	SourceTable  string // table alias the column comes from (if qualified reference)
	SourceColumn string // column name in the source table
	Aggregate    string // aggregate function name (uppercase), empty if not aggregate
	// Unaliased reports a column named after its source column, with no AS
	// and no view column list. SQLite renames such a column when its source
	// column is renamed, so only the SQLite view parser sets it.
	Unaliased bool
}

// ViewSQL holds the metadata extracted from a CREATE VIEW or CREATE
// MATERIALIZED VIEW statement by a dialect-specific parser.
type ViewSQL struct {
	Name         string
	Schema       string
	Columns      []ViewSelectColumn
	TableAliases map[string]string
	// TableSchemas maps the same keys as TableAliases to the table's schema,
	// for a dialect with schemas (PostgreSQL); nil otherwise.
	TableSchemas map[string]string
	// OuterAliases holds the TableAliases keys on the outer side of a join,
	// whose columns are NULL when no row matches: the right side of a LEFT
	// JOIN, the left side of a RIGHT JOIN, both sides of a FULL JOIN.
	OuterAliases map[string]bool
	// QualifiedAliases holds the TableAliases keys whose table name is
	// qualified with a schema (a MySQL database), which name a table even
	// where a common table expression has that name.
	QualifiedAliases map[string]bool
	// Subqueries holds the query of each derived table the SELECT reads, by
	// its alias, and CTEs the query of each common table expression of its
	// WITH clause, by its name. Each is read as a view's query is, so
	// ViewColumnSources can follow a column through it to its table. In
	// such a query a column whose SourceColumn is "*" selects every column
	// of its SourceTable's table, or of every FROM-clause table when
	// SourceTable is empty, under its own name. A query the parser cannot
	// read, such as a compound one, is recorded with no columns, so its
	// name still names no table. A FROM-clause key that names a derived
	// table or a common table expression in scope names no table.
	Subqueries map[string]ViewSQL
	CTEs       map[string]ViewSQL

	// Materialized reports whether the statement was CREATE MATERIALIZED VIEW
	// (PostgreSQL-only; the MySQL/SQLite parsers reject the syntax).
	Materialized bool
}

// ViewQueryError reports a CREATE VIEW whose name a dialect's ParseViewSQL
// read but whose query it cannot, such as a compound SELECT. An annotation
// file for a view the database defines needs only the name (PRD §16.2).
type ViewQueryError struct {
	Schema, Name string
	Err          error
}

func (e *ViewQueryError) Error() string { return e.Err.Error() }

func (e *ViewQueryError) Unwrap() error { return e.Err }

// ViewParseError reports a view query a dialect's ParseViewSQL cannot read,
// such as one that selects *. Reason says what it cannot read; Advice is what
// the error adds for a view file's author, which does not apply to a view the
// database defines, since its query is not the user's to change.
type ViewParseError struct {
	Reason, Advice string
}

func (e *ViewParseError) Error() string { return e.Reason + "; " + e.Advice }

// ParseViewAnnotations extracts @pk, @type, and @nullable directives from
// SQL line comments at the top of a view annotation file.
func ParseViewAnnotations(content string) (*ViewAnnotations, error) {
	annotations := &ViewAnnotations{
		TypeOverrides: make(map[string]ViewTypeAnnotation),
	}

	var hasPK bool
	for line := range strings.SplitSeq(content, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "--") {
			// Stop processing annotations at the first non-comment, non-empty line.
			if line != "" {
				break
			}
			continue
		}

		body := strings.TrimPrefix(line, "--")
		body = strings.TrimSpace(body)

		if !strings.HasPrefix(body, "@") {
			continue
		}

		directive, value, _ := strings.Cut(body, ":")
		directive = strings.TrimSpace(directive)
		value = strings.TrimSpace(value)

		switch directive {
		case "@pk":
			if hasPK {
				return nil, fmt.Errorf("duplicate @pk directive — declare all PK columns in one line")
			}
			hasPK = true
			for col := range strings.SplitSeq(value, ",") {
				col = strings.TrimSpace(col)
				if col != "" {
					annotations.PKColumns = append(annotations.PKColumns, col)
				}
			}

		case "@nullable":
			for col := range strings.SplitSeq(value, ",") {
				col = strings.TrimSpace(col)
				if col != "" {
					annotations.NullableColumns = append(annotations.NullableColumns, col)
				}
			}

		default:
			if colName, ok := strings.CutPrefix(directive, "@type"); ok {
				if err := parseTypeDirective(annotations, strings.TrimSpace(colName), value); err != nil {
					return nil, err
				}
			}
			// Unrecognized directives are ignored (per PRD).
		}
	}

	return annotations, nil
}

// parseTypeDirective handles a single @type directive.
func parseTypeDirective(annotations *ViewAnnotations, colName, value string) error {
	if colName == "" {
		return fmt.Errorf("@type directive missing column name")
	}
	if _, exists := annotations.TypeOverrides[colName]; exists {
		return fmt.Errorf("duplicate @type for column %q", colName)
	}

	parts := strings.Fields(value)
	if len(parts) == 0 {
		return fmt.Errorf("@type %s missing Go type", colName)
	}

	ta := ViewTypeAnnotation{GoType: parts[0]}
	if len(parts) > 1 {
		ta.ImportPath = parts[1]
	}

	// External package type (contains '.') requires import path.
	if strings.Contains(ta.GoType, ".") && ta.ImportPath == "" {
		return fmt.Errorf("@type %s: external type %q requires an import path", colName, ta.GoType)
	}

	annotations.TypeOverrides[colName] = ta
	return nil
}

// BuildView constructs a View from pre-parsed SQL metadata, annotations, and
// schema parsed so far. The parsed field is produced by a dialect-specific
// parser's ParseViewSQL method; annotations come from ParseViewAnnotations.
func BuildView(parsed ViewSQL, rawSQL string, annotations *ViewAnnotations, schema *Schema) (*View, error) {
	colSet := make(map[string]bool, len(parsed.Columns))
	for _, col := range parsed.Columns {
		colSet[col.Alias] = true
	}
	if err := checkAnnotationColumns(colSet, annotations); err != nil {
		return nil, err
	}

	viewColumns := resolveViewColumns(parsed.Columns, newViewSources(parsed, schema), annotations)

	// An annotation-file matview has no live unique index to discover, so @pk
	// doubles as the user's assertion that a unique key exists — the same
	// precondition REFRESH MATERIALIZED VIEW CONCURRENTLY needs.
	return &View{
		Name:                    parsed.Name,
		Schema:                  parsed.Schema,
		SQL:                     rawSQL,
		Columns:                 viewColumns,
		Materialized:            parsed.Materialized,
		ConcurrentlyRefreshable: parsed.Materialized && len(annotations.PKColumns) > 0,
	}, nil
}

// IsAggregateFunction returns true if funcName (uppercase) is a known SQL
// aggregate function. Dialect parsers use this when classifying SELECT expressions.
func IsAggregateFunction(funcName string) bool {
	switch funcName {
	case "COUNT", "AVG", "SUM", "MIN", "MAX",
		"STRING_AGG", "GROUP_CONCAT", "ARRAY_AGG",
		"BOOL_AND", "EVERY", "BOOL_OR",
		"JSON_AGG", "JSONB_AGG", "JSON_ARRAYAGG",
		"JSON_OBJECT_AGG", "JSONB_OBJECT_AGG", "JSON_OBJECTAGG":
		return true
	default:
		return false
	}
}

// resolveViewColumns resolves Go types for view columns using the PRD 16.3 chain:
// @type annotation → aggregate inference → schema matching → any fallback.
func resolveViewColumns(selectCols []ViewSelectColumn, sources *viewSources, annotations *ViewAnnotations) []Column {
	nullableSet := make(map[string]bool, len(annotations.NullableColumns))
	for _, col := range annotations.NullableColumns {
		nullableSet[col] = true
	}

	pkSet := make(map[string]bool, len(annotations.PKColumns))
	for _, col := range annotations.PKColumns {
		pkSet[col] = true
	}

	result := make([]Column, 0, len(selectCols))
	for _, sc := range selectCols {
		col := Column{Name: sc.Alias}
		col.Type, col.Nullable = resolveColumnType(sc, sources, annotations)

		// Mark columns whose Type is already a Go type literal (from @type or
		// aggregate inference). The code generator uses GoTypeLiteral directly
		// instead of running the SQL-to-Go resolver.
		if ta, ok := annotations.TypeOverrides[sc.Alias]; ok {
			col.GoTypeLiteral = ta.GoType
			col.GoTypeImport = ta.ImportPath
		} else if sc.Aggregate != "" {
			// Most aggregates always return Go types (COUNT→int64, AVG→*float64).
			// SUM/MIN/MAX/ARRAY_AGG return a SQL type from schema matching when
			// a source column is found — those should go through the SQL
			// resolver (ARRAY_AGG returns the array form, e.g. text[]).
			switch sc.Aggregate {
			case "SUM", "MIN", "MAX", "ARRAY_AGG":
				if typ, _ := sources.column(sc); typ == "" {
					col.GoTypeLiteral = col.Type // fallback "any"
				} else {
					// Flows through the SQL-to-Go resolver; record the aggregate
					// so codegen can apply dialect-specific result widening
					// (SUM widens: e.g. SUM(integer) → bigint in PostgreSQL).
					col.Aggregate = sc.Aggregate
				}
			default:
				col.GoTypeLiteral = col.Type
			}
		} else if col.Type == "any" {
			col.GoTypeLiteral = "any"
		}

		if nullableSet[sc.Alias] {
			col.Nullable = true
		}
		if pkSet[sc.Alias] {
			col.PrimaryKey = true
			col.Nullable = false
		}

		result = append(result, col)
	}

	return result
}

// resolveColumnType applies the PRD 16.3 resolution chain for a single column:
// @type annotation → aggregate inference → schema matching → any fallback.
// A schema-matched column is nullable when its source column is, or when its
// table is on the outer side of a join.
func resolveColumnType(sc ViewSelectColumn, sources *viewSources, annotations *ViewAnnotations) (string, bool) {
	if ta, ok := annotations.TypeOverrides[sc.Alias]; ok {
		return ta.GoType, false
	}
	if sc.Aggregate != "" {
		baseType, _ := sources.column(sc)
		return inferAggregateType(sc.Aggregate, baseType), isAggregateNullable(sc.Aggregate)
	}
	if sc.SourceColumn != "" {
		if resolved, nullable := sources.column(sc); resolved != "" {
			return resolved, nullable
		}
	}
	return "any", false
}

// viewSources finds a view column's source column and writes its type
// relative to the view's schema.
type viewSources struct {
	viewSchema string
	aliases    map[string]string
	schemas    map[string]string
	outer      map[string]bool
	// queries holds the FROM-clause keys that name a derived table or a
	// common table expression, and so no table.
	queries map[string]bool
	tables  []Table
	// userTypes holds "schema.name" for every enum, SET, composite and domain type.
	userTypes map[string]bool
}

// newViewSources returns the viewSources of a view's query.
func newViewSources(parsed ViewSQL, schema *Schema) *viewSources {
	return scopeSources([]traceScope{{query: parsed}}, nil, schema)
}

// scopeSources returns the viewSources of the innermost of scopes, whose
// enclosing scopes hold the common table expressions it can read; tracing
// is as subquery takes it.
func scopeSources(scopes []traceScope, tracing map[string]bool, schema *Schema) *viewSources {
	parsed := scopes[len(scopes)-1].query
	vs := &viewSources{
		viewSchema: scopes[0].query.Schema,
		aliases:    parsed.TableAliases,
		schemas:    parsed.TableSchemas,
		outer:      parsed.OuterAliases,
		queries:    make(map[string]bool),
		userTypes:  make(map[string]bool),
	}
	for key := range parsed.Subqueries {
		vs.queries[key] = true
	}
	for key := range parsed.TableAliases {
		if _, ok := cteScopes(key, scopes, tracing); ok {
			vs.queries[key] = true
		}
	}
	if schema == nil {
		return vs
	}
	vs.tables = schema.Tables
	for _, e := range schema.Enums {
		vs.userTypes[e.Schema+"."+e.Name] = true
	}
	for _, st := range schema.Sets {
		vs.userTypes[st.Schema+"."+st.Name] = true
	}
	for _, ct := range schema.CompositeTypes {
		vs.userTypes[ct.Schema+"."+ct.Name] = true
	}
	for _, d := range schema.DomainTypes {
		vs.userTypes[d.Schema+"."+d.Name] = true
	}
	return vs
}

// column returns the type of sc's source column, written relative to the
// view's schema, and whether the view can read it as NULL: the column is
// nullable or its table is on the outer side of a join. typ is "" when no
// source column is found.
func (vs *viewSources) column(sc ViewSelectColumn) (typ string, nullable bool) {
	t, key := vs.sourceTable(sc)
	if t == nil {
		return "", false
	}
	col := ColumnByName(t, sc.SourceColumn)
	return vs.relativeType(col.Type, t.Schema), col.Nullable || vs.outer[key]
}

// sourceTable returns the table that has sc's source column, and the
// FROM-clause key it is read through, or nil when no table has it. An
// unqualified column is looked up in the FROM-clause tables, in key order.
func (vs *viewSources) sourceTable(sc ViewSelectColumn) (*Table, string) {
	if sc.SourceColumn == "" {
		return nil, ""
	}
	keys := []string{sc.SourceTable}
	if sc.SourceTable == "" {
		keys = slices.Sorted(maps.Keys(vs.aliases))
	}
	for _, key := range keys {
		if t := vs.table(key); t != nil && ColumnByName(t, sc.SourceColumn) != nil {
			return t, key
		}
	}
	return nil, ""
}

// ViewColumnSource is the table column a view column reads.
type ViewColumnSource struct {
	// Table is nil for a view column that reads no table column.
	Table  *Table
	Column string
}

// ViewColumnSources returns, for each column of def, the table column it
// reads, resolved against schema: one of a FROM-clause table, as BuildView
// resolves it, or one a derived table or common table expression of def
// selects, followed through it. A FROM-clause key that names a derived
// table, or a common table expression in scope, names it rather than a
// table of that name. It does not change how BuildView types a column read
// through a subquery.
func ViewColumnSources(def ViewSQL, schema *Schema) []ViewColumnSource {
	vs := newViewSources(def, schema)
	sources := make([]ViewColumnSource, len(def.Columns))
	for i, sc := range def.Columns {
		sources[i] = traceViewColumn(sc, vs, []traceScope{{query: def}}, make(map[string]bool), schema)
	}
	return sources
}

// traceScope is a query on the path ViewColumnSources follows, and path the
// FROM-clause keys and common table expression names ("with:<name>") that
// lead to it from the view, which identify it.
type traceScope struct {
	query ViewSQL
	path  string
}

// traceViewColumn returns the table column sc reads in the innermost of
// scopes, whose FROM clause vs reads. The enclosing scopes are the queries
// whose common table expressions it can read, and tracing holds the paths
// of the common table expressions it is already inside.
func traceViewColumn(sc ViewSelectColumn, vs *viewSources, scopes []traceScope, tracing map[string]bool, schema *Schema) ViewColumnSource {
	if t, _ := vs.sourceTable(sc); t != nil {
		return ViewColumnSource{Table: t, Column: sc.SourceColumn}
	}
	if sc.SourceColumn == "" || sc.SourceColumn == "*" {
		return ViewColumnSource{}
	}
	def := scopes[len(scopes)-1].query
	keys := []string{sc.SourceTable}
	if sc.SourceTable == "" {
		keys = slices.Concat(slices.Collect(maps.Keys(def.Subqueries)), slices.Collect(maps.Keys(def.TableAliases)))
		slices.Sort(keys)
		keys = slices.Compact(keys)
	}
	for _, key := range keys {
		inner, cte, ok := subquery(key, scopes, tracing)
		if !ok {
			continue
		}
		path := inner[len(inner)-1].path
		if cte {
			tracing[path] = true
		}
		src, found := traceSubqueryColumn(sc.SourceColumn, inner, tracing, schema)
		delete(tracing, path)
		if found {
			return src
		}
	}
	return ViewColumnSource{}
}

// traceSubqueryColumn returns the table column that column of the
// innermost of scopes reads, and whether that query selects column at all.
func traceSubqueryColumn(column string, scopes []traceScope, tracing map[string]bool, schema *Schema) (ViewColumnSource, bool) {
	q := scopes[len(scopes)-1].query
	qs := scopeSources(scopes, tracing, schema)
	for _, c := range q.Columns {
		switch {
		case c.SourceColumn == "*":
			if src := traceViewColumn(ViewSelectColumn{SourceTable: c.SourceTable, SourceColumn: column}, qs, scopes, tracing, schema); src.Table != nil {
				return src, true
			}
		case c.Alias == column:
			return traceViewColumn(c, qs, scopes, tracing, schema), true
		}
	}
	return ViewColumnSource{}, false
}

// subquery returns the scopes of the query a FROM-clause key of the
// innermost of scopes reads when the key names no table, ending with that
// query: a derived table of the innermost query, or a common table
// expression as cteScopes finds it. cte reports the latter.
func subquery(key string, scopes []traceScope, tracing map[string]bool) (inner []traceScope, cte, ok bool) {
	last := scopes[len(scopes)-1]
	if q, ok := last.query.Subqueries[key]; ok {
		return append(slices.Clip(scopes), traceScope{q, last.path + "/" + key}), false, true
	}
	inner, ok = cteScopes(key, scopes, tracing)
	return inner, ok, ok
}

// cteScopes returns the scopes of the common table expression a
// FROM-clause key of the innermost of scopes names, ending with its query:
// the innermost one of the key's name, of that query or of an enclosing
// one, that the key is not inside and tracing does not hold, as a common
// table expression cannot read itself. A key whose table name is qualified
// names a table.
func cteScopes(key string, scopes []traceScope, tracing map[string]bool) ([]traceScope, bool) {
	last := scopes[len(scopes)-1]
	name, ok := last.query.TableAliases[key]
	if !ok || last.query.QualifiedAliases[key] {
		return nil, false
	}
	for i := len(scopes) - 1; i >= 0; i-- {
		path := scopes[i].path + "/with:" + name
		q, ok := scopes[i].query.CTEs[name]
		inside := last.path == path || strings.HasPrefix(last.path, path+"/")
		if ok && !inside && !tracing[path] {
			return append(slices.Clip(scopes[:i+1]), traceScope{q, path}), true
		}
	}
	return nil, false
}

// viewScopes yields the scopes of def and of each derived table and common
// table expression it reads, at any depth, each ending with that query.
func viewScopes(def ViewSQL) iter.Seq[[]traceScope] {
	return func(yield func([]traceScope) bool) {
		walkScopes([]traceScope{{query: def}}, yield)
	}
}

func walkScopes(scopes []traceScope, yield func([]traceScope) bool) bool {
	if !yield(scopes) {
		return false
	}
	last := scopes[len(scopes)-1]
	for _, step := range []struct {
		prefix  string
		queries map[string]ViewSQL
	}{{"/", last.query.Subqueries}, {"/with:", last.query.CTEs}} {
		for _, key := range slices.Sorted(maps.Keys(step.queries)) {
			if !walkScopes(append(slices.Clip(scopes), traceScope{step.queries[key], last.path + step.prefix + key}), yield) {
				return false
			}
		}
	}
	return true
}

// RenameViewColumnReferences repoints every view column whose Definition
// reads column oldName of table at newName, as PostgreSQL and SQLite do on
// RENAME COLUMN, so the view's columns can be read again after the rename.
// Column names match as dialect matches them (SameColumnName). An Unaliased
// column also takes the new name, as SQLite renames it. The columns of a
// derived table or common table expression the view reads are repointed
// the same way, so a view column read through one whose name changes no
// longer finds it, as SQLite then refuses the rename. Call it before the
// table's column is renamed.
func RenameViewColumnReferences(schema *Schema, table *Table, dialect, oldName, newName string) {
	for i := range schema.Views {
		def := schema.Views[i].Definition
		if def == nil {
			continue
		}
		for scopes := range viewScopes(*def) {
			// The scope's query shares its Columns with def or the map's entry.
			q := scopes[len(scopes)-1].query
			vs := scopeSources(scopes, nil, schema)
			for j := range q.Columns {
				sc := &q.Columns[j]
				if t, _ := vs.sourceTable(*sc); t != table || !SameColumnName(dialect, sc.SourceColumn, oldName) {
					continue
				}
				sc.SourceColumn = newName
				if sc.Unaliased {
					sc.Alias = newName
				}
			}
		}
	}
}

// ViewFromTables returns, by FROM-clause key, the table each key of def
// names, resolved against schema as BuildView resolves it, or nil for a key
// that names no table in schema (a common table expression, a view, or a
// table schema lacks). The keys of each derived table and common table
// expression def reads, at any depth, are listed too, under the path that
// leads to it: "<alias>/<key>" and "with:<name>/<key>".
func ViewFromTables(def ViewSQL, schema *Schema) map[string]*Table {
	tables := make(map[string]*Table)
	for scopes := range viewScopes(def) {
		last := scopes[len(scopes)-1]
		vs := scopeSources(scopes, nil, schema)
		prefix := strings.TrimPrefix(last.path+"/", "/")
		for key := range last.query.TableAliases {
			tables[prefix+key] = vs.table(key)
		}
	}
	return tables
}

// RenameViewTableReferences repoints every FROM-clause key of a view
// Definition, or of a derived table or common table expression it reads,
// that names table at (newSchema, newName), as PostgreSQL does on a table
// rename or SET SCHEMA and SQLite on a table rename, so the view's columns
// can be read again after the change. Call it before the table is renamed
// or moved.
func RenameViewTableReferences(schema *Schema, table *Table, newSchema, newName string) {
	for i := range schema.Views {
		def := schema.Views[i].Definition
		if def == nil {
			continue
		}
		for scopes := range viewScopes(*def) {
			// The scope's query shares its maps with def or the map's entry.
			q := scopes[len(scopes)-1].query
			vs := scopeSources(scopes, nil, schema)
			for key := range q.TableAliases {
				if vs.table(key) != table {
					continue
				}
				q.TableAliases[key] = newName
				if q.TableSchemas != nil {
					q.TableSchemas[key] = newSchema
				}
			}
		}
	}
}

// table returns the table a FROM-clause key (alias or table name) names: the
// one in the recorded schema when there is one, else one with no schema, or
// nil when the key names a derived table or common table expression.
func (vs *viewSources) table(key string) *Table {
	if vs.queries[key] {
		return nil
	}
	name := key
	if n, ok := vs.aliases[key]; ok {
		name = n
	}
	schema := vs.schemas[key]
	var schemaless *Table
	for i := range vs.tables {
		t := &vs.tables[i]
		if t.Name != name {
			continue
		}
		if schema == "" || t.Schema == schema {
			return t
		}
		if t.Schema == "" && schemaless == nil {
			schemaless = t
		}
	}
	return schemaless
}

// relativeType rewrites typ, written relative to the schema declared, so it
// reads relative to the view's schema, as a table column's type does: a
// user-defined type of declared is qualified, and a type qualified with the
// view's schema is written bare. Array suffixes are kept.
func (vs *viewSources) relativeType(typ, declared string) string {
	if declared == "" || vs.viewSchema == "" || declared == vs.viewSchema {
		return typ
	}
	base := strings.TrimRight(typ, "[]")
	suffix := typ[len(base):]
	if i := strings.LastIndexByte(base, '.'); i > 0 {
		if base[:i] == vs.viewSchema {
			return base[i+1:] + suffix
		}
		return typ
	}
	if vs.userTypes[declared+"."+base] {
		return declared + "." + base + suffix
	}
	return typ
}

// inferAggregateType returns the type for an aggregate function per PRD 16.3.
// Most aggregates resolve to a Go type directly; SUM, MIN, MAX and ARRAY_AGG
// instead return a SQL type for the SQL-to-Go resolver to finish, so that
// dialect-specific handling (SUM widening, array element mapping) applies.
func inferAggregateType(aggregate, baseType string) string {
	switch aggregate {
	case "COUNT":
		return "int64"
	case "AVG":
		return "*float64"
	case "SUM":
		if baseType != "" {
			return baseType
		}
		return "any"
	case "MIN", "MAX":
		if baseType != "" {
			return baseType // nullable semantics handled by isAggregateNullable
		}
		return "any"
	case "STRING_AGG", "GROUP_CONCAT":
		return "*string"
	case "ARRAY_AGG":
		// The SQL array type, not a Go literal: it has to reach the model
		// through the SQL-to-Go resolver, which is the only path that marks
		// the result as a slice and so the only one that earns the driver's
		// array scan handling (pq.Array under stdlib). A Go "[]T" literal
		// would bypass the resolver and fail to scan (PRD 16.3).
		if baseType != "" {
			return baseType + "[]"
		}
		return "[]any"
	case "BOOL_AND", "EVERY", "BOOL_OR":
		return "*bool"
	// types.JSON, not json.RawMessage: PRD §26.4 names types.JSON "the default
	// for JSON/JSONB columns", and the table path resolves every json/jsonb
	// column to it. A view's JSON aggregate IS a JSON column, so the two paths
	// have to land on one Go type — sqlgen binds one Go type per GraphQL
	// scalar (registerScalarUse is first-registration-wins), so a project
	// holding both types.JSON tables and a json.RawMessage view left the view
	// field unbindable and gqlgen answered it with a panicking field resolver.
	// types.JSON is a named json.RawMessage, so nothing changes at the scan
	// layer; it adds the MarshalGQL/UnmarshalGQL pair the surface needs.
	case "JSON_AGG", "JSONB_AGG", "JSON_ARRAYAGG":
		return "types.JSON"
	case "JSON_OBJECT_AGG", "JSONB_OBJECT_AGG", "JSON_OBJECTAGG":
		return "types.JSON"
	default:
		return "any"
	}
}

// isAggregateNullable returns true if the aggregate function produces a nullable result.
func isAggregateNullable(aggregate string) bool {
	switch aggregate {
	case "COUNT":
		return false
	case "AVG", "SUM", "MIN", "MAX", "STRING_AGG", "GROUP_CONCAT",
		"BOOL_AND", "EVERY", "BOOL_OR", "ARRAY_AGG":
		// ARRAY_AGG is nullable for the same reason MIN/MAX are: an aggregate
		// over zero rows yields SQL NULL. It also yields NULL for a group the
		// FILTER clause empties, which is the shape PRD 16.3 recommends for
		// outer joins. The Go type is unaffected -- a slice conveys NULL as
		// nil -- but the flag drives the nullable comparator and the manifest.
		return true
	default:
		return false
	}
}

// checkAnnotationColumns returns an error for an annotation that names a
// column not in columns.
func checkAnnotationColumns(columns map[string]bool, annotations *ViewAnnotations) error {
	for _, pk := range annotations.PKColumns {
		if !columns[pk] {
			return fmt.Errorf("@pk references unknown column %q", pk)
		}
	}
	for _, col := range slices.Sorted(maps.Keys(annotations.TypeOverrides)) {
		if !columns[col] {
			return fmt.Errorf("@type references unknown column %q", col)
		}
	}
	for _, col := range annotations.NullableColumns {
		if !columns[col] {
			return fmt.Errorf("@nullable references unknown column %q", col)
		}
	}
	return nil
}

// MergeViewAnnotations merges annotation overlays onto introspected views.
// For each view that has a matching annotation file, annotation @pk, @type,
// and @nullable take precedence over introspected values. The introspected
// view's columns stand (PRD §16.2), so an annotation naming a column it
// lacks is an error.
func MergeViewAnnotations(introspected []View, annotated []View, annotationMap map[string]*ViewAnnotations) ([]View, error) {
	annotatedMap := make(map[string]int, len(annotated))
	for i, v := range annotated {
		key := viewKey(v.Schema, v.Name)
		annotatedMap[key] = i
	}

	var result []View
	merged := make(map[string]bool)

	for _, iv := range introspected {
		key := viewKey(iv.Schema, iv.Name)
		if _, ok := annotatedMap[key]; ok {
			mergedView, err := mergeView(iv, annotationMap[key])
			if err != nil {
				return nil, fmt.Errorf("view %s: %w", key, err)
			}
			result = append(result, mergedView)
			merged[key] = true
		} else {
			result = append(result, iv)
		}
	}

	for _, av := range annotated {
		key := viewKey(av.Schema, av.Name)
		if !merged[key] {
			result = append(result, av)
		}
	}

	return result, nil
}

func viewKey(schema, name string) string {
	if schema == "" {
		return name
	}
	return schema + "." + name
}

// mergeView merges an introspected view with annotation overrides.
func mergeView(introspected View, annotations *ViewAnnotations) (View, error) {
	if annotations == nil {
		return introspected, nil
	}
	columns := make(map[string]bool, len(introspected.Columns))
	for _, col := range introspected.Columns {
		columns[col.Name] = true
	}
	if err := checkAnnotationColumns(columns, annotations); err != nil {
		return View{}, err
	}

	result := introspected

	pkSet := make(map[string]bool, len(annotations.PKColumns))
	for _, pk := range annotations.PKColumns {
		pkSet[pk] = true
	}

	nullableSet := make(map[string]bool, len(annotations.NullableColumns))
	for _, col := range annotations.NullableColumns {
		nullableSet[col] = true
	}

	// @pk overrides a discovered PK (explicit user intent wins): clear the
	// introspected PK columns before applying the annotation set. For a
	// materialized view, @pk also asserts a unique key exists, which is the
	// precondition for REFRESH ... CONCURRENTLY.
	if len(pkSet) > 0 {
		for i := range result.Columns {
			result.Columns[i].PrimaryKey = false
		}
		if result.Materialized {
			result.ConcurrentlyRefreshable = true
		}
	}

	// The same precedence BuildView applies: a @type column is NOT NULL,
	// @nullable makes it nullable, and @pk wins over both.
	for i := range result.Columns {
		col := &result.Columns[i]
		if ta, ok := annotations.TypeOverrides[col.Name]; ok {
			col.Type = ta.GoType
			col.GoTypeLiteral = ta.GoType
			col.GoTypeImport = ta.ImportPath
			col.Nullable = false
		}
		if nullableSet[col.Name] {
			col.Nullable = true
		}
		if pkSet[col.Name] {
			col.PrimaryKey = true
			col.Nullable = false
		}
	}

	return result, nil
}
