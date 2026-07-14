// Command gramleak is the CLI entry point: it delegates everything to the
// in-process, testable internal/cli package.
package main

import (
	"os"

	"github.com/JaydenCJ/gramleak/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
