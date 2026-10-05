package types

import (
	"database/sql/driver"
	"fmt"
	"io"
	"math"
	"strconv"
	"sync"
	"time"
)

// builtinLayouts are the time formats tried (in order) when scanning a
// string or []byte value into a DateTime. They cover the most common
// representations produced by SQLite's built-in date/time functions and
// by third-party tools.
//
// Users can append additional layouts via RegisterLayout.
var builtinLayouts = []string{
	"2006-01-02 15:04:05",                 // SQLite datetime() output
	"2006-01-02T15:04:05Z07:00",           // RFC 3339
	"2006-01-02T15:04:05.999999999Z07:00", // RFC 3339 Nano
	"2006-01-02 15:04:05.999999999",       // datetime with fractional seconds
	"2006-01-02 15:04:05Z07:00",           // datetime with timezone
	"2006-01-02",                          // date only
}

// layoutMu guards customLayouts and outputLayout for concurrent access.
var layoutMu sync.RWMutex

// customLayouts holds user-registered parse layouts appended via RegisterLayout.
var customLayouts []string

// outputLayout is the format used by DateTime.Value and NullDateTime.Value
// when marshaling a time to the database. Default is time.RFC3339Nano.
var outputLayout = time.RFC3339Nano

// RegisterLayout appends a time parse layout to the set of formats tried
// when scanning text values into DateTime or NullDateTime. Built-in
// layouts are always tried first; registered layouts are tried in the
// order they were added.
//
// RegisterLayout is safe for concurrent use.
func RegisterLayout(layout string) {
	layoutMu.Lock()
	defer layoutMu.Unlock()
	customLayouts = append(customLayouts, layout)
}

// SetOutputLayout sets the time layout used by DateTime.Value and
// NullDateTime.Value when marshaling to the database. The default is
// time.RFC3339Nano.
//
// SetOutputLayout is safe for concurrent use.
func SetOutputLayout(layout string) {
	layoutMu.Lock()
	defer layoutMu.Unlock()
	outputLayout = layout
}

// getOutputLayout returns the current output layout under a read lock.
func getOutputLayout() string {
	layoutMu.RLock()
	defer layoutMu.RUnlock()
	return outputLayout
}

// getCustomLayouts returns a snapshot of user-registered layouts under a read lock.
func getCustomLayouts() []string {
	layoutMu.RLock()
	defer layoutMu.RUnlock()
	return customLayouts
}

// julianDayEpoch is the Julian day number for the Unix epoch (1970-01-01).
const julianDayEpoch = 2440587.5

// DateTime wraps time.Time for SQLite datetime columns. It implements
// sql.Scanner and driver.Valuer to handle the various representations
// that SQLite drivers may produce (text, Unix epoch, Julian day, or a
// pre-parsed time.Time).
//
// Input layouts (for scanning) and the output layout (for Value) are
// configurable via RegisterLayout and SetOutputLayout.
type DateTime struct {
	time.Time
}

// Scan implements the sql.Scanner interface. It accepts:
//   - string or []byte: parsed using built-in layouts, then any
//     user-registered layouts (see RegisterLayout)
//   - int64: interpreted as a Unix epoch timestamp (seconds)
//   - float64: interpreted as a Julian day number
//   - time.Time: used directly (some drivers pre-parse datetime columns)
//   - nil: sets the embedded time to its zero value
func (dt *DateTime) Scan(src any) error {
	if src == nil {
		dt.Time = time.Time{}
		return nil
	}

	switch v := src.(type) {
	case time.Time:
		dt.Time = v
		return nil
	case string:
		return dt.parseText(v)
	case []byte:
		return dt.parseText(string(v))
	case int64:
		dt.Time = time.Unix(v, 0).UTC()
		return nil
	case float64:
		dt.Time = julianDayToTime(v)
		return nil
	default:
		return fmt.Errorf("scanning %T into DateTime", src)
	}
}

// parseText tries each layout in order (built-in first, then custom) and
// returns the first successful parse. Returns an error if no layout matches.
func (dt *DateTime) parseText(s string) error {
	extras := getCustomLayouts()

	for _, layout := range builtinLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			dt.Time = t
			return nil
		}
	}
	for _, layout := range extras {
		if t, err := time.Parse(layout, s); err == nil {
			dt.Time = t
			return nil
		}
	}
	return fmt.Errorf("parsing %q into DateTime: no matching layout", s)
}

