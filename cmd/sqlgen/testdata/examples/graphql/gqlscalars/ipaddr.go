// Package gqlscalars holds the consumer-supplied GraphQL marshalers for
// scalars sqlgen declares but does not own a body for.
//
// This is the `marshaling: external` half of `api.graphql.scalars` (PRD
// §26.4.1): sqlgen emits the `scalar IPAddr` declaration and merges a gqlgen
// `models:` entry pointing at this package, and gqlgen discovers the pair of
// free functions below and infers the bound Go type from their signature. No
// type alias or registration is needed here — the function names are the
// whole contract, and they must be `Marshal<Scalar>` / `Unmarshal<Scalar>`.
//
// sqlgen deliberately emits no body for a consumer-declared scalar: it cannot
// know the intended wire format, and cannot type-check this package to verify
// a guess. The wire format below is a plain JSON string, matching how the
// built-in registry marshals UUID and Decimal.
package gqlscalars

import (
	"fmt"
	"io"
	"net/netip"
	"strconv"

	"github.com/99designs/gqlgen/graphql"
)

// MarshalIPAddr writes a netip.Addr as a JSON-quoted string.
func MarshalIPAddr(v netip.Addr) graphql.Marshaler {
	return graphql.WriterFunc(func(w io.Writer) {
		_, _ = io.WriteString(w, strconv.Quote(v.String()))
	})
}

// UnmarshalIPAddr parses a netip.Addr from a GraphQL input value.
func UnmarshalIPAddr(v any) (netip.Addr, error) {
	s, ok := v.(string)
	if !ok {
		return netip.Addr{}, fmt.Errorf("IPAddr must be a string, got %T", v)
	}
	return netip.ParseAddr(s)
}
