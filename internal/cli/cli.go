// Package cli parses arguments and dispatches subcommands. It is designed
// to run in-process (tests call Run directly with fake stdio) and owns the
// exit-code contract: 0 ok, 1 gate breach, 2 usage error, 3 runtime error.
package cli

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/JaydenCJ/gramleak/internal/version"
)

// Exit codes. Scripts can rely on these.
const (
	ExitOK      = 0 // success (report printed, gate passed or unset)
	ExitGate    = 1 // --fail-over breached
	ExitUsage   = 2 // bad flags / arguments
	ExitRuntime = 3 // I/O error, corrupt index, malformed dataset
)

const usageText = `gramleak — n-gram contamination checker for eval sets

Usage:
  gramleak index  --out FILE [flags] <corpus-path>...   build a .glx shingle index
  gramleak check  --index FILE | --against PATH [flags] <eval-path>...
                                                        measure eval contamination
  gramleak stats  <index.glx>                           inspect an index file
  gramleak version                                      print the version

Inputs may be files, directories (walked recursively, dot-entries skipped)
or .jsonl/.ndjson datasets (--field picks the text field, dotted paths ok).
Run "gramleak <command> -h" for the command's flags.

Exit codes: 0 ok · 1 fail-over breached · 2 usage error · 3 runtime error
`

// Run executes the CLI and returns the process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usageText)
		return ExitUsage
	}
	switch args[0] {
	case "index":
		return runIndex(args[1:], stdout, stderr)
	case "check":
		return runCheck(args[1:], stdout, stderr)
	case "stats":
		return runStats(args[1:], stdout, stderr)
	case "version", "--version", "-v":
		fmt.Fprintf(stdout, "gramleak %s\n", version.Version)
		return ExitOK
	case "help", "--help", "-h":
		fmt.Fprint(stdout, usageText)
		return ExitOK
	default:
		fmt.Fprintf(stderr, "gramleak: unknown command %q\n\n", args[0])
		fmt.Fprint(stderr, usageText)
		return ExitUsage
	}
}

// newFlagSet builds a FlagSet that reports errors to stderr and never
// calls os.Exit, keeping Run testable in-process.
func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

// setFlags returns the names of flags explicitly provided on the command
// line, used to reject parameter overrides that conflict with --index.
func setFlags(fs *flag.FlagSet) map[string]bool {
	set := make(map[string]bool)
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	return set
}

// multiFlag is a repeatable string flag (--against a --against b).
type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func usageError(stderr io.Writer, format string, a ...any) int {
	fmt.Fprintf(stderr, "gramleak: "+format+"\n", a...)
	return ExitUsage
}

func runtimeError(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "gramleak: %v\n", err)
	return ExitRuntime
}
