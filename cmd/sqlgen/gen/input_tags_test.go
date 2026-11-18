package gen_test

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// Input structs are never scanned from a row, so each field carries a json
// tag naming its column and no db tag.
func TestInputTemplates_FieldsCarryJSONTagOnly(t *testing.T) {
	fieldTag := regexp.MustCompile("(?m)^\\s+\\w+\\s+[^`\\n]+`([^`]*)`")
	fieldLine := regexp.MustCompile(`(?m)^\s+[A-Z]\w*\s+[^\s/]`)
	ctx := testInputProductsContext()
	columns := map[string]bool{}
	for _, c := range ctx.Columns {
		columns[c.Name] = true
	}

	tests := []struct {
		name   string
		render func(*testing.T) string
	}{
		{name: "create input", render: func(t *testing.T) string { return executeCreateInputTemplate(t, ctx) }},
		{name: "update input", render: func(t *testing.T) string { return executeUpdateInputTemplate(t, ctx) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := tt.render(t)
			matches := fieldTag.FindAllStringSubmatch(out, -1)
			if len(matches) == 0 {
				t.Fatalf("no tagged fields in output:\n%s", out)
			}
			if fields := fieldLine.FindAllString(out, -1); len(fields) != len(matches) {
				t.Errorf("%d field lines but %d carry a tag; every input field carries a json tag:\n%s", len(fields), len(matches), out)
			}
			for _, m := range matches {
				tag := reflect.StructTag(m[1])
				if db, ok := tag.Lookup("db"); ok {
					t.Errorf("field %q carries db:%q; input fields carry no db tag", m[0], db)
				}
				name, _, _ := strings.Cut(tag.Get("json"), ",")
				if !columns[name] {
					t.Errorf("field %q json tag names %q, not a column of %s", m[0], name, ctx.TableName)
				}
			}
		})
	}
}
