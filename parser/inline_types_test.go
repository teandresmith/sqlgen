package parser_test

import (
	"testing"

	"github.com/teandresmith/sqlgen/parser"
)

func TestInlineTypeNames(t *testing.T) {
	tests := []struct {
		name          string
		table, column string
		wantEnum      string
		wantSet       string
	}{
		{name: "plain", table: "users", column: "role", wantEnum: "users__role_enum", wantSet: "users__role_set"},
		{name: "underscore in column", table: "a", column: "b_c", wantEnum: "a__b_c_enum", wantSet: "a__b_c_set"},
		{name: "underscore in table", table: "a_b", column: "c", wantEnum: "a_b__c_enum", wantSet: "a_b__c_set"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parser.InlineEnumName(tt.table, tt.column); got != tt.wantEnum {
				t.Errorf("InlineEnumName(%q, %q) = %q, want %q", tt.table, tt.column, got, tt.wantEnum)
			}
			if got := parser.InlineSetName(tt.table, tt.column); got != tt.wantSet {
				t.Errorf("InlineSetName(%q, %q) = %q, want %q", tt.table, tt.column, got, tt.wantSet)
			}
		})
	}
}
