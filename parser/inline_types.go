package parser

// InlineEnumName returns the name of the ENUM type a MySQL column declares
// inline: <table>__<column>_enum (PRD §5.2). MySQL has no named ENUM type, and
// two tables may give a column of one name different values, so the name is
// scoped to the table. The double underscore keeps table a, column b_c
// (a__b_c_enum) apart from table a_b, column c (a_b__c_enum).
func InlineEnumName(table, column string) string {
	return table + "__" + column + "_enum"
}

// InlineSetName returns the name of the SET type a MySQL column declares
// inline: <table>__<column>_set (PRD §5.2).
func InlineSetName(table, column string) string {
	return table + "__" + column + "_set"
}
