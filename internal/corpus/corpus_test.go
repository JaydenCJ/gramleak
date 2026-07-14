// Tests for the document readers: split modes, deterministic directory
// walks, binary sniffing and the skip accounting that keeps a narrowed
// check visible to the user.
package corpus

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// write creates a file under dir, making parents as needed.
func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func load(t *testing.T, paths []string, opt Options) ([]Doc, Stats) {
	t.Helper()
	docs, st, err := Load(paths, opt)
	if err != nil {
		t.Fatal(err)
	}
	return docs, st
}

func ids(docs []Doc) []string {
	out := make([]string, len(docs))
	for i, d := range docs {
		out[i] = d.ID
	}
	return out
}

func TestFileModeWholeFileIsOneDoc(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "a.txt", "hello world\nsecond line\n")
	docs, st := load(t, []string{p}, Options{})
	if len(docs) != 1 || docs[0].ID != p || docs[0].Text != "hello world\nsecond line\n" {
		t.Fatalf("got %+v", docs)
	}
	if st.Files != 1 || st.Docs != 1 {
		t.Fatalf("stats %+v", st)
	}
	// A whitespace-only file yields no document and is counted as skipped.
	empty := write(t, dir, "empty.txt", "  \n\t\n")
	docs, st = load(t, []string{empty}, Options{})
	if len(docs) != 0 || st.EmptySkipped != 1 {
		t.Fatalf("docs=%v stats=%+v", docs, st)
	}
}

func TestLineModeOneDocPerNonEmptyLine(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "qs.txt", "first question\n\nsecond question\nthird\n")
	docs, _ := load(t, []string{p}, Options{Split: "line"})
	// Blank line 2 is skipped but numbering stays 1-based on file lines.
	want := []string{p + ":1", p + ":3", p + ":4"}
	if !reflect.DeepEqual(ids(docs), want) {
		t.Fatalf("ids %v, want %v", ids(docs), want)
	}
	if docs[1].Text != "second question" {
		t.Fatalf("text %q", docs[1].Text)
	}
}

func TestLineModeCRLFAndUnterminatedTail(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "crlf.txt", "one\r\ntwo\r\n")
	docs, _ := load(t, []string{p}, Options{Split: "line"})
	if docs[0].Text != "one" || docs[1].Text != "two" {
		t.Fatalf("CR leaked into docs: %+v", docs)
	}
	p = write(t, dir, "tail.txt", "one\ntwo")
	docs, _ = load(t, []string{p}, Options{Split: "line"})
	if len(docs) != 2 || docs[1].Text != "two" {
		t.Fatalf("final unterminated line lost: %+v", docs)
	}
}

func TestParaModeSplitsOnBlankLines(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "essay.txt", "para one\nstill one\n\npara two\n\n\npara three\n")
	docs, _ := load(t, []string{p}, Options{Split: "para"})
	want := []string{p + "#1", p + "#2", p + "#3"}
	if !reflect.DeepEqual(ids(docs), want) {
		t.Fatalf("ids %v", ids(docs))
	}
	if docs[0].Text != "para one\nstill one" {
		t.Fatalf("para text %q", docs[0].Text)
	}
	// CRLF endings and space-bearing "blank" lines still separate blocks.
	p = write(t, dir, "crlf-essay.txt", "a b c\r\n \r\nd e f\r\n")
	docs, _ = load(t, []string{p}, Options{Split: "para"})
	if len(docs) != 2 {
		t.Fatalf("want 2 paragraphs, got %+v", docs)
	}
}

func TestJSONLDefaultTextField(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "data.jsonl", `{"text":"alpha beta"}
{"text":"gamma delta"}
`)
	docs, _ := load(t, []string{p}, Options{})
	want := []string{p + ":1", p + ":2"}
	if !reflect.DeepEqual(ids(docs), want) || docs[1].Text != "gamma delta" {
		t.Fatalf("docs %+v", docs)
	}
	// .ndjson gets the same treatment as .jsonl.
	nd := write(t, dir, "data.ndjson", `{"text":"hello"}`+"\n")
	docs, _ = load(t, []string{nd}, Options{})
	if len(docs) != 1 || docs[0].Text != "hello" {
		t.Fatalf("ndjson docs %+v", docs)
	}
}