// Value implements the driver.Valuer interface. It formats the time using
// the current output layout (default time.RFC3339Nano). The output layout
// is configurable via SetOutputLayout.
func (dt DateTime) Value() (driver.Value, error) {
	return dt.Format(getOutputLayout()), nil
}

// MarshalGQL implements the gqlgen Marshaler interface. It writes the time as
// a JSON-quoted RFC3339Nano string. The runtime module is stdlib-only, so this
// method takes io.Writer rather than gqlgen's own type.
func (dt DateTime) MarshalGQL(w io.Writer) {
	_, _ = io.WriteString(w, strconv.Quote(dt.Format(time.RFC3339Nano)))
}

// UnmarshalGQL implements the gqlgen Unmarshaler interface. It accepts a
// string formatted per RFC3339 (with or without nanoseconds). Any other input
// type is rejected; an empty or unparseable string is rejected with an error.
func (dt *DateTime) UnmarshalGQL(v any) error {
	s, ok := v.(string)
	if !ok {
		return fmt.Errorf("DateTime must be a string, got %T", v)
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, err = time.Parse(time.RFC3339, s)
		if err != nil {
			return fmt.Errorf("DateTime parse %q: %w", s, err)
		}
	}
	dt.Time = t
	return nil
}

// NullDateTime represents a nullable SQLite datetime column. It implements
// sql.Scanner and driver.Valuer with the same defensive parsing as
// DateTime. A nil or NULL value sets Valid to false.
//
// Input layouts (for scanning) and the output layout (for Value) are
// configurable via RegisterLayout and SetOutputLayout.
type NullDateTime struct {
	Time  time.Time
	Valid bool
}

// Scan implements the sql.Scanner interface. It accepts the same types as
// DateTime.Scan. A nil source sets Valid to false and zeroes the Time
// field.
func (n *NullDateTime) Scan(src any) error {
	if src == nil {
		n.Time = time.Time{}
		n.Valid = false
		return nil
	}

	var dt DateTime
	if err := dt.Scan(src); err != nil {
		return err
	}
	n.Time = dt.Time
	n.Valid = true
	return nil
}

// Value implements the driver.Valuer interface. If Valid is false, it
// returns nil (SQL NULL). Otherwise it formats the time using the current
// output layout (default time.RFC3339Nano, configurable via
// SetOutputLayout).
func (n NullDateTime) Value() (driver.Value, error) {
	if !n.Valid {
		return nil, nil
	}
	return n.Time.Format(getOutputLayout()), nil
}

// MarshalGQL implements the gqlgen Marshaler interface. When Valid is false
// it writes the literal `null`; otherwise it writes a JSON-quoted RFC3339Nano
// string. The runtime module is stdlib-only, so this method takes io.Writer
// rather than gqlgen's own type.
func (n NullDateTime) MarshalGQL(w io.Writer) {
	if !n.Valid {
		_, _ = io.WriteString(w, "null")
		return
	}
	_, _ = io.WriteString(w, strconv.Quote(n.Time.Format(time.RFC3339Nano)))
}

// UnmarshalGQL implements the gqlgen Unmarshaler interface. It accepts nil
// (sets Valid=false) or an RFC3339 string (sets Valid=true and parses the
// value). Any other input type is rejected.
func (n *NullDateTime) UnmarshalGQL(v any) error {
	if v == nil {
		n.Time = time.Time{}
		n.Valid = false
		return nil
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Errorf("NullDateTime must be a string or null, got %T", v)
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, err = time.Parse(time.RFC3339, s)
		if err != nil {
			return fmt.Errorf("NullDateTime parse %q: %w", s, err)
		}
	}
	n.Time = t
	n.Valid = true
	return nil
}

// julianDayToTime converts a Julian day number to a time.Time in UTC.
func julianDayToTime(jd float64) time.Time {
	unixSec := (jd - julianDayEpoch) * 86400
	sec := int64(unixSec)
	nsec := int64(math.Round((unixSec - float64(sec)) * 1e9))
	return time.Unix(sec, nsec).UTC()
}
