// Package index builds and serializes the hashed-shingle set of a corpus.
//
// The on-disk format (".glx", documented in docs/index-format.md) stores
// only sorted 64-bit fingerprints plus the parameters they were produced
// with, so an index can be shared without sharing the corpus text and a
// checker can refuse to compare incompatible parameter sets.
package index

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/JaydenCJ/gramleak/internal/shingle"
	"github.com/JaydenCJ/gramleak/internal/tokenize"
)

// Params are the shingling parameters an index was built with. A check run
// must use identical parameters or every comparison would be meaningless;
// Read/Write persist them so mismatches are detected, not silently wrong.
type Params struct {
	N             int
	CaseSensitive bool
	MaskDigits    bool
}

// Tokenize applies the params' normalization to raw text.
func (p Params) Tokenize(text string) []tokenize.Token {
	return tokenize.Tokenize(text, tokenize.Options{
		CaseSensitive: p.CaseSensitive,
		MaskDigits:    p.MaskDigits,
	})
}

// Describe renders the params for report headers, e.g.
// "n=8, case-folded, digits verbatim".
func (p Params) Describe() string {
	caseDesc := "case-folded"
	if p.CaseSensitive {
		caseDesc = "case-sensitive"
	}
	digitDesc := "digits verbatim"
	if p.MaskDigits {
		digitDesc = "digits masked"
	}
	return fmt.Sprintf("n=%d, %s, %s", p.N, caseDesc, digitDesc)
}

// Validate rejects parameter values the format cannot represent.
func (p Params) Validate() error {
	if p.N < 1 || p.N > 1<<16 {
		return fmt.Errorf("shingle size n must be between 1 and 65536, got %d", p.N)
	}
	return nil
}

// Builder accumulates the deduplicated shingle set of a corpus, one
// document at a time, so arbitrarily many corpus files can be streamed
// through without holding their text in memory.
type Builder struct {
	params Params
	set    map[uint64]struct{}
	docs   int
	tokens int64
}

// NewBuilder returns a Builder for the given parameters. Params must have
// been validated by the caller.
func NewBuilder(p Params) *Builder {
	return &Builder{params: p, set: make(map[uint64]struct{})}
}

// AddDoc shingles one document's normalized tokens into the set. Documents
// shorter than one window still count toward corpus statistics.
func (b *Builder) AddDoc(tokens []string) {
	b.docs++
	b.tokens += int64(len(tokens))
	for _, h := range shingle.Hashes(tokens, b.params.N) {
		b.set[h] = struct{}{}
	}
}

// AddText tokenizes raw text with the builder's params and adds it.
func (b *Builder) AddText(text string) {
	b.AddDoc(tokenize.Texts(b.params.Tokenize(text)))
}

// Build freezes the set into an immutable, queryable Index.
func (b *Builder) Build() *Index {
	hashes := make([]uint64, 0, len(b.set))
	for h := range b.set {
		hashes = append(hashes, h)
	}
	sort.Slice(hashes, func(i, j int) bool { return hashes[i] < hashes[j] })
	return &Index{Params: b.params, Docs: b.docs, Tokens: b.tokens, hashes: hashes}
}

// Index is an immutable, sorted set of shingle fingerprints plus the
// corpus statistics and parameters it was built from.
type Index struct {
	Params Params
	Docs   int   // corpus documents shingled in
	Tokens int64 // corpus tokens seen
	hashes []uint64
}

// Len returns the number of distinct shingles in the index.
func (ix *Index) Len() int { return len(ix.hashes) }

// Contains reports whether fingerprint h is in the index, by binary search.
func (ix *Index) Contains(h uint64) bool {
	i := sort.Search(len(ix.hashes), func(i int) bool { return ix.hashes[i] >= h })
	return i < len(ix.hashes) && ix.hashes[i] == h
}

// --- binary format (GLXI v1) -------------------------------------------

const (
	magic         = "GLXI"
	formatVersion = 1

	flagCaseSensitive = 1 << 0
	flagMaskDigits    = 1 << 1
)

// ErrNotIndex is returned when a file does not start with the GLXI magic.
var ErrNotIndex = errors.New("not a gramleak index (missing GLXI magic)")

