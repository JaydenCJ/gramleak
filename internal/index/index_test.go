// Tests for the shingle index: builder semantics, membership queries, and
// the GLXI binary format's round-trip and corruption defenses. A checker
// that trusts a corrupt index reports "no contamination" — the worst
// possible failure — so every guard here is load-bearing.
package index

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func buildIndex(t *testing.T, p Params, docs ...[]string) *Index {
	t.Helper()
	b := NewBuilder(p)
	for _, d := range docs {
		b.AddDoc(d)
	}
	return b.Build()
}

func TestBuilderDedupeAndAccounting(t *testing.T) {
	ix := buildIndex(t, Params{N: 2},
		[]string{"a", "b", "c"},
		[]string{"a", "b", "c"}) // identical doc adds nothing new
	if ix.Len() != 2 { // windows: [a b], [b c]
		t.Fatalf("want 2 distinct shingles, got %d", ix.Len())
	}
	ix = buildIndex(t, Params{N: 4},
		[]string{"a", "b", "c"}, // shorter than one window: still counted
		[]string{"a", "b", "c", "d", "e"})
	if ix.Docs != 2 || ix.Tokens != 8 {
		t.Fatalf("docs=%d tokens=%d", ix.Docs, ix.Tokens)
	}
}

func TestAddTextTokenizesWithParams(t *testing.T) {
	b := NewBuilder(Params{N: 2})
	b.AddText("Hello, WORLD again")
	ix := b.Build()
	if ix.Tokens != 3 || ix.Len() != 2 {
		t.Fatalf("tokens=%d shingles=%d", ix.Tokens, ix.Len())
	}
}

func TestContainsFindsEveryInsertedShingle(t *testing.T) {
	toks := strings.Fields("one two three four five six seven")
	ix := buildIndex(t, Params{N: 3}, toks)
	b := NewBuilder(Params{N: 3})
	b.AddDoc(toks)
	probe := b.Build()
	for _, h := range probe.hashes {
		if !ix.Contains(h) {
			t.Fatalf("missing shingle %d", h)
		}
	}
}

func TestContainsRejectsAbsentHash(t *testing.T) {
	ix := buildIndex(t, Params{N: 2}, []string{"a", "b", "c"})
	if ix.Contains(0xdeadbeef) {
		t.Fatal("absent hash reported present")
	}
	empty := buildIndex(t, Params{N: 8})
	if empty.Len() != 0 || empty.Contains(42) {
		t.Fatal("empty index misbehaved")
	}
}

func TestRoundTripPreservesEverything(t *testing.T) {
	orig := buildIndex(t, Params{N: 5},
		strings.Fields("the quick brown fox jumps over the lazy dog"),
		strings.Fields("pack my box with five dozen liquor jugs"))
	var buf bytes.Buffer
	if err := orig.Write(&buf); err != nil {
		t.Fatal(err)
	}
	got, err := Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if got.Params != orig.Params || got.Docs != orig.Docs || got.Tokens != orig.Tokens {
		t.Fatalf("metadata mismatch: %+v vs %+v", got, orig)
	}
	if !bytes.Equal(u64s(got.hashes), u64s(orig.hashes)) {
		t.Fatal("hash payload mismatch after round trip")
	}
	// The parameter flags travel too: a mismatch would silently compare
	// differently-normalized shingles.
	flagged := buildIndex(t, Params{N: 3, CaseSensitive: true, MaskDigits: true},
		strings.Fields("a b c d"))
	buf.Reset()
	if err := flagged.Write(&buf); err != nil {
		t.Fatal(err)
	}
	got, err = Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Params.CaseSensitive || !got.Params.MaskDigits || got.Params.N != 3 {
		t.Fatalf("params lost: %+v", got.Params)
	}
}

func TestReadRejectsBadMagicAndFutureVersion(t *testing.T) {
	_, err := Read(bytes.NewReader(bytes.Repeat([]byte{'x'}, 64)))
	if !errors.Is(err, ErrNotIndex) {
		t.Fatalf("want ErrNotIndex, got %v", err)
	}
	var buf bytes.Buffer
	if err := buildIndex(t, Params{N: 2}, []string{"a", "b"}).Write(&buf); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	binary.LittleEndian.PutUint16(data[4:6], 99)
	_, err = Read(bytes.NewReader(data))
	if err == nil || !strings.Contains(err.Error(), "version 99") {
		t.Fatalf("want version error, got %v", err)
	}
}

