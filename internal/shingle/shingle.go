// Package shingle hashes fixed-length token n-grams ("shingles") into
// 64-bit FNV-1a fingerprints. An explicit unit-separator byte is hashed
// between tokens so that ["ab","c"] and ["a","bc"] never collide by
// concatenation. Only fingerprints are ever stored or compared — the
// corpus text itself never leaves the machine and is not reconstructable
// from an index.
package shingle

const (
	// sep is US (unit separator, 0x1f): it cannot occur inside a token
	// because the tokenizer only emits letters and digits.
	sep = 0x1f

	offset64 = 14695981039346656037
	prime64  = 1099511628211
)

// Hash returns the FNV-1a fingerprint of a token sequence.
func Hash(tokens []string) uint64 {
	var h uint64 = offset64
	for i, t := range tokens {
		if i > 0 {
			h ^= sep
			h *= prime64
		}
		for j := 0; j < len(t); j++ {
			h ^= uint64(t[j])
			h *= prime64
		}
	}
	return h
}

// Hashes returns one fingerprint per n-token window of tokens, in order:
// element i is the hash of tokens[i : i+n]. It returns nil when the
// document is shorter than one full window or n is not positive.
func Hashes(tokens []string, n int) []uint64 {
	if n <= 0 || len(tokens) < n {
		return nil
	}
	out := make([]uint64, 0, len(tokens)-n+1)
	for i := 0; i+n <= len(tokens); i++ {
		out = append(out, Hash(tokens[i:i+n]))
	}
	return out
}
