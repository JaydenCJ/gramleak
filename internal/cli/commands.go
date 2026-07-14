package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/JaydenCJ/gramleak/internal/check"
	"github.com/JaydenCJ/gramleak/internal/corpus"
	"github.com/JaydenCJ/gramleak/internal/index"
	"github.com/JaydenCJ/gramleak/internal/render"
)

// runIndex builds a .glx index from one or more corpus paths.
func runIndex(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("gramleak index", stderr)
	out := fs.String("out", "", "output index file (required), conventionally *.glx")
	n := fs.Int("n", 8, "shingle size in tokens")
	caseSensitive := fs.Bool("case-sensitive", false, "do not case-fold tokens")
	maskDigits := fs.Bool("mask-digits", false, "collapse digit runs so templated text still matches")
	field := fs.String("field", "text", "JSON field holding the text in .jsonl records (dotted paths ok)")
	split := fs.String("split", "file", "plain-text splitting: file, line or para")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if *out == "" {
		return usageError(stderr, "index: --out FILE is required")
	}
	if fs.NArg() == 0 {
		return usageError(stderr, "index: at least one corpus path is required")
	}
	params := index.Params{N: *n, CaseSensitive: *caseSensitive, MaskDigits: *maskDigits}
	if err := params.Validate(); err != nil {
		return usageError(stderr, "index: %v", err)
	}
	opt := corpus.Options{Split: *split, Field: *field}
	if err := opt.Validate(); err != nil {
		return usageError(stderr, "index: %v", err)
	}

	b := index.NewBuilder(params)
	st, err := corpus.Each(fs.Args(), opt, func(d corpus.Doc) error {
		b.AddText(d.Text)
		return nil
	})
	if err != nil {
		return runtimeError(stderr, err)
	}
	noteSkips(stderr, st)
	if st.Docs == 0 {
		return runtimeError(stderr, fmt.Errorf("no corpus documents found under %s", strings.Join(fs.Args(), ", ")))
	}
	ix := b.Build()
	if err := ix.WriteFile(*out); err != nil {
		return runtimeError(stderr, err)
	}
	info, err := os.Stat(*out)
	if err != nil {
		return runtimeError(stderr, err)
	}
	fmt.Fprintf(stdout, "indexed %d document%s / %d token%s → %d shingle%s (%s)\n",
		ix.Docs, plural(ix.Docs), ix.Tokens, plural(ix.Tokens), ix.Len(), plural(ix.Len()), ix.Params.Describe())
	fmt.Fprintf(stdout, "wrote %s (%d bytes)\n", *out, info.Size())
	return ExitOK
}

