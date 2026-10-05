package types

import (
	"testing"
	"time"
)

// resetCustomState restores package-level layout state between tests.
func resetCustomState(t *testing.T) {
	t.Helper()
	layoutMu.Lock()
	customLayouts = nil
	outputLayout = time.RFC3339Nano
	layoutMu.Unlock()
}

// --- DateTime.Scan ---

func TestDateTimeScan(t *testing.T) {
	refTime := time.Date(2024, 3, 15, 10, 30, 45, 0, time.UTC)

	tests := []struct {
		name    string
		src     any
		want    time.Time
		wantErr bool
	}{
		// string — built-in layouts
		{
			name: "string: SQLite datetime format",
			src:  "2024-03-15 10:30:45",
			want: refTime,
		},
		{
			name: "string: RFC3339",
			src:  "2024-03-15T10:30:45Z",
			want: refTime,
		},
		{
			name: "string: RFC3339Nano",
			src:  "2024-03-15T10:30:45.123456789Z",
			want: time.Date(2024, 3, 15, 10, 30, 45, 123456789, time.UTC),
		},
		{
			name: "string: datetime with fractional seconds",
			src:  "2024-03-15 10:30:45.500000000",
			want: time.Date(2024, 3, 15, 10, 30, 45, 500000000, time.UTC),
		},
		{
			name: "string: datetime with timezone",
			src:  "2024-03-15 10:30:45+00:00",
			want: refTime,
		},
		{
			name: "string: date only",
			src:  "2024-03-15",
			want: time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
		},
		{
			name:    "string: unrecognized format",
			src:     "March 15, 2024",
			wantErr: true,
		},
		// []byte
		{
			name: "bytes: SQLite datetime format",
			src:  []byte("2024-03-15 10:30:45"),
			want: refTime,
		},
		// int64 — Unix epoch
		{
			name: "int64: Unix epoch",
			src:  int64(1710498645), // 2024-03-15 10:30:45 UTC
			want: refTime,
		},
		{
			name: "int64: Unix epoch zero",
			src:  int64(0),
			want: time.Unix(0, 0).UTC(),
		},
		// float64 — Julian day
		{
			name: "float64: Julian day for Unix epoch",
			src:  2440587.5, // 1970-01-01 00:00:00 UTC
			want: time.Unix(0, 0).UTC(),
		},
		{
			name: "float64: Julian day for 2024-03-15 12:00:00",
			src:  2460385.0, // 2024-03-15 12:00:00 UTC (noon = .0 Julian)
			want: time.Date(2024, 3, 15, 12, 0, 0, 0, time.UTC),
		},
		// time.Time — pre-parsed by driver
		{
			name: "time.Time: pre-parsed",
			src:  refTime,
			want: refTime,
		},
		// nil
		{
			name: "nil: zero time",
			src:  nil,
			want: time.Time{},
		},
		// unsupported type
		{
			name:    "unsupported: bool",
			src:     true,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var dt DateTime
			err := dt.Scan(tt.src)

			if tt.wantErr {
				if err == nil {
					t.Errorf("Scan(%v) = nil, want error", tt.src)
				}
				return
			}
			if err != nil {
				t.Fatalf("Scan(%v) unexpected error: %v", tt.src, err)
			}
			if !dt.Equal(tt.want) {
				t.Errorf("Scan(%v) = %v, want %v", tt.src, dt.Time, tt.want)
			}
		})
	}
}

// --- DateTime.Value ---

func TestDateTimeValue(t *testing.T) {
	resetCustomState(t)

	refTime := time.Date(2024, 3, 15, 10, 30, 45, 0, time.UTC)
	dt := DateTime{Time: refTime}

	val, err := dt.Value()
	if err != nil {
		t.Fatalf("Value() unexpected error: %v", err)
	}

	got, ok := val.(string)
	if !ok {
		t.Fatalf("Value() type = %T, want string", val)
	}

	want := refTime.Format(time.RFC3339Nano)
	if got != want {
		t.Errorf("Value() = %q, want %q", got, want)
	}
}

func TestDateTimeValueRoundTrip(t *testing.T) {
	resetCustomState(t)

	refTime := time.Date(2024, 3, 15, 10, 30, 45, 123456789, time.UTC)
	dt := DateTime{Time: refTime}

	val, err := dt.Value()
	if err != nil {
		t.Fatalf("Value() unexpected error: %v", err)
	}

	var dt2 DateTime
	if err := dt2.Scan(val); err != nil {
		t.Fatalf("Scan(%v) unexpected error: %v", val, err)
	}

	if !dt2.Equal(refTime) {
		t.Errorf("round-trip: got %v, want %v", dt2.Time, refTime)
	}
}

// --- NullDateTime.Scan ---

