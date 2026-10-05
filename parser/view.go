package parser

import (
	"fmt"
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
}

// ViewSQL holds the metadata extracted from a CREATE VIEW or CREATE
// MATERIALIZED VIEW statement by a dialect-specific parser.
type ViewSQL struct {
	Name         string
	Schema       string
	Columns      []ViewSelectColumn
	TableAliases map[string]string

	// Materialized reports whether the statement was CREATE MATERIALIZED VIEW
	// (PostgreSQL-only; the MySQL/SQLite parsers reject the syntax).
	Materialized bool
}

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
// known tables. The parsed field is produced by a dialect-specific parser's
// ParseViewSQL method; annotations come from ParseViewAnnotations.
func BuildView(parsed ViewSQL, rawSQL string, annotations *ViewAnnotations, tables []Table) (*View, error) {
	// Validate annotation column references against parsed columns.
	colSet := make(map[string]bool, len(parsed.Columns))
	for _, col := range parsed.Columns {
		colSet[col.Alias] = true
	}
	for _, pk := range annotations.PKColumns {
		if !colSet[pk] {
			return nil, fmt.Errorf("@pk references unknown column %q", pk)
		}
	}
	for col := range annotations.TypeOverrides {
		if !colSet[col] {
			return nil, fmt.Errorf("@type references unknown column %q", col)
		}
	}
	for _, col := range annotations.NullableColumns {
		if !colSet[col] {
			return nil, fmt.Errorf("@nullable references unknown column %q", col)
		}
	}

	viewColumns := resolveViewColumns(parsed.Columns, parsed.TableAliases, tables, annotations)

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
func resolveViewColumns(
	selectCols []ViewSelectColumn,
	tableAliases map[string]string,
	tables []Table,
	annotations *ViewAnnotations,
) []Column {
	tableMap := make(map[string]*Table, len(tables))
	for i := range tables {
		tableMap[tables[i].Name] = &tables[i]
	}

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
		col.Type, col.Nullable = resolveColumnType(sc, tableAliases, tableMap, annotations)

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
				baseSQL := resolveFromSchema(sc, tableAliases, tableMap)
				if baseSQL == "" {
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
func resolveColumnType(
	sc ViewSelectColumn,
	tableAliases map[string]string,
	tableMap map[string]*Table,
	annotations *ViewAnnotations,
) (string, bool) {
	if ta, ok := annotations.TypeOverrides[sc.Alias]; ok {
		return ta.GoType, false
	}
	if sc.Aggregate != "" {
		baseType := resolveFromSchema(sc, tableAliases, tableMap)
		return inferAggregateType(sc.Aggregate, baseType), isAggregateNullable(sc.Aggregate)
	}
	if sc.SourceColumn != "" {
		if resolved := resolveFromSchema(sc, tableAliases, tableMap); resolved != "" {
			return resolved, false
		}
	}
	return "any", false
}

// resolveFromSchema attempts to find the column type from a known table.
func resolveFromSchema(sc ViewSelectColumn, tableAliases map[string]string, tableMap map[string]*Table) string {
	if sc.SourceColumn == "" {
		return ""
	}
	tableName := sc.SourceTable
	if alias, ok := tableAliases[tableName]; ok {
		tableName = alias
	}
	if tableName == "" {
		for _, t := range tableMap {
			if col := ColumnByName(t, sc.SourceColumn); col != nil {
				return col.Type
			}
		}
		return ""
	}
	t, ok := tableMap[tableName]
	if !ok {
		return ""
	}
	if col := ColumnByName(t, sc.SourceColumn); col != nil {
		return col.Type
	}
	return ""
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

// MergeViewAnnotations merges annotation overlays onto introspected views.
// For each view that has a matching annotation file, annotation @pk, @type,
// and @nullable take precedence over introspected values.
func MergeViewAnnotations(introspected []View, annotated []View, annotationMap map[string]*ViewAnnotations) []View {
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
			mergedView := mergeView(iv, annotationMap[key])
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

	return result
}

func viewKey(schema, name string) string {
	if schema == "" {
		return name
	}
	return schema + "." + name
}

// mergeView merges an introspected view with annotation overrides.
func mergeView(introspected View, annotations *ViewAnnotations) View {
	if annotations == nil {
		return introspected
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

	for i := range result.Columns {
		col := &result.Columns[i]
		if pkSet[col.Name] {
			col.PrimaryKey = true
			col.Nullable = false
		}
		if ta, ok := annotations.TypeOverrides[col.Name]; ok {
			col.Type = ta.GoType
		}
		if nullableSet[col.Name] {
			col.Nullable = true
		}
	}

	return result
}
