package manifest_test

import (
	"reflect"
	"testing"

	builder "github.com/teandresmith/sqlgen/cmd/sqlgen/manifest"
	runtime "github.com/teandresmith/sqlgen/manifest"
)

// TestFeatureStructs_noDriftBetweenBuilderAndRuntime pins the hand-duplicated
// per-entity feature structs against each other. The builder-side package
// marshals them into manifest_gen.json; the runtime package — stdlib-only and
// embedded into every consumer binary via manifest_embed_gen.go — unmarshals
// the same bytes back on Client.Manifest(). Nothing in the build ties the two
// declarations together, so a field the runtime does not declare is silently
// dropped at decode time with no error anywhere.
//
// The comparison includes json tag *options*, not just names: an earlier
// change diverged TenancyFeature.MismatchError with `omitempty` on both sides
// at once (PRD §30.4.2), which is exactly the class of edit that is cheap to
// make on one side only.
func TestFeatureStructs_noDriftBetweenBuilderAndRuntime(t *testing.T) {
	tests := []struct {
		name    string
		builder reflect.Type
		runtime reflect.Type
	}{
		{"Features", reflect.TypeFor[builder.Features](), reflect.TypeFor[runtime.Features]()},
		{"SoftDeleteFeature", reflect.TypeFor[builder.SoftDeleteFeature](), reflect.TypeFor[runtime.SoftDeleteFeature]()},
		{"CacheFeature", reflect.TypeFor[builder.CacheFeature](), reflect.TypeFor[runtime.CacheFeature]()},
		{"EventsFeature", reflect.TypeFor[builder.EventsFeature](), reflect.TypeFor[runtime.EventsFeature]()},
		{"TenancyFeature", reflect.TypeFor[builder.TenancyFeature](), reflect.TypeFor[runtime.TenancyFeature]()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, want := tt.builder.NumField(), tt.runtime.NumField(); got != want {
				t.Fatalf("field count: builder = %d, runtime = %d", got, want)
			}
			for i := range tt.builder.NumField() {
				b, r := tt.builder.Field(i), tt.runtime.Field(i)
				if b.Name != r.Name {
					t.Errorf("field %d name: builder = %q, runtime = %q", i, b.Name, r.Name)
					continue
				}
				// Pointer-to-struct members (Features' four feature fields)
				// name types in different packages, so compare the shape by
				// string rather than by identity — package-qualified names
				// necessarily differ.
				if got, want := typeShape(b.Type), typeShape(r.Type); got != want {
					t.Errorf("field %s type: builder = %s, runtime = %s", b.Name, got, want)
				}
				if got, want := b.Tag.Get("json"), r.Tag.Get("json"); got != want {
					t.Errorf("field %s json tag: builder = %q, runtime = %q", b.Name, got, want)
				}
			}
		})
	}
}

// typeShape renders a type with its package qualifier stripped, so a builder
// field typed *manifest.CacheFeature and a runtime field typed
// *manifest.CacheFeature compare equal while a genuine type change does not.
func typeShape(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Pointer:
		return "*" + typeShape(t.Elem())
	case reflect.Slice:
		return "[]" + typeShape(t.Elem())
	default:
		if name := t.Name(); name != "" {
			return name
		}
		return t.String()
	}
}
