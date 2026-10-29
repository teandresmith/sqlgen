package reqcheck

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Repository-relative locations the check reads.
const (
	PRDPath     = "docs/PRD.md"
	RegistryDir = "docs/conformance"
	MapFile     = "README.md"
	ReportFile  = "REPORT.md"
)

// Finding is one problem the check reports. A note is reported but does not fail the check:
// coverage gaps in a module still being seeded (CONFORMANCE.md §5.4).
type Finding struct {
	File string
	Line int
	Msg  string
	Note bool
}

func (f Finding) String() string {
	if f.Line > 0 {
		return fmt.Sprintf("%s:%d: %s", f.File, f.Line, f.Msg)
	}
	return fmt.Sprintf("%s: %s", f.File, f.Msg)
}

// Result is the outcome of a check run.
type Result struct {
	Findings   []Finding
	Registries []*Registry
	Ownership  *Ownership
}

// Errors returns the findings that fail the check.
func (r Result) Errors() []Finding {
	return slices.DeleteFunc(slices.Clone(r.Findings), func(f Finding) bool { return f.Note })
}

// Notes returns the findings that are reported only.
func (r Result) Notes() []Finding {
	return slices.DeleteFunc(slices.Clone(r.Findings), func(f Finding) bool { return !f.Note })
}

var (
	idRE       = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)
	dateRE     = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	prdCiteRE  = regexp.MustCompile(`§\s*(Appendix [A-Z]|\d+[A-Za-z]?(?:\.\d+[a-z]?)*)`)
	appliesSet = []string{"all", "postgres", "mysql", "sqlite", "pgx", "stdlib"}
	rulingRE   = regexp.MustCompile(`^(amend-prd|drop-pin) (\d{4}-\d{2}-\d{2})$`)
)

// GapIDPrefix starts every PRD gap ID, so a gap never reads as a requirement.
const GapIDPrefix = "gap."

// Check runs every static check (CONFORMANCE.md §5.3) against the repository at root.
func Check(root string) (Result, error) {
	prd, err := LoadPRD(filepath.Join(root, PRDPath))
	if err != nil {
		return Result{}, err
	}
	regs, own, findings, err := loadRegistry(root)
	if err != nil {
		return Result{}, err
	}
	c := &checker{root: root, prd: prd, regs: regs, own: own, findings: findings, trees: map[string]testTree{}}
	c.checkOwnership()
	for _, r := range regs {
		c.checkRegistry(r)
	}
	// The unit orphan check runs once every file's claims are in: a row or gap in one file may
	// claim a test in a package another file lists.
	for _, r := range regs {
		if r.Seeding == SeedingComplete {
			c.coverTests(r)
		}
	}
	c.checkUniqueIDs()
	c.checkConformanceOrphans()
	if err := c.checkUnlistedPackages(); err != nil {
		return Result{}, err
	}
	slices.SortStableFunc(c.findings, func(a, b Finding) int {
		return cmp.Or(cmp.Compare(a.File, b.File), cmp.Compare(a.Line, b.Line))
	})
	return Result{Findings: c.findings, Registries: regs, Ownership: own}, nil
}

// loadRegistry reads the ownership map and every registry file: docs/conformance/*.md and, for
// a module split by package, docs/conformance/<module>/*.md.
func loadRegistry(root string) ([]*Registry, *Ownership, []Finding, error) {
	dir := filepath.Join(root, RegistryDir)
	mapRel := RegistryDir + "/" + MapFile
	data, err := os.ReadFile(filepath.Join(dir, MapFile)) //nolint:gosec // the registry's own README.md
	if err != nil {
		return nil, nil, nil, fmt.Errorf("reading ownership map: %w", err)
	}
	own, findings := ParseOwnership(string(data), mapRel)

	var names []string
	for _, pattern := range []string{"*.md", "*/*.md"} {
		matches, err := filepath.Glob(filepath.Join(dir, pattern))
		if err != nil {
			return nil, nil, nil, fmt.Errorf("listing registry files: %w", err)
		}
		for _, m := range matches {
			rel, err := filepath.Rel(dir, m)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("listing registry files: %w", err)
			}
			rel = filepath.ToSlash(rel)
			if rel != MapFile && rel != ReportFile {
				names = append(names, rel)
			}
		}
	}
	slices.Sort(names)

	var regs []*Registry
	for _, name := range names {
		r, fs, err := LoadRegistry(filepath.Join(dir, filepath.FromSlash(name)), RegistryDir+"/"+name, name)
		if err != nil {
			return nil, nil, nil, err
		}
		regs = append(regs, r)
		findings = append(findings, fs...)
	}
	return regs, own, findings, nil
}

