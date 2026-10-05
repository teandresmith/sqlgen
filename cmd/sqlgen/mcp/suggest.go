package mcp

import (
	"sort"
	"strings"
)

// suggestMaxDistance is the inclusive Levenshtein threshold for a fuzzy match
// (MCP.md §5.3). A candidate further than this from the target is never
// suggested.
const suggestMaxDistance = 2

// suggestCap bounds a suggestion list to the closest few candidates (MCP.md
// §5.3).
const suggestCap = 3

// suggest returns up to suggestCap candidates within Levenshtein distance
// suggestMaxDistance of target, matched case-insensitively, ordered by distance
// ascending then lexicographically (MCP.md §5.3). It returns a non-nil empty
// slice when nothing is within threshold so the JSON envelope always carries a
// suggestions array.
func suggest(target string, candidates []string) []string {
	lt := strings.ToLower(target)
	type scored struct {
		name string
		dist int
	}
	matches := make([]scored, 0, len(candidates))
	for _, c := range candidates {
		d := levenshtein(lt, strings.ToLower(c))
		if d <= suggestMaxDistance {
			matches = append(matches, scored{name: c, dist: d})
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].dist != matches[j].dist {
			return matches[i].dist < matches[j].dist
		}
		return matches[i].name < matches[j].name
	})
	if len(matches) > suggestCap {
		matches = matches[:suggestCap]
	}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m.name)
	}
	return out
}

// levenshtein computes the edit distance between a and b using the standard
// two-row dynamic-programming table. Inputs are compared rune-wise so
// multi-byte identifiers count edits by character, not byte.
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	if len(ra) == 0 {
		return len(rb)
	}
	if len(rb) == 0 {
		return len(ra)
	}
	prev := make([]int, len(rb)+1)
	curr := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		curr[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			curr[j] = min(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(rb)]
}
