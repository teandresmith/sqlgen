package omittable_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/omittable"
)

func TestSetAndOmit(t *testing.T) {
	tests := []struct {
		name    string
		value   omittable.Value[string]
		wantVal string
		wantSet bool
	}{
		{
			name:    "Set with non-zero value",
			value:   omittable.Set("hello"),
			wantVal: "hello",
			wantSet: true,
		},
		{
			name:    "Set with zero value (empty string)",
			value:   omittable.Set(""),
			wantVal: "",
			wantSet: true,
		},
		{
			name:    "Omit",
			value:   omittable.Omit[string](),
			wantVal: "",
			wantSet: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := tt.value.Get()
			if ok != tt.wantSet {
				t.Errorf("Get() set = %v, want %v", ok, tt.wantSet)
			}
			if got != tt.wantVal {
				t.Errorf("Get() value = %q, want %q", got, tt.wantVal)
			}
			if tt.value.IsSet() != tt.wantSet {
				t.Errorf("IsSet() = %v, want %v", tt.value.IsSet(), tt.wantSet)
			}
		})
	}
}

func TestZeroValueDistinction(t *testing.T) {
	t.Run("string: Set empty vs Omit", func(t *testing.T) {
		set := omittable.Set("")
		omitted := omittable.Omit[string]()

		if !set.IsSet() {
			t.Error("Set(\"\").IsSet() = false, want true")
		}
		if omitted.IsSet() {
			t.Error("Omit[string]().IsSet() = true, want false")
		}
	})

	t.Run("int: Set zero vs Omit", func(t *testing.T) {
		set := omittable.Set(0)
		omitted := omittable.Omit[int]()

		if !set.IsSet() {
			t.Error("Set(0).IsSet() = false, want true")
		}
		if omitted.IsSet() {
			t.Error("Omit[int]().IsSet() = true, want false")
		}
	})

	t.Run("bool: Set false vs Omit", func(t *testing.T) {
		set := omittable.Set(false)
		omitted := omittable.Omit[bool]()

		if !set.IsSet() {
			t.Error("Set(false).IsSet() = false, want true")
		}
		if omitted.IsSet() {
			t.Error("Omit[bool]().IsSet() = true, want false")
		}
	})
}

func TestUninitializedIsUnset(t *testing.T) {
	var v omittable.Value[string]
	if v.IsSet() {
		t.Error("zero-value Value[string].IsSet() = true, want false")
	}
	got, ok := v.Get()
	if ok {
		t.Error("zero-value Value[string].Get() set = true, want false")
	}
	if got != "" {
		t.Errorf("zero-value Value[string].Get() value = %q, want \"\"", got)
	}
}

func TestMustGet(t *testing.T) {
	t.Run("returns value when set", func(t *testing.T) {
		v := omittable.Set(42)
		got := v.MustGet()
		if got != 42 {
			t.Errorf("MustGet() = %d, want 42", got)
		}
	})

	t.Run("panics when not set", func(t *testing.T) {
		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("MustGet() did not panic on unset Value")
			}
			msg, ok := r.(string)
			if !ok {
				t.Fatalf("MustGet() panic value is %T, want string", r)
			}
			if msg != "omittable: MustGet called on unset Value" {
				t.Errorf("MustGet() panic = %q, want %q", msg, "omittable: MustGet called on unset Value")
			}
		}()

		v := omittable.Omit[int]()
		v.MustGet()
	})
}

