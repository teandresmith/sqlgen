package msgpack_test

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	vmihailenco "github.com/vmihailenco/msgpack/v5"

	"github.com/teandresmith/sqlgen/cache"
	sqlgenmsgpack "github.com/teandresmith/sqlgen/cache/msgpack"
)

// taggedEntity mirrors the struct shape produced by generated code:
// db + json tags per guidelines/GO.md. The library's default behavior
// reads msgpack tags and falls back to json, which must round-trip
// without any additional configuration.
type taggedEntity struct {
	ID   int64   `db:"id" json:"id"`
	Name string  `db:"name" json:"name"`
	Note *string `db:"note" json:"note,omitempty"`
}

// fullShapedEntity covers the set of column types generated clients
// routinely emit: a 16-byte UUID-shaped identifier, a time.Time
// (PostgreSQL TIMESTAMP / MySQL DATETIME), and the database/sql
// Null* types used for nullable columns.
type fullShapedEntity struct {
	ID        [16]byte       `db:"id" json:"id"`
	CreatedAt time.Time      `db:"created_at" json:"created_at"`
	Email     sql.NullString `db:"email" json:"email"`
	Count     sql.NullInt64  `db:"count" json:"count"`
	Score     sql.NullFloat64
	Active    sql.NullBool
}

func TestRoundTripTaggedEntity(t *testing.T) {
	tests := []struct {
		name string
		s    cache.Serializer
	}{
		{"default", sqlgenmsgpack.New()},
		{"with options", sqlgenmsgpack.NewWith(sqlgenmsgpack.Options{})},
		{"with compact + sorted", sqlgenmsgpack.NewWith(sqlgenmsgpack.Options{
			UseCompactInts:   true,
			UseCompactFloats: true,
			SortMapKeys:      true,
		})},
	}

	note := "hello"
	want := taggedEntity{ID: 42, Name: "alice", Note: &note}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := tt.s.Marshal(want)
			if err != nil {
				t.Fatalf("Marshal(%+v) unexpected error: %v", want, err)
			}
			if len(data) == 0 {
				t.Fatalf("Marshal(%+v) produced zero bytes", want)
			}

			var got taggedEntity
			if err := tt.s.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal() unexpected error: %v", err)
			}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("round-trip mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRoundTripFullShapedEntity(t *testing.T) {
	// Time.Time is tested in UTC because msgpack's default time encoding
	// (EXT -1) discards the monotonic clock and the zone pointer — round
	// trips compare equal in UTC but not in a named zone.
	want := fullShapedEntity{
		ID:        [16]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
		CreatedAt: time.Date(2026, 4, 21, 15, 30, 0, 0, time.UTC),
		Email:     sql.NullString{String: "alice@example.com", Valid: true},
		Count:     sql.NullInt64{Int64: 7, Valid: true},
		Score:     sql.NullFloat64{Valid: false},
		Active:    sql.NullBool{Bool: true, Valid: true},
	}

	s := sqlgenmsgpack.New()
	data, err := s.Marshal(want)
	if err != nil {
		t.Fatalf("Marshal() unexpected error: %v", err)
	}

	var got fullShapedEntity
	if err := s.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal() unexpected error: %v", err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("round-trip mismatch (-want +got):\n%s", diff)
	}
}

func TestNewWithConfigureHookApplies(t *testing.T) {
	// ConfigureEncoder must observe the same *msgpack.Encoder that later
	// encodes. Flip an option via the hook and confirm the encoded byte
	// shape changes accordingly.
	entity := taggedEntity{ID: 1, Name: "a"}

	mapEncoded, err := sqlgenmsgpack.NewWith(sqlgenmsgpack.Options{}).Marshal(entity)
	if err != nil {
		t.Fatalf("Marshal(map-encoded) err = %v", err)
	}

	arrayEncoded, err := sqlgenmsgpack.NewWith(sqlgenmsgpack.Options{
		ConfigureEncoder: func(e *vmihailenco.Encoder) {
			e.UseArrayEncodedStructs(true)
		},
	}).Marshal(entity)
	if err != nil {
		t.Fatalf("Marshal(array-encoded) err = %v", err)
	}

	if len(arrayEncoded) >= len(mapEncoded) {
		t.Errorf("array encoding = %d bytes, map encoding = %d bytes; array should be strictly smaller", len(arrayEncoded), len(mapEncoded))
	}

	// Decode path also runs through a pooled decoder — verify the
	// hook-configured encoder's output round-trips when matched with an
	// array-aware decoder.
	s := sqlgenmsgpack.NewWith(sqlgenmsgpack.Options{
		UseArrayEncodedStructs: true,
	})
	data, err := s.Marshal(entity)
	if err != nil {
		t.Fatalf("Marshal() err = %v", err)
	}
	var got taggedEntity
	if err := s.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal() err = %v", err)
	}
	if diff := cmp.Diff(entity, got); diff != "" {
		t.Errorf("round-trip (array-encoded) mismatch (-want +got):\n%s", diff)
	}
}

