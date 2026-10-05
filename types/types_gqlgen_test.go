package types

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// The four canonical JSON shapes the polymorphic JSON type must round-trip
// through every codec path (Scan / Value / MarshalGQL / UnmarshalGQL /
// Decode). Object was the only shape the prior JSONMap supported; array
// was the production bug shape (column DEFAULT '[{"year":1,...}]'::jsonb
// breaking every fresh insert). Scalar and null are included to pin that
// nothing about jsonb columns forces an object shape.
var fourShapes = []struct {
	name string
	bs   []byte
}{
	{"object", []byte(`{"k":"v","n":1}`)},
	{"array", []byte(`[{"year":1,"value":0.014},{"year":2,"value":0.025}]`)},
	{"scalar", []byte(`"hello"`)},
	{"null", []byte(`null`)},
}

func TestJSON_ScanValue_RoundTrip(t *testing.T) {
	for _, tt := range fourShapes {
		t.Run(tt.name, func(t *testing.T) {
			var j JSON
			if err := j.Scan(tt.bs); err != nil {
				t.Fatalf("Scan: %v", err)
			}
			got, err := j.Value()
			if err != nil {
				t.Fatalf("Value: %v", err)
			}
			gotBytes, ok := got.([]byte)
			if !ok {
				t.Fatalf("Value returned %T, want []byte", got)
			}
			if !bytes.Equal(gotBytes, tt.bs) {
				t.Fatalf("round-trip mismatch:\n got: %s\nwant: %s", gotBytes, tt.bs)
			}
		})
	}
}

func TestJSON_Scan_SQLNullProducesNilJSON(t *testing.T) {
	j := JSON([]byte(`{"prev":"value"}`))
	if err := j.Scan(nil); err != nil {
		t.Fatalf("Scan(nil): %v", err)
	}
	if j != nil {
		t.Fatalf("expected nil JSON after Scan(nil), got %v", j)
	}
}

func TestJSON_Scan_AcceptsStringAndBytes(t *testing.T) {
	t.Run("string", func(t *testing.T) {
		var j JSON
		if err := j.Scan(`{"k":"v"}`); err != nil {
			t.Fatalf("Scan(string): %v", err)
		}
		if string(j) != `{"k":"v"}` {
			t.Fatalf("got %q", string(j))
		}
	})

	t.Run("bytes", func(t *testing.T) {
		var j JSON
		if err := j.Scan([]byte(`{"k":"v"}`)); err != nil {
			t.Fatalf("Scan(bytes): %v", err)
		}
		if string(j) != `{"k":"v"}` {
			t.Fatalf("got %q", string(j))
		}
	})

	t.Run("rejects other types", func(t *testing.T) {
		var j JSON
		err := j.Scan(42)
		if err == nil {
			t.Fatal("expected error for int input")
		}
		if !strings.Contains(err.Error(), "int") || !strings.Contains(err.Error(), "JSON") {
			t.Fatalf("error should name input and JSON, got %q", err.Error())
		}
	})
}

func TestJSON_Scan_DefensiveCopy(t *testing.T) {
	// pgx and other drivers reuse row buffers across rows. If Scan kept a
	// reference to the input []byte rather than copying, the next row read
	// would clobber any JSON we previously scanned. The contract is: Scan
	// stores a defensive copy.
	src := []byte(`{"original":"value"}`)
	var j JSON
	if err := j.Scan(src); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	for i := range src {
		src[i] = 'X'
	}

	if string(j) != `{"original":"value"}` {
		t.Fatalf("Scan did not copy: got %q after mutating source", string(j))
	}
}

func TestJSON_Value_NilAndEmptyReturnSQLNULL(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		var j JSON
		v, err := j.Value()
		if err != nil {
			t.Fatalf("Value: %v", err)
		}
		if v != driver.Value(nil) {
			t.Fatalf("expected SQL NULL, got %v", v)
		}
	})

	t.Run("empty slice", func(t *testing.T) {
		j := JSON([]byte{})
		v, err := j.Value()
		if err != nil {
			t.Fatalf("Value: %v", err)
		}
		if v != driver.Value(nil) {
			t.Fatalf("expected SQL NULL, got %v", v)
		}
	})
}

