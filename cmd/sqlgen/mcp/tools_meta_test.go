package mcp

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestGetConventions(t *testing.T) {
	d := fixtureDeps()
	got, err := getConventions(d, GetConventionsInput{})
	if err != nil {
		t.Fatalf("getConventions: %v", err)
	}
	want := fixtureDoc().Conventions
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("conventions mismatch (-want +got):\n%s", diff)
	}
}

func TestGetExample(t *testing.T) {
	d := fixtureDeps()

	t.Run("returns read and write snippets", func(t *testing.T) {
		got, err := getExample(d, GetExampleInput{Entity: "User"})
		if err != nil {
			t.Fatalf("getExample: %v", err)
		}
		want := GetExampleOutput{
			Entity: "User",
			Read:   []string{"u, err := c.Users().Get(ctx, id)"},
			Write:  []string{"u, err := c.Users().Create(ctx, in)"},
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("example mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("op=read drops write", func(t *testing.T) {
		got, err := getExample(d, GetExampleInput{Entity: "User", Op: "read"})
		if err != nil {
			t.Fatalf("getExample: %v", err)
		}
		if len(got.Read) != 1 || len(got.Write) != 0 {
			t.Errorf("op=read should keep read only, got %+v", got)
		}
	})

	t.Run("entity without examples yields empty arrays", func(t *testing.T) {
		got, err := getExample(d, GetExampleInput{Entity: "Post"})
		if err != nil {
			t.Fatalf("getExample: %v", err)
		}
		if got.Read == nil || got.Write == nil || len(got.Read) != 0 || len(got.Write) != 0 {
			t.Errorf("expected empty non-nil arrays, got %+v", got)
		}
	})

	t.Run("unknown entity yields -32003", func(t *testing.T) {
		_, err := getExample(d, GetExampleInput{Entity: "Usr"})
		assertNotFound(t, err, CodeEntityNotFound, []string{"User"})
	})
}
