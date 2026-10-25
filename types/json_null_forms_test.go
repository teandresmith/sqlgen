package types

import (
	"encoding/json"
	"testing"
)

func TestJSON_MarshalJSON_EmptyEmitsNull(t *testing.T) {
	tests := []struct {
		name string
		j    JSON
	}{
		{"nil", nil},
		{"empty", JSON([]byte{})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.j.MarshalJSON()
			if err != nil || string(got) != "null" {
				t.Errorf("MarshalJSON() = %q, %v, want %q, nil", got, err, "null")
			}
			wrapped, err := json.Marshal(struct{ V JSON }{tt.j})
			if err != nil || string(wrapped) != `{"V":null}` {
				t.Errorf("json.Marshal(struct) = %q, %v, want %q, nil", wrapped, err, `{"V":null}`)
			}
		})
	}
}

func TestJSON_IsNull_SurroundingWhitespace(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"empty", "", true},
		{"whitespace only", " \t\n ", true},
		{"null literal", "null", true},
		{"leading whitespace", "  null", true},
		{"trailing whitespace", "null  ", true},
		{"whitespace on both sides", "\n null \t", true},
		{"null followed by more", "nullx", false},
		{"null string", `"null"`, false},
		{"object", "{}", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := JSON(tt.input).IsNull(); got != tt.want {
				t.Errorf("JSON(%q).IsNull() = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestJSON_Scan_NilBytesIsNull(t *testing.T) {
	j := JSON(`{"stale":true}`)
	if err := j.Scan([]byte(nil)); err != nil {
		t.Fatalf("Scan([]byte(nil)) error = %v", err)
	}
	if j != nil {
		t.Errorf("Scan([]byte(nil)) stored %q, want nil JSON", j)
	}
	if v, err := j.Value(); v != nil || err != nil {
		t.Errorf("Value() after Scan([]byte(nil)) = %v, %v, want nil, nil", v, err)
	}
}
