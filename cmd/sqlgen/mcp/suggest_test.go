package mcp

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestSuggest(t *testing.T) {
	candidates := []string{"User", "Users", "Post", "Role", "Comment"}
	tests := []struct {
		name   string
		target string
		cands  []string
		want   []string
	}{
		{
			name:   "within threshold, sorted by distance then name",
			target: "Usr",
			cands:  candidates,
			want:   []string{"User", "Users"}, // dist 1 (User), dist 2 (Users)
		},
		{
			name:   "case-insensitive match",
			target: "user",
			cands:  candidates,
			want:   []string{"User", "Users"}, // User dist 0, Users dist 1
		},
		{
			name:   "top-3 cap",
			target: "aaa",
			cands:  []string{"aab", "aac", "aad", "aae", "aaa"},
			want:   []string{"aaa", "aab", "aac"}, // dist 0,1,1,1,1 -> cap 3, lexicographic tie-break
		},
		{
			name:   "distance tie broken lexicographically",
			target: "cat",
			cands:  []string{"bat", "hat", "car"},
			want:   []string{"bat", "car", "hat"}, // all dist 1, sorted by name
		},
		{
			name:   "nothing within threshold yields empty",
			target: "xyzzy",
			cands:  candidates,
			want:   []string{},
		},
		{
			name:   "empty candidate set yields empty",
			target: "User",
			cands:  nil,
			want:   []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := suggest(tt.target, tt.cands)
			if got == nil {
				t.Fatal("suggest must return a non-nil slice")
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("suggestions mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestLevenshtein(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "", 3},
		{"", "abc", 3},
		{"kitten", "sitting", 3},
		{"flaw", "lawn", 2},
		{"User", "User", 0},
		{"User", "Usr", 1},
	}
	for _, tt := range tests {
		if got := levenshtein(tt.a, tt.b); got != tt.want {
			t.Errorf("levenshtein(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}