type checker struct {
	root     string
	prd      *PRD
	regs     []*Registry
	own      *Ownership
	findings []Finding
	trees    map[string]testTree // by package directory
	treeErrs map[string]error
	// claimed records every resolved test reference: dir → top-level test → claimed paths.
	claimed map[string]map[string][][]string
}

func (c *checker) fail(file string, line int, format string, args ...any) {
	c.findings = append(c.findings, Finding{File: file, Line: line, Msg: fmt.Sprintf(format, args...)})
}

func (c *checker) note(file string, line int, format string, args ...any) {
	c.findings = append(c.findings, Finding{File: file, Line: line, Msg: fmt.Sprintf(format, args...), Note: true})
}

// mappable reports whether a PRD section must appear in the ownership map: every numbered
// level-2 or level-3 heading, and every level-2 heading. Deeper and unnumbered headings belong
// to their nearest mapped ancestor.
func mappable(s Section) bool {
	return s.Key != "" && (s.Level == 2 || s.Level == 3)
}

// checkOwnership is check 5: every mappable section is owned exactly once, by a registry file
// that exists or as informative, and the map names no section the PRD lacks.
func (c *checker) checkOwnership() {
	files := map[string]bool{}
	for _, r := range c.regs {
		files[r.Name] = true
	}
	seen := map[string]int{}
	for _, e := range c.own.Entries {
		if prev, dup := seen[e.Key]; dup {
			c.fail(c.own.Path, e.Line, "§%s is mapped twice (also line %d)", e.Key, prev)
			continue
		}
		seen[e.Key] = e.Line
		s, ok := c.prd.Section(e.Key)
		switch {
		case !ok:
			c.fail(c.own.Path, e.Line, "§%s is not a PRD section heading", e.Key)
		case !mappable(s):
			c.fail(c.own.Path, e.Line, "§%s is a level-%d heading; the map lists level-2 and level-3 sections only", e.Key, s.Level)
		}
		if e.Owner != Informative && !files[e.Owner] {
			c.fail(c.own.Path, e.Line, "§%s is owned by %q, which is neither a registry file in %s nor %q", e.Key, e.Owner, RegistryDir, Informative)
		}
	}
	for _, s := range c.prd.Sections {
		if mappable(s) {
			if _, ok := seen[s.Key]; !ok {
				c.fail(c.own.Path, 0, "§%s %s (PRD line %d) has no owner; add it to the ownership map", s.Key, s.Title, s.Line)
			}
		}
	}
}

// owner returns the owner of a PRD key: the map entry for the key or its nearest mapped
// ancestor ("9.8.10" → "9.8" → "9").
func (c *checker) owner(key string) (string, bool) {
	for k := key; k != ""; {
		for _, e := range c.own.Entries {
			if e.Key == k {
				return e.Owner, true
			}
		}
		i := strings.LastIndex(k, ".")
		if i < 0 {
			break
		}
		k = k[:i]
	}
	return "", false
}

func (c *checker) checkRegistry(r *Registry) {
	for _, p := range r.Packages {
		if fi, err := os.Stat(filepath.Join(c.root, filepath.FromSlash(p))); err != nil || !fi.IsDir() {
			c.fail(r.Path, 0, "package directory %q does not exist", p)
		}
	}
	if r.Seeding == SeedingNotStarted && (len(r.Rows) > 0 || len(r.NA) > 0) {
		c.fail(r.Path, 0, "seeding is %s but the file has rows; set it to %s", SeedingNotStarted, SeedingInProgress)
	}
	c.checkAnchors(r)
	claimed := c.checkRows(r)
	c.checkNA(r, claimed)
	c.checkUnclaimed(r)
	c.checkGaps(r)
	c.checkCoverage(r, claimed)
}

