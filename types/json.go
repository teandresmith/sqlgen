// Package types provides database-aware Go types for generated code.
package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"io"
)

// JSON holds a raw JSON document of any shape — object, array, scalar, or
// null. It is the default Go type for `json` and `jsonb` columns when no
// typed Go struct is configured. Decode opportunistically with AsMap /
// AsSlice / Decode, or override the column's go_type to a concrete struct
// via overrides.types or per-column override for typed access at the field
// level.
//
// JSON is a reference type: nullability is conveyed by `nil`. A SQL NULL
// scans as a nil JSON, and Value() on a nil/empty JSON returns SQL NULL.
type JSON json.RawMessage

// Scan implements the sql.Scanner interface. It accepts []byte or string
// from the database and stores a defensive copy of the raw bytes (drivers
// such as pgx may reuse row buffers across rows). SQL NULL produces a nil
// JSON. No shape validation is performed.
func (j *JSON) Scan(src any) error {
	if src == nil {
		*j = nil
		return nil
	}

	switch v := src.(type) {
	case []byte:
		if v == nil {
			*j = nil
			return nil
		}
		buf := make([]byte, len(v))
		copy(buf, v)
		*j = buf
		return nil
	case string:
		*j = []byte(v)
		return nil
	default:
		return fmt.Errorf("scanning %T into JSON", src)
	}
}

// Value implements the driver.Valuer interface. It returns the raw bytes
// verbatim. A nil or empty JSON returns SQL NULL.
func (j JSON) Value() (driver.Value, error) {
	if len(j) == 0 {
		return nil, nil
	}
	return []byte(j), nil
}

// MarshalJSON delegates to json.RawMessage.MarshalJSON. A nil JSON emits
// the literal `null`.
func (j JSON) MarshalJSON() ([]byte, error) {
	b, err := json.RawMessage(j).MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("marshal JSON: %w", err)
	}
	return b, nil
}

// UnmarshalJSON delegates to json.RawMessage.UnmarshalJSON, which validates
// that the input is well-formed JSON.
func (j *JSON) UnmarshalJSON(data []byte) error {
	if err := (*json.RawMessage)(j).UnmarshalJSON(data); err != nil {
		return fmt.Errorf("unmarshal JSON: %w", err)
	}
	return nil
}

// MarshalGQL implements the gqlgen Marshaler interface. It writes the raw
// bytes verbatim; a nil or empty JSON writes the literal `null`. The
// runtime module is stdlib-only, so this method takes io.Writer rather
// than gqlgen's own type.
func (j JSON) MarshalGQL(w io.Writer) {
	if len(j) == 0 {
		_, _ = io.WriteString(w, "null")
		return
	}
	_, _ = w.Write([]byte(j))
}

// UnmarshalGQL implements the gqlgen Unmarshaler interface. It accepts any
// shape gqlgen produces (`map[string]any`, `[]any`, primitives, `nil`) and
// re-encodes the value to raw JSON bytes. Unlike the prior JSONMap, this
// does not reject non-object inputs — shape validation is a resolver-layer
// concern (see PRD §26.4.1).
func (j *JSON) UnmarshalGQL(v any) error {
	if v == nil {
		*j = nil
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal JSON from %T: %w", v, err)
	}
	*j = b
	return nil
}

// Decode is the typed-decode escape hatch. It delegates to json.Unmarshal,
// so any target shape supported by encoding/json works. An empty JSON
// returns the stdlib's "unexpected end of JSON input" error wrapped with
// context; the JSON `null` literal nils out the target via standard
// encoding/json semantics.
func (j JSON) Decode(v any) error {
	if err := json.Unmarshal(j, v); err != nil {
		return fmt.Errorf("decode JSON: %w", err)
	}
	return nil
}

// AsMap is a strict object decode. It returns an error when the value is
// not a JSON object — callers that want to branch without decoding should
// probe with IsObject first.
func (j JSON) AsMap() (map[string]any, error) {
	if !j.IsObject() {
		return nil, fmt.Errorf("JSON value is not an object")
	}
	m := make(map[string]any)
	if err := json.Unmarshal(j, &m); err != nil {
		return nil, fmt.Errorf("decode JSON object: %w", err)
	}
	return m, nil
}

// AsSlice is a strict array decode. It returns an error when the value is
// not a JSON array — callers that want to branch without decoding should
// probe with IsArray first.
func (j JSON) AsSlice() ([]any, error) {
	if !j.IsArray() {
		return nil, fmt.Errorf("JSON value is not an array")
	}
	var s []any
	if err := json.Unmarshal(j, &s); err != nil {
		return nil, fmt.Errorf("decode JSON array: %w", err)
	}
	return s, nil
}

// IsNull reports whether the value is SQL NULL (empty bytes) or holds the
// JSON `null` literal. The two are indistinguishable to most application
// code; use len(j) == 0 directly when you must distinguish.
func (j JSON) IsNull() bool {
	b := jsonFirstByte(j)
	if b == 0 {
		return true
	}
	return b == 'n' && string(j[skipWhitespace(j):]) == "null"
}

// IsObject reports whether the first non-whitespace byte is `{`. This is a
// cheap structural probe and does not validate the entire payload.
func (j JSON) IsObject() bool {
	return jsonFirstByte(j) == '{'
}

// IsArray reports whether the first non-whitespace byte is `[`. This is a
// cheap structural probe and does not validate the entire payload.
func (j JSON) IsArray() bool {
	return jsonFirstByte(j) == '['
}

// String returns the raw JSON document as a Go string. %v and %s formatting
// emit the document verbatim; there is no truncation.
func (j JSON) String() string {
	return string(j)
}

// jsonFirstByte returns the first non-whitespace byte of j, or 0 when j is
// empty or contains only whitespace.
func jsonFirstByte(j JSON) byte {
	i := skipWhitespace(j)
	if i >= len(j) {
		return 0
	}
	return j[i]
}

// skipWhitespace returns the index of the first non-whitespace byte in j.
// Whitespace is per RFC 8259: space, tab, CR, LF.
func skipWhitespace(j JSON) int {
	for i, c := range j {
		switch c {
		case ' ', '\t', '\r', '\n':
			continue
		default:
			return i
		}
	}
	return len(j)
}
