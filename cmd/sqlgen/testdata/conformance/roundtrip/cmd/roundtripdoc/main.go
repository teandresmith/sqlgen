// Command roundtripdoc assembles docs/round-trips.md from the results each
// conformance dialect module writes under `make round-trips`.
//
//	roundtripdoc -in <results dir> -out <report path>
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/roundtrip"
)

func main() {
	in := flag.String("in", "", "directory holding the dialect modules' results")
	out := flag.String("out", "", "report to write")
	flag.Parse()
	if *in == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "usage: roundtripdoc -in <results dir> -out <report path>")
		os.Exit(2)
	}
	if err := run(*in, *out); err != nil {
		fmt.Fprintln(os.Stderr, "roundtripdoc:", err)
		os.Exit(1)
	}
}

func run(in, out string) error {
	results, err := roundtrip.ReadResults(in)
	if err != nil {
		return err
	}
	doc, err := roundtrip.Render(roundtrip.Intro, results)
	if err != nil {
		return err
	}
	if err := os.WriteFile(out, []byte(doc), 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s from %d results\n", out, len(results))
	return nil
}