// checkAnchors is checks 3 and 4: every anchor names a PRD section that states behavior, and its
// hash matches the section's current text.
func (c *checker) checkAnchors(r *Registry) {
	for _, a := range r.Anchors {
		s, ok := c.prd.Section(a.Key)
		if !ok {
			c.fail(r.Path, a.Line, "anchor §%s is not a PRD section heading", a.Key)
			continue
		}
		if owner, ok := c.owner(a.Key); ok && owner == Informative {
			c.fail(r.Path, a.Line, "anchor §%s is an informative section; it states no behavior to require", a.Key)
		}
		switch a.Hash {
		case s.Hash:
		case "", "pending":
			c.fail(r.Path, a.Line, "anchor §%s has no reviewed hash; review its rows against the PRD, then run make req-rehash", a.Key)
		default:
			c.fail(r.Path, a.Line, "PRD §%s %s changed since its requirements were reviewed; review them, then run make req-rehash SECTIONS=%s", a.Key, s.Title, a.Key)
		}
	}
}

// checkRows validates each requirement row and returns the matrix cells the rows claim.
func (c *checker) checkRows(r *Registry) map[string]int {
	claimed := map[string]int{}
	for _, row := range r.Rows {
		if !idRE.MatchString(row.ID) {
			c.fail(r.Path, row.Line, "id %q is not a lower-case slug like `area.behavior`", row.ID)
		}
		if strings.HasPrefix(row.ID, GapIDPrefix) {
			c.fail(r.Path, row.Line, "requirement id %q starts with `%s`, which marks a PRD gap", row.ID, GapIDPrefix)
		}
		if strings.TrimSpace(row.Requirement) == "" {
			c.fail(r.Path, row.Line, "requirement %s has no statement", row.ID)
		}
		c.checkAppliesTo(r, row)
		for _, t := range row.Tests {
			c.checkTestRef(r, row.Line, t)
		}
		if row.Cell != "" && c.checkCell(r, row.Line, row.Cell) {
			claimed[normCell(row.Cell)] = row.Line
		}
	}
	return claimed
}

// checkNA is the n/a half of check 6.
func (c *checker) checkNA(r *Registry, claimed map[string]int) {
	for _, na := range r.NA {
		if !c.checkCell(r, na.Line, na.Cell) {
			continue
		}
		if line, ok := claimed[normCell(na.Cell)]; ok {
			c.fail(r.Path, na.Line, "cell %q is declared n/a but requirement on line %d claims it", na.Cell, line)
		}
		if cites := prdCiteRE.FindAllStringSubmatch(na.Reason, -1); len(cites) > 0 {
			for _, m := range cites {
				if _, ok := c.prd.Section(m[1]); !ok {
					c.fail(r.Path, na.Line, "n/a cell %q cites §%s, which is not a PRD section", na.Cell, m[1])
				}
			}
			continue
		}
		switch accepted := strings.TrimSpace(na.Accepted); {
		case accepted == "":
			c.fail(r.Path, na.Line, "n/a cell %q cites no PRD section; cite one (§…) or record the user's acceptance date", na.Cell)
		case !dateRE.MatchString(accepted):
			c.fail(r.Path, na.Line, "n/a cell %q: accepted %q is not a YYYY-MM-DD date", na.Cell, na.Accepted)
		}
	}
}

func (c *checker) checkUnclaimed(r *Registry) {
	for _, u := range r.Unclaimed {
		if strings.TrimSpace(u.Reason) == "" {
			c.fail(r.Path, u.Line, "unclaimed test %q has no reason", u.Test)
		}
		dir, name, ok := strings.Cut(u.Test, ":")
		if !ok {
			c.fail(r.Path, u.Line, "unclaimed test %q is not `<dir>: <TestName>`", u.Test)
			continue
		}
		c.resolve(r, u.Line, TestRef{Home: HomeUnit, Dir: strings.TrimSpace(dir), Path: splitPath(strings.TrimSpace(name)), Raw: u.Test}, false)
	}
}

