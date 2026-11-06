package gen

import (
	"errors"
	"fmt"
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
// the error names the relationship and the key to declare. Callers run
// checkDeclaredTarget first, so the related table resolves.
func declaredJunctionKeys(schema *parser.Schema, r config.TableRelationship, source string, targets *relationshipTargets) (local, reference string, err error) {
	sSchema, sName := splitQualifiedName(source)
	target, _ := targets.resolve(splitQualifiedName(r.Table))
	_, tName := splitQualifiedName(r.Table)

	local, localErr := junctionKeyColumn(schema, r, source, "junction_local_fk", r.JunctionLocalFK, sSchema, sName)
	reference, refErr := junctionKeyColumn(schema, r, source, "junction_reference_fk", r.JunctionReferenceFK, target.Schema, tName)
	return local, reference, errors.Join(localErr, refErr)
}

// junctionKeyColumn resolves one side of declaredJunctionKeys: the override's
// first column, else the column inferred from the linked table (tSchema,
// tName). checkDeclaredKeyColumns rejects an override of more than one column.
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

// checkDeclaredJunctionKeys rejects a declared m2m entry a junction key of
// which can be neither read from the config nor inferred (declaredJunctionKeys).
// Any other entry passes.
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

// checkDeclaredKeyColumns rejects a declared key of more than one column until
// relationships through a composite key generate.
func checkDeclaredKeyColumns(r config.TableRelationship, source string) error {
	if generateCompositeKeyEdges {
		return nil
	}
	var errs []error
	for _, k := range []struct {
		name string
		cols config.KeyColumns
	}{
		{"fk", r.FK},
		{"junction_local_fk", r.JunctionLocalFK},
		{"junction_reference_fk", r.JunctionReferenceFK},
	} {
		if len(k.cols) > 1 {
			errs = append(errs, fmt.Errorf(
				"tables.%s.relationships: %q declares %s: %s, a key of %d columns; relationships through a composite key are not generated yet (PRD §13.4)",
				source, r.Name, k.name, k.cols, len(k.cols),
			))
		}
	}
	return errors.Join(errs...)
}
