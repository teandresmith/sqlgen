// Package msgpack provides a cache.Serializer backed by
// github.com/vmihailenco/msgpack/v5. It is a separate Go module so the
// msgpack dependency only lands in consumer projects that opt in via
// cache.serializer: "msgpack" in sqlgen.yml.
//
// The generator wires this package in at codegen time — when
// cache.serializer == "msgpack" the generated cache_gen.go imports
// sqlgenmsgpack "github.com/teandresmith/sqlgen/cache/msgpack" and defaults
// the Cache's serializer to New(). See CACHE.md §7 and PRD §27.10.
package msgpack

import (
	"bytes"
	"fmt"
	"io"
	"sync"

	"github.com/vmihailenco/msgpack/v5"

	"github.com/teandresmith/sqlgen/cache"
)

// Options configures a Serializer built via NewWith. The zero value
// matches the library's defaults — identical to New().
//
// Struct-tag compatibility: with CustomStructTag unset the underlying
// library reads "msgpack" tags and falls back to "json" tags, which
// matches SQLGen's generated struct shape (db / json tags per
// guidelines/GO.md). Set CustomStructTag = "db" to read the db tag
// directly.
type Options struct {
	// CustomStructTag overrides the struct tag used for field lookup.
	// Empty leaves the library default in place.
	CustomStructTag string

	// SortMapKeys produces deterministic map encoding. Useful when
	// cached values contain maps and byte-level stability matters
	// across serializer instances.
	SortMapKeys bool

	// OmitEmpty drops zero-valued fields from encoded output,
	// mirroring the library's omitempty semantics per kind.
	OmitEmpty bool

	// UseArrayEncodedStructs encodes structs as arrays for smaller
	// output. Requires field-order stability between writer and
	// reader — the per-table fingerprint in the cache key (CACHE.md
	// §10.1) already enforces shape stability.
	UseArrayEncodedStructs bool

	// UseCompactInts picks the smallest integer representation that
	// fits the value.
	UseCompactInts bool

	// UseCompactFloats picks the smallest float representation that
	// fits the value.
	UseCompactFloats bool

	// UseInternedStrings deduplicates repeated strings during
	// encoding and decoding — valuable when cached entities share
	// many identical strings (enum names, tenant IDs, etc.).
	UseInternedStrings bool

	// DisallowUnknownFields makes Unmarshal error on payload fields
	// missing from the destination struct. The serializer-identity
	// fingerprint (CACHE.md §10.1) already guards against
	// cross-version decoding, but this adds a loud failure at
	// decode time.
	DisallowUnknownFields bool

	// ConfigureEncoder, when non-nil, runs on each pooled encoder
	// after the typed fields above are applied. Use for extension
	// registration or knobs not surfaced as typed fields.
	ConfigureEncoder func(*msgpack.Encoder)

	// ConfigureDecoder is the decoder counterpart to
	// ConfigureEncoder.
	ConfigureDecoder func(*msgpack.Decoder)
}

// New returns a Serializer that delegates to the library's package-level
// Marshal / Unmarshal. Suitable for the auto-injected codegen default —
// zero configuration, shared internal pool, library defaults.
func New() cache.Serializer {
	return defaultSerializer{}
}

// NewWith returns a Serializer backed by a local sync.Pool of encoders
// and decoders pre-configured from opts. Use it when you want isolated
// pool behavior for large-entity workloads or to apply custom encoder
// settings (struct tags, compact ints, extensions).
func NewWith(opts Options) cache.Serializer {
	s := &pooledSerializer{opts: opts}
	s.encPool.New = func() any { return s.buildEncoder() }
	s.decPool.New = func() any { return s.buildDecoder() }
	return s
}

// Compile-time interface assertions.
var (
	_ cache.Serializer = defaultSerializer{}
	_ cache.Serializer = (*pooledSerializer)(nil)
)

// defaultSerializer is the zero-configuration serializer returned by
// New. It reuses the library's own package-level encoder / decoder
// pool, which is the fast path for workloads with no custom options.
type defaultSerializer struct{}

// Marshal encodes v to msgpack.
func (defaultSerializer) Marshal(v any) ([]byte, error) {
	data, err := msgpack.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("cache msgpack marshal: %w", err)
	}
	return data, nil
}

// Unmarshal decodes msgpack data into v.
func (defaultSerializer) Unmarshal(data []byte, v any) error {
	if err := msgpack.Unmarshal(data, v); err != nil {
		return fmt.Errorf("cache msgpack unmarshal: %w", err)
	}
	return nil
}

// pooledSerializer owns local sync.Pools of encoders and decoders so
// option state (compact ints, custom tag, etc.) does not leak into the
// library's global pool shared with direct msgpack.Marshal callers.
type pooledSerializer struct {
	opts    Options
	encPool sync.Pool
	decPool sync.Pool
}

// Marshal encodes v using a pooled encoder preconfigured from Options.
//
// ResetWriter — not Reset — preserves the pooled encoder's flags and
// struct-tag between calls. Encoder.Reset in msgpack/v5 zeroes flags
// and structTag (see encode.go), which would silently drop every
// option applied in buildEncoder.
func (s *pooledSerializer) Marshal(v any) ([]byte, error) {
	enc, _ := s.encPool.Get().(*msgpack.Encoder)
	defer s.encPool.Put(enc)

	var buf bytes.Buffer
	enc.ResetWriter(&buf)
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("cache msgpack marshal: %w", err)
	}
	return buf.Bytes(), nil
}

// Unmarshal decodes data into v using a pooled decoder preconfigured
// from Options. ResetReader (vs Reset) is used for the same flag-
// preservation reason as Marshal.
func (s *pooledSerializer) Unmarshal(data []byte, v any) error {
	dec, _ := s.decPool.Get().(*msgpack.Decoder)
	defer s.decPool.Put(dec)

	dec.ResetReader(bytes.NewReader(data))
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("cache msgpack unmarshal: %w", err)
	}
	return nil
}

func (s *pooledSerializer) buildEncoder() *msgpack.Encoder {
	enc := msgpack.NewEncoder(io.Discard)
	if s.opts.CustomStructTag != "" {
		enc.SetCustomStructTag(s.opts.CustomStructTag)
	}
	enc.SetSortMapKeys(s.opts.SortMapKeys)
	enc.SetOmitEmpty(s.opts.OmitEmpty)
	enc.UseArrayEncodedStructs(s.opts.UseArrayEncodedStructs)
	enc.UseCompactInts(s.opts.UseCompactInts)
	enc.UseCompactFloats(s.opts.UseCompactFloats)
	enc.UseInternedStrings(s.opts.UseInternedStrings)
	if s.opts.ConfigureEncoder != nil {
		s.opts.ConfigureEncoder(enc)
	}
	return enc
}

func (s *pooledSerializer) buildDecoder() *msgpack.Decoder {
	dec := msgpack.NewDecoder(bytes.NewReader(nil))
	if s.opts.CustomStructTag != "" {
		dec.SetCustomStructTag(s.opts.CustomStructTag)
	}
	dec.UseInternedStrings(s.opts.UseInternedStrings)
	dec.DisallowUnknownFields(s.opts.DisallowUnknownFields)
	if s.opts.ConfigureDecoder != nil {
		s.opts.ConfigureDecoder(dec)
	}
	return dec
}
