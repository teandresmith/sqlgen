package comparator_test

import (
	"net"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/sql"
)

func TestOpaque_Parse(t *testing.T) {
	payload := []byte{0x01, 0x02, 0xff}

	tests := []struct {
		name string
		comp comparator.Opaque[[]byte]
		want []sql.Condition
	}{
		{
			name: "Eq",
			comp: comparator.Opaque[[]byte]{Eq: new(payload)},
			want: []sql.Condition{{Clause: "payload = $", Value: payload, Column: "payload"}},
		},
		{
			name: "Neq",
			comp: comparator.Opaque[[]byte]{Neq: new(payload)},
			want: []sql.Condition{{Clause: "payload != $", Value: payload, Column: "payload"}},
		},
		{
			name: "Gt",
			comp: comparator.Opaque[[]byte]{Gt: new(payload)},
			want: []sql.Condition{{Clause: "payload > $", Value: payload, Column: "payload"}},
		},
		{
			name: "Gte",
			comp: comparator.Opaque[[]byte]{Gte: new(payload)},
			want: []sql.Condition{{Clause: "payload >= $", Value: payload, Column: "payload"}},
		},
		{
			name: "Lt",
			comp: comparator.Opaque[[]byte]{Lt: new(payload)},
			want: []sql.Condition{{Clause: "payload < $", Value: payload, Column: "payload"}},
		},
		{
			name: "Lte",
			comp: comparator.Opaque[[]byte]{Lte: new(payload)},
			want: []sql.Condition{{Clause: "payload <= $", Value: payload, Column: "payload"}},
		},
		{
			name: "Custom passthrough",
			comp: comparator.Opaque[[]byte]{Custom: []sql.Condition{{Clause: "payload = $", Value: payload}}},
			want: []sql.Condition{{Clause: "payload = $", Value: payload}},
		},
		{
			name: "empty produces nil",
			comp: comparator.Opaque[[]byte]{},
			want: nil,
		},
		{
			// Operator order follows the struct's field order, which is the
			// order the shared-schema operator list is emitted in.
			name: "operators compose in field order",
			comp: comparator.Opaque[[]byte]{Eq: new(payload), Gt: new([]byte{0x00})},
			want: []sql.Condition{
				{Clause: "payload = $", Value: payload, Column: "payload"},
				{Clause: "payload > $", Value: []byte{0x00}, Column: "payload"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.comp.Parse("payload", pgDialect)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Opaque[[]byte].Parse() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestOpaque_Parse_In_DialectAware pins the same array-parameter split every
// other family follows: pgx takes a single array arg, the others expand.
func TestOpaque_Parse_In_DialectAware(t *testing.T) {
	in := [][]byte{{0x01}, {0x02}}
	comp := comparator.Opaque[[]byte]{In: in}

	t.Run("postgres pgx uses ANY", func(t *testing.T) {
		got := comp.Parse("payload", pgDialect)
		if len(got) != 1 {
			t.Fatalf("got %d conditions, want 1", len(got))
		}
		if got[0].Clause != "payload = ANY($)" {
			t.Errorf("clause = %q, want %q", got[0].Clause, "payload = ANY($)")
		}
		if diff := cmp.Diff(in, got[0].Value); diff != "" {
			t.Errorf("value mismatch (-want +got):\n%s", diff)
		}
	})

	for _, tc := range []struct {
		name    string
		dialect sql.Dialect
	}{
		{"mysql expands", mysqlDialect},
		{"sqlite expands", sqliteDialect},
		{"postgres stdlib expands", pgStdDialect},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := comp.Parse("payload", tc.dialect)
			if len(got) != 1 {
				t.Fatalf("got %d conditions, want 1", len(got))
			}
			if got[0].Clause != "payload IN $" {
				t.Errorf("clause = %q, want %q", got[0].Clause, "payload IN $")
			}
			if diff := cmp.Diff([]any{in[0], in[1]}, got[0].Value); diff != "" {
				t.Errorf("value mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestNullableOpaque_Parse(t *testing.T) {
	payload := []byte{0x01}

	tests := []struct {
		name string
		comp comparator.NullableOpaque[[]byte]
		want []sql.Condition
	}{
		{
			name: "Null true",
			comp: comparator.NullableOpaque[[]byte]{Null: new(true)},
			want: []sql.Condition{{Clause: "payload IS NULL", Column: "payload"}},
		},
		{
			name: "Null false",
			comp: comparator.NullableOpaque[[]byte]{Null: new(false)},
			want: []sql.Condition{{Clause: "payload IS NOT NULL", Column: "payload"}},
		},
		{
			// The embedded Opaque[T]'s operands are promoted, which is what
			// lets one generated operand block serve both variants.
			name: "promoted operand plus Null",
			comp: comparator.NullableOpaque[[]byte]{
				Opaque: comparator.Opaque[[]byte]{Eq: new(payload)},
				Null:   new(false),
			},
			want: []sql.Condition{
				{Clause: "payload = $", Value: payload, Column: "payload"},
				{Clause: "payload IS NOT NULL", Column: "payload"},
			},
		},
		{
			name: "empty produces nil",
			comp: comparator.NullableOpaque[[]byte]{},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.comp.Parse("payload", pgDialect)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("NullableOpaque[[]byte].Parse() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestOpaque_InstantiatesEveryResolvedGoType is the reason the type parameter
// is `any` rather than `comparable`: three of the four Go types the built-in
// dialect tables resolve binary and network columns to are not comparable, so
// this file would not build under the Enum[T] constraint. Each case also pins
// that the driver receives the column's own Go type rather than a rendering of
// it — the reason the Opaque comparator exists.
func TestOpaque_InstantiatesEveryResolvedGoType(t *testing.T) {
	_, block, err := net.ParseCIDR("10.0.0.0/8")
	if err != nil {
		t.Fatalf("ParseCIDR: %v", err)
	}
	mac, err := net.ParseMAC("01:23:45:67:89:ab")
	if err != nil {
		t.Fatalf("ParseMAC: %v", err)
	}
	ip := net.ParseIP("10.0.0.1").To4()

	t.Run("[]byte", func(t *testing.T) {
		got := comparator.Opaque[[]byte]{Eq: new([]byte{0x01})}.Parse("payload", pgDialect)
		assertSingle(t, got, "payload = $", []byte{0x01})
	})
	t.Run("net.IP", func(t *testing.T) {
		got := comparator.Opaque[net.IP]{Eq: new(ip)}.Parse("addr", pgDialect)
		assertSingle(t, got, "addr = $", ip)
	})
	t.Run("net.IPNet", func(t *testing.T) {
		got := comparator.Opaque[net.IPNet]{Eq: new(*block)}.Parse("net_block", pgDialect)
		assertSingle(t, got, "net_block = $", *block)
	})
	t.Run("net.HardwareAddr", func(t *testing.T) {
		got := comparator.Opaque[net.HardwareAddr]{Eq: new(mac)}.Parse("mac", pgDialect)
		assertSingle(t, got, "mac = $", mac)
	})
}

func assertSingle(t *testing.T, got []sql.Condition, clause string, value any) {
	t.Helper()
	if len(got) != 1 {
		t.Fatalf("got %d conditions, want 1", len(got))
	}
	if got[0].Clause != clause {
		t.Errorf("clause = %q, want %q", got[0].Clause, clause)
	}
	if diff := cmp.Diff(value, got[0].Value); diff != "" {
		t.Errorf("value mismatch (-want +got):\n%s", diff)
	}
}
