package types

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// TestJSON_TypeDefinition pins that JSON is defined as json.RawMessage: the
// two convert into each other and share the underlying []byte type.
func TestJSON_TypeDefinition(t *testing.T) {
	raw := json.RawMessage(`{"a":1}`)
	j := JSON(raw)
	back := json.RawMessage(j)
	if string(back) != string(raw) {
		t.Errorf("json.RawMessage(JSON(raw)) = %q, want %q", back, raw)
	}

	jt := reflect.TypeFor[JSON]()
	rt := reflect.TypeFor[json.RawMessage]()
	if jt.Kind() != reflect.Slice || jt.Elem() != reflect.TypeFor[byte]() {
		t.Errorf("JSON kind = %v of %v, want slice of uint8", jt.Kind(), jt.Elem())
	}
	if !jt.ConvertibleTo(rt) || !rt.ConvertibleTo(jt) {
		t.Errorf("JSON and json.RawMessage are not mutually convertible")
	}
	if jt.Kind() != rt.Kind() || jt.Elem() != rt.Elem() {
		t.Errorf("JSON underlying = %v of %v, want %v of %v", jt.Kind(), jt.Elem(), rt.Kind(), rt.Elem())
	}
}

// TestJSON_MarshalJSON_NilEmitsNull pins that MarshalJSON on a nil JSON
// emits the literal null.
func TestJSON_MarshalJSON_NilEmitsNull(t *testing.T) {
	var j JSON
	got, err := j.MarshalJSON()
	if err != nil {
		t.Fatalf("JSON(nil).MarshalJSON() unexpected error: %v", err)
	}
	if string(got) != "null" {
		t.Errorf("JSON(nil).MarshalJSON() = %q, want %q", got, "null")
	}
}

// TestJSON_StructuralProbes_NoFullValidation pins that IsObject and IsArray
// look only at the first non-whitespace byte: a malformed payload behind a
// leading { or [ still reports true.
func TestJSON_StructuralProbes_NoFullValidation(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantObject bool
		wantArray  bool
	}{
		{name: "malformed object", input: `{"a":`, wantObject: true},
		{name: "malformed object with leading whitespace", input: " \t\r\n{not json", wantObject: true},
		{name: "malformed array", input: `[1,,`, wantArray: true},
		{name: "malformed array with leading whitespace", input: " \t\r\n[}", wantArray: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j := JSON(tt.input)
			if json.Valid(j) {
				t.Fatalf("input %q is valid JSON; the case needs a malformed payload", tt.input)
			}
			if got := j.IsObject(); got != tt.wantObject {
				t.Errorf("JSON(%q).IsObject() = %v, want %v", tt.input, got, tt.wantObject)
			}
			if got := j.IsArray(); got != tt.wantArray {
				t.Errorf("JSON(%q).IsArray() = %v, want %v", tt.input, got, tt.wantArray)
			}
		})
	}
}

// TestJSON_StringFormatting pins that String returns string(j) and that %v
// and %s emit the raw document, with no truncation of a large one.
func TestJSON_StringFormatting(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "object", input: `{"a": [1, 2], "b": "x"}`},
		{name: "array", input: `[ {"year":1} ]`},
		{name: "large document", input: surfaceLargeJSON()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j := JSON(tt.input)
			if got := j.String(); got != string(j) {
				t.Errorf("JSON.String() = %q, want %q", got, string(j))
			}
			if got := fmt.Sprintf("%v", j); got != tt.input {
				t.Errorf("fmt.Sprintf(%%v) len = %d, want %d (raw document)", len(got), len(tt.input))
			}
			if got := fmt.Sprintf("%s", j); got != tt.input { //nolint:staticcheck // the %s verb itself is under test
				t.Errorf("fmt.Sprintf(%%s) len = %d, want %d (raw document)", len(got), len(tt.input))
			}
		})
	}
}

// surfaceLargeJSON returns a valid JSON array well past any plausible
// display truncation length.
func surfaceLargeJSON() string {
	var b strings.Builder
	b.WriteByte('[')
	for i := range 10000 {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"i":%d}`, i)
	}
	b.WriteByte(']')
	return b.String()
}
