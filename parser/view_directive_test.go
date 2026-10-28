package parser_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/parser"
)

// TestParseViewAnnotations_UnrecognizedDirectives pins that a directive the
// parser does not know is ignored and listed, in file order, for the caller
// to warn about, and that a known directive is not listed.
func TestParseViewAnnotations_UnrecognizedDirectives(t *testing.T) {
	tests := []struct {
		name          string
		content       string
		want          []string
		wantNoTypeFor string
	}{
		{name: "known directives only", content: "-- @pk: id\n-- @type n: int32\n-- @nullable: n\nCREATE VIEW v AS SELECT id, n FROM t;"},
		{name: "one unknown directive", content: "-- @pk: id\n-- @primary: id\nCREATE VIEW v AS SELECT id FROM t;", want: []string{"@primary"}},
		{name: "a word that starts with @type", content: "-- @typeface n: int32\nCREATE VIEW v AS SELECT n FROM t;", want: []string{"@typeface n"}, wantNoTypeFor: "face n"},
		{name: "two unknown directives in order", content: "-- @b: x\n-- @a: y\nCREATE VIEW v AS SELECT id FROM t;", want: []string{"@b", "@a"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parser.ParseViewAnnotations(tt.content)
			if err != nil {
				t.Fatalf("ParseViewAnnotations: %v", err)
			}
			if diff := cmp.Diff(tt.want, got.Unrecognized); diff != "" {
				t.Errorf("Unrecognized (-want +got):\n%s", diff)
			}
			if _, ok := got.TypeOverrides[tt.wantNoTypeFor]; tt.wantNoTypeFor != "" && ok {
				t.Errorf("TypeOverrides = %v, want no @type override for %q", got.TypeOverrides, tt.wantNoTypeFor)
			}
		})
	}
}