func TestReadRejectsTruncatedPayload(t *testing.T) {
	var buf bytes.Buffer
	ix := buildIndex(t, Params{N: 2}, strings.Fields("a b c d e f"))
	if err := ix.Write(&buf); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	_, err := Read(bytes.NewReader(data[:len(data)-9])) // lose checksum + 1
	if err == nil || !strings.Contains(err.Error(), "truncated") {
		t.Fatalf("want truncation error, got %v", err)
	}
}

func TestReadRejectsBitFlip(t *testing.T) {
	var buf bytes.Buffer
	ix := buildIndex(t, Params{N: 2}, strings.Fields("a b c d e f"))
	if err := ix.Write(&buf); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	data[40] ^= 0x01 // flip one payload bit
	_, err := Read(bytes.NewReader(data))
	if err == nil {
		t.Fatal("bit-flipped index was accepted")
	}
}

func TestReadRejectsUnsortedHashes(t *testing.T) {
	// Craft a stream whose checksum is valid but whose hashes are out of
	// order: the ordering check must catch it independently.
	data := craftStream(t, 2, []uint64{9, 3})
	_, err := Read(bytes.NewReader(data))
	if err == nil || !strings.Contains(err.Error(), "ascending") {
		t.Fatalf("want ordering error, got %v", err)
	}
	// Duplicates violate strict ascent the same way.
	data = craftStream(t, 2, []uint64{3, 3})
	_, err = Read(bytes.NewReader(data))
	if err == nil || !strings.Contains(err.Error(), "ascending") {
		t.Fatalf("want ordering error for duplicates, got %v", err)
	}
}

func TestReadRejectsCorruptHeaderN(t *testing.T) {
	data := craftStream(t, 0, nil) // n=0 is unrepresentable in a valid index
	_, err := Read(bytes.NewReader(data))
	if err == nil || !strings.Contains(err.Error(), "corrupt index header") {
		t.Fatalf("want header error, got %v", err)
	}
}

func TestWriteFileReadFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corpus.glx")
	orig := buildIndex(t, Params{N: 4}, strings.Fields("one two three four five"))
	if err := orig.WriteFile(path); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Len() != orig.Len() || got.Params != orig.Params {
		t.Fatal("file round trip lost data")
	}
	// No temp files may survive the atomic rename.
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("stray files after WriteFile: %v", entries)
	}
}

func TestReadFileErrorNamesThePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bogus.glx")
	if err := os.WriteFile(path, bytes.Repeat([]byte{'z'}, 64), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ReadFile(path)
	if err == nil || !strings.Contains(err.Error(), "bogus.glx") {
		t.Fatalf("error should name the file, got %v", err)
	}
}

func TestParamsValidateAndDescribe(t *testing.T) {
	if err := (Params{N: 1}).Validate(); err != nil {
		t.Fatalf("n=1 should be valid: %v", err)
	}
	if err := (Params{N: 0}).Validate(); err == nil {
		t.Fatal("n=0 accepted")
	}
	if err := (Params{N: 1 << 17}).Validate(); err == nil {
		t.Fatal("oversized n accepted")
	}
	if got := (Params{N: 8}).Describe(); got != "n=8, case-folded, digits verbatim" {
		t.Fatalf("got %q", got)
	}
	got := Params{N: 5, CaseSensitive: true, MaskDigits: true}.Describe()
	if got != "n=5, case-sensitive, digits masked" {
		t.Fatalf("got %q", got)
	}
}

// craftStream builds a GLXI v1 byte stream with an arbitrary (possibly
// invalid) hash sequence but a correct checksum, to exercise structural
// validation independently of checksum validation.
func craftStream(t *testing.T, n uint32, hashes []uint64) []byte {
	t.Helper()
	out := []byte(magic)
	out = binary.LittleEndian.AppendUint16(out, formatVersion)
	out = binary.LittleEndian.AppendUint16(out, 0)
	out = binary.LittleEndian.AppendUint32(out, n)
	out = binary.LittleEndian.AppendUint64(out, 1)                   // docs
	out = binary.LittleEndian.AppendUint64(out, 10)                  // tokens
	out = binary.LittleEndian.AppendUint64(out, uint64(len(hashes))) // count
	sum := newChecksum()
	buf := make([]byte, 8)
	for _, h := range hashes {
		binary.LittleEndian.PutUint64(buf, h)
		sum.add(buf)
		out = append(out, buf...)
	}
	out = binary.LittleEndian.AppendUint64(out, sum.value)
	return out
}

func u64s(hs []uint64) []byte {
	out := make([]byte, 0, 8*len(hs))
	for _, h := range hs {
		out = binary.LittleEndian.AppendUint64(out, h)
	}
	return out
}
