package gen

import "testing"

// The nested executor buckets and compares target ids by these spellings. A
// time key that buckets as itself compares its zone pointer, so one instant in
// two zones lands in two buckets.
func TestNestedBucketKey(t *testing.T) {
	tests := []struct {
		name     string
		goType   string
		expr     string
		wantKey  string
		wantType string
	}{
		{"time.Time", "time.Time", "v", "v.UTC()", "time.Time"},
		{"time.Time deref", "time.Time", "*nested.Connect", "(*nested.Connect).UTC()", "time.Time"},
		{"types.DateTime", "types.DateTime", "row.At", "row.At.Time.UTC()", "time.Time"},
		{"types.DateTime deref", "types.DateTime", "*nested.Connect", "(*nested.Connect).Time.UTC()", "time.Time"},
		{"int64 unchanged", "int64", "v", "v", "int64"},
		{"int64 deref unchanged", "int64", "*nested.Connect", "*nested.Connect", "int64"},
		{"uuid unchanged", "uuid.UUID", "tid", "tid", "uuid.UUID"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := funcNestedBucketKey(tt.goType, tt.expr); got != tt.wantKey {
				t.Errorf("funcNestedBucketKey(%q, %q) = %q, want %q", tt.goType, tt.expr, got, tt.wantKey)
			}
			if got := nestedBucketGoType(tt.goType); got != tt.wantType {
				t.Errorf("nestedBucketGoType(%q) = %q, want %q", tt.goType, got, tt.wantType)
			}
		})
	}
}
