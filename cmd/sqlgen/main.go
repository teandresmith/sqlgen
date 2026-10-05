// Package main is the entry point for the sqlgen CLI binary.
package main

import (
	"os"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/cli"
)

func main() {
	os.Exit(cli.Execute())
}