func TestJSONMarshal(t *testing.T) {
	type wrapper struct {
		Name  omittable.Value[string] `json:"name,omitzero"`
		Count omittable.Value[int]    `json:"count,omitzero"`
	}

	tests := []struct {
		name string
		val  wrapper
		want string
	}{
		{
			name: "set values are included",
			val:  wrapper{Name: omittable.Set("alice"), Count: omittable.Set(10)},
			want: `{"name":"alice","count":10}`,
		},
		{
			name: "unset values are omitted with omitzero",
			val:  wrapper{Name: omittable.Omit[string](), Count: omittable.Omit[int]()},
			want: `{}`,
		},
		{
			name: "mixed set and unset",
			val:  wrapper{Name: omittable.Set("bob"), Count: omittable.Omit[int]()},
			want: `{"name":"bob"}`,
		},
		{
			name: "set with zero values are included",
			val:  wrapper{Name: omittable.Set(""), Count: omittable.Set(0)},
			want: `{"name":"","count":0}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.val)
			if err != nil {
				t.Fatalf("json.Marshal() error: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("json.Marshal() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestJSONMarshalDirectValue(t *testing.T) {
	tests := []struct {
		name string
		val  omittable.Value[string]
		want string
	}{
		{
			name: "set string",
			val:  omittable.Set("hello"),
			want: `"hello"`,
		},
		{
			name: "set empty string",
			val:  omittable.Set(""),
			want: `""`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.val)
			if err != nil {
				t.Fatalf("json.Marshal() error: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("json.Marshal() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestJSONUnmarshal(t *testing.T) {
	type wrapper struct {
		Name  omittable.Value[string] `json:"name,omitzero"`
		Count omittable.Value[int]    `json:"count,omitzero"`
	}

	tests := []struct {
		name      string
		input     string
		wantName  bool
		wantCount bool
		nameVal   string
		countVal  int
	}{
		{
			name:      "both present",
			input:     `{"name":"alice","count":5}`,
			wantName:  true,
			wantCount: true,
			nameVal:   "alice",
			countVal:  5,
		},
		{
			name:      "absent fields are unset",
			input:     `{}`,
			wantName:  false,
			wantCount: false,
		},
		{
			name:      "partial: only name present",
			input:     `{"name":"bob"}`,
			wantName:  true,
			wantCount: false,
			nameVal:   "bob",
		},
		{
			name:      "zero values are set",
			input:     `{"name":"","count":0}`,
			wantName:  true,
			wantCount: true,
			nameVal:   "",
			countVal:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got wrapper
			if err := json.Unmarshal([]byte(tt.input), &got); err != nil {
				t.Fatalf("json.Unmarshal(%s) error: %v", tt.input, err)
			}
			if got.Name.IsSet() != tt.wantName {
				t.Errorf("Name.IsSet() = %v, want %v", got.Name.IsSet(), tt.wantName)
			}
			if got.Count.IsSet() != tt.wantCount {
				t.Errorf("Count.IsSet() = %v, want %v", got.Count.IsSet(), tt.wantCount)
			}
			if tt.wantName {
				if v := got.Name.MustGet(); v != tt.nameVal {
					t.Errorf("Name.MustGet() = %q, want %q", v, tt.nameVal)
				}
			}
			if tt.wantCount {
				if v := got.Count.MustGet(); v != tt.countVal {
					t.Errorf("Count.MustGet() = %d, want %d", v, tt.countVal)
				}
			}
		})
	}
}

func TestJSONUnmarshalNull(t *testing.T) {
	type wrapper struct {
		Ptr omittable.Value[*string] `json:"ptr,omitzero"`
	}

	t.Run("null sets nil for pointer type", func(t *testing.T) {
		var got wrapper
		if err := json.Unmarshal([]byte(`{"ptr":null}`), &got); err != nil {
			t.Fatalf("json.Unmarshal() error: %v", err)
		}
		if !got.Ptr.IsSet() {
			t.Fatal("Ptr.IsSet() = false, want true")
		}
		v := got.Ptr.MustGet()
		if v != nil {
			t.Errorf("Ptr.MustGet() = %v, want nil", v)
		}
	})

	t.Run("present value sets pointer", func(t *testing.T) {
		var got wrapper
		if err := json.Unmarshal([]byte(`{"ptr":"hello"}`), &got); err != nil {
			t.Fatalf("json.Unmarshal() error: %v", err)
		}
		if !got.Ptr.IsSet() {
			t.Fatal("Ptr.IsSet() = false, want true")
		}
		v := got.Ptr.MustGet()
		if v == nil {
			t.Fatal("Ptr.MustGet() = nil, want non-nil")
		}
		if *v != "hello" {
			t.Errorf("*Ptr.MustGet() = %q, want %q", *v, "hello")
		}
	})

	t.Run("absent field is unset", func(t *testing.T) {
		var got wrapper
		if err := json.Unmarshal([]byte(`{}`), &got); err != nil {
			t.Fatalf("json.Unmarshal() error: %v", err)
		}
		if got.Ptr.IsSet() {
			t.Error("Ptr.IsSet() = true, want false")
		}
	})
}

func TestJSONRoundTrip(t *testing.T) {
	t.Run("string", func(t *testing.T) {
		type w struct {
			V omittable.Value[string] `json:"v,omitzero"`
		}
		original := w{V: omittable.Set("test")}
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal error: %v", err)
		}
		var decoded w
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}
		if diff := cmp.Diff(true, decoded.V.IsSet()); diff != "" {
			t.Errorf("IsSet mismatch (-want +got):\n%s", diff)
		}
		if v := decoded.V.MustGet(); v != "test" {
			t.Errorf("MustGet() = %q, want %q", v, "test")
		}
	})

	t.Run("int", func(t *testing.T) {
		type w struct {
			V omittable.Value[int] `json:"v,omitzero"`
		}
		original := w{V: omittable.Set(42)}
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal error: %v", err)
		}
		var decoded w
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}
		if v := decoded.V.MustGet(); v != 42 {
			t.Errorf("MustGet() = %d, want 42", v)
		}
	})

	t.Run("float64", func(t *testing.T) {
		type w struct {
			V omittable.Value[float64] `json:"v,omitzero"`
		}
		original := w{V: omittable.Set(3.14)}
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal error: %v", err)
		}
		var decoded w
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}
		if v := decoded.V.MustGet(); v != 3.14 {
			t.Errorf("MustGet() = %f, want 3.14", v)
		}
	})

	t.Run("bool", func(t *testing.T) {
		type w struct {
			V omittable.Value[bool] `json:"v,omitzero"`
		}
		original := w{V: omittable.Set(true)}
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal error: %v", err)
		}
		var decoded w
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}
		if v := decoded.V.MustGet(); v != true {
			t.Errorf("MustGet() = %v, want true", v)
		}
	})

	t.Run("pointer string", func(t *testing.T) {
		type w struct {
			V omittable.Value[*string] `json:"v,omitzero"`
		}
		s := "hello"
		original := w{V: omittable.Set(&s)}
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal error: %v", err)
		}
		var decoded w
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}
		v := decoded.V.MustGet()
		if v == nil {
			t.Fatal("MustGet() = nil, want non-nil")
		}
		if *v != "hello" {
			t.Errorf("*MustGet() = %q, want %q", *v, "hello")
		}
	})

	t.Run("time.Time", func(t *testing.T) {
		type w struct {
			V omittable.Value[time.Time] `json:"v,omitzero"`
		}
		ts := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
		original := w{V: omittable.Set(ts)}
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal error: %v", err)
		}
		var decoded w
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}
		if v := decoded.V.MustGet(); !v.Equal(ts) {
			t.Errorf("MustGet() = %v, want %v", v, ts)
		}
	})

	t.Run("byte slice", func(t *testing.T) {
		type w struct {
			V omittable.Value[[]byte] `json:"v,omitzero"`
		}
		original := w{V: omittable.Set([]byte("binary data"))}
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal error: %v", err)
		}
		var decoded w
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}
		if diff := cmp.Diff([]byte("binary data"), decoded.V.MustGet()); diff != "" {
			t.Errorf("MustGet() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("unset round-trips as absent", func(t *testing.T) {
		type w struct {
			V omittable.Value[string] `json:"v,omitzero"`
		}
		original := w{V: omittable.Omit[string]()}
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal error: %v", err)
		}
		if string(data) != "{}" {
			t.Errorf("Marshal(omitted) = %s, want {}", data)
		}
		var decoded w
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}
		if decoded.V.IsSet() {
			t.Error("round-tripped omitted value: IsSet() = true, want false")
		}
	})
}
