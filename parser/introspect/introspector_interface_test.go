package introspect_test

import (
	"context"
	"testing"

	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/parser/introspect"
)

// inwThreeMethods has exactly the three methods an introspector has.
type inwThreeMethods struct{}

func (inwThreeMethods) Name() string { return "" }

func (inwThreeMethods) Introspect(context.Context, string, *parser.Schema, introspect.IntrospectionOptions) error {
	return nil
}

func (inwThreeMethods) Close() error { return nil }

// TestIntrospector_Methods pins the Introspector interface to Name,
// Introspect and Close with their signatures, and that each dialect's
// introspector implements it under its dialect's name.
func TestIntrospector_Methods(t *testing.T) {
	// A type with only the three methods satisfies the interface, so it asks
	// for nothing more.
	var _ introspect.Introspector = inwThreeMethods{}
	// The interface converts to one that lists the three methods, so it has
	// each of them with this signature.
	var _ interface {
		Name() string
		Introspect(ctx context.Context, connString string, schema *parser.Schema, opts introspect.IntrospectionOptions) error
		Close() error
	} = introspect.Introspector(nil)

	tests := []struct {
		name  string
		intro introspect.Introspector
		want  string
	}{
		{name: "postgres", intro: introspect.NewPostgresIntrospector(), want: "postgres"},
		{name: "mysql", intro: introspect.NewMySQLIntrospector(), want: "mysql"},
		{name: "sqlite", intro: introspect.NewSQLiteIntrospector(), want: "sqlite"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.intro.Name(); got != tt.want {
				t.Errorf("Name() = %q, want %q", got, tt.want)
			}
		})
	}
}