// checkGaps validates the PRD gaps table. A gap's tests must resolve, and they count as claimed:
// the gap is the reason they exist. An open gap is a note in every seeding state, and so is a
// ruled gap whose ruling has not been applied yet.
func (c *checker) checkGaps(r *Registry) {
	for _, g := range r.Gaps {
		if !idRE.MatchString(g.ID) || !strings.HasPrefix(g.ID, GapIDPrefix) {
			c.fail(r.Path, g.Line, "gap id %q is not a lower-case slug starting with `%s`", g.ID, GapIDPrefix)
		}
		if strings.TrimSpace(g.Behavior) == "" {
			c.fail(r.Path, g.Line, "gap %s has no behavior", g.ID)
		}
		for _, t := range g.Tests {
			c.checkTestRef(r, g.Line, t)
		}
		cites := prdCiteRE.FindAllStringSubmatch(g.Section, -1)
		if len(cites) == 0 {
			c.fail(r.Path, g.Line, "gap %s names no PRD section; cite the nearest one (§…)", g.ID)
		}
		for _, m := range cites {
			if _, ok := c.prd.Section(m[1]); !ok {
				c.fail(r.Path, g.Line, "gap %s cites §%s, which is not a PRD section", g.ID, m[1])
			}
		}
		switch m := rulingRE.FindStringSubmatch(g.Ruling); {
		case g.Ruling == "":
			c.note(r.Path, g.Line, "PRD gap %s is open; bring it to the user for a ruling (amend-prd or drop-pin)", g.ID)
		case m == nil:
			c.fail(r.Path, g.Line, "gap %s: ruling %q is not `amend-prd YYYY-MM-DD` or `drop-pin YYYY-MM-DD`", g.ID, g.Ruling)
		default:
			c.note(r.Path, g.Line, "PRD gap %s was ruled %s on %s; apply the ruling and delete the entry", g.ID, m[1], m[2])
		}
	}
}

func (c *checker) checkAppliesTo(r *Registry, row Row) {
	if len(row.AppliesTo) == 0 {
		c.fail(r.Path, row.Line, "requirement %s has no applies-to; use `all` or a dialect / driver list", row.ID)
		return
	}
	for _, a := range row.AppliesTo {
		if !slices.Contains(appliesSet, a) {
			c.fail(r.Path, row.Line, "requirement %s applies to %q, which is not one of %s", row.ID, a, strings.Join(appliesSet, ", "))
		}
	}
	if len(row.AppliesTo) > 1 && slices.Contains(row.AppliesTo, "all") {
		c.fail(r.Path, row.Line, "requirement %s lists `all` with other values", row.ID)
	}
}

// checkCell validates `<matrix>: <row> × <column>` against the file's declared matrices.
func (c *checker) checkCell(r *Registry, line int, cell string) bool {
	m, row, col, ok := parseCell(cell)
	if !ok {
		c.fail(r.Path, line, "matrix cell %q is not `<matrix>: <row> × <column>`", cell)
		return false
	}
	decl, ok := r.Matrices[m]
	if !ok {
		c.fail(r.Path, line, "matrix cell %q names matrix %q, which this file does not declare", cell, m)
		return false
	}
	if decl.Rows != nil && !slices.Contains(decl.Rows, row) {
		c.fail(r.Path, line, "matrix cell %q names row %q, which %s does not declare", cell, row, m)
		return false
	}
	if !slices.Contains(decl.Cols, col) {
		c.fail(r.Path, line, "matrix cell %q names column %q; %s declares %s", cell, col, m, strings.Join(decl.Cols, ", "))
		return false
	}
	return true
}

func parseCell(cell string) (matrix, row, col string, ok bool) {
	matrix, rest, ok := strings.Cut(cell, ":")
	if !ok {
		return "", "", "", false
	}
	row, col, ok = strings.Cut(rest, "×")
	matrix, row, col = strings.TrimSpace(matrix), strings.TrimSpace(row), strings.TrimSpace(col)
	return matrix, row, col, ok && matrix != "" && row != "" && col != ""
}

func normCell(cell string) string {
	m, row, col, _ := parseCell(cell)
	return m + "\x00" + row + "\x00" + col
}

