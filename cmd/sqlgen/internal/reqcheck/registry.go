package reqcheck

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Seeding states a registry file declares (CONFORMANCE.md §5.4).
const (
	SeedingNotStarted = "not-started"
	SeedingInProgress = "in-progress"
	SeedingComplete   = "complete"
)

// Informative marks a PRD section in the ownership map that states no behavior.
const Informative = "informative"

// Registry is one module's requirement file.
type Registry struct {
	Path      string // slash-separated, relative to the repository root
	Name      string // the ownership-map owner name, e.g. "cli.md" or "cli/config.md"
	Seeding   string
	Packages  []string
	Matrices  map[string]Matrix
	Anchors   []Anchor
	Rows      []Row
	NA        []NA
	Unclaimed []Unclaimed
}

// Matrix is a declared conformance matrix: an open set of rows and a fixed set of columns.
type Matrix struct {
	Name    string
	RowUnit string
	Cols    []string
	Line    int
}

// Anchor ties a section of the registry file to a PRD section and its reviewed hash.
type Anchor struct {
	Key  string
	Hash string
	Line int
}

// Row is one requirement.
type Row struct {
	ID          string
	Requirement string
	Tests       []string
	AppliesTo   []string
	Cell        string
	Anchor      string // the PRD key of the enclosing anchor
	Line        int
}

// NA declares a matrix cell that does not apply.
type NA struct {
	Cell     string
	Reason   string
	Accepted string
	Line     int
}

// Unclaimed lists a test that backs no PRD behavior.
type Unclaimed struct {
	Test   string
	Reason string
	Line   int
}

// Ownership is the PRD section ownership map (CONFORMANCE.md §5.2).
type Ownership struct {
	Path    string
	Entries []OwnershipEntry
	// Unregistered lists test directories that back no PRD behavior (test support, repository
	// tooling), so the unlisted-package check passes over them.
	Unregistered []string
}

// OwnershipEntry assigns one PRD section to a registry file or marks it informative.
type OwnershipEntry struct {
	Key   string
	Owner string
	Line  int
}

var (
	directiveRE    = regexp.MustCompile(`^<!--\s*(seeding|packages|matrix|prd):\s*(.*?)\s*-->$`)
	prdAnchorRE    = regexp.MustCompile(`^(\S+(?: [A-Z])?)(?:\s+sha256:\s*([0-9a-f]+|pending))?$`)
	matrixRE       = regexp.MustCompile(`^(\S+)\s+rows=(\S+)\s+cols=(\S+)$`)
	unregisteredRE = regexp.MustCompile(`^<!--\s*unregistered:\s*(.*?)\s*-->$`)
	mapKeyRE       = regexp.MustCompile(`^§\s*(Appendix [A-Z]|\d+[A-Za-z]?(?:\.\d+[a-z]?)*)\b`)
)

// Table headers, as cells joined by "|".
const (
	headerRows      = "ID|Requirement|Tests|Applies to|Matrix cell"
	headerNA        = "Matrix cell|Reason|Accepted"
	headerUnclaimed = "Unclaimed test|Reason"
	headerOwnership = "PRD section|Owner"
)

// LoadRegistry reads and parses a registry file. rel is its path relative to the repository
// root, and name its ownership-map owner name.
func LoadRegistry(path, rel, name string) (*Registry, []Finding, error) {
	data, err := os.ReadFile(path) //nolint:gosec // a registry file under docs/conformance, found by glob
	if err != nil {
		return nil, nil, fmt.Errorf("reading registry %s: %w", rel, err)
	}
	r, findings := ParseRegistry(string(data), rel, name)
	return r, findings, nil
}

// ParseRegistry parses registry markdown. Format errors come back as findings.
func ParseRegistry(text, rel, name string) (*Registry, []Finding) {
	p := &registryParser{r: &Registry{Path: rel, Name: name, Matrices: map[string]Matrix{}}}
	lines := strings.Split(text, "\n")
	var fence fenceState
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if fence.toggle(lines[i]) || fence.open() {
			continue
		}
		if m := directiveRE.FindStringSubmatch(line); m != nil {
			p.directive(i+1, m[1], m[2])
			continue
		}
		if strings.HasPrefix(line, "|") {
			i = p.table(lines, i)
		}
	}
	if p.r.Seeding == "" {
		p.bad(1, "no `<!-- seeding: … -->` declaration")
	}
	return p.r, p.findings
}

type registryParser struct {
	r        *Registry
	findings []Finding
	anchor   string // the PRD key of the most recent anchor
}

func (p *registryParser) bad(line int, format string, args ...any) {
	p.findings = append(p.findings, Finding{File: p.r.Path, Line: line, Msg: fmt.Sprintf(format, args...)})
}

