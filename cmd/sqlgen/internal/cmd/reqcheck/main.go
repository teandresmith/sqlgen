// Command reqcheck checks the PRD requirement registry under docs/conformance (make req-check)
// and, with -rehash, records that changed PRD sections were reviewed (make req-rehash). With
// -report it joins go test -json results with the registry and writes
// docs/conformance/REPORT.md (make conformance).
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/internal/reqcheck"
)

func main() {
	root := flag.String("root", ".", "repository root")
	rehash := flag.Bool("rehash", false, "rewrite registry anchor hashes to the PRD's current text")
	sections := flag.String("sections", "", "comma-separated PRD sections to rehash (default: every anchor)")
	verbose := flag.Bool("v", false, "list notes (coverage gaps, PRD gaps awaiting a ruling), not just their count")
	report := flag.String("report", "", "directory of go test -json results to join with the registry into docs/conformance/REPORT.md")
	commit := flag.String("commit", "", "the commit the results ran at, recorded in the report header")
	flag.Parse()

	if *report != "" {
		if err := writeReport(*root, *report, *commit); err != nil {
			fmt.Fprintln(os.Stderr, "conformance:", err)
			os.Exit(1)
		}
		return
	}

	if *rehash {
		runRehash(*root, *sections)
		return
	}

	res, err := reqcheck.Check(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "req-check:", err)
		os.Exit(1)
	}
	errs, notes := res.Errors(), res.Notes()
	if *verbose {
		for _, n := range notes {
			fmt.Println("  note", n)
		}
	}
	rows, gaps, open := 0, 0, 0
	for _, r := range res.Registries {
		rows += len(r.Rows)
		gaps += len(r.Gaps)
		for _, g := range r.Gaps {
			if g.Ruling == "" {
				open++
			}
		}
	}
	if len(errs) > 0 {
		fmt.Println("  FAIL req-check     the requirement registry disagrees with the PRD or the tests:")
		for _, e := range errs {
			fmt.Println("        " + e.String())
		}
		fmt.Println("  see docs/conformance/README.md")
		os.Exit(1)
	}
	fmt.Printf("  ok   req-check     %d registry file(s), %d requirement(s), %d PRD gap(s) (%d open), %d PRD section(s) mapped, %d note(s)\n",
		len(res.Registries), rows, gaps, open, len(res.Ownership.Entries), len(notes))
}

func writeReport(root, resultsDir, commit string) error {
	results, err := reqcheck.LoadResults(resultsDir)
	if err != nil {
		return fmt.Errorf("loading results: %w", err)
	}
	rep, err := reqcheck.BuildReport(root, results)
	if err != nil {
		return fmt.Errorf("joining results: %w", err)
	}
	h := reqcheck.Header{Commit: commit, Date: time.Now().UTC().Format(time.DateOnly)}
	if err := reqcheck.WriteReport(root, rep, h); err != nil {
		return fmt.Errorf("writing report: %w", err)
	}
	n := map[reqcheck.Status]int{}
	rows := 0
	for _, f := range rep.Files {
		rows += len(f.Rows)
		for _, r := range f.Rows {
			n[r.Status]++
		}
	}
	fmt.Printf("  ok   conformance   %s/%s: %d requirement(s): %d pass, %d fail, %d skip, %d not run, %d untested\n",
		reqcheck.RegistryDir, reqcheck.ReportFile, rows, n[reqcheck.StatusPass], n[reqcheck.StatusFail],
		n[reqcheck.StatusSkip], n[reqcheck.StatusNotRun], n[reqcheck.StatusUntested])
	return nil
}

func runRehash(root, sections string) {
	var keys []string
	for k := range strings.SplitSeq(sections, ",") {
		if k = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(k), "§")); k != "" {
			keys = append(keys, k)
		}
	}
	changed, err := reqcheck.Rehash(root, keys)
	if err != nil {
		fmt.Fprintln(os.Stderr, "req-rehash:", err)
		os.Exit(1)
	}
	for _, c := range changed {
		fmt.Println("  rehashed", c)
	}
	fmt.Printf("  ok   req-rehash    %d anchor(s) updated\n", len(changed))
}