// checkTestRef is check 1 for one Tests-cell entry.
func (c *checker) checkTestRef(r *Registry, line int, raw string) {
	ref, err := ParseTestRef(raw)
	if err != nil {
		c.fail(r.Path, line, "%v", err)
		return
	}
	c.resolve(r, line, ref, true)
}

func (c *checker) resolve(r *Registry, line int, ref TestRef, claim bool) {
	abs := filepath.Join(c.root, filepath.FromSlash(ref.Dir))
	if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
		c.fail(r.Path, line, "test %q: directory %s does not exist", ref.Raw, ref.Dir)
		return
	}
	if ref.Home == HomeGolden {
		return
	}
	tree, err := c.tree(ref.Dir, ref.Home == HomeConformance)
	if err != nil {
		c.fail(r.Path, line, "test %q: %v", ref.Raw, err)
		return
	}
	path := ref.Path
	var node *testNode
	if ref.Home == HomeConformance {
		node = tree[conformanceRoot]
		if node == nil {
			c.fail(r.Path, line, "test %q: the suite in %s has no %s function", ref.Raw, ref.Dir, conformanceRoot)
			return
		}
	} else {
		node = tree[path[0]]
		if node == nil {
			c.fail(r.Path, line, "test %q: %s has no test function %s", ref.Raw, ref.Dir, path[0])
			return
		}
		path = path[1:]
	}
	for i, seg := range path {
		next := node.lookup(seg)
		if next == nil {
			where := strings.Join(ref.Path[:len(ref.Path)-len(path)+i], "/")
			if where == "" {
				where = conformanceRoot
			}
			if node.computed {
				c.fail(r.Path, line, "test %q: subtest names under %s are computed, so %q cannot be verified; name the case with a string literal or a table field", ref.Raw, where, seg)
			} else {
				c.fail(r.Path, line, "test %q: %s has no subtest %q", ref.Raw, where, seg)
			}
			return
		}
		node = next
	}
	if !node.run {
		c.fail(r.Path, line, "test %q names only part of a subtest name containing \"/\", which go test never reports; name the whole subtest", ref.Raw)
		return
	}
	if claim {
		c.recordClaim(ref)
	}
}

func (c *checker) tree(dir string, allFiles bool) (testTree, error) {
	if c.treeErrs == nil {
		c.treeErrs = map[string]error{}
	}
	if t, ok := c.trees[dir]; ok {
		return t, c.treeErrs[dir]
	}
	t, err := loadTestTree(filepath.Join(c.root, filepath.FromSlash(dir)), allFiles)
	c.trees[dir], c.treeErrs[dir] = t, err
	return t, err
}

func (c *checker) recordClaim(ref TestRef) {
	if c.claimed == nil {
		c.claimed = map[string]map[string][][]string{}
	}
	top := conformanceRoot
	if ref.Home != HomeConformance {
		top = ref.Path[0]
	}
	if c.claimed[ref.Dir] == nil {
		c.claimed[ref.Dir] = map[string][][]string{}
	}
	c.claimed[ref.Dir][top] = append(c.claimed[ref.Dir][top], ref.Path)
}

// checkCoverage applies the ratchet (CONFORMANCE.md §5.4): in a complete file, owned sections
// with no requirement, matrices with no rows and empty matrix cells fail; while seeding they are
// notes. Unclaimed tests in a complete file are checked after every file (coverTests).
func (c *checker) checkCoverage(r *Registry, claimed map[string]int) {
	if r.Seeding == SeedingNotStarted {
		return
	}
	report := c.note
	if r.Seeding == SeedingComplete {
		report = c.fail
	}
	c.coverSections(r, report)
	c.coverCells(r, claimed, report)
}

// coverSections: an owned section with no owned descendant needs a requirement at or below it.
func (c *checker) coverSections(r *Registry, report func(string, int, string, ...any)) {
	for _, key := range uncoveredSections(c.own, r) {
		s, _ := c.prd.Section(key)
		report(r.Path, 0, "owned PRD §%s %s has no requirement rows", key, s.Title)
	}
}