func (p *registryParser) directive(lineNo int, kind, value string) {
	r := p.r
	switch kind {
	case "seeding":
		switch value {
		case SeedingNotStarted, SeedingInProgress, SeedingComplete:
			if r.Seeding != "" {
				p.bad(lineNo, "seeding is declared twice")
			}
			r.Seeding = value
		default:
			p.bad(lineNo, "seeding %q is not one of %s, %s, %s", value, SeedingNotStarted, SeedingInProgress, SeedingComplete)
		}
	case "packages":
		r.Packages = append(r.Packages, strings.Fields(value)...)
	case "matrix":
		m := matrixRE.FindStringSubmatch(value)
		if m == nil {
			p.bad(lineNo, "matrix declaration %q is not `<name> rows=<unit> cols=<a,b,…>`", value)
			return
		}
		if _, dup := r.Matrices[m[1]]; dup {
			p.bad(lineNo, "matrix %q is declared twice", m[1])
		}
		r.Matrices[m[1]] = Matrix{Name: m[1], RowUnit: m[2], Cols: strings.Split(m[3], ","), Line: lineNo}
	case "prd":
		m := prdAnchorRE.FindStringSubmatch(value)
		if m == nil {
			p.bad(lineNo, "prd anchor %q is not `<section> sha256: <hash>`", value)
			return
		}
		p.anchor = m[1]
		r.Anchors = append(r.Anchors, Anchor{Key: m[1], Hash: m[2], Line: lineNo})
	}
}

// table parses the table whose header is lines[i] and returns the index of its last row.
func (p *registryParser) table(lines []string, i int) int {
	headerNo := i + 1
	header := splitRow(lines[i])
	i++
	if i >= len(lines) || !isSeparator(lines[i]) {
		p.bad(headerNo, "table header is not followed by a separator row")
		return i - 1
	}
	kind := strings.Join(header, "|")
	switch kind {
	case headerRows, headerNA, headerUnclaimed:
	default:
		p.bad(headerNo, "unrecognized table %q; registry tables are requirements (%s), n/a (%s) or unclaimed (%s)",
			kind, headerRows, headerNA, headerUnclaimed)
	}
	for i+1 < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i+1]), "|") {
		i++
		cells := splitRow(lines[i])
		if len(cells) != len(header) {
			p.bad(i+1, "row has %d cells, the table header has %d", len(cells), len(header))
			continue
		}
		p.row(kind, cells, i+1)
	}
	return i
}

func (p *registryParser) row(kind string, cells []string, rowNo int) {
	r := p.r
	switch kind {
	case headerRows:
		if p.anchor == "" {
			p.bad(rowNo, "requirement %q is not under a `<!-- prd: … -->` anchor", cells[0])
		}
		r.Rows = append(r.Rows, Row{
			ID:          stripTicks(cells[0]),
			Requirement: cells[1],
			Tests:       splitList(cells[2], ";"),
			AppliesTo:   splitList(cells[3], ","),
			Cell:        stripTicks(cells[4]),
			Anchor:      p.anchor,
			Line:        rowNo,
		})
	case headerNA:
		r.NA = append(r.NA, NA{Cell: stripTicks(cells[0]), Reason: cells[1], Accepted: cells[2], Line: rowNo})
	case headerUnclaimed:
		r.Unclaimed = append(r.Unclaimed, Unclaimed{Test: stripTicks(cells[0]), Reason: cells[1], Line: rowNo})
	}
}

// ParseOwnership parses the ownership map: a "PRD section | Owner" table.
func ParseOwnership(text, rel string) (*Ownership, []Finding) {
	o := &Ownership{Path: rel}
	var findings []Finding
	lines := strings.Split(text, "\n")
	var fence fenceState
	for i := 0; i < len(lines); i++ {
		if fence.toggle(lines[i]) || fence.open() {
			continue
		}
		line := strings.TrimSpace(lines[i])
		if m := unregisteredRE.FindStringSubmatch(line); m != nil {
			o.Unregistered = append(o.Unregistered, strings.Fields(m[1])...)
			continue
		}
		if !strings.HasPrefix(line, "|") || strings.Join(splitRow(line), "|") != headerOwnership {
			continue
		}
		i++
		for i+1 < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i+1]), "|") {
			i++
			cells := splitRow(strings.TrimSpace(lines[i]))
			if len(cells) != 2 {
				findings = append(findings, Finding{File: rel, Line: i + 1, Msg: "ownership row needs two cells"})
				continue
			}
			key := cells[0]
			if m := mapKeyRE.FindStringSubmatch(key); m != nil {
				key = m[1]
			} else {
				key = strings.TrimSpace(strings.TrimPrefix(key, "§"))
			}
			o.Entries = append(o.Entries, OwnershipEntry{Key: key, Owner: stripTicks(cells[1]), Line: i + 1})
		}
	}
	if len(o.Entries) == 0 {
		findings = append(findings, Finding{File: rel, Line: 1, Msg: "no `| PRD section | Owner |` table"})
	}
	return o, findings
}

// splitRow splits a markdown table row on unescaped pipes.
func splitRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	var cells []string
	var b strings.Builder
	for i := 0; i < len(line); i++ {
		switch {
		case line[i] == '\\' && i+1 < len(line) && line[i+1] == '|':
			b.WriteByte('|')
			i++
		case line[i] == '|':
			cells = append(cells, strings.TrimSpace(b.String()))
			b.Reset()
		default:
			b.WriteByte(line[i])
		}
	}
	return append(cells, strings.TrimSpace(b.String()))
}

func isSeparator(line string) bool {
	for _, c := range splitRow(line) {
		if strings.Trim(c, ":-") != "" || !strings.Contains(c, "-") {
			return false
		}
	}
	return true
}

func splitList(cell, sep string) []string {
	var out []string
	for item := range strings.SplitSeq(cell, sep) {
		item = stripTicks(item)
		if item != "" && item != "—" && item != "-" {
			out = append(out, item)
		}
	}
	return out
}

func stripTicks(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(strings.TrimSpace(s), "`", ""))
}