// Write serializes the index in GLXI v1 format:
//
//	magic "GLXI" | u16 version | u16 flags | u32 n
//	u64 docs | u64 tokens | u64 count
//	count × u64 hashes (ascending) | u64 FNV-1a checksum of the hash bytes
//
// All integers are little-endian.
func (ix *Index) Write(w io.Writer) error {
	var flags uint16
	if ix.Params.CaseSensitive {
		flags |= flagCaseSensitive
	}
	if ix.Params.MaskDigits {
		flags |= flagMaskDigits
	}
	head := make([]byte, 0, 36)
	head = append(head, magic...)
	head = binary.LittleEndian.AppendUint16(head, formatVersion)
	head = binary.LittleEndian.AppendUint16(head, flags)
	head = binary.LittleEndian.AppendUint32(head, uint32(ix.Params.N))
	head = binary.LittleEndian.AppendUint64(head, uint64(ix.Docs))
	head = binary.LittleEndian.AppendUint64(head, uint64(ix.Tokens))
	head = binary.LittleEndian.AppendUint64(head, uint64(len(ix.hashes)))
	if _, err := w.Write(head); err != nil {
		return err
	}
	sum := newChecksum()
	buf := make([]byte, 8)
	for _, h := range ix.hashes {
		binary.LittleEndian.PutUint64(buf, h)
		sum.add(buf)
		if _, err := w.Write(buf); err != nil {
			return err
		}
	}
	binary.LittleEndian.PutUint64(buf, sum.value)
	_, err := w.Write(buf)
	return err
}

// Read parses a GLXI stream, verifying magic, version, ordering and the
// trailing checksum, so a truncated or bit-flipped index fails loudly
// instead of silently reporting "no contamination".
func Read(r io.Reader) (*Index, error) {
	head := make([]byte, 36)
	if _, err := io.ReadFull(r, head); err != nil {
		return nil, fmt.Errorf("reading index header: %w", err)
	}
	if string(head[:4]) != magic {
		return nil, ErrNotIndex
	}
	ver := binary.LittleEndian.Uint16(head[4:6])
	if ver != formatVersion {
		return nil, fmt.Errorf("unsupported index format version %d (this build reads v%d)", ver, formatVersion)
	}
	flags := binary.LittleEndian.Uint16(head[6:8])
	ix := &Index{
		Params: Params{
			N:             int(binary.LittleEndian.Uint32(head[8:12])),
			CaseSensitive: flags&flagCaseSensitive != 0,
			MaskDigits:    flags&flagMaskDigits != 0,
		},
		Docs:   int(binary.LittleEndian.Uint64(head[12:20])),
		Tokens: int64(binary.LittleEndian.Uint64(head[20:28])),
	}
	if err := ix.Params.Validate(); err != nil {
		return nil, fmt.Errorf("corrupt index header: %w", err)
	}
	count := binary.LittleEndian.Uint64(head[28:36])
	if count > 1<<40 {
		return nil, fmt.Errorf("corrupt index header: implausible shingle count %d", count)
	}
	ix.hashes = make([]uint64, count)
	sum := newChecksum()
	buf := make([]byte, 8)
	var prev uint64
	for i := uint64(0); i < count; i++ {
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, fmt.Errorf("truncated index: %w", err)
		}
		sum.add(buf)
		h := binary.LittleEndian.Uint64(buf)
		if i > 0 && h <= prev {
			return nil, errors.New("corrupt index: hashes not strictly ascending")
		}
		ix.hashes[i] = h
		prev = h
	}
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, fmt.Errorf("truncated index (missing checksum): %w", err)
	}
	if got := binary.LittleEndian.Uint64(buf); got != sum.value {
		return nil, errors.New("corrupt index: checksum mismatch")
	}
	return ix, nil
}

// WriteFile atomically-ish writes the index to path (temp file + rename in
// the same directory), so a crash mid-write never leaves a half index that
// a later check run would trust.
func (ix *Index) WriteFile(path string) error {
	dir, base := splitPath(path)
	tmp, err := os.CreateTemp(dir, "."+base+".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := ix.Write(tmp); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// ReadFile loads and verifies a GLXI index from path.
func ReadFile(path string) (*Index, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	ix, err := Read(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return ix, nil
}

func splitPath(path string) (dir, base string) {
	dir, base = filepath.Split(path)
	if dir == "" {
		dir = "."
	}
	return dir, base
}

// checksum is FNV-1a over the serialized hash bytes.
type checksum struct{ value uint64 }

func newChecksum() *checksum { return &checksum{value: 14695981039346656037} }

func (c *checksum) add(p []byte) {
	for _, b := range p {
		c.value ^= uint64(b)
		c.value *= 1099511628211
	}
}