// coverCells: every declared matrix has at least one row, and each of its rows (the ones the
// declaration lists, or else every row a claim or an n/a names) fills every declared column.
func (c *checker) coverCells(r *Registry, claimed map[string]int, report func(string, int, string, ...any)) {
	na := map[string]bool{}
	rows := map[string][]string{}
	addRow := func(cell string) {
		if m, row, _, ok := parseCell(cell); ok && !slices.Contains(rows[m], row) {
			rows[m] = append(rows[m], row)
		}
	}
	for _, n := range r.NA {
		na[normCell(n.Cell)] = true
		addRow(n.Cell)
	}
	for _, row := range r.Rows {
		addRow(row.Cell)
	}
	for _, name := range sortedKeys(r.Matrices) {
		decl := r.Matrices[name]
		if decl.Rows != nil {
			rows[name] = decl.Rows
		}
		if len(rows[name]) == 0 {
			report(r.Path, decl.Line, "matrix %s has no rows; seed it, or remove the declaration if the module has no such matrix", name)
			continue
		}
		for _, row := range rows[name] {
			for _, col := range decl.Cols {
				key := name + "\x00" + row + "\x00" + col
				if _, ok := claimed[key]; !ok && !na[key] {
					report(r.Path, decl.Line, "matrix %s: cell %s × %s is empty; claim it with a requirement or declare it n/a", name, row, col)
				}
			}
		}
	}
}

// coverTests is check 2 for unit tests: every top-level test in the module's packages is claimed
// by a row or listed as unclaimed.
func (c *checker) coverTests(r *Registry) {
	unclaimed := map[string]bool{}
	for _, u := range r.Unclaimed {
		dir, name, _ := strings.Cut(u.Test, ":")
		unclaimed[strings.TrimSpace(dir)+"\x00"+strings.TrimSpace(name)] = true
	}
	for _, pkg := range r.Packages {
		tree, err := c.tree(pkg, false)
		if err != nil {
			c.fail(r.Path, 0, "package %s: %v", pkg, err)
			continue
		}
		for _, name := range sortedKeys(tree) {
			if !strings.HasPrefix(name, "Test") || name == "TestMain" {
				continue
			}
			if len(c.claimed[pkg][name]) == 0 && !unclaimed[pkg+"\x00"+name] {
				c.fail(r.Path, 0, "test %s: %s is claimed by no requirement and not listed as unclaimed", pkg, name)
			}
		}
	}
}

// checkUniqueIDs: requirement and gap IDs are unique across every registry file.
func (c *checker) checkUniqueIDs() {
	seen := map[string]string{}
	add := func(r *Registry, id string, line int) {
		if prev, dup := seen[id]; dup {
			c.fail(r.Path, line, "id %s is also used at %s", id, prev)
			return
		}
		seen[id] = fmt.Sprintf("%s:%d", r.Path, line)
	}
	for _, r := range c.regs {
		for _, row := range r.Rows {
			add(r, row.ID, row.Line)
		}
		for _, g := range r.Gaps {
			add(r, g.ID, g.Line)
		}
	}
}

// checkConformanceOrphans is check 2 for the suite: every behavior subtest (two or more levels
// under Run) is claimed by a requirement. It runs once the suite exists.
func (c *checker) checkConformanceOrphans() {
	abs := filepath.Join(c.root, filepath.FromSlash(ConformanceSuiteDir))
	if _, err := os.Stat(abs); err != nil {
		return
	}
	tree, err := c.tree(ConformanceSuiteDir, true)
	if err != nil {
		c.fail(ConformanceSuiteDir, 0, "%v", err)
		return
	}
	root := tree[conformanceRoot]
	if root == nil {
		c.fail(ConformanceSuiteDir, 0, "the suite has no %s function", conformanceRoot)
		return
	}
	claims := c.claimed[ConformanceSuiteDir][conformanceRoot]
	var visit func(n *testNode, path []string)
	visit = func(n *testNode, path []string) {
		if n.computed {
			where := strings.Join(path, "/")
			if where == "" {
				where = conformanceRoot
			}
			c.fail(ConformanceSuiteDir, 0, "subtest names under %s are computed, so they cannot be claimed; name each behavior with a string literal", where)
		}
		if len(n.children) == 0 && len(path) >= 2 {
			if !slices.ContainsFunc(claims, func(p []string) bool { return len(p) >= 2 && hasPrefixPath(path, p) }) {
				c.fail(ConformanceSuiteDir, 0, "conformance subtest %s is claimed by no requirement", strings.Join(path, "/"))
			}
			return
		}
		for _, ch := range n.children {
			visit(ch, append(slices.Clone(path), ch.name))
		}
	}
	visit(root, nil)
}

