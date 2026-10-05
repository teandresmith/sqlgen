package tests

import (
	"context"
	"testing"
)

// Filter coverage for the opaque comparator families — the columns whose Go
// type is neither text nor a Go numeric, and which therefore filtered through
// comparator.String before the Opaque[T] / Duration families existed.
//
// This file is the behavioural half of the fix. The unit tests in
// cmd/sqlgen/gen pin which comparator input each column projects onto; nothing
// there can prove the resulting filter returns the right ROWS, which is where
// the defect actually lived. Every case below therefore goes through the live
// gqlgen handler against a real Postgres, and every one of them returned the
// wrong answer before the fix:
//
//   - Bytes: the wire form is base64, so `{ payload: { eq: <the value the
//     query just read back> } }` was compared as text against a bytea column
//     and matched NOTHING — silently, with no error.
//   - Duration: `like` raised SQLSTATE 42883, and the ordered operators were
//     text collation over a Go duration string, so `-2h45m0s` was read by
//     PostgreSQL as -1h15m (the sign applies to the leading field only).
//   - IP / CIDR / MacAddr: `like` raised 42883; `inet` equality missed
//     because UnmarshalIP stored the 16-byte IPv4-in-IPv6 form.
//
// Each case creates its own probes and filters them down, so a comparator
// that silently matched everything would fail just as loudly as one that
// matched nothing.

// probeIDs runs a scalarProbeList query with the given filter and returns the
// matched labels, so assertions read as "which rows came back" rather than as
// row counts.
func probeLabels(t *testing.T, filter string, vars map[string]any, decl string) []string {
	t.Helper()
	var out struct {
		ScalarProbeList struct {
			Items []struct {
				Label string `json:"label"`
			} `json:"items"`
			TotalCount int `json:"totalCount"`
		} `json:"scalarProbeList"`
	}
	// An operator whose operand is a literal (isNull: true) declares no
	// variables, and GraphQL rejects both an empty `query ()` and an unused
	// declaration — so the parameter list is omitted entirely in that case.
	params := ""
	if decl != "" {
		params = "(" + decl + ")"
	}
	gqlExecData(t, `
		query `+params+` {
			scalarProbeList(filter: {`+filter+`}, limit: 50) {
				items { label }
				totalCount
			}
		}
	`, vars, &out)
	labels := make([]string, 0, len(out.ScalarProbeList.Items))
	for _, it := range out.ScalarProbeList.Items {
		labels = append(labels, it.Label)
	}
	return labels
}

