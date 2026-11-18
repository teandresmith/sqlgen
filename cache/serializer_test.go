package cache_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cache"
)

type sampleEntity struct {
	ID   int64   `db:"id" json:"id"`
	Name string  `db:"name" json:"name"`
	Note *string `db:"note" json:"note,omitempty"`
}

func TestJSONSerializerRoundTrip(t *testing.T) {
	note := "hello"
	want := sampleEntity{ID: 42, Name: "alice", Note: &note}

	s := cache.JSONSerializer{}
	data, err := s.Marshal(want)
	if err != nil {
		t.Fatalf("JSONSerializer.Marshal() unexpected error: %v", err)
	}

	stdlib, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("json.Marshal() unexpected error: %v", err)
	}
	if !bytes.Equal(data, stdlib) {
		t.Errorf("JSONSerializer.Marshal() = %s, want the encoding/json bytes %s", data, stdlib)
	}

	var got sampleEntity
	if err := s.Unmarshal(data, &got); err != nil {
		t.Fatalf("JSONSerializer.Unmarshal() unexpected error: %v", err)
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("JSONSerializer round-trip mismatch (-want +got):\n%s", diff)
	}
}
