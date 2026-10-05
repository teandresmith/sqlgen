package cache

import (
	"context"
	"fmt"
	"time"
)

// GetAs reads a byte-slice from b for key and decodes it into T via s.
// Returns (zero, false, nil) on a cache miss so callers can distinguish a
// miss from a decoded zero value without pointer-returning variants.
// Backend and unmarshal errors propagate to the caller as (zero, false, err).
//
// T MUST be a value type — pointer Ts produce address-stringified keys
// via %v and are never valid here. The same constraint applies to SetAs,
// GetOrSet, and KeysFromAny when the key is built via the key helpers
// (BuildKey / BuildCompositeKey).
func GetAs[T any](ctx context.Context, b Backend, s Serializer, key string) (T, bool, error) {
	var zero T
	data, err := b.Get(ctx, key)
	if err != nil {
		return zero, false, fmt.Errorf("cache get %s: %w", key, err)
	}
	if data == nil {
		return zero, false, nil
	}
	var v T
	if err := s.Unmarshal(data, &v); err != nil {
		return zero, false, fmt.Errorf("cache unmarshal %s: %w", key, err)
	}
	return v, true, nil
}

// SetAs encodes v via s and stores the bytes in b under key with the
// given ttl. Marshal errors propagate without touching the backend.
//
// T MUST be a value type; see GetAs for the full constraint.
func SetAs[T any](ctx context.Context, b Backend, s Serializer, key string, v T, ttl time.Duration) error {
	data, err := s.Marshal(v)
	if err != nil {
		return fmt.Errorf("cache marshal %s: %w", key, err)
	}
	if err := b.Set(ctx, key, data, ttl); err != nil {
		return fmt.Errorf("cache set %s: %w", key, err)
	}
	return nil
}

// GetOrSet is the typed read-through pattern. If key is cached the
// stored value is returned; on a miss load is invoked, its result is
// stored under key with the given ttl, and returned. Errors from load
// propagate without a Set attempt. Useful for ad-hoc caching of computed
// values in handler code — the generated cache facade already does this
// internally for entities.
//
// T MUST be a value type; see GetAs for the full constraint.
func GetOrSet[T any](ctx context.Context, b Backend, s Serializer, key string, ttl time.Duration, load func(ctx context.Context) (T, error)) (T, error) {
	v, hit, err := GetAs[T](ctx, b, s, key)
	if err != nil {
		return v, err
	}
	if hit {
		return v, nil
	}
	v, err = load(ctx)
	if err != nil {
		var zero T
		return zero, err
	}
	if err := SetAs(ctx, b, s, key, v, ttl); err != nil {
		return v, err
	}
	return v, nil
}

// KeysFromAny projects a []any of PK values into []string cache keys via
// a typed keyer function. Each element of pks is type-asserted to T; a
// mismatch returns a descriptive error naming the element index, the
// expected type, and the actual type. Empty input returns an empty
// []string (non-nil) and a nil error. The generated Invalidate /
// InvalidateMany dispatch calls this once per table case so user-written
// Cache alternatives and tests can reuse the same path.
//
// T MUST be a value type; see GetAs for the full constraint.
func KeysFromAny[T any](pks []any, key func(T) string) ([]string, error) {
	out := make([]string, 0, len(pks))
	for i, raw := range pks {
		pk, ok := raw.(T)
		if !ok {
			var zero T
			return nil, fmt.Errorf("cache: pk at index %d: expected %T, got %T", i, zero, raw)
		}
		out = append(out, key(pk))
	}
	return out, nil
}
