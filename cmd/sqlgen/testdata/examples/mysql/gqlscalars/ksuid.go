// Package gqlscalars holds the consumer-supplied GraphQL marshalers for
// scalars sqlgen declares but does not own a body for.
//
// This is the `marshaling: external` half of `api.graphql.scalars` (PRD
// §26.4.1): sqlgen emits the `scalar KSUID` declaration and merges a gqlgen
// `models:` entry pointing at this package, and gqlgen discovers the pair of
// free functions below and infers the bound Go type from their signature. No
// type alias or registration is needed here — the function names are the whole
// contract, and they must be `Marshal<Scalar>` / `Unmarshal<Scalar>`.
//
// sqlgen deliberately emits no body for a consumer-declared scalar: it cannot
// know the intended wire format, and cannot type-check this package to verify
// a guess. A KSUID's canonical form is its 27-character base62 text, which is
// what String() / Parse() round-trip exactly.
package gqlscalars

import (
	"fmt"
	"io"
	"strconv"

	"github.com/99designs/gqlgen/graphql"
	"github.com/segmentio/ksuid"
)

// MarshalKSUID writes a ksuid.KSUID as a JSON-quoted base62 string.
func MarshalKSUID(v ksuid.KSUID) graphql.Marshaler {
	return graphql.WriterFunc(func(w io.Writer) {
		_, _ = io.WriteString(w, strconv.Quote(v.String()))
	})
}

// UnmarshalKSUID parses a ksuid.KSUID from a GraphQL input value.
func UnmarshalKSUID(v any) (ksuid.KSUID, error) {
	s, ok := v.(string)
	if !ok {
		return ksuid.Nil, fmt.Errorf("KSUID must be a string, got %T", v)
	}
	return ksuid.Parse(s)
}
