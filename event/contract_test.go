package event_test

import (
	"os/exec"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/event"
)

// field is one struct field as the wire contract sees it.
type field struct {
	Name string
	Type string
	Tag  string
}

func fieldsOf[T any]() []field {
	rt := reflect.TypeFor[T]()
	out := make([]field, 0, rt.NumField())
	for f := range rt.Fields() {
		out = append(out, field{Name: f.Name, Type: f.Type.String(), Tag: f.Tag.Get("json")})
	}
	return out
}

// TestEventStructMatchesWireContract pins every Event field: its name, Go type
// (PK and Input are any) and json key, in declaration order.
func TestEventStructMatchesWireContract(t *testing.T) {
	want := []field{
		{Name: "ID", Type: "string", Tag: "id"},
		{Name: "Table", Type: "string", Tag: "table"},
		{Name: "Schema", Type: "string", Tag: "schema"},
		{Name: "Action", Type: "event.Action", Tag: "action"},
		{Name: "PK", Type: "interface {}", Tag: "pk"},
		{Name: "Input", Type: "interface {}", Tag: "input"},
		{Name: "Timestamp", Type: "time.Time", Tag: "timestamp"},
		{Name: "Metadata", Type: "map[string]string", Tag: "metadata,omitempty"},
	}
	if diff := cmp.Diff(want, fieldsOf[event.Event]()); diff != "" {
		t.Errorf("Event fields mismatch (-want +got):\n%s", diff)
	}
	if got := reflect.TypeFor[event.Action]().Kind(); got != reflect.String {
		t.Errorf("Action kind = %v, want string", got)
	}
}

// TestSubscribeOptionsFields pins SubscribeOptions to the three
// transport-agnostic filters: no transport knob lives on it.
func TestSubscribeOptionsFields(t *testing.T) {
	want := []field{
		{Name: "Tables", Type: "[]string"},
		{Name: "Actions", Type: "[]event.Action"},
		{Name: "Group", Type: "string"},
	}
	if diff := cmp.Diff(want, fieldsOf[event.SubscribeOptions]()); diff != "" {
		t.Errorf("SubscribeOptions fields mismatch (-want +got):\n%s", diff)
	}
}

func methodsOf[T any]() []string {
	rt := reflect.TypeFor[T]()
	out := make([]string, 0, rt.NumMethod())
	for m := range rt.Methods() {
		out = append(out, m.Name+" "+m.Type.String())
	}
	return out
}

// TestInterfaceSignatures pins the method sets of Publisher, Subscriber and
// Subscription, and the Handler's parameters and result.
func TestInterfaceSignatures(t *testing.T) {
	tests := []struct {
		name string
		got  []string
		want []string
	}{
		{
			name: "Publisher",
			got:  methodsOf[event.Publisher](),
			want: []string{
				"Close func() error",
				"Publish func(context.Context, event.Event) error",
				"PublishBatch func(context.Context, []event.Event) error",
			},
		},
		{
			name: "Subscriber",
			got:  methodsOf[event.Subscriber](),
			want: []string{
				"Close func() error",
				"Subscribe func(event.SubscribeOptions, event.Handler) (event.Subscription, error)",
			},
		},
		{
			name: "Subscription",
			got:  methodsOf[event.Subscription](),
			want: []string{"Unsubscribe func() error"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if diff := cmp.Diff(tt.want, tt.got); diff != "" {
				t.Errorf("%s mismatch (-want +got):\n%s", tt.name, diff)
			}
		})
	}
	// The Handler's own signature, in and out.
	h := reflect.TypeFor[event.Handler]()
	var sig []string
	for in := range h.Ins() {
		sig = append(sig, "in "+in.String())
	}
	for out := range h.Outs() {
		sig = append(sig, "out "+out.String())
	}
	if diff := cmp.Diff([]string{"in context.Context", "in event.Event", "out error"}, sig); diff != "" {
		t.Errorf("Handler signature mismatch (-want +got):\n%s", diff)
	}
}

// TestEventPackagesImportOnlyStdlib: event depends only on the standard
// library, and memorybus only on event and the standard library.
func TestEventPackagesImportOnlyStdlib(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not on PATH; skipping deps check")
	}
	tests := []struct {
		name    string
		pkg     string
		allowed []string
	}{
		{name: "event", pkg: ".", allowed: []string{"github.com/teandresmith/sqlgen/event"}},
		{name: "memorybus", pkg: "./memorybus", allowed: []string{"github.com/teandresmith/sqlgen/event", "github.com/teandresmith/sqlgen/event/memorybus"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := exec.Command(goBin, "list", "-deps", tt.pkg).Output() //nolint:gosec // G204: constant args, trusted go binary
			if err != nil {
				t.Fatalf("go list -deps %s: %v", tt.pkg, err)
			}
			var external []string
			for dep := range strings.FieldsSeq(string(out)) {
				first, _, _ := strings.Cut(dep, "/")
				if !strings.Contains(first, ".") {
					continue // standard library
				}
				if !slices.Contains(tt.allowed, dep) {
					external = append(external, dep)
				}
			}
			if len(external) != 0 {
				t.Errorf("%s imports outside the standard library: %v", tt.pkg, external)
			}
		})
	}
}