func TestNullDateTimeScan(t *testing.T) {
	refTime := time.Date(2024, 3, 15, 10, 30, 45, 0, time.UTC)

	tests := []struct {
		name      string
		src       any
		wantTime  time.Time
		wantValid bool
		wantErr   bool
	}{
		{
			name:      "string: valid datetime",
			src:       "2024-03-15 10:30:45",
			wantTime:  refTime,
			wantValid: true,
		},
		{
			name:      "nil: NULL",
			src:       nil,
			wantTime:  time.Time{},
			wantValid: false,
		},
		{
			name:      "int64: Unix epoch",
			src:       int64(1710498645),
			wantTime:  refTime,
			wantValid: true,
		},
		{
			name:      "time.Time: pre-parsed",
			src:       refTime,
			wantTime:  refTime,
			wantValid: true,
		},
		{
			name:    "unsupported type",
			src:     true,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var n NullDateTime
			err := n.Scan(tt.src)

			if tt.wantErr {
				if err == nil {
					t.Errorf("Scan(%v) = nil, want error", tt.src)
				}
				return
			}
			if err != nil {
				t.Fatalf("Scan(%v) unexpected error: %v", tt.src, err)
			}
			if n.Valid != tt.wantValid {
				t.Errorf("Scan(%v).Valid = %v, want %v", tt.src, n.Valid, tt.wantValid)
			}
			if !n.Time.Equal(tt.wantTime) {
				t.Errorf("Scan(%v).Time = %v, want %v", tt.src, n.Time, tt.wantTime)
			}
		})
	}
}

// --- NullDateTime.Value ---

func TestNullDateTimeValue(t *testing.T) {
	resetCustomState(t)

	refTime := time.Date(2024, 3, 15, 10, 30, 45, 0, time.UTC)

	tests := []struct {
		name    string
		input   NullDateTime
		wantNil bool
		wantStr string
	}{
		{
			name:    "valid",
			input:   NullDateTime{Time: refTime, Valid: true},
			wantStr: refTime.Format(time.RFC3339Nano),
		},
		{
			name:    "invalid (NULL)",
			input:   NullDateTime{Valid: false},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, err := tt.input.Value()
			if err != nil {
				t.Fatalf("Value() unexpected error: %v", err)
			}
			if tt.wantNil {
				if val != nil {
					t.Errorf("Value() = %v, want nil", val)
				}
				return
			}
			got, ok := val.(string)
			if !ok {
				t.Fatalf("Value() type = %T, want string", val)
			}
			if got != tt.wantStr {
				t.Errorf("Value() = %q, want %q", got, tt.wantStr)
			}
		})
	}
}

// --- RegisterLayout ---

func TestRegisterLayout(t *testing.T) {
	resetCustomState(t)
	t.Cleanup(func() { resetCustomState(t) })

	// Custom format not recognized by built-in layouts
	RegisterLayout("Jan 02, 2006 03:04:05 PM")

	var dt DateTime
	err := dt.Scan("Mar 15, 2024 10:30:45 AM")
	if err != nil {
		t.Fatalf("Scan with custom layout: %v", err)
	}

	want := time.Date(2024, 3, 15, 10, 30, 45, 0, time.UTC)
	if !dt.Equal(want) {
		t.Errorf("Scan with custom layout = %v, want %v", dt.Time, want)
	}
}

// --- SetOutputLayout ---

func TestSetOutputLayout(t *testing.T) {
	resetCustomState(t)
	t.Cleanup(func() { resetCustomState(t) })

	SetOutputLayout("2006-01-02 15:04:05")

	refTime := time.Date(2024, 3, 15, 10, 30, 45, 0, time.UTC)
	dt := DateTime{Time: refTime}

	val, err := dt.Value()
	if err != nil {
		t.Fatalf("Value() unexpected error: %v", err)
	}

	got := val.(string)
	want := "2024-03-15 10:30:45"
	if got != want {
		t.Errorf("Value() with custom output layout = %q, want %q", got, want)
	}
}

func TestSetOutputLayoutNullDateTime(t *testing.T) {
	resetCustomState(t)
	t.Cleanup(func() { resetCustomState(t) })

	SetOutputLayout("2006-01-02")

	refTime := time.Date(2024, 3, 15, 10, 30, 45, 0, time.UTC)
	n := NullDateTime{Time: refTime, Valid: true}

	val, err := n.Value()
	if err != nil {
		t.Fatalf("Value() unexpected error: %v", err)
	}

	got := val.(string)
	want := "2024-03-15"
	if got != want {
		t.Errorf("NullDateTime.Value() with custom output layout = %q, want %q", got, want)
	}
}

// --- julianDayToTime ---

func TestJulianDayToTime(t *testing.T) {
	tests := []struct {
		name string
		jd   float64
		want time.Time
	}{
		{
			name: "Unix epoch",
			jd:   2440587.5,
			want: time.Unix(0, 0).UTC(),
		},
		{
			name: "2000-01-01 12:00:00 UTC",
			jd:   2451545.0,
			want: time.Date(2000, 1, 1, 12, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := julianDayToTime(tt.jd)
			// Allow 1-second tolerance for floating-point precision
			diff := got.Sub(tt.want)
			if diff < -time.Second || diff > time.Second {
				t.Errorf("julianDayToTime(%f) = %v, want %v (diff: %v)", tt.jd, got, tt.want, diff)
			}
		})
	}
}
