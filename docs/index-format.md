# The `.glx` index format (GLXI v1)

A `.glx` file is the serialized shingle set of a corpus: every distinct
hashed token n-gram, plus the parameters the hashes were produced with.
It is what `gramleak index` writes and what `gramleak check --index` and
`gramleak stats` read.

Two properties drive the design:

1. **Shareable without sharing the corpus.** Only 64-bit FNV-1a
   fingerprints are stored — never text. A data owner can hand an eval
   team a `.glx` built from proprietary training data; the eval team can
   measure overlap without ever seeing a training token. (Fingerprints
   are not encryption: a party who can guess a candidate n-gram can test
   membership. The format hides content, not membership.)
2. **Fail loudly, never quietly.** A corrupt index that silently loses
   hashes would report "no contamination" — the one wrong answer this
   tool must never give. The format therefore carries a magic, a version,
   a strict-ordering invariant and a trailing checksum, and `Read`
   rejects anything it cannot fully verify (runtime error, exit 3).

## Layout

All integers are little-endian. One header, one payload, one checksum.

| Offset | Size | Field | Notes |
|---|---|---|---|
| 0 | 4 | magic | ASCII `GLXI` |
| 4 | 2 | format version | `1` |
| 6 | 2 | flags | bit 0 `case-sensitive`, bit 1 `mask-digits` |
| 8 | 4 | `n` | shingle size in tokens (1 … 65536) |
| 12 | 8 | corpus documents | statistic only |
| 20 | 8 | corpus tokens | statistic only |
| 28 | 8 | shingle count `k` | |
| 36 | 8·k | hashes | `uint64`, strictly ascending (sorted + deduplicated) |
| 36+8·k | 8 | checksum | FNV-1a 64 over the serialized hash bytes |

## Hashing

Tokens are maximal runs of Unicode letters and digits, case-folded unless
`case-sensitive`, digit runs collapsed to `0` when `mask-digits` is set.
Each window of `n` consecutive tokens is hashed with FNV-1a 64, with a
unit-separator byte (`0x1f`) hashed between tokens so that token
boundaries cannot collide by concatenation (`["ab","c"]` ≠ `["a","bc"]`).

## Compatibility rules

- The parameters live in the file, so `check --index` **refuses**
  command-line overrides of `-n`, `--case-sensitive` or `--mask-digits`
  (usage error, exit 2) — comparing hashes produced under different
  normalization is meaningless.
- Readers reject: wrong magic, any version other than 1, an implausible
  count, non-ascending hashes, truncation, checksum mismatch.
- Writes are atomic (temp file + rename in the target directory), so an
  interrupted `gramleak index` never leaves a half-written file that a
  later run would trust.
- Any future layout change bumps the version; v1 readers will refuse it
  cleanly rather than misread it.

## Size expectations

8 bytes per distinct shingle plus a 44-byte envelope. A corpus of one
million distinct 8-gram shingles is ≈ 7.6 MiB; membership lookup is a
binary search over the sorted array, no hash table rebuild on load.
