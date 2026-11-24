package gen

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/parser"
)

// declaredJunctionKeys returns the junction columns a declared m2m entry links
// through: junction_local_fk for the table that declares it (source) and
// junction_reference_fk for the related table. A key left unset is inferred
// from the linked table's name as {singular table}_id, the singular §8.5's
// naming rules give a struct name, so products / tags infer product_id /
// tag_id (PRD §4.8).
//
// Inference gives one column per side, so a side whose table has a composite
// primary key and no override cannot be inferred: that key comes back "" and
// the error names the relationship and the key to declare. Two keys that
// resolve to the same column set are an error too: the loader, the
// relationship filter and the nested writer would join and write both sides
// through one column. Inference gives that for every self-referential entry
// that leaves both keys unset, since both sides name the same table. Callers
// run checkDeclaredTarget first, so the related table resolves.
func declaredJunctionKeys(schema *parser.Schema, r config.TableRelationship, source string, targets *relationshipTargets) (local, reference string, err error) {
	sSchema, sName := splitQualifiedName(source)
	target, _ := targets.resolve(splitQualifiedName(r.Table))
	_, tName := splitQualifiedName(r.Table)

	local, localErr := junctionKeyColumn(schema, r, source, "junction_local_fk", r.JunctionLocalFK, sSchema, sName)
	reference, refErr := junctionKeyColumn(schema, r, source, "junction_reference_fk", r.JunctionReferenceFK, target.Schema, tName)
	if err := errors.Join(localErr, refErr); err != nil {
		return local, reference, err
	}
	localCols := slices.Sorted(slices.Values(declaredJunctionKey(r.JunctionLocalFK, local)))
	if slices.Equal(localCols, slices.Sorted(slices.Values(declaredJunctionKey(r.JunctionReferenceFK, reference)))) {
		return local, reference, fmt.Errorf(
			"tables.%s.relationships: %q resolves junction_local_fk and junction_reference_fk to the same %s columns (%s); an unset key infers {singular table}_id, so a self-referential entry declares at least one key, naming columns the other does not (PRD §4.8)",
			source, r.Name, r.Junction, strings.Join(localCols, ", "),
		)
	}
	return local, reference, nil
}

// junctionKeyColumn resolves one side of declaredJunctionKeys: the override's
// first column, else the column inferred from the linked table (tSchema,
// tName). The pairs carry every column of an override.
func junctionKeyColumn(schema *parser.Schema, r config.TableRelationship, source, key string, override config.KeyColumns, tSchema, tName string) (string, error) {
	if len(override) > 0 {
		return override[0], nil
	}
	if pk := primaryKeyColumnNames(findSchemaTable(schema, tSchema, tName)); len(pk) > 1 {
		return "", fmt.Errorf(
			"tables.%s.relationships: %q links %s, whose primary key (%s) is composite, so its junction column cannot be inferred; declare its junction key columns in %s (PRD §4.8)",
			source, r.Name, qualifiedTableName(tSchema, tName), strings.Join(pk, ", "), key,
		)
	}
	return toSingular(tName) + "_id", nil
}

// primaryKeyColumnNames returns table's primary-key column names in column
// order, or nil for a nil table; declaredPrimaryKey gives the PRIMARY KEY (...)
// order.
func primaryKeyColumnNames(table *parser.Table) []string {
	if table == nil {
		return nil
	}
	var pk []string
	for _, c := range table.Columns {
		if c.PrimaryKey {
			pk = append(pk, c.Name)
		}
	}
	return pk
}

// checkDeclaredJunctionKeys rejects a declared m2m entry whose junction keys
// declaredJunctionKeys refuses: one that can be neither read nor inferred, or
// two that name the same columns. Any other entry passes.
func checkDeclaredJunctionKeys(schema *parser.Schema, r config.TableRelationship, source string, targets *relationshipTargets) error {
	if parseRelationshipType(r.Type) != parser.ManyToMany {
		return nil
	}
	_, _, err := declaredJunctionKeys(schema, r, source, targets)
	return err
}

// declaredJunctionKey is a junction key's columns: the override as written,
// else the inferred column, else none.
func declaredJunctionKey(override config.KeyColumns, resolved string) config.KeyColumns {
	switch {
	case len(override) > 0:
		return override
	case resolved != "":
		return config.KeyColumns{resolved}
	}
	return nil
}

// declaredKey maps a declared key's columns by position onto the referenced
// table's primary key (PRD §13.4). Post-parse validation rejects a list whose
// length differs from the key's, so a shorter side only truncates the pairs.
func declaredKey(cols config.KeyColumns, pk []string) []parser.KeyPair {
	n := min(len(cols), len(pk))
	if n == 0 {
		return nil
	}
	pairs := make([]parser.KeyPair, n)
	for i := range n {
		pairs[i] = parser.KeyPair{Local: cols[i], Referenced: pk[i]}
	}
	return pairs
}