func assertLabels(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("matched %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("matched[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// createProbe writes one scalar_probes row with every NOT NULL column set.
func createProbe(t *testing.T, label, payload, dur, addr, netBlock, mac string) {
	t.Helper()
	var out struct {
		CreateScalarProbe struct {
			ID string `json:"id"`
		} `json:"createScalarProbe"`
	}
	gqlExecData(t, `
		mutation ($label: String!, $payload: Bytes!, $dur: Duration!, $addr: IP!,
			$netBlock: CIDR!, $mac: MacAddr!, $createdAt: Time!) {
			createScalarProbe(input: {
				label: $label, payload: $payload, dur: $dur, addr: $addr,
				netBlock: $netBlock, mac: $mac, createdAt: $createdAt
			}) { id }
		}
	`, map[string]any{
		"label": label, "payload": payload, "dur": dur, "addr": addr,
		"netBlock": netBlock, "mac": mac, "createdAt": fixedTimestamp,
	}, &out)
	if out.CreateScalarProbe.ID == "" {
		t.Fatalf("createScalarProbe(%s) returned no id", label)
	}
}

func TestScalarProbeFilters(t *testing.T) {
	truncateAll(t)

	// Three probes differing on every axis under test.
	createProbe(t, "alpha", "AAEC", "1h30m0s", "10.0.0.1", "10.0.0.0/8", "01:23:45:67:89:ab")
	createProbe(t, "beta", "AAED", "45s", "192.168.1.7", "192.168.1.0/24", "de:ad:be:ef:00:01")
	createProbe(t, "gamma", "//8=", "3h0m0s", "2001:db8::1", "2001:db8::/32", "aa:bb:cc:dd:ee:ff")

	// --- Bytes ---------------------------------------------------------
	// The headline case. "AAEC" is the base64 the read path hands back, and
	// before the fix feeding it straight into `eq` matched zero rows because
	// the model compared a bytea column against that text.
	t.Run("Bytes/eq_roundtrips_the_wire_form", func(t *testing.T) {
		got := probeLabels(t, `payload: { eq: $v }`, map[string]any{"v": "AAEC"}, `$v: Bytes`)
		assertLabels(t, got, "alpha")
	})

	t.Run("Bytes/in", func(t *testing.T) {
		got := probeLabels(t, `payload: { in: $v }`,
			map[string]any{"v": []string{"AAEC", "//8="}}, `$v: [Bytes!]`)
		assertLabels(t, got, "alpha", "gamma")
	})

	t.Run("Bytes/neq_excludes_only_the_match", func(t *testing.T) {
		got := probeLabels(t, `payload: { neq: $v }`, map[string]any{"v": "AAEC"}, `$v: Bytes`)
		assertLabels(t, got, "beta", "gamma")
	})

	t.Run("Bytes/ordered_compares_bytewise", func(t *testing.T) {
		// 0x00 0x01 0x03 ("AAED") sorts above 0x00 0x01 0x02 and below 0xff 0xff.
		got := probeLabels(t, `payload: { gt: $v }`, map[string]any{"v": "AAEC"}, `$v: Bytes`)
		assertLabels(t, got, "beta", "gamma")
	})

	// --- Duration ------------------------------------------------------
	t.Run("Duration/ordered_compares_as_interval", func(t *testing.T) {
		got := probeLabels(t, `dur: { gt: $v }`, map[string]any{"v": "1h"}, `$v: Duration`)
		assertLabels(t, got, "alpha", "gamma")
	})

	t.Run("Duration/between", func(t *testing.T) {
		got := probeLabels(t, `dur: { between: { from: $from, to: $to } }`,
			map[string]any{"from": "1m", "to": "2h"}, `$from: Duration!, $to: Duration!`)
		assertLabels(t, got, "alpha")
	})

	t.Run("Duration/eq_is_not_string_equality", func(t *testing.T) {
		// "90m" and "1h30m0s" are the same interval and different strings.
		// Under the old comparator.String projection this matched nothing.
		got := probeLabels(t, `dur: { eq: $v }`, map[string]any{"v": "90m"}, `$v: Duration`)
		assertLabels(t, got, "alpha")
	})

	// --- IP / CIDR / MacAddr -------------------------------------------
	t.Run("IP/eq", func(t *testing.T) {
		got := probeLabels(t, `addr: { eq: $v }`, map[string]any{"v": "10.0.0.1"}, `$v: IP`)
		assertLabels(t, got, "alpha")
	})

	t.Run("IP/in_mixes_v4_and_v6", func(t *testing.T) {
		got := probeLabels(t, `addr: { in: $v }`,
			map[string]any{"v": []string{"10.0.0.1", "2001:db8::1"}}, `$v: [IP!]`)
		assertLabels(t, got, "alpha", "gamma")
	})

	t.Run("CIDR/eq", func(t *testing.T) {
		got := probeLabels(t, `netBlock: { eq: $v }`, map[string]any{"v": "10.0.0.0/8"}, `$v: CIDR`)
		assertLabels(t, got, "alpha")
	})

	t.Run("MacAddr/eq", func(t *testing.T) {
		got := probeLabels(t, `mac: { eq: $v }`,
			map[string]any{"v": "de:ad:be:ef:00:01"}, `$v: MacAddr`)
		assertLabels(t, got, "beta")
	})

	// --- nullable twins -------------------------------------------------
	// Every column above is NOT NULL and takes the base input; the nullable
	// members take Nullable<X>Comparator, a distinct GraphQL type with its
	// own translator (PRD §26.4 Rule 2). isNull is the operand only that
	// twin carries, so it also proves the twin is the one being dispatched.
	t.Run("Nullable/isNull_on_every_opaque_family", func(t *testing.T) {
		for _, f := range []string{"signature", "durN", "addrN", "netBlockN", "macN"} {
			t.Run(f, func(t *testing.T) {
				got := probeLabels(t, f+`: { isNull: true }`, nil, "")
				assertLabels(t, got, "alpha", "beta", "gamma")
			})
		}
	})
}

// TestIPScalarNormalizesIPv4 pins the write-path half of the IP scalar.
// net.ParseIP returns the 16-byte IPv4-in-IPv6 form for a dotted quad, which a
// driver encodes into `inet` as ::ffff:10.0.0.1/128 rather than 10.0.0.1/32.
// Both render as "10.0.0.1" through net.IP.String(), so a GraphQL round-trip
// cannot see the difference — this reads the column's own text form to make the
// stored value visible, and checks the subnet operators that the mapped form
// silently defeats.
func TestIPScalarNormalizesIPv4(t *testing.T) {
	truncateAll(t)
	createProbe(t, "v4", "AAEC", "1s", "10.0.0.1", "10.0.0.0/8", "01:23:45:67:89:ab")
	createProbe(t, "v6", "AAED", "1s", "2001:db8::1", "2001:db8::/32", "de:ad:be:ef:00:01")

	var stored string
	if err := testPool.QueryRow(context.Background(),
		`SELECT addr::text FROM scalar_probes WHERE label = 'v4'`).Scan(&stored); err != nil {
		t.Fatalf("read stored addr: %v", err)
	}
	if stored != "10.0.0.1/32" {
		t.Errorf("stored addr = %q, want %q — UnmarshalIP wrote the 16-byte "+
			"IPv4-in-IPv6 form, so subnet operators and text comparisons miss", stored, "10.0.0.1/32")
	}

	// The consequence the representation actually has: a subnet containment
	// probe against the stored row. ::ffff:10.0.0.1/128 is not inside
	// 10.0.0.0/8, so this returns 0 rows on the unnormalized form.
	var contained int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM scalar_probes WHERE addr <<= '10.0.0.0/8'::cidr`).Scan(&contained); err != nil {
		t.Fatalf("subnet probe: %v", err)
	}
	if contained != 1 {
		t.Errorf("addr <<= 10.0.0.0/8 matched %d rows, want 1", contained)
	}

	// A genuine IPv6 address must survive untouched — To4 reports nil for it,
	// and dropping the guard would collapse every v6 value to nil.
	var storedV6 string
	if err := testPool.QueryRow(context.Background(),
		`SELECT addr::text FROM scalar_probes WHERE label = 'v6'`).Scan(&storedV6); err != nil {
		t.Fatalf("read stored v6 addr: %v", err)
	}
	if storedV6 != "2001:db8::1/128" {
		t.Errorf("stored v6 addr = %q, want %q", storedV6, "2001:db8::1/128")
	}
}