// checkUnlistedPackages: every directory holding _test.go files belongs to a registry file's
// packages, or is listed as unregistered in the ownership map, so no module's tests escape the
// orphan check. testdata trees and hidden directories are skipped.
func (c *checker) checkUnlistedPackages() error {
	listed := map[string]bool{}
	for _, r := range c.regs {
		for _, p := range r.Packages {
			listed[p] = true
		}
	}
	for _, p := range c.own.Unregistered {
		listed[p] = true
	}
	var unlisted []string
	err := filepath.WalkDir(c.root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != c.root && (d.Name() == "testdata" || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(c.root, filepath.Dir(path))
		if err != nil {
			return fmt.Errorf("relative path of %s: %w", path, err)
		}
		rel = filepath.ToSlash(rel)
		if !listed[rel] && !slices.Contains(unlisted, rel) {
			unlisted = append(unlisted, rel)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("listing test packages: %w", err)
	}
	for _, dir := range unlisted {
		c.fail(c.own.Path, 0, "%s has tests but no registry file lists it in `<!-- packages: … -->`; add it, or list it as `<!-- unregistered: … -->` here", dir)
	}
	return nil
}

// hasPrefixPath reports whether claim names path or one of its ancestors, matching segments the
// way `go test` reports them.
func hasPrefixPath(path, claim []string) bool {
	if len(claim) > len(path) {
		return false
	}
	for i, seg := range claim {
		if seg != path[i] && seg != goTestName(path[i]) {
			return false
		}
	}
	return true
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// Rehash rewrites the reviewed hash of every registry anchor, or of the listed sections only,
// to the PRD's current text. It acknowledges a review: run it after checking the rows under a
// changed section still hold. It returns the anchors it changed.
func Rehash(root string, sections []string) ([]string, error) {
	prd, err := LoadPRD(filepath.Join(root, PRDPath))
	if err != nil {
		return nil, err
	}
	regs, _, _, err := loadRegistry(root)
	if err != nil {
		return nil, err
	}
	anchored := map[string]bool{}
	for _, r := range regs {
		for _, a := range r.Anchors {
			anchored[a.Key] = true
		}
	}
	for _, k := range sections {
		if !anchored[k] {
			return nil, fmt.Errorf("§%s names no registry anchor; nothing to rehash", k)
		}
	}
	var changed []string
	for _, r := range regs {
		c, err := rehashFile(root, r, prd, sections)
		if err != nil {
			return nil, err
		}
		changed = append(changed, c...)
	}
	return changed, nil
}

// rehashFile rewrites the stale anchors of one registry file and returns the ones it changed.
func rehashFile(root string, r *Registry, prd *PRD, sections []string) ([]string, error) {
	path := filepath.Join(root, filepath.FromSlash(r.Path))
	fi, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", r.Path, err)
	}
	data, err := os.ReadFile(path) //nolint:gosec // a registry file under docs/conformance, found by glob
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", r.Path, err)
	}
	lines := strings.Split(string(data), "\n")
	var changed []string
	for _, a := range r.Anchors {
		if len(sections) > 0 && !slices.Contains(sections, a.Key) {
			continue
		}
		s, ok := prd.Section(a.Key)
		if !ok || s.Hash == a.Hash {
			continue
		}
		lines[a.Line-1] = fmt.Sprintf("<!-- prd: %s sha256: %s -->", a.Key, s.Hash)
		changed = append(changed, fmt.Sprintf("%s:%d §%s", r.Path, a.Line, a.Key))
	}
	if len(changed) == 0 {
		return nil, nil
	}
	//nolint:gosec // rewrites the registry file it just read, found by glob under docs/conformance
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), fi.Mode().Perm()); err != nil {
		return nil, fmt.Errorf("writing %s: %w", r.Path, err)
	}
	return changed, nil
}