// runCheck measures an eval set against an index file or an ad-hoc corpus.
func runCheck(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("gramleak check", stderr)
	indexPath := fs.String("index", "", "read the corpus index from this .glx file")
	var against multiFlag
	fs.Var(&against, "against", "build an in-memory index from this corpus path (repeatable)")
	n := fs.Int("n", 8, "shingle size in tokens (only with --against)")
	caseSensitive := fs.Bool("case-sensitive", false, "do not case-fold tokens (only with --against)")
	maskDigits := fs.Bool("mask-digits", false, "collapse digit runs (only with --against)")
	field := fs.String("field", "text", "JSON field holding the text in eval .jsonl records")
	split := fs.String("split", "file", "eval plain-text splitting: file, line or para")
	corpusField := fs.String("corpus-field", "", "JSON field for --against corpora (defaults to --field)")
	corpusSplit := fs.String("corpus-split", "", "splitting for --against corpora (defaults to --split)")
	threshold := fs.Float64("threshold", 5.0, "flag documents at or above this contamination percent")
	failOver := fs.Float64("fail-over", -1, "exit 1 when any document reaches this percent (unset: report only)")
	top := fs.Int("top", 3, "evidence spans shown per document")
	minTokens := fs.Int("min-tokens", 0, "skip eval documents shorter than this many tokens (default: the shingle size n)")
	format := fs.String("format", "text", "output format: text, json or markdown")
	all := fs.Bool("all", false, "list every document, not only flagged ones")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if (*indexPath == "") == (len(against) == 0) {
		return usageError(stderr, "check: exactly one of --index or --against is required")
	}
	if fs.NArg() == 0 {
		return usageError(stderr, "check: at least one eval path is required")
	}
	switch *format {
	case "text", "json", "markdown":
	default:
		return usageError(stderr, "check: unknown --format %q (valid: text, json, markdown)", *format)
	}
	evalOpt := corpus.Options{Split: *split, Field: *field}
	if err := evalOpt.Validate(); err != nil {
		return usageError(stderr, "check: %v", err)
	}

	// Load or build the index.
	var ix *index.Index
	var indexLabel string
	if *indexPath != "" {
		// Shingling parameters live in the index file; overriding them on
		// the command line would silently compare apples to oranges.
		set := setFlags(fs)
		for _, name := range []string{"n", "case-sensitive", "mask-digits", "corpus-field", "corpus-split"} {
			if set[name] {
				return usageError(stderr, "check: --%s conflicts with --index (parameters come from the index file)", name)
			}
		}
		var err error
		ix, err = index.ReadFile(*indexPath)
		if err != nil {
			return runtimeError(stderr, err)
		}
		indexLabel = *indexPath
	} else {
		params := index.Params{N: *n, CaseSensitive: *caseSensitive, MaskDigits: *maskDigits}
		if err := params.Validate(); err != nil {
			return usageError(stderr, "check: %v", err)
		}
		copt := corpus.Options{Split: *corpusSplit, Field: *corpusField}
		if copt.Split == "" {
			copt.Split = evalOpt.Split
		}
		if copt.Field == "" {
			copt.Field = evalOpt.Field
		}
		if err := copt.Validate(); err != nil {
			return usageError(stderr, "check: %v", err)
		}
		b := index.NewBuilder(params)
		st, err := corpus.Each(against, copt, func(d corpus.Doc) error {
			b.AddText(d.Text)
			return nil
		})
		if err != nil {
			return runtimeError(stderr, err)
		}
		noteSkips(stderr, st)
		if st.Docs == 0 {
			return runtimeError(stderr, fmt.Errorf("no corpus documents found under %s", strings.Join(against, ", ")))
		}
		ix = b.Build()
		indexLabel = "in-memory (" + strings.Join(against, ", ") + ")"
	}

	// Stream the eval set through the checker.
	checker := check.New(ix, check.Options{
		ThresholdPct: *threshold,
		MinTokens:    *minTokens,
		TopSpans:     *top,
	})
	st, err := corpus.Each(fs.Args(), evalOpt, func(d corpus.Doc) error {
		checker.Add(d)
		return nil
	})
	if err != nil {
		return runtimeError(stderr, err)
	}
	noteSkips(stderr, st)
	if st.Docs == 0 {
		return runtimeError(stderr, fmt.Errorf("no eval documents found under %s", strings.Join(fs.Args(), ", ")))
	}
	sum := checker.Finish()

	meta := render.Meta{
		EvalLabel:  strings.Join(fs.Args(), ", "),
		IndexLabel: indexLabel,
		Index:      ix,
		ShowAll:    *all,
	}
	switch *format {
	case "json":
		if err := render.JSON(stdout, sum, meta); err != nil {
			return runtimeError(stderr, err)
		}
	case "markdown":
		render.Markdown(stdout, sum, meta)
	default:
		render.Text(stdout, sum, meta)
	}

	// Gate: breach exits 1. The verdict goes to stderr for machine formats
	// so stdout stays parseable.
	if *failOver >= 0 {
		gateOut := stdout
		if *format != "text" {
			gateOut = stderr
		}
		if sum.MaxPct >= *failOver {
			fmt.Fprintf(gateOut, "gate: max contamination %.1f%% ≥ fail-over %.1f%% → FAIL\n", sum.MaxPct, *failOver)
			return ExitGate
		}
		fmt.Fprintf(gateOut, "gate: max contamination %.1f%% < fail-over %.1f%% → ok\n", sum.MaxPct, *failOver)
	}
	return ExitOK
}

// runStats prints what an index file contains without needing the corpus.
func runStats(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("gramleak stats", stderr)
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if fs.NArg() != 1 {
		return usageError(stderr, "stats: exactly one index file is required")
	}
	path := fs.Arg(0)
	ix, err := index.ReadFile(path)
	if err != nil {
		return runtimeError(stderr, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return runtimeError(stderr, err)
	}
	caseDesc := "case-folded"
	if ix.Params.CaseSensitive {
		caseDesc = "case-sensitive"
	}
	digitDesc := "verbatim"
	if ix.Params.MaskDigits {
		digitDesc = "masked"
	}
	fmt.Fprintf(stdout, "gramleak index %s\n", path)
	fmt.Fprintf(stdout, "  format     GLXI v1\n")
	fmt.Fprintf(stdout, "  shingles   %d\n", ix.Len())
	fmt.Fprintf(stdout, "  n          %d\n", ix.Params.N)
	fmt.Fprintf(stdout, "  case       %s\n", caseDesc)
	fmt.Fprintf(stdout, "  digits     %s\n", digitDesc)
	fmt.Fprintf(stdout, "  corpus     %d document%s / %d token%s\n", ix.Docs, plural(ix.Docs), ix.Tokens, plural(ix.Tokens))
	fmt.Fprintf(stdout, "  file size  %d bytes\n", info.Size())
	return ExitOK
}

// noteSkips surfaces skipped inputs on stderr so a narrowing check is
// never silent.
func noteSkips(stderr io.Writer, st corpus.Stats) {
	if st.BinarySkipped > 0 {
		fmt.Fprintf(stderr, "note: skipped %d binary file%s\n", st.BinarySkipped, plural(st.BinarySkipped))
	}
	if st.EmptySkipped > 0 {
		fmt.Fprintf(stderr, "note: skipped %d empty document%s\n", st.EmptySkipped, plural(st.EmptySkipped))
	}
}

// plural returns "s" unless n is exactly 1, so status lines never print
// "1 documents".
func plural[T ~int | ~int64](n T) string {
	if n == 1 {
		return ""
	}
	return "s"
}
