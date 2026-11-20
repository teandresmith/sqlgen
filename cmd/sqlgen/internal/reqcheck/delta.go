package reqcheck

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ReportRows is a written REPORT.md read back: each requirement row's status. PRD-gap pinning
// tests are not rows.
type ReportRows struct {
	// Rows is keyed by requirement ID, which is unique across the registry.
	Rows map[string]ReportRow
	// IDs lists the rows in report order.
	IDs []string
}

// ReportRow is one requirement row of a report.
type ReportRow struct {
	File   string
	Status Status
}

// Totals counts the rows per status.
func (r *ReportRows) Totals() map[Status]int {
	n := map[Status]int{}
	for _, row := range r.Rows {
		n[row.Status]++
	}
	return n
}

// ParseReport reads the requirement statuses back from a report Markdown renders. A row's status
// is its most severe cell, raised by its line in the failing and not-run list: a row whose
// conformance tests apply to no target shows only `—` cells there. Each file's derived totals
// must match its Modules line, so a change to the rendering fails loudly here instead of
// misreading rows.
func ParseReport(md string) (*ReportRows, error) {
	r := &ReportRows{Rows: map[string]ReportRow{}}
	modules := map[string][]int{}
	attention := map[string]Status{}
	var section, file string
	seen := false
	for i, line := range strings.Split(md, "\n") {
		switch {
		case strings.HasPrefix(line, "## "):
			section, file = strings.TrimPrefix(line, "## "), ""
			seen = seen || section == "Requirements"
			continue
		case strings.HasPrefix(line, "### "):
			file = strings.TrimPrefix(line, "### ")
			continue
		case !strings.HasPrefix(line, "| ") || strings.HasPrefix(line, "|---"):
			continue
		}
		if err := r.parseTableLine(section, file, splitRow(line), modules, attention); err != nil {
			return nil, fmt.Errorf("report line %d: %w", i+1, err)
		}
	}
	if !seen {
		return nil, errors.New("report has no Requirements section")
	}
	for id, s := range attention {
		if row, ok := r.Rows[id]; ok {
			row.Status = max(row.Status, s)
			r.Rows[id] = row
		}
	}
	return r, r.checkTotals(modules)
}

// parseTableLine reads one table line of a report section; the sections not listed carry no
// row status.
func (r *ReportRows) parseTableLine(section, file string, cells []string, modules map[string][]int, attention map[string]Status) error {
	switch section {
	case "Modules":
		return parseModuleLine(cells, modules)
	case "Failing and not-run rows":
		if len(cells) != 4 || cells[0] == "File" {
			return nil
		}
		s, err := parseStatusCell(cells[3])
		if err != nil {
			return err
		}
		attention[cells[1]] = max(attention[cells[1]], s)
	case "Requirements":
		return r.parseRequirement(file, cells)
	}
	return nil
}

func parseModuleLine(cells []string, modules map[string][]int) error {
	if len(cells) != 8 || cells[0] == "File" {
		return nil
	}
	n := make([]int, 6)
	for j := range n {
		v, err := strconv.Atoi(cells[j+2])
		if err != nil {
			return fmt.Errorf("modules line for %s: %w", cells[0], err)
		}
		n[j] = v
	}
	modules[cells[0]] = n
	return nil
}

func (r *ReportRows) parseRequirement(file string, cells []string) error {
	if len(cells) < 3 || cells[0] == "Section" {
		return nil
	}
	if file == "" {
		return fmt.Errorf("requirement row %s outside a registry file heading", cells[1])
	}
	id := cells[1]
	if prev, ok := r.Rows[id]; ok {
		return fmt.Errorf("requirement %s appears in %s and %s", id, prev.File, file)
	}
	row := ReportRow{File: file, Status: StatusNA}
	for _, c := range cells[2:] {
		s, err := parseStatusCell(c)
		if err != nil {
			return fmt.Errorf("requirement %s: %w", id, err)
		}
		row.Status = max(row.Status, s)
	}
	r.Rows[id] = row
	r.IDs = append(r.IDs, id)
	return nil
}

// checkTotals compares each file's rows with its Modules line: rows, then pass, fail, skip,
// not run and untested.
func (r *ReportRows) checkTotals(modules map[string][]int) error {
	got := map[string][]int{}
	for _, row := range r.Rows {
		n := got[row.File]
		if n == nil {
			n = make([]int, 6)
			got[row.File] = n
		}
		n[0]++
		for j, s := range []Status{StatusPass, StatusFail, StatusSkip, StatusNotRun, StatusUntested} {
			if row.Status == s {
				n[j+1]++
			}
		}
	}
	for f, want := range modules {
		n := got[f]
		if n == nil {
			n = make([]int, 6)
		}
		if fmt.Sprint(n) != fmt.Sprint(want) {
			return fmt.Errorf("%s: rows read back as %v (rows, pass, fail, skip, not run, untested), but its Modules line says %v", f, n, want)
		}
	}
	for f := range got {
		if _, ok := modules[f]; !ok {
			return fmt.Errorf("%s has requirement rows but no Modules line", f)
		}
	}
	return nil
}

// parseStatusCell reads the status cellText rendered, dropping a skip's or not-run's reason.
func parseStatusCell(c string) (Status, error) {
	word, _, _ := strings.Cut(strings.Trim(c, "*"), ": ")
	for _, s := range []Status{StatusNA, StatusPass, StatusSkip, StatusUntested, StatusNotRun, StatusFail} {
		if word == s.String() {
			return s, nil
		}
	}
	return 0, fmt.Errorf("unknown status cell %q", c)
}

// RowChange is a requirement whose status differs between two reports, or that is in only one.
type RowChange struct {
	ID, File       string
	From, To       Status
	InBase, InHead bool
}

// Regressed reports whether the row left pass: it now has another status, or is gone.
func (c RowChange) Regressed() bool {
	return c.InBase && c.From == StatusPass && (!c.InHead || c.To != StatusPass)
}

// ReportDelta compares two reports.
type ReportDelta struct {
	Base, Head map[Status]int
	// Changed lists the rows that changed status, appeared or disappeared: base order, then
	// rows only in head.
	Changed []RowChange
}

// Regressions lists the changed rows that left pass.
func (d ReportDelta) Regressions() []RowChange {
	var out []RowChange
	for _, c := range d.Changed {
		if c.Regressed() {
			out = append(out, c)
		}
	}
	return out
}

// DiffReports compares head with base row by row.
func DiffReports(base, head *ReportRows) ReportDelta {
	d := ReportDelta{Base: base.Totals(), Head: head.Totals()}
	for _, id := range base.IDs {
		b := base.Rows[id]
		h, ok := head.Rows[id]
		if ok && h.Status == b.Status {
			continue
		}
		c := RowChange{ID: id, File: b.File, From: b.Status, InBase: true, InHead: ok}
		if ok {
			c.File, c.To = h.File, h.Status
		}
		d.Changed = append(d.Changed, c)
	}
	for _, id := range head.IDs {
		if _, ok := base.Rows[id]; !ok {
			h := head.Rows[id]
			d.Changed = append(d.Changed, RowChange{ID: id, File: h.File, To: h.Status, InHead: true})
		}
	}
	return d
}
