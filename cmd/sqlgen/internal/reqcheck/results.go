package reqcheck

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// RepoModule is the module path of the repository root. Every module in the repository, the
// example and conformance modules included, has the path RepoModule + "/" + its directory.
const RepoModule = "github.com/teandresmith/sqlgen"

// harnessMarker starts the line the conformance suite logs to name the database it runs on.
const harnessMarker = "conformance harness: database "

// Results holds the outcomes of `go test -json` runs, keyed by package import path.
type Results struct {
	Runs []Run
	pkgs map[string]*pkgResult
}

// Run summarizes one `go test -json` output file.
type Run struct {
	Name     string // the file name without .json
	Packages int
	Passed   int
	Failed   int // failed tests, a failed build, or a test binary that stopped early
	NoTests  int
}

type pkgResult struct {
	action      string // pass, fail or skip (no test files); empty while the package has not finished
	buildFailed bool
	tests       map[string]testResult
	output      map[string][]string // log lines per test, framing lines included
	database    string              // what the conformance harness logged
}

type testResult struct {
	action string // pass, fail or skip
	msg    string // a skip's message
}

// testEvent is one line of `go test -json` output (go doc test2json).
type testEvent struct {
	Action      string
	Package     string
	Test        string
	Output      string
	FailedBuild string
}

// NewResults returns an empty result set.
func NewResults() *Results {
	return &Results{pkgs: map[string]*pkgResult{}}
}

// LoadResults reads every *.json file in dir, in name order.
func LoadResults(dir string) (*Results, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, fmt.Errorf("listing test results: %w", err)
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no go test -json results (*.json) in %s", dir)
	}
	slices.Sort(matches)
	res := NewResults()
	for _, m := range matches {
		f, err := os.Open(m) //nolint:gosec // a results file make conformance wrote, found by glob
		if err != nil {
			return nil, fmt.Errorf("reading test results: %w", err)
		}
		err = res.Add(strings.TrimSuffix(filepath.Base(m), ".json"), f)
		_ = f.Close()
		if err != nil {
			return nil, err
		}
	}
	return res, nil
}

// Add reads one `go test -json` stream as the run called name.
func (r *Results) Add(name string, rd io.Reader) error {
	br := bufio.NewReader(rd)
	var seen []string
	for lineNo := 1; ; lineNo++ {
		line, err := br.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var ev testEvent
			if jerr := json.Unmarshal(line, &ev); jerr != nil {
				return fmt.Errorf("test results %s line %d is not a go test -json event: %w", name, lineNo, jerr)
			}
			if ev.Package != "" && !slices.Contains(seen, ev.Package) {
				seen = append(seen, ev.Package)
			}
			r.event(ev)
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("reading test results %s: %w", name, err)
		}
	}
	run := Run{Name: name, Packages: len(seen)}
	for _, pkg := range seen {
		switch r.pkgs[pkg].action {
		case "pass":
			run.Passed++
		case "skip":
			run.NoTests++
		default:
			run.Failed++
		}
	}
	r.Runs = append(r.Runs, run)
	slices.SortFunc(r.Runs, func(a, b Run) int { return strings.Compare(a.Name, b.Name) })
	return nil
}

func (r *Results) event(ev testEvent) {
	if ev.Package == "" { // build-output events name an ImportPath, not a Package
		return
	}
	p := r.pkgs[ev.Package]
	if p == nil {
		p = &pkgResult{tests: map[string]testResult{}, output: map[string][]string{}}
		r.pkgs[ev.Package] = p
	}
	switch ev.Action {
	case "output":
		if ev.Test != "" {
			p.output[ev.Test] = append(p.output[ev.Test], ev.Output)
		}
		if _, db, ok := strings.Cut(ev.Output, harnessMarker); ok {
			p.database = strings.TrimSpace(db)
		}
	case "pass", "fail", "skip":
		if ev.Test == "" {
			p.action = ev.Action
			p.buildFailed = p.buildFailed || ev.FailedBuild != ""
			return
		}
		tr := testResult{action: ev.Action}
		if ev.Action == "skip" {
			tr.msg = skipMessage(p.output[ev.Test])
		}
		p.tests[ev.Test] = tr
	}
}

var logPrefixRE = regexp.MustCompile(`^\s*\S+\.go:\d+: ?`)

// skipMessage returns the t.Skip message: the last line the test logged with a file:line prefix,
// joined with its continuation lines, which `go test` indents deeper than the prefixed line.
// Shallower lines are process output (a container's log, a cleanup's stderr), not the message.
func skipMessage(output []string) string {
	var after []string // non-frame lines after the candidate, nearest first
	for _, line := range slices.Backward(output) {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "=== ") || strings.HasPrefix(trimmed, "--- ") {
			after = nil
			continue
		}
		loc := logPrefixRE.FindStringIndex(line)
		if loc == nil {
			if trimmed != "" {
				after = append([]string{line}, after...)
			}
			continue
		}
		depth := indent(line)
		parts := []string{strings.TrimSpace(line[loc[1]:])}
		for _, c := range after {
			if indent(c) <= depth {
				break
			}
			parts = append(parts, strings.TrimSpace(c))
		}
		return strings.TrimSpace(strings.Join(parts, " "))
	}
	return ""
}

func indent(line string) int {
	return len(line) - len(strings.TrimLeft(line, " \t"))
}

// database returns what the conformance harness in the package at dir logged as its database.
func (r *Results) database(dir string) string {
	if p := r.pkgs[importPath(dir)]; p != nil {
		return p.database
	}
	return ""
}

// outcome returns the result of one test, named as `go test` reports it, in the package at dir.
// A test with no result of its own is not run, never a pass; the message says why.
func (r *Results) outcome(dir, test string) Outcome {
	p := r.pkgs[importPath(dir)]
	if p == nil {
		return Outcome{Status: StatusNotRun, Msg: "its package is not in the results"}
	}
	if tr, ok := p.tests[test]; ok {
		switch tr.action {
		case "pass":
			return Outcome{Status: StatusPass}
		case "skip":
			return Outcome{Status: StatusSkip, Msg: tr.msg}
		default:
			return Outcome{Status: StatusFail}
		}
	}
	if p.buildFailed {
		return Outcome{Status: StatusNotRun, Msg: "its package failed to build"}
	}
	for anc := parentTest(test); anc != ""; anc = parentTest(anc) {
		tr, ok := p.tests[anc]
		if !ok {
			continue
		}
		switch tr.action {
		case "skip":
			return Outcome{Status: StatusNotRun, Msg: fmt.Sprintf("%s skipped: %s", anc, tr.msg)}
		case "fail":
			return Outcome{Status: StatusNotRun, Msg: anc + " failed before it ran"}
		}
		break
	}
	if p.action == "fail" {
		return Outcome{Status: StatusNotRun, Msg: "its package failed before it ran"}
	}
	return Outcome{Status: StatusNotRun, Msg: "no result"}
}

func parentTest(name string) string {
	i := strings.LastIndex(name, "/")
	if i < 0 {
		return ""
	}
	return name[:i]
}

func importPath(dir string) string {
	if dir == "." || dir == "" {
		return RepoModule
	}
	return RepoModule + "/" + dir
}

// goTestName returns a test name as `go test` reports it: spaces become underscores.
func goTestName(seg string) string {
	return strings.ReplaceAll(seg, " ", "_")
}
