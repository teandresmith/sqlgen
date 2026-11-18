// Package reqcheck checks the PRD requirement registry under docs/conformance against the PRD and
// the tests it names (docs/design/CONFORMANCE.md §5). It is repository tooling: nothing in the
// sqlgen binary imports it.
package reqcheck

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Section is one PRD heading and the text under it.
type Section struct {
	// Key is the section number ("9.4c", "4", "Appendix A"). An unnumbered level-2 heading is
	// keyed by its title; a deeper unnumbered heading has no key and belongs to its parent.
	Key   string
	Title string
	Level int
	Line  int
	// Hash covers the heading line through the end of the section, subsections included.
	Hash string
}

// PRD is the parsed specification.
type PRD struct {
	Sections []Section
	byKey    map[string]int
}

var sectionKeyRE = regexp.MustCompile(`^(Appendix [A-Z]|\d+[A-Za-z]?(?:\.\d+[a-z]?)*)\.?(?:[:\s]+|$)`)

// LoadPRD reads and parses the PRD at path.
func LoadPRD(path string) (*PRD, error) {
	data, err := os.ReadFile(path) //nolint:gosec // the repository's docs/PRD.md
	if err != nil {
		return nil, fmt.Errorf("reading prd: %w", err)
	}
	return ParsePRD(string(data))
}

// ParsePRD parses PRD markdown. Headings inside fenced code blocks are ignored.
func ParsePRD(text string) (*PRD, error) {
	lines := strings.Split(text, "\n")
	type heading struct{ idx, level int }
	var heads []heading
	var fence fenceState
	for i, line := range lines {
		if fence.toggle(line) || fence.open() {
			continue
		}
		if level := headingLevel(line); level > 0 {
			heads = append(heads, heading{i, level})
		}
	}

	p := &PRD{byKey: map[string]int{}}
	for n, h := range heads {
		end := len(lines)
		for _, next := range heads[n+1:] {
			if next.level <= h.level {
				end = next.idx
				break
			}
		}
		text := strings.TrimSpace(lines[h.idx][h.level:])
		key, title := splitSectionKey(text)
		if key == "" && h.level == 2 {
			key = title
		}
		sum := sha256.Sum256([]byte(strings.Join(lines[h.idx:end], "\n")))
		s := Section{Key: key, Title: title, Level: h.level, Line: h.idx + 1, Hash: hex.EncodeToString(sum[:8])}
		if key != "" {
			if prev, dup := p.byKey[key]; dup {
				return nil, fmt.Errorf("prd line %d: section %q repeats the number of line %d", s.Line, key, p.Sections[prev].Line)
			}
			p.byKey[key] = len(p.Sections)
		}
		p.Sections = append(p.Sections, s)
	}
	return p, nil
}

// Section returns the section with the given key.
func (p *PRD) Section(key string) (Section, bool) {
	i, ok := p.byKey[key]
	if !ok {
		return Section{}, false
	}
	return p.Sections[i], true
}

func splitSectionKey(text string) (key, title string) {
	m := sectionKeyRE.FindStringSubmatchIndex(text)
	if m == nil {
		return "", text
	}
	return text[m[2]:m[3]], strings.TrimSpace(text[m[1]:])
}

func headingLevel(line string) int {
	n := 0
	for n < len(line) && line[n] == '#' {
		n++
	}
	if n == 0 || n > 6 || n == len(line) || line[n] != ' ' {
		return 0
	}
	return n
}

// fenceState tracks CommonMark fenced code blocks (``` or ~~~, three or more).
type fenceState struct {
	char byte
	size int
}

func (f *fenceState) open() bool { return f.size > 0 }

// toggle reports whether line opens or closes a fence, updating the state.
func (f *fenceState) toggle(line string) bool {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 || len(trimmed) < 3 || (trimmed[0] != '`' && trimmed[0] != '~') {
		return false
	}
	c := trimmed[0]
	n := 0
	for n < len(trimmed) && trimmed[n] == c {
		n++
	}
	if n < 3 {
		return false
	}
	if !f.open() {
		f.char, f.size = c, n
		return true
	}
	// A closing fence repeats the opening character at least as many times, with nothing after.
	if c == f.char && n >= f.size && strings.TrimSpace(trimmed[n:]) == "" {
		f.char, f.size = 0, 0
		return true
	}
	return false
}
