// Package corpus reads documents out of files, directories and JSONL
// datasets in a deterministic order, yielding one Doc at a time so corpora
// far larger than memory can be streamed into an index builder.
package corpus

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Doc is one logical document: an eval item or a corpus record. ID encodes
// where it came from (path, path:line for JSONL/line mode, path#k for
// paragraph mode) so reports point at the exact offending record.
type Doc struct {
	ID   string
	Text string
}

// Options control how files are turned into documents.
type Options struct {
	// Split applies to plain-text files: "file" (whole file is one doc,
	// the default), "line" (one doc per non-empty line) or "para"
	// (blank-line separated blocks).
	Split string
	// Field is the JSON field holding the text in .jsonl/.ndjson records.
	// Dotted paths descend into nested objects ("data.question").
	// Defaults to "text".
	Field string
}

// Stats reports what a walk saw, so the CLI can surface skipped inputs
// instead of silently narrowing the check.
type Stats struct {
	Files         int // regular files opened
	Docs          int // documents yielded to the callback
	BinarySkipped int // files skipped by the NUL-byte sniff
	EmptySkipped  int // documents dropped for having no text
}

// SplitModes lists the accepted Options.Split values.
var SplitModes = []string{"file", "line", "para"}

func (o Options) withDefaults() Options {
	if o.Split == "" {
		o.Split = "file"
	}
	if o.Field == "" {
		o.Field = "text"
	}
	return o
}

// Validate rejects unknown split modes before any file is touched.
func (o Options) Validate() error {
	o = o.withDefaults()
	for _, m := range SplitModes {
		if o.Split == m {
			return nil
		}
	}
	return fmt.Errorf("unknown --split mode %q (valid: %s)", o.Split, strings.Join(SplitModes, ", "))
}

// Each walks paths in the order given (directories recursively, in lexical
// order, skipping dot-files and dot-directories) and calls fn once per
// document. Explicitly named files are always processed, hidden or not.
// The walk is fully deterministic: same tree, same sequence.
func Each(paths []string, opt Options, fn func(Doc) error) (Stats, error) {
	opt = opt.withDefaults()
	var st Stats
	for _, root := range paths {
		info, err := os.Stat(root)
		if err != nil {
			return st, err
		}
		if !info.IsDir() {
			if err := processFile(root, opt, &st, fn); err != nil {
				return st, err
			}
			continue
		}
		var files []string
		err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			name := d.Name()
			if p != root && strings.HasPrefix(name, ".") {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.Type().IsRegular() {
				files = append(files, p)
			}
			return nil
		})
		if err != nil {
			return st, err
		}
		sort.Strings(files)
		for _, f := range files {
			if err := processFile(f, opt, &st, fn); err != nil {
				return st, err
			}
		}
	}
	return st, nil
}

// Load collects every document into memory. Convenient for eval sets,
// which are small; corpora should prefer Each.
func Load(paths []string, opt Options) ([]Doc, Stats, error) {
	var docs []Doc
	st, err := Each(paths, opt, func(d Doc) error {
		docs = append(docs, d)
		return nil
	})
	return docs, st, err
}

func processFile(path string, opt Options, st *Stats, fn func(Doc) error) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	st.Files++
	if isBinary(data) {
		st.BinarySkipped++
		return nil
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".jsonl" || ext == ".ndjson" {
		return eachJSONL(path, data, opt.Field, st, fn)
	}
	switch opt.Split {
	case "line":
		return eachLine(path, data, st, fn)
	case "para":
		return eachPara(path, data, st, fn)
	default: // "file"
		text := string(data)
		if strings.TrimSpace(text) == "" {
			st.EmptySkipped++
			return nil
		}
		st.Docs++
		return fn(Doc{ID: path, Text: text})
	}
}

func eachLine(path string, data []byte, st *Stats, fn func(Doc) error) error {
	for i, line := range splitLines(data) {
		if strings.TrimSpace(line) == "" {
			continue
		}
		st.Docs++
		if err := fn(Doc{ID: fmt.Sprintf("%s:%d", path, i+1), Text: line}); err != nil {
			return err
		}
	}
	return nil
}

var paraSep = regexp.MustCompile(`\n[ \t]*\n+`)

func eachPara(path string, data []byte, st *Stats, fn func(Doc) error) error {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	k := 0
	for _, block := range paraSep.Split(text, -1) {
		if strings.TrimSpace(block) == "" {
			continue
		}
		k++
		st.Docs++
		if err := fn(Doc{ID: fmt.Sprintf("%s#%d", path, k), Text: block}); err != nil {
			return err
		}
	}
	return nil
}

// splitLines splits on \n and strips a trailing \r per line, tolerating
// files without a final newline.
func splitLines(data []byte) []string {
	s := string(data)
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines
}

// isBinary sniffs the first 8 KiB for a NUL byte, the same heuristic git
// uses. Model weights or archives dropped into a corpus directory are
// skipped and counted rather than shingled as garbage.
func isBinary(data []byte) bool {
	n := len(data)
	if n > 8192 {
		n = 8192
	}
	return bytes.IndexByte(data[:n], 0) >= 0
}