func TestJSONLCustomAndDottedFieldPaths(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "qa.jsonl", `{"question":"why?","answer":"because"}`+"\n")
	docs, _ := load(t, []string{p}, Options{Field: "question"})
	if len(docs) != 1 || docs[0].Text != "why?" {
		t.Fatalf("docs %+v", docs)
	}
	p = write(t, dir, "nested.jsonl", `{"data":{"prompt":{"text":"deep value"}}}`+"\n")
	docs, _ = load(t, []string{p}, Options{Field: "data.prompt.text"})
	if len(docs) != 1 || docs[0].Text != "deep value" {
		t.Fatalf("dotted path docs %+v", docs)
	}
}

func TestJSONLMissingFieldErrorsWithLocation(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "bad.jsonl", `{"text":"ok"}
{"other":"no text here"}
`)
	_, _, err := Load([]string{p}, Options{})
	if err == nil {
		t.Fatal("missing field must be a hard error, not a silent skip")
	}
	if want := p + ":2"; !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q should cite %q", err, want)
	}
}

func TestJSONLMalformedRecordsError(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "num.jsonl", `{"text":42}`+"\n")
	_, _, err := Load([]string{p}, Options{})
	if err == nil || !strings.Contains(err.Error(), "not a string") {
		t.Fatalf("non-string field: got %v", err)
	}
	p = write(t, dir, "broken.jsonl", "{oops\n")
	_, _, err = Load([]string{p}, Options{})
	if err == nil || !strings.Contains(err.Error(), "invalid JSON") {
		t.Fatalf("broken JSON: got %v", err)
	}
}

func TestJSONLBlankLinesAndEmptyText(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "gaps.jsonl", `{"text":"one"}

{"text":"  "}
{"text":"two"}
`)
	docs, st := load(t, []string{p}, Options{})
	if len(docs) != 2 || st.EmptySkipped != 1 {
		t.Fatalf("docs=%d stats=%+v", len(docs), st)
	}
	if docs[1].ID != p+":4" {
		t.Fatalf("line numbering drifted: %v", ids(docs))
	}
}

func TestDirectoryWalkIsRecursiveAndSorted(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "b.txt", "bee")
	write(t, dir, "a.txt", "ay")
	write(t, dir, "sub/c.txt", "sea")
	docs, _ := load(t, []string{dir}, Options{})
	want := []string{
		filepath.Join(dir, "a.txt"),
		filepath.Join(dir, "b.txt"),
		filepath.Join(dir, "sub", "c.txt"),
	}
	if !reflect.DeepEqual(ids(docs), want) {
		t.Fatalf("walk order %v, want %v", ids(docs), want)
	}
}

func TestDirectoryWalkSkipsDotEntries(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "keep.txt", "kept")
	write(t, dir, ".hidden.txt", "secret")
	write(t, dir, ".git/objects/x.txt", "internal")
	docs, _ := load(t, []string{dir}, Options{})
	if len(docs) != 1 || docs[0].Text != "kept" {
		t.Fatalf("dot entries leaked: %v", ids(docs))
	}
	// Explicitly naming a hidden file overrides the walk-time skip.
	docs, _ = load(t, []string{filepath.Join(dir, ".hidden.txt")}, Options{})
	if len(docs) != 1 {
		t.Fatal("explicitly named file must not be hidden-skipped")
	}
}

func TestBinaryFileIsSkippedAndCounted(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "blob.bin", "PK\x00\x03garbage")
	write(t, dir, "ok.txt", "fine")
	docs, st := load(t, []string{dir}, Options{})
	if len(docs) != 1 || st.BinarySkipped != 1 {
		t.Fatalf("docs=%v stats=%+v", ids(docs), st)
	}
}

func TestMultipleRootsKeepGivenOrder(t *testing.T) {
	dir := t.TempDir()
	p1 := write(t, dir, "z.txt", "zed")
	p2 := write(t, dir, "a.txt", "ay")
	docs, _ := load(t, []string{p1, p2}, Options{})
	if !reflect.DeepEqual(ids(docs), []string{p1, p2}) {
		t.Fatalf("root order not preserved: %v", ids(docs))
	}
}

func TestMissingPathErrors(t *testing.T) {
	_, _, err := Load([]string{filepath.Join(t.TempDir(), "nope")}, Options{})
	if err == nil {
		t.Fatal("missing path must error")
	}
}

func TestOptionsValidateRejectsUnknownSplit(t *testing.T) {
	if err := (Options{Split: "sentence"}).Validate(); err == nil {
		t.Fatal("unknown split accepted")
	}
	for _, m := range SplitModes {
		if err := (Options{Split: m}).Validate(); err != nil {
			t.Fatalf("valid mode %q rejected: %v", m, err)
		}
	}
}