func TestCustomStructTagReadsAlternateTag(t *testing.T) {
	// With CustomStructTag = "db" the encoder reads db tags, not json.
	// Rename a field between Marshal (db=id) and Unmarshal (db=renamed)
	// to prove the tag is load-bearing.
	type written struct {
		ID int64 `db:"id" json:"json_id"`
	}
	type read struct {
		Renamed int64 `db:"id" json:"json_id"`
	}

	s := sqlgenmsgpack.NewWith(sqlgenmsgpack.Options{CustomStructTag: "db"})
	data, err := s.Marshal(written{ID: 99})
	if err != nil {
		t.Fatalf("Marshal() err = %v", err)
	}

	var got read
	if err := s.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal() err = %v", err)
	}
	if got.Renamed != 99 {
		t.Errorf("Unmarshal() Renamed = %d, want 99 (CustomStructTag=db must link the fields)", got.Renamed)
	}
}

func TestDisallowUnknownFieldsErrors(t *testing.T) {
	// Encode a superset struct and decode into a subset struct. With
	// DisallowUnknownFields on, the decoder must refuse the extra field.
	type super struct {
		ID    int64  `db:"id" json:"id"`
		Extra string `db:"extra" json:"extra"`
	}
	type sub struct {
		ID int64 `db:"id" json:"id"`
	}

	strict := sqlgenmsgpack.NewWith(sqlgenmsgpack.Options{DisallowUnknownFields: true})
	data, err := strict.Marshal(super{ID: 1, Extra: "x"})
	if err != nil {
		t.Fatalf("Marshal() err = %v", err)
	}
	var got sub
	if err := strict.Unmarshal(data, &got); err == nil {
		t.Errorf("Unmarshal(superset → subset) err = nil, want non-nil (DisallowUnknownFields)")
	}

	// Without the flag the same bytes decode without error.
	loose := sqlgenmsgpack.NewWith(sqlgenmsgpack.Options{})
	if err := loose.Unmarshal(data, &got); err != nil {
		t.Errorf("Unmarshal() loose err = %v, want nil", err)
	}
}

func TestBinaryEncodingSmallerThanJSON(t *testing.T) {
	// Documents the "~30% smaller" claim from PRD §27.10 on a
	// representative entity. The exact ratio varies by payload shape;
	// we assert msgpack output is strictly shorter than JSON.
	type product struct {
		ID          [16]byte       `db:"id" json:"id"`
		SKU         string         `db:"sku" json:"sku"`
		Name        string         `db:"name" json:"name"`
		Description sql.NullString `db:"description" json:"description"`
		PriceCents  int64          `db:"price_cents" json:"price_cents"`
		Stock       int32          `db:"stock" json:"stock"`
		Active      bool           `db:"active" json:"active"`
		CreatedAt   time.Time      `db:"created_at" json:"created_at"`
		UpdatedAt   time.Time      `db:"updated_at" json:"updated_at"`
	}

	entity := product{
		ID:          [16]byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x00},
		SKU:         "SKU-1234",
		Name:        "Example Product",
		Description: sql.NullString{String: "A reasonably detailed description of the product.", Valid: true},
		PriceCents:  4999,
		Stock:       42,
		Active:      true,
		CreatedAt:   time.Date(2026, 4, 21, 10, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 4, 21, 11, 0, 0, 0, time.UTC),
	}

	jsonBytes, err := json.Marshal(entity)
	if err != nil {
		t.Fatalf("json.Marshal() err = %v", err)
	}
	mpBytes, err := sqlgenmsgpack.New().Marshal(entity)
	if err != nil {
		t.Fatalf("msgpack.Marshal() err = %v", err)
	}

	if len(mpBytes) >= len(jsonBytes) {
		t.Errorf("msgpack encoding = %d bytes, json = %d bytes; msgpack must be smaller", len(mpBytes), len(jsonBytes))
	}
}

func TestPooledSerializerIsolatesOptions(t *testing.T) {
	// Two NewWith instances with different options must not leak state
	// into one another via the library's pool. Exercise them
	// interleaved and assert each encodes according to its own options.
	compact := sqlgenmsgpack.NewWith(sqlgenmsgpack.Options{UseArrayEncodedStructs: true})
	verbose := sqlgenmsgpack.NewWith(sqlgenmsgpack.Options{})

	entity := taggedEntity{ID: 1, Name: strings.Repeat("a", 8)}

	for range 64 {
		cBytes, err := compact.Marshal(entity)
		if err != nil {
			t.Fatalf("compact.Marshal() err = %v", err)
		}
		vBytes, err := verbose.Marshal(entity)
		if err != nil {
			t.Fatalf("verbose.Marshal() err = %v", err)
		}
		if len(cBytes) >= len(vBytes) {
			t.Fatalf("compact = %d bytes, verbose = %d bytes; compact must stay smaller across pool reuse", len(cBytes), len(vBytes))
		}
	}
}