func TestJSON_MarshalUnmarshalJSON_RoundTrip(t *testing.T) {
	for _, tt := range fourShapes {
		t.Run(tt.name, func(t *testing.T) {
			j := JSON(tt.bs)
			out, err := json.Marshal(j)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if !bytes.Equal(out, tt.bs) {
				t.Fatalf("Marshal: got %s, want %s", out, tt.bs)
			}

			var back JSON
			if err := json.Unmarshal(out, &back); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if !bytes.Equal(back, tt.bs) {
				t.Fatalf("Unmarshal: got %s, want %s", back, tt.bs)
			}
		})
	}
}

func TestJSON_MarshalGQL_RoundTrip(t *testing.T) {
	for _, tt := range fourShapes {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			JSON(tt.bs).MarshalGQL(&buf)
			if got := buf.String(); got != string(tt.bs) {
				t.Fatalf("MarshalGQL: got %q, want %q", got, tt.bs)
			}
		})
	}

	t.Run("nil writes null", func(t *testing.T) {
		var buf bytes.Buffer
		var j JSON
		j.MarshalGQL(&buf)
		if buf.String() != "null" {
			t.Fatalf("nil MarshalGQL: got %q, want null", buf.String())
		}
	})

	t.Run("empty writes null", func(t *testing.T) {
		var buf bytes.Buffer
		j := JSON([]byte{})
		j.MarshalGQL(&buf)
		if buf.String() != "null" {
			t.Fatalf("empty MarshalGQL: got %q, want null", buf.String())
		}
	})
}

// TestJSON_UnmarshalGQL_AcceptsAnyShape pins the behavior change from the
// prior JSONMap: UnmarshalGQL accepts any JSON shape gqlgen produces from
// input variables — object (map[string]any), array ([]any), scalar
// (string/float64/bool), and nil. The prior JSONMap.UnmarshalGQL rejected
// non-object inputs; shape validation is a resolver-layer concern (see
// PRD §26.4.1).
func TestJSON_UnmarshalGQL_AcceptsAnyShape(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want string
	}{
		{"object", map[string]any{"k": "v"}, `{"k":"v"}`},
		{"array of objects", []any{map[string]any{"year": float64(1)}}, `[{"year":1}]`},
		{"array of primitives", []any{"a", "b"}, `["a","b"]`},
		{"string scalar", "hello", `"hello"`},
		{"number scalar", float64(42), `42`},
		{"bool scalar", true, `true`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var j JSON
			if err := j.UnmarshalGQL(tt.in); err != nil {
				t.Fatalf("UnmarshalGQL: %v", err)
			}
			if string(j) != tt.want {
				t.Fatalf("got %s, want %s", string(j), tt.want)
			}
		})
	}

	t.Run("nil sets nil JSON", func(t *testing.T) {
		j := JSON([]byte(`{"keep":"me"}`))
		if err := j.UnmarshalGQL(nil); err != nil {
			t.Fatalf("UnmarshalGQL(nil): %v", err)
		}
		if j != nil {
			t.Fatalf("expected nil JSON, got %v", j)
		}
	})
}

