// Command reqcheck checks the PRD requirement registry under docs/conformance (make req-check)
// and, with -rehash, records that changed PRD sections were reviewed (make req-rehash).
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/internal/reqcheck"
)

func main() {
	root := flag.String("root", ".", "repository root")
	rehash := flag.Bool("rehash", false, "rewrite registry anchor hashes to the PRD's current text")
	sections := flag.String("sections", "", "comma-separated PRD sections to rehash (default: every anchor)")
	verbose := flag.Bool("v", false, "list coverage notes, not just their count")
	flag.Parse()

	if *rehash {
		var keys []string
		for k := range strings.SplitSeq(*sections, ",") {
			if k = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(k), "§")); k != "" {
				keys = append(keys, k)
			}
		}
		changed, err := reqcheck.Rehash(*root, keys)
		if err != nil {
			fmt.Fprintln(os.Stderr, "req-rehash:", err)
			os.Exit(1)
		}
		for _, c := range changed {
			fmt.Println("  rehashed", c)
		}
		fmt.Printf("  ok   req-rehash    %d anchor(s) updated\n", len(changed))
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
	rows := 0
	for _, r := range res.Registries {
		rows += len(r.Rows)
	}
	if len(errs) > 0 {
		fmt.Println("  FAIL req-check     the requirement registry disagrees with the PRD or the tests:")
		for _, e := range errs {
			fmt.Println("        " + e.String())
		}
		fmt.Println("  see docs/conformance/README.md")
		os.Exit(1)
	}
	fmt.Printf("  ok   req-check     %d registry file(s), %d requirement(s), %d PRD section(s) mapped, %d coverage note(s)\n",
		len(res.Registries), rows, len(res.Ownership.Entries), len(notes))
}