func TestJSON_Decode_RoundTrip(t *testing.T) {
	t.Run("object into map", func(t *testing.T) {
		j := JSON([]byte(`{"k":"v","n":1}`))
		var m map[string]any
		if err := j.Decode(&m); err != nil {
			t.Fatalf("Decode: %v", err)
		}
		if m["k"] != "v" || m["n"] != float64(1) {
			t.Fatalf("got %v", m)
		}
	})

	t.Run("array into typed slice", func(t *testing.T) {
		// The recommended pattern when the column shape is known: decode
		// directly into a typed slice rather than chaining AsSlice/conversions.
		type yearValue struct {
			Year  int     `json:"year"`
			Value float64 `json:"value"`
		}
		j := JSON([]byte(`[{"year":1,"value":0.014},{"year":2,"value":0.025}]`))
		var got []yearValue
		if err := j.Decode(&got); err != nil {
			t.Fatalf("Decode: %v", err)
		}
		if len(got) != 2 || got[0].Year != 1 || got[1].Value != 0.025 {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("null nils the target via stdlib semantics", func(t *testing.T) {
		j := JSON([]byte(`null`))
		m := map[string]any{"prev": "value"}
		if err := j.Decode(&m); err != nil {
			t.Fatalf("Decode(null): %v", err)
		}
		if m != nil {
			t.Fatalf("expected nil map after Decode(null), got %v", m)
		}
	})

	t.Run("empty wraps stdlib unexpected-end-of-input", func(t *testing.T) {
		var j JSON
		var v any
		err := j.Decode(&v)
		if err == nil {
			t.Fatal("expected error for empty JSON")
		}
		if !strings.Contains(err.Error(), "decode JSON") {
			t.Fatalf("error should wrap with `decode JSON`, got %q", err.Error())
		}
		if !strings.Contains(err.Error(), "unexpected end") {
			t.Fatalf("error should mention the stdlib reason, got %q", err.Error())
		}
	})
}

func TestJSON_AsMap_StrictObjectShape(t *testing.T) {
	t.Run("object decodes", func(t *testing.T) {
		j := JSON([]byte(`{"k":"v"}`))
		m, err := j.AsMap()
		if err != nil {
			t.Fatalf("AsMap: %v", err)
		}
		if m["k"] != "v" {
			t.Fatalf("got %v", m)
		}
	})

	t.Run("array errors", func(t *testing.T) {
		j := JSON([]byte(`[1,2,3]`))
		_, err := j.AsMap()
		if err == nil {
			t.Fatal("expected error for array")
		}
	})

	t.Run("scalar errors", func(t *testing.T) {
		j := JSON([]byte(`"hello"`))
		_, err := j.AsMap()
		if err == nil {
			t.Fatal("expected error for scalar")
		}
	})

	t.Run("null errors", func(t *testing.T) {
		j := JSON([]byte(`null`))
		_, err := j.AsMap()
		if err == nil {
			t.Fatal("expected error for null")
		}
	})
}

func TestJSON_AsSlice_StrictArrayShape(t *testing.T) {
	t.Run("array decodes", func(t *testing.T) {
		j := JSON([]byte(`[1,"two",3]`))
		s, err := j.AsSlice()
		if err != nil {
			t.Fatalf("AsSlice: %v", err)
		}
		if len(s) != 3 || s[0] != float64(1) || s[1] != "two" {
			t.Fatalf("got %v", s)
		}
	})

	t.Run("object errors", func(t *testing.T) {
		j := JSON([]byte(`{"k":"v"}`))
		_, err := j.AsSlice()
		if err == nil {
			t.Fatal("expected error for object")
		}
	})

	t.Run("scalar errors", func(t *testing.T) {
		j := JSON([]byte(`42`))
		_, err := j.AsSlice()
		if err == nil {
			t.Fatal("expected error for scalar")
		}
	})

	t.Run("null errors", func(t *testing.T) {
		j := JSON([]byte(`null`))
		_, err := j.AsSlice()
		if err == nil {
			t.Fatal("expected error for null")
		}
	})
}

func TestJSON_StructuralProbes(t *testing.T) {
	tests := []struct {
		name      string
		in        []byte
		isNull    bool
		isObject  bool
		isArray   bool
		strRepr   string
		probesAny bool
	}{
		{"empty", []byte{}, true, false, false, "", false},
		{"sql null bytes", nil, true, false, false, "", false},
		{"json null literal", []byte(`null`), true, false, false, "null", false},
		{"object", []byte(`{"k":"v"}`), false, true, false, `{"k":"v"}`, true},
		{"array", []byte(`[1,2]`), false, false, true, `[1,2]`, true},
		{"scalar string", []byte(`"hello"`), false, false, false, `"hello"`, false},
		{"scalar number", []byte(`42`), false, false, false, `42`, false},
		{"scalar true", []byte(`true`), false, false, false, `true`, false},
		{"object with leading whitespace", []byte(" \t\n{}"), false, true, false, " \t\n{}", true},
		{"array with leading whitespace", []byte("  [1]"), false, false, true, "  [1]", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j := JSON(tt.in)
			if got := j.IsNull(); got != tt.isNull {
				t.Errorf("IsNull: got %v, want %v", got, tt.isNull)
			}
			if got := j.IsObject(); got != tt.isObject {
				t.Errorf("IsObject: got %v, want %v", got, tt.isObject)
			}
			if got := j.IsArray(); got != tt.isArray {
				t.Errorf("IsArray: got %v, want %v", got, tt.isArray)
			}
			if got := j.String(); got != tt.strRepr {
				t.Errorf("String: got %q, want %q", got, tt.strRepr)
			}
		})
	}
}

// TestJSON_ProductionBugRepro pins the original bug shape: a column with
// `DEFAULT '[{"year":1,"value":0.014}]'::jsonb` produces bytes that the
// prior JSONMap (map[string]any) could not unmarshal. With types.JSON
// the same bytes round-trip through Scan/Value/AsSlice/Decode unchanged.
func TestJSON_ProductionBugRepro(t *testing.T) {
	driverBytes := []byte(`[{"year":1,"value":0.014},{"year":2,"value":0.025}]`)
	var j JSON
	if err := j.Scan(driverBytes); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if !j.IsArray() {
		t.Fatal("expected IsArray=true")
	}

	type yearValue struct {
		Year  int     `json:"year"`
		Value float64 `json:"value"`
	}
	var typed []yearValue
	if err := j.Decode(&typed); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(typed) != 2 || typed[1].Year != 2 || typed[1].Value != 0.025 {
		t.Fatalf("got %+v", typed)
	}

	v, err := j.Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	if !bytes.Equal(v.([]byte), driverBytes) {
		t.Fatalf("Value round-trip mismatch")
	}
}

func TestDateTime_MarshalGQL_RoundTrip(t *testing.T) {
	want := time.Date(2026, 4, 29, 10, 30, 45, 123456789, time.UTC)
	dt := DateTime{Time: want}

	var buf bytes.Buffer
	dt.MarshalGQL(&buf)

	got := buf.String()
	if !strings.HasPrefix(got, "\"") || !strings.HasSuffix(got, "\"") {
		t.Fatalf("MarshalGQL output must be a quoted string, got %q", got)
	}

	var parsed DateTime
	if err := parsed.UnmarshalGQL(strings.Trim(got, "\"")); err != nil {
		t.Fatalf("UnmarshalGQL roundtrip: %v", err)
	}
	if !parsed.Equal(want) {
		t.Fatalf("roundtrip mismatch: got %v, want %v", parsed.Time, want)
	}
}

func TestDateTime_UnmarshalGQL_Errors(t *testing.T) {
	t.Run("non-string input", func(t *testing.T) {
		var dt DateTime
		err := dt.UnmarshalGQL(42)
		if err == nil {
			t.Fatal("expected error for int input")
		}
	})

	t.Run("invalid string", func(t *testing.T) {
		var dt DateTime
		err := dt.UnmarshalGQL("not a date")
		if err == nil {
			t.Fatal("expected error for invalid string")
		}
	})

	t.Run("RFC3339 without nanos", func(t *testing.T) {
		var dt DateTime
		if err := dt.UnmarshalGQL("2026-04-29T10:30:45Z"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if dt.Year() != 2026 || dt.Second() != 45 {
			t.Fatalf("unexpected parsed time: %v", dt.Time)
		}
	})
}

func TestNullDateTime_MarshalGQL(t *testing.T) {
	t.Run("invalid marshals to null", func(t *testing.T) {
		var n NullDateTime
		var buf bytes.Buffer
		n.MarshalGQL(&buf)
		if buf.String() != "null" {
			t.Fatalf("expected null, got %q", buf.String())
		}
	})

	t.Run("valid marshals to quoted string", func(t *testing.T) {
		n := NullDateTime{Time: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), Valid: true}
		var buf bytes.Buffer
		n.MarshalGQL(&buf)
		got := buf.String()
		if !strings.HasPrefix(got, "\"") || got == "null" {
			t.Fatalf("expected quoted string, got %q", got)
		}
	})
}

func TestNullDateTime_UnmarshalGQL(t *testing.T) {
	t.Run("nil sets Valid=false", func(t *testing.T) {
		n := NullDateTime{Time: time.Now(), Valid: true}
		if err := n.UnmarshalGQL(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if n.Valid {
			t.Fatal("expected Valid=false after nil unmarshal")
		}
		if !n.Time.IsZero() {
			t.Fatalf("expected zero time, got %v", n.Time)
		}
	})

	t.Run("string sets Valid=true", func(t *testing.T) {
		var n NullDateTime
		if err := n.UnmarshalGQL("2026-04-29T10:30:45Z"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !n.Valid {
			t.Fatal("expected Valid=true after string unmarshal")
		}
	})

	t.Run("rejects non-string non-nil input", func(t *testing.T) {
		var n NullDateTime
		err := n.UnmarshalGQL(42)
		if err == nil {
			t.Fatal("expected error for int input")
		}
	})

	t.Run("rejects invalid string", func(t *testing.T) {
		var n NullDateTime
		err := n.UnmarshalGQL("garbage")
		if err == nil || errors.Is(err, errSentinelUnused) {
			t.Fatalf("expected parse error, got %v", err)
		}
	})
}

// errSentinelUnused only exists so the rejects-invalid-string test can use
// errors.Is with a deliberately distinct sentinel that won't match the parse
// error — keeping the test honest about checking that some error came back.
var errSentinelUnused = errors.New("unused")
